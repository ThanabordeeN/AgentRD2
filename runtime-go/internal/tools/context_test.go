package tools

import (
	"errors"
	"reflect"
	"testing"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/lore"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/state"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/timeline"
)

// The tools layer must accept the real stores the agent runtime wires in.
var (
	_ WorldReader    = (*state.WorldStore)(nil)
	_ TimelineReader = (*timeline.Store)(nil)
	_ LoreReader     = (*lore.Store)(nil)
)

func TestDispatchActionBuildsActionRequest(t *testing.T) {
	dispatcher := &fakeDispatcher{}
	ctx := &Context{NPCID: "npc_42", Dispatcher: dispatcher}

	result := ctx.DispatchAction("say", map[string]any{"text": "hello"})

	request := dispatcher.last(t)
	if request.Tool != "say" {
		t.Errorf("tool = %q, want say", request.Tool)
	}
	if request.NPCID != "npc_42" {
		t.Errorf("npc_id = %q, want npc_42", request.NPCID)
	}
	if request.RequestID == "" {
		t.Error("request_id must be generated")
	}
	if !reflect.DeepEqual(request.Arguments, map[string]any{"text": "hello"}) {
		t.Errorf("arguments = %v, want the tool arguments", request.Arguments)
	}
	if result.Status != domain.ActionStarted || result.RequestID != request.RequestID {
		t.Errorf("result must be the dispatcher reply for the request, got %+v", result)
	}
}

func TestDispatchActionNilArgumentsBecomesEmptyMap(t *testing.T) {
	dispatcher := &fakeDispatcher{}
	ctx := &Context{NPCID: "npc_1", Dispatcher: dispatcher}

	ctx.DispatchAction("stop", nil)

	if arguments := dispatcher.last(t).Arguments; arguments == nil || len(arguments) != 0 {
		t.Fatalf("arguments = %#v, want an empty non-nil map", arguments)
	}
}

func TestDispatchActionWithoutDispatcherFails(t *testing.T) {
	ctx := &Context{NPCID: "npc_1"}

	result := ctx.DispatchAction("stop", nil)

	if result.Status != domain.ActionFailed {
		t.Fatalf("status = %q, want failed", result.Status)
	}
	if result.Reason == "" {
		t.Fatal("failed result must carry a reason")
	}
	if result.Tool != "stop" || result.RequestID == "" {
		t.Fatalf("failed result must carry tool and request id, got %+v", result)
	}
}

func TestContextNilReceiverIsSafe(t *testing.T) {
	var ctx *Context
	if state := ctx.GetWorldState(); state == nil || len(state) != 0 {
		t.Fatalf("GetWorldState on a nil context = %v, want an empty map", state)
	}
	if _, err := ctx.GrabTimeline(timeline.GrabFilter{}); err == nil {
		t.Fatal("GrabTimeline on a nil context must report an error")
	}
	if result := ctx.DispatchAction("stop", nil); result.Status != domain.ActionFailed {
		t.Fatalf("DispatchAction on a nil context = %q, want failed", result.Status)
	}
}

func TestGetWorldStateReadsTheStore(t *testing.T) {
	ctx := &Context{
		NPCID: "npc_1",
		World: fakeWorld{states: map[string]map[string]any{
			"npc_1": {"location": map[string]any{"region": "Lemoyne"}},
		}},
	}
	state := ctx.GetWorldState()
	region := domain.MapFrom(state["location"])["region"]
	if region != "Lemoyne" {
		t.Fatalf("state = %v, want the store snapshot", state)
	}
	if other := (&Context{NPCID: "npc_2", World: fakeWorld{}}).GetWorldState(); len(other) != 0 {
		t.Fatalf("unknown NPC state = %v, want an empty map", other)
	}
}

func TestGetWorldStateWithoutStoreIsEmpty(t *testing.T) {
	ctx := &Context{NPCID: "npc_1"}
	if state := ctx.GetWorldState(); state == nil || len(state) != 0 {
		t.Fatalf("state = %v, want an empty non-nil map", state)
	}
}

func TestGrabTimelineForwardsFilterAndNPC(t *testing.T) {
	reader := &fakeTimeline{events: []domain.Event{
		domain.NewEvent(domain.NewEventOptions{Seq: 2, EventName: "GREETING", NPCID: "npc_1"}),
	}}
	ctx := &Context{NPCID: "npc_1", Timeline: reader}

	events, err := ctx.GrabTimeline(timeline.GrabFilter{Limit: 5})
	if err != nil {
		t.Fatalf("GrabTimeline: %v", err)
	}
	if len(events) != 1 || events[0].EventName != "GREETING" {
		t.Fatalf("events = %v, want the store result", events)
	}
	if len(reader.npcs) != 1 || reader.npcs[0] != "npc_1" {
		t.Fatalf("store npc ids = %v, want [npc_1]", reader.npcs)
	}
	if !reflect.DeepEqual(reader.filters[0], timeline.GrabFilter{Limit: 5}) {
		t.Fatalf("store filter = %v, want the forwarded filter", reader.filters[0])
	}
}

func TestGrabTimelineNormalisesEmptyResultsAndErrors(t *testing.T) {
	ctx := &Context{NPCID: "npc_1", Timeline: &fakeTimeline{}}
	events, err := ctx.GrabTimeline(timeline.GrabFilter{})
	if err != nil {
		t.Fatalf("GrabTimeline: %v", err)
	}
	if events == nil || len(events) != 0 {
		t.Fatalf("events = %#v, want an empty non-nil slice", events)
	}

	boom := errors.New("timeline is corrupt")
	failing := &Context{NPCID: "npc_1", Timeline: &fakeTimeline{err: boom}}
	if _, err := failing.GrabTimeline(timeline.GrabFilter{}); !errors.Is(err, boom) {
		t.Fatalf("error = %v, want the store error", err)
	}
}

func TestGrabTimelineWithoutStoreFails(t *testing.T) {
	ctx := &Context{NPCID: "npc_1"}
	if _, err := ctx.GrabTimeline(timeline.GrabFilter{}); err == nil {
		t.Fatal("GrabTimeline without a store must report an error")
	}
}

// TestLoreReaderFakeMatchesInterface keeps the interface used by the lore
// tests honest.
func TestLoreReaderFakeMatchesInterface(t *testing.T) {
	var reader LoreReader = &fakeLore{
		byTopic:  map[string][]map[string]any{"Valentine": {{"title": "Valentine"}}},
		fallback: []map[string]any{{"title": "Dutch"}},
	}
	if pages := reader.Lookup("Valentine", 3, 1200); len(pages) != 1 || pages[0]["title"] != "Valentine" {
		t.Fatalf("Lookup = %v", pages)
	}
	if pages := reader.ContextForProfile(map[string]any{"name": "Dutch"}, 5, 1200); len(pages) != 1 || pages[0]["title"] != "Dutch" {
		t.Fatalf("ContextForProfile = %v", pages)
	}
}
