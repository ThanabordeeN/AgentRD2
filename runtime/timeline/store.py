"""JSONL-backed event timeline (the MVP's only long-term memory)."""
from __future__ import annotations

from pathlib import Path
from typing import Any, Dict, Iterable, Iterator, List, Optional
import json
import os
import tempfile

from runtime.schemas import Event, new_event


class TimelineStore:
    """Append-only, per-NPC JSONL timeline store.

    Each line is one immutable :class:`~runtime.schemas.Event`.  No semantic
    retrieval, embeddings, or vector database is used.
    """

    def __init__(self, root: str | os.PathLike[str]):
        self.root = Path(root)
        self.root.mkdir(parents=True, exist_ok=True)
        self._last_seq: Dict[str, int] = {}

    # -- path helpers -----------------------------------------------------
    def path_for(self, npc_id: str) -> Path:
        safe = "".join(c if c.isalnum() or c in "-_." else "_" for c in npc_id)
        if not safe or safe in {".", ".."}:
            raise ValueError(f"invalid npc_id: {npc_id!r}")
        return self.root / f"{safe}.jsonl"

    # -- appends ----------------------------------------------------------
    @staticmethod
    def _tail_nonempty_lines(path: Path, limit: int) -> List[bytes]:
        """Read the last ``limit`` non-empty lines without scanning the file."""
        if limit <= 0 or not path.exists():
            return []
        with path.open("rb") as fh:
            fh.seek(0, os.SEEK_END)
            position = fh.tell()
            data = b""
            newline_count = 0
            while position > 0 and newline_count <= limit:
                read_size = min(64 * 1024, position)
                position -= read_size
                fh.seek(position)
                data = fh.read(read_size) + data
                newline_count = data.count(b"\n")
        lines = data.split(b"\n")
        if position > 0 and lines:
            # The first line may be a partial line because we started reading
            # in the middle of the file.
            lines = lines[1:]
        nonempty = [line for line in lines if line.strip()]
        return nonempty[-limit:]

    def next_seq(self, npc_id: str) -> int:
        if npc_id not in self._last_seq:
            last = 0
            path = self.path_for(npc_id)
            if path.exists():
                for event in self.iter_events(npc_id):
                    last = max(last, event.seq)
            self._last_seq[npc_id] = last
        return self._last_seq[npc_id] + 1

    def append(self, event: Event) -> Event:
        """Append an event. If ``event.seq`` is not the next sequence number it
        is replaced with the next number to keep the timeline linear."""
        expected = self.next_seq(event.npc_id)
        if event.seq != expected:
            event = Event.from_dict({**event.to_dict(), "seq": expected})
        path = self.path_for(event.npc_id)
        line = json.dumps(event.to_dict(), ensure_ascii=False, separators=(",", ":"))
        # Recreate the directory if it vanished (e.g. a temp timeline dir was
        # cleaned up while a disconnect handler was still writing); otherwise
        # Windows raises FileNotFoundError from the open() below.
        path.parent.mkdir(parents=True, exist_ok=True)
        # JSONL append; one write call is important enough for a game runtime.
        with path.open("a", encoding="utf-8") as fh:
            fh.write(line + "\n")
        self._last_seq[event.npc_id] = event.seq
        return event

    def append_event(
        self,
        npc_id: str,
        event_name: str,
        *,
        data: Optional[Dict[str, Any]] = None,
        entities: Optional[Iterable[str]] = None,
        tags: Optional[Iterable[str]] = None,
        summary: Optional[str] = None,
        importance: Optional[float] = None,
        game_time: Optional[Any] = None,
        location: Optional[Any] = None,
        timestamp: Optional[float] = None,
    ) -> Event:
        """Create and append a new event in one call."""
        seq = self.next_seq(npc_id)
        event = new_event(
            seq=seq,
            event_name=event_name,
            npc_id=npc_id,
            data=data,
            entities=entities,
            tags=tags,
            summary=summary,
            importance=importance,
            game_time=game_time,
            location=location,
            timestamp=timestamp,
        )
        return self.append(event)

    # -- reads ------------------------------------------------------------
    def iter_events(self, npc_id: str) -> Iterator[Event]:
        path = self.path_for(npc_id)
        if not path.exists():
            return
        with path.open("r", encoding="utf-8") as fh:
            for line_no, line in enumerate(fh, 1):
                line = line.strip()
                if not line:
                    continue
                try:
                    payload = json.loads(line)
                    yield Event.from_dict(payload)
                except (json.JSONDecodeError, KeyError, TypeError, ValueError) as exc:
                    raise ValueError(
                        f"invalid timeline line {path}:{line_no}: {exc}"
                    ) from exc

    def recent_events(self, npc_id: str, limit: int = 20) -> List[Event]:
        if limit <= 0:
            return []
        path = self.path_for(npc_id)
        events: List[Event] = []
        for line in self._tail_nonempty_lines(path, limit):
            try:
                payload = json.loads(line.decode("utf-8"))
                events.append(Event.from_dict(payload))
            except (json.JSONDecodeError, KeyError, TypeError, ValueError) as exc:
                raise ValueError(f"invalid timeline line {path}: {exc}") from exc
        return events

    def all_events(self, npc_id: str) -> List[Event]:
        return list(self.iter_events(npc_id))

    def grab_timeline(
        self,
        npc_id: str,
        *,
        event_name: Optional[str] = None,
        event_names: Optional[Iterable[str]] = None,
        tag: Optional[str] = None,
        tags: Optional[Iterable[str]] = None,
        entity: Optional[str] = None,
        minimum_importance: Optional[float] = None,
        since_seq: Optional[int] = None,
        before_seq: Optional[int] = None,
        limit: int = 20,
    ) -> List[Event]:
        """Deterministic timeline retrieval.

        Filters compose with AND.  Multiple values passed to ``event_names``
        or ``tags`` compose with OR within that field, matching the simple
        retrieval examples in the specification.
        """
        names = set(event_names or [])
        if event_name:
            names.add(event_name)
        tag_set = set(tags or [])
        if tag:
            tag_set.add(tag)

        matches: List[Event] = []
        for event in self.iter_events(npc_id):
            if names and event.event_name not in names:
                continue
            if tag_set and not (tag_set & set(event.tags)):
                continue
            if entity and entity not in event.entities:
                continue
            if minimum_importance is not None:
                if event.importance is None or event.importance < minimum_importance:
                    continue
            if since_seq is not None and event.seq <= since_seq:
                continue
            if before_seq is not None and event.seq >= before_seq:
                continue
            matches.append(event)

        if limit > 0:
            matches = matches[-limit:]
        return matches

    # -- maintenance ------------------------------------------------------
    def has_timeline(self, npc_id: str) -> bool:
        return self.path_for(npc_id).exists()

    def delete(self, npc_id: str) -> None:
        try:
            self.path_for(npc_id).unlink()
        except FileNotFoundError:
            pass
        self._last_seq.pop(npc_id, None)
