package agent

import (
	"bytes"
	"testing"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/state"
)

// questBackend mirrors the Python _QuestBackend: it always asks for a movement
// action that the dialogue-only overlay must block.
func questBackend() *fakeBackend {
	return &fakeBackend{
		supports: true,
		onDecide: func(int, *domain.AgentContext) *domain.AgentDecision {
			decision := domain.NewAgentDecision()
			goal := "help with the auction"
			mood := "busy"
			decision.Goal = &goal
			decision.Mood = &mood
			decision.Speech = &domain.AgentSpeech{
				Text:   "Auction's this evenin'. Keep them cattle movin'.",
				Target: "player",
			}
			decision.Actions = []domain.AgentAction{action("go_to", map[string]any{"destination": "Valentine Saloon"})}
			return decision
		},
	}
}

func questRuntime(t *testing.T, decider *fakeBackend) *Runtime {
	t.Helper()
	runtime := testRuntime(t, decider, nil)
	writeQuest(t, runtime, "quest_valentine_livestock")
	return runtime
}

func enterQuestDialogue(t *testing.T, runtime *Runtime, npcID string) {
	t.Helper()
	scan(t, runtime, questPed(npcID, 15.0))
	scan(t, runtime, questPed(npcID, 8.0))
}

// TestQuestNpcEntersDialogueOnlyWithoutLLMCall ports
// test_quest_npc_enters_dialogue_only_without_llm_call.
func TestQuestNpcEntersDialogueOnlyWithoutLLMCall(t *testing.T) {
	decider := questBackend()
	runtime := questRuntime(t, decider)
	enterQuestDialogue(t, runtime, "npc_quest_giver")

	if got := runtime.Ownership().State("npc_quest_giver"); got != state.StateQuestDialogue {
		t.Fatalf("state = %s, want QUEST_DIALOGUE", got)
	}
	agentState := runtime.State("npc_quest_giver")
	if !agentState.DialogueOnly {
		t.Fatal("dialogue_only should be true")
	}
	if agentState.QuestContext["quest_id"] != "quest_valentine_livestock" {
		t.Fatalf("quest_context = %v", agentState.QuestContext)
	}
	if decider.calls != 0 {
		t.Fatalf("backend calls = %d, want 0", decider.calls)
	}
	if !containsName(eventNames(t, runtime, "npc_quest_giver"), "QUEST_DIALOGUE_ENTERED") {
		t.Fatalf("QUEST_DIALOGUE_ENTERED missing: %v", eventNames(t, runtime, "npc_quest_giver"))
	}
}

// TestQuestDialogueSpeaksButBlocksMovement ports
// test_quest_dialogue_speaks_but_blocks_movement.
func TestQuestDialogueSpeaksButBlocksMovement(t *testing.T) {
	decider := questBackend()
	runtime := questRuntime(t, decider)
	enterQuestDialogue(t, runtime, "npc_quest_giver")
	gameEvent(t, runtime, "npc_quest_giver", "PLAYER_APPROACHED", map[string]any{"distance": 8.0})

	names := eventNames(t, runtime, "npc_quest_giver")
	if !containsName(names, "NPC_SPOKE") {
		t.Fatalf("NPC_SPOKE missing: %v", names)
	}
	started := startedTools(t, runtime, "npc_quest_giver")
	if !containsName(started, "say") {
		t.Fatalf("say was not dispatched: %v", started)
	}
	if containsName(started, "go_to") {
		t.Fatalf("go_to must be blocked: %v", started)
	}
	blocked := []map[string]any{}
	for _, failure := range failedActions(t, runtime, "npc_quest_giver") {
		if failure["reason"] == "dialogue_only_mode" {
			blocked = append(blocked, failure)
		}
	}
	if len(blocked) != 1 {
		t.Fatalf("dialogue_only_mode failures = %d, want 1", len(blocked))
	}
	// The overlay never plays a wait gesture: Rockstar owns the body.
	if len(thinkStyles(t, runtime, "npc_quest_giver")) != 0 {
		t.Fatal("dialogue-only mode must not dispatch think gestures")
	}
}

// TestQuestDialogueHidesActionToolsFromPrompt ports
// test_quest_dialogue_hides_action_tools_from_prompt.
func TestQuestDialogueHidesActionToolsFromPrompt(t *testing.T) {
	decider := questBackend()
	runtime := questRuntime(t, decider)
	enterQuestDialogue(t, runtime, "npc_quest_giver")

	agentContext := runtime.BuildContext("npc_quest_giver", "quest_dialogue", nil)
	if !domain.BoolFrom(agentContext.Flags["dialogue_only"]) {
		t.Fatalf("dialogue_only flag missing: %v", agentContext.Flags)
	}
	for _, tool := range agentContext.AvailableTools {
		if tool == "go_to" {
			t.Fatalf("go_to must be hidden: %v", agentContext.AvailableTools)
		}
	}
	if !containsName(agentContext.AvailableTools, "say") {
		t.Fatalf("say must stay available: %v", agentContext.AvailableTools)
	}
	if len(agentContext.QuestContext) == 0 {
		t.Fatal("quest context should be populated in dialogue-only mode")
	}
	prompt := BuildAgentPrompt(agentContext)
	for _, section := range []string{"CURRENT QUEST CONTEXT", "DIALOGUE-ONLY MODE"} {
		if !bytes.Contains([]byte(prompt), []byte(section)) {
			t.Fatalf("prompt missing %s:\n%s", section, prompt)
		}
	}
	if !bytes.Contains([]byte(prompt), []byte("AVAILABLE TOOLS\nget_world_lore, get_world_state, grab_timeline, say")) {
		t.Fatalf("prompt should advertise only the dialogue-only tools:\n%s", prompt)
	}
}

// TestPushToTalkWorksAndMovementStaysBlocked ports
// test_push_to_talk_works_and_movement_stays_blocked.
func TestPushToTalkWorksAndMovementStaysBlocked(t *testing.T) {
	decider := questBackend()
	runtime := questRuntime(t, decider)
	enterQuestDialogue(t, runtime, "npc_quest_giver")

	handle(t, runtime, map[string]any{"type": "push_to_talk", "action": "start", "npc_id": "npc_quest_giver"})
	if got := runtime.Ownership().State("npc_quest_giver"); got != state.StateAIConversation {
		t.Fatalf("state = %s, want AI_CONVERSATION", got)
	}
	handle(t, runtime, map[string]any{
		"type": "player_speech", "npc_id": "npc_quest_giver", "text": "Where are the pens?",
	})

	names := eventNames(t, runtime, "npc_quest_giver")
	if !containsName(names, "PLAYER_SPOKE") || !containsName(names, "NPC_SPOKE") {
		t.Fatalf("speech events missing: %v", names)
	}
	for _, tool := range startedTools(t, runtime, "npc_quest_giver") {
		switch tool {
		case "go_to", "wander", "follow":
			t.Fatalf("movement tool %s must stay blocked", tool)
		}
	}
}

// TestLeavingAreaExitsQuestDialogue ports
// test_leaving_area_exits_quest_dialogue.
func TestLeavingAreaExitsQuestDialogue(t *testing.T) {
	decider := questBackend()
	runtime := questRuntime(t, decider)
	enterQuestDialogue(t, runtime, "npc_quest_giver")

	scan(t, runtime, questPed("npc_quest_giver", 50.0))

	if got := runtime.Ownership().State("npc_quest_giver"); got != state.StateRockstar {
		t.Fatalf("state = %s, want ROCKSTAR", got)
	}
	if runtime.State("npc_quest_giver").DialogueOnly {
		t.Fatal("dialogue_only should be cleared on release")
	}
	names := eventNames(t, runtime, "npc_quest_giver")
	if !containsName(names, "QUEST_DIALOGUE_ENTERED") || !containsName(names, "NPC_RELEASED") {
		t.Fatalf("expected QUEST_DIALOGUE_ENTERED and NPC_RELEASED: %v", names)
	}
}

// TestQuestDialogueEndsWhenFlagDisappears covers the quest_dialogue_ended
// release path.
func TestQuestDialogueEndsWhenFlagDisappears(t *testing.T) {
	decider := questBackend()
	runtime := questRuntime(t, decider)
	enterQuestDialogue(t, runtime, "npc_quest_giver")

	plain := safePed("npc_quest_giver", 8.0)
	scan(t, runtime, plain)

	if got := runtime.Ownership().State("npc_quest_giver"); got != state.StateRockstar {
		t.Fatalf("state = %s, want ROCKSTAR", got)
	}
	if runtime.State("npc_quest_giver").DialogueOnly {
		t.Fatal("dialogue_only should be cleared")
	}
	released := map[string]any(nil)
	for _, event := range allEvents(t, runtime, "npc_quest_giver") {
		if event.EventName == "NPC_RELEASED" {
			released = event.Data
		}
	}
	if released == nil || released["reason"] != "quest_dialogue_ended" {
		t.Fatalf("release data = %v, want quest_dialogue_ended", released)
	}
}

// TestQuestDialogueConservativeChecks covers the base safety checks that the
// quest flag must not relax.
func TestQuestDialogueConservativeChecks(t *testing.T) {
	decider := questBackend()
	runtime := questRuntime(t, decider)

	dead := questPed("npc_dead", 8.0)
	dead["is_alive"] = false
	inCutscene := questPed("npc_cutscene", 8.0)
	inCutscene["in_cutscene"] = true
	unknownCutscene := questPed("npc_unknown", 8.0)
	delete(unknownCutscene, "in_cutscene")
	player := questPed("npc_player", 8.0)
	player["is_player"] = true
	scan(t, runtime, dead, inCutscene, unknownCutscene, player)

	for _, npcID := range []string{"npc_dead", "npc_cutscene", "npc_unknown", "npc_player"} {
		if runtime.State(npcID).DialogueOnly {
			t.Fatalf("%s must not enter dialogue-only mode", npcID)
		}
		if got := runtime.Ownership().State(npcID); got == state.StateQuestDialogue {
			t.Fatalf("%s state = %s", npcID, got)
		}
	}
	if decider.calls != 0 {
		t.Fatalf("backend calls = %d, want 0", decider.calls)
	}
}

// TestQuestProfileFallbackQuestID covers the profile-based quest_id lookup and
// the dialogue_only metadata alias.
func TestQuestProfileFallbackQuestID(t *testing.T) {
	decider := questBackend()
	runtime := questRuntime(t, decider)
	writeFile(t, runtime.Settings().ProfilesDir+"/npc_profile_quest.json",
		`{"npc_id":"npc_profile_quest","name":"Quest Farmer","quest_id":"quest_valentine_livestock"}`)

	ped := safePed("npc_profile_quest", 8.0)
	ped["metadata"] = map[string]any{"dialogue_only": true}
	scan(t, runtime, ped)

	if got := runtime.Ownership().State("npc_profile_quest"); got != state.StateQuestDialogue {
		t.Fatalf("state = %s, want QUEST_DIALOGUE", got)
	}
	if runtime.State("npc_profile_quest").QuestContext["quest_id"] != "quest_valentine_livestock" {
		t.Fatalf("quest context = %v", runtime.State("npc_profile_quest").QuestContext)
	}
}
