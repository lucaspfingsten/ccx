"""Render a parsed Session as Markdown or JSON."""

from __future__ import annotations

import json
from datetime import datetime, timezone
from pathlib import Path

from .parser import Session


def _fmt_ts(iso_str: str | None) -> str:
    if not iso_str:
        return ""
    try:
        dt = datetime.fromisoformat(iso_str.replace("Z", "+00:00"))
        return dt.astimezone().strftime("%Y-%m-%d %H:%M")
    except Exception:
        return iso_str


def _project_name(session: Session) -> str:
    if session.project_path:
        return Path(session.project_path).name
    return session.session_id


def render_markdown(session: Session) -> str:
    lines: list[str] = []
    lines.append(f"# Context: {_project_name(session)}")
    if session.project_path:
        lines.append(f"Path: `{session.project_path}`")
    lines.append(f"Session: `{session.session_id}`")
    if session.git_branch:
        lines.append(f"Branch: `{session.git_branch}`")
    if session.compact_meta:
        meta = session.compact_meta
        ts = _fmt_ts(session.compact_timestamp)
        trigger = meta.get("trigger", "?")
        pre = meta.get("preTokens")
        post = meta.get("postTokens")
        if pre and post:
            tok = f"{pre} → {post} tokens"
        elif pre:
            tok = f"{pre} tokens"
        else:
            tok = ""
        bits = [b for b in (ts, trigger, tok) if b]
        lines.append(f"Last compaction: {' · '.join(bits)}")
    lines.append("")

    if session.summary:
        lines.append("## Conversation Summary")
        lines.append("")
        lines.append(session.summary.strip())
        lines.append("")
        lines.append("## Continued Conversation")
        lines.append("")
    else:
        lines.append("## Conversation")
        lines.append("")

    if not session.turns:
        lines.append("_(no turns after the last compaction)_")
        lines.append("")
    else:
        for turn in session.turns:
            heading = "### User" if turn.role == "user" else "### Assistant"
            lines.append(heading)
            if turn.text.strip():
                lines.append(turn.text.strip())
            for tc in turn.tool_calls:
                lines.append(tc)
            lines.append("")

    return "\n".join(lines).rstrip() + "\n"


def render_json(session: Session) -> str:
    obj = {
        "project_path": session.project_path,
        "project_name": _project_name(session),
        "session_id": session.session_id,
        "git_branch": session.git_branch,
        "compaction": None,
        "summary": session.summary,
        "turns": [
            {
                "role": t.role,
                "text": t.text,
                "tool_calls": t.tool_calls,
                "timestamp": t.timestamp,
            }
            for t in session.turns
        ],
    }
    if session.compact_meta:
        obj["compaction"] = {
            "timestamp": session.compact_timestamp,
            **session.compact_meta,
        }
    return json.dumps(obj, indent=2, ensure_ascii=False)
