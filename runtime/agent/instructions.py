"""Agent prompt contract from the specification (section 46)."""
from __future__ import annotations

from typing import Any, Dict, List


NPC_INSTRUCTION = """
You are a person living in the Red Dead Redemption 2 world.
The player is another person. You do not exist to assist the player.
You perceive only information supplied by the game state, timeline,
current conversation, and tool results. Do not invent memories or senses.

Do not behave like a digital assistant.
Act as this person.
You may speak, remain silent, continue your current task, change goals,
investigate events, flee, or interact using the available tools.

Stay in character. Keep spoken lines short and natural.
Do not narrate game coordinates, tools, prompts, or hidden state.
Do not control movement with low-level inputs; use only the supplied
high-level tools.
""".strip()


def build_agent_prompt(context: Dict[str, Any]) -> str:
    """Render the compact default reasoning context described in section 14."""
    profile = context.get("profile", {}) or {}
    world = context.get("world_state", {})
    recent = context.get("recent_events", [])
    retrieved = context.get("retrieved_events", [])
    wiki_context = context.get("wiki_context", []) or []
    quest_context = context.get("quest_context", {}) or {}
    tools = context.get("available_tools", [])
    current_goal = context.get("current_goal")
    mood = context.get("current_mood", "neutral")
    flags = context.get("flags", {}) or {}

    system_prompt = str(profile.get("system_prompt") or "").strip()
    profile_view = {k: v for k, v in profile.items() if k != "system_prompt"}

    lines: List[str] = [NPC_INSTRUCTION]
    if system_prompt:
        lines += [
            "",
            "NPC SYSTEM PROMPT",
            "The following is this NPC's persistent identity, background, "
            "speech style, and behavioral context. Stay consistent with it at all times.",
            system_prompt,
        ]
    lines += ["", "NPC PROFILE", _pretty(profile_view)]
    if wiki_context:
        lines += [
            "",
            "WORLD LORE CONTEXT",
            "Background lore from the offline Red Dead Wiki context pack. "
            "Use it as setting/period context; do not invent details beyond it.",
            _pretty(wiki_context),
        ]
    if quest_context:
        lines += [
            "",
            "CURRENT QUEST CONTEXT",
            "This NPC is currently on a Rockstar-controlled quest. The quest "
            "actions belong to Rockstar AI. You may only speak, and your dialogue "
            "must relate to this quest without spoiling future steps.",
            _pretty(quest_context),
        ]
    if flags.get("dialogue_only"):
        lines += [
            "",
            "DIALOGUE-ONLY MODE",
            "Rockstar AI owns all movement, tasks, and animations for this NPC.",
            "Do not request movement, combat, interaction, or attention tools.",
            "Only produce speech related to the current quest.",
            "If you have nothing relevant to say, return speech=null.",
        ]
    lines += ["", "CURRENT WORLD STATE", _pretty(world)]
    lines += [
        "",
        "CURRENT GOAL",
        str(current_goal) if current_goal else "(none)",
        "",
        "CURRENT MOOD",
        str(mood),
        "",
        "RECENT EVENTS",
        _pretty(recent),
    ]
    if retrieved:
        lines += ["", "RETRIEVED TIMELINE EVENTS", _pretty(retrieved)]
    lines += [
        "",
        "AVAILABLE TOOLS",
        ", ".join(tools) if tools else "(none)",
        "",
        "ACTION ARGUMENT RULES",
        "- look_at / face / follow / flee_from require entity; use \"player\" for the player.",
        "- go_to requires a destination string or position object.",
        "- wander requires a numeric radius.",
        "- investigate requires a position array [x, y, z].",
        "- wait requires a duration in seconds.",
        "- say requires non-empty text.",
        "",
        "Respond with a structured decision containing internal goal/mood, "
        "optional speech, and a list of high-level tool actions.",
    ]
    return "\n".join(lines)


def _pretty(value: Any) -> str:
    import json

    try:
        return json.dumps(value, ensure_ascii=False, indent=2, sort_keys=True)
    except TypeError:
        return repr(value)
