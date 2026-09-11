import unittest

from runtime.events.normalizer import EventNormalizer
from runtime.schemas import PedSnapshot, WorldState


class NormalizerTests(unittest.TestCase):
    def setUp(self):
        self.normalizer = EventNormalizer()

    def test_world_state_from_message(self):
        state = self.normalizer.world_state_from_message({
            "type": "world_update",
            "npc_id": "npc_1",
            "timestamp": 123.0,
            "state": {
                "self": {"health": 82},
                "player": {"distance": 4.1},
                "nearby_peds": [],
            },
        })
        self.assertIsInstance(state, WorldState)
        self.assertEqual(state.self_state["health"], 82)
        self.assertEqual(state.player["distance"], 4.1)

    def test_event_is_fact_shaped_and_canonical(self):
        event = self.normalizer.event_from_message(
            {
                "type": "game_event",
                "npc_id": "npc_1",
                "event_name": "player_threatened_npc",
                "entities": "player",
                "tags": "threat",
                "data": {"weapon": "revolver", "distance": 2.6},
                "importance": 1.5,
            },
            seq=7,
        )
        self.assertEqual(event.seq, 7)
        self.assertEqual(event.event_name, "PLAYER_THREATENED_NPC")
        self.assertEqual(event.entities, ["player"])
        self.assertEqual(event.tags, ["threat"])
        self.assertEqual(event.importance, 1.0)
        self.assertEqual(event.data["weapon"], "revolver")

    def test_invalid_event_name_is_rejected(self):
        with self.assertRaises(ValueError):
            self.normalizer.event_from_message(
                {"npc_id": "npc_1", "event_name": "player is evil", "data": {}},
                seq=1,
            )

    def test_action_result_maps_status(self):
        raw = self.normalizer.action_result({
            "npc_id": "npc_1",
            "status": "failed",
            "tool": "go_to",
            "request_id": "act_1",
            "reason": "path_unreachable",
        })
        self.assertEqual(raw["event_name"], "ACTION_FAILED")
        self.assertEqual(raw["data"]["reason"], "path_unreachable")

    def test_ped_snapshot_from_dict(self):
        ped = PedSnapshot.from_dict({
            "entity_id": "npc_1",
            "is_ped": True,
            "is_human": True,
            "is_alive": True,
            "is_player": False,
            "is_story_character": False,
            "is_mission_owned": False,
            "in_scripted_state": False,
            "in_cutscene": False,
            "blacklisted": False,
        })
        self.assertTrue(ped.is_ped)
        self.assertIsNone(ped.distance_m)


if __name__ == "__main__":
    unittest.main()
