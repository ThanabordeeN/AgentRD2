"""Speech and gesture tools."""
from __future__ import annotations

from typing import Any, Dict, Optional

from runtime.schemas import ToolResult
from runtime.tools.registry import ToolContext, ToolRegistry, ToolSpec


SAY_SCHEMA: Dict[str, Any] = {
    "type": "object",
    "properties": {
        "text": {"type": "string", "minLength": 1, "maxLength": 500},
        "target": {"type": "string"},
        "emotion": {"type": "string"},
    },
    "required": ["text"],
    "additionalProperties": False,
}

GESTURE_SCHEMA: Dict[str, Any] = {
    "type": "object",
    "properties": {
        "type": {
            "type": "string",
            "enum": [
                "neutral", "friendly", "annoyed", "afraid",
                "wave", "nod", "shrug", "think", "ponder", "scheme",
            ],
        },
    },
    "required": ["type"],
    "additionalProperties": False,
}


def say(
    context: ToolContext,
    text: str,
    target: Optional[str] = None,
    emotion: Optional[str] = None,
) -> ToolResult:
    args: Dict[str, Any] = {"text": text}
    if target is not None:
        args["target"] = target
    if emotion is not None:
        args["emotion"] = emotion
    return context.dispatch_action("say", args)


def gesture(context: ToolContext, type: str) -> ToolResult:
    return context.dispatch_action("gesture", {"type": type})


def register_speech_tools(registry: ToolRegistry) -> None:
    registry.register(ToolSpec(
        "say",
        "Speak a short line through the TTS pipeline.",
        SAY_SCHEMA,
        say,
        "speech",
    ))
    registry.register(ToolSpec(
        "gesture",
        "Play a generic gesture animation.",
        GESTURE_SCHEMA,
        gesture,
        "speech",
    ))
