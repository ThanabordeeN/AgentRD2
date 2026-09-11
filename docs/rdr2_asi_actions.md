# RDR2 ASI / Native Action Inventory

Sources inspected (local shallow clones):

| Repo | Commit | Purpose |
|---|---|---|
| `Halen84/ScriptHookRDR2DotNet-V2` | `03f2264d8d2c1e4c6e79d10cf7ba48d247c6e506` | C# API wrapper, `Ped.Task`, `PlayAnimation`, `PlaySpeech`, examples |
| `alloc8or/rdr3-nativedb-data` | `b3c5d5eae2966202db17518d6b3978899be2a247` | full native list, hashes, parameter signatures, comments |
| `femga/rdr3_discoveries` | `49087fb2756594d2364e4abf79ee1df44d6ef3b4` | emotes, animation dictionaries, scenarios, conditional anims |

Machine-readable mapping: `bridge/native_action_map.json`

---

## 1. What the ASI API exposes

The ScriptHookRDR2DotNet `TaskInvoker` wrapper alone contains these action families:

- movement: go to, follow, wander, follow road, drive, lead horse
- attention: look at, turn to, aim
- speech and animation: `PlaySpeech`, `PlayAnimation`, `StopAnimation`
- scenarios: `StartScenario`, `UseRandomScenarioInGroup`
- reactions: `React`, `Flee`, `WalkAway`, `Cower`, `HandsUp`, `Duck`, `Jump`
- interaction: item interaction, animal interaction, horse action
- vehicle/mount: enter/leave vehicle, mount/dismount
- combat: combat, shoot, cover (deferred for this MVP)
- task control: pause, stand still, clear tasks, secondary tasks, force motion state

The native DB contains **360 `TASK_*` natives**, plus speech/audio, animation streaming, scenario, and animation-scene natives.

---

## 2. High-level tool mapping (current MVP)

| Runtime tool | Primary native candidates | Physical gesture/animation? |
|---|---|---|
| `wander(radius)` | `TASK_WANDER_STANDARD`, `TASK_WANDER_IN_AREA`, `TASK_WANDER_IN_VOLUME`, `TASK_WANDER_SWIM` | movement only |
| `go_to(destination)` | `TASK_GO_TO_COORD_ANY_MEANS`, `TASK_FOLLOW_NAV_MESH_TO_COORD`, `TASK_GO_STRAIGHT_TO_COORD` | movement only |
| `go_to(entity)` | `TASK_GOTO_ENTITY_OFFSET_XY`, `TASK_GO_TO_ENTITY` | movement only |
| `follow(entity)` | `TASK_FOLLOW_TO_OFFSET_OF_ENTITY`, `TASK_GOTO_ENTITY_OFFSET_XY` | movement only |
| `stop()` | `CLEAR_PED_TASKS`, `CLEAR_PED_TASKS_IMMEDIATELY`, `CLEAR_PED_SECONDARY_TASK`, `TASK_PAUSE`, `TASK_STAND_STILL` | stops gesture/animation |
| `wait(duration)` | `TASK_PAUSE`, `TASK_STAND_STILL` | no |
| `look_at(entity)` | `TASK_LOOK_AT_ENTITY` | head/eye gesture |
| `look_at(position)` | `TASK_LOOK_AT_COORD` | head/eye gesture |
| `clear_attention()` | `TASK_CLEAR_LOOK_AT` | releases attention |
| `face(entity)` | `TASK_TURN_PED_TO_FACE_ENTITY`, `TASK_ACHIEVE_HEADING` | body/head gesture |
| `face(position)` | `TASK_TURN_PED_TO_FACE_COORD`, `TASK_ACHIEVE_HEADING` | body/head gesture |
| `say(text)` | `PLAY_PED_AMBIENT_SPEECH_NATIVE`, scripted speech batch | voice + mouth animation |
| `gesture(type)` | `TASK_PLAY_ANIM`, `TASK_PLAY_ANIM_ADVANCED`, `TASK_PLAY_EMOTE_WITH_HASH`, scenarios | **yes** |
| `investigate(position)` | `TASK_GO_TO_COORD_ANY_MEANS` + `TASK_LOOK_AT_COORD` + `TASK_REACT` | movement + reaction |
| `flee_from(entity)` | `TASK_FLEE_PED`, `TASK_SMART_FLEE_PED`, `TASK_WALK_AWAY` | fleeing animation |
| `react_entity` | `TASK_REACT`, `TASK_SHOCKING_EVENT_REACT` | reaction animation |

Full hashes/params/comments are in `bridge/native_action_map.json`.

---

## 3. Actions that make the NPC perform gestures / animations

### 3.1 Generic animation — recommended default

`TASK_PLAY_ANIM` — `0xEA47FE3719165B94`

```text
Ped ped, const char* animDict, const char* animName,
float speed, float speedMultiplier, int duration, int flags,
float playbackRate, BOOL p8, int ikFlags, BOOL p10,
const char* taskFilter, BOOL p12
```

Useful flags from `eScriptedAnimFlags`:

```text
Looping          = 1 << 0
HoldLastFrame    = 1 << 1
NotInterruptable = 1 << 2
Upperbody        = 1 << 3
Secondary        = 1 << 4
AbortOnPedMovement = 1 << 5
Additive         = 1 << 6
Gesture          = 1 << 22
```

Also available:

- `TASK_PLAY_ANIM_ADVANCED` — `0x83CDB10EA29B370B` (position/rotation-aware)
- `TASK_SCRIPTED_ANIMATION` — `0x126EF75F1E17ABE5` (struct args)

Animation dicts must be streamed first:

- `REQUEST_ANIM_DICT` — `0xA862A2AD321F94B4`
- `HAS_ANIM_DICT_LOADED` — `0x27FF6FE8009B40CA`
- `REMOVE_ANIM_DICT` — `0x4763145053A33D46`

Useful dictionaries discovered in `femga/rdr3_discoveries`:

```text
ai_gestures@script_story@ridentalk
  positive_nodding_001..005
  negative_headshake_001..004
  positive_shrug_001..002
  positive_punctuate_001..004
  concerned_001
  sad_head_down_001

g_speak_talk_*
  g_speak_talk_head_enter
  g_speak_talk_lhand_enter
  g_speak_talk_rhand_enter
  g_speak_talk_lhand_soft_exit
  g_speak_urgent_lhand_enter
```

### 3.2 Emotes

`TASK_PLAY_EMOTE_WITH_HASH` — `0xB31A277C1AC7B7FF`

```text
Ped ped, int emoteType, int playbackMode, Hash emote,
BOOL isSecondaryTask, BOOL canBreakOut, BOOL disableEarlyOutAnimTag,
BOOL ignoreInvalidMainTask, BOOL destroyProps
```

- `_TASK_PLAY_EMOTE` — `0x884E3436CC1F41DD`
  Similar but checks whether the ped's inventory contains the emote kit.
- `_TASK_EMOTE_ACTION` — `0x6A1AF481407BF6E9`
  Fires the "action / flourish" sub-clip of the current emote.
- `_TASK_EMOTE_OUTRO` — `0xBDFEEB7600BCD938`
- `IS_EMOTE_TASK_RUNNING` — `0xCF9B71C0AF824036`

`eEmoteType` categories:

```text
-1 INVALID
 0 REACT
 1 ACTION
 2 TAUNT
 3 GREET
 4 TWIRL_GUN
 5 DANCE
```

Example `KIT_EMOTE_*` entries from `femga/rdr3_discoveries`:

| Emote | Hash | Category |
|---|---:|---|
| `KIT_EMOTE_GREET_HAT_TIP_1` | `0xA927A00F` | Greet |
| `KIT_EMOTE_GREET_SUBTLE_WAVE_1` | `0xA38D1E64` | Greet |
| `KIT_EMOTE_GREET_HAND_SHAKE_1` | `0x6A662B8A` | Greet |
| `KIT_EMOTE_ACTION_POINT_1` | `0x1CFB34E2` | Action |
| `KIT_EMOTE_ACTION_BECKON_1` | `0x7FC09D55` | Action |
| `KIT_EMOTE_ACTION_LOOK_YONDER_1` | `0x0078D3CC` | Action |
| `KIT_EMOTE_REACTION_NOD_HEAD_1` | `0xCEF7AA76` | Reaction |
| `KIT_EMOTE_REACTION_SHRUG_1` | `0x2E097BB5` | Reaction |
| `KIT_EMOTE_REACTION_SCARED_1` | `0xB1C3DE80` | Reaction |
| `KIT_EMOTE_TAUNT_FLIP_OFF_1` | `0x39C68938` | Taunt |
| `KIT_EMOTE_TAUNT_WAR_CRY_1` | `0x3AD8141A` | Taunt |
| `KIT_EMOTE_DANCE_CAREFREE_A_1` | `0xF0AF179A` | Dance |
| `KIT_EMOTE_TWIRL_GUN_DUAL` | `0xE04E36A5` | TwirlGun |

Caveat: emotes are designed around player kits. For ambient NPCs, `TASK_PLAY_ANIM`
with a suitable animation dict + `Gesture`/`Upperbody` flags is often safer.

### 3.3 Scenario / ambient gestures

- `TASK_START_SCENARIO_IN_PLACE_HASH` — `0x524B54361229154F`
- `TASK_START_SCENARIO_AT_POSITION` — `0x4D1F61FC34AF3CD1`
- `TASK_USE_RANDOM_SCENARIO_IN_GROUP` — `0x14747F4A5971DE4E`
- `TASK_USE_SCENARIO_POINT` — `0xCCDAE6324B6A821C`

Scenarios are conditional animation containers (props, conditions, transitions).
They are ideal for "ambient NPC doing something" (leaning, smoking, sitting,
working, warming hands) without hand-authoring exact animation clips.
See `femga/rdr3_discoveries/animations/scenarios/`.

### 3.4 Body/attention gestures

- `TASK_LOOK_AT_ENTITY` — `0x69F4BE8C8CC4796C`
- `TASK_LOOK_AT_COORD` — `0x6FA46612594F7973`
- `TASK_CLEAR_LOOK_AT` — `0x0F804F1DB19B9689`
- `TASK_TURN_PED_TO_FACE_ENTITY` — `0x5AD23D40115353AC`
- `TASK_TURN_PED_TO_FACE_COORD` — `0x1DDA930A0AC38571`
- `TASK_ACHIEVE_HEADING` — `0x93B93A37987F1F3D`

These make the NPC visibly look/turn toward the player or a point, but do not
play a full-body emote.

### 3.5 Pose / reaction / panic gestures

- `TASK_HANDS_UP` — `0xF2EAB31979A7F910`
- `TASK_COWER` — `0x3EB1FE9E8E908E15`
- `TASK_DUCK` — `0xA14B5FBF986BAC23`
- `TASK_JUMP` — `0x0AE4086104E067B1`
- `TASK_KNOCKED_OUT` — `0xF90427F00A495A28`
- `TASK_KNOCKED_OUT_AND_HOGTIED` — `0x42AC6401ABB8C7E5`
- `TASK_REACT` — `0xC4C32C31920E1B70`
- `TASK_SHOCKING_EVENT_REACT` — `0x452419CBD838065B`
- `TASK_FLEE_PED` — `0xFD45175A6DFD7CE9`
- `TASK_FLEE_COORD` — `0x58428248BF4B64E4`
- `TASK_SMART_FLEE_PED` — `0x22B0D0E37CCB840D`
- `TASK_SMART_FLEE_COORD` — `0x94587F17E9C365D5`
- `TASK_WALK_AWAY` — `0x04ACFAC71E6858F9`

### 3.6 Speech / talking animation

- `PLAY_PED_AMBIENT_SPEECH_NATIVE` — `0x8E04FEDD28D42462`
  Takes `ScriptedSpeechParams` struct: speech name, voice name, variation,
  speech param hash, listener ped, network sync flags.
- `_CREATE_NEW_SCRIPTED_PED_AMBIENT_SPEECH` — `0x72E4D1C4639BC465`
- `_PLAY_SOUND_FROM_SCRIPTED_PED_AMBIENT_SPEECH` — `0xB18FEC133C7C6C69`
- `STOP_CURRENT_PLAYING_SPEECH` — `0x79D2F0E66F81D90D` (barge-in)
- `SET_AMBIENT_VOICE_NAME` — `0x6C8065A3B780185B`

Most generic talking/gesture animation can still come from `TASK_PLAY_ANIM`;
the speech natives only handle the voice line, not arbitrary body animation.

### 3.7 Suggested gesture typed mapping

`bridge/native_action_map.json` contains a `gesture_map` for the runtime
`gesture(type)` tool. Initial candidates:

| `gesture(type)` | Preferred primitive |
|---|---|
| `neutral` | `TASK_PLAY_ANIM("g_speak_talk", "g_speak_talk_head_enter")` |
| `friendly` | `ai_gestures@script_story@ridentalk` / `positive_nodding_001` or `KIT_EMOTE_GREET_HAND_SHAKE_1` |
| `annoyed` | `negative_headshake_001` or `KIT_EMOTE_REACTION_WAG_FINGER_1` |
| `afraid` | `TASK_COWER` or `KIT_EMOTE_REACTION_SCARED_1` |
| `wave` | `KIT_EMOTE_GREET_SUBTLE_WAVE_1` or `g_speak_talk_rhand_enter` |
| `nod` | `positive_nodding_001` or `KIT_EMOTE_REACTION_NOD_HEAD_1` |
| `shrug` | `positive_shrug_001` or `KIT_EMOTE_REACTION_SHRUG_1` |
| `think` | `ai_gestures@gen_male@standing@silent` / `aknwoledge_tough_chin_scratch_l_001`, or `KIT_EMOTE_ACTION_IDEA_1` |
| `ponder` | `aknwoledge_timid_look_down_f_001`, or `ai_gestures@script_story@ridentalk` / `concerned_001` |
| `scheme` | `KIT_EMOTE_ACTION_SCHEME_1`, or `script_mp@emotes@scheme@male@unarmed@upper` / `loop` |

### 3.7.1 "Thinking" animation candidates

Thinking poses exist in the game animation data:

- **Chin scratch / pondering**
  - `TASK_PLAY_ANIM`
  - dict: `ai_gestures@gen_male@standing@silent`
  - dict: `ai_gestures@gen_female@standing@silent`
  - clips:
    - `aknwoledge_tough_chin_scratch_l_001`
    - `aknwoledge_tough_chin_scratch_r_001`
    - `aknwoledge_timid_look_down_f_001`
    - `aknwoledge_tough_inspect_f_001`
  - suggested flags: `Gesture | Upperbody | NotInterruptable`
- **Idea emote**
  - `TASK_PLAY_EMOTE_WITH_HASH`
  - `KIT_EMOTE_ACTION_IDEA_1` = `0xEEC55CB7`
  - `emoteType = 1` (ACTION)
  - anim dict fallback: `script_mp@emotes@idea@male@unarmed@upper`
- **Scheme / plotting emote**
  - `KIT_EMOTE_ACTION_SCHEME_1` = `0x2322C484`
  - `emoteType = 1` (ACTION)
  - anim dict fallback: `script_mp@emotes@scheme@male@unarmed@upper`
- **Facial-only thinking**
  - `mini_games@dominoes@...` clips `face_think`, `face_think_extreme`

So yes: the API can express a thinking gesture, but for ambient NPCs the
safest first implementation is `TASK_PLAY_ANIM` with a chin-scratch/ponder
clip and the `Gesture | Upperbody` flags.

### 3.8 Gesture control

- `SET_PED_CAN_PLAY_GESTURE_ANIMS` — `0xBAF20C5432058024`
- `SET_PED_GESTURE_GROUP` — `0xDDF803377F94AAA8`

Use these to enable/disable gesture animations or assign a gesture styleset.

---

## 3.9 Waiting / thinking overlay

The runtime now emits a transient `think` action while a slow LLM/TTS
generation is in progress. It is a soft overlay and does not block idle
planning. Style selection:

| Trigger | Wait gesture style | Suggested native |
|---|---|---|
| `PLAYER_SPOKE` / Push-to-Talk | `listen` | `TASK_PLAY_ANIM("g_speak_talk_head_enter")` or `positive_nodding_001` |
| `PLAYER_APPROACHED` / `PLAYER_LOOKED_AT_NPC` | `think` | chin scratch / `TASK_PLAY_ANIM` |
| `GUNSHOT_HEARD` / threat / damage / fight | `alert` | `TASK_LOOK_AT_ENTITY` / `TASK_TURN_PED_TO_FACE_ENTITY` |
| `action_failed` | `ponder` | `aknwoledge_timid_look_down_f_001` |
| idle autonomous turn | `ponder` | chin scratch / `scheme` |

The bridge receives:

```json
{"type":"action_request","npc_id":"npc_001","request":{
  "tool":"think","request_id":"act_...","npc_id":"npc_001",
  "arguments":{"style":"listen","duration":2.5}
}}
```

A subsequent real action (`say`, `go_to`, `flee_from`, etc.) cancels or
replaces the wait gesture.

## 3.10 Agent action catalog

All registered high-level tools now have entries in `bridge/native_action_map.json`
under `tool_map`:

```text
get_world_state, grab_timeline, get_world_lore,
wander, go_to, follow, stop, wait,
look_at, face, clear_attention, say, gesture, think,
investigate, flee_from, walk_away, react,
hands_up, cower, duck, jump,
mount, dismount, item_interaction, animal_interaction, horse_action,
aim_at, shoot_at, attack, take_cover
```

The LLM sees these names in `AVAILABLE TOOLS`; it is still forbidden to emit
raw native hashes.

## 4. Recommended bridge implementation order

1. `say` → speech native + stop for barge-in.
2. `look_at` / `face` / `clear_attention` → look/turn natives.
3. `wander`, `go_to`, `follow`, `stop`, `wait`, `flee_from`, `investigate`
   → TASK movement natives above.
4. `gesture`:
   - start with a small curated map such as
     `nod → TASK_PLAY_ANIM("ai_gestures@script_story@ridentalk", "positive_nodding_001")`
   - extend to scenarios and `KIT_EMOTE_*` later.
5. Only then add deferred combat/vehicle/item interaction tools.

---

## 5. Safety notes

- Native hashes in the DB are build-specific; test against the target RDR2 build.
- Scenarios/emotes may fail if conditions/props/inventory are not satisfied.
- Animation dictionaries must be streamed before `TASK_PLAY_ANIM`.
- Never expose raw native hashes to the LLM; keep the high-level tool abstraction.
- Story/mission/cutscene safety still overrides every animation/action.
