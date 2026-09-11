#!/usr/bin/env python3
"""Deterministic scenario runner for the RDR2 Living NPC runtime.

Scenarios are JSON files in ``scenarios/``.  Each scenario builds a fresh
runtime in a temporary timeline directory, feeds messages/tools, and asserts
the resulting events and agent state.
"""
from __future__ import annotations

from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Dict, Iterable, List, Optional
import copy
import json
import tempfile

from runtime.agent.backends import RuleBasedAgentBackend
from runtime.agent.npc_agent import NpcAgentRuntime
from runtime.agent.openai_compat import OpenAICompatibleBackend
from runtime.config import load_settings
from runtime.env import load_env
from runtime.schemas import ToolResult
from runtime.state.ownership import OwnershipState
from runtime.timeline.store import TimelineStore
from runtime.tools import ToolContext


SCENARIOS_DIR = Path(__file__).resolve().parent / "scenarios"


@dataclass
class ScenarioResult:
    name: str
    path: str
    passed: bool
    failures: List[str] = field(default_factory=list)
    event_names: List[str] = field(default_factory=list)
    final_state: Dict[str, Any] = field(default_factory=dict)

    def failure_text(self) -> str:
        return "; ".join(self.failures)


def discover_scenarios(directory: str | Path = SCENARIOS_DIR) -> List[Path]:
    root = Path(directory)
    if not root.exists():
        return []
    return sorted(root.glob("*.json"))


def run_scenario_file(
    path: str | Path,
    *,
    backend: str = "rule",
    backend_options: Optional[Dict[str, Any]] = None,
) -> ScenarioResult:
    with Path(path).open("r", encoding="utf-8") as fh:
        scenario = json.load(fh)
    return run_scenario(
        scenario,
        source_path=str(path),
        backend=backend,
        backend_options=backend_options,
    )


def run_scenario(
    scenario: Dict[str, Any],
    *,
    source_path: str = "<inline>",
    backend: str = "rule",
    backend_options: Optional[Dict[str, Any]] = None,
) -> ScenarioResult:
    name = str(scenario.get("name") or Path(source_path).stem)
    failures: List[str] = []
    saved_results: Dict[str, Any] = {}

    try:
        with tempfile.TemporaryDirectory() as tmp:
            settings = copy.deepcopy(load_settings())
            settings["timelines_dir"] = str(Path(tmp) / "timelines")
            timeline = TimelineStore(settings["timelines_dir"])

            if backend == "llm":
                llm_cfg = dict(scenario.get("llm_backend") or {})
                llm_cfg.update(backend_options or {})
                backend_obj = OpenAICompatibleBackend(**{
                    k: v for k, v in llm_cfg.items() if v is not None
                })
            else:
                backend_cfg = dict(scenario.get("backend") or {})
                backend_obj = RuleBasedAgentBackend(**backend_cfg)
            runtime = NpcAgentRuntime(settings=settings, timeline=timeline, backend=backend_obj)
            npc_id = str(scenario.get("npc_id") or "npc_001")

            # Seed timeline facts before the scenario starts.
            for seed in scenario.get("seed_events") or []:
                seed = dict(seed)
                timeline.append_event(
                    str(seed.get("npc_id") or npc_id),
                    str(seed["event_name"]),
                    data=seed.get("data"),
                    entities=seed.get("entities"),
                    tags=seed.get("tags"),
                    summary=seed.get("summary"),
                    importance=seed.get("importance"),
                )

            for step in scenario.get("steps") or []:
                if "message" in step:
                    runtime.handle_message(dict(step["message"]))
                elif "tool_call" in step:
                    call = dict(step["tool_call"])
                    tool_name = str(call["tool"])
                    arguments = dict(call.get("arguments") or {})
                    ctx = ToolContext(
                        npc_id, runtime.world, runtime.timeline, runtime.dispatcher,
                        wiki=runtime.wiki, profile=runtime.profiles.get(npc_id),
                    )
                    result = runtime.registry.call(tool_name, ctx, **arguments)
                    saved_results[str(call.get("save_as") or tool_name)] = _to_jsonable(result)
                elif "pending_action_result" in step:
                    pending = dict(step["pending_action_result"])
                    request = _find_pending(runtime, str(pending.get("tool") or ""))
                    if request is None:
                        failures.append(f"pending action not found for tool={pending.get('tool')!r}")
                        continue
                    raw = {
                        "type": "action_result",
                        "npc_id": npc_id,
                        "request_id": request.request_id,
                        "tool": request.tool,
                        "status": pending.get("status", "failed"),
                    }
                    for key, value in pending.items():
                        if key != "tool":
                            raw[key] = value
                    runtime.handle_message(raw)
                elif "release_all" in step:
                    runtime.release_all(str(dict(step["release_all"]).get("reason", "scenario_release")))
                else:
                    failures.append(f"unknown scenario step: {step!r}")

            events = runtime.timeline.all_events(npc_id)
            event_names = [event.event_name for event in events]
            state = runtime._agent_state(npc_id)
            ownership = runtime.ownership.state(npc_id)
            backend_calls = int(getattr(backend_obj, "call_count", 0))
            final_state = {
                "ownership": ownership.value,
                "current_goal": state.current_goal,
                "mood": state.mood,
                "conversation_active": state.conversation_active,
                "last_event_name": event_names[-1] if event_names else None,
                "backend_calls": backend_calls,
            }
            failures.extend(
                _evaluate_expectations(
                    scenario.get("expect") or {},
                    event_names=event_names,
                    events=events,
                    ownership=ownership.value,
                    state=final_state,
                    saved_results=saved_results,
                    backend_calls=backend_calls,
                )
            )
            return ScenarioResult(
                name=name,
                path=source_path,
                passed=not failures,
                failures=failures,
                event_names=event_names,
                final_state=final_state,
            )
    except Exception as exc:  # noqa: BLE001 - scenario runner must report, not crash
        failures.append(f"{type(exc).__name__}: {exc}")
        return ScenarioResult(name=name, path=source_path, passed=False, failures=failures)


# ---------------------------------------------------------------------------
# Assertions
# ---------------------------------------------------------------------------

def _evaluate_expectations(
    expect: Dict[str, Any],
    *,
    event_names: List[str],
    events: Iterable[Any],
    ownership: str,
    state: Dict[str, Any],
    saved_results: Dict[str, Any],
    backend_calls: int = 0,
) -> List[str]:
    failures: List[str] = []
    events = list(events)

    if "ownership" in expect and ownership != expect["ownership"]:
        failures.append(f"ownership expected {expect['ownership']!r}, got {ownership!r}")

    for key in ("current_goal", "mood", "conversation_active", "last_event_name"):
        if key in expect and state.get(key) != expect[key]:
            failures.append(f"{key} expected {expect[key]!r}, got {state.get(key)!r}")

    for name in expect.get("event_names_contain") or []:
        if name not in event_names:
            failures.append(f"missing event {name!r}")

    for name in expect.get("event_names_not_contain") or []:
        if name in event_names:
            failures.append(f"unexpected event {name!r}")

    ordered = expect.get("ordered_event_names") or []
    if ordered and not _is_subsequence(ordered, event_names):
        failures.append(f"ordered events {ordered!r} not found in {event_names!r}")

    action_tools = [
        (event.data or {}).get("tool")
        for event in events
        if event.event_name == "ACTION_STARTED"
    ]
    for tool in expect.get("action_tools_contain") or []:
        if tool not in action_tools:
            failures.append(f"missing action tool {tool!r} in {action_tools!r}")
    for tool in expect.get("action_tools_not_contain") or []:
        if tool in action_tools:
            failures.append(f"unexpected action tool {tool!r} in {action_tools!r}")

    spoken = " ".join(
        str((event.data or {}).get("text", ""))
        for event in events
        if event.event_name == "NPC_SPOKE"
    )
    for fragment in expect.get("npc_spoke_contains") or []:
        if fragment not in spoken:
            failures.append(f"NPC speech missing {fragment!r}; got {spoken!r}")
    for fragment in expect.get("npc_spoke_not_contains") or []:
        if fragment in spoken:
            failures.append(f"NPC speech unexpectedly contains {fragment!r}")

    for name, minimum in (expect.get("event_count_at_least") or {}).items():
        count = event_names.count(name)
        if count < int(minimum):
            failures.append(f"event {name!r} count {count} < {minimum}")

    for name, exact in (expect.get("event_count_exact") or {}).items():
        count = event_names.count(name)
        if count != int(exact):
            failures.append(f"event {name!r} count {count} != {exact}")

    minimum_calls = expect.get("llm_calls_at_least")
    if minimum_calls is not None and backend_calls < int(minimum_calls):
        failures.append(f"backend calls {backend_calls} < {minimum_calls}")

    for save_as, expected_names in (expect.get("saved_timelines") or {}).items():
        payload = saved_results.get(save_as)
        actual_names = _extract_saved_event_names(payload)
        for expected in expected_names:
            if expected not in actual_names:
                failures.append(
                    f"saved timeline {save_as!r} missing {expected!r}; got {actual_names!r}"
                )

    return failures


def _is_subsequence(needle: List[str], haystack: List[str]) -> bool:
    index = 0
    for value in haystack:
        if index < len(needle) and value == needle[index]:
            index += 1
    return index == len(needle)


def _find_pending(runtime: NpcAgentRuntime, tool: str):
    matches = [request for request in runtime.dispatcher.pending.values() if request.tool == tool]
    return matches[-1] if matches else None


def _to_jsonable(value: Any) -> Any:
    if isinstance(value, ToolResult):
        return value.to_dict()
    if isinstance(value, dict):
        return {k: _to_jsonable(v) for k, v in value.items()}
    if isinstance(value, list):
        return [_to_jsonable(v) for v in value]
    return value


def _extract_saved_event_names(payload: Any) -> List[str]:
    if not isinstance(payload, dict):
        return []
    events = payload.get("events") or []
    if not isinstance(events, list):
        return []
    return [str(event.get("event_name")) for event in events if isinstance(event, dict)]


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

def main(argv: Optional[List[str]] = None) -> int:
    import argparse

    load_env()

    parser = argparse.ArgumentParser(description="Run RDR2 Living NPC scenarios")
    parser.add_argument("--scenarios", default=str(SCENARIOS_DIR))
    parser.add_argument("--json", action="store_true", help="emit machine-readable results")
    parser.add_argument(
        "--backend",
        choices=["rule", "llm"],
        default="rule",
        help="rule = deterministic test double, llm = live OpenAI-compatible backend",
    )
    parser.add_argument("--model", default=None)
    parser.add_argument("--api-base", default=None)
    parser.add_argument("--api-key", default=None)
    parser.add_argument("--api-key-env", default=None)
    parser.add_argument("--reasoning-effort", default=None)
    args = parser.parse_args(argv)

    paths = discover_scenarios(args.scenarios)
    if not paths:
        print(f"no scenarios found in {args.scenarios}")
        return 1

    backend_options: Dict[str, Any] = {}
    if args.backend == "llm":
        settings = load_settings()
        adk = settings.get("adk", {})
        backend_options = {
            "model": args.model or adk.get("model"),
            "api_base": args.api_base or adk.get("api_base"),
            "api_key": args.api_key,
            "api_key_env": args.api_key_env or adk.get("api_key_env"),
            "reasoning_effort": args.reasoning_effort or adk.get("reasoning_effort"),
            "extra_body": adk.get("extra_body") or {},
        }

    results = [
        run_scenario_file(path, backend=args.backend, backend_options=backend_options)
        for path in paths
    ]
    if args.json:
        print(json.dumps([result.__dict__ for result in results], ensure_ascii=False, indent=2))
    else:
        for result in results:
            status = "PASS" if result.passed else "FAIL"
            detail = "" if result.passed else f" -- {result.failure_text()}"
            print(f"[{status}] {Path(result.path).name}: {result.name} "
                  f"({len(result.event_names)} events){detail}")
        passed = sum(1 for result in results if result.passed)
        print(f"\n{passed}/{len(results)} scenarios passed")

    return 0 if all(result.passed for result in results) else 1


if __name__ == "__main__":
    raise SystemExit(main())
