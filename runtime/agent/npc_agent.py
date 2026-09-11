"""Runtime orchestration for one or more NPC agents.

This is the event-driven core: bridge messages become timeline facts, facts
trigger ownership changes, and ownership changes wake the configured agent
backend.  Tool actions are validated and sent back to the bridge as a batch.
"""
from __future__ import annotations

from collections import OrderedDict
from dataclasses import dataclass, field
from typing import Any, Callable, Dict, List, Optional, Tuple
import time

from runtime.agent.backends import AgentBackend, RuleBasedAgentBackend
from runtime.config import ProfileStore, load_settings
from runtime.events.normalizer import EventNormalizer
from runtime.lore import WikiContextStore
from runtime.quests import QuestStore
from runtime.schemas import (
    ActionRequest,
    ActionStatus,
    AgentAction,
    AgentContext,
    AgentDecision,
    AgentSpeech,
    Event,
    PedSnapshot,
    ToolResult,
    WorldState,
)
from runtime.state.eligibility import EligibilityGate
from runtime.state.ownership import OwnershipManager, OwnershipState, OwnershipTransition
from runtime.state.world_state import WorldStateStore
from runtime.timeline.store import TimelineStore
from runtime.tools import ToolContext, ToolRegistry, build_default_registry


class RuntimeActionDispatcher:
    """Validates/logs action starts and hands them to the bridge transport."""

    # These actions are short/soft overlays.  They should not block idle
    # planning while waiting for a bridge action_result.
    TRANSIENT_TOOLS = {"think", "gesture", "clear_attention"}

    def __init__(self, timeline: TimelineStore):
        self.timeline = timeline
        self.pending: Dict[str, ActionRequest] = {}
        self._send: Optional[Callable[[Dict[str, Any]], None]] = None

    def set_send(self, send: Optional[Callable[[Dict[str, Any]], None]]) -> None:
        self._send = send

    def dispatch(self, request: ActionRequest) -> ToolResult:
        if request.tool not in self.TRANSIENT_TOOLS:
            self.pending[request.request_id] = request
        if request.npc_id:
            data = {
                "tool": request.tool,
                "request_id": request.request_id,
                "arguments": dict(request.arguments),
            }
            self.timeline.append_event(
                request.npc_id,
                "ACTION_STARTED",
                data=data,
                tags=["action"],
                importance=0.3,
            )
            if request.tool == "say":
                self.timeline.append_event(
                    request.npc_id,
                    "NPC_SPOKE",
                    data={
                        "text": request.arguments.get("text"),
                        "target": request.arguments.get("target"),
                        "emotion": request.arguments.get("emotion"),
                    },
                    entities=[request.npc_id],
                    tags=["npc", "speech"],
                    importance=0.4,
                )
        if self._send is not None:
            self._send(
                {
                    "type": "action_request",
                    "npc_id": request.npc_id,
                    "request": request.to_dict(),
                }
            )
        return ToolResult.started(request)

    def resolve(self, raw: Dict[str, Any]) -> Optional[ActionRequest]:
        request_id = raw.get("request_id")
        if not request_id:
            return None
        return self.pending.pop(str(request_id), None)


@dataclass
class AgentRuntimeState:
    npc_id: str
    current_goal: Optional[str] = None
    mood: str = "neutral"
    conversation_active: bool = False
    last_spoken_at: float = 0.0
    last_decision_at: float = 0.0
    reasoning: bool = False
    dialogue_only: bool = False
    quest_context: Dict[str, Any] = field(default_factory=dict)


class NpcAgentRuntime:
    def __init__(
        self,
        *,
        settings: Optional[Dict[str, Any]] = None,
        timeline: Optional[TimelineStore] = None,
        world: Optional[WorldStateStore] = None,
        profiles: Optional[ProfileStore] = None,
        ownership: Optional[OwnershipManager] = None,
        eligibility: Optional[EligibilityGate] = None,
        backend: Optional[AgentBackend] = None,
        registry: Optional[ToolRegistry] = None,
        dispatcher: Optional[RuntimeActionDispatcher] = None,
        wiki: Optional[WikiContextStore] = None,
    ):
        self.settings = settings or load_settings()
        self.timeline = timeline or TimelineStore(self.settings.get("timelines_dir", "data/timelines"))
        self.world = world or WorldStateStore()
        self.profiles = profiles or ProfileStore(self.settings.get("profiles_dir", "data/profiles"))
        self.ownership = ownership or OwnershipManager()
        self.registry = registry or build_default_registry()
        self.eligibility = eligibility or EligibilityGate.from_config(
            self.settings.get("story_blacklist_path", "config/story_blacklist.json"),
            block_if_uncertain=bool(self.settings.get("story_safety", {}).get("block_if_uncertain", True)),
        )
        self.backend = backend or RuleBasedAgentBackend()
        self.dispatcher = dispatcher or RuntimeActionDispatcher(self.timeline)
        self.wiki = wiki or WikiContextStore(
            self.settings.get("wiki_context_path", "data/wiki/rdr2_context.json"),
            character_path=self.settings.get(
                "character_context_path", "data/wiki/characters.json"
            ),
        )
        self.quests = QuestStore(self.settings.get("quests_dir", "data/quests"))
        self.normalizer = EventNormalizer()
        self.agent_state: Dict[str, AgentRuntimeState] = {}
        self.last_ped_scan: Dict[str, PedSnapshot] = {}
        self._last_global_speech_at = 0.0
        self._deferred_agents: "OrderedDict[str, str]" = OrderedDict()

    # ------------------------------------------------------------------
    # Public bridge message handling
    # ------------------------------------------------------------------
    def handle_message(self, raw: Dict[str, Any]) -> Dict[str, Any]:
        msg_type = str(raw.get("type", "")).lower()
        if msg_type == "world_update":
            return self.handle_world_update(raw)
        if msg_type in {"ped_scan", "nearby_peds"}:
            return self.handle_ped_scan(raw)
        if msg_type in {"game_event", "event"}:
            return self.handle_game_event(raw)
        if msg_type in {"action_result", "action_completed", "action_failed"}:
            return self.handle_action_result(raw)
        if msg_type in {"player_speech", "push_to_talk_transcript"}:
            return self.handle_player_speech(raw)
        if msg_type == "push_to_talk":
            return self.handle_push_to_talk(raw)
        if msg_type in {"story_safety", "mission_state"}:
            return self.handle_story_safety(raw)
        if msg_type in {"hello", "bridge_hello"}:
            return {"type": "hello_ack", "server_time": time.time()}
        return {"type": "error", "reason": f"unknown message type: {msg_type!r}"}

    def handle_world_update(self, raw: Dict[str, Any]) -> Dict[str, Any]:
        try:
            state = self.normalizer.world_state_from_message(raw)
        except ValueError as exc:
            return {"type": "error", "reason": str(exc)}
        self.world.update(state)
        self._maybe_release_from_world_state(state)
        self._maybe_schedule_idle_thinking()
        return {"type": "world_update_ack", "npc_id": state.npc_id}

    def _maybe_release_from_world_state(self, state: WorldState) -> None:
        distance = state.player.get("distance") if isinstance(state.player, dict) else None
        if distance is None:
            return
        try:
            distance_f = float(distance)
        except (TypeError, ValueError):
            return
        candidate_d = float(self.settings.get("activation", {}).get("candidate_distance_m", 20.0))
        if distance_f <= candidate_d:
            return
        transition = self.ownership.update_from_scan(
            state.npc_id,
            distance_m=distance_f,
            eligible=True,
            candidate_distance_m=candidate_d,
            aware_distance_m=float(self.settings.get("activation", {}).get("aware_distance_m", 10.0)),
        )
        if transition is not None:
            self._record_transition(transition)

    def handle_ped_scan(self, raw: Dict[str, Any]) -> Dict[str, Any]:
        activation = self.settings.get("activation", {})
        candidate_d = float(activation.get("candidate_distance_m", 20.0))
        aware_d = float(activation.get("aware_distance_m", 10.0))
        processed = 0
        transitions: List[Dict[str, Any]] = []
        peds = raw.get("peds") or []
        if not isinstance(peds, list):
            return {"type": "error", "reason": "ped_scan.peds must be an array"}
        tracked_limit = int(self.settings.get("scheduler", {}).get("nearby_tracked_limit", 32))
        peds = sorted(
            peds,
            key=lambda p: float((p or {}).get("distance_m") or 0.0)
            if isinstance(p, dict) else 0.0,
        )[: max(0, tracked_limit)]

        for ped_payload in peds:
            if not isinstance(ped_payload, dict):
                continue
            ped = PedSnapshot.from_dict(ped_payload)
            if not ped.entity_id:
                continue
            self.last_ped_scan[ped.entity_id] = ped
            distance = float(ped.distance_m or 0.0)
            quest_transition = self._handle_quest_dialogue_ped(
                ped, distance, candidate_d, aware_d
            )
            if quest_transition is not None:
                transitions.append(quest_transition)
                processed += 1
                continue
            if self._agent_state(ped.entity_id).dialogue_only:
                # Quest dialogue overlay is active; Rockstar keeps all tasks.
                processed += 1
                continue
            eligibility = self.eligibility.can_activate(ped)
            transition = self.ownership.update_from_scan(
                ped.entity_id,
                distance_m=distance,
                eligible=bool(eligibility),
                candidate_distance_m=candidate_d,
                aware_distance_m=aware_d,
            )
            if transition is not None:
                transitions.append(self._record_transition(transition, ped=ped, eligibility_reasons=eligibility.reasons))
                if transition.to_state == OwnershipState.AI_ACTIVE:
                    self.schedule_reasoning(ped.entity_id, "ped_scan_activation")
            processed += 1
        self._maybe_schedule_idle_thinking()
        return {"type": "ped_scan_ack", "processed": processed, "transitions": transitions}

    def handle_game_event(self, raw: Dict[str, Any]) -> Dict[str, Any]:
        npc_id = str(raw.get("npc_id") or self._npc_from_pending(raw) or "")
        if not npc_id:
            return {"type": "error", "reason": "game_event missing npc_id"}
        try:
            event = self.normalizer.event_from_message(
                raw,
                seq=self.timeline.next_seq(npc_id),
                default_npc_id=npc_id,
            )
        except ValueError as exc:
            return {"type": "error", "reason": str(exc)}
        event = self.timeline.append(event)
        self.world.apply_event(event)
        self._react_to_event(event)
        return {"type": "event_ack", "npc_id": npc_id, "seq": event.seq}

    def handle_action_result(self, raw: Dict[str, Any]) -> Dict[str, Any]:
        request = self.dispatcher.resolve(raw)
        npc_id = str(raw.get("npc_id") or (request.npc_id if request else "") or "")
        if not npc_id:
            return {"type": "error", "reason": "action_result missing npc_id and no pending request"}
        event = self.normalizer.action_result({**raw, "npc_id": npc_id})
        try:
            event_obj = self.normalizer.event_from_message(
                event, seq=self.timeline.next_seq(npc_id), default_npc_id=npc_id
            )
        except ValueError as exc:
            return {"type": "error", "reason": str(exc)}
        self.timeline.append(event_obj)
        self.world.apply_event(event_obj)
        status = event_obj.event_name
        if status == "ACTION_FAILED":
            self.schedule_reasoning(npc_id, "action_failed", trigger_event=event_obj)
        return {"type": "action_result_ack", "npc_id": npc_id, "seq": event_obj.seq}

    def handle_player_speech(self, raw: Dict[str, Any]) -> Dict[str, Any]:
        npc_id = str(raw.get("npc_id") or raw.get("target_npc_id") or "")
        text = str(raw.get("text") or "")
        if not npc_id:
            return {"type": "error", "reason": "player_speech missing npc_id"}
        if not text:
            return {"type": "error", "reason": "player_speech missing text"}
        return self.handle_game_event(
            {
                **self.normalizer.player_spoke(npc_id, text),
                "timestamp": raw.get("timestamp"),
                "game_time": raw.get("game_time"),
                "location": raw.get("location"),
            }
        )

    def handle_push_to_talk(self, raw: Dict[str, Any]) -> Dict[str, Any]:
        action = str(raw.get("action", "start")).lower()
        npc_id = str(raw.get("npc_id") or raw.get("target_npc_id") or "")
        if action == "start":
            if not npc_id:
                npc_id = self.select_conversation_target() or ""
            if not npc_id:
                return {"type": "push_to_talk", "target": None, "reason": "no_valid_target"}
            state = self._agent_state(npc_id)
            self._ensure_single_conversation(npc_id)
            if self.ownership.state(npc_id) == OwnershipState.AI_CONVERSATION:
                # Barge-in: stop current TTS/attention and listen again.
                self.timeline.append_event(
                    npc_id,
                    "CONVERSATION_INTERRUPTED",
                    data={"by": "player"},
                    tags=["conversation"],
                    importance=0.4,
                )
            else:
                transition = self.ownership.push_to_talk(npc_id)
                if transition is not None:
                    self._record_transition(transition)
            state.conversation_active = True
            return {"type": "push_to_talk", "action": "start", "target": npc_id}
        if action == "stop":
            if npc_id:
                transition = self.ownership.conversation_ended(npc_id)
                if transition is not None:
                    self._record_transition(transition)
                self._agent_state(npc_id).conversation_active = False
                self.timeline.append_event(
                    npc_id, "CONVERSATION_ENDED", data={"by": "player"}, tags=["conversation"]
                )
            return {"type": "push_to_talk", "action": "stop", "target": npc_id or None}
        return {"type": "error", "reason": f"unknown push_to_talk action: {action!r}"}

    def handle_story_safety(self, raw: Dict[str, Any]) -> Dict[str, Any]:
        """Suspend or resume all non-Rockstar ownership for story safety.

        The bridge is expected to send this conservatively around missions,
        cutscenes, and other protected script states.
        """
        active = bool(raw.get("active", raw.get("in_story", False)))
        npc_id = raw.get("npc_id")
        transitions: List[Dict[str, Any]] = []

        def targets() -> List[str]:
            if npc_id:
                return [str(npc_id)]
            return list(self.ownership.records.keys()) or list(self.agent_state.keys())

        for target in targets():
            if active:
                transition = self.ownership.suspend(target, reason=str(raw.get("reason", "story_safety")))
            else:
                transition = self.ownership.resume(target, reason=str(raw.get("reason", "story_safety_cleared")))
            if transition is not None:
                transitions.append(self._record_transition(transition))
        return {"type": "story_safety_ack", "active": active, "transitions": transitions}

    # ------------------------------------------------------------------
    # Context, reasoning, and actions
    # ------------------------------------------------------------------
    def schedule_reasoning(
        self,
        npc_id: str,
        reason: str = "event",
        *,
        trigger_event: Optional[Event] = None,
    ) -> Optional[AgentDecision]:
        state = self._agent_state(npc_id)
        if state.reasoning:
            return None
        if not self.ownership.can_reason(npc_id):
            return None
        max_agents = int(self.settings.get("scheduler", {}).get("max_reasoning_agents", 3))
        active = sum(1 for s in self.agent_state.values() if s.reasoning)
        if active >= max_agents:
            return None
        state.reasoning = True
        state.last_decision_at = time.time()
        try:
            self._emit_wait_gesture(npc_id, reason, trigger_event)
            context = self.build_context(npc_id, reason=reason, trigger_event=trigger_event)
            tool_context = ToolContext(
                npc_id=npc_id,
                world=self.world,
                timeline=self.timeline,
                dispatcher=self.dispatcher,
                wiki=self.wiki,
                profile=context.profile,
            )
            decision = self._call_backend(context, tool_context)
            if not state.dialogue_only:
                decision = self._apply_behavior_guardrails(npc_id, decision, context)
            decision = self._apply_speech_policy(npc_id, decision, reason=reason)
            self.execute_decision(npc_id, decision)
            state.last_decision_at = time.time()
            return decision
        finally:
            state.reasoning = False

    def build_context(
        self,
        npc_id: str,
        *,
        reason: str = "event",
        trigger_event: Optional[Event] = None,
    ) -> AgentContext:
        state = self._agent_state(npc_id)
        recent = [event.to_dict() for event in self.timeline.recent_events(npc_id, 20)]
        profile = self.profiles.get(npc_id)
        world_state = self.world.get_dict(npc_id)
        flags: Dict[str, Any] = {
            "reason": reason,
            "ownership_state": self.ownership.state(npc_id).value,
            "thinking_mode": self._thinking_mode_for(reason, trigger_event),
        }
        if trigger_event is not None:
            flags["trigger_event"] = trigger_event.to_dict()
        if state.dialogue_only:
            flags["dialogue_only"] = True
        return AgentContext(
            npc_id=npc_id,
            profile=profile,
            world_state=world_state,
            current_goal=state.current_goal,
            current_mood=state.mood,
            recent_events=recent,
            wiki_context=self.wiki.context_for_profile(profile, world_state=world_state),
            quest_context=state.quest_context if state.dialogue_only else {},
            available_tools=self._available_tools_for(npc_id),
            flags=flags,
        )

    def _available_tools_for(self, npc_id: str) -> List[str]:
        state = self._agent_state(npc_id)
        if state.dialogue_only:
            allowed = {"say", "get_world_state", "grab_timeline", "get_world_lore"}
            return [name for name in self.registry.names() if name in allowed]
        return self.registry.names()

    def _wait_gesture_enabled(self) -> bool:
        policy = self.settings.get("thinking_policy", {}) or {}
        return bool(policy.get("wait_gestures", True))

    def _emit_wait_gesture(
        self,
        npc_id: str,
        reason: str,
        trigger_event: Optional[Event],
    ) -> None:
        """Play a short thinking/listening gesture while the LLM/TTS is working.

        This is a soft overlay and is marked as a transient action, so it does
        not block idle planning while the real decision is being generated.
        """
        if not self._wait_gesture_enabled():
            return
        if self._agent_state(npc_id).dialogue_only:
            # Rockstar owns the body; no runtime-injected animations.
            return
        if not getattr(self.backend, "supports_wait_gestures", False):
            return
        if self.ownership.state(npc_id) not in {
            OwnershipState.AI_ACTIVE,
            OwnershipState.AI_CONVERSATION,
        }:
            return
        style = self._wait_gesture_style(reason, trigger_event)
        tool_ctx = ToolContext(
            npc_id, self.world, self.timeline, self.dispatcher,
            wiki=self.wiki, profile=self.profiles.get(npc_id),
        )
        self._call_tool("think", tool_ctx, style=style, duration=2.5)

    @staticmethod
    def _wait_gesture_style(reason: str, trigger_event: Optional[Event]) -> str:
        event_name = trigger_event.event_name if trigger_event is not None else ""
        if event_name == "PLAYER_SPOKE" or reason in {"player_spoke", "push_to_talk"}:
            return "listen"
        if event_name in {
            "GUNSHOT_HEARD",
            "PLAYER_THREATENED_NPC",
            "PLAYER_ATTACKED_NPC",
            "NPC_DAMAGED",
            "FIGHT_STARTED",
        }:
            return "alert"
        if event_name in {"PLAYER_APPROACHED", "PLAYER_LOOKED_AT_NPC"}:
            return "think"
        if reason.startswith("action_failed"):
            return "ponder"
        if reason.startswith("idle"):
            return "ponder"
        return "think"

    def _thinking_mode_for(self, reason: str, trigger_event: Optional[Event]) -> str:
        """Choose low vs disabled reasoning per event using config policy."""
        policy = self.settings.get("thinking_policy", {}) or {}
        default_mode = str(policy.get("default_mode", "low"))

        disabled_reasons = set(policy.get("disabled_for_reasons", []) or [])
        if reason in disabled_reasons:
            return "disabled"
        for prefix in disabled_reasons:
            if prefix and reason.startswith(prefix):
                return "disabled"

        event_name = trigger_event.event_name if trigger_event is not None else ""
        disabled_events = set(policy.get("disabled_for_events", []) or [])
        if event_name in disabled_events:
            return "disabled"

        return default_mode if default_mode in {"low", "disabled"} else "low"

    def _quest_dialogue_payload(self, ped: PedSnapshot) -> Optional[Dict[str, Any]]:
        metadata = ped.metadata or {}
        quest_id = metadata.get("quest_id")
        if quest_id is None:
            quest_id = (self.profiles.get(ped.entity_id) or {}).get("quest_id")
        dialogue_flag = metadata.get("quest_dialogue", metadata.get("dialogue_only", False))
        if not quest_id or not dialogue_flag:
            return None
        # Conservative base checks: never overlay a dead, player, non-human, or
        # cutscene entity.  The quest_dialogue flag only relaxes the
        # mission/story blacklist, not these.
        if ped.is_ped is not True or ped.is_human is not True or ped.is_alive is not True:
            return None
        if ped.is_player is True:
            return None
        if ped.in_cutscene is not False:
            return None
        runtime_state = metadata.get("quest_state") or metadata.get("quest") or {}
        if not isinstance(runtime_state, dict):
            runtime_state = {}
        return self.quests.context_for(str(quest_id), runtime_state)

    def _handle_quest_dialogue_ped(
        self,
        ped: PedSnapshot,
        distance: float,
        candidate_distance_m: float,
        aware_distance_m: float,
    ) -> Optional[Dict[str, Any]]:
        """Allow a dialogue-only overlay for Rockstar-controlled quest NPCs.

        Movement, tasks, and animation remain owned by Rockstar.  Only speech
        from our agent runtime is allowed while the NPC is in range.
        """
        state = self._agent_state(ped.entity_id)
        payload = self._quest_dialogue_payload(ped)

        if not payload:
            if state.dialogue_only:
                reason = (
                    "quest_dialogue_left_area"
                    if distance > candidate_distance_m
                    else "quest_dialogue_ended"
                )
                transition = self.ownership.request_release(ped.entity_id, reason)
                if transition is not None:
                    return self._record_transition(transition, ped=ped)
            return None

        state.dialogue_only = True
        state.quest_context = payload
        if distance > candidate_distance_m:
            if self.ownership.state(ped.entity_id) in {
                OwnershipState.QUEST_DIALOGUE,
                OwnershipState.AI_ACTIVE,
                OwnershipState.AI_CONVERSATION,
            }:
                transition = self.ownership.request_release(
                    ped.entity_id, "quest_dialogue_left_area"
                )
                if transition is not None:
                    return self._record_transition(transition, ped=ped)
            return None

        if distance <= aware_distance_m:
            transition = self.ownership.enter_quest_dialogue(ped.entity_id)
            if transition is not None:
                return self._record_transition(transition, ped=ped)
        return None

    def _maybe_schedule_idle_thinking(self) -> None:
        """Fire an autonomous planning turn after a period of inactivity.

        This implements the spec's "autonomous planning timer" so an
        AI_ACTIVE NPC can decide to speak, change goals, or continue silently
        while the player is still nearby.
        """
        idle_seconds = float(
            self.settings.get("scheduler", {}).get("idle_thinking_seconds", 5.0)
        )
        if idle_seconds <= 0:
            return
        now = time.time()
        busy = {
            request.npc_id
            for request in self.dispatcher.pending.values()
            if request.npc_id
        }
        for npc_id, state in list(self.agent_state.items()):
            if state.reasoning or npc_id in busy or state.dialogue_only:
                continue
            if self.ownership.state(npc_id) != OwnershipState.AI_ACTIVE:
                continue
            if npc_id not in self.last_ped_scan:
                continue
            if now - state.last_decision_at < idle_seconds:
                continue
            self.schedule_reasoning(npc_id, "idle_autonomous")

    def execute_decision(self, npc_id: str, decision: AgentDecision) -> List[ToolResult]:
        state = self._agent_state(npc_id)
        results: List[ToolResult] = []
        if state.dialogue_only:
            # Quest NPC actions belong to Rockstar.  Keep only speech, and log
            # any blocked action so the timeline remains debuggable.
            allowed_actions = []
            for action in decision.actions:
                if action.tool == "say":
                    allowed_actions.append(action)
                else:
                    self.timeline.append_event(
                        npc_id,
                        "ACTION_FAILED",
                        data={
                            "tool": action.tool,
                            "reason": "dialogue_only_mode",
                            "message": "Rockstar owns quest NPC actions",
                        },
                        tags=["action", "quest_dialogue"],
                        importance=0.3,
                    )
            decision.actions = allowed_actions
        else:
            decision = self._stabilize_decision(npc_id, decision)

        if decision.goal and decision.goal != state.current_goal:
            event_name = "GOAL_CHANGED" if state.current_goal else "GOAL_CREATED"
            self.timeline.append_event(
                npc_id,
                event_name,
                data={"previous_goal": state.current_goal, "goal": decision.goal},
                tags=["goal"],
                importance=0.4,
            )
            state.current_goal = decision.goal
        if decision.mood:
            state.mood = decision.mood

        tool_ctx = ToolContext(
            npc_id, self.world, self.timeline, self.dispatcher,
            wiki=self.wiki, profile=self.profiles.get(npc_id),
        )
        if decision.speech is not None:
            if self._speech_allowed(npc_id):
                results.append(
                    self._call_tool(
                        "say",
                        tool_ctx,
                        text=decision.speech.text,
                        target=decision.speech.target,
                        emotion=decision.speech.emotion,
                    )
                )
                state.last_spoken_at = time.time()

        for action in decision.actions:
            if action.tool == "say":
                # Avoid double speaking when the backend emits both.
                if decision.speech is not None:
                    continue
            results.append(self._call_tool(action.tool, tool_ctx, **action.arguments))

        if any(
            result.tool == "say" and result.status == ActionStatus.STARTED
            for result in results
        ):
            self._last_global_speech_at = time.time()
        return results

    def _stabilize_decision(self, npc_id: str, decision: AgentDecision) -> AgentDecision:
        """Validate and normalize LLM tool actions before they reach the bridge.

        LLMs occasionally omit required tool arguments.  The runtime fills
        safe/intention-preserving defaults where possible and drops invalid
        actions rather than sending malformed commands to RDR2.
        """
        if not decision.actions:
            return decision

        normalized: List[AgentAction] = []
        for action in decision.actions:
            fixed, reason = self._normalize_action(npc_id, action, decision)
            if fixed is not None:
                normalized.append(fixed)
            elif reason:
                self.timeline.append_event(
                    npc_id,
                    "ACTION_FAILED",
                    data={
                        "tool": action.tool,
                        "reason": reason,
                        "arguments": dict(action.arguments or {}),
                    },
                    tags=["action", "validation"],
                    importance=0.3,
                )
        decision.actions = normalized
        return decision

    def _normalize_action(
        self,
        npc_id: str,
        action: AgentAction,
        decision: AgentDecision,
    ) -> tuple[Optional[AgentAction], Optional[str]]:
        tool = action.tool
        args = {k: v for k, v in dict(action.arguments or {}).items() if v is not None}

        if tool == "say":
            if decision.speech is not None and decision.speech.text:
                # The runtime already speaks decision.speech; avoid a duplicate.
                return None, None
            if not str(args.get("text") or "").strip():
                return None, "say_missing_text"
            return AgentAction("say", args), None

        if tool in {"look_at", "face", "follow", "flee_from"}:
            args.setdefault("entity", "player")
            return AgentAction(tool, args), None

        if tool == "wander":
            try:
                args["radius"] = float(args.get("radius", 8.0))
            except (TypeError, ValueError):
                args["radius"] = 8.0
            return AgentAction(tool, args), None

        if tool == "go_to":
            if not args.get("destination"):
                internal_destination = (decision.internal or {}).get("destination")
                if internal_destination:
                    args["destination"] = internal_destination
                elif decision.goal:
                    # Intention-preserving fallback: wander locally rather than
                    # sending a go_to with no destination.
                    return AgentAction("wander", {"radius": 8.0}), None
                else:
                    return None, "go_to_missing_destination"
            return AgentAction(tool, args), None

        if tool == "investigate":
            position = args.get("position") or self._recent_position(npc_id)
            if not position:
                return None, "investigate_missing_position"
            args["position"] = position
            return AgentAction(tool, args), None

        if tool == "wait":
            try:
                args["duration"] = float(args.get("duration", 2.0))
            except (TypeError, ValueError):
                args["duration"] = 2.0
            return AgentAction(tool, args), None

        if tool == "gesture":
            args.setdefault("type", "neutral")
            return AgentAction(tool, args), None

        if tool in {"stop", "clear_attention"}:
            return AgentAction(tool, {}), None

        return None, "unsupported_tool"

    def _recent_position(self, npc_id: str) -> Optional[List[float]]:
        for event in reversed(self.timeline.recent_events(npc_id, limit=30)):
            data = event.data or {}
            for key in ("position", "target_position", "destination_position"):
                raw = data.get(key)
                if isinstance(raw, list) and len(raw) >= 3:
                    try:
                        return [float(raw[0]), float(raw[1]), float(raw[2])]
                    except (TypeError, ValueError):
                        continue
        return None

    def _call_tool(self, name: str, tool_ctx: ToolContext, **arguments: Any) -> ToolResult:
        # Drop None arguments so schemas stay clean.
        filtered = {k: v for k, v in arguments.items() if v is not None}
        try:
            result = self.registry.call(name, tool_ctx, **filtered)
        except Exception as exc:  # noqa: BLE001 - action failures must not kill runtime
            request = ActionRequest(tool=name, arguments=filtered, npc_id=tool_ctx.npc_id)
            if tool_ctx.npc_id:
                self.timeline.append_event(
                    tool_ctx.npc_id,
                    "ACTION_FAILED",
                    data={"tool": name, "reason": type(exc).__name__, "message": str(exc)},
                    tags=["action"],
                    importance=0.5,
                )
            return ToolResult.failed(request, reason=str(exc))
        if isinstance(result, ToolResult):
            return result
        # Perception tools return data, not bridge actions.
        request = ActionRequest(tool=name, arguments=filtered, npc_id=tool_ctx.npc_id)
        return ToolResult.completed(request, result=result)

    def _apply_behavior_guardrails(
        self,
        npc_id: str,
        decision: AgentDecision,
        context: AgentContext,
    ) -> AgentDecision:
        """Guarantee critical spec behaviors even if the LLM omits an action.

        Speech remains LLM-authored.  The guardrail only ensures that urgent
        events produce at least one appropriate high-level action.
        """
        trigger = (context.flags or {}).get("trigger_event") or {}
        event_name = str(trigger.get("event_name") or "")
        data = trigger.get("data") or {}
        profile = self.profiles.get(npc_id)
        personality = profile.get("personality") or {}
        courage = float(personality.get("courage", 0.5))
        actions = list(decision.actions or [])
        tools = {action.tool for action in actions}

        if event_name in {"PLAYER_THREATENED_NPC", "PLAYER_ATTACKED_NPC", "NPC_DAMAGED"}:
            if courage < 0.48:
                # Low-courage NPC must try to escape.  Remove conflicting
                # movement intentions and put flee_from first.
                actions = [a for a in actions if a.tool not in {"go_to", "follow", "wander", "investigate"}]
                if "flee_from" not in {a.tool for a in actions}:
                    actions.insert(0, AgentAction("flee_from", {"entity": "player"}))
                decision.mood = decision.mood or "afraid"
                decision.goal = decision.goal or "get away from the player"
            else:
                if "face" not in tools and "look_at" not in tools:
                    actions.append(AgentAction("face", {"entity": "player"}))

        elif event_name == "GUNSHOT_HEARD":
            position = data.get("position")
            if courage >= 0.55 and isinstance(position, list) and len(position) >= 3:
                if "investigate" not in {a.tool for a in actions}:
                    actions = [a for a in actions if a.tool not in {"wander", "go_to", "follow", "flee_from"}]
                    actions.append(AgentAction("investigate", {"position": position}))
                decision.mood = decision.mood or "alert"
                decision.goal = decision.goal or "investigate the gunshot"
            elif courage < 0.40 and not any(
                a.tool in {"wander", "go_to", "flee_from"} for a in actions
            ):
                actions.append(AgentAction("wander", {"radius": 12.0}))
                decision.mood = decision.mood or "afraid"

        decision.actions = actions
        return decision

    def _call_backend(self, context: AgentContext, tool_context: ToolContext) -> AgentDecision:
        import inspect

        parameters = inspect.signature(self.backend.decide).parameters
        if "tool_context" in parameters:
            return self.backend.decide(context, tool_context)  # type: ignore[call-arg]
        return self.backend.decide(context)

    def _apply_speech_policy(
        self, npc_id: str, decision: AgentDecision, *, reason: str
    ) -> AgentDecision:
        if decision.speech is None:
            return decision
        # Direct push-to-talk answers and urgent reactions bypass cooldown;
        # ordinary autonomous speech does not.
        urgent_reasons = {
            "meaningful_event:PLAYER_THREATENED_NPC",
            "meaningful_event:PLAYER_ATTACKED_NPC",
            "meaningful_event:NPC_DAMAGED",
            "meaningful_event:GUNSHOT_HEARD",
        }
        if reason in {"push_to_talk", "player_spoke"} or reason in urgent_reasons:
            return decision
        speech_cfg = self.settings.get("speech", {})
        cooldown = float(speech_cfg.get("cooldown_seconds", 6.0))
        global_cooldown = float(speech_cfg.get("global_cooldown_seconds", 2.0))
        state = self._agent_state(npc_id)
        now = time.time()
        if time.time() - state.last_spoken_at < cooldown:
            decision.speech = None
            return decision
        if now - self._last_global_speech_at < global_cooldown:
            decision.speech = None
        return decision

    def _speech_allowed(self, npc_id: str) -> bool:
        if not self.ownership.can_speak(npc_id):
            return False
        return True

    # ------------------------------------------------------------------
    def _react_to_event(self, event: Event) -> None:
        state = self._agent_state(event.npc_id)
        name = event.event_name
        if name == "PLAYER_SPOKE":
            # A Push-to-Talk start already puts the NPC in AI_CONVERSATION.
            # Player speech during that conversation must not duplicate the
            # CONVERSATION_STARTED event.
            self._ensure_single_conversation(event.npc_id)
            if self.ownership.state(event.npc_id) != OwnershipState.AI_CONVERSATION:
                transition = self.ownership.push_to_talk(event.npc_id)
                if transition is not None:
                    self._record_transition(transition)
            state.conversation_active = True
            self.schedule_reasoning(event.npc_id, "player_spoke", trigger_event=event)
        elif name in {
            "PLAYER_THREATENED_NPC",
            "PLAYER_ATTACKED_NPC",
            "NPC_DAMAGED",
            "GUNSHOT_HEARD",
            "DEAD_BODY_DISCOVERED",
            "FIGHT_STARTED",
        }:
            self._make_meaningful(event.npc_id, f"meaningful_event:{name}", trigger_event=event)
        elif name in {"PLAYER_APPROACHED", "PLAYER_LOOKED_AT_NPC"}:
            if state.dialogue_only:
                # Quest NPC: generate a dialogue-only line tied to the quest,
                # while Rockstar keeps movement/tasks.
                self.schedule_reasoning(event.npc_id, "quest_dialogue", trigger_event=event)
            elif self.ownership.state(event.npc_id) == OwnershipState.AWARE:
                # These make an aware NPC active only when the owner state allows.
                self._make_meaningful(event.npc_id, f"meaningful_event:{name}", trigger_event=event)
        elif name == "ACTION_FAILED":
            self.schedule_reasoning(event.npc_id, "action_failed", trigger_event=event)
        elif name == "GOAL_COMPLETED":
            state.current_goal = None
        elif name in {"CONVERSATION_ENDED", "CONVERSATION_INTERRUPTED"}:
            self.ownership.conversation_ended(event.npc_id)
            state.conversation_active = False

    def _make_meaningful(
        self,
        npc_id: str,
        reason: str,
        *,
        trigger_event: Optional[Event] = None,
    ) -> None:
        state = self.ownership.state(npc_id)
        if state == OwnershipState.AWARE:
            if not self._can_promote_agent(npc_id):
                self._deferred_agents[npc_id] = reason
                self.timeline.append_event(
                    npc_id,
                    "AGENT_ACTIVATION_DEFERRED",
                    data={
                        "reason": "scheduler_cap",
                        "active_agents": self._active_agent_count(),
                        "max_active_agents": self._max_active_agents(),
                    },
                    tags=["ownership", "scheduler"],
                    importance=0.3,
                )
                return
            transition = self.ownership.update_from_scan(
                npc_id,
                distance_m=0.0,
                eligible=True,
                candidate_distance_m=float(self.settings["activation"]["candidate_distance_m"]),
                aware_distance_m=float(self.settings["activation"]["aware_distance_m"]),
                meaningful_trigger=True,
            )
            if transition is not None:
                self._record_transition(transition)
        if self.ownership.can_reason(npc_id):
            self.schedule_reasoning(npc_id, reason, trigger_event=trigger_event)

    def _active_agent_count(self) -> int:
        return sum(
            1
            for record in self.ownership.records.values()
            if record.state in {OwnershipState.AI_ACTIVE, OwnershipState.AI_CONVERSATION}
        )

    def _max_active_agents(self) -> int:
        return int(self.settings.get("scheduler", {}).get("max_reasoning_agents", 3))

    def _can_promote_agent(self, npc_id: str) -> bool:
        record = self.ownership.get(npc_id)
        if record.state in {OwnershipState.AI_ACTIVE, OwnershipState.AI_CONVERSATION}:
            return True
        return self._active_agent_count() < self._max_active_agents()

    def _promote_next_deferred(self) -> None:
        """Promote the oldest deferred AWARE NPC when a reasoning slot frees up."""
        while self._deferred_agents and self._active_agent_count() < self._max_active_agents():
            npc_id, reason = self._deferred_agents.popitem(last=False)
            if self.ownership.state(npc_id) != OwnershipState.AWARE:
                continue
            self._make_meaningful(npc_id, f"deferred_promotion:{reason}")

    def _ensure_single_conversation(self, npc_id: str) -> None:
        max_conversations = int(
            self.settings.get("scheduler", {}).get("max_conversation_agents", 1)
        )
        active = [
            other_id
            for other_id, record in self.ownership.records.items()
            if record.state == OwnershipState.AI_CONVERSATION and other_id != npc_id
        ]
        if len(active) < max_conversations:
            return
        # Keep at most max_conversations; stop the oldest excess conversations.
        for other_id in active[: max(0, len(active) - max_conversations + 1)]:
            transition = self.ownership.conversation_ended(other_id)
            if transition is not None:
                self._record_transition(transition)
            self.timeline.append_event(
                other_id,
                "CONVERSATION_ENDED",
                data={"by": "scheduler", "reason": "max_conversation_agents"},
                tags=["conversation", "scheduler"],
                importance=0.3,
            )
            self._agent_state(other_id).conversation_active = False

    def _record_transition(
        self,
        transition: OwnershipTransition,
        *,
        ped: Optional[PedSnapshot] = None,
        eligibility_reasons: Optional[List[str]] = None,
    ) -> Dict[str, Any]:
        event_name = transition.event_name
        if event_name is None and transition.to_state == OwnershipState.AI_CONVERSATION:
            event_name = "CONVERSATION_STARTED"
        if event_name is not None:
            data: Dict[str, Any] = {
                "from_state": transition.from_state.value,
                "to_state": transition.to_state.value,
                "reason": transition.reason,
            }
            if ped is not None:
                data["entity"] = ped.entity_id
                if ped.model:
                    data["model"] = ped.model
            if eligibility_reasons:
                data["eligibility_reasons"] = list(eligibility_reasons)
            self.timeline.append_event(
                transition.npc_id,
                event_name,
                data=data,
                tags=["ownership"],
                importance=0.8 if transition.to_state in {OwnershipState.AI_ACTIVE, OwnershipState.RELEASE, OwnershipState.SUSPENDED} else 0.3,
            )
        # RELEASE is a transient hand-off state; cleanup returns ownership to
        # Rockstar immediately after the release event is recorded.
        if transition.to_state in {OwnershipState.RELEASE, OwnershipState.ROCKSTAR}:
            self._deferred_agents.pop(transition.npc_id, None)
            runtime_state = self._agent_state(transition.npc_id)
            runtime_state.dialogue_only = False
            runtime_state.quest_context = {}
        if transition.to_state == OwnershipState.RELEASE:
            self.ownership.finalize_release(transition.npc_id)
            self._promote_next_deferred()
        return {
            "npc_id": transition.npc_id,
            "from": transition.from_state.value,
            "to": transition.to_state.value,
            "reason": transition.reason,
            "event_name": event_name,
        }

    def release_all(self, reason: str = "runtime_disconnect") -> List[Dict[str, Any]]:
        """Release every agent-owned NPC back to Rockstar AI."""
        released: List[Dict[str, Any]] = []
        for npc_id, record in list(self.ownership.records.items()):
            if record.state in {OwnershipState.ROCKSTAR, OwnershipState.RELEASE}:
                continue
            transition = self.ownership.request_release(npc_id, reason=reason)
            if transition is not None:
                released.append(self._record_transition(transition))
        return released

    # ------------------------------------------------------------------
    def select_conversation_target(self) -> Optional[str]:
        candidates = []
        cfg = self.settings.get("activation", {})
        max_range = float(cfg.get("conversation_range_m", 8.0))
        for ped in self.last_ped_scan.values():
            if not ped.entity_id or ped.distance_m is None:
                continue
            if ped.distance_m > max_range:
                continue
            ownership_state = self.ownership.state(ped.entity_id)
            if ownership_state in {OwnershipState.ROCKSTAR, OwnershipState.RELEASE}:
                continue
            if ownership_state != OwnershipState.QUEST_DIALOGUE:
                if not self.eligibility.can_activate(ped).allowed:
                    continue
            alignment = float(ped.metadata.get("camera_alignment", 0.0)) if ped.metadata else 0.0
            distance_score = max(0.0, 1.0 - (ped.distance_m / max_range))
            existing = 0.15 if self._agent_state(ped.entity_id).conversation_active else 0.0
            score = 0.5 * alignment + 0.3 * distance_score + existing
            candidates.append((score, ped.entity_id))
        if not candidates:
            return None
        candidates.sort(reverse=True)
        return candidates[0][1]

    def _agent_state(self, npc_id: str) -> AgentRuntimeState:
        if npc_id not in self.agent_state:
            state = AgentRuntimeState(npc_id=npc_id)
            # Rehydrate the current goal from persisted events so a runtime
            # restart does not silently discard the NPC's active intention.
            try:
                for event in reversed(self.timeline.recent_events(npc_id, limit=50)):
                    if event.event_name == "GOAL_COMPLETED":
                        break
                    if event.event_name in {"GOAL_CREATED", "GOAL_CHANGED"}:
                        state.current_goal = (event.data or {}).get("goal")
                        break
            except (OSError, ValueError):
                pass
            self.agent_state[npc_id] = state
        return self.agent_state[npc_id]

    def _npc_from_pending(self, raw: Dict[str, Any]) -> Optional[str]:
        request_id = raw.get("request_id")
        if not request_id:
            return None
        request = self.dispatcher.pending.get(str(request_id))
        return request.npc_id if request else None
