import tempfile
import unittest
from pathlib import Path

from runtime.agent.npc_agent import NpcAgentRuntime
from runtime.config import load_settings
from runtime.schemas import AgentAction, AgentDecision, AgentSpeech
from runtime.timeline.store import TimelineStore


class ActionStabilizationTests(unittest.TestCase):
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

    def test_entity_arguments_default_to_player(self):
        decision = AgentDecision(actions=[AgentAction("face", {}), AgentAction("look_at", {})])
        result = self.runtime._stabilize_decision(self.npc_id, decision)
        self.assertEqual([a.tool for a in result.actions], ["face", "look_at"])
        self.assertEqual(result.actions[0].arguments["entity"], "player")
        self.assertEqual(result.actions[1].arguments["entity"], "player")

    def test_wait_duration_defaults(self):
        decision = AgentDecision(actions=[AgentAction("wait", {})])
        result = self.runtime._stabilize_decision(self.npc_id, decision)
        self.assertEqual(result.actions[0].arguments["duration"], 2.0)

    def test_go_to_without_destination_falls_back_to_wander(self):
        decision = AgentDecision(goal="walk to the saloon", actions=[AgentAction("go_to", {})])
        result = self.runtime._stabilize_decision(self.npc_id, decision)
        self.assertEqual(result.actions[0].tool, "wander")
        self.assertEqual(result.actions[0].arguments["radius"], 8.0)

    def test_investigate_uses_recent_position(self):
        self.runtime.timeline.append_event(
            self.npc_id,
            "GUNSHOT_HEARD",
            data={"position": [100.0, 200.0, 10.0]},
            tags=["world"],
        )
        decision = AgentDecision(actions=[AgentAction("investigate", {})])
        result = self.runtime._stabilize_decision(self.npc_id, decision)
        self.assertEqual(result.actions[0].tool, "investigate")
        self.assertEqual(result.actions[0].arguments["position"], [100.0, 200.0, 10.0])

    def test_duplicate_empty_say_is_dropped_silently(self):
        decision = AgentDecision(
            speech=AgentSpeech("Evening."),
            actions=[AgentAction("say", {})],
        )
        result = self.runtime._stabilize_decision(self.npc_id, decision)
        self.assertEqual(result.actions, [])
        self.assertFalse(
            any(e.event_name == "ACTION_FAILED" for e in self.runtime.timeline.all_events(self.npc_id))
        )

    def test_unsupported_action_is_logged_and_dropped(self):
        decision = AgentDecision(actions=[AgentAction("attack", {})])
        result = self.runtime._stabilize_decision(self.npc_id, decision)
        self.assertEqual(result.actions, [])
        events = self.runtime.timeline.all_events(self.npc_id)
        self.assertEqual(events[-1].event_name, "ACTION_FAILED")
        self.assertEqual(events[-1].data["reason"], "unsupported_tool")


if __name__ == "__main__":
    unittest.main()


class BehaviorGuardrailTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        settings = load_settings()
        settings["timelines_dir"] = str(Path(self.tmp.name) / "timelines")
        self.runtime = NpcAgentRuntime(
            settings=settings,
            timeline=TimelineStore(settings["timelines_dir"]),
        )

    def tearDown(self):
        self.tmp.cleanup()

    def _context(self, npc_id, event):
        from runtime.schemas import AgentContext

        return AgentContext(
            npc_id=npc_id,
            profile=self.runtime.profiles.get(npc_id),
            world_state={},
            recent_events=[event],
            flags={"trigger_event": event},
        )

    def test_low_courage_threat_gets_flee_action(self):
        decision = AgentDecision(actions=[])
        context = self._context(
            "npc_001",
            {
                "event_name": "PLAYER_THREATENED_NPC",
                "data": {"weapon": "revolver", "distance": 2.0},
            },
        )
        result = self.runtime._apply_behavior_guardrails("npc_001", decision, context)
        self.assertIn("flee_from", [action.tool for action in result.actions])

    def test_high_courage_gunshot_gets_investigate_action(self):
        decision = AgentDecision(actions=[])
        context = self._context(
            "npc_hunter",
            {
                "event_name": "GUNSHOT_HEARD",
                "data": {"position": [100.0, 200.0, 10.0]},
            },
        )
        result = self.runtime._apply_behavior_guardrails("npc_hunter", decision, context)
        investigate = next(action for action in result.actions if action.tool == "investigate")
        self.assertEqual(investigate.arguments["position"], [100.0, 200.0, 10.0])
