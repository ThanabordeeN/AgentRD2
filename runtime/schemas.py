"""Canonical data structures shared by the RDR2 Living NPC runtime.

This module intentionally depends only on the Python standard library so the
runtime can be developed, tested, and debugged without the game bridge.
"""
from __future__ import annotations

from dataclasses import dataclass, field, asdict
from enum import Enum
from typing import Any, Dict, Iterable, List, Optional
import time
import uuid


# ---------------------------------------------------------------------------
# Utility helpers
# ---------------------------------------------------------------------------

def utc_timestamp() -> float:
    """Return a Unix timestamp with sub-second precision."""
    return time.time()


def new_event_id() -> str:
    return "evt_" + uuid.uuid4().hex[:12]


def _clean_none(value: Any) -> Any:
    """Recursively drop ``None`` values from JSON-like structures."""
    if isinstance(value, dict):
        return {k: _clean_none(v) for k, v in value.items() if v is not None}
    if isinstance(value, list):
        return [_clean_none(v) for v in value]
    return value


# ---------------------------------------------------------------------------
# Event schema
# ---------------------------------------------------------------------------

@dataclass
class GameTime:
    day: int
    hour: int
    minute: int

    def to_dict(self) -> Dict[str, int]:
        return {"day": self.day, "hour": self.hour, "minute": self.minute}

    @classmethod
    def from_dict(cls, value: Optional[Dict[str, Any]]) -> Optional["GameTime"]:
        if not value:
            return None
        return cls(
            day=int(value.get("day", 0)),
            hour=int(value.get("hour", 0)),
            minute=int(value.get("minute", 0)),
        )


@dataclass
class Location:
    region: Optional[str] = None
    position: Optional[List[float]] = None

    def to_dict(self) -> Dict[str, Any]:
        return _clean_none({"region": self.region, "position": self.position})

    @classmethod
    def from_dict(cls, value: Optional[Dict[str, Any]]) -> Optional["Location"]:
        if not value:
            return None
        position = value.get("position")
        if position is not None:
            position = [float(p) for p in position]
        return cls(region=value.get("region"), position=position)


@dataclass
class Event:
    """One immutable entry in an NPC's JSONL timeline.

    ``data`` is deliberately free-form.  The bridge/normalizer must store
    observable facts, not interpretations (see spec section 12).
    """

    seq: int
    event_name: str
    npc_id: str
    event_id: str = field(default_factory=new_event_id)
    timestamp: float = field(default_factory=utc_timestamp)
    game_time: Optional[GameTime] = None
    location: Optional[Location] = None
    entities: List[str] = field(default_factory=list)
    data: Dict[str, Any] = field(default_factory=dict)
    summary: Optional[str] = None
    importance: Optional[float] = None
    tags: List[str] = field(default_factory=list)

    def __post_init__(self) -> None:
        if not self.event_name:
            raise ValueError("event_name is required")
        if not self.npc_id:
            raise ValueError("npc_id is required")
        if self.seq < 1:
            raise ValueError("seq must be >= 1")
        if self.importance is not None:
            self.importance = max(0.0, min(1.0, float(self.importance)))

    def to_dict(self) -> Dict[str, Any]:
        payload: Dict[str, Any] = {
            "seq": self.seq,
            "event_id": self.event_id,
            "event_name": self.event_name,
            "timestamp": self.timestamp,
            "npc_id": self.npc_id,
            "entities": list(self.entities),
            "data": _clean_none(self.data),
            "tags": list(self.tags),
        }
        if self.game_time is not None:
            payload["game_time"] = self.game_time.to_dict()
        if self.location is not None:
            payload["location"] = self.location.to_dict()
        if self.summary is not None:
            payload["summary"] = self.summary
        if self.importance is not None:
            payload["importance"] = self.importance
        return payload

    @classmethod
    def from_dict(cls, value: Dict[str, Any]) -> "Event":
        return cls(
            seq=int(value["seq"]),
            event_name=str(value["event_name"]),
            npc_id=str(value["npc_id"]),
            event_id=str(value.get("event_id") or new_event_id()),
            timestamp=float(value.get("timestamp") or utc_timestamp()),
            game_time=GameTime.from_dict(value.get("game_time")),
            location=Location.from_dict(value.get("location")),
            entities=list(value.get("entities") or []),
            data=dict(value.get("data") or {}),
            summary=value.get("summary"),
            importance=value.get("importance"),
            tags=list(value.get("tags") or []),
        )


def new_event(
    *,
    seq: int,
    event_name: str,
    npc_id: str,
    data: Optional[Dict[str, Any]] = None,
    entities: Optional[Iterable[str]] = None,
    tags: Optional[Iterable[str]] = None,
    summary: Optional[str] = None,
    importance: Optional[float] = None,
    game_time: Optional[GameTime] = None,
    location: Optional[Location] = None,
    timestamp: Optional[float] = None,
    event_id: Optional[str] = None,
) -> Event:
    return Event(
        seq=seq,
        event_name=event_name,
        npc_id=npc_id,
        event_id=event_id or new_event_id(),
        timestamp=timestamp if timestamp is not None else utc_timestamp(),
        game_time=game_time,
        location=location,
        entities=list(entities or []),
        data=dict(data or {}),
        summary=summary,
        importance=importance,
        tags=list(tags or []),
    )


# ---------------------------------------------------------------------------
# World state / bridge snapshots
# ---------------------------------------------------------------------------

@dataclass
class WorldState:
    npc_id: str
    self_state: Dict[str, Any] = field(default_factory=dict)
    player: Dict[str, Any] = field(default_factory=dict)
    nearby_peds: List[Dict[str, Any]] = field(default_factory=list)
    nearby_horses: List[Dict[str, Any]] = field(default_factory=list)
    recent_game_events: List[Dict[str, Any]] = field(default_factory=list)
    timestamp: float = field(default_factory=utc_timestamp)
    raw: Dict[str, Any] = field(default_factory=dict)

    def to_dict(self) -> Dict[str, Any]:
        return {
            "self": dict(self.self_state),
            "player": dict(self.player),
            "nearby_peds": [dict(p) for p in self.nearby_peds],
            "nearby_horses": [dict(h) for h in self.nearby_horses],
            "recent_game_events": [dict(e) for e in self.recent_game_events],
            "timestamp": self.timestamp,
        }

    @classmethod
    def from_bridge_payload(cls, npc_id: str, payload: Dict[str, Any]) -> "WorldState":
        state = payload.get("state") or {}
        return cls(
            npc_id=npc_id,
            timestamp=float(payload.get("timestamp") or utc_timestamp()),
            self_state=dict(state.get("self") or {}),
            player=dict(state.get("player") or {}),
            nearby_peds=list(state.get("nearby_peds") or []),
            nearby_horses=list(state.get("nearby_horses") or []),
            recent_game_events=list(state.get("recent_game_events") or []),
            raw=dict(payload),
        )


@dataclass
class PedSnapshot:
    """The subset of a ped used by the conservative eligibility gate."""

    entity_id: str
    model: str = ""
    name: str = ""
    # ``None`` means the bridge did not provide a reliable value.  The
    # eligibility gate fails closed on any uncertain safety field.
    is_ped: Optional[bool] = None
    is_human: Optional[bool] = None
    is_alive: Optional[bool] = None
    is_player: Optional[bool] = None
    is_story_character: Optional[bool] = None
    is_mission_owned: Optional[bool] = None
    in_scripted_state: Optional[bool] = None
    in_cutscene: Optional[bool] = None
    blacklisted: Optional[bool] = None
    distance_m: Optional[float] = None
    visible: bool = False
    health: Optional[float] = None
    metadata: Dict[str, Any] = field(default_factory=dict)

    def to_dict(self) -> Dict[str, Any]:
        return asdict(self)

    @classmethod
    def from_dict(cls, value: Dict[str, Any]) -> "PedSnapshot":
        known = {f for f in cls.__dataclass_fields__}  # type: ignore[attr-defined]
        return cls(**{k: v for k, v in value.items() if k in known})


# ---------------------------------------------------------------------------
# Actions / tool result contracts
# ---------------------------------------------------------------------------

class ActionStatus(str, Enum):
    STARTED = "started"
    COMPLETED = "completed"
    FAILED = "failed"


@dataclass
class ActionRequest:
    tool: str
    arguments: Dict[str, Any] = field(default_factory=dict)
    request_id: str = field(default_factory=lambda: "act_" + uuid.uuid4().hex[:12])
    npc_id: Optional[str] = None

    def to_dict(self) -> Dict[str, Any]:
        payload = {
            "tool": self.tool,
            "request_id": self.request_id,
            "arguments": _clean_none(self.arguments),
        }
        if self.npc_id:
            payload["npc_id"] = self.npc_id
        return payload


@dataclass
class ToolResult:
    request_id: str
    tool: str
    status: ActionStatus
    reason: Optional[str] = None
    detail: Dict[str, Any] = field(default_factory=dict)

    @classmethod
    def started(cls, request: ActionRequest, **detail: Any) -> "ToolResult":
        return cls(
            request_id=request.request_id,
            tool=request.tool,
            status=ActionStatus.STARTED,
            detail=detail,
        )

    @classmethod
    def completed(cls, request: ActionRequest, **detail: Any) -> "ToolResult":
        return cls(
            request_id=request.request_id,
            tool=request.tool,
            status=ActionStatus.COMPLETED,
            detail=detail,
        )

    @classmethod
    def failed(cls, request: ActionRequest, reason: str, **detail: Any) -> "ToolResult":
        return cls(
            request_id=request.request_id,
            tool=request.tool,
            status=ActionStatus.FAILED,
            reason=reason,
            detail=detail,
        )

    def to_dict(self) -> Dict[str, Any]:
        payload: Dict[str, Any] = {
            "tool": self.tool,
            "request_id": self.request_id,
            "status": self.status.value,
        }
        if self.reason:
            payload["reason"] = self.reason
        if self.detail:
            payload.update(_clean_none(self.detail))
        return payload


# ---------------------------------------------------------------------------
# Agent context / decision
# ---------------------------------------------------------------------------

@dataclass
class AgentContext:
    npc_id: str
    profile: Dict[str, Any]
    world_state: Dict[str, Any]
    current_goal: Optional[str] = None
    current_mood: str = "neutral"
    recent_events: List[Dict[str, Any]] = field(default_factory=list)
    retrieved_events: List[Dict[str, Any]] = field(default_factory=list)
    wiki_context: List[Dict[str, Any]] = field(default_factory=list)
    quest_context: Dict[str, Any] = field(default_factory=dict)
    available_tools: List[str] = field(default_factory=list)
    flags: Dict[str, Any] = field(default_factory=dict)

    def to_dict(self) -> Dict[str, Any]:
        return {
            "npc_id": self.npc_id,
            "profile": self.profile,
            "world_state": self.world_state,
            "current_goal": self.current_goal,
            "current_mood": self.current_mood,
            "recent_events": self.recent_events,
            "retrieved_events": self.retrieved_events,
            "wiki_context": self.wiki_context,
            "quest_context": self.quest_context,
            "available_tools": self.available_tools,
            "flags": self.flags,
        }


@dataclass
class AgentSpeech:
    text: str
    target: Optional[str] = None
    emotion: Optional[str] = None

    def to_dict(self) -> Dict[str, Any]:
        return _clean_none(
            {"text": self.text, "target": self.target, "emotion": self.emotion}
        )


@dataclass
class AgentAction:
    tool: str
    arguments: Dict[str, Any] = field(default_factory=dict)

    def to_dict(self) -> Dict[str, Any]:
        return {"tool": self.tool, "arguments": _clean_none(self.arguments)}


@dataclass
class AgentDecision:
    """Structured output produced by an agent backend.

    ``speech=None`` means silence.  ``actions=[]`` means do nothing.
    """

    goal: Optional[str] = None
    mood: Optional[str] = None
    speech: Optional[AgentSpeech] = None
    actions: List[AgentAction] = field(default_factory=list)
    internal: Dict[str, Any] = field(default_factory=dict)

    def to_dict(self) -> Dict[str, Any]:
        return _clean_none(
            {
                "internal": {
                    "goal": self.goal,
                    "mood": self.mood,
                    **self.internal,
                },
                "speech": self.speech.to_dict() if self.speech else None,
                "actions": [a.to_dict() for a in self.actions],
            }
        )
