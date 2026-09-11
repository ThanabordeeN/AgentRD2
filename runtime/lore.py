"""Offline wiki/lore context loaded from local JSON packs.

The runtime does not scrape the web during gameplay.  Use
``scripts/fetch_wiki_context.py`` and ``scripts/fetch_character_context.py``
to refresh the local packs.
"""
from __future__ import annotations

from pathlib import Path
from typing import Any, Dict, List, Optional
import json


class WikiContextStore:
    def __init__(
        self,
        path: str | Path = "data/wiki/rdr2_context.json",
        character_path: str | Path | None = "data/wiki/characters.json",
    ):
        self.path = Path(path)
        self.character_path = Path(character_path) if character_path else None
        self.meta: Dict[str, Any] = {}
        self.pages: Dict[str, Dict[str, Any]] = {}
        self.characters: Dict[str, Dict[str, Any]] = {}
        self._lookup: Dict[str, str] = {}
        self._character_lookup: Dict[str, str] = {}
        self._load()

    def _load(self) -> None:
        if self.path.exists():
            try:
                data = json.loads(self.path.read_text(encoding="utf-8"))
                self.meta = dict(data.get("meta") or {})
                for title, page in (data.get("pages") or {}).items():
                    if isinstance(page, dict):
                        entry = dict(page)
                        entry.setdefault("title", title)
                        entry.setdefault("source", self.meta.get("source", "Red Dead Wiki"))
                        self.pages[title] = entry
            except (OSError, json.JSONDecodeError):
                pass
        if self.character_path and self.character_path.exists():
            try:
                data = json.loads(self.character_path.read_text(encoding="utf-8"))
                char_meta = dict(data.get("meta") or {})
                if char_meta:
                    self.meta = {**char_meta, **self.meta}
                for title, character in (data.get("characters") or {}).items():
                    if isinstance(character, dict):
                        entry = dict(character)
                        entry.setdefault("title", title)
                        entry.setdefault("source", self.meta.get("source", "Red Dead Wiki"))
                        entry.setdefault("kind", "character")
                        self.characters[title] = entry
            except (OSError, json.JSONDecodeError):
                pass
        self._lookup = {title.lower(): title for title in self.pages}
        self._character_lookup = {title.lower(): title for title in self.characters}

    def available_topics(self) -> List[str]:
        return sorted(self.pages)

    def available_characters(self) -> List[str]:
        return sorted(self.characters)

    def get(self, topic: str) -> Optional[Dict[str, Any]]:
        if not topic:
            return None
        needle = topic.strip().lower()
        key = self._lookup.get(needle) or self._character_lookup.get(needle)
        if key is None:
            for lookup, values in ((self._lookup, self.pages), (self._character_lookup, self.characters)):
                for title in values:
                    if needle in title.lower():
                        key = title
                        break
                if key is not None:
                    break
        if key is None:
            return None
        source = self.characters if key in self.characters else self.pages
        return dict(source[key])

    def character(self, name: str) -> Optional[Dict[str, Any]]:
        if not name:
            return None
        key = self._character_lookup.get(name.strip().lower())
        if key is None:
            for title in self.characters:
                if name.strip().lower() in title.lower():
                    key = title
                    break
        return dict(self.characters[key]) if key else None

    def lookup(self, query: str, *, limit: int = 3, max_chars: int = 1200) -> List[Dict[str, Any]]:
        query_l = (query or "").strip().lower()
        if not query_l:
            return []
        scored = []
        for title, entry in list(self.characters.items()) + list(self.pages.items()):
            haystack = (title + " " + str(entry.get("summary", ""))).lower()
            if query_l in title.lower():
                scored.append((0, title, entry))
            elif query_l in haystack:
                scored.append((1, title, entry))
        scored.sort(key=lambda item: (item[0], item[1]))
        return [self._trim(entry, max_chars) for _, _, entry in scored[:limit]]

    def context_for_profile(
        self,
        profile: Dict[str, Any],
        *,
        world_state: Optional[Dict[str, Any]] = None,
        max_topics: int = 5,
        max_chars_per_topic: int = 1200,
    ) -> List[Dict[str, Any]]:
        profile = profile or {}
        topics: List[str] = []

        # Canonical character context first (when the profile maps to a named
        # Red Dead Wiki character).
        for key in ("wiki_character", "character", "name"):
            value = str(profile.get(key) or "").strip()
            if value:
                topics.append(value)

        raw_topics = profile.get("wiki_topics") or []
        if isinstance(raw_topics, str):
            raw_topics = [raw_topics]
        topics.extend(str(topic) for topic in raw_topics)

        region = str(profile.get("region") or "").strip()
        if region:
            topics.append(region)
        if world_state:
            game_region = (
                (world_state.get("location") or {}).get("region")
                if isinstance(world_state.get("location"), dict)
                else None
            )
            if game_region:
                topics.append(str(game_region))
        if not topics:
            topics = ["Red Dead Redemption 2"]

        result: List[Dict[str, Any]] = []
        seen = set()
        for topic in topics:
            if len(result) >= max_topics:
                break
            page = self.get(topic)
            if page is None or page["title"] in seen:
                continue
            seen.add(page["title"])
            result.append(self._trim(page, max_chars_per_topic))
        if not result and (self.pages or self.characters):
            first = next(iter(self.pages.values()), None) or next(iter(self.characters.values()))
            result.append(self._trim(first, max_chars_per_topic))
        return result

    @staticmethod
    def _trim(page: Dict[str, Any], max_chars: int) -> Dict[str, Any]:
        entry = dict(page)
        summary = str(entry.get("summary") or "")
        entry["summary"] = summary[:max_chars].strip()
        return entry
