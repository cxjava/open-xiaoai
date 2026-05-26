import asyncio

import numpy as np
import open_xiaoai_server

from gemini import Gemini


class XiaoAi:
    loop: asyncio.AbstractEventLoop
    is_ai_speaking = False
    speaking_count = 0

    @classmethod
    async def start(cls):
        cls.loop = asyncio.get_event_loop()
        open_xiaoai_server.register_fn("on_input_data", cls.input_audio)
        await open_xiaoai_server.start_server()

    @classmethod
    async def output_audio(cls, data: bytes):
        await open_xiaoai_server.on_output_data(data)

    @classmethod
    def input_audio(cls, data: bytes):
        if cls.is_ai_speaking:
            # 如果 AI 正在回答问题，则不发送音频，防止把 AI 的声音被当做输入录制进来
            # 暂不支持中断 AI 的回复，需要等待 AI 回答完成后才能重新响应用户的语音输入
            return

        async def send_audio_task():
            audio_array = np.frombuffer(data, dtype=np.uint16)
            await Gemini.send_audio(audio_array.tobytes())

        asyncio.run_coroutine_threadsafe(send_audio_task(), cls.loop)

    @classmethod
    async def set_is_speaking(cls, is_speaking: bool):
        if is_speaking:
            cls.speaking_count += 1
            cls.is_ai_speaking = True
            return

        # 延迟 1 秒，如果 AI 还在说话，则设置为不说话
        async def set_is_speaking_task():
            speaking_count = cls.speaking_count
            await asyncio.sleep(1)
            if cls.speaking_count == speaking_count:
                cls.is_ai_speaking = False

        asyncio.run_coroutine_threadsafe(set_is_speaking_task(), cls.loop)
