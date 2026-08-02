import os

from openai import OpenAI


client = OpenAI(
    api_key=os.environ["UNIROUTER_API_KEY"],
    base_url=os.environ["UNIROUTER_BASE_URL"],
)

response = client.chat.completions.create(
    model=os.environ["UNIROUTER_MODEL"],
    messages=[{"role": "user", "content": "Reply with exactly: unirouter-ok"}],
)
print(response.choices[0].message.content)
