"""Speech-to-text adapters.

MVP capture is controlled by Push-to-Talk.  This module provides a tiny
interface plus optional local implementations; the core runtime never imports
heavy audio dependencies unless explicitly configured.
"""
from __future__ import annotations

from typing import Optional, Protocol


class SpeechToText(Protocol):
    def transcribe(self, audio: bytes, *, sample_rate: int = 16000) -> str:  # pragma: no cover
        ...


class NullSTT:
    """Placeholder used when no STT engine is configured."""

    def transcribe(self, audio: bytes, *, sample_rate: int = 16000) -> str:
        return ""


class FasterWhisperSTT:
    """Optional local STT adapter.

    Install ``faster-whisper`` to use it.  Heavy model loading is deferred to
    construction time, which the runtime should perform outside the game
    thread.
    """

    def __init__(self, model_size: str = "base", device: str = "cpu"):
        try:
            from faster_whisper import WhisperModel  # type: ignore
        except ImportError as exc:  # pragma: no cover - optional path
            raise RuntimeError("faster-whisper is not installed") from exc
        self.model = WhisperModel(model_size, device=device)

    def transcribe(self, audio: bytes, *, sample_rate: int = 16000) -> str:
        # Writing raw PCM to a temp WAV is left to the caller/audio layer.
        segments, _ = self.model.transcribe(audio)  # pragma: no cover - optional path
        return " ".join(segment.text.strip() for segment in segments).strip()
