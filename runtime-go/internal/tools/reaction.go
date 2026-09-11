package tools

import "github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"

// reactionPositionSchema mirrors “POSITION_SCHEMA“.
var reactionPositionSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"position": map[string]any{
			"type":     "array",
			"items":    map[string]any{"type": "number"},
			"minItems": 3,
			"maxItems": 3,
		},
	},
	"required":             []any{"position"},
	"additionalProperties": false,
}

// fleeSchema mirrors “FLEE_SCHEMA“ (also used by walk_away).
var fleeSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"entity": map[string]any{"type": "string"},
	},
	"required":             []any{"entity"},
	"additionalProperties": false,
}

// waitSchema mirrors “WAIT_SCHEMA“.
var waitSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"duration": map[string]any{"type": "number", "minimum": 0.0, "maximum": 120.0},
	},
	"required":             []any{"duration"},
	"additionalProperties": false,
}

// reactSchema mirrors “REACT_SCHEMA“.
var reactSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"entity": map[string]any{"type": "string"},
		"position": map[string]any{
			"type":     "array",
			"items":    map[string]any{"type": "number"},
			"minItems": 3,
			"maxItems": 3,
		},
		"reaction": map[string]any{"type": "string"},
	},
	"additionalProperties": false,
}

// durationSchema mirrors “DURATION_SCHEMA“.
var durationSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"duration": map[string]any{"type": "number", "minimum": 0.2, "maximum": 30.0},
	},
	"additionalProperties": false,
}

// handsUpSchema mirrors “HANDS_UP_SCHEMA“.
var handsUpSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"duration": map[string]any{
			"type": "number", "minimum": 0.2, "maximum": 30.0, "default": 3.0,
		},
		"face_entity": map[string]any{"type": "string"},
	},
	"additionalProperties": false,
}

// reactionEmptySchema mirrors “EMPTY_SCHEMA“ in “reaction.py“.
var reactionEmptySchema = map[string]any{
	"type":                 "object",
	"properties":           map[string]any{},
	"additionalProperties": false,
}

// cowerSchema mirrors “COWER_SCHEMA“.
var cowerSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"duration": map[string]any{
			"type": "number", "minimum": 0.2, "maximum": 30.0, "default": 3.0,
		},
		"from_entity": map[string]any{"type": "string"},
	},
	"additionalProperties": false,
}

// investigate mirrors “runtime/tools/reaction.py::investigate“. The agent
// layer falls back to the NPC's most recent known position when the model
// omits one; that fallback needs timeline scanning and stays in the agent
// port, so the tool layer reports a missing position.
func investigate(ctx *Context, args map[string]any) domain.ToolResult {
	position, ok := positionArg(args, "position")
	if !ok {
		return failedCall("investigate", ctx, args, "missing required argument: position")
	}
	return ctx.DispatchAction("investigate", map[string]any{"position": position})
}

// fleeFrom mirrors “flee_from“. The agent layer defaults a missing entity to
// "player".
func fleeFrom(ctx *Context, args map[string]any) domain.ToolResult {
	entity, ok := stringArg(args, "entity")
	if !ok {
		entity = "player"
	}
	return ctx.DispatchAction("flee_from", map[string]any{"entity": entity})
}

// wait mirrors “wait“. The Python agent layer fills “duration“ with 2.0
// and falls back to it when the value is not numeric.
func wait(ctx *Context, args map[string]any) domain.ToolResult {
	duration, ok := numberArg(args, "duration")
	if !ok {
		duration = 2.0
	}
	return ctx.DispatchAction("wait", map[string]any{"duration": duration})
}

// react mirrors “react“: every argument is optional and only supplied values
// are forwarded.
func react(ctx *Context, args map[string]any) domain.ToolResult {
	out := map[string]any{}
	if entity, ok := stringArg(args, "entity"); ok {
		out["entity"] = entity
	}
	if position, ok := raw(args, "position"); ok {
		out["position"] = position
	}
	if reaction, ok := stringArg(args, "reaction"); ok {
		out["reaction"] = reaction
	}
	return ctx.DispatchAction("react", out)
}

// handsUp mirrors “hands_up“ (duration defaults to 3.0).
func handsUp(ctx *Context, args map[string]any) domain.ToolResult {
	duration, ok := numberArg(args, "duration")
	if !ok {
		duration = 3.0
	}
	out := map[string]any{"duration": duration}
	if faceEntity, ok := stringArg(args, "face_entity"); ok {
		out["face_entity"] = faceEntity
	}
	return ctx.DispatchAction("hands_up", out)
}

// cower mirrors “cower“ (duration defaults to 3.0).
func cower(ctx *Context, args map[string]any) domain.ToolResult {
	duration, ok := numberArg(args, "duration")
	if !ok {
		duration = 3.0
	}
	out := map[string]any{"duration": duration}
	if fromEntity, ok := stringArg(args, "from_entity"); ok {
		out["from_entity"] = fromEntity
	}
	return ctx.DispatchAction("cower", out)
}

// duck mirrors “duck“ (duration defaults to 2.0; the schema has no default).
func duck(ctx *Context, args map[string]any) domain.ToolResult {
	duration, ok := numberArg(args, "duration")
	if !ok {
		duration = 2.0
	}
	return ctx.DispatchAction("duck", map[string]any{"duration": duration})
}

// jump mirrors “jump“.
func jump(ctx *Context, args map[string]any) domain.ToolResult {
	return ctx.DispatchAction("jump", map[string]any{})
}

// walkAway mirrors “walk_away“ (entity is required, with no agent-level
// default).
func walkAway(ctx *Context, args map[string]any) domain.ToolResult {
	entity, ok := stringArg(args, "entity")
	if !ok {
		return failedCall("walk_away", ctx, args, "missing required argument: entity")
	}
	return ctx.DispatchAction("walk_away", map[string]any{"entity": entity})
}

// RegisterReactionTools registers the reaction family (9 tools).
func RegisterReactionTools(registry *Registry) {
	_ = registry.Register(Spec{
		Name:        "investigate",
		Description: "Move toward a position to investigate it.",
		Parameters:  reactionPositionSchema,
		Category:    "reaction",
		Handler:     investigate,
	})
	_ = registry.Register(Spec{
		Name:        "flee_from",
		Description: "Move away from an entity in a panic/flee state.",
		Parameters:  fleeSchema,
		Category:    "reaction",
		Handler:     fleeFrom,
	})
	_ = registry.Register(Spec{
		Name:        "wait",
		Description: "Wait for a short duration before the next decision.",
		Parameters:  waitSchema,
		Category:    "reaction",
		Handler:     wait,
	})
	_ = registry.Register(Spec{
		Name:        "react",
		Description: "React physically to an entity or position.",
		Parameters:  reactSchema,
		Category:    "reaction",
		Handler:     react,
	})
	_ = registry.Register(Spec{
		Name:        "hands_up",
		Description: "Raise hands in surrender or fear.",
		Parameters:  handsUpSchema,
		Category:    "reaction",
		Handler:     handsUp,
	})
	_ = registry.Register(Spec{
		Name:        "cower",
		Description: "Cower away from a threat.",
		Parameters:  cowerSchema,
		Category:    "reaction",
		Handler:     cower,
	})
	_ = registry.Register(Spec{
		Name:        "duck",
		Description: "Duck down defensively for a short time.",
		Parameters:  durationSchema,
		Category:    "reaction",
		Handler:     duck,
	})
	_ = registry.Register(Spec{
		Name:        "jump",
		Description: "Jump or startle in place.",
		Parameters:  reactionEmptySchema,
		Category:    "reaction",
		Handler:     jump,
	})
	_ = registry.Register(Spec{
		Name:        "walk_away",
		Description: "Walk away from an entity without panic.",
		Parameters:  fleeSchema,
		Category:    "reaction",
		Handler:     walkAway,
	})
}
