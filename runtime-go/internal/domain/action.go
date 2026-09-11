package domain

// ActionStatus mirrors the Python “ActionStatus“ enum values.
type ActionStatus string

const (
	ActionStarted   ActionStatus = "started"
	ActionCompleted ActionStatus = "completed"
	ActionFailed    ActionStatus = "failed"
)

// ActionRequest is a high-level tool invocation sent to the bridge.
type ActionRequest struct {
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"arguments"`
	RequestID string         `json:"request_id"`
	NPCID     string         `json:"npc_id,omitempty"`
}

// NewActionRequest mirrors “ActionRequest“ defaults.
func NewActionRequest(tool string, arguments map[string]any, npcID string) ActionRequest {
	if arguments == nil {
		arguments = map[string]any{}
	}
	return ActionRequest{
		Tool:      tool,
		Arguments: arguments,
		RequestID: NewActionID(),
		NPCID:     npcID,
	}
}

// ToMap mirrors “ActionRequest.to_dict“.
func (a ActionRequest) ToMap() map[string]any {
	payload := map[string]any{
		"tool":       a.Tool,
		"request_id": a.RequestID,
		"arguments":  CleanMap(a.Arguments),
	}
	if a.NPCID != "" {
		payload["npc_id"] = a.NPCID
	}
	return payload
}

// ToolResult reports the outcome of an action back to the runtime.
type ToolResult struct {
	RequestID string         `json:"request_id"`
	Tool      string         `json:"tool"`
	Status    ActionStatus   `json:"status"`
	Reason    string         `json:"reason,omitempty"`
	Detail    map[string]any `json:"detail,omitempty"`
}

// StartedResult mirrors “ToolResult.started“.
func StartedResult(request ActionRequest, detail map[string]any) ToolResult {
	return ToolResult{RequestID: request.RequestID, Tool: request.Tool, Status: ActionStarted, Detail: detail}
}

// CompletedResult mirrors “ToolResult.completed“.
func CompletedResult(request ActionRequest, detail map[string]any) ToolResult {
	return ToolResult{RequestID: request.RequestID, Tool: request.Tool, Status: ActionCompleted, Detail: detail}
}

// FailedResult mirrors “ToolResult.failed“.
func FailedResult(request ActionRequest, reason string, detail map[string]any) ToolResult {
	return ToolResult{
		RequestID: request.RequestID,
		Tool:      request.Tool,
		Status:    ActionFailed,
		Reason:    reason,
		Detail:    detail,
	}
}

// ToMap mirrors “ToolResult.to_dict“: the detail map is merged at the top
// level rather than nested under “detail“.
func (t ToolResult) ToMap() map[string]any {
	payload := map[string]any{
		"tool":       t.Tool,
		"request_id": t.RequestID,
		"status":     string(t.Status),
	}
	if t.Reason != "" {
		payload["reason"] = t.Reason
	}
	for key, value := range CleanMap(t.Detail) {
		payload[key] = value
	}
	return payload
}
