from pathlib import Path

from ccx import codex

FIXTURES = Path(__file__).parent / "fixtures"


def test_parse_codex_basic():
    s = codex.parse(FIXTURES / "codex_session.jsonl")
    assert s.summary is None
    assert s.compact_meta is None
    assert s.session_id == "019ddead-beef-7000-8888-aaaaaaaaaaaa"
    assert s.project_path == "/tmp/fake-project"
    assert s.git_branch == "feat/test"
    # Skipped: developer message, permissions-noise user, reasoning, tool output.
    # Kept: user → (assistant block: 2 messages + 2 tool calls merged) → user.
    roles = [t.role for t in s.turns]
    assert roles == ["user", "assistant", "user"]
    assert s.turns[0].text == "please help me debug this"
    assert s.turns[1].text == "Let me look at the code.\nFound the bug."
    assert s.turns[1].tool_calls == [
        "↳ Bash: ls -la",
        "↳ Read: /tmp/fake-project/main.py",
    ]
    assert s.turns[2].text == "thanks"


def test_parse_codex_no_tool_calls():
    s = codex.parse(FIXTURES / "codex_session.jsonl", include_tool_calls=False)
    for t in s.turns:
        assert t.tool_calls == []


def test_parse_codex_max_turns():
    s = codex.parse(FIXTURES / "codex_session.jsonl", max_turns=2)
    assert len(s.turns) == 2
    assert [t.role for t in s.turns] == ["assistant", "user"]
    assert s.turns[1].text == "thanks"


def test_summarize_function_call_known():
    assert codex._summarize_function_call(
        {"name": "exec_command", "arguments": '{"cmd":"echo hi"}'}
    ) == "↳ Bash: echo hi"
    assert codex._summarize_function_call(
        {"name": "view", "arguments": '{"path":"/x.py"}'}
    ) == "↳ Read: /x.py"
    assert codex._summarize_function_call(
        {"name": "apply_patch", "arguments": "{}"}
    ) == "↳ Edit"
    assert codex._summarize_function_call(
        {"name": "unknown_tool", "arguments": "{}"}
    ) == "↳ unknown_tool"


def test_parse_codex_uses_last_compaction():
    s = codex.parse(FIXTURES / "codex_with_compaction.jsonl")
    # Pre-compaction turns are dropped; only post-last-compaction events remain.
    assert [t.role for t in s.turns] == ["user", "assistant"]
    assert s.turns[0].text == "post-compaction question"
    assert s.turns[1].text == "answer after the last compaction"
    assert s.turns[1].tool_calls == ["↳ Bash: ls"]
    # The summary is the LAST compaction's replacement_history, role-labeled,
    # with developer + opaque compaction entries skipped.
    assert s.summary is not None
    assert "**User:** latest goal: do Y" in s.summary
    assert "**Assistant:** updated plan: A', B', C'" in s.summary
    # Should NOT carry over the earlier compaction's content.
    assert "original goal" not in s.summary
    assert "plan-so-far" not in s.summary
    assert "permissions noise" not in s.summary
    assert s.compact_timestamp == "2026-04-29T10:02:00Z"
    assert s.compact_meta == {"trigger": "compacted"}


def test_format_replacement_history_skips_developer_and_compaction():
    rh = [
        {"type": "message", "role": "developer",
         "content": [{"type": "input_text", "text": "noise"}]},
        {"type": "compaction", "encrypted_content": "opaque"},
        {"type": "message", "role": "user",
         "content": [{"type": "input_text", "text": "real user msg"}]},
        {"type": "message", "role": "assistant",
         "content": [{"type": "output_text", "text": "real assistant msg"}]},
    ]
    out = codex._format_replacement_history(rh)
    assert out == "**User:** real user msg\n\n**Assistant:** real assistant msg"


def test_user_input_noise_filter():
    assert codex._is_user_input_noise("<permissions instructions>x")
    assert codex._is_user_input_noise("<environment_context>x")
    assert codex._is_user_input_noise("")
    assert not codex._is_user_input_noise("hello")
