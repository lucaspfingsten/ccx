// Package picker presents a session list with arrow-key navigation (huh) when
// stdin is a TTY, and falls back to a numbered prompt otherwise.
package picker

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/lucaspfingsten/ccx/internal/projects"
)

// ErrNoSessions is returned when there are no sessions to pick from.
var ErrNoSessions = errors.New("no sessions found")

// ErrAborted is returned when the user cancels the picker (e.g. Ctrl-C).
var ErrAborted = errors.New("aborted")

// ErrNotATTY is returned when stdin is not a TTY and we have no fallback to
// pick from non-interactively.
var ErrNotATTY = errors.New("stdin is not a tty")

// IsTerminal returns true if the given file is connected to an interactive
// terminal. Uses platform-specific stdlib syscalls (see picker_unix.go and
// picker_windows.go) so we can avoid pulling in golang.org/x/term.
func IsTerminal(f *os.File) bool {
	return isTerminalFD(f)
}

// Pick lets the user select one of sessions and returns the chosen entry.
// If stdin is a TTY, it shows a huh select form. Otherwise it returns
// ErrNotATTY so the caller can print a helpful error.
func Pick(sessions []projects.SessionInfo) (*projects.SessionInfo, error) {
	if len(sessions) == 0 {
		return nil, ErrNoSessions
	}
	if !IsTerminal(os.Stdin) {
		return nil, ErrNotATTY
	}
	return pickInteractive(sessions)
}

// PickFallback prints a numbered list to out and reads a 1-based selection
// from in. Used by tests and as a manual entry point.
func PickFallback(out io.Writer, in io.Reader, sessions []projects.SessionInfo) (*projects.SessionInfo, error) {
	if len(sessions) == 0 {
		return nil, ErrNoSessions
	}
	fmt.Fprintln(out)
	for i, s := range sessions {
		fmt.Fprintf(out, "  %2d. %s\n", i+1, formatLabel(s))
	}
	fmt.Fprintf(out, "\nPick [1-%d]: ", len(sessions))

	var raw string
	if _, err := fmt.Fscanln(in, &raw); err != nil {
		return nil, ErrAborted
	}
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "q") {
		return nil, ErrAborted
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > len(sessions) {
		return nil, fmt.Errorf("invalid choice: %s", raw)
	}
	return &sessions[n-1], nil
}

func pickInteractive(sessions []projects.SessionInfo) (*projects.SessionInfo, error) {
	options := make([]huh.Option[int], len(sessions))
	for i, s := range sessions {
		options[i] = huh.NewOption(formatLabel(s), i)
	}
	var idx int
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[int]().
				Title("ccx · pick a session").
				Options(options...).
				Value(&idx),
		),
	).WithShowHelp(true)
	if err := form.Run(); err != nil {
		return nil, ErrAborted
	}
	if idx < 0 || idx >= len(sessions) {
		return nil, ErrAborted
	}
	return &sessions[idx], nil
}

func formatLabel(s projects.SessionInfo) string {
	parts := []string{s.ProjectName}
	if s.GitBranch != "" {
		parts = append(parts, "["+s.GitBranch+"]")
	}
	prompt := strings.TrimSpace(s.FirstPrompt)
	if prompt != "" {
		if len(prompt) > 60 {
			prompt = prompt[:57] + "..."
		}
		parts = append(parts, "— "+prompt)
	}
	parts = append(parts, "· "+timeAgo(s.MTime))
	return strings.Join(parts, " ")
}

func timeAgo(mtimeMillis int64) string {
	if mtimeMillis == 0 {
		return ""
	}
	t := time.UnixMilli(mtimeMillis)
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
