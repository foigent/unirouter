import asyncio
import os

from agents import Agent, AsyncOpenAI, OpenAIChatCompletionsModel, Runner, set_tracing_disabled


async def main() -> None:
    set_tracing_disabled(True)
    client = AsyncOpenAI(
        api_key=os.environ["UNIROUTER_API_KEY"],
        base_url=os.environ["UNIROUTER_BASE_URL"],
    )
    model = OpenAIChatCompletionsModel(
        model=os.environ["UNIROUTER_MODEL"],
        openai_client=client,
        strict_feature_validation=True,
    )
    agent = Agent(name="UniRouter POC", instructions="Reply briefly.", model=model)
    result = await Runner.run(agent, "Reply with exactly: unirouter-ok")
    print(result.final_output)


if __name__ == "__main__":
    asyncio.run(main())
