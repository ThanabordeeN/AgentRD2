package agent

import (
	"testing"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/config"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
)

// slowStubBackend mirrors the Python _SlowStubBackend: it claims generation
// latency so the runtime plays a wait gesture.
func slowStubBackend() *fakeBackend { return &fakeBackend{supports: true} }

func awareRuntime(t *testing.T, runtime *Runtime, npcID string) {
	t.Helper()
	scan(t, runtime, safePed(npcID, 15.0))
	scan(t, runtime, safePed(npcID, 8.0))
}

// TestActivationPlaysThinkGestureForSlowBackend ports
// test_wait_gesture.test_activation_plays_think_gesture_for_slow_backend.
func TestActivationPlaysThinkGestureForSlowBackend(t *testing.T) {
	runtime := testRuntime(t, slowStubBackend(), nil)
	awareRuntime(t, runtime, "npc_001")
	gameEvent(t, runtime, "npc_001", "PLAYER_LOOKED_AT_NPC", nil)

	styles := thinkStyles(t, runtime, "npc_001")
	if len(styles) != 1 {
		t.Fatalf("think gestures = %v, want exactly one", styles)
	}
	if styles[0] != "think" {
		t.Fatalf("style = %q, want think", styles[0])
	}
	// The gesture carries the Python duration.
	for _, event := range allEvents(t, runtime, "npc_001") {
		if event.EventName != "ACTION_STARTED" || event.Data["tool"] != "think" {
			continue
		}
		arguments := domain.MapFrom(event.Data["arguments"])
		if got := domain.FloatFrom(arguments["duration"]); got != 2.5 {
			t.Fatalf("duration = %v, want 2.5", arguments["duration"])
		}
	}
}

// TestPlayerSpeechPlaysListenGesture ports
// test_player_speech_plays_listen_gesture.
func TestPlayerSpeechPlaysListenGesture(t *testing.T) {
	runtime := testRuntime(t, slowStubBackend(), nil)
	awareRuntime(t, runtime, "npc_001")
	handle(t, runtime, map[string]any{"type": "player_speech", "npc_id": "npc_001", "text": "Hello there."})

	styles := thinkStyles(t, runtime, "npc_001")
	if !containsName(styles, "listen") {
		t.Fatalf("styles = %v, want listen", styles)
	}
}

// TestWaitGestureCanBeDisabled ports test_wait_gesture_can_be_disabled.
func TestWaitGestureCanBeDisabled(t *testing.T) {
	runtime := testRuntime(t, slowStubBackend(), func(settings *config.Settings) {
		settings.ThinkingPolicy.WaitGestures = false
	})
	awareRuntime(t, runtime, "npc_001")
	gameEvent(t, runtime, "npc_001", "PLAYER_LOOKED_AT_NPC", nil)
	if styles := thinkStyles(t, runtime, "npc_001"); len(styles) != 0 {
		t.Fatalf("think gestures = %v, want none", styles)
	}
}

// TestRuleBasedBackendDoesNotEmitWaitGesture ports
// test_rule_based_backend_does_not_emit_wait_gesture.
func TestRuleBasedBackendDoesNotEmitWaitGesture(t *testing.T) {
	runtime := testRuntime(t, ruleBackend(0.0), nil)
	awareRuntime(t, runtime, "npc_001")
	gameEvent(t, runtime, "npc_001", "PLAYER_LOOKED_AT_NPC", nil)
	if styles := thinkStyles(t, runtime, "npc_001"); len(styles) != 0 {
		t.Fatalf("think gestures = %v, want none", styles)
	}
}

// TestWaitGestureRequiresActiveOwnership covers the ownership guard: a merely
// AWARE NPC gets no gesture.
func TestWaitGestureRequiresActiveOwnership(t *testing.T) {
	decider := slowStubBackend()
	runtime := testRuntime(t, decider, nil)
	awareRuntime(t, runtime, "npc_001")
	if styles := thinkStyles(t, runtime, "npc_001"); len(styles) != 0 {
		t.Fatalf("think gestures = %v, want none while AWARE", styles)
	}
	// A direct schedule for an owned NPC does play one.
	activate(t, runtime, "npc_001")
	if styles := thinkStyles(t, runtime, "npc_001"); len(styles) != 1 {
		t.Fatalf("think gestures = %v, want one after activation", styles)
	}
}

// TestWaitGestureStyles covers the style selection table.
func TestWaitGestureStyles(t *testing.T) {
	cases := []struct {
		reason string
		event  string
		want   string
	}{
		{"player_spoke", "", "listen"},
		{"push_to_talk", "", "listen"},
		{"event", "PLAYER_SPOKE", "listen"},
		{"event", "GUNSHOT_HEARD", "alert"},
		{"event", "PLAYER_THREATENED_NPC", "alert"},
		{"event", "PLAYER_ATTACKED_NPC", "alert"},
		{"event", "NPC_DAMAGED", "alert"},
		{"event", "FIGHT_STARTED", "alert"},
		{"event", "PLAYER_APPROACHED", "think"},
		{"event", "PLAYER_LOOKED_AT_NPC", "think"},
		{"action_failed:go_to", "", "ponder"},
		{"idle_autonomous", "", "ponder"},
		{"meaningful_event:DEAD_BODY_DISCOVERED", "", "think"},
	}
	for _, testCase := range cases {
		var trigger *domain.Event
		if testCase.event != "" {
			trigger = &domain.Event{EventName: testCase.event}
		}
		if got := waitGestureStyle(testCase.reason, trigger); got != testCase.want {
			t.Fatalf("style(%q, %q) = %q, want %q", testCase.reason, testCase.event, got, testCase.want)
		}
	}
}

// TestWaitGestureIsTransient checks that a think gesture never blocks idle
// planning.
func TestWaitGestureIsTransient(t *testing.T) {
	decider := slowStubBackend()
	runtime := testRuntime(t, decider, nil)
	activate(t, runtime, "npc_001")
	if runtime.Dispatcher().Pending() != 0 {
		t.Fatalf("pending = %d, want 0 after a transient think gesture", runtime.Dispatcher().Pending())
	}
	liveState(runtime, "npc_001").LastDecisionAt = runtime.now() - 100
	scan(t, runtime, safePed("npc_001", 8.0))
	if decider.calls != 2 {
		t.Fatalf("backend calls = %d, want 2 (think must not block idle thinking)", decider.calls)
	}
}
