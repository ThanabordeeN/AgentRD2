"""High-level agent tool registry.

LLM agents see only these stable, high-level tools.  Raw native task hashes
are never exposed.
"""
from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Callable, Dict, List, Optional, Protocol

from runtime.schemas import ActionRequest, ToolResult


class ActionDispatcher(Protocol):
    """Sends a high-level action to the RDR2 bridge."""

    def dispatch(self, request: ActionRequest) -> ToolResult:  # pragma: no cover - protocol
        ...


@dataclass
class ToolSpec:
    name: str
    description: str
    parameters: Dict[str, Any]
    handler: Callable[..., Any]
    category: str = "general"

    def as_dict(self) -> Dict[str, Any]:
        return {
            "name": self.name,
            "description": self.description,
            "parameters": self.parameters,
        }


@dataclass
class ToolContext:
    npc_id: str
    world: Any
    timeline: Any
    dispatcher: ActionDispatcher
    wiki: Any = None
    profile: Optional[Dict[str, Any]] = None
    extra: Dict[str, Any] = field(default_factory=dict)

    def get_world_state(self) -> Dict[str, Any]:
        return self.world.get_dict(self.npc_id)

    def grab_timeline(self, **filters: Any) -> List[Dict[str, Any]]:
        events = self.timeline.grab_timeline(self.npc_id, **filters)
        return [event.to_dict() for event in events]

    def dispatch_action(self, tool: str, arguments: Optional[Dict[str, Any]] = None) -> ToolResult:
        request = ActionRequest(tool=tool, arguments=arguments or {}, npc_id=self.npc_id)
        return self.dispatcher.dispatch(request)


class ToolRegistry:
    def __init__(self) -> None:
        self._tools: Dict[str, ToolSpec] = {}

    def register(self, spec: ToolSpec) -> None:
        if spec.name in self._tools:
            raise ValueError(f"tool already registered: {spec.name}")
        self._tools[spec.name] = spec

    def get(self, name: str) -> ToolSpec:
        try:
            return self._tools[name]
        except KeyError as exc:
            raise KeyError(f"unknown agent tool: {name}") from exc

    def names(self) -> List[str]:
        return sorted(self._tools)

    def describe(self) -> List[Dict[str, Any]]:
        return [self._tools[name].as_dict() for name in self.names()]

    def call(self, name: str, context: ToolContext, **arguments: Any) -> Any:
        spec = self.get(name)
        return spec.handler(context, **arguments)
