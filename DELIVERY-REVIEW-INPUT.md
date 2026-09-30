# Independent pre-integration review input — UniRouter delivery 2026-09-26

Read-only review requested before copying release/migration/identity code into the delivery branch. Do not edit either source tree or this checkout.

## Exact object

- Delivery checkout `/Users/m4/.codex/worktrees/b424/unirouter`, branch `codex/ur-github-delivery-20260926`, HEAD `fe771d5c177f255523f290141acfc2809bb9cc31` after fast-forward from deployed `cd9f354b8a104a6f0677345a5694b19c000ab015`.
- Candidate source `/private/tmp/ur-data-actions-20260925/persistent-build/source`, committed snapshot HEAD `921d354f4ff9e3ea15f6171572cf11fa9687a360` and 48 worktree changes. It is byte-for-byte equal to the active 1.4.8 source working tree except 48 candidate changes and one excluded Python bytecode cache.
- Planned integration is exact copy of the **260** paths in `DELIVERY-REVIEW-MANIFEST.json` (SHA-256 `6bd2c109340ec8b99f7acd6d4a740774a868a7e9dc9aa7d5118a463aa50c9ca8`). 157 are new. All are Git-tracked or Git-unignored source files in the candidate. No deletes, no blanket `git add`, no secrets/DB backups/logs; exclude source A's `scripts/__pycache__/perf-content-http.cpython-314.pyc`.
- Triggered paths: M119 content capture, M120 unpublished content reclamation, M121 gateway pre-dispatch terminal migrations up/down; `internal/schemaauthority/version.go`, `internal/schemaauthority/compatibility.go`; `internal/migrate/{migrate,fresh_install}.go`; `cmd/uniroute/{compatible_expand_cli,fresh_install_cli}.go`; VERSION/CHANGELOG and frontend versions; content credential/identity and source-fact contracts.

## Milestone, invariants, and non-goals

Milestone: preserve all effective 1.4.8/M119-M120 and 1.5.0/M121 source as one reviewable GitHub candidate, then assess supported 1.2.77/M115 upgrade before any Target action. Existing daily UR keys, model routing, accounts, billing, historical data and license authority must survive. M115->M121 must use formal compatible-expand transitions with exact predecessor/ledger/digest and read-only check; no fresh-install overwrite of the daily DB. M121 current source declares `PreDispatchExpandHead` and explicit transitions, but local/Target qualification is pending. Candidate 1.5.0 version text is not proof of production readiness.

Non-goals: no deployment, no provider calls, no Target DB/secret writes, no K3 or DeepSeek side repair, no service shutdown or XI migration in this subtask. Old `/Users/m4/Desktop/projects/UniRouter` checkout has independent governance/research changes and is not an integration source for this runtime delivery unless separately justified.

## Review request

Classify as `validated_blocker`, `validated_required`, `valid_deferred`, `rejected`, or `product_decision_required`. Focus on whether any of the 260-path planned copies would silently break existing 1.2.77/M115 upgrade, license/recovery, public API, source identity/content credentials, irreversible migration handling, or provider/billing authority. Point to exact path/line and the smallest necessary fix. Source A and B are preserved; no coding of triggered areas has occurred yet.
