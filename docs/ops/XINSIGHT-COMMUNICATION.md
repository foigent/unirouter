# X-Insight communication controls

UniRouter 1.13.0 / M127 controls **UR-originated requests per connection**. XI
controls its own collectors. Pause each end locally for a quiet test connection;
one end cannot promise the other is quiet. Feed availability is a separate control.

New connections start in manual mode. Confirm and connect performs one finite
initialization burst; later **Sync once** uses the current installation and
revision. It admits at most 30 seconds of network work, 8 units and 24 dispatches,
including credential query/rotate/confirm and Poll/ACK. Local receipt persistence
may drain for a bounded additional interval. Partial/empty results do not prove XI
has caught up. A repeat of an unconfirmed request uses its original idempotency key.
New manual runs are refused while automatic mode is enabled; recorded receipts
remain readable/replayable.

Automatic mode uses a persisted interval (250–3,600,000 ms); new connections start
with 60 seconds. M127 preserves existing connections' automatic behavior and their
deployment's configured interval. UI Save interval makes the selected value explicit.
The interval is a minimum gap after completion; dispatch occurs on the existing
supervisor tick and does not promise an exact wall-clock schedule. During delivery, switch only the named test connection to manual on both ends.
Do not globally rewrite unrelated connections.

Pause prevents new units and new Poll/rotate/recover/status requests. An already
received operation or credential result still gets its truthful local journal and
only its exact required ACK/confirmation. The UI shows stopping until completion.
A dispatch deadline alone is **not** proof the worker exited. An abandoned slot
stays completion-unknown and blocks new work until the existing runtime-generation
owner proves that exact process stopped with ingress absent. A subsequent local
safety update reconciles it as interrupted. Resume through the authenticated local
UR UI/API; a silent UR cannot receive a queued XI wakeup.

Status GETs are local-only. Peer status is a bounded cached observation with its
observation time, not a current bilateral state. Local authorization, expiry,
retention, grants, cursors and setup advancement continue. Credentials may expire
while manual; recover through the existing explicit recovery flow rather than
silently regranting access.

## Upgrade and recovery

M127 is a maintenance boundary. In development/test, the existing
`runtime-generation-retire.py --maintenance` carrier accepts the exact M126→M127
predecessor using the same withdrawal, stopped-process and ingress proofs.
M125→M126 remains supported by its exact M126 artifact. Use the existing
**predecessor artifact's** runtime and ingress owners to withdraw and retire M126 writers, keep a restorable backup,
then use the new artifact's explicit migration owner. Ordinary runtime startup does
not apply M127. Its complete ledger and runtime support requirements reject an old
binary; do not bypass schema authority to roll back. The down migration deliberately
refuses to discard manual intent or run receipts. A rollback needs the exact
pre-M127 backup and stopped-writer proof.

Fresh installation uses the existing owner through M127, including interrupted
phase recovery and paired backup/restore rebasing. Previously accepted 1.12.0
images, sources and evidence retain their original identities. A 1.13.0 delivery
requires new application bytes and provenance; unchanged third-party source
archives and public trust material can be reused after hash verification.
