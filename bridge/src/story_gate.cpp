#include "story_gate.hpp"

namespace rdr2ai {

bool StoryGate::can_activate(const PedSnapshot& ped) const {
    if (!ped.is_ped || !ped.is_human || !ped.is_alive) return false;
    if (ped.is_player || ped.is_story_character || ped.is_mission_owned) return false;
    if (ped.in_scripted_state || ped.in_cutscene || ped.blacklisted) return false;
    return true;
}

bool StoryGate::can_activate(const PedSnapshotData& ped) const {
    if (!ped.is_ped || !ped.is_human || !ped.is_alive) return false;
    if (ped.is_player || ped.is_story_character || ped.is_mission_owned) return false;
    if (ped.in_scripted_state || ped.in_cutscene || ped.blacklisted) return false;
    return true;
}

}  // namespace rdr2ai
