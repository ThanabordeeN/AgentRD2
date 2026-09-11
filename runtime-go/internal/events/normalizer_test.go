package events

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/project"
)

// normalizer is shorthand for the stateless normalizer used across the tests.
func normalizer() *Normalizer { return NewNormalizer() }

// TestWorldStateFromMessage mirrors the Python normalizer test and covers the
// missing-npc_id error.
func TestWorldStateFromMessage(t *testing.T) {
	normalizer := NewNormalizer()

	state, err := normalizer.WorldStateFromMessage(map[string]any{
		"type":      "world_update",
		"npc_id":    "npc_1",
		"timestamp": 123.0,
		"state": map[string]any{
			"self":         map[string]any{"health": 82},
			"player":       map[string]any{"distance": 4.1},
			"nearby_peds":  []any{},
			"nearby_horse": []any{},
		},
	})
	if err != nil {
		t.Fatalf("WorldStateFromMessage: %v", err)
	}
	if state.NPCID != "npc_1" {
		t.Fatalf("npc_id = %q", state.NPCID)
	}
	if fmt.Sprint(state.SelfState["health"]) != "82" {
		t.Fatalf("self_state = %+v", state.SelfState)
	}
	if fmt.Sprint(state.Player["distance"]) != "4.1" {
		t.Fatalf("player = %+v", state.Player)
	}
	if state.Timestamp != 123.0 {
		t.Fatalf("timestamp = %v, want 123", state.Timestamp)
	}
	if state.Raw["type"] != "world_update" {
		t.Fatalf("raw payload was not preserved: %+v", state.Raw)
	}

	for _, raw := range []map[string]any{
		nil,
		{"type": "world_update"},
		{"npc_id": ""},
		{"npc_id": nil},
		{"npc_id": 0},
		{"npc_id": false},
		{"npc_id": []any{}},
		{"npc_id": map[string]any{}},
	} {
		if _, err := normalizer.WorldStateFromMessage(raw); err == nil {
			t.Fatalf("WorldStateFromMessage(%v) did not fail", raw)
		} else if !strings.Contains(err.Error(), "missing npc_id") {
			t.Fatalf("error = %v, want a missing npc_id error", err)
		}
	}
}

// TestEventFromMessageCanonical mirrors the Python canonicalisation test.
func TestEventFromMessageCanonical(t *testing.T) {
	normalizer := NewNormalizer()

	event, err := normalizer.EventFromMessage(map[string]any{
		"type":       "game_event",
		"npc_id":     "npc_1",
		"event_name": "player_threatened_npc",
		"entities":   "player",
		"tags":       "threat",
		"data":       map[string]any{"weapon": "revolver", "distance": 2.6},
		"importance": 1.5,
	}, 7, "")
	if err != nil {
		t.Fatalf("EventFromMessage: %v", err)
	}

	if event.Seq != 7 {
		t.Fatalf("seq = %d, want 7", event.Seq)
	}
	if event.EventName != "PLAYER_THREATENED_NPC" {
		t.Fatalf("event_name = %q", event.EventName)
	}
	if fmt.Sprint(event.Entities) != "[player]" {
		t.Fatalf("entities = %v", event.Entities)
	}
	if fmt.Sprint(event.Tags) != "[threat]" {
		t.Fatalf("tags = %v", event.Tags)
	}
	if event.Importance == nil || *event.Importance != 1.0 {
		t.Fatalf("importance = %v, want 1.0", event.Importance)
	}
	if event.Data["weapon"] != "revolver" {
		t.Fatalf("data = %+v", event.Data)
	}
	if event.NPCID != "npc_1" {
		t.Fatalf("npc_id = %q", event.NPCID)
	}
	if event.EventID == "" || !strings.HasPrefix(event.EventID, "evt_") {
		t.Fatalf("event_id = %q, want a generated evt_ id", event.EventID)
	}
	if event.Timestamp <= 0 {
		t.Fatalf("timestamp = %v, want a generated timestamp", event.Timestamp)
	}
}

// TestEventFromMessageAcceptsUppercaseAndNameAlias covers the `name` fallback
// and the already-canonical spelling.
func TestEventFromMessageAcceptsUppercaseAndNameAlias(t *testing.T) {
	normalizer := NewNormalizer()

	tests := []struct {
		name string
		raw  map[string]any
		want string
	}{
		{"already uppercase", map[string]any{"npc_id": "npc_1", "event_name": "NPC_SPOKE"}, "NPC_SPOKE"},
		{"name alias", map[string]any{"npc_id": "npc_1", "name": "gunshot_heard"}, "GUNSHOT_HEARD"},
		{"empty event_name falls back to name", map[string]any{"npc_id": "npc_1", "event_name": "", "name": "player_spoke"}, "PLAYER_SPOKE"},
		{"digits and underscores", map[string]any{"npc_id": "npc_1", "event_name": "EVENT_2"}, "EVENT_2"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event, err := normalizer.EventFromMessage(test.raw, 1, "")
			if err != nil {
				t.Fatalf("EventFromMessage: %v", err)
			}
			if event.EventName != test.want {
				t.Fatalf("event_name = %q, want %q", event.EventName, test.want)
			}
		})
	}
}

// TestEventFromMessageValidation is the table of every rejection rule.
func TestEventFromMessageValidation(t *testing.T) {
	normalizer := NewNormalizer()
	tests := []struct {
		name   string
		raw    map[string]any
		seq    int
		def    string
		wantIn string
	}{
		{"missing npc_id", map[string]any{"event_name": "A_EVENT"}, 1, "", "missing npc_id"},
		{"empty npc_id with no default", map[string]any{"npc_id": "", "event_name": "A_EVENT"}, 1, "", "missing npc_id"},
		{"missing event_name", map[string]any{"npc_id": "npc_1"}, 1, "", "missing event_name"},
		{"spaces in name", map[string]any{"npc_id": "npc_1", "event_name": "player is evil"}, 1, "", "invalid event_name"},
		{"leading digit", map[string]any{"npc_id": "npc_1", "event_name": "1PLAYER_SPOKE"}, 1, "", "invalid event_name"},
		{"dash in name", map[string]any{"npc_id": "npc_1", "event_name": "PLAYER-SPOKE"}, 1, "", "invalid event_name"},
		{"space in name", map[string]any{"npc_id": "npc_1", "event_name": "PLAYER SPOKE"}, 1, "", "invalid event_name"},
		{"data is a list", map[string]any{"npc_id": "npc_1", "event_name": "A_EVENT", "data": []any{1}}, 1, "", "data must be an object"},
		{"data is a string", map[string]any{"npc_id": "npc_1", "event_name": "A_EVENT", "data": "text"}, 1, "", "data must be an object"},
		{"data is a number", map[string]any{"npc_id": "npc_1", "event_name": "A_EVENT", "data": 5}, 1, "", "data must be an object"},
		{"entities is a number", map[string]any{"npc_id": "npc_1", "event_name": "A_EVENT", "entities": 5}, 1, "", "entities"},
		{"tags is a number", map[string]any{"npc_id": "npc_1", "event_name": "A_EVENT", "tags": 5}, 1, "", "tags"},
		{"game_time is a list", map[string]any{"npc_id": "npc_1", "event_name": "A_EVENT", "game_time": []any{1}}, 1, "", "game_time must be an object"},
		{"location is a string", map[string]any{"npc_id": "npc_1", "event_name": "A_EVENT", "location": "Valentine"}, 1, "", "location must be an object"},
		{"importance is not numeric", map[string]any{"npc_id": "npc_1", "event_name": "A_EVENT", "importance": "high"}, 1, "", "invalid importance"},
		{"seq below one", map[string]any{"npc_id": "npc_1", "event_name": "A_EVENT"}, 0, "", "seq must be >= 1"},
		{"negative seq", map[string]any{"npc_id": "npc_1", "event_name": "A_EVENT"}, -3, "", "seq must be >= 1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := normalizer.EventFromMessage(test.raw, test.seq, test.def); err == nil {
				t.Fatalf("EventFromMessage(%v) did not fail", test.raw)
			} else if !strings.Contains(err.Error(), test.wantIn) {
				t.Fatalf("error = %v, want containing %q", err, test.wantIn)
			}
		})
	}
}

// TestEventFromMessageDefaultNPCID covers the default_npc_id fallback.
func TestEventFromMessageDefaultNPCID(t *testing.T) {
	normalizer := NewNormalizer()

	event, err := normalizer.EventFromMessage(
		map[string]any{"event_name": "A_EVENT"},
		3,
		"npc_default",
	)
	if err != nil {
		t.Fatalf("EventFromMessage: %v", err)
	}
	if event.NPCID != "npc_default" {
		t.Fatalf("npc_id = %q, want npc_default", event.NPCID)
	}
	if event.Seq != 3 {
		t.Fatalf("seq = %d, want 3", event.Seq)
	}

	// An explicit payload id still wins over the default.
	event, err = normalizer.EventFromMessage(
		map[string]any{"npc_id": "npc_payload", "event_name": "A_EVENT"},
		1,
		"npc_default",
	)
	if err != nil {
		t.Fatalf("EventFromMessage: %v", err)
	}
	if event.NPCID != "npc_payload" {
		t.Fatalf("npc_id = %q, want npc_payload", event.NPCID)
	}
}

// TestEventFromMessageOptionalFields covers truthiness rules, string-to-list
// coercion, and the optional payload fields.
func TestEventFromMessageOptionalFields(t *testing.T) {
	normalizer := NewNormalizer()

	event, err := normalizer.EventFromMessage(map[string]any{
		"npc_id":     "npc_1",
		"event_name": "PLAYER_HELPED_NPC",
		"entities":   []any{"player", "npc_1"},
		"tags":       []any{"player", 7},
		"data":       map[string]any{"verb": "help", "empty": nil},
		"importance": 0.25,
		"timestamp":  1700000000.5,
		"event_id":   "evt_fixed",
		"summary":    "held the door",
		"game_time":  map[string]any{"day": 2, "hour": 9, "minute": 30},
		"location":   map[string]any{"region": "Valentine", "position": []any{1.5, 2.0}},
	}, 4, "")
	if err != nil {
		t.Fatalf("EventFromMessage: %v", err)
	}

	if fmt.Sprint(event.Entities) != "[player npc_1]" {
		t.Fatalf("entities = %v", event.Entities)
	}
	if fmt.Sprint(event.Tags) != "[player 7]" {
		t.Fatalf("tags = %v", event.Tags)
	}
	if event.Importance == nil || *event.Importance != 0.25 {
		t.Fatalf("importance = %v", event.Importance)
	}
	if event.Timestamp != 1700000000.5 {
		t.Fatalf("timestamp = %v", event.Timestamp)
	}
	if event.EventID != "evt_fixed" {
		t.Fatalf("event_id = %q", event.EventID)
	}
	if event.Summary == nil || *event.Summary != "held the door" {
		t.Fatalf("summary = %v", event.Summary)
	}
	if event.GameTime == nil || event.GameTime.Day != 2 || event.GameTime.Hour != 9 || event.GameTime.Minute != 30 {
		t.Fatalf("game_time = %+v", event.GameTime)
	}
	if event.Location == nil || event.Location.Region == nil || *event.Location.Region != "Valentine" {
		t.Fatalf("location = %+v", event.Location)
	}
	if len(event.Location.Position) != 2 || event.Location.Position[0] != 1.5 {
		t.Fatalf("position = %v", event.Location.Position)
	}
}

// TestEventFromMessageFalsyOptionalValues mirrors Python's `x or default`
// shortcuts for every empty container.
func TestEventFromMessageFalsyOptionalValues(t *testing.T) {
	normalizer := NewNormalizer()
	tests := []struct {
		name string
		raw  map[string]any
	}{
		{"nil data", map[string]any{"npc_id": "npc_1", "event_name": "A_EVENT", "data": nil}},
		{"empty data list", map[string]any{"npc_id": "npc_1", "event_name": "A_EVENT", "data": []any{}}},
		{"empty data string", map[string]any{"npc_id": "npc_1", "event_name": "A_EVENT", "data": ""}},
		{"empty entities", map[string]any{"npc_id": "npc_1", "event_name": "A_EVENT", "entities": []any{}}},
		{"empty tags list", map[string]any{"npc_id": "npc_1", "event_name": "A_EVENT", "tags": []any{}}},
		{"empty game_time", map[string]any{"npc_id": "npc_1", "event_name": "A_EVENT", "game_time": map[string]any{}}},
		{"empty location", map[string]any{"npc_id": "npc_1", "event_name": "A_EVENT", "location": map[string]any{}}},
		{"null importance", map[string]any{"npc_id": "npc_1", "event_name": "A_EVENT", "importance": nil}},
		{"zero timestamp", map[string]any{"npc_id": "npc_1", "event_name": "A_EVENT", "timestamp": 0}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event, err := normalizer.EventFromMessage(test.raw, 1, "")
			if err != nil {
				t.Fatalf("EventFromMessage: %v", err)
			}
			if event.Data == nil || len(event.Data) != 0 {
				t.Fatalf("data = %+v, want an empty object", event.Data)
			}
			if event.Entities == nil || len(event.Entities) != 0 {
				t.Fatalf("entities = %v, want an empty list", event.Entities)
			}
			if event.Tags == nil || len(event.Tags) != 0 {
				t.Fatalf("tags = %v, want an empty list", event.Tags)
			}
			if event.GameTime != nil || event.Location != nil {
				t.Fatalf("game_time/location = %+v/%+v, want nil", event.GameTime, event.Location)
			}
			if event.Importance != nil {
				t.Fatalf("importance = %v, want nil", event.Importance)
			}
		})
	}
}

// TestEventFromMessageTrailingNewlineName pins the Python regex nuance: Python's
// `$` also matches before one trailing newline, and the Go port accepts the
// same language so a name both runtimes accept cannot diverge.
func TestEventFromMessageTrailingNewlineName(t *testing.T) {
	normalizer := NewNormalizer()

	event, err := normalizer.EventFromMessage(map[string]any{
		"npc_id":     "npc_1",
		"event_name": "player_spoke\n",
	}, 1, "")
	if err != nil {
		t.Fatalf("EventFromMessage: %v", err)
	}
	if event.EventName != "PLAYER_SPOKE\n" {
		t.Fatalf("event_name = %q", event.EventName)
	}

	if _, err := normalizer.EventFromMessage(map[string]any{
		"npc_id":     "npc_1",
		"event_name": "PLAYER_SPOKE\n\n",
	}, 1, ""); err == nil {
		t.Fatal("a double trailing newline was accepted")
	}
}

// TestPlayerSpokeAndNPCSpoke mirrors the standard fact events.
func TestPlayerSpokeAndNPCSpoke(t *testing.T) {
	normalizer := NewNormalizer()

	player := normalizer.PlayerSpoke("npc_1", "Where are you headed?", nil)
	if player["event_name"] != "PLAYER_SPOKE" || player["npc_id"] != "npc_1" {
		t.Fatalf("player_spoke = %+v", player)
	}
	if fmt.Sprint(player["entities"]) != "[player]" || fmt.Sprint(player["tags"]) != "[player speech]" {
		t.Fatalf("player_spoke entities/tags = %v/%v", player["entities"], player["tags"])
	}
	playerData, ok := player["data"].(map[string]any)
	if !ok || playerData["text"] != "Where are you headed?" {
		t.Fatalf("player_spoke data = %+v", player["data"])
	}

	npc := normalizer.NPCSpoke("npc_1", "Valentine.", map[string]any{"emotion": "calm"})
	if fmt.Sprint(npc["entities"]) != "[npc_1]" || fmt.Sprint(npc["tags"]) != "[npc speech]" {
		t.Fatalf("npc_spoke entities/tags = %v/%v", npc["entities"], npc["tags"])
	}
	npcData, ok := npc["data"].(map[string]any)
	if !ok || npcData["text"] != "Valentine." || npcData["emotion"] != "calm" {
		t.Fatalf("npc_spoke data = %+v", npc["data"])
	}
}

// TestSpokeExtraOverridesText checks Python's `{"text": text, **data}` order.
func TestSpokeExtraOverridesText(t *testing.T) {
	normalizer := NewNormalizer()

	payload := normalizer.PlayerSpoke("npc_1", "original", map[string]any{"text": "override", "target": "npc_1"})
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("data = %+v", payload["data"])
	}
	if data["text"] != "override" {
		t.Fatalf("text = %v, want the extra value to win", data["text"])
	}
	if data["target"] != "npc_1" {
		t.Fatalf("target = %v", data["target"])
	}
}

// TestActionResultMapsStatus mirrors the Python status mapping test and covers
// the field filtering rules.
func TestActionResultMapsStatus(t *testing.T) {
	normalizer := NewNormalizer()
	tests := []struct {
		name   string
		raw    map[string]any
		want   string
		npcID  any
		absent []string
	}{
		{
			name: "failed",
			raw: map[string]any{
				"type": "action_result", "npc_id": "npc_1", "status": "failed",
				"tool": "go_to", "request_id": "act_1", "reason": "path_unreachable",
			},
			want: "ACTION_FAILED", npcID: "npc_1",
			absent: []string{"type", "npc_id", "status"},
		},
		{
			name: "completed",
			raw: map[string]any{
				"type": "action_result", "npc_id": "npc_1", "status": "COMPLETED",
				"tool": "say", "request_id": "act_2",
			},
			want: "ACTION_COMPLETED", npcID: "npc_1",
			absent: []string{"type", "npc_id", "status"},
		},
		{
			name: "started by default",
			raw: map[string]any{
				"type": "action_result", "npc_id": "npc_1", "status": "started",
				"tool": "wait", "request_id": "act_3",
			},
			want: "ACTION_STARTED", npcID: "npc_1",
			absent: []string{"type", "npc_id", "status"},
		},
		{
			name: "unknown status",
			raw: map[string]any{
				"npc_id": "npc_1", "status": "weird", "tool": "wait", "request_id": "act_4",
			},
			want: "ACTION_STARTED", npcID: "npc_1",
		},
		{
			name: "missing status",
			raw:  map[string]any{"npc_id": "npc_1", "tool": "wait"},
			want: "ACTION_STARTED", npcID: "npc_1",
		},
		{
			name: "missing npc id",
			raw:  map[string]any{"status": "failed", "tool": "wait"},
			want: "ACTION_FAILED", npcID: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := normalizer.ActionResult(test.raw)
			if payload["event_name"] != test.want {
				t.Fatalf("event_name = %v, want %v", payload["event_name"], test.want)
			}
			if fmt.Sprint(payload["npc_id"]) != fmt.Sprint(test.npcID) {
				t.Fatalf("npc_id = %v, want %v", payload["npc_id"], test.npcID)
			}
			if fmt.Sprint(payload["tags"]) != "[action]" {
				t.Fatalf("tags = %v", payload["tags"])
			}
			data, ok := payload["data"].(map[string]any)
			if !ok {
				t.Fatalf("data = %+v", payload["data"])
			}
			if _, present := data["tool"]; !present {
				t.Fatalf("data lost the tool key: %+v", data)
			}
			if _, present := data["request_id"]; !present {
				t.Fatalf("data lost the request_id key: %+v", data)
			}
			for _, key := range test.absent {
				if _, present := data[key]; present {
					t.Fatalf("data kept the %q key: %+v", key, data)
				}
			}
			if test.raw["reason"] != nil && data["reason"] != test.raw["reason"] {
				t.Fatalf("data lost the extra field: %+v", data)
			}
			if _, present := payload["summary"]; !present {
				t.Fatalf("summary key missing: %+v", payload)
			}
			if _, present := payload["importance"]; !present {
				t.Fatalf("importance key missing: %+v", payload)
			}
		})
	}
}

// TestGoalEvent mirrors the goal event helper.
func TestGoalEvent(t *testing.T) {
	normalizer := NewNormalizer()

	payload := normalizer.GoalEvent("npc_1", "GOAL_SET", map[string]any{"goal": "find shelter"})
	if payload["event_name"] != "GOAL_SET" || payload["npc_id"] != "npc_1" {
		t.Fatalf("goal_event = %+v", payload)
	}
	if fmt.Sprint(payload["tags"]) != "[goal]" {
		t.Fatalf("tags = %v", payload["tags"])
	}
	data, ok := payload["data"].(map[string]any)
	if !ok || data["goal"] != "find shelter" {
		t.Fatalf("data = %+v", payload["data"])
	}

	// The caller's map must not be aliased.
	source := map[string]any{"goal": "a"}
	payload = normalizer.GoalEvent("npc_1", "GOAL_SET", source)
	source["goal"] = "b"
	data, _ = payload["data"].(map[string]any)
	if data["goal"] != "a" {
		t.Fatalf("goal_event aliased the caller map: %+v", data)
	}
}

// TestNormalizerIsStatelessAndConcurrent exercises the type concurrently: it
// holds no state, so results must not influence each other.
func TestNormalizerIsStatelessAndConcurrent(t *testing.T) {
	normalizer := NewNormalizer()
	const workers = 16

	done := make(chan string, workers)
	for index := 0; index < workers; index++ {
		go func(index int) {
			event, err := normalizer.EventFromMessage(map[string]any{
				"npc_id":     fmt.Sprintf("npc_%d", index),
				"event_name": "concurrent_event",
				"entities":   "player",
				"tags":       []any{"a", "b"},
				"data":       map[string]any{"index": index},
			}, index+1, "")
			if err != nil {
				done <- "error: " + err.Error()
				return
			}
			done <- fmt.Sprintf("%s/%d", event.NPCID, event.Seq)
		}(index)
	}
	for index := 0; index < workers; index++ {
		got := <-done
		if strings.HasPrefix(got, "error:") {
			t.Fatal(got)
		}
	}
}

// ---------------------------------------------------------------------------
// Differential check against the reference Python normalizer
// ---------------------------------------------------------------------------

// differentialEvents are payloads both implementations must normalise
// identically. Every case supplies timestamp and event_id so neither side has
// to generate a non-deterministic value.
var differentialEvents = []struct {
	raw          map[string]any
	seq          int
	defaultNPCID string
}{
	{
		raw: map[string]any{
			"npc_id": "npc_1", "event_name": "player_threatened_npc",
			"entities": "player", "tags": "threat",
			"data":       map[string]any{"weapon": "revolver", "distance": 2.6},
			"importance": 1.5, "timestamp": 1700000000.25, "event_id": "evt_diff_1",
		},
		seq: 7,
	},
	{
		raw: map[string]any{
			"npc_id": "npc_1", "name": "gunshot_heard", "entities": []any{"player", "npc_1"},
			"tags": []any{"world"}, "data": map[string]any{"distance": 30},
			"timestamp": 1700000001.5, "event_id": "evt_diff_2",
		},
		seq: 8,
	},
	{
		raw: map[string]any{
			"event_name": "npc_activated", "data": map[string]any{"source": "scan"},
			"timestamp": 1700000002.0, "event_id": "evt_diff_3",
			"importance": 0.25, "summary": "Elias s'éveille <encore>",
		},
		seq: 1, defaultNPCID: "npc_default",
	},
	{
		raw: map[string]any{
			"npc_id": "npc_1", "event_name": "PLAYER_HELPED_NPC",
			"data":      map[string]any{"note": nil, "nested": map[string]any{"drop": nil, "keep": 1}},
			"game_time": map[string]any{"day": 2, "hour": 9, "minute": 30},
			"location":  map[string]any{"region": "Valentine", "position": []any{1.5, 2.0, 3}},
			"summary":   "", "importance": 0, "timestamp": 0, "event_id": "evt_diff_4",
		},
		seq: 2,
	},
	{
		raw: map[string]any{
			"npc_id": "npc_1", "event_name": "A_EVENT", "data": []any{},
			"entities": []any{}, "tags": nil, "importance": "0.75",
			"timestamp": 1700000003.5, "event_id": "evt_diff_5",
		},
		seq: 3,
	},
	{
		raw: map[string]any{
			"npc_id": "npc_1", "event_name": "", "name": "fallback_name",
			"timestamp": 1700000004.5, "event_id": "evt_diff_6",
		},
		seq: 4,
	},
	{raw: map[string]any{"event_name": "A_EVENT"}, seq: 1},
	{raw: map[string]any{"npc_id": "npc_1"}, seq: 1},
	{raw: map[string]any{"npc_id": "npc_1", "event_name": "player is evil"}, seq: 1},
	{raw: map[string]any{"npc_id": "npc_1", "event_name": "A_EVENT", "data": []any{1}}, seq: 1},
	{raw: map[string]any{"npc_id": "npc_1", "event_name": "A_EVENT"}, seq: 0},
}

// differentialHelpers are the standard fact events both implementations build.
var differentialHelpers = map[string]any{
	"npc_id":   "npc_1",
	"text":     "Where are you headed?",
	"npc_text": "Valentine.",
	"extra":    map[string]any{"emotion": "calm", "target": "player"},
	"action_result": map[string]any{
		"type": "action_result", "npc_id": "npc_1", "status": "failed",
		"tool": "go_to", "request_id": "act_1", "reason": "path_unreachable",
	},
	"goal_data": map[string]any{"goal": "find shelter"},
}

// TestMatchesPythonNormalizer runs the reference implementation from the
// repository and compares normalised events and helper payloads. It is skipped
// when python3 or the Python tree is unavailable.
func TestMatchesPythonNormalizer(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available")
	}
	normalizerPath := project.Resolve(filepath.Join("runtime", "events", "normalizer.py"))
	if _, err := os.Stat(normalizerPath); err != nil {
		t.Skipf("reference normalizer not found at %s: %v", normalizerPath, err)
	}

	type eventCase struct {
		Raw          map[string]any `json:"raw"`
		Seq          int            `json:"seq"`
		DefaultNPCID string         `json:"default_npc_id"`
	}
	events := make([]eventCase, 0, len(differentialEvents))
	for _, test := range differentialEvents {
		events = append(events, eventCase{Raw: test.raw, Seq: test.seq, DefaultNPCID: test.defaultNPCID})
	}
	request := map[string]any{"events": events, "helpers": differentialHelpers}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	command := exec.Command(python, "-c", pythonNormalizerProbe)
	command.Dir = project.Root()
	command.Stdin = bytes.NewReader(encoded)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("python probe failed: %v\n%s", err, output)
	}

	var response struct {
		Events []struct {
			OK    bool           `json:"ok"`
			Error string         `json:"error"`
			Event map[string]any `json:"event"`
		} `json:"events"`
		Helpers map[string]map[string]any `json:"helpers"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		t.Fatalf("decode probe output: %v\n%s", err, output)
	}
	if len(response.Events) != len(differentialEvents) {
		t.Fatalf("probe returned %d events, want %d", len(response.Events), len(differentialEvents))
	}

	for index, test := range differentialEvents {
		reference := response.Events[index]
		event, err := normalizer().EventFromMessage(test.raw, test.seq, test.defaultNPCID)
		if !reference.OK {
			if err == nil {
				t.Fatalf("case %d: Python rejected the payload (%s) but the Go port accepted it", index, reference.Error)
			}
			continue
		}
		if err != nil {
			t.Fatalf("case %d: Python accepted the payload but the Go port failed: %v", index, err)
		}
		got := decodeJSONValue(t, event.ToMap())
		want := decodeJSONValue(t, reference.Event)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("case %d mismatch:\n got %s\nwant %s", index, mustJSON(t, got), mustJSON(t, want))
		}
	}

	helpers := map[string]map[string]any{
		"player_spoke":  normalizer().PlayerSpoke("npc_1", "Where are you headed?", differentialHelpers["extra"].(map[string]any)),
		"npc_spoke":     normalizer().NPCSpoke("npc_1", "Valentine.", differentialHelpers["extra"].(map[string]any)),
		"action_result": normalizer().ActionResult(differentialHelpers["action_result"].(map[string]any)),
		"goal_event":    normalizer().GoalEvent("npc_1", "GOAL_SET", map[string]any{"goal": "find shelter"}),
	}
	for name, want := range response.Helpers {
		got, ok := helpers[name]
		if !ok {
			t.Fatalf("no Go helper named %q", name)
		}
		if !reflect.DeepEqual(decodeJSONValue(t, got), decodeJSONValue(t, want)) {
			t.Fatalf("%s mismatch:\n got %s\nwant %s", name, mustJSON(t, got), mustJSON(t, want))
		}
	}
}

// pythonNormalizerProbe normalises the request with the reference
// implementation and prints one JSON document.
const pythonNormalizerProbe = `import json, sys
from runtime.events.normalizer import EventNormalizer

request = json.load(sys.stdin)
normalizer = EventNormalizer()
out = {"events": [], "helpers": {}}
for case in request["events"]:
    try:
        event = normalizer.event_from_message(
            case["raw"], seq=case["seq"], default_npc_id=case.get("default_npc_id")
        )
        out["events"].append({"ok": True, "event": event.to_dict()})
    except Exception as exc:  # noqa: BLE001
        out["events"].append({"ok": False, "error": type(exc).__name__})

helpers = request["helpers"]
out["helpers"]["player_spoke"] = normalizer.player_spoke(
    helpers["npc_id"], helpers["text"], **helpers["extra"]
)
out["helpers"]["npc_spoke"] = normalizer.npc_spoke(
    helpers["npc_id"], helpers["npc_text"], **helpers["extra"]
)
out["helpers"]["action_result"] = normalizer.action_result(helpers["action_result"])
out["helpers"]["goal_event"] = normalizer.goal_event(
    helpers["npc_id"], "GOAL_SET", **helpers["goal_data"]
)
print(json.dumps(out, ensure_ascii=False))
`

// decodeJSONValue round-trips a payload through JSON so both sides are compared
// as decoded JSON values rather than as Go types.
func decodeJSONValue(t *testing.T, value any) any {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	return decoded
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return string(encoded)
}
