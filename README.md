# ccx

> Give your side agents the same context as your main Claude Code session — without re-explaining anything.

`ccx` is a tiny CLI that extracts the *current working context* from a Claude Code session and outputs it as a Markdown block you can paste into ChatGPT, Cursor, Codex, another Claude Code window — anywhere.

No LLM calls. No data leaves your machine. Stdlib-only Python.

---

## The problem

You're deep in a Claude Code session, building something complex. Halfway through you want to ask a side question — maybe in ChatGPT, maybe in Cursor, maybe in a fresh Claude Code window — without burning tokens or polluting the focus of your main session.

The annoying part: the side agent has no idea what you've been working on. You re-explain the project, the stack, the current focus, the decisions. And even then it's a partial picture.

## The trick

When a Claude Code conversation gets long, Claude Code auto-compacts it: it asks the model to summarize what's happened so far, and saves that summary inside the session JSONL on disk (look for `isCompactSummary: true`). When you resume the session later, Claude Code injects that summary as the starting context — that's how it "remembers" where you left off.

`ccx` reuses exactly that summary. It finds the most recent compaction in the latest session for your project, grabs the verbatim summary, appends the conversation that happened after it, and outputs the whole thing.

**The smartest model already did the work; we just read its output.**

---

## Install

```bash
# from this repo (until PyPI release)
pip install git+https://github.com/lucaspfingsten/ccx

# or for an isolated install:
pipx install git+https://github.com/lucaspfingsten/ccx
```

Requires Python 3.9+. No third-party dependencies.

---

## Usage

```bash
# From inside a project directory — finds the latest session for the cwd
ccx

# Pick interactively across all projects (great when running from a side terminal)
ccx --list

# Copy to clipboard (paste into ChatGPT, Claude.ai, Cursor chat, ...)
ccx --copy

# Write to a file (e.g. for `@`-references in Cursor chat or Claude Code Read)
ccx --output .ccx.md

# Pipe straight into another tool
ccx | pbcopy
ccx | codex "based on this context, refactor the auth flow"

# Specific project / specific session
ccx ~/code/myapp
ccx --session ab7f6a92-1f1a-4b02-b187-3c81c77317d8

# Cap the number of turns after compaction
ccx --max-turns 20

# Strip tool-call summary lines (just user/assistant text)
ccx --no-tool-calls

# JSON output for programmatic use
ccx --format json
```

### Use cases

| Side agent | How |
|------------|-----|
| **ChatGPT / Claude.ai** | `ccx --copy`, paste into chat |
| **Cursor** | `ccx --output .ccx.md`, then `@.ccx.md` in Cursor chat |
| **Codex CLI** | `ccx \| codex "..."` |
| **Second Claude Code session** | `ccx --output .ccx.md`, then start the new session with `Read .ccx.md` |

---

## What gets included

For each session, `ccx` outputs:

1. **Header**: project name, session id, git branch, last compaction time and token counts
2. **Conversation Summary**: the verbatim text of the most recent `isCompactSummary` (if the session has been compacted)
3. **Continued Conversation**: every user and assistant turn after that compaction, with optional one-line summaries of tool calls (`↳ Read: foo.py`, `↳ Bash: npm test`, …)

What's filtered out:

- Queue operations, file-history snapshots, todo reminders, hook injections
- IDE-injected events (`<ide_opened_file>`, `<ide_selection>`)
- Progress events, raw tool results, sidechain (subagent) traces
- Assistant `thinking` blocks

If a session has never been compacted, the whole conversation is emitted (filtered the same way).

---

## Privacy

Everything runs locally. `ccx` reads `~/.claude/projects/<project-key>/<session-id>.jsonl` and writes to stdout / clipboard / file you specify. **No network requests, ever.**

---

## How it works (technical)

Claude Code stores each session as a JSONL file at `~/.claude/projects/<project-key>/<session-id>.jsonl`, where `<project-key>` is the absolute project path with `/` replaced by `-`.

A session is a stream of events: `user`, `assistant`, `system`, `attachment`, `queue-operation`, etc. When auto-compaction fires, Claude Code writes:

1. A `system` event carrying `compactMetadata: { trigger, preTokens, postTokens, durationMs, ... }`
2. A `user` event with `isCompactSummary: true` whose content is the full summary text.

A session can be compacted multiple times. `ccx` finds the **last** `isCompactSummary` and emits it followed by all subsequent meaningful turns.

---

## Roadmap

- [ ] Cursor session extraction (different format, same idea)
- [ ] Codex CLI session extraction
- [ ] Optional MCP server wrapper (so any MCP-aware agent can call `ccx` natively)
- [ ] Optional Claude Code skill (`/ccx` to inject into the current session)
- [ ] PyPI release as `ccx-cli`

---

## Contributing

PRs welcome.

```bash
git clone https://github.com/lucaspfingsten/ccx
cd ccx
pip install -e ".[dev]"
pytest
```

---

## License

MIT — see [LICENSE](LICENSE).
