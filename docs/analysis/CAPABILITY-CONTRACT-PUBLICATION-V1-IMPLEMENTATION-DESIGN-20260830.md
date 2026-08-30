# Capability Contract Publication V1 — Exact Implementation Design

> Status: exact design approved for implementation and Local/CI verification;
> implementation is work in progress. This document still authorizes no Target
> publication apply, deployed migration, K2.7/K3 work, paid call, credential,
> billing, Release, Alias, canonical or Production change.
>
> Source inventory: `main@36b01fef0468e3ffd90b4798e284f98b0a280acc`
> (`tree d14c14c2a9153172e6c88affade1b66b16bbe0a5`).
>
> Development/test read-only inventory: 2026-08-30 CST, Target
> `1.2.20@cf2bee4bbee7074e91661400f5edf61ca2c08f32`, PostgreSQL 16.14,
> migration ceiling `000100_kimi_k26_function_tools_tool_index_normalizer`.
>
> This document follows the completed
> [publication-cost audit](CAPABILITY-CONTRACT-PUBLICATION-COST-AUDIT-20260830.md).
> It does not publish K2.7, create a migration, change a current selector, run a
> Probe, spend provider budget or authorize the first real publication apply.

## 1. Decision summary

Publication V1 should **not** introduce a second database writer beside the
existing migration owner. The minimum compatible implementation is:

1. one typed, immutable artifact declaration source for binding history and
   explicit selector transitions;
2. deterministic generation of the existing protected migration projection;
3. a read-only Target plan command;
4. application by the existing single migration owner and advisory-lock domain;
5. transaction-internal final verification plus post-commit verification while
   the migration owner's session lock is still held.

The database remains the runtime query authority for Planner, qualification,
Admin and pre-dispatch revalidation. The declaration becomes the source for
static publication semantics and generated migration artifacts; it is not read
as an alternative live routing authority.

This choice preserves the exact schema-ledger compatibility gate. A standalone
publisher outside the migration ledger would allow an older binary to see a
database state that its artifact ledger still appears to accept. Avoiding that
requires a new compatibility ledger or schema contract, so it is not the
minimum V1 path.

Publication V1 reduces hand-written static declaration, SQL tuple, fixed-count
and predecessor-validation repetition. It does **not** remove the need for an
artifact, CI, a maintenance window or deployment when accepted runtime contract
semantics change, and it does not by itself prevent repeated releases caused by
late provider-wire discovery. Earlier bounded conformance characterization and
Delivery Batch freezing remain the controls for reducing those release rounds.

## 2. Verified current-state inventory

### 2.1 Supported writers

| Path | Current behavior | V1 disposition |
|---|---|---|
| `internal/migrate.Run` from backend startup | Acquires session advisory lock `86420517831702491`, applies every pending `.up.sql` in one transaction per migration and atomically inserts its `schema_migrations` row | Retain; a normal runtime process must not become an implicit static-contract publisher |
| `./uniroute migrate up` | Uses the same runner and lock; development/test already invokes it from one no-port maintenance container after stopping stable and canary | Retain as the only apply owner |
| M88–M100 up/down SQL | The only production DML found for `model_capability_probe_contract_bindings` and `model_capability_current_probe_contracts`; temporarily drops and restores immutable triggers inside the migration transaction | Replace hand-written tuple repetition with generated migration content, not with a new writer |
| Direct manual SQL | A migration file can be run manually, but the runbook states that this does not write the ledger and must not be mixed with the binary migration flow | Recovery-only, unsupported for normal publication |
| Runtime Go, bootstrap and reset | Runtime code only reads the static tables; first-customer/reset logic protects them rather than mutating them | No change |
| DBA/superuser | Can always bypass application policy by altering triggers or tables | Trusted operations boundary; controlled by runbook, evidence and drift detection |

No bootstrap script, runtime handler, model onboarding handler, Planner,
revalidation path or supported operations script was found to write either
static contract table outside migrations. Tests contain fixture/negative DML
and are not production writers.

### 2.2 Target roles and ACL

The development/test database has one login role, `uniroute`. It is a
superuser, owns the inspected tables and has insert/update/delete/truncate on
the binding, selector, schema-ledger and audit tables. Stable, canary, the
migration CLI and PostgreSQL all currently use this shared deployment identity.
There is no table ACL split and no separate publication role.

Both running backend containers have `SKIP_MIGRATION=true`; the runbook stops
them before launching one `SKIP_MIGRATION=false` CLI owner. V1 therefore uses
that maintenance-window exclusion. Creating a new database role, changing
ownership or adding ACLs is not part of V1. A later least-privilege change must
separately validate deployment, backup/restore and emergency recovery.

### 2.3 Static projection model

The Target contains 76 immutable binding-history rows and 51 current selector
rows. The K2.6 subset contains 19 historical bindings and six current
selectors. Binding identity is the complete tuple:

```text
operation
+ capability
+ execution Profile ID/version
+ protocol Profile ID/version
+ Probe contract ID/version
```

The selector primary key excludes Probe contract ID/version, so absence is row
absence, not a nullable predecessor. A first selector is an insert. A promotion
is an exact compare-and-swap from one complete Probe contract identity to
another. A selector must never be derived from the highest version.

Both static tables have statement-level triggers that reject insert, update,
delete and truncate. Current migrations open this boundary only after taking
the shared migration advisory lock and `SHARE ROW EXCLUSIVE` table locks, then
restore the triggers before commit.

### 2.4 Audit and old-binary boundary

`schema_migrations` stores only version and application time. `audit_logs` can
hold structured before/after/detail data, actor and request metadata, but it is
not append-only, has no publication-specific uniqueness and requires an
organization/user semantic that a schema owner must not impersonate. It is not
a sufficient immutable publication receipt and must not be overloaded in V1.

V1 instead uses the existing atomic schema ledger plus exact source/artifact,
canonical plan and serialized readback evidence. This provides an auditable
internal publication chain but does **not** claim a new customer-visible or
database-resident immutable publication receipt. If that stronger product
requirement is later approved, it requires a separately reviewed schema and
receipt contract.

Every backend, including `SKIP_MIGRATION=true`, requires its complete artifact
ledger to equal the database ledger before serving HTTP. Consequently an older
binary cannot start against a database containing a later publication
migration. Rollback after a committed publication is roll-forward or an
explicit guarded down/backup restore; it is not a mixed-version rolling deploy.

### 2.5 Evidence anchors

- Migration ownership and session-lock lifetime:
  [`internal/migrate/migrate.go`](../../internal/migrate/migrate.go).
- Startup exact-ledger gate:
  [`cmd/uniroute/main.go`](../../cmd/uniroute/main.go) and
  [`internal/schemaauthority/ledger.go`](../../internal/schemaauthority/ledger.go).
- Static tables, immutable triggers and qualification joins:
  [`migrations/000088_model_capability_execution_qualification.up.sql`](../../migrations/000088_model_capability_execution_qualification.up.sql).
- Initial K2.6 publication and latest exact selector promotion:
  [`migrations/000095_kimi_k26_chat_core_qualification.up.sql`](../../migrations/000095_kimi_k26_chat_core_qualification.up.sql) and
  [`migrations/000100_kimi_k26_function_tools_tool_index_normalizer.up.sql`](../../migrations/000100_kimi_k26_function_tools_tool_index_normalizer.up.sql).
- Guarded K2.6 rollback:
  [`migrations/000100_kimi_k26_function_tools_tool_index_normalizer.down.sql`](../../migrations/000100_kimi_k26_function_tools_tool_index_normalizer.down.sql).
- Reset protection rather than mutation:
  [`internal/resetplan/plan.go`](../../internal/resetplan/plan.go).
- Development/test single-owner sequence:
  [`docs/ops/deployment-runbook.md`](../ops/deployment-runbook.md), section
  9.10.1.
- Exact current Target snapshot:
  [`docs/ops/CURRENT-STATUS.md`](../ops/CURRENT-STATUS.md).

## 3. Artifact declaration V1

### 3.1 Package and files

Create a low-level package with no dependency on Admin, Planner, gateway or
database runtime state:

```text
internal/modelcapabilitypublication/
  contract.go
  canonical.go
  registry.go
  export.go
  generator.go
  plan.go
  apply.go
  executor.go
  runtime.go
  recovery.go
  install.go
  declarations/
    baseline-m100.json
    <future-publication-id>.json
```

`baseline-m100.json` is an immutable import boundary containing the exact 76
binding rows and 51 selectors at M100. It is generated deterministically by the
exact source artifact after applying M1–M100 to a fresh isolated PostgreSQL
database. The exporter and byte-for-byte equivalence check are repository gates;
the 76/51 rows are never maintained by hand. A development/test Target is only
a read-only cross-check and is not a baseline content source. The baseline does
not replace or reinterpret M88–M100. Future files are append-only publication
units. A later declaration file may append binding identities and declare
selector transitions; it may not edit the baseline or a prior publication
unit.

`internal/modelcapabilityqualification` consumes only the folded exact current
bindings from this package. It no longer maintains a parallel hand-written
current-binding list. Runtime still compares those compiled bindings with the
database current projection before onboarding or qualification.

First-customer bootstrap and its verification tests consume the same folded
expected projection. The current hand-maintained `76/51` counts and exact
K2.6 tuple assertions must not become a third declaration source. Bootstrap
may still materialize or validate its existing database state, but all expected
static-contract identities, counts and current selectors are derived from this
package.

### 3.2 Declaration schema

Each future publication unit has:

```text
schema_version
publication_id
expected_static_predecessor
expected_schema_predecessor
binding_appends[]
selector_transitions[]
declared_projection_effects[]
```

Each binding append contains every current database column except `created_at`.
`introduced_migration` equals `publication_id`. Each selector transition
contains its complete selector key, `expected_predecessor` and desired exact
Probe contract identity. `expected_predecessor` is exactly one of:

```text
absent
exact(probe_contract_id, probe_contract_version)
```

Projection effects are explicit exact Profile/protocol/provider/model/
capability filters. They may insert an exactly absent declared capability row
or update only an exact `evidence_state='declared'` row from the declared
expected state. Live Probe, Approval and frozen execution evidence are never
rewritten. The environment-specific plan expands every effect into exact Target
rows; filters are not apply authority.

The declaration loader rejects padding, empty fields, duplicate binding or
selector identities, missing binding targets, inferred predecessors, unordered
or duplicate capability lists, unknown schema versions and any attempt to
modify a prior publication unit. CI permits a normal publication diff to add one
new declaration unit but not edit the M100 baseline or an earlier unit. Each
unit has its own canonical digest, while publication order remains the exact
schema predecessor and migration ledger rather than a second global declaration
digest chain.

The JSON input boundary is strict before typed decoding: duplicate members,
including members whose escaped names decode to the same Unicode string,
unknown members, trailing JSON values and loose numeric representations are
rejected. The implementation uses the repository strict-JSON validator, then a
single typed decoder with unknown-field rejection and an explicit EOF check.
Publication identity, versions and counts are represented by contract-defined
typed fields; they are not accepted through `float64` or implementation-defined
map coercion.

### 3.3 Canonicalization and digests

Canonicalization version is
`unirouter.capability-contract-publication-canonical.v1`.

- Encoding is UTF-8 JSON from fixed structs only; maps are forbidden.
- Field order is contract-defined.
- Binding appends are sorted by the complete binding primary key.
- Selector transitions are sorted by the complete selector primary key.
- Projection effects are sorted by their complete exact filter identity.
- Duplicates are rejected before hashing.
- No timestamps, hostnames, database addresses, execution order, health state,
  credentials or environment labels enter the declaration digest.
- SHA-256 lower-case hex is computed over the canonical bytes with no trailing
  newline.

The environment-specific plan separately binds exact commit, tree, artifact
revision/version, publisher executable identity, declaration digest, complete
expected schema ledger, the ordered SHA-256 digest of every artifact `.up.sql`
file, the current publication's generated down/verify digests, database
pre-state and proposed post-state. Migration IDs are limited to the 64-byte
`schema_migrations.version` contract. The Plan uses fixed-struct canonical JSON
and its own SHA-256 plan digest. Time and connection metadata remain outside
that digest.

There is no global artifact/database digest equality. Every database projection
used by the current environment must be supported by the artifact. The artifact
may contain historical or inactive declarations that are not current in that
database.

## 4. Deterministic generated projection

The declaration compiler generates reviewable repository artifacts:

```text
migrations/<publication-id>.up.sql
migrations/<publication-id>.down.sql
migrations/<publication-id>.verify.sql
```

Generation is byte-deterministic. A check command fails when generated files
differ from the declaration. Reviewers continue to see ordinary SQL, while
identities, counts, predecessor checks, trigger open/reseal and projection
updates are no longer copied by hand.

M100 import equivalence compares every canonical binding and selector field,
excluding only the database-assigned `binding.created_at` and
`selector.updated_at` timestamps. No other field, trigger state, count or
identity difference is normalized away.

The generated up migration and its exact Plan executor must together:

1. take `pg_advisory_xact_lock(86420517831702491)`;
2. lock the same static/projection/schema tables in the repository-defined
   order and mode;
3. validate the exact predecessor schema ledger and complete expected static
   pre-state;
4. drop only the three existing publication/projection guards it must cross;
5. for the pending publication unit, insert every declared binding into an
   exactly absent identity; an already-present current-unit binding without the
   corresponding ledger entry is `unledgered_publication_state`, not a no-op;
   `binding_action=no_op` is reserved for baseline/predecessor verification;
6. for `selector_action=current_promotion`, conditionally insert from `absent`
   or update from the complete exact predecessor; affected rows must be exactly
   one; an already-promoted current-unit selector without the corresponding
   ledger entry is `unledgered_publication_state`; `selector_action=no_op` is
   reserved for baseline/predecessor verification;
7. use declaration effects only during read-only planning to expand exact
   Target rows; generated SQL never writes by re-running those filters, and the
   Plan executor consumes only the approved exact rows. Insert requires an
   absent row; update requires complete expected `support_state`,
   `evidence_state`, `evidence_ref` and `revision`; and each
   operation produces the complete declared final row. The eligible mutation
   set contains only absent rows and exact `enabled/declared` predecessor rows;
   `live_probed`, `production_approved` and other lifecycle-owned rows remain
   untouched and outside the publication's instantaneous dynamic readback. An already-final row
   without the publication ledger is `unledgered_publication_state`, never a
   no-op, and generated SQL must not use `ON CONFLICT DO NOTHING` to hide it;
8. have the Plan executor verify each projection action's expected affected
   rows, bind every update predecessor and deterministic final transition to
   one exact declared effect, and verify the complete final static and
   explicitly mutated deployment projection in the same transaction;
9. restore every static or projection trigger opened by its respective writer
   before returning to the migration runner.

The migration runner rejects publication up or guarded-down execution unless
the caller supplies every Plan-validation, projection-mutation, in-transaction
verification and post-commit verification callback. A publication with no
dynamic projection rows uses explicit no-op callbacks; omission never means
permission to skip a control.

The runner inserts the migration ledger row in the same transaction and then
commits. Any conflict, unexpected row count, undeclared promotion or trigger
state mismatch aborts the whole transaction.

The generated down migration remains guarded. It refuses when a later ledger
entry, retained Probe/Approval, non-declared projection, frozen execution
evidence or unexpected selector exists. It restores only the exact declared
predecessor and deletes only rows introduced by its own publication. The
compiler does not make downgrade universally safe. Because the M85 Freeze
snapshot hash cannot be reversed to one capability tuple, V1 conservatively
refuses guarded down while any `freeze_idempotency` or provider-attempt
`execution_snapshot_hash` remains non-null.

The compiled declaration registry also identifies which migration IDs are
static publications. Existing M88–M100 ledgers remain accepted history; the
Strict V1 gate applies only to future declaration-owned publication migrations.
Ordinary backend startup and plain `migrate up` cannot cross one without its
exact publication Plan context.

## 5. Dry-run contract

Add one read-only command:

```text
./uniroute capability-contract-publication plan \
  --publication <publication-id> \
  --output <local-plan-path>
```

It runs `REPEATABLE READ READ ONLY`, takes no `FOR UPDATE`/`FOR SHARE` lock and
creates no Target receipt, audit row, temp table or other persistent state.
Normal snapshot/access locks are allowed. The output file is local evidence,
not a reservation.

The plan binds a server-owned Target identity, not a client label. V1 derives
it from the configured environment identity plus a stable database-cluster and
database fingerprint that contains no DSN, credential or secret. Apply must
re-resolve the same identity from its own trusted configuration and database
session; a client-supplied environment string is never authoritative.

Each item is represented orthogonally:

```text
binding_action: append | no_op
selector_action: current_promotion | no_op
projection_action: insert | update | no_op
validation: valid | conflict
```

Projection plan items contain the exact Target deployment, operation and
capability identity, complete expected row or explicit `absent`, complete final
row and exact expected affected-row count. The declaration's filters are only
used to discover this normalized set during plan generation. Apply consumes the
exact rows in the Plan and must regenerate the identical set after locking; it
never reruns a filter and silently accepts a different Target result.

Stable conflicts include at least:

```text
binding_content_mismatch
selector_predecessor_mismatch
undeclared_selector_promotion
schema_ledger_mismatch
artifact_declaration_mismatch
projection_pre_state_mismatch
unledgered_publication_state
```

The plan is apply-eligible only when every item has `validation=valid`. It
contains the canonical expected predecessor, never a version inferred from the
database maximum. It also lists the complete pending migration set; an
unapproved extra migration is a conflict. Dry-run returns nonzero on a conflict
and never shrinks the approved set automatically.

For a normally pending publication, every item owned by the current unit must
be `append`, `current_promotion`, `insert` or `update`; `no_op` items only
validate folded baseline/predecessor state. A ledger-absent database that
already contains any current-unit final binding, selector or deployment
projection row is invalid.

`pending_apply` and `post_commit_recovery_check` are execution modes outside the
immutable Plan; they never change its actions or digest:

- `pending_apply` requires the publication ledger to be absent and executes the
  exact original actions after lock-protected revalidation.
- `post_commit_recovery_check` is a read-only recovery operation after command
  output or external evidence is lost. It does not replay publication or create
  a replacement Plan. With the original Plan and no later declaration
  publication it may return `static_projection_verified_for_plan`; with later
  compatible publications it verifies the current artifact/ledger folded
  static projection and may return `current_static_projection_verified`.
  Missing/unexpected ledger, unavailable Plan, or static mismatch are reported
  explicitly. `dynamic_readback` is always `unreconstructable` because V1 has no
  persistent runtime-start marker. The check never returns
  `already_applied_verified`, writes a second receipt, repairs state, or updates
  the database.

## 6. Apply and serialization contract

Publication apply remains the existing migration-owner operation; it is exposed
only as an explicit CLI wrapper over the same migration runner and physical
connection:

```text
./uniroute capability-contract-publication apply --plan <local-plan-path>
```

It is not an HTTP endpoint.

1. Verify exact artifact commit/tree/image/version and generated-file checks.
2. Verify the exact validated plan's publication ID, declaration digest,
   server-owned Target identity, artifact identity, expected ledger and exact
   tuple set. Re-hash the complete migration artifact under the publication
   session lock; each migration is rechecked on the exact bytes read for
   execution, so a bind-mounted file cannot change between preflight and use.
3. Enter the existing non-rolling maintenance window and stop every other
   static-contract writer, including stable and canary.
4. Start one no-port CLI owner with `SKIP_MIGRATION=false` and run the supported
   publication apply command.
5. The runner acquires the existing **session-level** migration advisory lock,
   rereads the complete ledger/static/projection pre-state and recomputes the
   canonical plan. Its digest, expected pre-state, Target identity, artifact
   identity and tuple set must equal the supplied exact validated plan before
   any write transaction begins.
6. The generated migration then acquires its transaction lock/table locks,
   rechecks the exact predecessor protected by those locks and applies
   atomically.
7. Before commit, validate affected rows, the complete final static projection
   and every explicitly mutated deployment-projection row, then atomically
   record the schema ledger and commit.
8. While the runner still holds the session advisory lock, execute the generated
   read-only `.verify.sql` on the same physical connection for the complete
   static projection, then run the exact Plan-owned verifier for every
   deployment-projection row explicitly mutated by this Plan. The immediate
   combined verifier does not demand a historical selector state that a later
   publication legitimately advanced. A failure marks publication verification
   failed and blocks backend startup/K2.7 activity; it never triggers an
   automatic second apply.
9. Release the session lock only after verification completes. If unlock is
   uncertain, retain the current behavior of closing the physical connection.
10. A later independent Target readback may create deployment evidence, but it
   is not the correctness window and does not claim the transaction lock remains
   held.

The runner must not use a session-level lock through a pool connection that can
change underneath it. V1 reuses the existing acquired physical connection and
its verified unlock/close behavior; it does not add a distributed lock.

Apply correctness comes from the existing stable migration namespace lock,
exact pre-state validation, transaction atomicity, unique constraints and
in-transaction final verification. The earlier dry-run is evidence only.

The CLI does not prove human approval: there is no signature or immutable
approval receipt in V1. Authorization is an external runbook/deployment
decision over the exact plan digest, artifact and Target identity. The command
proves only that the supplied plan is the exact plan revalidated under the
publication lock.

If commit succeeded but command output or post-commit readback was lost, the
owner may run `post_commit_recovery_check`. The result has four independent
dimensions:

```text
ledger_state: exact_publication_present | missing | unexpected
ledger_relation: exact_plan_post_state | advanced_compatible | unexpected
static_verification:
  static_projection_verified_for_plan
  | current_static_projection_verified
  | mismatch
  | plan_unavailable
dynamic_readback: unreconstructable
```

The operation reads the original Plan when available and never regenerates an
all-no-op replacement. It verifies static state only. Legitimate later Probe,
Approval or deployment-projection lifecycle progress is neither publication
corruption nor historical readback evidence. This recovery check cannot replace
normal apply's transaction-internal final verification and immediate
post-commit readback on the same locked physical connection.

### 6.1 Strict installation, upgrade and recovery orchestration

Strict V1 provides explicit non-startup entry points for every supported
deployment shape:

- Existing environment upgrade: one exact Plan followed by explicit apply.
- Fresh installation: the install owner brings an empty database through the
  historical M1–M100 baseline, then applies each later declaration-owned
  publication through its own exact Plan.
- Backup restore: the owner starts from the restored exact ledger, reaches M100
  through the historical path when necessary and independently plans only the
  remaining declaration-owned publications.
- On-prem installation/upgrade: uses the same explicit install/upgrade owner;
  backend startup is never the publication owner.
- Emergency roll-forward or guarded down: uses an explicit recovery context
  bound to exact artifact, ledger, expected state and reviewed recovery Plan.
  It cannot obtain authority from ordinary startup or silently select a new
  predecessor.

A fresh-install orchestration envelope contains only exact artifact/Target
identity, start/end ledger expectations and an ordered list of independent
publication ID/Plan digest references:

```text
install_sequence[] = publication_id + plan_digest
```

It is orchestration evidence, not a merged Plan, publication authority, new
ledger or cross-publication transaction. Each publication preserves its own
identity, declaration digest, exact ledger predecessor, actions, Plan digest,
transaction and verification result. Before each step the owner revalidates the
current ledger/pre-state against that Plan. A sequence may make partial forward
progress: if M104 fails after M101–M103 committed, the owner retains their valid
ledger entries, refuses backend startup and stops at M104. Recovery regenerates
only the remaining independent Plans from the current exact ledger; it does not
down successful publications to simulate whole-sequence atomicity.

The explicit baseline entry point is
`uniroute capability-contract-publication install-baseline`. It stops at and
verifies the exact M100 ledger/static projection. Before applying any
historical migration it proves that the existing ledger is an exact artifact
prefix through M100, rejecting gaps, unknown rows and ledgers already beyond
the ceiling. It cannot apply a later declaration-owned publication. Each later
step still requires its own stored Plan and `apply --plan` command.

Guarded down is exposed only as
`uniroute capability-contract-publication guarded-down --plan <path>`. It
requires the original exact Plan and artifact, verifies the server-owned Target
identity and exact ledger ceiling, and refuses retained evidence before any
write.

## 7. Runtime, ACL and compatibility behavior

- The database projection remains the only runtime SQL query authority.
- Backend startup never inserts, updates or synchronizes static contracts.
- Ordinary backend startup and plain `migrate up` validate the compiled
  publication inventory before invoking the migration runner. Every compiled
  post-M100 declaration must have one exact marked artifact migration whose
  generated up/down/verify bytes match the declaration and folded predecessor,
  and every marked publication migration must have one compiled declaration.
  Startup then refuses to cross a pending declaration-owned publication. A
  missing, unpaired or drifted artifact therefore fails before any migration
  write. Only the explicit Strict V1 install, upgrade, publication apply or
  recovery owner supplies publication context.
- Normal runtime request paths must not mutate the two static tables.
- Development/test V1 keeps the current shared database role but requires the
  maintenance window and single owner. This is an explicit limitation, not a
  least-privilege claim.
- The artifact may retain historical declaration/migration pairs, but a
  declaration without its exact artifact migration is invalid rather than an
  inactive runtime candidate. Only database current selectors are executable.
- Missing compiled support, content mismatch, an unknown selector or an exact
  tuple mismatch fails closed for that tuple and through the existing startup
  gates where applicable.
- A committed publication migration raises the artifact ledger ceiling, so old
  binaries fail the exact schema gate. Stable/canary are recreated only from the
  matching artifact.
- After the exact schema-ledger gate, startup performs a complete exact
  comparison of the folded binding history and current selector projection for
  that ledger. It is read-only and never repairs drift.
- Startup does not compare `model_deployment_capabilities` with the publication-
  time snapshot and does not add a global dynamic qualification scan. The
  explicit apply transaction validates only its declared projection inserts or
  updates. Later Probe, Approval and lifecycle evolution continue through the
  existing qualification resolver, Planner, pre-dispatch revalidation and
  already-supported compatibility/reference-integrity gates.
- Rollback uses the generated guarded down or backup/roll-forward runbook. It
  never rewrites historical receipts or silently selects another predecessor.

Strict V1 adds no schema, distributed lock, startup synchronization or dynamic
qualification framework. Its new serving prerequisite is limited to the
static folded projection verifier and the refusal to perform declaration-owned
publication from an unqualified startup/migration mode.

## 8. Required implementation tests

### Declaration and generation

- The exact artifact applies M1–M100 to a fresh isolated PostgreSQL database and
  deterministically exports the byte-identical baseline; a Target export is
  rejected as a source.
- M100 baseline folds to exactly 76 bindings and 51 selectors.
- A database migrated through M100 is exactly equivalent to the baseline
  declaration except for `binding.created_at` and `selector.updated_at`; no row
  is rewritten.
- Declaration ordering does not change canonical bytes or digest.
- Duplicate identity, missing target, inferred predecessor and prior-file edit
  fail.
- Duplicate JSON members (including escaped-equivalent names), unknown fields,
  trailing values and loose numeric encodings fail before typed decoding.
- Generated up/down/verify files are byte-stable and `--check` detects drift.
- First-customer bootstrap expectations are derived from the folded projection;
  no hand-maintained binding/selector count or exact tuple list remains.

### Plan

- `append/no_op`, `current_promotion/no_op`, `insert/update/no_op` and every
  stable conflict project correctly.
- Every deployment projection item includes an exact Target row identity,
  complete expected/final state and affected-row count; post-lock filter drift
  conflicts rather than changing the Plan.
- Normal pending current-unit rows cannot be no-ops; ledger-absent prewritten
  binding, selector or projection rows return
  `unledgered_publication_state`.
- `pending_apply` and `post_commit_recovery_check` do not alter the original Plan
  actions or digest.
- Recovery never returns `already_applied_verified`; it distinguishes exact-plan
  and advanced-compatible static verification while reporting dynamic readback
  as unreconstructable.
- `expected_predecessor=absent` is distinguished from an exact predecessor.
- Dry-run is `REPEATABLE READ READ ONLY` and leaves schema, audit, receipt and
  static-table counts unchanged.
- Artifact, declaration or schema-ledger mismatch fails before an apply-eligible
  plan is emitted.

### Apply and rollback

- A clean M100 predecessor applies one generated K2.7-shaped fixture atomically.
- Binding, selector or exact deployment-projection drift after dry-run causes
  zero partial writes.
- Exact no-op rows are accepted only as folded baseline/predecessor checks; an
  externally completed current-unit append, selector promotion or projection
  mutation does not silently replace the validated Plan.
- Generated projection SQL does not use `ON CONFLICT DO NOTHING`; absent insert,
  exact update and affected-row failures are independently exercised.
- Missing selector uses conditional insert; exact predecessor promotion affects
  one row; every other affected-row count rolls back.
- Final projection verification occurs before commit.
- Post-commit verify runs on the same physical connection before the session
  lock is released.
- A lost apply response or failed post-commit readback can run only the read-only
  `post_commit_recovery_check`; it performs no second write and never emits a
  replacement no-op Plan or publication receipt.
- Legitimate later lifecycle progress is not classified as static publication
  corruption, but its original immediate dynamic readback remains
  unreconstructable.
- A concurrent migration owner waits on the existing advisory lock.
- Trigger resealing is verified on success and rollback.
- Down succeeds only in the empty-evidence fixture and refuses retained
  Probe/Approval/projection/frozen evidence.
- An older artifact refuses the post-publication ledger.
- Backend startup refuses a static folded-projection mismatch even when the
  schema ledger itself is exact, and plain migration startup cannot apply a
  pending publication.
- Startup does not introduce a full-table dynamic qualification validator and
  existing dynamic qualification tests remain the authority.
- Fresh install applies M1–M100 followed by ordered independent publication
  Plans; a middle failure preserves prior committed publications, blocks
  startup and resumes only the remaining sequence.
- Restore from pre-M100 and post-M100 ledgers, on-prem explicit upgrade and
  emergency recovery contexts all preserve independent Plan identities and do
  not merge publications or infer predecessors.

### Repository gates

- Current Admin/Planner/qualification/revalidation behavior remains unchanged.
- Compiled current bindings derive from the declaration and still require exact
  database equality.
- Existing M88–M100 migration and integration tests remain green.
- No Profile, Release, Alias, pricing, wallet, provider or Target capability is
  created by the publication framework tests.

## 9. Explicit non-goals

- No new table, manifest database, publication state machine or customer-facing
  receipt.
- No database role/ACL split in V1.
- No startup synchronization or per-replica upsert.
- No startup-wide dynamic deployment-capability/qualification scan.
- No direct runtime resolution from a compiled-only registry.
- No global catalog/database digest equality.
- No Probe/runtime parser consolidation.
- No grouped approval command, UI workflow or Planner change.
- No K2.7/K3 capability implementation, Target apply, paid call, pricing or
  Release/canonical promotion.
- No claim that Publication V1 eliminates artifacts, CI, maintenance windows,
  deployments or late provider-wire discovery rounds.

## 10. Authorization gates and stop conditions

The product/architecture owner approved coding and Local/CI verification of
this exact design on 2026-08-30. That approval does not authorize Target apply,
deployment of a new migration, K2.7/K3 work, provider calls or
credentials/billing, Release, Alias, canonical or Production changes.

After Local/CI compatibility, rollback tests and an exact development/test
zero-write plan pass, the first real publication apply requires a separate
conditional authorization. It must stop if any of these conditions fail:

- a schema change becomes necessary;
- implementation introduces a second writer or startup synchronization;
- the M100 baseline cannot be reproduced exactly;
- the Target plan contains any conflict, history rewrite, undeclared current
  promotion or tuple outside the approved K2.7 set;
- stable/canary cannot be stopped or the single migration owner cannot cover
  apply through serialized post-commit verification;
- exact artifact, ledger or rollback compatibility is not proven.

An approved security, financial or deployed correctness emergency may still use
the current minimal migration path under its own explicit authority. It does not
authorize this framework or establish precedent for normal publication.

## 11. Final implementation disposition

| Item | Disposition |
|---|---|
| Reuse existing migration owner, session lock and exact ledger | recommended V1 |
| Typed artifact declaration with M100 immutable baseline | recommended V1 |
| Deterministic up/down/verify generation | recommended V1 |
| Read-only canonical Target plan | recommended V1 |
| Strict startup publication-mode gate and static folded verifier | recommended V1 |
| Fresh/install/restore/on-prem orchestration of independent Plans | recommended V1 |
| Separate standalone DB publisher | rejected for V1 |
| `audit_logs` as immutable publication receipt | rejected |
| New receipt/schema/ACL | not authorized; unnecessary for recommended V1 |
| First K2.7 publication apply | not authorized |

The first Target publication apply remains a separate conditional decision
after implementation, closing review and Local/CI verification are complete.
