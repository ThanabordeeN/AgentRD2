# RDR2 Living NPC Agent — Go runtime

A standalone port of the Python runtime. It speaks the same IPC protocol, reads
the same `config/`, `data/` and `scenarios/` files, and is built so the agent can
ship as **one executable with no Python installation**.

```bash
cd runtime-go
go build -trimpath -ldflags "-s -w" -o rdr2-npc ./cmd/rdr2-npc
./rdr2-npc --check
./rdr2-npc --backend adk
```

On Windows the result is `rdr2-npc.exe`; CI builds and publishes it, so users
never need a Go toolchain either.

## Why a Go port

| | Python runtime | Go runtime |
|---|---|---|
| Install | Python 3.10+ | none — single executable |
| Startup | ~200–400 ms | ~10 ms |
| Distribution | source tree + interpreter | one file |
| Model layer | built-in stdlib client + optional ADK | Google ADK for Go (`google.golang.org/adk/v2`) |
| Bridge | C++ `.asi` over TCP JSON | identical — same protocol, same files |

The C++ bridge (`bridge/`) is unchanged and works with both runtimes: the IPC
protocol is the contract, and the on-disk timeline is a shared JSONL format.

## Layout

```text
runtime-go/
├── cmd/rdr2-npc/          # CLI: runtime, --check, --scenarios
└── internal/
    ├── agent/             # orchestration, ownership flow, guardrails, prompts
    ├── backend/           # Backend interface, rule backend, Google ADK backend
    ├── config/            # settings.json + NPC profiles
    ├── domain/            # canonical types (Event, WorldState, AgentDecision…)
    ├── dotenv/            # .env loading (parity with runtime/env.py)
    ├── events/            # bridge message → canonical fact normalisation
    ├── ipc/               # newline-delimited JSON TCP server
    ├── lore/              # offline Red Dead Wiki packs + quest context
    ├── project/           # repo/executable root resolution
    ├── scenarios/         # scenario parity harness
    ├── state/             # ownership machine, eligibility gate, world state
    ├── timeline/          # append-only JSONL timeline
    └── tools/             # the 31 high-level agent tools
```

## The model backend

`internal/backend/adk.go` uses Google's ADK for Go with its OpenAI-compatible
adapter, so any `/v1/responses` endpoint works (OpenCode Go, OpenAI, and
gateways that speak the same surface).

Configuration follows the same precedence as the Python runtime —
**CLI flag > environment (`.env`) > `config/settings.json`**:

```bash
cp ../.env.example ../.env      # OPENCODE_API_KEY=...

cd runtime-go
go run ./cmd/rdr2-npc --backend adk
go run ./cmd/rdr2-npc --backend rule     # deterministic, offline
```

| Variable | Purpose |
|---|---|
| `OPENCODE_API_KEY` / `OPENAI_API_KEY` | model credentials |
| `RDR2AI_BACKEND` | `rule` or `adk` |
| `RDR2AI_API_BASE` | OpenAI-compatible base URL |
| `RDR2AI_MODEL` | model id |
| `RDR2AI_ROOT` | override where `config/` and `data/` live |

## Parity with the Python runtime

Both runtimes are held to the same contract: the scenario files in
`../scenarios/`. The Go runner replays them and asserts the same events,
ownership transitions, action tools, speech fragments and event counts:

```bash
go run ./cmd/rdr2-npc --scenarios ../scenarios
go run ./cmd/rdr2-npc --scenarios ../scenarios --json
```

They also share the timeline format, so a timeline written by one runtime can be
read by the other — useful when migrating an existing install.

## Tests

```bash
go test ./...                 # unit tests, offline
gofmt -l .                    # formatting
go vet ./...                  # static checks

# one real model call (needs a key):
RDR2AI_LIVE=1 go test ./internal/backend/ -run TestLiveADKBackend -v
```

## Doctor

```bash
./rdr2-npc --check          # human readable
./rdr2-npc --check --json   # machine readable
```

It verifies the runtime build, project root, settings, profiles, wiki context
packs, timeline writability, the tool catalog, the story blacklist, and whether
a model key resolves.
