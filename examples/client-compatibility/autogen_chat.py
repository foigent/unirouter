import asyncio
import os

from autogen_core.models import UserMessage
from autogen_ext.models.openai import OpenAIChatCompletionClient


async def main() -> None:
    client = OpenAIChatCompletionClient(
        api_key=os.environ["UNIROUTER_API_KEY"],
        base_url=os.environ["UNIROUTER_BASE_URL"],
        model=os.environ["UNIROUTER_MODEL"],
        model_info={"vision": False, "function_calling": True, "json_output": True, "family": "unknown", "structured_output": False},
    )
    try:
        result = await client.create([UserMessage(content="Reply with exactly: unirouter-ok", source="user")])
        print(result.content)
    finally:
        await client.close()


if __name__ == "__main__":
    asyncio.run(main())
