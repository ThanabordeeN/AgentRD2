import unittest

from runtime.state.ownership import OwnershipManager, OwnershipState


class OwnershipManagerTests(unittest.TestCase):
    def test_full_flow(self):
        mgr = OwnershipManager()
        t1 = mgr.update_from_scan("npc_1", distance_m=15.0, eligible=True)
        self.assertEqual(t1.to_state, OwnershipState.CANDIDATE)
        t2 = mgr.update_from_scan("npc_1", distance_m=8.0, eligible=True)
        self.assertEqual(t2.to_state, OwnershipState.AWARE)
        t3 = mgr.update_from_scan("npc_1", distance_m=8.0, eligible=True, meaningful_trigger=True)
        self.assertEqual(t3.to_state, OwnershipState.AI_ACTIVE)
        t4 = mgr.push_to_talk("npc_1")
        self.assertEqual(t4.to_state, OwnershipState.AI_CONVERSATION)
        t5 = mgr.conversation_ended("npc_1")
        self.assertEqual(t5.to_state, OwnershipState.AI_ACTIVE)
        t6 = mgr.update_from_scan("npc_1", distance_m=50.0, eligible=True)
        self.assertEqual(t6.to_state, OwnershipState.RELEASE)
        t7 = mgr.finalize_release("npc_1")
        self.assertEqual(t7.to_state, OwnershipState.ROCKSTAR)

    def test_eligibility_failure_releases_active_agent(self):
        mgr = OwnershipManager()
        mgr.update_from_scan("npc_1", distance_m=15.0, eligible=True)
        mgr.update_from_scan("npc_1", distance_m=8.0, eligible=True)
        mgr.update_from_scan("npc_1", distance_m=8.0, eligible=True, meaningful_trigger=True)
        transition = mgr.update_from_scan("npc_1", distance_m=8.0, eligible=False)
        self.assertEqual(transition.to_state, OwnershipState.RELEASE)
        self.assertEqual(transition.reason, "eligibility_failed")

    def test_suspend_and_resume(self):
        mgr = OwnershipManager()
        mgr.update_from_scan("npc_1", distance_m=8.0, eligible=True)
        mgr.update_from_scan("npc_1", distance_m=8.0, eligible=True)
        mgr.update_from_scan("npc_1", distance_m=8.0, eligible=True, meaningful_trigger=True)
        suspended = mgr.suspend("npc_1", reason="mission")
        self.assertEqual(suspended.to_state, OwnershipState.SUSPENDED)
        resumed = mgr.resume("npc_1")
        self.assertEqual(resumed.to_state, OwnershipState.AI_ACTIVE)


if __name__ == "__main__":
    unittest.main()
