#pragma once

#include <algorithm>
#include <cstdint>
#include <string>
#include <unordered_map>
#include <utility>
#include <vector>

#include "native_api.hpp"

namespace rdr2ai::test {

struct RecordedCall {
    std::uint64_t hash = 0;
    std::vector<NativeArg> args;
};

class MockNativeApi : public INativeApi {
public:
    std::vector<RecordedCall> calls;
    std::vector<PedSnapshotData> peds;
    std::unordered_map<std::string, Vec3> destinations;

    std::uint64_t invoke(std::uint64_t hash, const std::vector<NativeArg>& args) override {
        calls.push_back({hash, args});
        return 0;
    }

    std::vector<PedSnapshotData> scanNearbyPeds(float) override { return peds; }

    std::uint32_t playerPed() override { return 1; }

    std::uint32_t resolveEntity(const std::string& entity) override {
        if (entity == "player" || entity == "self") return 1;
        if (entity == "horse") return 2;
        if (entity == "target") return 3;
        return 0;
    }

    bool resolveDestination(const std::string& destination, Vec3& out) override {
        auto it = destinations.find(destination);
        if (it == destinations.end()) return false;
        out = it->second;
        return true;
    }

    bool getEntityPosition(std::uint32_t entity, Vec3& out) override {
        if (entity == 0) return false;
        out = {10.0f, 20.0f, 30.0f};
        return true;
    }

    float getEntityHeading(std::uint32_t) override { return 90.0f; }

    bool has_hash(std::uint64_t hash) const {
        return std::any_of(calls.begin(), calls.end(), [hash](const RecordedCall& call) {
            return call.hash == hash;
        });
    }
};

}  // namespace rdr2ai::test
