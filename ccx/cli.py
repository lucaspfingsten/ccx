"""ccx — extract context from Claude Code sessions for use in side agents."""

from __future__ import annotations

import argparse
import sys
from pathlib import Path
from typing import Optional

from . import __version__
from .clipboard import copy_to_clipboard
from .parser import parse_session
from .projects import (
    find_project_for_cwd,
    latest_session_jsonl,
    list_recent_sessions,
    project_dir_for,
    session_jsonl,
)
from .render import render_json, render_markdown


def _err(msg: str) -> None:
    print(msg, file=sys.stderr)


def _resolve_jsonl(args: argparse.Namespace) -> Optional[Path]:
    if args.project_path:
        project_dir = project_dir_for(args.project_path)
        if project_dir is None:
            _err(f"No Claude Code session directory for: {args.project_path}")
            _err("(Looked under ~/.claude/projects/ for the matching key.)")
            return None
    else:
        project_dir = find_project_for_cwd()
        if project_dir is None:
            _err("No Claude Code session for the current directory.")
            _err("Try `ccx --list` to pick from all projects, or pass a path: `ccx <project>`.")
            return None

    if args.session:
        p = session_jsonl(project_dir, args.session)
        if p is None:
            _err(f"Session {args.session!r} not found in {project_dir}")
            return None
        return p

    p = latest_session_jsonl(project_dir)
    if p is None:
        _err(f"No .jsonl session files in {project_dir}")
    return p


def _emit(jsonl_path: Path, args: argparse.Namespace) -> int:
    session = parse_session(
        jsonl_path,
        include_tool_calls=not args.no_tool_calls,
        max_turns=args.max_turns,
    )

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


def cmd_list(args: argparse.Namespace) -> int:
    sessions = list_recent_sessions(limit=args.limit)
    if not sessions:
        _err("No Claude Code sessions found in ~/.claude/projects/")
        return 1

    print()
    for i, s in enumerate(sessions, 1):
        prompt = s["first_prompt"]
        if len(prompt) > 70:
            prompt = prompt[:67] + "..."
        branch = f" [{s['git_branch']}]" if s["git_branch"] else ""
        print(f"  {i:2}. {s['project_name']}{branch}")
        if prompt:
            print(f"      {prompt}")
        print(f"      {s['session_id']}  ·  {s['message_count']} msgs")
        print()

    if not sys.stdin.isatty():
        _err("Re-run in an interactive shell to pick, or use `ccx --session <id>`.")
        return 0

    try:
        choice = input(f"Pick a session [1-{len(sessions)}] (q to quit): ").strip()
    except (EOFError, KeyboardInterrupt):
        print()
        return 0
    if not choice or choice.lower() in ("q", "quit"):
        return 0
    try:
        idx = int(choice) - 1
        if not 0 <= idx < len(sessions):
            raise ValueError
    except ValueError:
        _err(f"Invalid choice: {choice}")
        return 1

    chosen = sessions[idx]
    return _emit(Path(chosen["jsonl_path"]), args)


def main(argv: Optional[list[str]] = None) -> int:
    p = argparse.ArgumentParser(
        prog="ccx",
        description=(
            "Extract context from Claude Code sessions for use in side agents "
            "(Cursor, Codex, ChatGPT, second Claude Code session, ...). "
            "Reads ~/.claude/projects/, finds the latest isCompactSummary, "
            "and emits the slice of conversation that came after it."
        ),
    )
    p.add_argument(
        "project_path",
        nargs="?",
        help="Path to the project (default: current working directory).",
    )
    p.add_argument(
        "--list",
        action="store_true",
        help="Interactive picker over recent sessions across all projects.",
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

    jsonl_path = _resolve_jsonl(args)
    if jsonl_path is None:
        return 1
    return _emit(jsonl_path, args)


if __name__ == "__main__":
    raise SystemExit(main())
