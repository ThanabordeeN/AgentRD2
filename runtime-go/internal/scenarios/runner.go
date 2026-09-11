// Package scenarios replays the shared scenario JSON files against the Go
// runtime. It is a port of the Python ``scenario_runner.py`` and exists so the
// two implementations can be held to the same behavioural contract: both read
// the same files from ``scenarios/`` and assert the same events.
package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/agent"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/backend"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/config"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/timeline"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/tools"
)

// Result is the outcome of one scenario file.
type Result struct {
	Name       string         `json:"name"`
	Path       string         `json:"path"`
	Passed     bool           `json:"passed"`
	Failures   []string       `json:"failures"`
	EventNames []string       `json:"event_names"`
	FinalState map[string]any `json:"final_state"`
}

// Options controls how scenarios are executed.
type Options struct {
	// Settings is the base configuration; TimelinesDir is replaced with a
	// temporary directory per scenario.
	Settings *config.Settings
	// MakeBackend builds the backend for a scenario. The default builds the
	// deterministic rule backend from the scenario's "backend" block.
	MakeBackend func(scenario map[string]any) (backend.Backend, error)
	// Verbose prints each message as it is handled.
	Verbose bool
}

// Discover returns the scenario JSON files in dir, sorted by name.
func Discover(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		paths = append(paths, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(paths)
	return paths, nil
}

// RunDir runs every scenario in dir.
func RunDir(dir string, opts Options) ([]Result, error) {
	paths, err := Discover(dir)
	if err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(paths))
	for _, path := range paths {
		results = append(results, RunFile(path, opts))
	}
	return results, nil
}

// countingBackend records how many decisions a backend produced, so scenarios
// can assert on model usage without every backend having to expose a counter.
type countingBackend struct {
	inner backend.Backend
	calls int64
}

func (c *countingBackend) Name() string                { return c.inner.Name() }
func (c *countingBackend) SupportsWaitGestures() bool  { return c.inner.SupportsWaitGestures() }
func (c *countingBackend) Calls() int                 { return int(atomic.LoadInt64(&c.calls)) }

func (c *countingBackend) Decide(ctx context.Context, agentCtx *domain.AgentContext) (*domain.AgentDecision, error) {
	atomic.AddInt64(&c.calls, 1)
	return c.inner.Decide(ctx, agentCtx)
}

// RunFile executes a single scenario and evaluates its expectations.
func RunFile(path string, opts Options) Result {
	result := Result{Path: path, Failures: []string{}, FinalState: map[string]any{}}

	raw, err := os.ReadFile(path)
	if err != nil {
		result.Name = filepath.Base(path)
		result.Failures = append(result.Failures, fmt.Sprintf("read error: %v", err))
		return result
	}
	var scenario map[string]any
	if err := json.Unmarshal(raw, &scenario); err != nil {
		result.Name = filepath.Base(path)
		result.Failures = append(result.Failures, fmt.Sprintf("invalid JSON: %v", err))
		return result
	}

	name := domain.StringFrom(scenario["name"])
	if name == "" {
		name = filepath.Base(path)
	}
	result.Name = name

	npcID := domain.StringFrom(scenario["npc_id"])
	if npcID == "" {
		result.Failures = append(result.Failures, "scenario is missing npc_id")
		return result
	}

	ctx := context.Background()
	tmpDir, err := os.MkdirTemp("", "rdr2-scenario-")
	if err != nil {
		result.Failures = append(result.Failures, fmt.Sprintf("temp dir: %v", err))
		return result
	}
	defer os.RemoveAll(tmpDir)

	settings := config.DefaultSettings()
	if opts.Settings != nil {
		settings = *opts.Settings
	}
	settings.TimelinesDir = tmpDir

	makeBackend := opts.MakeBackend
	if makeBackend == nil {
		makeBackend = defaultBackend
	}
	inner, err := makeBackend(scenario)
	if err != nil {
		result.Failures = append(result.Failures, fmt.Sprintf("backend: %v", err))
		return result
	}
	counter := &countingBackend{inner: inner}

	runtime, err := agent.NewRuntime(agent.Options{Settings: &settings, Backend: counter})
	if err != nil {
		result.Failures = append(result.Failures, fmt.Sprintf("runtime: %v", err))
		return result
	}

	saved := map[string]any{}

	// Seed timeline facts before the scenario starts.
	for _, rawSeed := range domain.MapsFrom(scenario["seed_events"]) {
		seedNPC := domain.StringFrom(rawSeed["npc_id"])
		if seedNPC == "" {
			seedNPC = npcID
		}
		options := timeline.AppendOptions{
			Data:       domain.MapFrom(rawSeed["data"]),
			Entities:   domain.StringsFrom(rawSeed["entities"]),
			Tags:       domain.StringsFrom(rawSeed["tags"]),
			Summary:    optionalString(rawSeed["summary"]),
			Importance: domain.FloatPtrFrom(rawSeed["importance"]),
		}
		if _, err := runtime.Timeline().AppendEvent(
			seedNPC, domain.StringFrom(rawSeed["event_name"]), options,
		); err != nil {
			result.Failures = append(result.Failures, fmt.Sprintf("seed event: %v", err))
		}
	}

	for _, step := range domain.MapsFrom(scenario["steps"]) {
		switch {
		case step["message"] != nil:
			message := domain.MapFrom(step["message"])
			if _, err := runtime.HandleMessage(ctx, message); err != nil {
				result.Failures = append(result.Failures, fmt.Sprintf("handle_message: %v", err))
			}
		case step["tool_call"] != nil:
			call := domain.MapFrom(step["tool_call"])
			toolName := domain.StringFrom(call["tool"])
			arguments := domain.MapFrom(call["arguments"])
			toolCtx := &tools.Context{
				NPCID:      npcID,
				World:      runtime.World(),
				Timeline:   runtime.Timeline(),
				Lore:       runtime.Lore(),
				Profile:    runtime.Profiles().Get(npcID),
				Dispatcher: runtime.Dispatcher(),
			}
			toolResult := runtime.Registry().Call(toolName, toolCtx, arguments)
			saveAs := domain.StringFrom(call["save_as"])
			if saveAs == "" {
				saveAs = toolName
			}
			saved[saveAs] = toolResult.ToMap()
		case step["pending_action_result"] != nil:
			pending := domain.MapFrom(step["pending_action_result"])
			toolName := domain.StringFrom(pending["tool"])
			request, ok := runtime.Dispatcher().FindPending(toolName)
			if !ok {
				result.Failures = append(result.Failures,
					fmt.Sprintf("pending action not found for tool=%q", toolName))
				continue
			}
			message := map[string]any{
				"type":       "action_result",
				"npc_id":     npcID,
				"request_id": request.RequestID,
				"tool":       request.Tool,
				"status":     firstNonEmpty(domain.StringFrom(pending["status"]), "failed"),
			}
			for key, value := range pending {
				if key == "tool" {
					continue
				}
				message[key] = value
			}
			if _, err := runtime.HandleMessage(ctx, message); err != nil {
				result.Failures = append(result.Failures, fmt.Sprintf("handle_message: %v", err))
			}
		case step["release_all"] != nil:
			reason := domain.StringFrom(domain.MapFrom(step["release_all"])["reason"])
			if reason == "" {
				reason = "scenario_release"
			}
			if err := runtime.ReleaseAll(ctx, reason); err != nil {
				result.Failures = append(result.Failures, fmt.Sprintf("release_all: %v", err))
			}
		default:
			result.Failures = append(result.Failures, fmt.Sprintf("unknown scenario step: %v", step))
		}
	}

	events, err := runtime.Timeline().AllEvents(npcID)
	if err != nil {
		result.Failures = append(result.Failures, fmt.Sprintf("read timeline: %v", err))
	}
	eventNames := make([]string, 0, len(events))
	for _, event := range events {
		eventNames = append(eventNames, event.EventName)
	}
	result.EventNames = eventNames

	state := runtime.State(npcID)
	ownership := string(runtime.Ownership().State(npcID))
	lastEvent := ""
	if len(eventNames) > 0 {
		lastEvent = eventNames[len(eventNames)-1]
	}
	finalState := map[string]any{
		"ownership":           ownership,
		"current_goal":        nil,
		"mood":                "neutral",
		"conversation_active": false,
		"last_event_name":     nil,
		"backend_calls":       counter.Calls(),
	}
	if state != nil {
		if state.CurrentGoal != nil {
			finalState["current_goal"] = *state.CurrentGoal
		}
		finalState["mood"] = state.Mood
		finalState["conversation_active"] = state.ConversationActive
	}
	if lastEvent != "" {
		finalState["last_event_name"] = lastEvent
	}
	result.FinalState = finalState

	result.Failures = append(result.Failures, evaluate(
		domain.MapFrom(scenario["expect"]),
		events, eventNames, ownership, finalState, saved, counter.Calls(),
	)...)
	result.Passed = len(result.Failures) == 0
	return result
}

// defaultBackend builds the deterministic rule backend from the scenario's
// "backend" block, mirroring the Python runner.
func defaultBackend(scenario map[string]any) (backend.Backend, error) {
	cfg := domain.MapFrom(scenario["backend"])
	options := backend.RuleOptions{}
	if cfg["silence_probability"] != nil {
		options.SilenceProbability = domain.FloatFrom(cfg["silence_probability"])
	}
	if cfg["seed"] != nil {
		options.Seed = int64(domain.IntFrom(cfg["seed"]))
	}
	return backend.NewRuleBackend(options), nil
}

// ---------------------------------------------------------------------------
// Expectations
// ---------------------------------------------------------------------------

func evaluate(
	expect map[string]any,
	events []domain.Event,
	eventNames []string,
	ownership string,
	state map[string]any,
	saved map[string]any,
	backendCalls int,
) []string {
	failures := []string{}

	if want := domain.StringFrom(expect["ownership"]); want != "" && ownership != want {
		failures = append(failures, fmt.Sprintf("ownership expected %q, got %q", want, ownership))
	}

	for _, key := range []string{"current_goal", "mood", "conversation_active", "last_event_name"} {
		if _, present := expect[key]; !present {
			continue
		}
		if fmt.Sprint(expect[key]) != fmt.Sprint(state[key]) {
			failures = append(failures, fmt.Sprintf("%s expected %v, got %v", key, expect[key], state[key]))
		}
	}

	contains := func(list []string, value string) bool {
		for _, item := range list {
			if item == value {
				return true
			}
		}
		return false
	}

	for _, name := range domain.StringsFrom(expect["event_names_contain"]) {
		if !contains(eventNames, name) {
			failures = append(failures, fmt.Sprintf("missing event %q", name))
		}
	}
	for _, name := range domain.StringsFrom(expect["event_names_not_contain"]) {
		if contains(eventNames, name) {
			failures = append(failures, fmt.Sprintf("unexpected event %q", name))
		}
	}

	if ordered := domain.StringsFrom(expect["ordered_event_names"]); len(ordered) > 0 {
		if !isSubsequence(ordered, eventNames) {
			failures = append(failures, fmt.Sprintf("ordered events %v not found in %v", ordered, eventNames))
		}
	}

	actionTools := []string{}
	spokenParts := []string{}
	for _, event := range events {
		switch event.EventName {
		case "ACTION_STARTED":
			actionTools = append(actionTools, domain.StringFrom(event.Data["tool"]))
		case "NPC_SPOKE":
			spokenParts = append(spokenParts, domain.StringFrom(event.Data["text"]))
		}
	}
	for _, tool := range domain.StringsFrom(expect["action_tools_contain"]) {
		if !contains(actionTools, tool) {
			failures = append(failures, fmt.Sprintf("missing action tool %q in %v", tool, actionTools))
		}
	}
	if expect["action_tools_not_contain"] != nil {
		for _, tool := range domain.StringsFrom(expect["action_tools_not_contain"]) {
			if contains(actionTools, tool) {
				failures = append(failures, fmt.Sprintf("unexpected action tool %q in %v", tool, actionTools))
			}
		}
	}

	spoken := strings.Join(spokenParts, " ")
	for _, fragment := range domain.StringsFrom(expect["npc_spoke_contains"]) {
		if !strings.Contains(spoken, fragment) {
			failures = append(failures, fmt.Sprintf("NPC speech missing %q; got %q", fragment, spoken))
		}
	}
	if expect["npc_spoke_not_contains"] != nil {
		for _, fragment := range domain.StringsFrom(expect["npc_spoke_not_contains"]) {
			if strings.Contains(spoken, fragment) {
				failures = append(failures, fmt.Sprintf("NPC speech unexpectedly contains %q", fragment))
			}
		}
	}

	for name, minimum := range domain.MapFrom(expect["event_count_at_least"]) {
		count := countOf(eventNames, name)
		if count < domain.IntFrom(minimum) {
			failures = append(failures, fmt.Sprintf("event %q count %d < %v", name, count, minimum))
		}
	}
	for name, exact := range domain.MapFrom(expect["event_count_exact"]) {
		count := countOf(eventNames, name)
		if count != domain.IntFrom(exact) {
			failures = append(failures, fmt.Sprintf("event %q count %d != %v", name, count, exact))
		}
	}

	if expect["llm_calls_at_least"] != nil {
		minimum := domain.IntFrom(expect["llm_calls_at_least"])
		if backendCalls < minimum {
			failures = append(failures, fmt.Sprintf("backend calls %d < %d", backendCalls, minimum))
		}
	}

	for saveAs, rawExpected := range domain.MapFrom(expect["saved_timelines"]) {
		payload, ok := saved[saveAs]
		if !ok {
			failures = append(failures, fmt.Sprintf("saved timeline %q missing", saveAs))
			continue
		}
		actual := extractSavedEventNames(payload)
		for _, want := range domain.StringsFrom(rawExpected) {
			if !contains(actual, want) {
				failures = append(failures, fmt.Sprintf("saved timeline %q missing %q; got %v", saveAs, want, actual))
			}
		}
	}

	return failures
}

// extractSavedEventNames pulls event names out of a saved `grab_timeline`
// tool result, which may be a list of events or a wrapper object.
func extractSavedEventNames(payload any) []string {
	switch typed := payload.(type) {
	case []any:
		names := make([]string, 0, len(typed))
		for _, item := range typed {
			if object, ok := item.(map[string]any); ok {
				names = append(names, domain.StringFrom(object["event_name"]))
			}
		}
		return names
	case map[string]any:
		for _, key := range []string{"events", "timeline", "result", "items"} {
			if inner, ok := typed[key]; ok {
				if names := extractSavedEventNames(inner); len(names) > 0 {
					return names
				}
			}
		}
		if name := domain.StringFrom(typed["event_name"]); name != "" {
			return []string{name}
		}
	}
	return nil
}

func countOf(values []string, want string) int {
	count := 0
	for _, value := range values {
		if value == want {
			count++
		}
	}
	return count
}

func isSubsequence(needle, haystack []string) bool {
	index := 0
	for _, value := range haystack {
		if index < len(needle) && value == needle[index] {
			index++
		}
	}
	return index == len(needle)
}

func optionalString(value any) *string {
	if text, ok := value.(string); ok && text != "" {
		return &text
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
