#pragma once

#include <chrono>
#include <string>
#include <vector>

#include "action_executor.hpp"
#include "ipc_client.hpp"
#include "native_api.hpp"
#include "ped_scanner.hpp"
#include "story_gate.hpp"

namespace rdr2ai {

// Portable bridge core: owns IPC, scanner, story gate, action executor and
// PTT hooks.  The game-specific ASI entry point only needs to call tick()
// and forward keyboard events.
class BridgeRuntime {
public:
    BridgeRuntime(INativeApi* native_api, std::string host, unsigned short port,
                  int scan_interval_ms = 200);
    ~BridgeRuntime();

    bool start();
    void stop();
    void tick();
    void onPushToTalk(bool down);

private:
    void onIpcMessage(const std::string& line);
    void queueCompletion(const ActionRequest& request, ActionResult result);
    void sendActionFailures();

    INativeApi* api_ = nullptr;
    PedScanner scanner_;
    StoryGate story_gate_;
    ActionExecutor action_executor_;
    IpcClient ipc_;
    int scan_interval_ms_ = 200;
    std::chrono::steady_clock::time_point last_scan_;

    struct PendingCompletion {
        ActionResult result;
        std::chrono::steady_clock::time_point due;
    };
    std::vector<PendingCompletion> pending_;
};

}  // namespace rdr2ai
