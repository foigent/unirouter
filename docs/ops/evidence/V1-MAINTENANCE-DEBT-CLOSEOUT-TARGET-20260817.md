# V1 Maintenance Debt Closeout and Target Evidence — 2026-08-17

## Scope and evidence boundary

This record closes the maintenance batch requested for GitHub Actions runtime
warnings, model-endpoint maintainability, build reuse, Compose warnings, and
release-worktree lifecycle reporting. It records separate Local, CI, GitHub,
Artifact, and development/test Target evidence. It is not Production evidence.

The code-bearing squash merge is
`dbd61156d804b340843bf3521f98cddcea75efe6` (PR #300), with tree
`7eb3b7876e37b0840b34fe607ac5974f670d00eb`. The subsequent Dependabot-only
configuration merge is `a23e2298ef5459a4cc5b0c68b25812b5739e0a9e`
(PR #301). The development/test Target intentionally runs the last
code-bearing merge, not the later repository-only automation configuration.

## Implemented changes

1. GitHub-owned workflow actions were upgraded to their Node 24 generation:
   `checkout@v7`, `setup-go@v7`, `setup-node@v7`, cache restore/save `@v6`,
   and `upload-artifact@v7`. `create-github-app-token` was pinned to v3.2.0
   and its deprecated `app-id` input was replaced with `client-id`.
2. Weekly grouped Dependabot maintenance was added for public GitHub Actions.
   The private cross-repository protocol action is explicitly excluded because
   Dependabot cannot read it without widening organization-level private
   repository permission. Its immutable pin remains governed by the dedicated
   protocol conformance workflow. No permission expansion was applied.
3. The unused compatibility `ChannelsPage` and its test were deleted.
   `ModelEndpointsPage.tsx` was reduced from 721 to 401 lines by extracting
   endpoint DTO/API transport, presentation helpers, and create/details sheets.
4. `model_endpoint_handlers.go` was reduced from 824 to 485 lines by moving
   endpoint projection into `model_endpoint_projection.go`.
   `model_governance_handlers.go` was reduced from 796 to 432 lines by
   separating catalog and helper responsibilities.
5. The endpoint client now has typed request/response contracts. The shared
   `frontend/src/api/client.ts` fell from 791 to 688 lines. The rest of that
   client is still a handwritten generic transport surface and is not claimed
   as fully generated or fully typed.
6. Projection tracing confirmed Dashboard already consumes the canonical
   endpoint projection and the Readiness route axis already delegates to the
   gateway route preview. Remaining Readiness access, pricing, and policy read
   models are still separate projections and remain follow-up debt.
7. Docker builds now use persistent BuildKit Go module and build caches. A
   frontend-only source change still rebuilds the backend binary because the
   binary embeds the exact source revision; this truthful revision binding was
   retained. The cache reduces the rebuild cost without introducing mixed-
   revision artifacts.
8. The obsolete root Compose `version` field was removed.
9. Release-worktree lifecycle report and maintenance scripts were added with a
   fail-closed N/N-1/N-2 policy, dry-run default, exact canonical-directory
   checks, dirty/attached-worktree protection, and explicit apply control.

## Local and GitHub verification

- Local Go gate: `go test ./... -count=1` passed.
- Frontend: type-check passed; 69 test files / 508 tests passed.
- Local production frontend build passed in 12.62 seconds total; Vite itself
  took 6.89 seconds and the bundle budget passed.
- Release-worktree script tests, YAML parsing, Bash syntax, `gofmt`, and
  `git diff --check` passed.
- PR #300 passed Change Routing, Backend, Frontend, Browser E2E, Docker, and
  protocol conformance. All six check annotation lists were empty.
- The post-merge protocol workflow on `main` passed with zero annotations.
- PR #301 passed all five routed CI checks with zero annotations.
- The first Dependabot run exposed the expected private-action access boundary.
  After the official path exclusion was merged, run `32025987242` passed with
  zero annotations. The private action was not silently converted to a mutable
  or public dependency.

## Development/test Target deployment

Target: `https://8.130.18.85/` (development/test, not Production).

- Incremental Git bundle:
  `/opt/unirouter/bundles/unirouter-dbd61156d804b340843bf3521f98cddcea75efe6.bundle`
  (40,214 bytes), SHA-256
  `25bf4533af1b2c75290198de6bcd5cef4a3cf230d57f4fdb93730e31b1334585`.
  Local and Target bundle verification passed.
- Clean detached worktree:
  `/opt/unirouter/releases/dbd61156d804b340843bf3521f98cddcea75efe6`,
  exact tree `7eb3b7876e37b0840b34fe607ac5974f670d00eb`.
- No migration delta existed from the predecessor release. Schema remained at
  65 applied migrations, maximum `000066_runtime_assignment_projection`.
- Built version: `1.1.30-maintenance`. Backend build step took 75.7 seconds;
  frontend production build took 33.1 seconds (Vite 14.58 seconds); the complete
  helper took 123.91 seconds. The existing frontend `npm ci` layer was cached.
- Immutable images:
  backend `sha256:60b81a4ee348ccfb8869fe3f8ec09266c24d45c2de06c631b2aff5899330796f`;
  frontend `sha256:414ae703f3b91080a82abce45b7217ee32a15b19c8759e6a75d55a5829df9c9a`.
- Compose configuration for root, canary, TLS, and immutable overlays passed
  without the deprecated-version warning.
- Pre-switch backup:
  `/opt/unirouter/backups/pre-maintenance-dbd61156-20260817T112957Z.dump`,
  mode `0600`, 690,450 bytes, SHA-256
  `432df531c5b4b2d5c5436ba0979f35849dd408c31429916b6486174b20e7ef6e`;
  `pg_restore -l` passed.
- The first canary command omitted the existing Compose project name. Docker
  rejected the new network before creating a container because of an address
  overlap. The existing canary remained healthy. The retry used the explicit
  `unirouter` project name and succeeded.
- Stable, canary, and frontend run the expected immutable images. Stable and
  canary report revision `dbd61156d804`, version `1.1.30-maintenance`, healthy,
  with zero restarts.
- Public HTTP redirects to HTTPS; public HTTPS root and readiness return 200;
  database and Redis are ready. Direct stable/canary readiness also passed.
- Core entity counts remained `1|2|1|2|2|2|2|2` for organizations, users,
  projects, channels, channel keys, model registry, runtime releases, and
  runtime slots. No customer data reset, migration, credential change, provider
  call, financial action, or traffic-policy change occurred.
- Runtime status in the authenticated browser shows
  `1.1.30-maintenance · dbd61156d804`, and the browser console contained no
  errors or warnings. The Model Endpoints page showed two public models, two
  routable/stable endpoints, and zero items needing action.

## Lifecycle reports and deletion boundary

The installed scripts are under `/usr/local/libexec/unirouter`.

- Image dry-run: running protected 7, explicit keep 8, young 38, stopped 0,
  unmanaged 0, deletion candidates 0. No apply occurred.
- Release-worktree dry-run: deletion candidates 5 (146.1 MiB), running
  protected 1, explicit keep 2, young 27, review-required 32. `APPLY` remained
  false. The review-required entries are intentionally fail-closed because they
  are legacy, noncanonical, attached, or dirty.

No release worktree was deleted. Deleting the five exact candidates is a
separate destructive operation and requires a separately reviewed target list
and authorization.

## Honest remaining debt

This batch does not claim all technical debt is zero:

- The shared frontend API client still contains 688 lines of handwritten,
  partly generic interfaces outside the newly typed model-endpoint surface.
- Readiness access, pricing, and policy projections remain independently
  assembled even though Dashboard and route preview now use canonical sources.
- The backend must continue to rebuild when creating a new exact-revision image.
  Removing that constraint would require a product-level artifact/version
  contract, not a build-script shortcut.
- Thirty-two Target release directories require human classification before
  any deletion; five canonical candidates are eligible but were not deleted.

These are bounded P1/P2 follow-ups. None is an open P0 for the verified model
call path or this deployment.
