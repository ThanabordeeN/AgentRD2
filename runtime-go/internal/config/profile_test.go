package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/project"
)

func TestProfileStoreLoadsRepositoryProfile(t *testing.T) {
	root := project.Resolve(DefaultProfilesDir)
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("the repository profile directory is required: %v", err)
	}
	profile := NewProfileStore(root).Get("npc_001")

	if profile["npc_id"] != "npc_001" {
		t.Errorf("npc_id = %#v", profile["npc_id"])
	}
	if profile["name"] != "Elias Carter" {
		t.Errorf("name = %#v, want Elias Carter", profile["name"])
	}
	if background, _ := profile["background"].(string); !strings.Contains(background, "tenant farmer") {
		t.Errorf("background = %q, want it to mention tenant farmer", background)
	}
	if prompt, _ := profile["system_prompt"].(string); !strings.Contains(prompt, "Elias Carter") {
		t.Errorf("system_prompt = %q, want it to name the NPC", prompt)
	}
	if style, _ := profile["speech_style"].(string); style == "" {
		t.Error("speech_style must be present")
	}
	for _, key := range []string{"behavior_rules", "goals", "knowledge", "fears", "quirks"} {
		items, ok := profile[key].([]any)
		if !ok || len(items) == 0 {
			t.Errorf("%s = %#v, want a non-empty list", key, profile[key])
		}
	}
	if _, ok := profile["relationships"].(map[string]any); !ok {
		t.Errorf("relationships = %#v, want an object", profile["relationships"])
	}
	personality, ok := profile["personality"].(map[string]any)
	if !ok || len(personality) != 5 {
		t.Fatalf("personality = %#v, want the five traits", profile["personality"])
	}
}

func TestProfileStoreEmptyRootUsesRepositoryDirectory(t *testing.T) {
	store := NewProfileStore("")
	if want := project.Resolve(DefaultProfilesDir); store.Root() != want {
		t.Fatalf("Root() = %q, want %q", store.Root(), want)
	}
	if profile := store.Get("npc_hunter"); profile["name"] != "Silas Boone" {
		t.Fatalf("name = %#v, want Silas Boone", profile["name"])
	}
}

func TestProfileStoreNeutralDefaultsForMissingProfile(t *testing.T) {
	root := t.TempDir()
	store := NewProfileStore(root)
	if store.Root() != root {
		t.Fatalf("Root() = %q, want %q", store.Root(), root)
	}
	profile := store.Get("npc_ghost")

	if profile["npc_id"] != "npc_ghost" || profile["name"] != "npc_ghost" {
		t.Errorf("identity = %#v / %#v, want the requested id", profile["npc_id"], profile["name"])
	}
	wantPersonality := map[string]any{
		"sociability": 0.5,
		"curiosity":   0.5,
		"aggression":  0.3,
		"courage":     0.5,
		"patience":    0.5,
	}
	if !reflect.DeepEqual(profile["personality"], wantPersonality) {
		t.Errorf("personality = %#v, want %#v", profile["personality"], wantPersonality)
	}
	for key, want := range map[string]any{
		"system_prompt": "",
		"background":    "",
		"speech_style":  "",
	} {
		if profile[key] != want {
			t.Errorf("%s = %#v, want %#v", key, profile[key], want)
		}
	}
	for _, key := range []string{"behavior_rules", "goals", "knowledge", "fears", "quirks"} {
		items, ok := profile[key].([]any)
		if !ok {
			t.Errorf("%s = %#v, want an empty list", key, profile[key])
			continue
		}
		if len(items) != 0 {
			t.Errorf("%s = %#v, want an empty list", key, items)
		}
	}
	relationships, ok := profile["relationships"].(map[string]any)
	if !ok || len(relationships) != 0 {
		t.Errorf("relationships = %#v, want an empty object", profile["relationships"])
	}
}

func TestProfileStorePartialFileKeepsItsValuesAndFillsTheRest(t *testing.T) {
	root := t.TempDir()
	writeProfile(t, root, "npc_partial", `{
	  "npc_id": "npc_partial",
	  "name": "Partial",
	  "personality": {"sociability": 0.9},
	  "goals": ["keep the farm"]
	}`)
	profile := NewProfileStore(root).Get("npc_partial")

	if profile["name"] != "Partial" || profile["npc_id"] != "npc_partial" {
		t.Errorf("identity = %#v / %#v, want the file values", profile["npc_id"], profile["name"])
	}
	// Python only fills in a missing personality object; it never merges the
	// trait defaults into a partial one.
	if !reflect.DeepEqual(profile["personality"], map[string]any{"sociability": 0.9}) {
		t.Errorf("personality = %#v, want only the supplied trait", profile["personality"])
	}
	if !reflect.DeepEqual(profile["goals"], []any{"keep the farm"}) {
		t.Errorf("goals = %#v", profile["goals"])
	}
	if profile["background"] != "" {
		t.Errorf("background = %#v, want the default empty string", profile["background"])
	}
	if !reflect.DeepEqual(profile["relationships"], map[string]any{}) {
		t.Errorf("relationships = %#v, want an empty object", profile["relationships"])
	}
}

func TestProfileStoreFillsIdentityAndEmptyPersonalityWhenKeysAreAbsent(t *testing.T) {
	root := t.TempDir()
	writeProfile(t, root, "npc_noid", `{"background": "a stranger"}`)
	profile := NewProfileStore(root).Get("npc_noid")

	if profile["npc_id"] != "npc_noid" || profile["name"] != "npc_noid" {
		t.Errorf("identity = %#v / %#v, want the requested id", profile["npc_id"], profile["name"])
	}
	if !reflect.DeepEqual(profile["personality"], map[string]any{}) {
		t.Errorf("personality = %#v, want an empty object", profile["personality"])
	}
	if profile["background"] != "a stranger" {
		t.Errorf("background = %#v", profile["background"])
	}
}

func TestProfileStoreCorruptFileYieldsNeutralProfile(t *testing.T) {
	root := t.TempDir()
	writeProfile(t, root, "npc_broken", "{not json")
	profile := NewProfileStore(root).Get("npc_broken")

	if profile["name"] != "npc_broken" {
		t.Errorf("name = %#v, want the neutral profile", profile["name"])
	}
	if _, ok := profile["personality"].(map[string]any); !ok {
		t.Errorf("personality = %#v, want the neutral trait object", profile["personality"])
	}
}

func TestProfileStoreReturnsIndependentMaps(t *testing.T) {
	store := NewProfileStore(t.TempDir())

	first := store.Get("npc_a")
	first["behavior_rules"] = append(first["behavior_rules"].([]any), "mutated")
	first["relationships"].(map[string]any)["player"] = "mutated"
	first["personality"].(map[string]any)["sociability"] = 0.1

	second := store.Get("npc_a")
	if items := second["behavior_rules"].([]any); len(items) != 0 {
		t.Errorf("behavior_rules = %#v, want a fresh list", items)
	}
	if relationships := second["relationships"].(map[string]any); len(relationships) != 0 {
		t.Errorf("relationships = %#v, want a fresh object", relationships)
	}
	if personality := second["personality"].(map[string]any); personality["sociability"] != 0.5 {
		t.Errorf("personality = %#v, want fresh defaults", personality)
	}
}

// writeProfile writes “<root>/<npcID>.json“.
func writeProfile(t *testing.T, root, npcID, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, npcID+".json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
