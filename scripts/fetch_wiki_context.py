#!/usr/bin/env python3
"""Fetch Red Dead Wiki (Fandom) page text into a local context pack.

The runtime never scrapes at gameplay time.  This script builds a small,
offline, attributed JSON context pack that `WikiContextStore` can load.

Usage:
    python3 scripts/fetch_wiki_context.py
    python3 scripts/fetch_wiki_context.py --pages Valentine "New Hanover"
"""
from __future__ import annotations

import argparse
import html as html_module
import json
import re
import time
import urllib.parse
import urllib.request
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Dict, List

DEFAULT_PAGES = [
    "Red Dead Redemption 2",
    "Valentine",
    "New Hanover",
    "The Heartlands",
    "Ambarino",
    "Hunting",
    "Van der Linde gang",
    "Pinkerton National Detective Agency",
]
API_URL = "https://reddead.fandom.com/api.php"
USER_AGENT = "rdr2-living-npc/0.2 (offline context import)"


def fetch_page(title: str, *, api_url: str = API_URL) -> Dict[str, Any]:
    params = {
        "action": "parse",
        "page": title,
        "prop": "text|revid",
        "redirects": "1",
        "format": "json",
        "formatversion": "2",
    }
    url = api_url + "?" + urllib.parse.urlencode(params)
    request = urllib.request.Request(url, headers={"User-Agent": USER_AGENT})
    with urllib.request.urlopen(request, timeout=45) as response:
        payload = json.loads(response.read().decode("utf-8"))
    parse = payload.get("parse") or {}
    if not parse.get("title"):
        raise RuntimeError(f"page not found: {title}")
    return {
        "title": parse["title"],
        "pageid": parse.get("pageid"),
        "revision": parse.get("revid"),
        "url": f"https://reddead.fandom.com/wiki/{urllib.parse.quote(parse['title'].replace(' ', '_'))}",
        "html": parse.get("text", ""),
    }


def html_to_text(html_text: str) -> str:
    """Convert Fandom HTML to a whitespace-normalized plain text extract."""
    text = re.sub(r"<script.*?</script>|<style.*?</style>", " ", html_text, flags=re.S | re.I)
    text = re.sub(r"<!--.*?-->", " ", text, flags=re.S)
    text = re.sub(r"<[^>]+>", "\n", text)
    text = html_module.unescape(text)
    text = re.sub(r"\[\s*\d*\s*\]", "", text)           # citation markers [1]
    text = re.sub(r"\[\s*(?:edit|hide|show)\s*\]", "", text, flags=re.I)
    text = re.sub(r"[ \t]+", " ", text)
    text = re.sub(r"\n\s*\n+", "\n\n", text)
    # Trim community/navigation noise that often follows the article body.
    for marker in ("\n\nNavigation\n", "\n\nRetrieved from", "\n\nCategories", "\n\nGallery\n"):
        if marker in text:
            text = text.split(marker, 1)[0]
    return text.strip()


def build_context(pages: List[str], *, max_chars: int, delay: float) -> Dict[str, Any]:
    pack: Dict[str, Any] = {
        "meta": {
            "source": "Red Dead Wiki (Fandom)",
            "api_url": API_URL,
            "license": "CC BY-SA (Fandom community content)",
            "attribution": "Red Dead Wiki contributors",
            "fetched_at": datetime.now(timezone.utc).isoformat(),
            "note": "Offline extract for NPC context. Preserve attribution when redistributing.",
        },
        "pages": {},
    }
    for index, title in enumerate(pages):
        try:
            page = fetch_page(title)
            plain = html_to_text(page.pop("html"))
            plain = plain[:max_chars].strip()
            if not plain:
                raise RuntimeError("empty extract after HTML cleanup")
            key = page["title"]
            pack["pages"][key] = {
                "title": page["title"],
                "pageid": page.get("pageid"),
                "revision": page.get("revision"),
                "url": page["url"],
                "summary": plain,
            }
            print(f"[ok] {page['title']} ({len(plain)} chars)")
        except Exception as exc:  # noqa: BLE001 - importer reports and continues
            print(f"[fail] {title}: {type(exc).__name__}: {exc}")
        if index < len(pages) - 1 and delay > 0:
            time.sleep(delay)
    return pack


def main(argv: List[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--pages", nargs="*", default=DEFAULT_PAGES)
    parser.add_argument("--out", default="data/wiki/rdr2_context.json")
    parser.add_argument("--max-chars", type=int, default=4000)
    parser.add_argument("--delay", type=float, default=0.3)
    args = parser.parse_args(argv)

    pack = build_context(args.pages, max_chars=args.max_chars, delay=args.delay)
    out = Path(args.out)
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(pack, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"wrote {out} with {len(pack['pages'])} pages")
    return 0 if pack["pages"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
