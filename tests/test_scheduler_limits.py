import tempfile
import unittest
from pathlib import Path

from runtime.agent.npc_agent import NpcAgentRuntime
from runtime.config import load_settings
from runtime.schemas import AgentDecision, AgentSpeech
from runtime.state.ownership import OwnershipState
from runtime.timeline.store import TimelineStore


class _CountingBackend:
    def __init__(self):
        self.calls = 0

    def decide(self, context, tool_context=None):
        self.calls += 1
        return AgentDecision(goal=None, mood="neutral", speech=None, actions=[])


class _TalkingBackend(_CountingBackend):
    def decide(self, context, tool_context=None):
        self.calls += 1
        return AgentDecision(speech=AgentSpeech("Hello there."), actions=[])


class SchedulerLimitTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.settings = load_settings()
        self.settings["timelines_dir"] = str(Path(self.tmp.name) / "timelines")
        self.timeline = TimelineStore(self.settings["timelines_dir"])
        self.npcs = [f"npc_{i:02d}" for i in range(1, 11)]

    def tearDown(self):
        self.tmp.cleanup()

    def _ped(self, npc_id, distance=8.0):
        return {
            "entity_id": npc_id,
            "model": "a_m_m_farmer_01",
            "name": npc_id,
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
        }

    def _scan_all(self, distance=8.0):
        self.runtime.handle_message(
            {"type": "ped_scan", "peds": [self._ped(npc, distance) for npc in self.npcs]}
        )

    def _activate_all(self):
        for npc in self.npcs:
            self.runtime.handle_message(
                {
                    "type": "game_event",
                    "event_name": "PLAYER_LOOKED_AT_NPC",
                    "npc_id": npc,
                    "entities": ["player"],
                    "tags": ["player"],
                    "data": {},
                }
            )

    def test_many_nearby_npcs_are_aware_without_llm_calls(self):
        backend = _CountingBackend()
        self.runtime = NpcAgentRuntime(settings=self.settings, timeline=self.timeline, backend=backend)
        self.runtime.handle_message({"type": "ped_scan", "peds": [self._ped(n, 15.0) for n in self.npcs]})
        self._scan_all(8.0)
        states = [self.runtime.ownership.state(n) for n in self.npcs]
        self.assertTrue(all(state == OwnershipState.AWARE for state in states))
        self.assertEqual(backend.calls, 0)

    def test_scheduler_caps_active_reasoning_agents(self):
        backend = _CountingBackend()
        self.runtime = NpcAgentRuntime(settings=self.settings, timeline=self.timeline, backend=backend)
        self.runtime.handle_message({"type": "ped_scan", "peds": [self._ped(n, 15.0) for n in self.npcs]})
        self._scan_all(8.0)
        self._activate_all()

        active = [
            n for n in self.npcs
            if self.runtime.ownership.state(n) in {OwnershipState.AI_ACTIVE, OwnershipState.AI_CONVERSATION}
        ]
        aware = [n for n in self.npcs if self.runtime.ownership.state(n) == OwnershipState.AWARE]
        self.assertEqual(len(active), 3)
        self.assertEqual(len(aware), 7)
        self.assertEqual(backend.calls, 3)

    def test_deferred_agent_is_promoted_when_slot_frees(self):
        backend = _CountingBackend()
        self.runtime = NpcAgentRuntime(settings=self.settings, timeline=self.timeline, backend=backend)
        self.runtime.handle_message({"type": "ped_scan", "peds": [self._ped(n, 15.0) for n in self.npcs[:4]]})
        for npc in self.npcs[:4]:
            self.runtime.handle_message({"type": "ped_scan", "peds": [self._ped(npc, 8.0)]})
        for npc in self.npcs[:4]:
            self.runtime.handle_message({
                "type": "game_event",
                "event_name": "PLAYER_LOOKED_AT_NPC",
                "npc_id": npc,
                "entities": ["player"],
                "tags": ["player"],
                "data": {},
            })

        self.assertEqual(backend.calls, 3)
        self.assertEqual(self.runtime.ownership.state(self.npcs[3]), OwnershipState.AWARE)

        # Release one active agent; the deferred one should take its slot.
        first_active = next(
            n for n in self.npcs[:3]
            if self.runtime.ownership.state(n) == OwnershipState.AI_ACTIVE
        )
        transition = self.runtime.ownership.request_release(first_active, "test_release")
        self.runtime._record_transition(transition)
        self.assertEqual(self.runtime.ownership.state(self.npcs[3]), OwnershipState.AI_ACTIVE)
        self.assertEqual(backend.calls, 4)

    def test_global_speech_cooldown_prevents_chorus(self):
        backend = _TalkingBackend()
        self.runtime = NpcAgentRuntime(settings=self.settings, timeline=self.timeline, backend=backend)
        self.runtime.handle_message({"type": "ped_scan", "peds": [self._ped(n, 15.0) for n in self.npcs]})
        self._scan_all(8.0)
        self._activate_all()

        spoken = [
            e for n in self.npcs for e in self.timeline.all_events(n)
            if e.event_name == "NPC_SPOKE"
        ]
        self.assertEqual(len(spoken), 1)
        self.assertEqual(backend.calls, 3)

    def test_only_one_conversation_agent_at_a_time(self):
        backend = _CountingBackend()
        self.runtime = NpcAgentRuntime(settings=self.settings, timeline=self.timeline, backend=backend)
        # Put two NPCs into AWARE.
        for npc in ("npc_01", "npc_02"):
            self.runtime.handle_message({"type": "ped_scan", "peds": [self._ped(npc, 15.0)]})
            self.runtime.handle_message({"type": "ped_scan", "peds": [self._ped(npc, 8.0)]})

        self.runtime.handle_message({"type": "push_to_talk", "action": "start", "npc_id": "npc_01"})
        self.runtime.handle_message({"type": "push_to_talk", "action": "start", "npc_id": "npc_02"})

        self.assertEqual(self.runtime.ownership.state("npc_01"), OwnershipState.AI_ACTIVE)
        self.assertEqual(self.runtime.ownership.state("npc_02"), OwnershipState.AI_CONVERSATION)
        conversations = [
            n for n, record in self.runtime.ownership.records.items()
            if record.state == OwnershipState.AI_CONVERSATION
        ]
        self.assertEqual(conversations, ["npc_02"])


if __name__ == "__main__":
    unittest.main()
