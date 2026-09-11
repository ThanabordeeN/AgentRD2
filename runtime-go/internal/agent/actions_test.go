package agent

import (
	"testing"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/timeline"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/tools"
)

// floatSliceOf converts a decision position argument for assertions.
func floatSliceOf(value any) []float64 {
	position, _ := coercePosition(value)
	return position
}

func action(tool string, arguments map[string]any) domain.AgentAction {
	if arguments == nil {
		arguments = map[string]any{}
	}
	return domain.AgentAction{Tool: tool, Arguments: arguments}
}

func decisionWith(actions ...domain.AgentAction) *domain.AgentDecision {
	decision := domain.NewAgentDecision()
	decision.Actions = actions
	return decision
}

func stabilizedActions(t *testing.T, runtime *Runtime, npcID string, decision *domain.AgentDecision) []domain.AgentAction {
	t.Helper()
	stabilized, err := runtime.stabilizeDecision(npcID, decision)
	if err != nil {
		t.Fatalf("stabilize: %v", err)
	}
	return stabilized.Actions
}

// TestEntityArgumentsDefaultToPlayer ports
// test_action_stabilization.test_entity_arguments_default_to_player.
func TestEntityArgumentsDefaultToPlayer(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	actions := stabilizedActions(t, runtime, "npc_001", decisionWith(action("face", nil), action("look_at", nil)))
	if len(actions) != 2 || actions[0].Tool != "face" || actions[1].Tool != "look_at" {
		t.Fatalf("actions = %v", actions)
	}
	for _, result := range actions {
		if result.Arguments["entity"] != "player" {
			t.Fatalf("%s entity = %v, want player", result.Tool, result.Arguments["entity"])
		}
	}

	// follow and flee_from get the same default.
	actions = stabilizedActions(t, runtime, "npc_001", decisionWith(action("follow", nil), action("flee_from", nil)))
	for _, result := range actions {
		if result.Arguments["entity"] != "player" {
			t.Fatalf("%s entity = %v, want player", result.Tool, result.Arguments["entity"])
		}
	}
}

// TestWaitDurationDefaults ports test_wait_duration_defaults.
func TestWaitDurationDefaults(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	actions := stabilizedActions(t, runtime, "npc_001", decisionWith(action("wait", nil)))
	if len(actions) != 1 || domain.FloatFrom(actions[0].Arguments["duration"]) != 2.0 {
		t.Fatalf("actions = %v", actions)
	}
	// An unconvertible duration falls back too.
	actions = stabilizedActions(t, runtime, "npc_001", decisionWith(action("wait", map[string]any{"duration": "soon"})))
	if domain.FloatFrom(actions[0].Arguments["duration"]) != 2.0 {
		t.Fatalf("duration = %v, want 2.0", actions[0].Arguments["duration"])
	}
	// A numeric string converts like Python's float().
	actions = stabilizedActions(t, runtime, "npc_001", decisionWith(action("wait", map[string]any{"duration": "4.5"})))
	if domain.FloatFrom(actions[0].Arguments["duration"]) != 4.5 {
		t.Fatalf("duration = %v, want 4.5", actions[0].Arguments["duration"])
	}
}

// TestWanderRadiusDefaults covers the wander radius guard.
func TestWanderRadiusDefaults(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	actions := stabilizedActions(t, runtime, "npc_001", decisionWith(action("wander", nil)))
	if len(actions) != 1 || domain.FloatFrom(actions[0].Arguments["radius"]) != 8.0 {
		t.Fatalf("actions = %v", actions)
	}
	actions = stabilizedActions(t, runtime, "npc_001", decisionWith(action("wander", map[string]any{"radius": 12})))
	if domain.FloatFrom(actions[0].Arguments["radius"]) != 12.0 {
		t.Fatalf("radius = %v, want 12", actions[0].Arguments["radius"])
	}
}

// TestGoToWithoutDestinationFallsBackToWander ports
// test_go_to_without_destination_falls_back_to_wander.
func TestGoToWithoutDestinationFallsBackToWander(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	decision := decisionWith(action("go_to", nil))
	goal := "walk to the saloon"
	decision.Goal = &goal
	actions := stabilizedActions(t, runtime, "npc_001", decision)
	if len(actions) != 1 || actions[0].Tool != "wander" {
		t.Fatalf("actions = %v", actions)
	}
	if domain.FloatFrom(actions[0].Arguments["radius"]) != 8.0 {
		t.Fatalf("radius = %v, want 8.0", actions[0].Arguments["radius"])
	}
}

// TestGoToWithoutDestinationOrGoalIsDropped covers the dropped branch and the
// internal-destination rescue.
func TestGoToWithoutDestinationOrGoalIsDropped(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	actions := stabilizedActions(t, runtime, "npc_001", decisionWith(action("go_to", nil)))
	if len(actions) != 0 {
		t.Fatalf("actions = %v, want none", actions)
	}
	failures := failedActions(t, runtime, "npc_001")
	if len(failures) != 1 || failures[0]["reason"] != "go_to_missing_destination" {
		t.Fatalf("failures = %v", failures)
	}

	decision := decisionWith(action("go_to", nil))
	decision.Internal["destination"] = "Valentine Saloon"
	actions = stabilizedActions(t, runtime, "npc_001", decision)
	if len(actions) != 1 || actions[0].Arguments["destination"] != "Valentine Saloon" {
		t.Fatalf("actions = %v", actions)
	}
}

// TestInvestigateUsesRecentPosition ports
// test_investigate_uses_recent_position.
func TestInvestigateUsesRecentPosition(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	if _, err := runtime.Timeline().AppendEvent("npc_001", "GUNSHOT_HEARD", timeline.AppendOptions{
		Data: map[string]any{"position": []any{100.0, 200.0, 10.0}},
		Tags: []string{"world"},
	}); err != nil {
		t.Fatalf("append: %v", err)
	}
	actions := stabilizedActions(t, runtime, "npc_001", decisionWith(action("investigate", nil)))
	if len(actions) != 1 || actions[0].Tool != "investigate" {
		t.Fatalf("actions = %v", actions)
	}
	position := floatSliceOf(actions[0].Arguments["position"])
	if len(position) != 3 || position[0] != 100 || position[1] != 200 || position[2] != 10 {
		t.Fatalf("position = %v, want [100 200 10]", actions[0].Arguments["position"])
	}

	// Without any recorded position the action is dropped.
	actions = stabilizedActions(t, runtime, "npc_002", decisionWith(action("investigate", nil)))
	if len(actions) != 0 {
		t.Fatalf("actions = %v, want none", actions)
	}
	if failures := failedActions(t, runtime, "npc_002"); len(failures) != 1 || failures[0]["reason"] != "investigate_missing_position" {
		t.Fatalf("failures = %v", failures)
	}
}

// TestDuplicateEmptySayIsDroppedSilently ports
// test_duplicate_empty_say_is_dropped_silently.
func TestDuplicateEmptySayIsDroppedSilently(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	decision := decisionWith(action("say", nil))
	decision.Speech = &domain.AgentSpeech{Text: "Evening."}
	actions := stabilizedActions(t, runtime, "npc_001", decision)
	if len(actions) != 0 {
		t.Fatalf("actions = %v, want none", actions)
	}
	if len(failedActions(t, runtime, "npc_001")) != 0 {
		t.Fatal("a duplicate say must not be logged as ACTION_FAILED")
	}
}

// TestSayMissingTextIsLogged covers the say_missing_text guard.
func TestSayMissingTextIsLogged(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	actions := stabilizedActions(t, runtime, "npc_001", decisionWith(action("say", map[string]any{"text": "   "})))
	if len(actions) != 0 {
		t.Fatalf("actions = %v, want none", actions)
	}
	failures := failedActions(t, runtime, "npc_001")
	if len(failures) != 1 || failures[0]["reason"] != "say_missing_text" {
		t.Fatalf("failures = %v", failures)
	}

	actions = stabilizedActions(t, runtime, "npc_001", decisionWith(action("say", map[string]any{"text": "Evening."})))
	if len(actions) != 1 || actions[0].Tool != "say" {
		t.Fatalf("actions = %v", actions)
	}
}

// TestUnsupportedActionIsLoggedAndDropped ports
// test_unsupported_action_is_logged_and_dropped.
func TestUnsupportedActionIsLoggedAndDropped(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	actions := stabilizedActions(t, runtime, "npc_001", decisionWith(action("attack", nil)))
	if len(actions) != 0 {
		t.Fatalf("actions = %v, want none", actions)
	}
	events := allEvents(t, runtime, "npc_001")
	last := events[len(events)-1]
	if last.EventName != "ACTION_FAILED" || last.Data["reason"] != "unsupported_tool" {
		t.Fatalf("last event = %v %v", last.EventName, last.Data)
	}
}

// TestStopAndClearAttentionClearArguments covers the argument-clearing tools.
func TestStopAndClearAttentionClearArguments(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	actions := stabilizedActions(t, runtime, "npc_001", decisionWith(
		action("stop", map[string]any{"reason": "ignore"}),
		action("clear_attention", map[string]any{"entity": "player"}),
	))
	if len(actions) != 2 {
		t.Fatalf("actions = %v", actions)
	}
	for _, result := range actions {
		if len(result.Arguments) != 0 {
			t.Fatalf("%s arguments = %v, want empty", result.Tool, result.Arguments)
		}
	}
}

// TestGestureTypeDefaults covers the gesture default.
func TestGestureTypeDefaults(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	actions := stabilizedActions(t, runtime, "npc_001", decisionWith(action("gesture", nil)))
	if len(actions) != 1 || actions[0].Arguments["type"] != "neutral" {
		t.Fatalf("actions = %v", actions)
	}
}

// TestLowCourageThreatGetsFleeAction ports
// test_low_courage_threat_gets_flee_action.
func TestLowCourageThreatGetsFleeAction(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	writeProfile(t, runtime, "npc_001", 0.44)
	decision := decisionWith(
		action("go_to", map[string]any{"destination": "Valentine"}),
		action("wander", map[string]any{"radius": 20}),
	)
	agentContext := domain.NewAgentContext("npc_001")
	agentContext.Flags = map[string]any{"trigger_event": map[string]any{
		"event_name": "PLAYER_THREATENED_NPC",
		"data":       map[string]any{"weapon": "revolver", "distance": 2.0},
	}}
	result := runtime.applyBehaviorGuardrails("npc_001", decision, agentContext)

	if len(result.Actions) != 1 || result.Actions[0].Tool != "flee_from" {
		t.Fatalf("actions = %v, want only flee_from", result.Actions)
	}
	if result.Actions[0].Arguments["entity"] != "player" {
		t.Fatalf("flee_from entity = %v", result.Actions[0].Arguments["entity"])
	}
	if result.MoodText() != "afraid" {
		t.Fatalf("mood = %q, want afraid", result.MoodText())
	}
	if result.GoalText() != "get away from the player" {
		t.Fatalf("goal = %q", result.GoalText())
	}
}

// TestHighCourageThreatGetsFaceAction covers the other half of the threat
// guardrail.
func TestHighCourageThreatGetsFaceAction(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	writeProfile(t, runtime, "npc_hunter", 0.82)
	decision := decisionWith()
	agentContext := domain.NewAgentContext("npc_hunter")
	agentContext.Flags = map[string]any{"trigger_event": map[string]any{
		"event_name": "PLAYER_ATTACKED_NPC",
		"data":       map[string]any{"distance": 2.0},
	}}
	result := runtime.applyBehaviorGuardrails("npc_hunter", decision, agentContext)
	if len(result.Actions) != 1 || result.Actions[0].Tool != "face" {
		t.Fatalf("actions = %v, want face", result.Actions)
	}
	if result.Mood != nil || result.Goal != nil {
		t.Fatalf("high-courage guardrail must not invent mood/goal: %v %v", result.Mood, result.Goal)
	}
}

// TestHighCourageGunshotGetsInvestigateAction ports
// test_high_courage_gunshot_gets_investigate_action.
func TestHighCourageGunshotGetsInvestigateAction(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	writeProfile(t, runtime, "npc_hunter", 0.82)
	decision := decisionWith(action("wander", map[string]any{"radius": 5.0}))
	agentContext := domain.NewAgentContext("npc_hunter")
	agentContext.Flags = map[string]any{"trigger_event": map[string]any{
		"event_name": "GUNSHOT_HEARD",
		"data":       map[string]any{"position": []any{100.0, 200.0, 10.0}},
	}}
	result := runtime.applyBehaviorGuardrails("npc_hunter", decision, agentContext)

	investigate := domain.AgentAction{}
	found := false
	for _, candidate := range result.Actions {
		if candidate.Tool == "investigate" {
			investigate = candidate
			found = true
		}
		if candidate.Tool == "wander" {
			t.Fatal("wander should be replaced by investigate")
		}
	}
	if !found {
		t.Fatalf("actions = %v, want investigate", result.Actions)
	}
	position := floatSliceOf(investigate.Arguments["position"])
	if len(position) != 3 || position[0] != 100 || position[1] != 200 || position[2] != 10 {
		t.Fatalf("position = %v", investigate.Arguments["position"])
	}
	if result.MoodText() != "alert" || result.GoalText() != "investigate the gunshot" {
		t.Fatalf("mood/goal = %q/%q", result.MoodText(), result.GoalText())
	}
}

// TestLowCourageGunshotWandersAway covers the fright branch.
func TestLowCourageGunshotWandersAway(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	writeProfile(t, runtime, "npc_coward", 0.3)
	decision := decisionWith()
	agentContext := domain.NewAgentContext("npc_coward")
	agentContext.Flags = map[string]any{"trigger_event": map[string]any{
		"event_name": "GUNSHOT_HEARD",
		"data":       map[string]any{"position": []any{100.0, 200.0, 10.0}},
	}}
	result := runtime.applyBehaviorGuardrails("npc_coward", decision, agentContext)
	if len(result.Actions) != 1 || result.Actions[0].Tool != "wander" {
		t.Fatalf("actions = %v, want wander", result.Actions)
	}
	if domain.FloatFrom(result.Actions[0].Arguments["radius"]) != 12.0 {
		t.Fatalf("radius = %v, want 12.0", result.Actions[0].Arguments["radius"])
	}
	if result.MoodText() != "afraid" {
		t.Fatalf("mood = %q, want afraid", result.MoodText())
	}
}

// TestSpeechPolicyCooldowns covers the local and global speech cooldowns.
func TestSpeechPolicyCooldowns(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	state := liveState(runtime, "npc_001")

	// Local cooldown suppresses ordinary autonomous speech.
	state.LastSpokenAt = runtime.now() - 1
	if got := runtime.applySpeechPolicy("npc_001", speechDecision(), "idle_autonomous"); got.Speech != nil {
		t.Fatal("local speech cooldown should suppress speech")
	}

	// Global cooldown suppresses a second speaker.
	state.LastSpokenAt = 0
	runtime.lastGlobalSpeechAt = runtime.now() - 1
	if got := runtime.applySpeechPolicy("npc_001", speechDecision(), "idle_autonomous"); got.Speech != nil {
		t.Fatal("global speech cooldown should suppress speech")
	}

	// After both cooldowns elapse, speech survives.
	runtime.lastGlobalSpeechAt = 0
	if got := runtime.applySpeechPolicy("npc_001", speechDecision(), "idle_autonomous"); got.Speech == nil {
		t.Fatal("speech should survive once cooldowns elapse")
	}
}

// TestSpeechPolicyBypassesCooldown covers direct answers and urgent reactions.
func TestSpeechPolicyBypassesCooldown(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	state := liveState(runtime, "npc_001")
	state.LastSpokenAt = runtime.now()
	runtime.lastGlobalSpeechAt = runtime.now()

	for _, reason := range []string{
		"push_to_talk",
		"player_spoke",
		"meaningful_event:PLAYER_THREATENED_NPC",
		"meaningful_event:PLAYER_ATTACKED_NPC",
		"meaningful_event:NPC_DAMAGED",
		"meaningful_event:GUNSHOT_HEARD",
	} {
		if got := runtime.applySpeechPolicy("npc_001", speechDecision(), reason); got.Speech == nil {
			t.Fatalf("reason %q should bypass the speech cooldown", reason)
		}
	}
	// No speech means no policy work at all.
	decision := domain.NewAgentDecision()
	if got := runtime.applySpeechPolicy("npc_001", decision, "idle_autonomous"); got != decision {
		t.Fatal("decision without speech should pass through")
	}
}

func speechDecision() *domain.AgentDecision {
	decision := domain.NewAgentDecision()
	decision.Speech = &domain.AgentSpeech{Text: "Hello there."}
	return decision
}

// TestSpeechRequiresOwnership covers “_speech_allowed“.
func TestSpeechRequiresOwnership(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	if runtime.speechAllowed("npc_001") {
		t.Fatal("a Rockstar-owned NPC must not speak")
	}
	activate(t, runtime, "npc_001")
	if !runtime.speechAllowed("npc_001") {
		t.Fatal("an AI_ACTIVE NPC may speak")
	}
}

// TestExecuteDecisionBlocksMovementInDialogueOnly covers the quest overlay
// filtering inside execute_decision.
func TestExecuteDecisionBlocksMovementInDialogueOnly(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	writeQuest(t, runtime, "quest_valentine_livestock")
	scan(t, runtime, questPed("npc_quest_giver", 15.0))
	scan(t, runtime, questPed("npc_quest_giver", 8.0))

	decision := decisionWith(
		action("go_to", map[string]any{"destination": "Valentine Saloon"}),
		action("say", map[string]any{"text": "Keep them cattle movin'."}),
	)
	if _, err := runtime.executeDecision("npc_quest_giver", decision); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(decision.Actions) != 1 || decision.Actions[0].Tool != "say" {
		t.Fatalf("actions = %v, want only say", decision.Actions)
	}
	failures := failedActions(t, runtime, "npc_quest_giver")
	if len(failures) != 1 || failures[0]["reason"] != "dialogue_only_mode" {
		t.Fatalf("failures = %v", failures)
	}
	if failures[0]["message"] != "Rockstar owns quest NPC actions" {
		t.Fatalf("message = %v", failures[0]["message"])
	}
	if !containsName(startedTools(t, runtime, "npc_quest_giver"), "say") {
		t.Fatalf("say was not dispatched: %v", startedTools(t, runtime, "npc_quest_giver"))
	}
	if containsName(startedTools(t, runtime, "npc_quest_giver"), "go_to") {
		t.Fatal("go_to must never reach the bridge in dialogue-only mode")
	}
}

// TestExecuteDecisionRecordsGoalLifecycle covers GOAL_CREATED/GOAL_CHANGED.
func TestExecuteDecisionRecordsGoalLifecycle(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	activate(t, runtime, "npc_001")

	first := "watch the herd"
	decision := domain.NewAgentDecision()
	decision.Goal = &first
	if _, err := runtime.executeDecision("npc_001", decision); err != nil {
		t.Fatalf("execute: %v", err)
	}
	names := eventNames(t, runtime, "npc_001")
	if !containsName(names, "GOAL_CREATED") {
		t.Fatalf("GOAL_CREATED missing: %v", names)
	}

	second := "bring the cattle in"
	decision = domain.NewAgentDecision()
	decision.Goal = &second
	if _, err := runtime.executeDecision("npc_001", decision); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !containsName(eventNames(t, runtime, "npc_001"), "GOAL_CHANGED") {
		t.Fatalf("GOAL_CHANGED missing: %v", eventNames(t, runtime, "npc_001"))
	}
	if got := runtime.State("npc_001").CurrentGoal; got == nil || *got != second {
		t.Fatalf("goal = %v, want %q", got, second)
	}

	// GOAL_COMPLETED clears the goal through _react_to_event.
	gameEvent(t, runtime, "npc_001", "GOAL_COMPLETED", nil)
	if got := runtime.State("npc_001").CurrentGoal; got != nil {
		t.Fatalf("goal = %v, want nil after GOAL_COMPLETED", *got)
	}
}

// TestActionFailedSchedulesRecovery covers the ACTION_FAILED reaction.
func TestActionFailedSchedulesRecovery(t *testing.T) {
	decider := quietBackend()
	runtime := testRuntime(t, decider, nil)
	activate(t, runtime, "npc_001")
	callsBefore := decider.calls
	gameEvent(t, runtime, "npc_001", "ACTION_FAILED", map[string]any{"tool": "go_to", "reason": "blocked"})
	if decider.calls != callsBefore+1 {
		t.Fatalf("backend calls = %d, want %d after ACTION_FAILED", decider.calls, callsBefore+1)
	}
}

// TestDispatcherTracksPendingActions covers the dispatcher's pending set,
// transient tools, resolve, and the transport payload.
func TestDispatcherTracksPendingActions(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	dispatcher := runtime.Dispatcher()
	sent := []map[string]any{}
	dispatcher.SetSend(func(payload map[string]any) { sent = append(sent, payload) })

	request := domain.NewActionRequest("go_to", map[string]any{"destination": "Valentine Saloon"}, "npc_001")
	result := dispatcher.Dispatch(request)
	if result.Status != domain.ActionStarted || result.Tool != "go_to" {
		t.Fatalf("dispatch result = %v", result)
	}
	if dispatcher.Pending() != 1 {
		t.Fatalf("pending = %d, want 1", dispatcher.Pending())
	}
	if len(sent) != 1 || sent[0]["type"] != "action_request" || sent[0]["npc_id"] != "npc_001" {
		t.Fatalf("sent payloads = %v", sent)
	}
	envelope := domain.MapFrom(sent[0]["request"])
	if envelope["tool"] != "go_to" || envelope["request_id"] != request.RequestID {
		t.Fatalf("envelope = %v", envelope)
	}

	// Transient tools never become pending.
	think := domain.NewActionRequest("think", map[string]any{"style": "ponder"}, "npc_001")
	dispatcher.Dispatch(think)
	if dispatcher.Pending() != 1 {
		t.Fatalf("pending = %d, want 1 after transient think", dispatcher.Pending())
	}

	// FindPending returns the oldest matching request.
	second := domain.NewActionRequest("go_to", map[string]any{"destination": "camp"}, "npc_001")
	dispatcher.Dispatch(second)
	found, ok := dispatcher.FindPending("go_to")
	if !ok || found.RequestID != request.RequestID {
		t.Fatalf("FindPending = %v/%v, want oldest request", found.RequestID, ok)
	}
	if _, ok := dispatcher.FindPending("wander"); ok {
		t.Fatal("FindPending found a tool that was never dispatched")
	}

	// Resolve pops the request and reports unknown ids as missing.
	resolved, ok := dispatcher.Resolve(map[string]any{"request_id": request.RequestID})
	if !ok || resolved.RequestID != request.RequestID {
		t.Fatalf("resolve = %v/%v", resolved.RequestID, ok)
	}
	if _, ok := dispatcher.Resolve(map[string]any{"request_id": request.RequestID}); ok {
		t.Fatal("resolving twice must not succeed")
	}
	if _, ok := dispatcher.Resolve(map[string]any{}); ok {
		t.Fatal("resolving without a request id must not succeed")
	}
	if dispatcher.Pending() != 1 {
		t.Fatalf("pending = %d, want 1", dispatcher.Pending())
	}
}

// TestDispatcherSayRecordsNpcSpoke covers the say side effects.
func TestDispatcherSayRecordsNpcSpoke(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	request := domain.NewActionRequest("say", map[string]any{
		"text": "Evening.", "target": "player", "emotion": "friendly",
	}, "npc_001")
	runtime.Dispatcher().Dispatch(request)

	names := eventNames(t, runtime, "npc_001")
	if !containsName(names, "ACTION_STARTED") || !containsName(names, "NPC_SPOKE") {
		t.Fatalf("events = %v", names)
	}
	spoke := map[string]any(nil)
	for _, event := range allEvents(t, runtime, "npc_001") {
		if event.EventName == "NPC_SPOKE" {
			spoke = event.Data
		}
	}
	if spoke["text"] != "Evening." || spoke["target"] != "player" || spoke["emotion"] != "friendly" {
		t.Fatalf("NPC_SPOKE data = %v", spoke)
	}
	// say is not transient: it stays pending until the bridge answers.
	if runtime.Dispatcher().Pending() != 1 {
		t.Fatalf("pending = %d, want 1", runtime.Dispatcher().Pending())
	}
}

// TestExecutionFailureIsContained covers the panic containment in callTool.
func TestExecutionFailureIsContained(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	activate(t, runtime, "npc_001")
	// Unknown tools reach callTool only when stabilization could not drop them.
	result, err := runtime.callTool("definitely_not_a_tool", runtime.toolContext("npc_001"), nil)
	if err != nil {
		t.Fatalf("callTool: %v", err)
	}
	if result.Status != domain.ActionFailed {
		t.Fatalf("status = %s, want failed", result.Status)
	}
	failures := failedActions(t, runtime, "npc_001")
	found := false
	for _, failure := range failures {
		if failure["reason"] == "KeyError" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a KeyError ACTION_FAILED record, got %v", failures)
	}
}

// TestSayOmitsEmptyOptionalArguments pins the Optional[str] -> "" mapping:
// Python's AgentSpeech(target=None) must not send a blank target to the bridge.
func TestSayOmitsEmptyOptionalArguments(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	activate(t, runtime, "npc_001")

	decision := domain.NewAgentDecision()
	decision.Speech = &domain.AgentSpeech{Text: "What the hell was that?", Emotion: "alert"}
	if _, err := runtime.executeDecision("npc_001", decision); err != nil {
		t.Fatalf("execute: %v", err)
	}

	arguments := map[string]any(nil)
	for _, event := range allEvents(t, runtime, "npc_001") {
		if event.EventName != "ACTION_STARTED" || domain.StringFrom(event.Data["tool"]) != "say" {
			continue
		}
		arguments = domain.MapFrom(event.Data["arguments"])
	}
	if arguments == nil {
		t.Fatal("say was not dispatched")
	}
	if _, present := arguments["target"]; present {
		t.Fatalf("empty target must be omitted, got %v", arguments)
	}
	if arguments["text"] != "What the hell was that?" || arguments["emotion"] != "alert" {
		t.Fatalf("arguments = %v", arguments)
	}

	// A non-empty target is forwarded.
	decision = domain.NewAgentDecision()
	decision.Speech = &domain.AgentSpeech{Text: "Evening.", Target: "player"}
	if _, err := runtime.executeDecision("npc_001", decision); err != nil {
		t.Fatalf("execute: %v", err)
	}
	targets := []any{}
	for _, event := range allEvents(t, runtime, "npc_001") {
		if event.EventName != "ACTION_STARTED" || domain.StringFrom(event.Data["tool"]) != "say" {
			continue
		}
		targets = append(targets, domain.MapFrom(event.Data["arguments"])["target"])
	}
	if len(targets) != 2 || targets[1] != "player" {
		t.Fatalf("targets = %v, want the last say to carry player", targets)
	}
}

// TestPanickingToolIsContained keeps a misbehaving tool handler from killing
// the runtime (Python catches every exception in “_call_tool“).
func TestPanickingToolIsContained(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	activate(t, runtime, "npc_001")
	if err := runtime.Registry().Register(tools.Spec{
		Name: "explode",
		Handler: func(*tools.Context, map[string]any) domain.ToolResult {
			panic("boom")
		},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	result, err := runtime.callTool("explode", runtime.toolContext("npc_001"), nil)
	if err != nil {
		t.Fatalf("callTool must contain the panic: %v", err)
	}
	if result.Status != domain.ActionFailed || result.Reason != "boom" {
		t.Fatalf("result = %v", result)
	}
	found := false
	for _, failure := range failedActions(t, runtime, "npc_001") {
		if failure["reason"] == "panic" && failure["message"] == "boom" {
			found = true
		}
	}
	if !found {
		t.Fatalf("panic was not recorded on the timeline")
	}
}
