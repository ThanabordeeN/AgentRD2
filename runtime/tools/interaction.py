"""Interaction tools: mount, dismount, item/animal interaction, horse action."""
from __future__ import annotations

from typing import Any, Dict, Optional

from runtime.schemas import ToolResult
from runtime.tools.registry import ToolContext, ToolRegistry, ToolSpec


ENTITY_SCHEMA: Dict[str, Any] = {
    "type": "object",
    "properties": {"entity": {"type": "string"}},
    "required": ["entity"],
    "additionalProperties": False,
}

EMPTY_SCHEMA: Dict[str, Any] = {"type": "object", "properties": {}, "additionalProperties": False}

ITEM_INTERACTION_SCHEMA: Dict[str, Any] = {
    "type": "object",
    "properties": {
        "item": {"type": "string"},
        "interaction": {"type": "string"},
    },
    "required": ["item", "interaction"],
    "additionalProperties": False,
}

ANIMAL_INTERACTION_SCHEMA: Dict[str, Any] = {
    "type": "object",
    "properties": {
        "target": {"type": "string"},
        "interaction_type": {"type": "string"},
        "interaction_model": {"type": "string"},
        "skip_idle_animation": {"type": "boolean", "default": False},
    },
    "required": ["target", "interaction_type", "interaction_model"],
    "additionalProperties": False,
}

HORSE_ACTION_SCHEMA: Dict[str, Any] = {
    "type": "object",
    "properties": {
        "action": {"type": "integer"},
        "target": {"type": "string"},
    },
    "required": ["action"],
    "additionalProperties": False,
}


def mount(context: ToolContext, entity: str) -> ToolResult:
    return context.dispatch_action("mount", {"entity": entity})


def dismount(context: ToolContext) -> ToolResult:
    return context.dispatch_action("dismount", {})


def item_interaction(context: ToolContext, item: str, interaction: str) -> ToolResult:
    return context.dispatch_action("item_interaction", {"item": item, "interaction": interaction})


def animal_interaction(
    context: ToolContext,
    target: str,
    interaction_type: str,
    interaction_model: str,
    skip_idle_animation: bool = False,
) -> ToolResult:
    return context.dispatch_action(
        "animal_interaction",
        {
            "target": target,
            "interaction_type": interaction_type,
            "interaction_model": interaction_model,
            "skip_idle_animation": skip_idle_animation,
        },
    )


def horse_action(context: ToolContext, action: int, target: Optional[str] = None) -> ToolResult:
    args: Dict[str, Any] = {"action": int(action)}
    if target is not None:
        args["target"] = target
    return context.dispatch_action("horse_action", args)


def register_interaction_tools(registry: ToolRegistry) -> None:
    registry.register(ToolSpec(
        "mount",
        "Mount a horse or other rideable animal.",
        ENTITY_SCHEMA,
        mount,
        "interaction",
    ))
    registry.register(ToolSpec(
        "dismount",
        "Dismount the currently mounted animal.",
        EMPTY_SCHEMA,
        dismount,
        "interaction",
    ))
    registry.register(ToolSpec(
        "item_interaction",
        "Perform a scripted item interaction.",
        ITEM_INTERACTION_SCHEMA,
        item_interaction,
        "interaction",
    ))
    registry.register(ToolSpec(
        "animal_interaction",
        "Perform a scripted interaction with an animal.",
        ANIMAL_INTERACTION_SCHEMA,
        animal_interaction,
        "interaction",
    ))
    registry.register(ToolSpec(
        "horse_action",
        "Perform a scripted horse action.",
        HORSE_ACTION_SCHEMA,
        horse_action,
        "interaction",
    ))
