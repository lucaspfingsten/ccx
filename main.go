// Command ccx extracts context from Claude Code sessions for use in side
// agents (Cursor, Codex, ChatGPT, second Claude Code session, ...).
package main

import (
	"os"

	"github.com/lucaspfingsten/ccx/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
