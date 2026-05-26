import asyncio

from gemini.gemini import Gemini
from gemini.xiaoai import XiaoAi


async def main():
    xiaoai_task = asyncio.create_task(XiaoAi.start())
    gemini_task = asyncio.create_task(
        Gemini.start(
            on_audio=XiaoAi.output_audio,
            set_is_speaking=XiaoAi.set_is_speaking,
        )
    )
    await asyncio.gather(xiaoai_task, gemini_task)


if __name__ == "__main__":
    asyncio.run(main())
