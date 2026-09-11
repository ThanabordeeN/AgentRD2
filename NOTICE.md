# Notices and third-party attribution

This repository mixes original code with community-sourced reference data, so
different parts carry different terms. The short version:

| Part | Terms |
|---|---|
| All source code (`runtime/`, `bridge/`, `install.py`, `tests/`, scripts) | MIT — see [`LICENSE`](LICENSE) |
| `data/wiki/*.json` (world + character context packs) | CC BY-SA 4.0 — see below |
| `docs/rdr2_asi_actions.md`, `bridge/native_action_map.json` | factual API references — see below |
| Red Dead Redemption 2 game assets | **none are redistributed** |

---

## 1. Source code — MIT

Copyright (c) 2026 ThanabordeeN. See [`LICENSE`](LICENSE).

## 2. Wiki context packs — CC BY-SA 4.0

`data/wiki/rdr2_context.json` and `data/wiki/characters.json` are offline
extracts of pages from the **Red Dead Wiki** (Fandom), retrieved through the
public MediaWiki API at `https://reddead.fandom.com/api.php`.

- **Source:** <https://reddead.fandom.com>
- **Attribution:** Red Dead Wiki contributors
- **License:** Creative Commons Attribution-ShareAlike 4.0 International
  (<https://creativecommons.org/licenses/by-sa/4.0/>)

These two data files are therefore distributed under **CC BY-SA 4.0**, not MIT.
If you redistribute them — or a modified version of them — you must keep this
attribution and license them under the same terms.

World/location pack pages (`data/wiki/rdr2_context.json`):

```text
Red Dead Redemption 2, Valentine, New Hanover, The Heartlands,
Ambarino, Hunting, Van der Linde gang, Pinkerton National Detective Agency
```

Character pack (`data/wiki/characters.json`): 838 character pages drawn from
`Category:Characters in Redemption 2` and `Category:Characters in Online`.
Every entry keeps its own `pageid`, `revision`, and `url`, so any record can be
traced back to the exact wiki revision it came from.

Both files record this metadata inline under `meta` and can be regenerated at
any time:

```bash
python3 scripts/fetch_wiki_context.py        # world/location/faction pack
python3 scripts/fetch_character_context.py   # 838-character pack
```

The runtime never scrapes the wiki at game time; it reads these local files
only.

## 3. Red Dead Redemption 2 — trademarks

Red Dead Redemption 2, Red Dead Online, and all related characters, locations,
and marks are trademarks and copyright of Take-Two Interactive Software, Inc.
and Rockstar Games. This project is an unofficial, fan-made interoperability
tool. It is **not** affiliated with, endorsed by, or supported by Rockstar
Games or Take-Two Interactive.

No game assets — models, textures, audio, animations, or data files — are
included in this repository. Animations, emotes, scenarios, and natives are
referenced by **name and hash only**, so the game itself supplies the content at
runtime.

## 4. ScriptHookRDR2 SDK — not redistributed

The native bridge builds against the ScriptHookRDR2 SDK by **Alexander Blade**,
available from <http://www.dev-c.com/rdr2/scripthookrdr2/>.

The SDK archive is **not** part of this repository and is git-ignored at
`bridge/.sdk/`. `scripts/install_scripthook_sdk.py` downloads it from the
official site on your machine, and its readme/terms are printed after
extraction. `ScriptHookRDR2.dll` is likewise not redistributed — download it
yourself from the official page.

## 5. Native API reference data

`docs/rdr2_asi_actions.md` and `bridge/native_action_map.json` were compiled
from public community references:

- <https://github.com/Halen84/ScriptHookRDR2DotNet-V2> — C# API wrapper
- <https://github.com/alloc8or/rdr3-nativedb-data> — native database dump
- <https://github.com/femga/rdr3_discoveries> — events, animations, scenarios

These are factual references (native names, hashes, parameter lists) used to map
high-level agent tools onto game natives. Refer to each repository for its own
license terms before reusing that material elsewhere.

## 6. Model providers

The runtime talks to any OpenAI-compatible endpoint you configure. No model
weights, prompts, or provider credentials are bundled here. API keys live in
your local, git-ignored `.env` file.
