"""Perception tools: get_world_state()."""
from __future__ import annotations

from typing import Any, Dict

from runtime.tools.registry import ToolContext, ToolRegistry, ToolSpec


GET_WORLD_STATE_SCHEMA: Dict[str, Any] = {
    "type": "object",
    "properties": {},
    "additionalProperties": False,
}


def get_world_state(context: ToolContext) -> Dict[str, Any]:
    return context.get_world_state()


def register_world_tools(registry: ToolRegistry) -> None:
    registry.register(
        ToolSpec(
            name="get_world_state",
            description="Return the NPC's current relevant world state.",
            parameters=GET_WORLD_STATE_SCHEMA,
            handler=get_world_state,
            category="perception",
        )
    )
