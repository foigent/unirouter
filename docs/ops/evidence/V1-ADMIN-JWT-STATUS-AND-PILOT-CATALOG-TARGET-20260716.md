# V1 Admin JWT Status And Pilot Catalog — Target Evidence

**Date:** 2026-07-16 (Asia/Shanghai)

**Environment:** development ECS test system at `http://8.130.18.85/`; this is
not Production or a production-equivalent financial environment

**Status:** Target verified / closed for `V1-ADMIN-STATUS-001`,
`V1-PILOT-CATALOG-001`, and the exact two-model configuration slice of
`V1-TARGET-ROUTING-001`. `V1-TARGET-PILOT-001` and supplier-bill
reconciliation remain open.

## Scope and immutable history

This evidence promotes the implementation checkpoint in
`V1-ADMIN-JWT-STATUS-AND-PILOT-CATALOG-LOCAL-20260716.md`; it does not rewrite
that earlier local-only history. It is Post-PRD6 V1 release work. PRD6 remains
`Completed (Accepted Scope)` with 27/27 accepted rows closed.

The approved target operation was limited to:

1. formally releasing PR #214 to the development ECS;
2. proving an already-issued administrator JWT is denied after that exact
   principal is disabled;
3. configuring the first pilot to expose/admit only `mimo-v2.5-pro` and
   `DeepSeek-V4-Flash` without deleting internal model records;
4. proving an excluded Kimi request is rejected before provider dispatch and
   customer-ledger mutation; and
5. disabling all temporary users and keys.

## Repository and required CI

- PR #214 exact head:
  `4cf13761edbdc50163cbade1151051a74d3d42dc`.
- Run `29441931266` passed Change Routing and the three required checks:
  Backend Tests, Frontend Build, and Docker Build.
- Squash merge / repository release commit:
  `46b49b60835225a0b32b641615e6afe4a8a9696b`.
- The PR head and merge commit have the same tree
  `7fdf8b435aad322335b050f63188757be4bcf028`.
- No automatic post-merge main run was created; this is the intended
  trusted-dispatch/docs-routing CI design, not missing verification.

## Formal development-target release

The cutover/acceptance window ran from the pre-cutover backup at approximately
`2026-07-15T19:11:00Z` through the final public health recheck at
`2026-07-15T20:17:58Z` (`2026-07-16T04:17:58+08:00`). Sequence `4` was recorded
at `2026-07-15T19:15:30Z`. Exact ECS command bodies and timestamps remain in the
named invocation logs; secret-bearing command material is not copied into this
repository evidence.

The signed release was applied from
`/opt/unirouter/releases/46b49b60835225a0b32b641615e6afe4a8a9696b`.

| Component | Exact target authority |
|---|---|
| stable/canary backend | `sha256:60b2f5620f815eff3e75716c7f20516dda49c611cfa8202df37ae98643634e7f` |
| frontend | `sha256:7a3c8eca48309e6743386bb2d24c1f66fb7838c2d57970fdba34876d72e36993` |
| Nginx | `sha256:65645c7bb6a0661892a8b03b89d0743208a18dd2f3f17a54ef4b76fb8e2f2a10` |
| schema | `000037_v1_commercial_release_control` |
| trust / License | `license_keys=1`, `upgrade_keys=1`, active enforceable test License |
| signed upgrade | version `1.0.0`, commit `46b49b6`, sequence `4`, key `test-upgrade-b`; replay rejected |

The signed package is
`/opt/unirouter/incoming/unirouter-1.0.0-ecs-46b49b6-seq4.tar.gz`, size 8164
bytes, SHA-256
`842b2fc3d74d25c308c2e03c91edebc1360842895a4c0fd309244e0e20383883`.
Its signing private key remained local and was neither uploaded nor printed.

The pre-cutover PostgreSQL backup is
`/opt/unirouter/backups/uniroute-before-46b49b6-20260715T191100Z.dump`, mode
`0600`, size 315150 bytes; its checksum was recorded in the target invocation
and `pg_restore -l` passed. Stable/canary readiness passed with zero restarts,
the frontend and Nginx were running with zero restarts, and public
`/health/ready` returned HTTP 200 with DB and Redis healthy.

Operational invocations were recorded separately by the target operator:
preverification/backup `t-wl06r0ukezpuwao`, canary
`t-wl06r0umojzi6f4`, stable `t-wl06r0up7sbzz7k`, frontend/Nginx
`t-wl06r0usmitdzwg`, and sequence-4 record `t-wl06r0uw23ojpj4`.

## Exact pilot configuration readback

The final target database readback was:

```text
approved_models|DeepSeek-V4-Flash,mimo-v2.5-pro|2
pilot_set|v1-pilot-20260716:active|2
pilot_items|DeepSeek-V4-Flash,mimo-v2.5-pro|2
project_binding|1|1
kimi|disabled|4
temp_users|2|0
temp_keys|2|0
negative_rows|0|0
schema|000037_v1_commercial_release_control|1
```

Model set #1 is `V1 Pilot 20260716` / `v1-pilot-20260716`, active, with the
two exact items above. Project #1 has exactly that one model-set binding.
`approved_catalog` is true only for the two pilot models. Kimi channel #6 is
disabled at routing epoch 4; it was not deleted, retired, or declared broken.

Stable and canary `/v1/models` each returned exactly the two pilot identities.
Stable and canary routing preview selected channel #5 for DeepSeek Flash and
channel #7 for Mimo Pro. Kimi was excluded as `channel_inactive`.

The successful acceptance invocation was `t-wl06r0xy2ytst8g`; final database
readback was `t-wl06r0yugvx42rk`.

## Authentication and negative-request proof

A temporary platform administrator's already-issued JWT returned HTTP 200 from
the protected current-user endpoint while the user was active. After the exact
same user was disabled, the same unexpired JWT returned HTTP 401. Invocation:
`t-wl06r0yktbxpw5c`.

This proves request-time principal-state rejection. It is not a token blacklist
or cryptographic/per-session revocation: restoring the exact user/org/role and
active state before expiry can make the old token usable again.

One excluded request for `moonshot-v1-8k`, operation
`op_163bb39294d192e19e664d7912111512`, returned HTTP 403
`auth_error.model_not_allowed` with reason `project_model_not_allowed`.
Readback found zero call logs, freezes, provider attempts, transactions and
internal-outbox rows for that operation; wallet balance, granted, frozen and
spent fields were unchanged. This proves no UniRouter dispatch and no
UniRouter customer-ledger charge for this request. It does not claim a supplier
billing-console observation.

Temporary API key #44 was disabled and replay returned HTTP 401. Temporary
administrator #17 was disabled. The final readback found both temporary users
inactive and both temporary keys inactive; records were retained for audit
rather than destructively deleted.

## Real browser evidence

An authenticated public-browser session on the deployed frontend verified:

- the `开发环境` label and platform-admin identity;
- Model Governance "仅看已批准" showing exactly the two pilot rows;
- model set `V1 Pilot 20260716`, its two exact items, and Default Project's
  assignment;
- channels #5/#7 active and channel #6 Kimi disabled;
- routing preview selecting DeepSeek channel #5 and excluding Kimi with
  `通道未启用`;
- Access Credentials explaining one-time full-key display, showing a created-
  time column, and showing temporary keys disabled; and
- logout returning to `/login` with no browser-console errors observed.

The credential handoff from ECS into the browser-automation runtime used RSA
ciphertext, and plaintext was not printed. The actual login POST still crossed
the target's public HTTP endpoint without TLS; only a temporary credential was
used, it was disabled immediately after verification, the tab was closed, and
the temporary credential/key-pair objects were cleared. This is not transport-
security or Production evidence.

## Recoveries and final-state authority

Three non-final attempts were safely contained:

1. a build timed out at `proxy.golang.org` and was terminated before image
   promotion; the successful build used the approved alternate Go proxy;
2. a first validation used the wrong `/api/v1/auth/me` path and rolled back
   before target acceptance; and
3. a temporary key's loopback-only IP restriction did not match the actual
   auth address, so that attempt rolled back. The supported API could not
   remove its empty model set, so the final run reused that exact set and made
   it the desired two-item target instead of creating an orphan.

Final DB/API/container/browser readbacks above, not intermediate attempt state,
are the acceptance authority.

## Supplier billing boundary — not verified

No provider billing console, finalized billing API/export, invoice, or isolated
account-balance delta was inspected. Existing positive-call evidence proves
provider-returned usage/status and UniRouter's customer-ledger
freeze/settlement/conservation only. Public list prices do not prove actual
supplier cost because free credit, negotiated discount, cache classification,
minimum billing, rounding, tax, currency conversion and billing delay may
differ.

Therefore:

- the cumulative 810007 micro-CNY historical UniRouter charge is not a supplier
  COGS figure;
- the DeepSeek 7-micro-CNY pass-through result is dated internal-price
  alignment, not supplier-invoice reconciliation; and
- `V1-PROVIDER-BILL-RECON-001` remains a P1 Production/external-evidence gap.

Closing that issue requires a separately approved bounded request/window and
an authoritative provider billing export or supervised console readback,
followed by a side-by-side customer charge versus actual supplier cost
reconciliation including model, token classes, discounts, tax, currency,
rounding and unexplained delta.

## Remaining non-claims

This evidence does not prove capacity/load, external detector behavior,
enterprise IdP, customer backup/restore/upgrade execution, Production/TLS/HA,
long-running reliability, broader Kimi/DeepSeek Pro/provider readiness,
X-Insight v2 consumer acceptance, permanent JWT revocation, or supplier-bill
agreement. Those gates remain open and do not reopen PRD6.
