package xinsightconnector

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNormalizeConsentBuildsStableProjectSourceScope(t *testing.T) {
	input := Consent{
		InstallationID:   "xci_AbCdEfGhIjKlMnOpQrStUv",
		XDeploymentID:    "xid_ZyXwVuTsRqPoNmLkJiHgFe",
		InvitationSHA256: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		OrgID:            7, ProjectID: 11,
		EventScope:        EventScopeModelCompleted,
		IdempotencyKey:    "CONNECT.A",
		ConsentedByUserID: 13,
	}
	normalized, binding, digest, err := normalizeConsent(input, "source.instance.v1")
	require.NoError(t, err)
	require.Equal(t, input.InstallationID, normalized.InstallationID)
	require.Equal(t, input.XDeploymentID, normalized.XDeploymentID)
	require.Equal(t, "source.instance.v1", binding.SourceInstance)
	require.Equal(t, SourceBindingSchemaVersion, binding.SchemaVersion)
	require.Equal(t, CompletedFeedSchemaVersion, binding.FeedSchema)
	require.Equal(t, "sha256:e0c733b7c2a239b68edbbf12886ef223435e1913786995e8b78f0a6d0701a66a", digest)

	_, _, repeated, err := normalizeConsent(normalized, "source.instance.v1")
	require.NoError(t, err)
	require.Equal(t, digest, repeated)

	caseAlias := input
	caseAlias.InstallationID = "xci_abcdefghijklmnopqrstuv"
	_, _, caseAliasDigest, err := normalizeConsent(caseAlias, "source.instance.v1")
	require.NoError(t, err)
	require.NotEqual(t, digest, caseAliasDigest, "X management identities are exact bytes, not case aliases")
}

func TestConsentAndOperationContractsFailClosed(t *testing.T) {
	base := Consent{
		InstallationID: "xci_AbCdEfGhIjKlMnOpQrStUv", XDeploymentID: "xid_ZyXwVuTsRqPoNmLkJiHgFe",
		InvitationSHA256: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		OrgID:            1, ProjectID: 2, EventScope: EventScopeModelCompleted,
		IdempotencyKey: "connect-a", ConsentedByUserID: 3,
	}
	tests := []struct {
		name   string
		mutate func(*Consent)
	}{
		{"unknown event scope", func(value *Consent) { value.EventScope = "billing.execution.settled" }},
		{"unbound project", func(value *Consent) { value.ProjectID = 0 }},
		{"invalid invitation hash", func(value *Consent) { value.InvitationSHA256 = "operator-assertion" }},
		{"normalized installation identity", func(value *Consent) { value.InstallationID = " " + value.InstallationID }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := base
			test.mutate(&input)
			_, _, _, err := normalizeConsent(input, "source-a")
			require.ErrorIs(t, err, ErrInvalid)
		})
	}
	for _, sourceInstance := range []string{"", "source\ncontrol", string([]byte{0xff, 0xfe})} {
		_, _, _, err := normalizeConsent(base, sourceInstance)
		require.ErrorIs(t, err, ErrInvalid)
	}

	_, _, err := validateOperationRequest(OperationRequest{
		InstallationID: "xci_AbCdEfGhIjKlMnOpQrStUv", OrgID: 1, ProjectID: 2,
		Action: "reset", IdempotencyKey: "reset-a", RequestedByUserID: 3,
	})
	require.ErrorIs(t, err, ErrInvalid)

	_, err = validateOutcome(ActionReplay, OperationOutcome{
		ObservedState: ObservedConnected,
		ResultCode:    "raw upstream error: timeout",
	})
	require.ErrorIs(t, err, ErrInvalid)
}

func TestProductStatusRequiresExactLiveFeedState(t *testing.T) {
	now := time.Now().UTC()
	installation := &Installation{ObservedState: ObservedConnected, DesiredState: DesiredActive}
	health := ProjectHealth{FeedConnectionStatus: "active", ActiveCredentialCount: 1}
	require.Equal(t, ProductConnected, productStatus(installation, health, true))
	require.Equal(t, ProductAbnormal, productStatus(installation, health, false), "disabled or drifted Source authority fails closed")

	health.CursorOutsideRetention = true
	require.Equal(t, ProductAbnormal, productStatus(installation, health, true))
	health.CursorOutsideRetention = false
	installation.LastErrorCode = "bootstrap_timeout"
	installation.LastErrorAt = &now
	require.Equal(t, ProductAbnormal, productStatus(installation, health, true))
	installation.LastErrorCode = ""
	installation.LastErrorAt = nil
	health.FeedConnectionStatus = "paused"
	require.Equal(t, ProductPaused, productStatus(installation, health, true))
	require.Equal(t, ProductNotConnected, productStatus(nil, health, false))
}

func TestValidateOutcomeChronologyAndMachineFields(t *testing.T) {
	when := time.Date(2030, 1, 2, 3, 4, 5, 123456789, time.FixedZone("test", 8*60*60))
	validated, err := validateOutcome(ActionConnect, OperationOutcome{
		ObservedState:          ObservedConnected,
		ResultCode:             "bootstrap_complete",
		ResultSHA256:           "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		BootstrapReceiptSHA256: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		SuccessfulSyncAt:       &when,
	})
	require.NoError(t, err)
	require.Equal(t, time.UTC, validated.SuccessfulSyncAt.Location())
	require.Equal(t, 123456000, validated.SuccessfulSyncAt.Nanosecond())

	_, err = validateOutcome(ActionPause, OperationOutcome{ObservedState: ObservedConnected, ResultCode: "paused"})
	require.True(t, errors.Is(err, ErrInvalid))
}
