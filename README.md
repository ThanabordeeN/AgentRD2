# RDR2 Living NPC Agent

[![build](https://github.com/ThanabordeeN/AgentRD2/actions/workflows/build.yml/badge.svg)](https://github.com/ThanabordeeN/AgentRD2/actions/workflows/build.yml)
[![license: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![python 3.10+](https://img.shields.io/badge/python-3.10%2B-blue.svg)](pyproject.toml)
[![dependencies: none](https://img.shields.io/badge/dependencies-none-brightgreen.svg)](requirements.txt)

Implementation scaffold for **RDR2 Living NPC Agent — Technical Product
Specification v0.2**.

Rockstar AI remains the default owner of every NPC.  The runtime temporarily
takes over eligible ambient NPCs, records facts as timeline events, and uses a
configurable agent backend to choose high-level actions.

## Implemented MVP pieces

| Spec area | Status |
|---|---|
| JSONL event timeline + filters (`grab_timeline`) | yes |
| Canonical event schema and fact-before-interpretation normalizer | yes |
| NPC ownership state machine | yes |
| Conservative eligibility gate + story blacklist | yes |
| World-state store | yes |
| High-level tool registry (31 mapped tools: perception/lore, movement, attention, speech, gesture/think, reaction, interaction, deferred combat) | yes |
| Wait gestures while LLM/TTS are generating | yes |
| Action start/complete/fail contract and event logging | yes |
| Event-driven runtime scheduling | yes |
| Push-to-Talk target selection + STT/TTS interfaces | interfaces + Null adapters |
| Localhost TCP JSON bridge IPC | yes |
| Per-NPC system prompt / background context | yes |
| ANIM/gesture native action inventory | `docs/rdr2_asi_actions.md`, `bridge/native_action_map.json` |
| Runtime action validation + behavior guardrails | yes |
| Google ADK backend with OpenAI-compatible models | optional adapter via ADK + LiteLLM |
| Built-in OpenAI-compatible backend (standard library only) | yes — `--backend llm`, no packages |
| C++ ScriptHookRDR2 `.asi` bridge | core implemented in `bridge/`, builds against the official SDK |

## Install

The runtime core has **no third-party Python dependencies**, so a checkout runs
as-is. One command verifies everything and wires up the optional parts:

```bash
python3 install.py          # guided install + doctor + smoke test
python3 install.py --check  # diagnose only, changes nothing
```

Or go straight to the demo — nothing to install:

```bash
python3 run_demo.py         # scan -> activate -> decide -> speak, no API key
```

Full tiers (offline, live model, in-game `.asi`), Windows one-liners, and
troubleshooting: **[`docs/INSTALL.md`](docs/INSTALL.md)**.

### Live model, zero packages

```bash
cp .env.example .env        # add OPENCODE_API_KEY (or any OpenAI-compatible key)
python3 -m runtime.main --backend llm
```

`--backend llm` uses the built-in standard-library OpenAI-compatible client —
no `google-adk`, no `litellm`, no `pip install`. Google ADK remains available
via `python3 -m runtime.main --backend adk` (`install.py --with-adk --venv`).

### Native bridge, one command (Windows)

```powershell
powershell -ExecutionPolicy Bypass -File .\install.ps1 -WithAsi -Deploy
```

Downloads the official SDK, builds `rdr2_ai_bridge.asi`, and copies it into the
RDR2 folder. See [`docs/INSTALL.md`](docs/INSTALL.md#tier-2--in-game-native-bridge).

## Quick start (offline, no external packages)

```bash
python3 run_demo.py
python3 scenario_runner.py
python3 -m unittest discover -s tests -v
```

`run_demo.py` drives the full path: proximity scan -> CANDIDATE -> AWARE ->
meaningful trigger -> AI_ACTIVE -> Push-to-Talk -> `PLAYER_SPOKE` -> agent
decision -> `say`/`look_at` action events.

`scenario_runner.py` runs deterministic end-to-end scenarios from
`scenarios/*.json` and reports PASS/FAIL with event-level assertions.

## Scenario tests

The scenario suite covers:

- `01_proximity_greeting.json` — proximity activation and autonomous greeting
- `02_push_to_talk_conversation.json` — Push-to-Talk conversation lifecycle
- `03_threat_flee.json` — threat reaction, fear mood, flee action
- `04_gunshot_investigate.json` — high-courage investigation decision
- `05_action_failure_recovery.json` — `ACTION_FAILED` recovery to `wander`
- `06_story_safety_suspend_resume.json` — story safety suspend/resume
- `07_barge_in.json` — barge-in interruption without duplicate conversation start
- `08_historical_memory.json` — seeded timeline fact changes greeting and `grab_timeline` retrieves it
- `09_release_policy.json` — release back to Rockstar ownership
- `10_silence_policy.json` — activated NPC may still choose silence

Run just the scenarios:

```bash
python3 scenario_runner.py
```

Run them through the normal test suite:

```bash
python3 -m unittest tests.test_scenarios -v
```

## Integration tests

Real runtime IPC test:

```bash
python3 -m unittest tests.test_ipc_integration -v
```

It starts the real TCP `BridgeServer`, sends `hello` / `ped_scan` /
`player_speech`, receives a real `action_request`, returns `action_result`,
and verifies the JSONL timeline.

Cross-language bridge test (needs `g++`):

```bash
python3 -m unittest tests.test_bridge_ipc_e2e -v
```

It compiles `bridge/tests/ipc_e2e_client.cpp` against the portable C++ bridge
core, connects over real TCP, executes an action request, and verifies the
returned `action_result`.

Portable C++ core test:

```bash
cmake -S bridge -B bridge/build
cmake --build bridge/build
ctest --test-dir bridge/build --output-on-failure
```

## Live LLM testing

The deterministic scenarios above use `RuleBasedAgentBackend`.  For a live
OpenAI-compatible LLM check, the project includes a dependency-free
`OpenAICompatibleBackend` using only the standard library.

One model call:

```bash
python3 live_llm_check.py
```

End-to-end live scenarios:

```bash
python3 scenario_runner.py \
  --backend llm \
  --scenarios scenarios_live
```

Live scenario files:

- `01_llm_activation_smoke.json` — real model returns a structured decision
- `02_live_threat_guardrail.json` — low-courage threat forces a `flee_from`
- `03_live_gunshot_guardrail.json` — high-courage gunshot forces `investigate`

Live LLM unit/integration tests:

```bash
RUN_LIVE_LLM=1 python3 -m unittest tests.test_live_llm -v
```

The live backend resolves the key in this order:

1. explicit `--api-key`
2. `OPENCODE_API_KEY`
3. `OPENAI_API_KEY`
4. local `~/.local/share/opencode/auth.json` (`opencode-go` entry)

Default live target is OpenCode Go `deepseek-v4.1-flash` with
`reasoning_effort=low` and `thinking.type=enabled`.

## Quest dialogue-only overlay

For NPCs that are currently on a Rockstar-controlled quest, the runtime can
add a **dialogue-only overlay**:

```text
Rockstar AI    -> owns movement, tasks, animation, quest scripting
Our runtime    -> owns speech only, tied to current quest context
```

Bridge metadata example:

```json
{
  "quest_dialogue": true,
  "quest_id": "quest_valentine_livestock",
  "quest_state": {
    "current_objective": "Drive the cattle to the auction pens before evening."
  }
}
```

When such a ped comes within `aware_distance_m`, the runtime enters
`QUEST_DIALOGUE`.  The agent prompt then receives:

- `CURRENT QUEST CONTEXT`
- `DIALOGUE-ONLY MODE`
- only speech/perception tools (`say`, `get_world_state`, `grab_timeline`,
  `get_world_lore`)

Movement, combat, interaction, and animation actions are dropped and logged
as `ACTION_FAILED / dialogue_only_mode`; Rockstar keeps the body.

Quest context lives in `data/quests/<quest_id>.json`; see
`data/quests/quest_valentine_livestock.json` for an example.

## Offline Red Dead Wiki context

The runtime does **not** scrape the web during gameplay.  A local context pack
is built on demand from the Red Dead Wiki (Fandom) API:

```bash
python3 scripts/fetch_wiki_context.py
```

Output:

```text
data/wiki/rdr2_context.json
```

The pack includes attribution/license metadata and plain-text extracts for
pages such as `Valentine`, `New Hanover`, `The Heartlands`, `Hunting`, and
`Red Dead Redemption 2`.

For characters, build the bulk character pack:

```bash
python3 scripts/fetch_character_context.py
```

Output:

```text
data/wiki/characters.json
```

It currently contains **838 character pages** from `Category:Characters in
Redemption 2` and `Category:Characters in Online`, each with:

- lead summary
- infobox fields (`gender`, `affiliations`, `family`, `occupation`, etc.)
- aliases
- page id / revision / source URL

Each NPC profile can declare relevant topics:

```json
{
  "region": "New Hanover",
  "wiki_topics": ["Valentine", "New Hanover", "The Heartlands"]
}
```

A profile can also map directly to a canonical character:

```json
{
  "name": "Arthur Morgan",
  "wiki_character": "Arthur Morgan"
}
```

At activation the runtime injects the matching extracts into the agent prompt
under `WORLD LORE CONTEXT`.  The agent can also call:

```text
get_world_lore(topic?)
```

`get_world_lore("Arthur Morgan")` searches the character pack too.
Full page inventory and source URLs are stored in the packs.

## Adaptive thinking policy

The runtime chooses per-decision reasoning effort:

```text
PLAYER_APPROACHED / PLAYER_LOOKED_AT_NPC / pass-by / idle
→ thinking disabled

PLAYER_THREATENED_NPC / PLAYER_ATTACKED_NPC / NPC_DAMAGED
GUNSHOT_HEARD / action failure / planning
→ low thinking

Push-to-Talk and PLAYER_SPOKE
→ low thinking
```

Policy lives in `config/settings.json`:

```json
"thinking_policy": {
  "default_mode": "low",
  "disabled_for_events": [
    "PLAYER_APPROACHED",
    "PLAYER_LOOKED_AT_NPC",
    "PLAYER_LEFT_AREA",
    "NPC_ACTIVATED"
  ],
  "disabled_for_reasons": [
    "ped_scan_activation",
    "idle",
    "pass_by",
    "deferred_promotion"
  ]
}
```

`build_agent_prompt` receives `flags.thinking_mode`; both `GoogleADKBackend`
and `OpenAICompatibleBackend` translate it into LiteLLM/OpenAI-compatible
`reasoning_effort` and `thinking` parameters.

## Idle autonomous thinking

While an NPC is `AI_ACTIVE` and the player stays nearby, the runtime fires an
idle planning turn after `scheduler.idle_thinking_seconds` (default `5.0`)
without a decision or pending action.

- reason: `idle_autonomous`
- thinking mode: `disabled` for speed
- skipped while `wait`/movement action is still pending
- scheduler cap still applies (`max_reasoning_agents`)
- may result in speech, goal change, new action, or silence

Set `idle_thinking_seconds` to `0` to disable it.

## Latency benchmark

```bash
# runtime/tool latency only
python3 benchmark_latency.py

# include live OpenAI-compatible model latency
python3 benchmark_latency.py --live --live-iterations 3
```

The runtime-side action path (validation + timeline append + local dispatch)
is sub-millisecond.  Live LLM decision latency depends on provider load and
prompt size; measure with the command above.

## Behavior stabilization

Raw LLM output is treated as intent, not executable truth.  Before any tool
reaches the bridge, the runtime normalizes arguments and applies safety
guardrails:

- `look_at` / `face` / `follow` / `flee_from` missing `entity` → `"player"`
- `wait` missing `duration` → `2.0`
- `gesture` missing `type` → `"neutral"`
- `wander` missing/invalid `radius` → `8.0`
- `go_to` missing `destination` → local `wander(8.0)` fallback
- `investigate` missing `position` → latest recent event position if available
- duplicate empty `say` is dropped when `speech` is already present
- unknown/malformed actions are dropped and logged as `ACTION_FAILED`

Critical-event guardrails then ensure the spec behavior:

- threat + low courage → at least one `flee_from(player)`
- gunshot + high courage → `investigate(position)` if a position is known
- gunshot + low courage → `wander` escape fallback

Speech remains LLM-authored; guardrails only guarantee a valid action.

## Per-NPC system prompt context

Each `data/profiles/<npc_id>.json` can now define:

- `system_prompt` — persistent identity/behavior prompt
- `background` — biography and lived context
- `speech_style`
- `behavior_rules`
- `goals`, `knowledge`, `relationships`, `fears`, `quirks`
- `personality`

When the NPC enters `AI_ACTIVE` / `AI_CONVERSATION`, `build_agent_prompt`
injects this context as `NPC SYSTEM PROMPT` and `NPC PROFILE` before the
current world state, recent events, goal, mood, and tool list.

## Run the runtime and mock bridge over IPC

Terminal 1:

```bash
python3 -u -m runtime.main --host 127.0.0.1 --port 8765 --backend llm
```

Terminal 2:

```bash
python3 mock_bridge.py --host 127.0.0.1 --port 8765
```

The runtime is the TCP server.  The bridge connects to it and exchanges one
JSON object per line.  `mock_bridge.py` is a useful smoke test before the
native ASI plugin exists.  Both the host/port and the backend can be left to
`.env`; with `RDR2AI_BACKEND=llm` set, plain `python3 -m runtime.main` is enough.

## Built-in OpenAI-compatible backend (no packages)

`OpenAICompatibleBackend` implements the same agent contract using only the
Python standard library, so live models work without `google-adk` or `litellm`:

```bash
python3 -m runtime.main --backend llm
```

It reads its key from `--api-key`, `$RDR2AI_API_KEY_ENV`, `$OPENCODE_API_KEY`,
`$OPENAI_API_KEY`, or `~/.local/share/opencode/auth.json` (in that order), and
its endpoint from `--api-base`, `$RDR2AI_API_BASE`, or `config/settings.json`.

## Google ADK backend with OpenAI-compatible models

The runtime defaults to a deterministic rule-based backend so it works
offline.  `GoogleADKBackend` uses Google ADK for orchestration and ADK's
LiteLLM adapter for model calls, so the same backend works with:

- OpenAI
- vLLM
- Ollama OpenAI-compatible endpoint
- LM Studio
- llama.cpp server
- LiteLLM proxy
- any other OpenAI-compatible `/v1` chat-completions endpoint

This path is optional; `--backend llm` above covers the same endpoints without
the extra packages.  Install the ADK extras only if you want ADK orchestration:

```bash
python3 install.py --with-adk --venv     # or: pip install google-adk litellm
```

Default target in `config/settings.json` is now OpenCode Go:

- provider: `openai` (OpenAI-compatible transport)
- model: `deepseek-v4.1-flash`
- base URL: `https://opencode.ai/zen/go/v1`
- API key env: `OPENCODE_API_KEY`
- thinking: `reasoning_effort=low` + `extra_body={"thinking":{"type":"enabled"}}`

Run it with:

```bash
export OPENCODE_API_KEY="..."
python3 -m runtime.main --backend adk
```

Override the model or thinking level when needed:

```bash
python3 -m runtime.main --backend adk --reasoning-effort low --model deepseek-v4.1-flash
```

OpenAI cloud example:

```bash
export OPENAI_API_KEY="sk-..."
python3 -m runtime.main --backend adk --model gpt-4o-mini
```

Local OpenAI-compatible server example:

```bash
python3 -m runtime.main \
  --backend adk \
  --model local-model \
  --api-base http://127.0.0.1:8000/v1 \
  --api-key not-needed
```

Environment-variable form:

```bash
export OPENAI_API_KEY="not-needed"
export OPENAI_BASE_URL="http://127.0.0.1:11434/v1"
python3 -m runtime.main --backend adk --model llama3.1
```

The adapter is in `runtime/agent/backends.py` (`GoogleADKBackend`).  It builds
an ADK `LlmAgent` with `LiteLlm(model="openai/<model>", api_base=..., api_key=...)`,
keeps perception tools live, captures action tools, and parses a structured
JSON decision.  ADK/LiteLLM versions change; if a future version uses a
different runner/session/model-init signature, adapt only that class.

## Configuration

Precedence, highest first: CLI flag > environment (`.env`) > `config/settings.json`
> built-in defaults.  Copy `.env.example` to `.env`, or let
`python3 install.py --api-key <KEY>` create it.

- `.env` — `OPENCODE_API_KEY` / `OPENAI_API_KEY`, `RDR2AI_BACKEND`,
  `RDR2AI_API_BASE`, `RDR2AI_MODEL` (git-ignored; also read from `.env.local`
  and `config/local.env`).
- `config/settings.json` — distances, scheduler limits, push-to-talk key,
  speech cooldown, paths.
- `config/story_blacklist.json` — protagonists, gang members, named story
  characters, mission-only models, unstable models.
- `data/profiles/<npc_id>.json` — per-NPC system prompt, background,
  personality, speech style, behavior rules, knowledge, and relationships
  (configuration, not memory).
- `data/timelines/<npc_id>.jsonl` — append-only event history.

Eligibility fails closed.  Any missing/uncertain safety field blocks agent
activation.

## Project layout

```text
rdr2-ai-npc/
├── install.py              # one-command installer + doctor (stdlib only)
├── install.ps1 / install.cmd   # Windows bootstrap + deploy
├── run_runtime.cmd         # Windows double-click runtime launcher
├── Makefile                # optional shortcuts (make doctor/demo/test/asi)
├── .env.example            # copy to .env for key + backend defaults
├── bridge/                 # C++17 core + ScriptHookRDR2 ASI adapter
│   ├── CMakePresets.json   # `cmake --preset core` / `--preset asi`
│   └── native_action_map.json
├── runtime/
│   ├── agent/              # instructions, backend adapters, orchestration
│   ├── audio/              # STT/TTS interfaces and optional adapters
│   ├── events/             # bridge-message normalizer
│   ├── ipc/                # newline-delimited JSON TCP server
│   ├── state/              # eligibility, ownership, world state
│   ├── timeline/           # JSONL timeline store
│   ├── tools/              # high-level tool registry
│   ├── config.py
│   ├── env.py              # .env loader (stdlib only)
│   ├── main.py
│   └── schemas.py
├── docs/                   # INSTALL.md + native API / action inventory
├── scripts/                # wiki/character importers + SDK installer
├── data/wiki/              # world + 838-character Red Dead Wiki packs
├── data/quests/            # Rockstar quest dialogue context
├── scenarios/              # deterministic JSON scenario definitions
├── scenarios_live/         # live LLM JSON scenarios
├── config/
├── data/
├── tests/
├── mock_bridge.py
├── run_demo.py
├── live_llm_check.py
├── benchmark_latency.py
└── scenario_runner.py
```

## Non-negotiable design rules enforced by the scaffold

- The LLM gets high-level tools only; raw native hashes are never accepted.
- The game thread must never perform LLM/STT/TTS/timeline I/O.  The bridge
  worker is separate from the game tick.
- The timeline is the only memory.  No vector DB, embeddings, or semantic
  memory is used in the MVP.
- Every action start/completion/failure and goal change is written as an
  event.
- Story/mission/cutscene safety always wins; uncertain ped state blocks
  activation.

## Native action reference

Inspected ASI/TaskInvoker/native DB references:

- `docs/rdr2_asi_actions.md`
- `bridge/native_action_map.json`

The mapping covers movement, attention, speech, gesture/animation, scenario,
emote, reaction, flee, interaction, mount/vehicle, and deferred combat tools.

## Native bridge status

`bridge/` now contains a real C++17 core:

- newline TCP JSON `IpcClient` with reconnect and thread-safe send queue
- portable `ActionExecutor` mapping 28 high-level tools to native hashes
- `PedScanner`, `StoryGate`, `BridgeRuntime`, PTT hooks, estimated
  action-completion reporting
- `ScriptHookNativeApi` adapter for the real SDK (`-DRDR2AI_HAS_SCRIPTHOOK`)
- dependency-free `mini_json` parser/serializer
- portable core test: `bridge/tests/bridge_core_test.cpp`
- `V` Push-to-Talk key handler in the ASI entry point

Run the portable core test:

```bash
python3 install.py --yes --with-bridge    # CMake if present, else a direct g++ build
# or explicitly:
cmake --preset core -S bridge && cmake --build --preset core
ctest --test-dir bridge/build/core --output-on-failure
```

Download the official ScriptHookRDR2 SDK locally:

```bash
python3 install.py --yes --with-sdk       # or: python3 scripts/install_scripthook_sdk.py
```

This installs to `bridge/.sdk/ScriptHookRDR2_SDK_1.0.1207.73/` (git-ignored;
the SDK archive is not redistributed).  Then build the ASI:

```bash
python3 install.py --yes --with-asi       # needs CMake + MSVC on Windows
# or explicitly:
cmake --preset asi -S bridge && cmake --build --preset asi
```

On Windows, `.\install.ps1 -WithAsi -Deploy` also copies the plugin into the
game folder and warns if `ScriptHookRDR2.dll` is missing.

Verified locally:

```text
bridge_core_test: 28 actions mapped, scanner/story gate OK
IPC end-to-end smoke: hello -> action_request(say) -> action_result(completed)
```

## License and attribution

The **source code is MIT** licensed — see [`LICENSE`](LICENSE).

The offline wiki context packs in `data/wiki/*.json` are extracts of the
[Red Dead Wiki](https://reddead.fandom.com) and are distributed under
**CC BY-SA 4.0**. Red Dead Redemption 2 is a trademark of Take-Two Interactive;
this project is unofficial and ships **no game assets**. The ScriptHookRDR2 SDK
and `ScriptHookRDR2.dll` are downloaded from their official site and are never
redistributed here.

Full details, including per-file terms and regeneration commands, are in
[`NOTICE.md`](NOTICE.md).

