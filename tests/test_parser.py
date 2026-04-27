from pathlib import Path

from ccx.parser import (
    event_to_turn,
    extract_text_from_message,
    find_last_compaction,
    load_jsonl,
    parse_session,
    summarize_tool_use,
)

FIXTURES = Path(__file__).parent / "fixtures"


def test_load_jsonl_skips_blank_and_malformed(tmp_path):
    p = tmp_path / "x.jsonl"
    p.write_text('{"a":1}\n\n{"b":2}\n{not json}\n{"c":3}\n')
    assert load_jsonl(p) == [{"a": 1}, {"b": 2}, {"c": 3}]


def test_extract_text_from_string_content():
    assert extract_text_from_message({"content": "hello"}) == "hello"


def test_extract_text_from_list_content_skips_ide_and_tool_use():
    msg = {
        "content": [
            {"type": "text", "text": "<ide_opened_file>foo</ide_opened_file>"},
            {"type": "text", "text": "<ide_selection>bar</ide_selection>"},
            {"type": "text", "text": "real content"},
            {"type": "tool_use", "name": "Read"},
        ]
    }
    assert extract_text_from_message(msg) == "real content"


def test_summarize_tool_use_bash_with_description():
    part = {
        "type": "tool_use",
        "name": "Bash",
        "input": {"description": "List files", "command": "ls -la"},
    }
    assert summarize_tool_use(part) == "↳ Bash: List files"


def test_summarize_tool_use_bash_without_description():
    part = {"type": "tool_use", "name": "Bash", "input": {"command": "ls -la"}}
    assert summarize_tool_use(part) == "↳ Bash: ls -la"


def test_summarize_tool_use_read():
    part = {"type": "tool_use", "name": "Read", "input": {"file_path": "/x/y.py"}}
    assert summarize_tool_use(part) == "↳ Read: /x/y.py"


def test_summarize_tool_use_todo_write():
    part = {"type": "tool_use", "name": "TodoWrite", "input": {"todos": [1, 2, 3]}}
    assert summarize_tool_use(part) == "↳ TodoWrite: 3 items"


def test_find_last_compaction_picks_last_of_many():
    events = [
        {"type": "user", "message": {"content": "first"}},
        {"type": "system", "compactMetadata": {"trigger": "auto"}},
        {"type": "user", "isCompactSummary": True, "message": {"content": "first summary"}},
        {"type": "assistant", "message": {"content": [{"type": "text", "text": "reply"}]}},
        {"type": "system", "compactMetadata": {"trigger": "manual", "preTokens": 100}},
        {"type": "user", "isCompactSummary": True, "message": {"content": "second summary"}},
        {"type": "user", "message": {"content": "after"}},
    ]
    idx, summary, meta = find_last_compaction(events)
    assert idx == 5
    assert summary == "second summary"
    assert meta == {"trigger": "manual", "preTokens": 100}


def test_find_last_compaction_no_compaction():
    events = [
        {"type": "user", "message": {"content": "hi"}},
        {"type": "assistant", "message": {"content": [{"type": "text", "text": "hello"}]}},
    ]
    idx, summary, meta = find_last_compaction(events)
    assert idx is None
    assert summary is None
    assert meta is None


def test_event_to_turn_skips_sidechain():
    ev = {
        "type": "user",
        "isSidechain": True,
        "message": {"content": "subagent prompt"},
    }
    assert event_to_turn(ev) is None


def test_event_to_turn_skips_tool_result_only_user():
    ev = {
        "type": "user",
        "isSidechain": False,
        "message": {"content": [{"type": "tool_result", "content": "x"}]},
    }
    assert event_to_turn(ev) is None


def test_event_to_turn_skips_ide_only_user():
    ev = {
        "type": "user",
        "isSidechain": False,
        "message": {
            "content": [
                {"type": "text", "text": "<ide_opened_file>foo</ide_opened_file>"}
            ]
        },
    }
    assert event_to_turn(ev) is None


def test_event_to_turn_assistant_skips_thinking():
    ev = {
        "type": "assistant",
        "isSidechain": False,
        "message": {
            "content": [
                {"type": "thinking", "thinking": "hidden internal"},
                {"type": "text", "text": "visible reply"},
            ]
        },
    }
    turn = event_to_turn(ev)
    assert turn is not None
    assert turn.text == "visible reply"
    assert turn.tool_calls == []


def test_event_to_turn_skips_compact_summary():
    ev = {
        "type": "user",
        "isSidechain": False,
        "isCompactSummary": True,
        "message": {"content": "summary text"},
    }
    assert event_to_turn(ev) is None


def test_parse_session_with_compaction_uses_last_summary():
    s = parse_session(FIXTURES / "with_compaction.jsonl")
    assert s.summary == "final summary block"
    assert s.compact_meta == {
        "trigger": "manual",
        "preTokens": 120000,
        "postTokens": 5000,
        "durationMs": 12345,
    }
    assert s.git_branch == "main"
    # Post-compaction turns: user msg + assistant msg.
    # Sidechain assistant and tool_result-only user are skipped.
    assert len(s.turns) == 2
    assert s.turns[0].role == "user"
    assert s.turns[0].text == "latest user msg"
    assert s.turns[1].role == "assistant"
    assert s.turns[1].text == "latest reply"
    assert s.turns[1].tool_calls == ["↳ Read: /foo.py"]


def test_parse_session_no_compaction():
    s = parse_session(FIXTURES / "no_compaction.jsonl")
    assert s.summary is None
    assert s.compact_meta is None
    assert len(s.turns) == 2
    assert s.turns[0].text == "hello"
    assert s.turns[1].text == "hi there"
    assert s.turns[1].tool_calls == ["↳ Bash: check status"]


def test_parse_session_max_turns_keeps_tail():
    s = parse_session(FIXTURES / "with_compaction.jsonl", max_turns=1)
    assert len(s.turns) == 1
    assert s.turns[0].role == "assistant"


def test_parse_session_no_tool_calls_flag():
    s = parse_session(
        FIXTURES / "with_compaction.jsonl", include_tool_calls=False
    )
    for t in s.turns:
        assert t.tool_calls == []
