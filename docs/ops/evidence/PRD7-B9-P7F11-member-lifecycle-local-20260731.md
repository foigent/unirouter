# PRD7 B9 Member Lifecycle — Local Evidence

> Date: 2026-07-31
> Work item: `UR-PRD7-B9-MEMBER-LIFECYCLE`
> Requirement: P7-F11
> Result: Local verified
> External/runtime authority: none

## Outcome

UniRouter now has an IdP-neutral, strict member-lifecycle contract and visible
offline workspace for create, update, suspend, reactivate and terminal
deprovision. It reuses current product authority facts and has no SCIM-vendor
or runtime provisioning path.

One existing IAM gap was closed in repository code: a production-path user
role or status transition now increments `auth_epoch`, revokes all live
administrator sessions in the same transaction, and records the sanitized
result in the existing audit. This prevents a suspended then restored account
from regaining a still-unexpired old JWT.

## Verified Semantics

- portable subject/source references contain only SHA-256 digests;
- revisions are canonical int64 decimal strings with exact parent CAS;
- times use six-digit UTC and reject sub-microsecond input;
- membership and capability arrays are deterministic and role compatible;
- update removals generate exact project/capability revocations;
- suspension retains assignments for possible restore but makes all of them
  ineffective, denies sessions and requires live-session revocation;
- deprovision clears assignments, disables personal keys before user delete
  and is terminal;
- duplicate semantic replay is no-op; changed event identity, stale snapshot
  and post-terminal actions conflict;
- unknown fields, nested duplicate keys, SCIM-vendor fields, missing required
  fields and int64 overflow fail closed;
- every report says `vendor_adapter_authorized=false` and
  `runtime_mutation=none`.

## Immutable Artifacts

| Artifact | SHA-256 |
|---|---|
| `member-lifecycle.v1.schema.json` | `3f871bfca5ee66e34a0d1d804e4181c5228be5f0ea7b831878e0a30add27b964` |
| `project-member-chain.v1.json` | `dfdc04f263438b98f332aded4645393dac611fb6bd51d06634bbb292767919b8` |
| `core.v1.json` golden report | `b8d6d9b1b643ade073061c96effd2a6d84335722219977ffd5743cedf9bb02b2` |

Two runner invocations were byte-identical to the embedded golden: `1`
fixture, `17/17` cases passed.

## Product Surface

- no-store platform/org-admin contract and preview API;
- zero-network CLI runner;
- **成员生命周期** page under **访问与安全**;
- safe lifecycle fixture auto-selected and auto-validated;
- persistent Lifecycle Inspector at desktop and stacked narrow widths;
- no secondary “open details” interaction.

## Verification

Passed before closeout:

```text
go test ./internal/memberlifecycle -count=1
go test -race ./internal/memberlifecycle -count=1
go test ./internal/admin ./cmd/uniroute -run ... -count=1
go run ./cmd/uniroute-member-lifecycle-conformance --report <a>
go run ./cmd/uniroute-member-lifecycle-conformance --report <b>
cmp <a> <b>
cmp <a> internal/memberlifecycle/artifacts/reports/core.v1.json
npm --prefix frontend test -- --run \
  src/pages/__tests__/MemberLifecyclePage.test.tsx \
  src/api/__tests__/client.test.ts src/__tests__/navigation.test.tsx
npm --prefix frontend test -- --run --maxWorkers=1
npm --prefix frontend run type-check
npm --prefix frontend run build
go test ./... -count=1
go vet ./...
git diff --check
```

Focused results:

- member lifecycle race and ordinary tests: pass;
- API/route/IAM session-revocation tests: pass;
- conformance: `17/17`, byte-exact golden;
- targeted frontend: `3` files / `65` tests pass;
- full frontend: `44` files / `320` tests pass;
- frontend type-check and production build: pass.
- full Go tests and `go vet ./...`: pass;
- synthetic browser at `1440x1000` and `390x844`: no horizontal overflow,
  selected-event changes update the always-present inspector without a details
  button, and the browser reported `0` errors / `0` warnings;
- repository diff whitespace check: pass.

## Compatibility And Non-Claims

- Current create/update/member-delete/user-delete API shape remains unchanged.
- Existing role/membership transaction locks, last-platform-admin guard,
  personal-key authorization and Agent authority cache SLO remain unchanged.
- Role/status changes intentionally invalidate old administrator sessions;
  this is an authority reduction, not a new permission.
- Completed-execution Feed bytes/hash, event/Outbox identity, endpoint,
  credentials, page and cursor behavior remain unchanged.
- No SCIM/IdP vendor, attribute/group mapping, credentials, webhook/polling,
  live provisioning or directory source of truth is selected.
- No database migration or live database mutation occurred.
- No deployment, traffic, credential, cursor/checkpoint, push, PR or merge
  occurred.
- This is Local verified, not CI, Target, External integration or Production
  evidence.
