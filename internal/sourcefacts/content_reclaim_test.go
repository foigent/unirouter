package sourcefacts

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/installstate"
	"github.com/uniroute/unirouter/internal/schemaauthority"
)

func TestUnpublishedReclaimRequiresExplicitOfflineAndExactLedger(t *testing.T) {
	ledger, err := schemaauthority.ExpectedLedger("../../migrations")
	require.NoError(t, err)
	r := UnpublishedReclaimRequest{Target: installstate.Target{Environment: "synthetic", Database: "test", DatabaseOID: 1, SystemIdentifier: "123", Owner: "test", Role: "test"}, ExpectedLedger: ledger, SourceInstance: "synthetic", OrgID: 1, ProjectID: 2, ContentRef: "uc1_" + strings.Repeat("a", 64), AllOwnersStopped: true, RestartsProhibited: true, OfflineEvidenceSHA256: "sha256:" + strings.Repeat("b", 64)}
	require.NoError(t, validateUnpublishedRequest(r))
	for _, change := range []func(*UnpublishedReclaimRequest){
		func(r *UnpublishedReclaimRequest) { r.AllOwnersStopped = false }, func(r *UnpublishedReclaimRequest) { r.RestartsProhibited = false },
		func(r *UnpublishedReclaimRequest) { r.OfflineEvidenceSHA256 = "ack" }, func(r *UnpublishedReclaimRequest) { r.Target.DatabaseOID = 0 },
		func(r *UnpublishedReclaimRequest) { r.ExpectedLedger = r.ExpectedLedger[:len(r.ExpectedLedger)-1] },
		func(r *UnpublishedReclaimRequest) {
			r.ExpectedLedger = append(append([]string{}, r.ExpectedLedger[:len(r.ExpectedLedger)-2]...), schemaauthority.ContentReclaimExpandHead)
		},
	} {
		bad := r
		change(&bad)
		require.Error(t, validateUnpublishedRequest(bad))
	}
}
func TestUnpublishedManifestAcceptsUncapturedChainAndRejectsPublishedOrMissingHistory(t *testing.T) {
	m := syntheticContentManifest([]byte("body"))
	r := UnpublishedReclaimRequest{SourceInstance: m.SourceInstance, OrgID: 1, ProjectID: 2}
	m.OrgID = "1"
	m.ProjectID = "2"
	m.SourceFactID = m.FactID()
	m.EventID = m.EventIDForRevision()
	wire, err := EncodeContentManifest(m)
	require.NoError(t, err)
	_, err = unpublishedManifest(wire, eventconnector.HashSourceBytes(wire), r, m.SourceFactID, 1, nil)
	require.Error(t, err)
	m.ContentRef = ""
	m.Representation = nil
	m.CapturedAt = ""
	m.ExpiresAt = ""
	m.Capture.Completeness = "pending"
	m.Capture.Reason = "capture_in_progress"
	wire, err = EncodeContentManifest(m)
	require.NoError(t, err)
	_, err = unpublishedManifest(wire, eventconnector.HashSourceBytes(wire), r, m.SourceFactID, 1, nil)
	require.NoError(t, err)
	next := m
	next.Revision = "2"
	next.PreviousRevision = "1"
	next.PreviousHash = eventconnector.HashSourceBytes(wire)
	next.CorrectionReason = "capture_finished"
	next.EventID = next.EventIDForRevision()
	next.Capture.Completeness = "not_captured"
	next.Capture.Reason = "storage_write_failed"
	wire, err = EncodeContentManifest(next)
	require.NoError(t, err)
	hash := eventconnector.HashSourceBytes(wire)
	_, err = unpublishedManifest(wire, hash, r, m.SourceFactID, 2, &m)
	require.NoError(t, err)
	_, err = unpublishedManifest(wire, hash, r, m.SourceFactID, 2, nil)
	require.Error(t, err)
	_, err = unpublishedManifest(wire, "sha256:"+strings.Repeat("a", 64), r, m.SourceFactID, 2, &m)
	require.Error(t, err)
	r.SourceInstance = "wrong"
	_, err = unpublishedManifest(wire, hash, r, m.SourceFactID, 2, &m)
	require.Error(t, err)
}
