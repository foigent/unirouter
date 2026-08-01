# PRD7 B1 Provider Conformance and Inspector — Local Evidence

> Date: 2026-07-31
> Work item: `UR-PRD7-B1-PROVIDER-CONFORMANCE`
> Risk: R2/R3 provider adapter and user-visible Admin UI
> Status: Local verified
> Start head: `67e6d7903ac66b9170c2314147a3191570e8925a`
> Start tree: `2faa9df91c6b9d8e512f45d5d9dfbdc0af7d47af`
> Branch: `codex/product-iteration-20260730`

## Delivered result

B1 implements:

- strict embedded Provider Conformance Pack V1 artifacts for OpenAI,
  Anthropic and Azure OpenAI;
- a deterministic zero-network Go runner and checked-in golden report;
- a native deployment-scoped Azure OpenAI Chat/Embeddings adapter;
- atomic validation of the resulting adapter type/config pair on partial
  channel updates;
- platform-admin `GET /api/v1/provider-packs`, OpenAPI and route parity;
- a no-second-click persistent Provider compatibility inspector with
  `channel_id` deep links and keyboard row selection.

Provider Pack matching uses `(provider, adapter_type)`. A Kimi channel using
`openai_compatible` is explicitly `undeclared`; it does not inherit OpenAI's
fixture result.

## Truth and safety boundaries

- `fixture_verified` means only that the exact checked-in synthetic fixture
  passed the offline runner.
- `network_verified` remains `false`.
- Missing provider usage is not normalized to zero.
- No sensitive adapter config is returned by the catalog or displayed by the
  inspector. The channel API retains its existing admin-only configuration
  response; the inspector allowlists only non-secret protocol fields.
- Azure accepts only a credential-free HTTPS origin, GA `2024-10-21`,
  deployment-scoped paths with single-pass URL encoding, and explicit
  `api_key`/`entra_bearer` wire modes.
- No provider request, credential read/write, invoice, deployment, traffic,
  non-test database, Feed, Outbox, Cursor/Checkpoint, X-Insight or shared
  protocol mutation occurred.

## Immutable artifact identities

| Artifact | SHA-256 |
|----------|---------|
| Pack schema | `f2252611cc0bb84c98ef39a1bba50284aeeb09ec782046f0a15847135896377e` |
| Anthropic manifest | `2cc0948f70a70ade76f1dc6e5868e971e8c71cd3c4404028960d50e4895a2c67` |
| Azure OpenAI manifest | `d38c6ffe9147790d7cdbaa4f01fd6e772df29dbe2acd63e1e607244282fde9d6` |
| OpenAI manifest | `aca97cdb6199acab6cb5107ba4213eec88bb873c415d0085d4c4176c96a0ced9` |
| Anthropic Chat fixture | `34a12e76ec3d77259c91cf8d7a7486f976ed0d0fbc925f82093da7669da5da6e` |
| Azure OpenAI Chat / Embeddings fixtures | `8f9f36ff23c7d61d2708ac97c8cd09fd7ff631985622ec9de86ac9638581bb49` / `c38f2d0e6b92539125d5aa170a433582ad680cafb82ebe857592aac65bb7df69` |
| OpenAI Chat / Embeddings fixtures | `6b95c320fb75b9e74e30dc2a5e07a282ad83e20329a19e4271d8fb79ac87a861` / `5ba8db14c310d1ca60e953dc27806e2b71ea5dcaee3210b3ecd5849dc9d6b150` |
| Golden report | `803111ecad2b365508bcac0afae2d92185d7d12de3190db31653c8eb72b33488` |

## Verification

| Command/check | Actual result |
|---------------|---------------|
| `GOCACHE=/tmp/unirouter-go-cache go test ./... -count=1` | PASS — all Go packages, including adapters, provider pack, Admin route parity and runner |
| `GOCACHE=/tmp/unirouter-go-cache go vet ./...` | PASS |
| runner twice with separate reports, then `cmp` | PASS — both reports byte-identical |
| runner report compared with checked-in golden | PASS — byte-identical; 3 packs, 5 fixtures, 20 cases, 20 passed, 0 failed |
| `npm test -- --reporter=dot --silent=passed-only` | PASS — 41 files, 300 tests |
| `npm run type-check` | PASS |
| `npm run build` | PASS — TypeScript and Vite production build |
| synthetic Playwright at 1440×1000 | PASS — inspector visible by default, side by side, first row 58 px, no detail overflow |
| synthetic Playwright at 1024×900 | PASS — inspector remains side by side, no detail overflow |
| synthetic Playwright at 390×844 | PASS — inspector stacks without a detail-open action; page has no horizontal overflow |
| Kimi selection and reload | PASS — `channel_id=13` persists; `undeclared`; no Pack identity card |
| final browser console | PASS — 0 errors, 0 warnings |
| `git diff --check` | PASS |

The real-browser pass found and closed two pre-evidence defects: the wide
channel table initially collapsed text vertically after the inspector became
persistent, and long SHA values overflowed the detail card. Explicit table
width/scroll behavior and bounded digest wrapping fixed both before this
evidence was recorded. Narrow pagination hides redundant range text because
the channel count remains visible above the table.

The production build retains the pre-existing Vite large-chunk warning. No
bundle-size or performance claim is made by B1.

## Evidence level and next batch

P7-F03 and the Provider portion of P7-F02 are `Local verified`. This is not CI,
Target, external-integration or Production evidence. Provider Change Firewall
promotion is not implemented by B1.

The next dependency-ordered repository-local batch is B2: persistent
inspectors and saved/deep-linked state for Audit and Projects.
