"""High-level tools exposed to the agent."""
from runtime.tools.registry import ActionDispatcher, ToolContext, ToolRegistry, ToolSpec
from runtime.tools.attention import register_attention_tools
from runtime.tools.combat import register_combat_tools
from runtime.tools.interaction import register_interaction_tools
from runtime.tools.lore import register_lore_tools
from runtime.tools.movement import register_movement_tools
from runtime.tools.reaction import register_reaction_tools
from runtime.tools.speech import register_speech_tools
from runtime.tools.think import register_think_tools
from runtime.tools.timeline import register_timeline_tools
from runtime.tools.world import register_world_tools


def build_default_registry(*, include_deferred: bool = True) -> ToolRegistry:
    registry = ToolRegistry()
    register_world_tools(registry)
    register_timeline_tools(registry)
    register_lore_tools(registry)
    register_movement_tools(registry)
    register_attention_tools(registry)
    register_speech_tools(registry)
    register_think_tools(registry)
    register_reaction_tools(registry)
    register_interaction_tools(registry)
    if include_deferred:
        register_combat_tools(registry)
    return registry


__all__ = [
    "ActionDispatcher",
    "ToolContext",
    "ToolRegistry",
    "ToolSpec",
    "build_default_registry",
]
