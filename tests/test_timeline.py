import tempfile
import unittest
from pathlib import Path

from runtime.timeline.store import TimelineStore


class TimelineStoreTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.store = TimelineStore(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_append_assigns_linear_sequence(self):
        e1 = self.store.append_event("npc_1", "NPC_ACTIVATED", tags=["npc"])
        e2 = self.store.append_event("npc_1", "PLAYER_APPROACHED", tags=["player"])
        e3 = self.store.append_event("npc_2", "NPC_ACTIVATED", tags=["npc"])
        self.assertEqual([e1.seq, e2.seq, e3.seq], [1, 2, 1])
        self.assertEqual([e.seq for e in self.store.all_events("npc_1")], [1, 2])

    def test_grab_timeline_filters(self):
        self.store.append_event("npc_1", "PLAYER_THREATENED_NPC", tags=["player", "threat"], importance=0.9, entities=["player"])
        self.store.append_event("npc_1", "PLAYER_HELPED_NPC", tags=["player", "help"], importance=0.8, entities=["player"])
        self.store.append_event("npc_1", "GUNSHOT_HEARD", tags=["world"], importance=0.6)

        by_name = self.store.grab_timeline("npc_1", event_name="PLAYER_THREATENED_NPC")
        self.assertEqual(len(by_name), 1)

        player_events = self.store.grab_timeline("npc_1", tags=["player"])
        self.assertEqual([e.event_name for e in player_events], ["PLAYER_THREATENED_NPC", "PLAYER_HELPED_NPC"])

        important = self.store.grab_timeline("npc_1", minimum_importance=0.75)
        self.assertEqual([e.event_name for e in important], ["PLAYER_THREATENED_NPC", "PLAYER_HELPED_NPC"])

        entity_events = self.store.grab_timeline("npc_1", entity="player")
        self.assertEqual(len(entity_events), 2)

        later = self.store.grab_timeline("npc_1", since_seq=1)
        self.assertEqual([e.seq for e in later], [2, 3])

        before = self.store.grab_timeline("npc_1", before_seq=3)
        self.assertEqual([e.seq for e in before], [1, 2])

        limited = self.store.grab_timeline("npc_1", limit=1)
        self.assertEqual([e.seq for e in limited], [3])

    def test_recent_events_returns_tail(self):
        for i in range(5):
            self.store.append_event("npc_1", "EVENT", data={"i": i})
        recent = self.store.recent_events("npc_1", limit=2)
        self.assertEqual([e.data["i"] for e in recent], [3, 4])


if __name__ == "__main__":
    unittest.main()
