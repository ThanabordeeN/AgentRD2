import tempfile
import unittest
from pathlib import Path

from runtime.agent.backends import RuleBasedAgentBackend
from runtime.agent.npc_agent import NpcAgentRuntime
from runtime.config import load_settings
from runtime.schemas import AgentDecision
from runtime.timeline.store import TimelineStore


class _SlowStubBackend:
    supports_wait_gestures = True

    def __init__(self):
        self.calls = 0

    def decide(self, context, tool_context=None):
        self.calls += 1
        return AgentDecision(goal=None, mood="neutral", speech=None, actions=[])


def _ped(npc_id="npc_001", distance=8.0):
    return {
        "entity_id": npc_id, "model": "a_m_m_farmer_01", "name": "Elias Carter",
        "is_ped": True, "is_human": True, "is_alive": True, "is_player": False,
        "is_story_character": False, "is_mission_owned": False,
        "in_scripted_state": False, "in_cutscene": False, "blacklisted": False,
        "distance_m": distance, "visible": True, "health": 100,
    }


class WaitGestureTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.settings = load_settings()
        self.settings["timelines_dir"] = str(Path(self.tmp.name) / "timelines")
        self.timeline = TimelineStore(self.settings["timelines_dir"])
        self.npc_id = "npc_001"

    def tearDown(self):
        self.tmp.cleanup()

    def _runtime(self, backend):
        return NpcAgentRuntime(settings=self.settings, timeline=self.timeline, backend=backend)

    def _aware(self, runtime):
        runtime.handle_message({"type": "ped_scan", "peds": [_ped(distance=15.0)]})
        runtime.handle_message({"type": "ped_scan", "peds": [_ped(distance=8.0)]})

    def _think_events(self, runtime):
        return [
            event for event in runtime.timeline.all_events(self.npc_id)
            if event.event_name == "ACTION_STARTED" and event.data.get("tool") == "think"
        ]

    def test_activation_plays_think_gesture_for_slow_backend(self):
        runtime = self._runtime(_SlowStubBackend())
        self._aware(runtime)
        runtime.handle_message({
            "type": "game_event", "event_name": "PLAYER_LOOKED_AT_NPC",
            "npc_id": self.npc_id, "entities": ["player"], "tags": ["player"], "data": {},
        })
        events = self._think_events(runtime)
        self.assertEqual(len(events), 1)
        self.assertEqual(events[0].data["arguments"]["style"], "think")

    def test_player_speech_plays_listen_gesture(self):
        runtime = self._runtime(_SlowStubBackend())
        self._aware(runtime)
        runtime.handle_player_speech({"npc_id": self.npc_id, "text": "Hello there."})
        events = self._think_events(runtime)
        self.assertTrue(any(e.data["arguments"]["style"] == "listen" for e in events))

    def test_wait_gesture_can_be_disabled(self):
        self.settings["thinking_policy"]["wait_gestures"] = False
        runtime = self._runtime(_SlowStubBackend())
        self._aware(runtime)
        runtime.handle_message({
            "type": "game_event", "event_name": "PLAYER_LOOKED_AT_NPC",
            "npc_id": self.npc_id, "entities": ["player"], "tags": ["player"], "data": {},
        })
        self.assertEqual(self._think_events(runtime), [])

    def test_rule_based_backend_does_not_emit_wait_gesture(self):
        runtime = self._runtime(RuleBasedAgentBackend())
        self._aware(runtime)
        runtime.handle_message({
            "type": "game_event", "event_name": "PLAYER_LOOKED_AT_NPC",
            "npc_id": self.npc_id, "entities": ["player"], "tags": ["player"], "data": {},
        })
        self.assertEqual(self._think_events(runtime), [])


if __name__ == "__main__":
    unittest.main()
