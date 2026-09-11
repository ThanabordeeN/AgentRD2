"""World lore / Red Dead Wiki context tools."""
from __future__ import annotations

from typing import Any, Dict, Optional

from runtime.tools.registry import ToolContext, ToolRegistry, ToolSpec


GET_WORLD_LORE_SCHEMA: Dict[str, Any] = {
    "type": "object",
    "properties": {
        "topic": {
            "type": "string",
            "description": "Location, faction, activity, or event to look up.",
        }
    },
    "additionalProperties": False,
}


def get_world_lore(context: ToolContext, topic: Optional[str] = None) -> Dict[str, Any]:
    wiki = context.wiki
    if wiki is None:
        return {"lore": []}
    if topic:
        return {"lore": wiki.lookup(topic)}
    return {"lore": wiki.context_for_profile(context.profile or {})}


def register_lore_tools(registry: ToolRegistry) -> None:
    registry.register(ToolSpec(
        "get_world_lore",
        "Look up offline Red Dead Wiki lore/context for a topic or for this NPC.",
        GET_WORLD_LORE_SCHEMA,
        get_world_lore,
        "perception",
    ))
