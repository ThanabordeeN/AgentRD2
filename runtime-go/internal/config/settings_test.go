package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/project"
)

func TestDefaultSettingsMatchesPythonDefaults(t *testing.T) {
	settings := DefaultSettings()

	if settings.IPC.Host != "127.0.0.1" || settings.IPC.Port != 8765 {
		t.Errorf("IPC = %+v", settings.IPC)
	}
	if settings.Activation != (ActivationConfig{
		CandidateDistanceM:    20.0,
		AwareDistanceM:        10.0,
		ConversationRangeM:    8.0,
		MinConversationRangeM: 5.0,
	}) {
		t.Errorf("Activation = %+v", settings.Activation)
	}
	if settings.Scheduler != (SchedulerConfig{
		MaxConversationAgents: 1,
		MaxReasoningAgents:    3,
		NearbyTrackedLimit:    32,
		IdleThinkingSeconds:   5.0,
	}) {
		t.Errorf("Scheduler = %+v", settings.Scheduler)
	}
	if settings.PushToTalk != (PushToTalkConfig{Key: "V", CaptureWhileHeld: true}) {
		t.Errorf("PushToTalk = %+v", settings.PushToTalk)
	}
	if settings.Speech != (SpeechConfig{
		CooldownSeconds:            6.0,
		RecentSpeechPenaltySeconds: 12.0,
		SocialRelevanceThreshold:   0.45,
		SilenceProbability:         0.25,
		GlobalCooldownSeconds:      2.0,
	}) {
		t.Errorf("Speech = %+v", settings.Speech)
	}
	if !settings.StorySafety.BlockIfUncertain {
		t.Error("StorySafety.BlockIfUncertain must default to true")
	}
	if settings.ThinkingPolicy.DefaultMode != "low" || !settings.ThinkingPolicy.WaitGestures {
		t.Errorf("ThinkingPolicy = %+v", settings.ThinkingPolicy)
	}
	wantEvents := []string{"PLAYER_APPROACHED", "PLAYER_LOOKED_AT_NPC", "PLAYER_LEFT_AREA", "NPC_ACTIVATED"}
	if !reflect.DeepEqual(settings.ThinkingPolicy.DisabledForEvents, wantEvents) {
		t.Errorf("DisabledForEvents = %v, want %v", settings.ThinkingPolicy.DisabledForEvents, wantEvents)
	}
	wantReasons := []string{"ped_scan_activation", "idle", "pass_by", "deferred_promotion"}
	if !reflect.DeepEqual(settings.ThinkingPolicy.DisabledForReasons, wantReasons) {
		t.Errorf("DisabledForReasons = %v, want %v", settings.ThinkingPolicy.DisabledForReasons, wantReasons)
	}
	wantADK := ADKConfig{
		Provider:        "openai",
		Model:           "deepseek-v4.1-flash",
		APIBase:         "https://opencode.ai/zen/go/v1",
		APIKeyEnv:       "OPENCODE_API_KEY",
		ReasoningEffort: "low",
	}
	if settings.ADK.Provider != wantADK.Provider ||
		settings.ADK.Model != wantADK.Model ||
		settings.ADK.APIBase != wantADK.APIBase ||
		settings.ADK.APIKeyEnv != wantADK.APIKeyEnv ||
		settings.ADK.ReasoningEffort != wantADK.ReasoningEffort {
		t.Errorf("ADK = %+v, want %+v", settings.ADK, wantADK)
	}
	if !reflect.DeepEqual(settings.ADK.ExtraBody, map[string]any{
		"thinking": map[string]any{"type": "enabled"},
	}) {
		t.Errorf("ADK.ExtraBody = %#v", settings.ADK.ExtraBody)
	}
	for field, want := range map[string]string{
		"story_blacklist_path":   "config/story_blacklist.json",
		"profiles_dir":           "data/profiles",
		"timelines_dir":          "data/timelines",
		"wiki_context_path":      "data/wiki/rdr2_context.json",
		"character_context_path": "data/wiki/characters.json",
		"quests_dir":             "data/quests",
	} {
		var got string
		switch field {
		case "story_blacklist_path":
			got = settings.StoryBlacklistPath
		case "profiles_dir":
			got = settings.ProfilesDir
		case "timelines_dir":
			got = settings.TimelinesDir
		case "wiki_context_path":
			got = settings.WikiContextPath
		case "character_context_path":
			got = settings.CharacterContextPath
		case "quests_dir":
			got = settings.QuestsDir
		}
		if got != want {
			t.Errorf("%s = %q, want %q", field, got, want)
		}
	}
}

func TestDefaultSettingsReturnsFreshContainers(t *testing.T) {
	first := DefaultSettings()
	first.ThinkingPolicy.DisabledForEvents[0] = "MUTATED"
	first.ThinkingPolicy.DisabledForReasons[0] = "MUTATED"
	first.ADK.ExtraBody["thinking"].(map[string]any)["type"] = "MUTATED"

	second := DefaultSettings()
	if second.ThinkingPolicy.DisabledForEvents[0] != "PLAYER_APPROACHED" {
		t.Error("DisabledForEvents must not be shared between calls")
	}
	if second.ThinkingPolicy.DisabledForReasons[0] != "ped_scan_activation" {
		t.Error("DisabledForReasons must not be shared between calls")
	}
	if second.ADK.ExtraBody["thinking"].(map[string]any)["type"] != "enabled" {
		t.Error("ADK.ExtraBody must not be shared between calls")
	}
}

func TestLoadSettingsReadsRepositoryFile(t *testing.T) {
	path := project.Resolve(DefaultSettingsPath)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the repository settings file is required: %v", err)
	}
	settings, err := LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	want := DefaultSettings().ResolvePaths()
	if !reflect.DeepEqual(settings, want) {
		t.Fatalf("config/settings.json must match the built-in defaults:\ngot  %#v\nwant %#v", settings, want)
	}
}

func TestLoadSettingsEmptyPathUsesDefaultFile(t *testing.T) {
	settings, err := LoadSettings("")
	if err != nil {
		t.Fatalf("LoadSettings(\"\"): %v", err)
	}
	want, err := LoadSettings(DefaultSettingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(settings, want) {
		t.Fatalf("LoadSettings(\"\") = %#v, want %#v", settings, want)
	}
}

func TestLoadSettingsMissingFileReturnsDefaults(t *testing.T) {
	settings, err := LoadSettings(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("a missing file must not be an error: %v", err)
	}
	if !reflect.DeepEqual(settings, DefaultSettings()) {
		t.Fatalf("missing file = %#v, want the exact built-in defaults", settings)
	}
	if filepath.IsAbs(settings.ProfilesDir) {
		t.Error("the missing-file branch mirrors Python and keeps default paths relative")
	}
}

func TestLoadSettingsPartialFileDeepMerges(t *testing.T) {
	path := writeSettings(t, `{
	  "ipc": {"port": 9001},
	  "speech": {"cooldown_seconds": 1.5},
	  "thinking_policy": {"disabled_for_events": ["CUSTOM_EVENT"]},
	  "adk": {"extra_body": {"thinking": {"budget": 5}, "temperature": 0.2}}
	}`)
	settings, err := LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if settings.IPC.Port != 9001 || settings.IPC.Host != "127.0.0.1" {
		t.Errorf("IPC = %+v, want the overlay port and the default host", settings.IPC)
	}
	if settings.Speech.CooldownSeconds != 1.5 || settings.Speech.SilenceProbability != 0.25 {
		t.Errorf("Speech = %+v, want the overlay cooldown and the default silence probability", settings.Speech)
	}
	if !reflect.DeepEqual(settings.ThinkingPolicy.DisabledForEvents, []string{"CUSTOM_EVENT"}) {
		t.Errorf("DisabledForEvents = %v, want the overlay list", settings.ThinkingPolicy.DisabledForEvents)
	}
	if len(settings.ThinkingPolicy.DisabledForReasons) != 4 || settings.ThinkingPolicy.DefaultMode != "low" {
		t.Errorf("ThinkingPolicy = %+v, want untouched defaults", settings.ThinkingPolicy)
	}
	if settings.ADK.Model != "deepseek-v4.1-flash" {
		t.Errorf("ADK.Model = %q, want the default", settings.ADK.Model)
	}
	thinking, ok := settings.ADK.ExtraBody["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("ADK.ExtraBody[thinking] = %#v", settings.ADK.ExtraBody["thinking"])
	}
	if thinking["type"] != "enabled" || thinking["budget"] != float64(5) {
		t.Errorf("nested extra_body must deep-merge, got %#v", thinking)
	}
	if settings.ADK.ExtraBody["temperature"] != 0.2 {
		t.Errorf("extra_body temperature = %#v, want 0.2", settings.ADK.ExtraBody["temperature"])
	}
}

func TestLoadSettingsResolvesRelativeDataPaths(t *testing.T) {
	path := writeSettings(t, `{
	  "profiles_dir": "custom/profiles",
	  "wiki_context_path": "custom/wiki.json"
	}`)
	settings, err := LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	for field, got := range map[string]string{
		"profiles_dir":           settings.ProfilesDir,
		"wiki_context_path":      settings.WikiContextPath,
		"quests_dir":             settings.QuestsDir,
		"timelines_dir":          settings.TimelinesDir,
		"story_blacklist_path":   settings.StoryBlacklistPath,
		"character_context_path": settings.CharacterContextPath,
	} {
		if !filepath.IsAbs(got) {
			t.Errorf("%s = %q, want an absolute path", field, got)
		}
	}
	if want := project.Resolve("custom/profiles"); settings.ProfilesDir != want {
		t.Errorf("ProfilesDir = %q, want %q", settings.ProfilesDir, want)
	}
	if want := project.Resolve("data/quests"); settings.QuestsDir != want {
		t.Errorf("QuestsDir = %q, want the resolved default %q", settings.QuestsDir, want)
	}
}

func TestLoadSettingsKeepsAbsolutePaths(t *testing.T) {
	absolute := filepath.Join(t.TempDir(), "profiles")
	path := writeSettings(t, `{"profiles_dir": `+strconv.Quote(absolute)+`}`)
	settings, err := LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if settings.ProfilesDir != absolute {
		t.Errorf("ProfilesDir = %q, want the absolute path unchanged", settings.ProfilesDir)
	}
}

func TestLoadSettingsInvalidJSONReturnsDefaultsAndError(t *testing.T) {
	path := writeSettings(t, "{not json")
	settings, err := LoadSettings(path)
	if err == nil {
		t.Fatal("invalid JSON must be reported")
	}
	if !reflect.DeepEqual(settings, DefaultSettings()) {
		t.Fatalf("settings = %#v, want the defaults alongside the error", settings)
	}
}

func TestLoadSettingsTypeMismatchReturnsError(t *testing.T) {
	path := writeSettings(t, `{"ipc": {"port": "8765"}}`)
	if _, err := LoadSettings(path); err == nil {
		t.Fatal("a string port must be reported instead of silently ignored")
	}
}

func TestLoadSettingsJSONNullReturnsDefaults(t *testing.T) {
	path := writeSettings(t, "null")
	settings, err := LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if !reflect.DeepEqual(settings, DefaultSettings()) {
		t.Fatalf("settings = %#v, want the defaults", settings)
	}
}

func TestResolvePathsLeavesEmptyValuesAlone(t *testing.T) {
	empty := Settings{}
	if resolved := empty.ResolvePaths(); !reflect.DeepEqual(resolved, Settings{}) {
		t.Fatalf("ResolvePaths() = %#v, want it unchanged", resolved)
	}
}

// writeSettings writes a temporary settings file.
func writeSettings(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
