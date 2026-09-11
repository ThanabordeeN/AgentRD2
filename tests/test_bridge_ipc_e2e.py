import json
import shutil
import socket
import subprocess
import sys
import tempfile
import threading
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
IS_WINDOWS = sys.platform.startswith("win")


class BridgeIpcEndToEndTests(unittest.TestCase):
    @unittest.skipUnless(shutil.which("g++"), "g++ is required for bridge integration test")
    def test_cpp_bridge_executes_action_request_over_real_tcp(self):
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            # MinGW insists on the .exe suffix when the target is executed.
            client_bin = tmp_path / ("ipc_e2e_client.exe" if IS_WINDOWS else "ipc_e2e_client")
            compile_cmd = [
                "g++", "-std=c++17", "-O0",
                "-pthread",
                "-I", str(ROOT / "bridge/include"),
                "-I", str(ROOT / "bridge/tests"),
                str(ROOT / "bridge/tests/ipc_e2e_client.cpp"),
                str(ROOT / "bridge/src/action_executor.cpp"),
                str(ROOT / "bridge/src/ped_scanner.cpp"),
                str(ROOT / "bridge/src/story_gate.cpp"),
                str(ROOT / "bridge/src/ipc_client.cpp"),
                str(ROOT / "bridge/src/bridge_runtime.cpp"),
                "-o", str(client_bin),
            ]
            if IS_WINDOWS:
                # ipc_client.cpp uses winsock on Windows.
                compile_cmd.append("-lws2_32")
            compiled = subprocess.run(compile_cmd, capture_output=True, text=True)
            self.assertEqual(compiled.returncode, 0, compiled.stderr)

            server = socket.socket()
            server.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            server.bind(("127.0.0.1", 0))
            server.listen(1)
            port = server.getsockname()[1]
            received = {}

            def server_thread():
                conn, _ = server.accept()
                conn.settimeout(10)
                buffer = b""
                # Read hello.
                while b"\n" not in buffer:
                    data = conn.recv(4096)
                    if not data:
                        return
                    buffer += data
                hello, _, buffer = buffer.partition(b"\n")
                received["hello"] = json.loads(hello)
                conn.sendall((
                    json.dumps({
                        "type": "action_request",
                        "npc_id": "ped_2",
                        "request": {
                            "tool": "say",
                            "request_id": "act_e2e",
                            "arguments": {"text": "Evening."},
                        },
                    }) + "\n"
                ).encode())
                deadline = 10
                while deadline > 0 and "result" not in received:
                    try:
                        data = conn.recv(4096)
                    except socket.timeout:
                        break
                    if not data:
                        break
                    buffer += data
                    while b"\n" in buffer:
                        line, buffer = buffer.split(b"\n", 1)
                        if not line.strip():
                            continue
                        message = json.loads(line)
                        if message.get("type") == "action_result":
                            received["result"] = message
                conn.close()

            thread = threading.Thread(target=server_thread, daemon=True)
            thread.start()
            client = subprocess.run(
                [str(client_bin), str(port)],
                capture_output=True,
                text=True,
                timeout=15,
            )
            thread.join(timeout=10)
            server.close()

            self.assertEqual(client.returncode, 0, client.stderr)
            self.assertEqual(received.get("hello", {}).get("type"), "hello")
            result = received.get("result")
            self.assertIsNotNone(result, "bridge did not return action_result")
            self.assertEqual(result["status"], "completed")
            self.assertEqual(result["tool"], "say")
            self.assertEqual(result["request_id"], "act_e2e")


if __name__ == "__main__":
    unittest.main()
