package cli

import (
	"bytes"
	"strings"
	"testing"
)

func runCLI(t *testing.T, argv ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(argv, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestRun_Version(t *testing.T) {
	code, stdout, _ := runCLI(t, "--version")
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(stdout, Version) {
		t.Fatalf("stdout=%q", stdout)
	}
}

func TestRun_Help(t *testing.T) {
	code, stdout, _ := runCLI(t, "--help")
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(stdout, "USAGE") {
		t.Fatalf("stdout missing USAGE: %q", stdout)
	}
}

func TestRun_SaveOutputConflict(t *testing.T) {
	code, _, stderr := runCLI(t, "--save", "--output", "/tmp/x.md")
	if code != 2 {
		t.Fatalf("code=%d want 2", code)
	}
	if !strings.Contains(stderr, "incompatible") {
		t.Fatalf("stderr=%q", stderr)
	}
}

func TestRun_FullWithoutSaveErrors(t *testing.T) {
	code, _, stderr := runCLI(t, "--full")
	if code != 2 {
		t.Fatalf("code=%d want 2", code)
	}
	if !strings.Contains(stderr, "--full") {
		t.Fatalf("stderr=%q", stderr)
	}
}

func TestRun_SaveJSONErrors(t *testing.T) {
	code, _, stderr := runCLI(t, "--save", "--format", "json")
	if code != 2 {
		t.Fatalf("code=%d want 2", code)
	}
	if !strings.Contains(stderr, "markdown") {
		t.Fatalf("stderr=%q", stderr)
	}
}

func TestRun_BadFormatErrors(t *testing.T) {
	code, _, stderr := runCLI(t, "--format", "yaml")
	if code != 2 {
		t.Fatalf("code=%d want 2", code)
	}
	if !strings.Contains(stderr, "format") {
		t.Fatalf("stderr=%q", stderr)
	}
}

func TestRun_ExtraArgsError(t *testing.T) {
	code, _, stderr := runCLI(t, "/a", "/b")
	if code != 2 {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(stderr, "unexpected") {
		t.Fatalf("stderr=%q", stderr)
	}
}

func TestRun_UnknownFlagError(t *testing.T) {
	code, _, stderr := runCLI(t, "--nope")
	if code != 2 {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(stderr, "flag") {
		t.Fatalf("stderr=%q", stderr)
	}
}
