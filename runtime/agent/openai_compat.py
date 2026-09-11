"""Direct OpenAI-compatible chat-completions backend.

This is a dependency-free test/fallback backend for environments where
``google-adk`` + ``litellm`` are not installed.  The production ADK path is
still :class:`~runtime.agent.backends.GoogleADKBackend`; this class exists so
the runtime and live scenario checks can call an OpenAI-compatible endpoint
using only the Python standard library.
"""
from __future__ import annotations

from pathlib import Path
from typing import Any, Dict, Optional
import json
import os
import re
import urllib.error
import urllib.request
import uuid

from runtime.agent.instructions import build_agent_prompt
from runtime.schemas import AgentAction, AgentContext, AgentDecision, AgentSpeech


OPENCODE_AUTH_PATH = Path.home() / ".local/share/opencode/auth.json"


def resolve_api_key(
    api_key: Optional[str] = None,
    api_key_env: str = "OPENCODE_API_KEY",
    *,
    allow_opencode_auth: bool = True,
) -> Optional[str]:
    """Resolve an API key from args, env vars, or the local OpenCode auth file."""
    if api_key:
        return api_key
    for env_name in (api_key_env, "OPENCODE_API_KEY", "OPENAI_API_KEY"):
        value = os.environ.get(env_name)
        if value:
            return value
    if allow_opencode_auth and OPENCODE_AUTH_PATH.exists():
        try:
            data = json.loads(OPENCODE_AUTH_PATH.read_text(encoding="utf-8"))
            entry = data.get("opencode-go") or {}
            key = entry.get("key")
            if key:
                return str(key)
        except (OSError, ValueError, TypeError):
            return None
    return None


class OpenAICompatibleBackend:
    """Call any OpenAI-compatible ``/chat/completions`` endpoint.

    The model is prompted with the same agent contract as the ADK backend and
    must return the structured JSON decision described in the specification.
    """

    def __init__(
        self,
        *,
        model: str = "deepseek-v4.1-flash",
        api_base: str = "https://opencode.ai/zen/go/v1",
        api_key: Optional[str] = None,
        api_key_env: str = "OPENCODE_API_KEY",
        reasoning_effort: Optional[str] = "low",
        extra_body: Optional[Dict[str, Any]] = None,
        extra_headers: Optional[Dict[str, str]] = None,
        timeout: float = 120.0,
        max_tokens: int = 4096,
        temperature: float = 0.3,
        thinking_mode: str = "low",
    ):
        self.model = model
        self.api_base = (api_base or "").rstrip("/")
        self.api_key = resolve_api_key(api_key, api_key_env)
        if not self.api_key:
            raise RuntimeError(
                "No OpenAI-compatible API key found. Set OPENCODE_API_KEY / "
                "OPENAI_API_KEY, pass api_key, or log in with OpenCode."
            )
        self.reasoning_effort = reasoning_effort
        self.extra_body = dict(extra_body or {})
        self.extra_headers = dict(extra_headers or {})
        self.timeout = timeout
        self.max_tokens = max_tokens
        self.temperature = temperature
        self.thinking_mode = thinking_mode if thinking_mode in {"low", "disabled"} else "low"
        self.session_id = "ses_" + uuid.uuid4().hex
        self.call_count = 0
        self.mode_calls: Dict[str, int] = {"low": 0, "disabled": 0}
        self.last_thinking_mode: Optional[str] = None
        # Indicates the backend has meaningful generation latency, so the
        # runtime should play a short thinking/listening gesture while waiting.
        self.supports_wait_gestures = True

    def decide(self, context: AgentContext, tool_context: Any = None) -> AgentDecision:
        self.call_count += 1
        body = self.build_body(context)

        last_error: Optional[Exception] = None
        for attempt in range(3):
            response = self._post_json(self.api_base + "/chat/completions", body)
            try:
                return self._decision_from_response(response)
            except RuntimeError as exc:
                last_error = exc
                if "no final message content" not in str(exc):
                    raise
                # The endpoint occasionally returns a reasoning-only response.
                # Retry with more output room, then relax response_format.
                body["max_tokens"] = max(int(body.get("max_tokens") or 0), 8192)
                body["messages"] = body["messages"][:1] + [
                    {
                        "role": "user",
                        "content": "Return only the final JSON decision object now. "
                        "The assistant message must contain content.",
                    }
                ]
                if attempt >= 1:
                    body.pop("response_format", None)
                    body.pop("thinking", None)
        assert last_error is not None
        raise last_error

    # ------------------------------------------------------------------
    def build_body(self, context: AgentContext) -> Dict[str, Any]:
        """Build the request body for this decision, including thinking mode."""
        prompt = build_agent_prompt(context.to_dict()) + (
            "\n\nReturn a single JSON object with keys: internal, speech, actions. "
            "The word JSON appears here so OpenAI-compatible response_format=json_object "
            "requests are accepted."
        )
        body: Dict[str, Any] = {
            "model": self.model,
            "messages": [{"role": "system", "content": prompt}],
            "max_tokens": self.max_tokens,
            "temperature": self.temperature,
            "response_format": {"type": "json_object"},
        }
        mode = str((context.flags or {}).get("thinking_mode") or self.thinking_mode)
        if mode not in {"low", "disabled"}:
            mode = "low"
        self.last_thinking_mode = mode
        self.mode_calls[mode] = self.mode_calls.get(mode, 0) + 1
        non_thinking_extra = {
            key: value for key, value in self.extra_body.items() if key != "thinking"
        }
        body.update(non_thinking_extra)
        if mode == "disabled":
            body.pop("reasoning_effort", None)
            body["thinking"] = {"type": "disabled"}
        else:
            if self.reasoning_effort:
                body["reasoning_effort"] = self.reasoning_effort
            thinking_extra = self.extra_body.get("thinking")
            body["thinking"] = thinking_extra or {"type": "enabled"}
        return body

    def _post_json(self, url: str, body: Dict[str, Any]) -> Dict[str, Any]:
        headers = {
            "Authorization": "Bearer " + self.api_key,
            "Content-Type": "application/json",
            "User-Agent": "rdr2-living-npc/0.2",
        }
        if "opencode.ai" in url:
            headers["x-opencode-session"] = self.session_id
            headers["x-opencode-client"] = "rdr2-living-npc"
        headers.update(self.extra_headers)
        request = urllib.request.Request(
            url,
            data=json.dumps(body).encode("utf-8"),
            method="POST",
            headers=headers,
        )
        try:
            with urllib.request.urlopen(request, timeout=self.timeout) as response:
                raw = response.read().decode("utf-8")
        except urllib.error.HTTPError as exc:
            detail = exc.read().decode("utf-8", errors="replace")[:800]
            raise RuntimeError(f"LLM HTTP {exc.code}: {detail}") from exc
        except urllib.error.URLError as exc:
            raise RuntimeError(f"LLM connection failed: {exc.reason}") from exc
        try:
            return json.loads(raw)
        except json.JSONDecodeError as exc:
            raise RuntimeError(f"LLM returned invalid JSON envelope: {raw[:400]}") from exc

    def _decision_from_response(self, response: Dict[str, Any]) -> AgentDecision:
        try:
            choice = response["choices"][0]
            message = choice["message"]
            content = message.get("content") or ""
            finish_reason = choice.get("finish_reason")
        except (KeyError, IndexError, TypeError) as exc:
            raise RuntimeError(f"unexpected LLM response shape: {response}") from exc
        if not content.strip():
            # Some reasoning models occasionally place the JSON answer in the
            # reasoning field on truncation/streaming edge cases.
            reasoning = str(message.get("reasoning_content") or "")
            match = re.search(r"\{.*\}", reasoning, re.DOTALL)
            if match:
                content = match.group(0)
        if not content.strip():
            reasoning = str(message.get("reasoning_content") or "")
            raise RuntimeError(
                "LLM response contained no final message content "
                f"(finish_reason={finish_reason!r}, reasoning_chars={len(reasoning)})"
            )
        return self._parse_decision(content)

    @staticmethod
    def _parse_decision(text: str) -> AgentDecision:
        candidate = text.strip()
        if candidate.startswith("```"):
            candidate = re.sub(r"^```(?:json)?\s*", "", candidate)
            candidate = re.sub(r"\s*```$", "", candidate)
        try:
            payload = json.loads(candidate)
        except json.JSONDecodeError:
            match = re.search(r"\{.*\}", candidate, re.DOTALL)
            if not match:
                raise RuntimeError(f"LLM did not return JSON decision: {text[:300]!r}")
            try:
                payload = json.loads(match.group(0))
            except json.JSONDecodeError as exc:
                raise RuntimeError(f"LLM returned malformed JSON: {text[:300]!r}") from exc

        if isinstance(payload, str):
            try:
                payload = json.loads(payload)
            except json.JSONDecodeError as exc:
                raise RuntimeError(f"LLM returned double-encoded non-JSON: {text[:300]!r}") from exc
        if not isinstance(payload, dict):
            raise RuntimeError(f"LLM JSON decision was not an object: {text[:300]!r}")

        internal = payload.get("internal") or {}
        if not isinstance(internal, dict):
            internal = {}
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

        target_defaults = {
            "look_at": {"entity": "player"},
            "face": {"entity": "player"},
            "follow": {"entity": "player"},
            "flee_from": {"entity": "player"},
        }
        actions = []
        raw_actions = payload.get("actions") or []
        if not isinstance(raw_actions, list):
            raw_actions = []
        for action in raw_actions:
            if isinstance(action, dict) and action.get("tool"):
                tool = str(action["tool"])
                arguments = dict(action.get("arguments") or {})
                for key, value in target_defaults.get(tool, {}).items():
                    arguments.setdefault(key, value)
                actions.append(AgentAction(tool=tool, arguments=arguments))
        return AgentDecision(
            goal=internal.get("goal"),
            mood=internal.get("mood"),
            speech=speech,
            actions=actions,
            internal=internal,
        )
