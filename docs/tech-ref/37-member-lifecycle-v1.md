# Member Lifecycle V1

## Outcome

UniRouter now defines one IdP-neutral project-member lifecycle contract for
`create`, `update`, `suspend` and `deprovision`. It evaluates effective project
memberships, personal API keys, Agent capability roots and administrator
sessions without embedding SCIM-vendor fields or changing product state.

The event contract is `unirouter.member-lifecycle.v1`; evaluation reports are
`unirouter.member-lifecycle-report.v1`.

## Existing Product Truth

The contract maps to existing UniRouter authority rather than inventing a
second IAM model:

- users own an organization role, account status and monotonic `auth_epoch`;
- project-scoped users have transactionally serialized `project_members`;
- personal project API keys require an active owner and live membership;
- Agent capability authority binds key, organization, project, owner status,
  owner role and membership, and rechecks that authority within the existing
  bounded 15-second cache SLO;
- administrator JWTs require an active user/organization, exact role,
  `auth_epoch` and a live server-side session;
- hard user deletion disables owned personal keys first, then database foreign
  keys cascade member and session removal.

Role or status changes now atomically increment `auth_epoch` and revoke all
live administrator sessions. This prevents a pre-suspension JWT from becoming
valid again if the account is later restored.

## Lifecycle Rules

| Operation | Required transition | Effective-access result |
|---|---|---|
| `create` | absent → `active`, revision `0 -> 1` | declared valid memberships and active capabilities |
| `update` | active/suspended → `active` | exact new assignment set; removals become revocations |
| `suspend` | `active` → `suspended`, assignments unchanged | zero effective memberships and capabilities; sessions denied and revoked |
| `deprovision` | active/suspended → terminal `deprovisioned` | zero assignments; sessions/memberships cascade and personal keys are disabled before delete |

Suspension is reversible through a new versioned update. Deprovision is
terminal: re-provisioning requires a new portable subject identity, preventing
accidental resurrection of old event or capability history.

Every event carries:

- portable SHA-256 `subject_ref` and `source_ref`, never an email, username,
  SCIM ID, tenant secret or credential;
- canonical int64 decimal `parent_revision`/`revision`;
- exact six-digit UTC `occurred_at`;
- sorted membership and capability snapshots;
- exact `before` and `after` state for CAS/replay evidence.

## Replay And Revocation

The isolated memory ledger proves:

- same `event_id` plus the same canonical semantics → `duplicate_noop`;
- same `event_id` plus different semantics → `event_identity_conflict`;
- stale revision or mismatched `before` snapshot → CAS conflict;
- no event can follow deprovision;
- removed projects revoke their effective membership and every removed
  project-bound capability;
- suspend/deprovision revoke every effective membership and capability.

The memory ledger is conformance evidence only. It is not a directory sync
queue or production provisioning store.

## Product Surface

- `GET /api/v1/member-lifecycle/offline-contract`
- `POST /api/v1/member-lifecycle/offline-preview`
- `cmd/uniroute-member-lifecycle-conformance`
- platform/organization-admin **成员生命周期** workspace

The page auto-validates the first safe fixture, keeps the Lifecycle Inspector
visible, and lets operators inspect the full lifecycle chain without opening a
drawer. Both endpoints set `Cache-Control: no-store`; preview is bounded to
1 MiB and has no persistence or network path.

## Immutable Local Artifacts

| Artifact | SHA-256 |
|---|---|
| `member-lifecycle.v1.schema.json` | `3f871bfca5ee66e34a0d1d804e4181c5228be5f0ea7b831878e0a30add27b964` |
| `fixtures/project-member-chain.v1.json` | `dfdc04f263438b98f332aded4645393dac611fb6bd51d06634bbb292767919b8` |
| `reports/core.v1.json` | `b8d6d9b1b643ade073061c96effd2a6d84335722219977ffd5743cedf9bb02b2` |

The deterministic report contains 17 passing cases for lifecycle/CAS,
revocation, suspension, reactivation, terminal deprovision, replay, canonical
hashing, vendor/privacy rejection, strict JSON, timestamp precision and int64
boundaries.

## SCIM And Runtime Boundary

No SCIM vendor, IdP source of truth, attribute mapping, group semantics,
credential, endpoint, polling/webhook direction or conflict policy has been
selected. Those decisions remain under P7-D02. A later adapter must translate
its source into this contract; it may not redefine UniRouter IAM, membership
or capability semantics.

This implementation performs no IdP call, live provisioning, database
migration, deployment, traffic, credential, cursor/checkpoint or cross-product
operation.
