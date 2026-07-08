# FIX-SIGNAL.md - PRD5 X-Insight Scope Discovery

**Date:** 2026-07-08
**Risk level:** R2 acceptance-sensitive documentation change
**Status:** awaiting Canary verification

## Context

PRD4 is complete for the confirmed current scope. This signal must not reopen
PRD4, change PRD4 acceptance counts, or promote any PRD4 row to
`Production verified`.

This round starts PRD5 scope discovery for X-Insight integration. It updates the
PRD5 backlog and creates a shared UniRouter/X-Insight communication workspace:

```text
/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation
```

The shared workspace is outside the UniRouter git repository and is used only
for cross-project communication, draft contracts, decisions, and sanitized
evidence.

## Intended Changes

UniRouter repo:

- `docs/planning/PRD5-BACKLOG.md` adds PRD5 discovery items for:
  - `PRD5-XINSIGHT-001`
  - `PRD5-EVIDENCE-001`
  - `PRD5-PAYLOAD-001`
  - `PRD5-COMPARE-001`
  - `PRD5-TRACEUI-001`
- `docs/planning/PRD5-BACKLOG.md` records the shared-folder path and states that
  UniRouter should emit high-fidelity gateway evidence while X-Insight owns the
  broader enterprise AI/Agent observability and governance platform unless the
  User changes that boundary.

Shared folder:

- `README.md` defines the communication rules, directory contract, decision
  rule, and data boundary.
- `requests/2026-07-08-unirouter-prd5-integration-questions.md` asks
  X-Insight to answer integration questions before PRD5 implementation starts.
- `contracts/README.md`, `responses/README.md`, `decisions/README.md`, and
  `evidence/README.md` define the workspace subdirectories.

## Non-Goals

This round does not:

- modify Go, frontend, migration, OpenAPI, API route, runtime behavior,
  deployment scripts, Docker, or monitoring config;
- implement X-Insight ingestion;
- implement LiteLLM/NewAPI integration;
- add raw prompt/response storage;
- change PRD4 acceptance matrix counts;
- create a `Production verified` claim.

The worktree may already contain PRD4 closeout documentation changes from a
previous state. Canary should validate this signal's PRD5/X-Insight scope
changes and report any conflicting pre-existing changes separately.

## Canary Verification Instructions

1. Re-read the current boundary docs:
   - `docs/PROJECT-HANDOFF.md`
   - `docs/ops/CURRENT-STATUS.md`
   - `docs/ops/evidence/PRD4-final-closeout-20260708.md`
   - `docs/planning/PRD5-BACKLOG.md`
2. Verify PRD4 remains closed for the confirmed current scope and is not
   reopened by this PRD5/X-Insight scope discovery.
3. Verify `docs/planning/PRD5-BACKLOG.md` contains the five new PRD5 discovery
   IDs listed above, and that they are framed as prerequisites/discovery items,
   not accepted implementation.
4. Verify the shared workspace exists with these files:
   - `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/README.md`
   - `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/requests/2026-07-08-unirouter-prd5-integration-questions.md`
   - `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/contracts/README.md`
   - `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/responses/README.md`
   - `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/decisions/README.md`
   - `/Users/m4/Desktop/projects/UniRouter-X-Insight Cooperation/evidence/README.md`
5. Verify the shared README forbids production secrets, raw real prompt/response
   payloads, and authoritative source-code storage.
6. Verify the X-Insight request asks about:
   - ingestion path;
   - trace/span/session/upstream request ownership;
   - schema versioning;
   - `source_component`, `capture_mode`, and `capture_gap`;
   - `evidence_ref`;
   - decision taxonomy;
   - actor/delegation modeling;
   - payload storage boundary;
   - UI boundary;
   - competitor research reuse for LiteLLM/NewAPI/one-api and observability
     references;
   - PRD5 priority recommendation.
7. Run:

```bash
git diff --check -- docs/planning/PRD5-BACKLOG.md docs/agent-workflows/FIX-SIGNAL.md
```

## Expected Result

Canary should write `docs/agent-workflows/VERIFY-RESULT.md` with:

- PASS/FAIL for each numbered verification instruction;
- any PRD4 boundary regression as a blocking failure;
- any shared-folder issue as a documentation/workspace finding;
- a note that no runtime code validation was required for this docs-only R2
  scope-discovery change.
