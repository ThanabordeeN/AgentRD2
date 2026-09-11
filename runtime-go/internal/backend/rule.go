package backend

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"strings"
	"sync"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
)

// RuleOptions configures a RuleBackend.
type RuleOptions struct {
	// SilenceProbability is how likely the NPC is to stay quiet instead of
	// making small talk. It is clamped to [0, 1], matching Python's
	// ``max(0.0, min(1.0, silence_probability))``.
	SilenceProbability float64

	// Seed seeds the backend's pseudo-random stream so that two backends
	// built with the same seed answer the same sequence of Decide calls
	// identically.
	//
	// Python's RuleBasedAgentBackend keys its pass-by silence decision off
	// ``hash((seed, npc_id, len(recent_events)))``; Python salts string
	// hashes per process, so that key is not reproducible even between two
	// Python runs. The Go port replaces it with a seeded per-backend stream,
	// which makes the same behaviour reproducible here.
	Seed int64
}

// RuleBackend is the deterministic offline backend used by tests, the dry-run
// demo and the scenario runner. It is a faithful port of Python's
// “RuleBasedAgentBackend“: it never calls a model, so it answers
// instantaneously and SupportsWaitGestures reports false.
//
// A RuleBackend is safe for concurrent use. Its only mutable state is the
// pseudo-random stream used for the pass-by silence decision, which is
// guarded by a mutex.
type RuleBackend struct {
	silenceProbability float64

	mu  sync.Mutex
	rng *rand.Rand
}

var _ Backend = (*RuleBackend)(nil)

// NewRuleBackend builds a rule backend from opts.
func NewRuleBackend(opts RuleOptions) *RuleBackend {
	return &RuleBackend{
		silenceProbability: clampProbability(opts.SilenceProbability),
		rng:                rand.New(rand.NewSource(opts.Seed)),
	}
}

// Name implements Backend.
func (b *RuleBackend) Name() string { return "rule" }

// SupportsWaitGestures implements Backend. The rule backend is instant, so the
// runtime should never play a thinking/listening gesture while it decides.
func (b *RuleBackend) SupportsWaitGestures() bool { return false }

// Decide implements Backend. It reproduces the branch ladder of Python's
// “RuleBasedAgentBackend.decide“: threats first, then friendly events, then
// the single newest event (“flags["trigger_event"]“ when the runtime
// supplied one, otherwise the last entry of “recent_events“).
//
// Decide performs no I/O and returns as soon as it has looked at the context;
// ctx is only checked for an already-cancelled caller. A nil agentCtx is
// rejected with an error rather than a panic.
func (b *RuleBackend) Decide(ctx context.Context, agentCtx *domain.AgentContext) (*domain.AgentDecision, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	if agentCtx == nil {
		return nil, errors.New("backend: rule backend needs a non-nil agent context")
	}

	personality := domain.MapFrom(agentCtx.Profile["personality"])
	sociability := ruleTrait(personality, "sociability", 0.5)
	courage := ruleTrait(personality, "courage", 0.5)
	aggression := ruleTrait(personality, "aggression", 0.3)

	recent := agentCtx.RecentEvents
	trigger := domain.MapFrom(agentCtx.Flag("trigger_event"))
	latest := map[string]any{}
	switch {
	case len(trigger) > 0:
		latest = trigger
	case len(recent) > 0:
		latest = recent[len(recent)-1]
	}

	goal := ruleGoalCopy(agentCtx.CurrentGoal)
	mood := agentCtx.CurrentMood
	if mood == "" {
		mood = "neutral"
	}
	var speech *domain.AgentSpeech
	actions := make([]domain.AgentAction, 0, 1)

	if _, ok := ruleFindRecent(recent, ruleIsThreatEvent); ok {
		if courage < 0.48 {
			mood = "afraid"
			goal = ruleGoal("get away from the player")
			actions = append(actions, rulePlayerAction("flee_from"))
			speech = &domain.AgentSpeech{Text: "Stay back!", Target: "player", Emotion: "afraid"}
		} else {
			if aggression >= 0.4 {
				mood = "angry"
			} else {
				mood = "wary"
			}
			goal = ruleGoal("stand ground")
			actions = append(actions, rulePlayerAction("look_at"))
			speech = &domain.AgentSpeech{
				Text:    "You had better move along.",
				Target:  "player",
				Emotion: "annoyed",
			}
		}
		return ruleDecision(goal, mood, speech, actions), nil
	}

	if _, ok := ruleFindRecent(recent, ruleIsFriendlyEvent); ok {
		mood = "warm"
		speech = &domain.AgentSpeech{Text: "Good to see a friendly face.", Target: "player", Emotion: "friendly"}
		actions = append(actions, rulePlayerAction("look_at"))
		return ruleDecision(goal, mood, speech, actions), nil
	}

	switch ruleEventName(latest) {
	case "GUNSHOT_HEARD":
		position := domain.MapFrom(latest["data"])["position"]
		if courage >= 0.55 && ruleTruthy(position) {
			mood = "alert"
			goal = ruleGoal("investigate the gunshot")
			actions = append(actions, domain.AgentAction{
				Tool:      "investigate",
				Arguments: map[string]any{"position": position},
			})
			speech = &domain.AgentSpeech{Text: "What the hell was that?", Emotion: "alert"}
		} else if courage < 0.4 {
			mood = "afraid"
			goal = ruleGoal("leave the area")
			actions = append(actions, domain.AgentAction{
				Tool:      "wander",
				Arguments: map[string]any{"radius": 12.0},
			})
		}
		return ruleDecision(goal, mood, speech, actions), nil

	case "PLAYER_SPOKE":
		transcript := ruleDataText(domain.MapFrom(latest["data"]))
		if b.shouldSpeak(true) {
			if reply, ok := ruleReplyTo(transcript); ok {
				emotion := "neutral"
				if sociability >= 0.7 {
					emotion = "friendly"
				}
				speech = &domain.AgentSpeech{Text: reply, Target: "player", Emotion: emotion}
				actions = append(actions, rulePlayerAction("look_at"))
			}
		}
		return ruleDecision(goal, mood, speech, actions), nil

	case "PLAYER_APPROACHED", "PLAYER_LOOKED_AT_NPC":
		if sociability >= 0.62 && b.shouldSpeak(false) {
			text := "Howdy."
			if sociability >= 0.75 {
				text = "Evening."
			}
			speech = &domain.AgentSpeech{Text: text, Target: "player", Emotion: "neutral"}
			actions = append(actions, rulePlayerAction("look_at"))
		}
		return ruleDecision(goal, mood, speech, actions), nil

	case "ACTION_FAILED":
		mood = "frustrated"
		if tool, _ := domain.MapFrom(latest["data"])["tool"].(string); tool == "go_to" {
			// The runtime already attempted low-level recovery; choose a
			// modest local fallback rather than infinite retries.
			actions = append(actions, domain.AgentAction{
				Tool:      "wander",
				Arguments: map[string]any{"radius": 8.0},
			})
		}
		return ruleDecision(goal, mood, speech, actions), nil
	}

	return ruleDecision(goal, mood, speech, actions), nil
}

// shouldSpeak mirrors “RuleBasedAgentBackend._should_speak“. Only the
// pass-by path (“direct == false“) consumes a random draw, and only when the
// silence probability is strictly positive, so a backend configured with 0.0
// never advances its stream.
func (b *RuleBackend) shouldSpeak(direct bool) bool {
	if !direct {
		if b.silenceProbability <= 0 {
			return true
		}
		key := b.roll()
		return float64(key)/float64(ruleRollScale) >= b.silenceProbability
	}
	return b.silenceProbability <= 0.9
}

// roll returns the next integer in [0, ruleRollScale).
func (b *RuleBackend) roll() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.rng.Intn(ruleRollScale)
}

// ruleRollScale mirrors the “% 1000“ modulus in the Python implementation.
const ruleRollScale = 1000

// ruleReplyTo mirrors “RuleBasedAgentBackend._reply_to“. The boolean result
// reports whether the NPC has something to say; the Python original returns
// None for an empty transcript.
func ruleReplyTo(transcript string) (string, bool) {
	text := strings.ToLower(transcript)
	if strings.TrimSpace(text) == "" {
		return "", false
	}
	switch {
	case strings.Contains(text, "where"),
		strings.Contains(text, "headed"),
		strings.Contains(text, "going"):
		return "Valentine, if I keep moving.", true
	case strings.Contains(text, "hello"),
		strings.Contains(text, "hey"),
		strings.Contains(text, "evening"),
		strings.Contains(text, "morning"),
		strings.Contains(text, "howdy"):
		return "Evening.", true
	case strings.Contains(text, "thank"):
		return "Don't mention it.", true
	case strings.Contains(text, "help"):
		return "I have my own troubles.", true
	case strings.Contains(text, "your name"), strings.Contains(text, "who are you"):
		return "Nobody important.", true
	default:
		return "Hmph.", true
	}
}

// ruleFindRecent mirrors “_find_recent“: scan newest-first for the first
// event whose name satisfies match.
func ruleFindRecent(recent []map[string]any, match func(string) bool) (map[string]any, bool) {
	for index := len(recent) - 1; index >= 0; index-- {
		if match(ruleEventName(recent[index])) {
			return recent[index], true
		}
	}
	return nil, false
}

// ruleIsThreatEvent mirrors “RuleBasedAgentBackend.THREAT_EVENTS“.
func ruleIsThreatEvent(name string) bool {
	switch name {
	case "PLAYER_THREATENED_NPC", "PLAYER_ATTACKED_NPC", "NPC_DAMAGED":
		return true
	default:
		return false
	}
}

// ruleIsFriendlyEvent mirrors “RuleBasedAgentBackend.FRIENDLY_EVENTS“.
func ruleIsFriendlyEvent(name string) bool {
	return name == "PLAYER_HELPED_NPC"
}

// ruleEventName reads the event name of one timeline event.
func ruleEventName(event map[string]any) string {
	name, _ := event["event_name"].(string)
	return name
}

// ruleTrait mirrors “personality.get(key, fallback)“: the fallback applies
// only when the key is absent, so an explicit 0.0 is preserved.
func ruleTrait(personality map[string]any, key string, fallback float64) float64 {
	value, ok := personality[key]
	if !ok {
		return fallback
	}
	return domain.FloatFrom(value)
}

// ruleGoalCopy detaches the decision's goal from the caller's pointer, so a
// later context mutation cannot rewrite a decision that was already returned.
func ruleGoalCopy(goal *string) *string {
	if goal == nil {
		return nil
	}
	value := *goal
	return &value
}

// ruleGoal returns a pointer to text.
func ruleGoal(text string) *string { return &text }

// rulePlayerAction builds the “{"entity": "player"}“ action the Python
// backend emits for look_at and flee_from.
func rulePlayerAction(tool string) domain.AgentAction {
	return domain.AgentAction{Tool: tool, Arguments: map[string]any{"entity": "player"}}
}

// ruleDecision assembles a decision the way “AgentDecision(goal=..., ...)“
// does: mood is always present, goal is dropped when the context had none,
// and a nil speech means silence.
func ruleDecision(
	goal *string,
	mood string,
	speech *domain.AgentSpeech,
	actions []domain.AgentAction,
) *domain.AgentDecision {
	moodValue := mood
	return &domain.AgentDecision{
		Goal:     goal,
		Mood:     &moodValue,
		Speech:   speech,
		Actions:  actions,
		Internal: map[string]any{},
	}
}

// ruleTruthy mirrors Python truthiness for the JSON-ish values a data field
// can hold (“if courage >= 0.55 and position“).
func ruleTruthy(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return typed != ""
	case float64:
		return typed != 0
	case float32:
		return typed != 0
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case []any:
		return len(typed) > 0
	case []float64:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return true
	}
}

// ruleText mirrors Python's “str(value)“ for the scalar JSON values a
// timeline “data“ field can hold: a present-but-null text stringifies to
// "None" in Python, which is non-empty and therefore still earns a reply.
// Non-scalar values fall back to the runtime's coercion.
func ruleText(value any) string {
	switch typed := value.(type) {
	case nil:
		return "None"
	case string:
		return typed
	case bool:
		if typed {
			return "True"
		}
		return "False"
	default:
		return domain.StringFrom(value)
	}
}

// ruleDataText mirrors Python's str((data or {}).get("text", "")): the default
// applies only when the key is absent, so a transcript key that is present but
// null still stringifies (to "None") and earns a reply.
func ruleDataText(data map[string]any) string {
	raw, ok := data["text"]
	if !ok {
		return ""
	}
	return ruleText(raw)
}

// clampProbability mirrors Python's max(0.0, min(1.0, value)). Python's
// min(1.0, nan) keeps 1.0 because every comparison with NaN is false, so NaN
// clamps to 1.0 here as well instead of propagating.
func clampProbability(value float64) float64 {
	if math.IsNaN(value) {
		return 1
	}
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
