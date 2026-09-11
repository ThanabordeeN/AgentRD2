#!/usr/bin/env python3
"""Tiny bridge simulator for manual end-to-end IPC testing."""
from __future__ import annotations

import argparse
import json
import socket
import time


def send(sock: socket.socket, payload: dict) -> None:
    sock.sendall((json.dumps(payload) + "\n").encode("utf-8"))
    print("->", json.dumps(payload, ensure_ascii=False))


def recv_available(sock: socket.socket) -> None:
    sock.settimeout(0.2)
    while True:
        try:
            data = sock.recv(65536)
        except socket.timeout:
            break
        if not data:
            break
        for line in data.decode("utf-8").splitlines():
            if line.strip():
                print("<-", json.dumps(json.loads(line), ensure_ascii=False))


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=8765)
    parser.add_argument("--npc-id", default="npc_001")
    args = parser.parse_args()

    with socket.create_connection((args.host, args.port)) as sock:
        send(sock, {"type": "hello", "bridge": "mock"})
        time.sleep(0.1)
        recv_available(sock)
        ped = {
            "entity_id": args.npc_id,
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
            "distance_m": 15.0,
            "visible": True,
            "health": 100,
            "metadata": {"camera_alignment": 0.9},
        }
        send(sock, {"type": "ped_scan", "peds": [ped]})
        ped["distance_m"] = 8.0
        send(sock, {"type": "ped_scan", "peds": [ped]})
        time.sleep(0.1)
        recv_available(sock)
        send(sock, {"type": "game_event", "event_name": "PLAYER_APPROACHED", "npc_id": args.npc_id, "entities": ["player"], "tags": ["player"], "data": {"distance": 8.0}})
        send(sock, {"type": "game_event", "event_name": "PLAYER_LOOKED_AT_NPC", "npc_id": args.npc_id, "entities": ["player"], "tags": ["player"], "data": {}})
        time.sleep(0.2)
        recv_available(sock)
        send(sock, {"type": "player_speech", "npc_id": args.npc_id, "text": "Where are you headed?"})
        time.sleep(0.5)
        recv_available(sock)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
