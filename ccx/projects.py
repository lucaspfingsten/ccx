"""Resolve Claude Code project directories and enumerate sessions."""

from __future__ import annotations

import json
import re
from pathlib import Path
from typing import Optional

CLAUDE_PROJECTS = Path.home() / ".claude" / "projects"

_NON_ALNUM = re.compile(r"[^a-zA-Z0-9]")

_PROMPT_NOISE_PREFIXES = (
    "<ide_opened_file>",
    "<ide_selection>",
    "<local-command-",
    "<command-",
)


def _clean_prompt(text: str) -> str:
    text = (text or "").strip()
    if not text:
        return ""
    for pre in _PROMPT_NOISE_PREFIXES:
        if text.startswith(pre):
            return ""
    return text


def project_key_from_path(path: str | Path) -> str:
    """Convert an absolute project path into Claude Code's directory key.

    Claude Code derives the key by replacing every non-alphanumeric character
    with `-`, so `/Users/foo/My App/.config` becomes `-Users-foo-My-App--config`.
    """
    abs_path = str(Path(path).expanduser().resolve())
    return _NON_ALNUM.sub("-", abs_path)


def project_dir_for(path: str | Path) -> Optional[Path]:
    """Return the ~/.claude/projects/ directory for a given project path, or None."""
    key = project_key_from_path(path)
    d = CLAUDE_PROJECTS / key
    return d if d.exists() else None


def find_project_for_cwd(cwd: Optional[Path] = None) -> Optional[Path]:
    """Walk up from cwd to find a matching project dir.
    Useful when running ccx from a subdirectory of the project."""
    if cwd is None:
        cwd = Path.cwd()
    cwd = Path(cwd).expanduser().resolve()
    for p in [cwd] + list(cwd.parents):
        d = project_dir_for(p)
        if d is not None:
            return d
    return None


def latest_session_jsonl(project_dir: str | Path) -> Optional[Path]:
    project_dir = Path(project_dir)
    jsonls = [p for p in project_dir.glob("*.jsonl") if p.is_file()]
    if not jsonls:
        return None
    return max(jsonls, key=lambda p: p.stat().st_mtime)


def session_jsonl(project_dir: str | Path, session_id: str) -> Optional[Path]:
    p = Path(project_dir) / f"{session_id}.jsonl"
    return p if p.exists() else None


def _peek_jsonl_metadata(jsonl_path: Path) -> dict:
    """Scan a JSONL for cwd / sessionId / gitBranch / first user prompt / aiTitle.

    `ai-title` records are appended throughout a session as Claude Code refines
    the title; the *last* one wins. We bail early once we have everything else
    and have seen at least one title, but keep reading otherwise so the title
    we surface is as fresh as possible.
    """
    cwd = ""
    git_branch = ""
    session_id = jsonl_path.stem
    first_prompt = ""
    ai_title = ""
    try:
        with open(jsonl_path, "r", encoding="utf-8") as f:
            for i, line in enumerate(f):
                line = line.strip()
                if not line:
                    continue
                try:
                    obj = json.loads(line)
                except Exception:
                    continue
                if not cwd and "cwd" in obj:
                    cwd = obj.get("cwd") or ""
                    git_branch = obj.get("gitBranch") or git_branch
                    session_id = obj.get("sessionId") or session_id
                if obj.get("type") == "ai-title":
                    t = (obj.get("aiTitle") or "").strip()
                    if t:
                        ai_title = t
                if not first_prompt and obj.get("type") == "user" and not obj.get("isSidechain"):
                    msg = obj.get("message") or {}
                    content = msg.get("content")
                    text = ""
                    if isinstance(content, str):
                        text = _clean_prompt(content)
                    elif isinstance(content, list):
                        for part in content:
                            if isinstance(part, dict) and part.get("type") == "text":
                                cleaned = _clean_prompt(part.get("text") or "")
                                if cleaned:
                                    text = cleaned
                                    break
                    if text:
                        first_prompt = text
                if i > 30 and cwd and first_prompt and ai_title:
                    break
    except OSError:
        pass
    return {
        "cwd": cwd,
        "git_branch": git_branch,
        "session_id": session_id,
        "first_prompt": first_prompt,
        "ai_title": ai_title,
    }


def _extract_latest_ai_title(jsonl_path: Path) -> str:
    """Cheap pass that finds the most recent ai-title in a JSONL.

    Uses a substring filter so the json.loads cost is paid only on the few
    lines that actually carry a title.
    """
    title = ""
    needle = b'"ai-title"'
    try:
        with open(jsonl_path, "rb") as f:
            for raw in f:
                if needle not in raw:
                    continue
                try:
                    obj = json.loads(raw)
                except Exception:
                    continue
                if obj.get("type") == "ai-title":
                    t = (obj.get("aiTitle") or "").strip()
                    if t:
                        title = t
    except OSError:
        pass
    return title


def list_recent_sessions(limit: int = 15) -> list[dict]:
    """Enumerate recent sessions across all projects, newest first.

    Reads each project's sessions-index.json when available; for sessions not
    listed there (e.g. an in-progress one), falls back to peeking the JSONL.
    """
    out: list[dict] = []
    if not CLAUDE_PROJECTS.exists():
        return out

    for project_dir in CLAUDE_PROJECTS.iterdir():
        if not project_dir.is_dir():
            continue

        indexed_paths: set[str] = set()
        idx_file = project_dir / "sessions-index.json"
        if idx_file.exists():
            try:
                idx = json.loads(idx_file.read_text(encoding="utf-8"))
                for e in idx.get("entries") or []:
                    full = e.get("fullPath")
                    if not full:
                        continue
                    full_p = Path(full)
                    if not full_p.exists():
                        continue
                    indexed_paths.add(str(full_p))
                    project_path = e.get("projectPath") or ""
                    project_name = Path(project_path).name if project_path else project_dir.name
                    try:
                        size_bytes = full_p.stat().st_size
                    except OSError:
                        size_bytes = 0
                    out.append(
                        {
                            "project_name": project_name,
                            "project_path": project_path,
                            "session_id": e.get("sessionId") or full_p.stem,
                            "jsonl_path": str(full_p),
                            "first_prompt": _clean_prompt(e.get("firstPrompt") or ""),
                            "title": "",  # filled lazily after sort+limit
                            "message_count": e.get("messageCount", 0),
                            "modified": e.get("modified") or "",
                            "git_branch": e.get("gitBranch") or "",
                            "size_bytes": size_bytes,
                            "mtime": int(
                                e.get("fileMtime") or full_p.stat().st_mtime * 1000
                            ),
                        }
                    )
            except Exception:
                pass

        for j in project_dir.glob("*.jsonl"):
            if not j.is_file():
                continue
            if str(j) in indexed_paths:
                continue
            meta = _peek_jsonl_metadata(j)
            project_path = meta["cwd"]
            project_name = (
                Path(project_path).name if project_path else project_dir.name
            )
            try:
                stat = j.stat()
            except OSError:
                continue
            out.append(
                {
                    "project_name": project_name,
                    "project_path": project_path,
                    "session_id": meta["session_id"],
                    "jsonl_path": str(j),
                    "first_prompt": meta["first_prompt"],
                    "title": meta["ai_title"],
                    "message_count": 0,
                    "modified": "",
                    "git_branch": meta["git_branch"],
                    "size_bytes": stat.st_size,
                    "mtime": int(stat.st_mtime * 1000),
                }
            )

    out.sort(key=lambda x: x["mtime"], reverse=True)
    out = out[:limit]
    for entry in out:
        if not entry.get("title"):
            jp = entry.get("jsonl_path")
            if jp:
                entry["title"] = _extract_latest_ai_title(Path(jp))
    return out
