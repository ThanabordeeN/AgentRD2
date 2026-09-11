package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// toolCategories is the category every tool in the Python ``ToolSpec`` carries.
// ``describe()`` does not expose categories, so they are asserted separately.
var toolCategories = map[string]string{
	"get_world_state":    "perception",
	"grab_timeline":      "perception",
	"get_world_lore":     "perception",
	"wander":             "movement",
	"go_to":              "movement",
	"follow":             "movement",
	"stop":               "movement",
	"look_at":            "attention",
	"face":               "attention",
	"clear_attention":    "attention",
	"say":                "speech",
	"gesture":            "speech",
	"think":              "thinking",
	"investigate":        "reaction",
	"flee_from":          "reaction",
	"wait":               "reaction",
	"react":              "reaction",
	"hands_up":           "reaction",
	"cower":              "reaction",
	"duck":               "reaction",
	"jump":               "reaction",
	"walk_away":          "reaction",
	"mount":              "interaction",
	"dismount":           "interaction",
	"item_interaction":   "interaction",
	"animal_interaction": "interaction",
	"horse_action":       "interaction",
	"aim_at":             "combat",
	"shoot_at":           "combat",
	"attack":             "combat",
	"take_cover":         "combat",
}

// TestCatalogMatchesPythonGolden is the parity gate: the Go catalog must equal
// the output of
//
//	python3 -c "import json; from runtime.tools import build_default_registry as b; \
//	            print(json.dumps(b().describe(), indent=1, sort_keys=True))"
//
// which is committed at testdata/tool_catalog.json.
func TestCatalogMatchesPythonGolden(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "tool_catalog.json"))
	if err != nil {
		t.Fatalf("read golden catalog: %v", err)
	}
	var want []map[string]any
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("decode golden catalog: %v", err)
	}

	gotJSON, err := json.Marshal(BuildDefaultRegistry().Describe())
	if err != nil {
		t.Fatalf("marshal go catalog: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal(gotJSON, &got); err != nil {
		t.Fatalf("decode go catalog: %v", err)
	}

	if len(want) != 31 {
		t.Fatalf("golden catalog holds %d tools, expected 31", len(want))
	}
	if len(got) != len(want) {
		t.Fatalf("tool count = %d, want %d", len(got), len(want))
	}
	if !reflect.DeepEqual(got, want) {
		reportCatalogDiff(t, got, want)
	}
}

// TestCatalogToolCount pins the number of tools exposed by the default
// registry.
func TestCatalogToolCount(t *testing.T) {
	if got := len(BuildDefaultRegistry().Names()); got != 31 {
		t.Fatalf("tool count = %d, want 31", got)
	}
}

// TestCatalogCategories asserts each tool is filed under the Python category.
func TestCatalogCategories(t *testing.T) {
	registry := BuildDefaultRegistry()
	names := registry.Names()
	if len(names) != len(toolCategories) {
		t.Fatalf("tool count = %d, category table holds %d", len(names), len(toolCategories))
	}
	for _, name := range names {
		spec, ok := registry.Get(name)
		if !ok {
			t.Fatalf("registry lost tool %q", name)
		}
		want, known := toolCategories[name]
		if !known {
			t.Errorf("tool %q is not in the category table", name)
			continue
		}
		if spec.Category != want {
			t.Errorf("tool %q category = %q, want %q", name, spec.Category, want)
		}
	}
	for name := range toolCategories {
		if _, ok := registry.Get(name); !ok {
			t.Errorf("category table lists missing tool %q", name)
		}
	}
}

// reportCatalogDiff prints the first differing tool in a readable form.
func reportCatalogDiff(t *testing.T, got, want []map[string]any) {
	t.Helper()
	for i := range got {
		if i >= len(want) || !reflect.DeepEqual(got[i], want[i]) {
			gotJSON, _ := json.MarshalIndent(got[i], "", " ")
			wantJSON := []byte("<missing>")
			if i < len(want) {
				wantJSON, _ = json.MarshalIndent(want[i], "", " ")
			}
			t.Fatalf("catalog entry %d differs\n got: %s\nwant: %s", i, gotJSON, wantJSON)
		}
	}
	t.Fatalf("catalog holds %d entries, want %d", len(got), len(want))
}
