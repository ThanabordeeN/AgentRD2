"""Configuration and profile loading."""
from __future__ import annotations

from pathlib import Path
from typing import Any, Dict, Optional
import copy
import json


PROJECT_ROOT = Path(__file__).resolve().parents[1]

DEFAULT_SETTINGS: Dict[str, Any] = {
    "ipc": {"host": "127.0.0.1", "port": 8765},
    "activation": {
        "candidate_distance_m": 20.0,
        "aware_distance_m": 10.0,
        "conversation_range_m": 8.0,
        "min_conversation_range_m": 5.0,
    },
    "scheduler": {
        "max_conversation_agents": 1,
        "max_reasoning_agents": 3,
        "nearby_tracked_limit": 32,
        "idle_thinking_seconds": 5.0,
    },
    "push_to_talk": {"key": "V", "capture_while_held": True},
    "speech": {
        "cooldown_seconds": 6.0,
        "recent_speech_penalty_seconds": 12.0,
        "social_relevance_threshold": 0.45,
        "silence_probability": 0.25,
        "global_cooldown_seconds": 2.0,
    },
    "story_safety": {"block_if_uncertain": True},
    "thinking_policy": {
        "default_mode": "low",
        "wait_gestures": True,
        "disabled_for_events": [
            "PLAYER_APPROACHED",
            "PLAYER_LOOKED_AT_NPC",
            "PLAYER_LEFT_AREA",
            "NPC_ACTIVATED",
        ],
        "disabled_for_reasons": [
            "ped_scan_activation",
            "idle",
            "pass_by",
            "deferred_promotion",
        ],
    },
    "adk": {
        "provider": "openai",
        "model": "deepseek-v4.1-flash",
        "api_base": "https://opencode.ai/zen/go/v1",
        "api_key_env": "OPENCODE_API_KEY",
        "reasoning_effort": "low",
        "extra_body": {"thinking": {"type": "enabled"}},
    },
    "story_blacklist_path": "config/story_blacklist.json",
    "profiles_dir": "data/profiles",
    "timelines_dir": "data/timelines",
    "wiki_context_path": "data/wiki/rdr2_context.json",
    "character_context_path": "data/wiki/characters.json",
    "quests_dir": "data/quests",
}


def _deep_merge(base: Dict[str, Any], overlay: Dict[str, Any]) -> Dict[str, Any]:
    result = copy.deepcopy(base)
    for key, value in overlay.items():
        if isinstance(value, dict) and isinstance(result.get(key), dict):
            result[key] = _deep_merge(result[key], value)
        else:
            result[key] = copy.deepcopy(value)
    return result


def load_settings(path: str | Path = "config/settings.json") -> Dict[str, Any]:
    p = Path(path)
    if not p.exists():
        return copy.deepcopy(DEFAULT_SETTINGS)
    with p.open("r", encoding="utf-8") as fh:
        data = json.load(fh)
    merged = _deep_merge(DEFAULT_SETTINGS, data)
    # Resolve default project-relative paths so the runtime can be launched
    # from any working directory.
    for key in (
        "profiles_dir", "timelines_dir", "story_blacklist_path",
        "wiki_context_path", "character_context_path", "quests_dir",
    ):
        value = merged.get(key)
        if value and not Path(value).is_absolute():
            merged[key] = str(PROJECT_ROOT / value)
    return merged


class ProfileStore:
    """Loads NPC profiles from ``data/profiles``.

    Profiles are configuration, not memory.  Missing profiles fall back to a
    neutral ambient profile so the runtime still works in tests/demos.
    """

    def __init__(self, root: str | Path = "data/profiles"):
        self.root = Path(root)

    def get(self, npc_id: str) -> Dict[str, Any]:
        path = self.root / f"{npc_id}.json"
        if path.exists():
            with path.open("r", encoding="utf-8") as fh:
                profile = json.load(fh)
            profile.setdefault("npc_id", npc_id)
            profile.setdefault("name", npc_id)
            profile.setdefault("personality", {})
            for key, default in _PROFILE_DEFAULTS.items():
                if key != "personality":
                    profile.setdefault(key, copy.deepcopy(default))
            return profile
        profile = copy.deepcopy(_NEUTRAL_PROFILE)
        profile["npc_id"] = npc_id
        profile["name"] = npc_id
        return profile


_PROFILE_DEFAULTS: Dict[str, Any] = {
    "personality": {
        "sociability": 0.5,
        "curiosity": 0.5,
        "aggression": 0.3,
        "courage": 0.5,
        "patience": 0.5,
    },
    "system_prompt": "",
    "background": "",
    "speech_style": "",
    "behavior_rules": [],
    "goals": [],
    "knowledge": [],
    "relationships": {},
    "fears": [],
    "quirks": [],
}

_NEUTRAL_PROFILE: Dict[str, Any] = {
    "npc_id": "",
    "name": "",
    **_PROFILE_DEFAULTS,
}
