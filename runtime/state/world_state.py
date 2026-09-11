"""In-memory world state for active NPCs.

The bridge owns the authoritative simulation.  The runtime only keeps the
latest snapshot needed to build an agent prompt and validate actions.
"""
from __future__ import annotations

from typing import Any, Dict, List, Optional
import copy

from runtime.schemas import Event, WorldState


class WorldStateStore:
    def __init__(self) -> None:
        self._states: Dict[str, WorldState] = {}
        self._recent_events: Dict[str, List[Dict[str, Any]]] = {}

    def update(self, state: WorldState) -> None:
        self._states[state.npc_id] = state

    def get(self, npc_id: str) -> WorldState:
        if npc_id not in self._states:
            self._states[npc_id] = WorldState(npc_id=npc_id)
        return self._states[npc_id]

    def get_dict(self, npc_id: str) -> Dict[str, Any]:
        payload = self.get(npc_id).to_dict()
        # Merge timeline-backed recent events so get_world_state() is a
        # single current picture even when the bridge sent no event list.
        timeline_events = self.recent_game_events(npc_id, limit=20)
        if timeline_events:
            payload["recent_game_events"] = timeline_events
        return copy.deepcopy(payload)

    def apply_event(self, event: Event, max_recent: int = 50) -> None:
        bucket = self._recent_events.setdefault(event.npc_id, [])
        bucket.append(event.to_dict())
        if len(bucket) > max_recent:
            del bucket[:-max_recent]

    def recent_game_events(self, npc_id: str, limit: int = 20) -> List[Dict[str, Any]]:
        return list(self._recent_events.get(npc_id, []))[-limit:]

    def set_self_field(self, npc_id: str, key: str, value: Any) -> None:
        self.get(npc_id).self_state[key] = value

    def set_player_field(self, npc_id: str, key: str, value: Any) -> None:
        self.get(npc_id).player[key] = value

    def remove(self, npc_id: str) -> None:
        self._states.pop(npc_id, None)
        self._recent_events.pop(npc_id, None)
