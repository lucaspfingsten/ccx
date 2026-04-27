"""Cross-platform clipboard support using only stdlib."""

from __future__ import annotations

import shutil
import subprocess
import sys


def copy_to_clipboard(text: str) -> bool:
    """Copy `text` to the system clipboard. Returns True on success."""
    cmd: list[str] | None = None
    if sys.platform == "darwin":
        cmd = ["pbcopy"]
    elif sys.platform == "win32":
        cmd = ["clip"]
    elif sys.platform.startswith("linux") or sys.platform.startswith("freebsd"):
        if shutil.which("wl-copy"):
            cmd = ["wl-copy"]
        elif shutil.which("xclip"):
            cmd = ["xclip", "-selection", "clipboard"]
        elif shutil.which("xsel"):
            cmd = ["xsel", "--clipboard", "--input"]

    if not cmd:
        return False

    try:
        subprocess.run(cmd, input=text.encode("utf-8"), check=True)
        return True
    except (subprocess.CalledProcessError, FileNotFoundError, OSError):
        return False
