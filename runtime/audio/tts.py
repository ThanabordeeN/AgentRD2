"""Text-to-speech adapters."""
from __future__ import annotations

from typing import Optional, Protocol


class TextToSpeech(Protocol):
    def synthesize(self, text: str, *, emotion: Optional[str] = None) -> bytes:  # pragma: no cover
        ...


class NullTTS:
    """Placeholder TTS that returns no audio."""

    def synthesize(self, text: str, *, emotion: Optional[str] = None) -> bytes:
        return b""


class Pyttsx3TTS:
    """Optional offline TTS adapter.

    Install ``pyttsx3`` and save audio through the platform driver.
    """

    def __init__(self) -> None:
        try:
            import pyttsx3  # type: ignore
        except ImportError as exc:  # pragma: no cover - optional path
            raise RuntimeError("pyttsx3 is not installed") from exc
        self.engine = pyttsx3.init()

    def synthesize(self, text: str, *, emotion: Optional[str] = None) -> bytes:
        # pyttsx3 is playback-oriented; production should render to a file
        # using the selected driver.  Keep this adapter intentionally small.
        self.engine.say(text)  # pragma: no cover - optional path
        self.engine.runAndWait()
        return b""
