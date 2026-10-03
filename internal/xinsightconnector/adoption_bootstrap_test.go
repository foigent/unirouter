package xinsightconnector

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

func TestAdoptionRedeemStrictVersionedPublicResult(t *testing.T) {
	now := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	i := Installation{InstallationID: "xci_AAAAAAAAAAAAAAAAAAAAAA", XDeploymentID: "xid_ZZZZZZZZZZZZZZZZZZZZZZ", OrgID: 1, ProjectID: 2, SourceInstance: "synthetic-source", EventScope: EventScopeModelCompleted, ConsentedAt: now}
	i.SourceBindingSHA256 = PublicCanonicalSHA256(SourceBinding{SourceBindingSchemaVersion, i.InstallationID, i.XDeploymentID, i.OrgID, i.ProjectID, i.SourceInstance, i.EventScope, CompletedFeedSchemaVersion})
	boot := ReceiverAdoptionBootstrap{i.InstallationID, 1, 2, i.SourceInstance, i.SourceBindingSHA256, "105", "192.0.2.10/32", "ur-xf1-0123456789abc", strings.Repeat("2", 32), "receiver_sha256_v1"}
	wire, e := json.Marshal(boot)
	require.NoError(t, e)
	b := controlBindingRecord{controlProjectID: "project.synthetic", adoptionBootstrapCanonical: wire, adoptionBootstrapSHA256: hashBytes(wire)}
	_, request, digest, e := buildRedeemRequestForBinding(i, b, "xiiv1_"+strings.Repeat("A", 43), b.controlProjectID, "xib_"+strings.Repeat("A", 43), now.Add(time.Hour))
	require.NoError(t, e)
	require.Equal(t, hashBytes(request), digest)
	_, e = decodeRedeemRequestForBinding(request, b)
	require.NoError(t, e)
	_, e = decodeRedeemRequest(request)
	require.Error(t, e, "old request remains closed")
	v := adoptionRegistrationInstallation{SchemaVersion: "xinsight.connector-installation.v1", InstallationID: i.InstallationID, DeploymentID: i.XDeploymentID, ConnectorType: "unirouter", RequestedEventScope: []string{i.EventScope}, FinalEventScope: []string{i.EventScope}, ProjectID: b.controlProjectID, Status: "pending", DesiredState: "active", ObservedState: "agent_registered", CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano), Version: 1}
	// The acknowledgement digest is checked independently of the bootstrap digest.
	result, _ := json.Marshal(adoptionRegistrationResult{RedeemAdoptionResultSchema, v, b.adoptionBootstrapSHA256})
	_, e = canonicalizeAdoptionRedeemResult(result, i, b)
	require.ErrorIs(t, e, ErrConflict)
	for _, bad := range []string{strings.Replace(string(request), `"schema_version":`, `"operation":{},"schema_version":`, 1), strings.Replace(string(request), `"schema_version":`, `"Schema_Version":`, 1), strings.Replace(string(request), `"org_id":1`, `"org_id":null`, 1), strings.Replace(string(request), `"org_id":1`, `"org_id":1,"org_id":1`, 1)} {
		_, e = decodeRedeemRequestForBinding([]byte(bad), b)
		require.Error(t, e)
	}
}
