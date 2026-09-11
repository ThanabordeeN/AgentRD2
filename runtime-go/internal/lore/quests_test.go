package lore

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/project"
)

func TestQuestStoreLoadsRepositoryQuests(t *testing.T) {
	root := project.Resolve(DefaultQuestsDir)
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("the repository quest directory is required: %v", err)
	}
	store := NewQuestStore(root)

	hunting, ok := store.Get("quest_hunting_supplies")
	if !ok {
		t.Fatal("Get(\"quest_hunting_supplies\") must succeed")
	}
	if hunting["title"] != "Hunting Supplies" {
		t.Errorf("title = %#v", hunting["title"])
	}
	if source, _ := hunting["source"].(string); !strings.HasSuffix(source, "quest_hunting_supplies.json") {
		t.Errorf("source = %#v, want the pack path", hunting["source"])
	}
	if _, ok := store.Get("quest_valentine_livestock"); !ok {
		t.Error("Get(\"quest_valentine_livestock\") must succeed")
	}
	if _, ok := store.Get(""); ok {
		t.Error("Get(\"\") must not match")
	}
	if _, ok := store.Get("quest_does_not_exist"); ok {
		t.Error("an unknown quest id must not match")
	}
}

func TestQuestStoreGetAcceptsCaseInsensitiveIDsAndStems(t *testing.T) {
	root := project.Resolve(DefaultQuestsDir)
	store := NewQuestStore(root)

	for _, questID := range []string{"quest_hunting_supplies", "QUEST_HUNTING_SUPPLIES", "Quest_Hunting_Supplies"} {
		quest, ok := store.Get(questID)
		if !ok {
			t.Fatalf("Get(%q) must succeed", questID)
		}
		if quest["quest_id"] != "quest_hunting_supplies" {
			t.Errorf("Get(%q) quest_id = %#v", questID, quest["quest_id"])
		}
	}

	// A file whose stem differs from the quest_id inside it is still found by
	// the stem, mirroring ``Path(value["source"]).stem == quest_id``.
	dir := t.TempDir()
	writeQuestFile(t, dir, "stem_only.json", `{"quest_id": "internal-id", "title": "Stem Only"}`)
	stemStore := NewQuestStore(dir)
	quest, ok := stemStore.Get("stem_only")
	if !ok {
		t.Fatal("Get must accept a file stem")
	}
	if quest["quest_id"] != "internal-id" {
		t.Errorf("quest_id = %#v, want the id from the file", quest["quest_id"])
	}
	if _, ok := stemStore.Get("internal-id"); !ok {
		t.Error("Get must still accept the declared quest id")
	}
}

func TestQuestStoreEmptyRootUsesRepositoryDirectory(t *testing.T) {
	store := NewQuestStore("")
	if want := project.Resolve(DefaultQuestsDir); store.Root() != want {
		t.Fatalf("Root() = %q, want %q", store.Root(), want)
	}
	if _, ok := store.Get("quest_hunting_supplies"); !ok {
		t.Fatal("the default root must serve the repository quests")
	}
}

func TestQuestStoreContextForRealQuest(t *testing.T) {
	store := NewQuestStore(project.Resolve(DefaultQuestsDir))
	context := store.ContextFor("quest_hunting_supplies", nil)

	if context["quest_id"] != "quest_hunting_supplies" || context["title"] != "Hunting Supplies" {
		t.Errorf("identity = %#v / %#v", context["quest_id"], context["title"])
	}
	if description, _ := context["description"].(string); !strings.Contains(description, "hunting supplies") {
		t.Errorf("description = %q", description)
	}
	if role, _ := context["npc_role"].(string); !strings.Contains(role, "experienced hunter") {
		t.Errorf("npc_role = %q", role)
	}
	if context["location"] != "Woodland north of Valentine, New Hanover" {
		t.Errorf("location = %#v", context["location"])
	}
	if objective, _ := context["current_objective"].(string); objective == "" {
		t.Error("current_objective must be present")
	}
	if context["next_objective"] != "The player should bring back three perfect pelts." {
		t.Errorf("next_objective = %#v", context["next_objective"])
	}
	for key, want := range map[string]int{
		"dialogue_guidelines": 4,
		"known_facts":         3,
		"forbidden_spoilers":  2,
		"sample_lines":        3,
	} {
		items, ok := context[key].([]any)
		if !ok || len(items) != want {
			t.Errorf("%s = %#v, want %d entries", key, context[key], want)
		}
	}
}

func TestQuestStoreContextForRuntimeStateOverrides(t *testing.T) {
	store := NewQuestStore(project.Resolve(DefaultQuestsDir))
	state := map[string]any{
		"current_objective": "Drive the cattle to the pens before dark.",
		"next_objective":    "",
		"location":          nil,
		"quest_state":       map[string]any{"phase": float64(2)},
	}
	context := store.ContextFor("quest_valentine_livestock", state)

	if context["current_objective"] != state["current_objective"] {
		t.Errorf("current_objective = %#v, want the runtime override", context["current_objective"])
	}
	if context["next_objective"] != "Speak with the auction clerk at the pens." {
		t.Errorf("next_objective = %#v, want the pack value (empty override ignored)", context["next_objective"])
	}
	if context["location"] != "Valentine stockyards, New Hanover" {
		t.Errorf("location = %#v, want the pack value (nil override ignored)", context["location"])
	}
	if !reflect.DeepEqual(context["quest_state"], map[string]any{"phase": float64(2)}) {
		t.Errorf("quest_state = %#v", context["quest_state"])
	}
}

func TestQuestStoreContextForUnknownQuestFallsBack(t *testing.T) {
	store := NewQuestStore(project.Resolve(DefaultQuestsDir))

	context := store.ContextFor("quest_unknown", nil)
	if context["quest_id"] != "quest_unknown" || context["title"] != "quest_unknown" {
		t.Errorf("fallback identity = %#v / %#v", context["quest_id"], context["title"])
	}
	if context["description"] != "" || context["npc_role"] != "" {
		t.Errorf("fallback text = %#v / %#v, want empty strings", context["description"], context["npc_role"])
	}
	if context["current_objective"] != nil || context["next_objective"] != nil || context["location"] != nil {
		t.Errorf("fallback objectives = %#v / %#v / %#v, want nil",
			context["current_objective"], context["next_objective"], context["location"])
	}
	guidelines, ok := context["dialogue_guidelines"].([]any)
	if !ok || len(guidelines) != 2 {
		t.Fatalf("dialogue_guidelines = %#v, want the two defaults", context["dialogue_guidelines"])
	}
	if guidelines[0] != "Speak briefly and in character." {
		t.Errorf("dialogue_guidelines[0] = %#v", guidelines[0])
	}
	for _, key := range []string{"known_facts", "forbidden_spoilers", "sample_lines"} {
		items, ok := context[key].([]any)
		if !ok || len(items) != 0 {
			t.Errorf("%s = %#v, want an empty list", key, context[key])
		}
	}

	blank := store.ContextFor("", nil)
	if blank["title"] != "unknown quest" || blank["quest_id"] != "" {
		t.Errorf("empty quest id fell back to %#v / %#v", blank["quest_id"], blank["title"])
	}
}

func TestQuestStoreReturnsCopies(t *testing.T) {
	store := NewQuestStore(project.Resolve(DefaultQuestsDir))

	first, ok := store.Get("quest_hunting_supplies")
	if !ok {
		t.Fatal("the hunting quest must load")
	}
	first["title"] = "mutated"
	first["known_facts"].([]any)[0] = "mutated"

	second, ok := store.Get("quest_hunting_supplies")
	if !ok {
		t.Fatal("the hunting quest must still load")
	}
	if second["title"] != "Hunting Supplies" {
		t.Errorf("title = %#v, want the stored value", second["title"])
	}
	if facts := second["known_facts"].([]any); facts[0] == "mutated" {
		t.Error("nested lists must be copied too")
	}
}

func TestQuestStoreToleratesMissingDirectoryAndCorruptFiles(t *testing.T) {
	missing := NewQuestStore(filepath.Join(t.TempDir(), "absent"))
	if _, ok := missing.Get("quest_hunting_supplies"); ok {
		t.Error("a missing directory must yield no quests")
	}
	fallback := missing.ContextFor("quest_any", nil)
	if fallback["title"] != "quest_any" {
		t.Errorf("ContextFor must still answer for a missing pack, got %#v", fallback["title"])
	}

	dir := t.TempDir()
	writeQuestFile(t, dir, "quest_good.json", `{"quest_id": "quest_good", "title": "Good"}`)
	writeQuestFile(t, dir, "quest_bad.json", "{not json")
	writeQuestFile(t, dir, "quest_wrong_shape.json", `["not", "an", "object"]`)
	store := NewQuestStore(dir)
	if _, ok := store.Get("quest_good"); !ok {
		t.Error("the valid quest must load")
	}
	if _, ok := store.Get("quest_bad"); ok {
		t.Error("a corrupt file must be skipped")
	}
	if _, ok := store.Get("quest_wrong_shape"); ok {
		t.Error("a file that is not a JSON object must be skipped")
	}
}

func TestQuestStoreUsesFileStemAndSkipsHiddenFiles(t *testing.T) {
	dir := t.TempDir()
	writeQuestFile(t, dir, "no_id.json", `{"title": "No Id"}`)
	writeQuestFile(t, dir, ".hidden.json", `{"quest_id": "hidden", "title": "Hidden"}`)
	store := NewQuestStore(dir)

	quest, ok := store.Get("no_id")
	if !ok {
		t.Fatal("a quest without an id must fall back to the file stem")
	}
	if quest["quest_id"] != "no_id" || quest["title"] != "No Id" {
		t.Errorf("quest = %#v", quest)
	}
	if _, ok := store.Get("hidden"); ok {
		t.Error("hidden files must be skipped, like pathlib's glob")
	}
}

// writeQuestFile writes a quest pack file into dir.
func writeQuestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
