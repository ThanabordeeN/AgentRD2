package domain

// WorldState is the normalised snapshot the bridge sends for one NPC.
type WorldState struct {
	NPCID            string           `json:"npc_id"`
	SelfState        map[string]any   `json:"self_state"`
	Player           map[string]any   `json:"player"`
	NearbyPeds       []map[string]any `json:"nearby_peds"`
	NearbyHorses     []map[string]any `json:"nearby_horses"`
	RecentGameEvents []map[string]any `json:"recent_game_events"`
	Timestamp        float64          `json:"timestamp"`
	Raw              map[string]any   `json:"raw"`
}

// ToMap mirrors “WorldState.to_dict“ (note the “self“ key name).
func (w WorldState) ToMap() map[string]any {
	return map[string]any{
		"self":               nonNilMap(w.SelfState),
		"player":             nonNilMap(w.Player),
		"nearby_peds":        nonNilMaps(w.NearbyPeds),
		"nearby_horses":      nonNilMaps(w.NearbyHorses),
		"recent_game_events": nonNilMaps(w.RecentGameEvents),
		"timestamp":          w.Timestamp,
	}
}

// WorldStateFromBridgePayload mirrors “WorldState.from_bridge_payload“.
func WorldStateFromBridgePayload(npcID string, payload map[string]any) WorldState {
	state := MapFrom(payload["state"])
	timestamp := FloatFrom(payload["timestamp"])
	if timestamp == 0 {
		timestamp = Timestamp()
	}
	return WorldState{
		NPCID:            npcID,
		Timestamp:        timestamp,
		SelfState:        MapFrom(state["self"]),
		Player:           MapFrom(state["player"]),
		NearbyPeds:       MapsFrom(state["nearby_peds"]),
		NearbyHorses:     MapsFrom(state["nearby_horses"]),
		RecentGameEvents: MapsFrom(state["recent_game_events"]),
		Raw:              payload,
	}
}

// PedSnapshot is the subset of a ped used by the conservative eligibility
// gate. A nil boolean means "the bridge did not provide a reliable value";
// the gate fails closed on any uncertain safety field.
type PedSnapshot struct {
	EntityID         string         `json:"entity_id"`
	Model            string         `json:"model"`
	Name             string         `json:"name"`
	IsPed            *bool          `json:"is_ped"`
	IsHuman          *bool          `json:"is_human"`
	IsAlive          *bool          `json:"is_alive"`
	IsPlayer         *bool          `json:"is_player"`
	IsStoryCharacter *bool          `json:"is_story_character"`
	IsMissionOwned   *bool          `json:"is_mission_owned"`
	InScriptedState  *bool          `json:"in_scripted_state"`
	InCutscene       *bool          `json:"in_cutscene"`
	Blacklisted      *bool          `json:"blacklisted"`
	DistanceM        *float64       `json:"distance_m"`
	Visible          bool           `json:"visible"`
	Health           *float64       `json:"health"`
	Metadata         map[string]any `json:"metadata"`
}

// ToMap mirrors dataclasses.asdict: every field is emitted.
func (p PedSnapshot) ToMap() map[string]any {
	return map[string]any{
		"entity_id":          p.EntityID,
		"model":              p.Model,
		"name":               p.Name,
		"is_ped":             p.IsPed,
		"is_human":           p.IsHuman,
		"is_alive":           p.IsAlive,
		"is_player":          p.IsPlayer,
		"is_story_character": p.IsStoryCharacter,
		"is_mission_owned":   p.IsMissionOwned,
		"in_scripted_state":  p.InScriptedState,
		"in_cutscene":        p.InCutscene,
		"blacklisted":        p.Blacklisted,
		"distance_m":         p.DistanceM,
		"visible":            p.Visible,
		"health":             p.Health,
		"metadata":           nonNilMap(p.Metadata),
	}
}

// PedSnapshotFromMap mirrors “PedSnapshot.from_dict“: unknown keys are
// ignored rather than rejected.
func PedSnapshotFromMap(value map[string]any) PedSnapshot {
	return PedSnapshot{
		EntityID:         StringFrom(value["entity_id"]),
		Model:            StringFrom(value["model"]),
		Name:             StringFrom(value["name"]),
		IsPed:            BoolPtrFrom(value["is_ped"]),
		IsHuman:          BoolPtrFrom(value["is_human"]),
		IsAlive:          BoolPtrFrom(value["is_alive"]),
		IsPlayer:         BoolPtrFrom(value["is_player"]),
		IsStoryCharacter: BoolPtrFrom(value["is_story_character"]),
		IsMissionOwned:   BoolPtrFrom(value["is_mission_owned"]),
		InScriptedState:  BoolPtrFrom(value["in_scripted_state"]),
		InCutscene:       BoolPtrFrom(value["in_cutscene"]),
		Blacklisted:      BoolPtrFrom(value["blacklisted"]),
		DistanceM:        FloatPtrFrom(value["distance_m"]),
		Visible:          BoolFrom(value["visible"]),
		Health:           FloatPtrFrom(value["health"]),
		Metadata:         MapFrom(value["metadata"]),
	}
}

// BoolFrom coerces a decoded JSON value to bool (false when absent).
func BoolFrom(value any) bool {
	if ptr := BoolPtrFrom(value); ptr != nil {
		return *ptr
	}
	return false
}

func nonNilMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func nonNilMaps(value []map[string]any) []map[string]any {
	if value == nil {
		return []map[string]any{}
	}
	return value
}
