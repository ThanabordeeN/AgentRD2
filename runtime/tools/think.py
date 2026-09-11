"""Internal thinking / waiting gestures."""
from __future__ import annotations

from typing import Any, Dict, Optional

from runtime.schemas import ToolResult
from runtime.tools.registry import ToolContext, ToolRegistry, ToolSpec


THINK_SCHEMA: Dict[str, Any] = {
    "type": "object",
    "properties": {
        "style": {
            "type": "string",
            "enum": ["neutral", "think", "ponder", "listen", "alert", "scheme"],
            "default": "think",
        },
        "duration": {"type": "number", "minimum": 0.2, "maximum": 30.0, "default": 2.5},
    },
    "additionalProperties": False,
}


def think(
    context: ToolContext,
    style: str = "think",
    duration: float = 2.5,
) -> ToolResult:
    """Play a short thinking/waiting gesture.

    The bridge maps ``style`` to a suitable animation or emote.  This is a
    transient action: it is meant to be replaced by the real decision when
    the LLM or TTS finishes.
    """
    return context.dispatch_action("think", {"style": style, "duration": duration})


def register_think_tools(registry: ToolRegistry) -> None:
    registry.register(ToolSpec(
        "think",
        "Play a short thinking/listening/waiting gesture while a decision or speech is being prepared.",
        THINK_SCHEMA,
        think,
        "thinking",
    ))
