# UniRouter Changelog

All product releases follow Semantic Versioning (`MAJOR.MINOR.PATCH`). Deployment
artifacts additionally retain an immutable runtime version and exact Git commit;
those build identities do not replace the product version.

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
