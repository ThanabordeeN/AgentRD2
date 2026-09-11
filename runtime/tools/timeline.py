"""Explicit deterministic timeline retrieval: grab_timeline()."""
from __future__ import annotations

from typing import Any, Dict

from runtime.tools.registry import ToolContext, ToolRegistry, ToolSpec


GRAB_TIMELINE_SCHEMA: Dict[str, Any] = {
    "type": "object",
    "properties": {
        "event_name": {"type": "string"},
        "event_names": {"type": "array", "items": {"type": "string"}},
        "tag": {"type": "string"},
        "tags": {"type": "array", "items": {"type": "string"}},
        "entity": {"type": "string"},
        "minimum_importance": {"type": "number", "minimum": 0.0, "maximum": 1.0},
        "since_seq": {"type": "integer", "minimum": 0},
        "before_seq": {"type": "integer", "minimum": 1},
        "limit": {"type": "integer", "minimum": 1, "maximum": 200, "default": 20},
    },
    "additionalProperties": False,
}


def grab_timeline(context: ToolContext, **filters: Any) -> Dict[str, Any]:
    return {"events": context.grab_timeline(**filters)}


def register_timeline_tools(registry: ToolRegistry) -> None:
    registry.register(
        ToolSpec(
            name="grab_timeline",
            description=(
                "Retrieve older named events from this NPC's JSONL timeline. "
                "Use deterministic filters; no semantic search exists in the MVP."
            ),
            parameters=GRAB_TIMELINE_SCHEMA,
            handler=grab_timeline,
            category="perception",
        )
    )
