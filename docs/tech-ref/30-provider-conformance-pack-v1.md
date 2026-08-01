# Provider Conformance Pack V1

> Version: v1.0
> Date: 2026-07-31
> Status: Local verified
> Scope: UniRouter product-local provider compatibility contract

## Purpose

Provider Conformance Pack V1 separates three statements that the product must
not collapse:

1. an adapter is registered in UniRouter;
2. a named provider/adapter pair passes an immutable offline fixture contract;
3. a real account, network path, quota and bill were observed.

V1 proves only statement 2. The Admin API and UI therefore expose
`fixture_verified` and `network_verified=false` separately. No credential,
provider request or billing fact is used by the pack.

This is a UniRouter product-local provider contract. It does not redefine the
Foigent Event & Connector protocol, completed-execution Feed, event identity,
Outbox, Cursor or Checkpoint semantics.

## Identity and matching

Each pack has an immutable identity:

- `pack_id`;
- SemVer `pack_version`;
- `provider`;
- `adapter_type`;
- manifest and fixture SHA-256 digests.

Compatibility lookup uses the full `(provider, adapter_type)` pair. Adapter
type alone is insufficient: a Kimi or DeepSeek channel using
`openai_compatible` does not inherit the OpenAI pack claim.

The embedded catalog rejects malformed JSON, duplicate identities, duplicate
fixture paths, digest drift and unknown manifest fields before returning a
claim.

## Frozen V1 artifacts

| Artifact | SHA-256 |
|----------|---------|
| `provider-pack.v1.schema.json` | `f2252611cc0bb84c98ef39a1bba50284aeeb09ec782046f0a15847135896377e` |
| `anthropic.v1.json` manifest | `2cc0948f70a70ade76f1dc6e5868e971e8c71cd3c4404028960d50e4895a2c67` |
| `azure-openai.v1.json` manifest | `d38c6ffe9147790d7cdbaa4f01fd6e772df29dbe2acd63e1e607244282fde9d6` |
| `openai.v1.json` manifest | `aca97cdb6199acab6cb5107ba4213eec88bb873c415d0085d4c4176c96a0ced9` |
| Anthropic Chat fixture | `34a12e76ec3d77259c91cf8d7a7486f976ed0d0fbc925f82093da7669da5da6e` |
| Azure OpenAI Chat fixture | `8f9f36ff23c7d61d2708ac97c8cd09fd7ff631985622ec9de86ac9638581bb49` |
| Azure OpenAI Embeddings fixture | `c38f2d0e6b92539125d5aa170a433582ad680cafb82ebe857592aac65bb7df69` |
| OpenAI Chat fixture | `6b95c320fb75b9e74e30dc2a5e07a282ad83e20329a19e4271d8fb79ac87a861` |
| OpenAI Embeddings fixture | `5ba8db14c310d1ca60e953dc27806e2b71ea5dcaee3210b3ecd5849dc9d6b150` |
| Golden conformance report | `803111ecad2b365508bcac0afae2d92185d7d12de3190db31653c8eb72b33488` |

## Offline conformance

Run from the repository root:

```bash
go run ./cmd/uniroute-provider-conformance --report /tmp/provider-report.json
```

The zero-network runner emits
`unirouter.provider-conformance-report.v1`. V1 contains three packs, five
endpoint/auth fixtures and four cases per fixture:

1. request wire shape and provider-specific headers;
2. success usage and finality normalization;
3. missing usage remains missing rather than becoming zero;
4. provider error classification.

Reports are deterministic byte-for-byte. The checked-in golden report is the
expected result for the embedded V1 artifacts.

## Azure OpenAI adapter

`azure_openai` is a native upstream adapter, not an alias for generic
OpenAI-compatible routing.

- The base URL must be a credential-free HTTPS origin with no path, query or
  fragment.
- Chat and Embeddings use the deployment-scoped Azure path.
- The default and only V1 API version is the GA date `2024-10-21`.
- `api_key` sends `api-key`; `entra_bearer` sends `Authorization: Bearer`.
- The deployment is authoritative in the URL, so the downstream `model` field
  is removed from the Azure request body.
- Deployment names reject path/query/fragment separators and are URL-encoded
  exactly once.
- Adapter config is strict JSON with duplicate and unknown fields rejected.
- Updating only `adapter_type` or `adapter_config` locks and validates the
  resulting persisted pair before mutation.
- Missing usage remains unavailable; provider HTTP and transport errors use
  the existing normalized failure contract.

The endpoint and authentication choices follow Microsoft's official Azure
OpenAI REST documentation:

- <https://learn.microsoft.com/en-us/rest/api/microsoft-foundry/azureopenai/completions>
- <https://learn.microsoft.com/en-us/azure/foundry/foundry-models/concepts/endpoints>

Those references justify the wire contract; they are not live-account
evidence.

## Admin API and product UI

`GET /api/v1/provider-packs` is platform-admin scoped and returns only
credential-free catalog data, exact digests and the explicit claim boundary.

The Channels page keeps a Provider compatibility inspector visible:

- the first channel is selected automatically;
- `channel_id` restores selection;
- mouse and keyboard row selection are supported;
- desktop and medium layouts keep list and inspector side by side;
- narrow layouts stack the inspector below the list without a detail-open
  action;
- only non-secret adapter facts are allowlisted for display;
- fixture, network and circuit status remain separate.

The table uses horizontal scrolling for its wide operational columns; it does
not shrink them into unreadable vertical text when the inspector is present.

## Explicit non-goals

- No real Provider, account, network, quota, latency, invoice or production
  compatibility claim.
- No Provider Change Firewall promotion or automatic runtime enablement.
- No credentials in manifests, fixtures, reports, API responses or UI.
- No shared high-privilege plugin runtime or SDK extension point into routing,
  billing or IAM.
- No change to downstream Chat/Embeddings APIs, request records, financial
  results, Outbox identity, Feed API or cursor behavior.
