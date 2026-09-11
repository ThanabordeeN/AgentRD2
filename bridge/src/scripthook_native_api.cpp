#include "scripthook_native_api.hpp"

#ifdef RDR2AI_HAS_SCRIPTHOOK

#include <cmath>
#include <cstdio>
#include <sstream>

// ScriptHookRDR2 SDK headers.
#include <Main.h>
#include <natives.h>

#include "native_hashes.hpp"

namespace rdr2ai {
namespace {

std::string hex_hash(std::uint32_t value) {
    char buffer[16];
    std::snprintf(buffer, sizeof(buffer), "0x%08X", value);
    return buffer;
}

}  // namespace

ScriptHookNativeApi::ScriptHookNativeApi() = default;

std::uint64_t ScriptHookNativeApi::invoke(std::uint64_t hash, const std::vector<NativeArg>& args) {
    nativeInit(hash);
    for (const NativeArg& arg : args) {
        nativePush64(arg.payload());
    }
    PUINT64 result = nativeCall();
    return result != nullptr ? *result : 0;
}

std::uint32_t ScriptHookNativeApi::playerPed() {
    return static_cast<std::uint32_t>(PLAYER::PLAYER_PED_ID());
}

std::uint32_t ScriptHookNativeApi::resolveEntity(const std::string& entity) {
    if (entity == "player" || entity == "self") return playerPed();
    auto it = known_peds_.find(entity);
    if (it != known_peds_.end()) return it->second;
    if (entity.rfind("ped_", 0) == 0) {
        try {
            return static_cast<std::uint32_t>(std::stoul(entity.substr(4), nullptr, 0));
        } catch (...) {
        }
    }
    try {
        return static_cast<std::uint32_t>(std::stoul(entity, nullptr, 0));
    } catch (...) {
    }
    return 0;
}

bool ScriptHookNativeApi::resolveDestination(const std::string& destination, Vec3& out) {
    // Named destination resolution belongs in the bridge's data layer.  A
    // production build can load a generated destinations table from
    // RDR2 navigation/waypoint data.  For now only positional/entity targets
    // are accepted by ActionExecutor.
    (void)destination;
    (void)out;
    return false;
}

bool ScriptHookNativeApi::getEntityPosition(std::uint32_t entity, Vec3& out) {
    if (entity == 0) return false;
    auto coords = ENTITY::GET_ENTITY_COORDS(entity, true, false);
    out.x = coords.x;
    out.y = coords.y;
    out.z = coords.z;
    return true;
}

float ScriptHookNativeApi::getEntityHeading(std::uint32_t entity) {
    if (entity == 0) return 0.0f;
    return ENTITY::GET_ENTITY_HEADING(entity);
}

std::vector<PedSnapshotData> ScriptHookNativeApi::scanNearbyPeds(float radius_m) {
    std::vector<PedSnapshotData> result;
    std::uint32_t player = playerPed();
    if (player == 0) return result;

    // ScriptHookRDR2 exposes entity-pool access directly; it is faster and
    // more reliable than the nearby-peds wrapper for a periodic scan.
    constexpr int kMaxPeds = 2048;
    std::vector<int> handles(kMaxPeds, 0);
    int count = worldGetAllPeds(handles.data(), kMaxPeds);
    if (count <= 0) return result;

    Vec3 player_pos;
    if (!getEntityPosition(player, player_pos)) return result;

    for (int i = 0; i < count; ++i) {
        int handle = handles[static_cast<std::size_t>(i)];
        if (handle == 0) continue;

        PedSnapshotData ped;
        ped.handle = static_cast<std::uint32_t>(handle);
        ped.entity_id = "ped_" + std::to_string(handle);
        known_peds_[ped.entity_id] = ped.handle;
        ped.is_ped = true;
        ped.is_player = PED::IS_PED_A_PLAYER(handle) != 0;
        ped.is_human = PED::IS_PED_HUMAN(handle) != 0;
        ped.is_alive = PED::IS_PED_DEAD_OR_DYING(handle, TRUE) == FALSE &&
                       ENTITY::IS_ENTITY_DEAD(handle) == FALSE;
        ped.is_story_character = false;   // populated by a future story metadata provider
        ped.is_mission_owned = false;     // populated by a future mission state provider
        ped.in_scripted_state = false;    // populated by a future script state provider
        ped.in_cutscene = false;          // populated by a future cutscene state provider
        ped.blacklisted = false;

        std::uint32_t model_hash = ENTITY::GET_ENTITY_MODEL(handle);
        ped.model = hex_hash(model_hash);
        ped.name = ped.model;
        ped.health = static_cast<float>(ENTITY::GET_ENTITY_HEALTH(handle));
        ped.visible = ENTITY::IS_ENTITY_VISIBLE(handle) != 0;

        Vec3 pos;
        if (getEntityPosition(handle, pos)) {
            ped.position = pos;
            float dx = pos.x - player_pos.x;
            float dy = pos.y - player_pos.y;
            float dz = pos.z - player_pos.z;
            ped.distance_m = std::sqrt(dx * dx + dy * dy + dz * dz);
        }

        if (ped.distance_m <= radius_m && !ped.is_player && ped.is_alive) {
            result.push_back(std::move(ped));
        }
    }
    return result;
}

}  // namespace rdr2ai

#else

namespace rdr2ai {

ScriptHookNativeApi::ScriptHookNativeApi() = default;
std::uint64_t ScriptHookNativeApi::invoke(std::uint64_t, const std::vector<NativeArg>&) { return 0; }
std::vector<PedSnapshotData> ScriptHookNativeApi::scanNearbyPeds(float) { return {}; }
std::uint32_t ScriptHookNativeApi::playerPed() { return 0; }
std::uint32_t ScriptHookNativeApi::resolveEntity(const std::string&) { return 0; }
bool ScriptHookNativeApi::resolveDestination(const std::string&, Vec3&) { return false; }
bool ScriptHookNativeApi::getEntityPosition(std::uint32_t, Vec3&) { return false; }
float ScriptHookNativeApi::getEntityHeading(std::uint32_t) { return 0.0f; }

}  // namespace rdr2ai

#endif
