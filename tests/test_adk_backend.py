import os
import unittest
from unittest.mock import patch

from runtime.agent.backends import GoogleADKBackend
from runtime.config import load_settings


class GoogleADKOpenAICompatibilityTests(unittest.TestCase):
    def test_explicit_openai_compatible_arguments(self):
        backend = GoogleADKBackend(
            model="local-model",
            provider="openai",
            api_base="http://127.0.0.1:8000/v1",
            api_key="dummy-key",
            reasoning_effort="low",
            extra_body={"thinking": {"type": "enabled"}},
        )
        self.assertEqual(backend.litellm_model_name(), "openai/local-model")
        kwargs = backend._litellm_kwargs()
        self.assertEqual(kwargs["model"], "openai/local-model")
        self.assertEqual(kwargs["api_base"], "http://127.0.0.1:8000/v1")
        self.assertEqual(kwargs["api_key"], "dummy-key")
        self.assertEqual(kwargs["reasoning_effort"], "low")
        self.assertEqual(kwargs["extra_body"], {"thinking": {"type": "enabled"}})

    def test_default_settings_target_opencode_go_deepseek_low(self):
        settings = load_settings()
        adk = settings["adk"]
        self.assertEqual(adk["provider"], "openai")
        self.assertEqual(adk["model"], "deepseek-v4.1-flash")
        self.assertEqual(adk["api_base"], "https://opencode.ai/zen/go/v1")
        self.assertEqual(adk["api_key_env"], "OPENCODE_API_KEY")
        self.assertEqual(adk["reasoning_effort"], "low")
        self.assertEqual(adk["extra_body"], {"thinking": {"type": "enabled"}})

    def test_already_qualified_model_name_is_kept(self):
        backend = GoogleADKBackend(
            model="openai/gpt-4o-mini",
            api_base="https://api.openai.com/v1",
            api_key="sk-test",
        )
        self.assertEqual(backend.litellm_model_name(), "openai/gpt-4o-mini")

    def test_environment_fallback(self):
        with patch.dict(
            os.environ,
            {
                "MY_OPENAI_KEY": "env-key",
                "OPENAI_BASE_URL": "http://localhost:1234/v1",
            },
            clear=True,
        ):
            backend = GoogleADKBackend(
                model="env-model",
                api_key_env="MY_OPENAI_KEY",
            )
        self.assertEqual(backend.api_key, "env-key")
        self.assertEqual(backend.api_base, "http://localhost:1234/v1")
        self.assertEqual(backend.litellm_model_name(), "openai/env-model")

    def test_local_endpoint_can_use_dummy_key(self):
        with patch.dict(os.environ, {}, clear=True):
            backend = GoogleADKBackend(
                model="local-model",
                api_base="http://127.0.0.1:11434/v1",
                api_key_env="DOES_NOT_EXIST",
            )
        self.assertEqual(backend.api_key, "not-needed")
        self.assertTrue(backend._is_local_url("http://localhost:8000/v1"))
        self.assertFalse(backend._is_local_url("https://api.openai.com/v1"))


if __name__ == "__main__":
    unittest.main()
