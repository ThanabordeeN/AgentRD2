package tools

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/timeline"
)

// Shared fakes for the tool-layer tests.

// fakeDispatcher records every request and answers with a started result,
// mirroring the bridge stub used by the Python tests.
type fakeDispatcher struct {
	requests []domain.ActionRequest
	reply    func(domain.ActionRequest) domain.ToolResult
}

func (f *fakeDispatcher) Dispatch(request domain.ActionRequest) domain.ToolResult {
	f.requests = append(f.requests, request)
	if f.reply != nil {
		return f.reply(request)
	}
	return domain.StartedResult(request, nil)
}

// last returns the most recent request.
func (f *fakeDispatcher) last(t *testing.T) domain.ActionRequest {
	t.Helper()
	if len(f.requests) == 0 {
		t.Fatal("expected the dispatcher to receive a request")
	}
	return f.requests[len(f.requests)-1]
}

// fakeWorld mirrors state.WorldStore.GetMap.
type fakeWorld struct {
	states map[string]map[string]any
}

func (w fakeWorld) GetMap(npcID string) map[string]any {
	return w.states[npcID]
}

// fakeTimeline records the filters it is handed.
type fakeTimeline struct {
	events  []domain.Event
	err     error
	filters []timeline.GrabFilter
	npcs    []string
}

func (f *fakeTimeline) GrabTimeline(npcID string, filter timeline.GrabFilter) ([]domain.Event, error) {
	f.npcs = append(f.npcs, npcID)
	f.filters = append(f.filters, filter)
	if f.err != nil {
		return nil, f.err
	}
	return f.events, nil
}

// lookupCall records one LoreReader.Lookup invocation.
type lookupCall struct {
	query    string
	limit    int
	maxChars int
}

// profileCall records one LoreReader.ContextForProfile invocation.
type profileCall struct {
	profile  map[string]any
	limit    int
	maxChars int
}

// fakeLore mirrors the two WikiContextStore methods get_world_lore uses.
type fakeLore struct {
	byTopic  map[string][]map[string]any
	fallback []map[string]any
	lookups  []lookupCall
	profiles []profileCall
}

func (f *fakeLore) Lookup(query string, limit, maxChars int) []map[string]any {
	f.lookups = append(f.lookups, lookupCall{query: query, limit: limit, maxChars: maxChars})
	if pages, ok := f.byTopic[query]; ok {
		return pages
	}
	return nil
}

func (f *fakeLore) ContextForProfile(profile map[string]any, limit, maxChars int) []map[string]any {
	f.profiles = append(f.profiles, profileCall{profile: profile, limit: limit, maxChars: maxChars})
	return f.fallback
}

// dispatchCase is one tool invocation with its expected bridge payload.
// A nil wantArgs means the call must fail without reaching the dispatcher.
type dispatchCase struct {
	name     string
	tool     string
	args     map[string]any
	wantArgs map[string]any
}

// runDispatchCases executes table-driven defaulting/dispatch checks against the
// default registry and a recording dispatcher.
func runDispatchCases(t *testing.T, cases []dispatchCase) {
	t.Helper()
	registry := BuildDefaultRegistry()
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			dispatcher := &fakeDispatcher{}
			ctx := &Context{NPCID: "npc_test", Dispatcher: dispatcher}

			result := registry.Call(testCase.tool, ctx, testCase.args)

			if testCase.wantArgs == nil {
				if result.Status != domain.ActionFailed {
					t.Fatalf("expected a failed result, got status=%q detail=%v", result.Status, result.Detail)
				}
				if result.Reason == "" {
					t.Fatal("failed result must carry a reason")
				}
				if len(dispatcher.requests) != 0 {
					t.Fatalf("failed call must not reach the dispatcher, got %d requests", len(dispatcher.requests))
				}
				return
			}

			if result.Status == domain.ActionFailed {
				t.Fatalf("unexpected failure: %s", result.Reason)
			}
			if len(dispatcher.requests) != 1 {
				t.Fatalf("expected exactly one dispatched request, got %d", len(dispatcher.requests))
			}
			request := dispatcher.requests[0]
			if request.Tool != testCase.tool {
				t.Errorf("dispatched tool = %q, want %q", request.Tool, testCase.tool)
			}
			if request.NPCID != "npc_test" {
				t.Errorf("dispatched npc_id = %q, want %q", request.NPCID, "npc_test")
			}
			if request.RequestID == "" {
				t.Error("dispatched request_id must not be empty")
			}
			if got := domain.CleanMap(request.Arguments); !reflect.DeepEqual(got, domain.CleanMap(testCase.wantArgs)) {
				t.Errorf("dispatched arguments = %#v, want %#v", got, testCase.wantArgs)
			}
			if result.Tool != testCase.tool || result.RequestID != request.RequestID {
				t.Errorf("result must echo the dispatched request, got tool=%q request_id=%q", result.Tool, result.RequestID)
			}
		})
	}
}

// assertJSONEqual compares a Go value with a JSON literal after a
// marshal/unmarshal round trip, so numeric shapes (int vs float64) and map
// ordering do not matter.
func assertJSONEqual(t *testing.T, got any, wantJSON string) {
	t.Helper()
	gotBytes, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal got: %v", err)
	}
	var want any
	if err := json.Unmarshal([]byte(wantJSON), &want); err != nil {
		t.Fatalf("bad want JSON %q: %v", wantJSON, err)
	}
	wantBytes, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal want: %v", err)
	}
	if string(gotBytes) != string(wantBytes) {
		t.Fatalf("payload mismatch\n got: %s\nwant: %s", gotBytes, wantBytes)
	}
}
