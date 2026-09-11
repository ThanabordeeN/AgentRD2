package lore

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/project"
)

// realDataPaths returns the repository wiki packs, failing the test when they
// are missing: these tests are meant to exercise the shipped data.
func realDataPaths(t *testing.T) (worldPath, charactersPath string) {
	t.Helper()
	worldPath = project.Resolve(DefaultWorldPath)
	charactersPath = project.Resolve(DefaultCharactersPath)
	for _, path := range []string{worldPath, charactersPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("the repository wiki pack %s is required: %v", path, err)
		}
	}
	return worldPath, charactersPath
}

func TestStoreLoadsRealWikiPack(t *testing.T) {
	worldPath, charactersPath := realDataPaths(t)
	store := NewStore(worldPath, charactersPath)

	topics := store.AvailableTopics()
	if len(topics) != 8 {
		t.Fatalf("AvailableTopics() = %d topics (%v), want 8", len(topics), topics)
	}
	if !sort.StringsAreSorted(topics) {
		t.Errorf("AvailableTopics() must be sorted: %v", topics)
	}
	for _, want := range []string{"Valentine", "Hunting", "Red Dead Redemption 2"} {
		if !containsString(topics, want) {
			t.Errorf("AvailableTopics() missing %q: %v", want, topics)
		}
	}

	page, ok := store.Get("Valentine")
	if !ok {
		t.Fatal("Get(\"Valentine\") must find the settlement page")
	}
	summary, _ := page["summary"].(string)
	if strings.TrimSpace(summary) == "" {
		t.Error("the Valentine page must carry a summary")
	}
	if url, _ := page["url"].(string); !strings.Contains(url, "fandom.com") {
		t.Errorf("url = %q, want a fandom.com link", url)
	}
	if page["title"] != "Valentine" {
		t.Errorf("title = %#v", page["title"])
	}
	if _, ok := store.Get("Hunting"); !ok {
		t.Error("Get(\"Hunting\") must find the hunting page")
	}
}

func TestStoreLoadsRealCharacterPack(t *testing.T) {
	worldPath, charactersPath := realDataPaths(t)
	store := NewStore(worldPath, charactersPath)

	characters := store.AvailableCharacters()
	if len(characters) != 838 {
		t.Fatalf("AvailableCharacters() = %d characters, want 838", len(characters))
	}
	if !sort.StringsAreSorted(characters) {
		t.Error("AvailableCharacters() must be sorted")
	}
	for _, want := range []string{"Arthur Morgan", "Sadie Adler"} {
		if !containsString(characters, want) {
			t.Errorf("AvailableCharacters() missing %q", want)
		}
	}

	arthur, ok := store.Character("Arthur Morgan")
	if !ok {
		t.Fatal("Character(\"Arthur Morgan\") must succeed")
	}
	summary, _ := arthur["summary"].(string)
	if !strings.Contains(strings.ToLower(summary), "protagonist") {
		t.Errorf("Arthur's summary should mention the protagonist: %q", firstRunes(summary, 120))
	}
	infobox, ok := arthur["infobox"].(map[string]any)
	if !ok {
		t.Fatalf("infobox = %#v, want an object", arthur["infobox"])
	}
	if gender, _ := infobox["gender"].(string); gender == "" {
		t.Error("Arthur's infobox must carry a gender")
	}
	if arthur["kind"] != "character" {
		t.Errorf("kind = %#v, want \"character\"", arthur["kind"])
	}
	if arthur["source"] == nil {
		t.Error("character entries must carry their source attribution")
	}
}

func TestStoreGetAndCharacterMatchCaseInsensitivelyAndFuzzily(t *testing.T) {
	worldPath, charactersPath := realDataPaths(t)
	store := NewStore(worldPath, charactersPath)

	for _, topic := range []string{"Valentine", "valentine", "VALENTINE", "  valentine  "} {
		page, ok := store.Get(topic)
		if !ok {
			t.Fatalf("Get(%q) must succeed", topic)
		}
		if page["title"] != "Valentine" {
			t.Errorf("Get(%q) title = %#v", topic, page["title"])
		}
	}
	if page, ok := store.Get("Pinkerton"); !ok || page["title"] != "Pinkerton National Detective Agency" {
		t.Errorf("a substring topic must find its page, got %#v (found=%v)", page, ok)
	}
	if _, ok := store.Get(""); ok {
		t.Error("Get(\"\") must not match")
	}
	if _, ok := store.Get("No Such Topic Exists"); ok {
		t.Error("an unknown topic must not match")
	}

	for _, name := range []string{"Arthur Morgan", "arthur morgan"} {
		if _, ok := store.Character(name); !ok {
			t.Fatalf("Character(%q) must succeed", name)
		}
	}
	if character, ok := store.Character("Sadie"); !ok || character["title"] != "Sadie Adler" {
		t.Errorf("a substring name must find its character, got %#v (found=%v)", character, ok)
	}
	if _, ok := store.Character(""); ok {
		t.Error("Character(\"\") must not match")
	}
	if _, ok := store.Character("Nobody At All"); ok {
		t.Error("an unknown name must not match")
	}
}

func TestStoreLookup(t *testing.T) {
	worldPath, charactersPath := realDataPaths(t)
	store := NewStore(worldPath, charactersPath)

	results := store.Lookup("Hunting", DefaultLookupLimit, DefaultLookupMaxChars)
	if len(results) == 0 {
		t.Fatal("Lookup(\"Hunting\") must return the hunting page")
	}
	if results[0]["title"] != "Hunting" {
		t.Errorf("Lookup(\"Hunting\")[0].title = %#v, want Hunting", results[0]["title"])
	}
	if len(results) > DefaultLookupLimit {
		t.Errorf("Lookup returned %d results, want at most %d", len(results), DefaultLookupLimit)
	}
	if lower := store.Lookup("hunting", DefaultLookupLimit, DefaultLookupMaxChars); len(lower) == 0 || lower[0]["title"] != "Hunting" {
		t.Errorf("Lookup must be case-insensitive, got %#v", lower)
	}
	if empty := store.Lookup("   ", DefaultLookupLimit, DefaultLookupMaxChars); len(empty) != 0 {
		t.Errorf("Lookup(\"   \") = %#v, want nothing", empty)
	}
	if none := store.Lookup("zzzzzz-not-a-topic", DefaultLookupLimit, DefaultLookupMaxChars); len(none) != 0 {
		t.Errorf("Lookup of an unknown query = %#v, want nothing", none)
	}

	trimmed := store.Lookup("Arthur Morgan", 1, 40)
	if len(trimmed) != 1 || trimmed[0]["title"] != "Arthur Morgan" {
		t.Fatalf("Lookup(\"Arthur Morgan\", 1, 40) = %#v", trimmed)
	}
	summary, _ := trimmed[0]["summary"].(string)
	if utf8.RuneCountInString(summary) > 40 {
		t.Errorf("summary has %d characters, want at most 40", utf8.RuneCountInString(summary))
	}
	if summary != strings.TrimSpace(summary) {
		t.Errorf("summary must be stripped, got %q", summary)
	}
	if limited := store.Lookup("a", 2, DefaultLookupMaxChars); len(limited) != 2 {
		t.Errorf("Lookup with limit 2 returned %d results", len(limited))
	}
}

func TestStoreContextForProfileRegionAndWikiTopics(t *testing.T) {
	worldPath, charactersPath := realDataPaths(t)
	store := NewStore(worldPath, charactersPath)

	profile := map[string]any{
		"npc_id":      "npc_001",
		"wiki_topics": []any{"Valentine", "New Hanover"},
		"region":      "Ambarino",
	}
	entries := store.ContextForProfile(profile, DefaultContextTopics, DefaultContextMaxChars)
	titles := entryTitles(entries)
	want := []string{"Valentine", "New Hanover", "Ambarino"}
	if len(titles) < len(want) {
		t.Fatalf("ContextForProfile titles = %v, want %v", titles, want)
	}
	for index, title := range want {
		if titles[index] != title {
			t.Errorf("ContextForProfile titles = %v, want %v in order", titles, want)
			break
		}
	}
	for _, entry := range entries {
		if summary, _ := entry["summary"].(string); strings.TrimSpace(summary) == "" {
			t.Errorf("entry %#v must carry a summary", entry["title"])
		}
	}
}

func TestStoreContextForProfileVariants(t *testing.T) {
	worldPath, charactersPath := realDataPaths(t)
	store := NewStore(worldPath, charactersPath)

	// A profile name that maps to a wiki character wins over everything else.
	named := store.ContextForProfile(map[string]any{"npc_id": "npc_arthur", "name": "Arthur Morgan"}, DefaultContextTopics, DefaultContextMaxChars)
	if len(named) == 0 || named[0]["title"] != "Arthur Morgan" {
		t.Fatalf("named profile context = %#v, want Arthur Morgan first", entryTitles(named))
	}

	// A bare profile falls back to the game page.
	fallback := store.ContextForProfile(map[string]any{"npc_id": "npc_001"}, DefaultContextTopics, DefaultContextMaxChars)
	if len(fallback) == 0 || fallback[0]["title"] != "Red Dead Redemption 2" {
		t.Fatalf("bare profile context = %#v, want the game page", entryTitles(fallback))
	}

	// wiki_topics may be a single string, and duplicate titles are dropped.
	duplicated := store.ContextForProfile(map[string]any{
		"wiki_topics": []any{"Valentine", "valentine", "Hunting"},
	}, DefaultContextTopics, DefaultContextMaxChars)
	if got := entryTitles(duplicated); len(got) != 2 || got[0] != "Valentine" || got[1] != "Hunting" {
		t.Errorf("deduplicated context = %v, want [Valentine Hunting]", got)
	}
	single := store.ContextForProfile(map[string]any{"wiki_topics": "Valentine"}, DefaultContextTopics, DefaultContextMaxChars)
	if len(single) == 0 || single[0]["title"] != "Valentine" {
		t.Errorf("string wiki_topics context = %v, want Valentine", entryTitles(single))
	}

	// The world state contributes the in-game region.
	world := map[string]any{"location": map[string]any{"region": "Ambarino"}}
	withWorld := store.ContextForProfileWithWorld(map[string]any{}, world, DefaultContextTopics, DefaultContextMaxChars)
	if got := entryTitles(withWorld); !containsString(got, "Ambarino") {
		t.Errorf("world-state context = %v, want Ambarino", got)
	}

	// limit and maxChars are honoured.
	limited := store.ContextForProfile(map[string]any{"wiki_topics": []any{"Valentine", "New Hanover"}}, 1, DefaultContextMaxChars)
	if len(limited) != 1 {
		t.Errorf("limit 1 returned %d entries", len(limited))
	}
	short := store.ContextForProfile(map[string]any{"wiki_topics": []any{"Valentine"}}, DefaultContextTopics, 20)
	if len(short) == 0 {
		t.Fatal("a short maxChars must still return the entry")
	}
	if summary, _ := short[0]["summary"].(string); utf8.RuneCountInString(summary) > 20 {
		t.Errorf("summary has %d characters, want at most 20", utf8.RuneCountInString(summary))
	}

	// Python's context_for_profile still returns one entry when the topic cap
	// is zero, because the "no results" fallback runs afterwards.
	zero := store.ContextForProfile(map[string]any{"wiki_topics": []any{"Valentine"}}, 0, DefaultContextMaxChars)
	if len(zero) != 1 {
		t.Errorf("limit 0 returned %d entries, want the fallback entry", len(zero))
	}
}

func TestStoreMetaMergesPacks(t *testing.T) {
	worldPath, charactersPath := realDataPaths(t)
	store := NewStore(worldPath, charactersPath)

	meta := store.Meta()
	if meta["source"] != "Red Dead Wiki (Fandom)" {
		t.Errorf("meta source = %#v", meta["source"])
	}
	// "categories" only exists in the character pack, which proves the
	// character metadata is merged underneath the wiki metadata.
	if _, ok := meta["categories"]; !ok {
		t.Errorf("meta = %#v, want the character-pack keys merged in", meta)
	}
	meta["source"] = "mutated"
	if store.Meta()["source"] != "Red Dead Wiki (Fandom)" {
		t.Error("Meta must return a copy")
	}
}

func TestStoreMissingOrCorruptPacksAreTolerated(t *testing.T) {
	dir := t.TempDir()
	corrupt := filepath.Join(dir, "characters.json")
	if err := os.WriteFile(corrupt, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewStore(filepath.Join(dir, "absent.json"), corrupt)

	if topics := store.AvailableTopics(); len(topics) != 0 {
		t.Errorf("AvailableTopics() = %v, want nothing", topics)
	}
	if characters := store.AvailableCharacters(); len(characters) != 0 {
		t.Errorf("AvailableCharacters() = %v, want nothing", characters)
	}
	if _, ok := store.Get("Valentine"); ok {
		t.Error("Get must not match with no packs")
	}
	if _, ok := store.Character("Arthur Morgan"); ok {
		t.Error("Character must not match with no packs")
	}
	if results := store.Lookup("hunting", DefaultLookupLimit, DefaultLookupMaxChars); len(results) != 0 {
		t.Errorf("Lookup = %#v, want nothing", results)
	}
	if results := store.ContextForProfile(map[string]any{"region": "Ambarino"}, DefaultContextTopics, DefaultContextMaxChars); len(results) != 0 {
		t.Errorf("ContextForProfile = %#v, want nothing", results)
	}
	if meta := store.Meta(); meta == nil || len(meta) != 0 {
		t.Errorf("Meta() = %#v, want an empty map", meta)
	}
}

func TestStoreWorldPackOnlyStillAnswersCharactersFromWiki(t *testing.T) {
	worldPath, _ := realDataPaths(t)
	store := NewStore(worldPath, filepath.Join(t.TempDir(), "absent.json"))

	if topics := store.AvailableTopics(); len(topics) != 8 {
		t.Fatalf("AvailableTopics() = %d topics, want 8", len(topics))
	}
	if characters := store.AvailableCharacters(); len(characters) != 0 {
		t.Fatalf("AvailableCharacters() = %d, want 0", len(characters))
	}
	if _, ok := store.Get("Valentine"); !ok {
		t.Error("Get must still work without the character pack")
	}
	if _, ok := store.Character("Arthur Morgan"); ok {
		t.Error("Character must not match without the character pack")
	}
}

func TestStoreCharacterPackOnlyFallsBackToACharacter(t *testing.T) {
	_, charactersPath := realDataPaths(t)
	store := NewStore(filepath.Join(t.TempDir(), "absent.json"), charactersPath)

	if characters := store.AvailableCharacters(); len(characters) != 838 {
		t.Fatalf("AvailableCharacters() = %d, want 838", len(characters))
	}
	character, ok := store.Get("Arthur Morgan")
	if !ok || character["title"] != "Arthur Morgan" {
		t.Fatalf("Get must fall back to characters, got %#v (found=%v)", character, ok)
	}
	entries := store.ContextForProfile(map[string]any{}, DefaultContextTopics, DefaultContextMaxChars)
	if len(entries) != 1 {
		t.Fatalf("ContextForProfile = %#v, want the first character", entryTitles(entries))
	}
	if entries[0]["title"] != "Sadie Adler" {
		t.Errorf("fallback entry = %#v, want the first character in file order", entries[0]["title"])
	}
}

func TestStoreReturnsCopies(t *testing.T) {
	worldPath, charactersPath := realDataPaths(t)
	store := NewStore(worldPath, charactersPath)

	page, _ := store.Get("Valentine")
	originalURL := page["url"]
	page["url"] = "mutated"
	if again, _ := store.Get("Valentine"); again["url"] != originalURL {
		t.Error("Get must return a copy the caller cannot use to mutate the pack")
	}

	arthur, _ := store.Character("Arthur Morgan")
	infobox, _ := arthur["infobox"].(map[string]any)
	originalGender := infobox["gender"]
	infobox["gender"] = "mutated"
	if again, _ := store.Character("Arthur Morgan"); again["infobox"].(map[string]any)["gender"] != originalGender {
		t.Error("nested objects must be copied too")
	}
}

func TestStoreIsSafeForConcurrentUse(t *testing.T) {
	worldPath, charactersPath := realDataPaths(t)
	store := NewStore(worldPath, charactersPath)

	var wait sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if got := len(store.AvailableCharacters()); got != 838 {
				t.Errorf("AvailableCharacters() = %d, want 838", got)
			}
			if _, ok := store.Get("Valentine"); !ok {
				t.Error("Get(\"Valentine\") must succeed concurrently")
			}
			if entries := store.ContextForProfile(map[string]any{"region": "Ambarino"}, 2, 100); len(entries) == 0 {
				t.Error("ContextForProfile must succeed concurrently")
			}
		}()
	}
	wait.Wait()
}

// entryTitles lists the titles of context entries, in order.
func entryTitles(entries []map[string]any) []string {
	titles := make([]string, 0, len(entries))
	for _, entry := range entries {
		title, _ := entry["title"].(string)
		titles = append(titles, title)
	}
	return titles
}

// containsString reports whether values holds want.
func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// firstRunes returns at most n leading characters, for readable failures.
func firstRunes(value string, n int) string {
	runes := []rune(value)
	if len(runes) <= n {
		return value
	}
	return string(runes[:n]) + "..."
}
