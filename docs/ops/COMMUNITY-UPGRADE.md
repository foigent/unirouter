# Community upgrade: retained installations and exact schema transitions

Current signed entry on 2026-10-31: [1.15.3 / sequence 20](https://github.com/foigent/unirouter/releases/tag/v1.15.3-preview.20) targets M129. [Source PR #486](https://github.com/foigent/unirouter/pull/486) passed [disposable Linux qualification](https://github.com/foigent/unirouter/actions/runs/41166284675) for exact M127 → M129, M128 → M129 and later M129 → M129 ordinary upgrades. [Formal public-package consumer](https://github.com/foigent/unirouter/actions/runs/41196358700) separately passed fresh M129 installation and recovery. M129 corrects the persisted M128 observation validator's namespace for native PostgreSQL restore. Existing-server maintenance still requires the original-host admission, cold pair and independent restore below; these results do not establish its deployment.

Historical status on 2026-10-29: [1.15.0 / sequence 17](https://github.com/foigent/unirouter/releases/tag/v1.15.0-preview.17) is signed and public. Its [formal Linux consumer](https://github.com/foigent/unirouter/actions/runs/40739178872) passed exact signed-package download, anonymous image pulls, fresh M128 installation and recovery. Source PR #482 separately passed real disposable Linux M127 → M128 upgrade cases. Each existing server still requires the original-host checks and retained evidence below before maintenance; these results do not establish its deployment.

The entry upgrades one existing Linux amd64 community installation while
preserving its database, content, settings, credentials and release history.
It reuses the full signed release, installed trust ring, installer identity,
native ingress/generation/upgrade owners and existing cold-recovery checks.
Fresh installation continues to use `install`/`resume` and the exact candidate schema.

## Supported source

- Exact complete M127, M128 or M129, completed native installation envelope, and its recorded
  signed full package. Use the package actually named by native release history;
  after an earlier manual patch it may differ from the install directory's
  original `release.tar.gz`.
- The standard single backend, or the named `ur_edge_pair` adapter with the
  retained edge files and one/two backend writers. All writers and receivers
  must be on the same original rootful Linux Docker daemon and owned resources.
- Existing PostgreSQL/dependency image digests stay unchanged. Candidate backend
  and frontend images are selected from the verified full package and pulled
  before withdrawal. `check-upgrade` and `upgrade-status` do not pull or change
  services, configuration or database state.

Unknown layouts, receivers, writers, configuration drift, unresolved ingress,
missing source/trust bindings and insufficient recovery capacity stop the whole
operation. This finite route does not search arbitrary version paths or provide
rolling compatibility or arbitrary future schema heads. Existing historical maintenance paths retain
only their previously supported scope.

## One entry for check, upgrade, resume and status

First prepare and independently verify the complete release using the existing
[bootstrap instructions](../community/install-linux.md). The same signed full
bundle supports fresh sequence 0 and its declared upgrade window; exact native
predecessor binding is checked separately. There is no new differential download
or executable-fragment channel.

Use the prepared signed payload with the original operator, for example:

```sh
python3 -I -B "$RELEASE/payload/deploy/upgrade.py" check-upgrade \
  --release "$RELEASE" --verifier "$RELEASE/uniroute-upgrade" \
  --public-keys "$RELEASE/public-keys.json" --directory /opt/unirouter \
  --source-package "$RETAINED_SOURCE_PACKAGE"
```

Replace only `check-upgrade` with `upgrade` to perform maintenance. Retain the
same installation, release, verifier, key ring, source package and named layout
arguments for `resume-upgrade`. The original source package is never overwritten.
A full candidate copy is retained under the installation's private `releases/`.

For a subsequent upgrade, omit `--source-package` to select the package in the
completed `current_release` record. An explicit package must still match the
actual native release state/history and recorded signature admission time.
`resume-upgrade` refers to the current candidate's unfinished operation; a
completed prior candidate does not block a new `upgrade` claim. The new claim
retains the exact prior `install-state.json` bytes in its own private operation
directory with a digest, instead of recursively embedding old witnesses. Legacy
embedded witnesses remain readable; closed recovery-tool fields are historical
data and are never executed. The existing image override is bound to the source
and replaced atomically only at the image configuration step.
For the named adapter also supply `--layout ur_edge_pair --edge-directory` with
its original absolute edge directory. These are existing paths, not new resources.

### Retained historical exits

A normally restarted supported installation may retain earlier runtime-generation
rows. Supply `--historical-exits /absolute/private/historical-exits.json` when
those rows have original, independently reviewed termination evidence. The
same optional argument works through the online bootstrap. The current serving
layout remains one writer or stable/canary; its complete union with the explicit
historical set must fit the native limit of 32 before maintenance starts.

The closed `unirouter.historical-exits.v1` document binds the installation,
database/system/OID, installed source package/sequence and each exact historical
generation, support digest and revision. Private original observations are
selected by bounded RFC6901 JSON data references and their SHA-256 identities.
Current native resource queries recheck ownership and physical liveness. A tool
failure remains unknown. Original files and frozen input must survive resume;
the operation retains their bytes alongside its existing private evidence.

Two kinds preserve the original facts:

- `process_exit` binds the original container/image/instance, actual successful
  native stop, stopped inspection and PID/start observations. A retained PID
  without its start value must currently be absent; an existing/reused PID
  remains unknown.
- `native_replacement` binds original and distinct successor resources and
  runtime identities to the actual successful Compose replacement records.
  It proves historical termination under that native operation's contract.
  An unrecorded application exit code or PID stays unknown. Current maintenance
  writers still require the existing complete clean-stop proof.

Native command observations require explicit recorded arguments for the exact
project/service. Older split records are normalized during operator preparation
and reviewed against the actual program and original receipts. The closed
`unirouter.normalized-command-observation.v1` record retains those origins and
their hashes. The signed entry validates data and bindings; it never executes
or parses an old program. Caller `approved`, `gone` or `clean_exit` flags cannot
replace the original observations.

Once all current receivers and writers are stopped and leases have expired,
the existing signed maintenance owner rechecks the exact entire nonstopped set
and retires it atomically. Its original signature, installation, full ledger,
ingress, financial-resolution, compatibility lock and old-identity fencing
remain required. Every historical row and original observation stays retained.

The verified online bootstrap delegates the same four actions to this signed
program. Keep its existing `--bootstrap`, `--bootstrap-sha256`, `--directory`
(prepared release) and `--install-directory` arguments. Upgrade actions accept
`--source-package`, `--layout`, `--edge-directory` and `--historical-exits`; they do not use fresh
installation name, port or socket overrides.

Status can also be read directly without Docker, signature verification or
service changes:

```sh
python3 -I -B "$RELEASE/payload/deploy/upgrade.py" upgrade-status \
  --directory /opt/unirouter
```

Status is a recovery witness, not database or release authority. Completed
`resume-upgrade` still requires the frozen Compose/settings and exact upgrade
image override before invoking any native carrier; changed execution
configuration refuses without reopening maintenance. New business records are
outside that file check and remain valid.

## Maintenance and recovery

1. Recheck the exact daemon, source package/history, complete ledger, settings,
   effective Compose, processes, writer fleet and ingress predecessor after
   candidate image acquisition and before withdrawal. The existing private
   installer state receives one complete atomic upgrade witness. Its package
   identity also prevents the retained M127 installer from restarting the old
   release through fresh-install resume.
2. Prepare complete native withdrawal, gracefully stop every receiver, complete
   withdrawal, then gracefully stop every writer. Each process/container exit
   is proved independently. Expired native leases alone never prove physical
   exit. Waiting for lease expiry does not claim a pending retirement command.
3. Check and atomically retire the exact complete writer batch through native
   signed-package maintenance admission. Read back stopped rows. Empty readback
   can check authority; an empty retirement cannot invent a new retirement.
4. Keep the original quiet predicates, take a native custom database dump and
   pair its content/configuration dependencies with GNU tar ACL/xattr options.
   Verify an actual independent database/file restore, unchanged original tables,
   body bindings and the existing financial/content restore rebase. Both the
   original and verification objects are retained. No restore overwrites the
   original database or content tree.
5. For M127/M128 sources, run the candidate's existing native migration owner
   for the exact missing suffix. A proved interrupted M127 → M128 prefix may
   continue only the missing M129 migration after full cold/data checks. For a
   same-schema M129 source, verify the full ledger/cold pair without invoking
   migrations. Verify original table facts, preserve configuration/credentials, start exact candidate writer
   images and verify native readiness. Record the signed target release only
   with the exact installed source sequence/package under the native lock.
6. Verify original database/content/configuration before opening receivers.
   Prepare new native membership, start/read back the receivers, then complete
   admission. Post-admission checks verify native release/runtime/admission and
   retained recovery archive identity. Legitimate new business writes do not
   invalidate the successful upgrade or cause migration/recording to repeat.

A legacy M128 cold dump with a nonempty observation uses PostgreSQL's native
pre-data/data/post-data restore stages on the private empty verification clone.
Between pre-data and COPY, the candidate's existing upgrade CLI verifies the
exact signed source, database/clone identity, retained dump and known function
bodies/configuration, then applies only the canonical function-local namespace
correction from M129. The receipt declares this DDL delta separately. The raw
dump, source database and full M128 clone ledger remain unchanged. No CHECK,
foreign key or trigger is disabled. The read-only `fresh-install check
--completed-source-schema 000128_upstream_release_observation` accepts only this
full corrected predecessor with its original completed Plan; it grants no
ordinary runtime, fresh apply or resume authority. Corrected M129 archives use
ordinary PostgreSQL restore and the existing financial/content rebase checks.

An interrupted operation uses the same host witness and each native outcome's
readback. A proved unstarted action can proceed; an unknown physical stop or
partial cold backup remains pending. Inspect status and preserve the files.
The entry never shortens graceful stop periods, blindly repeats an unknown
stop, resets a password, drops the verification database or automatically
restores/restarts the old release. Once M128 or new writes exist, an M127 restart
is not an automatic recovery path.

The old installation state bytes, source package, release history, stopped
runtime rows, paired backup and independent restore remain addressable.

## Evidence and implementation

The host entry is `deploy/customer/upgrade.py`; database decisions remain in
`uniroute upgrade maintenance-check|maintenance-retire`, `migrate up`,
`upgrade record-applied` and the original runtime-generation owner. The private
`recovery.py` extracts the already used byte/archive/SQL checks; it does not
certify a new schema or Target by itself. Narrow, code-owned M128 projections
exclude only the exact newly added schema rows, the M128 columns when the
source lacks them, and separately verified native appends. Existing M128/M129
observations and nonzero revisions stay in the full comparison. Other original tables, including financial and
immutable business history, stay in the comparison.

`scripts/test-community-upgrade.py` runs the real finite host entry against
synthetic Docker/SQL and archive transport fixtures: single/two writers,
check/status without mutation, each checkpoint, unknown stops, source drift,
lease waits, old-reader refusal and legitimate writes after admission. Actual
isolated PostgreSQL tests additionally execute native admission, atomic drift
refusal, dump/independent restore, original full-table fingerprints, M128 and
exact final predecessor recording. The host fixture is not real GNU tar,
Docker, Linux or Provider evidence.

This work creates no key, rotates no trust root and performs no model call.
Formal signed payload assembly adds `upgrade.py` and `recovery.py` under
`payload/deploy/` and reuses the already shipped restore owner at
`payload/scripts/source-budget-restore-rebase.sql`. No duplicate SQL payload is
introduced; all remain covered by the same full manifest.
