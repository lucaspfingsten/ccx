"""Read Cursor IDE chat sessions from its SQLite stores.

Cursor splits chat data across two databases:

- Per-workspace `~/Library/Application Support/Cursor/User/workspaceStorage/<hash>/`
  contains `workspace.json` (mapping the hash → project folder URI) and
  `state.vscdb` (SQLite). Inside `ItemTable`, the key `composer.composerData`
  holds JSON with `allComposers: [{composerId, name, createdAt, lastUpdatedAt, ...}]`.

- Globally, `~/Library/Application Support/Cursor/User/globalStorage/state.vscdb`
  holds a `cursorDiskKV` table where rows keyed `bubbleId:<composerId>:<bubbleId>`
  carry the actual messages: `{type: 1=user|2=assistant, text, createdAt,
  toolFormerData: {name, rawArgs, ...}}`.

Cursor has no compaction concept, so `Session.summary` is always None.
"""

from __future__ import annotations

import json
import os
import sqlite3
import urllib.parse
from dataclasses import dataclass
from pathlib import Path
from typing import Optional

from .parser import Session, Turn

CURSOR_USER = (
    Path.home()
    / "Library"
    / "Application Support"
    / "Cursor"
    / "User"
)
WORKSPACE_STORAGE = CURSOR_USER / "workspaceStorage"
GLOBAL_DB = CURSOR_USER / "globalStorage" / "state.vscdb"


@dataclass
class CursorRef:
    """Opaque session reference: composer id + which workspace it belongs to."""

    composer_id: str
    workspace_dir: Path
    project_path: str


def _folder_uri_to_path(uri: str) -> str:
    if not uri:
        return ""
    if uri.startswith("file://"):
        return urllib.parse.unquote(uri[7:])
    return uri


def _workspace_folder(workspace_dir: Path) -> str:
    wj = workspace_dir / "workspace.json"
    if not wj.exists():
        return ""
    try:
        data = json.loads(wj.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return ""
    return _folder_uri_to_path(data.get("folder") or "")


def _composers_in_workspace(workspace_dir: Path) -> list[dict]:
    db = workspace_dir / "state.vscdb"
    if not db.exists():
        return []
    try:
        con = sqlite3.connect(f"file:{db}?mode=ro", uri=True)
    except sqlite3.Error:
        return []
    try:
        cur = con.cursor()
        row = cur.execute(
            "select value from ItemTable where key='composer.composerData'"
        ).fetchone()
    except sqlite3.Error:
        con.close()
        return []
    con.close()
    if not row:
        return []
    try:
        data = json.loads(row[0])
    except (TypeError, json.JSONDecodeError):
        return []
    composers = data.get("allComposers") if isinstance(data, dict) else None
    if not isinstance(composers, list):
        return []
    return [c for c in composers if isinstance(c, dict) and c.get("composerId")]


def _walk_workspaces() -> list[Path]:
    if not WORKSPACE_STORAGE.exists():
        return []
    return [d for d in WORKSPACE_STORAGE.iterdir() if d.is_dir()]


def _find_workspace_for_path(target: Path) -> Optional[Path]:
    target = target.expanduser().resolve()
    ancestors = [target] + list(target.parents)
    matches: list[tuple[float, Path]] = []
    for ws in _walk_workspaces():
        folder = _workspace_folder(ws)
        if not folder:
            continue
        try:
            folder_path = Path(folder).expanduser().resolve()
        except OSError:
            continue
        if folder_path in ancestors:
            matches.append((ws.stat().st_mtime, ws))
    if not matches:
        return None
    matches.sort(reverse=True)
    return matches[0][1]


def _summarize_tool(former: dict) -> str:
    name = former.get("name") or "?"
    raw = former.get("rawArgs") or former.get("params") or "{}"
    args: dict = {}
    if isinstance(raw, str):
        try:
            args = json.loads(raw)
        except json.JSONDecodeError:
            args = {}
    elif isinstance(raw, dict):
        args = raw

    base = name.split("_v")[0] if "_v" in name else name

    if base in ("read_file", "read"):
        return f"↳ Read: {args.get('relative_workspace_path') or args.get('path') or '?'}"
    if base in ("edit_file", "search_replace", "apply_edit", "write_file"):
        return f"↳ Edit: {args.get('relative_workspace_path') or args.get('target_file') or '?'}"
    if base == "list_dir":
        return f"↳ ListDir: {args.get('relative_workspace_path') or args.get('directoryPath') or '.'}"
    if base in ("run_terminal_cmd", "run_terminal_command", "terminal", "shell"):
        cmd = args.get("command") or args.get("cmd") or ""
        return f"↳ Bash: {cmd[:80]}" if cmd else "↳ Bash"
    if name in ("grep", "grep_search"):
        return f"↳ Grep: {args.get('query') or args.get('pattern') or '?'}"
    if name in ("codebase_search", "semantic_search"):
        return f"↳ Search: {args.get('query', '?')}"
    if name in ("file_search", "glob"):
        return f"↳ Glob: {args.get('query') or args.get('pattern') or '?'}"
    if name in ("web_search",):
        return f"↳ WebSearch: {args.get('query', '?')}"
    return f"↳ {name}"


def _load_bubbles(composer_id: str) -> list[dict]:
    if not GLOBAL_DB.exists():
        return []
    try:
        con = sqlite3.connect(f"file:{GLOBAL_DB}?mode=ro", uri=True)
    except sqlite3.Error:
        return []
    rows: list[dict] = []
    try:
        cur = con.cursor()
        prefix = f"bubbleId:{composer_id}:"
        for r in cur.execute(
            "select value from cursorDiskKV where key like ?",
            (prefix + "%",),
        ):
            val = r[0]
            if isinstance(val, (bytes, bytearray)):
                try:
                    val = val.decode("utf-8")
                except UnicodeDecodeError:
                    continue
            try:
                rows.append(json.loads(val))
            except (TypeError, json.JSONDecodeError):
                continue
    except sqlite3.Error:
        pass
    con.close()
    return rows


def _composer_meta(workspace_dir: Path, composer_id: str) -> dict:
    for c in _composers_in_workspace(workspace_dir):
        if c.get("composerId") == composer_id:
            return c
    return {}


def parse(ref: CursorRef, include_tool_calls: bool = True,
          max_turns: Optional[int] = None) -> Session:
    bubbles = _load_bubbles(ref.composer_id)

    def created(b: dict) -> str:
        return b.get("createdAt") or ""

    bubbles.sort(key=created)

    turns: list[Turn] = []
    for b in bubbles:
        bt = b.get("type")
        text = (b.get("text") or "").strip()
        former = b.get("toolFormerData") or {}
        ts = b.get("createdAt") or ""

        if bt == 1:  # user
            if not text:
                continue
            turns.append(Turn(role="user", text=text, timestamp=ts))
        elif bt == 2:  # assistant
            tool_calls: list[str] = []
            if include_tool_calls and isinstance(former, dict) and former.get("name"):
                tool_calls.append(_summarize_tool(former))
            if not text and not tool_calls:
                continue
            turns.append(Turn(role="assistant", text=text, tool_calls=tool_calls, timestamp=ts))

    if max_turns is not None and max_turns > 0:
        turns = turns[-max_turns:]

    meta = _composer_meta(ref.workspace_dir, ref.composer_id)
    sid = ref.composer_id
    if meta.get("name"):
        # session_id stays the UUID; name is descriptive only
        pass

    return Session(
        project_path=ref.project_path,
        session_id=sid,
        git_branch=None,
        summary=None,
        compact_meta=None,
        compact_timestamp=None,
        turns=turns,
    )


def resolve(project_path: Optional[str], session_id: Optional[str]
            ) -> tuple[Optional[CursorRef], Optional[str]]:
    if session_id:
        # Walk all workspaces looking for this composer id.
        for ws in _walk_workspaces():
            for c in _composers_in_workspace(ws):
                if c.get("composerId") == session_id:
                    return CursorRef(
                        composer_id=session_id,
                        workspace_dir=ws,
                        project_path=_workspace_folder(ws),
                    ), None
        return None, f"No Cursor composer found with id {session_id!r}."

    target = Path(project_path).expanduser().resolve() if project_path else Path.cwd().resolve()
    ws = _find_workspace_for_path(target)
    if ws is None:
        return None, "No Cursor workspace found for this project."

    composers = _composers_in_workspace(ws)
    if not composers:
        return None, f"No Cursor composers in workspace {ws.name}."

    # Prefer most recently updated.
    composers.sort(key=lambda c: c.get("lastUpdatedAt") or 0, reverse=True)
    chosen = composers[0]
    return CursorRef(
        composer_id=chosen["composerId"],
        workspace_dir=ws,
        project_path=_workspace_folder(ws),
    ), None


def list_recent(limit: int = 15) -> list[dict]:
    out: list[dict] = []
    for ws in _walk_workspaces():
        folder = _workspace_folder(ws)
        for c in _composers_in_workspace(ws):
            cid = c.get("composerId") or ""
            updated = c.get("lastUpdatedAt") or 0
            out.append({
                "source": "cursor",
                "project_name": Path(folder).name if folder else ws.name,
                "project_path": folder,
                "session_id": cid,
                "ref": {
                    "composer_id": cid,
                    "workspace_dir": str(ws),
                    "project_path": folder,
                },
                "first_prompt": (c.get("name") or "").strip(),
                "message_count": 0,
                "git_branch": "",
                "mtime": int(updated),
            })
    out.sort(key=lambda x: x["mtime"], reverse=True)
    return out[:limit]


def ref_from_dict(d: dict) -> CursorRef:
    return CursorRef(
        composer_id=d["composer_id"],
        workspace_dir=Path(d["workspace_dir"]),
        project_path=d["project_path"],
    )
