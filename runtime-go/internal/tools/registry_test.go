package tools

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
)

func TestNewRegistryIsEmpty(t *testing.T) {
	registry := NewRegistry()
	if names := registry.Names(); len(names) != 0 {
		t.Fatalf("new registry holds %v, want no tools", names)
	}
	if described := registry.Describe(); len(described) != 0 {
		t.Fatalf("new registry describes %v, want nothing", described)
	}
	if _, ok := registry.Get("nope"); ok {
		t.Fatal("Get on an empty registry must report ok=false")
	}
}

func TestRegisterRejectsDuplicateNames(t *testing.T) {
	registry := NewRegistry()
	spec := Spec{Name: "dupe", Description: "first", Parameters: map[string]any{}}
	if err := registry.Register(spec); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	err := registry.Register(Spec{Name: "dupe", Description: "second"})
	if err == nil {
		t.Fatal("duplicate Register must fail")
	}
	if want := "tool already registered: dupe"; err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
	if spec, _ := registry.Get("dupe"); spec.Description != "first" {
		t.Fatalf("duplicate registration replaced the original spec: %q", spec.Description)
	}
}

func TestRegisterDefaultsCategoryToGeneral(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(Spec{Name: "bare"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	spec, ok := registry.Get("bare")
	if !ok {
		t.Fatal("registered tool is missing")
	}
	if spec.Category != "general" {
		t.Fatalf("category = %q, want %q", spec.Category, "general")
	}
}

func TestNamesAreSortedAndDescribeFollows(t *testing.T) {
	registry := NewRegistry()
	for _, name := range []string{"charlie", "alpha", "bravo"} {
		if err := registry.Register(Spec{Name: name, Description: "d", Parameters: map[string]any{}}); err != nil {
			t.Fatalf("Register(%q): %v", name, err)
		}
	}
	if got, want := registry.Names(), []string{"alpha", "bravo", "charlie"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
	described := registry.Describe()
	want := []map[string]any{
		{"name": "alpha", "description": "d", "parameters": map[string]any{}},
		{"name": "bravo", "description": "d", "parameters": map[string]any{}},
		{"name": "charlie", "description": "d", "parameters": map[string]any{}},
	}
	assertJSONEqual(t, described, mustJSONString(t, want))
}

func TestDescribeOnlyExposesModelFacingFields(t *testing.T) {
	described := BuildDefaultRegistry().Describe()
	if len(described) == 0 {
		t.Fatal("default registry is empty")
	}
	for _, entry := range described {
		keys := make([]string, 0, len(entry))
		for key := range entry {
			keys = append(keys, key)
		}
		if len(keys) != 3 {
			t.Fatalf("entry %v must expose exactly name/description/parameters", entry)
		}
		for _, key := range []string{"name", "description", "parameters"} {
			if _, ok := entry[key]; !ok {
				t.Fatalf("entry %v is missing %q", entry, key)
			}
		}
	}
}

func TestCallUnknownToolFails(t *testing.T) {
	dispatcher := &fakeDispatcher{}
	ctx := &Context{NPCID: "npc_1", Dispatcher: dispatcher}

	result := BuildDefaultRegistry().Call("does_not_exist", ctx, nil)

	if result.Status != domain.ActionFailed {
		t.Fatalf("status = %q, want failed", result.Status)
	}
	if !strings.Contains(result.Reason, "unknown agent tool: does_not_exist") {
		t.Fatalf("reason = %q, want it to mention the unknown tool", result.Reason)
	}
	if result.Tool != "does_not_exist" || result.RequestID == "" {
		t.Fatalf("failed result must still carry tool and request id, got %+v", result)
	}
	if len(dispatcher.requests) != 0 {
		t.Fatal("an unknown tool must not reach the dispatcher")
	}
}

func TestCallNilHandlerFails(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(Spec{Name: "broken", Category: "general"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	result := registry.Call("broken", &Context{}, nil)
	if result.Status != domain.ActionFailed {
		t.Fatalf("status = %q, want failed", result.Status)
	}
	if !strings.Contains(result.Reason, "no handler") {
		t.Fatalf("reason = %q, want it to mention the missing handler", result.Reason)
	}
}

func TestCallNilContextFailsWithoutPanicking(t *testing.T) {
	result := BuildDefaultRegistry().Call("jump", nil, nil)
	if result.Status != domain.ActionFailed {
		t.Fatalf("status = %q, want failed", result.Status)
	}
	if !strings.Contains(result.Reason, "context is nil") {
		t.Fatalf("reason = %q, want it to mention the nil context", result.Reason)
	}
}

func TestCallNilArgumentsBecomesEmptyMap(t *testing.T) {
	dispatcher := &fakeDispatcher{}
	ctx := &Context{NPCID: "npc_1", Dispatcher: dispatcher}

	result := BuildDefaultRegistry().Call("jump", ctx, nil)

	if result.Status == domain.ActionFailed {
		t.Fatalf("unexpected failure: %s", result.Reason)
	}
	request := dispatcher.last(t)
	if request.Arguments == nil {
		t.Fatal("dispatched arguments must be an empty map, never nil")
	}
	if len(request.Arguments) != 0 {
		t.Fatalf("dispatched arguments = %v, want empty", request.Arguments)
	}
}

// mustJSONString renders a Go value as JSON for the shared assertJSONEqual
// helper.
func mustJSONString(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(encoded)
}
