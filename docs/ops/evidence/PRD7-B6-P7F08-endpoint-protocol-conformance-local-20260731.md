# PRD7 B6 Endpoint Protocol Conformance — Local Evidence

> Date: 2026-07-31
> Work item: `UR-PRD7-B6-ENDPOINT-PROTOCOL-CONFORMANCE`
> Requirement: P7-F08
> Result: Local verified

## Outcome

UniRouter now has a separate, fixed Endpoint Protocol V1 layer for:

1. OpenAI Responses text-only/stateless requests;
2. Cohere Rerank V2 string-document requests;
3. OpenAI GPT Image single-inline-result requests.

The layer is contract and conformance evidence only. It is not registered in
the Gateway runtime, does not alter Provider Pack V1, and does not create
customer billing authority.

## Exact Evidence

| Artifact | SHA-256 |
|----------|---------|
| manifest Schema | `e1546a5a7638e400f89b43ea6684e2879a072eb13f0a74a312979aea356752cc` |
| Cohere rerank manifest | `0860ccff6d441738c494cfc5d8adaf06cac56455b551041b34135975de7604e2` |
| OpenAI GPT Image manifest | `792d54b5341ae13425afd24ffa864c068b705043f2b09f29c6c717963b9febd7` |
| OpenAI Responses manifest | `4757286316fb7f323cfbb5ef7b317ccf469ff341af7ff2c50494b52a13f1cc4a` |
| Cohere rerank fixture | `d7de1ac04e98d7dd73d1616115f05d60daa3598fa747f26b0bbf48a4ce4ab49f` |
| OpenAI GPT Image fixture | `3b802f15539ff4fe0309ca93a779a3639da5910b0f28df3087aa24520fa16755` |
| OpenAI Responses fixture | `005c126da2d47bab7fdd4056d5e0c343fb74d5983c8d8c6288f943566b44d4c9` |
| deterministic golden report | `ab3d4ec66077922ce921f1def0ccfa501ba8bf5866190c3581fa5c71a91b5759` |

The golden report is byte-identical to two independent local runner
invocations and reports:

- profiles: `3`;
- cases: `25`;
- passed: `25`;
- failed: `0`.

## Verified Invariants

- HTTPS origin-only provider targets and explicit model mapping;
- strict nested duplicate-key rejection;
- deterministic request paths, bytes and OpenAI request-ID header;
- stateless/non-streaming Responses request and completed text output;
- exact Responses usage total plus cache-read/cache-write observations;
- rerank string-document, source-index, score-order and API-version semantics;
- exact positive `search_units` without exponent or floating-point conversion;
- one strict base64 image and exact input/output usage partitions;
- provider observations remain
  `provider_observed_not_customer_chargeable`;
- unsupported state/tools/media/output semantics fail explicitly;
- safe error codes do not contain prompt, document, image or credential data;
- retry classes match the existing UniRouter HTTP failure contract;
- image profile remains `decision_gated_p7_d03`.

## Verification Commands And Results

```text
GOCACHE=/tmp/unirouter-go-cache go test -race ./internal/endpointprotocol ./cmd/uniroute-endpoint-conformance
PASS

GOCACHE=/tmp/unirouter-go-cache go vet ./internal/endpointprotocol ./cmd/uniroute-endpoint-conformance
PASS

GOCACHE=/tmp/unirouter-go-cache go run ./cmd/uniroute-endpoint-conformance
endpoint conformance passed: profiles=3 cases=25

cmp /tmp/unirouter-endpoint-report.json /tmp/unirouter-endpoint-report-second.json
PASS, byte-identical

GOCACHE=/tmp/unirouter-go-cache go test -p 1 ./...
PASS, all Go packages

GOCACHE=/tmp/unirouter-go-cache go vet ./...
PASS

cd frontend && npm test -- --run --reporter=dot
PASS, 42 files / 308 tests

cd frontend && npm run type-check
PASS

cd frontend && npm run build
PASS
```

The frontend suite retained pre-existing Semi UI/React Router/`act(...)`
warnings. This batch changes no frontend source. Vite retained its existing
large-chunk warning.

## Compatibility Proof

All executable changes are new files under:

- `internal/endpointprotocol`;
- `cmd/uniroute-endpoint-conformance`.

No Gateway, adapter, IAM, routing, billing, database, Outbox, completed Feed,
cursor, response-cache or frontend executable file changed. Full Go and
frontend suites passed. Provider Pack V1 manifests, fixtures and golden digest
remain untouched.

## Explicit Non-Claims

This evidence is not:

- live provider or credential verification;
- Gateway endpoint/runtime support;
- customer price or ledger authority;
- image/media storage, retention or async-job implementation;
- CI, Target, External integration or Production evidence;
- authorization for deployment, traffic, database, cursor/checkpoint, push,
  PR or merge.

The next dependency-ordered local batch is B7: the provider COGS offline
connector and Financial Truth workspace. P7-D03 and P7-D07 remain open and do
not block B7 contract/synthetic work.
