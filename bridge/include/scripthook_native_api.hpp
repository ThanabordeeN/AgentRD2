#pragma once

#include <cstdint>
#include <string>
#include <unordered_map>
#include <vector>

#include "native_api.hpp"

namespace rdr2ai {

// Adapter for the real ScriptHookRDR2 SDK.  Build with
// -DRDR2AI_HAS_SCRIPTHOOK and the SDK include/library paths configured.
class ScriptHookNativeApi : public INativeApi {
public:
    ScriptHookNativeApi();

    std::uint64_t invoke(std::uint64_t hash, const std::vector<NativeArg>& args) override;
    std::vector<PedSnapshotData> scanNearbyPeds(float radius_m) override;
    std::uint32_t playerPed() override;
    std::uint32_t resolveEntity(const std::string& entity) override;
    bool resolveDestination(const std::string& destination, Vec3& out) override;
    bool getEntityPosition(std::uint32_t entity, Vec3& out) override;
    float getEntityHeading(std::uint32_t entity) override;

private:
    std::unordered_map<std::string, std::uint32_t> known_peds_;
};

}  // namespace rdr2ai
