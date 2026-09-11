#pragma once

#include <string>
#include <vector>
#include <optional>

namespace rdr2ai {

struct Vec3 {
    float x = 0.0f;
    float y = 0.0f;
    float z = 0.0f;
};

struct PedSnapshot {
    std::string entity_id;
    std::string model;
    std::string name;
    bool is_ped = false;
    bool is_human = false;
    bool is_alive = false;
    bool is_player = false;
    bool is_story_character = false;
    bool is_mission_owned = false;
    bool in_scripted_state = false;
    bool in_cutscene = false;
    bool blacklisted = false;
    float distance_m = 0.0f;
    bool visible = false;
    float health = 0.0f;
    float camera_alignment = 0.0f;
    Vec3 position;
};

struct ActionRequest {
    std::string npc_id;
    std::string tool;
    std::string request_id;
    std::string arguments_json;
};

struct ActionResult {
    std::string npc_id;
    std::string tool;
    std::string request_id;
    std::string status;  // started | completed | failed
    std::string reason;
};

}  // namespace rdr2ai
