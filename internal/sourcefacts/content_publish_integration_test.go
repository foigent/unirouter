//go:build integration

package sourcefacts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/contentcapture"
)

// Synthetic isolated PostgreSQL fixtures only. Compiling these tests does not
// prove transactional behavior; execution requires the separately authorized DB.
type contentPublishPG struct {
	pool        *pgxpool.Pool
	service     *Service
	files       *contentcapture.FileStore
	root        string
	reservation contentcapture.Reservation
	receipt     contentcapture.StoredReceipt
	manifest    ContentManifest
	limits      contentcapture.StoreLimits
	principal   contentcapture.CapturePrincipal
}

func setupContentPublishPG(t *testing.T, body []byte, revision int64) contentPublishPG {
	t.Helper()
	return setupContentPublishPGWithRetention(t, body, revision, 86400)
}
func setupContentPublishPGWithRetention(t *testing.T, body []byte, revision, retention int64) contentPublishPG {
	t.Helper()
	pool, s, org, project := setupContentJournal(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	binding, err := contentcapture.InitializeFileStore(root, "synthetic-publish-store")
	require.NoError(t, err)
	files, err := contentcapture.OpenFileStore(root, binding)
	require.NoError(t, err)
	t.Cleanup(func() { _ = files.Close() })
	r, _, m, _ := contentPublishFixture(body)
	m.OrgID = fmt.Sprint(org)
	m.ProjectID = fmt.Sprint(project)
	m.SourceInstance = s.SourceInstance()
	m.Revision = fmt.Sprint(revision)
	if revision == 2 {
		m.PreviousRevision = "1"
		m.PreviousHash = "sha256:" + strings.Repeat("a", 64)
		m.CorrectionReason = "capture_finished"
	}
	m.SourceFactID = m.FactID()
	m.EventID = m.EventIDForRevision()
	r.Effective.Config.RetentionSeconds = &retention
	config, err := json.Marshal(r.Effective.Config)
	require.NoError(t, err)
	// Explicit synthetic full policy; no default/GET path enables capture.
	_, err = pool.Exec(t.Context(), `INSERT INTO execution_content_policies(org_id,project_id,desired_version,effective_version,desired_config,effective_config,effective_at,application_status,application_reason) VALUES($1,$2,1,1,$3,$3,clock_timestamp(),'applied','none')`, org, project, config)
	require.NoError(t, err)
	policy, err := contentcapture.NewPolicyStore(pool, contentcapture.PolicyStoreOptions{SourceInstance: s.SourceInstance(), DefaultMode: contentcapture.ModeMetadataOnly, SupportedProtocols: []string{m.Protocol}, ObservationPoints: []string{m.ObservationPoint}, Limits: contentcapture.PolicyLimits{MaxSavedBytes: 128, MaxReadBytes: 128, MaxBufferBytes: 7, MaxManifestBytes: 4096, MaxManifestItems: 10}, FullContentEnabled: true})
	require.NoError(t, err)
	var keyID int64
	require.NoError(t, pool.QueryRow(t.Context(), `INSERT INTO api_keys(org_id,project_id,name,key_hash,key_prefix,status)
      VALUES($1,$2,'synthetic-publish',repeat('d',64),'synthetic-publish','active') RETURNING id`, org, project).Scan(&keyID))
	principal := contentcapture.CapturePrincipal{APIKeyID: keyID, KeyType: "shared"}
	snapshot, err := policy.EffectiveScopedForCapture(t.Context(), principal, org, project, "ready")
	require.NoError(t, err)
	r, err = policy.Reserve(t.Context(), files, contentcapture.ReservationRequest{OrgID: org, ProjectID: project, ContentRef: m.ContentRef, SourceFactID: m.SourceFactID, Revision: revision, ExpectedPolicyVersion: snapshot.Effective.PolicyVersion, ExpectedPolicyRef: snapshot.PolicyRef, ExpectedPolicyVector: snapshot.Vector, Principal: principal, ExpectedRecoveryEpoch: 1, Protocol: m.Protocol, ObservationPoint: m.ObservationPoint, MaxSavedBytes: 128, LeaseOwner: "synthetic-publisher", LeaseTTL: min(time.Hour, time.Duration(retention)*time.Second)})
	require.NoError(t, err)
	limits := contentcapture.StoreLimits{MaxSavedBytes: 128, MaxReadBytes: 128, MaxBufferBytes: 7}
	receipt, err := files.SaveReservation(t.Context(), r, bytes.NewReader(body), limits)
	require.NoError(t, err)
	m.Capture.PolicyRef = fmt.Sprintf("content-policy:%d:%d", org, project)
	m.Capture.PolicyVersion = r.Effective.PolicyVersion
	m.Capture.PolicyEffectiveAt = r.EffectiveAt
	m.CapturedAt = r.ExpiresAt.Add(-time.Duration(*r.Effective.Config.RetentionSeconds) * time.Second).UTC().Format("2006-01-02T15:04:05.000000Z")
	m.ExpiresAt = r.ExpiresAt.UTC().Format("2006-01-02T15:04:05.000000Z")
	return contentPublishPG{pool: pool, service: s, files: files, root: root, reservation: r, receipt: receipt, manifest: m, limits: limits, principal: principal}
}

func (p contentPublishPG) publish(t *testing.T) (ContentAppendResult, error) {
	t.Helper()
	return p.service.PublishReservedContent(t.Context(), p.files, p.reservation, p.receipt, p.manifest, p.limits)
}

func (p contentPublishPG) assertState(t *testing.T, state string, reserved, stored, high int64) {
	t.Helper()
	var actualState string
	var actualReserved, actualStored, actualHigh, manifests int64
	require.NoError(t, p.pool.QueryRow(t.Context(), `SELECT o.state,p.reserved_bytes,p.stored_bytes,s.high_sequence,(SELECT count(*) FROM execution_content_manifests m WHERE m.org_id=o.org_id AND m.project_id=o.project_id) FROM execution_content_objects o JOIN execution_content_policies p USING(org_id,project_id) JOIN execution_content_streams s USING(org_id,project_id) WHERE o.content_ref=$1`, p.reservation.ContentRef).Scan(&actualState, &actualReserved, &actualStored, &actualHigh, &manifests))
	require.Equal(t, state, actualState)
	require.Equal(t, reserved, actualReserved)
	require.Equal(t, stored, actualStored)
	require.Equal(t, high, actualHigh)
	require.Equal(t, high, manifests)
	_, err := os.Stat(filepath.Join(p.root, p.reservation.ObjectName))
	require.NoError(t, err, "file stays identifiable by original reservation")
}

func TestContentPublishPGAtomicReadyAndExactReplay(t *testing.T) {
	for _, body := range [][]byte{nil, []byte("binary\x00not-json")} {
		t.Run(fmt.Sprint(len(body)), func(t *testing.T) {
			p := setupContentPublishPG(t, body, 1)
			first, err := p.publish(t)
			require.NoError(t, err)
			require.False(t, first.Replayed)
			p.assertState(t, "ready", 0, int64(len(body)), 1)
			// A completed publish remains an exact replay after its worker lease ends.
			p.reservation.LeaseExpiresAt = p.reservation.ExpiresAt.Add(-25 * time.Hour)
			_, err = p.pool.Exec(t.Context(), `UPDATE execution_content_objects SET lease_expires_at=$2 WHERE content_ref=$1`, p.reservation.ContentRef, p.reservation.LeaseExpiresAt)
			require.NoError(t, err)
			replay, err := p.publish(t)
			require.NoError(t, err)
			require.True(t, replay.Replayed)
			require.Equal(t, first.Sequence, replay.Sequence)
			p.assertState(t, "ready", 0, int64(len(body)), 1)
			p.manifest.RecordedAt = "2026-09-15T00:00:01.000000Z"
			_, err = p.publish(t)
			require.ErrorIs(t, err, ErrContentJournalRevision)
			p.assertState(t, "ready", 0, int64(len(body)), 1)
		})
	}
}

func TestContentPublishPGOriginalPolicySurvivesLaterDisable(t *testing.T) {
	p := setupContentPublishPG(t, []byte("saved"), 1)
	disabled := contentcapture.PolicyConfig{Mode: contentcapture.ModeDisabled, SupportedProtocols: []string{p.manifest.Protocol}, ObservationPoints: []string{p.manifest.ObservationPoint}, AllowedPurposes: []string{}}
	wire, err := json.Marshal(disabled)
	require.NoError(t, err)
	_, err = p.pool.Exec(t.Context(), `UPDATE execution_content_policies SET desired_version=2,effective_version=2,desired_config=$3,effective_config=$3,effective_at=clock_timestamp() WHERE org_id=$1 AND project_id=$2`, p.reservation.OrgID, p.reservation.ProjectID, wire)
	require.NoError(t, err)
	_, err = p.publish(t)
	require.NoError(t, err)
	p.assertState(t, "ready", 0, 5, 1)
}

func TestContentPublishPGRejectsExpiredLeaseEpochAndPersistentMismatch(t *testing.T) {
	for _, name := range []string{"lease", "epoch", "marker", "name", "snapshot"} {
		t.Run(name, func(t *testing.T) {
			p := setupContentPublishPG(t, []byte("body"), 1)
			var err error
			want := ErrContentPublishBinding
			switch name {
			case "lease":
				p.reservation.LeaseExpiresAt = p.reservation.ExpiresAt.Add(-25 * time.Hour)
				_, err = p.pool.Exec(t.Context(), `UPDATE execution_content_objects SET lease_expires_at=$2 WHERE content_ref=$1`, p.reservation.ContentRef, p.reservation.LeaseExpiresAt)
				want = ErrContentPublishLease
			case "epoch":
				_, err = p.pool.Exec(t.Context(), `UPDATE execution_content_streams SET recovery_epoch=recovery_epoch+1 WHERE org_id=$1 AND project_id=$2`, p.reservation.OrgID, p.reservation.ProjectID)
				want = ErrContentEpochChanged
			case "marker":
				_, err = p.pool.Exec(t.Context(), `UPDATE execution_content_objects SET storage_binding_token=repeat('b',64) WHERE content_ref=$1`, p.reservation.ContentRef)
			case "name":
				_, err = p.pool.Exec(t.Context(), `UPDATE execution_content_objects SET relative_path=repeat('c',64) WHERE content_ref=$1`, p.reservation.ContentRef)
			case "snapshot":
				_, err = p.pool.Exec(t.Context(), `UPDATE execution_content_objects SET policy_effective_at=policy_effective_at+interval '1 second' WHERE content_ref=$1`, p.reservation.ContentRef)
			}
			require.NoError(t, err)
			_, err = p.publish(t)
			require.ErrorIs(t, err, want)
			p.assertState(t, "reserved", 128, 0, 0)
		})
	}
}

func TestContentPublishPGManifestFailureRollsBackReadyAndAccounting(t *testing.T) {
	p := setupContentPublishPG(t, []byte("body"), 2)
	// Missing predecessor is detected by the shared appender after ready and
	// accounting updates. The entire top-level transaction must roll back.
	_, err := p.publish(t)
	require.ErrorIs(t, err, ErrContentJournalRevision)
	p.assertState(t, "reserved", 128, 0, 0)
	var saved int64
	var digest *string
	require.NoError(t, p.pool.QueryRow(t.Context(), `SELECT saved_bytes,digest FROM execution_content_objects WHERE content_ref=$1`, p.reservation.ContentRef).Scan(&saved, &digest))
	require.Zero(t, saved)
	require.Nil(t, digest)
}
