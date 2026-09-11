#!/usr/bin/env python3
"""Latency benchmark for runtime tool/actions and optional live LLM calls."""
from __future__ import annotations

import argparse
import statistics
import tempfile
import time
from pathlib import Path
from typing import Callable, Dict, List, Tuple

from runtime.agent.backends import RuleBasedAgentBackend
from runtime.agent.npc_agent import NpcAgentRuntime
from runtime.agent.openai_compat import OpenAICompatibleBackend
from runtime.config import ProfileStore, load_settings
from runtime.schemas import AgentContext
from runtime.state.ownership import OwnershipState
from runtime.timeline.store import TimelineStore
from runtime.tools import ToolContext, build_default_registry


def measure(fn: Callable[[], object], iterations: int) -> Dict[str, float]:
    samples: List[float] = []
    for _ in range(iterations):
        start = time.perf_counter()
        fn()
        samples.append((time.perf_counter() - start) * 1000.0)
    samples.sort()
    return {
        "n": len(samples),
        "mean_ms": statistics.mean(samples),
        "p50_ms": statistics.median(samples),
        "p95_ms": samples[max(0, int(len(samples) * 0.95) - 1)],
        "min_ms": min(samples),
        "max_ms": max(samples),
    }


def h(name: str, stats: Dict[str, float]) -> str:
    return f"{name:<34} {stats['mean_ms']:>8.3f} {stats['p50_ms']:>8.3f} {stats['p95_ms']:>8.3f} {stats['max_ms']:>8.3f}"


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--iterations", type=int, default=200)
    parser.add_argument("--live", action="store_true", help="include live OpenAI-compatible model latency")
    parser.add_argument("--live-iterations", type=int, default=3)
    args = parser.parse_args()

    tmp = tempfile.TemporaryDirectory()
    settings = load_settings()
    settings["timelines_dir"] = str(Path(tmp.name) / "timelines")
    timeline = TimelineStore(settings["timelines_dir"])
    runtime = NpcAgentRuntime(settings=settings, timeline=timeline, backend=RuleBasedAgentBackend())

    npc_id = "npc_001"
    safe_ped = {
        "entity_id": npc_id, "model": "a_m_m_farmer_01", "name": "Elias Carter",
        "is_ped": True, "is_human": True, "is_alive": True, "is_player": False,
        "is_story_character": False, "is_mission_owned": False,
        "in_scripted_state": False, "in_cutscene": False, "blacklisted": False,
        "distance_m": 8.0, "visible": True, "health": 100,
    }
    # Seed a usable size for grab_timeline.
    for i in range(1000):
        timeline.append_event(
            npc_id,
            "PLAYER_APPROACHED" if i % 3 else "GUNSHOT_HEARD",
            data={"i": i, "position": [1.0, 2.0, 3.0]},
            tags=["player"] if i % 3 else ["world"],
            entities=["player"],
            importance=0.5,
        )
    runtime.handle_message({"type": "ped_scan", "peds": [safe_ped]})
    runtime.handle_message({"type": "ped_scan", "peds": [safe_ped]})
    runtime.handle_message({
        "type": "game_event", "event_name": "PLAYER_LOOKED_AT_NPC",
        "npc_id": npc_id, "entities": ["player"], "tags": ["player"], "data": {},
    })

    tool_ctx = ToolContext(npc_id, runtime.world, timeline, runtime.dispatcher)
    registry = runtime.registry

    results: List[Tuple[str, Dict[str, float]]] = []
    results.append(("timeline.append_event", measure(
        lambda: timeline.append_event(npc_id, "TEST_EVENT", data={"x": 1}), args.iterations)))
    results.append(("timeline.grab_timeline(20 of 1000)", measure(
        lambda: timeline.grab_timeline(npc_id, limit=20), args.iterations)))
    results.append(("get_world_state tool", measure(
        lambda: runtime._call_tool("get_world_state", tool_ctx), args.iterations)))
    results.append(("grab_timeline tool", measure(
        lambda: runtime._call_tool("grab_timeline", tool_ctx, limit=20), args.iterations)))
    for tool, kwargs in [
        ("say", {"text": "Evening.", "target": "player"}),
        ("look_at", {"entity": "player"}),
        ("face", {"entity": "player"}),
        ("clear_attention", {}),
        ("wander", {"radius": 8.0}),
        ("go_to", {"destination": "Valentine Saloon"}),
        ("follow", {"entity": "player"}),
        ("stop", {}),
        ("investigate", {"position": [1.0, 2.0, 3.0]}),
        ("flee_from", {"entity": "player"}),
        ("wait", {"duration": 2.0}),
        ("gesture", {"type": "neutral"}),
    ]:
        results.append((f"tool.{tool}", measure(
            lambda tool=tool, kwargs=kwargs: runtime._call_tool(tool, tool_ctx, **kwargs), args.iterations)))

    # Event handling path with RuleBased backend (synchronous).
    results.append(("handle_message ped_scan(1)", measure(
        lambda: runtime.handle_message({"type": "ped_scan", "peds": [safe_ped]}), args.iterations)))
    results.append(("RuleBasedAgentBackend.decide", measure(
        lambda: runtime.backend.decide(runtime.build_context(npc_id, reason="bench")), args.iterations)))

    if args.live:
        adk = settings.get("adk", {})
        backend = OpenAICompatibleBackend(
            model=adk.get("model", "deepseek-v4.1-flash"),
            api_base=adk.get("api_base", "https://opencode.ai/zen/go/v1"),
            api_key_env=adk.get("api_key_env", "OPENCODE_API_KEY"),
            reasoning_effort=adk.get("reasoning_effort", "low"),
            extra_body=adk.get("extra_body") or {},
        )
        context = AgentContext(
            npc_id=npc_id,
            profile=ProfileStore().get(npc_id),
            world_state=runtime.world.get_dict(npc_id),
            current_goal=None,
            current_mood="neutral",
            recent_events=[e.to_dict() for e in timeline.recent_events(npc_id, 20)],
            available_tools=registry.names(),
            flags={"reason": "latency_benchmark"},
        )
        results.append(("LLM.decide (live)", measure(lambda: backend.decide(context), args.live_iterations)))

    print("Latency (milliseconds; local IPC/bridge not included)")
    print(f"{'operation':<34} {'mean':>8} {'p50':>8} {'p95':>8} {'max':>8}")
    print("-" * 72)
    for name, stats in results:
        print(h(name, stats))
    tmp.cleanup()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
