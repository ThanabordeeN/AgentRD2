import os
import unittest
from pathlib import Path

from runtime.agent.openai_compat import OpenAICompatibleBackend, resolve_api_key
from runtime.config import load_settings
from runtime.schemas import AgentContext, AgentDecision
from runtime.tools import build_default_registry
from scenario_runner import run_scenario_file


RUN_LIVE = os.environ.get("RUN_LIVE_LLM") == "1"
HAS_KEY = bool(resolve_api_key(None, "OPENCODE_API_KEY"))


@unittest.skipUnless(RUN_LIVE and HAS_KEY, "set RUN_LIVE_LLM=1 and provide OPENCODE_API_KEY")
class LiveLLMTests(unittest.TestCase):
    def _backend(self):
        adk = load_settings().get("adk", {})
        return OpenAICompatibleBackend(
            model=adk.get("model", "deepseek-v4.1-flash"),
            api_base=adk.get("api_base", "https://opencode.ai/zen/go/v1"),
            api_key_env=adk.get("api_key_env", "OPENCODE_API_KEY"),
            reasoning_effort=adk.get("reasoning_effort", "low"),
            extra_body=adk.get("extra_body") or {},
            max_tokens=4096,
        )

    def test_live_model_returns_structured_decision(self):
        backend = self._backend()
        registry = build_default_registry()
        context = AgentContext(
            npc_id="npc_001",
            profile={"npc_id": "npc_001", "name": "Elias Carter", "personality": {"sociability": 0.5}},
            world_state={"self": {"health": 100}, "player": {"distance": 4.0}},
            current_goal=None,
            current_mood="neutral",
            recent_events=[{"event_name": "PLAYER_APPROACHED", "data": {"distance": 4.0}}],
            available_tools=registry.names(),
            flags={"reason": "live_test"},
        )
        decision = backend.decide(context)
        self.assertIsInstance(decision, AgentDecision)
        self.assertEqual(backend.call_count, 1)
        payload = decision.to_dict()
        self.assertIn("internal", payload)
        self.assertIn("speech", payload)
        self.assertIn("actions", payload)

    def test_live_activation_scenario(self):
        scenario = Path("scenarios_live/01_llm_activation_smoke.json")
        adk = load_settings().get("adk", {})
        result = run_scenario_file(
            scenario,
            backend="llm",
            backend_options={
                "model": adk.get("model", "deepseek-v4.1-flash"),
                "api_base": adk.get("api_base", "https://opencode.ai/zen/go/v1"),
                "api_key_env": adk.get("api_key_env", "OPENCODE_API_KEY"),
                "reasoning_effort": adk.get("reasoning_effort", "low"),
                "extra_body": adk.get("extra_body") or {},
            },
        )
        self.assertTrue(result.passed, result.failure_text())


if __name__ == "__main__":
    unittest.main()
