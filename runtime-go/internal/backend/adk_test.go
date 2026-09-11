package backend

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
)

func TestParseDecisionPlainObject(t *testing.T) {
	decision, err := ParseDecision(`{
		"internal": {"goal": "size up the stranger", "mood": "wary"},
		"speech": {"text": "Evenin'.", "target": "player", "emotion": "neutral"},
		"actions": [{"tool": "face", "arguments": {}}]
	}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := decision.GoalText(); got != "size up the stranger" {
		t.Errorf("goal = %q", got)
	}
	if got := decision.MoodText(); got != "wary" {
		t.Errorf("mood = %q", got)
	}
	if got := decision.SpeechText(); got != "Evenin'." {
		t.Errorf("speech = %q", got)
	}
	if len(decision.Actions) != 1 || decision.Actions[0].Tool != "face" {
		t.Fatalf("actions = %+v", decision.Actions)
	}
	// face defaults its entity to the player, exactly like the Python parser.
	if got := decision.Actions[0].Arguments["entity"]; got != "player" {
		t.Errorf("face entity default = %v", got)
	}
}

func TestParseDecisionStripsMarkdownFence(t *testing.T) {
	decision, err := ParseDecision("```json\n{\"internal\":{\"mood\":\"calm\"},\"actions\":[]}\n```")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := decision.MoodText(); got != "calm" {
		t.Errorf("mood = %q", got)
	}
	if len(decision.Actions) != 0 {
		t.Errorf("actions = %+v", decision.Actions)
	}
	if decision.Speech != nil {
		t.Errorf("speech should be nil for silence, got %+v", decision.Speech)
	}
}

func TestParseDecisionFindsObjectInsideProse(t *testing.T) {
	text := `Sure, here is my decision: {"internal":{"goal":"keep walking"},"actions":[{"tool":"wander","arguments":{"radius":6}}]} hope that helps`
	decision, err := ParseDecision(text)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := decision.GoalText(); got != "keep walking" {
		t.Errorf("goal = %q", got)
	}
	if len(decision.Actions) != 1 || decision.Actions[0].Tool != "wander" {
		t.Fatalf("actions = %+v", decision.Actions)
	}
	if got := decision.Actions[0].Arguments["radius"]; got != float64(6) {
		t.Errorf("radius = %v", got)
	}
}

func TestParseDecisionUnwrapsDoubleEncodedPayload(t *testing.T) {
	inner := `{"internal":{"goal":"hide the ledger"},"actions":[]}`
	encoded, err := json.Marshal(inner)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	decision, err := ParseDecision(string(encoded))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := decision.GoalText(); got != "hide the ledger" {
		t.Errorf("goal = %q", got)
	}
}

func TestParseDecisionAcceptsStringSpeech(t *testing.T) {
	decision, err := ParseDecision(`{"speech":"Whoa there.","actions":[]}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Speech == nil {
		t.Fatal("speech should be set")
	}
	if decision.Speech.Text != "Whoa there." || decision.Speech.Target != "player" {
		t.Errorf("speech = %+v", decision.Speech)
	}
}

func TestParseDecisionAppliesEveryEntityDefault(t *testing.T) {
	decision, err := ParseDecision(`{"actions":[
		{"tool":"look_at","arguments":{}},
		{"tool":"face","arguments":{}},
		{"tool":"follow","arguments":{}},
		{"tool":"flee_from","arguments":{}}
	]}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, action := range decision.Actions {
		if got := action.Arguments["entity"]; got != "player" {
			t.Errorf("%s entity = %v, want player", action.Tool, got)
		}
	}
}

func TestParseDecisionKeepsExplicitEntityAndDropsToollessEntries(t *testing.T) {
	decision, err := ParseDecision(`{"actions":[
		{"tool":"follow","arguments":{"entity":"ped_7","distance":3}},
		{"arguments":{"entity":"player"}},
		"not an object"
	]}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(decision.Actions) != 1 {
		t.Fatalf("actions = %+v", decision.Actions)
	}
	if got := decision.Actions[0].Arguments["entity"]; got != "ped_7" {
		t.Errorf("entity = %v, want the explicit ped_7", got)
	}
}

func TestParseDecisionRejectsNonObjectsAndEmptyText(t *testing.T) {
	for _, text := range []string{"", "   ", "[1,2,3]", "42", "no json at all"} {
		if _, err := ParseDecision(text); err == nil {
			t.Errorf("ParseDecision(%.20q) should fail", text)
		}
	}
}

func TestParseDecisionRetriesOnEmptyContent(t *testing.T) {
	_, err := ParseDecision("")
	if err == nil {
		t.Fatal("expected an error")
	}
	var noContent ErrNoDecisionContent
	if !errors.As(err, &noContent) {
		t.Fatalf("empty content must be retryable, got %T", err)
	}
	if !isRetryable(err) {
		t.Error("empty content should be marked retryable")
	}
}

func TestParseDecisionToMapMatchesPythonShape(t *testing.T) {
	decision, err := ParseDecision(`{"internal":{"goal":"g","mood":"m"},"speech":{"text":"hi"},"actions":[{"tool":"stop","arguments":{}}]}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	payload := decision.ToMap()
	internal, ok := payload["internal"].(map[string]any)
	if !ok {
		t.Fatalf("internal missing: %+v", payload)
	}
	if internal["goal"] != "g" || internal["mood"] != "m" {
		t.Errorf("internal = %+v", internal)
	}
	speech, ok := payload["speech"].(map[string]any)
	if !ok || speech["text"] != "hi" {
		t.Errorf("speech = %+v", payload["speech"])
	}
	actions, ok := payload["actions"].([]any)
	if !ok || len(actions) != 1 {
		t.Errorf("actions = %+v", payload["actions"])
	}
}

func TestResolveAPIKeyPrefersExplicitValue(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "from-env")
	if got := ResolveAPIKey("explicit", "OPENCODE_API_KEY"); got != "explicit" {
		t.Errorf("got %q, want the explicit key", got)
	}
}

func TestResolveAPIKeyUsesConfiguredEnvVar(t *testing.T) {
	t.Setenv("RDR2AI_TEST_KEY", "custom-env")
	if got := ResolveAPIKey("", "RDR2AI_TEST_KEY"); got != "custom-env" {
		t.Errorf("got %q", got)
	}
}

func TestResolveAPIKeyFallsBackToOpenCodeEnvironment(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "opencode-env")
	if got := ResolveAPIKey("", "RDR2AI_MISSING_KEY"); got != "opencode-env" {
		t.Errorf("got %q", got)
	}
}

// TestLiveADKBackend exercises the real endpoint. It is opt-in so the default
// suite stays offline:
//
//	RDR2AI_LIVE=1 go test ./internal/backend/ -run TestLiveADKBackend -v
func TestLiveADKBackend(t *testing.T) {
	if os.Getenv("RDR2AI_LIVE") != "1" {
		t.Skip("set RDR2AI_LIVE=1 to call the real model")
	}
	baseURL := firstNonEmpty(os.Getenv("RDR2AI_API_BASE"), "https://opencode.ai/zen/go/v1")
	modelName := firstNonEmpty(os.Getenv("RDR2AI_MODEL"), "deepseek-v4.1-flash")

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	backend, err := NewADKBackend(ctx, ADKOptions{
		Model:     modelName,
		APIBase:   baseURL,
		APIKeyEnv: "OPENCODE_API_KEY",
		MaxTokens: 2048,
		PromptBuilder: func(*domain.AgentContext) string {
			return "You are a cowboy in Red Dead Redemption 2. Reply with ONLY a JSON object of the form " +
				`{"internal":{"goal":"...","mood":"..."},"speech":{"text":"...","target":"player"},"actions":[]}. ` +
				"Say one short greeting to a stranger who just walked up."
		},
	})
	if err != nil {
		t.Fatalf("NewADKBackend: %v", err)
	}
	if backend.Name() != "adk" || !backend.SupportsWaitGestures() {
		t.Errorf("unexpected backend identity: %s %v", backend.Name(), backend.SupportsWaitGestures())
	}

	decision, err := backend.Decide(ctx, domain.NewAgentContext("npc_001"))
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if backend.Calls() != 1 {
		t.Errorf("calls = %d", backend.Calls())
	}
	if decision.SpeechText() == "" {
		t.Logf("model returned no speech, decision=%+v", decision.ToMap())
	} else {
		t.Logf("model said: %q", decision.SpeechText())
	}
}
