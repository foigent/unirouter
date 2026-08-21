package billing

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeProviderAttemptHealthFenceRejectsUnboundedOrUnsupportedInput(t *testing.T) {
	valid := ProviderAttemptHealthFenceRequest{
		OrgID: 1, RequestID: "request", AttemptID: "attempt",
		EvidenceHash:      "sha256:" + strings.Repeat("a", 64),
		DispatchOwnerHash: "sha256:" + strings.Repeat("b", 64),
		Outcome:           ProviderAttemptHealthSuccess,
	}
	_, err := normalizeProviderAttemptHealthFence(valid)
	require.NoError(t, err)

	invalidOwner := valid
	invalidOwner.DispatchOwnerHash = "sha256:not-a-digest"
	_, err = normalizeProviderAttemptHealthFence(invalidOwner)
	require.Error(t, err)

	unsupported := valid
	unsupported.Outcome = "retry"
	_, err = normalizeProviderAttemptHealthFence(unsupported)
	require.ErrorContains(t, err, "unsupported")
}

func TestNormalizeProviderAttemptOwnerRequiresBoundedExactDigests(t *testing.T) {
	valid := ProviderAttemptOwnerRequest{
		OrgID: 1, RequestID: "request", AttemptID: "attempt",
		EvidenceHash:      "sha256:" + strings.Repeat("a", 64),
		DispatchOwnerHash: "sha256:" + strings.Repeat("b", 64),
	}
	_, err := normalizeProviderAttemptOwner(valid)
	require.NoError(t, err)

	invalidEvidence := valid
	invalidEvidence.EvidenceHash = "sha256:" + strings.Repeat("A", 64)
	_, err = normalizeProviderAttemptOwner(invalidEvidence)
	require.Error(t, err)

	unboundedAttempt := valid
	unboundedAttempt.AttemptID = strings.Repeat("a", 65)
	_, err = normalizeProviderAttemptOwner(unboundedAttempt)
	require.Error(t, err)
}

func TestOwnedProviderAttemptRequiresVersionedEvidenceWithoutRemovingLegacyWrapperInput(t *testing.T) {
	legacy := providerAttemptFixture("request", "attempt", 1, 1, 1)
	_, err := normalizeProviderAttemptStart(legacy)
	require.NoError(t, err, "the historical wrapper must retain its double-nil legacy envelope")

	_, err = (&Billing{}).BeginProviderAttemptOwnedTx(
		t.Context(), nil, legacy, "sha256:"+strings.Repeat("a", 64),
	)
	require.ErrorContains(t, err, "requires versioned route evidence")
}

func TestProviderAttemptLeaseStateKeepsAcceptedStreamingBodiesRenewable(t *testing.T) {
	require.True(t, providerAttemptLeaseState(providerAttemptStateDispatchStarted))
	require.True(t, providerAttemptLeaseState(ProviderAttemptOutcomeResponseAccepted))
	for _, state := range []string{
		ProviderAttemptOutcomeRejected,
		ProviderAttemptOutcomeUnknown,
		ProviderAttemptOutcomeResponseUnusable,
		providerAttemptStateSettled,
	} {
		require.False(t, providerAttemptLeaseState(state), state)
	}
}

func TestReleasedUndispatchedCanOnlyBeClaimedBeforeProviderDispatch(t *testing.T) {
	require.True(t, providerAttemptMayClaimHealth(
		providerAttemptStateDispatchStarted, sql.NullString{}, ProviderAttemptHealthReleasedUndispatched,
	))
	require.False(t, providerAttemptMayClaimHealth(
		ProviderAttemptOutcomeResponseAccepted, sql.NullString{}, ProviderAttemptHealthReleasedUndispatched,
	))
	require.True(t, providerAttemptMayClaimHealth(
		ProviderAttemptOutcomeResponseAccepted, sql.NullString{}, ProviderAttemptHealthSuccess,
	))
	require.True(t, providerAttemptMayClaimHealth(
		ProviderAttemptOutcomeRejected, sql.NullString{Valid: true, String: "auth"}, ProviderAttemptHealthAuthFailure,
	))
}

func TestProviderAttemptHealthOutcomeRequiresItsExactBillingPrestate(t *testing.T) {
	tests := []struct {
		name        string
		state       string
		failureKind sql.NullString
		outcome     string
		allowed     bool
	}{
		{name: "success after accepted headers", state: ProviderAttemptOutcomeResponseAccepted, outcome: ProviderAttemptHealthSuccess, allowed: true},
		{name: "success before dispatch", state: providerAttemptStateDispatchStarted, outcome: ProviderAttemptHealthSuccess},
		{name: "success with failure metadata", state: ProviderAttemptOutcomeResponseAccepted, failureKind: sql.NullString{Valid: true, String: "channel_failure"}, outcome: ProviderAttemptHealthSuccess},
		{name: "transport channel failure", state: ProviderAttemptOutcomeUnknown, failureKind: sql.NullString{Valid: true, String: "channel_failure"}, outcome: ProviderAttemptHealthProviderFailure, allowed: true},
		{name: "rejected channel failure", state: ProviderAttemptOutcomeRejected, failureKind: sql.NullString{Valid: true, String: "channel_failure"}, outcome: ProviderAttemptHealthProviderFailure, allowed: true},
		{name: "unusable response channel failure", state: ProviderAttemptOutcomeResponseUnusable, failureKind: sql.NullString{Valid: true, String: "channel_failure"}, outcome: ProviderAttemptHealthProviderFailure, allowed: true},
		{name: "unusable response wrong kind", state: ProviderAttemptOutcomeResponseUnusable, failureKind: sql.NullString{Valid: true, String: "invalid_response"}, outcome: ProviderAttemptHealthProviderFailure},
		{name: "accepted malformed body", state: ProviderAttemptOutcomeResponseAccepted, outcome: ProviderAttemptHealthProviderFailure, allowed: true},
		{name: "unknown auth is not provider failure", state: ProviderAttemptOutcomeUnknown, failureKind: sql.NullString{Valid: true, String: "auth"}, outcome: ProviderAttemptHealthProviderFailure},
		{name: "auth exact", state: ProviderAttemptOutcomeRejected, failureKind: sql.NullString{Valid: true, String: "auth"}, outcome: ProviderAttemptHealthAuthFailure, allowed: true},
		{name: "auth wrong state", state: ProviderAttemptOutcomeUnknown, failureKind: sql.NullString{Valid: true, String: "auth"}, outcome: ProviderAttemptHealthAuthFailure},
		{name: "rate exact", state: ProviderAttemptOutcomeRejected, failureKind: sql.NullString{Valid: true, String: "rate_limit"}, outcome: ProviderAttemptHealthRateLimited, allowed: true},
		{name: "rate wrong kind", state: ProviderAttemptOutcomeRejected, failureKind: sql.NullString{Valid: true, String: "client_request"}, outcome: ProviderAttemptHealthRateLimited},
		{name: "release exact", state: providerAttemptStateDispatchStarted, outcome: ProviderAttemptHealthReleasedUndispatched, allowed: true},
		{name: "release after accepted", state: ProviderAttemptOutcomeResponseAccepted, outcome: ProviderAttemptHealthReleasedUndispatched},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.allowed, providerAttemptMayClaimHealth(test.state, test.failureKind, test.outcome))
		})
	}
}
