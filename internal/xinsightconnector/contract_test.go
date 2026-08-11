package xinsightconnector

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNormalizeConsentBuildsStableProjectSourceScope(t *testing.T) {
	input := Consent{
		InstallationID:   "INSTALLATION.A",
		XDeploymentID:    "X-DEPLOYMENT.A",
		InvitationSHA256: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		OrgID:            7, ProjectID: 11,
		EventScope:        EventScopeModelCompleted,
		IdempotencyKey:    "CONNECT.A",
		ConsentedByUserID: 13,
	}
	normalized, binding, digest, err := normalizeConsent(input, "source.instance.v1")
	require.NoError(t, err)
	require.Equal(t, "installation.a", normalized.InstallationID)
	require.Equal(t, "x-deployment.a", normalized.XDeploymentID)
	require.Equal(t, "source.instance.v1", binding.SourceInstance)
	require.Equal(t, SourceBindingSchemaVersion, binding.SchemaVersion)
	require.Equal(t, CompletedFeedSchemaVersion, binding.FeedSchema)
	require.Equal(t, "sha256:a65994a631d60e05ba865ff61eacdde2fda7e3e5829b6240c0e4061abb56e9a5", digest)

	_, _, repeated, err := normalizeConsent(normalized, "source.instance.v1")
	require.NoError(t, err)
	require.Equal(t, digest, repeated)
}

func TestConsentAndOperationContractsFailClosed(t *testing.T) {
	base := Consent{
		InstallationID: "installation-a", XDeploymentID: "deployment-a",
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
		{"invalid installation identity", func(value *Consent) { value.InstallationID = "../../installation" }},
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
		InstallationID: "installation-a", OrgID: 1, ProjectID: 2,
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
