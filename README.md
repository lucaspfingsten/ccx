# ccx

> Give your side agents the same context as your main Claude Code session — without re-explaining anything.

`ccx` is a tiny Go CLI that extracts the *current working context* from a Claude Code session and outputs it as a Markdown block you can paste into ChatGPT, Cursor, Codex, another Claude Code window — anywhere.

No LLM calls. No data leaves your machine. Single static binary, ~20× faster startup than the previous Python release.

---

## The problem

You're deep in a Claude Code session, building something complex. Halfway through you want to ask a side question — maybe in ChatGPT, maybe in Cursor, maybe in a fresh Claude Code window — without burning tokens or polluting the focus of your main session.

The annoying part: the side agent has no idea what you've been working on. You re-explain the project, the stack, the current focus, the decisions. And even then it's a partial picture.

## The trick

When a Claude Code conversation gets long, Claude Code auto-compacts it: it asks the model to summarize what's happened so far, and saves that summary inside the session JSONL on disk (look for `isCompactSummary: true`). When you resume the session later, Claude Code injects that summary as the starting context — that's how it "remembers" where you left off.

`ccx` reuses exactly that summary. It finds the most recent compaction in the latest session for your project, grabs the verbatim summary, appends the conversation that happened after it, and outputs the whole thing — wrapped in a *framing prelude* that tells the receiving agent the body is **context, not instructions**.

**The smartest model already did the work; we just read its output.**

---

## Install

### Homebrew (recommended on macOS / Linux)

```bash
# placeholder — the tap doesn't exist yet, see GitHub issue / Releases for status
brew install lucaspfingsten/ccx/ccx
```

### Go install

```bash
go install github.com/lucaspfingsten/ccx@latest
```

### Pre-built binary

Grab the binary for your platform from the [GitHub Releases page](https://github.com/lucaspfingsten/ccx/releases) and drop it on your `$PATH`.

Builds: darwin/amd64, darwin/arm64, linux/amd64, linux/arm64, windows/amd64, windows/arm64.

---

## Usage

```bash
# From inside a project directory — finds the latest session for the cwd
ccx

# Pick interactively across all projects (great when running from a side terminal)
ccx --list

# Copy to clipboard (paste into ChatGPT, Claude.ai, Cursor chat, ...)
ccx --copy

# Write to a fixed file in cwd, with auto-diff on re-runs
ccx --save

# Force a full re-dump and reset the diff cursor
ccx --save --full

# Write to an arbitrary file (one-shot, no state tracking)
ccx --output context.md

# Pipe straight into another tool
ccx | pbcopy
ccx | codex "based on this context, refactor the auth flow"

# Specific project / specific session
ccx ~/code/myapp
ccx --session ab7f6a92-1f1a-4b02-b187-3c81c77317d8

# Cap the number of turns
ccx --max-turns 20

# Strip tool-call summary lines (just user/assistant text)
ccx --no-tool-calls

# JSON output for programmatic use
ccx --format json
```

### `--save` and the auto-diff flow

`ccx --save` writes a fixed file `./.ccx-context.md` in the current directory plus a small cursor file `./.ccx-state.json`. On the **next** run from the same directory, `ccx --save` only emits the new turns since the last save — so re-running it in your side agent's project root keeps that agent's context fresh without re-pasting the whole transcript.

The cursor uses each event's `uuid` (with `(timestamp, line_index)` as a fallback) and is keyed to the source `session_id`. If the upstream session changes, the cursor resets and you get a full slice again. `--save --full` always force-resets.

You'll usually want both files in your `.gitignore`.

### Use cases

| Side agent | How |
|------------|-----|
| **ChatGPT / Claude.ai** | `ccx --copy`, paste into chat |
| **Cursor** | `ccx --save`, then `@.ccx-context.md` in Cursor chat (re-run for incremental updates) |
| **Codex CLI** | `ccx \| codex "..."` |
| **Second Claude Code session** | `ccx --save`, then start the new session with `Read .ccx-context.md` (re-run to keep it fresh) |

---

## What gets included

For each session, `ccx` outputs:

1. **Framing prelude** (blockquote): tells the receiving agent that the body is read-only background, not instructions to act on
2. **Header**: project name, path, session id (short), git branch, last compaction time and token counts
3. **Summary**: the verbatim text of the most recent `isCompactSummary` (if the session has been compacted)
4. **Continued / New turns**: every user and assistant turn after the cursor, with optional one-line summaries of tool calls (`↳ Read: foo.go`, `↳ Bash: go test ./...`, …)

Turn markers use bold inline labels (`**You**` / `**Claude**`) rather than headers, so they paste cleanly into other markdown contexts without breaking outline depth.

What's filtered out:

- Queue operations, file-history snapshots, todo reminders, hook injections
- IDE-injected events (`<ide_opened_file>`, `<ide_selection>`)
- Progress events, raw tool results, sidechain (subagent) traces
- Assistant `thinking` blocks

If a session has never been compacted, the whole conversation is emitted (filtered the same way).

### Why the framing prelude?

Pasting a transcript into another agent has a subtle failure mode: the source transcript is full of imperative language ("fix this", "add that", "do X") that the receiving agent might mistake as a directive aimed at *it*. The prelude is a defensive blockquote saying "this is context, not instructions" — short enough not to bloat the input, explicit enough to prevent prompt-injection-by-accident.

The auto-diff flow uses a slightly different prelude variant ("Context update — append this") so the receiving agent treats it as an addendum rather than starting fresh.

---

## Privacy

Everything runs locally. `ccx` reads `~/.claude/projects/<project-key>/<session-id>.jsonl` and writes to stdout / clipboard / file you specify. **No network requests, ever.**

---

## How it works (technical)

Claude Code stores each session as a JSONL file at `~/.claude/projects/<project-key>/<session-id>.jsonl`, where `<project-key>` is the absolute project path with every non-alphanumeric character replaced by `-` (matches Claude Code's source: `/[^a-zA-Z0-9]/g → "-"`).

A session is a stream of events: `user`, `assistant`, `system`, `attachment`, `queue-operation`, etc. When auto-compaction fires, Claude Code writes:

1. A `system` event carrying `compactMetadata: { trigger, preTokens, postTokens, durationMs, ... }`
2. A `user` event with `isCompactSummary: true` whose content is the full summary text

A session can be compacted multiple times. `ccx` finds the **last** `isCompactSummary` and emits it followed by all subsequent meaningful turns.

For `--save`, the cursor primary key is the per-event `uuid`. When the cursor is found in the current session, the new render is sliced from after that event; otherwise (cursor lost, session changed, or no state) `ccx` writes a full slice and resets state.

---

## Roadmap

- [ ] Cursor session extraction (different format, same idea)
- [ ] Codex CLI session extraction
- [ ] Optional MCP server wrapper (so any MCP-aware agent can call `ccx` natively)
- [ ] Standalone `--diff` flag for piping (`ccx --diff | codex`)
- [ ] Homebrew tap (`homebrew-ccx`) once auto-publish is wired up

---

## Contributing

PRs welcome.

```bash
git clone https://github.com/lucaspfingsten/ccx
cd ccx
go build ./...
go test ./...
```

Stack: Go 1.22+, two third-party deps total (`charmbracelet/lipgloss` and `charmbracelet/huh`); everything else is stdlib.

---

## License

MIT — see [LICENSE](LICENSE).
