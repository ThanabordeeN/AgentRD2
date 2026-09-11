"""Attention and look-at tools (soft takeover)."""
from __future__ import annotations

from typing import Any, Dict

from runtime.schemas import ToolResult
from runtime.tools.registry import ToolContext, ToolRegistry, ToolSpec


ENTITY_SCHEMA: Dict[str, Any] = {
    "type": "object",
    "properties": {"entity": {"type": "string"}},
    "required": ["entity"],
    "additionalProperties": False,
}

EMPTY_SCHEMA: Dict[str, Any] = {"type": "object", "properties": {}, "additionalProperties": False}


def look_at(context: ToolContext, entity: str) -> ToolResult:
    return context.dispatch_action("look_at", {"entity": entity})


def face(context: ToolContext, entity: str) -> ToolResult:
    return context.dispatch_action("face", {"entity": entity})


def clear_attention(context: ToolContext) -> ToolResult:
    return context.dispatch_action("clear_attention", {})


def register_attention_tools(registry: ToolRegistry) -> None:
    registry.register(ToolSpec(
        "look_at",
        "Look at an entity without preventing other actions.",
        ENTITY_SCHEMA,
        look_at,
        "attention",
    ))
    registry.register(ToolSpec(
        "face",
        "Turn the body/head toward an entity.",
        ENTITY_SCHEMA,
        face,
        "attention",
    ))
    registry.register(ToolSpec(
        "clear_attention",
        "Stop looking/attending to the current target.",
        EMPTY_SCHEMA,
        clear_attention,
        "attention",
    ))
