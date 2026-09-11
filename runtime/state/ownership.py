"""NPC ownership state machine.

Rockstar AI remains the default owner.  The runtime only takes temporary
ownership when the eligibility gate and contextual triggers permit it.
"""
from __future__ import annotations

from dataclasses import dataclass, field
from enum import Enum
from typing import Dict, List, Optional
import time


class OwnershipState(str, Enum):
    ROCKSTAR = "ROCKSTAR"
    CANDIDATE = "CANDIDATE"
    AWARE = "AWARE"
    AI_ACTIVE = "AI_ACTIVE"
    AI_CONVERSATION = "AI_CONVERSATION"
    QUEST_DIALOGUE = "QUEST_DIALOGUE"
    SUSPENDED = "SUSPENDED"
    RELEASE = "RELEASE"


@dataclass
class OwnershipTransition:
    npc_id: str
    from_state: OwnershipState
    to_state: OwnershipState
    reason: str
    timestamp: float = field(default_factory=time.time)

    @property
    def event_name(self) -> Optional[str]:
        # Resume/suspend and release are checked before activation so a
        # suspended -> AI_ACTIVE resume is not mislabeled as a new activation.
        if self.from_state == OwnershipState.SUSPENDED:
            return "AGENT_RESUMED"
        if self.to_state == OwnershipState.QUEST_DIALOGUE:
            return "QUEST_DIALOGUE_ENTERED"
        if self.to_state == OwnershipState.SUSPENDED:
            return "AGENT_SUSPENDED"
        if self.to_state == OwnershipState.RELEASE:
            return "NPC_RELEASED"
        if (
            self.to_state == OwnershipState.AI_ACTIVE
            and self.from_state in {OwnershipState.CANDIDATE, OwnershipState.AWARE}
        ):
            return "NPC_ACTIVATED"
        return None


@dataclass
class OwnershipRecord:
    npc_id: str
    state: OwnershipState = OwnershipState.ROCKSTAR
    previous_state: Optional[OwnershipState] = None
    history: List[OwnershipTransition] = field(default_factory=list)

    def transition(self, to_state: OwnershipState, reason: str) -> OwnershipTransition:
        transition = OwnershipTransition(
            npc_id=self.npc_id,
            from_state=self.state,
            to_state=to_state,
            reason=reason,
        )
        self.state = to_state
        self.history.append(transition)
        return transition


class OwnershipManager:
    """Tracks ownership state for all NPCs known to the runtime."""

    def __init__(self):
        self.records: Dict[str, OwnershipRecord] = {}

    def get(self, npc_id: str) -> OwnershipRecord:
        if npc_id not in self.records:
            self.records[npc_id] = OwnershipRecord(npc_id=npc_id)
        return self.records[npc_id]

    def state(self, npc_id: str) -> OwnershipState:
        return self.get(npc_id).state

    def can_reason(self, npc_id: str) -> bool:
        return self.state(npc_id) in {
            OwnershipState.AWARE,
            OwnershipState.AI_ACTIVE,
            OwnershipState.AI_CONVERSATION,
            OwnershipState.QUEST_DIALOGUE,
        }

    def can_speak(self, npc_id: str) -> bool:
        return self.state(npc_id) in {
            OwnershipState.AI_ACTIVE,
            OwnershipState.AI_CONVERSATION,
            OwnershipState.QUEST_DIALOGUE,
        }

    # ------------------------------------------------------------------
    def update_from_scan(
        self,
        npc_id: str,
        *,
        distance_m: float,
        eligible: bool,
        candidate_distance_m: float = 20.0,
        aware_distance_m: float = 10.0,
        meaningful_trigger: bool = False,
    ) -> Optional[OwnershipTransition]:
        """Apply one proximity/eligibility scan result.

        The caller is responsible for providing a conservative eligibility
        result.  This method never activates a ped by itself unless
        ``meaningful_trigger`` is true and the ped is already ``AWARE``.
        """
        record = self.get(npc_id)

        if record.state == OwnershipState.SUSPENDED:
            return None

        # Story safety / eligibility is always strongest.
        if not eligible and record.state not in {
            OwnershipState.ROCKSTAR,
            OwnershipState.RELEASE,
        }:
            return record.transition(OwnershipState.RELEASE, "eligibility_failed")

        far_away = distance_m > candidate_distance_m
        if far_away and record.state not in {
            OwnershipState.ROCKSTAR,
            OwnershipState.CANDIDATE,
            OwnershipState.RELEASE,
        }:
            return record.transition(OwnershipState.RELEASE, "player_left_area")

        if record.state in {OwnershipState.ROCKSTAR, OwnershipState.RELEASE}:
            if distance_m <= candidate_distance_m and eligible:
                return record.transition(OwnershipState.CANDIDATE, "player_nearby")
            if record.state == OwnershipState.RELEASE:
                return record.transition(OwnershipState.ROCKSTAR, "release_complete")
            return None

        if record.state == OwnershipState.CANDIDATE:
            if distance_m <= aware_distance_m and eligible:
                return record.transition(OwnershipState.AWARE, "eligibility_passed")
            if far_away:
                return record.transition(OwnershipState.ROCKSTAR, "candidate_left_area")
            return None

        if record.state == OwnershipState.AWARE:
            if meaningful_trigger and eligible:
                return record.transition(OwnershipState.AI_ACTIVE, "meaningful_trigger")
            if far_away:
                return record.transition(OwnershipState.RELEASE, "aware_left_area")
            return None

        if record.state == OwnershipState.AI_ACTIVE:
            if far_away:
                return record.transition(OwnershipState.RELEASE, "player_left_area")
            return None

        if record.state == OwnershipState.AI_CONVERSATION:
            if far_away:
                return record.transition(OwnershipState.RELEASE, "player_left_area")
            return None

        return None

    # ------------------------------------------------------------------
    def enter_quest_dialogue(self, npc_id: str) -> Optional[OwnershipTransition]:
        """Allow dialogue-only overlay while Rockstar keeps the action task."""
        record = self.get(npc_id)
        if record.state == OwnershipState.QUEST_DIALOGUE:
            return None
        if record.state in {
            OwnershipState.ROCKSTAR,
            OwnershipState.CANDIDATE,
            OwnershipState.AWARE,
            OwnershipState.AI_ACTIVE,
            OwnershipState.AI_CONVERSATION,
        }:
            return record.transition(OwnershipState.QUEST_DIALOGUE, "quest_dialogue")
        return None

    def push_to_talk(self, npc_id: str) -> Optional[OwnershipTransition]:
        record = self.get(npc_id)
        if record.state in {
            OwnershipState.AWARE,
            OwnershipState.AI_ACTIVE,
            OwnershipState.AI_CONVERSATION,
            OwnershipState.QUEST_DIALOGUE,
        }:
            return record.transition(OwnershipState.AI_CONVERSATION, "push_to_talk")
        return None

    def conversation_ended(self, npc_id: str) -> Optional[OwnershipTransition]:
        record = self.get(npc_id)
        if record.state == OwnershipState.AI_CONVERSATION:
            return record.transition(OwnershipState.AI_ACTIVE, "conversation_ended")
        return None

    def request_release(self, npc_id: str, reason: str = "release_policy") -> Optional[OwnershipTransition]:
        record = self.get(npc_id)
        if record.state == OwnershipState.RELEASE:
            return None
        return record.transition(OwnershipState.RELEASE, reason)

    def finalize_release(self, npc_id: str) -> Optional[OwnershipTransition]:
        record = self.get(npc_id)
        if record.state == OwnershipState.RELEASE:
            return record.transition(OwnershipState.ROCKSTAR, "release_complete")
        return None

    def suspend(self, npc_id: str, reason: str = "story_safety") -> Optional[OwnershipTransition]:
        record = self.get(npc_id)
        if record.state in {OwnershipState.ROCKSTAR, OwnershipState.SUSPENDED}:
            return None
        record.previous_state = record.state
        return record.transition(OwnershipState.SUSPENDED, reason)

    def resume(self, npc_id: str, reason: str = "story_safety_cleared") -> Optional[OwnershipTransition]:
        record = self.get(npc_id)
        if record.state != OwnershipState.SUSPENDED:
            return None
        resume_to = record.previous_state or OwnershipState.AWARE
        if resume_to in {OwnershipState.ROCKSTAR, OwnershipState.RELEASE, OwnershipState.SUSPENDED}:
            resume_to = OwnershipState.AWARE
        record.previous_state = None
        return record.transition(resume_to, reason)
