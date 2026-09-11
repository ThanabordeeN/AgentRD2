#!/usr/bin/env python3
"""Offline dry-run of the complete event -> ownership -> agent -> action path."""
from __future__ import annotations

import json
import tempfile
from pathlib import Path

from runtime.agent.backends import RuleBasedAgentBackend
from runtime.agent.npc_agent import NpcAgentRuntime
from runtime.config import load_settings
from runtime.timeline.store import TimelineStore


def main() -> int:
    with tempfile.TemporaryDirectory() as tmp:
        settings = load_settings()
        settings["timelines_dir"] = str(Path(tmp) / "timelines")
        timeline = TimelineStore(settings["timelines_dir"])
        runtime = NpcAgentRuntime(
            settings=settings,
            timeline=timeline,
            backend=RuleBasedAgentBackend(),
        )

        npc_id = "npc_001"
        ped = {
            "entity_id": npc_id,
            "model": "a_m_m_farmer_01",
            "name": "Elias Carter",
            "is_ped": True,
            "is_human": True,
            "is_alive": True,
            "is_player": False,
            "is_story_character": False,
            "is_mission_owned": False,
            "in_scripted_state": False,
            "in_cutscene": False,
            "blacklisted": False,
            "distance_m": 6.2,
            "visible": True,
            "health": 100,
            "metadata": {"camera_alignment": 0.9},
        }

        print("1. ped scan ->", runtime.handle_ped_scan({"type": "ped_scan", "peds": [ped]}))
        print("2. ped scan ->", runtime.handle_ped_scan({"type": "ped_scan", "peds": [ped]}))
        print("3. approached ->", runtime.handle_game_event({"type": "game_event", "event_name": "PLAYER_APPROACHED", "npc_id": npc_id, "entities": ["player"], "tags": ["player"], "data": {"distance": 6.2}}))
        print("4. looked at ->", runtime.handle_game_event({"type": "game_event", "event_name": "PLAYER_LOOKED_AT_NPC", "npc_id": npc_id, "entities": ["player"], "tags": ["player"], "data": {}}))
        print("5. player speaks ->", runtime.handle_player_speech({"npc_id": npc_id, "text": "Where are you headed?"}))

        print("\n--- timeline ---")
        for event in timeline.all_events(npc_id):
            print(json.dumps(event.to_dict(), ensure_ascii=False))
        print("\n--- runtime state ---")
        state = runtime._agent_state(npc_id)
        print(json.dumps({
            "ownership": runtime.ownership.state(npc_id).value,
            "goal": state.current_goal,
            "mood": state.mood,
            "conversation_active": state.conversation_active,
        }, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
