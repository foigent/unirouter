# Fresh single-instance installer

Current 1.13.0 delivery requires the M127 maintenance/fresh-install contract. See
[connection communication controls](XINSIGHT-COMMUNICATION.md) for manual defaults, migration and recovery. Earlier M126 candidate notes below remain historical.

For the 1.10.0 candidate, the final owner step is M126 commercial License
retirement. No commercial License import or public key is required; exact
Artifact identity, publications, coverage and atomic completion remain required.
See [the M126 contract](COMMERCIAL-LICENSE-RETIREMENT.md). Local synthetic PG16
fresh/resume and real restore/rebase checks passed on 2026-10-10; see the
[execution record](../agent-workflows/EXECUTION-RECORD.md). Docker installation
and public artifact acceptance remain pending. The new
[Linux wrapper](../community/install-linux.md) uses this existing owner.

## Retained original design context

This is the bounded installer design and command contract reviewed by the UR
supervisor, including the ordinary-prefix Plan recovery, actual-writer admission
and historical-completion amendments. At the original 1.4.1 design checkpoint,
the candidate was source only; the baseline and worktree below describe that
historical checkpoint, not the current 1.10.0 validation state above.

Accepted product baseline: `59f7712d09653034756b846b15b8d69a16782791`, tree
`ab09c48939539a10dbb039814489d764c0b53ca5`, version 1.4.0. The retained workflow
optimization is `6676c0c123805b53f1f0337d565d8bf8cb9c63ac`, tree
`e0688f59376e71eb6446fe034195b270c1fe7a7b`. Work proceeds on
`codex/fresh-install-owner` in the existing isolated worktree
`/private/tmp/unirouter-test-preparation-20260911`. Neither the original accepted
worktree nor the user's main checkout is part of this change.

## Product contract

The same backend executable/image runs one one-off `fresh-install` owner. It
does not create a second backend, service, schema ledger or serving generation.
All M1–M118 SQL bytes, Publication contracts and the original session migration
lock remain in use. No M119 is introduced. Runtime and successful M118 reset
remain separate contracts; this installer does not make reset supported.

`plan` accepts a genuinely empty database, or the exact original empty
`schema_migrations` table left by a failed older startup. It checks catalogue
objects, not just the public-table count. Mixed schemas, unknown ledger entries,
altered empty ledger tables and existing comments refuse fresh ownership.
The original empty ledger must also have no policy (even with RLS disabled),
and each column must retain its type's default collation identity.
`plan` and `check` do not write to PostgreSQL. Plan output is a new private file.

`apply` rechecks the exact target, artifact and empty shape under migration lock
86420517831702491. It creates/claims the original ledger and writes a bounded
canonical COMMENT before M1 in one transaction. The COMMENT contains the outer
Plan and independent Publication Plan references. It is orchestration evidence;
only the original `schema_migrations` rows determine schema progress. It adds no
public table and does not bypass first-customer seed checks.

Active ownership pins database system identifier, name, OID, owner, connecting
role, environment, embedded build identity, inspected image digest, actual
executable digest, support manifest and all migration SQL bytes. The original
physical connection holds the existing session migration lock across all
steps. There is no separate control connection or cancellation-after-lock-loss
mechanism. A disconnected owner cannot silently acquire a replacement pool
connection. A pool with one connection is supported.

Ordinary startup, including SKIP_MIGRATION, rejects an active envelope. Formal
migrate-up, baseline continuation, publications, guarded down and compatible
expansion check admission on their actual locked connection. Admin writes check
their actual writer connection; runtime ingress, model reconciliation/sealing,
connection pause/revocation, reset and upgrade recording check inside their
actual writer transactions. Their existing permission and domain checks remain.

Ordinary admission reads metadata and ledger together. An empty ledger never
grants admission. Already admitted supported writers cannot recreate a fresh
database: their mutations preserve the ledger and its nonempty historical
prefix. Reset preserves the schema ledger; publication down retains M100/history.
Manual DROP/SQL, out-of-band restore and an old binary entering an incomplete
new installation are not supported concurrent actors. Restore retains the
existing stopped-writer maintenance contract.

Low-level `migrate.Run` and `RunThrough` retain historical test/restore fixtures
and reject an active envelope. The formal CLI uses `RunExisting` and
`RunExistingThrough`; normal startup uses `RunRuntime`. Existing historical
migration tests are not silently rewritten to use the new installer.

## Commands and durable files

Use the existing approved environment file, database role and backend image.
No credentials are created or rotated by these commands. Keep DSNs and secrets
in the approved environment channel, not command arguments or logs. Supply the
actual inspected `RUNTIME_IMAGE_DIGEST`; release build version/commit/tree must
be embedded by the original Dockerfile. Environment strings cannot replace the
embedded identity. Do not use a default `local`/`unknown` build.

```sh
/app/uniroute fresh-install plan --out /evidence/install.json
/app/uniroute fresh-install check --plan /evidence/install.json
/app/uniroute fresh-install apply --plan /evidence/install.json --evidence-dir /evidence/publications
/app/uniroute fresh-install resume --plan /evidence/install.json --evidence-dir /evidence/publications
```

The evidence parent must already exist in the approved persistent mount. `apply`
creates a new private directory; `resume` uses that exact directory. Files are
bounded private regular files, created exclusively and fsynced along with their
directory. Preserve the outer Plan, every independent Plan and any orphan file.
`check` reports that it has not checked independent local Plan files; successful
`check` alone is not authorization or proof of a complete resume.

For each publication P0 is durable before its reference is adopted in the DB,
and adoption commits before any P0 migration. M107/M109/M112 can commit before
their following publication. On interruption, the owner proves the actual
ordinary-only prefix against P0, builds a new original-framework Plan P1 from
that prefix, keeps the same target/authority/final ledger and appends a
`supersedes` reference. Original hooks recheck P1's complete digest; P0 is never
edited. Publication DDL, projection, ledger row and committed reference are one
transaction. A committed publication is recovered by its original Plan and the
original read-only post-commit checker, never by a fabricated no-op Plan.

A durable but unadopted file is not execution evidence. If the DB has not moved,
the same validated file can be made durable again and adopted. An adopted Plan
needs no local adoption receipt. Missing or altered referenced Plan bytes stop
active resume; restore the original evidence rather than synthesize history.
There is no claim that later static readback reconstructs historical dynamic
readback.

M114–M118 use the opaque fresh owner, not an online compatible-expand with a
fictional empty fleet. M118 invokes the existing budget certification, verifies
the actual final ledger/static projection/current coverage and stores completion
in the same transaction as its DDL and ledger row. A lost final command response
does not require another certification.

Completed metadata describes origin. Ordinary runtime does not require old Plan
files, DB OID/name/owner or the original image to remain current. It validates
the historical envelope, actual supported ledger/static projection and actual
effective coverage certificate using the current binary. `check` also accepts
an intact completed origin after a supported restore to another DB. Run the
supported `scripts/source-budget-restore-rebase.sql` under the original stopped
restore owner; preserve the origin COMMENT and initial certification history.
On M119 the same transaction also replaces every C recovery epoch, for both
SQL-only and paired SQL/body restores. All content readers and capture/media
writers must remain stopped until the restore and binding checks finish.
Active `apply`/`resume` retain the strict original target/artifact binding.

## Native verification and delivery boundary

The source-writing authorization permits only small text/Git changes and static
checks on M4. The last local reading was 21,267,664 KiB available, below the
21 GiB reserve plus 3 GiB admitted validation increment. No Go compile/test,
image build, database creation, dependency installation or cache generation is
permitted under that reading. No cleanup of existing assets is requested.

Native execution is dispatched by the overall coordinator through the original
F plan. This executor does not contact XI or touch protected original ASUS UR
assets. The F resource, signing, environment and transfer approvals remain
separate; this source document does not enlarge them. Recheck actual headroom
before native work. Reuse the existing F build/carrier and database/Redis
configuration, APK preparation, 2100-second stop grace and local/10m/3 logging.
Do not invent another fixture, bootstrap SQL or legacy schema initializer.

Build the candidate using the existing Dockerfile with `RUNTIME_VERSION` from
VERSION and exact `RUNTIME_COMMIT`/`RUNTIME_TREE` from the delivered Git objects.
Use the already approved trust mode/public trust inputs. Record image inspection
and backend SHA-256 before running it. The build flags already exist; no new
private signing-key path or second service is needed.

For the additive test entry, supply the same synthetic TEST_DATABASE_URL,
REDIS_ADDR, REDIS_DB and REQUIRE_TEST_DATABASE=1 used by the retained runner.
Set UR_FRESH_TEST_BINARY to the native executable from the inspected candidate
image and UR_FRESH_TEST_IMAGE_DIGEST to that image's actual identity. PostgreSQL
client tools must match the fixture's supported server major. The script checks
tool availability before compiling. Use a new evidence directory:

```sh
bash scripts/test-fresh-install.sh --check /evidence/fresh-run
bash scripts/test-fresh-install.sh /evidence/fresh-run
bash scripts/test-backend-integration.sh --log-dir /evidence/backend-regression
```

The additive runner retains JSON logs and original process/restore evidence,
then uses the accepted summary tool. It does not replace any of the five
existing selectors or other required Backend/Frontend/Docker checks. The native
image still needs the original first-customer password/login, canonical
activation, same-backend restart and F overall runtime chain. An executable
running outside its image is byte-level evidence, not container/resource or
end-to-end F acceptance.

| Required evidence | Executable coverage / remaining work |
| --- | --- |
| Canonical metadata, malformed/foreign COMMENT, missing binary identity, immutable files | `TestFreshEnvelope*`, `TestFreshPlanFiles*`; written, unrun |
| Exact empty DB and original empty ledger; mixed DB refusal; plan/check no DB writes | `TestFreshInstallClaimsOnlyOriginalEmptyLedgerIntegration`, `TestFreshInstallRefusesMixedAndAlteredEmptyDatabasesIntegration`, process/native tests; written, unrun |
| Disabled policy or explicit column collation on the empty ledger; formal plan/check refusal with unchanged DB and original Plan | `TestFreshInstallPlanCheckRefuseEmptyLedgerPolicyAndCollationDriftIntegration`; written, unrun |
| Kill before M1, after M100, durable-unadopted and adopted-unexecuted Plan | `TestFreshInstallProcessRecoveryIntegration`; actual SIGKILL observer exists only in Go test invocation; written, unrun |
| M107→108, M109→110, M112→113 ordinary-prefix successor Plans; missing P0; committed response loss | Same process test; written, unrun |
| Active M113 refuses runtime; actual migration backend termination; stale Plan; pool size one | Same process test; written, unrun; full native CLI startup/writer cases still required |
| M118 atomic certification/completion, no fake runtime generation, no recertification on retry | Same process/native tests; written, unrun |
| Actual pg_dump/psql restore, original rebase, different DB identity, ordinary writer disconnect | Same process restore test; written, unrun |
| Same-M118 successor image, wrong active target/artifact and corrupted current coverage | Native operator must retain exact negative/positive image/runtime results; pending |
| Exact image build, first-customer seed/lifecycle, real same-instance restart and full regression | Original F/required checks; all pending |

Static syntax, formatting and source checks cannot fill these execution gaps.
The supervisor reviews the exact source commit/tree and routes native validation
through the overall coordinator. Product acceptance remains open until its
required native evidence passes. Experience file: no change needed; proposed
recovery lessons lack the execution evidence required for admission.
