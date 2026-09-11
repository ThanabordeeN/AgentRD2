# Installation

Three tiers. Most people only need the first two.

| Tier | What you get | Requirements | Time |
|---|---|---|---|
| **0 — Zero install** | Offline demo, scenarios, full test suite | Python 3.10+ | ~10 s |
| **1 — Standard** | Above + live model over the network | Python 3.10+ and an API key | ~1 min |
| **2 — In-game bridge** | Above + `rdr2_ai_bridge.asi` inside RDR2 | + Windows and ScriptHookRDR2 (compiler only if you build it yourself) | ~2–10 min |

The runtime core has **no third-party Python dependencies**. `pip` is only ever
needed for the optional Google ADK or local-audio extras, so a fresh checkout
runs immediately.

---

## Tier 0 — Zero install

```bash
git clone <repo> && cd AgentRD2
python3 install.py --check     # diagnose: verifies Python, layout, packs, key
python3 run_demo.py            # full path: scan -> activate -> speak
```

That is the whole install. `install.py --check` writes nothing and exits
non-zero only on a blocking problem.

## Tier 1 — Standard install (recommended)

```bash
python3 install.py
```

It will:

1. verify the Python version, project layout, imports, tool catalog, and context packs
2. confirm `data/timelines` is writable
3. offer to save your API key into `.env` so no shell exports are needed
4. optionally create `.venv` and install extras (`--with-adk`, `--with-audio`)
5. optionally download the ScriptHookRDR2 SDK and build the bridge
6. run `run_demo.py` as a smoke test
7. print the exact commands to run next

Non-interactive (CI, scripts, other people's machines):

```bash
python3 install.py --yes                       # accept defaults
python3 install.py --check --json              # machine-readable report
python3 install.py --yes --api-key sk-...      # save a key and verify
python3 install.py --yes --full-smoke --probe  # unit suite + one real model call
```

### Adding a model key

Either let the installer prompt you, or write `.env` yourself:

```bash
cp .env.example .env
$EDITOR .env
```

```dotenv
OPENCODE_API_KEY=sk-...
RDR2AI_BACKEND=llm
RDR2AI_API_BASE=https://opencode.ai/zen/go/v1
RDR2AI_MODEL=deepseek-v4.1-flash
```

`.env`, `.env.local`, and `config/local.env` are all loaded automatically and are
git-ignored. Real environment variables always win over these files.

### Running with a live model

```bash
python3 -m runtime.main --backend llm
```

`--backend llm` uses the built-in OpenAI-compatible client (standard library
only) — **no `google-adk`, no `litellm`, no `pip install`**. It works with any
`/chat/completions` endpoint:

| Provider | `RDR2AI_API_BASE` |
|---|---|
| OpenCode Go | `https://opencode.ai/zen/go/v1` |
| OpenAI | `https://api.openai.com/v1` |
| Ollama | `http://127.0.0.1:11434/v1` |
| LM Studio | `http://127.0.0.1:1234/v1` |

`--backend adk` remains available for Google ADK + LiteLLM users
(`python3 install.py --with-adk --venv`).

Verify the model path on its own:

```bash
python3 live_llm_check.py --thinking disabled   # one real call
python3 scenario_runner.py --backend llm --scenarios scenarios_live
```

## Tier 2 — In-game native bridge

The Python runtime works without the bridge (`mock_bridge.py` stands in for the
game). To drive real NPCs you need the `.asi` plugin inside RDR2.

### Option A — download the prebuilt plugin (no compiler)

CI builds `rdr2_ai_bridge.asi` on every push and attaches it to tagged
releases, so you can skip the SDK, CMake, and MSVC entirely:

1. grab `rdr2_ai_bridge.asi` from the latest release or the `build` workflow artifacts
2. download `ScriptHookRDR2.dll` from <http://www.dev-c.com/rdr2/scripthookrdr2/>
3. copy both next to `RDR2.exe`

### Option B — Windows, one command

```powershell
powershell -ExecutionPolicy Bypass -File .\install.ps1 -WithAsi -Deploy
```

or double-click `install.cmd`. The script finds Python, downloads the official
SDK, builds the plugin, locates your RDR2 folder, and copies the `.asi` in.
Add `-GameDir "D:\Games\Red Dead Redemption 2"` if auto-detection misses it.

### Option C — manually

```bash
python3 install.py --with-sdk      # official SDK -> bridge/.sdk/...
python3 install.py --with-asi      # needs CMake + MSVC on Windows
```

Or with CMake directly:

```bash
cmake --preset asi -S bridge
cmake --build --preset asi
```

Then copy into the game folder next to `RDR2.exe`:

```text
rdr2_ai_bridge.asi     <- built by this project
ScriptHookRDR2.dll     <- from http://www.dev-c.com/rdr2/scripthookrdr2/
```

`ScriptHookRDR2.dll` is Alexander Blade's runtime and is **not** redistributed
here; the SDK download script fetches the SDK from the official site only.

### Portable core only (any OS, no SDK)

Useful for verifying the C++ side without the game:

```bash
python3 install.py --with-bridge
# CMake if available, otherwise a direct g++ build:
#   bridge/build/bridge_core_test
```

---

## Platform notes

### Windows

* Install Python 3.10+ with **"Add python.exe to PATH"** ticked, or
  `winget install -e --id Python.Python.3.12`.
* Use `py -3` if `python` is not on PATH. `install.cmd` and `run_runtime.cmd`
  handle this for you.
* For the `.asi` you need **Visual Studio Build Tools** (Desktop C++ workload)
  plus CMake. CI also publishes prebuilt artifacts, so a compiler is optional.
* The game must be launched with ScriptHookRDR2 present or the `.asi` is ignored.

### Linux / macOS

Tiers 0, 1, and the portable bridge core all work. The `.asi` cannot be built or
loaded outside Windows; use `mock_bridge.py` for integration work.

```bash
python3 install.py --yes
python3 -m unittest discover -s tests -v
```

---

## Configuration

Precedence, highest first:

```text
CLI flag  >  environment (.env)  >  config/settings.json  >  built-in defaults
```

| Variable | Purpose |
|---|---|
| `OPENCODE_API_KEY` / `OPENAI_API_KEY` | model credentials |
| `RDR2AI_BACKEND` | `rule`, `llm`, or `adk` |
| `RDR2AI_API_BASE` | OpenAI-compatible base URL |
| `RDR2AI_MODEL` | model name |
| `RDR2AI_PROVIDER` | LiteLLM provider prefix for `--backend adk` |
| `RDR2AI_API_KEY_ENV` | which env var holds the key |
| `RDR2AI_SCRIPTHOOK_ROOT` | override the SDK location for the bridge build |

Key resolution order (first hit wins):

```text
--api-key  >  $RDR2AI_API_KEY_ENV  >  $OPENCODE_API_KEY  >  $OPENAI_API_KEY
           >  ~/.local/share/opencode/auth.json -> opencode-go.key
```

Runtime behaviour (activation distances, scheduler caps, speech cooldowns,
thinking policy, model defaults) lives in `config/settings.json`.

---

## Make shortcuts

`make` is optional — every target is a thin wrapper.

```bash
make setup      # install.py --yes
make doctor     # diagnose, change nothing
make demo       # offline demo
make llm        # runtime with the live model backend
make test       # full unit + integration suite
make scenarios  # deterministic scenarios
make bridge     # portable C++ core
make asi        # in-game plugin
make wiki       # refresh offline wiki context packs
```

---

## Verifying an install

```bash
python3 install.py --check                              # doctor
python3 run_demo.py                                     # offline path
python3 scenario_runner.py                              # 10/10 scenarios
python3 -m unittest discover -s tests -v                # full suite
python3 install.py --yes --probe                        # one real model call
```

The doctor checks, in order: Python version, project layout, runtime imports,
tool catalog (31 tools), context packs (profiles/quests/838 wiki characters),
`settings.json` validity, timeline writability, virtualenv state, `.env` files,
API key resolution, SDK presence, and the C++ toolchain.

---

## Troubleshooting

| Symptom | Fix |
|---|---|
| `python3: command not found` | Install Python 3.10+, or use `py -3` on Windows |
| `No module named runtime` | Run from the repo root: `cd AgentRD2` |
| `invalid backend 'x' from RDR2AI_BACKEND` | Fix `RDR2AI_BACKEND` in `.env` (or delete the line) |
| `No OpenAI-compatible API key found` | `python3 install.py --api-key <KEY>` or edit `.env` |
| Model call returns 401/404 | Wrong key, or `RDR2AI_API_BASE` needs `/v1` (or shouldn't have it) |
| Live replies feel slow | Lower `reasoning_effort`, or see the thinking policy in `config/settings.json` |
| `.asi` not loading in game | `ScriptHookRDR2.dll` missing next to `RDR2.exe`, or wrong game build |
| `pip install` fails for `--with-adk` | Extras are optional; `--backend llm` needs no packages |
| Doctor says layout FAIL | You are running from a partial copy of the repo |
| `data/timelines` not writable | Fix permissions; NPC memory is append-only JSONL there |

---

## Uninstall / reset

```bash
rm -rf .venv bridge/build            # generated artifacts
rm -f .env                           # local secrets
rm -f data/timelines/*.jsonl         # NPC memory (deletes all history)
python3 install.py --check           # confirm a clean checkout still passes
```

Nothing is written outside the project directory except the SDK download inside
`bridge/.sdk/`.
