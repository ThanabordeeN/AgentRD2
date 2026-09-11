import tempfile
import unittest
from pathlib import Path

from runtime.agent.backends import GoogleADKBackend
from runtime.agent.npc_agent import NpcAgentRuntime
from runtime.agent.openai_compat import OpenAICompatibleBackend
from runtime.config import load_settings
from runtime.schemas import AgentAction, AgentContext, AgentDecision, Event
from runtime.timeline.store import TimelineStore


class ThinkingPolicyTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.settings = load_settings()
        self.settings["timelines_dir"] = str(Path(self.tmp.name) / "timelines")
        self.timeline = TimelineStore(self.settings["timelines_dir"])
        self.runtime = NpcAgentRuntime(settings=self.settings, timeline=self.timeline)

    def tearDown(self):
        self.tmp.cleanup()

    def _context(self, mode):
        return AgentContext(
            npc_id="npc_001",
            profile={},
            world_state={},
            available_tools=[],
            flags={"thinking_mode": mode},
        )

    def test_short_event_uses_disabled_thinking(self):
        event = Event(seq=1, event_name="PLAYER_APPROACHED", npc_id="npc_001")
        context = self.runtime.build_context("npc_001", reason="meaningful_event:PLAYER_APPROACHED", trigger_event=event)
        self.assertEqual(context.flags["thinking_mode"], "disabled")

    def test_threat_event_uses_low_thinking(self):
        event = Event(seq=1, event_name="PLAYER_THREATENED_NPC", npc_id="npc_001")
        context = self.runtime.build_context("npc_001", reason="meaningful_event:PLAYER_THREATENED_NPC", trigger_event=event)
        self.assertEqual(context.flags["thinking_mode"], "low")

    def test_player_speech_uses_low_thinking(self):
        event = Event(seq=1, event_name="PLAYER_SPOKE", npc_id="npc_001")
        context = self.runtime.build_context("npc_001", reason="player_spoke", trigger_event=event)
        self.assertEqual(context.flags["thinking_mode"], "low")

    def test_ped_scan_activation_uses_disabled_thinking(self):
        context = self.runtime.build_context("npc_001", reason="ped_scan_activation")
        self.assertEqual(context.flags["thinking_mode"], "disabled")

    def test_openai_body_disabled_thinking(self):
        backend = OpenAICompatibleBackend(
            model="test-model",
            api_base="http://127.0.0.1:9999/v1",
            api_key="test-key",
            reasoning_effort="low",
            extra_body={"thinking": {"type": "enabled"}},
        )
        body = backend.build_body(self._context("disabled"))
        self.assertNotIn("reasoning_effort", body)
        self.assertEqual(body["thinking"], {"type": "disabled"})

    def test_openai_body_low_thinking(self):
        backend = OpenAICompatibleBackend(
            model="test-model",
            api_base="http://127.0.0.1:9999/v1",
            api_key="test-key",
            reasoning_effort="low",
            extra_body={"thinking": {"type": "enabled"}},
        )
        body = backend.build_body(self._context("low"))
        self.assertEqual(body["reasoning_effort"], "low")
        self.assertEqual(body["thinking"], {"type": "enabled"})

    def test_adk_litellm_kwargs_mode_switch(self):
        backend = GoogleADKBackend(
            model="test-model",
            api_base="http://127.0.0.1:9999/v1",
            api_key="test-key",
            reasoning_effort="low",
            extra_body={"thinking": {"type": "enabled"}},
        )
        disabled = backend._litellm_kwargs("disabled")
        self.assertNotIn("reasoning_effort", disabled)
        self.assertEqual(disabled["extra_body"]["thinking"], {"type": "disabled"})
        low = backend._litellm_kwargs("low")
        self.assertEqual(low["reasoning_effort"], "low")
        self.assertEqual(low["extra_body"]["thinking"], {"type": "enabled"})


if __name__ == "__main__":
    unittest.main()
