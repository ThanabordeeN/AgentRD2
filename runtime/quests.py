"""Quest context pack for dialogue-only quest NPCs."""
from __future__ import annotations

from pathlib import Path
from typing import Any, Dict, Optional
import copy
import json


class QuestStore:
    def __init__(self, root: str | Path = "data/quests"):
        self.root = Path(root)
        self.quests: Dict[str, Dict[str, Any]] = {}
        self._load()

    def _load(self) -> None:
        if not self.root.exists():
            return
        for path in sorted(self.root.glob("*.json")):
            try:
                data = json.loads(path.read_text(encoding="utf-8"))
            except (OSError, json.JSONDecodeError):
                continue
            quest_id = str(data.get("quest_id") or path.stem)
            data["quest_id"] = quest_id
            data.setdefault("source", str(path))
            self.quests[quest_id] = data

    def get(self, quest_id: str) -> Optional[Dict[str, Any]]:
        if not quest_id:
            return None
        quest = self.quests.get(quest_id)
        if quest is None:
            # Accept a file stem lookup as a convenience.
            for key, value in self.quests.items():
                if key.lower() == quest_id.lower() or Path(value.get("source", "")).stem == quest_id:
                    quest = value
                    break
        return copy.deepcopy(quest) if quest else None

    def context_for(
        self,
        quest_id: str,
        runtime_state: Optional[Dict[str, Any]] = None,
        *,
        max_chars: int = 2500,
    ) -> Dict[str, Any]:
        quest = self.get(quest_id) or {
            "quest_id": quest_id,
            "title": quest_id or "unknown quest",
            "description": "",
            "npc_role": "",
            "dialogue_guidelines": [
                "Speak briefly and in character.",
                "Do not invent quest steps that were not provided.",
            ],
            "forbidden_spoilers": [],
        }
        context = {
            "quest_id": quest.get("quest_id"),
            "title": quest.get("title"),
            "description": str(quest.get("description") or "")[:max_chars],
            "npc_role": quest.get("npc_role"),
            "current_objective": quest.get("current_objective"),
            "next_objective": quest.get("next_objective"),
            "location": quest.get("location"),
            "dialogue_guidelines": list(quest.get("dialogue_guidelines") or []),
            "known_facts": list(quest.get("known_facts") or []),
            "forbidden_spoilers": list(quest.get("forbidden_spoilers") or []),
            "sample_lines": list(quest.get("sample_lines") or [])[:8],
        }
        if runtime_state:
            for key in ("current_objective", "next_objective", "location", "quest_state"):
                if key in runtime_state and runtime_state[key] not in (None, "", []):
                    context[key] = runtime_state[key]
        return context
