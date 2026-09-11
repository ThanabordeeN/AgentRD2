import tempfile
import unittest
from pathlib import Path

from runtime.agent.npc_agent import NpcAgentRuntime
from runtime.config import load_settings
from runtime.schemas import AgentDecision
from runtime.state.ownership import OwnershipState
from runtime.timeline.store import TimelineStore
from runtime.tools import ToolContext


class _GoalBackend:
    def decide(self, context, tool_context=None):
        return AgentDecision(goal="finish the day's work", mood="determined", actions=[])


class _NoopBackend:
    def decide(self, context, tool_context=None):
        return AgentDecision(goal=None, mood="neutral", actions=[])


class ActivityPersistenceTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.settings = load_settings()
        self.settings["timelines_dir"] = str(Path(self.tmp.name) / "timelines")
        self.npc_id = "npc_001"

    def tearDown(self):
        self.tmp.cleanup()

    def _ped(self, distance):
        return {
            "entity_id": self.npc_id,
            "model": "a_m_m_farmer_01",
            "name": "Elias Carter",
            "is_ped": True, "is_human": True, "is_alive": True,
            "is_player": False, "is_story_character": False,
            "is_mission_owned": False, "in_scripted_state": False,
            "in_cutscene": False, "blacklisted": False,
            "distance_m": distance, "visible": True, "health": 100,
        }

    def _activate(self, runtime):
        runtime.handle_message({"type": "ped_scan", "peds": [self._ped(15.0)]})
        runtime.handle_message({"type": "ped_scan", "peds": [self._ped(8.0)]})
        runtime.handle_message({
            "type": "game_event", "event_name": "PLAYER_LOOKED_AT_NPC",
            "npc_id": self.npc_id, "entities": ["player"], "tags": ["player"], "data": {},
        })

    def test_release_does_not_delete_activity_timeline(self):
        timeline = TimelineStore(self.settings["timelines_dir"])
        runtime = NpcAgentRuntime(settings=self.settings, timeline=timeline)
        self._activate(runtime)
        runtime.handle_player_speech({"npc_id": self.npc_id, "text": "Where are you headed?"})
        before = [e.event_name for e in timeline.all_events(self.npc_id)]
        self.assertIn("PLAYER_SPOKE", before)

        runtime.handle_message({"type": "ped_scan", "peds": [self._ped(50.0)]})
        self.assertEqual(runtime.ownership.state(self.npc_id), OwnershipState.ROCKSTAR)
        after = [e.event_name for e in timeline.all_events(self.npc_id)]
        self.assertEqual(before, after[: len(before)])
        self.assertIn("NPC_RELEASED", after)

    def test_history_is_available_to_new_runtime_instance(self):
        timeline = TimelineStore(self.settings["timelines_dir"])
        runtime = NpcAgentRuntime(settings=self.settings, timeline=timeline, backend=_GoalBackend())
        self._activate(runtime)
        runtime.handle_player_speech({"npc_id": self.npc_id, "text": "Hello there."})
        old_names = [e.event_name for e in timeline.all_events(self.npc_id)]

        # Simulate a runtime restart against the same JSONL directory.
        restarted_timeline = TimelineStore(self.settings["timelines_dir"])
        restarted = NpcAgentRuntime(
            settings=self.settings,
            timeline=restarted_timeline,
            backend=_GoalBackend(),
        )
        self._activate(restarted)
        context = ToolContext(self.npc_id, restarted.world, restarted_timeline, restarted.dispatcher)
        history = restarted.registry.call("grab_timeline", context, limit=50)
        history_names = [event["event_name"] for event in history["events"]]
        for name in old_names:
            self.assertIn(name, history_names)

    def test_goal_is_rehydrated_after_runtime_restart(self):
        timeline = TimelineStore(self.settings["timelines_dir"])
        first = NpcAgentRuntime(settings=self.settings, timeline=timeline, backend=_GoalBackend())
        self._activate(first)
        self.assertEqual(first._agent_state(self.npc_id).current_goal, "finish the day's work")
        first.handle_message({"type": "ped_scan", "peds": [self._ped(50.0)]})

        restarted_timeline = TimelineStore(self.settings["timelines_dir"])
        restarted = NpcAgentRuntime(
            settings=self.settings,
            timeline=restarted_timeline,
            backend=_NoopBackend(),
        )
        self._activate(restarted)
        self.assertEqual(restarted._agent_state(self.npc_id).current_goal, "finish the day's work")

    def test_session_memory_survives_release_inside_runtime(self):
        timeline = TimelineStore(self.settings["timelines_dir"])
        runtime = NpcAgentRuntime(settings=self.settings, timeline=timeline, backend=_GoalBackend())
        self._activate(runtime)
        self.assertEqual(runtime._agent_state(self.npc_id).current_goal, "finish the day's work")
        runtime.handle_message({"type": "ped_scan", "peds": [self._ped(50.0)]})
        self.assertEqual(runtime.ownership.state(self.npc_id), OwnershipState.ROCKSTAR)
        # Temporary ownership is released, but in-memory session context is not
        # erased; the JSONL history is the long-term truth.
        self.assertEqual(runtime._agent_state(self.npc_id).current_goal, "finish the day's work")


if __name__ == "__main__":
    unittest.main()
