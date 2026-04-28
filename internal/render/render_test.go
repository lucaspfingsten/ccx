package render

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucaspfingsten/ccx/internal/parser"
)

var update = flag.Bool("update", false, "update golden files")

// TestMain forces UTC so the golden timestamps render deterministically.
func TestMain(m *testing.M) {
	flag.Parse()
	os.Setenv("TZ", "UTC")
	os.Exit(m.Run())
}

func loadFixture(t *testing.T, name string) *parser.Session {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "parser", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	s, err := parser.ParseSession(root, true, 0)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func goldenPath(name string) string {
	return filepath.Join("testdata", name)
}

func compareGolden(t *testing.T, gotName string, got string) {
	t.Helper()
	p := goldenPath(gotName)
	if *update {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("missing golden %q (run with -update): %v", p, err)
	}
	if string(want) != got {
		t.Fatalf("golden %s mismatch\n--- got ---\n%s\n--- want ---\n%s", gotName, got, string(want))
	}
}

func TestMarkdown_FullWithCompaction_Golden(t *testing.T) {
	s := loadFixture(t, "with_compaction.jsonl")
	got := Markdown(s)
	compareGolden(t, "full_with_compaction.md", got)
}

func TestMarkdown_FullNoCompaction_Golden(t *testing.T) {
	s := loadFixture(t, "no_compaction.jsonl")
	got := Markdown(s)
	compareGolden(t, "full_no_compaction.md", got)
}

func TestMarkdown_UpdateSlice_Golden(t *testing.T) {
	s := loadFixture(t, "with_uuids.jsonl")
	// Take the last 2 turns only as the "new" slice.
	slice := s.Turns[len(s.Turns)-2:]
	got := MarkdownUpdate(s, slice)
	compareGolden(t, "update_slice.md", got)
}

func TestMarkdown_NoNewTurns_Golden(t *testing.T) {
	s := loadFixture(t, "with_uuids.jsonl")
	got := MarkdownNoNewTurns(s, "2026-04-28T10:05:00Z")
	compareGolden(t, "no_new_turns.md", got)
}

func TestMarkdown_HasFullPrelude(t *testing.T) {
	s := loadFixture(t, "with_compaction.jsonl")
	got := Markdown(s)
	if !strings.HasPrefix(got, FullPrelude) {
		t.Fatalf("output missing full prelude:\n%s", got)
	}
}

func TestMarkdown_UpdateHasUpdatePrelude(t *testing.T) {
	s := loadFixture(t, "with_uuids.jsonl")
	got := MarkdownUpdate(s, s.Turns)
	if !strings.HasPrefix(got, UpdatePrelude) {
		t.Fatalf("output missing update prelude:\n%s", got)
	}
}

func TestMarkdown_UsesBoldLabelsNotH3(t *testing.T) {
	s := loadFixture(t, "with_compaction.jsonl")
	got := Markdown(s)
	if strings.Contains(got, "### User") || strings.Contains(got, "### Assistant") {
		t.Fatalf("output still uses H3 turn headers:\n%s", got)
	}
	if !strings.Contains(got, "**You**") || !strings.Contains(got, "**Claude**") {
		t.Fatalf("output missing bold-label turn markers:\n%s", got)
	}
}

func TestJSON_ParsesAndContainsKeys(t *testing.T) {
	s := loadFixture(t, "with_compaction.jsonl")
	got, err := JSON(s)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(got), &doc); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, got)
	}
	for _, k := range []string{"project_path", "project_name", "session_id", "git_branch", "compaction", "summary", "turns"} {
		if _, ok := doc[k]; !ok {
			t.Fatalf("missing key %q in JSON output", k)
		}
	}
}

func TestFormatTimestamp(t *testing.T) {
	// Non-RFC3339 strings pass through unchanged.
	if got := FormatTimestamp("not-a-time"); got != "not-a-time" {
		t.Fatalf("got %q", got)
	}
	if got := FormatTimestamp(""); got != "" {
		t.Fatalf("got %q", got)
	}
}
