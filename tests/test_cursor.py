"""Tests for the Cursor IDE source.

Cursor stores chat data in two SQLite databases. The tests build minimal
synthetic SQLite stores in tmp_path and monkeypatch the module's path
constants to point at them, exercising the reader end-to-end.
"""

from __future__ import annotations

import json
import sqlite3
from pathlib import Path

import pytest

from ccx import cursor


def _make_workspace(root: Path, ws_name: str, folder: str,
                    composers: list[dict]) -> Path:
    ws = root / ws_name
    ws.mkdir(parents=True, exist_ok=True)
    (ws / "workspace.json").write_text(
        json.dumps({"folder": f"file://{folder}"}), encoding="utf-8"
    )
    db = ws / "state.vscdb"
    con = sqlite3.connect(db)
    con.execute("create table ItemTable (key text primary key, value text)")
    con.execute(
        "insert into ItemTable values (?, ?)",
        ("composer.composerData", json.dumps({"allComposers": composers})),
    )
    con.commit()
    con.close()
    return ws


def _make_global_db(path: Path, bubbles: list[tuple[str, dict]]) -> None:
    con = sqlite3.connect(path)
    con.execute("create table cursorDiskKV (key text primary key, value blob)")
    for key, payload in bubbles:
        con.execute(
            "insert into cursorDiskKV values (?, ?)",
            (key, json.dumps(payload)),
        )
    con.commit()
    con.close()


@pytest.fixture
def cursor_env(tmp_path, monkeypatch):
    ws_root = tmp_path / "workspaceStorage"
    ws_root.mkdir()
    global_db = tmp_path / "globalStorage" / "state.vscdb"
    global_db.parent.mkdir()

    project_path = str(tmp_path / "my-project")
    Path(project_path).mkdir()

    composers = [
        {
            "composerId": "comp-A",
            "name": "Newer chat",
            "createdAt": 100,
            "lastUpdatedAt": 200,
        },
        {
            "composerId": "comp-B",
            "name": "Older chat",
            "createdAt": 50,
            "lastUpdatedAt": 80,
        },
    ]
    ws = _make_workspace(ws_root, "ws1", project_path, composers)

    bubbles = [
        ("bubbleId:comp-A:b1", {
            "type": 1, "text": "hello cursor",
            "createdAt": "2026-04-29T10:00:00Z",
        }),
        ("bubbleId:comp-A:b2", {
            "type": 2, "text": "Hi there.",
            "createdAt": "2026-04-29T10:00:01Z",
            "toolFormerData": {
                "name": "list_dir",
                "rawArgs": '{"relative_workspace_path":"."}',
            },
        }),
        ("bubbleId:comp-A:b3", {
            "type": 1, "text": "thanks",
            "createdAt": "2026-04-29T10:00:02Z",
        }),
        ("bubbleId:comp-B:b1", {
            "type": 1, "text": "old msg",
            "createdAt": "2026-04-28T09:00:00Z",
        }),
    ]
    _make_global_db(global_db, bubbles)

    monkeypatch.setattr(cursor, "WORKSPACE_STORAGE", ws_root)
    monkeypatch.setattr(cursor, "GLOBAL_DB", global_db)

    return {
        "project_path": project_path,
        "workspace_dir": ws,
        "composer_id": "comp-A",
    }


def test_resolve_picks_most_recent_composer(cursor_env):
    ref, err = cursor.resolve(cursor_env["project_path"], None)
    assert err is None
    assert ref.composer_id == "comp-A"
    assert ref.project_path == cursor_env["project_path"]


def test_resolve_by_session_id(cursor_env):
    ref, err = cursor.resolve(None, "comp-B")
    assert err is None
    assert ref.composer_id == "comp-B"


def test_resolve_unknown_session_id(cursor_env):
    ref, err = cursor.resolve(None, "does-not-exist")
    assert ref is None
    assert err is not None and "does-not-exist" in err


def test_parse_orders_by_created_at_and_summarizes_tool(cursor_env):
    ref, _ = cursor.resolve(cursor_env["project_path"], None)
    s = cursor.parse(ref)
    assert s.session_id == "comp-A"
    assert s.project_path == cursor_env["project_path"]
    assert [t.role for t in s.turns] == ["user", "assistant", "user"]
    assert s.turns[0].text == "hello cursor"
    assert s.turns[1].text == "Hi there."
    assert s.turns[1].tool_calls == ["↳ ListDir: ."]
    assert s.turns[2].text == "thanks"


def test_parse_no_tool_calls(cursor_env):
    ref, _ = cursor.resolve(cursor_env["project_path"], None)
    s = cursor.parse(ref, include_tool_calls=False)
    for t in s.turns:
        assert t.tool_calls == []


def test_list_recent_returns_both_composers(cursor_env):
    out = cursor.list_recent(limit=10)
    assert {e["session_id"] for e in out} == {"comp-A", "comp-B"}
    assert all(e["source"] == "cursor" for e in out)
    # Newer first.
    assert out[0]["session_id"] == "comp-A"


def test_summarize_tool_known_names():
    assert cursor._summarize_tool({
        "name": "read_file",
        "rawArgs": '{"relative_workspace_path":"src/x.py"}',
    }) == "↳ Read: src/x.py"
    assert cursor._summarize_tool({
        "name": "run_terminal_cmd",
        "rawArgs": '{"command":"npm test"}',
    }) == "↳ Bash: npm test"
    assert cursor._summarize_tool({
        "name": "edit_file",
        "rawArgs": '{"target_file":"app.ts"}',
    }) == "↳ Edit: app.ts"
    assert cursor._summarize_tool({
        "name": "weird_tool",
        "rawArgs": "{}",
    }) == "↳ weird_tool"
