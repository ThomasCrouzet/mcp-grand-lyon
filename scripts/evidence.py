"""Small evidence helpers shared by local and CI verification commands."""

from __future__ import annotations

import hashlib
import json
import platform
import subprocess
import sys
from pathlib import Path


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def write_json(path: Path, data: object) -> None:
    path.write_text(json.dumps(data, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")


def source_identity(root: Path, output: Path) -> dict:
    def git(*args: str) -> bytes:
        return subprocess.check_output(["git", *args], cwd=root)

    diff = git("diff", "--binary", "HEAD")
    (output / "working.patch").write_bytes(diff)
    untracked = {
        name: digest(root / name)
        for name in git("ls-files", "--others", "--exclude-standard").decode().splitlines()
        if (root / name).is_file()
    }
    return {
        "revision": git("rev-parse", "HEAD").decode().strip(),
        "working_diff_sha256": hashlib.sha256(diff).hexdigest(),
        "untracked_sha256": untracked,
        "lock_sha256": digest(root / "uv.lock"),
        "command": [sys.executable, *sys.argv],
        "python": sys.version,
        "platform": platform.platform(),
    }
