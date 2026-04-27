"""Parse Claude Code session JSONL files and extract the post-compaction slice."""

from __future__ import annotations

import json
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Iterable, Optional


@dataclass
class Turn:
    role: str
    text: str
    tool_calls: list[str] = field(default_factory=list)
    timestamp: str = ""


@dataclass
class Session:
    project_path: str
    session_id: str
    git_branch: Optional[str]
    summary: Optional[str]
    compact_meta: Optional[dict]
    compact_timestamp: Optional[str]
    turns: list[Turn]


def load_jsonl(path: str | Path) -> list[dict]:
    events: list[dict] = []
    with open(path, "r", encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                events.append(json.loads(line))
            except json.JSONDecodeError:
                continue
    return events


def extract_text_from_message(message: dict) -> str:
    """Get plain text from a message.content (string or list-of-parts)."""
    content = message.get("content", "")
    if isinstance(content, str):
        return content
    if not isinstance(content, list):
        return ""
    parts: list[str] = []
    for part in content:
        if not isinstance(part, dict):
            continue
        if part.get("type") != "text":
            continue
        text = part.get("text", "") or ""
        if text.startswith("<ide_opened_file>") or text.startswith("<ide_selection>"):
            continue
        parts.append(text)
    return "\n".join(parts)


def find_last_compaction(events: list[dict]) -> tuple[Optional[int], Optional[str], Optional[dict]]:
    """Return (idx, summary_text, compact_metadata) for the last isCompactSummary message,
    or (None, None, None) if the session has never been compacted."""
    summary_idx: Optional[int] = None
    summary_text: Optional[str] = None
    for i in range(len(events) - 1, -1, -1):
        ev = events[i]
        if ev.get("type") == "user" and ev.get("isCompactSummary"):
            summary_idx = i
            summary_text = extract_text_from_message(ev.get("message", {}))
            break

    if summary_idx is None:
        return None, None, None

    compact_meta: Optional[dict] = None
    lower = max(-1, summary_idx - 5)
    for i in range(summary_idx, lower, -1):
        ev = events[i]
        if ev.get("type") == "system" and "compactMetadata" in ev:
            compact_meta = ev.get("compactMetadata")
            break

    return summary_idx, summary_text, compact_meta


def summarize_tool_use(part: dict) -> str:
    name = part.get("name", "?")
    inp = part.get("input") or {}
    if not isinstance(inp, dict):
        inp = {}

    if name == "Bash":
        desc = inp.get("description") or ""
        cmd = inp.get("command") or ""
        if desc:
            return f"↳ Bash: {desc}"
        return f"↳ Bash: {cmd[:80]}"
    if name in ("Read", "Write"):
        return f"↳ {name}: {inp.get('file_path', '?')}"
    if name == "Edit":
        return f"↳ Edit: {inp.get('file_path', '?')}"
    if name == "NotebookEdit":
        return f"↳ NotebookEdit: {inp.get('notebook_path', '?')}"
    if name == "Grep":
        return f"↳ Grep: {inp.get('pattern', '?')}"
    if name == "Glob":
        return f"↳ Glob: {inp.get('pattern', '?')}"
    if name == "WebFetch":
        return f"↳ WebFetch: {inp.get('url', '?')}"
    if name == "WebSearch":
        return f"↳ WebSearch: {inp.get('query', '?')}"
    if name == "TodoWrite":
        n = len(inp.get("todos", []) or [])
        return f"↳ TodoWrite: {n} items"
    if name in ("Task", "Agent"):
        desc = inp.get("description") or ""
        sub = inp.get("subagent_type")
        label = f"{sub}: " if sub else ""
        return f"↳ Agent: {label}{desc}"
    return f"↳ {name}"


def event_to_turn(event: dict, include_tool_calls: bool = True) -> Optional[Turn]:
    et = event.get("type")
    if et not in ("user", "assistant"):
        return None
    if event.get("isSidechain"):
        return None
    if event.get("isCompactSummary"):
        return None

    msg = event.get("message", {}) or {}
    content = msg.get("content", [])

    if et == "user":
        if isinstance(content, list):
            has_real_text = False
            for part in content:
                if not isinstance(part, dict):
                    continue
                if part.get("type") == "text":
                    text = part.get("text", "") or ""
                    if text and not (
                        text.startswith("<ide_opened_file>")
                        or text.startswith("<ide_selection>")
                    ):
                        has_real_text = True
                        break
            if not has_real_text:
                return None
        text = extract_text_from_message(msg)
        if not text.strip():
            return None
        return Turn(role="user", text=text, timestamp=event.get("timestamp", ""))

    text_parts: list[str] = []
    tool_calls: list[str] = []
    if isinstance(content, list):
        for part in content:
            if not isinstance(part, dict):
                continue
            t = part.get("type")
            if t == "text":
                txt = part.get("text", "") or ""
                if txt.strip():
                    text_parts.append(txt)
            elif t == "tool_use" and include_tool_calls:
                tool_calls.append(summarize_tool_use(part))
    elif isinstance(content, str):
        if content.strip():
            text_parts.append(content)

    text = "\n".join(text_parts).strip()
    if not text and not tool_calls:
        return None

    return Turn(
        role="assistant",
        text=text,
        tool_calls=tool_calls,
        timestamp=event.get("timestamp", ""),
    )


def _first_meta(events: Iterable[dict]) -> dict[str, Any]:
    for ev in events:
        if "cwd" in ev:
            return {
                "cwd": ev.get("cwd", ""),
                "sessionId": ev.get("sessionId", ""),
                "gitBranch": ev.get("gitBranch", ""),
            }
    return {"cwd": "", "sessionId": "", "gitBranch": ""}


def parse_session(
    jsonl_path: str | Path,
    include_tool_calls: bool = True,
    max_turns: Optional[int] = None,
) -> Session:
    jsonl_path = Path(jsonl_path)
    events = load_jsonl(jsonl_path)
    meta = _first_meta(events)

    summary_idx, summary_text, compact_meta = find_last_compaction(events)
    compact_timestamp: Optional[str] = None
    if summary_idx is not None:
        compact_timestamp = events[summary_idx].get("timestamp")
        post_events = events[summary_idx + 1 :]
    else:
        post_events = events

    turns: list[Turn] = []
    for ev in post_events:
        turn = event_to_turn(ev, include_tool_calls=include_tool_calls)
        if turn is not None:
            turns.append(turn)

    if max_turns is not None and max_turns > 0:
        turns = turns[-max_turns:]

    return Session(
        project_path=meta["cwd"],
        session_id=meta["sessionId"] or jsonl_path.stem,
        git_branch=meta["gitBranch"] or None,
        summary=summary_text,
        compact_meta=compact_meta,
        compact_timestamp=compact_timestamp,
        turns=turns,
    )
