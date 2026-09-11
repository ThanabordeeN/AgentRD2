package agent

import (
	"sync"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/timeline"
)

// transientTools are short/soft overlays. They must not block idle planning
// while the runtime waits for a bridge action_result. It mirrors
// ``RuntimeActionDispatcher.TRANSIENT_TOOLS`` in the Python runtime.
var transientTools = map[string]bool{
	"think":           true,
	"gesture":         true,
	"clear_attention": true,
}

// RuntimeActionDispatcher validates and logs action starts and hands them to
// the bridge transport. It ports the Python class of the same name.
type RuntimeActionDispatcher struct {
	mu      sync.Mutex
	store   *timeline.Store
	pending map[string]domain.ActionRequest
	order   []string
	send    func(payload map[string]any)
}

// NewRuntimeActionDispatcher builds a dispatcher that records action events on
// the given timeline.
func NewRuntimeActionDispatcher(store *timeline.Store) *RuntimeActionDispatcher {
	return &RuntimeActionDispatcher{
		store:   store,
		pending: map[string]domain.ActionRequest{},
	}
}

// SetSend installs the transport callback used to push action requests to the
// bridge. A nil callback disables sending.
func (d *RuntimeActionDispatcher) SetSend(fn func(payload map[string]any)) {
	d.mu.Lock()
	d.send = fn
	d.mu.Unlock()
}

// Dispatch validates one action request, records ACTION_STARTED (plus
// NPC_SPOKE for speech) on the timeline, and forwards it to the bridge.
//
// Non-transient tools stay pending until Resolve reports their result.
func (d *RuntimeActionDispatcher) Dispatch(request domain.ActionRequest) domain.ToolResult {
	d.mu.Lock()
	if !transientTools[request.Tool] {
		if _, exists := d.pending[request.RequestID]; !exists {
			d.order = append(d.order, request.RequestID)
		}
		d.pending[request.RequestID] = request
	}
	send := d.send
	d.mu.Unlock()

	if request.NPCID != "" {
		data := map[string]any{
			"tool":       request.Tool,
			"request_id": request.RequestID,
			"arguments":  copyMap(request.Arguments),
		}
		_, _ = d.store.AppendEvent(request.NPCID, "ACTION_STARTED", timeline.AppendOptions{
			Data:       data,
			Tags:       []string{"action"},
			Importance: float64Pointer(0.3),
		})
		if request.Tool == "say" {
			_, _ = d.store.AppendEvent(request.NPCID, "NPC_SPOKE", timeline.AppendOptions{
				Data: map[string]any{
					"text":    request.Arguments["text"],
					"target":  request.Arguments["target"],
					"emotion": request.Arguments["emotion"],
				},
				Entities:   []string{request.NPCID},
				Tags:       []string{"npc", "speech"},
				Importance: float64Pointer(0.4),
			})
		}
	}
	if send != nil {
		send(map[string]any{
			"type":    "action_request",
			"npc_id":  request.NPCID,
			"request": request.ToMap(),
		})
	}
	return domain.StartedResult(request, nil)
}

// Resolve clears and returns the pending request matching raw["request_id"].
// The boolean is false when the payload carries no request id or the request
// is unknown.
func (d *RuntimeActionDispatcher) Resolve(raw map[string]any) (domain.ActionRequest, bool) {
	requestID := domain.StringFrom(raw["request_id"])
	if requestID == "" {
		return domain.ActionRequest{}, false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	request, ok := d.pending[requestID]
	if !ok {
		return domain.ActionRequest{}, false
	}
	d.removeLocked(requestID)
	return request, true
}

// Pending reports how many actions are still awaiting a bridge result.
func (d *RuntimeActionDispatcher) Pending() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.pending)
}

// FindPending returns the oldest still-pending request for the named tool.
func (d *RuntimeActionDispatcher) FindPending(tool string) (domain.ActionRequest, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, requestID := range d.order {
		if request, ok := d.pending[requestID]; ok && request.Tool == tool {
			return request, true
		}
	}
	return domain.ActionRequest{}, false
}

// PendingNPCIDs returns the set of NPC ids with at least one pending action.
// The Python runtime derives the same set from ``dispatcher.pending.values()``.
func (d *RuntimeActionDispatcher) PendingNPCIDs() map[string]bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	ids := make(map[string]bool, len(d.pending))
	for _, request := range d.pending {
		if request.NPCID != "" {
			ids[request.NPCID] = true
		}
	}
	return ids
}

// PendingRequests returns a snapshot of pending requests in insertion order.
func (d *RuntimeActionDispatcher) PendingRequests() []domain.ActionRequest {
	d.mu.Lock()
	defer d.mu.Unlock()
	requests := make([]domain.ActionRequest, 0, len(d.pending))
	for _, requestID := range d.order {
		if request, ok := d.pending[requestID]; ok {
			requests = append(requests, request)
		}
	}
	return requests
}

func (d *RuntimeActionDispatcher) removeLocked(requestID string) {
	delete(d.pending, requestID)
	for index, candidate := range d.order {
		if candidate == requestID {
			d.order = append(d.order[:index], d.order[index+1:]...)
			return
		}
	}
}

func copyMap(source map[string]any) map[string]any {
	if source == nil {
		return map[string]any{}
	}
	target := make(map[string]any, len(source))
	for key, value := range source {
		target[key] = value
	}
	return target
}

func float64Pointer(value float64) *float64 {
	return &value
}
