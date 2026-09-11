package agent

import (
	"context"
	"testing"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/config"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/state"
)

func npcNames(count int) []string {
	names := make([]string, 0, count)
	for index := 1; index <= count; index++ {
		names = append(names, npcName(index))
	}
	return names
}

func pedsAt(ids []string, distance float64) []map[string]any {
	peds := make([]map[string]any, 0, len(ids))
	for _, npcID := range ids {
		peds = append(peds, safePed(npcID, distance))
	}
	return peds
}

func activeNPCs(runtime *Runtime) []string {
	active := []string{}
	for _, npcID := range runtime.Ownership().OwnedNPCs() {
		switch runtime.Ownership().State(npcID) {
		case state.StateAIActive, state.StateAIConversation:
			active = append(active, npcID)
		}
	}
	return active
}

func awareNPCs(runtime *Runtime) []string {
	aware := []string{}
	for _, npcID := range runtime.Ownership().OwnedNPCs() {
		if runtime.Ownership().State(npcID) == state.StateAware {
			aware = append(aware, npcID)
		}
	}
	return aware
}

// TestManyNearbyNpcsAreAwareWithoutLLMCalls ports
// test_scheduler_limits.test_many_nearby_npcs_are_aware_without_llm_calls.
func TestManyNearbyNpcsAreAwareWithoutLLMCalls(t *testing.T) {
	decider := quietBackend()
	runtime := testRuntime(t, decider, nil)
	npcs := npcNames(10)
	scan(t, runtime, pedsAt(npcs, 15.0)...)
	scan(t, runtime, pedsAt(npcs, 8.0)...)

	if got := len(awareNPCs(runtime)); got != len(npcs) {
		t.Fatalf("aware = %d, want %d", got, len(npcs))
	}
	if decider.calls != 0 {
		t.Fatalf("backend calls = %d, want 0", decider.calls)
	}
}

// TestSchedulerCapsActiveReasoningAgents ports
// test_scheduler_caps_active_reasoning_agents.
func TestSchedulerCapsActiveReasoningAgents(t *testing.T) {
	decider := quietBackend()
	runtime := testRuntime(t, decider, nil)
	npcs := npcNames(10)
	scan(t, runtime, pedsAt(npcs, 15.0)...)
	scan(t, runtime, pedsAt(npcs, 8.0)...)
	for _, npcID := range npcs {
		gameEvent(t, runtime, npcID, "PLAYER_LOOKED_AT_NPC", nil)
	}

	if got := len(activeNPCs(runtime)); got != 3 {
		t.Fatalf("active = %d, want 3", got)
	}
	if got := len(awareNPCs(runtime)); got != 7 {
		t.Fatalf("aware = %d, want 7", got)
	}
	if decider.calls != 3 {
		t.Fatalf("backend calls = %d, want 3", decider.calls)
	}
	if !containsName(eventNames(t, runtime, npcName(4)), "AGENT_ACTIVATION_DEFERRED") {
		t.Fatalf("AGENT_ACTIVATION_DEFERRED missing for %s", npcName(4))
	}
	deferred := deferredEventData(t, runtime, npcName(4))
	if deferred["reason"] != "scheduler_cap" {
		t.Fatalf("deferred reason = %v, want scheduler_cap", deferred["reason"])
	}
	if domain.IntFrom(deferred["active_agents"]) != 3 || domain.IntFrom(deferred["max_active_agents"]) != 3 {
		t.Fatalf("deferred caps = %v/%v, want 3/3", deferred["active_agents"], deferred["max_active_agents"])
	}
}

func deferredEventData(t *testing.T, runtime *Runtime, npcID string) map[string]any {
	t.Helper()
	for _, event := range allEvents(t, runtime, npcID) {
		if event.EventName == "AGENT_ACTIVATION_DEFERRED" {
			return event.Data
		}
	}
	t.Fatalf("no AGENT_ACTIVATION_DEFERRED for %s", npcID)
	return nil
}

// TestDeferredAgentIsPromotedWhenSlotFrees ports
// test_deferred_agent_is_promoted_when_slot_frees.
func TestDeferredAgentIsPromotedWhenSlotFrees(t *testing.T) {
	decider := quietBackend()
	runtime := testRuntime(t, decider, nil)
	npcs := npcNames(4)
	for _, npcID := range npcs {
		scan(t, runtime, safePed(npcID, 15.0))
	}
	for _, npcID := range npcs {
		scan(t, runtime, safePed(npcID, 8.0))
	}
	for _, npcID := range npcs {
		gameEvent(t, runtime, npcID, "PLAYER_LOOKED_AT_NPC", nil)
	}

	if decider.calls != 3 {
		t.Fatalf("backend calls = %d, want 3", decider.calls)
	}
	if got := runtime.Ownership().State(npcs[3]); got != state.StateAware {
		t.Fatalf("state = %s, want AWARE", got)
	}

	// Release one active agent; the deferred one should take its slot.
	firstActive := activeNPCs(runtime)[0]
	transition := runtime.Ownership().RequestRelease(firstActive, "test_release")
	if transition == nil {
		t.Fatal("request release returned no transition")
	}
	if _, err := runtime.recordTransition(context.Background(), transition, nil, nil); err != nil {
		t.Fatalf("record transition: %v", err)
	}
	if got := runtime.Ownership().State(npcs[3]); got != state.StateAIActive {
		t.Fatalf("state = %s, want AI_ACTIVE after promotion", got)
	}
	if decider.calls != 4 {
		t.Fatalf("backend calls = %d, want 4", decider.calls)
	}
}

// TestGlobalSpeechCooldownPreventsChorus ports
// test_global_speech_cooldown_prevents_chorus.
func TestGlobalSpeechCooldownPreventsChorus(t *testing.T) {
	decider := talkingBackend()
	runtime := testRuntime(t, decider, nil)
	npcs := npcNames(10)
	scan(t, runtime, pedsAt(npcs, 15.0)...)
	scan(t, runtime, pedsAt(npcs, 8.0)...)
	for _, npcID := range npcs {
		gameEvent(t, runtime, npcID, "PLAYER_LOOKED_AT_NPC", nil)
	}

	spoken := 0
	for _, npcID := range npcs {
		for _, event := range allEvents(t, runtime, npcID) {
			if event.EventName == "NPC_SPOKE" {
				spoken++
			}
		}
	}
	if spoken != 1 {
		t.Fatalf("NPC_SPOKE events = %d, want 1", spoken)
	}
	if decider.calls != 3 {
		t.Fatalf("backend calls = %d, want 3", decider.calls)
	}
}

// TestOnlyOneConversationAgentAtATime ports
// test_only_one_conversation_agent_at_a_time.
func TestOnlyOneConversationAgentAtATime(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	for _, npcID := range []string{"npc_01", "npc_02"} {
		scan(t, runtime, safePed(npcID, 15.0))
		scan(t, runtime, safePed(npcID, 8.0))
	}
	handle(t, runtime, map[string]any{"type": "push_to_talk", "action": "start", "npc_id": "npc_01"})
	handle(t, runtime, map[string]any{"type": "push_to_talk", "action": "start", "npc_id": "npc_02"})

	if got := runtime.Ownership().State("npc_01"); got != state.StateAIActive {
		t.Fatalf("npc_01 state = %s, want AI_ACTIVE", got)
	}
	if got := runtime.Ownership().State("npc_02"); got != state.StateAIConversation {
		t.Fatalf("npc_02 state = %s, want AI_CONVERSATION", got)
	}
	conversations := []string{}
	for _, npcID := range runtime.Ownership().OwnedNPCs() {
		if runtime.Ownership().State(npcID) == state.StateAIConversation {
			conversations = append(conversations, npcID)
		}
	}
	if len(conversations) != 1 || conversations[0] != "npc_02" {
		t.Fatalf("conversations = %v, want [npc_02]", conversations)
	}
	if !containsName(eventNames(t, runtime, "npc_01"), "CONVERSATION_ENDED") {
		t.Fatalf("scheduler CONVERSATION_ENDED missing: %v", eventNames(t, runtime, "npc_01"))
	}
}

// TestIdleThinkingFiresAfterInterval ports
// test_idle_thinking.test_idle_thinking_fires_after_interval.
func TestIdleThinkingFiresAfterInterval(t *testing.T) {
	decider := quietBackend()
	runtime := testRuntime(t, decider, nil)
	activate(t, runtime, "npc_001")
	if decider.calls != 1 {
		t.Fatalf("backend calls = %d, want 1", decider.calls)
	}
	liveState(runtime, "npc_001").LastDecisionAt = runtime.now() - 100
	scan(t, runtime, safePed("npc_001", 8.0))
	if decider.calls != 2 {
		t.Fatalf("backend calls = %d, want 2 after idle thinking", decider.calls)
	}
}

// TestIdleThinkingSkipsWhenDisabled ports
// test_idle_thinking_skips_when_disabled.
func TestIdleThinkingSkipsWhenDisabled(t *testing.T) {
	decider := quietBackend()
	runtime := testRuntime(t, decider, func(settings *config.Settings) {
		settings.Scheduler.IdleThinkingSeconds = 0
	})
	activate(t, runtime, "npc_001")
	liveState(runtime, "npc_001").LastDecisionAt = runtime.now() - 100
	scan(t, runtime, safePed("npc_001", 8.0))
	if decider.calls != 1 {
		t.Fatalf("backend calls = %d, want 1", decider.calls)
	}
}

// TestIdleThinkingSkipsWhileActionPending ports
// test_idle_thinking_skips_while_action_pending.
func TestIdleThinkingSkipsWhileActionPending(t *testing.T) {
	decider := quietBackend()
	runtime := testRuntime(t, decider, nil)
	activate(t, runtime, "npc_001")
	result := runtime.Registry().Call("go_to", runtime.toolContext("npc_001"), map[string]any{"destination": "Valentine Saloon"})
	if result.Status != domain.ActionStarted {
		t.Fatalf("go_to status = %s, want started", result.Status)
	}
	liveState(runtime, "npc_001").LastDecisionAt = runtime.now() - 100
	scan(t, runtime, safePed("npc_001", 8.0))
	if decider.calls != 1 {
		t.Fatalf("backend calls = %d, want 1 while an action is pending", decider.calls)
	}
}

// TestIdleThinkingSkipsWhileSuspended ports
// test_idle_thinking_skips_while_suspended.
func TestIdleThinkingSkipsWhileSuspended(t *testing.T) {
	decider := quietBackend()
	runtime := testRuntime(t, decider, nil)
	activate(t, runtime, "npc_001")
	handle(t, runtime, map[string]any{"type": "story_safety", "active": true, "reason": "mission"})
	liveState(runtime, "npc_001").LastDecisionAt = runtime.now() - 100
	scan(t, runtime, safePed("npc_001", 8.0))
	if decider.calls != 1 {
		t.Fatalf("backend calls = %d, want 1 while suspended", decider.calls)
	}
}

// TestIdleThinkingSkipsWhileReasoning covers the "already reasoning" guard.
func TestIdleThinkingSkipsWhileReasoning(t *testing.T) {
	decider := quietBackend()
	runtime := testRuntime(t, decider, nil)
	activate(t, runtime, "npc_001")
	state := liveState(runtime, "npc_001")
	state.Reasoning = true
	state.LastDecisionAt = runtime.now() - 100
	scan(t, runtime, safePed("npc_001", 8.0))
	if decider.calls != 1 {
		t.Fatalf("backend calls = %d, want 1 while reasoning", decider.calls)
	}
}

// TestIdleThinkingSkipsDialogueOnly covers the quest overlay guard.
func TestIdleThinkingSkipsDialogueOnly(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	writeQuest(t, runtime, "quest_valentine_livestock")
	scan(t, runtime, questPed("npc_quest_giver", 15.0))
	scan(t, runtime, questPed("npc_quest_giver", 8.0))
	liveState(runtime, "npc_quest_giver").LastDecisionAt = runtime.now() - 100
	scan(t, runtime, questPed("npc_quest_giver", 8.0))
	if got := runtime.Ownership().State("npc_quest_giver"); got != state.StateQuestDialogue {
		t.Fatalf("state = %s, want QUEST_DIALOGUE", got)
	}
}

// TestTickRunsIdleThinkingAndPromotion covers the scheduler tick.
func TestTickRunsIdleThinkingAndPromotion(t *testing.T) {
	decider := quietBackend()
	runtime := testRuntime(t, decider, nil)
	activate(t, runtime, "npc_001")
	liveState(runtime, "npc_001").LastDecisionAt = runtime.now() - 100
	runtime.Tick(nil)
	if decider.calls != 2 {
		t.Fatalf("backend calls = %d, want 2 after Tick idle thinking", decider.calls)
	}

	scan(t, runtime, safePed("npc_002", 15.0))
	scan(t, runtime, safePed("npc_002", 8.0))
	runtime.setDeferred("npc_002", "test_reason")
	runtime.Tick(nil)
	if got := runtime.Ownership().State("npc_002"); got != state.StateAIActive {
		t.Fatalf("state = %s, want AI_ACTIVE after deferred promotion", got)
	}
	if decider.calls != 3 {
		t.Fatalf("backend calls = %d, want 3", decider.calls)
	}
}

// TestThinkingPolicyModes ports the reasoning-mode half of
// test_thinking_policy: mode selection from the trigger event and reason.
func TestThinkingPolicyModes(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	cases := []struct {
		name   string
		reason string
		event  map[string]any
		want   string
	}{
		{"short_event", "meaningful_event:PLAYER_APPROACHED", map[string]any{"seq": 1, "event_name": "PLAYER_APPROACHED"}, "disabled"},
		{"threat_event", "meaningful_event:PLAYER_THREATENED_NPC", map[string]any{"seq": 1, "event_name": "PLAYER_THREATENED_NPC"}, "low"},
		{"player_speech", "player_spoke", map[string]any{"seq": 1, "event_name": "PLAYER_SPOKE"}, "low"},
		{"ped_scan_activation", "ped_scan_activation", nil, "disabled"},
		{"idle_prefix", "idle_autonomous", nil, "disabled"},
		{"deferred_prefix", "deferred_promotion:meaningful_event:GUNSHOT_HEARD", nil, "disabled"},
		{"pass_by_prefix", "pass_by", nil, "disabled"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			context := runtime.BuildContext("npc_001", testCase.reason, testCase.event)
			if got := context.Flags["thinking_mode"]; got != testCase.want {
				t.Fatalf("thinking_mode = %v, want %s", got, testCase.want)
			}
		})
	}
}

// TestThinkingPolicyDefaultMode covers the configured default and the fallback
// when the configured mode is neither low nor disabled.
func TestThinkingPolicyDefaultMode(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), func(settings *config.Settings) {
		settings.ThinkingPolicy.DefaultMode = "disabled"
	})
	context := runtime.BuildContext("npc_001", "meaningful_event:PLAYER_APPROACHED", nil)
	if got := context.Flags["thinking_mode"]; got != "disabled" {
		t.Fatalf("thinking_mode = %v, want disabled", got)
	}

	runtime = testRuntime(t, quietBackend(), func(settings *config.Settings) {
		settings.ThinkingPolicy.DefaultMode = "turbo"
	})
	context = runtime.BuildContext("npc_001", "meaningful_event:PLAYER_APPROACHED", nil)
	if got := context.Flags["thinking_mode"]; got != "low" {
		t.Fatalf("thinking_mode = %v, want low fallback", got)
	}
}

// TestThinkingModeReachesBackend checks that the policy reaches the backend
// through AgentContext.Flags.
func TestThinkingModeReachesBackend(t *testing.T) {
	seen := []string{}
	decider := &fakeBackend{onDecide: func(_ int, agentContext *domain.AgentContext) *domain.AgentDecision {
		seen = append(seen, domain.StringFrom(agentContext.Flags["thinking_mode"]))
		return domain.NewAgentDecision()
	}}
	runtime := testRuntime(t, decider, nil)
	activate(t, runtime, "npc_001")
	handle(t, runtime, map[string]any{"type": "player_speech", "npc_id": "npc_001", "text": "Hello."})

	if len(seen) != 2 {
		t.Fatalf("backend saw %d calls, want 2", len(seen))
	}
	if seen[0] != "disabled" {
		t.Fatalf("activation thinking_mode = %s, want disabled", seen[0])
	}
	if seen[1] != "low" {
		t.Fatalf("player_speech thinking_mode = %s, want low", seen[1])
	}
}

// TestNearbyTrackedLimitCapsTheScan covers the scheduler cap on how many peds
// one scan may track.
func TestNearbyTrackedLimitCapsTheScan(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), func(settings *config.Settings) {
		settings.Scheduler.NearbyTrackedLimit = 2
	})
	peds := []map[string]any{
		safePed("npc_far", 18.0),
		safePed("npc_near", 4.0),
		safePed("npc_mid", 9.0),
	}
	reply := scan(t, runtime, peds...)
	if domain.IntFrom(reply["processed"]) != 2 {
		t.Fatalf("processed = %v, want 2", reply["processed"])
	}
	if runtime.Ownership().State("npc_far") != state.StateRockstar {
		t.Fatal("the farthest ped must be dropped by nearby_tracked_limit")
	}
	if runtime.Ownership().State("npc_near") != state.StateCandidate {
		t.Fatalf("npc_near state = %s, want CANDIDATE", runtime.Ownership().State("npc_near"))
	}
	if runtime.Ownership().State("npc_mid") != state.StateCandidate {
		t.Fatalf("npc_mid state = %s, want CANDIDATE", runtime.Ownership().State("npc_mid"))
	}

	// A zero limit tracks nothing.
	runtime = testRuntime(t, quietBackend(), func(settings *config.Settings) {
		settings.Scheduler.NearbyTrackedLimit = 0
	})
	reply = scan(t, runtime, safePed("npc_near", 4.0))
	if domain.IntFrom(reply["processed"]) != 0 {
		t.Fatalf("processed = %v, want 0", reply["processed"])
	}
}

// TestDeferredQueueIsFIFO covers the OrderedDict semantics of the deferred
// queue: re-deferring keeps the original position.
func TestDeferredQueueIsFIFO(t *testing.T) {
	decider := quietBackend()
	runtime := testRuntime(t, decider, nil)
	npcs := npcNames(5)
	for _, npcID := range npcs {
		scan(t, runtime, safePed(npcID, 15.0))
		scan(t, runtime, safePed(npcID, 8.0))
	}
	for _, npcID := range npcs {
		gameEvent(t, runtime, npcID, "PLAYER_LOOKED_AT_NPC", nil)
	}
	// npc_04 and npc_05 are deferred, in that order.
	runtime.setDeferred(npcs[3], "retriggered")
	if got := runtime.deferredOrder; len(got) != 2 || got[0] != npcs[3] || got[1] != npcs[4] {
		t.Fatalf("deferred order = %v, want [%s %s]", got, npcs[3], npcs[4])
	}

	// Free one slot: only the oldest deferred NPC is promoted.
	firstActive := activeNPCs(runtime)[0]
	if _, err := runtime.recordTransition(context.Background(), runtime.Ownership().RequestRelease(firstActive, "test"), nil, nil); err != nil {
		t.Fatalf("record: %v", err)
	}
	if got := runtime.Ownership().State(npcs[3]); got != state.StateAIActive {
		t.Fatalf("%s state = %s, want AI_ACTIVE", npcs[3], got)
	}
	if got := runtime.Ownership().State(npcs[4]); got != state.StateAware {
		t.Fatalf("%s state = %s, want AWARE", npcs[4], got)
	}
	if got := runtime.deferredOrder; len(got) != 1 || got[0] != npcs[4] {
		t.Fatalf("deferred order = %v, want [%s]", got, npcs[4])
	}
}

// TestConversationTargetAllowsQuestDialogueDespiteEligibility covers the
// QUEST_DIALOGUE eligibility bypass in select_conversation_target.
func TestConversationTargetAllowsQuestDialogueDespiteEligibility(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	writeQuest(t, runtime, "quest_valentine_livestock")
	enterQuestDialogue(t, runtime, "npc_quest_giver")
	if got := runtime.selectConversationTarget(); got != "npc_quest_giver" {
		t.Fatalf("target = %q, want npc_quest_giver", got)
	}
}
