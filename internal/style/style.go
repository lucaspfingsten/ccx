// Package style centralizes lipgloss styles used by the CLI and picker.
// Markdown output stays plain — terminal colors don't survive being pasted
// into other agents.
package style

import "github.com/charmbracelet/lipgloss"

var (
	// Header is used for the help banner title and similar headings.
	Header = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99"))

	// Path styles file paths.
	Path = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))

	// Dim is for secondary text (counts, timestamps, hints).
	Dim = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	// Hint is the styled "saved to ..." line printed to stderr after --save.
	Hint = lipgloss.NewStyle().Foreground(lipgloss.Color("78"))

	// Error styles error messages on stderr.
	Error = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))

	// Tag wraps small inline labels like project names in the picker.
	Tag = lipgloss.NewStyle().Bold(true)
)
