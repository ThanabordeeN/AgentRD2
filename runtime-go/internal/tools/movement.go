package tools

import "github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"

// movementDestinationSchema mirrors “DESTINATION_SCHEMA“.
var movementDestinationSchema = map[string]any{
	"oneOf": []any{
		map[string]any{"type": "string"},
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"region": map[string]any{"type": "string"},
				"position": map[string]any{
					"type":     "array",
					"items":    map[string]any{"type": "number"},
					"minItems": 3,
					"maxItems": 3,
				},
			},
			"additionalProperties": false,
		},
	},
}

// wanderSchema mirrors “WANDER_SCHEMA“.
var wanderSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"radius": map[string]any{"type": "number", "minimum": 0.5, "maximum": 100.0},
	},
	"required":             []any{"radius"},
	"additionalProperties": false,
}

// goToSchema mirrors “GO_TO_SCHEMA“.
var goToSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"destination": movementDestinationSchema,
		"priority":    map[string]any{"type": "string", "enum": []any{"low", "normal", "high"}},
	},
	"required":             []any{"destination"},
	"additionalProperties": false,
}

// followSchema mirrors “FOLLOW_SCHEMA“.
var followSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"entity":   map[string]any{"type": "string"},
		"distance": map[string]any{"type": "number", "minimum": 0.5, "maximum": 50.0},
	},
	"required":             []any{"entity"},
	"additionalProperties": false,
}

// stopSchema mirrors “STOP_SCHEMA“.
var stopSchema = map[string]any{
	"type":                 "object",
	"properties":           map[string]any{},
	"additionalProperties": false,
}

// wander mirrors “runtime/tools/movement.py::wander“. The Python agent layer
// fills “radius“ with 8.0 and falls back to it when the value is not
// numeric, so this handler never fails on a missing radius.
func wander(ctx *Context, args map[string]any) domain.ToolResult {
	radius, ok := numberArg(args, "radius")
	if !ok {
		radius = 8.0
	}
	return ctx.DispatchAction("wander", map[string]any{"radius": radius})
}

// goTo mirrors “go_to“: destination is required, priority is only forwarded
// when supplied. Python's agent-level destination fallback needs the decision
// context and stays in the agent port.
func goTo(ctx *Context, args map[string]any) domain.ToolResult {
	destination, ok := raw(args, "destination")
	if !ok {
		return failedCall("go_to", ctx, args, "missing required argument: destination")
	}
	out := map[string]any{"destination": destination}
	if priority, ok := stringArg(args, "priority"); ok {
		out["priority"] = priority
	}
	return ctx.DispatchAction("go_to", out)
}

// follow mirrors “follow“. The agent layer defaults a missing entity to
// "player".
func follow(ctx *Context, args map[string]any) domain.ToolResult {
	entity, ok := stringArg(args, "entity")
	if !ok {
		entity = "player"
	}
	out := map[string]any{"entity": entity}
	if distance, ok := numberArg(args, "distance"); ok {
		out["distance"] = distance
	}
	return ctx.DispatchAction("follow", out)
}

// stop mirrors “stop“.
func stop(ctx *Context, args map[string]any) domain.ToolResult {
	return ctx.DispatchAction("stop", map[string]any{})
}

// RegisterMovementTools registers the movement family (4 tools).
func RegisterMovementTools(registry *Registry) {
	_ = registry.Register(Spec{
		Name:        "wander",
		Description: "Wander around the current area within a radius.",
		Parameters:  wanderSchema,
		Category:    "movement",
		Handler:     wander,
	})
	_ = registry.Register(Spec{
		Name:        "go_to",
		Description: "Walk or ride to a named or positional destination using RDR2 navigation.",
		Parameters:  goToSchema,
		Category:    "movement",
		Handler:     goTo,
	})
	_ = registry.Register(Spec{
		Name:        "follow",
		Description: "Follow an entity at a comfortable distance.",
		Parameters:  followSchema,
		Category:    "movement",
		Handler:     follow,
	})
	_ = registry.Register(Spec{
		Name:        "stop",
		Description: "Stop the current movement action.",
		Parameters:  stopSchema,
		Category:    "movement",
		Handler:     stop,
	})
}
