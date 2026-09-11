package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/backend"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/config"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/state"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/timeline"
)

// TestNewRuntimeDefaults checks every constructor default the Python
// “NpcAgentRuntime“ applies when a collaborator is omitted.
func TestNewRuntimeDefaults(t *testing.T) {
	dir := t.TempDir()
	settings := config.DefaultSettings()
	settings.TimelinesDir = filepath.Join(dir, "timelines")
	settings.ProfilesDir = filepath.Join(dir, "profiles")
	settings.QuestsDir = filepath.Join(dir, "quests")
	settings.StoryBlacklistPath = filepath.Join(dir, "story_blacklist.json")
	writeFile(t, settings.StoryBlacklistPath, `{"categories": {}}`)

	runtime, err := NewRuntime(Options{Settings: &settings})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if runtime.Backend() == nil || runtime.Backend().Name() != "rule" {
		t.Fatalf("default backend = %v, want the rule backend", runtime.Backend())
	}
	if runtime.Registry() == nil || len(runtime.Registry().Names()) == 0 {
		t.Fatal("default registry is empty")
	}
	if runtime.Dispatcher() == nil || runtime.World() == nil || runtime.Profiles() == nil {
		t.Fatal("default collaborators were not created")
	}
	if runtime.Ownership() == nil || runtime.Lore() == nil || runtime.Quests() == nil {
		t.Fatal("default collaborators were not created")
	}
	if runtime.Timeline() == nil {
		t.Fatal("default timeline store was not created")
	}
	// The rule backend's silence probability comes from the speech config.
	rule, ok := runtime.Backend().(*backend.RuleBackend)
	if !ok {
		t.Fatalf("backend type = %T", runtime.Backend())
	}
	if rule.Name() != "rule" || rule.SupportsWaitGestures() {
		t.Fatalf("unexpected rule backend: %s/%v", rule.Name(), rule.SupportsWaitGestures())
	}

	// A missing story blacklist is fatal, exactly like Python's
	// EligibilityGate.from_config: eligibility fails closed, so the runtime
	// refuses to start rather than taking over peds it cannot vet.
	missing := settings
	missing.StoryBlacklistPath = filepath.Join(dir, "nope.json")
	if _, err := NewRuntime(Options{Settings: &missing}); err == nil {
		t.Fatal("a missing blacklist must stop the runtime from starting (fails closed)")
	}

	// Malformed content is fatal too.
	broken := settings
	brokenPath := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(brokenPath, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write broken blacklist: %v", err)
	}
	broken.StoryBlacklistPath = brokenPath
	if _, err := NewRuntime(Options{Settings: &broken}); err == nil {
		t.Fatal("an unparseable blacklist must stop the runtime from starting")
	}
}

// flakyBackend fails its first decision and succeeds afterwards, so the
// runtime's error propagation and recovery can both be checked.
type flakyBackend struct{ calls int }

func (b *flakyBackend) Name() string               { return "flaky" }
func (b *flakyBackend) SupportsWaitGestures() bool { return false }
func (b *flakyBackend) Decide(context.Context, *domain.AgentContext) (*domain.AgentDecision, error) {
	b.calls++
	if b.calls == 1 {
		return nil, errors.New("model exploded")
	}
	return domain.NewAgentDecision(), nil
}

// TestBackendFailurePropagatesAndClearsReasoning mirrors Python letting the
// backend exception escape handle_message (the IPC layer closes the
// connection) while the “finally“ clears the reasoning flag.
func TestBackendFailurePropagatesAndClearsReasoning(t *testing.T) {
	decider := &flakyBackend{}
	runtime := testRuntime(t, decider, nil)
	scan(t, runtime, safePed("npc_001", 15.0))
	scan(t, runtime, safePed("npc_001", 8.0))

	if _, err := runtime.HandleMessage(context.Background(), map[string]any{
		"type": "game_event", "event_name": "PLAYER_LOOKED_AT_NPC", "npc_id": "npc_001", "data": map[string]any{},
	}); err == nil {
		t.Fatal("expected the backend error to propagate")
	}
	if liveState(runtime, "npc_001").Reasoning {
		t.Fatal("the reasoning flag must be cleared after a backend failure")
	}

	// The NPC is not stuck: the next turn reaches the backend again.
	reply := handle(t, runtime, map[string]any{
		"type": "player_speech", "npc_id": "npc_001", "text": "You alright?",
	})
	if reply["type"] != "event_ack" {
		t.Fatalf("reply = %v", reply)
	}
	if decider.calls != 2 {
		t.Fatalf("backend calls = %d, want 2", decider.calls)
	}
}

// TestActionResultWithoutNpcIDUsesPendingRequest covers “_npc_from_pending“.
func TestActionResultWithoutNpcIDUsesPendingRequest(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	activate(t, runtime, "npc_001")
	request := domain.NewActionRequest("go_to", map[string]any{"destination": "camp"}, "npc_001")
	runtime.Dispatcher().Dispatch(request)

	reply := handle(t, runtime, map[string]any{
		"type": "action_result", "request_id": request.RequestID, "status": "failed", "reason": "blocked",
	})
	if reply["type"] != "action_result_ack" || reply["npc_id"] != "npc_001" {
		t.Fatalf("reply = %v", reply)
	}
	if !containsName(eventNames(t, runtime, "npc_001"), "ACTION_FAILED") {
		t.Fatalf("ACTION_FAILED missing: %v", eventNames(t, runtime, "npc_001"))
	}
}

// TestRemoveDeferredOnRelease covers the deferred-queue cleanup on release.
func TestRemoveDeferredOnRelease(t *testing.T) {
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
	if len(runtime.deferredOrder) != 2 {
		t.Fatalf("deferred = %v, want two entries", runtime.deferredOrder)
	}
	// Walking away releases the oldest deferred NPC and drops it from the
	// queue without promoting it.
	scan(t, runtime, safePed(npcs[3], 55.0))
	if got := runtime.Ownership().State(npcs[3]); got != state.StateRockstar {
		t.Fatalf("state = %s, want ROCKSTAR", got)
	}
	if len(runtime.deferredOrder) != 1 || runtime.deferredOrder[0] != npcs[4] {
		t.Fatalf("deferred = %v, want [%s]", runtime.deferredOrder, npcs[4])
	}
}

// TestPythonCompatibleHelpers pins the small Python-compatibility helpers.
func TestPythonCompatibleHelpers(t *testing.T) {
	if got := pyReprString("plain"); got != "'plain'" {
		t.Fatalf("pyReprString = %s", got)
	}
	if got := pyReprString("it's"); got != `"it's"` {
		t.Fatalf("pyReprString = %s", got)
	}
	if got := pyReprString("both'\""); got != `'both\'"'` {
		t.Fatalf("pyReprString = %s", got)
	}
	if got := pyReprString("tab\there"); got != `'tab\there'` {
		t.Fatalf("pyReprString = %s", got)
	}
	if got := pyStr(nil); got != "None" {
		t.Fatalf("pyStr(nil) = %s", got)
	}
	if got := pyStr(true); got != "True" {
		t.Fatalf("pyStr(true) = %s", got)
	}
	if got := pyStr(2.0); got != "2.0" {
		t.Fatalf("pyStr(2.0) = %s", got)
	}

	if value, ok := coerceFloat(" 12.5 "); !ok || value != 12.5 {
		t.Fatalf("coerceFloat = %v/%v", value, ok)
	}
	if _, ok := coerceFloat("soon"); ok {
		t.Fatal("coerceFloat should reject non-numeric strings")
	}
	if _, ok := coerceFloat(nil); ok {
		t.Fatal("coerceFloat should reject nil")
	}
	if !isTruthy([]any{1}) || isTruthy([]any{}) || isTruthy("") || isTruthy(0) || isTruthy(nil) {
		t.Fatal("isTruthy does not mirror Python truthiness")
	}
	if !isTruthy(map[string]any{"a": 1}) || isTruthy(map[string]any{}) {
		t.Fatal("isTruthy does not mirror Python dict truthiness")
	}
}

// TestPositionCoercionCoversSliceShapes covers the []float64/[]int branches
// (Go-native callers, unlike JSON-decoded []any).
func TestPositionCoercionCoversSliceShapes(t *testing.T) {
	if position, ok := coercePosition([]float64{1, 2, 3}); !ok || position[2] != 3 {
		t.Fatalf("[]float64 position = %v/%v", position, ok)
	}
	if position, ok := coercePosition([]int{4, 5, 6}); !ok || position[0] != 4 {
		t.Fatalf("[]int position = %v/%v", position, ok)
	}
	if _, ok := coercePosition([]any{1, 2}); ok {
		t.Fatal("a two-element position must be rejected")
	}
	if !isPositionValue([]float64{1, 2, 3}) || isPositionValue([]int{1, 2}) {
		t.Fatal("isPositionValue does not mirror the Python list check")
	}
}

// TestPrettyJSONReflectFallbacks covers the reflection path used for Go-native
// slice and map types.
func TestPrettyJSONReflectFallbacks(t *testing.T) {
	value := map[string]any{
		"position": []float64{1.5, 2.0, -3.0},
		"ints":     []int{1, 2},
		"labels":   map[string]string{"b": "two", "a": "one"},
	}
	want := `{
  "ints": [
    1,
    2
  ],
  "labels": {
    "a": "one",
    "b": "two"
  },
  "position": [
    1.5,
    2.0,
    -3.0
  ]
}`
	if got := prettyJSON(value); got != want {
		t.Fatalf("prettyJSON mismatch\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
	if got := prettyJSON([]map[string]any(nil)); got != "[]" {
		t.Fatalf("nil slice = %s", got)
	}
	if got := prettyJSON(map[string]any(nil)); got != "{}" {
		t.Fatalf("nil map = %s", got)
	}
}

// TestTimelineStoreAccessors covers the helpers the wiring layer relies on.
func TestTimelineStoreAccessors(t *testing.T) {
	runtime := testRuntime(t, quietBackend(), nil)
	settings := runtime.Settings()
	if settings.TimelinesDir == "" {
		t.Fatal("settings were not retained")
	}
	if _, err := runtime.Timeline().AppendEvent("npc_001", "TEST_EVENT", timeline.AppendOptions{
		Data: map[string]any{"a": 1},
	}); err != nil {
		t.Fatalf("append: %v", err)
	}
	runtime.State("npc_001")
	if got := runtime.agentStateIDsLocked(); len(got) != 1 || got[0] != "npc_001" {
		t.Fatalf("agent state ids = %v", got)
	}
}
