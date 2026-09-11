#pragma once

#include <cstdint>
#include <cstring>
#include <string>
#include <vector>

#include "bridge_protocol.hpp"

namespace rdr2ai {

struct PedSnapshotData {
    std::uint32_t handle = 0;
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
    // Optional quest dialogue overlay metadata.
    bool quest_dialogue = false;
    std::string quest_id;
};

struct NativeArg {
    enum class Type { I32, U32, F32, U64, PTR };
    Type type = Type::U64;
    std::int32_t i32 = 0;
    std::uint32_t u32 = 0;
    float f32 = 0.0f;
    std::uint64_t u64 = 0;
    void* ptr = nullptr;

    static NativeArg i(int value) { return {Type::I32, value, 0, 0.0f, 0, nullptr}; }
    static NativeArg u(std::uint32_t value) { return {Type::U32, 0, value, 0.0f, 0, nullptr}; }
    static NativeArg f(float value) { return {Type::F32, 0, 0, value, 0, nullptr}; }
    static NativeArg raw(std::uint64_t value) { return {Type::U64, 0, 0, 0.0f, value, nullptr}; }
    static NativeArg pointer(void* value) { return {Type::PTR, 0, 0, 0.0f, 0, value}; }

    std::uint64_t payload() const {
        switch (type) {
        case Type::I32: return static_cast<std::uint64_t>(static_cast<std::int64_t>(i32));
        case Type::U32: return static_cast<std::uint64_t>(u32);
        case Type::F32: {
            std::uint32_t bits = 0;
            static_assert(sizeof(bits) == sizeof(f32), "float size mismatch");
            std::memcpy(&bits, &f32, sizeof(bits));
            return static_cast<std::uint64_t>(bits);
        }
        case Type::U64: return u64;
        case Type::PTR: return reinterpret_cast<std::uint64_t>(ptr);
        }
        return 0;
    }
};

class INativeApi {
public:
    virtual ~INativeApi() = default;

    // Invoke a raw RDR2 native by hash.  Arguments are pushed in order.
    virtual std::uint64_t invoke(std::uint64_t hash, const std::vector<NativeArg>& args) = 0;

    // Snapshot nearby peds for the IPC ped_scan message.
    virtual std::vector<PedSnapshotData> scanNearbyPeds(float radius_m) = 0;

    virtual std::uint32_t playerPed() = 0;
    virtual std::uint32_t resolveEntity(const std::string& entity) = 0;
    virtual bool resolveDestination(const std::string& destination, Vec3& out) = 0;
    virtual bool getEntityPosition(std::uint32_t entity, Vec3& out) = 0;
    virtual float getEntityHeading(std::uint32_t entity) = 0;
};

}  // namespace rdr2ai
