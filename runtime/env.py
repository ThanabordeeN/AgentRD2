"""Minimal ``.env`` loader so setup does not require shell exports.

The runtime is deliberately dependency-free, so this module implements the
small subset of ``python-dotenv`` behaviour that the project needs:

* ``KEY=value`` lines
* optional ``export`` prefix
* ``#`` comments and blank lines
* single/double quoted values
* ``${OTHER_KEY}`` interpolation against already-resolved values
* Windows ``set KEY=value`` style prefix (tolerated, stripped)

Real environment variables always win unless ``override=True`` is passed, so a
CI runner or shell export is never clobbered by a stale local file.
"""
from __future__ import annotations

import os
import re
from pathlib import Path
from typing import Dict, Iterable, Optional

PROJECT_ROOT = Path(__file__).resolve().parents[1]

#: Files searched by :func:`load_env`, in priority order (first match wins).
DEFAULT_ENV_FILES = (
    ".env",
    ".env.local",
    "config/local.env",
)

_KEY_RE = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*$")
_INTERPOLATE_RE = re.compile(r"\$\{([A-Za-z_][A-Za-z0-9_]*)\}")


class EnvFileError(ValueError):
    """Raised when a ``.env`` file contains an unusable line."""


def _strip_wrapping_quotes(value: str) -> str:
    if len(value) >= 2 and value[0] == value[-1] and value[0] in {"'", '"'}:
        return value[1:-1]
    return value


def parse_env_text(text: str) -> Dict[str, str]:
    """Parse ``.env`` content into a plain dict.

    ``${VAR}`` references are resolved from earlier keys in the same file and
    then from ``os.environ``.  Unresolvable references resolve to an empty
    string, matching ``python-dotenv``.
    """
    parsed: Dict[str, str] = {}
    for raw_line in text.splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        if line.lower().startswith("export "):
            line = line[len("export "):].strip()
        elif line.lower().startswith("set "):
            line = line[len("set "):].strip()
        if "=" not in line:
            continue
        key, _, value = line.partition("=")
        key = key.strip()
        if not _KEY_RE.match(key):
            continue
        value = value.strip()

        # Strip a trailing comment only for unquoted values.
        if value[:1] not in {"'", '"'}:
            hash_index = value.find(" #")
            if hash_index == -1:
                hash_index = value.find("\t#")
            if hash_index != -1:
                value = value[:hash_index].strip()

        value = _strip_wrapping_quotes(value)

        def _replace(match: "re.Match[str]") -> str:
            name = match.group(1)
            if name in parsed:
                return parsed[name]
            return os.environ.get(name, "")

        value = _INTERPOLATE_RE.sub(_replace, value)
        parsed[key] = value
    return parsed


def load_env_file(path: str | Path, override: bool = False) -> Dict[str, str]:
    """Load one ``.env`` file into ``os.environ``.

    Returns the key/value pairs that were actually applied.
    """
    env_path = Path(path)
    if not env_path.is_file():
        return {}
    values = parse_env_text(env_path.read_text(encoding="utf-8", errors="replace"))
    applied: Dict[str, str] = {}
    for key, value in values.items():
        if override or key not in os.environ:
            os.environ[key] = value
            applied[key] = value
    return applied


def load_env(
    paths: Optional[Iterable[str | Path]] = None,
    override: bool = False,
    root: str | Path = PROJECT_ROOT,
) -> Dict[str, str]:
    """Load the project's env files, skipping any that do not exist.

    ``paths`` entries may be absolute or relative to ``root``.
    """
    root_path = Path(root)
    candidates = list(paths) if paths is not None else list(DEFAULT_ENV_FILES)
    applied: Dict[str, str] = {}
    for candidate in candidates:
        candidate_path = Path(candidate)
        if not candidate_path.is_absolute():
            candidate_path = root_path / candidate_path
        applied.update(load_env_file(candidate_path, override=override))
    return applied


def env_file_path(name: str = ".env", root: str | Path = PROJECT_ROOT) -> Path:
    """Return the absolute path of an env file inside the project root."""
    return Path(root) / name


def upsert_env_file(path: str | Path, values: Dict[str, str]) -> Path:
    """Create or update ``KEY=value`` entries while preserving other lines.

    Existing keys are rewritten in place, missing keys are appended.  Comments,
    blank lines, and unrelated keys are left untouched so a hand-edited file
    survives repeated installer runs.
    """
    env_path = Path(path)
    env_path.parent.mkdir(parents=True, exist_ok=True)

    remaining = dict(values)
    output: list[str] = []
    if env_path.is_file():
        for raw_line in env_path.read_text(encoding="utf-8").splitlines():
            stripped = raw_line.strip()
            body = stripped[len("export "):].strip() if stripped.lower().startswith("export ") else stripped
            key = body.partition("=")[0].strip() if "=" in body else ""
            if key and key in remaining:
                output.append(f"{key}={remaining.pop(key)}")
            else:
                output.append(raw_line)

    if remaining:
        if output and output[-1].strip():
            output.append("")
        output.extend(f"{key}={value}" for key, value in remaining.items())

    env_path.write_text("\n".join(output) + "\n", encoding="utf-8")
    return env_path
