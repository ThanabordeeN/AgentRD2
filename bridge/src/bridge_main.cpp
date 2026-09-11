// RDR2 Living NPC ASI plugin entry point.
//
// Build with the ScriptHookRDR2 SDK and -DRDR2AI_HAS_SCRIPTHOOK.  The bridge
// core (IPC, scanner, action executor) is SDK-independent and is covered by
// bridge/tests/bridge_core_test.cpp.

#ifdef RDR2AI_HAS_SCRIPTHOOK

#include <Windows.h>
#include <memory>

// ScriptHookRDR2 SDK.
#include <Main.h>

#include "bridge_runtime.hpp"
#include "scripthook_native_api.hpp"

namespace {

constexpr char kBridgeHost[] = "127.0.0.1";
constexpr unsigned short kBridgePort = 8765;

ScriptHookNativeApi g_native_api;
std::unique_ptr<rdr2ai::BridgeRuntime> g_bridge;
bool g_push_to_talk_down = false;

void ScriptKeyboardMessage(DWORD key, WORD repeats, BYTE scanCode, BOOL isExtended,
                           BOOL isWithAlt, BOOL wasDownBefore, BOOL isUpNow) {
    (void)repeats;
    (void)scanCode;
    (void)isExtended;
    (void)isWithAlt;
    (void)wasDownBefore;
    if (key != 'V') return;
    bool down = isUpNow == FALSE;
    if (down == g_push_to_talk_down) return;
    g_push_to_talk_down = down;
    if (g_bridge) {
        g_bridge->onPushToTalk(down);
    }
}

}  // namespace

BOOL WINAPI DllMain(HMODULE hModule, DWORD reason, LPVOID reserved) {
    (void)reserved;
    switch (reason) {
    case DLL_PROCESS_ATTACH:
        DisableThreadLibraryCalls(hModule);
        scriptRegister(hModule, [] {
            // ScriptMain runs on the game fiber.  We keep it alive and tick
            // the bridge from this thread because BridgeRuntime only does
            // non-blocking queue/IPC work.
            g_bridge = std::make_unique<rdr2ai::BridgeRuntime>(&g_native_api, kBridgeHost, kBridgePort, 200);
            if (g_bridge->start()) {
                while (true) {
                    g_bridge->tick();
                    scriptWait(0);
                }
            }
        });
        keyboardHandlerRegister(ScriptKeyboardMessage);
        break;
    case DLL_PROCESS_DETACH:
        if (g_bridge) {
            g_bridge->stop();
            g_bridge.reset();
        }
        keyboardHandlerUnregister(ScriptKeyboardMessage);
        scriptUnregister(hModule);
        break;
    default:
        break;
    }
    return TRUE;
}

#else

#include <iostream>

int main() {
    std::cout << "rdr2_ai_bridge portable stub. Build with -DRDR2AI_HAS_SCRIPTHOOK and the "
                 "ScriptHookRDR2 SDK for the in-game ASI.\n";
    return 0;
}

#endif
