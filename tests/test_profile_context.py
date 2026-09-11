import unittest

from runtime.agent.instructions import build_agent_prompt
from runtime.config import ProfileStore


class ProfileContextTests(unittest.TestCase):
    def test_npc_profile_contains_system_prompt_context(self):
        profile = ProfileStore().get("npc_001")
        self.assertEqual(profile["name"], "Elias Carter")
        self.assertIn("tenant farmer", profile["background"])
        self.assertIn("Elias Carter", profile["system_prompt"])
        self.assertTrue(profile["speech_style"])
        self.assertTrue(profile["behavior_rules"])
        self.assertTrue(profile["goals"])

    def test_agent_prompt_includes_npc_system_prompt(self):
        profile = ProfileStore().get("npc_001")
        prompt = build_agent_prompt(
            {
                "profile": profile,
                "world_state": {},
                "recent_events": [],
                "available_tools": ["say", "look_at"],
                "current_goal": None,
                "current_mood": "neutral",
            }
        )
        self.assertIn("NPC SYSTEM PROMPT", prompt)
        self.assertIn("Elias Carter", prompt)
        self.assertIn("tenant farmer", prompt)
        self.assertIn("ACTION ARGUMENT RULES", prompt)


if __name__ == "__main__":
    unittest.main()
