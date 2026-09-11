"""Conservative NPC eligibility gate.

The gate fails closed: a missing or ambiguous safety field blocks activation.
This implements spec section 6 without assuming that one mission flag is
enough to prove an NPC is safe.
"""
from __future__ import annotations

from dataclasses import dataclass, field
from fnmatch import fnmatchcase
from pathlib import Path
from typing import Any, Dict, Iterable, List, Optional, Tuple
import json

from runtime.schemas import PedSnapshot


@dataclass
class EligibilityResult:
    allowed: bool
    reasons: List[str] = field(default_factory=list)

    def __bool__(self) -> bool:  # allow ``if gate.can_activate(ped):``
        return self.allowed


class StoryBlacklist:
    """Model/name blacklist loaded from ``config/story_blacklist.json``."""

    def __init__(self, categories: Optional[Dict[str, Any]] = None):
        self.categories = categories or {}
        self.models: List[str] = []
        self.names: List[str] = []
        for category in self.categories.values():
            if not isinstance(category, dict):
                continue
            self.models.extend(str(m).lower() for m in category.get("models", []))
            self.names.extend(str(n).lower() for n in category.get("names", []))

    @classmethod
    def from_file(cls, path: str | Path) -> "StoryBlacklist":
        with Path(path).open("r", encoding="utf-8") as fh:
            return cls(json.load(fh))

    def matches_model(self, model: str) -> bool:
        model_l = (model or "").lower()
        return any(fnmatchcase(model_l, pattern) for pattern in self.models)

    def matches_name(self, name: str) -> bool:
        name_l = (name or "").lower()
        return any(fnmatchcase(name_l, pattern) for pattern in self.names)

    def is_blacklisted(self, ped: PedSnapshot) -> bool:
        if ped.blacklisted is True:
            return True
        if ped.is_story_character is True:
            return True
        return self.matches_model(ped.model) or self.matches_name(ped.name)


class EligibilityGate:
    """Decides whether a ped may be activated as an LLM-backed agent."""

    def __init__(
        self,
        blacklist: Optional[StoryBlacklist] = None,
        *,
        block_if_uncertain: bool = True,
    ):
        self.blacklist = blacklist or StoryBlacklist()
        self.block_if_uncertain = block_if_uncertain

    @classmethod
    def from_config(
        cls,
        blacklist_path: str | Path = "config/story_blacklist.json",
        *,
        block_if_uncertain: bool = True,
    ) -> "EligibilityGate":
        return cls(StoryBlacklist.from_file(blacklist_path), block_if_uncertain=block_if_uncertain)

    # ------------------------------------------------------------------
    def can_activate(self, ped: PedSnapshot) -> EligibilityResult:
        reasons: List[str] = []

        def require_true(value: Optional[bool], label: str) -> None:
            if value is not True:
                reasons.append(f"{label} is not confirmed true")

        def require_false(value: Optional[bool], label: str) -> None:
            if value is not False:
                reasons.append(f"{label} is not confirmed false")

        if not ped.entity_id:
            reasons.append("entity_id is empty")
        require_true(ped.is_ped, "is_ped")
        require_true(ped.is_human, "is_human")
        require_true(ped.is_alive, "is_alive")
        require_false(ped.is_player, "is_player")
        require_false(ped.is_story_character, "is_story_character")
        require_false(ped.is_mission_owned, "is_mission_owned")
        require_false(ped.in_scripted_state, "in_scripted_state")
        require_false(ped.in_cutscene, "in_cutscene")
        require_false(ped.blacklisted, "blacklisted")

        if self.block_if_uncertain and any(
            value is None
            for value in (
                ped.is_ped,
                ped.is_human,
                ped.is_alive,
                ped.is_player,
                ped.is_story_character,
                ped.is_mission_owned,
                ped.in_scripted_state,
                ped.in_cutscene,
                ped.blacklisted,
            )
        ):
            if "one or more eligibility fields are unknown" not in reasons:
                reasons.append("one or more eligibility fields are unknown")

        # Optional extra conservative signals supplied by the bridge.
        if ped.metadata:
            for flag in (
                "mission_entity",
                "is_mission_entity",
                "scripted",
                "is_scripted",
                "cutscene",
                "is_cutscene",
                "critical",
                "is_critical",
            ):
                if ped.metadata.get(flag) is True:
                    reasons.append(f"metadata.{flag} is true")

        if self.blacklist.is_blacklisted(ped):
            reasons.append("matches story/blacklist configuration")

        # Deduplicate while preserving order.
        seen = set()
        unique_reasons = []
        for reason in reasons:
            if reason not in seen:
                seen.add(reason)
                unique_reasons.append(reason)

        return EligibilityResult(allowed=not unique_reasons, reasons=unique_reasons)
