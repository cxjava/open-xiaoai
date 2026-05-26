import os
from typing import Awaitable, Callable, Optional
from google import genai
from google.genai import types

GEMINI_MODEL = "gemini-2.0-flash-live-001"
GEMINI_API_KEY = os.getenv("GEMINI_API_KEY") or "你的 API KEY"


class Gemini:
    running = False

    client = genai.Client(api_key=GEMINI_API_KEY)

    session: Optional[genai.live.AsyncSession] = None

    config = types.LiveConnectConfig(
        response_modalities=[types.Modality.AUDIO],
        system_instruction=types.Content(
            parts=[types.Part(text="你是小爱音箱，请用中文回答用户的问题。")]
        ),
        speech_config=types.SpeechConfig(
            language_code="cmn-CN",
            voice_config=types.VoiceConfig(
                prebuilt_voice_config=types.PrebuiltVoiceConfig(voice_name="Leda")
            ),
        ),
        context_window_compression=(
            types.ContextWindowCompressionConfig(
                sliding_window=types.SlidingWindow(),
            )
        ),
    )

    @classmethod
    async def send_text(cls, text: str):
        if not cls.session:
            return
        await cls.session.send_client_content(
            turns={"role": "user", "parts": [{"text": text}]},
            turn_complete=True,
        )

    @classmethod
    async def send_audio(cls, data: bytes):
        if not cls.session:
            return
        await cls.session.send_realtime_input(
            audio=types.Blob(data=data, mime_type="audio/pcm;rate=16000")
        )

    @classmethod
    def stop(cls):
        cls.running = False
        if cls.session:
            cls.session.close()

    @classmethod
    async def start(
        cls,
        on_audio: Optional[Callable[[bytes], Awaitable[None]]] = None,
        on_text: Optional[Callable[[str], Awaitable[None]]] = None,
        set_is_speaking: Optional[Callable[[bool], Awaitable[None]]] = None,
    ):
        if cls.running:
            return

        cls.running = True

        async with cls.client.aio.live.connect(
            model=GEMINI_MODEL, config=cls.config
        ) as session:
            cls.session = session

            print("🔊 AI: ", "session connected")

            while True:
                print("🔊 AI: ", "waiting for response")

                if not cls.running:
                    break

                if set_is_speaking:
                    await set_is_speaking(False)

                async for response in session.receive():
                    if response.server_content is None:
                        continue

                    if response.server_content.interrupted is True:
                        continue

                    if set_is_speaking:
                        await set_is_speaking(True)

                    if response.data is not None:
                        print("🔊 AI: ", len(response.data))
                        if on_audio:
                            await on_audio(response.data)

                    if response.text is not None:
                        print("✅ AI: ", response.text)
                        if on_text:
                            await on_text(response.text)
