package admin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/xinsightconnector"
)

func TestXInsightConnectorOperationSummaryNeverExposesLeaseOrIdempotencyAuthority(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	summary := summarizeXInsightConnectorOperation(xinsightconnector.Operation{
		SchemaVersion: xinsightconnector.OperationSchemaVersion,
		OperationRef:  "xcop_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", InstallationID: "installation-a",
		Action: xinsightconnector.ActionConnect, DesiredState: xinsightconnector.DesiredActive,
		State: xinsightconnector.OperationClaimed, RequestedRevision: 1, ControlFence: 1,
		AttemptCount: 1, IdempotencyKey: "private-correlation",
		LeaseOwner: "fake-control-agent", LeaseToken: strings.Repeat("f", 32),
		LeaseExpiresAt: &now, CreatedAt: now, UpdatedAt: now,
	})
	wire, err := json.Marshal(summary)
	require.NoError(t, err)
	require.NotContains(t, string(wire), "lease")
	require.NotContains(t, string(wire), "idempotency")
	require.NotContains(t, string(wire), strings.Repeat("f", 32))
	require.Contains(t, string(wire), `"operation_ref":"xcop_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`)
}

func TestXInsightConnectorInstallationSummaryOmitsInvitationAndDuplicateTenantAuthority(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	invitation := "sha256:" + strings.Repeat("a", 64)
	summary := summarizeXInsightConnectorInstallation(xinsightconnector.Installation{
		SchemaVersion:  xinsightconnector.InstallationSchemaVersion,
		InstallationID: "installation-a", XDeploymentID: "x-deployment-a",
		InvitationSHA256: invitation, OrgID: 7, ProjectID: 11,
		EventScope: xinsightconnector.EventScopeModelCompleted, SourceInstance: "source-a",
		SourceBindingSHA256: "sha256:" + strings.Repeat("b", 64),
		DesiredState:        xinsightconnector.DesiredActive, ObservedState: xinsightconnector.ObservedProvisioning,
		StateRevision: 1, ControlFence: 1, ConsentedAt: now, UpdatedAt: now,
	})
	wire, err := json.Marshal(summary)
	require.NoError(t, err)
	require.NotContains(t, string(wire), "invitation")
	require.NotContains(t, string(wire), invitation)
	require.NotContains(t, string(wire), "org_id")
	require.NotContains(t, string(wire), "project_id")
	require.Contains(t, string(wire), `"installation_id":"installation-a"`)
}

func TestAuthorizeXInsightConnectorControlRejectsCallerSuppliedActorEvidence(t *testing.T) {
	handler := &Handler{}
	ctx := context.WithValue(context.Background(), middleware.CtxUserID, int64(41))
	err := handler.AuthorizeXInsightConnectorControl(
		ctx, nil, 42, 7, 11, xinsightconnector.ActionConnect,
	)
	require.ErrorIs(t, err, xinsightconnector.ErrUnauthorized)
}
