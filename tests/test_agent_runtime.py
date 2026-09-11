import tempfile
import unittest
from pathlib import Path

from runtime.agent.backends import RuleBasedAgentBackend
from runtime.agent.npc_agent import NpcAgentRuntime
from runtime.config import load_settings
from runtime.state.ownership import OwnershipState
from runtime.timeline.store import TimelineStore


def safe_ped(distance=6.2):
    return {
        "entity_id": "npc_001",
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
        "distance_m": distance,
        "visible": True,
        "health": 100,
        "metadata": {"camera_alignment": 0.9},
    }


class AgentRuntimeTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        settings = load_settings()
        settings["timelines_dir"] = str(Path(self.tmp.name) / "timelines")
        self.runtime = NpcAgentRuntime(
            settings=settings,
            timeline=TimelineStore(settings["timelines_dir"]),
            backend=RuleBasedAgentBackend(silence_probability=0.0),
        )
        self.npc_id = "npc_001"

    def tearDown(self):
        self.tmp.cleanup()

    def _activate(self):
        self.runtime.handle_ped_scan({"type": "ped_scan", "peds": [safe_ped(15.0)]})
        self.runtime.handle_ped_scan({"type": "ped_scan", "peds": [safe_ped(8.0)]})
        self.runtime.handle_game_event({
            "type": "game_event",
            "event_name": "PLAYER_LOOKED_AT_NPC",
            "npc_id": self.npc_id,
            "entities": ["player"],
            "tags": ["player"],
            "data": {},
        })
        self.assertEqual(self.runtime.ownership.state(self.npc_id), OwnershipState.AI_ACTIVE)

    def test_proximity_and_trigger_activate_npc(self):
        self._activate()
        names = [e.event_name for e in self.runtime.timeline.all_events(self.npc_id)]
        self.assertIn("NPC_ACTIVATED", names)

    def test_player_speech_moves_to_conversation_and_generates_reply(self):
        self._activate()
        self.runtime.handle_player_speech({"npc_id": self.npc_id, "text": "Where are you headed?"})
        self.assertEqual(self.runtime.ownership.state(self.npc_id), OwnershipState.AI_CONVERSATION)
        events = self.runtime.timeline.all_events(self.npc_id)
        names = [e.event_name for e in events]
        self.assertIn("PLAYER_SPOKE", names)
        self.assertIn("NPC_SPOKE", names)
        spoken = [e.data["text"] for e in events if e.event_name == "NPC_SPOKE"]
        self.assertTrue(any("Valentine" in text for text in spoken))

    def test_threat_causes_flee_for_low_courage_profile(self):
        self._activate()
        self.runtime.handle_game_event({
            "type": "game_event",
            "event_name": "PLAYER_THREATENED_NPC",
            "npc_id": self.npc_id,
            "entities": ["player"],
            "tags": ["player", "threat"],
            "data": {"weapon": "revolver", "distance": 2.6},
        })
        events = self.runtime.timeline.all_events(self.npc_id)
        actions = [
            e.data.get("tool")
            for e in events
            if e.event_name == "ACTION_STARTED"
        ]
        self.assertIn("flee_from", actions)
        self.assertTrue(self.runtime.ownership.can_reason(self.npc_id))

    def test_direct_speech_still_works_with_silence_probability(self):
        runtime = NpcAgentRuntime(
            settings=self.runtime.settings,
            timeline=TimelineStore(str(Path(self.tmp.name) / "other")),
            backend=RuleBasedAgentBackend(silence_probability=1.0),
        )
        runtime.handle_ped_scan({"type": "ped_scan", "peds": [safe_ped(15.0)]})
        runtime.handle_ped_scan({"type": "ped_scan", "peds": [safe_ped(8.0)]})
        runtime.handle_game_event({
            "type": "game_event", "event_name": "PLAYER_LOOKED_AT_NPC",
            "npc_id": self.npc_id, "entities": ["player"], "data": {},
        })
        runtime.handle_player_speech({"npc_id": self.npc_id, "text": "Hello there."})
        names = [e.event_name for e in runtime.timeline.all_events(self.npc_id)]
        self.assertNotIn("NPC_SPOKE", names)


if __name__ == "__main__":
    unittest.main()


class StorySafetyTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        settings = load_settings()
        settings["timelines_dir"] = str(Path(self.tmp.name) / "timelines")
        self.runtime = NpcAgentRuntime(
            settings=settings,
            timeline=TimelineStore(settings["timelines_dir"]),
        )
        self.npc_id = "npc_001"

    def tearDown(self):
        self.tmp.cleanup()

    def _activate(self):
        self.runtime.handle_ped_scan({"type": "ped_scan", "peds": [safe_ped(15.0)]})
        self.runtime.handle_ped_scan({"type": "ped_scan", "peds": [safe_ped(8.0)]})
        self.runtime.handle_game_event({
            "type": "game_event",
            "event_name": "PLAYER_LOOKED_AT_NPC",
            "npc_id": self.npc_id,
            "entities": ["player"],
            "tags": ["player"],
            "data": {},
        })

    def test_story_safety_suspends_and_resumes(self):
        self._activate()
        self.runtime.handle_story_safety({"active": True, "reason": "mission"})
        self.assertEqual(self.runtime.ownership.state(self.npc_id).value, "SUSPENDED")
        names = [e.event_name for e in self.runtime.timeline.all_events(self.npc_id)]
        self.assertIn("AGENT_SUSPENDED", names)
        self.runtime.handle_story_safety({"active": False, "reason": "mission_complete"})
        self.assertEqual(self.runtime.ownership.state(self.npc_id).value, "AI_ACTIVE")
        names = [e.event_name for e in self.runtime.timeline.all_events(self.npc_id)]
        self.assertIn("AGENT_RESUMED", names)
