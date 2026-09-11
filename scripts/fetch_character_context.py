#!/usr/bin/env python3
"""Fetch Red Dead Wiki character pages into an offline character context pack.

This is bulk-optimized: category members are enumerated, then wikitext is
fetched in batches of 50 pages and reduced to infobox fields + lead summary.
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
from typing import Any, Dict, Iterable, List

API_URL = "https://reddead.fandom.com/api.php"
USER_AGENT = "rdr2-living-npc/0.2 (offline character context importer)"
DEFAULT_CATEGORIES = [
    "Category:Characters in Redemption 2",
    "Category:Characters in Online",
]
PAGE_BATCH = 50


def api_query(params: Dict[str, Any], *, api_url: str = API_URL) -> Dict[str, Any]:
    request_params = dict(params)
    request_params.setdefault("format", "json")
    request_params.setdefault("formatversion", "2")
    url = api_url + "?" + urllib.parse.urlencode(request_params)
    request = urllib.request.Request(url, headers={"User-Agent": USER_AGENT})
    with urllib.request.urlopen(request, timeout=60) as response:
        return json.loads(response.read().decode("utf-8"))


def category_members(category: str) -> List[str]:
    titles: List[str] = []
    cont: str | None = None
    while True:
        params = {
            "action": "query",
            "list": "categorymembers",
            "cmtitle": category,
            "cmlimit": "500",
            "cmtype": "page",
        }
        if cont:
            params["cmcontinue"] = cont
        data = api_query(params)
        titles.extend(member["title"] for member in data.get("query", {}).get("categorymembers", []))
        cont = data.get("continue", {}).get("cmcontinue")
        if not cont:
            break
    return titles


def fetch_wikitext_batch(titles: List[str]) -> Dict[str, Dict[str, Any]]:
    params = {
        "action": "query",
        "prop": "revisions",
        "rvprop": "content|ids",
        "rvslots": "main",
        "redirects": "1",
        "titles": "|".join(titles),
    }
    data = api_query(params)
    pages: Dict[str, Dict[str, Any]] = {}
    redirects = {
        item["from"]: item["to"] for item in data.get("query", {}).get("redirects", [])
    }
    for page in data.get("query", {}).get("pages", []):
        title = page.get("title")
        if not title:
            continue
        revisions = page.get("revisions") or []
        if not revisions:
            continue
        content = ((revisions[0].get("slots") or {}).get("main") or {}).get("content", "")
        pages[title] = {
            "title": title,
            "pageid": page.get("pageid"),
            "revision": revisions[0].get("revid"),
            "wikitext": content,
            "redirected_from": [src for src, target in redirects.items() if target == title],
        }
    return pages


def clean_wiki_markup(text: str) -> str:
    if not text:
        return ""
    text = re.sub(r"<ref[^>]*>.*?</ref>", " ", text, flags=re.S | re.I)
    text = re.sub(r"<ref[^>]*/>", " ", text, flags=re.I)
    text = re.sub(r"<gallery.*?</gallery>", " ", text, flags=re.S | re.I)
    text = re.sub(r"<[^>]+>", " ", text)
    text = re.sub(r"\[\[File:[^\]]*\]\]", " ", text, flags=re.I)
    text = re.sub(r"\[\[(?:[^\]|]*\|)?([^\]]+)\]\]", lambda m: m.group(1), text)
    text = re.sub(r"\[([^\s\]]+)\s+([^\]]+)\]", lambda m: m.group(2), text)
    text = re.sub(r"\{\|.*?\|\}", " ", text, flags=re.S)
    text = re.sub(r"\{\{[^{}]*\}\}", " ", text)
    text = re.sub(r"'{2,}", "", text)
    text = html_module.unescape(text)
    text = re.sub(r"\s+", " ", text)
    return text.strip(" |\n\t")


def _extract_balanced_template(text: str, start: int) -> str | None:
    depth = 0
    index = start
    while index < len(text):
        if text.startswith("{{", index):
            depth += 1
            index += 2
        elif text.startswith("}}", index):
            depth -= 1
            index += 2
            if depth == 0:
                return text[start:index]
        else:
            index += 1
    return None


def _template_name(template: str) -> str:
    body = template[2:-2]
    name = body.split("|", 1)[0].split("\n", 1)[0]
    return name.strip().lower().replace("_", " ")


def extract_infobox(wikitext: str) -> Dict[str, str]:
    start = wikitext.find("{{")
    while start != -1:
        template = _extract_balanced_template(wikitext, start)
        if template is None:
            return {}
        name = _template_name(template)
        if "characterbox" in name or "character box" in name or "infobox" in name:
            body = template[2:-2]
            fields: Dict[str, str] = {}
            parts: List[str] = []
            depth = 0
            current: List[str] = []
            for char in body:
                if char in "[{":
                    depth += 1
                elif char in "]}":
                    depth = max(0, depth - 1)
                if char == "|" and depth == 0:
                    parts.append("".join(current))
                    current = []
                else:
                    current.append(char)
            parts.append("".join(current))
            for part in parts:
                if "=" not in part:
                    continue
                key, value = part.split("=", 1)
                key = key.strip().lower().replace(" ", "_")
                value = clean_wiki_markup(value)
                if key and value:
                    fields[key] = value
            return fields
        start = wikitext.find("{{", start + 1)
    return {}


def extract_summary(wikitext: str, max_chars: int = 2500) -> str:
    # Remove the first major template (infobox/user characterbox) plus any
    # remaining nested templates before the first section heading.
    text = re.sub(r"^\s*\{\{.*?\n\}\}", " ", wikitext, flags=re.S)
    text = re.split(r"\n==+[^=]+==+", text, maxsplit=1)[0]
    for _ in range(8):
        cleaned = re.sub(r"\{\{[^{}]*\}\}", " ", text, flags=re.S)
        if cleaned == text:
            break
        text = cleaned
    text = clean_wiki_markup(text)
    # Remove the page-title line and common maintenance banners.
    text = re.sub(r"^(?:For .*?see .*?\.|This article .*?\.)\s*", "", text, flags=re.I)
    text = text.strip()
    if text.startswith("[["):
        text = re.sub(r"^\[\[[^\]]+\]\]\s*", "", text)
    return text[:max_chars].strip()


def main(argv: List[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--categories", nargs="*", default=DEFAULT_CATEGORIES)
    parser.add_argument("--out", default="data/wiki/characters.json")
    parser.add_argument("--max-summary-chars", type=int, default=2500)
    parser.add_argument("--delay", type=float, default=0.15)
    parser.add_argument("--limit", type=int, default=0, help="debug: only fetch first N titles")
    args = parser.parse_args(argv)

    titles: List[str] = []
    for category in args.categories:
        found = category_members(category)
        print(f"[category] {category}: {len(found)} pages")
        titles.extend(found)
    titles = list(dict.fromkeys(titles))
    excluded_prefixes = (
        "Characters in ",
        "Special Characters in ",
        "Template:",
        "Category:",
        "List of ",
        "Multiplayer characters",
    )
    before_filter = len(titles)
    titles = [title for title in titles if not title.startswith(excluded_prefixes)]
    print(f"[filter] removed {before_filter - len(titles)} list/template pages")
    if args.limit:
        titles = titles[: args.limit]
    print(f"[plan] {len(titles)} unique character pages")

    characters: Dict[str, Dict[str, Any]] = {}
    for start in range(0, len(titles), PAGE_BATCH):
        batch = titles[start : start + PAGE_BATCH]
        pages = fetch_wikitext_batch(batch)
        for title, page in pages.items():
            infobox = extract_infobox(page["wikitext"])
            characters[title] = {
                "title": title,
                "pageid": page.get("pageid"),
                "revision": page.get("revision"),
                "url": f"https://reddead.fandom.com/wiki/{urllib.parse.quote(title.replace(' ', '_'))}",
                "categories": [
                    cat for cat in args.categories if title in category_members(cat)
                ] if len(batch) <= 2 else [],
                "aliases": _aliases_from_infobox(infobox),
                "infobox": infobox,
                "summary": extract_summary(page["wikitext"], args.max_summary_chars),
            }
        missing = [title for title in batch if title not in pages]
        print(f"[batch] {start + 1}-{start + len(batch)}/{len(titles)} fetched={len(pages)}"
              + (f" missing={missing}" if missing else ""))
        if start + PAGE_BATCH < len(titles) and args.delay > 0:
            time.sleep(args.delay)

    pack = {
        "meta": {
            "source": "Red Dead Wiki (Fandom)",
            "license": "CC BY-SA (Fandom community content)",
            "attribution": "Red Dead Wiki contributors",
            "api_url": API_URL,
            "fetched_at": datetime.now(timezone.utc).isoformat(),
            "categories": args.categories,
            "note": "Offline character context pack. Preserve attribution when redistributing.",
        },
        "characters": characters,
    }
    out = Path(args.out)
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(pack, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"[done] wrote {out} with {len(characters)} characters")
    return 0


def _aliases_from_infobox(infobox: Dict[str, str]) -> List[str]:
    values: List[str] = []
    for key in ("alias", "aliases", "nickname", "nicknames", "full_name"):
        value = infobox.get(key)
        if not value:
            continue
        for part in re.split(r"[,;]", value):
            part = part.strip()
            if part and part not in values:
                values.append(part)
    return values


if __name__ == "__main__":
    raise SystemExit(main())
