package tools

import "github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"

// interactionEntitySchema mirrors “ENTITY_SCHEMA“ in “interaction.py“.
var interactionEntitySchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"entity": map[string]any{"type": "string"},
	},
	"required":             []any{"entity"},
	"additionalProperties": false,
}

// interactionEmptySchema mirrors “EMPTY_SCHEMA“ in “interaction.py“.
var interactionEmptySchema = map[string]any{
	"type":                 "object",
	"properties":           map[string]any{},
	"additionalProperties": false,
}

// itemInteractionSchema mirrors “ITEM_INTERACTION_SCHEMA“.
var itemInteractionSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"item":        map[string]any{"type": "string"},
		"interaction": map[string]any{"type": "string"},
	},
	"required":             []any{"item", "interaction"},
	"additionalProperties": false,
}

// animalInteractionSchema mirrors “ANIMAL_INTERACTION_SCHEMA“.
var animalInteractionSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"target":              map[string]any{"type": "string"},
		"interaction_type":    map[string]any{"type": "string"},
		"interaction_model":   map[string]any{"type": "string"},
		"skip_idle_animation": map[string]any{"type": "boolean", "default": false},
	},
	"required":             []any{"target", "interaction_type", "interaction_model"},
	"additionalProperties": false,
}

// horseActionSchema mirrors “HORSE_ACTION_SCHEMA“.
var horseActionSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"action": map[string]any{"type": "integer"},
		"target": map[string]any{"type": "string"},
	},
	"required":             []any{"action"},
	"additionalProperties": false,
}

// mount mirrors “runtime/tools/interaction.py::mount“.
func mount(ctx *Context, args map[string]any) domain.ToolResult {
	entity, ok := stringArg(args, "entity")
	if !ok {
		return failedCall("mount", ctx, args, "missing required argument: entity")
	}
	return ctx.DispatchAction("mount", map[string]any{"entity": entity})
}

// dismount mirrors “dismount“.
func dismount(ctx *Context, args map[string]any) domain.ToolResult {
	return ctx.DispatchAction("dismount", map[string]any{})
}

// itemInteraction mirrors “item_interaction“.
func itemInteraction(ctx *Context, args map[string]any) domain.ToolResult {
	item, ok := stringArg(args, "item")
	if !ok {
		return failedCall("item_interaction", ctx, args, "missing required argument: item")
	}
	interaction, ok := stringArg(args, "interaction")
	if !ok {
		return failedCall("item_interaction", ctx, args, "missing required argument: interaction")
	}
	return ctx.DispatchAction("item_interaction", map[string]any{"item": item, "interaction": interaction})
}

// animalInteraction mirrors “animal_interaction“. Python always forwards
// “skip_idle_animation“ (default false).
func animalInteraction(ctx *Context, args map[string]any) domain.ToolResult {
	target, ok := stringArg(args, "target")
	if !ok {
		return failedCall("animal_interaction", ctx, args, "missing required argument: target")
	}
	interactionType, ok := stringArg(args, "interaction_type")
	if !ok {
		return failedCall("animal_interaction", ctx, args, "missing required argument: interaction_type")
	}
	interactionModel, ok := stringArg(args, "interaction_model")
	if !ok {
		return failedCall("animal_interaction", ctx, args, "missing required argument: interaction_model")
	}
	skipIdle := false
	if value, ok := raw(args, "skip_idle_animation"); ok {
		skipIdle = domain.BoolFrom(value)
	}
	return ctx.DispatchAction("animal_interaction", map[string]any{
		"target":              target,
		"interaction_type":    interactionType,
		"interaction_model":   interactionModel,
		"skip_idle_animation": skipIdle,
	})
}

// horseAction mirrors “horse_action“: Python coerces the action with
// “int(action)“ and only forwards a target when one was supplied.
func horseAction(ctx *Context, args map[string]any) domain.ToolResult {
	action, ok := intArg(args, "action")
	if !ok {
		return failedCall("horse_action", ctx, args, "missing required argument: action")
	}
	out := map[string]any{"action": action}
	if target, ok := stringArg(args, "target"); ok {
		out["target"] = target
	}
	return ctx.DispatchAction("horse_action", out)
}

// RegisterInteractionTools registers the interaction family (5 tools).
func RegisterInteractionTools(registry *Registry) {
	_ = registry.Register(Spec{
		Name:        "mount",
		Description: "Mount a horse or other rideable animal.",
		Parameters:  interactionEntitySchema,
		Category:    "interaction",
		Handler:     mount,
	})
	_ = registry.Register(Spec{
		Name:        "dismount",
		Description: "Dismount the currently mounted animal.",
		Parameters:  interactionEmptySchema,
		Category:    "interaction",
		Handler:     dismount,
	})
	_ = registry.Register(Spec{
		Name:        "item_interaction",
		Description: "Perform a scripted item interaction.",
		Parameters:  itemInteractionSchema,
		Category:    "interaction",
		Handler:     itemInteraction,
	})
	_ = registry.Register(Spec{
		Name:        "animal_interaction",
		Description: "Perform a scripted interaction with an animal.",
		Parameters:  animalInteractionSchema,
		Category:    "interaction",
		Handler:     animalInteraction,
	})
	_ = registry.Register(Spec{
		Name:        "horse_action",
		Description: "Perform a scripted horse action.",
		Parameters:  horseActionSchema,
		Category:    "interaction",
		Handler:     horseAction,
	})
}
