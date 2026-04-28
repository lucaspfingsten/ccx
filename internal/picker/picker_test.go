package picker

import (
	"bytes"
	"strings"
	"testing"

	"github.com/lucaspfingsten/ccx/internal/projects"
)

func sample() []projects.SessionInfo {
	return []projects.SessionInfo{
		{ProjectName: "alpha", SessionID: "a1", JSONLPath: "/x/a.jsonl", FirstPrompt: "do alpha"},
		{ProjectName: "beta", SessionID: "b1", JSONLPath: "/x/b.jsonl", GitBranch: "main"},
	}
}

func TestPick_NoSessionsReturnsErr(t *testing.T) {
	if _, err := Pick(nil); err != ErrNoSessions {
		t.Fatalf("got %v want ErrNoSessions", err)
	}
}

func TestPickFallback_NumberedSelection(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("2\n")
	got, err := PickFallback(&out, in, sample())
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.SessionID != "b1" {
		t.Fatalf("got %+v", got)
	}
	if !strings.Contains(out.String(), "alpha") || !strings.Contains(out.String(), "beta") {
		t.Fatalf("output missing entries: %q", out.String())
	}
}

func TestPickFallback_QuitReturnsAborted(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("q\n")
	if _, err := PickFallback(&out, in, sample()); err != ErrAborted {
		t.Fatalf("got %v want ErrAborted", err)
	}
}

func TestPickFallback_InvalidChoiceErrors(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("99\n")
	if _, err := PickFallback(&out, in, sample()); err == nil {
		t.Fatal("expected error")
	}
}

func TestPick_NotATTY(t *testing.T) {
	// In `go test`, stdin is connected to a pipe, not a TTY, so Pick should
	// return ErrNotATTY rather than blocking on huh.
	_, err := Pick(sample())
	if err != ErrNotATTY {
		t.Fatalf("got %v want ErrNotATTY", err)
	}
}
