# UniRouter Changelog

All product releases follow Semantic Versioning (`MAJOR.MINOR.PATCH`). Deployment
artifacts additionally retain an immutable runtime version and exact Git commit;
those build identities do not replace the product version.

## [1.2.2] - 2026-08-24

### Fixed

- Removed all React `act(...)` and Semi Form validation-race warnings from the
  frontend unit suite by waiting for observable state, real user interactions,
  and file-owned Semi portal cleanup.
- Kept the custom provider template selected after an execution-Profile catalog
  failure by binding initialization to the SideSheet open transition and
  preserving unrelated fields when applying partial Semi Form updates.

### Added

- Added a Vitest stderr fingerprint ratchet: an unknown diagnostic or an
  increase in an approved fingerprint now fails the test command even when all
  assertions pass. Reporter output remains visible.
- Kept expected validation-boundary and explicit failure-path diagnostics in
  exact, independently capped fingerprints so one warning family cannot
  silently replace another.

### Compatibility boundary

- The exact React 18 / Semi UI 2.100.0 `findDOMNode` waiver remains visible and
  capped at ten occurrences. UniRouter-owned calls remain forbidden and React
  19 remains blocked until that third-party warning reaches zero.

## [1.2.1] - 2026-08-24

### Changed

- Enabled the reviewed React Router v7 transition and relative-splat behavior
  in production and shared test routers before the eventual router upgrade.
- Added a React compatibility guard that retains Strict Mode, rejects
  UniRouter-owned `findDOMNode` calls, and blocks React 19 while the exact Semi
  UI dependency waiver remains active.

### Fixed

- Covered absolute deep links, query/hash restoration and catch-all replace
  redirects under the future router behavior, while making affected async
  component tests wait for observable UI state instead of render counts.

### Compatibility boundary

- UniRouter source has no `findDOMNode` call. The visible React 18 warning from
  Semi UI 2.100.0 remains an exact, non-suppressed temporary waiver and must be
  removed before React 19; this release does not claim the upstream warning is
  fixed.

## [1.2.0] - 2026-08-24

### Added

- Reviewed provider/model onboarding templates that compose provider presets
  with an exact versioned Model Execution Profile.
- Safe defaults and constrained choices for public model identity, upstream
  model identity, context/output limits, and operation.
- A visible product version in the administration shell plus a CI guard that
  keeps `VERSION`, frontend package metadata, and this changelog aligned.

### Changed

- Renamed the primary model onboarding UI from the implementation-oriented
  “Canonical Deployment” wording to the user-facing “模型接入”; canonical
  deployment and operation identities remain visible as advanced details.
- Canonical model creation now pins the selected reviewed execution profile;
  omitted profile selection remains explicitly unverified rather than inferred.
- Updated the system fact map and codebase map for the current M83/Profile
  onboarding boundary.

### Fixed

- Kept model-onboarding SideSheets and nested page action groups inside narrow
  mobile viewports without changing their desktop widths.

### Verification scope

- Local backend/frontend contract tests, frontend type/build gates, and a
  bounded browser UI audit are required before this release entry is marked
  externally delivered.

> This is the first repository-governed product SemVer entry. It advances the
> existing deployed `1.1.62-*` runtime line without rewriting that historical
> build identity.
