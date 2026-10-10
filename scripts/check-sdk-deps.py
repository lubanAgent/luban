#!/usr/bin/env python3
"""Reject known company dependency roots in the standalone SDK module."""

from __future__ import annotations

import subprocess
import sys


def main() -> int:
    imports = subprocess.run(
        ["go", "list", "-deps", "-f", "{{.ImportPath}}", "./..."],
        check=True,
        capture_output=True,
        text=True,
    ).stdout.splitlines()
    blocked_roots = (
        "git.yukework.com/",
        "git.zuoyebang.cc/",
    )
    blocked = [item for item in imports if item.startswith(blocked_roots)]
    if blocked:
        print("Company dependencies are not allowed in sdk/:", file=sys.stderr)
        print("\n".join(sorted(set(blocked))), file=sys.stderr)
        return 1
    print("SDK dependency boundary: clean")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
