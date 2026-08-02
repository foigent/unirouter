# UniRouter client compatibility examples

These examples bind an explicit UniRouter Gateway Endpoint, application credential and approved model alias. They never infer a Base URL from a browser location and contain no real credential.

Required environment for Python examples:

```bash
export UNIROUTER_BASE_URL='https://gateway.example.internal/v1'
export UNIROUTER_API_KEY='replace-at-runtime'
export UNIROUTER_MODEL='approved-model-alias'
```

Evidence labels are intentionally fail-closed:

- OpenAI SDK Chat Completions is `verified` by the current UniRouter protocol suite; the exact external-device run must still be recorded for the POC environment.
- OpenAI Agents SDK must use `OpenAIChatCompletionsModel`; its default Responses path remains unsupported.
- LangChain, AutoGen, OpenClaw and Tencent WorkBuddy remain `protocol_compatible` until their exact versions pass the second-integration run.
- Feishu Bot and Feishu Aily require isolated adapters and a real tenant; they are not direct Gateway client examples.

Tencent WorkBuddy currently expects a full `/v1/chat/completions` URL in its `models.json`. Its file format embeds the key value, so generate the file only on the target device, restrict permissions and never commit the rendered file.
