# ccx - Sidequests

> Give your side agents the same context as your main coding agent session — without re-explaining anything.

`ccx` is a tiny CLI that extracts the *current working context* from a coding-agent session — Claude Code, Cursor, or Codex CLI — and outputs it as a Markdown block you can paste into ChatGPT, another agent, a fresh window — anywhere.

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

## vs. Claude Code's `/export`

`/export` dumps the terminal scrollback — banner, rendered diffs, status lines and all — for a human to read. `ccx` reads the underlying session log and emits structured Markdown for *another agent* to read. On the same conversation, `ccx` is roughly half the size with no UI noise.

Three things `/export` structurally can't do:

- **Run in parallel.** It's a slash command, so it queues behind whatever the main runner is doing. `ccx` reads the session file from disk, from any terminal, while the main agent keeps working.
- **Reach across sessions.** `--list` / `--session` work on any past session, not just the active one.
- **Span tools.** `ccx` aims to read Claude Code, Cursor, and Codex sessions through one interface. `/export` is Claude-Code-only by definition.

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
# From inside a project directory — finds the latest Claude Code session for the cwd
ccx

# Same, but read from Cursor or Codex CLI instead
ccx --source cursor
ccx --source codex

# Pick interactively across all projects (great when running from a side terminal)
ccx --list                    # claude only
ccx --source all --list       # claude + cursor + codex, newest first

# Copy to clipboard (paste into ChatGPT, Claude.ai, Cursor chat, ...)
ccx --copy

# Write to a file (e.g. for `@`-references in Cursor chat or Claude Code Read)
ccx --output .ccx.md

# Pipe straight into another tool
ccx | pbcopy
ccx --source cursor | codex "based on this context, refactor the auth flow"

# Specific project / specific session
ccx ~/code/myapp
ccx --session ab7f6a92-1f1a-4b02-b187-3c81c77317d8

# Cap the number of turns after the last compaction (Claude Code) or in total
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

1. **Header**: project name, session id, git branch, and last compaction time when applicable
2. **Conversation Summary** *(Claude Code & Codex CLI)*: if the session has been compacted, the model's curated summary — `isCompactSummary` text for Claude Code, or `replacement_history` (rendered as role-labeled blocks) for Codex
3. **Conversation**: every user and assistant turn after the last compaction (or the full chat if there was none), with optional one-line summaries of tool calls (`↳ Read: foo.py`, `↳ Bash: npm test`, …)

What's filtered out:

- Claude Code: queue operations, file-history snapshots, todo reminders, hook injections, IDE events (`<ide_opened_file>`, `<ide_selection>`), raw tool results, sidechain (subagent) traces, `thinking` blocks
- Codex CLI: `developer` system messages, permission/environment preambles, `reasoning` events, `function_call_output` blobs
- Cursor: empty bubbles, rich-text duplicates of plain text

---

## Privacy

Everything runs locally. `ccx` reads:

- Claude Code: `~/.claude/projects/<project-key>/<session-id>.jsonl`
- Codex CLI: `~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl`
- Cursor: `~/Library/Application Support/Cursor/User/{workspaceStorage,globalStorage}/state.vscdb` (read-only)

Writes only to stdout / clipboard / file you specify. **No network requests, ever.**

---

## How it works (technical)

Each supported tool persists its session somewhere on disk; `ccx` reads it and renders a uniform Markdown / JSON shape.

**Claude Code** stores each session as a JSONL file at `~/.claude/projects/<project-key>/<session-id>.jsonl`, where `<project-key>` is the absolute project path with `/` replaced by `-`.

A session is a stream of events: `user`, `assistant`, `system`, `attachment`, `queue-operation`, etc. When auto-compaction fires, Claude Code writes:

1. A `system` event carrying `compactMetadata: { trigger, preTokens, postTokens, durationMs, ... }`
2. A `user` event with `isCompactSummary: true` whose content is the full summary text.

A session can be compacted multiple times. `ccx` finds the **last** `isCompactSummary` and emits it followed by all subsequent meaningful turns.

**Codex CLI** writes each session as a JSONL "rollout" at `~/.codex/sessions/YYYY/MM/DD/rollout-<ts>-<id>.jsonl`. Each line is `{timestamp, type, payload}`; the first line carries `cwd` and `id`. Conversation events arrive as `response_item` payloads (messages with `role` + `input_text`/`output_text` parts, plus `function_call` / `function_call_output` for tool use). When Codex compacts (auto or via `/compact`), it writes a top-level `type: "compacted"` event whose `payload.replacement_history` is a curated list of messages that replaces all prior history; `ccx` finds the **last** such event and emits its `replacement_history` as the summary, followed by everything after.

**Cursor** splits chat data across two SQLite stores. Per-workspace `~/Library/Application Support/Cursor/User/workspaceStorage/<hash>/state.vscdb` maps the workspace hash to a project folder and lists composer (chat) IDs. The global `globalStorage/state.vscdb` holds the actual messages keyed `bubbleId:<composerId>:<bubbleId>`, with `type: 1=user|2=assistant`, `text`, `createdAt`, and `toolFormerData` for tool calls.

---

## Roadmap

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
