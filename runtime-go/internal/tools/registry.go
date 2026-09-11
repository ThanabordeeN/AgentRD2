// Package tools ports the Python runtime's high-level agent tool layer
// (“runtime/tools“).
//
// The catalog is the contract: the LLM sees exactly the same 31 tool names,
// descriptions, and JSON-schema parameter blocks as the Python runtime, so
// “BuildDefaultRegistry().Describe()“ stays directly comparable with
// “runtime.tools.build_default_registry().describe()“. The golden copy of
// the Python output lives in “testdata/tool_catalog.json“ and is asserted by
// “catalog_test.go“.
//
// Python handlers rely on exceptions for bad input: “AgentRuntime._call_tool“
// catches whatever a handler raises and converts it into
// “ToolResult.failed“. Go has no such net, so every handler in this package
// validates its arguments and returns a failed result instead of panicking.
//
// Two normalisations the Python runtime performs before a handler runs are
// absorbed here as well, because the Go agent may call the registry directly:
//
//   - “None“/nil arguments are treated as absent (Python drops them in
//     “_call_tool“).
//   - the critical defaults from “AgentRuntime._normalize_action“ are applied
//     (“entity="player"“ for look_at/face/follow/flee_from, “radius=8.0“
//     for wander, “duration=2.0“ for wait, “type="neutral"“ for gesture).
//
// Numeric arguments are coerced tolerantly (JSON numbers, numeric strings, and
// bools) exactly like the rest of the Go port; Python's “float(...)“ and
// “int(...)“ casts accept the same range of values an LLM can produce.
package tools

import (
	"fmt"
	"sort"
	"sync"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/timeline"
)

// Dispatcher sends a high-level action to the RDR2 bridge, mirroring the
// Python “ActionDispatcher“ protocol.
type Dispatcher interface {
	Dispatch(request domain.ActionRequest) domain.ToolResult
}

// WorldReader is the slice of the per-NPC world-state store the tools layer
// needs. “*state.WorldStore“ satisfies it.
type WorldReader interface {
	GetMap(npcID string) map[string]any
}

// grab_timeline argument names, matching “GRAB_TIMELINE_SCHEMA“ in
// “runtime/tools/timeline.py“. They map one-to-one onto the fields of
// “timeline.GrabFilter“.
const (
	FilterEventName         = "event_name"
	FilterEventNames        = "event_names"
	FilterTag               = "tag"
	FilterTags              = "tags"
	FilterEntity            = "entity"
	FilterMinimumImportance = "minimum_importance"
	FilterSinceSeq          = "since_seq"
	FilterBeforeSeq         = "before_seq"
	FilterLimit             = "limit"
)

// grabTimelineDefaultLimit is the Python store's “limit“ keyword default. The
// Go filter uses 0 for "no truncation", so the tool layer supplies the default
// the JSON schema advertises.
const grabTimelineDefaultLimit = 20

// TimelineReader retrieves timeline events for one NPC.
// “*timeline.Store“ satisfies it.
type TimelineReader interface {
	GrabTimeline(npcID string, filter timeline.GrabFilter) ([]domain.Event, error)
}

// LoreReader is the slice of the offline wiki/lore store the tools layer needs.
//
// “*lore.Store“ satisfies it. Both methods take the store's limit/maxChars
// arguments so the tool layer keeps supplying the same defaults the Python
// tool relied on (3/1200 for a topic lookup, 5/1200 for profile context).
type LoreReader interface {
	// ContextForProfile returns the canonical context pages for an NPC profile.
	ContextForProfile(profile map[string]any, limit, maxChars int) []map[string]any
	// Lookup returns the best pages for a free-text query.
	Lookup(query string, limit, maxChars int) []map[string]any
}

// Spec describes one high-level tool, mirroring the Python “ToolSpec“
// dataclass.
type Spec struct {
	// Name is the tool name the LLM calls.
	Name string
	// Description is the model-facing description.
	Description string
	// Parameters is the JSON-schema parameter block, exactly as the Python
	// catalog publishes it.
	Parameters map[string]any
	// Category groups the tool ("perception", "movement", ...). An empty
	// category is stored as "general", the Python default.
	Category string
	// Handler executes the tool. It must never panic: invalid arguments are
	// reported through a failed domain.ToolResult.
	Handler func(ctx *Context, args map[string]any) domain.ToolResult
}

// asDict mirrors “ToolSpec.as_dict“.
func (s Spec) asDict() map[string]any {
	return map[string]any{
		"name":        s.Name,
		"description": s.Description,
		"parameters":  s.Parameters,
	}
}

// Context carries everything a handler needs, mirroring the Python
// “ToolContext“.
type Context struct {
	// NPCID is the NPC the tools act for.
	NPCID string
	// World reads the current world state; it may be nil.
	World WorldReader
	// Timeline reads the NPC's JSONL timeline; it may be nil.
	Timeline TimelineReader
	// Lore reads the offline wiki packs; it may be nil.
	Lore LoreReader
	// Profile is the NPC profile used by ``get_world_lore`` when no topic is
	// given; it may be nil.
	Profile map[string]any
	// Dispatcher forwards action tools to the bridge; it may be nil in tests.
	Dispatcher Dispatcher
	// Extra is free-form runtime data, mirroring the Python ``extra`` field.
	Extra map[string]any
}

// GetWorldState mirrors “ToolContext.get_world_state“. A missing store or a
// nil map yields an empty map; use World directly to detect a missing store.
func (c *Context) GetWorldState() map[string]any {
	world := worldOf(c)
	if world == nil {
		return map[string]any{}
	}
	state := world.GetMap(c.NPCID)
	if state == nil {
		return map[string]any{}
	}
	return state
}

// GrabTimeline mirrors “ToolContext.grab_timeline“: it forwards the typed
// filter to the store and returns the raw events. The caller formats them with
// “domain.Event.ToMap“.
func (c *Context) GrabTimeline(filter timeline.GrabFilter) ([]domain.Event, error) {
	if c == nil || c.Timeline == nil {
		return nil, fmt.Errorf("timeline reader is not configured")
	}
	events, err := c.Timeline.GrabTimeline(c.NPCID, filter)
	if err != nil {
		return nil, err
	}
	if events == nil {
		return []domain.Event{}, nil
	}
	return events, nil
}

// DispatchAction mirrors “ToolContext.dispatch_action“: it builds the
// “domain.ActionRequest“ for this NPC and hands it to the dispatcher. A
// missing dispatcher fails the call instead of panicking.
func (c *Context) DispatchAction(tool string, args map[string]any) domain.ToolResult {
	request := requestFor(tool, c, args)
	if c == nil || c.Dispatcher == nil {
		return domain.FailedResult(request, "action dispatcher is not configured", nil)
	}
	return c.Dispatcher.Dispatch(request)
}

// Registry holds the tool catalog. It is safe for concurrent use: the runtime
// builds it once at start-up and reads it from every NPC goroutine.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Spec
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{tools: map[string]Spec{}}
}

// Register adds a spec, mirroring “ToolRegistry.register“. Registering a
// name twice is an error. An empty category becomes "general", the Python
// dataclass default.
func (r *Registry) Register(spec Spec) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tools == nil {
		r.tools = map[string]Spec{}
	}
	if _, exists := r.tools[spec.Name]; exists {
		return fmt.Errorf("tool already registered: %s", spec.Name)
	}
	if spec.Category == "" {
		spec.Category = "general"
	}
	r.tools[spec.Name] = spec
	return nil
}

// Get returns the spec registered under name. The boolean is false for an
// unknown tool (Python raises “KeyError“ instead).
func (r *Registry) Get(name string) (Spec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	spec, ok := r.tools[name]
	return spec, ok
}

// Names returns the registered tool names in sorted order, mirroring
// “ToolRegistry.names“.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Describe returns the model-facing catalog (name/description/parameters) in
// sorted name order, mirroring “ToolRegistry.describe“.
func (r *Registry) Describe() []map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]map[string]any, 0, len(names))
	for _, name := range names {
		out = append(out, r.tools[name].asDict())
	}
	return out
}

// Call executes a tool. It mirrors “ToolRegistry.call“ but never panics and
// never returns an error: an unknown tool, a spec without a handler, or a nil
// context produces a failed result with a reason.
func (r *Registry) Call(name string, ctx *Context, args map[string]any) domain.ToolResult {
	spec, ok := r.Get(name)
	if !ok {
		return failedCall(name, ctx, args, fmt.Sprintf("unknown agent tool: %s", name))
	}
	if spec.Handler == nil {
		return failedCall(name, ctx, args, fmt.Sprintf("tool has no handler: %s", name))
	}
	if ctx == nil {
		return failedCall(name, ctx, args, "tool context is nil")
	}
	if args == nil {
		args = map[string]any{}
	}
	return spec.Handler(ctx, args)
}

// BuildDefaultRegistry returns the full catalog, mirroring
// “build_default_registry(include_deferred=True)“: the same 31 tools in the
// same registration order (combat is the deferred family and is registered
// last).
func BuildDefaultRegistry() *Registry {
	registry := NewRegistry()
	RegisterWorldTools(registry)
	RegisterTimelineTools(registry)
	RegisterLoreTools(registry)
	RegisterMovementTools(registry)
	RegisterAttentionTools(registry)
	RegisterSpeechTools(registry)
	RegisterThinkTools(registry)
	RegisterReactionTools(registry)
	RegisterInteractionTools(registry)
	RegisterCombatTools(registry)
	return registry
}
