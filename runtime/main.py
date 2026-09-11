"""Entry point for the external agent runtime."""
from __future__ import annotations

import argparse
import asyncio
import os

from runtime.agent.backends import GoogleADKBackend, RuleBasedAgentBackend
from runtime.agent.npc_agent import NpcAgentRuntime
from runtime.agent.openai_compat import OpenAICompatibleBackend
from runtime.config import load_settings
from runtime.env import load_env
from runtime.ipc.server import BridgeServer


def build_runtime(args: argparse.Namespace) -> NpcAgentRuntime:
    settings = load_settings(args.settings)
    if args.backend == "llm":
        # Dependency-free live backend: standard library only, no google-adk.
        # This is the recommended way to run a real model.
        backend = OpenAICompatibleBackend(
            model=args.model,
            api_base=args.api_base or settings.get("adk", {}).get(
                "api_base", "https://opencode.ai/zen/go/v1"
            ),
            api_key=args.api_key or None,
            api_key_env=args.api_key_env,
            reasoning_effort=getattr(args, "reasoning_effort", None) or None,
            extra_body=getattr(args, "extra_body", None) or None,
        )
    elif args.backend == "adk":
        backend = GoogleADKBackend(
            model=args.model,
            provider=args.provider,
            api_base=args.api_base or None,
            api_key=args.api_key or None,
            api_key_env=args.api_key_env,
            reasoning_effort=getattr(args, "reasoning_effort", None) or None,
            extra_body=getattr(args, "extra_body", None) or None,
        )
    else:
        backend = RuleBasedAgentBackend()
    runtime = NpcAgentRuntime(settings=settings, backend=backend)
    return runtime


async def run(args: argparse.Namespace) -> None:
    runtime = build_runtime(args)
    server = BridgeServer(runtime, host=args.host, port=args.port)
    try:
        await server.serve_forever()
    except asyncio.CancelledError:
        pass
    finally:
        await server.stop()


def main(argv: list[str] | None = None) -> int:
    # Pick up .env / .env.local / config/local.env before reading settings so
    # users never have to hand-export API keys in their shell.
    load_env()

    parser = argparse.ArgumentParser(description="RDR2 Living NPC agent runtime")
    parser.add_argument("--host", default=None, help="IPC bind host (default: settings.ipc.host)")
    parser.add_argument("--port", type=int, default=None, help="IPC bind port (default: settings.ipc.port)")
    parser.add_argument("--settings", default="config/settings.json")
    parser.add_argument(
        "--backend",
        choices=["rule", "llm", "adk"],
        default=os.environ.get("RDR2AI_BACKEND", "rule"),
        help="rule = offline deterministic, llm = built-in OpenAI-compatible "
        "(no extra packages), adk = Google ADK + LiteLLM "
        "(default: RDR2AI_BACKEND or rule)",
    )
    parser.add_argument(
        "--model",
        default=None,
        help="OpenAI-compatible model name (default: RDR2AI_MODEL or settings.adk.model)",
    )
    parser.add_argument(
        "--provider",
        default=None,
        help="LiteLLM provider prefix for --backend adk (default: openai)",
    )
    parser.add_argument(
        "--api-base",
        default=None,
        help="OpenAI-compatible base URL (default: RDR2AI_API_BASE or settings.adk.api_base)",
    )
    parser.add_argument("--api-key", default=None, help="OpenAI-compatible API key")
    parser.add_argument(
        "--api-key-env",
        default=None,
        help="Environment variable containing the API key "
        "(default: RDR2AI_API_KEY_ENV or settings.adk.api_key_env)",
    )
    parser.add_argument(
        "--reasoning-effort",
        default=None,
        choices=["minimal", "low", "medium", "high", "xhigh", "max"],
        help="Reasoning/thinking level for models that support it (e.g. low)",
    )
    args = parser.parse_args(argv)

    # argparse validates choices only for values given on the command line, so
    # guard the RDR2AI_BACKEND default coming from .env as well.
    if args.backend not in {"rule", "llm", "adk"}:
        parser.error(
            f"invalid backend {args.backend!r} from RDR2AI_BACKEND "
            "(expected rule, llm, or adk)"
        )

    settings = load_settings(args.settings)
    adk_cfg = settings.get("adk", {})
    # Precedence for every backend setting: CLI flag > environment (.env) > settings.json
    if args.model is None:
        args.model = os.environ.get("RDR2AI_MODEL") or adk_cfg.get("model", "gpt-4o-mini")
    if args.provider is None:
        args.provider = os.environ.get("RDR2AI_PROVIDER") or adk_cfg.get("provider", "openai")
    if args.api_base is None:
        args.api_base = os.environ.get("RDR2AI_API_BASE") or adk_cfg.get("api_base") or None
    if args.api_key_env is None:
        args.api_key_env = os.environ.get("RDR2AI_API_KEY_ENV") or adk_cfg.get(
            "api_key_env", "OPENAI_API_KEY"
        )
    if args.reasoning_effort is None:
        args.reasoning_effort = adk_cfg.get("reasoning_effort")
    args.extra_body = adk_cfg.get("extra_body") or None

    if args.host is None:
        args.host = settings.get("ipc", {}).get("host", "127.0.0.1")
    if args.port is None:
        args.port = int(settings.get("ipc", {}).get("port", 8765))

    try:
        asyncio.run(run(args))
    except KeyboardInterrupt:
        print("\n[ipc] stopped")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
