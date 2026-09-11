import json
import unittest
from pathlib import Path

from runtime.tools import build_default_registry


class ToolCatalogTests(unittest.TestCase):
    def test_all_registered_tools_are_mapped(self):
        registry = build_default_registry()
        names = set(registry.names())
        mapping = json.loads(Path("bridge/native_action_map.json").read_text(encoding="utf-8"))
        mapped = set(mapping.get("tool_map", {}))
        self.assertTrue(
            names.issubset(mapped),
            f"tools missing from tool_map: {sorted(names - mapped)}",
        )

    def test_extended_actions_are_agent_selectable(self):
        names = set(build_default_registry().names())
        for required in (
            "think", "react", "hands_up", "cower", "duck", "jump",
            "walk_away", "mount", "dismount", "item_interaction",
            "animal_interaction", "horse_action", "aim_at", "shoot_at",
            "attack", "take_cover",
        ):
            self.assertIn(required, names)

    def test_gesture_schema_has_thinking_styles(self):
        registry = build_default_registry()
        schema = registry.get("gesture").parameters["properties"]["type"]["enum"]
        for style in ("think", "ponder", "scheme"):
            self.assertIn(style, schema)


if __name__ == "__main__":
    unittest.main()
