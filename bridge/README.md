# RDR2 Living NPC Bridge

The bridge is split into a portable C++17 core and a thin ScriptHookRDR2
adapter.

```text
bridge/
├── include/
│   ├── action_executor.hpp
│   ├── bridge_runtime.hpp
│   ├── bridge_protocol.hpp
│   ├── ipc_client.hpp
│   ├── mini_json.hpp
│   ├── native_api.hpp
│   ├── native_hashes.hpp
│   ├── ped_scanner.hpp
│   └── scripthook_native_api.hpp
├── src/
│   ├── action_executor.cpp
│   ├── bridge_main.cpp
│   ├── bridge_runtime.cpp
│   ├── ipc_client.cpp
│   ├── ped_scanner.cpp
│   ├── scripthook_native_api.cpp
│   └── story_gate.cpp
└── tests/
    ├── bridge_core_test.cpp
    └── mock_native_api.hpp
```

## Responsibilities

- scan entities fast through `worldGetAllPeds` (ScriptHookRDR2 entity pool)
- send `ped_scan` / event JSON over localhost TCP
- receive `action_request` and execute high-level tools
- keep LLM/STT/TTS/timeline work off the game thread
- keep Rockstar AI as default owner
- support dialogue-only overlay for Rockstar quest NPCs
- report action results back to the runtime

## Get the SDK

Download the official SDK locally (the archive itself is not redistributed
with this repository):

```bash
python3 install.py --yes --with-sdk    # or: python3 scripts/install_scripthook_sdk.py
```

Default install:

```text
bridge/.sdk/ScriptHookRDR2_SDK_1.0.1207.73/
├── inc/main.h
├── inc/natives.h
├── inc/nativeCaller.h
├── inc/types.h
└── lib/ScriptHookRDR2.lib
```

The directory is git-ignored.  CMake auto-detects it, or pass
`-DSCRIPTHOOK_RDR2_ROOT=/path/to/sdk`.

## Build

One command (SDK + CMake auto-detected, falls back to a direct `g++` build of
the portable core when CMake is absent):

```bash
python3 install.py --yes --with-bridge    # portable core + core test
python3 install.py --yes --with-asi       # + the in-game .asi
```

Portable core + tests (no SDK needed):

```bash
cmake --preset core -S bridge
cmake --build --preset core
ctest --test-dir bridge/build/core --output-on-failure
```

ASI plugin with the ScriptHookRDR2 SDK:

```bash
cmake --preset asi -S bridge
cmake --build --preset asi
```

The output is named `rdr2_ai_bridge.asi` (the target sets the `.asi` suffix on
Windows, because ScriptHookRDR2 only loads that extension).

CMake auto-detects the local SDK at
`bridge/.sdk/ScriptHookRDR2_SDK_1.0.1207.73`; otherwise pass
`-DSCRIPTHOOK_RDR2_ROOT=...` or set `RDR2AI_SCRIPTHOOK_ROOT`.

Then copy the built `rdr2_ai_bridge.asi` (and the official
`ScriptHookRDR2.dll` runtime from dev-c.com) into the RDR2 game directory.
On Windows `.\install.ps1 -WithAsi -Deploy` does the copying for you.

## IPC

Runtime is the TCP server. Bridge connects to `127.0.0.1:8765` by default.

Bridge -> Runtime:

```json
{"type":"hello","bridge_version":"0.2"}
{"type":"ped_scan","peds":[...]}
{"type":"action_result","npc_id":"ped_42","tool":"go_to","request_id":"act_...","status":"completed"}
```

Runtime -> Bridge:

```json
{"type":"action_request","npc_id":"ped_42","request":{"tool":"think","request_id":"act_...","arguments":{"style":"listen","duration":2.5}}}
```

## Action execution

`ActionExecutor` maps high-level tools to native hashes from
`include/native_hashes.hpp` and validates arguments through `mini_json`.
It never accepts raw native hashes from the LLM.

Implemented tool families:

- movement: `wander`, `go_to`, `follow`, `stop`, `wait`
- attention: `look_at`, `face`, `clear_attention`
- speech: `say` (`PLAY_PED_AMBIENT_SPEECH_NATIVE`)
- gesture/think: `gesture`, `think` (`TASK_PLAY_ANIM`, emote/scenario candidates)
- reaction: `investigate`, `flee_from`, `walk_away`, `react`,
  `hands_up`, `cower`, `duck`, `jump`
- interaction: `mount`, `dismount`, `item_interaction`,
  `animal_interaction`, `horse_action`
- deferred combat: `aim_at`, `shoot_at`, `attack`, `take_cover`

Full native/hash/parameter mappings are in
`../docs/rdr2_asi_actions.md` and `native_action_map.json`.

## PTT

Keyboard handler watches `V`:

```text
V down -> {"type":"push_to_talk","action":"start"}
V up   -> {"type":"push_to_talk","action":"stop"}
```

The runtime selects the conversation target.

## Waiting / thinking gestures

While the LLM/TTS is generating, the runtime sends a transient `think`
action. `ActionExecutor` maps the style to an animation dict/clip (for
example `ai_gestures@gen_male@standing@silent` /
`aknwoledge_tough_chin_scratch_l_001` for `think`).

## Quest dialogue-only overlay

When a ped scan includes:

```json
{"metadata":{"quest_dialogue":true,"quest_id":"quest_valentine_livestock"}}
```

the runtime enters dialogue-only mode. The bridge still owns all quest
actions through Rockstar; only `say` requests are sent to it.

## Known TODOs

- named destination resolution (`Valentine Saloon` -> coordinates)
- mission/script/cutscene flag providers for `PedSnapshotData`
- camera alignment for PTT target scoring
- full action completion detection (currently an estimated timer)
- real TTS audio playback hook
