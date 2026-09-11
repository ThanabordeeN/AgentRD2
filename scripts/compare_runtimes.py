#!/usr/bin/env python3
"""Compare the Python and Go runtimes on the shared scenario suite.

Both runtimes read the same ``scenarios/*.json`` files and assert the same
behaviour, so replaying them through each implementation and diffing the
results is the parity check for the Go port.

Example::

    python3 scripts/compare_runtimes.py
    python3 scripts/compare_runtimes.py --go-bin runtime-go/rdr2-npc
    python3 scripts/compare_runtimes.py --json
"""
from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
from pathlib import Path
from typing import Any, Dict, List, Optional

PROJECT_ROOT = Path(__file__).resolve().parents[1]
GO_DIR = PROJECT_ROOT / "runtime-go"

# Fields compared per scenario. Paths and timing are deliberately excluded.
COMPARED_STATE_FIELDS = ("ownership", "current_goal", "mood", "conversation_active", "last_event_name")


def run_python(scenarios: str) -> List[Dict[str, Any]]:
    result = subprocess.run(
        [sys.executable, "scenario_runner.py", "--json", "--scenarios", scenarios],
        cwd=str(PROJECT_ROOT),
        capture_output=True,
        text=True,
    )
    if result.returncode not in (0, 1):  # 1 == a scenario failed, still usable output
        raise SystemExit(f"python scenario runner failed ({result.returncode}):\n{result.stderr[-2000:]}")
    return _parse_json(result.stdout, engine="python")


def run_go(scenarios: str, go_bin: Optional[str]) -> List[Dict[str, Any]]:
    if go_bin:
        binary = Path(go_bin)
        if not binary.is_absolute():
            binary = PROJECT_ROOT / binary
        command = [str(binary), "--scenarios", scenarios, "--json"]
        cwd = PROJECT_ROOT
    else:
        go = shutil.which("go")
        if go is None:
            raise SystemExit("go not found on PATH; pass --go-bin path/to/rdr2-npc")
        command = [go, "run", "./cmd/rdr2-npc", "--scenarios", scenarios, "--json"]
        cwd = GO_DIR
    result = subprocess.run(command, cwd=str(cwd), capture_output=True, text=True)
    if result.returncode not in (0, 1):
        raise SystemExit(f"go scenario runner failed ({result.returncode}):\n{result.stderr[-2000:]}")
    return _parse_json(result.stdout, engine="go")


def _parse_json(stdout: str, *, engine: str) -> List[Dict[str, Any]]:
    start = stdout.find("{")
    array_start = stdout.find("[")
    if start == -1 or (array_start != -1 and array_start < start):
        start = array_start
    if start == -1:
        raise SystemExit(f"{engine} produced no JSON:\n{stdout[-2000:]}")
    payload = json.loads(stdout[start:])
    if isinstance(payload, dict):
        payload = payload.get("results", [])
    return list(payload)


def index(results: List[Dict[str, Any]]) -> Dict[str, Dict[str, Any]]:
    indexed: Dict[str, Dict[str, Any]] = {}
    for entry in results:
        key = Path(str(entry.get("path", ""))).name or str(entry.get("name", ""))
        indexed[key] = entry
    return indexed


def compare(py_results: List[Dict[str, Any]], go_results: List[Dict[str, Any]]) -> Dict[str, List[str]]:
    py_index = index(py_results)
    go_index = index(go_results)
    problems: Dict[str, List[str]] = {}

    for key in sorted(set(py_index) | set(go_index)):
        issues: List[str] = []
        py_entry = py_index.get(key)
        go_entry = go_index.get(key)
        if py_entry is None:
            problems[key] = ["missing from the python run"]
            continue
        if go_entry is None:
            problems[key] = ["missing from the go run"]
            continue

        if bool(py_entry.get("passed")) != bool(go_entry.get("passed")):
            issues.append(
                f"pass/fail differs: python={py_entry.get('passed')} go={go_entry.get('passed')}"
            )
            if go_entry.get("failures"):
                issues.append(f"go failures: {go_entry['failures']}")
            if py_entry.get("failures"):
                issues.append(f"python failures: {py_entry['failures']}")

        py_events = list(py_entry.get("event_names") or [])
        go_events = list(go_entry.get("event_names") or [])
        if py_events != go_events:
            issues.append(_diff_events(py_events, go_events))

        py_state = py_entry.get("final_state") or {}
        go_state = go_entry.get("final_state") or {}
        for field in COMPARED_STATE_FIELDS:
            if field in py_state or field in go_state:
                if py_state.get(field) != go_state.get(field):
                    issues.append(
                        f"{field}: python={py_state.get(field)!r} go={go_state.get(field)!r}"
                    )

        if issues:
            problems[key] = issues
    return problems


def _diff_events(py_events: List[str], go_events: List[str]) -> str:
    only_python = [name for name in py_events if name not in go_events]
    only_go = [name for name in go_events if name not in py_events]
    detail = f"event sequence differs (python {len(py_events)} vs go {len(go_events)})"
    if only_python:
        detail += f"; only python: {sorted(set(only_python))}"
    if only_go:
        detail += f"; only go: {sorted(set(only_go))}"
    if not only_python and not only_go:
        # Same multiset, different order.
        detail += f"; python order={py_events} go order={go_events}"
    return detail


def main(argv: Optional[List[str]] = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--scenarios", default=str(PROJECT_ROOT / "scenarios"))
    parser.add_argument("--go-bin", default=None, help="prebuilt rdr2-npc binary (skips 'go run')")
    parser.add_argument("--json", action="store_true", help="machine-readable report")
    args = parser.parse_args(argv)

    py_results = run_python(args.scenarios)
    go_results = run_go(args.scenarios, args.go_bin)
    problems = compare(py_results, go_results)
    total = len(index(py_results))

    if args.json:
        print(json.dumps(
            {
                "scenarios": total,
                "matching": total - len(problems),
                "mismatched": len(problems),
                "problems": problems,
                "ok": not problems,
            },
            indent=2,
        ))
        return 1 if problems else 0

    print(f"python scenarios: {len(py_results)}   go scenarios: {len(go_results)}")
    for key in sorted(index(py_results)):
        marker = "OK " if key not in problems else "DIFF"
        print(f"  [{marker}] {key}")
        for issue in problems.get(key, []):
            print(f"         - {issue}")
    print()
    if problems:
        print(f"{len(problems)}/{total} scenarios differ")
        return 1
    print(f"parity: {total}/{total} scenarios match")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
