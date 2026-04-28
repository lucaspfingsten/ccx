package projects

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProjectKeyFromPath_NonAlnumIsDash(t *testing.T) {
	// Pinned by Python: every non-alphanumeric character (incl. spaces, dots,
	// slashes, dashes) becomes "-".
	dir := t.TempDir()
	p := filepath.Join(dir, "My App.config")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	key, err := ProjectKeyFromPath(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range key {
		isAlnum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-'
		if !isAlnum {
			t.Fatalf("key has non-alnum-non-dash char: %q in %q", c, key)
		}
	}
	if !strings.Contains(key, "My-App-config") {
		t.Fatalf("expected 'My-App-config' fragment, got %q", key)
	}
}

func TestLatestSessionJSONL_PicksNewest(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.jsonl")
	newer := filepath.Join(dir, "newer.jsonl")
	if err := os.WriteFile(old, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	now := time.Now()
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newer, now, now); err != nil {
		t.Fatal(err)
	}
	got := LatestSessionJSONL(dir)
	if got != newer {
		t.Fatalf("got %q want %q", got, newer)
	}
}

func TestSessionJSONL_PresentAndAbsent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "abc.jsonl")
	if err := os.WriteFile(p, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := SessionJSONL(dir, "abc"); got != p {
		t.Fatalf("got %q want %q", got, p)
	}
	if got := SessionJSONL(dir, "missing"); got != "" {
		t.Fatalf("got %q want empty", got)
	}
}

func TestFindProjectForCWD_WalksUp(t *testing.T) {
	// Stage a fake $HOME with .claude/projects/<key>/ for a known directory,
	// then verify the walk-up from a deeper path still finds the project.
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	projectRoot := t.TempDir()
	subdir := filepath.Join(projectRoot, "src", "deep")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	key, err := ProjectKeyFromPath(projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	claudeProjects := filepath.Join(homeDir, ".claude", "projects", key)
	if err := os.MkdirAll(claudeProjects, 0o755); err != nil {
		t.Fatal(err)
	}

	got := FindProjectForCWD(subdir)
	if got != claudeProjects {
		t.Fatalf("got %q want %q", got, claudeProjects)
	}
}
