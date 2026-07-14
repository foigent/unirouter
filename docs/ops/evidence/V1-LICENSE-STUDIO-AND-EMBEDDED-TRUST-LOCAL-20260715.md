# V1 License Studio And Embedded Release Trust — Local Evidence

**Date:** 2026-07-15 (Asia/Shanghai)

**Scope:** Post-PRD6 V1 commercial-release hardening; PRD6 remains Completed
(Accepted Scope) and is not reopened

**Baseline:** `origin/main` `e94a4b184a6f675d76c292602b7c18293ab2ce61`

**Branch:** `codex/v1-commercial-license-upgrade`

**PR:** #205 open; current exact-head CI pending when this local evidence was written

**Evidence level:** Local verified only

## Outcome

1. The release trust-root bypass found after the original PR #205 implementation
   is closed locally: formal backend binaries accept only separately embedded
   License and upgrade public-key rings. A runtime environment cannot replace
   them; any `LICENSE_PUBLIC_KEY`, `LICENSE_PUBLIC_KEYS` or
   `UPGRADE_PUBLIC_KEYS` override makes a release binary fail startup.
2. `deploy/customer` no longer accepts either public ring from customer `.env`.
   The source Compose explicitly builds in `development` trust mode and remains
   non-release-eligible.
3. A vendor-only, double-click macOS `UniRouter License Studio.app` now covers
   separate License/upgrade key generation, customer License form signing and
   signature/deployment/time verification. The normal operator does not need
   Terminal.
4. No persistent production/customer private key, public trust root or License
   was created. No ECS image, environment, database, migration, deployment ID,
   License or release state was changed.

## Issue Matrix

| ID | Classification | Impact and cause | Priority | Owner | Resolution / acceptance | Status |
|----|----------------|------------------|----------|-------|-------------------------|--------|
| V1-LIC-TRUST-001 | confirmed bug/regression | Customer-controlled Compose supplied `LICENSE_PUBLIC_KEYS`; a host operator could substitute a self-generated public key and self-sign unrestricted authority without patching the binary | P0 | UniRouter release/runtime | `internal/releasetrust` selects immutable build variables; `cmd/uniroute` resolves trust before any server/upgrade command; release mode rejects all runtime roots; customer Compose/env removed the variables; release build fails when either embedded ring is absent | Local verified; CI pending |
| V1-UPG-TRUST-001 | confirmed bug/regression | `UPGRADE_PUBLIC_KEYS` had the same replaceable runtime-root weakness and could make installed upgrade verification trust customer-selected packages | P0 | UniRouter release/runtime | Separate upgrade ring is embedded with the License ring and uses the same no-runtime-override contract | Local verified; CI pending |
| V1-LIC-UX-001 | UI/UX improvement | Vendor issuance existed only as CLI flags and payload JSON, which was error-prone for daily commercial operations | P1 | UniRouter vendor tooling | License Studio provides native macOS folder/file pickers, typed form fields, supported feature/limit choices, automatic IDs/dates, no-overwrite writes, self-verification and a separate verify screen | Local/browser verified; CI pending |
| V1-LIC-CUSTODY-001 | target environment configuration | A GUI cannot make an everyday Full-Disk-Access Mac an approved production key ceremony; private-key custody is organizational and physical, not a UI property | P0 delivery gate | User / release owner | The app warns the operator to quit ChatGPT/Codex and similar apps, use an encrypted external volume or dedicated offline Mac, keep two encrypted offline backups, and generate License/upgrade keys separately | No real key generated; decision remains external |
| V1-LIC-FLOW-001 | misunderstanding or no customer change | Customers do not generate vendor Licenses. They receive one signed JSON file and import it into the existing commercial-authority page | P1 documentation | UniRouter product/ops | README, technical reference, deployment runbook and customer README distinguish vendor signing from customer import | Documented |
| V1-LIC-DIST-001 | target environment configuration | The local `.app` is ad-hoc signed and is appropriate for local use, but public distribution would still trigger normal Apple trust requirements | P1 before external distribution | Release owner | Build script supports a `CODESIGN_IDENTITY`; external delivery requires Developer ID signing and notarization | Not claimed / future release operation |

## Trust-Root Controls

- `Dockerfile` defaults `UNIROUTER_RELEASE_TRUST_MODE=release` and refuses a
  release build unless both public ring build values are non-empty.
- Release public JSON rings are base64url-encoded only to survive `-ldflags`;
  they are public material, not secrets.
- The build embeds mode and rings into `internal/releasetrust` string variables.
  There is no runtime environment lookup for those build variables.
- `releasetrust.Resolve` copies canonical parsed rings into the existing
  verifier path. Development retains explicit runtime rings for source tests;
  the customer Compose does not.
- The CLI-only standalone pre-install verifier can still take an explicitly
  bootstrapped public file. Installed `uniroute upgrade verify/record-applied`
  receives the embedded ring through the main release-trust resolver.

This does not claim that software can defeat a customer who patches the binary
or rebuilds from source. The control prevents the materially easier configuration
replacement path and relies on source-free digest-pinned signed releases,
contract enforcement and custody for the broader commercial boundary.

## License Studio Security Boundary

- Listens only on a random `127.0.0.1` port and random 256-bit session path.
- Normal double-click launch opens the browser without printing the session URL
  or token; only explicit developer `-no-open` mode prints it for browser QA.
- Requires exact loopback remote address, exact Host, same Origin, JSON content
  type and session header for every state-changing request.
- Sends CSP, frame, MIME, referrer, cross-origin and no-store headers.
- Embedded page has no external assets or network dependency. Playwright saw
  only page/CSS/JS loopback requests.
- macOS native dialogs choose directories/private/public/License files. Private
  bytes never enter browser JavaScript, browser storage, URL, HTTP response,
  database or application log.
- Private input and decoded key slices are cleared after signing on a best-effort
  basis; files are size-bounded and private/new License files use `0600`.
- Key IDs are filename-safe, outputs use `O_EXCL`, existing files are never
  overwritten, and a generated document is cryptographically self-verified
  before it is written.
- Idle sessions stop after two hours; the UI also has an explicit safe-exit
  action.

## Local Verification

### Focused and full Go

```text
go test ./internal/releasetrust ./internal/licensestudio \
  ./cmd/uniroute-license-studio ./cmd/uniroute -count=1             PASS
go vet ./internal/signing ./internal/releasetrust \
  ./internal/licensestudio ./cmd/uniroute-license-studio ./cmd/uniroute PASS
go test ./internal/licensestudio ./internal/releasetrust \
  ./internal/signing ./internal/licensing ./cmd/uniroute \
  ./cmd/uniroute-license ./cmd/uniroute-license-studio \
  ./cmd/uniroute-upgrade -race -count=1                            PASS
go test -p 1 ./... -count=1                                       PASS
go vet ./... && go build ./...                                    PASS
```

The first unconstrained parallel `go test ./... -count=1` run completed every
package except one `internal/billing` cleanup: the business test reached cleanup
but a temporary PostgreSQL database exceeded its cleanup timeout. The exact test
then passed in 1.981 s, the complete billing package passed in 19.600 s, the
serial full suite passed, and the one exact leftover zero-session temporary
database was dropped by exact name. Final `uniroute_it_%` database count: `0`.
No test was silently reported as a first-run pass.

### Frontend and static validation

```text
npm test -- --run                    31 files / 164 tests PASS
npm run type-check                   PASS
npm run build                        PASS (existing chunk-size warning only)
node --check .../assets/app.js       PASS
Ruby YAML parse of CI/root/customer Compose PASS
plutil -lint Info.plist              PASS
git diff --check                     PASS
```

### macOS package and release-build path

- `scripts/build-license-studio-macos.sh`: produced an arm64 `.app` without
  overwriting an existing bundle.
- `codesign --verify --strict --verbose=2`: `valid on disk` and `satisfies its
  Designated Requirement` under local ad-hoc signing.
- Embedded release `go build -ldflags` with separate CI-only public rings:
  `PASS`; the temporary executable was deleted by exact path.
- Local app binary SHA-256:
  `be3260e3af171aa97cc2e085ceaaff19ad533ac5e39a6d93a684e75f21b28469`
  (5,815,040 bytes, arm64). This ignored local artifact is not a notarized
  customer release.
- Local Docker is unavailable, so the real Dockerfile release build and
  customer Compose rendering remain required CI evidence.

### Playwright real browser

- Headed Chrome desktop: page/title, three tabs, 2026-07-15 local default dates,
  random License ID, key-purpose ID switch and all-features/single-feature state
  verified.
- 390 x 844 mobile: responsive form/card/footer render verified.
- Console: 0 errors, 0 warnings after adding the embedded favicon.
- Requests: exactly three static `GET` requests, all to the random
  `http://127.0.0.1:<port>/<session>/` origin.
- Local screenshots (ignored, not committed):
  `output/playwright/license-studio-desktop.png` SHA-256
  `16a758d357d8d0f7a50f1019633b2e3e7ae581fe7d30c5824c95224c0d85bc64`;
  `output/playwright/license-studio-mobile.png` SHA-256
  `ac99b6455c64820c1472ed04c8f4fea4c288593d56e0e19970f4e47fdf2921ea`.

The browser did not drive a real production ceremony. HTTP/API tests used
temporary test keys and fake native-dialog paths to prove the complete
keygen -> issue -> verify flow, overwrite refusal, malformed key,
wrong-deployment, cancellation, host/origin/token and header cases.

## Remaining Gates

1. Commit/push the current head and require PR #205 Backend Tests, Frontend
   Build, Docker Build and Change Routing to pass on that exact head.
2. User decides whether to merge PR #205. This evidence does not authorize it.
3. Before the first formal customer release, run a separate production key
   ceremony outside this AI session, record public fingerprints and configure
   protected release-build values. Never put either private key in GitHub/ECS.
4. Before public distribution of License Studio, sign/notarize it with the
   company's Apple Developer ID.
5. ECS remains development/test. Deploying embedded test roots, migration
   `000037` and a non-production License is a separate R4 action after merge.
