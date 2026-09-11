package tools

import (
	"errors"
	"reflect"
	"testing"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/timeline"
)

func floatPtr(value float64) *float64 { return &value }
func intPtr(value int) *int           { return &value }

func TestGetWorldStateTool(t *testing.T) {
	registry := BuildDefaultRegistry()
	ctx := &Context{
		NPCID: "npc_1",
		World: fakeWorld{states: map[string]map[string]any{
			"npc_1": {"health": 0.75, "location": map[string]any{"region": "Lemoyne"}},
		}},
	}

	result := registry.Call("get_world_state", ctx, map[string]any{})

	if result.Status != domain.ActionCompleted {
		t.Fatalf("status = %q, want completed (%s)", result.Status, result.Reason)
	}
	if result.Tool != "get_world_state" || result.RequestID == "" {
		t.Fatalf("result must echo the tool and a request id, got %+v", result)
	}
	// Python's _call_tool wraps perception data as
	// ToolResult.completed(request, result=<payload>).
	assertJSONEqual(t, result.Detail, `{"result": {"health": 0.75, "location": {"region": "Lemoyne"}}}`)
}

func TestGetWorldStateToolWithoutStoreFails(t *testing.T) {
	result := BuildDefaultRegistry().Call("get_world_state", &Context{NPCID: "npc_1"}, nil)

	if result.Status != domain.ActionFailed {
		t.Fatalf("status = %q, want failed", result.Status)
	}
	if result.Reason == "" {
		t.Fatal("failed result must carry a reason")
	}
}

func TestGrabTimelineMapsEveryFilter(t *testing.T) {
	reader := &fakeTimeline{}
	ctx := &Context{NPCID: "npc_1", Timeline: reader}

	result := BuildDefaultRegistry().Call("grab_timeline", ctx, map[string]any{
		"event_name":         "GREETING",
		"event_names":        []any{"THREAT", "GUNSHOT"},
		"tag":                "action",
		"tags":               []any{"combat", "social"},
		"entity":             "player",
		"minimum_importance": 0.5,
		"since_seq":          3,
		"before_seq":         9,
		"limit":              10,
	})

	if result.Status == domain.ActionFailed {
		t.Fatalf("unexpected failure: %s", result.Reason)
	}
	want := timeline.GrabFilter{
		EventName:         "GREETING",
		EventNames:        []string{"THREAT", "GUNSHOT"},
		Tag:               "action",
		Tags:              []string{"combat", "social"},
		Entity:            "player",
		MinimumImportance: floatPtr(0.5),
		SinceSeq:          intPtr(3),
		BeforeSeq:         intPtr(9),
		Limit:             10,
	}
	if len(reader.filters) != 1 {
		t.Fatalf("store received %d filters, want 1", len(reader.filters))
	}
	if !reflect.DeepEqual(reader.filters[0], want) {
		t.Fatalf("store filter = %+v, want %+v", reader.filters[0], want)
	}
	if reader.npcs[0] != "npc_1" {
		t.Fatalf("store npc = %q, want npc_1", reader.npcs[0])
	}
}

func TestGrabTimelineAppliesStoreDefaults(t *testing.T) {
	reader := &fakeTimeline{}
	ctx := &Context{NPCID: "npc_1", Timeline: reader}

	result := BuildDefaultRegistry().Call("grab_timeline", ctx, map[string]any{})

	if result.Status == domain.ActionFailed {
		t.Fatalf("unexpected failure: %s", result.Reason)
	}
	want := timeline.GrabFilter{Limit: 20}
	if !reflect.DeepEqual(reader.filters[0], want) {
		t.Fatalf("store filter = %+v, want %+v", reader.filters[0], want)
	}
}

func TestGrabTimelineDefaultsLimitWhenAbsentButKeepsZero(t *testing.T) {
	reader := &fakeTimeline{}
	ctx := &Context{NPCID: "npc_1", Timeline: reader}

	BuildDefaultRegistry().Call("grab_timeline", ctx, map[string]any{"limit": 0})

	if reader.filters[0].Limit != 0 {
		t.Fatalf("limit = %d, want 0 (Python's 'no truncation')", reader.filters[0].Limit)
	}
}

func TestGrabTimelineRejectsUnparsableFilters(t *testing.T) {
	for _, testCase := range []struct {
		name string
		args map[string]any
	}{
		{"limit", map[string]any{"limit": "many"}},
		{"minimum_importance", map[string]any{"minimum_importance": "high"}},
		{"since_seq", map[string]any{"since_seq": "later"}},
		{"before_seq", map[string]any{"before_seq": "earlier"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			reader := &fakeTimeline{}
			ctx := &Context{NPCID: "npc_1", Timeline: reader}

			result := BuildDefaultRegistry().Call("grab_timeline", ctx, testCase.args)

			if result.Status != domain.ActionFailed {
				t.Fatalf("status = %q, want failed", result.Status)
			}
			if len(reader.filters) != 0 {
				t.Fatal("an invalid filter must not reach the store")
			}
		})
	}
}

func TestGrabTimelineFormatsEvents(t *testing.T) {
	timestamp := 1700000000.0
	summary := "A greeting"
	importance := 0.5
	event := domain.NewEvent(domain.NewEventOptions{
		Seq:        2,
		EventName:  "GREETING",
		NPCID:      "npc_1",
		EventID:    "evt_test",
		Timestamp:  &timestamp,
		Entities:   []string{"player"},
		Tags:       []string{"social"},
		Summary:    &summary,
		Importance: &importance,
		Data:       map[string]any{"text": "hi"},
	})
	ctx := &Context{NPCID: "npc_1", Timeline: &fakeTimeline{events: []domain.Event{event}}}

	result := BuildDefaultRegistry().Call("grab_timeline", ctx, map[string]any{})

	if result.Status != domain.ActionCompleted {
		t.Fatalf("status = %q, want completed (%s)", result.Status, result.Reason)
	}
	assertJSONEqual(t, result.Detail, `{"result": {"events": [{
		"seq": 2,
		"event_id": "evt_test",
		"event_name": "GREETING",
		"timestamp": 1700000000,
		"npc_id": "npc_1",
		"entities": ["player"],
		"data": {"text": "hi"},
		"tags": ["social"],
		"summary": "A greeting",
		"importance": 0.5
	}]}}`)
}

func TestGrabTimelineFormatsEmptyResults(t *testing.T) {
	ctx := &Context{NPCID: "npc_1", Timeline: &fakeTimeline{}}

	result := BuildDefaultRegistry().Call("grab_timeline", ctx, map[string]any{})

	assertJSONEqual(t, result.Detail, `{"result": {"events": []}}`)
}

func TestGrabTimelineStoreErrorFails(t *testing.T) {
	ctx := &Context{NPCID: "npc_1", Timeline: &fakeTimeline{err: errors.New("timeline is corrupt")}}

	result := BuildDefaultRegistry().Call("grab_timeline", ctx, map[string]any{})

	if result.Status != domain.ActionFailed {
		t.Fatalf("status = %q, want failed", result.Status)
	}
	if result.Reason != "timeline is corrupt" {
		t.Fatalf("reason = %q, want the store error", result.Reason)
	}
}

func TestGrabTimelineToolWithoutStoreFails(t *testing.T) {
	result := BuildDefaultRegistry().Call("grab_timeline", &Context{NPCID: "npc_1"}, map[string]any{})

	if result.Status != domain.ActionFailed {
		t.Fatalf("status = %q, want failed", result.Status)
	}
}

func TestGetWorldLoreToolWithoutStoreReturnsEmpty(t *testing.T) {
	result := BuildDefaultRegistry().Call("get_world_lore", &Context{NPCID: "npc_1"}, map[string]any{"topic": "Valentine"})

	if result.Status != domain.ActionCompleted {
		t.Fatalf("status = %q, want completed", result.Status)
	}
	assertJSONEqual(t, result.Detail, `{"result": {"lore": []}}`)
}

func TestGetWorldLoreToolLooksUpTopic(t *testing.T) {
	lore := &fakeLore{byTopic: map[string][]map[string]any{
		"Valentine": {{"title": "Valentine", "summary": "A livestock town."}},
	}}
	ctx := &Context{NPCID: "npc_1", Lore: lore, Profile: map[string]any{"name": "Dutch"}}

	result := BuildDefaultRegistry().Call("get_world_lore", ctx, map[string]any{"topic": "Valentine"})

	if result.Status == domain.ActionFailed {
		t.Fatalf("unexpected failure: %s", result.Reason)
	}
	if len(lore.lookups) != 1 {
		t.Fatalf("Lookup calls = %v, want 1", lore.lookups)
	}
	if got, want := lore.lookups[0], (lookupCall{query: "Valentine", limit: 3, maxChars: 1200}); got != want {
		t.Fatalf("Lookup call = %+v, want %+v", got, want)
	}
	if len(lore.profiles) != 0 {
		t.Fatal("a topic lookup must not read the profile")
	}
	assertJSONEqual(t, result.Detail, `{"result": {"lore": [{"title": "Valentine", "summary": "A livestock town."}]}}`)
}

func TestGetWorldLoreToolUsesProfileWhenNoTopic(t *testing.T) {
	profile := map[string]any{"name": "Dutch", "wiki_topics": []any{"Van der Linde gang"}}
	lore := &fakeLore{fallback: []map[string]any{{"title": "Dutch van der Linde"}}}
	ctx := &Context{NPCID: "npc_1", Lore: lore, Profile: profile}

	result := BuildDefaultRegistry().Call("get_world_lore", ctx, map[string]any{})

	if result.Status == domain.ActionFailed {
		t.Fatalf("unexpected failure: %s", result.Reason)
	}
	if len(lore.profiles) != 1 {
		t.Fatalf("ContextForProfile calls = %v, want 1", lore.profiles)
	}
	call := lore.profiles[0]
	if call.limit != 5 || call.maxChars != 1200 {
		t.Fatalf("ContextForProfile limits = %d/%d, want 5/1200", call.limit, call.maxChars)
	}
	if !reflect.DeepEqual(call.profile, profile) {
		t.Fatalf("ContextForProfile profile = %v, want the NPC profile", call.profile)
	}
	assertJSONEqual(t, result.Detail, `{"result": {"lore": [{"title": "Dutch van der Linde"}]}}`)
}

func TestGetWorldLoreToolTreatsEmptyTopicAsNoTopic(t *testing.T) {
	lore := &fakeLore{fallback: []map[string]any{}}
	ctx := &Context{NPCID: "npc_1", Lore: lore, Profile: map[string]any{"name": "Dutch"}}

	BuildDefaultRegistry().Call("get_world_lore", ctx, map[string]any{"topic": ""})

	if len(lore.profiles) != 1 || len(lore.lookups) != 0 {
		t.Fatalf("empty topic must use the profile path, got lookups=%v profiles=%v", lore.lookups, lore.profiles)
	}
}
