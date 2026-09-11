package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// DefaultProfilesDir is the project-relative directory NewProfileStore uses
// when it is given an empty root.
const DefaultProfilesDir = "data/profiles"

// ProfileStore loads NPC profiles from a directory of “<npc_id>.json“ files.
//
// Profiles are configuration, not memory. A missing (or unreadable) profile
// falls back to a neutral ambient profile so the runtime still works in tests
// and demos, mirroring “ProfileStore“ in “runtime/config.py“.
type ProfileStore struct {
	root string
}

// NewProfileStore returns a store rooted at root. An empty root selects
// DefaultProfilesDir. A relative root that exists below the project root is
// resolved against it, so the runtime finds data/profiles from any working
// directory.
func NewProfileStore(root string) *ProfileStore {
	if root == "" {
		root = DefaultProfilesDir
	}
	return &ProfileStore{root: resolvePath(root)}
}

// Root returns the directory the store reads profiles from.
func (p *ProfileStore) Root() string {
	return p.root
}

// Get returns the profile for npcID.
//
// The returned map is freshly built on every call, so callers may mutate it.
// A profile file that is missing, unreadable or not a JSON object yields the
// neutral profile with npc_id and name set to npcID. An existing profile keeps
// its own values and is only filled in where a key is absent: npc_id and name
// default to npcID, personality to an empty object, and every remaining
// “_PROFILE_DEFAULTS“ key to its built-in default.
func (p *ProfileStore) Get(npcID string) map[string]any {
	profile := p.read(npcID)
	if profile == nil {
		return neutralProfile(npcID)
	}
	if _, ok := profile["npc_id"]; !ok {
		profile["npc_id"] = npcID
	}
	if _, ok := profile["name"]; !ok {
		profile["name"] = npcID
	}
	if _, ok := profile["personality"]; !ok {
		profile["personality"] = map[string]any{}
	}
	defaults := profileDefaults()
	for key, value := range defaults {
		if key == "personality" {
			continue
		}
		if _, ok := profile[key]; !ok {
			profile[key] = value
		}
	}
	return profile
}

// read decodes one profile file. It returns nil when the file cannot be used,
// in which case Get falls back to the neutral profile.
func (p *ProfileStore) read(npcID string) map[string]any {
	data, err := os.ReadFile(filepath.Join(p.root, npcID+".json"))
	if err != nil {
		return nil
	}
	var profile map[string]any
	if err := json.Unmarshal(data, &profile); err != nil {
		return nil
	}
	return profile
}

// neutralProfile mirrors “_NEUTRAL_PROFILE“: every profile default with the
// requested identity stamped on top.
func neutralProfile(npcID string) map[string]any {
	profile := profileDefaults()
	profile["npc_id"] = npcID
	profile["name"] = npcID
	return profile
}

// profileDefaults returns fresh copies of “_PROFILE_DEFAULTS“ so no caller
// can share (or mutate) another profile's slices and maps.
func profileDefaults() map[string]any {
	return map[string]any{
		"personality": map[string]any{
			"sociability": 0.5,
			"curiosity":   0.5,
			"aggression":  0.3,
			"courage":     0.5,
			"patience":    0.5,
		},
		"system_prompt":  "",
		"background":     "",
		"speech_style":   "",
		"behavior_rules": []any{},
		"goals":          []any{},
		"knowledge":      []any{},
		"relationships":  map[string]any{},
		"fears":          []any{},
		"quirks":         []any{},
	}
}
