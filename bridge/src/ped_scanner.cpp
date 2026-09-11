#include "ped_scanner.hpp"

#include "mini_json.hpp"

namespace rdr2ai {

PedScanner::PedScanner(INativeApi* native_api, float scan_radius_m)
    : api_(native_api), scan_radius_m_(scan_radius_m) {}

std::vector<PedSnapshotData> PedScanner::scan_nearby_peds() const {
    if (api_ == nullptr) return {};
    return api_->scanNearbyPeds(scan_radius_m_);
}

std::string PedScanner::to_scan_json(const std::vector<PedSnapshotData>& peds) const {
    Json message = Json::object();
    message["type"] = Json("ped_scan");
    Json list = Json::array();
    for (const auto& ped : peds) {
        Json item = Json::object();
        item["entity_id"] = Json(ped.entity_id);
        item["handle"] = Json(static_cast<int>(ped.handle));
        item["model"] = Json(ped.model);
        item["name"] = Json(ped.name);
        item["is_ped"] = Json(ped.is_ped);
        item["is_human"] = Json(ped.is_human);
        item["is_alive"] = Json(ped.is_alive);
        item["is_player"] = Json(ped.is_player);
        item["is_story_character"] = Json(ped.is_story_character);
        item["is_mission_owned"] = Json(ped.is_mission_owned);
        item["in_scripted_state"] = Json(ped.in_scripted_state);
        item["in_cutscene"] = Json(ped.in_cutscene);
        item["blacklisted"] = Json(ped.blacklisted);
        item["distance_m"] = Json(static_cast<double>(ped.distance_m));
        item["visible"] = Json(ped.visible);
        item["health"] = Json(static_cast<double>(ped.health));
        Json position = Json::array();
        position.push_back(Json(static_cast<double>(ped.position.x)));
        position.push_back(Json(static_cast<double>(ped.position.y)));
        position.push_back(Json(static_cast<double>(ped.position.z)));
        item["position"] = position;
        Json metadata = Json::object();
        metadata["camera_alignment"] = Json(static_cast<double>(ped.camera_alignment));
        if (ped.quest_dialogue) {
            metadata["quest_dialogue"] = Json(true);
            metadata["quest_id"] = Json(ped.quest_id);
        }
        item["metadata"] = metadata;
        list.push_back(item);
    }
    message["peds"] = list;
    return message.dump();
}

}  // namespace rdr2ai
