#pragma once

#include <string>

#include "bridge_protocol.hpp"
#include "native_api.hpp"

namespace rdr2ai {

// Converts high-level tool names such as go_to/wander/flee_from into validated
// RDR2 native calls.  Raw native hashes are never accepted from the agent.
class ActionExecutor {
public:
    explicit ActionExecutor(INativeApi* native_api);

    bool execute(const ActionRequest& request, ActionResult& out_result);
    void on_tick();

private:
    INativeApi* api_ = nullptr;
};

}  // namespace rdr2ai
