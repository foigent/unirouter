import os

from langchain_openai import ChatOpenAI


model = ChatOpenAI(
    api_key=os.environ["UNIROUTER_API_KEY"],
    base_url=os.environ["UNIROUTER_BASE_URL"],
    model=os.environ["UNIROUTER_MODEL"],
)
print(model.invoke("Reply with exactly: unirouter-ok").content)
