"""Newline-delimited JSON IPC server for the RDR2 bridge.

The Python runtime is the server; the C++ ASI bridge connects to localhost and
exchanges JSON messages.  All blocking work inside ``handle_message`` is run in
a thread so the asyncio event loop remains responsive.
"""
from __future__ import annotations

import asyncio
import json
from typing import Any, Dict, Optional

from runtime.agent.npc_agent import NpcAgentRuntime


class BridgeServer:
    def __init__(
        self,
        runtime: NpcAgentRuntime,
        host: str = "127.0.0.1",
        port: int = 8765,
    ):
        self.runtime = runtime
        self.host = host
        self.port = port
        self._server: Optional[asyncio.AbstractServer] = None
        self._writer: Optional[asyncio.StreamWriter] = None
        self._loop: Optional[asyncio.AbstractEventLoop] = None
        self.runtime.dispatcher.set_send(self.send_json)

    async def start(self) -> None:
        self._loop = asyncio.get_running_loop()
        self._server = await asyncio.start_server(
            self._handle_client,
            host=self.host,
            port=self.port,
        )
        addrs = ", ".join(str(sock.getsockname()) for sock in (self._server.sockets or []))
        print(f"[ipc] listening on {addrs}", flush=True)

    async def stop(self) -> None:
        if self._server is not None:
            self._server.close()
            await self._server.wait_closed()
            self._server = None

    async def serve_forever(self) -> None:
        await self.start()
        assert self._server is not None
        async with self._server:
            await self._server.serve_forever()

    # ------------------------------------------------------------------
    async def _handle_client(
        self, reader: asyncio.StreamReader, writer: asyncio.StreamWriter
    ) -> None:
        self._writer = writer
        peer = writer.get_extra_info("peername")
        print(f"[ipc] bridge connected: {peer}", flush=True)
        try:
            while not reader.at_eof():
                line = await reader.readline()
                if not line:
                    break
                try:
                    raw = json.loads(line.decode("utf-8"))
                except json.JSONDecodeError:
                    await self._write({"type": "error", "reason": "invalid json"})
                    continue
                if not isinstance(raw, dict):
                    await self._write({"type": "error", "reason": "message must be an object"})
                    continue
                reply = await asyncio.to_thread(self.runtime.handle_message, raw)
                if reply is not None:
                    await self._write(reply)
        except (ConnectionResetError, BrokenPipeError):
            pass
        finally:
            if self._writer is writer:
                self._writer = None
                # Spec release policy: a bridge disconnect releases all
                # temporary AI ownership back to Rockstar AI.
                await asyncio.to_thread(self.runtime.release_all, "bridge_disconnect")
            writer.close()
            try:
                await writer.wait_closed()
            except Exception:  # noqa: BLE001
                pass
            print(f"[ipc] bridge disconnected: {peer}", flush=True)

    async def _write(self, payload: Dict[str, Any]) -> None:
        writer = self._writer
        if writer is None:
            return
        data = json.dumps(payload, ensure_ascii=False, separators=(",", ":")) + "\n"
        writer.write(data.encode("utf-8"))
        await writer.drain()

    # ------------------------------------------------------------------
    def send_json(self, payload: Dict[str, Any]) -> None:
        """Thread-safe send used by the action dispatcher."""
        loop = self._loop
        if loop is None or self._writer is None:
            return
        asyncio.run_coroutine_threadsafe(self._write(payload), loop)
