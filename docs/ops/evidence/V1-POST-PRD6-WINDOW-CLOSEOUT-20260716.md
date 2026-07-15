# V1 Post-PRD6 First-Release Window Closeout

**Date:** 2026-07-16 (Asia/Shanghai)

**Decision authority:** User-directed closeout after the first-release target
verification; X-Insight cooperation remains outside this closed UniRouter-owned
work window.

**Status:** Closed for the selected Post-PRD6 V1 first-release scope. This
closes the work window, not unexecuted evidence or future product candidates.

## Repository and runtime boundary

- Documentation decision baseline: clean/aligned `main` and `origin/main` at
  `90ffc010c4220d4cae738f04cad4021d2eb20f0e`.
- Latest code-bearing repository and development-target release remains
  `46b49b60835225a0b32b641615e6afe4a8a9696b` from PR #214.
- This closeout performs no code, schema, ECS, License, signing-key, credential,
  provider-price or routing mutation and sends no paid provider request.
- PRD6 remains frozen as `Completed (Accepted Scope)` with 27/27 accepted rows;
  this Post-PRD6 decision does not reopen or rewrite it.

## What the window completed

The selected V1 scope has repository/CI or development-target evidence for:

1. CI change routing, PR concurrency, exact Go version/cache control and removal
   of duplicate automatic post-merge full CI;
2. atomic wallet adjustment, public million-token pricing authority, fail-closed
   unpriced models and micro-CNY customer-ledger precision;
3. the development environment label;
4. fail-closed commercial License enforcement, embedded non-replaceable trust
   roots, signed offline upgrade verification/sequence control, License Studio
   and exact-digest source-free customer Compose;
5. first-release UI/API correctness fixes from the product audit;
6. bounded real Mimo and DeepSeek Flash routing, provider-returned usage,
   upstream-status persistence, exact internal settlement and the dated selected
   DeepSeek Flash pilot price;
7. request-time administrator state enforcement and an exact two-model pilot
   catalog/configuration, including excluded-model zero-dispatch evidence.

The detailed evidence chain remains in the V1 documents indexed by
`docs/DOC-INVENTORY.md`; the latest target checkpoint is
`V1-ADMIN-JWT-STATUS-AND-PILOT-CATALOG-TARGET-20260716.md`.

## Deferred or not implemented

| Issue | Closeout classification | Why it does not remain active in this window |
|---|---|---|
| `V1-TARGET-PILOT-001` | Deferred — future customer/Production acceptance | The exact two-model development-target pilot is verified. Capacity, detector, enterprise IdP, customer operations, TLS/HA and Production require a separately approved environment and evidence window; they are not current regressions. |
| `V1-PROVIDER-BILL-RECON-001` | Deferred — actual supplier-bill reconciliation not executed; recurring/productized capability not implemented | No actual provider billing console, finalized export/API, invoice or isolated balance delta has been inspected. Current evidence proves provider-returned usage/status and UniRouter customer-ledger settlement only. Recurring/productized reconciliation remains candidate `D7-COST-001/002`. |
| `V1-CHANNEL-MAPPING-001` | Deferred — post-pilot feedback | The audited controlled mapping procedure and routing preview work on the development target. A validated RBAC/audit-aware editor is a future UX decision, not a first-release regression. |

Deferring supplier-bill reconciliation does not block the current functional
pilot. It continues to block any claim that UniRouter charges equal actual
supplier cost, that pass-through is proven, that gross margin is accurate, or
that Production financial accuracy has been accepted. Future work must preserve
separate Customer Revenue and Provider COGS ledgers and requires explicit
FinOps/provider-account ownership and authorization.

A one-time bounded manual/account reconciliation may be authorized separately
as external evidence without first implementing PRD7. A recurring control plane
or connector product requires separate acceptance of `D7-COST-001/002`.
Neither path is authorized by this closeout.

## Sole active Post-PRD6 follow-up

`V1-XI-LEDGER-001` remains `Coordination pending`: UniRouter's v2
`cost_micros` producer contract is merged and deployed, but X-Insight consumer
acknowledgement and a joint readback have not occurred. UniRouter-only tests do
not prove external integration acceptance.

There is no other active UniRouter-owned implementation or target mutation in
this Post-PRD6 window. Any deferred item requires an explicit new decision and
work window; it must not be silently reopened or represented as verified.
