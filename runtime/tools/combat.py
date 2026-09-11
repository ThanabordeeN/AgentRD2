"""Deferred combat tools.

These are mapped for completeness, but the MVP should keep them disabled or
low-priority in the prompt unless story/combat context explicitly requires
them.
"""
from __future__ import annotations

from typing import Any, Dict, Optional

from runtime.schemas import ToolResult
from runtime.tools.registry import ToolContext, ToolRegistry, ToolSpec


TARGET_SCHEMA: Dict[str, Any] = {
    "type": "object",
    "properties": {
        "entity": {"type": "string"},
        "position": {
            "type": "array",
            "items": {"type": "number"},
            "minItems": 3,
            "maxItems": 3,
        },
        "duration": {"type": "number", "minimum": 0.1, "maximum": 30.0, "default": 3.0},
    },
    "additionalProperties": False,
}

ATTACK_SCHEMA: Dict[str, Any] = {
    "type": "object",
    "properties": {"entity": {"type": "string"}},
    "required": ["entity"],
    "additionalProperties": False,
}

COVER_SCHEMA: Dict[str, Any] = {
    "type": "object",
    "properties": {
        "from_entity": {"type": "string"},
        "from_position": {
            "type": "array",
            "items": {"type": "number"},
            "minItems": 3,
            "maxItems": 3,
        },
        "duration": {"type": "number", "minimum": 0.1, "maximum": 30.0, "default": 5.0},
    },
    "additionalProperties": False,
}


def aim_at(
    context: ToolContext,
    entity: Optional[str] = None,
    position: Optional[list[float]] = None,
    duration: float = 3.0,
) -> ToolResult:
    args: Dict[str, Any] = {"duration": duration}
    if entity is not None:
        args["entity"] = entity
    if position is not None:
        args["position"] = position
    return context.dispatch_action("aim_at", args)


def shoot_at(
    context: ToolContext,
    entity: Optional[str] = None,
    position: Optional[list[float]] = None,
    duration: float = 0.5,
) -> ToolResult:
    args: Dict[str, Any] = {"duration": duration}
    if entity is not None:
        args["entity"] = entity
    if position is not None:
        args["position"] = position
    return context.dispatch_action("shoot_at", args)


def attack(context: ToolContext, entity: str) -> ToolResult:
    return context.dispatch_action("attack", {"entity": entity})


def take_cover(
    context: ToolContext,
    from_entity: Optional[str] = None,
    from_position: Optional[list[float]] = None,
    duration: float = 5.0,
) -> ToolResult:
    args: Dict[str, Any] = {"duration": duration}
    if from_entity is not None:
        args["from_entity"] = from_entity
    if from_position is not None:
        args["from_position"] = from_position
    return context.dispatch_action("take_cover", args)


def register_combat_tools(registry: ToolRegistry) -> None:
    registry.register(ToolSpec(
        "aim_at",
        "Aim a weapon at an entity or position (deferred combat).",
        TARGET_SCHEMA,
        aim_at,
        "combat",
    ))
    registry.register(ToolSpec(
        "shoot_at",
        "Shoot at an entity or position (deferred combat).",
        TARGET_SCHEMA,
        shoot_at,
        "combat",
    ))
    registry.register(ToolSpec(
        "attack",
        "Attack an entity (deferred combat).",
        ATTACK_SCHEMA,
        attack,
        "combat",
    ))
    registry.register(ToolSpec(
        "take_cover",
        "Take cover from an entity or position (deferred combat).",
        COVER_SCHEMA,
        take_cover,
        "combat",
    ))
