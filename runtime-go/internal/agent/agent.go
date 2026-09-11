// Package agent ports the heart of the Python runtime: the event-driven
// orchestration that turns bridge messages into timeline facts, wakes the
// configured backend when ownership changes, validates the model's high-level
// actions, and sends them to the bridge.
//
// It is a faithful port of “runtime/agent/npc_agent.py“ and
// “runtime/agent/instructions.py“: rules, thresholds, reason strings, event
// names, ordering decisions, and guardrails follow the Python implementation.
package agent

import (
	"fmt"
	"sync"
	"time"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/backend"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/config"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/events"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/lore"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/project"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/state"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/timeline"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/tools"
)

// AgentRuntimeState is the per-NPC runtime bookkeeping kept between decision
// turns. It mirrors the Python “AgentRuntimeState“ dataclass.
type AgentRuntimeState struct {
	NPCID              string
	CurrentGoal        *string
	Mood               string
	ConversationActive bool
	LastSpokenAt       float64
	LastDecisionAt     float64
	Reasoning          bool
	DialogueOnly       bool
	QuestContext       map[string]any
}

// clone returns a snapshot safe to hand to callers outside the runtime lock.
func (s *AgentRuntimeState) clone() *AgentRuntimeState {
	if s == nil {
		return nil
	}
	copied := *s
	if s.CurrentGoal != nil {
		goal := *s.CurrentGoal
		copied.CurrentGoal = &goal
	}
	copied.QuestContext = copyMap(s.QuestContext)
	return &copied
}

// Options configures NewRuntime. Every nil field falls back to the same
// default the Python constructor used.
type Options struct {
	// Settings is the runtime configuration. Nil loads
	// config/settings.json from the project root.
	Settings *config.Settings
	// Timeline is the JSONL event store. Nil uses Settings.TimelinesDir.
	Timeline *timeline.Store
	// World is the in-memory world snapshot store. Nil creates an empty one.
	World *state.WorldStore
	// Profiles loads NPC profile JSON. Nil uses Settings.ProfilesDir.
	Profiles *config.ProfileStore
	// Ownership tracks the ownership state machine. Nil creates an empty one.
	Ownership *state.Manager
	// Eligibility gates ped activation. Nil loads the story blacklist.
	Eligibility *state.Gate
	// Backend decides what an NPC does next. Nil uses the rule backend.
	Backend backend.Backend
	// Registry holds the high-level tools. Nil builds the default registry.
	Registry *tools.Registry
	// Dispatcher records and sends actions. Nil creates one over Timeline.
	Dispatcher *RuntimeActionDispatcher
	// Lore is the offline wiki/lore context pack. Nil loads it from Settings.
	Lore *lore.Store
	// Quests is the quest context pack. Nil loads it from Settings.
	Quests *lore.QuestStore
}

// Runtime ports the Python “NpcAgentRuntime“.
//
// All exported methods are safe for concurrent use: the IPC server calls
// HandleMessage from several connections at once, so shared maps are guarded
// by a single mutex. Unexported methods assume the lock is already held.
type Runtime struct {
	mu sync.Mutex

	settings    config.Settings
	timeline    *timeline.Store
	world       *state.WorldStore
	profiles    *config.ProfileStore
	ownership   *state.Manager
	eligibility *state.Gate
	backend     backend.Backend
	registry    *tools.Registry
	dispatcher  *RuntimeActionDispatcher
	lore        *lore.Store
	quests      *lore.QuestStore
	normalizer  *events.Normalizer

	agentState         map[string]*AgentRuntimeState
	agentOrder         []string
	lastPedScan        map[string]domain.PedSnapshot
	lastGlobalSpeechAt float64
	deferredOrder      []string
	deferredReason     map[string]string

	// now is the clock, injectable so tests can advance time deterministically.
	now func() float64
}

// NewRuntime builds a runtime, filling in the same defaults as the Python
// “NpcAgentRuntime“ constructor.
func NewRuntime(opts Options) (*Runtime, error) {
	settings := config.DefaultSettings()
	if opts.Settings != nil {
		settings = *opts.Settings
	} else {
		loaded, err := config.LoadSettings(project.Resolve("config/settings.json"))
		if err != nil {
			return nil, fmt.Errorf("agent: load settings: %w", err)
		}
		settings = loaded
	}

	store := opts.Timeline
	if store == nil {
		created, err := timeline.NewStore(settings.TimelinesDir)
		if err != nil {
			return nil, fmt.Errorf("agent: open timeline store: %w", err)
		}
		store = created
	}

	world := opts.World
	if world == nil {
		world = state.NewWorldStore()
	}

	profiles := opts.Profiles
	if profiles == nil {
		profiles = config.NewProfileStore(settings.ProfilesDir)
	}

	ownership := opts.Ownership
	if ownership == nil {
		ownership = state.NewManager()
	}

	eligibility := opts.Eligibility
	if eligibility == nil {
		// Fail closed: the Python runtime refuses to start when the story
		// blacklist is missing, so a ped can never be taken over without it.
		blacklist, err := state.LoadBlacklistStrict(settings.StoryBlacklistPath)
		if err != nil {
			return nil, fmt.Errorf("agent: load story blacklist: %w", err)
		}
		eligibility = state.NewGate(settings.StorySafety.BlockIfUncertain, blacklist)
	}

	decider := opts.Backend
	if decider == nil {
		decider = newDefaultBackend(settings)
	}

	registry := opts.Registry
	if registry == nil {
		registry = tools.BuildDefaultRegistry()
	}

	dispatcher := opts.Dispatcher
	if dispatcher == nil {
		dispatcher = NewRuntimeActionDispatcher(store)
	}

	loreStore := opts.Lore
	if loreStore == nil {
		loreStore = lore.NewStore(settings.WikiContextPath, settings.CharacterContextPath)
	}

	quests := opts.Quests
	if quests == nil {
		quests = lore.NewQuestStore(settings.QuestsDir)
	}

	return &Runtime{
		settings:       settings,
		timeline:       store,
		world:          world,
		profiles:       profiles,
		ownership:      ownership,
		eligibility:    eligibility,
		backend:        decider,
		registry:       registry,
		dispatcher:     dispatcher,
		lore:           loreStore,
		quests:         quests,
		normalizer:     events.NewNormalizer(),
		agentState:     map[string]*AgentRuntimeState{},
		lastPedScan:    map[string]domain.PedSnapshot{},
		deferredReason: map[string]string{},
		now:            func() float64 { return float64(time.Now().UnixNano()) / 1e9 },
	}, nil
}

// Dispatcher exposes the action dispatcher so the IPC server can install its
// transport callback with SetSend.
func (r *Runtime) Dispatcher() *RuntimeActionDispatcher { return r.dispatcher }

// Timeline exposes the JSONL timeline store.
func (r *Runtime) Timeline() *timeline.Store { return r.timeline }

// World exposes the world snapshot store.
func (r *Runtime) World() *state.WorldStore { return r.world }

// Registry exposes the high-level tool registry.
func (r *Runtime) Registry() *tools.Registry { return r.registry }

// Profiles exposes the NPC profile store.
func (r *Runtime) Profiles() *config.ProfileStore { return r.profiles }

// Ownership exposes the ownership state machine.
func (r *Runtime) Ownership() *state.Manager { return r.ownership }

// Lore exposes the offline lore context store.
func (r *Runtime) Lore() *lore.Store { return r.lore }

// Quests exposes the quest context store.
func (r *Runtime) Quests() *lore.QuestStore { return r.quests }

// Backend exposes the configured decision backend.
func (r *Runtime) Backend() backend.Backend { return r.backend }

// Settings returns the effective runtime settings.
func (r *Runtime) Settings() config.Settings { return r.settings }

// State returns a snapshot of one NPC's runtime bookkeeping, creating the
// entry (and rehydrating its goal from the timeline) when it is unknown.
func (r *Runtime) State(npcID string) *AgentRuntimeState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.agentStateLocked(npcID).clone()
}

// agentStateLocked returns the live per-NPC state, creating it on first use.
// The caller must hold r.mu.
func (r *Runtime) agentStateLocked(npcID string) *AgentRuntimeState {
	if existing, ok := r.agentState[npcID]; ok {
		return existing
	}
	created := &AgentRuntimeState{
		NPCID:        npcID,
		Mood:         "neutral",
		QuestContext: map[string]any{},
	}
	// Rehydrate the current goal from persisted events so a runtime restart
	// does not silently discard the NPC's active intention.
	recent, err := r.timeline.RecentEvents(npcID, 50)
	if err == nil {
		for index := len(recent) - 1; index >= 0; index-- {
			event := recent[index]
			if event.EventName == "GOAL_COMPLETED" {
				break
			}
			if event.EventName == "GOAL_CREATED" || event.EventName == "GOAL_CHANGED" {
				if goal, ok := event.Data["goal"].(string); ok {
					created.CurrentGoal = &goal
				}
				break
			}
		}
	}
	r.agentState[npcID] = created
	r.agentOrder = append(r.agentOrder, npcID)
	return created
}

// agentStateIDsLocked returns the tracked NPC ids in insertion order, matching
// Python's “agent_state“ dict order.
// The caller must hold r.mu.
func (r *Runtime) agentStateIDsLocked() []string {
	ids := make([]string, len(r.agentOrder))
	copy(ids, r.agentOrder)
	return ids
}

// nextSeq mirrors “TimelineStore.next_seq“.
func (r *Runtime) nextSeq(npcID string) (int, error) {
	return r.timeline.NextSeq(npcID), nil
}

// appendEvent records one timeline fact.
func (r *Runtime) appendEvent(npcID, eventName string, opts timeline.AppendOptions) (domain.Event, error) {
	return r.timeline.AppendEvent(npcID, eventName, opts)
}

// npcFromPending resolves an NPC id from a pending action request id, matching
// “_npc_from_pending“.
func (r *Runtime) npcFromPending(raw map[string]any) string {
	requestID := domain.StringFrom(raw["request_id"])
	if requestID == "" {
		return ""
	}
	for _, request := range r.dispatcher.PendingRequests() {
		if request.RequestID == requestID {
			return request.NPCID
		}
	}
	return ""
}
