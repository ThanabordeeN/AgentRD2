"""Convert bridge messages into canonical world state and timeline events."""
from __future__ import annotations

from typing import Any, Dict, Iterable, Optional
import re

from runtime.schemas import Event, GameTime, Location, WorldState, new_event


_EVENT_NAME_RE = re.compile(r"^[A-Z][A-Z0-9_]*$")


class EventNormalizer:
    """Small, dependency-free normalizer for the MVP IPC payloads.

    The normalizer deliberately preserves fact-shaped data.  It does not
    assign moral labels such as ``PLAYER_IS_EVIL`` to observations.
    """

    def world_state_from_message(self, raw: Dict[str, Any]) -> WorldState:
        npc_id = self._require_npc_id(raw)
        return WorldState.from_bridge_payload(npc_id, raw)

    def event_from_message(
        self,
        raw: Dict[str, Any],
        *,
        seq: int,
        default_npc_id: Optional[str] = None,
    ) -> Event:
        """Build an :class:`Event` from a ``game_event`` or similar payload."""
        npc_id = raw.get("npc_id") or default_npc_id
        if not npc_id:
            raise ValueError("game event is missing npc_id")
        npc_id = str(npc_id)

        event_name = raw.get("event_name") or raw.get("name")
        if not event_name:
            raise ValueError("game event is missing event_name")
        event_name = str(event_name).upper()
        if not _EVENT_NAME_RE.match(event_name):
            raise ValueError(f"invalid event_name: {event_name!r}")

        data = raw.get("data") or {}
        if not isinstance(data, dict):
            raise ValueError("event data must be an object")

        entities = raw.get("entities") or []
        tags = raw.get("tags") or []
        if isinstance(entities, str):
            entities = [entities]
        if isinstance(tags, str):
            tags = [tags]

        game_time = None
        if raw.get("game_time"):
            game_time = GameTime.from_dict(raw["game_time"])
        location = None
        if raw.get("location"):
            location = Location.from_dict(raw["location"])

        return new_event(
            seq=seq,
            event_name=event_name,
            npc_id=npc_id,
            data=dict(data),
            entities=list(entities),
            tags=list(tags),
            summary=raw.get("summary"),
            importance=raw.get("importance"),
            game_time=game_time,
            location=location,
            timestamp=raw.get("timestamp"),
            event_id=raw.get("event_id"),
        )

    # -- standard fact events -------------------------------------------
    def player_spoke(self, npc_id: str, text: str, **data: Any) -> Dict[str, Any]:
        return {
            "event_name": "PLAYER_SPOKE",
            "npc_id": npc_id,
            "entities": ["player"],
            "tags": ["player", "speech"],
            "data": {"text": text, **data},
        }

    def npc_spoke(self, npc_id: str, text: str, **data: Any) -> Dict[str, Any]:
        return {
            "event_name": "NPC_SPOKE",
            "npc_id": npc_id,
            "entities": [npc_id],
            "tags": ["npc", "speech"],
            "data": {"text": text, **data},
        }

    def action_result(self, raw: Dict[str, Any]) -> Dict[str, Any]:
        status = str(raw.get("status", "")).lower()
        if status == "completed":
            event_name = "ACTION_COMPLETED"
        elif status == "failed":
            event_name = "ACTION_FAILED"
        else:
            event_name = "ACTION_STARTED"
        data = {
            "tool": raw.get("tool"),
            "request_id": raw.get("request_id"),
            **{k: v for k, v in raw.items() if k not in {"type", "npc_id", "status", "tool", "request_id"}},
        }
        return {
            "event_name": event_name,
            "npc_id": raw.get("npc_id"),
            "tags": ["action"],
            "data": data,
            "summary": raw.get("summary"),
            "importance": raw.get("importance"),
        }

    def goal_event(self, npc_id: str, event_name: str, **data: Any) -> Dict[str, Any]:
        return {
            "event_name": event_name,
            "npc_id": npc_id,
            "tags": ["goal"],
            "data": data,
        }

    # ------------------------------------------------------------------
    @staticmethod
    def _require_npc_id(raw: Dict[str, Any]) -> str:
        npc_id = raw.get("npc_id")
        if not npc_id:
            raise ValueError("message is missing npc_id")
        return str(npc_id)
