#pragma once

#include "bridge_protocol.hpp"
#include "native_api.hpp"

namespace rdr2ai {

// Conservative story/mission safety gate.  If any signal is uncertain it
// returns false.  This is intentionally independent of Python.
class StoryGate {
public:
    bool can_activate(const PedSnapshot& ped) const;
    bool can_activate(const PedSnapshotData& ped) const;
};

}  // namespace rdr2ai
