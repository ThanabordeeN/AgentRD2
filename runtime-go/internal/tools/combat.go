package tools

import "github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"

// targetSchema mirrors “TARGET_SCHEMA“, shared by aim_at and shoot_at.
//
// Note the schema advertises “duration“ 3.0 while “shoot_at“'s Python
// signature defaults to 0.5; the tool handlers preserve that difference.
var targetSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"entity": map[string]any{"type": "string"},
		"position": map[string]any{
			"type":     "array",
			"items":    map[string]any{"type": "number"},
			"minItems": 3,
			"maxItems": 3,
		},
		"duration": map[string]any{
			"type": "number", "minimum": 0.1, "maximum": 30.0, "default": 3.0,
		},
	},
	"additionalProperties": false,
}

// attackSchema mirrors “ATTACK_SCHEMA“.
var attackSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"entity": map[string]any{"type": "string"},
	},
	"required":             []any{"entity"},
	"additionalProperties": false,
}

// coverSchema mirrors “COVER_SCHEMA“.
var coverSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"from_entity": map[string]any{"type": "string"},
		"from_position": map[string]any{
			"type":     "array",
			"items":    map[string]any{"type": "number"},
			"minItems": 3,
			"maxItems": 3,
		},
		"duration": map[string]any{
			"type": "number", "minimum": 0.1, "maximum": 30.0, "default": 5.0,
		},
	},
	"additionalProperties": false,
}

// aimAt mirrors “runtime/tools/combat.py::aim_at“ (duration defaults to 3.0).
func aimAt(ctx *Context, args map[string]any) domain.ToolResult {
	duration, ok := numberArg(args, "duration")
	if !ok {
		duration = 3.0
	}
	out := map[string]any{"duration": duration}
	if entity, ok := stringArg(args, "entity"); ok {
		out["entity"] = entity
	}
	if position, ok := raw(args, "position"); ok {
		out["position"] = position
	}
	return ctx.DispatchAction("aim_at", out)
}

// shootAt mirrors “shoot_at“ (duration defaults to 0.5).
func shootAt(ctx *Context, args map[string]any) domain.ToolResult {
	duration, ok := numberArg(args, "duration")
	if !ok {
		duration = 0.5
	}
	out := map[string]any{"duration": duration}
	if entity, ok := stringArg(args, "entity"); ok {
		out["entity"] = entity
	}
	if position, ok := raw(args, "position"); ok {
		out["position"] = position
	}
	return ctx.DispatchAction("shoot_at", out)
}

// attack mirrors “attack“ (entity required, no default).
func attack(ctx *Context, args map[string]any) domain.ToolResult {
	entity, ok := stringArg(args, "entity")
	if !ok {
		return failedCall("attack", ctx, args, "missing required argument: entity")
	}
	return ctx.DispatchAction("attack", map[string]any{"entity": entity})
}

// takeCover mirrors “take_cover“ (duration defaults to 5.0).
func takeCover(ctx *Context, args map[string]any) domain.ToolResult {
	duration, ok := numberArg(args, "duration")
	if !ok {
		duration = 5.0
	}
	out := map[string]any{"duration": duration}
	if fromEntity, ok := stringArg(args, "from_entity"); ok {
		out["from_entity"] = fromEntity
	}
	if fromPosition, ok := raw(args, "from_position"); ok {
		out["from_position"] = fromPosition
	}
	return ctx.DispatchAction("take_cover", out)
}

// RegisterCombatTools registers the deferred combat family (4 tools). Python
// registers these last, and only when “include_deferred“ is true.
func RegisterCombatTools(registry *Registry) {
	_ = registry.Register(Spec{
		Name:        "aim_at",
		Description: "Aim a weapon at an entity or position (deferred combat).",
		Parameters:  targetSchema,
		Category:    "combat",
		Handler:     aimAt,
	})
	_ = registry.Register(Spec{
		Name:        "shoot_at",
		Description: "Shoot at an entity or position (deferred combat).",
		Parameters:  targetSchema,
		Category:    "combat",
		Handler:     shootAt,
	})
	_ = registry.Register(Spec{
		Name:        "attack",
		Description: "Attack an entity (deferred combat).",
		Parameters:  attackSchema,
		Category:    "combat",
		Handler:     attack,
	})
	_ = registry.Register(Spec{
		Name:        "take_cover",
		Description: "Take cover from an entity or position (deferred combat).",
		Parameters:  coverSchema,
		Category:    "combat",
		Handler:     takeCover,
	})
}
