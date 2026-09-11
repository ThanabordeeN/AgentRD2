package state

import (
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
)

func testEvent(npcID, eventName string, seq int) domain.Event {
	return domain.Event{
		Seq:       seq,
		EventName: eventName,
		NPCID:     npcID,
		EventID:   "evt_test",
		Timestamp: 1700000000 + float64(seq),
		Entities:  []string{},
		Data:      map[string]any{},
		Tags:      []string{},
	}
}

// TestWorldStoreUpdateAndGet covers the snapshot round trip and the default
// record created for an unknown NPC.
func TestWorldStoreUpdateAndGet(t *testing.T) {
	store := NewWorldStore()
	state := domain.WorldState{
		NPCID:     "npc_1",
		SelfState: map[string]any{"mood": "calm"},
		Player:    map[string]any{"distance": 4.5},
		NearbyPeds: []map[string]any{
			{"entity_id": "npc_2"},
		},
		Timestamp: 123.5,
	}
	store.Update(state)

	got := store.Get("npc_1")
	if got.NPCID != "npc_1" || got.Timestamp != 123.5 {
		t.Fatalf("Get = %+v, want npc_1 at 123.5", got)
	}
	if got.SelfState["mood"] != "calm" || got.Player["distance"] != 4.5 {
		t.Fatalf("Get = %+v, want the stored fields", got)
	}
	if len(got.NearbyPeds) != 1 || got.NearbyPeds[0]["entity_id"] != "npc_2" {
		t.Fatalf("NearbyPeds = %v, want the stored ped", got.NearbyPeds)
	}

	unknown := store.Get("npc_unknown")
	if unknown.NPCID != "npc_unknown" {
		t.Fatalf("NPCID = %q, want npc_unknown", unknown.NPCID)
	}
	if unknown.SelfState == nil || unknown.Player == nil {
		t.Fatalf("unknown state maps must be usable: %+v", unknown)
	}
	if unknown.Timestamp <= 0 {
		t.Fatalf("Timestamp = %v, want a default timestamp", unknown.Timestamp)
	}
	if cached := store.Get("npc_unknown"); cached.Timestamp != unknown.Timestamp {
		t.Fatal("Get must cache the default state")
	}
}

// TestWorldStoreGetMapShape checks the payload keys and values, mirroring
// "WorldState.to_dict" (which omits npc_id).
func TestWorldStoreGetMapShape(t *testing.T) {
	store := NewWorldStore()
	store.Update(domain.WorldState{
		NPCID:     "npc_1",
		SelfState: map[string]any{"mood": "calm"},
		Timestamp: 42.0,
	})

	payload := store.GetMap("npc_1")
	wantKeys := []string{"self", "player", "nearby_peds", "nearby_horses", "recent_game_events", "timestamp"}
	gotKeys := make([]string, 0, len(payload))
	for key := range payload {
		gotKeys = append(gotKeys, key)
	}
	sort.Strings(gotKeys)
	sort.Strings(wantKeys)
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("payload keys = %v, want %v", gotKeys, wantKeys)
	}
	if payload["timestamp"] != 42.0 {
		t.Fatalf("timestamp = %v, want 42", payload["timestamp"])
	}
	self, ok := payload["self"].(map[string]any)
	if !ok || self["mood"] != "calm" {
		t.Fatalf("self = %v, want the stored map", payload["self"])
	}
	if peds, ok := payload["nearby_peds"].([]map[string]any); !ok || len(peds) != 0 {
		t.Fatalf("nearby_peds = %#v, want an empty []map[string]any", payload["nearby_peds"])
	}
}

// TestWorldStoreGetMapIsDeepCopy protects the store from callers mutating the
// payload, mirroring "copy.deepcopy".
func TestWorldStoreGetMapIsDeepCopy(t *testing.T) {
	store := NewWorldStore()
	store.Update(domain.WorldState{
		NPCID:     "npc_1",
		SelfState: map[string]any{"mood": "calm", "nested": map[string]any{"level": 1}},
		NearbyPeds: []map[string]any{
			{"entity_id": "npc_2"},
		},
		Player: map[string]any{"distance": 4.5},
	})
	store.ApplyEvent(testEvent("npc_1", "PLAYER_APPROACHED", 1), 50)

	payload := store.GetMap("npc_1")
	payload["self"].(map[string]any)["mood"] = "angry"
	payload["self"].(map[string]any)["nested"].(map[string]any)["level"] = 99
	payload["nearby_peds"].([]map[string]any)[0]["entity_id"] = "mutated"
	payload["recent_game_events"].([]map[string]any)[0]["event_name"] = "mutated"

	fresh := store.GetMap("npc_1")
	if fresh["self"].(map[string]any)["mood"] != "calm" {
		t.Fatal("self map was mutated through the payload")
	}
	if fresh["self"].(map[string]any)["nested"].(map[string]any)["level"] != 1 {
		t.Fatal("nested self map was mutated through the payload")
	}
	if fresh["nearby_peds"].([]map[string]any)[0]["entity_id"] != "npc_2" {
		t.Fatal("nearby ped was mutated through the payload")
	}
	if fresh["recent_game_events"].([]map[string]any)[0]["event_name"] != "PLAYER_APPROACHED" {
		t.Fatal("recent event was mutated through the payload")
	}
}

// TestWorldStoreGetMapMergesRecentEvents covers the timeline-backed merge.
func TestWorldStoreGetMapMergesRecentEvents(t *testing.T) {
	t.Run("bridge events are kept when no timeline events exist", func(t *testing.T) {
		store := NewWorldStore()
		store.Update(domain.WorldState{
			NPCID:            "npc_1",
			RecentGameEvents: []map[string]any{{"event_name": "BRIDGE_EVENT"}},
		})
		payload := store.GetMap("npc_1")
		events := payload["recent_game_events"].([]map[string]any)
		if len(events) != 1 || events[0]["event_name"] != "BRIDGE_EVENT" {
			t.Fatalf("recent_game_events = %v, want the bridge event", events)
		}
	})

	t.Run("timeline events win and cap at 20", func(t *testing.T) {
		store := NewWorldStore()
		store.Update(domain.WorldState{
			NPCID:            "npc_1",
			RecentGameEvents: []map[string]any{{"event_name": "BRIDGE_EVENT"}},
		})
		for seq := 1; seq <= 25; seq++ {
			store.ApplyEvent(testEvent("npc_1", eventNameForSeq(seq), seq), 50)
		}
		payload := store.GetMap("npc_1")
		events := payload["recent_game_events"].([]map[string]any)
		if len(events) != 20 {
			t.Fatalf("recent_game_events length = %d, want 20", len(events))
		}
		if events[0]["event_name"] != "EVENT_6" || events[19]["event_name"] != "EVENT_25" {
			t.Fatalf("recent_game_events = %v ... %v, want EVENT_6 ... EVENT_25",
				events[0]["event_name"], events[19]["event_name"])
		}
	})
}

func eventNameForSeq(seq int) string {
	return fmt.Sprintf("EVENT_%d", seq)
}

// TestWorldStoreApplyEventTrim covers "del bucket[:-max_recent]" exactly,
// including the zero and negative bounds.  Trimming happens on every append,
// so the bucket is filled with a permissive bound first and one more event
// then drives the bound under test.
func TestWorldStoreApplyEventTrim(t *testing.T) {
	cases := []struct {
		name      string
		maxRecent int
		wantNames []string
	}{
		{name: "keeps the last three", maxRecent: 3, wantNames: []string{"EVENT_4", "EVENT_5", "EVENT_6"}},
		{name: "zero trims nothing", maxRecent: 0, wantNames: []string{"EVENT_1", "EVENT_2", "EVENT_3", "EVENT_4", "EVENT_5", "EVENT_6"}},
		{name: "negative bound skips leading events", maxRecent: -2, wantNames: []string{"EVENT_3", "EVENT_4", "EVENT_5", "EVENT_6"}},
		{name: "large negative bound empties the bucket", maxRecent: -10, wantNames: []string{}},
		{name: "larger bound keeps everything", maxRecent: 50, wantNames: []string{"EVENT_1", "EVENT_2", "EVENT_3", "EVENT_4", "EVENT_5", "EVENT_6"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			store := NewWorldStore()
			for seq := 1; seq <= 5; seq++ {
				store.ApplyEvent(testEvent("npc_1", eventNameForSeq(seq), seq), 50)
			}
			store.ApplyEvent(testEvent("npc_1", eventNameForSeq(6), 6), testCase.maxRecent)

			events := store.RecentGameEvents("npc_1", 100)
			names := make([]string, 0, len(events))
			for _, event := range events {
				names = append(names, domain.StringFrom(event["event_name"]))
			}
			if !reflect.DeepEqual(names, testCase.wantNames) {
				t.Fatalf("events = %v, want %v", names, testCase.wantNames)
			}
		})
	}
}

// TestWorldStoreApplyEventTrimIsIncremental pins the per-append behaviour: a
// negative bound trims on every event, so a one-element bucket is emptied
// immediately (matching Python's "del bucket[:2]" on a one-element list).
func TestWorldStoreApplyEventTrimIsIncremental(t *testing.T) {
	store := NewWorldStore()
	for seq := 1; seq <= 5; seq++ {
		store.ApplyEvent(testEvent("npc_1", eventNameForSeq(seq), seq), -2)
	}
	if events := store.RecentGameEvents("npc_1", 100); len(events) != 0 {
		t.Fatalf("events = %v, want empty", events)
	}
}

// TestWorldStoreRecentGameEventsLimit covers Python's "bucket[-limit:]".
func TestWorldStoreRecentGameEventsLimit(t *testing.T) {
	store := NewWorldStore()
	for seq := 1; seq <= 5; seq++ {
		store.ApplyEvent(testEvent("npc_1", eventNameForSeq(seq), seq), 50)
	}

	cases := []struct {
		name      string
		limit     int
		wantNames []string
	}{
		{name: "positive limit keeps the tail", limit: 2, wantNames: []string{"EVENT_4", "EVENT_5"}},
		{name: "zero limit returns everything", limit: 0, wantNames: []string{"EVENT_1", "EVENT_2", "EVENT_3", "EVENT_4", "EVENT_5"}},
		{name: "negative limit skips leading events", limit: -2, wantNames: []string{"EVENT_3", "EVENT_4", "EVENT_5"}},
		{name: "negative limit past the end is empty", limit: -7, wantNames: []string{}},
		{name: "limit past the end returns everything", limit: 10, wantNames: []string{"EVENT_1", "EVENT_2", "EVENT_3", "EVENT_4", "EVENT_5"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			events := store.RecentGameEvents("npc_1", testCase.limit)
			names := make([]string, 0, len(events))
			for _, event := range events {
				names = append(names, domain.StringFrom(event["event_name"]))
			}
			if !reflect.DeepEqual(names, testCase.wantNames) {
				t.Fatalf("events = %v, want %v", names, testCase.wantNames)
			}
		})
	}

	if events := store.RecentGameEvents("npc_unknown", 20); events == nil || len(events) != 0 {
		t.Fatalf("unknown npc events = %#v, want an empty non-nil slice", events)
	}
}

// TestWorldStoreApplyEventShape checks that the stored payload is Event.to_dict
// shaped and that unknown NPCs get a bucket.
func TestWorldStoreApplyEventShape(t *testing.T) {
	store := NewWorldStore()
	summary := "The player approaches."
	importance := 0.5
	event := domain.NewEvent(domain.NewEventOptions{
		Seq:        1,
		EventName:  "PLAYER_APPROACHED",
		NPCID:      "npc_1",
		Data:       map[string]any{"distance": 4.5, "unused": nil},
		Entities:   []string{"player"},
		Tags:       []string{"proximity"},
		Summary:    &summary,
		Importance: &importance,
		GameTime:   &domain.GameTime{Day: 2, Hour: 7, Minute: 30},
		Location:   &domain.Location{Region: strPtr("Lemoyne")},
	})
	store.ApplyEvent(event, 50)

	events := store.RecentGameEvents("npc_1", 20)
	if len(events) != 1 {
		t.Fatalf("events = %v, want one event", events)
	}
	stored := events[0]
	if stored["event_name"] != "PLAYER_APPROACHED" || stored["npc_id"] != "npc_1" {
		t.Fatalf("stored = %v", stored)
	}
	if _, ok := stored["game_time"]; !ok {
		t.Fatalf("stored = %v, want game_time", stored)
	}
	if _, ok := stored["location"]; !ok {
		t.Fatalf("stored = %v, want location", stored)
	}
	data, ok := stored["data"].(map[string]any)
	if !ok {
		t.Fatalf("data = %#v, want a map", stored["data"])
	}
	if _, ok := data["unused"]; ok {
		t.Fatalf("data = %v, want nil values stripped", data)
	}

	unknown := store.RecentGameEvents("npc_absent", 5)
	if len(unknown) != 0 {
		t.Fatalf("unknown npc events = %v, want empty", unknown)
	}
}

func strPtr(value string) *string { return &value }

// TestWorldStoreSetFields covers the self/player field writers, including the
// implicit record creation.
func TestWorldStoreSetFields(t *testing.T) {
	store := NewWorldStore()
	store.SetSelfField("npc_1", "mood", "calm")
	store.SetPlayerField("npc_1", "distance", 4.5)
	store.SetSelfField("npc_1", "mood", "wary")

	state := store.Get("npc_1")
	if state.SelfState["mood"] != "wary" {
		t.Fatalf("mood = %v, want wary", state.SelfState["mood"])
	}
	if state.Player["distance"] != 4.5 {
		t.Fatalf("distance = %v, want 4.5", state.Player["distance"])
	}

	// An unknown NPC is created on write and is visible in GetMap.
	store.SetSelfField("npc_2", "alert", true)
	payload := store.GetMap("npc_2")
	if payload["self"].(map[string]any)["alert"] != true {
		t.Fatalf("payload = %v, want the written self field", payload)
	}

	// A snapshot stored with nil maps must still accept writes.
	store.Update(domain.WorldState{NPCID: "npc_3"})
	store.SetSelfField("npc_3", "key", "value")
	store.SetPlayerField("npc_3", "key", "value")
	state = store.Get("npc_3")
	if state.SelfState["key"] != "value" || state.Player["key"] != "value" {
		t.Fatalf("state = %+v, want the written fields", state)
	}
}

// TestWorldStoreRemove covers forgetting an NPC.
func TestWorldStoreRemove(t *testing.T) {
	store := NewWorldStore()
	store.Update(domain.WorldState{NPCID: "npc_1", SelfState: map[string]any{"mood": "calm"}})
	store.ApplyEvent(testEvent("npc_1", "EVENT", 1), 50)

	store.Remove("npc_1")
	if events := store.RecentGameEvents("npc_1", 20); len(events) != 0 {
		t.Fatalf("events = %v, want empty after Remove", events)
	}
	fresh := store.Get("npc_1")
	if fresh.SelfState["mood"] != nil {
		t.Fatalf("state = %+v, want a fresh record after Remove", fresh)
	}
	if len(store.GetMap("npc_1")["recent_game_events"].([]map[string]any)) != 0 {
		t.Fatal("GetMap must not resurrect removed events")
	}

	// Removing an unknown NPC is a no-op.
	store.Remove("npc_absent")
}

// TestWorldStoreZeroValue ensures the zero store works like an empty one.
func TestWorldStoreZeroValue(t *testing.T) {
	var store WorldStore
	payload := store.GetMap("npc_1")
	if len(payload) != 6 {
		t.Fatalf("payload = %v, want six keys", payload)
	}
	store.ApplyEvent(testEvent("npc_1", "EVENT", 1), 50)
	if events := store.RecentGameEvents("npc_1", 20); len(events) != 1 {
		t.Fatalf("events = %v, want one event", events)
	}
	store.Remove("npc_1")
	if events := store.RecentGameEvents("npc_1", 20); len(events) != 0 {
		t.Fatalf("events = %v, want empty", events)
	}
}

// TestWorldStoreRecentEventsSliceIsACopy ensures callers cannot shrink the
// stored bucket through the returned slice header.
func TestWorldStoreRecentEventsSliceIsACopy(t *testing.T) {
	store := NewWorldStore()
	for seq := 1; seq <= 3; seq++ {
		store.ApplyEvent(testEvent("npc_1", eventNameForSeq(seq), seq), 50)
	}
	events := store.RecentGameEvents("npc_1", 20)
	if len(events) != 3 {
		t.Fatalf("events = %v, want three", events)
	}
	events[0] = map[string]any{"event_name": "mutated"}
	again := store.RecentGameEvents("npc_1", 20)
	if again[0]["event_name"] != "EVENT_1" {
		t.Fatal("the returned slice must be a copy of the bucket")
	}
}
