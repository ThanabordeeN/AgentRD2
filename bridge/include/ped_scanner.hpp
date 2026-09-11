#pragma once

#include <string>
#include <vector>

#include "native_api.hpp"

namespace rdr2ai {

// Reads nearby peds through INativeApi and serializes them as the runtime's
// ped_scan JSON message.
class PedScanner {
public:
    explicit PedScanner(INativeApi* native_api, float scan_radius_m = 20.0f);

    std::vector<PedSnapshotData> scan_nearby_peds() const;
    std::string to_scan_json(const std::vector<PedSnapshotData>& peds) const;

private:
    INativeApi* api_ = nullptr;
    float scan_radius_m_ = 20.0f;
};

}  // namespace rdr2ai
