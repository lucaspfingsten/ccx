"""Read Codex CLI sessions (~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl).

Each rollout line is `{"timestamp", "type", "payload"}`. The first line is a
`session_meta` event carrying `cwd`, `id`, and `git` info. Conversation events
arrive as `response_item` with payload variants:

- `payload.type == "message"` with `role` ∈ {user, assistant, developer} and a
  list `content` of `{type: input_text|output_text, text}` parts.
- `payload.type == "function_call"` with `name` + JSON `arguments`.
- `payload.type == "function_call_output"` (skipped — tool result noise).
- `payload.type == "reasoning"` (skipped — model thinking).
- `payload.type == "custom_tool_call"` / `custom_tool_call_output` — same
  treatment as function calls / outputs.

When Codex auto-compacts (or the user runs `/compact`), it writes a top-level
event of `type == "compacted"` whose `payload.replacement_history` is a list
of curated `{type: "message", role, content}` entries that replace all prior
history. A session can be compacted multiple times — we use the *last* one
as the summary, then emit only response_items that came after it.
"""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any, Iterable, Optional

from .parser import Session, Turn

CODEX_SESSIONS = Path.home() / ".codex" / "sessions"


def _iter_rollouts(root: Path = CODEX_SESSIONS) -> Iterable[Path]:
    if not root.exists():
        return
    for p in root.rglob("rollout-*.jsonl"):
        if p.is_file():
            yield p


def _read_session_meta(jsonl_path: Path) -> dict:
    """Return the first session_meta payload in a rollout file (or {})."""
    try:
        with open(jsonl_path, "r", encoding="utf-8") as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                try:
                    obj = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if obj.get("type") == "session_meta":
                    return obj.get("payload") or {}
                # session_meta is always near the top; bail after a few lines.
                break
    except OSError:
        pass
    return {}


def _first_user_text(jsonl_path: Path, max_lines: int = 200) -> str:
    """Return the first non-empty user message from a rollout, for previews."""
    try:
        with open(jsonl_path, "r", encoding="utf-8") as f:
            for i, line in enumerate(f):
                if i > max_lines:
                    break
                line = line.strip()
                if not line:
                    continue
                try:
                    obj = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if obj.get("type") != "response_item":
                    continue
                p = obj.get("payload") or {}
                if p.get("type") != "message" or p.get("role") != "user":
                    continue
                text = _join_text_parts(p.get("content"))
                if text.strip() and not _is_user_input_noise(text):
                    return text.strip()
    except OSError:
        pass
    return ""


def _join_text_parts(content: Any) -> str:
    if isinstance(content, str):
        return content
    if not isinstance(content, list):
        return ""
    parts: list[str] = []
    for part in content:
        if not isinstance(part, dict):
            continue
        t = part.get("type")
        if t in ("input_text", "output_text"):
            txt = part.get("text") or ""
            if txt:
                parts.append(txt)
    return "\n".join(parts)


def _summarize_function_call(payload: dict) -> str:
    name = payload.get("name") or "?"
    raw = payload.get("arguments")
    args: dict = {}
    if isinstance(raw, str):
        try:
            args = json.loads(raw)
        except json.JSONDecodeError:
            args = {}
    elif isinstance(raw, dict):
        args = raw

    if name == "exec_command":
        cmd = args.get("cmd") or ""
        return f"↳ Bash: {cmd[:80]}" if cmd else "↳ Bash"
    if name == "apply_patch":
        return "↳ Edit"
    if name == "shell":
        cmd = args.get("command") or ""
        if isinstance(cmd, list):
            cmd = " ".join(cmd)
        return f"↳ Shell: {cmd[:80]}" if cmd else "↳ Shell"
    if name == "view":
        return f"↳ Read: {args.get('path', '?')}"
    if name == "update_plan":
        return "↳ UpdatePlan"
    return f"↳ {name}"


def _format_replacement_history(history: list) -> str:
    """Render Codex's replacement_history (a list of curated messages) as a
    role-labeled summary block. Skips developer messages and opaque nested
    compaction entries."""
    parts: list[str] = []
    for entry in history or []:
        if not isinstance(entry, dict):
            continue
        if entry.get("type") != "message":
            continue
        role = entry.get("role")
        if role not in ("user", "assistant"):
            continue
        text = _join_text_parts(entry.get("content"))
        if not text.strip():
            continue
        if role == "user" and _is_user_input_noise(text):
            continue
        label = "User" if role == "user" else "Assistant"
        parts.append(f"**{label}:** {text.strip()}")
    return "\n\n".join(parts)


def _is_user_input_noise(text: str) -> bool:
    """Skip permission preambles and tool-output user messages we don't want."""
    if not text:
        return True
    stripped = text.lstrip()
    if stripped.startswith("<permissions instructions>"):
        return True
    if stripped.startswith("<environment_context>"):
        return True
    if stripped.startswith("<user_instructions>"):
        return True
    return False


def parse(jsonl_path: str | Path, include_tool_calls: bool = True,
          max_turns: Optional[int] = None) -> Session:
    jsonl_path = Path(jsonl_path)
    meta = _read_session_meta(jsonl_path)
    git = meta.get("git") or {}

    turns: list[Turn] = []
    pending_assistant: Optional[Turn] = None
    summary_text: Optional[str] = None
    compact_timestamp: Optional[str] = None
    compact_meta: Optional[dict] = None

    def flush_assistant():
        nonlocal pending_assistant
        if pending_assistant is not None and (
            pending_assistant.text.strip() or pending_assistant.tool_calls
        ):
            turns.append(pending_assistant)
        pending_assistant = None

    with open(jsonl_path, "r", encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                obj = json.loads(line)
            except json.JSONDecodeError:
                continue

            if obj.get("type") == "compacted":
                # Last-wins: a new compaction supersedes everything before.
                flush_assistant()
                turns.clear()
                payload = obj.get("payload") or {}
                summary_text = _format_replacement_history(
                    payload.get("replacement_history") or []
                )
                compact_timestamp = obj.get("timestamp")
                compact_meta = {"trigger": "compacted"}
                continue

            if obj.get("type") != "response_item":
                continue
            p = obj.get("payload") or {}
            ptype = p.get("type")
            ts = obj.get("timestamp", "")

            if ptype == "message":
                role = p.get("role")
                if role == "developer":
                    continue
                text = _join_text_parts(p.get("content"))
                if role == "user":
                    flush_assistant()
                    if _is_user_input_noise(text):
                        continue
                    if not text.strip():
                        continue
                    turns.append(Turn(role="user", text=text, timestamp=ts))
                elif role == "assistant":
                    if pending_assistant is None:
                        pending_assistant = Turn(role="assistant", text="", timestamp=ts)
                    if text.strip():
                        if pending_assistant.text:
                            pending_assistant.text += "\n" + text
                        else:
                            pending_assistant.text = text
            elif ptype in ("function_call", "custom_tool_call"):
                if not include_tool_calls:
                    continue
                if pending_assistant is None:
                    pending_assistant = Turn(role="assistant", text="", timestamp=ts)
                pending_assistant.tool_calls.append(_summarize_function_call(p))
            elif ptype in ("function_call_output", "custom_tool_call_output", "reasoning"):
                continue

    flush_assistant()

    if max_turns is not None and max_turns > 0:
        turns = turns[-max_turns:]

    return Session(
        project_path=meta.get("cwd", "") or "",
        session_id=meta.get("id") or jsonl_path.stem,
        git_branch=(git.get("branch") if isinstance(git, dict) else None),
        summary=summary_text or None,
        compact_meta=compact_meta,
        compact_timestamp=compact_timestamp,
        turns=turns,
    )


def _path_matches(meta_cwd: str, target: Path) -> bool:
    if not meta_cwd:
        return False
    try:
        a = Path(meta_cwd).expanduser().resolve()
    except OSError:
        return False
    return a == target


def resolve(project_path: Optional[str], session_id: Optional[str]
            ) -> tuple[Optional[Path], Optional[str]]:
    """Find a Codex rollout for the given project / session id.

    Returns (path, error_msg). If session_id is given, scans rollouts for a
    matching id regardless of project. Otherwise, returns the most recent
    rollout whose session_meta.cwd resolves to the target path.
    """
    if session_id:
        for p in _iter_rollouts():
            meta = _read_session_meta(p)
            if meta.get("id") == session_id:
                return p, None
        return None, f"No Codex rollout found with id {session_id!r}."

    if project_path:
        target = Path(project_path).expanduser().resolve()
    else:
        target = Path.cwd().resolve()

    candidates: list[tuple[float, Path]] = []
    ancestors = [target] + list(target.parents)
    for p in _iter_rollouts():
        meta = _read_session_meta(p)
        cwd_str = meta.get("cwd") or ""
        if not cwd_str:
            continue
        try:
            cwd_path = Path(cwd_str).expanduser().resolve()
        except OSError:
            continue
        if cwd_path in ancestors:
            candidates.append((p.stat().st_mtime, p))

    if not candidates:
        return None, "No Codex sessions found for this project."
    candidates.sort(reverse=True)
    return candidates[0][1], None


def list_recent(limit: int = 15) -> list[dict]:
    out: list[dict] = []
    for p in _iter_rollouts():
        try:
            stat = p.stat()
        except OSError:
            continue
        meta = _read_session_meta(p)
        cwd = meta.get("cwd") or ""
        sid = meta.get("id") or p.stem
        first = _first_user_text(p)
        out.append({
            "source": "codex",
            "project_name": Path(cwd).name if cwd else p.parent.name,
            "project_path": cwd,
            "session_id": sid,
            "ref": str(p),
            "first_prompt": first,
            "title": "",
            "message_count": 0,
            "git_branch": (meta.get("git") or {}).get("branch") if isinstance(meta.get("git"), dict) else "",
            "size_bytes": stat.st_size,
            "mtime": int(stat.st_mtime * 1000),
        })
    out.sort(key=lambda x: x["mtime"], reverse=True)
    return out[:limit]
