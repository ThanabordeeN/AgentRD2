"""Agent backends.

The runtime depends on the small :class:`AgentBackend` protocol, so the LLM
provider remains configurable (spec section 19).
"""
from __future__ import annotations

from typing import Any, Dict, List, Optional, Protocol
import asyncio
import json
import os
import re

from runtime.agent.instructions import NPC_INSTRUCTION, build_agent_prompt
from runtime.agent.openai_compat import resolve_api_key
from runtime.schemas import AgentAction, AgentContext, AgentDecision, AgentSpeech


class AgentBackend(Protocol):
    def decide(self, context: AgentContext, tool_context: Any = None) -> AgentDecision:  # pragma: no cover - protocol
        ...


# ---------------------------------------------------------------------------
# Deterministic offline backend used by tests and the dry-run demo
# ---------------------------------------------------------------------------

class RuleBasedAgentBackend:
    """A small deterministic backend that exercises the complete pipeline.

    It is not intended to feel like an LLM; it is a test double and offline
    demo.  Production deployments should use :class:`GoogleADKBackend`.
    """

    THREAT_EVENTS = {"PLAYER_THREATENED_NPC", "PLAYER_ATTACKED_NPC", "NPC_DAMAGED"}
    FRIENDLY_EVENTS = {"PLAYER_HELPED_NPC"}

    def __init__(self, *, silence_probability: float = 0.0, seed: int = 7):
        self.silence_probability = max(0.0, min(1.0, silence_probability))
        self.seed = seed

    def decide(self, context: AgentContext, tool_context: Any = None) -> AgentDecision:
        profile = context.profile or {}
        personality = profile.get("personality") or {}
        sociability = float(personality.get("sociability", 0.5))
        courage = float(personality.get("courage", 0.5))
        aggression = float(personality.get("aggression", 0.3))
        recent = context.recent_events or []
        trigger = (context.flags or {}).get("trigger_event") or {}
        latest = trigger if trigger else (recent[-1] if recent else {})

        goal = context.current_goal
        mood = context.current_mood or "neutral"
        speech: Optional[AgentSpeech] = None
        actions: List[AgentAction] = []

        threat = self._find_recent(context, self.THREAT_EVENTS)
        friendly = self._find_recent(context, self.FRIENDLY_EVENTS)

        if threat is not None:
            if courage < 0.48:
                mood = "afraid"
                goal = "get away from the player"
                actions.append(AgentAction("flee_from", {"entity": "player"}))
                speech = AgentSpeech("Stay back!", target="player", emotion="afraid")
            else:
                mood = "angry" if aggression >= 0.4 else "wary"
                goal = "stand ground"
                actions.append(AgentAction("look_at", {"entity": "player"}))
                speech = AgentSpeech(
                    "You had better move along.",
                    target="player",
                    emotion="annoyed",
                )
            return AgentDecision(goal=goal, mood=mood, speech=speech, actions=actions)

        if friendly is not None:
            mood = "warm"
            speech = AgentSpeech("Good to see a friendly face.", target="player", emotion="friendly")
            actions.append(AgentAction("look_at", {"entity": "player"}))
            return AgentDecision(goal=goal, mood=mood, speech=speech, actions=actions)

        if latest.get("event_name") == "GUNSHOT_HEARD":
            position = (latest.get("data") or {}).get("position")
            if courage >= 0.55 and position:
                mood = "alert"
                goal = "investigate the gunshot"
                actions.append(AgentAction("investigate", {"position": position}))
                speech = AgentSpeech("What the hell was that?", target=None, emotion="alert")
            elif courage < 0.4:
                mood = "afraid"
                goal = "leave the area"
                actions.append(AgentAction("wander", {"radius": 12.0}))
            return AgentDecision(goal=goal, mood=mood, speech=speech, actions=actions)

        if latest.get("event_name") == "PLAYER_SPOKE":
            transcript = str((latest.get("data") or {}).get("text", ""))
            if self._should_speak(context, direct=True):
                text = self._reply_to(transcript)
                if text:
                    speech = AgentSpeech(
                        text,
                        target="player",
                        emotion="neutral" if sociability < 0.7 else "friendly",
                    )
                    actions.append(AgentAction("look_at", {"entity": "player"}))
            return AgentDecision(goal=goal, mood=mood, speech=speech, actions=actions)

        if latest.get("event_name") in {"PLAYER_APPROACHED", "PLAYER_LOOKED_AT_NPC"}:
            if sociability >= 0.62 and self._should_speak(context, direct=False):
                text = "Evening." if sociability >= 0.75 else "Howdy."
                speech = AgentSpeech(text, target="player", emotion="neutral")
                actions.append(AgentAction("look_at", {"entity": "player"}))
            return AgentDecision(goal=goal, mood=mood, speech=speech, actions=actions)

        if latest.get("event_name") == "ACTION_FAILED":
            mood = "frustrated"
            tool = (latest.get("data") or {}).get("tool")
            if tool == "go_to":
                # Runtime already attempted low-level recovery; choose a
                # modest local fallback rather than infinite retries.
                actions.append(AgentAction("wander", {"radius": 8.0}))
            return AgentDecision(goal=goal, mood=mood, speech=speech, actions=actions)

        return AgentDecision(goal=goal, mood=mood, speech=speech, actions=actions)

    # ------------------------------------------------------------------
    def _find_recent(self, context: AgentContext, names: set[str]) -> Optional[Dict[str, Any]]:
        for event in reversed(context.recent_events or []):
            if event.get("event_name") in names:
                return event
        return None

    def _should_speak(self, context: AgentContext, *, direct: bool) -> bool:
        if not direct:
            # Deterministic pseudo-random silence decision for pass-by lines.
            if self.silence_probability <= 0.0:
                return True
            key = abs(hash((self.seed, context.npc_id, len(context.recent_events)))) % 1000
            if key / 1000.0 < self.silence_probability:
                return False
        else:
            if self.silence_probability > 0.9:
                return False
        return True

    @staticmethod
    def _reply_to(transcript: str) -> Optional[str]:
        text = transcript.lower()
        if not text.strip():
            return None
        if any(word in text for word in ("where", "headed", "going")):
            return "Valentine, if I keep moving."
        if any(word in text for word in ("hello", "hey", "evening", "morning", "howdy")):
            return "Evening."
        if "thank" in text:
            return "Don't mention it."
        if "help" in text:
            return "I have my own troubles."
        if "your name" in text or "who are you" in text:
            return "Nobody important."
        return "Hmph."


# ---------------------------------------------------------------------------
# Google ADK backend
# ---------------------------------------------------------------------------

class GoogleADKBackend:
    """Google Agent Development Kit orchestration backend.

    ADK is used as the orchestration layer, but the model is wired through
    ADK's LiteLLM adapter to an OpenAI-compatible endpoint.  This works with
    OpenAI itself, vLLM, Ollama's OpenAI endpoint, LM Studio, llama.cpp
    server, LiteLLM proxy, and similar services.

    Install the optional dependencies:

    .. code-block:: bash

        pip install google-adk litellm

    Configuration precedence:

    1. explicit ``provider`` / ``model`` / ``api_base`` / ``api_key`` args
    2. ``OPENAI_BASE_URL`` or ``OPENAI_API_BASE``
    3. ``OPENAI_API_KEY`` (or the configured ``api_key_env``)

    Perception tools (``get_world_state`` / ``grab_timeline``) execute directly
    and return facts to the model.  Action tools are captured and returned in
    the structured :class:`AgentDecision`; the runtime then sends them through
    the validated action dispatcher.  This keeps action execution and timeline
    logging in one place.
    """

    def __init__(
        self,
        *,
        model: str = "gpt-4o-mini",
        provider: str = "openai",
        app_name: str = "rdr2_living_npc",
        user_id: str = "player",
        api_base: Optional[str] = None,
        api_key: Optional[str] = None,
        api_key_env: str = "OPENAI_API_KEY",
        extra_headers: Optional[Dict[str, str]] = None,
        reasoning_effort: Optional[str] = None,
        extra_body: Optional[Dict[str, Any]] = None,
    ):
        self.model = model
        self.provider = provider
        self.app_name = app_name
        self.user_id = user_id
        self.api_key_env = api_key_env or "OPENAI_API_KEY"
        self.api_base = (
            api_base
            or os.environ.get("OPENAI_BASE_URL")
            or os.environ.get("OPENAI_API_BASE")
            or ""
        ).rstrip("/")
        self.api_key = resolve_api_key(
            api_key,
            self.api_key_env,
            allow_opencode_auth="opencode.ai" in self.api_base.lower(),
        )
        # Local OpenAI-compatible servers commonly accept any non-empty key.
        if not self.api_key and self._is_local_url(self.api_base):
            self.api_key = "not-needed"
        self.extra_headers = dict(extra_headers or {})
        self.reasoning_effort = reasoning_effort.strip() if isinstance(reasoning_effort, str) else reasoning_effort
        self.extra_body = dict(extra_body or {})
        if self.api_key:
            os.environ.setdefault("OPENAI_API_KEY", self.api_key)
        if self.api_base:
            os.environ.setdefault("OPENAI_BASE_URL", self.api_base)
            os.environ.setdefault("OPENAI_API_BASE", self.api_base)
        self._captured_actions: List[AgentAction] = []
        self.supports_wait_gestures = True

    def decide(self, context: AgentContext, tool_context: Any = None) -> AgentDecision:
        self._ensure_google_adk()
        try:
            return asyncio.run(self._decide_async(context, tool_context))
        except RuntimeError as exc:
            # Called from inside an existing event loop (e.g. async server).
            if "asyncio.run() cannot be called" in str(exc):
                loop = asyncio.new_event_loop()
                try:
                    return loop.run_until_complete(self._decide_async(context, tool_context))
                finally:
                    loop.close()
            raise

    async def _decide_async(self, context: AgentContext, tool_context: Any = None) -> AgentDecision:
        try:
            from google.adk.agents import LlmAgent
        except ImportError:  # pragma: no cover - older ADK naming
            from google.adk.agents import Agent as LlmAgent  # type: ignore
        from google.adk.models.lite_llm import LiteLlm
        from google.adk.runners import Runner
        from google.adk.sessions import InMemorySessionService
        from google.genai import types

        self._captured_actions = []
        tools = self._build_tools(tool_context, context.available_tools)
        prompt = build_agent_prompt(context.to_dict())
        thinking_mode = str((context.flags or {}).get("thinking_mode") or "low")
        agent = LlmAgent(
            name="rdr2_npc_agent",
            model=LiteLlm(**self._litellm_kwargs(thinking_mode)),
            instruction=prompt,
            tools=tools,
        )
        session_service = InMemorySessionService()
        session = session_service.create_session(
            app_name=self.app_name,
            user_id=self.user_id,
        )
        if asyncio.iscoroutine(session):
            session = await session
        runner = Runner(
            agent=agent,
            app_name=self.app_name,
            session_service=session_service,
        )
        message = types.Content(
            role="user",
            parts=[types.Part(text="Take your turn. Use tools when useful, then return the structured JSON decision.")],
        )
        final_text = ""
        stream = runner.run_async(
            user_id=self.user_id,
            session_id=session.id,
            new_message=message,
        )
        if hasattr(stream, "__aiter__"):
            async for event in stream:
                if self._is_final_response(event):
                    final_text = self._event_text(event) or final_text
        else:  # pragma: no cover - older/sync ADK runner
            for event in stream:
                if self._is_final_response(event):
                    final_text = self._event_text(event) or final_text
        return self._parse_decision(final_text)

    # ------------------------------------------------------------------
    def litellm_model_name(self) -> str:
        """Return the provider-qualified model name used by LiteLLM."""
        name = (self.model or "").strip()
        if "/" in name or not self.provider:
            return name
        return f"{self.provider}/{name}"

    def _litellm_kwargs(self, thinking_mode: str = "low") -> Dict[str, Any]:
        mode = thinking_mode if thinking_mode in {"low", "disabled"} else "low"
        kwargs: Dict[str, Any] = {"model": self.litellm_model_name()}
        if self.api_base:
            kwargs["api_base"] = self.api_base
        if self.api_key:
            kwargs["api_key"] = self.api_key
        if self.extra_headers:
            kwargs["extra_headers"] = dict(self.extra_headers)
        extra_body = dict(self.extra_body or {})
        if mode == "disabled":
            extra_body["thinking"] = {"type": "disabled"}
        else:
            if self.reasoning_effort:
                kwargs["reasoning_effort"] = self.reasoning_effort
            extra_body.setdefault("thinking", {"type": "enabled"})
        if extra_body:
            kwargs["extra_body"] = extra_body
        return kwargs

    @staticmethod
    def _is_local_url(url: str) -> bool:
        lowered = (url or "").lower()
        return any(
            host in lowered
            for host in ("127.0.0.1", "localhost", "0.0.0.0", "[::1]", "host.docker.internal")
        )

    def _ensure_google_adk(self) -> None:
        try:
            import google.adk  # noqa: F401
            from google.adk.models.lite_llm import LiteLlm  # noqa: F401
        except ImportError as exc:
            raise RuntimeError(
                "google-adk + litellm are required for the OpenAI-compatible "
                "ADK backend. Install them with `pip install google-adk litellm` "
                "or use RuleBasedAgentBackend."
            ) from exc
        if not self.api_key:
            raise RuntimeError(
                "No OpenAI-compatible API key configured. Set "
                f"{self.api_key_env} (or OPENAI_API_KEY), pass --api-key, or use "
                "a local api_base that accepts a dummy key."
            )

    def _build_tools(self, tool_context: Any, available_tools: List[str]):
        if tool_context is None:
            return []
        enabled = set(available_tools or [])

        def allowed(name: str) -> bool:
            return not enabled or name in enabled

        tools = []

        def get_world_state() -> Dict[str, Any]:
            """Return the NPC's current relevant world state."""
            return tool_context.get_world_state()

        def get_world_lore(topic: Optional[str] = None) -> Dict[str, Any]:
            """Look up offline Red Dead Wiki lore/context for a topic or this NPC."""
            wiki = getattr(tool_context, "wiki", None)
            if wiki is None:
                return {"lore": []}
            if topic:
                return {"lore": wiki.lookup(topic)}
            profile = getattr(tool_context, "profile", {}) or {}
            return {"lore": wiki.context_for_profile(profile)}

        def grab_timeline(
            event_name: Optional[str] = None,
            event_names: Optional[List[str]] = None,
            tag: Optional[str] = None,
            tags: Optional[List[str]] = None,
            entity: Optional[str] = None,
            minimum_importance: Optional[float] = None,
            since_seq: Optional[int] = None,
            before_seq: Optional[int] = None,
            limit: int = 20,
        ) -> Dict[str, Any]:
            """Retrieve older named events from this NPC's JSONL timeline."""
            return {"events": tool_context.grab_timeline(
                event_name=event_name,
                event_names=event_names,
                tag=tag,
                tags=tags,
                entity=entity,
                minimum_importance=minimum_importance,
                since_seq=since_seq,
                before_seq=before_seq,
                limit=limit,
            )}

        def think(style: str = "think", duration: float = 2.5) -> Dict[str, Any]:
            """Play a short thinking/listening/waiting gesture."""
            return self._capture("think", {"style": style, "duration": duration})

        def say(text: str, target: Optional[str] = None, emotion: Optional[str] = None) -> Dict[str, Any]:
            """Speak a short line through the TTS pipeline."""
            return self._capture("say", {"text": text, "target": target, "emotion": emotion})

        def look_at(entity: str) -> Dict[str, Any]:
            """Look at an entity without preventing other actions."""
            return self._capture("look_at", {"entity": entity})

        def face(entity: str) -> Dict[str, Any]:
            """Turn the body/head toward an entity."""
            return self._capture("face", {"entity": entity})

        def clear_attention() -> Dict[str, Any]:
            """Stop looking/attending to the current target."""
            return self._capture("clear_attention", {})

        def wander(radius: float) -> Dict[str, Any]:
            """Wander around the current area within a radius."""
            return self._capture("wander", {"radius": radius})

        def go_to(destination: str, priority: Optional[str] = None) -> Dict[str, Any]:
            """Walk to a named destination such as 'Valentine Saloon'."""
            return self._capture("go_to", {"destination": destination, "priority": priority})

        def follow(entity: str, distance: Optional[float] = None) -> Dict[str, Any]:
            """Follow an entity at a comfortable distance."""
            return self._capture("follow", {"entity": entity, "distance": distance})

        def stop() -> Dict[str, Any]:
            """Stop the current movement action."""
            return self._capture("stop", {})

        def investigate(position: List[float]) -> Dict[str, Any]:
            """Move toward a position to investigate it."""
            return self._capture("investigate", {"position": position})

        def flee_from(entity: str) -> Dict[str, Any]:
            """Move away from an entity in a panic/flee state."""
            return self._capture("flee_from", {"entity": entity})

        def wait(duration: float) -> Dict[str, Any]:
            """Wait for a short duration before the next decision."""
            return self._capture("wait", {"duration": duration})

        def react(entity: Optional[str] = None, position: Optional[List[float]] = None, reaction: Optional[str] = None) -> Dict[str, Any]:
            """React physically to an entity or position."""
            args = {"entity": entity, "position": position, "reaction": reaction}
            return self._capture("react", {k: v for k, v in args.items() if v is not None})

        def hands_up(duration: float = 3.0, face_entity: Optional[str] = None) -> Dict[str, Any]:
            """Raise hands in surrender or fear."""
            return self._capture("hands_up", {"duration": duration, "face_entity": face_entity})

        def cower(duration: float = 3.0, from_entity: Optional[str] = None) -> Dict[str, Any]:
            """Cower away from a threat."""
            return self._capture("cower", {"duration": duration, "from_entity": from_entity})

        def duck(duration: float = 2.0) -> Dict[str, Any]:
            """Duck down defensively."""
            return self._capture("duck", {"duration": duration})

        def jump() -> Dict[str, Any]:
            """Jump or startle in place."""
            return self._capture("jump", {})

        def walk_away(entity: str) -> Dict[str, Any]:
            """Walk away from an entity."""
            return self._capture("walk_away", {"entity": entity})

        def mount(entity: str) -> Dict[str, Any]:
            """Mount a horse or rideable animal."""
            return self._capture("mount", {"entity": entity})

        def dismount() -> Dict[str, Any]:
            """Dismount the current animal."""
            return self._capture("dismount", {})

        def item_interaction(item: str, interaction: str) -> Dict[str, Any]:
            """Perform a scripted item interaction."""
            return self._capture("item_interaction", {"item": item, "interaction": interaction})

        def animal_interaction(target: str, interaction_type: str, interaction_model: str) -> Dict[str, Any]:
            """Perform a scripted animal interaction."""
            return self._capture("animal_interaction", {"target": target, "interaction_type": interaction_type, "interaction_model": interaction_model})

        def horse_action(action: int, target: Optional[str] = None) -> Dict[str, Any]:
            """Perform a scripted horse action."""
            return self._capture("horse_action", {"action": action, "target": target})

        def aim_at(entity: Optional[str] = None, position: Optional[List[float]] = None, duration: float = 3.0) -> Dict[str, Any]:
            """Aim a weapon at an entity or position (deferred combat)."""
            args = {"entity": entity, "position": position, "duration": duration}
            return self._capture("aim_at", {k: v for k, v in args.items() if v is not None})

        def shoot_at(entity: Optional[str] = None, position: Optional[List[float]] = None, duration: float = 0.5) -> Dict[str, Any]:
            """Shoot at an entity or position (deferred combat)."""
            args = {"entity": entity, "position": position, "duration": duration}
            return self._capture("shoot_at", {k: v for k, v in args.items() if v is not None})

        def attack(entity: str) -> Dict[str, Any]:
            """Attack an entity (deferred combat)."""
            return self._capture("attack", {"entity": entity})

        def take_cover(from_entity: Optional[str] = None, from_position: Optional[List[float]] = None, duration: float = 5.0) -> Dict[str, Any]:
            """Take cover from an entity or position (deferred combat)."""
            args = {"from_entity": from_entity, "from_position": from_position, "duration": duration}
            return self._capture("take_cover", {k: v for k, v in args.items() if v is not None})

        # Registration is conservative: ADK versions that need a ToolContext
        # annotation can still introspect these plain Python functions.
        candidates = [
            get_world_state, grab_timeline, get_world_lore,
            say, look_at, face, clear_attention, think, wander, go_to,
            follow, stop, investigate, flee_from, wait, react, hands_up,
            cower, duck, jump, walk_away, mount, dismount,
            item_interaction, animal_interaction, horse_action,
            aim_at, shoot_at, attack, take_cover,
        ]
        return [fn for fn in candidates if allowed(fn.__name__)]

    def _capture(self, tool: str, arguments: Dict[str, Any]) -> Dict[str, Any]:
        action = AgentAction(tool=tool, arguments={k: v for k, v in arguments.items() if v is not None})
        self._captured_actions.append(action)
        return {"status": "started", "tool": tool, "request_id": f"adk_{len(self._captured_actions)}"}

    def _parse_decision(self, text: str) -> AgentDecision:
        payload: Dict[str, Any] = {}
        if text:
            candidate = text.strip()
            if candidate.startswith("```"):
                candidate = re.sub(r"^```(?:json)?\s*", "", candidate)
                candidate = re.sub(r"\s*```$", "", candidate)
            try:
                payload = json.loads(candidate)
            except json.JSONDecodeError:
                match = re.search(r"\{.*\}", candidate, re.DOTALL)
                if match:
                    try:
                        payload = json.loads(match.group(0))
                    except json.JSONDecodeError:
                        payload = {}
                if not payload:
                    payload = {"speech": {"text": candidate, "target": "player"}}

        internal = payload.get("internal") or {}
        speech_payload = payload.get("speech")
        speech: Optional[AgentSpeech] = None
        if isinstance(speech_payload, dict) and speech_payload.get("text"):
            speech = AgentSpeech(
                text=str(speech_payload["text"]),
                target=speech_payload.get("target"),
                emotion=speech_payload.get("emotion"),
            )
        elif isinstance(speech_payload, str) and speech_payload:
            speech = AgentSpeech(text=speech_payload, target="player")

        actions: List[AgentAction] = []
        for action in payload.get("actions") or []:
            if isinstance(action, dict) and action.get("tool"):
                actions.append(
                    AgentAction(
                        tool=str(action["tool"]),
                        arguments=dict(action.get("arguments") or {}),
                    )
                )
        if not actions:
            actions = list(self._captured_actions)

        return AgentDecision(
            goal=internal.get("goal"),
            mood=internal.get("mood"),
            speech=speech,
            actions=actions,
            internal=internal,
        )

    @staticmethod
    def _is_final_response(event: Any) -> bool:
        flag = getattr(event, "is_final_response", False)
        if callable(flag):
            try:
                return bool(flag())
            except TypeError:
                return True
        return bool(flag)

    @staticmethod
    def _event_text(event: Any) -> str:
        content = getattr(event, "content", None)
        if content is None:
            return ""
        parts = getattr(content, "parts", None) or []
        texts = [getattr(part, "text", "") for part in parts]
        return "".join(t for t in texts if t)
