// Minimal Linux/POSIX integration client for the portable bridge core.
// It connects to a local TCP server, sends hello and eventually processes an
// action_request, then sends action_result back.
#include <chrono>
#include <cstdlib>
#include <iostream>
#include <thread>

#include "bridge_runtime.hpp"
#include "mock_native_api.hpp"

int main(int argc, char** argv) {
    if (argc < 2) {
        std::cerr << "usage: ipc_e2e_client <port>\n";
        return 2;
    }
    rdr2ai::test::MockNativeApi api;
    api.destinations["Valentine Saloon"] = {100.0f, 200.0f, 10.0f};

    rdr2ai::BridgeRuntime bridge(&api, "127.0.0.1", static_cast<unsigned short>(std::stoi(argv[1])), 100);
    if (!bridge.start()) {
        std::cerr << "failed to start bridge runtime\n";
        return 3;
    }
    auto end = std::chrono::steady_clock::now() + std::chrono::seconds(3);
    while (std::chrono::steady_clock::now() < end) {
        bridge.tick();
        std::this_thread::sleep_for(std::chrono::milliseconds(50));
    }
    bridge.stop();
    std::cout << "ipc_e2e_client done\n";
    return 0;
}
