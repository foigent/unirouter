# Agent client compatibility refresh — 2026-08-02

## Decision

UniRouter should productize compatibility through `GatewayEndpoint × EndpointProfile × ClientProfile × WorkloadApplication`, not through client-specific first- or second-level menus. Chat Completions is the POC runtime path. `/v1/responses` remains a contract-only future Endpoint Profile.

## Current official-source findings

| Client | Official configuration fact | UniRouter classification |
|---|---|---|
| OpenAI Agents SDK | The SDK defaults to Responses, but supports `OpenAIChatCompletionsModel` with a custom `AsyncOpenAI(base_url, api_key)`. Strict feature validation can fail on Responses-only features. | Chat path: protocol-compatible; default Responses: unsupported |
| LangChain | `ChatOpenAI` accepts a custom `base_url` for OpenAI Chat Completions-compatible endpoints and does not preserve non-standard response fields. | Protocol-compatible |
| AutoGen | `OpenAIChatCompletionClient` is the current model client; official docs warn that OpenAI-compatible providers can have response differences. | Protocol-compatible until exact-version POC |
| OpenClaw | A custom provider uses `baseUrl`; `api: openai-completions` is the correct adapter for `/v1/chat/completions`; `openai-responses` must only be selected when `/v1/responses` exists. | Protocol-compatible |
| Tencent WorkBuddy Enterprise | The current official `models.json` format accepts only OpenAI-style APIs and expects a full URL normally ending in `/chat/completions`; its rendered config contains the actual key. | Protocol-compatible; render only on target device |
| Feishu bot | Event callbacks require an HTTP 200 within 3 seconds; time-consuming work should be asynchronous and callbacks can be retried. | Isolated adapter required |
| Feishu Aily | Exact contract depends on a real enterprise tenant/application and cannot be certified from public docs alone. | Unsupported until environment-backed evidence |

## Sources

- OpenAI Agents SDK: https://openai.github.io/openai-agents-python/models/
- OpenAI Agents SDK configuration: https://openai.github.io/openai-agents-python/config/
- LangChain OpenAI-compatible endpoints: https://docs.langchain.com/oss/python/concepts/providers-and-models
- AutoGen model clients: https://microsoft.github.io/autogen/stable/user-guide/agentchat-user-guide/tutorial/models.html
- OpenClaw custom providers: https://docs.openclaw.ai/gateway/config-tools
- Tencent WorkBuddy Enterprise `models.json`: https://cloud.tencent.com/document/product/1831/134513
- Tencent WorkBuddy Enterprise model configuration: https://cloud.tencent.com/document/product/1831/134445
- Feishu event callback optimization: https://open.feishu.cn/document/event-subscription-guide/event-subscriptions/event-callback-optimization-guide?lang=zh-CN

## Evidence boundary

These sources establish configuration capability, not UniRouter end-to-end compatibility. Evidence is promoted only after the exact client version, configured Gateway Endpoint, application credential, approved model and expected response semantics pass on the target device.
