package state

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/lucaspfingsten/ccx/internal/parser"
)

func TestRead_MissingReturnsNil(t *testing.T) {
	got, err := Read(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("got %+v want nil", got)
	}
}

func TestWriteThenRead_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "state.json")
	want := &File{
		SessionID:     "sess-1",
		LastUUID:      "u-007",
		LastTimestamp: "2026-04-28T10:00:00Z",
		LastSave:      "2026-04-28T10:01:00Z",
		LastLineIndex: 42,
	}
	if err := Write(p, want); err != nil {
		t.Fatal(err)
	}
	got, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func loadUUIDFixture(t *testing.T) []parser.Event {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "parser", "testdata", "with_uuids.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	events, err := parser.LoadJSONL(root)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func TestCursorSlice_NoStateIsFull(t *testing.T) {
	events := loadUUIDFixture(t)
	res := CursorSlice(nil, "uuid-session", events)
	if res.Mode != "full" {
		t.Fatalf("mode=%q", res.Mode)
	}
	if len(res.Events) != len(events) {
		t.Fatalf("events=%d want %d", len(res.Events), len(events))
	}
}

func TestCursorSlice_MismatchedSessionIsFull(t *testing.T) {
	events := loadUUIDFixture(t)
	st := &File{SessionID: "different", LastUUID: "u-002"}
	res := CursorSlice(st, "uuid-session", events)
	if res.Mode != "full" {
		t.Fatalf("mode=%q", res.Mode)
	}
}

func TestCursorSlice_UUIDMatchSlicesAfter(t *testing.T) {
	events := loadUUIDFixture(t)
	st := &File{SessionID: "uuid-session", LastUUID: "u-004"}
	res := CursorSlice(st, "uuid-session", events)
	if res.Mode != "diff" {
		t.Fatalf("mode=%q", res.Mode)
	}
	if len(res.Events) != 2 {
		t.Fatalf("events=%d want 2", len(res.Events))
	}
	if u, _ := res.Events[0].Raw["uuid"].(string); u != "u-005" {
		t.Fatalf("first uuid=%q", u)
	}
}

func TestCursorSlice_CursorAtEndIsEmpty(t *testing.T) {
	events := loadUUIDFixture(t)
	st := &File{SessionID: "uuid-session", LastUUID: "u-006"}
	res := CursorSlice(st, "uuid-session", events)
	if res.Mode != "empty" {
		t.Fatalf("mode=%q", res.Mode)
	}
	if len(res.Events) != 0 {
		t.Fatalf("events=%d want 0", len(res.Events))
	}
}

func TestCursorSlice_CursorMissingIsFull(t *testing.T) {
	events := loadUUIDFixture(t)
	st := &File{SessionID: "uuid-session", LastUUID: "u-not-here"}
	res := CursorSlice(st, "uuid-session", events)
	if res.Mode != "full" {
		t.Fatalf("mode=%q", res.Mode)
	}
}

func TestCursorSlice_TimestampFallback(t *testing.T) {
	events := loadUUIDFixture(t)
	// No uuid, but timestamp+line_index pair matches the third event (index 2,
	// uuid u-003).
	target := events[2]
	ts, _ := target.Raw["timestamp"].(string)
	st := &File{
		SessionID:     "uuid-session",
		LastTimestamp: ts,
		LastLineIndex: target.LineIndex,
	}
	res := CursorSlice(st, "uuid-session", events)
	if res.Mode != "diff" {
		t.Fatalf("mode=%q", res.Mode)
	}
	if len(res.Events) != 3 {
		t.Fatalf("events=%d want 3", len(res.Events))
	}
}

func TestFromLastEvent_PopulatesFields(t *testing.T) {
	events := loadUUIDFixture(t)
	got := FromLastEvent("sess-x", events)
	if got.SessionID != "sess-x" {
		t.Fatalf("session_id=%q", got.SessionID)
	}
	if got.LastUUID != "u-006" {
		t.Fatalf("last_uuid=%q", got.LastUUID)
	}
	if got.LastTimestamp == "" {
		t.Fatal("expected last_timestamp set")
	}
	if got.LastSave == "" {
		t.Fatal("expected last_save set")
	}
}
