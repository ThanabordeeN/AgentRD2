// Package config loads the runtime's settings and NPC profiles.
//
// It is a typed port of the Python “runtime/config.py“. Every default value
// and the deep-merge semantics are identical, but callers read compile-time
// fields instead of a loosely-typed map.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/project"
)

// DefaultSettingsPath is the project-relative file LoadSettings reads when it
// is given an empty path.
const DefaultSettingsPath = "config/settings.json"

// IPCConfig configures the bridge socket the runtime listens on.
type IPCConfig struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

// ActivationConfig holds the distance gates (in metres) that decide when an
// NPC becomes a candidate for awareness and conversation.
type ActivationConfig struct {
	CandidateDistanceM    float64 `json:"candidate_distance_m"`
	AwareDistanceM        float64 `json:"aware_distance_m"`
	ConversationRangeM    float64 `json:"conversation_range_m"`
	MinConversationRangeM float64 `json:"min_conversation_range_m"`
}

// SchedulerConfig caps how much reasoning work the runtime does at once.
type SchedulerConfig struct {
	MaxConversationAgents int     `json:"max_conversation_agents"`
	MaxReasoningAgents    int     `json:"max_reasoning_agents"`
	NearbyTrackedLimit    int     `json:"nearby_tracked_limit"`
	IdleThinkingSeconds   float64 `json:"idle_thinking_seconds"`
}

// PushToTalkConfig describes the push-to-talk capture key.
type PushToTalkConfig struct {
	Key              string `json:"key"`
	CaptureWhileHeld bool   `json:"capture_while_held"`
}

// SpeechConfig throttles how often an NPC may speak.
type SpeechConfig struct {
	CooldownSeconds            float64 `json:"cooldown_seconds"`
	RecentSpeechPenaltySeconds float64 `json:"recent_speech_penalty_seconds"`
	SocialRelevanceThreshold   float64 `json:"social_relevance_threshold"`
	SilenceProbability         float64 `json:"silence_probability"`
	GlobalCooldownSeconds      float64 `json:"global_cooldown_seconds"`
}

// StorySafetyConfig controls the conservative story-content gate.
type StorySafetyConfig struct {
	BlockIfUncertain bool `json:"block_if_uncertain"`
}

// ThinkingPolicy describes when an NPC may pause for a thinking gesture and
// which events or reasons skip reasoning entirely.
type ThinkingPolicy struct {
	DefaultMode        string   `json:"default_mode"`
	WaitGestures       bool     `json:"wait_gestures"`
	DisabledForEvents  []string `json:"disabled_for_events"`
	DisabledForReasons []string `json:"disabled_for_reasons"`
}

// ADKConfig selects the model backend and its credentials.
type ADKConfig struct {
	Provider        string         `json:"provider"`
	Model           string         `json:"model"`
	APIBase         string         `json:"api_base"`
	APIKeyEnv       string         `json:"api_key_env"`
	ReasoningEffort string         `json:"reasoning_effort"`
	ExtraBody       map[string]any `json:"extra_body"`
}

// Settings is the fully-merged runtime configuration.
//
// The directory and file fields are project-relative in DefaultSettings and
// are resolved to absolute paths by LoadSettings, mirroring
// “runtime.config.load_settings“.
type Settings struct {
	IPC            IPCConfig         `json:"ipc"`
	Activation     ActivationConfig  `json:"activation"`
	Scheduler      SchedulerConfig   `json:"scheduler"`
	PushToTalk     PushToTalkConfig  `json:"push_to_talk"`
	Speech         SpeechConfig      `json:"speech"`
	StorySafety    StorySafetyConfig `json:"story_safety"`
	ThinkingPolicy ThinkingPolicy    `json:"thinking_policy"`
	ADK            ADKConfig         `json:"adk"`

	StoryBlacklistPath   string `json:"story_blacklist_path"`
	ProfilesDir          string `json:"profiles_dir"`
	TimelinesDir         string `json:"timelines_dir"`
	WikiContextPath      string `json:"wiki_context_path"`
	CharacterContextPath string `json:"character_context_path"`
	QuestsDir            string `json:"quests_dir"`
}

// DefaultSettings returns the built-in defaults from “DEFAULT_SETTINGS“.
//
// It returns fresh slices and maps on every call, so callers may mutate the
// result freely.
func DefaultSettings() Settings {
	return Settings{
		IPC: IPCConfig{Host: "127.0.0.1", Port: 8765},
		Activation: ActivationConfig{
			CandidateDistanceM:    20.0,
			AwareDistanceM:        10.0,
			ConversationRangeM:    8.0,
			MinConversationRangeM: 5.0,
		},
		Scheduler: SchedulerConfig{
			MaxConversationAgents: 1,
			MaxReasoningAgents:    3,
			NearbyTrackedLimit:    32,
			IdleThinkingSeconds:   5.0,
		},
		PushToTalk: PushToTalkConfig{Key: "V", CaptureWhileHeld: true},
		Speech: SpeechConfig{
			CooldownSeconds:            6.0,
			RecentSpeechPenaltySeconds: 12.0,
			SocialRelevanceThreshold:   0.45,
			SilenceProbability:         0.25,
			GlobalCooldownSeconds:      2.0,
		},
		StorySafety: StorySafetyConfig{BlockIfUncertain: true},
		ThinkingPolicy: ThinkingPolicy{
			DefaultMode:  "low",
			WaitGestures: true,
			DisabledForEvents: []string{
				"PLAYER_APPROACHED",
				"PLAYER_LOOKED_AT_NPC",
				"PLAYER_LEFT_AREA",
				"NPC_ACTIVATED",
			},
			DisabledForReasons: []string{
				"ped_scan_activation",
				"idle",
				"pass_by",
				"deferred_promotion",
			},
		},
		ADK: ADKConfig{
			Provider:        "openai",
			Model:           "deepseek-v4.1-flash",
			APIBase:         "https://opencode.ai/zen/go/v1",
			APIKeyEnv:       "OPENCODE_API_KEY",
			ReasoningEffort: "low",
			ExtraBody:       map[string]any{"thinking": map[string]any{"type": "enabled"}},
		},
		StoryBlacklistPath:   "config/story_blacklist.json",
		ProfilesDir:          "data/profiles",
		TimelinesDir:         "data/timelines",
		WikiContextPath:      "data/wiki/rdr2_context.json",
		CharacterContextPath: "data/wiki/characters.json",
		QuestsDir:            "data/quests",
	}
}

// LoadSettings reads a settings file and merges it over DefaultSettings.
//
// An empty path means DefaultSettingsPath. A missing file is not an error: the
// built-in defaults are returned verbatim, exactly like “load_settings“
// (including their project-relative data paths). A partial file is deep-merged
// over the defaults, so nested objects such as “adk.extra_body“ keep the
// keys the file does not mention, and the directory and file fields are then
// resolved to absolute paths via project.Resolve.
//
// Invalid JSON, or a value whose JSON type does not fit the typed field,
// returns the defaults together with the error.
func LoadSettings(path string) (Settings, error) {
	settings := DefaultSettings()
	if path == "" {
		path = DefaultSettingsPath
	}
	target := resolvePath(path)
	data, err := os.ReadFile(target)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return settings, nil
		}
		return settings, fmt.Errorf("config: read %s: %w", target, err)
	}

	var overlay map[string]any
	if err := json.Unmarshal(data, &overlay); err != nil {
		return settings, fmt.Errorf("config: parse %s: %w", target, err)
	}
	if overlay == nil {
		return settings, nil
	}

	merged, err := mergeInto(settings, overlay)
	if err != nil {
		return settings, fmt.Errorf("config: merge %s: %w", target, err)
	}
	return merged.ResolvePaths(), nil
}

// LoadDefaultSettings loads DefaultSettingsPath.
func LoadDefaultSettings() (Settings, error) {
	return LoadSettings(DefaultSettingsPath)
}

// ResolvePaths returns a copy of the settings whose project-relative data
// paths are absolute. Absolute paths and empty values are left alone.
func (s Settings) ResolvePaths() Settings {
	for _, field := range []*string{
		&s.StoryBlacklistPath,
		&s.ProfilesDir,
		&s.TimelinesDir,
		&s.WikiContextPath,
		&s.CharacterContextPath,
		&s.QuestsDir,
	} {
		if *field == "" {
			continue
		}
		*field = project.Resolve(*field)
	}
	return s
}

// mergeInto deep-merges a decoded JSON object over the defaults and decodes
// the result back into the typed settings.
func mergeInto(defaults Settings, overlay map[string]any) (Settings, error) {
	encoded, err := json.Marshal(defaults)
	if err != nil {
		return defaults, err
	}
	base := map[string]any{}
	if err := json.Unmarshal(encoded, &base); err != nil {
		return defaults, err
	}
	merged, err := json.Marshal(deepMerge(base, overlay))
	if err != nil {
		return defaults, err
	}
	var result Settings
	if err := json.Unmarshal(merged, &result); err != nil {
		return defaults, err
	}
	return result, nil
}

// deepMerge mirrors the Python “_deep_merge“: nested objects merge
// key-by-key, anything else is replaced by the overlay value.
func deepMerge(base, overlay map[string]any) map[string]any {
	result := make(map[string]any, len(base)+len(overlay))
	for key, value := range base {
		result[key] = value
	}
	for key, value := range overlay {
		overlayMap, overlayIsMap := value.(map[string]any)
		baseMap, baseIsMap := result[key].(map[string]any)
		if overlayIsMap && baseIsMap {
			result[key] = deepMerge(baseMap, overlayMap)
			continue
		}
		result[key] = value
	}
	return result
}

// resolvePath prefers a project-root file when the relative path names one, so
// the runtime finds its configuration from any working directory, and falls
// back to the working directory for package-relative files (such as test
// fixtures) that do not exist under the project root.
func resolvePath(path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	resolved := project.Resolve(path)
	if _, err := os.Stat(resolved); err == nil {
		return resolved
	}
	return path
}
