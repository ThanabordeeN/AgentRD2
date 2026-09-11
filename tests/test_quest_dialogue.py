import tempfile
import unittest
from pathlib import Path

from runtime.agent.npc_agent import NpcAgentRuntime
from runtime.config import load_settings
from runtime.schemas import AgentAction, AgentDecision, AgentSpeech
from runtime.state.ownership import OwnershipState
from runtime.timeline.store import TimelineStore


class _QuestBackend:
    supports_wait_gestures = True

    def __init__(self):
        self.calls = 0

    def decide(self, context, tool_context=None):
        self.calls += 1
        return AgentDecision(
            goal="help with the auction",
            mood="busy",
            speech=AgentSpeech("Auction's this evenin'. Keep them cattle movin'.", target="player"),
            actions=[AgentAction("go_to", {"destination": "Valentine Saloon"})],
        )


def _quest_ped(npc_id="npc_quest_giver", distance=8.0):
    return {
        "entity_id": npc_id,
        "model": "a_m_m_farmer_01",
        "name": "Quest Farmer",
        "is_ped": True, "is_human": True, "is_alive": True, "is_player": False,
        "is_story_character": True, "is_mission_owned": True,
        "in_scripted_state": False, "in_cutscene": False, "blacklisted": False,
        "distance_m": distance, "visible": True, "health": 100,
        "metadata": {
            "quest_dialogue": True,
            "quest_id": "quest_valentine_livestock",
            "quest_state": {"current_objective": "Drive the cattle to the auction pens before evening."},
        },
    }


class QuestDialogueTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.settings = load_settings()
        self.settings["timelines_dir"] = str(Path(self.tmp.name) / "timelines")
        self.timeline = TimelineStore(self.settings["timelines_dir"])
        self.backend = _QuestBackend()
        self.runtime = NpcAgentRuntime(settings=self.settings, timeline=self.timeline, backend=self.backend)
        self.npc_id = "npc_quest_giver"

    def tearDown(self):
        self.tmp.cleanup()

    def _enter(self):
        self.runtime.handle_message({"type": "ped_scan", "peds": [_quest_ped(distance=15.0)]})
        self.runtime.handle_message({"type": "ped_scan", "peds": [_quest_ped(distance=8.0)]})

    def test_quest_npc_enters_dialogue_only_without_llm_call(self):
        self._enter()
        self.assertEqual(self.runtime.ownership.state(self.npc_id), OwnershipState.QUEST_DIALOGUE)
        state = self.runtime._agent_state(self.npc_id)
        self.assertTrue(state.dialogue_only)
        self.assertEqual(state.quest_context["quest_id"], "quest_valentine_livestock")
        self.assertEqual(self.backend.calls, 0)

    def test_quest_dialogue_speaks_but_blocks_movement(self):
        self._enter()
        self.runtime.handle_message({
            "type": "game_event", "event_name": "PLAYER_APPROACHED",
            "npc_id": self.npc_id, "entities": ["player"], "tags": ["player"], "data": {"distance": 8.0},
        })
        events = self.timeline.all_events(self.npc_id)
        names = [event.event_name for event in events]
        self.assertIn("NPC_SPOKE", names)
        # Movement action must be blocked in dialogue-only mode.
        action_tools = [
            event.data.get("tool") for event in events if event.event_name == "ACTION_STARTED"
        ]
        self.assertIn("say", action_tools)
        self.assertNotIn("go_to", action_tools)
        blocked = [e for e in events if e.event_name == "ACTION_FAILED" and e.data.get("reason") == "dialogue_only_mode"]
        self.assertEqual(len(blocked), 1)

    def test_quest_dialogue_hides_action_tools_from_prompt(self):
        self._enter()
        context = self.runtime.build_context(self.npc_id, reason="quest_dialogue")
        self.assertTrue(context.flags.get("dialogue_only"))
        self.assertNotIn("go_to", context.available_tools)
        self.assertIn("say", context.available_tools)
        self.assertTrue(context.quest_context)
        self.assertIn("CURRENT QUEST CONTEXT", self._prompt(context))

    def test_push_to_talk_works_and_movement_stays_blocked(self):
        self._enter()
        self.runtime.handle_message({"type": "push_to_talk", "action": "start", "npc_id": self.npc_id})
        self.assertEqual(self.runtime.ownership.state(self.npc_id), OwnershipState.AI_CONVERSATION)
        self.runtime.handle_player_speech({"npc_id": self.npc_id, "text": "Where are the pens?"})
        events = self.timeline.all_events(self.npc_id)
        names = [event.event_name for event in events]
        self.assertIn("PLAYER_SPOKE", names)
        self.assertIn("NPC_SPOKE", names)
        self.assertNotIn("ACTION_STARTED", [])  # sanity
        moving = [
            e.data.get("tool") for e in events
            if e.event_name == "ACTION_STARTED" and e.data.get("tool") in {"go_to", "wander", "follow"}
        ]
        self.assertEqual(moving, [])

    def test_leaving_area_exits_quest_dialogue(self):
        self._enter()
        self.runtime.handle_message({"type": "ped_scan", "peds": [_quest_ped(distance=50.0)]})
        self.assertEqual(self.runtime.ownership.state(self.npc_id), OwnershipState.ROCKSTAR)
        self.assertFalse(self.runtime._agent_state(self.npc_id).dialogue_only)
        names = [event.event_name for event in self.timeline.all_events(self.npc_id)]
        self.assertIn("QUEST_DIALOGUE_ENTERED", names)
        self.assertIn("NPC_RELEASED", names)

    @staticmethod
    def _prompt(context):
        from runtime.agent.instructions import build_agent_prompt

        return build_agent_prompt(context.to_dict())


if __name__ == "__main__":
    unittest.main()
