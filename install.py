#!/usr/bin/env python3
"""One-command installer and doctor for the RDR2 Living NPC agent.

The runtime core has **zero third-party dependencies**, so a working install
is usually just "clone and run".  This script removes the remaining friction:

* verifies the Python version and project layout
* profiles the optional extras (Google ADK, local audio, native bridge)
* writes a local ``.env`` so API keys never need shell exports
* optionally creates a virtualenv and installs the extras you asked for
* optionally downloads the official ScriptHookRDR2 SDK and builds the bridge
* runs a smoke test and prints exactly what to run next

Usage::

    python3 install.py                # guided install (asks before changes)
    python3 install.py --check        # doctor only, never writes anything
    python3 install.py --yes          # non-interactive defaults
    python3 install.py --json         # machine-readable report

Nothing here requires pip, a compiler, or network access unless you request
the corresponding optional feature.
"""
from __future__ import annotations

import argparse
import getpass
import json
import os
import platform
import shutil
import subprocess
import sys
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Callable, Dict, List, Optional, Sequence

PROJECT_ROOT = Path(__file__).resolve().parent
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

from runtime.env import (  # noqa: E402  (import after sys.path fix)
    DEFAULT_ENV_FILES,
    load_env,
    parse_env_text,
    upsert_env_file,
)

MIN_PYTHON = (3, 10)
SDK_DIR_NAME = "ScriptHookRDR2_SDK_1.0.1207.73"

OK = "ok"
WARN = "warn"
FAIL = "fail"
SKIP = "skip"

_MARKS = {OK: "PASS", WARN: "WARN", FAIL: "FAIL", SKIP: "SKIP"}


# ---------------------------------------------------------------------------
# Reporting
# ---------------------------------------------------------------------------

@dataclass
class Check:
    name: str
    status: str
    detail: str = ""
    hint: str = ""

    def to_dict(self) -> Dict[str, str]:
        return {
            "name": self.name,
            "status": self.status,
            "detail": self.detail,
            "hint": self.hint,
        }


@dataclass
class Context:
    """Everything the steps need, resolved once from the CLI arguments."""

    args: argparse.Namespace
    interactive: bool = False
    env_path: Path = field(default_factory=lambda: PROJECT_ROOT / ".env")
    env_applied: Dict[str, str] = field(default_factory=dict)
    notes: List[str] = field(default_factory=list)

    def log(self, message: str) -> None:
        print(message, flush=True)


def _use_colour() -> bool:
    if os.environ.get("NO_COLOR"):
        return False
    return sys.stdout.isatty()


def _paint(text: str, code: str) -> str:
    if not _use_colour():
        return text
    return f"\033[{code}m{text}\033[0m"


def print_report(checks: Sequence[Check], title: str) -> None:
    if not checks:
        return
    print()
    print(_paint(title, "1"))
    width = max(len(check.name) for check in checks)
    for check in checks:
        colour = {OK: "32", WARN: "33", FAIL: "31", SKIP: "90"}[check.status]
        label = _paint(_MARKS[check.status].ljust(4), colour)
        line = f"  {label} {check.name.ljust(width)}  {check.detail}"
        print(line.rstrip())
        if check.hint and check.status in {WARN, FAIL}:
            print(f"         {_paint('->', '90')} {check.hint}")


# ---------------------------------------------------------------------------
# Doctor checks
# ---------------------------------------------------------------------------

def check_python() -> Check:
    current = sys.version_info
    version = f"{current.major}.{current.minor}.{current.micro}"
    if (current.major, current.minor) >= MIN_PYTHON:
        where = sys.executable
        return Check("python", OK, f"{version} ({where})")
    wanted = ".".join(str(part) for part in MIN_PYTHON)
    return Check(
        "python",
        FAIL,
        f"{version} is too old",
        f"install Python {wanted}+ and re-run this script with it",
    )


def check_venv() -> Check:
    if sys.prefix != getattr(sys, "base_prefix", sys.prefix):
        return Check("environment", OK, f"virtualenv active ({sys.prefix})")
    return Check(
        "environment",
        OK,
        "system interpreter (fine: core needs no packages)",
        "add --venv if you want the optional extras isolated in .venv/",
    )


def check_layout() -> Check:
    required = [
        "runtime/main.py",
        "config/settings.json",
        "data/profiles",
        "data/quests",
        "data/wiki",
        "scenarios",
        "tests",
    ]
    missing = [item for item in required if not (PROJECT_ROOT / item).exists()]
    if missing:
        return Check(
            "layout",
            FAIL,
            "missing: " + ", ".join(missing),
            "run install.py from a full checkout of the repository",
        )
    return Check("layout", OK, f"{len(required)} required paths present")


def check_imports() -> Check:
    modules = [
        "runtime.main",
        "runtime.agent.npc_agent",
        "runtime.agent.openai_compat",
        "runtime.tools",
        "runtime.ipc.server",
    ]
    broken: List[str] = []
    for module in modules:
        try:
            __import__(module)
        except Exception as exc:  # pragma: no cover - defensive
            broken.append(f"{module} ({exc.__class__.__name__}: {exc})")
    if broken:
        return Check(
            "runtime import",
            FAIL,
            "; ".join(broken),
            "this is a bug, not a setup problem - re-clone or report it",
        )
    return Check("runtime import", OK, f"{len(modules)} modules import cleanly")


def check_tool_catalog() -> Check:
    try:
        from runtime.tools import build_default_registry
    except Exception as exc:  # pragma: no cover - defensive
        return Check("tool catalog", FAIL, f"cannot import registry: {exc}")
    try:
        registry = build_default_registry()
        names = list(registry.names())
    except Exception as exc:  # pragma: no cover - defensive
        return Check("tool catalog", FAIL, f"registry build failed: {exc}")
    if not names:
        return Check("tool catalog", FAIL, "registry is empty")
    return Check("tool catalog", OK, f"{len(names)} agent tools registered")


def check_data_packs() -> Check:
    counts: Dict[str, Any] = {}
    problems: List[str] = []

    profiles_dir = PROJECT_ROOT / "data/profiles"
    profiles = sorted(profiles_dir.glob("*.json")) if profiles_dir.is_dir() else []
    counts["profiles"] = len(profiles)

    quests_dir = PROJECT_ROOT / "data/quests"
    quests = sorted(quests_dir.glob("*.json")) if quests_dir.is_dir() else []
    counts["quests"] = len(quests)

    characters = PROJECT_ROOT / "data/wiki/characters.json"
    if characters.is_file():
        try:
            payload = json.loads(characters.read_text(encoding="utf-8"))
            counts["characters"] = len(payload.get("characters", payload))
        except (OSError, ValueError) as exc:
            problems.append(f"characters.json unreadable ({exc})")
    else:
        problems.append("data/wiki/characters.json missing")

    if not profiles:
        problems.append("no NPC profiles found")

    if problems:
        return Check(
            "context packs",
            WARN,
            "; ".join(problems),
            "run: python3 scripts/fetch_character_context.py (needs network)",
        )
    summary = ", ".join(f"{value} {key}" for key, value in counts.items() if value)
    return Check("context packs", OK, summary or "local context present")


def check_settings() -> Check:
    path = PROJECT_ROOT / "config/settings.json"
    if not path.is_file():
        return Check(
            "settings",
            WARN,
            "config/settings.json missing; built-in defaults will be used",
            "restore config/settings.json to customise the runtime",
        )
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, ValueError) as exc:
        return Check("settings", FAIL, f"invalid JSON: {exc}", "fix config/settings.json")
    adk = payload.get("adk", {})
    model = adk.get("model", "(unset)")
    return Check("settings", OK, f"valid JSON, model={model}")


def _candidate_env_files(args: Optional[argparse.Namespace] = None) -> List[Path]:
    names = list(DEFAULT_ENV_FILES)
    if args is not None and args.env_file:
        custom = Path(args.env_file)
        if custom.is_absolute():
            return [custom] + [PROJECT_ROOT / name for name in names]
        if str(custom) not in names:
            names.append(str(custom))
    return [PROJECT_ROOT / name for name in names]


def check_env_files(args: Optional[argparse.Namespace] = None) -> Check:
    existing = [path for path in _candidate_env_files(args) if path.is_file()]
    if not existing:
        return Check(
            "env file",
            SKIP,
            "none yet (not required for --backend rule)",
            "install.py will create .env when you provide an API key",
        )
    names = ", ".join(path.name for path in existing)
    return Check("env file", OK, names)


def check_api_key(args: Optional[argparse.Namespace] = None) -> Check:
    from runtime.agent.openai_compat import OPENCODE_AUTH_PATH, resolve_api_key

    if args is not None and args.api_key:
        return Check("api key", OK, "provided on the command line")

    env_names = ("OPENCODE_API_KEY", "OPENAI_API_KEY")
    for name in env_names:
        if os.environ.get(name):
            return Check("api key", OK, f"found in environment ({name})")

    for path in _candidate_env_files(args):
        if not path.is_file():
            continue
        values = parse_env_text(path.read_text(encoding="utf-8", errors="replace"))
        for name in env_names:
            if values.get(name):
                return Check("api key", OK, f"found in {path.name} ({name})")

    if OPENCODE_AUTH_PATH.is_file():
        try:
            payload = json.loads(OPENCODE_AUTH_PATH.read_text(encoding="utf-8"))
        except (OSError, ValueError):
            payload = {}
        if (payload.get("opencode-go") or {}).get("key"):
            return Check("api key", OK, f"found in {OPENCODE_AUTH_PATH}")
    if resolve_api_key():
        return Check("api key", OK, "resolved from local OpenCode credentials")

    return Check(
        "api key",
        SKIP,
        "not configured (only needed for --backend llm / adk)",
        "run: python3 install.py --api-key <KEY>   (writes .env)",
    )


def check_timeline_writable() -> Check:
    target = PROJECT_ROOT / "data/timelines"
    try:
        target.mkdir(parents=True, exist_ok=True)
        probe = target / ".install_probe"
        probe.write_text("ok", encoding="utf-8")
        probe.unlink()
    except OSError as exc:
        return Check(
            "timeline store",
            FAIL,
            f"not writable: {exc}",
            "fix permissions on data/timelines",
        )
    return Check("timeline store", OK, "data/timelines is writable")


def _sdk_root(args: argparse.Namespace) -> Path:
    if args.sdk_root:
        return Path(args.sdk_root)
    env_root = os.environ.get("RDR2AI_SCRIPTHOOK_ROOT")
    if env_root:
        return Path(env_root)
    return PROJECT_ROOT / "bridge/.sdk" / SDK_DIR_NAME


def check_bridge_sdk(args: argparse.Namespace) -> Check:
    root = _sdk_root(args)
    header = root / "inc/main.h"
    lib = root / "lib/ScriptHookRDR2.lib"
    if header.is_file() and lib.is_file():
        return Check("ScriptHook SDK", OK, str(root))
    if root.exists():
        return Check(
            "ScriptHook SDK",
            WARN,
            f"incomplete at {root}",
            "re-run with --with-asi to download it again (--force)",
        )
    return Check(
        "ScriptHook SDK",
        SKIP,
        "not installed (only needed for the in-game .asi)",
        "run: python3 install.py --with-asi",
    )


def _which(*names: str) -> Optional[str]:
    for name in names:
        found = shutil.which(name)
        if found:
            return found
    return None


def check_build_tools(args: argparse.Namespace) -> Check:
    cmake = _which("cmake")
    compiler = _which("g++", "clang++", "cl")
    parts = []
    parts.append(f"cmake={cmake or 'no'}")
    parts.append(f"c++={compiler or 'no'}")
    if platform.system() == "Windows":
        parts.append("platform=windows")
    if cmake and compiler:
        return Check("bridge toolchain", OK, ", ".join(parts))
    return Check(
        "bridge toolchain",
        SKIP,
        ", ".join(parts),
        "optional: install CMake + a C++17 compiler to build the native bridge",
    )


def check_network() -> Check:
    import socket

    host = "opencode.ai"
    try:
        socket.setdefaulttimeout(4.0)
        with socket.create_connection((host, 443), timeout=4.0):
            pass
    except OSError as exc:
        return Check(
            "network",
            WARN,
            f"cannot reach {host} ({exc.__class__.__name__})",
            "offline install still works: model and wiki-pack refresh need network",
        )
    finally:
        socket.setdefaulttimeout(None)
    return Check("network", OK, f"reached {host}:443")


# ---------------------------------------------------------------------------
# Install steps
# ---------------------------------------------------------------------------

def step_local_env(ctx: Context) -> Check:
    """Load .env files and record what they provided."""
    applied = load_env()
    ctx.env_applied = applied
    if not applied:
        return Check("load .env", SKIP, "no .env values to load")
    keys = ", ".join(sorted(applied))
    return Check("load .env", OK, f"loaded {keys}")


def _prompt_yes_no(question: str, default: bool = False) -> bool:
    suffix = "[Y/n]" if default else "[y/N]"
    try:
        answer = input(f"{question} {suffix} ").strip().lower()
    except (EOFError, KeyboardInterrupt):
        print()
        return False
    if not answer:
        return default
    return answer in {"y", "yes"}


def _prompt_api_key() -> Optional[str]:
    try:
        value = getpass.getpass("Paste your API key (input hidden, blank to skip): ").strip()
    except (EOFError, KeyboardInterrupt):
        print()
        return None
    return value or None


def step_write_env(ctx: Context) -> Check:
    """Persist the API key / endpoint so future runs need no exports."""
    args = ctx.args
    values: Dict[str, str] = {}

    if args.api_key:
        values[args.api_key_env or "OPENCODE_API_KEY"] = args.api_key
    elif ctx.interactive and not args.offline and not args.check:
        existing = check_api_key(args)
        if existing.status != OK and _prompt_yes_no(
            "No API key detected. Save one to .env now?", default=False
        ):
            key = _prompt_api_key()
            if key:
                values[args.api_key_env or "OPENCODE_API_KEY"] = key

    if args.api_base:
        values["RDR2AI_API_BASE"] = args.api_base
    if args.model:
        values["RDR2AI_MODEL"] = args.model

    if not values:
        return Check("write .env", SKIP, "nothing to save")

    path = upsert_env_file(ctx.env_path, values)
    # Keep the process environment consistent with what we just wrote.
    os.environ.update(values)
    names = ", ".join(sorted(values))
    return Check("write .env", OK, f"{path.name} updated ({names})")


def _run(command: Sequence[str], *, quiet: bool = False) -> subprocess.CompletedProcess:
    return subprocess.run(
        list(command),
        cwd=str(PROJECT_ROOT),
        capture_output=True,
        text=True,
        check=False,
    )


def step_venv(ctx: Context) -> Check:
    if not ctx.args.venv:
        return Check("virtualenv", SKIP, "not requested (--venv)")

    venv_dir = PROJECT_ROOT / ".venv"
    python = venv_dir / ("Scripts/python.exe" if platform.system() == "Windows" else "bin/python")
    if python.is_file():
        return Check("virtualenv", OK, f"reusing {venv_dir}")

    result = _run([sys.executable, "-m", "venv", str(venv_dir)])
    if result.returncode != 0 or not python.is_file():
        detail = (result.stderr or result.stdout or "").strip().splitlines()
        return Check(
            "virtualenv",
            WARN,
            f"could not create {venv_dir}",
            detail[-1] if detail else "install python3-venv (Debian/Ubuntu) and retry",
        )
    return Check("virtualenv", OK, f"created {venv_dir}")


def _venv_python() -> str:
    venv_dir = PROJECT_ROOT / ".venv"
    candidate = venv_dir / ("Scripts/python.exe" if platform.system() == "Windows" else "bin/python")
    return str(candidate) if candidate.is_file() else sys.executable


def step_optional_deps(ctx: Context) -> Check:
    args = ctx.args
    packages: List[str] = []
    if args.with_adk:
        packages += ["google-adk", "litellm"]
    if args.with_audio:
        packages += ["faster-whisper", "pyttsx3"]

    if not packages:
        return Check("optional packages", SKIP, "none requested")

    if args.offline:
        return Check(
            "optional packages",
            WARN,
            "skipped (--offline)",
            "re-run without --offline to install: " + " ".join(packages),
        )

    interpreter = _venv_python()
    result = _run([interpreter, "-m", "pip", "install", *packages])
    if result.returncode != 0:
        tail = (result.stderr or result.stdout or "").strip().splitlines()
        return Check(
            "optional packages",
            WARN,
            f"pip install failed ({len(packages)} packages)",
            tail[-1] if tail else "check network/pip availability",
        )
    return Check("optional packages", OK, " ".join(packages))


def step_sdk(ctx: Context) -> Check:
    args = ctx.args
    root = _sdk_root(args)
    if (root / "inc/main.h").is_file() and not args.force:
        return Check("ScriptHook SDK", OK, f"already present at {root}")
    if args.offline:
        return Check("ScriptHook SDK", WARN, "skipped (--offline)")

    command = [
        sys.executable,
        str(PROJECT_ROOT / "scripts/install_scripthook_sdk.py"),
        "--dest",
        str(root),
    ]
    if args.force:
        command.append("--force")
    result = _run(command)
    if result.returncode != 0 or not (root / "inc/main.h").is_file():
        tail = (result.stderr or result.stdout or "").strip().splitlines()
        return Check(
            "ScriptHook SDK",
            WARN,
            "download failed",
            tail[-1] if tail else "download manually from dev-c.com/rdr2/scripthookrdr2",
        )
    return Check("ScriptHook SDK", OK, f"installed at {root}")


def step_bridge(ctx: Context) -> Check:
    args = ctx.args
    if not (args.with_bridge or args.with_asi):
        return Check("bridge build", SKIP, "not requested (--with-bridge / --with-asi)")

    build_dir = PROJECT_ROOT / "bridge/build"
    cmake = _which("cmake")

    if cmake:
        configure = [
            cmake,
            "-S",
            str(PROJECT_ROOT / "bridge"),
            "-B",
            str(build_dir),
            "-DCMAKE_BUILD_TYPE=Release",
        ]
        if args.with_asi:
            configure.append("-DRDR2AI_HAS_SCRIPTHOOK=ON")
            sdk_root = _sdk_root(args)
            if (sdk_root / "inc/main.h").is_file():
                configure.append(f"-DSCRIPTHOOK_RDR2_ROOT={sdk_root}")
        result = _run(configure)
        if result.returncode != 0:
            tail = (result.stderr or result.stdout).strip().splitlines()
            return Check("bridge build", WARN, "cmake configure failed", tail[-1] if tail else "")
        result = _run([cmake, "--build", str(build_dir), "--config", "Release"])
        if result.returncode != 0:
            tail = (result.stderr or result.stdout).strip().splitlines()
            return Check("bridge build", WARN, "cmake build failed", tail[-1] if tail else "")
        produced = sorted(
            str(path.relative_to(PROJECT_ROOT))
            for pattern in ("*.asi", "*.dll", "bridge_core_test*")
            for path in build_dir.rglob(pattern)
        )
        detail = ", ".join(produced) if produced else "built"
        return Check("bridge build", OK, detail)

    compiler = _which("g++", "clang++")
    if compiler and not args.with_asi:
        # No CMake: fall back to a direct portable-core build.
        target = build_dir / "bridge_core_test"
        build_dir.mkdir(parents=True, exist_ok=True)
        sources = [
            "bridge/tests/bridge_core_test.cpp",
            "bridge/src/action_executor.cpp",
            "bridge/src/ped_scanner.cpp",
            "bridge/src/story_gate.cpp",
        ]
        command = [
            compiler,
            "-std=c++17",
            "-O2",
            "-pthread",
            "-I",
            "bridge/include",
            "-I",
            "bridge/tests",
            *sources,
            "-o",
            str(target),
        ]
        result = _run(command)
        if result.returncode != 0:
            tail = (result.stderr or result.stdout).strip().splitlines()
            return Check("bridge build", WARN, "g++ build failed", tail[-1] if tail else "")
        smoke = _run([str(target)])
        if smoke.returncode != 0:
            return Check("bridge build", WARN, "core test binary failed to run")
        return Check(
            "bridge build",
            OK,
            f"{target.relative_to(PROJECT_ROOT)} (portable core, no CMake)",
        )

    if args.with_asi:
        return Check(
            "bridge build",
            WARN,
            "the .asi needs CMake + MSVC on Windows",
            "install Visual Studio Build Tools + CMake, then re-run --with-asi",
        )
    return Check(
        "bridge build",
        SKIP,
        "no cmake or C++ compiler found",
        "optional: the Python runtime works without the native bridge",
    )


def step_smoke(ctx: Context) -> Check:
    args = ctx.args
    if args.no_smoke:
        return Check("smoke test", SKIP, "disabled (--no-smoke)")
    result = _run([sys.executable, "run_demo.py"])
    if result.returncode != 0:
        tail = (result.stderr or result.stdout).strip().splitlines()
        return Check("smoke test", FAIL, "run_demo.py failed", tail[-1] if tail else "")
    # The demo prints a JSON summary; confirm it produced a decision.
    if "AI_CONVERSATION" not in result.stdout and "NPC_SPOKE" not in result.stdout:
        return Check("smoke test", WARN, "demo ran but produced no NPC speech")
    return Check("smoke test", OK, "run_demo.py exercised the full path")


def step_probe(ctx: Context) -> Check:
    if not ctx.args.probe:
        return Check("live model probe", SKIP, "not requested (--probe)")
    if ctx.args.offline:
        return Check("live model probe", WARN, "skipped (--offline)")

    result = _run([sys.executable, "live_llm_check.py", "--thinking", "disabled"])
    if result.returncode != 0:
        tail = (result.stderr or result.stdout).strip().splitlines()
        return Check(
            "live model probe",
            WARN,
            "model call failed",
            tail[-1] if tail else "check API key and network",
        )
    try:
        payload = json.loads(result.stdout[result.stdout.index("{"):])
        decision = payload.get("decision", {})
        speech = (decision.get("speech") or {}).get("text", "")
        return Check("live model probe", OK, f"model replied: {speech[:60]!r}")
    except (ValueError, KeyError):
        return Check("live model probe", OK, "model replied (unparsed output)")


def step_unit_tests(ctx: Context) -> Check:
    if not ctx.args.full_smoke:
        return Check("test suite", SKIP, "not requested (--full-smoke)")
    result = _run([sys.executable, "-m", "unittest", "discover", "-s", "tests"])
    tail = (result.stderr or result.stdout).strip().splitlines()
    summary = next((line for line in reversed(tail) if line.startswith("Ran ")), "")
    if result.returncode != 0:
        return Check("test suite", FAIL, "unit tests failed", summary or "see output above")
    return Check("test suite", OK, summary or "all tests passed")


DOCTOR_CHECKS: Sequence[Callable[[argparse.Namespace], Check]] = (
    lambda args: check_python(),
    lambda args: check_layout(),
    lambda args: check_imports(),
    lambda args: check_tool_catalog(),
    lambda args: check_data_packs(),
    lambda args: check_settings(),
    lambda args: check_timeline_writable(),
    lambda args: check_venv(),
    lambda args: check_env_files(args),
    lambda args: check_api_key(args),
    lambda args: check_bridge_sdk(args),
    lambda args: check_build_tools(args),
)

INSTALL_STEPS: Sequence[Callable[[Context], Check]] = (
    step_local_env,
    step_write_env,
    step_venv,
    step_optional_deps,
    step_sdk,
    step_bridge,
    step_smoke,
    step_unit_tests,
    step_probe,
)


def run_doctor(args: argparse.Namespace) -> List[Check]:
    checks: List[Check] = []
    for factory in DOCTOR_CHECKS:
        checks.append(factory(args))
    if args.probe:
        checks.append(check_network())
    return checks


# ---------------------------------------------------------------------------
# Next steps
# ---------------------------------------------------------------------------

def next_steps(args: argparse.Namespace, checks: Sequence[Check]) -> List[str]:
    by_name = {check.name: check for check in checks}
    backend = args.backend
    python = "python" if platform.system() == "Windows" else "python3"

    entries: List[tuple[str, str]] = []
    entries.append((f"{python} run_demo.py", "offline demo, no API key"))
    if backend == "rule":
        entries.append((f"{python} -m runtime.main --backend rule", "offline deterministic runtime"))
    else:
        key_state = by_name.get("api key")
        if key_state is not None and key_state.status != OK and not args.api_key:
            entries.append((f"{python} install.py --api-key <KEY>", "save your key to .env first"))
        command = f"{python} -m runtime.main --backend {backend}"
        if args.model:
            command += f" --model {args.model}"
        entries.append((command, "live model, no extra packages"))
    entries.append((f"{python} mock_bridge.py", "fake in-game bridge"))
    entries.append((f"{python} scenario_runner.py", "10 deterministic scenarios"))
    entries.append((f"{python} -m unittest discover -s tests -v", "full test suite"))

    if by_name.get("ScriptHook SDK", Check("", SKIP)).status != OK:
        entries.append((f"{python} install.py --with-asi", "official SDK + in-game bridge"))

    width = max(len(command) for command, _ in entries)
    return [f"{command.ljust(width)}   # {comment}" for command, comment in entries]


def print_summary(
    args: argparse.Namespace,
    doctor_checks: Sequence[Check],
    step_checks: Sequence[Check],
) -> None:
    failures = [c for c in doctor_checks if c.status == FAIL]
    warnings = [c for c in doctor_checks if c.status == WARN]

    print()
    print(_paint("=" * 68, "90"))
    if failures:
        headline = f"Setup is incomplete: {len(failures)} blocking issue(s)."
        print(_paint(headline, "31"))
    elif warnings:
        headline = f"Ready to run, with {len(warnings)} optional item(s) to look at."
        print(_paint(headline, "33"))
    else:
        print(_paint("Ready to run.", "32"))

    if step_checks:
        applied = [c for c in step_checks if c.status == OK]
        if applied:
            print(f"Applied: {', '.join(c.name for c in applied)}")

    print()
    print("Next steps:")
    for line in next_steps(args, doctor_checks):
        print(f"  {line}")
    print(_paint("=" * 68, "90"))
    print()


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="install.py",
        description="Install and verify the RDR2 Living NPC agent",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=(
            "examples:\n"
            "  python3 install.py                 guided install\n"
            "  python3 install.py --check         doctor only, changes nothing\n"
            "  python3 install.py --yes --probe   unattended install + live model call\n"
            "  python3 install.py --with-asi      official SDK + native bridge\n"
        ),
    )
    parser.add_argument("--check", action="store_true", help="diagnose only, never modify anything")
    parser.add_argument("-y", "--yes", action="store_true", help="non-interactive, accept defaults")
    parser.add_argument("--offline", action="store_true", help="skip every network operation")
    parser.add_argument("--json", action="store_true", help="emit a machine-readable report")

    parser.add_argument("--api-key", default=None, help="API key to save into .env")
    parser.add_argument("--api-key-env", default="OPENCODE_API_KEY", help="env var name for the key")
    parser.add_argument("--model", default=None, help="default model name to record")
    parser.add_argument("--api-base", default=None, help="OpenAI-compatible base URL to record")
    parser.add_argument(
        "--env-file",
        default=".env",
        help="file to write API key / endpoint settings into (default: .env)",
    )
    parser.add_argument(
        "--backend",
        choices=["rule", "llm", "adk"],
        default="llm",
        help="backend the printed next steps should target (default: llm)",
    )

    parser.add_argument("--venv", action="store_true", help="create .venv for optional packages")
    parser.add_argument("--with-adk", action="store_true", help="install google-adk + litellm")
    parser.add_argument("--with-audio", action="store_true", help="install faster-whisper + pyttsx3")
    parser.add_argument("--with-sdk", action="store_true", help="download the ScriptHookRDR2 SDK")
    parser.add_argument("--with-bridge", action="store_true", help="build the portable bridge core")
    parser.add_argument("--with-asi", action="store_true", help="build the in-game .asi (Windows)")
    parser.add_argument("--sdk-root", default=None, help="override the SDK install directory")
    parser.add_argument("--force", action="store_true", help="re-download / rebuild existing artifacts")

    parser.add_argument("--probe", action="store_true", help="make one real model call at the end")
    parser.add_argument("--full-smoke", action="store_true", help="also run the whole unit test suite")
    parser.add_argument("--no-smoke", action="store_true", help="skip run_demo.py")
    return parser


def main(argv: Optional[Sequence[str]] = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)

    ctx = Context(args=args, interactive=not args.yes and sys.stdin.isatty())
    env_path = Path(args.env_file)
    ctx.env_path = env_path if env_path.is_absolute() else PROJECT_ROOT / env_path

    if not args.json:
        print()
        print(_paint("RDR2 Living NPC Agent - installer", "1"))
        print(f"project: {PROJECT_ROOT}")
        print(f"python:  {sys.version.split()[0]} ({sys.executable})")

    doctor_checks = run_doctor(args)
    step_checks: List[Check] = []

    if args.check:
        if not args.json:
            print_report(doctor_checks, "Doctor")
            failures = sum(1 for check in doctor_checks if check.status == FAIL)
            if failures:
                print(f"\n{_paint(str(failures) + ' blocking issue(s)', '31')}")
            else:
                print(f"\n{_paint('No blocking issues found.', '32')}")
                print("\nNext steps:")
                for line in next_steps(args, doctor_checks):
                    print(f"  {line}")
            print()
        else:
            print(json.dumps(
                {
                    "mode": "check",
                    "project_root": str(PROJECT_ROOT),
                    "doctor": [check.to_dict() for check in doctor_checks],
                    "ok": not any(check.status == FAIL for check in doctor_checks),
                },
                indent=2,
            ))
        return 1 if any(check.status == FAIL for check in doctor_checks) else 0

    # Pre-flight: block only on genuinely fatal problems.
    fatal = [check for check in doctor_checks if check.status == FAIL]
    if fatal:
        if args.json:
            print(json.dumps(
                {
                    "mode": "install",
                    "ok": False,
                    "doctor": [check.to_dict() for check in doctor_checks],
                },
                indent=2,
            ))
        else:
            print_report(doctor_checks, "Doctor")
            print(_paint("\nCannot continue until the blocking issues above are fixed.", "31"))
        return 1

    for step in INSTALL_STEPS:
        result = step(ctx)
        step_checks.append(result)
        if not args.json:
            colour = {OK: "32", WARN: "33", FAIL: "31", SKIP: "90"}[result.status]
            print(
                f"  {_paint(_MARKS[result.status].ljust(4), colour)} "
                f"{result.name}: {result.detail}"
            )

    # Re-run the doctor so the printed report reflects the post-install state.
    final_checks = run_doctor(args)

    if args.json:
        print(json.dumps(
            {
                "mode": "install",
                "project_root": str(PROJECT_ROOT),
                "doctor_before": [check.to_dict() for check in doctor_checks],
                "doctor": [check.to_dict() for check in final_checks],
                "steps": [check.to_dict() for check in step_checks],
                "ok": not any(check.status == FAIL for check in final_checks + step_checks),
            },
            indent=2,
        ))
    else:
        print_report(final_checks, "Doctor")
        print_summary(args, final_checks, step_checks)

    failed = any(check.status == FAIL for check in final_checks + step_checks)
    return 1 if failed else 0


if __name__ == "__main__":
    raise SystemExit(main())
