// Package state holds the runtime's in-memory ownership machine, eligibility
// gate and world-state store.
//
// It is a direct port of the Python "runtime/state" package.  Rockstar AI
// remains the default owner of every ped; the runtime only takes temporary
// ownership when the conservative eligibility gate and contextual triggers
// permit it.
package state

import (
	"sync"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
)

// OwnershipState is the current owner of an NPC's behaviour.
type OwnershipState string

// The ownership states, mirroring "OwnershipState" in Python.
const (
	// StateRockstar means Rockstar's own AI owns the ped (the default).
	StateRockstar OwnershipState = "ROCKSTAR"
	// StateCandidate is a nearby, eligible ped the runtime is watching.
	StateCandidate OwnershipState = "CANDIDATE"
	// StateAware is a candidate close enough to be activated by a trigger.
	StateAware OwnershipState = "AWARE"
	// StateAIActive means the runtime owns the ped's reasoning.
	StateAIActive OwnershipState = "AI_ACTIVE"
	// StateAIConversation means the runtime owns an active conversation.
	StateAIConversation OwnershipState = "AI_CONVERSATION"
	// StateQuestDialogue is the dialogue-only overlay that leaves the action
	// task with Rockstar.
	StateQuestDialogue OwnershipState = "QUEST_DIALOGUE"
	// StateSuspended means story safety froze runtime ownership.
	StateSuspended OwnershipState = "SUSPENDED"
	// StateRelease is the transient hand-off state back to Rockstar.
	StateRelease OwnershipState = "RELEASE"
)

// Default activation distances, matching the Python defaults for
// "update_from_scan".
const (
	// DefaultCandidateDistanceM is the radius in which an eligible ped becomes
	// a CANDIDATE.
	DefaultCandidateDistanceM = 20.0
	// DefaultAwareDistanceM is the radius in which an eligible candidate
	// becomes AWARE.
	DefaultAwareDistanceM = 10.0
)

// Default reason strings, matching the Python keyword defaults.
const (
	// DefaultReleaseReason is used by RequestRelease when no reason is given.
	DefaultReleaseReason = "release_policy"
	// DefaultSuspendReason is used by Suspend when no reason is given.
	DefaultSuspendReason = "story_safety"
	// DefaultResumeReason is used by Resume when no reason is given.
	DefaultResumeReason = "story_safety_cleared"
)

// Transition is a single ownership change.
type Transition struct {
	NPCID     string         `json:"npc_id"`
	From      OwnershipState `json:"from_state"`
	To        OwnershipState `json:"to_state"`
	Reason    string         `json:"reason"`
	Timestamp float64        `json:"timestamp"`
}

// EventName maps the transition onto the timeline event the runtime appends,
// reporting false when the transition is not notable.
//
// Resume/suspend and release are checked before activation so a suspended ->
// AI_ACTIVE resume is not mislabeled as a new activation.
func (t Transition) EventName() (string, bool) {
	if t.From == StateSuspended {
		return "AGENT_RESUMED", true
	}
	if t.To == StateQuestDialogue {
		return "QUEST_DIALOGUE_ENTERED", true
	}
	if t.To == StateSuspended {
		return "AGENT_SUSPENDED", true
	}
	if t.To == StateRelease {
		return "NPC_RELEASED", true
	}
	if t.To == StateAIActive && (t.From == StateCandidate || t.From == StateAware) {
		return "NPC_ACTIVATED", true
	}
	return "", false
}

// Record is the ownership bookkeeping for one NPC.
type Record struct {
	NPCID         string          `json:"npc_id"`
	State         OwnershipState  `json:"state"`
	PreviousState *OwnershipState `json:"previous_state,omitempty"`
	History       []Transition    `json:"history"`
}

// transition applies a state change and records it, mirroring
// "OwnershipRecord.transition".
func (r *Record) transition(to OwnershipState, reason string) *Transition {
	applied := Transition{
		NPCID:     r.NPCID,
		From:      r.State,
		To:        to,
		Reason:    reason,
		Timestamp: domain.Timestamp(),
	}
	r.State = to
	r.History = append(r.History, applied)
	return &applied
}

// Manager tracks ownership state for all NPCs known to the runtime.
//
// The zero Manager is ready to use.  The manager is safe for concurrent use;
// the records it returns are not synchronised, exactly like the mutable Python
// dataclasses they mirror.
type Manager struct {
	mu      sync.Mutex
	records map[string]*Record
	order   []string
}

// NewManager returns an empty ownership manager.
func NewManager() *Manager {
	return &Manager{records: map[string]*Record{}}
}

// record returns the record for npcID, creating a ROCKSTAR-owned one on first
// sight.  Callers must hold m.mu (or be single-threaded).
func (m *Manager) record(npcID string) *Record {
	if m.records == nil {
		m.records = map[string]*Record{}
	}
	existing, ok := m.records[npcID]
	if !ok {
		existing = &Record{NPCID: npcID, State: StateRockstar}
		m.records[npcID] = existing
		m.order = append(m.order, npcID)
	}
	return existing
}

// Get returns the record for npcID, creating a ROCKSTAR-owned record when the
// NPC has not been seen before.
func (m *Manager) Get(npcID string) *Record {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.record(npcID)
}

// State returns the current ownership state for npcID.
func (m *Manager) State(npcID string) OwnershipState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.record(npcID).State
}

// CanReason reports whether the runtime may run reasoning for npcID.
func (m *Manager) CanReason(npcID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch m.record(npcID).State {
	case StateAware, StateAIActive, StateAIConversation, StateQuestDialogue:
		return true
	default:
		return false
	}
}

// CanSpeak reports whether the runtime may emit dialogue for npcID.
func (m *Manager) CanSpeak(npcID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch m.record(npcID).State {
	case StateAIActive, StateAIConversation, StateQuestDialogue:
		return true
	default:
		return false
	}
}

// ScanUpdate is one proximity/eligibility scan result.
//
// A zero CandidateDistanceM or AwareDistanceM means "not supplied" and falls
// back to DefaultCandidateDistanceM / DefaultAwareDistanceM, mirroring the
// Python keyword defaults.
type ScanUpdate struct {
	NPCID              string
	DistanceM          float64
	Eligible           bool
	CandidateDistanceM float64
	AwareDistanceM     float64
	MeaningfulTrigger  bool
}

// UpdateFromScan applies one proximity/eligibility scan result.
//
// The caller is responsible for providing a conservative eligibility result.
// This method never activates a ped by itself unless MeaningfulTrigger is true
// and the ped is already AWARE.
func (m *Manager) UpdateFromScan(update ScanUpdate) *Transition {
	m.mu.Lock()
	defer m.mu.Unlock()

	record := m.record(update.NPCID)

	candidateDistance := update.CandidateDistanceM
	if candidateDistance == 0 {
		candidateDistance = DefaultCandidateDistanceM
	}
	awareDistance := update.AwareDistanceM
	if awareDistance == 0 {
		awareDistance = DefaultAwareDistanceM
	}

	if record.State == StateSuspended {
		return nil
	}

	// Story safety / eligibility is always strongest.
	if !update.Eligible && record.State != StateRockstar && record.State != StateRelease {
		return record.transition(StateRelease, "eligibility_failed")
	}

	farAway := update.DistanceM > candidateDistance
	if farAway && record.State != StateRockstar && record.State != StateCandidate && record.State != StateRelease {
		return record.transition(StateRelease, "player_left_area")
	}

	switch record.State {
	case StateRockstar, StateRelease:
		if update.DistanceM <= candidateDistance && update.Eligible {
			return record.transition(StateCandidate, "player_nearby")
		}
		if record.State == StateRelease {
			return record.transition(StateRockstar, "release_complete")
		}
		return nil
	case StateCandidate:
		if update.DistanceM <= awareDistance && update.Eligible {
			return record.transition(StateAware, "eligibility_passed")
		}
		if farAway {
			return record.transition(StateRockstar, "candidate_left_area")
		}
		return nil
	case StateAware:
		if update.MeaningfulTrigger && update.Eligible {
			return record.transition(StateAIActive, "meaningful_trigger")
		}
		// Unreachable while the blanket ``player_left_area`` check above
		// stands; kept because the Python branch structure keeps it.
		if farAway {
			return record.transition(StateRelease, "aware_left_area")
		}
		return nil
	case StateAIActive:
		if farAway {
			return record.transition(StateRelease, "player_left_area")
		}
		return nil
	case StateAIConversation:
		if farAway {
			return record.transition(StateRelease, "player_left_area")
		}
		return nil
	default:
		return nil
	}
}

// EnterQuestDialogue allows the dialogue-only overlay while Rockstar keeps the
// action task.
func (m *Manager) EnterQuestDialogue(npcID string) *Transition {
	m.mu.Lock()
	defer m.mu.Unlock()

	record := m.record(npcID)
	if record.State == StateQuestDialogue {
		return nil
	}
	switch record.State {
	case StateRockstar, StateCandidate, StateAware, StateAIActive, StateAIConversation:
		return record.transition(StateQuestDialogue, "quest_dialogue")
	default:
		return nil
	}
}

// PushToTalk promotes a speakable NPC into an active conversation.
func (m *Manager) PushToTalk(npcID string) *Transition {
	m.mu.Lock()
	defer m.mu.Unlock()

	record := m.record(npcID)
	switch record.State {
	case StateAware, StateAIActive, StateAIConversation, StateQuestDialogue:
		return record.transition(StateAIConversation, "push_to_talk")
	default:
		return nil
	}
}

// ConversationEnded returns an active conversation to AI_ACTIVE.
func (m *Manager) ConversationEnded(npcID string) *Transition {
	m.mu.Lock()
	defer m.mu.Unlock()

	record := m.record(npcID)
	if record.State == StateAIConversation {
		return record.transition(StateAIActive, "conversation_ended")
	}
	return nil
}

// RequestRelease asks for the transient RELEASE hand-off.  An empty reason
// falls back to DefaultReleaseReason.
func (m *Manager) RequestRelease(npcID, reason string) *Transition {
	m.mu.Lock()
	defer m.mu.Unlock()

	if reason == "" {
		reason = DefaultReleaseReason
	}
	record := m.record(npcID)
	if record.State == StateRelease {
		return nil
	}
	return record.transition(StateRelease, reason)
}

// FinalizeRelease completes the hand-off, returning ownership to Rockstar.
func (m *Manager) FinalizeRelease(npcID string) *Transition {
	m.mu.Lock()
	defer m.mu.Unlock()

	record := m.record(npcID)
	if record.State == StateRelease {
		return record.transition(StateRockstar, "release_complete")
	}
	return nil
}

// Suspend freezes runtime ownership for story safety.  Rockstar-owned and
// already-suspended NPCs are left alone.  An empty reason falls back to
// DefaultSuspendReason.
func (m *Manager) Suspend(npcID, reason string) *Transition {
	m.mu.Lock()
	defer m.mu.Unlock()

	if reason == "" {
		reason = DefaultSuspendReason
	}
	record := m.record(npcID)
	if record.State == StateRockstar || record.State == StateSuspended {
		return nil
	}
	previous := record.State
	record.PreviousState = &previous
	return record.transition(StateSuspended, reason)
}

// Resume restores the state recorded by Suspend, falling back to AWARE when
// there is no usable previous state.  An empty reason falls back to
// DefaultResumeReason.
func (m *Manager) Resume(npcID, reason string) *Transition {
	m.mu.Lock()
	defer m.mu.Unlock()

	if reason == "" {
		reason = DefaultResumeReason
	}
	record := m.record(npcID)
	if record.State != StateSuspended {
		return nil
	}
	resumeTo := StateAware
	if record.PreviousState != nil {
		resumeTo = *record.PreviousState
	}
	switch resumeTo {
	case StateRockstar, StateRelease, StateSuspended:
		resumeTo = StateAware
	}
	record.PreviousState = nil
	return record.transition(resumeTo, reason)
}

// OwnedNPCs returns every npc_id the manager has a record for, in the order
// the NPCs were first seen.
func (m *Manager) OwnedNPCs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	owned := make([]string, len(m.order))
	copy(owned, m.order)
	return owned
}
