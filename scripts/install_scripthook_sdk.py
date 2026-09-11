#!/usr/bin/env python3
"""Download the official ScriptHookRDR2 SDK for local bridge builds.

The SDK archive is not redistributed with this repository.  This script
fetches it directly from Alexander Blade's official site and extracts it
into a local, git-ignored directory.

Official page:
    http://www.dev-c.com/rdr2/scripthookrdr2/
"""
from __future__ import annotations

import argparse
import io
import shutil
import urllib.request
import zipfile
from pathlib import Path

DEFAULT_URL = "http://www.dev-c.com/files/ScriptHookRDR2_SDK_1.0.1207.73.zip"
DEFAULT_VERSION = "ScriptHookRDR2_SDK_1.0.1207.73"
USER_AGENT = "Mozilla/5.0 (Windows NT 10.0; Win64; x64)"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--url", default=DEFAULT_URL)
    parser.add_argument("--dest", default=f"bridge/.sdk/{DEFAULT_VERSION}")
    parser.add_argument("--force", action="store_true")
    args = parser.parse_args()

    dest = Path(args.dest)
    if dest.exists() and not args.force:
        print(f"[skip] already exists: {dest}")
        print_readme(dest)
        return 0

    print(f"[download] {args.url}")
    request = urllib.request.Request(
        args.url,
        headers={
            "User-Agent": USER_AGENT,
            "Referer": "http://www.dev-c.com/rdr2/scripthookrdr2/",
        },
    )
    with urllib.request.urlopen(request, timeout=120) as response:
        payload = response.read()
    print(f"[download] {len(payload)} bytes")

    if dest.exists():
        shutil.rmtree(dest)
    dest.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(io.BytesIO(payload)) as archive:
        archive.extractall(dest)
    print(f"[extract] {dest}")
    print_readme(dest)
    return 0


def print_readme(dest: Path) -> None:
    readme = dest / "readme.txt"
    if readme.exists():
        print("\n--- SDK readme / terms ---")
        print(readme.read_text(encoding="utf-8", errors="replace")[:2500])


if __name__ == "__main__":
    raise SystemExit(main())
