package tools

import "github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"

// getWorldStateSchema mirrors “GET_WORLD_STATE_SCHEMA“.
var getWorldStateSchema = map[string]any{
	"type":                 "object",
	"properties":           map[string]any{},
	"additionalProperties": false,
}

// getWorldState mirrors “runtime/tools/world.py::get_world_state“. The
// Python handler returns the world dict itself; “_call_tool“ then wraps it as
// “ToolResult.completed(request, result=<dict>)“, which is what
// completedData reproduces.
func getWorldState(ctx *Context, args map[string]any) domain.ToolResult {
	if worldOf(ctx) == nil {
		return failedCall("get_world_state", ctx, args, "world reader is not configured")
	}
	return completedData("get_world_state", ctx, args, ctx.GetWorldState())
}

// RegisterWorldTools registers the perception tool “get_world_state“.
func RegisterWorldTools(registry *Registry) {
	_ = registry.Register(Spec{
		Name:        "get_world_state",
		Description: "Return the NPC's current relevant world state.",
		Parameters:  getWorldStateSchema,
		Category:    "perception",
		Handler:     getWorldState,
	})
}
