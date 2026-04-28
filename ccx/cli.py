"""ccx — extract context from agent sessions for use in side agents."""

from __future__ import annotations

import argparse
import sys
from pathlib import Path
from typing import Optional

from . import __version__
from . import sources
from .clipboard import copy_to_clipboard
from .render import render_json, render_markdown


def _err(msg: str) -> None:
    print(msg, file=sys.stderr)


def _emit_session(session, args: argparse.Namespace) -> int:
    if args.format == "json":
        out = render_json(session)
    else:
        out = render_markdown(session)

    if args.output:
        Path(args.output).write_text(out, encoding="utf-8")
        _err(f"Wrote {len(out)} bytes to {args.output}")
    else:
        sys.stdout.write(out)

    if args.copy:
        if copy_to_clipboard(out):
            _err(f"Copied {len(out)} bytes to clipboard.")
        else:
            _err("Could not copy to clipboard (no pbcopy/xclip/wl-copy/clip found).")
            return 1

    return 0


def _emit_for_source(source_name: str, ref, args: argparse.Namespace) -> int:
    src = sources.get(source_name)
    session = src.parse(
        ref,
        include_tool_calls=not args.no_tool_calls,
        max_turns=args.max_turns,
    )
    return _emit_session(session, args)


def _format_list_row(i: int, entry: dict) -> str:
    prompt = entry.get("first_prompt", "") or ""
    if len(prompt) > 70:
        prompt = prompt[:67] + "..."
    branch = f" [{entry['git_branch']}]" if entry.get("git_branch") else ""
    src_tag = f"[{entry.get('source', '?')}]"
    lines = [
        f"  {i:2}. {src_tag} {entry.get('project_name', '?')}{branch}",
    ]
    if prompt:
        lines.append(f"      {prompt}")
    sid = entry.get("session_id", "")
    msgs = entry.get("message_count", 0)
    msg_part = f"  ·  {msgs} msgs" if msgs else ""
    lines.append(f"      {sid}{msg_part}")
    return "\n".join(lines)


def cmd_list(args: argparse.Namespace) -> int:
    if args.source == "all":
        entries = sources.list_all(limit=args.limit)
    else:
        src = sources.get(args.source)
        entries = src.list_recent(limit=args.limit)

    if not entries:
        _err(f"No {args.source} sessions found.")
        return 1

    print()
    for i, e in enumerate(entries, 1):
        print(_format_list_row(i, e))
        print()

    if not sys.stdin.isatty():
        _err("Re-run in an interactive shell to pick, or use `ccx --session <id>`.")
        return 0

    try:
        choice = input(f"Pick a session [1-{len(entries)}] (q to quit): ").strip()
    except (EOFError, KeyboardInterrupt):
        print()
        return 0
    if not choice or choice.lower() in ("q", "quit"):
        return 0
    try:
        idx = int(choice) - 1
        if not 0 <= idx < len(entries):
            raise ValueError
    except ValueError:
        _err(f"Invalid choice: {choice}")
        return 1

    chosen = entries[idx]
    session = sources.dispatch_parse(
        chosen,
        include_tool_calls=not args.no_tool_calls,
        max_turns=args.max_turns,
    )
    return _emit_session(session, args)


def main(argv: Optional[list[str]] = None) -> int:
    p = argparse.ArgumentParser(
        prog="ccx",
        description=(
            "Extract context from agent sessions (Claude Code, Cursor, Codex) "
            "for use in side agents. Reads sessions from disk and emits a "
            "Markdown / JSON block tuned for handoff."
        ),
    )
    p.add_argument(
        "project_path",
        nargs="?",
        help="Path to the project (default: current working directory).",
    )
    p.add_argument(
        "--source",
        choices=["claude", "cursor", "codex", "all"],
        default="claude",
        help=(
            "Which agent's sessions to read. `all` is only valid with --list. "
            "Default: claude."
        ),
    )
    p.add_argument(
        "--list",
        action="store_true",
        help="Interactive picker over recent sessions for the chosen source(s).",
    )
    p.add_argument(
        "--limit",
        type=int,
        default=15,
        help="With --list, max entries to show (default: 15).",
    )
    p.add_argument(
        "--session",
        metavar="ID",
        help="Specific session id (default: latest in the resolved project).",
    )
    p.add_argument(
        "--copy",
        action="store_true",
        help="Also copy the rendered output to the system clipboard.",
    )
    p.add_argument(
        "--output",
        "-o",
        metavar="FILE",
        help="Write output to FILE instead of stdout.",
    )
    p.add_argument(
        "--max-turns",
        type=int,
        default=None,
        help="Cap the number of post-compaction turns to include (default: all).",
    )
    p.add_argument(
        "--no-tool-calls",
        action="store_true",
        help="Omit one-line tool-call summaries from assistant turns.",
    )
    p.add_argument(
        "--format",
        choices=["markdown", "json"],
        default="markdown",
        help="Output format (default: markdown).",
    )
    p.add_argument("--version", action="version", version=f"ccx {__version__}")

    args = p.parse_args(argv)

    if args.list:
        return cmd_list(args)

    if args.source == "all":
        _err("--source all is only valid with --list.")
        return 2

    src = sources.get(args.source)
    ref, err = src.resolve(args.project_path, args.session)
    if ref is None:
        _err(err or f"No {args.source} session found.")
        return 1
    return _emit_for_source(args.source, ref, args)


if __name__ == "__main__":
    raise SystemExit(main())
