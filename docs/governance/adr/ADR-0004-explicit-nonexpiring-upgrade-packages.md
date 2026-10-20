# ADR 0004: Explicit non-expiring upgrade packages

> Status: Proposed (local implementation; release acceptance pending)
> Date: 2026-10-20
> Owners: UniRouter maintainers
> Related: [wire contract](../../tech-ref/27-signed-offline-upgrade.md),
> [installer runbook](../../ops/COMMUNITY-INSTALLER.md)

## Context

The owner selected packages without an expiry deadline. Existing signed v1
manifests require an RFC3339 expiry; the backend also verifies the package when
recording a completed installation. Changing only the standalone reader would
leave installation unable to record the new package.

## Decision

Add explicit `pack --no-expiry`, producing signed v2 with exactly
`expires_at: "never"`. Keep the manifest layout and Ed25519 signature envelope.
Finite v1 remains unchanged, including canonical bytes and expiry at the exact
deadline. No omitted/default/far-future lifetime or global time-check bypass.
Keep real creation time and the existing five-minute tolerance, trust,
sequence, schema, platform, provenance and payload checks. Use the same native
verifier in bootstrap/check/install/resume and a v2-capable backend for release
recording. M127 and existing JSONB storage need no migration.

## Consequences and alternatives

Old readers reject unsupported v2; build new backend and verifier artifacts.
The existing consistent-version producer also builds the new 1.14.0 frontend.
Retain old packages/images and original installation-plan hash binding.
Package lifetime does not extend the finite authorization to sign. No online
renewal service is added. Reusing v1 with a new non-date token was rejected to
preserve v1's published grammar; adding another lifetime object/serializer was
unnecessary for this scope.

## Verification

Require finite byte compatibility against the old verifier, old-reader refusal,
far-future v2 acceptance, invalid/tampered policy refusal, real transactional
record-applied tests and original bootstrap/install/resume identity checks.
Local results cannot substitute new-source CI, artifacts, public distribution
or Target acceptance. The prepared owner/consumer inputs remain unbound until
actual new artifacts exist.

## Rollback or exit criteria

Retain v1 reading. Supersede this ADR explicitly if a future format replaces v2;
never rewrite historical signed manifests or reuse their release identities.
Do not downgrade a deployment that must read a recorded v2 package to an old
reader without an independently approved compatible recovery plan.
