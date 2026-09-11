"""High-level movement tools.

The bridge maps these intention-level commands to RDR2 tasks/natives.
"""
from __future__ import annotations

from typing import Any, Dict, Optional

from runtime.schemas import ToolResult
from runtime.tools.registry import ToolContext, ToolRegistry, ToolSpec


DESTINATION_SCHEMA = {
    "oneOf": [
        {"type": "string"},
        {
            "type": "object",
            "properties": {
                "region": {"type": "string"},
                "position": {
                    "type": "array",
                    "items": {"type": "number"},
                    "minItems": 3,
                    "maxItems": 3,
                },
            },
            "additionalProperties": False,
        },
    ]
}

WANDER_SCHEMA: Dict[str, Any] = {
    "type": "object",
    "properties": {
        "radius": {"type": "number", "minimum": 0.5, "maximum": 100.0},
    },
    "required": ["radius"],
    "additionalProperties": False,
}

GO_TO_SCHEMA: Dict[str, Any] = {
    "type": "object",
    "properties": {
        "destination": DESTINATION_SCHEMA,
        "priority": {"type": "string", "enum": ["low", "normal", "high"]},
    },
    "required": ["destination"],
    "additionalProperties": False,
}

FOLLOW_SCHEMA: Dict[str, Any] = {
    "type": "object",
    "properties": {
        "entity": {"type": "string"},
        "distance": {"type": "number", "minimum": 0.5, "maximum": 50.0},
    },
    "required": ["entity"],
    "additionalProperties": False,
}

STOP_SCHEMA: Dict[str, Any] = {
    "type": "object",
    "properties": {},
    "additionalProperties": False,
}


def _delegate(context: ToolContext, tool: str, arguments: Dict[str, Any]) -> ToolResult:
    return context.dispatch_action(tool, arguments)


def wander(context: ToolContext, radius: float) -> ToolResult:
    return _delegate(context, "wander", {"radius": radius})


def go_to(context: ToolContext, destination: Any, priority: Optional[str] = None) -> ToolResult:
    args: Dict[str, Any] = {"destination": destination}
    if priority is not None:
        args["priority"] = priority
    return _delegate(context, "go_to", args)


def follow(context: ToolContext, entity: str, distance: Optional[float] = None) -> ToolResult:
    args: Dict[str, Any] = {"entity": entity}
    if distance is not None:
        args["distance"] = distance
    return _delegate(context, "follow", args)


def stop(context: ToolContext) -> ToolResult:
    return _delegate(context, "stop", {})


def register_movement_tools(registry: ToolRegistry) -> None:
    registry.register(ToolSpec(
        "wander",
        "Wander around the current area within a radius.",
        WANDER_SCHEMA,
        wander,
        "movement",
    ))
    registry.register(ToolSpec(
        "go_to",
        "Walk or ride to a named or positional destination using RDR2 navigation.",
        GO_TO_SCHEMA,
        go_to,
        "movement",
    ))
    registry.register(ToolSpec(
        "follow",
        "Follow an entity at a comfortable distance.",
        FOLLOW_SCHEMA,
        follow,
        "movement",
    ))
    registry.register(ToolSpec(
        "stop",
        "Stop the current movement action.",
        STOP_SCHEMA,
        stop,
        "movement",
    ))
