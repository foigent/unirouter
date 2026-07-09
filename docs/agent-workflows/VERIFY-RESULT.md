# VERIFY-RESULT.md - PRD5 Batch 1T Gate A Exchange Envelope And Controlled Run Authorization

**Verifier:** Canary
**Date:** 2026-07-10
**Signal:** `docs/agent-workflows/FIX-SIGNAL.md`
**Result:** ✅ **PASS — 7/7**

## Verification Summary

| # | Instruction | Result | Evidence |
|---|-------------|--------|----------|
| 1 | `origin/main` contains Batch 1S as `c6ae2a7` / PR #122; previous VERIFY-RESULT records Batch 1S PASS 6/6 | ✅ PASS | `git log --oneline -5 origin/main` → HEAD `c6ae2a7 docs: PRD5 Batch 1S — X-Insight data exchange control surface (P5-F28) (#122)`; VERIFY-RESULT.md title: "PRD5 Batch 1S X-Insight Data Exchange Control Surface", result: "PASS — 6/6" |
| 2 | Snapshot tooling: exchange_id, manifest hash, source_instance binding, trailing JSON rejection, handoff preservation, no artifact hash | ✅ PASS | See Snapshot Tooling Evidence below |
| 3 | Focused tests cover ID format/uniqueness, exact-byte hash, trailing JSON, manifest-mode handoff | ✅ PASS | 7/7 tests PASS (see Command Results) |
| 4 | Runbook and authorization worksheet require prior approval; only two approved scenarios; unique X-Request-ID; no broad discovery | ✅ PASS | `PRD5-GATE-A-CONTROLLED-RUN-AUTHORIZATION.md`: Required Approval Fields table (scenario set, data classes, source instance, safety, retention, operator all "pending"); Operator Preconditions section; Narrow Run Procedure uses only `successful_non_stream` + `model_permission_denied`; unique `X-Request-ID` per request; "no broad request-ID discovery query is allowed"; Authorization request file: "No API key, DSN, password, prompt, response, IP, or upstream error text should be written in this file" |
| 5 | Exchange contract and UniRouter response match X-Insight receipt contract: `validated`/`persisted`/`rejected`, no `import_job_id`, no deep link, reconciliation by `(exchange_id, artifact.sha256)` | ✅ PASS | Exchange contract: reconciliation key `(exchange_id, artifact.sha256)` (line 36); "X-Insight computes the snapshot artifact hash after receipt" (line 21-22); no `import_job_id` or deep link in contract. UniRouter response: "X-Insight may then return the implemented `xinsight.import-receipt.v1` sidecar" (line 45); no `import_job_id` or deep link. X-Insight data exchange control surface response (section 4): receipt returns `validated`/`persisted`/`rejected`; "The current CLI does not return `import_job_id` or a deep link because it does not yet persist a durable import job" |
| 6 | PRD5 status/matrix/plan/backlog/handoff/tech-ref/index docs route P5-F29 / Batch 1T; mark Batch 1S complete as `c6ae2a7` / PR #122 | ✅ PASS | All 12 docs contain P5-F29 / Batch 1T references (see grep results); CURRENT-STATUS.md line 5 lists `c6ae2a7` / PR #122 for Batch 1S; ACCEPTANCE-MATRIX.md line 75 has P5-F29 row with "Fixed awaiting Canary"; PRD-Phase5.md line 114 P5-F29 row, line 810 Batch 1T entry |
| 7 | No non-goal or PRD4 boundary regression | ✅ PASS | See Non-Goal and PRD4 Boundary Evidence below |

## Snapshot Tooling Evidence

| Check | Code Location | Evidence |
|-------|---------------|----------|
| `exchange_id` format: `uxchg_<32-lowercase-hex>` | `newExchangeID()` lines 460-465 | 16 bytes crypto/rand → `"uxchg_" + hex.EncodeToString(value[:])` → 32 lowercase hex chars |
| `exchange_id` uniqueness | `TestNewExchangeIDFormatAndUniqueness` | Generates 128 IDs, regex `^uxchg_[0-9a-f]{32}$`, zero duplicates |
| Manifest hash: exact bytes + `sha256:` prefix | `sha256Digest()` + `loadApprovalManifest()` | `sha256.Sum256(payload)` → `"sha256:" + hex.EncodeToString(sum[:])`; test `TestLoadApprovalManifestHashesExactBytes` verifies exact expected digest |
| Manifest hash only in manifest mode | `TestSnapshotEnvelopeJSONOmitsManifestAndArtifactHashesInDirectMode` | Direct-mode JSON omits `approval_manifest_sha256`; `snapshotFile` struct has `omitempty` on that field |
| Manifest `source_instance` required | `TestRunSnapshotRequiresManifestSourceInstanceBeforeDSN` | Empty `source_instance` → error "approval manifest source_instance is required" before DSN access |
| Conflicting CLI override rejected | `TestRunSnapshotRejectsManifestSourceInstanceOverrideBeforeDSN` | Mismatched `--source-instance` → error "must match approval manifest source_instance" |
| Trailing JSON rejected | `TestLoadApprovalManifestRejectsTrailingJSONObject` | Second JSON value → error "exactly one JSON object" |
| `--approval-manifest` preserved in handoff | `TestFormatHandoffNotePreservesApprovalManifestMode` | Handoff note contains `--approval-manifest 'approval.json'` and no `--request-id`/`--scenario` flags |
| No artifact hash in snapshot | `snapshotFile` struct | No `artifact_sha256` field; `snapshotFile` contains `ExchangeID` and `ApprovalManifestSHA256` but no self-referential artifact hash |

## Non-Goal and PRD4 Boundary Evidence

| Check | Evidence |
|-------|----------|
| No frontend code changes | `git diff -- frontend` → empty (exit 0) |
| No internal/cmd/migrations changes | `git diff -- internal cmd migrations` → empty (exit 0) |
| No PRD4 doc changes | `git diff -- docs/PRD-Phase4.md docs/ops/PRD4-ACCEPTANCE-MATRIX.md docs/ops/evidence/PRD4-final-closeout-20260708.md` → empty (exit 0) |
| No whitespace errors | `git diff --check` → clean (exit 0) |
| Working tree: docs + scripts only | `git status --short` → 14 modified `.md` files + 2 new files (`PRD5-GATE-A-CONTROLLED-RUN-AUTHORIZATION.md`, `prd5_xinsight_snapshot_test.go`); all changes are docs or snapshot tooling/tests |
| No real request/snapshot/ID/secret/UI/API/migration/runtime | All changes confined to docs and `scripts/prd5_xinsight_snapshot{,_test}.go`; snapshot tool requires DSN env var (not set in repo); no request IDs invented; no credentials written |

## Command Results

| Command | Result |
|---------|--------|
| `git log --oneline -5 origin/main` | ✅ `c6ae2a7` at HEAD for PRD5 Batch 1S / #122 |
| `go test scripts/prd5_xinsight_snapshot.go scripts/prd5_xinsight_snapshot_test.go -count=1 -v` | ✅ **7/7 PASS** (0.693s): TestLoadApprovalManifestHashesExactBytes, TestLoadApprovalManifestRejectsTrailingJSONObject, TestRunSnapshotRejectsManifestSourceInstanceOverrideBeforeDSN, TestRunSnapshotRequiresManifestSourceInstanceBeforeDSN, TestNewExchangeIDFormatAndUniqueness, TestSnapshotEnvelopeJSONOmitsManifestAndArtifactHashesInDirectMode, TestFormatHandoffNotePreservesApprovalManifestMode |
| `go run scripts/prd5_xinsight_snapshot.go -help` | ✅ Shows all flags including `--approval-manifest`, `--source-instance`, `--handoff-note` |
| `go test ./... -count=1` | ✅ All packages PASS, 0 FAIL (18 packages, scripts excluded by `//go:build ignore`) |
| `git diff -- frontend internal cmd migrations` | ✅ Empty (no code changes) |
| `git diff -- docs/PRD-Phase4.md ...` | ✅ Empty (no PRD4 changes) |
| `git status --short` | ✅ 14 modified docs + 2 new files (docs + scripts only) |
| `git diff --check` | ✅ Clean |

## New Discoveries

No new bugs or scope violations found.

## Final Result

**PRD5 Batch 1T Gate A Exchange Envelope And Controlled Run Authorization: ✅ PASS — 7/7**

All seven Canary verification instructions pass. The batch adds exchange identity (`exchange_id`), exact manifest hashing (`approval_manifest_sha256`), manifest-preserving handoff commands, focused Go tests (7/7), a controlled-run authorization runbook, and shared exchange contract/authorization/response files. No non-goal, PRD4 boundary regression, or scope violation is present.
