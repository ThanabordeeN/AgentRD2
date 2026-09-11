package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/backend"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/config"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/timeline"
)

// fakeBackend is the tiny backend the ported tests use. It counts Decide calls
// and can report wait-gesture support, matching the Python test doubles.
type fakeBackend struct {
	name     string
	calls    int
	supports bool
	onDecide func(call int, agentContext *domain.AgentContext) *domain.AgentDecision
}

func (b *fakeBackend) Name() string {
	if b.name != "" {
		return b.name
	}
	return "fake"
}

func (b *fakeBackend) SupportsWaitGestures() bool { return b.supports }

func (b *fakeBackend) Decide(_ context.Context, agentContext *domain.AgentContext) (*domain.AgentDecision, error) {
	b.calls++
	if b.onDecide != nil {
		return b.onDecide(b.calls, agentContext), nil
	}
	return domain.NewAgentDecision(), nil
}

// quietBackend mirrors the Python _CountingBackend.
func quietBackend() *fakeBackend { return &fakeBackend{} }

// talkingBackend mirrors the Python _TalkingBackend.
func talkingBackend() *fakeBackend {
	return &fakeBackend{onDecide: func(int, *domain.AgentContext) *domain.AgentDecision {
		decision := domain.NewAgentDecision()
		speech := &domain.AgentSpeech{Text: "Hello there."}
		decision.Speech = speech
		return decision
	}}
}

// testRuntime builds a runtime over a temporary project layout so tests never
// touch the repository's data directory.
func testRuntime(t *testing.T, decider backend.Backend, mutate func(*config.Settings)) *Runtime {
	t.Helper()
	dir := t.TempDir()
	settings := config.DefaultSettings()
	settings.TimelinesDir = filepath.Join(dir, "timelines")
	settings.ProfilesDir = filepath.Join(dir, "profiles")
	settings.QuestsDir = filepath.Join(dir, "quests")
	settings.WikiContextPath = filepath.Join(dir, "wiki", "rdr2_context.json")
	settings.CharacterContextPath = filepath.Join(dir, "wiki", "characters.json")
	settings.StoryBlacklistPath = filepath.Join(dir, "story_blacklist.json")
	if mutate != nil {
		mutate(&settings)
	}
	writeFile(t, settings.StoryBlacklistPath, `{"categories": {}}`)
	store, err := timeline.NewStore(settings.TimelinesDir)
	if err != nil {
		t.Fatalf("open timeline store: %v", err)
	}
	runtime, err := NewRuntime(Options{Settings: &settings, Timeline: store, Backend: decider})
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}
	return runtime
}

// writeProfile installs a minimal NPC profile so courage-sensitive behaviour
// can be exercised without depending on the repository's data directory.
func writeProfile(t *testing.T, runtime *Runtime, npcID string, courage float64) {
	t.Helper()
	path := filepath.Join(runtime.Settings().ProfilesDir, npcID+".json")
	writeFile(t, path, `{"npc_id":"`+npcID+`","name":"`+npcID+`","personality":{"courage":`+formatFloat(courage)+`}}`)
}

// writeQuest installs a minimal quest pack.
func writeQuest(t *testing.T, runtime *Runtime, questID string) {
	t.Helper()
	path := filepath.Join(runtime.Settings().QuestsDir, questID+".json")
	writeFile(t, path, `{"quest_id":"`+questID+`","title":"The Livestock Auction",`+
		`"description":"Drive the cattle to the auction pens before evening.",`+
		`"npc_role":"Quest giver","dialogue_guidelines":["Speak briefly and in character."],`+
		`"forbidden_spoilers":["The pens are ambushed."]}`)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func formatFloat(value float64) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

// handle drives one bridge message through the runtime.
func handle(t *testing.T, runtime *Runtime, raw map[string]any) map[string]any {
	t.Helper()
	reply, err := runtime.HandleMessage(context.Background(), raw)
	if err != nil {
		t.Fatalf("HandleMessage(%v): %v", raw["type"], err)
	}
	return reply
}

// safePed mirrors the Python tests' safe_ped fixture.
func safePed(npcID string, distance float64) map[string]any {
	return map[string]any{
		"entity_id":          npcID,
		"model":              "a_m_m_farmer_01",
		"name":               "Elias Carter",
		"is_ped":             true,
		"is_human":           true,
		"is_alive":           true,
		"is_player":          false,
		"is_story_character": false,
		"is_mission_owned":   false,
		"in_scripted_state":  false,
		"in_cutscene":        false,
		"blacklisted":        false,
		"distance_m":         distance,
		"visible":            true,
		"health":             100,
		"metadata":           map[string]any{"camera_alignment": 0.9},
	}
}

// questPed mirrors the Python tests' _quest_ped fixture.
func questPed(npcID string, distance float64) map[string]any {
	return map[string]any{
		"entity_id":          npcID,
		"model":              "a_m_m_farmer_01",
		"name":               "Quest Farmer",
		"is_ped":             true,
		"is_human":           true,
		"is_alive":           true,
		"is_player":          false,
		"is_story_character": true,
		"is_mission_owned":   true,
		"in_scripted_state":  false,
		"in_cutscene":        false,
		"blacklisted":        false,
		"distance_m":         distance,
		"visible":            true,
		"health":             100,
		"metadata": map[string]any{
			"quest_dialogue": true,
			"quest_id":       "quest_valentine_livestock",
			"quest_state": map[string]any{
				"current_objective": "Drive the cattle to the auction pens before evening.",
			},
		},
	}
}

func scan(t *testing.T, runtime *Runtime, peds ...map[string]any) map[string]any {
	t.Helper()
	payload := make([]any, 0, len(peds))
	for _, ped := range peds {
		payload = append(payload, ped)
	}
	return handle(t, runtime, map[string]any{"type": "ped_scan", "peds": payload})
}

func gameEvent(t *testing.T, runtime *Runtime, npcID, eventName string, data map[string]any) map[string]any {
	t.Helper()
	if data == nil {
		data = map[string]any{}
	}
	return handle(t, runtime, map[string]any{
		"type":       "game_event",
		"event_name": eventName,
		"npc_id":     npcID,
		"entities":   []any{"player"},
		"tags":       []any{"player"},
		"data":       data,
	})
}

func allEvents(t *testing.T, runtime *Runtime, npcID string) []domain.Event {
	t.Helper()
	events, err := runtime.Timeline().AllEvents(npcID)
	if err != nil {
		t.Fatalf("all events: %v", err)
	}
	return events
}

func eventNames(t *testing.T, runtime *Runtime, npcID string) []string {
	t.Helper()
	events := allEvents(t, runtime, npcID)
	names := make([]string, 0, len(events))
	for _, event := range events {
		names = append(names, event.EventName)
	}
	return names
}

func containsName(names []string, wanted string) bool {
	for _, name := range names {
		if name == wanted {
			return true
		}
	}
	return false
}

// startedTools lists the tools recorded as ACTION_STARTED.
func startedTools(t *testing.T, runtime *Runtime, npcID string) []string {
	t.Helper()
	toolsSeen := []string{}
	for _, event := range allEvents(t, runtime, npcID) {
		if event.EventName != "ACTION_STARTED" {
			continue
		}
		toolsSeen = append(toolsSeen, domain.StringFrom(event.Data["tool"]))
	}
	return toolsSeen
}

// thinkStyles lists the styles of dispatched think gestures.
func thinkStyles(t *testing.T, runtime *Runtime, npcID string) []string {
	t.Helper()
	styles := []string{}
	for _, event := range allEvents(t, runtime, npcID) {
		if event.EventName != "ACTION_STARTED" || domain.StringFrom(event.Data["tool"]) != "think" {
			continue
		}
		arguments := domain.MapFrom(event.Data["arguments"])
		styles = append(styles, domain.StringFrom(arguments["style"]))
	}
	return styles
}

// failedActions lists ACTION_FAILED data entries.
func failedActions(t *testing.T, runtime *Runtime, npcID string) []map[string]any {
	t.Helper()
	failures := []map[string]any{}
	for _, event := range allEvents(t, runtime, npcID) {
		if event.EventName == "ACTION_FAILED" {
			failures = append(failures, event.Data)
		}
	}
	return failures
}

// npcName matches the Python tests' “npc_%02d“ ids.
func npcName(index int) string { return fmt.Sprintf("npc_%02d", index) }

func containsSubstring(haystack, needle string) bool { return strings.Contains(haystack, needle) }

// appendGoal records a goal lifecycle event.
func appendGoal(t *testing.T, runtime *Runtime, npcID, eventName, goal string) {
	t.Helper()
	if _, err := runtime.Timeline().AppendEvent(npcID, eventName, timeline.AppendOptions{
		Data: map[string]any{"goal": goal},
		Tags: []string{"goal"},
	}); err != nil {
		t.Fatalf("append %s: %v", eventName, err)
	}
}

// liveState returns the runtime's own state object so tests can move the
// decision clock exactly like the Python tests did.
func liveState(runtime *Runtime, npcID string) *AgentRuntimeState {
	return runtime.agentStateLocked(npcID)
}

// activate mirrors the Python tests' _activate helper.
func activate(t *testing.T, runtime *Runtime, npcID string) {
	t.Helper()
	scan(t, runtime, safePed(npcID, 15.0))
	scan(t, runtime, safePed(npcID, 8.0))
	gameEvent(t, runtime, npcID, "PLAYER_LOOKED_AT_NPC", nil)
}
