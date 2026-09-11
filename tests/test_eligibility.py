import unittest

from runtime.schemas import PedSnapshot
from runtime.state.eligibility import EligibilityGate, StoryBlacklist


def safe_ped(**overrides):
    payload = {
        "entity_id": "npc_1",
        "model": "a_m_m_farmer_01",
        "name": "Elias Carter",
        "is_ped": True,
        "is_human": True,
        "is_alive": True,
        "is_player": False,
        "is_story_character": False,
        "is_mission_owned": False,
        "in_scripted_state": False,
        "in_cutscene": False,
        "blacklisted": False,
        "distance_m": 4.0,
        "visible": True,
    }
    payload.update(overrides)
    return PedSnapshot.from_dict(payload)


class EligibilityGateTests(unittest.TestCase):
    def test_safe_ped_is_allowed(self):
        gate = EligibilityGate()
        self.assertTrue(gate.can_activate(safe_ped()).allowed)

    def test_unknown_safety_field_blocks(self):
        gate = EligibilityGate()
        ped = safe_ped()
        ped.is_mission_owned = None
        result = gate.can_activate(ped)
        self.assertFalse(result.allowed)
        self.assertTrue(any("mission_owned" in reason for reason in result.reasons))

    def test_story_blacklist_blocks_named_character(self):
        blacklist = StoryBlacklist({
            "named_story_characters": {"models": [], "names": ["Dutch van der Linde"]}
        })
        gate = EligibilityGate(blacklist)
        result = gate.can_activate(safe_ped(name="Dutch van der Linde"))
        self.assertFalse(result.allowed)
        self.assertTrue(any("blacklist" in reason for reason in result.reasons))

    def test_story_blacklist_blocks_wildcard_model(self):
        blacklist = StoryBlacklist({
            "mission_only_models": {"models": ["mp_*"], "names": []}
        })
        gate = EligibilityGate(blacklist)
        result = gate.can_activate(safe_ped(model="mp_fake_model"))
        self.assertFalse(result.allowed)

    def test_mission_and_cutscene_block(self):
        gate = EligibilityGate()
        self.assertFalse(gate.can_activate(safe_ped(is_mission_owned=True)).allowed)
        self.assertFalse(gate.can_activate(safe_ped(in_cutscene=True)).allowed)
        self.assertFalse(gate.can_activate(safe_ped(in_scripted_state=True)).allowed)


if __name__ == "__main__":
    unittest.main()
