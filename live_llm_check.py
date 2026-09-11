#!/usr/bin/env python3
"""Live check for the OpenAI-compatible LLM backend.

This is intentionally dependency-free: it uses only the Python standard
library and the OpenCode/OpenAI-compatible endpoint configured in
``config/settings.json``.

Example:
    python3 live_llm_check.py
"""
from __future__ import annotations

import argparse
import json

from runtime.agent.openai_compat import OpenAICompatibleBackend
from runtime.config import load_settings, ProfileStore
from runtime.env import load_env
from runtime.schemas import AgentContext
from runtime.tools import build_default_registry


def main(argv: list[str] | None = None) -> int:
    load_env()
    settings = load_settings()
    adk = settings.get("adk", {})

    parser = argparse.ArgumentParser(description="Call the configured OpenAI-compatible model once")
    parser.add_argument("--model", default=adk.get("model", "deepseek-v4.1-flash"))
    parser.add_argument("--api-base", default=adk.get("api_base", "https://opencode.ai/zen/go/v1"))
    parser.add_argument("--api-key", default=None)
    parser.add_argument("--api-key-env", default=adk.get("api_key_env", "OPENCODE_API_KEY"))
    parser.add_argument("--reasoning-effort", default=adk.get("reasoning_effort", "low"))
    parser.add_argument("--thinking", choices=["low", "disabled"], default="low")
    parser.add_argument("--max-tokens", type=int, default=4096)
    args = parser.parse_args(argv)

    extra_body = adk.get("extra_body") or {}
    backend = OpenAICompatibleBackend(
        model=args.model,
        api_base=args.api_base,
        api_key=args.api_key,
        api_key_env=args.api_key_env,
        reasoning_effort=args.reasoning_effort,
        extra_body=extra_body,
        max_tokens=args.max_tokens,
        thinking_mode=args.thinking,
    )

    registry = build_default_registry()
    context = AgentContext(
        npc_id="npc_001",
        profile=ProfileStore().get("npc_001"),
        world_state={
            "self": {"position": [123.3, 442.1, 25.2], "health": 82, "in_combat": False},
            "player": {"distance": 4.1, "visible": True, "looking_at_me": True},
            "nearby_peds": [],
            "recent_game_events": [],
        },
        current_goal=None,
        current_mood="neutral",
        recent_events=[
            {
                "seq": 1,
                "event_name": "PLAYER_APPROACHED",
                "data": {"distance": 4.1},
                "tags": ["player"],
                "entities": ["player"],
            }
        ],
        available_tools=registry.names(),
        flags={"reason": "live_llm_check", "thinking_mode": args.thinking},
    )

    decision = backend.decide(context)
    print(
        json.dumps(
            {
                "model": args.model,
                "api_base": args.api_base,
                "calls": backend.call_count,
                "decision": decision.to_dict(),
            },
            ensure_ascii=False,
            indent=2,
        )
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
