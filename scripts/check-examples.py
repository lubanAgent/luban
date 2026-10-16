"""Run both offline examples and verify their documented, equal outputs."""

import json
import pathlib
import subprocess

sdk = pathlib.Path(__file__).resolve().parents[1] / "sdk"
expected = {"summary": "客户想了解周末课程，顾问将于明天下午发送课程安排并电话确认。"}
for name in ("call-summary", "eino-call-summary"):
    result = subprocess.run(
        ["go", "run", f"./examples/{name}"],
        cwd=sdk,
        encoding="utf-8",
        capture_output=True,
        check=True,
        timeout=120,
    )
    lines = [line[len("输出："):] for line in result.stdout.splitlines() if line.startswith("输出：")]
    if len(lines) != 1 or json.loads(lines[0]) != expected:
        raise SystemExit(f"{name}: unexpected output\n{result.stdout}\n{result.stderr}")
    print(f"{name}: HTTP flow returned the expected summary")
