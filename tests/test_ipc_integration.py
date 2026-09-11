import asyncio
import json
import tempfile
import unittest
from pathlib import Path

from runtime.agent.npc_agent import NpcAgentRuntime
from runtime.config import load_settings
from runtime.ipc.server import BridgeServer
from runtime.timeline.store import TimelineStore


def _ped(distance=8.0):
    return {
        "entity_id": "npc_001",
        "model": "a_m_m_farmer_01",
        "name": "Elias Carter",
        "is_ped": True,
        "is_human": True,
        "is_alive": True,
        "is_player": False,
        "is_story_character": False,
        "is_mission_owned": False,
        "in_scripted_state": False,
        "in_cutscene": False,
        "blacklisted": False,
        "distance_m": distance,
        "visible": True,
        "health": 100,
    }


class RuntimeIpcIntegrationTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        settings = load_settings()
        settings["timelines_dir"] = str(Path(self.tmp.name) / "timelines")
        self.timeline = TimelineStore(settings["timelines_dir"])
        self.runtime = NpcAgentRuntime(settings=settings, timeline=self.timeline)
        self.server = BridgeServer(self.runtime, host="127.0.0.1", port=0)
        await self.server.start()
        self.port = self.server._server.sockets[0].getsockname()[1]
        self.reader, self.writer = await asyncio.open_connection("127.0.0.1", self.port)

    async def asyncTearDown(self):
        self.writer.close()
        try:
            await self.writer.wait_closed()
        except Exception:
            pass
        await self.server.stop()
        self.tmp.cleanup()

    async def send(self, payload):
        self.writer.write(json.dumps(payload).encode("utf-8") + b"\n")
        await self.writer.drain()

    async def read_one(self, timeout=5):
        line = await asyncio.wait_for(self.reader.readline(), timeout=timeout)
        self.assertTrue(line, "bridge connection closed")
        return json.loads(line.decode("utf-8"))

    async def test_real_tcp_conversation_flow(self):
        await self.send({"type": "hello"})
        hello_ack = await self.read_one()
        self.assertEqual(hello_ack["type"], "hello_ack")

        await self.send({"type": "ped_scan", "peds": [_ped(15.0)]})
        await self.read_one()
        await self.send({"type": "ped_scan", "peds": [_ped(8.0)]})
        await self.read_one()

        await self.send({"type": "player_speech", "npc_id": "npc_001", "text": "Where are you headed?"})

        action_requests = []
        for _ in range(10):
            message = await self.read_one(timeout=5)
            if message.get("type") == "action_request":
                action_requests.append(message)
            if any(item.get("request", {}).get("tool") == "say" for item in action_requests):
                break

        say_request = next(
            item for item in action_requests
            if item.get("request", {}).get("tool") == "say"
        )
        request_id = say_request["request"]["request_id"]
        await self.send({
            "type": "action_result",
            "npc_id": "npc_001",
            "tool": "say",
            "request_id": request_id,
            "status": "completed",
        })

        # Let the server-side worker thread finish appending the result event.
        await asyncio.sleep(0.2)
        events = self.timeline.all_events("npc_001")
        names = [event.event_name for event in events]
        self.assertIn("PLAYER_SPOKE", names)
        self.assertIn("NPC_SPOKE", names)
        self.assertIn("ACTION_STARTED", names)
        self.assertIn("ACTION_COMPLETED", names)
        spoken = " ".join(
            event.data.get("text", "")
            for event in events
            if event.event_name == "NPC_SPOKE"
        )
        self.assertIn("Valentine", spoken)

    async def test_quest_dialogue_metadata_round_trip(self):
        ped = _ped(6.0)
        ped["entity_id"] = "npc_quest"
        ped["is_story_character"] = True
        ped["is_mission_owned"] = True
        ped["metadata"] = {
            "quest_dialogue": True,
            "quest_id": "quest_valentine_livestock",
        }
        await self.send({"type": "ped_scan", "peds": [ped]})
        ack = await self.read_one()
        self.assertEqual(ack["type"], "ped_scan_ack")
        self.assertEqual(self.runtime.ownership.state("npc_quest").value, "QUEST_DIALOGUE")


if __name__ == "__main__":
    unittest.main()
