package parser

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeJSONL(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "x.jsonl")
	if err := os.WriteFile(p, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadJSONL_SkipsBlankAndMalformed(t *testing.T) {
	p := writeJSONL(t, `{"a":1}

{"b":2}
{not json}
{"c":3}
`)
	events, err := LoadJSONL(p)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]map[string]any, len(events))
	for i, e := range events {
		got[i] = e.Raw
	}
	want := []map[string]any{
		{"a": float64(1)},
		{"b": float64(2)},
		{"c": float64(3)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestExtractText_StringContent(t *testing.T) {
	got := ExtractTextFromMessage(map[string]any{"content": "hello"})
	if got != "hello" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractText_ListSkipsIDEAndToolUse(t *testing.T) {
	msg := map[string]any{
		"content": []any{
			map[string]any{"type": "text", "text": "<ide_opened_file>foo</ide_opened_file>"},
			map[string]any{"type": "text", "text": "<ide_selection>bar</ide_selection>"},
			map[string]any{"type": "text", "text": "real content"},
			map[string]any{"type": "tool_use", "name": "Read"},
		},
	}
	got := ExtractTextFromMessage(msg)
	if got != "real content" {
		t.Fatalf("got %q", got)
	}
}

func TestSummarizeToolUse_Table(t *testing.T) {
	cases := []struct {
		name string
		part map[string]any
		want string
	}{
		{
			"bash with description",
			map[string]any{"type": "tool_use", "name": "Bash", "input": map[string]any{"description": "List files", "command": "ls -la"}},
			"↳ Bash: List files",
		},
		{
			"bash without description",
			map[string]any{"type": "tool_use", "name": "Bash", "input": map[string]any{"command": "ls -la"}},
			"↳ Bash: ls -la",
		},
		{
			"bash long command truncated to 80",
			map[string]any{"type": "tool_use", "name": "Bash", "input": map[string]any{"command": "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz1234567890extra"}},
			"↳ Bash: abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz12",
		},
		{
			"read",
			map[string]any{"type": "tool_use", "name": "Read", "input": map[string]any{"file_path": "/x/y.py"}},
			"↳ Read: /x/y.py",
		},
		{
			"edit",
			map[string]any{"type": "tool_use", "name": "Edit", "input": map[string]any{"file_path": "/edit.go"}},
			"↳ Edit: /edit.go",
		},
		{
			"grep",
			map[string]any{"type": "tool_use", "name": "Grep", "input": map[string]any{"pattern": "TODO"}},
			"↳ Grep: TODO",
		},
		{
			"glob",
			map[string]any{"type": "tool_use", "name": "Glob", "input": map[string]any{"pattern": "*.go"}},
			"↳ Glob: *.go",
		},
		{
			"todoWrite",
			map[string]any{"type": "tool_use", "name": "TodoWrite", "input": map[string]any{"todos": []any{1, 2, 3}}},
			"↳ TodoWrite: 3 items",
		},
		{
			"webFetch",
			map[string]any{"type": "tool_use", "name": "WebFetch", "input": map[string]any{"url": "https://example.com"}},
			"↳ WebFetch: https://example.com",
		},
		{
			"webSearch",
			map[string]any{"type": "tool_use", "name": "WebSearch", "input": map[string]any{"query": "go testing"}},
			"↳ WebSearch: go testing",
		},
		{
			"notebookEdit",
			map[string]any{"type": "tool_use", "name": "NotebookEdit", "input": map[string]any{"notebook_path": "/n.ipynb"}},
			"↳ NotebookEdit: /n.ipynb",
		},
		{
			"agent with subagent",
			map[string]any{"type": "tool_use", "name": "Agent", "input": map[string]any{"subagent_type": "researcher", "description": "look up x"}},
			"↳ Agent: researcher: look up x",
		},
		{
			"agent without subagent",
			map[string]any{"type": "tool_use", "name": "Task", "input": map[string]any{"description": "do something"}},
			"↳ Agent: do something",
		},
		{
			"unknown tool",
			map[string]any{"type": "tool_use", "name": "Frobulate", "input": map[string]any{}},
			"↳ Frobulate",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SummarizeToolUse(c.part); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestFindLastCompaction_PicksLastOfMany(t *testing.T) {
	events := mkEvents(
		map[string]any{"type": "user", "message": map[string]any{"content": "first"}},
		map[string]any{"type": "system", "compactMetadata": map[string]any{"trigger": "auto"}},
		map[string]any{"type": "user", "isCompactSummary": true, "message": map[string]any{"content": "first summary"}},
		map[string]any{"type": "assistant", "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "reply"}}}},
		map[string]any{"type": "system", "compactMetadata": map[string]any{"trigger": "manual", "preTokens": float64(100)}},
		map[string]any{"type": "user", "isCompactSummary": true, "message": map[string]any{"content": "second summary"}},
		map[string]any{"type": "user", "message": map[string]any{"content": "after"}},
	)
	idx, summary, meta := FindLastCompaction(events)
	if idx != 5 {
		t.Fatalf("idx=%d want 5", idx)
	}
	if summary != "second summary" {
		t.Fatalf("summary=%q", summary)
	}
	if meta == nil || meta.Trigger != "manual" || meta.PreTokens != 100 {
		t.Fatalf("meta=%+v", meta)
	}
}

func TestFindLastCompaction_None(t *testing.T) {
	events := mkEvents(
		map[string]any{"type": "user", "message": map[string]any{"content": "hi"}},
		map[string]any{"type": "assistant", "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "hello"}}}},
	)
	idx, summary, meta := FindLastCompaction(events)
	if idx != -1 || summary != "" || meta != nil {
		t.Fatalf("got idx=%d summary=%q meta=%+v", idx, summary, meta)
	}
}

func TestEventToTurn_SkipsSidechain(t *testing.T) {
	ev := mkEvent(map[string]any{
		"type":        "user",
		"isSidechain": true,
		"message":     map[string]any{"content": "subagent prompt"},
	})
	if EventToTurn(ev, true) != nil {
		t.Fatal("expected nil")
	}
}

func TestEventToTurn_SkipsToolResultOnlyUser(t *testing.T) {
	ev := mkEvent(map[string]any{
		"type":        "user",
		"isSidechain": false,
		"message": map[string]any{"content": []any{
			map[string]any{"type": "tool_result", "content": "x"},
		}},
	})
	if EventToTurn(ev, true) != nil {
		t.Fatal("expected nil")
	}
}

func TestEventToTurn_SkipsIDEOnlyUser(t *testing.T) {
	ev := mkEvent(map[string]any{
		"type":        "user",
		"isSidechain": false,
		"message": map[string]any{"content": []any{
			map[string]any{"type": "text", "text": "<ide_opened_file>foo</ide_opened_file>"},
		}},
	})
	if EventToTurn(ev, true) != nil {
		t.Fatal("expected nil")
	}
}

func TestEventToTurn_AssistantSkipsThinking(t *testing.T) {
	ev := mkEvent(map[string]any{
		"type":        "assistant",
		"isSidechain": false,
		"message": map[string]any{"content": []any{
			map[string]any{"type": "thinking", "thinking": "hidden internal"},
			map[string]any{"type": "text", "text": "visible reply"},
		}},
	})
	turn := EventToTurn(ev, true)
	if turn == nil {
		t.Fatal("expected turn")
	}
	if turn.Text != "visible reply" {
		t.Fatalf("text=%q", turn.Text)
	}
	if len(turn.ToolCalls) != 0 {
		t.Fatalf("tool_calls=%v", turn.ToolCalls)
	}
}

func TestEventToTurn_SkipsCompactSummary(t *testing.T) {
	ev := mkEvent(map[string]any{
		"type":             "user",
		"isSidechain":      false,
		"isCompactSummary": true,
		"message":          map[string]any{"content": "summary text"},
	})
	if EventToTurn(ev, true) != nil {
		t.Fatal("expected nil")
	}
}

func TestParseSession_WithCompaction(t *testing.T) {
	s, err := ParseSession("testdata/with_compaction.jsonl", true, 0)
	if err != nil {
		t.Fatal(err)
	}
	if s.Summary != "final summary block" {
		t.Fatalf("summary=%q", s.Summary)
	}
	if s.CompactMeta == nil ||
		s.CompactMeta.Trigger != "manual" ||
		s.CompactMeta.PreTokens != 120000 ||
		s.CompactMeta.PostTokens != 5000 ||
		s.CompactMeta.DurationMs != 12345 {
		t.Fatalf("meta=%+v", s.CompactMeta)
	}
	if s.GitBranch != "main" {
		t.Fatalf("branch=%q", s.GitBranch)
	}
	if len(s.Turns) != 2 {
		t.Fatalf("turns=%d want 2", len(s.Turns))
	}
	if s.Turns[0].Role != "user" || s.Turns[0].Text != "latest user msg" {
		t.Fatalf("turn0=%+v", s.Turns[0])
	}
	if s.Turns[1].Role != "assistant" || s.Turns[1].Text != "latest reply" {
		t.Fatalf("turn1=%+v", s.Turns[1])
	}
	if !reflect.DeepEqual(s.Turns[1].ToolCalls, []string{"↳ Read: /foo.py"}) {
		t.Fatalf("tool_calls=%v", s.Turns[1].ToolCalls)
	}
}

func TestParseSession_NoCompaction(t *testing.T) {
	s, err := ParseSession("testdata/no_compaction.jsonl", true, 0)
	if err != nil {
		t.Fatal(err)
	}
	if s.HasSummary {
		t.Fatal("expected no summary")
	}
	if s.CompactMeta != nil {
		t.Fatalf("meta=%+v", s.CompactMeta)
	}
	if len(s.Turns) != 2 {
		t.Fatalf("turns=%d want 2", len(s.Turns))
	}
	if s.Turns[0].Text != "hello" {
		t.Fatalf("turn0=%+v", s.Turns[0])
	}
	if s.Turns[1].Text != "hi there" || !reflect.DeepEqual(s.Turns[1].ToolCalls, []string{"↳ Bash: check status"}) {
		t.Fatalf("turn1=%+v", s.Turns[1])
	}
}

func TestParseSession_MaxTurnsKeepsTail(t *testing.T) {
	s, err := ParseSession("testdata/with_compaction.jsonl", true, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Turns) != 1 {
		t.Fatalf("turns=%d want 1", len(s.Turns))
	}
	if s.Turns[0].Role != "assistant" {
		t.Fatalf("role=%q", s.Turns[0].Role)
	}
}

func TestParseSession_NoToolCallsFlag(t *testing.T) {
	s, err := ParseSession("testdata/with_compaction.jsonl", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, tn := range s.Turns {
		if len(tn.ToolCalls) != 0 {
			t.Fatalf("tool_calls=%v", tn.ToolCalls)
		}
	}
}

func TestParseSession_WithUUIDs(t *testing.T) {
	s, err := ParseSession("testdata/with_uuids.jsonl", true, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 6 {
		t.Fatalf("events=%d want 6", len(s.Events))
	}
	if s.SummaryIdx != -1 {
		t.Fatalf("summaryIdx=%d", s.SummaryIdx)
	}
	if len(s.Turns) != 6 {
		t.Fatalf("turns=%d want 6", len(s.Turns))
	}
	if s.Turns[0].UUID != "u-001" || s.Turns[5].UUID != "u-006" {
		t.Fatalf("uuids: %q .. %q", s.Turns[0].UUID, s.Turns[5].UUID)
	}
}

// helpers

func mkEvents(maps ...map[string]any) []Event {
	out := make([]Event, len(maps))
	for i, m := range maps {
		out[i] = Event{Raw: m, LineIndex: i}
	}
	return out
}

func mkEvent(m map[string]any) Event {
	return Event{Raw: m, LineIndex: 0}
}
