// Package lore serves the runtime's offline world-lore, character and quest
// context from local JSON packs.
//
// The runtime never scrapes the web during gameplay: the packs under data/ are
// refreshed out of band by scripts/fetch_wiki_context.py and
// scripts/fetch_character_context.py. Store ports “runtime/lore.py“
// (WikiContextStore) and QuestStore ports “runtime/quests.py“.
//
// Both stores load lazily on first use and tolerate missing or corrupt files
// by returning empty results instead of failing, so a partially installed
// runtime still runs.
package lore

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/project"
)

const (
	// DefaultWorldPath is the wiki pack NewStore loads when worldPath is
	// empty: meta plus the eight world pages.
	DefaultWorldPath = "data/wiki/rdr2_context.json"
	// DefaultCharactersPath is the character pack NewStore loads when
	// charactersPath is empty: meta plus 838 character entries.
	DefaultCharactersPath = "data/wiki/characters.json"
	// DefaultQuestsDir is the quest pack directory NewQuestStore uses when it
	// is given an empty root.
	DefaultQuestsDir = "data/quests"

	// DefaultLookupLimit and DefaultLookupMaxChars mirror lookup()'s keyword
	// defaults.
	DefaultLookupLimit    = 3
	DefaultLookupMaxChars = 1200

	// DefaultContextTopics and DefaultContextMaxChars mirror
	// context_for_profile()'s keyword defaults.
	DefaultContextTopics   = 5
	DefaultContextMaxChars = 1200

	// defaultQuestMaxChars mirrors context_for()'s max_chars keyword default.
	defaultQuestMaxChars = 2500
)

// Store serves offline wiki and character context.
//
// It is safe for concurrent use; the packs are read once, on first access.
type Store struct {
	worldPath      string
	charactersPath string

	once sync.Once

	meta            map[string]any
	pages           map[string]map[string]any
	characters      map[string]map[string]any
	pageOrder       []string
	characterOrder  []string
	pageLookup      map[string]string
	characterLookup map[string]string
}

// NewStore returns a store reading the given pack paths.
//
// An empty path selects the corresponding project default. Relative paths
// that exist below the project root are resolved against it, so the runtime
// finds data/wiki from any working directory. Nothing is read until the first
// lookup.
func NewStore(worldPath, charactersPath string) *Store {
	if worldPath == "" {
		worldPath = DefaultWorldPath
	}
	if charactersPath == "" {
		charactersPath = DefaultCharactersPath
	}
	return &Store{
		worldPath:      resolvePath(worldPath),
		charactersPath: resolvePath(charactersPath),
	}
}

// AvailableTopics returns the sorted world-page titles the pack provides.
func (s *Store) AvailableTopics() []string {
	s.ensureLoaded()
	topics := make([]string, len(s.pageOrder))
	copy(topics, s.pageOrder)
	sort.Strings(topics)
	return topics
}

// AvailableCharacters returns the sorted character names the pack provides.
func (s *Store) AvailableCharacters() []string {
	s.ensureLoaded()
	names := make([]string, len(s.characterOrder))
	copy(names, s.characterOrder)
	sort.Strings(names)
	return names
}

// Meta returns a copy of the pack metadata, such as the source attribution
// the licence requires redistributors to preserve.
func (s *Store) Meta() map[string]any {
	s.ensureLoaded()
	return deepCopyMap(s.meta)
}

// Get returns the page or character named by topic.
//
// The match is case-insensitive: an exact title wins, then the first title
// containing topic in file order. A character entry wins over a world page
// when both share the title, mirroring WikiContextStore.get.
func (s *Store) Get(topic string) (map[string]any, bool) {
	if topic == "" {
		return nil, false
	}
	s.ensureLoaded()
	needle := strings.ToLower(pyStrip(topic))
	key, found := s.pageLookup[needle]
	if !found {
		key, found = s.characterLookup[needle]
	}
	if !found {
		for _, titles := range [][]string{s.pageOrder, s.characterOrder} {
			for _, title := range titles {
				if strings.Contains(strings.ToLower(title), needle) {
					key, found = title, true
					break
				}
			}
			if found {
				break
			}
		}
	}
	if !found {
		return nil, false
	}
	if entry, ok := s.characters[key]; ok {
		return deepCopyMap(entry), true
	}
	if entry, ok := s.pages[key]; ok {
		return deepCopyMap(entry), true
	}
	return nil, false
}

// Character returns the character entry named by name, matching exactly first
// and then by substring, case-insensitively.
func (s *Store) Character(name string) (map[string]any, bool) {
	if name == "" {
		return nil, false
	}
	s.ensureLoaded()
	needle := strings.ToLower(pyStrip(name))
	key, found := s.characterLookup[needle]
	if !found {
		for _, title := range s.characterOrder {
			if strings.Contains(strings.ToLower(title), needle) {
				key, found = title, true
				break
			}
		}
	}
	if !found {
		return nil, false
	}
	return deepCopyMap(s.characters[key]), true
}

// Lookup returns up to limit entries whose title contains query, or failing
// that whose title or summary mentions it. Exact title matches sort first,
// then titles alphabetically, mirroring WikiContextStore.lookup. A summary is
// trimmed to maxChars.
func (s *Store) Lookup(query string, limit int, maxChars int) []map[string]any {
	s.ensureLoaded()
	needle := strings.ToLower(pyStrip(query))
	if needle == "" {
		return []map[string]any{}
	}

	type scored struct {
		rank  int
		title string
		entry map[string]any
	}
	matches := []scored{}
	collect := func(order []string, entries map[string]map[string]any) {
		for _, title := range order {
			entry := entries[title]
			summary, ok := entry["summary"]
			text := ""
			if ok {
				text = domain.StringFrom(summary)
			}
			switch {
			case strings.Contains(strings.ToLower(title), needle):
				matches = append(matches, scored{rank: 0, title: title, entry: entry})
			case strings.Contains(strings.ToLower(title+" "+text), needle):
				matches = append(matches, scored{rank: 1, title: title, entry: entry})
			}
		}
	}
	collect(s.characterOrder, s.characters)
	collect(s.pageOrder, s.pages)
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].rank != matches[j].rank {
			return matches[i].rank < matches[j].rank
		}
		return matches[i].title < matches[j].title
	})

	results := []map[string]any{}
	for _, match := range pySlice(matches, limit) {
		results = append(results, trimSummary(match.entry, maxChars))
	}
	return results
}

// ContextForProfile returns the lore entries relevant to an NPC profile.
//
// Topics are taken from the profile's wiki_character, character and name
// fields, then wiki_topics (a string or a list of strings), then region. When
// the profile names none, the game page is used. Duplicate titles are
// dropped, at most limit entries are returned, and each summary is trimmed to
// maxChars. When nothing matches, the first available page (or character) is
// returned instead, mirroring context_for_profile.
func (s *Store) ContextForProfile(profile map[string]any, limit int, maxChars int) []map[string]any {
	return s.ContextForProfileWithWorld(profile, nil, limit, maxChars)
}

// ContextForProfileWithWorld is ContextForProfile plus the in-game region from
// a world state snapshot, mirroring context_for_profile's world_state keyword.
func (s *Store) ContextForProfileWithWorld(profile, worldState map[string]any, limit int, maxChars int) []map[string]any {
	s.ensureLoaded()
	topics := []string{}
	for _, key := range []string{"wiki_character", "character", "name"} {
		if value := pyStrip(orEmptyText(profile[key])); value != "" {
			topics = append(topics, value)
		}
	}
	switch raw := profile["wiki_topics"].(type) {
	case string:
		if raw != "" {
			topics = append(topics, raw)
		}
	case []any:
		for _, topic := range raw {
			topics = append(topics, domain.StringFrom(topic))
		}
	case []string:
		topics = append(topics, raw...)
	}
	if region := pyStrip(orEmptyText(profile["region"])); region != "" {
		topics = append(topics, region)
	}
	if location, ok := worldState["location"].(map[string]any); ok {
		if region := orEmptyText(location["region"]); region != "" {
			topics = append(topics, region)
		}
	}
	if len(topics) == 0 {
		topics = []string{"Red Dead Redemption 2"}
	}

	results := []map[string]any{}
	seen := map[string]bool{}
	for _, topic := range topics {
		if len(results) >= limit {
			break
		}
		page, ok := s.Get(topic)
		if !ok {
			continue
		}
		title := domain.StringFrom(page["title"])
		if seen[title] {
			continue
		}
		seen[title] = true
		results = append(results, trimSummary(page, maxChars))
	}
	if len(results) == 0 && (len(s.pageOrder) > 0 || len(s.characterOrder) > 0) {
		first := s.pages[s.pageOrder[0]]
		if first == nil {
			first = s.characters[s.characterOrder[0]]
		}
		results = append(results, trimSummary(first, maxChars))
	}
	return results
}

// ensureLoaded reads the packs once, on first use.
func (s *Store) ensureLoaded() {
	s.once.Do(s.load)
}

// load fills the internal maps. Every failure leaves the store empty rather
// than propagating: a corrupt pack must not stop an NPC from speaking.
func (s *Store) load() {
	s.meta = map[string]any{}
	s.pages = map[string]map[string]any{}
	s.characters = map[string]map[string]any{}
	s.loadWorldPack()
	s.loadCharacterPack()

	s.pageLookup = make(map[string]string, len(s.pageOrder))
	for _, title := range s.pageOrder {
		s.pageLookup[strings.ToLower(title)] = title
	}
	s.characterLookup = make(map[string]string, len(s.characterOrder))
	for _, title := range s.characterOrder {
		s.characterLookup[strings.ToLower(title)] = title
	}
}

// loadWorldPack reads the wiki pack: “meta“ plus “pages“.
func (s *Store) loadWorldPack() {
	data, err := os.ReadFile(s.worldPath)
	if err != nil {
		return
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(data, &document); err != nil {
		return
	}
	if meta := decodeObject(document["meta"]); meta != nil {
		s.meta = meta
	}
	for _, object := range decodeObjectEntries(document["pages"]) {
		page := decodeObject(object.value)
		if page == nil {
			continue
		}
		if _, ok := page["title"]; !ok {
			page["title"] = object.key
		}
		if _, ok := page["source"]; !ok {
			page["source"] = s.defaultSource()
		}
		s.pages[object.key] = page
		s.pageOrder = append(s.pageOrder, object.key)
	}
}

// loadCharacterPack reads the character pack: “meta“ plus “characters“.
// Character metadata is merged *under* the wiki metadata, mirroring
// “{**char_meta, **self.meta}“.
func (s *Store) loadCharacterPack() {
	data, err := os.ReadFile(s.charactersPath)
	if err != nil {
		return
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(data, &document); err != nil {
		return
	}
	if characterMeta := decodeObject(document["meta"]); len(characterMeta) > 0 {
		merged := make(map[string]any, len(characterMeta)+len(s.meta))
		for key, value := range characterMeta {
			merged[key] = value
		}
		for key, value := range s.meta {
			merged[key] = value
		}
		s.meta = merged
	}
	for _, object := range decodeObjectEntries(document["characters"]) {
		character := decodeObject(object.value)
		if character == nil {
			continue
		}
		if _, ok := character["title"]; !ok {
			character["title"] = object.key
		}
		if _, ok := character["source"]; !ok {
			character["source"] = s.defaultSource()
		}
		if _, ok := character["kind"]; !ok {
			character["kind"] = "character"
		}
		s.characters[object.key] = character
		s.characterOrder = append(s.characterOrder, object.key)
	}
}

// defaultSource mirrors “self.meta.get("source", "Red Dead Wiki")“.
func (s *Store) defaultSource() any {
	if value, ok := s.meta["source"]; ok {
		return value
	}
	return "Red Dead Wiki"
}

// objectEntry is one key/value pair of a JSON object.
type objectEntry struct {
	key   string
	value json.RawMessage
}

// decodeObject decodes a JSON object. It returns nil for JSON null, for any
// non-object value and for invalid JSON, and an empty (non-nil) map for {}.
func decodeObject(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil
	}
	return object
}

// decodeObjectEntries decodes a JSON object into its pairs in file order.
//
// Go maps do not preserve order, and the substring fallback in Get and
// Character scans titles in file order exactly like the Python dicts they
// mirror.
func decodeObjectEntries(raw json.RawMessage) []objectEntry {
	if len(raw) == 0 {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return nil
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return nil
	}
	entries := []objectEntry{}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			break
		}
		key, ok := keyToken.(string)
		if !ok {
			break
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			break
		}
		entries = append(entries, objectEntry{key: key, value: value})
	}
	return entries
}

// trimSummary mirrors “_trim“: the summary is stringified, truncated to
// maxChars characters and stripped.
func trimSummary(page map[string]any, maxChars int) map[string]any {
	entry := deepCopyMap(page)
	if entry == nil {
		entry = map[string]any{}
	}
	entry["summary"] = pyStrip(pySliceRunes(orEmptyText(entry["summary"]), maxChars))
	return entry
}

// orEmptyText approximates Python's “str(value or "")“ for the JSON values
// the packs contain.
func orEmptyText(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case bool:
		if !typed {
			return ""
		}
	case float64:
		if typed == 0 {
			return ""
		}
	case int:
		if typed == 0 {
			return ""
		}
	case []any:
		if len(typed) == 0 {
			return ""
		}
	case []string:
		if len(typed) == 0 {
			return ""
		}
	case map[string]any:
		if len(typed) == 0 {
			return ""
		}
	}
	return domain.StringFrom(value)
}

// pyStrip trims the characters Python's “str.strip“ removes.
func pyStrip(value string) string {
	return strings.TrimFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)
	})
}

// pySliceRunes mirrors Python's “value[:limit]“ on a string: truncation
// counts characters, and a negative limit drops characters from the end.
func pySliceRunes(value string, limit int) string {
	return string(pySlice([]rune(value), limit))
}

// pySlice mirrors Python's “items[:limit]“, including negative limits. It
// always returns a fresh slice.
func pySlice[T any](items []T, limit int) []T {
	if limit < 0 {
		limit += len(items)
		if limit < 0 {
			limit = 0
		}
	}
	if limit > len(items) {
		limit = len(items)
	}
	clipped := make([]T, limit)
	copy(clipped, items[:limit])
	return clipped
}

// deepCopyMap copies a decoded JSON object so callers cannot mutate the loaded
// packs through the maps they are handed.
func deepCopyMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = deepCopyValue(value)
	}
	return result
}

// deepCopyValue copies the containers JSON decoding can produce; scalars are
// immutable and are returned as-is.
func deepCopyValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return deepCopyMap(typed)
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = deepCopyValue(item)
		}
		return result
	case []string:
		result := make([]string, len(typed))
		copy(result, typed)
		return result
	default:
		return value
	}
}

// resolvePath prefers a project-root pack when the relative path names one,
// so the runtime finds data/ from any working directory, and falls back to the
// working directory for package-relative paths (such as test fixtures) that
// do not exist under the project root.
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
