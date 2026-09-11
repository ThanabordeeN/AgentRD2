package agent

import (
	"testing"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/backend"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/state"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/timeline"
)

func ruleBackend(silenceProbability float64) backend.Backend {
	return backend.NewRuleBackend(backend.RuleOptions{SilenceProbability: silenceProbability, Seed: ruleBackendSeed})
}

// TestProximityAndTriggerActivateNpc ports
// test_agent_runtime.AgentRuntimeTests.test_proximity_and_trigger_activate_npc.
func TestProximityAndTriggerActivateNpc(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	activate(t, runtime, "npc_001")
	if !containsName(eventNames(t, runtime, "npc_001"), "NPC_ACTIVATED") {
		t.Fatalf("NPC_ACTIVATED missing: %v", eventNames(t, runtime, "npc_001"))
	}
	if got := runtime.Ownership().State("npc_001"); got != state.StateAIActive {
		t.Fatalf("state = %s, want AI_ACTIVE", got)
	}
}

// TestPlayerSpeechMovesToConversationAndGeneratesReply ports
// test_player_speech_moves_to_conversation_and_generates_reply.
func TestPlayerSpeechMovesToConversationAndGeneratesReply(t *testing.T) {
	runtime := testRuntime(t, ruleBackend(0.0), nil)
	activate(t, runtime, "npc_001")

	handle(t, runtime, map[string]any{
		"type": "player_speech", "npc_id": "npc_001", "text": "Where are you headed?",
	})

	if got := runtime.Ownership().State("npc_001"); got != state.StateAIConversation {
		t.Fatalf("state = %s, want AI_CONVERSATION", got)
	}
	names := eventNames(t, runtime, "npc_001")
	if !containsName(names, "PLAYER_SPOKE") || !containsName(names, "NPC_SPOKE") {
		t.Fatalf("missing speech events: %v", names)
	}
	replied := false
	for _, event := range allEvents(t, runtime, "npc_001") {
		if event.EventName == "NPC_SPOKE" && containsSubstring(pyStr(event.Data["text"]), "Valentine") {
			replied = true
		}
	}
	if !replied {
		t.Fatal("expected a reply mentioning Valentine")
	}
}

// TestThreatCausesFleeForLowCourageProfile ports
// test_threat_causes_flee_for_low_courage_profile.
func TestThreatCausesFleeForLowCourageProfile(t *testing.T) {
	runtime := testRuntime(t, ruleBackend(0.0), nil)
	writeProfile(t, runtime, "npc_001", 0.44)
	activate(t, runtime, "npc_001")

	gameEvent(t, runtime, "npc_001", "PLAYER_THREATENED_NPC", map[string]any{
		"weapon": "revolver", "distance": 2.6,
	})

	if !containsName(startedTools(t, runtime, "npc_001"), "flee_from") {
		t.Fatalf("flee_from missing: %v", startedTools(t, runtime, "npc_001"))
	}
	if !runtime.Ownership().CanReason("npc_001") {
		t.Fatal("NPC should still be allowed to reason")
	}
}

// TestDirectSpeechSilencedBySilenceProbability ports
// test_direct_speech_still_works_with_silence_probability (the Python test
// asserts a silence-probability of 1.0 suppresses the reply).
func TestDirectSpeechSilencedBySilenceProbability(t *testing.T) {
	runtime := testRuntime(t, ruleBackend(1.0), nil)
	activate(t, runtime, "npc_001")
	handle(t, runtime, map[string]any{
		"type": "player_speech", "npc_id": "npc_001", "text": "Hello there.",
	})
	if containsName(eventNames(t, runtime, "npc_001"), "NPC_SPOKE") {
		t.Fatal("silence probability 1.0 should suppress the reply")
	}
}

// TestStorySafetySuspendsAndResumes ports StorySafetyTests.
func TestStorySafetySuspendsAndResumes(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	activate(t, runtime, "npc_001")

	reply := handle(t, runtime, map[string]any{"type": "story_safety", "active": true, "reason": "mission"})
	if reply["type"] != "story_safety_ack" || reply["active"] != true {
		t.Fatalf("unexpected story safety reply: %v", reply)
	}
	if got := runtime.Ownership().State("npc_001"); got != state.StateSuspended {
		t.Fatalf("state = %s, want SUSPENDED", got)
	}
	if !containsName(eventNames(t, runtime, "npc_001"), "AGENT_SUSPENDED") {
		t.Fatalf("AGENT_SUSPENDED missing: %v", eventNames(t, runtime, "npc_001"))
	}

	handle(t, runtime, map[string]any{"type": "story_safety", "active": false, "reason": "mission_complete"})
	if got := runtime.Ownership().State("npc_001"); got != state.StateAIActive {
		t.Fatalf("state = %s, want AI_ACTIVE", got)
	}
	if !containsName(eventNames(t, runtime, "npc_001"), "AGENT_RESUMED") {
		t.Fatalf("AGENT_RESUMED missing: %v", eventNames(t, runtime, "npc_001"))
	}
}

// TestDeferredPromotionPausedWhileSuspended checks that story safety freezes
// the deferred queue: a deferred NPC is not promoted by Tick while suspended.
func TestDeferredPromotionPausedWhileSuspended(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	handle(t, runtime, map[string]any{"type": "story_safety", "active": true, "reason": "mission"})

	// Four AWARE NPCs, but only three may activate.
	for index := 1; index <= 4; index++ {
		npcID := npcName(index)
		scan(t, runtime, safePed(npcID, 15.0))
		scan(t, runtime, safePed(npcID, 8.0))
	}
	if got := runtime.Ownership().State(npcName(4)); got != state.StateAware {
		t.Fatalf("state = %s, want AWARE (suspended)", got)
	}
	runtime.Tick(nil)
	if got := runtime.Ownership().State(npcName(4)); got != state.StateAware {
		t.Fatalf("state = %s, want AWARE after tick while suspended", got)
	}
}

// TestReleaseAllReturnsOwnershipToRockstar ports “release_all“.
func TestReleaseAllReturnsOwnershipToRockstar(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	activate(t, runtime, "npc_001")
	if err := runtime.ReleaseAll(nil, "runtime_disconnect"); err != nil {
		t.Fatalf("release all: %v", err)
	}
	if got := runtime.Ownership().State("npc_001"); got != state.StateRockstar {
		t.Fatalf("state = %s, want ROCKSTAR", got)
	}
	if !containsName(eventNames(t, runtime, "npc_001"), "NPC_RELEASED") {
		t.Fatalf("NPC_RELEASED missing: %v", eventNames(t, runtime, "npc_001"))
	}
}

// TestWorldUpdateAckAndDistanceRelease ports the world_update handlers,
// including the "player walked away" release path.
func TestWorldUpdateAckAndDistanceRelease(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	activate(t, runtime, "npc_001")

	reply := handle(t, runtime, map[string]any{
		"type": "world_update", "npc_id": "npc_001",
		"state": map[string]any{"self": map[string]any{}, "player": map[string]any{"distance": 6.0}},
	})
	if reply["type"] != "world_update_ack" || reply["npc_id"] != "npc_001" {
		t.Fatalf("unexpected world update reply: %v", reply)
	}
	if got := runtime.Ownership().State("npc_001"); got != state.StateAIActive {
		t.Fatalf("state = %s, want AI_ACTIVE", got)
	}

	handle(t, runtime, map[string]any{
		"type": "world_update", "npc_id": "npc_001",
		"state": map[string]any{"self": map[string]any{}, "player": map[string]any{"distance": 55.0}},
	})
	if got := runtime.Ownership().State("npc_001"); got != state.StateRockstar {
		t.Fatalf("state = %s, want ROCKSTAR after player_left_area", got)
	}
	if !containsName(eventNames(t, runtime, "npc_001"), "NPC_RELEASED") {
		t.Fatalf("NPC_RELEASED missing: %v", eventNames(t, runtime, "npc_001"))
	}
}

// TestPushToTalkStartStop covers the push_to_talk state machine.
func TestPushToTalkStartStop(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	scan(t, runtime, safePed("npc_001", 15.0))
	scan(t, runtime, safePed("npc_001", 8.0))

	reply := handle(t, runtime, map[string]any{"type": "push_to_talk", "action": "start", "npc_id": "npc_001"})
	if reply["type"] != "push_to_talk" || reply["action"] != "start" || reply["target"] != "npc_001" {
		t.Fatalf("unexpected push_to_talk start reply: %v", reply)
	}
	if got := runtime.Ownership().State("npc_001"); got != state.StateAIConversation {
		t.Fatalf("state = %s, want AI_CONVERSATION", got)
	}
	if !containsName(eventNames(t, runtime, "npc_001"), "CONVERSATION_STARTED") {
		t.Fatalf("CONVERSATION_STARTED missing: %v", eventNames(t, runtime, "npc_001"))
	}

	// A second start is a barge-in and records CONVERSATION_INTERRUPTED.
	handle(t, runtime, map[string]any{"type": "push_to_talk", "action": "start", "npc_id": "npc_001"})
	if !containsName(eventNames(t, runtime, "npc_001"), "CONVERSATION_INTERRUPTED") {
		t.Fatalf("CONVERSATION_INTERRUPTED missing: %v", eventNames(t, runtime, "npc_001"))
	}

	reply = handle(t, runtime, map[string]any{"type": "push_to_talk", "action": "stop", "npc_id": "npc_001"})
	if reply["type"] != "push_to_talk" || reply["action"] != "stop" || reply["target"] != "npc_001" {
		t.Fatalf("unexpected push_to_talk stop reply: %v", reply)
	}
	if got := runtime.Ownership().State("npc_001"); got != state.StateAIActive {
		t.Fatalf("state = %s, want AI_ACTIVE", got)
	}
}

// TestPushToTalkWithoutTarget covers the no_valid_target reply.
func TestPushToTalkWithoutTarget(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	reply := handle(t, runtime, map[string]any{"type": "push_to_talk", "action": "start"})
	if reply["type"] != "push_to_talk" || reply["reason"] != "no_valid_target" || reply["target"] != nil {
		t.Fatalf("unexpected reply: %v", reply)
	}
	reply = handle(t, runtime, map[string]any{"type": "push_to_talk", "action": "dance"})
	if reply["type"] != "error" || reply["reason"] != "unknown push_to_talk action: 'dance'" {
		t.Fatalf("unexpected reply: %v", reply)
	}
}

// TestSelectConversationTarget scores camera alignment, distance, and existing
// conversation.
func TestSelectConversationTarget(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	close := safePed("npc_close", 4.0)
	close["metadata"] = map[string]any{"camera_alignment": 0.1}
	far := safePed("npc_far", 7.0)
	far["metadata"] = map[string]any{"camera_alignment": 0.9}
	scan(t, runtime, close, far)
	if got := runtime.selectConversationTarget(); got != "npc_far" {
		t.Fatalf("target = %q, want npc_far", got)
	}

	// Out-of-range and Rockstar-owned peds are skipped.
	scan(t, runtime, safePed("npc_far", 30.0))
	if got := runtime.selectConversationTarget(); got != "npc_close" {
		t.Fatalf("target = %q, want npc_close", got)
	}
}

// TestActionResultAcknowledgement covers handle_action_result, including
// resolving npc_id from the pending request.
func TestActionResultAcknowledgement(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	activate(t, runtime, "npc_001")

	result := runtime.Registry().Call("go_to", runtime.toolContext("npc_001"), map[string]any{"destination": "Valentine Saloon"})
	if result.Status != "started" {
		t.Fatalf("go_to status = %s, want started", result.Status)
	}
	if runtime.Dispatcher().Pending() != 1 {
		t.Fatalf("pending = %d, want 1", runtime.Dispatcher().Pending())
	}
	request, ok := runtime.Dispatcher().FindPending("go_to")
	if !ok {
		t.Fatal("pending go_to not found")
	}

	reply := handle(t, runtime, map[string]any{
		"type": "action_result", "request_id": request.RequestID, "status": "completed",
	})
	if reply["type"] != "action_result_ack" || reply["npc_id"] != "npc_001" {
		t.Fatalf("unexpected action_result reply: %v", reply)
	}
	if runtime.Dispatcher().Pending() != 0 {
		t.Fatalf("pending = %d, want 0", runtime.Dispatcher().Pending())
	}
	if !containsName(eventNames(t, runtime, "npc_001"), "ACTION_COMPLETED") {
		t.Fatalf("ACTION_COMPLETED missing: %v", eventNames(t, runtime, "npc_001"))
	}
}

// TestMessageValidationErrors pins the exact error replies.
func TestMessageValidationErrors(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	cases := []struct {
		raw    map[string]any
		reason string
	}{
		{map[string]any{"type": "game_event", "event_name": "PLAYER_SPOKE"}, "game_event missing npc_id"},
		{map[string]any{"type": "player_speech", "text": "hi"}, "player_speech missing npc_id"},
		{map[string]any{"type": "player_speech", "npc_id": "npc_001"}, "player_speech missing text"},
		{map[string]any{"type": "action_result"}, "action_result missing npc_id and no pending request"},
		{map[string]any{"type": "ped_scan", "peds": "nope"}, "ped_scan.peds must be an array"},
	}
	for _, testCase := range cases {
		reply, err := runtime.HandleMessage(nil, testCase.raw)
		if err != nil {
			t.Fatalf("HandleMessage(%v): %v", testCase.raw["type"], err)
		}
		if reply["type"] != "error" || reply["reason"] != testCase.reason {
			t.Fatalf("reply = %v, want error %q", reply, testCase.reason)
		}
	}
}

// TestHelloAck pins the hello reply payload.
func TestHelloAck(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	for _, messageType := range []string{"hello", "bridge_hello"} {
		reply := handle(t, runtime, map[string]any{"type": messageType})
		if reply["type"] != "hello_ack" {
			t.Fatalf("%s reply = %v", messageType, reply)
		}
		if _, ok := reply["server_time"].(float64); !ok {
			t.Fatalf("%s reply missing server_time: %v", messageType, reply)
		}
	}
	reply := handle(t, runtime, map[string]any{"type": "??"})
	if reply["reason"] != "unknown message type: '??'" {
		t.Fatalf("unknown type reply = %v", reply)
	}
}

// TestAgentStateRehydratesGoalFromTimeline mirrors “_agent_state“.
func TestAgentStateRehydratesGoalFromTimeline(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	appendGoal(t, runtime, "npc_009", "GOAL_CREATED", "finish the day's work")
	state := runtime.State("npc_009")
	if state.CurrentGoal == nil || *state.CurrentGoal != "finish the day's work" {
		t.Fatalf("goal = %v, want rehydrated goal", state.CurrentGoal)
	}
	if state.Mood != "neutral" {
		t.Fatalf("mood = %q, want neutral", state.Mood)
	}

	// The newest goal event wins; an older goal does not survive a newer
	// GOAL_COMPLETED.
	appendGoal(t, runtime, "npc_010", "GOAL_CREATED", "old")
	if _, err := runtime.Timeline().AppendEvent("npc_010", "GOAL_COMPLETED", timeline.AppendOptions{Tags: []string{"goal"}}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if got := runtime.State("npc_010").CurrentGoal; got != nil {
		t.Fatalf("goal = %v, want nil after GOAL_COMPLETED", *got)
	}
	// State is cached after the first lookup, exactly like Python's
	// ``_agent_state``; a later goal event does not retroactively change it.
	appendGoal(t, runtime, "npc_010", "GOAL_CHANGED", "fresh")
	if got := runtime.State("npc_010").CurrentGoal; got != nil {
		t.Fatalf("goal = %v, want cached nil", *got)
	}
}

// TestStateReturnsSnapshot guards against callers mutating runtime state.
func TestStateReturnsSnapshot(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	snapshot := runtime.State("npc_001")
	snapshot.Mood = "mutated"
	snapshot.QuestContext["x"] = 1
	if runtime.State("npc_001").Mood != "neutral" {
		t.Fatal("State() leaked internal state")
	}
	if len(runtime.State("npc_001").QuestContext) != 0 {
		t.Fatal("State() leaked the quest context map")
	}
}

// TestBuildContextFlags covers the context flags, wiki context, and available
// tools surfaces.
func TestBuildContextFlags(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	activate(t, runtime, "npc_001")

	context := runtime.BuildContext("npc_001", "meaningful_event:PLAYER_LOOKED_AT_NPC", map[string]any{
		"seq": 4, "event_name": "PLAYER_LOOKED_AT_NPC", "npc_id": "npc_001", "data": map[string]any{},
	})
	if context.Flags["reason"] != "meaningful_event:PLAYER_LOOKED_AT_NPC" {
		t.Fatalf("reason flag = %v", context.Flags["reason"])
	}
	if context.Flags["ownership_state"] != "AI_ACTIVE" {
		t.Fatalf("ownership_state flag = %v", context.Flags["ownership_state"])
	}
	if _, ok := context.Flags["trigger_event"].(map[string]any); !ok {
		t.Fatalf("trigger_event flag missing: %v", context.Flags)
	}
	if len(context.AvailableTools) == 0 {
		t.Fatal("available tools should not be empty")
	}
	if len(context.RecentEvents) == 0 {
		t.Fatal("recent events should not be empty")
	}
}

// TestConcurrentHandleMessageIsSafe exercises the IPC pattern: several
// connections drive the same runtime at once. Run with -race.
func TestConcurrentHandleMessageIsSafe(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	done := make(chan struct{})
	for worker := 0; worker < 4; worker++ {
		npcID := npcName(worker + 1)
		go func() {
			defer func() { done <- struct{}{} }()
			for step := 0; step < 8; step++ {
				if _, err := runtime.HandleMessage(nil, map[string]any{
					"type": "ped_scan",
					"peds": []any{safePed(npcID, 8.0)},
				}); err != nil {
					t.Errorf("concurrent ped_scan: %v", err)
					return
				}
				if _, err := runtime.HandleMessage(nil, map[string]any{
					"type": "game_event", "event_name": "PLAYER_LOOKED_AT_NPC",
					"npc_id": npcID, "data": map[string]any{},
				}); err != nil {
					t.Errorf("concurrent game_event: %v", err)
					return
				}
				_ = runtime.State(npcID)
			}
		}()
	}
	for worker := 0; worker < 4; worker++ {
		<-done
	}
	// The scheduler cap still holds under concurrency: exactly three NPCs may
	// be active at once, and the rest wait in AWARE.
	if got := len(activeNPCs(runtime)); got != 3 {
		t.Fatalf("active = %d, want 3", got)
	}
	for _, npcID := range npcNames(4) {
		switch got := runtime.Ownership().State(npcID); got {
		case state.StateAIActive, state.StateAware:
		default:
			t.Fatalf("%s state = %s, want AI_ACTIVE or AWARE", npcID, got)
		}
	}
	for _, npcID := range npcNames(4) {
		if _, err := runtime.Timeline().AllEvents(npcID); err != nil {
			t.Fatalf("timeline for %s is corrupt: %v", npcID, err)
		}
	}
}
