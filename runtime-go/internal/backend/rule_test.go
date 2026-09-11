package backend

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// ruleTestEvent builds one timeline event the way the runtime stores it.
func ruleTestEvent(name string, data map[string]any) map[string]any {
	if data == nil {
		data = map[string]any{}
	}
	return map[string]any{"event_name": name, "npc_id": "npc_001", "data": data}
}

// ruleTestContext builds the smallest context the rule backend reads.
func ruleTestContext(personality map[string]any) *domain.AgentContext {
	ctx := domain.NewAgentContext("npc_001")
	ctx.Profile = map[string]any{}
	if personality != nil {
		ctx.Profile["personality"] = personality
	}
	return ctx
}

// ruleTestWant describes the decision a test expects. A nil goal means the
// decision must not carry one.
type ruleTestWant struct {
	goal    *string
	mood    string
	silent  bool
	speech  string
	target  string
	emotion string
	tools   []string
	args    []map[string]any
}

// ruleTestGoal returns a pointer to text for use in ruleTestWant.
func ruleTestGoal(text string) *string { return &text }

// ruleTestAssertDecision checks a decision against a ruleTestWant.
func ruleTestAssertDecision(t *testing.T, got *domain.AgentDecision, want ruleTestWant) {
	t.Helper()
	if got == nil {
		t.Fatal("Decide returned a nil decision")
	}
	if got.MoodText() != want.mood {
		t.Errorf("mood = %q, want %q", got.MoodText(), want.mood)
	}
	switch {
	case want.goal == nil && got.Goal != nil:
		t.Errorf("goal = %q, want no goal", got.GoalText())
	case want.goal != nil && got.GoalText() != *want.goal:
		t.Errorf("goal = %q, want %q", got.GoalText(), *want.goal)
	}
	switch {
	case want.silent && got.Speech != nil:
		t.Errorf("speech = %q, want silence", got.Speech.Text)
	case !want.silent && got.Speech == nil:
		t.Errorf("speech = nil, want %q", want.speech)
	case !want.silent:
		if got.Speech.Text != want.speech {
			t.Errorf("speech text = %q, want %q", got.Speech.Text, want.speech)
		}
		if got.Speech.Target != want.target {
			t.Errorf("speech target = %q, want %q", got.Speech.Target, want.target)
		}
		if got.Speech.Emotion != want.emotion {
			t.Errorf("speech emotion = %q, want %q", got.Speech.Emotion, want.emotion)
		}
	}
	gotTools := strings.Join(got.ToolNames(), ",")
	wantTools := strings.Join(want.tools, ",")
	if gotTools != wantTools {
		t.Errorf("tools = %q, want %q", gotTools, wantTools)
	}
	if len(want.args) > 0 {
		if len(got.Actions) != len(want.args) {
			t.Fatalf("actions = %d, want %d", len(got.Actions), len(want.args))
		}
		for index, wantArgs := range want.args {
			if !reflect.DeepEqual(got.Actions[index].Arguments, wantArgs) {
				t.Errorf("action %d arguments = %#v, want %#v", index, got.Actions[index].Arguments, wantArgs)
			}
		}
	}
}

// ruleTestDecide runs one Decide call and fails the test on error.
func ruleTestDecide(t *testing.T, backend *RuleBackend, ctx *domain.AgentContext) *domain.AgentDecision {
	t.Helper()
	decision, err := backend.Decide(context.Background(), ctx)
	if err != nil {
		t.Fatalf("Decide returned an error: %v", err)
	}
	return decision
}

// ---------------------------------------------------------------------------
// backend contract
// ---------------------------------------------------------------------------

func TestRuleBackendContract(t *testing.T) {
	var backend Backend = NewRuleBackend(RuleOptions{})
	if got := backend.Name(); got != "rule" {
		t.Errorf("Name() = %q, want %q", got, "rule")
	}
	if backend.SupportsWaitGestures() {
		t.Error("SupportsWaitGestures() = true, want false: the rule backend is instant")
	}
}

func TestRuleBackendOptionsAreClamped(t *testing.T) {
	tests := []struct {
		name         string
		value        float64
		want         float64
		checkSilence bool
		wantSilent   bool
	}{
		{name: "below range clamps to zero", value: -3, want: 0, checkSilence: true, wantSilent: false},
		{name: "in range is kept", value: 0.25, want: 0.25},
		{name: "zero is kept", value: 0, want: 0, checkSilence: true, wantSilent: false},
		{name: "one is kept", value: 1, want: 1, checkSilence: true, wantSilent: true},
		{name: "above range clamps to one", value: 4.5, want: 1, checkSilence: true, wantSilent: true},
		{name: "NaN clamps to one like Python", value: math.NaN(), want: 1, checkSilence: true, wantSilent: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := NewRuleBackend(RuleOptions{SilenceProbability: test.value, Seed: 1})
			if backend.silenceProbability != test.want {
				t.Fatalf("silenceProbability = %v, want %v", backend.silenceProbability, test.want)
			}
			if !test.checkSilence {
				return
			}
			ctx := ruleTestContext(map[string]any{"sociability": 0.9})
			ctx.RecentEvents = []map[string]any{ruleTestEvent("PLAYER_APPROACHED", nil)}
			decision := ruleTestDecide(t, backend, ctx)
			if silent := decision.Speech == nil; silent != test.wantSilent {
				t.Errorf("silent = %v, want %v", silent, test.wantSilent)
			}
		})
	}
}

func TestRuleBackendRejectsNilAndCancelledContexts(t *testing.T) {
	backend := NewRuleBackend(RuleOptions{Seed: 7})
	if _, err := backend.Decide(context.Background(), nil); err == nil {
		t.Error("Decide(nil agent context) = nil error, want error")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := backend.Decide(cancelled, domain.NewAgentContext("npc_001")); err == nil {
		t.Error("Decide(cancelled ctx) = nil error, want error")
	}
}

// ---------------------------------------------------------------------------
// threat / friendly branching
// ---------------------------------------------------------------------------

func TestRuleBackendThreatReaction(t *testing.T) {
	tests := []struct {
		name        string
		personality map[string]any
		recent      []map[string]any
		flags       map[string]any
		want        ruleTestWant
	}{
		{
			name:        "low courage flees",
			personality: map[string]any{"courage": 0.2},
			recent:      []map[string]any{ruleTestEvent("PLAYER_THREATENED_NPC", nil)},
			want: ruleTestWant{
				goal:    ruleTestGoal("get away from the player"),
				mood:    "afraid",
				speech:  "Stay back!",
				target:  "player",
				emotion: "afraid",
				tools:   []string{"flee_from"},
				args:    []map[string]any{{"entity": "player"}},
			},
		},
		{
			name:        "courage just below threshold still flees",
			personality: map[string]any{"courage": 0.479},
			recent:      []map[string]any{ruleTestEvent("PLAYER_ATTACKED_NPC", nil)},
			want: ruleTestWant{
				goal:    ruleTestGoal("get away from the player"),
				mood:    "afraid",
				speech:  "Stay back!",
				target:  "player",
				emotion: "afraid",
				tools:   []string{"flee_from"},
				args:    []map[string]any{{"entity": "player"}},
			},
		},
		{
			name:        "default courage stands ground warily",
			personality: nil,
			recent:      []map[string]any{ruleTestEvent("NPC_DAMAGED", map[string]any{"amount": 12.0})},
			want: ruleTestWant{
				goal:    ruleTestGoal("stand ground"),
				mood:    "wary",
				speech:  "You had better move along.",
				target:  "player",
				emotion: "annoyed",
				tools:   []string{"look_at"},
				args:    []map[string]any{{"entity": "player"}},
			},
		},
		{
			name:        "courage exactly at threshold stands ground",
			personality: map[string]any{"courage": 0.48},
			recent:      []map[string]any{ruleTestEvent("PLAYER_THREATENED_NPC", nil)},
			want: ruleTestWant{
				goal:    ruleTestGoal("stand ground"),
				mood:    "wary",
				speech:  "You had better move along.",
				target:  "player",
				emotion: "annoyed",
				tools:   []string{"look_at"},
			},
		},
		{
			name:        "aggressive brave NPC gets angry",
			personality: map[string]any{"courage": 0.9, "aggression": 0.4},
			recent:      []map[string]any{ruleTestEvent("PLAYER_THREATENED_NPC", nil)},
			want: ruleTestWant{
				goal:    ruleTestGoal("stand ground"),
				mood:    "angry",
				speech:  "You had better move along.",
				target:  "player",
				emotion: "annoyed",
				tools:   []string{"look_at"},
			},
		},
		{
			name:        "threat older than the newest event still wins",
			personality: map[string]any{"courage": 0.1},
			recent: []map[string]any{
				ruleTestEvent("PLAYER_THREATENED_NPC", nil),
				ruleTestEvent("PLAYER_SPOKE", map[string]any{"text": "Hello there."}),
			},
			want: ruleTestWant{
				goal:    ruleTestGoal("get away from the player"),
				mood:    "afraid",
				speech:  "Stay back!",
				target:  "player",
				emotion: "afraid",
				tools:   []string{"flee_from"},
			},
		},
		{
			// Python scans only ``context.recent_events`` for threats, so a
			// trigger flag that the timeline has not recorded yet falls
			// through to the fallback decision.
			name:        "trigger flag alone is not part of the threat scan",
			personality: map[string]any{"courage": 0.1},
			flags:       map[string]any{"trigger_event": ruleTestEvent("NPC_DAMAGED", nil)},
			want:        ruleTestWant{mood: "neutral", silent: true},
		},
		{
			name:        "trigger flag plus recent threat still flees",
			personality: map[string]any{"courage": 0.1},
			recent:      []map[string]any{ruleTestEvent("NPC_DAMAGED", nil)},
			flags:       map[string]any{"trigger_event": ruleTestEvent("NPC_DAMAGED", nil)},
			want: ruleTestWant{
				goal:    ruleTestGoal("get away from the player"),
				mood:    "afraid",
				speech:  "Stay back!",
				target:  "player",
				emotion: "afraid",
				tools:   []string{"flee_from"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := NewRuleBackend(RuleOptions{SilenceProbability: 0, Seed: 7})
			ctx := ruleTestContext(test.personality)
			ctx.RecentEvents = test.recent
			for key, value := range test.flags {
				ctx.Flags[key] = value
			}
			ruleTestAssertDecision(t, ruleTestDecide(t, backend, ctx), test.want)
		})
	}
}

func TestRuleBackendFriendlyReaction(t *testing.T) {
	backend := NewRuleBackend(RuleOptions{SilenceProbability: 1.0, Seed: 7})
	ctx := ruleTestContext(map[string]any{"courage": 0.1})
	goal := "watch the road"
	ctx.CurrentGoal = &goal
	ctx.RecentEvents = []map[string]any{ruleTestEvent("PLAYER_HELPED_NPC", nil)}

	decision := ruleTestDecide(t, backend, ctx)
	ruleTestAssertDecision(t, decision, ruleTestWant{
		goal:    ruleTestGoal("watch the road"),
		mood:    "warm",
		speech:  "Good to see a friendly face.",
		target:  "player",
		emotion: "friendly",
		tools:   []string{"look_at"},
		args:    []map[string]any{{"entity": "player"}},
	})
	if decision.Speech == nil {
		t.Fatal("friendly events must bypass the silence probability")
	}
}

func TestRuleBackendThreatBeatsFriendly(t *testing.T) {
	backend := NewRuleBackend(RuleOptions{Seed: 7})
	ctx := ruleTestContext(map[string]any{"courage": 0.1})
	ctx.RecentEvents = []map[string]any{
		ruleTestEvent("PLAYER_HELPED_NPC", nil),
		ruleTestEvent("PLAYER_THREATENED_NPC", nil),
	}
	ruleTestAssertDecision(t, ruleTestDecide(t, backend, ctx), ruleTestWant{
		goal:    ruleTestGoal("get away from the player"),
		mood:    "afraid",
		speech:  "Stay back!",
		target:  "player",
		emotion: "afraid",
		tools:   []string{"flee_from"},
	})
}

// ---------------------------------------------------------------------------
// silence policy
// ---------------------------------------------------------------------------

func TestRuleBackendSilenceProbability(t *testing.T) {
	tests := []struct {
		name        string
		probability float64
		event       string
		personality map[string]any
		want        ruleTestWant
		repeat      int
	}{
		{
			name:        "direct speech is answered at zero silence",
			probability: 0,
			event:       "PLAYER_SPOKE",
			personality: map[string]any{"sociability": 0.9},
			want: ruleTestWant{
				mood:    "neutral",
				speech:  "Evening.",
				target:  "player",
				emotion: "friendly",
				tools:   []string{"look_at"},
			},
		},
		{
			name:        "direct speech stays silent at full silence",
			probability: 1,
			event:       "PLAYER_SPOKE",
			personality: map[string]any{"sociability": 0.9},
			want:        ruleTestWant{mood: "neutral", silent: true},
		},
		{
			name:        "direct speech survives the high-silence cutoff",
			probability: 0.9,
			event:       "PLAYER_SPOKE",
			personality: map[string]any{"sociability": 0.9},
			want: ruleTestWant{
				mood:    "neutral",
				speech:  "Evening.",
				target:  "player",
				emotion: "friendly",
				tools:   []string{"look_at"},
			},
		},
		{
			name:        "pass-by greeting at zero silence",
			probability: 0,
			event:       "PLAYER_APPROACHED",
			personality: map[string]any{"sociability": 0.9},
			want: ruleTestWant{
				mood:    "neutral",
				speech:  "Evening.",
				target:  "player",
				emotion: "neutral",
				tools:   []string{"look_at"},
			},
		},
		{
			name:        "pass-by greeting never fires at full silence",
			probability: 1,
			event:       "PLAYER_APPROACHED",
			personality: map[string]any{"sociability": 0.9},
			want:        ruleTestWant{mood: "neutral", silent: true},
			repeat:      25,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := NewRuleBackend(RuleOptions{SilenceProbability: test.probability, Seed: 7})
			ctx := ruleTestContext(test.personality)
			ctx.RecentEvents = []map[string]any{
				ruleTestEvent(test.event, map[string]any{"text": "Hello there."}),
			}
			repeats := test.repeat
			if repeats == 0 {
				repeats = 1
			}
			for index := 0; index < repeats; index++ {
				ruleTestAssertDecision(t, ruleTestDecide(t, backend, ctx), test.want)
			}
		})
	}
}

func TestRuleBackendZeroSilenceDoesNotAdvanceTheStream(t *testing.T) {
	quiet := NewRuleBackend(RuleOptions{SilenceProbability: 0, Seed: 11})
	random := NewRuleBackend(RuleOptions{SilenceProbability: 0.5, Seed: 11})
	for index := 0; index < 10; index++ {
		if !quiet.shouldSpeak(false) {
			t.Fatal("a zero silence probability must always allow the pass-by greeting")
		}
	}
	if quiet.roll() != random.roll() {
		t.Error("silence probability 0.0 consumed a random draw; Python short-circuits before the key")
	}
}

func TestRuleBackendDirectSpeechIgnoresTheRandomStream(t *testing.T) {
	backend := NewRuleBackend(RuleOptions{SilenceProbability: 0.05, Seed: 13})
	reference := NewRuleBackend(RuleOptions{SilenceProbability: 0.05, Seed: 13})
	for index := 0; index < 20; index++ {
		if !backend.shouldSpeak(true) {
			t.Fatal("silence probability below 0.9 must answer direct speech")
		}
	}
	if backend.roll() != reference.roll() {
		t.Error("the direct-speech path consumed a random draw")
	}
}

// ---------------------------------------------------------------------------
// small talk, gunshots and failures
// ---------------------------------------------------------------------------

func TestRuleBackendSmallTalk(t *testing.T) {
	tests := []struct {
		name        string
		personality map[string]any
		event       string
		want        ruleTestWant
	}{
		{
			name:        "unsociable NPC stays quiet when approached",
			personality: map[string]any{"sociability": 0.61},
			event:       "PLAYER_APPROACHED",
			want:        ruleTestWant{mood: "neutral", silent: true},
		},
		{
			name:        "default profile is below the greeting threshold",
			personality: nil,
			event:       "PLAYER_APPROACHED",
			want:        ruleTestWant{mood: "neutral", silent: true},
		},
		{
			name:        "sociable NPC says howdy",
			personality: map[string]any{"sociability": 0.62},
			event:       "PLAYER_APPROACHED",
			want: ruleTestWant{
				mood:    "neutral",
				speech:  "Howdy.",
				target:  "player",
				emotion: "neutral",
				tools:   []string{"look_at"},
			},
		},
		{
			name:        "very sociable NPC says evening",
			personality: map[string]any{"sociability": 0.75},
			event:       "PLAYER_LOOKED_AT_NPC",
			want: ruleTestWant{
				mood:    "neutral",
				speech:  "Evening.",
				target:  "player",
				emotion: "neutral",
				tools:   []string{"look_at"},
			},
		},
		{
			name:        "existing goal and mood survive small talk",
			personality: map[string]any{"sociability": 0.8},
			event:       "PLAYER_APPROACHED",
			want: ruleTestWant{
				goal:    ruleTestGoal("finish the fence"),
				mood:    "content",
				speech:  "Evening.",
				target:  "player",
				emotion: "neutral",
				tools:   []string{"look_at"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := NewRuleBackend(RuleOptions{SilenceProbability: 0, Seed: 7})
			ctx := ruleTestContext(test.personality)
			if test.want.goal != nil {
				goal := *test.want.goal
				ctx.CurrentGoal = &goal
			}
			if test.want.mood != "neutral" {
				ctx.CurrentMood = test.want.mood
			}
			ctx.RecentEvents = []map[string]any{ruleTestEvent(test.event, nil)}
			ruleTestAssertDecision(t, ruleTestDecide(t, backend, ctx), test.want)
		})
	}
}

func TestRuleBackendPlayerSpeechReplies(t *testing.T) {
	tests := []struct {
		name string
		data map[string]any
		want ruleTestWant
	}{
		{
			name: "directions question",
			data: map[string]any{"text": "Where are you headed?"},
			want: ruleTestWant{
				mood: "neutral", speech: "Valentine, if I keep moving.",
				target: "player", emotion: "neutral", tools: []string{"look_at"},
			},
		},
		{
			name: "greeting",
			data: map[string]any{"text": "Hello there."},
			want: ruleTestWant{
				mood: "neutral", speech: "Evening.",
				target: "player", emotion: "neutral", tools: []string{"look_at"},
			},
		},
		{
			name: "thanks",
			data: map[string]any{"text": "Thank you, mister."},
			want: ruleTestWant{
				mood: "neutral", speech: "Don't mention it.",
				target: "player", emotion: "neutral", tools: []string{"look_at"},
			},
		},
		{
			name: "request for help",
			data: map[string]any{"text": "Can you help me?"},
			want: ruleTestWant{
				mood: "neutral", speech: "I have my own troubles.",
				target: "player", emotion: "neutral", tools: []string{"look_at"},
			},
		},
		{
			name: "identity question",
			data: map[string]any{"text": "Who are you?"},
			want: ruleTestWant{
				mood: "neutral", speech: "Nobody important.",
				target: "player", emotion: "neutral", tools: []string{"look_at"},
			},
		},
		{
			name: "small talk falls through",
			data: map[string]any{"text": "Nice weather we are having."},
			want: ruleTestWant{
				mood: "neutral", speech: "Hmph.",
				target: "player", emotion: "neutral", tools: []string{"look_at"},
			},
		},
		{
			name: "greeting wins over directions ordering",
			data: map[string]any{"text": "Hey, where are you going?"},
			want: ruleTestWant{
				mood: "neutral", speech: "Valentine, if I keep moving.",
				target: "player", emotion: "neutral", tools: []string{"look_at"},
			},
		},
		{
			name: "empty transcript gets no reply",
			data: map[string]any{"text": ""},
			want: ruleTestWant{mood: "neutral", silent: true},
		},
		{
			name: "whitespace transcript gets no reply",
			data: map[string]any{"text": "   "},
			want: ruleTestWant{mood: "neutral", silent: true},
		},
		{
			name: "missing text key gets no reply",
			data: map[string]any{},
			want: ruleTestWant{mood: "neutral", silent: true},
		},
		{
			name: "numeric transcript stringifies and earns a reply",
			data: map[string]any{"text": 42},
			want: ruleTestWant{mood: "neutral", speech: "Hmph.", target: "player", emotion: "neutral", tools: []string{"look_at"}},
		},
		{
			name: "null transcript mirrors Python str(None)",
			data: map[string]any{"text": nil},
			want: ruleTestWant{mood: "neutral", speech: "Hmph.", target: "player", emotion: "neutral", tools: []string{"look_at"}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := NewRuleBackend(RuleOptions{SilenceProbability: 0, Seed: 7})
			ctx := ruleTestContext(map[string]any{"sociability": 0.65})
			ctx.RecentEvents = []map[string]any{ruleTestEvent("PLAYER_SPOKE", test.data)}
			ruleTestAssertDecision(t, ruleTestDecide(t, backend, ctx), test.want)
		})
	}
}

func TestRuleBackendGunshot(t *testing.T) {
	position := []any{1.0, 2.0, 3.0}
	tests := []struct {
		name        string
		personality map[string]any
		data        map[string]any
		want        ruleTestWant
	}{
		{
			name:        "brave NPC investigates a located gunshot",
			personality: map[string]any{"courage": 0.55},
			data:        map[string]any{"position": position},
			want: ruleTestWant{
				goal:    ruleTestGoal("investigate the gunshot"),
				mood:    "alert",
				speech:  "What the hell was that?",
				emotion: "alert",
				tools:   []string{"investigate"},
				args:    []map[string]any{{"position": position}},
			},
		},
		{
			name:        "brave NPC without a position does nothing",
			personality: map[string]any{"courage": 0.9},
			data:        map[string]any{},
			want:        ruleTestWant{mood: "neutral", silent: true},
		},
		{
			name:        "empty position list is not investigated",
			personality: map[string]any{"courage": 0.9},
			data:        map[string]any{"position": []any{}},
			want:        ruleTestWant{mood: "neutral", silent: true},
		},
		{
			name:        "cowardly NPC leaves the area",
			personality: map[string]any{"courage": 0.39},
			data:        map[string]any{"position": position},
			want: ruleTestWant{
				goal:   ruleTestGoal("leave the area"),
				mood:   "afraid",
				silent: true,
				tools:  []string{"wander"},
				args:   []map[string]any{{"radius": 12.0}},
			},
		},
		{
			name:        "middling courage neither investigates nor flees",
			personality: map[string]any{"courage": 0.5},
			data:        map[string]any{"position": position},
			want:        ruleTestWant{mood: "neutral", silent: true},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := NewRuleBackend(RuleOptions{SilenceProbability: 0, Seed: 7})
			ctx := ruleTestContext(test.personality)
			ctx.RecentEvents = []map[string]any{ruleTestEvent("GUNSHOT_HEARD", test.data)}
			ruleTestAssertDecision(t, ruleTestDecide(t, backend, ctx), test.want)
		})
	}
}

func TestRuleBackendActionFailed(t *testing.T) {
	tests := []struct {
		name string
		data map[string]any
		want ruleTestWant
	}{
		{
			name: "failed go_to falls back to a small wander",
			data: map[string]any{"tool": "go_to", "reason": "path_unreachable"},
			want: ruleTestWant{
				mood:   "frustrated",
				silent: true,
				tools:  []string{"wander"},
				args:   []map[string]any{{"radius": 8.0}},
			},
		},
		{
			name: "other failed tools only change the mood",
			data: map[string]any{"tool": "look_at"},
			want: ruleTestWant{mood: "frustrated", silent: true},
		},
		{
			name: "missing tool data only changes the mood",
			data: map[string]any{},
			want: ruleTestWant{mood: "frustrated", silent: true},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := NewRuleBackend(RuleOptions{SilenceProbability: 0, Seed: 7})
			ctx := ruleTestContext(nil)
			ctx.RecentEvents = []map[string]any{ruleTestEvent("ACTION_FAILED", test.data)}
			ruleTestAssertDecision(t, ruleTestDecide(t, backend, ctx), test.want)
		})
	}
}

func TestRuleBackendFallbackKeepsContext(t *testing.T) {
	tests := []struct {
		name        string
		event       string
		currentMood string
		currentGoal *string
		want        ruleTestWant
	}{
		{
			name:        "unknown event keeps goal and mood",
			event:       "HORSE_SPOTTED",
			currentMood: "curious",
			currentGoal: ruleTestGoal("tend the horses"),
			want:        ruleTestWant{goal: ruleTestGoal("tend the horses"), mood: "curious", silent: true},
		},
		{
			name:  "empty context falls back to neutral",
			event: "CONVERSATION_ENDED",
			want:  ruleTestWant{mood: "neutral", silent: true},
		},
		{
			name:        "empty mood string becomes neutral",
			event:       "CONVERSATION_ENDED",
			currentMood: "",
			want:        ruleTestWant{mood: "neutral", silent: true},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := NewRuleBackend(RuleOptions{SilenceProbability: 0, Seed: 7})
			ctx := ruleTestContext(nil)
			ctx.CurrentMood = test.currentMood
			ctx.CurrentGoal = test.currentGoal
			ctx.RecentEvents = []map[string]any{ruleTestEvent(test.event, nil)}
			ruleTestAssertDecision(t, ruleTestDecide(t, backend, ctx), test.want)
		})
	}
}

func TestRuleBackendTriggerFlagPrecedence(t *testing.T) {
	tests := []struct {
		name  string
		flags map[string]any
		want  ruleTestWant
	}{
		{
			name:  "empty trigger flag falls back to the newest event",
			flags: map[string]any{"trigger_event": map[string]any{}},
			want: ruleTestWant{
				mood: "neutral", speech: "Valentine, if I keep moving.", target: "player",
				emotion: "friendly", tools: []string{"look_at"},
			},
		},
		{
			name:  "nil trigger flag falls back to the newest event",
			flags: map[string]any{"trigger_event": nil},
			want: ruleTestWant{
				mood: "neutral", speech: "Valentine, if I keep moving.", target: "player",
				emotion: "friendly", tools: []string{"look_at"},
			},
		},
		{
			name: "trigger flag wins over the newest event",
			flags: map[string]any{
				"trigger_event": ruleTestEvent("PLAYER_APPROACHED", nil),
			},
			want: ruleTestWant{
				mood: "neutral", speech: "Evening.", target: "player",
				emotion: "neutral", tools: []string{"look_at"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := NewRuleBackend(RuleOptions{SilenceProbability: 0, Seed: 7})
			ctx := ruleTestContext(map[string]any{"sociability": 0.9})
			ctx.RecentEvents = []map[string]any{
				ruleTestEvent("PLAYER_SPOKE", map[string]any{"text": "Where are you headed?"}),
			}
			ctx.Flags = test.flags
			ruleTestAssertDecision(t, ruleTestDecide(t, backend, ctx), test.want)
		})
	}
}

// ---------------------------------------------------------------------------
// determinism, concurrency and JSON shape
// ---------------------------------------------------------------------------

// ruleSpeakSequence runs count pass-by greetings and reports which ones spoke.
func ruleSpeakSequence(t *testing.T, backend *RuleBackend, count int) []bool {
	t.Helper()
	ctx := ruleTestContext(map[string]any{"sociability": 0.9})
	ctx.RecentEvents = []map[string]any{ruleTestEvent("PLAYER_APPROACHED", nil)}
	sequence := make([]bool, 0, count)
	for index := 0; index < count; index++ {
		decision := ruleTestDecide(t, backend, ctx)
		sequence = append(sequence, decision.Speech != nil)
	}
	return sequence
}

func TestRuleBackendDeterminism(t *testing.T) {
	const calls = 64
	first := ruleSpeakSequence(t, NewRuleBackend(RuleOptions{SilenceProbability: 0.5, Seed: 7}), calls)
	second := ruleSpeakSequence(t, NewRuleBackend(RuleOptions{SilenceProbability: 0.5, Seed: 7}), calls)
	if !reflect.DeepEqual(first, second) {
		t.Errorf("two backends with the same seed diverged:\n first = %v\nsecond = %v", first, second)
	}
	speaking := 0
	for _, spoke := range first {
		if spoke {
			speaking++
		}
	}
	if speaking == 0 || speaking == calls {
		t.Errorf("seed 7 produced a degenerate silence sequence (%d/%d spoke)", speaking, calls)
	}

	other := ruleSpeakSequence(t, NewRuleBackend(RuleOptions{SilenceProbability: 0.5, Seed: 8}), calls)
	if reflect.DeepEqual(first, other) {
		t.Error("different seeds produced the same silence sequence")
	}
}

func TestRuleBackendConcurrentDecide(t *testing.T) {
	const goroutines = 8
	const perGoroutine = 25
	backend := NewRuleBackend(RuleOptions{SilenceProbability: 0.5, Seed: 21})

	var group sync.WaitGroup
	for worker := 0; worker < goroutines; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			ctx := ruleTestContext(map[string]any{"sociability": 0.9})
			ctx.RecentEvents = []map[string]any{ruleTestEvent("PLAYER_APPROACHED", nil)}
			for call := 0; call < perGoroutine; call++ {
				decision, err := backend.Decide(context.Background(), ctx)
				if err != nil || decision == nil {
					t.Errorf("concurrent Decide failed: decision=%v err=%v", decision, err)
					return
				}
			}
		}()
	}
	group.Wait()

	// Exactly one draw happens per pass-by call, so the shared stream must sit
	// where a reference backend that drew the same number of times sits.
	reference := NewRuleBackend(RuleOptions{SilenceProbability: 0.5, Seed: 21})
	for draw := 0; draw < goroutines*perGoroutine; draw++ {
		reference.roll()
	}
	if backend.roll() != reference.roll() {
		t.Error("concurrent calls did not consume exactly one draw each")
	}
}

func TestRuleBackendDecisionJSONShape(t *testing.T) {
	tests := []struct {
		name string
		ctx  func() *domain.AgentContext
		want string
	}{
		{
			name: "silent fallback keeps mood and drops speech",
			ctx: func() *domain.AgentContext {
				ctx := ruleTestContext(nil)
				ctx.RecentEvents = []map[string]any{ruleTestEvent("CONVERSATION_ENDED", nil)}
				return ctx
			},
			want: `{"actions":[],"internal":{"mood":"neutral"}}`,
		},
		{
			name: "threat carries goal, mood, speech and action",
			ctx: func() *domain.AgentContext {
				ctx := ruleTestContext(map[string]any{"courage": 0.1})
				ctx.RecentEvents = []map[string]any{ruleTestEvent("PLAYER_THREATENED_NPC", nil)}
				return ctx
			},
			want: `{"actions":[{"arguments":{"entity":"player"},"tool":"flee_from"}],` +
				`"internal":{"goal":"get away from the player","mood":"afraid"},` +
				`"speech":{"emotion":"afraid","target":"player","text":"Stay back!"}}`,
		},
		{
			name: "gunshot speech has no target",
			ctx: func() *domain.AgentContext {
				ctx := ruleTestContext(map[string]any{"courage": 0.8})
				ctx.RecentEvents = []map[string]any{
					ruleTestEvent("GUNSHOT_HEARD", map[string]any{"position": []any{1.0, 2.0, 3.0}}),
				}
				return ctx
			},
			want: `{"actions":[{"arguments":{"position":[1,2,3]},"tool":"investigate"}],` +
				`"internal":{"goal":"investigate the gunshot","mood":"alert"},` +
				`"speech":{"emotion":"alert","text":"What the hell was that?"}}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := NewRuleBackend(RuleOptions{SilenceProbability: 0, Seed: 7})
			decision := ruleTestDecide(t, backend, test.ctx())
			raw, err := json.Marshal(decision.ToMap())
			if err != nil {
				t.Fatalf("marshal decision: %v", err)
			}
			if string(raw) != test.want {
				t.Errorf("decision JSON =\n %s\nwant\n %s", raw, test.want)
			}
		})
	}
}

func TestRuleBackendDecisionDoesNotAliasContextGoal(t *testing.T) {
	backend := NewRuleBackend(RuleOptions{SilenceProbability: 0, Seed: 7})
	goal := "original goal"
	ctx := ruleTestContext(map[string]any{"sociability": 0.9})
	ctx.CurrentGoal = &goal
	ctx.RecentEvents = []map[string]any{ruleTestEvent("PLAYER_APPROACHED", nil)}

	decision := ruleTestDecide(t, backend, ctx)
	goal = "mutated afterwards"
	if got := decision.GoalText(); got != "original goal" {
		t.Errorf("decision goal = %q, want %q: the decision must not alias the context", got, "original goal")
	}
}

func TestRuleReplyTo(t *testing.T) {
	tests := []struct {
		name       string
		transcript string
		wantText   string
		wantOK     bool
	}{
		{name: "empty", transcript: "", wantOK: false},
		{name: "whitespace", transcript: "\t \n", wantOK: false},
		{name: "where", transcript: "WHERE IS THE SALOON", wantText: "Valentine, if I keep moving.", wantOK: true},
		{name: "headed", transcript: "Headed somewhere?", wantText: "Valentine, if I keep moving.", wantOK: true},
		{name: "going", transcript: "Going to town?", wantText: "Valentine, if I keep moving.", wantOK: true},
		{name: "howdy", transcript: "Howdy partner", wantText: "Evening.", wantOK: true},
		{name: "morning", transcript: "Morning.", wantText: "Evening.", wantOK: true},
		{name: "thank", transcript: "Thanks", wantText: "Don't mention it.", wantOK: true},
		{name: "help", transcript: "Help!", wantText: "I have my own troubles.", wantOK: true},
		{name: "your name", transcript: "What is your name?", wantText: "Nobody important.", wantOK: true},
		{name: "who are you", transcript: "So who are you then", wantText: "Nobody important.", wantOK: true},
		{name: "fallthrough", transcript: "Nice horse", wantText: "Hmph.", wantOK: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			text, ok := ruleReplyTo(test.transcript)
			if ok != test.wantOK || text != test.wantText {
				t.Errorf("ruleReplyTo(%q) = (%q, %v), want (%q, %v)", test.transcript, text, ok, test.wantText, test.wantOK)
			}
		})
	}
}
