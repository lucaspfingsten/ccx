// Package cli wires flag parsing, dispatch, and the overall command flow.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lucaspfingsten/ccx/internal/clipboard"
	"github.com/lucaspfingsten/ccx/internal/parser"
	"github.com/lucaspfingsten/ccx/internal/picker"
	"github.com/lucaspfingsten/ccx/internal/projects"
	"github.com/lucaspfingsten/ccx/internal/render"
	"github.com/lucaspfingsten/ccx/internal/state"
	"github.com/lucaspfingsten/ccx/internal/style"
)

// Version is the ccx binary version.
const Version = "0.2.0"

// SaveFile is the pinned filename written by --save in CWD.
const SaveFile = ".ccx-context.md"

// StateFile is the pinned cursor filename written next to SaveFile.
const StateFile = ".ccx-state.json"

type options struct {
	projectPath string
	list        bool
	limit       int
	sessionID   string
	copy        bool
	output      string
	save        bool
	full        bool
	maxTurns    int
	noToolCalls bool
	format      string
	version     bool
	help        bool
}

// Run parses argv and executes the command. Returns the process exit code.
func Run(argv []string, stdout, stderr io.Writer) int {
	opts, err := parseFlags(argv, stderr)
	if err != nil {
		// flag.ErrHelp is signalled when -h/--help is passed; the FlagSet
		// already printed usage in that case (we override Usage to our help).
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		printErr(stderr, err)
		return 2
	}

	if opts.help {
		printHelp(stdout)
		return 0
	}
	if opts.version {
		fmt.Fprintln(stdout, "ccx "+Version)
		return 0
	}

	if err := validateOptions(opts); err != nil {
		printErr(stderr, err)
		return 2
	}

	if opts.list {
		return runList(opts, stdout, stderr)
	}
	return runDirect(opts, stdout, stderr)
}

func printErr(w io.Writer, err error) {
	fmt.Fprintln(w, style.Error.Render("error: ")+err.Error())
}

// fail prints err to stderr and returns 1.
func fail(stderr io.Writer, err error) int {
	printErr(stderr, err)
	return 1
}

func parseFlags(argv []string, stderr io.Writer) (*options, error) {
	fs := flag.NewFlagSet("ccx", flag.ContinueOnError)
	fs.SetOutput(stderr)
	// Suppress flag's default usage; we render our own.
	fs.Usage = func() { printHelp(stderr) }

	opts := &options{limit: 15, format: "markdown"}
	fs.BoolVar(&opts.list, "list", false, "")
	fs.IntVar(&opts.limit, "limit", 15, "")
	fs.StringVar(&opts.sessionID, "session", "", "")
	fs.BoolVar(&opts.copy, "copy", false, "")
	fs.StringVar(&opts.output, "output", "", "")
	fs.StringVar(&opts.output, "o", "", "")
	fs.BoolVar(&opts.save, "save", false, "")
	fs.BoolVar(&opts.full, "full", false, "")
	fs.IntVar(&opts.maxTurns, "max-turns", 0, "")
	fs.BoolVar(&opts.noToolCalls, "no-tool-calls", false, "")
	fs.StringVar(&opts.format, "format", "markdown", "")
	fs.BoolVar(&opts.version, "version", false, "")
	fs.BoolVar(&opts.help, "help", false, "")
	fs.BoolVar(&opts.help, "h", false, "")

	if err := fs.Parse(argv); err != nil {
		return nil, err
	}
	switch fs.NArg() {
	case 0:
	case 1:
		opts.projectPath = fs.Arg(0)
	default:
		return nil, fmt.Errorf("unexpected extra arguments: %v", fs.Args()[1:])
	}

	if opts.format != "markdown" && opts.format != "json" {
		return nil, fmt.Errorf("--format must be 'markdown' or 'json' (got %q)", opts.format)
	}
	return opts, nil
}

func validateOptions(o *options) error {
	if o.save && o.output != "" {
		return errors.New("--save and --output are incompatible (pick one)")
	}
	if o.full && !o.save {
		return errors.New("--full only applies with --save")
	}
	if o.save && o.format != "markdown" {
		return errors.New("--save only supports --format markdown")
	}
	return nil
}

func runList(o *options, stdout, stderr io.Writer) int {
	sessions := projects.ListRecentSessions(o.limit)
	if len(sessions) == 0 {
		fmt.Fprintln(stderr, style.Error.Render("no Claude Code sessions found in ~/.claude/projects/"))
		return 1
	}
	chosen, err := picker.Pick(sessions)
	switch {
	case errors.Is(err, picker.ErrNotATTY):
		fmt.Fprintln(stderr, style.Error.Render("--list requires an interactive terminal."))
		fmt.Fprintln(stderr, "Re-run in a terminal, or pass a path/session id directly.")
		return 1
	case errors.Is(err, picker.ErrAborted):
		return 0
	case err != nil:
		return fail(stderr, err)
	}
	return emitFromPath(chosen.JSONLPath, o, stdout, stderr)
}

func runDirect(o *options, stdout, stderr io.Writer) int {
	jsonl, err := resolveJSONL(o, stderr)
	if err != nil {
		return fail(stderr, err)
	}
	if o.save {
		return runSave(jsonl, o, stderr)
	}
	return emitFromPath(jsonl, o, stdout, stderr)
}

func resolveJSONL(o *options, stderr io.Writer) (string, error) {
	var projectDir string
	if o.projectPath != "" {
		projectDir = projects.ProjectDirFor(o.projectPath)
		if projectDir == "" {
			fmt.Fprintln(stderr, "(Looked under ~/.claude/projects/ for the matching key.)")
			return "", fmt.Errorf("no Claude Code session directory for: %s", o.projectPath)
		}
	} else {
		projectDir = projects.FindProjectForCWD("")
		if projectDir == "" {
			fmt.Fprintln(stderr, "Try `ccx --list` to pick from all projects, or pass a path: `ccx <project>`.")
			return "", errors.New("no Claude Code session for the current directory")
		}
	}

	if o.sessionID != "" {
		p := projects.SessionJSONL(projectDir, o.sessionID)
		if p == "" {
			return "", fmt.Errorf("session %q not found in %s", o.sessionID, projectDir)
		}
		return p, nil
	}

	p := projects.LatestSessionJSONL(projectDir)
	if p == "" {
		return "", fmt.Errorf("no .jsonl session files in %s", projectDir)
	}
	return p, nil
}

// emitFromPath is the standard non-save path: parse, render, write to stdout
// or --output, optionally copy to clipboard.
func emitFromPath(jsonl string, o *options, stdout, stderr io.Writer) int {
	s, err := parser.ParseSession(jsonl, !o.noToolCalls, o.maxTurns)
	if err != nil {
		return fail(stderr, err)
	}

	out, err := renderOne(s, o.format)
	if err != nil {
		return fail(stderr, err)
	}

	if o.output != "" {
		if err := os.WriteFile(o.output, []byte(out), 0o644); err != nil {
			return fail(stderr, err)
		}
		fmt.Fprintf(stderr, "Wrote %d bytes to %s\n", len(out), o.output)
	} else {
		fmt.Fprint(stdout, out)
	}

	if o.copy {
		if err := clipboard.Copy(out); err != nil {
			return fail(stderr, err)
		}
		fmt.Fprintf(stderr, "Copied %d bytes to clipboard.\n", len(out))
	}
	return 0
}

func renderOne(s *parser.Session, format string) (string, error) {
	if format == "json" {
		return render.JSON(s)
	}
	return render.Markdown(s), nil
}

// runSave handles the --save flow with auto-diff, force-full, and state cursor
// management. Output goes to ./.ccx-context.md; nothing is written to stdout.
func runSave(jsonl string, o *options, stderr io.Writer) int {
	cwd, err := os.Getwd()
	if err != nil {
		return fail(stderr, err)
	}
	savePath := filepath.Join(cwd, SaveFile)
	statePath := filepath.Join(cwd, StateFile)

	events, err := parser.LoadJSONL(jsonl)
	if err != nil {
		return fail(stderr, err)
	}
	session := parser.ParseSessionFromEvents(events, jsonl, !o.noToolCalls, 0)

	var prior *state.File
	if !o.full {
		prior, err = state.Read(statePath)
		if err != nil {
			return fail(stderr, err)
		}
	}

	out, newCursor, err := computeSaveOutput(o, session, events, prior)
	if err != nil {
		return fail(stderr, err)
	}

	if err := os.WriteFile(savePath, []byte(out), 0o644); err != nil {
		return fail(stderr, err)
	}
	if err := state.Write(statePath, newCursor); err != nil {
		return fail(stderr, err)
	}

	hint := fmt.Sprintf("saved to %s (you may want to add it + %s to .gitignore)", SaveFile, StateFile)
	fmt.Fprintln(stderr, style.Hint.Render(hint))

	if o.copy {
		if err := clipboard.Copy(out); err != nil {
			return fail(stderr, err)
		}
		fmt.Fprintf(stderr, "Copied %d bytes to clipboard.\n", len(out))
	}
	return 0
}

// computeSaveOutput decides full vs. diff vs. empty based on the prior cursor
// and produces the markdown plus the cursor to persist for next run.
func computeSaveOutput(o *options, session *parser.Session, events []parser.Event, prior *state.File) (string, *state.File, error) {
	if o.full || prior == nil || prior.SessionID != session.SessionID {
		out, err := renderFromSession(session, o)
		if err != nil {
			return "", nil, err
		}
		return out, state.FromLastEvent(session.SessionID, events), nil
	}

	slice := state.CursorSlice(prior, session.SessionID, events)
	switch slice.Mode {
	case "full":
		out, err := renderFromSession(session, o)
		if err != nil {
			return "", nil, err
		}
		return out, state.FromLastEvent(session.SessionID, events), nil

	case "empty":
		out := render.MarkdownNoNewTurns(session, prior.LastTimestamp)
		// Keep cursor where it is, but bump LastSave so we record the touch.
		bumped := *prior
		bumped.LastSave = time.Now().UTC().Format(time.RFC3339)
		return out, &bumped, nil

	case "diff":
		turns := parser.TurnsFromEvents(slice.Events, !o.noToolCalls)
		if len(turns) == 0 {
			// Slice had events but they all got filtered as noise; treat as empty
			// from the user's perspective but still advance the cursor so we don't
			// re-process them next time.
			out := render.MarkdownNoNewTurns(session, prior.LastTimestamp)
			return out, state.FromLastEvent(session.SessionID, events), nil
		}
		out := render.MarkdownUpdate(session, turns)
		return out, state.FromLastEvent(session.SessionID, events), nil
	}
	return "", nil, fmt.Errorf("internal: unknown slice mode %q", slice.Mode)
}

func renderFromSession(s *parser.Session, o *options) (string, error) {
	if o.maxTurns > 0 && len(s.Turns) > o.maxTurns {
		s.Turns = s.Turns[len(s.Turns)-o.maxTurns:]
	}
	return renderOne(s, o.format)
}

func printHelp(w io.Writer) {
	hdr := style.Header.Render("ccx")
	dim := style.Dim
	var b strings.Builder
	fmt.Fprintln(&b, hdr+" — extract context from Claude Code sessions for use in side agents.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "USAGE")
	fmt.Fprintln(&b, "  ccx [flags] [project_path]")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "FLAGS")
	for _, line := range []struct{ flag, help string }{
		{"--list", "interactive picker over recent sessions across all projects"},
		{"--limit N", "with --list, max entries to show (default: 15)"},
		{"--session ID", "specific session id (default: latest in the resolved project)"},
		{"--copy", "also copy rendered output to the system clipboard"},
		{"--output FILE / -o FILE", "write to FILE instead of stdout (one-shot, no state)"},
		{"--save", "write to ./.ccx-context.md, auto-diff after first run"},
		{"--save --full", "force a full re-dump and reset the diff cursor"},
		{"--max-turns N", "cap to the last N turns"},
		{"--no-tool-calls", "omit ↳ Tool: ... lines from assistant turns"},
		{"--format markdown|json", "output format (default: markdown)"},
		{"--version", "print version and exit"},
		{"-h, --help", "show this help"},
	} {
		fmt.Fprintf(&b, "  %-26s %s\n", line.flag, dim.Render(line.help))
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "EXAMPLES")
	for _, line := range []string{
		"ccx                                  # latest session for cwd → stdout",
		"ccx --list                           # pick from all recent sessions",
		"ccx --copy                           # also copy to clipboard",
		"ccx --save ~/code/myapp              # write .ccx-context.md (auto-diff next time)",
		"ccx --save --full                    # force a full re-dump",
		"ccx --format json | jq .summary      # programmatic JSON",
	} {
		fmt.Fprintln(&b, "  "+dim.Render(line))
	}
	fmt.Fprintln(w, b.String())
}
