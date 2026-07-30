# PRD7 B7 Provider COGS And Financial Truth — Local Evidence

> Date: 2026-07-31
> Work item: `UR-PRD7-B7-PROVIDER-COGS-FINANCIAL-TRUTH`
> Result: Local verified
> External/runtime authority: none

## Outcome

UniRouter now has a strict, zero-network Provider COGS offline contract and a
Financial Truth workspace. Supplier costs, UniRouter provider estimates and
customer revenue are represented by separate authority objects. No imported
supplier line can create customer debt. Aggregate supplier costs remain
unattributed even when their project or period overlaps source attempts.

## Implementation

- `internal/providercogs` implements strict import validation, exact decimal
  arithmetic, canonical semantic identity, page/cursor/finality rules,
  immutable line duplicate/conflict handling, coverage/gap semantics and
  explainable matching.
- Replay accepts the first semantic identity, treats identical or
  whitespace-only re-encodings as duplicate/no-op and rejects changed content
  under the same `import_id`.
- Closed coverage distinguishes `source_only`; provisional coverage requires a
  declared delay and preserves `supplier_evidence_pending`.
- Four fixed declared gaps cover provider delay, missing request attribution,
  unallocated discounts/credits and unallocated tax/FX.
- Billing-authorized no-store contract and preview routes expose only in-memory
  evaluation. They do not have a database, provider-network, credential or
  customer-ledger write path.
- The former “计量校验” page is now “财务真值”. Existing customer usage
  reconciliation remains intact; the added supplier evidence area accepts
  pasted/local JSON, shows exact-scale values and keeps a persistent,
  auto-selected right-side inspector.
- Two zero-network CLIs provide deterministic conformance and file-in/report-out
  preview.

## Immutable artifacts

| Artifact | SHA-256 |
|----------|---------|
| `provider-cogs-offline.v1.schema.json` | `43a903f2eb31a6c7a0ffc46631d33cf40b28bc23dff4b31982480a28a713fb74` |
| `provisional-aggregate-gaps.v1.json` | `45acc838c7873d8a3d5a60f2460e48d203d5f2fd70f83401d9af5df2dab869f6` |
| `request-and-pagination.v1.json` | `ba1002ede765ef4ad272592baa9a1a27c4528bc637a07f3413824a6681f5b18d` |
| `core.v1.json` golden report | `87ddc2600b028ca3ae41623b4479361dacdd3d9b5323a184141a77b9717490b4` |

The golden report is byte-identical across two independent invocations and
records `2` fixtures, `15` cases, `15` passed and `0` failed.

## Verification

Passed:

```text
go test -race ./internal/providercogs
go test ./internal/admin -run TestProviderCOGSOffline -count=1
go test ./cmd/uniroute -run 'Test(ProviderCOGSOfflineRoutesUseFinancialAuthority|OpenAPISpecCoversRegisteredRoutes|FrontendDocsPageCoversRegisteredRoutes|BackendDocsPageUsesCurrentContractLanguage)' -count=1
go vet ./...
go test ./... -count=1
npm run test -- --run src/pages/__tests__/ReconciliationPage.test.tsx src/api/__tests__/client.test.ts
npm run test -- --maxWorkers=1 --fileParallelism=false
npm run type-check
npm run build
```

Results:

- full Go package suite: pass;
- full Go vet: pass;
- frontend: `42` files / `311` tests pass;
- frontend type-check and production build: pass;
- targeted API/UI tests: `49` tests pass;
- conformance output matches the embedded golden byte-for-byte;
- offline preview returns `customer_ledger_mutation=none` and the exact
  synthetic total `420 / 1000000 USD`;
- `git diff --check`: pass at closeout.

The first parallel frontend run exposed four existing 10-second timing
timeouts and one expected label that changed from “计量校验” to “财务真值”.
The label assertion was updated. The four timeout files passed when rerun
serially, and the final complete single-worker suite passed 311/311. No timeout
was hidden or reclassified as a product pass.

One attempted temporary verification command was rejected before execution
because it scripted deletion of three temporary files, contrary to the
repository deletion rule. Verification was rerun without deletion; no
repository or runtime state was affected.

## Compatibility and non-claims

- Existing customer request records, prices, wallet and provider-attempt
  recovery semantics are unchanged.
- Existing completed-execution Feed endpoint, DTO, source bytes/hash,
  credentials, pages and cursors are unchanged.
- No Provider Pack V1 or shared Foigent event/connector artifact changed.
- No supplier account, credential, API, export, invoice or negotiated price was
  accessed.
- No database migration or non-test write, provider traffic, deployment,
  cursor/checkpoint operation, push, PR or merge occurred.
- This is Local verified, not CI, Target, External integration or Production
  evidence.
- P7-D05 still gates real account scope, credentials and retention. B8 can
  proceed entirely through deterministic snapshots without that decision.
