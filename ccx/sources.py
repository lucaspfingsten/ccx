"""Unified interface over per-tool session sources.

Each source module exposes the same three functions:

- ``resolve(project_path, session_id) -> (ref, err)``
- ``parse(ref, include_tool_calls, max_turns) -> Session``
- ``list_recent(limit) -> list[dict]``  (each dict has ``source``, ``ref``, …)

This module wraps Claude Code (the original behavior) in that shape so the CLI
can dispatch uniformly across ``claude``, ``cursor``, and ``codex``.
"""

from __future__ import annotations

from pathlib import Path
from types import SimpleNamespace
from typing import Optional

from . import codex as codex_src
from . import cursor as cursor_src
from .parser import Session, parse_session
from .projects import (
    find_project_for_cwd,
    latest_session_jsonl,
    list_recent_sessions,
    project_dir_for,
    session_jsonl,
)


def _claude_resolve(project_path: Optional[str], session_id: Optional[str]
                    ) -> tuple[Optional[Path], Optional[str]]:
    if project_path:
        project_dir = project_dir_for(project_path)
        if project_dir is None:
            return None, (
                f"No Claude Code session directory for: {project_path}\n"
                "(Looked under ~/.claude/projects/ for the matching key.)"
            )
    else:
        project_dir = find_project_for_cwd()
        if project_dir is None:
            return None, (
                "No Claude Code session for the current directory.\n"
                "Try `ccx --list` to pick from all projects, or pass a path: `ccx <project>`."
            )

    if session_id:
        p = session_jsonl(project_dir, session_id)
        if p is None:
            return None, f"Session {session_id!r} not found in {project_dir}"
        return p, None

    p = latest_session_jsonl(project_dir)
    if p is None:
        return None, f"No .jsonl session files in {project_dir}"
    return p, None


def _claude_parse(ref: Path, include_tool_calls: bool = True,
                  max_turns: Optional[int] = None) -> Session:
    return parse_session(ref, include_tool_calls=include_tool_calls, max_turns=max_turns)


def _claude_list(limit: int) -> list[dict]:
    out = list_recent_sessions(limit=limit)
    for entry in out:
        entry["source"] = "claude"
        entry["ref"] = entry.get("jsonl_path", "")
    return out


claude_src = SimpleNamespace(
    resolve=_claude_resolve,
    parse=_claude_parse,
    list_recent=_claude_list,
)


SOURCES: dict[str, object] = {
    "claude": claude_src,
    "cursor": cursor_src,
    "codex": codex_src,
}


def get(name: str):
    if name not in SOURCES:
        raise KeyError(f"Unknown source: {name}")
    return SOURCES[name]


def list_all(limit: int) -> list[dict]:
    """Aggregate `--list` across every source, newest first."""
    out: list[dict] = []
    for name, src in SOURCES.items():
        try:
            entries = src.list_recent(limit=limit)
        except Exception:
            entries = []
        out.extend(entries)
    out.sort(key=lambda x: x.get("mtime", 0), reverse=True)
    return out[:limit]


def dispatch_parse(entry: dict, include_tool_calls: bool = True,
                   max_turns: Optional[int] = None) -> Session:
    """Parse a list-entry produced by `list_recent` / `list_all`."""
    src_name = entry.get("source", "claude")
    src = get(src_name)
    ref = entry.get("ref")
    if src_name == "cursor" and isinstance(ref, dict):
        ref = cursor_src.ref_from_dict(ref)
    elif src_name in ("claude", "codex"):
        ref = Path(ref) if ref else ref
    return src.parse(ref, include_tool_calls=include_tool_calls, max_turns=max_turns)
