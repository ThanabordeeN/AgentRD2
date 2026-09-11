package state

import (
	"sync"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
)

// recentEventsMergeLimit is the number of timeline-backed events GetMap
// merges into the world snapshot, mirroring the Python default.
const recentEventsMergeLimit = 20

// WorldStore keeps the latest world snapshot per NPC plus a short list of
// recent game events.
//
// The bridge owns the authoritative simulation; the runtime only keeps the
// latest snapshot needed to build an agent prompt and validate actions.
//
// The zero WorldStore is ready to use and safe for concurrent use.  Get and
// GetMap hand out the stored maps so callers see the same live values Python
// exposes; GetMap is the deep-copied, prompt-safe view.
type WorldStore struct {
	mu     sync.Mutex
	states map[string]domain.WorldState
	recent map[string][]map[string]any
}

// NewWorldStore returns an empty store.
func NewWorldStore() *WorldStore {
	return &WorldStore{
		states: map[string]domain.WorldState{},
		recent: map[string][]map[string]any{},
	}
}

// Update stores the latest snapshot for its NPC.
func (w *WorldStore) Update(state domain.WorldState) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.states == nil {
		w.states = map[string]domain.WorldState{}
	}
	w.states[state.NPCID] = normalizeWorldState(state)
}

// Get returns the stored snapshot, creating an empty one on first sight.  The
// returned value shares the stored maps, matching the live Python object.
func (w *WorldStore) Get(npcID string) domain.WorldState {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.get(npcID)
}

// get returns the stored snapshot for npcID, creating and caching an empty one
// when the NPC is unknown.  Callers must hold w.mu (or be single-threaded).
func (w *WorldStore) get(npcID string) domain.WorldState {
	if w.states == nil {
		w.states = map[string]domain.WorldState{}
	}
	state, ok := w.states[npcID]
	if !ok {
		state = normalizeWorldState(domain.WorldState{
			NPCID:     npcID,
			Timestamp: domain.Timestamp(),
		})
		w.states[npcID] = state
	}
	return state
}

// GetMap returns the snapshot as the prompt/JSON payload, merging the
// timeline-backed recent events so a single call is one current picture even
// when the bridge sent no event list.  The result is a deep copy: mutating it
// never touches the store.
func (w *WorldStore) GetMap(npcID string) map[string]any {
	w.mu.Lock()
	defer w.mu.Unlock()

	payload := w.get(npcID).ToMap()
	if events := w.recentEvents(npcID, recentEventsMergeLimit); len(events) > 0 {
		payload["recent_game_events"] = events
	}
	return deepCopyMap(payload)
}

// ApplyEvent appends one event to the NPC's recent-event list and trims the
// list exactly like Python's "del bucket[:-max_recent]".
//
// The Python slice bound is "-max_recent", so a positive maxRecent keeps the
// last maxRecent events, a zero maxRecent trims nothing ("-0 == 0") and a
// negative maxRecent skips that many leading events.
func (w *WorldStore) ApplyEvent(event domain.Event, maxRecent int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.recent == nil {
		w.recent = map[string][]map[string]any{}
	}
	bucket := append(w.recent[event.NPCID], event.ToMap())
	if len(bucket) > maxRecent {
		stop := -maxRecent
		if stop < 0 {
			stop += len(bucket)
		}
		if stop < 0 {
			stop = 0
		}
		if stop > len(bucket) {
			stop = len(bucket)
		}
		if stop > 0 {
			bucket = bucket[stop:]
		}
	}
	w.recent[event.NPCID] = bucket
}

// RecentGameEvents returns the NPC's most recent events, oldest first.
//
// Following Python's "list(bucket[-limit:])", a positive limit keeps the
// last limit events, a zero limit returns everything and a negative limit
// skips that many leading events.  The returned slice is a copy; the event
// maps inside it are shared with the store.
func (w *WorldStore) RecentGameEvents(npcID string, limit int) []map[string]any {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.recentEvents(npcID, limit)
}

// recentEvents implements the Python slice semantics.  Callers must hold w.mu
// (or be single-threaded).
func (w *WorldStore) recentEvents(npcID string, limit int) []map[string]any {
	bucket := w.recent[npcID]
	start := 0
	switch {
	case limit > 0:
		if start = len(bucket) - limit; start < 0 {
			start = 0
		}
	case limit < 0:
		if start = -limit; start > len(bucket) {
			start = len(bucket)
		}
	}
	events := make([]map[string]any, 0, len(bucket)-start)
	events = append(events, bucket[start:]...)
	return events
}

// SetSelfField sets one field of the NPC's own state, creating the snapshot
// when needed.
func (w *WorldStore) SetSelfField(npcID, key string, value any) {
	w.mu.Lock()
	defer w.mu.Unlock()

	state := w.get(npcID)
	if state.SelfState == nil {
		state.SelfState = map[string]any{}
	}
	state.SelfState[key] = value
	w.states[npcID] = state
}

// SetPlayerField sets one field of the player state the NPC can observe,
// creating the snapshot when needed.
func (w *WorldStore) SetPlayerField(npcID, key string, value any) {
	w.mu.Lock()
	defer w.mu.Unlock()

	state := w.get(npcID)
	if state.Player == nil {
		state.Player = map[string]any{}
	}
	state.Player[key] = value
	w.states[npcID] = state
}

// Remove forgets everything stored for the NPC.
func (w *WorldStore) Remove(npcID string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	delete(w.states, npcID)
	delete(w.recent, npcID)
}

// normalizeWorldState gives a snapshot the empty maps the Python dataclass
// always has, so later field writes never need to test for nil.
func normalizeWorldState(state domain.WorldState) domain.WorldState {
	if state.SelfState == nil {
		state.SelfState = map[string]any{}
	}
	if state.Player == nil {
		state.Player = map[string]any{}
	}
	return state
}

// deepCopyMap recursively copies a JSON-like map, mirroring copy.deepcopy.
func deepCopyMap(value map[string]any) map[string]any {
	copied := make(map[string]any, len(value))
	for key, item := range value {
		copied[key] = deepCopyValue(item)
	}
	return copied
}

func deepCopyValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return deepCopyMap(typed)
	case []map[string]any:
		copied := make([]map[string]any, len(typed))
		for index, item := range typed {
			copied[index] = deepCopyMap(item)
		}
		return copied
	case []any:
		copied := make([]any, len(typed))
		for index, item := range typed {
			copied[index] = deepCopyValue(item)
		}
		return copied
	case []string:
		return append([]string(nil), typed...)
	default:
		return value
	}
}
