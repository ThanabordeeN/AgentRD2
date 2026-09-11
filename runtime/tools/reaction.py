"""Reaction tools: investigate, flee, wait, react, hands_up, cower, duck, jump."""
from __future__ import annotations

from typing import Any, Dict, Optional

from runtime.schemas import ToolResult
from runtime.tools.registry import ToolContext, ToolRegistry, ToolSpec


POSITION_SCHEMA: Dict[str, Any] = {
    "type": "object",
    "properties": {
        "position": {
            "type": "array",
            "items": {"type": "number"},
            "minItems": 3,
            "maxItems": 3,
        }
    },
    "required": ["position"],
    "additionalProperties": False,
}

FLEE_SCHEMA: Dict[str, Any] = {
    "type": "object",
    "properties": {"entity": {"type": "string"}},
    "required": ["entity"],
    "additionalProperties": False,
}

WAIT_SCHEMA: Dict[str, Any] = {
    "type": "object",
    "properties": {"duration": {"type": "number", "minimum": 0.0, "maximum": 120.0}},
    "required": ["duration"],
    "additionalProperties": False,
}

REACT_SCHEMA: Dict[str, Any] = {
    "type": "object",
    "properties": {
        "entity": {"type": "string"},
        "position": {
            "type": "array",
            "items": {"type": "number"},
            "minItems": 3,
            "maxItems": 3,
        },
        "reaction": {"type": "string"},
    },
    "additionalProperties": False,
}

DURATION_SCHEMA: Dict[str, Any] = {
    "type": "object",
    "properties": {"duration": {"type": "number", "minimum": 0.2, "maximum": 30.0}},
    "additionalProperties": False,
}

HANDS_UP_SCHEMA: Dict[str, Any] = {
    "type": "object",
    "properties": {
        "duration": {"type": "number", "minimum": 0.2, "maximum": 30.0, "default": 3.0},
        "face_entity": {"type": "string"},
    },
    "additionalProperties": False,
}

EMPTY_SCHEMA: Dict[str, Any] = {"type": "object", "properties": {}, "additionalProperties": False}

COWER_SCHEMA: Dict[str, Any] = {
    "type": "object",
    "properties": {
        "duration": {"type": "number", "minimum": 0.2, "maximum": 30.0, "default": 3.0},
        "from_entity": {"type": "string"},
    },
    "additionalProperties": False,
}


def investigate(context: ToolContext, position: list[float]) -> ToolResult:
    return context.dispatch_action("investigate", {"position": position})


def flee_from(context: ToolContext, entity: str) -> ToolResult:
    return context.dispatch_action("flee_from", {"entity": entity})


def wait(context: ToolContext, duration: float) -> ToolResult:
    return context.dispatch_action("wait", {"duration": duration})


def react(
    context: ToolContext,
    entity: Optional[str] = None,
    position: Optional[list[float]] = None,
    reaction: Optional[str] = None,
) -> ToolResult:
    args: Dict[str, Any] = {}
    if entity is not None:
        args["entity"] = entity
    if position is not None:
        args["position"] = position
    if reaction is not None:
        args["reaction"] = reaction
    return context.dispatch_action("react", args)


def hands_up(
    context: ToolContext,
    duration: float = 3.0,
    face_entity: Optional[str] = None,
) -> ToolResult:
    args: Dict[str, Any] = {"duration": duration}
    if face_entity is not None:
        args["face_entity"] = face_entity
    return context.dispatch_action("hands_up", args)


def cower(
    context: ToolContext,
    duration: float = 3.0,
    from_entity: Optional[str] = None,
) -> ToolResult:
    args: Dict[str, Any] = {"duration": duration}
    if from_entity is not None:
        args["from_entity"] = from_entity
    return context.dispatch_action("cower", args)


def duck(context: ToolContext, duration: float = 2.0) -> ToolResult:
    return context.dispatch_action("duck", {"duration": duration})


def jump(context: ToolContext) -> ToolResult:
    return context.dispatch_action("jump", {})


def walk_away(context: ToolContext, entity: str) -> ToolResult:
    return context.dispatch_action("walk_away", {"entity": entity})


def register_reaction_tools(registry: ToolRegistry) -> None:
    registry.register(ToolSpec(
        "investigate",
        "Move toward a position to investigate it.",
        POSITION_SCHEMA,
        investigate,
        "reaction",
    ))
    registry.register(ToolSpec(
        "flee_from",
        "Move away from an entity in a panic/flee state.",
        FLEE_SCHEMA,
        flee_from,
        "reaction",
    ))
    registry.register(ToolSpec(
        "wait",
        "Wait for a short duration before the next decision.",
        WAIT_SCHEMA,
        wait,
        "reaction",
    ))
    registry.register(ToolSpec(
        "react",
        "React physically to an entity or position.",
        REACT_SCHEMA,
        react,
        "reaction",
    ))
    registry.register(ToolSpec(
        "hands_up",
        "Raise hands in surrender or fear.",
        HANDS_UP_SCHEMA,
        hands_up,
        "reaction",
    ))
    registry.register(ToolSpec(
        "cower",
        "Cower away from a threat.",
        COWER_SCHEMA,
        cower,
        "reaction",
    ))
    registry.register(ToolSpec(
        "duck",
        "Duck down defensively for a short time.",
        DURATION_SCHEMA,
        duck,
        "reaction",
    ))
    registry.register(ToolSpec(
        "jump",
        "Jump or startle in place.",
        EMPTY_SCHEMA,
        jump,
        "reaction",
    ))
    registry.register(ToolSpec(
        "walk_away",
        "Walk away from an entity without panic.",
        FLEE_SCHEMA,
        walk_away,
        "reaction",
    ))
