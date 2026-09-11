package tools

import "github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"

// attentionEntitySchema mirrors “ENTITY_SCHEMA“ in “attention.py“.
var attentionEntitySchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"entity": map[string]any{"type": "string"},
	},
	"required":             []any{"entity"},
	"additionalProperties": false,
}

// attentionEmptySchema mirrors “EMPTY_SCHEMA“ in “attention.py“.
var attentionEmptySchema = map[string]any{
	"type":                 "object",
	"properties":           map[string]any{},
	"additionalProperties": false,
}

// lookAt mirrors “runtime/tools/attention.py::look_at“. The agent layer
// defaults a missing entity to "player".
func lookAt(ctx *Context, args map[string]any) domain.ToolResult {
	entity, ok := stringArg(args, "entity")
	if !ok {
		entity = "player"
	}
	return ctx.DispatchAction("look_at", map[string]any{"entity": entity})
}

// face mirrors “face“.
func face(ctx *Context, args map[string]any) domain.ToolResult {
	entity, ok := stringArg(args, "entity")
	if !ok {
		entity = "player"
	}
	return ctx.DispatchAction("face", map[string]any{"entity": entity})
}

// clearAttention mirrors “clear_attention“.
func clearAttention(ctx *Context, args map[string]any) domain.ToolResult {
	return ctx.DispatchAction("clear_attention", map[string]any{})
}

// RegisterAttentionTools registers the attention family (3 tools).
func RegisterAttentionTools(registry *Registry) {
	_ = registry.Register(Spec{
		Name:        "look_at",
		Description: "Look at an entity without preventing other actions.",
		Parameters:  attentionEntitySchema,
		Category:    "attention",
		Handler:     lookAt,
	})
	_ = registry.Register(Spec{
		Name:        "face",
		Description: "Turn the body/head toward an entity.",
		Parameters:  attentionEntitySchema,
		Category:    "attention",
		Handler:     face,
	})
	_ = registry.Register(Spec{
		Name:        "clear_attention",
		Description: "Stop looking/attending to the current target.",
		Parameters:  attentionEmptySchema,
		Category:    "attention",
		Handler:     clearAttention,
	})
}
