# Provider Adapter Framework

> Version: v0.1
> Phase: PRD2
> Status: Draft for Review
> Related PRD: `docs/PRD-Phase2.md` P2-F05

## 1. Goal

UniRouter must keep one stable downstream contract:

```text
Client -> UniRouter OpenAI-compatible /v1/*
```

Provider differences should be handled upstream through adapters:

```text
UniRouter -> provider adapter -> upstream provider HTTP API
```

This preserves the product value: enterprise developers use one API while admins can connect multiple provider protocols behind the gateway.

## 2. Current Baseline

Current PRD1 relay behavior is an OpenAI-compatible HTTP relay:

- Route by requested model.
- Select channel and channel key.
- Apply `model_mapping`.
- Build upstream URL from `base_url`.
- Send `Authorization: Bearer <key>`.
- Forward to `/v1/chat/completions` or `/v1/embeddings`.
- Parse OpenAI-compatible usage fields.
- Inject `_uniroute`.

PRD2 should refactor this behavior into the first adapter type:

```text
adapter_type = openai_compatible
```

This is a compatibility refactor, not a downstream API change.

## 3. Non-Goals

- Do not expose Anthropic/Gemini/Baidu/Tongyi/Wenxin native APIs to downstream clients.
- Do not require customers to change SDKs.
- Do not implement all native providers in the first PRD2 pass.
- Do not add cross-model fallback.
- Do not let provider-specific payloads leak into billing, call logs, or circuit breaker logic.

## 4. Adapter Types

| Adapter Type | Phase | Purpose |
|--------------|-------|---------|
| `openai_compatible` | P0 | Existing relay semantics for OpenAI-compatible upstreams |
| `anthropic_native` | P1 proof in progress | Native Anthropic Messages API non-stream proof; native stream translation remains a later batch |
| `gemini_native` | P1/P2 candidate | Native Gemini API proof if customer demand requires |
| `vendor_native_*` | Later | Baidu, Tongyi, Wenxin, or other native providers |

## 5. Channel Model Changes

`channels` should include:

| Field | Purpose |
|-------|---------|
| `adapter_type` | Adapter registry key |
| `base_url` | Provider API root |
| `provider` | Display/provider family label |
| `models` | UniRouter-visible models or aliases supported by this channel |
| `model_mapping` | Requested model/alias to upstream model mapping |
| `adapter_config` | JSONB for provider-specific config, optional |

Migration rule:

- Existing channels get `adapter_type = 'openai_compatible'`.
- Existing behavior must remain unchanged after migration.

## 6. Adapter Interface

Draft Go shape:

```go
type Adapter interface {
    Type() string
    BuildRequest(ctx context.Context, in RequestInput) (*http.Request, error)
    ParseResponse(ctx context.Context, in ResponseInput) (*NormalizedResponse, error)
    Stream(ctx context.Context, in StreamInput) (*StreamResult, error)
    Test(ctx context.Context, in TestInput) (*TestResult, error)
}
```

Core data structures:

```go
type RequestInput struct {
    Endpoint       Endpoint
    DownstreamBody []byte
    RequestedModel string
    MappedModel    string
    Channel        ChannelSnapshot
    APIKey         string
    RequestID      string
}

type NormalizedResponse struct {
    StatusCode int
    Header     http.Header
    Body       []byte
    Usage      Usage
    Meta       ProviderMeta
    Error      *NormalizedError
}

type Usage struct {
    PromptTokens     int
    CompletionTokens int
    TotalTokens      int
    Source           string
}

type ProviderMeta struct {
    Provider      string
    ChannelID     int64
    ChannelName   string
    RequestedModel string
    UpstreamModel  string
    TTFTMillis     int
}

type NormalizedError struct {
    HTTPStatus int
    Category   string
    Code       string
    Message    string
    Retryable  bool
    FailureKind FailureKind
}
```

The exact Go names may change during implementation, but the boundary is fixed: relay consumes normalized output.

## 7. Failure Classification

Adapters must map provider-specific errors into UniRouter's routing classes.

| Provider Outcome | Normalized Failure |
|------------------|--------------------|
| 400 client request error | no channel failure |
| 401/403 auth error | key invalid or cooldown depending provider semantics |
| 408 timeout | channel failure |
| 429 rate limit | key/channel cooldown, not permanent invalidation |
| 500/502/503 provider error | channel failure and failover candidate |
| connection timeout | channel failure |
| stream broken before usage | settle with estimated usage if chunks were received |

This preserves PRD1 F12 classification behavior.

## 8. Streaming Contract

All adapters must normalize streaming into UniRouter's downstream SSE contract:

- `Content-Type: text/event-stream`
- forward OpenAI-compatible data chunks to client
- final data chunk includes usage and `_uniroute`
- `[DONE]` is sent once at the end
- client disconnect cancels upstream
- missing upstream usage uses token metering fallback

Native adapters may need to translate event formats. Example:

- Anthropic `message_start` usage maps to prompt/input tokens.
- Anthropic `message_delta` usage maps to completion/output tokens.
- Gemini final usage maps to prompt/completion tokens where available.

## 9. Billing And Logs Boundary

Billing must not parse provider-specific response bodies.

Billing consumes:

- normalized usage
- usage source
- requested model or alias
- resolved upstream model where needed
- price lookup result

Call logs should record:

- requested model/alias
- resolved upstream model
- provider
- channel
- adapter_type
- usage source
- normalized error code

## 10. Adapter Registry

Draft registry behavior:

```go
type Registry struct {
    adapters map[string]AdapterFactory
}

func (r *Registry) Get(adapterType string) (Adapter, error)
```

Rules:

- Unknown adapter type is a configuration error.
- Admin channel test should fail clearly if adapter type is unknown.
- New adapters must provide mock upstream tests before being enabled in PRD2 acceptance.

## 11. Test Requirements

Every adapter must include:

- non-stream request conversion test
- non-stream usage parsing test
- stream success test
- stream missing-usage test
- provider 401/403 classification test
- provider 429 classification test
- provider 5xx failover/circuit classification test
- channel test behavior
- model_mapping behavior where applicable

`openai_compatible` must additionally prove existing PRD1 providers still work through the refactor path.

## 12. Current Implementation Notes

As of PRD2 Batch 12 local implementation, `anthropic_native` is registered in the default adapter registry for non-stream chat completions:

- Downstream clients still call UniRouter's OpenAI-compatible `/v1/chat/completions`.
- The adapter converts OpenAI-compatible chat requests to Anthropic `/v1/messages`.
- Anthropic `input_tokens/output_tokens` are normalized to `prompt_tokens/completion_tokens`.
- Anthropic non-stream responses are converted back to OpenAI-compatible `chat.completion` response bodies before UniRouter injects `_uniroute`.
- Adapter-specific HTTP failure classification is used by relay routing/cooldown logic.
- Admin channel test and channel configuration support `adapter_type = anthropic_native`.

Known Batch 12 boundary:

- `anthropic_native` does not support embeddings.
- `anthropic_native` rejects `stream: true` until native SSE translation is implemented.
- Real upstream Anthropic E2E remains pending configured provider credentials.

## 13. Implementation Order

1. Add `adapter_type` migration with default `openai_compatible`.
2. Introduce normalized adapter data types.
3. Wrap current relay request/response behavior as `openai_compatible`.
4. Move response/usage parsing behind adapter boundary.
5. Move channel test behind adapter boundary.
6. Add adapter mock upstream tests.
7. Implement first native adapter only after P0 path is stable.
