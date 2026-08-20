package runtimechange

import "context"

func (m *Manager) Validate(ctx context.Context, ref string, expectedVersion, actorUserID int64) (Release, error) {
	return Release{}, ErrLegacyRuntimeChangeRetired
}

func (m *Manager) AdvanceShadow(ctx context.Context, ref string, expectedVersion, actorUserID int64) (Release, error) {
	return Release{}, ErrLegacyRuntimeChangeRetired
}

// Reject is retained only as a stable fail-closed API for retired callers.
func (m *Manager) Reject(ctx context.Context, ref string, expectedVersion, actorUserID int64, reason string) (Release, error) {
	return Release{}, ErrLegacyRuntimeChangeRetired
}

// ActivateInitial is retained only as a stable fail-closed API for retired
// callers.
func (m *Manager) ActivateInitial(ctx context.Context, ref string, expectedVersion, actorUserID, customerOrgID int64) (Release, error) {
	return Release{}, ErrLegacyRuntimeChangeRetired
}

// ReplaceUnavailableStable is retained only as a stable fail-closed API for
// retired callers.
func (m *Manager) ReplaceUnavailableStable(
	ctx context.Context,
	candidateRef string,
	candidateExpectedVersion int64,
	retainedRef string,
	retainedExpectedVersion int64,
	actorUserID int64,
	customerOrgID int64,
) (Release, error) {
	return Release{}, ErrLegacyRuntimeChangeRetired
}

func (m *Manager) StartCanary(ctx context.Context, ref string, expectedVersion int64, basisPoints int, actorUserID int64) (Release, error) {
	return Release{}, ErrLegacyRuntimeChangeRetired
}

func (m *Manager) Promote(ctx context.Context, ref string, expectedVersion, actorUserID int64) (Release, error) {
	return Release{}, ErrLegacyRuntimeChangeRetired
}

func (m *Manager) Rollback(ctx context.Context, ref string, expectedVersion, actorUserID int64) (Release, error) {
	return Release{}, ErrLegacyRuntimeChangeRetired
}
