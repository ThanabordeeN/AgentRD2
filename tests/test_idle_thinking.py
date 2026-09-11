import tempfile
import time
import unittest
from pathlib import Path

from runtime.agent.npc_agent import NpcAgentRuntime
from runtime.config import load_settings
from runtime.schemas import AgentDecision
from runtime.state.ownership import OwnershipState
from runtime.timeline.store import TimelineStore
from runtime.tools import ToolContext


class _CountingBackend:
    def __init__(self):
        self.calls = 0

    def decide(self, context, tool_context=None):
        self.calls += 1
        return AgentDecision(goal=None, mood="neutral", speech=None, actions=[])


class IdleThinkingTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.settings = load_settings()
        self.settings["timelines_dir"] = str(Path(self.tmp.name) / "timelines")
        self.settings["scheduler"]["idle_thinking_seconds"] = 5.0
        self.timeline = TimelineStore(self.settings["timelines_dir"])
        self.backend = _CountingBackend()
        self.runtime = NpcAgentRuntime(settings=self.settings, timeline=self.timeline, backend=self.backend)
        self.npc_id = "npc_001"

    def tearDown(self):
        self.tmp.cleanup()

    def _ped(self, distance=8.0):
        return {
            "entity_id": self.npc_id, "model": "a_m_m_farmer_01", "name": "Elias Carter",
            "is_ped": True, "is_human": True, "is_alive": True, "is_player": False,
            "is_story_character": False, "is_mission_owned": False,
            "in_scripted_state": False, "in_cutscene": False, "blacklisted": False,
            "distance_m": distance, "visible": True, "health": 100,
        }

    def _activate(self):
        self.runtime.handle_message({"type": "ped_scan", "peds": [self._ped(15.0)]})
        self.runtime.handle_message({"type": "ped_scan", "peds": [self._ped(8.0)]})
        self.runtime.handle_message({
            "type": "game_event", "event_name": "PLAYER_LOOKED_AT_NPC",
            "npc_id": self.npc_id, "entities": ["player"], "tags": ["player"], "data": {},
        })
        self.assertEqual(self.backend.calls, 1)
        self.assertEqual(self.runtime.ownership.state(self.npc_id), OwnershipState.AI_ACTIVE)

    def test_idle_thinking_fires_after_interval(self):
        self._activate()
        state = self.runtime._agent_state(self.npc_id)
        state.last_decision_at = time.time() - 100
        self.runtime.handle_message({"type": "ped_scan", "peds": [self._ped(8.0)]})
        self.assertEqual(self.backend.calls, 2)

    def test_idle_thinking_skips_when_disabled(self):
        self._activate()
        self.settings["scheduler"]["idle_thinking_seconds"] = 0
        state = self.runtime._agent_state(self.npc_id)
        state.last_decision_at = time.time() - 100
        self.runtime.handle_message({"type": "ped_scan", "peds": [self._ped(8.0)]})
        self.assertEqual(self.backend.calls, 1)

    def test_idle_thinking_skips_while_action_pending(self):
        self._activate()
        ctx = ToolContext(self.npc_id, self.runtime.world, self.timeline, self.runtime.dispatcher)
        self.runtime.registry.call("go_to", ctx, destination="Valentine Saloon")
        state = self.runtime._agent_state(self.npc_id)
        state.last_decision_at = time.time() - 100
        self.runtime.handle_message({"type": "ped_scan", "peds": [self._ped(8.0)]})
        self.assertEqual(self.backend.calls, 1)

    def test_idle_thinking_skips_while_suspended(self):
        self._activate()
        self.runtime.handle_message({"type": "story_safety", "active": True, "reason": "mission"})
        state = self.runtime._agent_state(self.npc_id)
        state.last_decision_at = time.time() - 100
        self.runtime.handle_message({"type": "ped_scan", "peds": [self._ped(8.0)]})
        self.assertEqual(self.backend.calls, 1)


if __name__ == "__main__":
    unittest.main()
