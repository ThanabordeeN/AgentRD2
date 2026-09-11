package lore

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
)

// QuestStore serves dialogue context for quest NPCs from “*.json“ files in a
// directory.
//
// It ports “runtime/quests.py“: quests are loaded lazily, a missing or
// corrupt file is skipped, and every returned map is a copy so callers cannot
// mutate the pack. Quest NPCs are dialogue-only, so the context is deliberately
// small: what the NPC knows, what they must not spoil, and how they should
// speak.
type QuestStore struct {
	root string

	once   sync.Once
	quests map[string]map[string]any
	order  []string
}

// NewQuestStore returns a store reading quest files from root. An empty root
// selects DefaultQuestsDir. A relative root that exists below the project root
// is resolved against it, so the runtime finds data/quests from any working
// directory. Nothing is read until the first lookup.
func NewQuestStore(root string) *QuestStore {
	if root == "" {
		root = DefaultQuestsDir
	}
	return &QuestStore{root: resolvePath(root)}
}

// Root returns the directory the store reads quest files from.
func (q *QuestStore) Root() string {
	return q.root
}

// Get returns a copy of the quest with questID.
//
// The exact id wins; otherwise the lookup accepts a case-insensitive id or a
// file stem, matching “QuestStore.get“. The second result is false when no
// quest matches.
func (q *QuestStore) Get(questID string) (map[string]any, bool) {
	if questID == "" {
		return nil, false
	}
	q.ensureLoaded()
	quest, found := q.quests[questID]
	if !found {
		for _, key := range q.order {
			candidate := q.quests[key]
			if strings.EqualFold(key, questID) || fileStem(domain.StringFrom(candidate["source"])) == questID {
				quest, found = candidate, true
				break
			}
		}
	}
	if !found {
		return nil, false
	}
	return deepCopyMap(quest), true
}

// ContextFor returns the dialogue context for a quest, mirroring
// “QuestStore.context_for“.
//
// An unknown questID yields a neutral quest with the runtime's two standard
// dialogue guidelines rather than an error, because a quest NPC must still be
// able to talk when its pack is missing. When state is non-nil, its
// current_objective, next_objective, location and quest_state override the
// pack values unless they are empty. The description is trimmed to 2500
// characters.
func (q *QuestStore) ContextFor(questID string, state map[string]any) map[string]any {
	quest, found := q.Get(questID)
	if !found {
		title := any(questID)
		if questID == "" {
			title = "unknown quest"
		}
		quest = map[string]any{
			"quest_id":    questID,
			"title":       title,
			"description": "",
			"npc_role":    "",
			"dialogue_guidelines": []any{
				"Speak briefly and in character.",
				"Do not invent quest steps that were not provided.",
			},
			"forbidden_spoilers": []any{},
		}
	}

	context := map[string]any{
		"quest_id":            quest["quest_id"],
		"title":               quest["title"],
		"description":         pySliceRunes(orEmptyText(quest["description"]), defaultQuestMaxChars),
		"npc_role":            quest["npc_role"],
		"current_objective":   quest["current_objective"],
		"next_objective":      quest["next_objective"],
		"location":            quest["location"],
		"dialogue_guidelines": listFrom(quest["dialogue_guidelines"]),
		"known_facts":         listFrom(quest["known_facts"]),
		"forbidden_spoilers":  listFrom(quest["forbidden_spoilers"]),
		"sample_lines":        pySlice(listFrom(quest["sample_lines"]), 8),
	}
	for _, key := range []string{"current_objective", "next_objective", "location", "quest_state"} {
		if value, ok := state[key]; ok && presentStateValue(value) {
			context[key] = value
		}
	}
	return context
}

// ensureLoaded reads the quest directory once, on first use.
func (q *QuestStore) ensureLoaded() {
	q.once.Do(q.load)
}

// load reads every “*.json“ file in the root, in filename order, mirroring
// “sorted(self.root.glob("*.json"))“. Hidden files are skipped like the
// Python glob does, and unreadable or corrupt files are ignored.
func (q *QuestStore) load() {
	q.quests = map[string]map[string]any{}
	q.order = []string{}
	entries, err := os.ReadDir(q.root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(q.root, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		quest := decodeObject(data)
		if quest == nil {
			continue
		}
		questID := domain.StringFrom(quest["quest_id"])
		if questID == "" {
			questID = strings.TrimSuffix(name, ".json")
		}
		quest["quest_id"] = questID
		if _, ok := quest["source"]; !ok {
			quest["source"] = path
		}
		if _, exists := q.quests[questID]; !exists {
			q.order = append(q.order, questID)
		}
		q.quests[questID] = quest
	}
}

// listFrom copies a JSON array. Python's “list(value or [])“ also explodes
// strings and maps, but the packs only ever contain arrays here.
func listFrom(value any) []any {
	switch typed := value.(type) {
	case []any:
		result := make([]any, len(typed))
		copy(result, typed)
		return result
	case []string:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = item
		}
		return result
	default:
		return []any{}
	}
}

// presentStateValue mirrors Python's “value not in (None, "", [])“: only an
// absent, nil, empty-string or empty-list override is ignored.
func presentStateValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case string:
		return typed != ""
	case []any:
		return len(typed) != 0
	case []string:
		return len(typed) != 0
	default:
		return true
	}
}

// fileStem returns the file name without its extension, mirroring
// “pathlib.Path(...).stem“.
func fileStem(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
