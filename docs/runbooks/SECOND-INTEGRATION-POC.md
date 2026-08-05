# UniRouter second-integration POC runbook

## Authority model

Opening a task, saying “start the second integration”, accepting a design, or
reusing evidence from an earlier campaign grants no mutation authority. The
three user batons are independent and never inherit authority from one another:

1. `START_SECOND_INTEGRATION_DATA_RESET` authorizes only quiescence, one fresh
   rollback backup, final preview, controlled product-data reset, Source/Cursor
   epoch transition, and empty-baseline verification.
2. `START_SECOND_INTEGRATION_BOOTSTRAP` authorizes creation of the new customer
   organization, zero wallet, users, projects, channels, Provider/Feed
   credentials, Workload Applications, and application credentials.
3. `START_SECOND_INTEGRATION_TRAFFIC` authorizes only the jointly frozen callers,
   models, request/provider-attempt counts, CNY cap, time window, and stop
   conditions.

The DATA_RESET user baton is further split by the coordinator into two narrow
operator batons. `DATA_RESET.PREPARE` permits quiescence, backup, isolated
restore verification, next-Source configuration, and the final read-only
preview. `DATA_RESET.DELETE` permits `reset apply` only when bound to the exact
final artifact SHA-256, stable scope SHA-256, next Source Instance, Target
runtime identity, classification, and user-confirmation time. A changed table,
count, classification, runtime identity, schema/trigger/FK/sequence digest, or
protected-content digest returns `RESET_PLAN_DRIFT_DETECTED` and stops.

Receiver credential creation or staging is BOOTSTRAP-only. Provider calls,
Feed pulls, and test requests are TRAFFIC-only. Invocation Optimization, Exact
Cache, and Compression remain absent/default-off unless separately authorized.

## Release and environmental gate

Before any destructive preparation, freeze one sanitized gate artifact that
proves all of the following:

- GitHub `main` exact commit/tree and terminal-green CI for that commit;
- Target runtime version, commit, source tree, release manifest/artifact/image
  digests, schema ledger, and protocol/binding pin are exact-current-main;
- health/readiness and PostgreSQL/Redis dependencies are ready;
- commercial License is enforceable and remains valid through the planned
  integration window plus seven days;
- TLS hostname/SAN validation succeeds and the leaf remains valid for strictly
  more than seven days at reset time;
- authenticated admin UI smoke is verified with the preserved account;
- Feed/Outbox workers, current Source Instance, and public/auth boundaries have
  been read back without sending a model or Provider request; and
- disk and build headroom pass the formulas below.

Record actual byte values, not percentages alone:

- `R0 = pg_database_size + max(25% of pg_database_size, current WAL retained)`:
  transactional database/WAL rollback headroom;
- `R1 = actual fresh-backup bytes` when staged on the Target filesystem, or
  zero on the Target only when the verified backup resides on separate durable
  storage; and
- `R2 = new uncompressed image bytes + largest retained rollback image bytes +
  2 GiB` for image extraction and build/runtime scratch.

Stop if Target root usage exceeds 75%, or if free Target bytes are less than
`R0 + R1 + R2`. The backup destination separately needs at least twice the
estimated backup size before backup starts. Re-read these values after an
exact-main deployment. Deployment, restart, TLS renewal, and storage cleanup
require their own authority and are not implied by this runbook.

## Product-owned reset contract

The supported command is an in-place PostgreSQL transaction. It never uses a
broad table wipe, cascading truncate, sequence reset, replication-role bypass,
or an ad-hoc operator SQL script.

The current 88-table contract classifies:

- 80 reset-candidate tables: 78 are deleted completely; `users` retains only
  the selected admin; `orgs` retains only that admin’s active root organization
  shell;
- `settings` as partially protected, retaining only `ledger.amount_scale`,
  `ledger.version`, optional `license.offline`, plus the runtime-required
  `sensitive_words` row reset to the safe empty JSON-array baseline;
- six fully protected platform tables:
  `commercial_license_entitlements`, `deployment_profiles`,
  `gateway_endpoints`, `micro_ledger_runtime_state`, `release_history`, and
  `release_state`; and
- `schema_migrations` as the protected schema ledger.

TLS keys/certificates, database and Redis credentials, host configuration,
release images/manifests, and certificate-renewal configuration are outside the
product-data reset. Every PostgreSQL sequence is read and digest-bound but is
not changed. Its owner table/column, positive increment, non-cycling state,
current maximum row ID, and next allocatable value must prove the next value is
strictly greater than every existing ID. A new or unclassified table blocks
the plan.

The selected-account preflight requires exactly one local account whose
normalized login is `admin`, active platform-admin authority, an active root
organization shell, and a supported password verifier. The plan stores only
row/verifier digests; it never serializes the verifier. Protected
deployment/network rows may reference only the retained account. Apply removes
every pre-reset session, preserves the exact verifier and login/role identity,
clears optional email/OIDC identity fields, advances `auth_epoch`, and empties
the retained organization’s settings. Those minimum-shell postconditions are
read back before commit.

Migration `000057_second_integration_reset_support` changes only the two
reviewed cross-table cycle constraints from non-deferrable `RESTRICT` to
initially-immediate, deferrable `NO ACTION`; PostgreSQL `RESTRICT` itself cannot
be deferred. Preview derives a deterministic
child-before-parent order from the live FK catalog and rejects any other cycle.
Apply takes a transaction-scoped advisory lock and `ACCESS EXCLUSIVE` locks on
the exact classified table set, repeats the plan under lock, defers only those
two constraints, and temporarily disables only the exact reviewed append-only
and micro-ledger evidence triggers. Trigger definitions and enabled states are
digest-bound. Apply validates the deferred FK events before restoring those
triggers, then re-reads both before commit. Every delete command asserts its
exact affected-row count.

Redis is not globally flushed. Only these reviewed product prefixes are scanned
and unlinked, then re-scanned to zero:

- `limit:{`
- `rl:`
- `uniroute:response-cache:v2:`
- `ur:agentcap:v2:`
- `wallet:v2:`
- `workload:{admission-v1}:`

Unrelated Redis keys are preserved. A read-only no-drift PostgreSQL check runs
before Redis mutation. If PostgreSQL later fails, its transaction rolls back;
the only possible residual is a safe cold product cache, which must be recorded
and revalidated before retry.

## Stable preview and apply inputs

Preview requires the explicit current runtime identity, selected account, and
next Source Instance:

```bash
uniroute reset preview \
  --next-source-instance '<reviewed-next-source>' \
  --target-tree '<40-character-target-tree>' \
  --preserve-local-login admin \
  --output '<new-private-preview-file>'
```

The output is a new mode-`0600` file. `artifact_sha256` covers the exact bytes,
including `generated_at` and current Redis observations. `scope_sha256` is the
stable semantic digest and deliberately excludes `generated_at`, its own digest,
and volatile Redis counts. It binds contract/profile, Target version/commit/tree,
current/next Source, selected admin/org digests, protected contents, ordered
table counts/classifications/reasons/actions, the required setting’s current-row
and safe-target digests, delete order, FK/trigger/sequence schema evidence, and
execution preconditions. A new timestamp alone does not require renewed user
approval; semantic drift does.

`reset apply` requires all flags below and verifies every referenced regular
file is private and matches its supplied SHA-256 before connecting to runtime
dependencies:

```bash
uniroute reset apply \
  --plan-file '<final-preview>' \
  --plan-sha256 'sha256:<exact-artifact>' \
  --scope-sha256 'sha256:<stable-scope>' \
  --target-tree '<40-character-target-tree>' \
  --backup-file '<one-fresh-rollback-backup>' \
  --backup-sha256 'sha256:<backup>' \
  --isolated-restore-evidence-file '<restore-evidence>' \
  --isolated-restore-evidence-sha256 'sha256:<restore-evidence>' \
  --target-gate-evidence-file '<exact-main-ci-tls-license-capacity-evidence>' \
  --target-gate-evidence-sha256 'sha256:<target-gate-evidence>' \
  --quiesce-evidence-file '<quiesce-evidence>' \
  --quiesce-evidence-sha256 'sha256:<quiesce-evidence>' \
  --consumer-epoch-evidence-file '<xinsight-epoch-evidence>' \
  --consumer-epoch-evidence-sha256 'sha256:<epoch-evidence>' \
  --authorization-id '<coordinator-delete-baton-id>' \
  --authorized-at '<user-confirmation-RFC3339>' \
  --confirm DELETE_PRODUCT_DATA_KEEP_EXACT_ADMIN
```

The apply process additionally requires the effective runtime Source Instance
to equal the reviewed `next_source_instance`. It is not allowed to fall back to
an empty/default value or the old identity.

## DATA_RESET execution and rollback order

1. Confirm exact-main Target, CI, TLS, License, capacity, authenticated UI, and
   protocol/binding gates.
2. Stop new model admission and background producers. Drain in-flight model
   work, billing finalization, internal Outbox, Feed materialization, and
   delivery attempts. Prove pending, claimed, and dead-letter counts.
3. X-Insight Owner stops the UniRouter consumer and records its old
   Cursor/Checkpoint. UniRouter never edits X-Insight Cursor/Checkpoint state.
4. Create one fresh PostgreSQL rollback backup, compute SHA-256, run archive or
   directory consistency checks, restore it to an isolated database, and verify
   schema ledger, protected state, selected admin digest, and representative
   counts. Never restore over Development during verification.
5. While the old Source Instance is still the explicit effective identity, run
   the final preview with the reviewed next Source supplied separately. The
   coordinator compares the stable scope digest and issues `DATA_RESET.DELETE`
   only for the exact artifact and Target.
6. Keep every producer stopped, configure and read back the reviewed next
   Source Instance, then run product-owned `reset apply`; apply refuses to
   delete unless its effective Source equals the plan's next Source. The
   proposed `ecs.second-integration.v2` remains conditional until X-Insight
   independently proves non-use and epoch binding. Any drift or assertion
   failure stops.
7. Verify the empty baseline before login/bootstrap: exactly the selected admin
   and normalized minimum organization shell; unchanged password verifier;
   advanced auth epoch; zero sessions; zero rows in every delete-all table;
   only protected settings plus empty `sensitive_words`; unchanged
   schema/FK/trigger/sequence evidence; zero reviewed Redis prefixes; no
   Provider/Feed traffic.
8. Verify old Source identity, API/application/provider/Feed credentials, and
   old IDs cannot authenticate, publish, pull, or be selected. Do not create a
   replacement credential during this phase.

Rollback points are intentionally small:

- before apply: resume only after canceling the unused delete baton and
  restoring the old explicit Source configuration;
- before PostgreSQL commit: the transaction rolls back automatically; recheck
  cold Redis state and regenerate the final preview;
- after commit but before BOOTSTRAP: restore the one verified backup only by an
  explicitly approved recovery procedure, then restore the old Source epoch as
  one coordinated action; and
- after BOOTSTRAP begins: stop and make a joint recovery decision. Never mix a
  restored pre-reset database with new-epoch Feed/Cursor state.

Retain the one fresh backup until the second-integration acceptance plus seven
days, with a minimum of 14 days. Deletion after that window requires the normal
backup-retention approval; this runbook grants none.

## BOOTSTRAP order

After the empty checkpoint is accepted, one new authenticated admin UI session
and its login audit/outbox record are allowed. Under the separate BOOTSTRAP
baton, the platform admin calls:

```text
POST /api/v1/bootstrap/first-customer
X-UniRouter-Client-Request-ID: <stable-operation-id>
{"organization_name":"<reviewed-customer-name>"}
```

The endpoint runs serializably, locks the unique normalized `admin`, and
rechecks the strict empty baseline. It atomically creates only one active root
customer organization, one CNY wallet whose balance/granted/frozen/spent values
are all zero, and one audit row. It creates no users, projects, credentials,
policies, connections, or traffic. The same client request identity is an exact
idempotent replay; a different payload or a second customer attempt fails.

Only after that succeeds may the remaining bootstrap objects be created in a
reviewed order: customer admin/users, project, channel metadata, Provider
credential, model governance/runtime release, Workload Application, application
credential, then receiver-generated staged Feed credential and connection.
Credential plaintext follows the existing one-time handling contract and is
never placed in evidence. The Feed stays paused until both owners accept the new
epoch.

## X-Insight boundary

X-Insight cleanup is limited to the dependency closure of the old UniRouter
Source. It must preserve unrelated LiteLLM/OpenInference data and unchanged
unrelated checkpoints. Ambiguous cross-source rows fail closed. Therefore
“empty baseline” on X-Insight means zero UniRouter-derived rows/state plus a
reconciliation proving unrelated-source preservation; it never means a global
X-Insight product reset.

Old-epoch Outbox/Feed facts must never be republished under the new Source
identity. Feed connection/credential deletion, replacement registration,
activation, retained floor, acknowledgement, and X-Insight checkpoint creation
are joint decisions. Any identity/Feed/Cursor disagreement returns to the
coordinator before either side proceeds.

## TRAFFIC freeze and stop conditions

Before `START_SECOND_INTEGRATION_TRAFFIC`, record the exact current Provider
price readback, billing unit, model, caller/device, number of external requests,
maximum Provider attempts per request, total Provider-attempt ceiling, CNY cap,
time window, and request-level stop conditions. No prior campaign key, budget,
traffic allowance, or unused request count carries forward.

Stop immediately on identity mismatch, more Provider attempts than frozen,
ambiguous billing finality, CNY cap exhaustion, TLS/License degradation,
unexpected Feed publication, dead-letter growth, or any request outside the
approved caller/model set.

## Verification matrix

| Gate | Required evidence | Mutating external traffic |
|---|---|---:|
| Local Go | format, unit, race, vet, reset semantic-digest and security-literal tests | 0 |
| PostgreSQL | clean migrate up/down/up; preview read-only; exact admin/password digest; FK cycle; row assertions; rollback/drift; protected/settings/sequence readback | 0 |
| Redis | prefix observation/purge; unrelated-key preservation; no global flush | 0 |
| Frontend | type-check, unit tests, production build, platform-only bootstrap UI | 0 |
| Docker | image build, migration availability, non-root/runtime identity checks | 0 |
| Protocol | exact Binding pin and existing Feed/OpenAI conformance; no protocol semantic expansion | 0 |
| Security literals | no broad truncate/cascade, sequence reset, replication-role bypass, all-trigger disable, secret/credential literal, or private address in new evidence | 0 |
| External integration | only after TRAFFIC baton and frozen budget/request envelope | bounded |

Post-deploy/pre-reset acceptance requires exact artifact/source parity, terminal
CI, TLS `>7d`, valid License window, authenticated admin UI, healthy dependencies,
idle/quiesced workers, drained Feed/Outbox, and zero test traffic. Post-reset
acceptance requires the strict UniRouter baseline and X-Insight source-closure
reconciliation above. Production remains not authorized.
