import tempfile
import unittest
from pathlib import Path

from runtime.agent.instructions import build_agent_prompt
from runtime.agent.npc_agent import NpcAgentRuntime
from runtime.config import load_settings
from runtime.lore import WikiContextStore
from runtime.timeline.store import TimelineStore
from runtime.tools import ToolContext, build_default_registry


class WikiContextTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.settings = load_settings()
        self.settings["timelines_dir"] = str(Path(self.tmp.name) / "timelines")
        self.wiki = WikiContextStore(
            self.settings["wiki_context_path"],
            character_path=self.settings["character_context_path"],
        )

    def tearDown(self):
        self.tmp.cleanup()

    def test_wiki_pack_loads_and_looks_up(self):
        topics = self.wiki.available_topics()
        self.assertIn("Valentine", topics)
        self.assertIn("Hunting", topics)
        page = self.wiki.get("Valentine")
        self.assertIsNotNone(page)
        self.assertTrue(page["summary"])
        self.assertIn("fandom.com", page["url"])

    def test_character_pack_loads_and_looks_up(self):
        characters = self.wiki.available_characters()
        self.assertIn("Arthur Morgan", characters)
        self.assertIn("Sadie Adler", characters)
        arthur = self.wiki.character("Arthur Morgan")
        self.assertIsNotNone(arthur)
        self.assertIn("protagonist", arthur["summary"])
        self.assertTrue(arthur["infobox"].get("gender"))

    def test_profile_name_injects_character_context(self):
        profile = {"npc_id": "npc_arthur", "name": "Arthur Morgan"}
        entries = self.wiki.context_for_profile(profile)
        self.assertEqual(entries[0]["title"], "Arthur Morgan")

    def test_profile_context_returns_relevant_topics(self):
        profile = {
            "npc_id": "npc_001",
            "wiki_topics": ["Valentine", "New Hanover"],
        }
        entries = self.wiki.context_for_profile(profile)
        titles = [entry["title"] for entry in entries]
        self.assertIn("Valentine", titles)
        self.assertIn("New Hanover", titles)

    def test_get_world_lore_tool(self):
        registry = build_default_registry()
        context = ToolContext(
            "npc_001",
            world=None,
            timeline=None,
            dispatcher=None,
            wiki=self.wiki,
            profile={"wiki_topics": ["Hunting"]},
        )
        result = registry.call("get_world_lore", context, topic="Valentine")
        self.assertTrue(result["lore"])
        self.assertEqual(result["lore"][0]["title"], "Valentine")
        character = registry.call("get_world_lore", context, topic="Arthur Morgan")
        self.assertEqual(character["lore"][0]["title"], "Arthur Morgan")

    def test_runtime_context_includes_world_lore(self):
        timeline = TimelineStore(self.settings["timelines_dir"])
        runtime = NpcAgentRuntime(settings=self.settings, timeline=timeline)
        context = runtime.build_context("npc_001", reason="test")
        self.assertTrue(context.wiki_context)
        titles = [entry["title"] for entry in context.wiki_context]
        self.assertIn("Valentine", titles)
        prompt = build_agent_prompt(context.to_dict())
        self.assertIn("WORLD LORE CONTEXT", prompt)
        self.assertIn("Valentine", prompt)


if __name__ == "__main__":
    unittest.main()
