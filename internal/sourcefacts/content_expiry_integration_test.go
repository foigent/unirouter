//go:build integration

package sourcefacts

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func expiryWait(t *testing.T, p contentPublishPG) {
	t.Helper()
	// Wait for the actual original allocation deadline, never rewrite expiry.
	timer := time.NewTimer(time.Until(p.reservation.ExpiresAt) + 20*time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
}
func expiryCounters(t *testing.T, p contentPublishPG, objects, reserved, stored int64) {
	t.Helper()
	var o, r, s int64
	require.NoError(t, p.pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM execution_content_objects WHERE org_id=$1 AND project_id=$2),reserved_bytes,stored_bytes FROM execution_content_policies WHERE org_id=$1 AND project_id=$2`, p.reservation.OrgID, p.reservation.ProjectID).Scan(&o, &r, &s))
	require.Equal(t, []int64{objects, reserved, stored}, []int64{o, r, s})
}
func expiryPolicy(t *testing.T, p contentPublishPG) *contentcapture.PolicyStore {
	t.Helper()
	policy, err := contentcapture.NewPolicyStore(p.pool, contentcapture.PolicyStoreOptions{SourceInstance: p.service.SourceInstance(), DefaultMode: contentcapture.ModeMetadataOnly, SupportedProtocols: []string{p.manifest.Protocol}, ObservationPoints: []string{p.manifest.ObservationPoint}, Limits: contentcapture.PolicyLimits{MaxSavedBytes: 128, MaxReadBytes: 128, MaxBufferBytes: 7, MaxManifestBytes: 4096, MaxManifestItems: 10}, FullContentEnabled: true})
	require.NoError(t, err)
	return policy
}
func expiryRequest(p contentPublishPG) contentcapture.ReservationRequest {
	return contentcapture.ReservationRequest{OrgID: p.reservation.OrgID, ProjectID: p.reservation.ProjectID, ContentRef: "uc1_" + strings.Repeat("b", 64), SourceFactID: p.reservation.SourceFactID, Revision: 1, ExpectedPolicyVersion: "1", ExpectedRecoveryEpoch: 1, Protocol: p.manifest.Protocol, ObservationPoint: p.manifest.ObservationPoint, MaxSavedBytes: 128, LeaseOwner: "expiry-recapture", LeaseTTL: time.Second}
}
func TestContentExpiryPGPublishedOriginalDeadlineAndQuota(t *testing.T) {
	for _, body := range [][]byte{nil, []byte("body")} {
		t.Run(fmt.Sprint(len(body)), func(t *testing.T) {
			p := setupContentPublishPGWithRetention(t, body, 1, 2)
			_, err := p.publish(t)
			require.NoError(t, err)
			var before string
			require.NoError(t, p.pool.QueryRow(t.Context(), `SELECT to_jsonb(m)::text FROM execution_content_manifests m WHERE org_id=$1 AND project_id=$2`, p.reservation.OrgID, p.reservation.ProjectID).Scan(&before))
			result, err := p.service.ReclaimExpiredContent(t.Context(), p.files, 1)
			require.NoError(t, err)
			require.Zero(t, result.Reclaimed)
			expiryCounters(t, p, 1, 0, int64(len(body)))
			expiryWait(t, p)
			result, err = p.service.ReclaimExpiredContent(t.Context(), p.files, 1)
			require.NoError(t, err)
			require.Equal(t, 1, result.Reclaimed)
			require.EqualValues(t, len(body), result.ReleasedBytes)
			expiryCounters(t, p, 0, 0, 0)
			require.NoFileExists(t, filepath.Join(p.root, p.reservation.ObjectName))
			require.NoError(t, p.files.CheckReady(t.Context()))
			var after string
			require.NoError(t, p.pool.QueryRow(t.Context(), `SELECT to_jsonb(m)::text FROM execution_content_manifests m WHERE org_id=$1 AND project_id=$2`, p.reservation.OrgID, p.reservation.ProjectID).Scan(&after))
			require.Equal(t, before, after)
			result, err = p.service.ReclaimExpiredContent(t.Context(), p.files, 1)
			require.NoError(t, err)
			require.Zero(t, result.Reclaimed)
			policy := expiryPolicy(t, p)
			request := expiryRequest(p)
			_, err = policy.Reserve(t.Context(), p.files, request)
			require.ErrorIs(t, err, contentcapture.ErrReservationConflict)
			expiryCounters(t, p, 0, 0, 0)
			request.SourceFactID = "uf1_" + strings.Repeat("c", 64)
			_, err = policy.Reserve(t.Context(), p.files, request)
			require.NoError(t, err)
			expiryCounters(t, p, 1, 128, 0)
		})
	}
}
func TestContentExpiryPGPreservesExcludedAndFailedFiles(t *testing.T) {
	for _, kind := range []string{"reserved", "orphan", "source", "binding", "directory", "manifest"} {
		t.Run(kind, func(t *testing.T) {
			p := setupContentPublishPGWithRetention(t, []byte("body"), 1, 2)
			if kind != "reserved" && kind != "orphan" {
				_, err := p.publish(t)
				require.NoError(t, err)
			}
			var err error
			switch kind {
			case "orphan":
				_, err = p.pool.Exec(t.Context(), `UPDATE execution_content_objects SET state='orphan' WHERE content_ref=$1`, p.reservation.ContentRef)
			case "source":
				p.service.options.SourceInstance = "another-source"
			case "binding":
				_, err = p.pool.Exec(t.Context(), `UPDATE execution_content_objects SET storage_binding_token=repeat('f',64) WHERE content_ref=$1`, p.reservation.ContentRef)
			case "directory":
				require.NoError(t, os.Rename(filepath.Join(p.root, p.reservation.ObjectName), filepath.Join(p.root, p.reservation.ObjectName+".retained")))
				err = os.Mkdir(filepath.Join(p.root, p.reservation.ObjectName), 0700)
			case "manifest":
				_, err = p.pool.Exec(t.Context(), `UPDATE execution_content_objects SET saved_bytes=0 WHERE content_ref=$1`, p.reservation.ContentRef)
			}
			require.NoError(t, err)
			expiryWait(t, p)
			result, err := p.service.ReclaimExpiredContent(t.Context(), p.files, 10)
			if kind == "binding" || kind == "directory" || kind == "manifest" {
				require.Error(t, err)
				require.Equal(t, 1, result.Failed)
			} else {
				require.NoError(t, err)
			}
			require.Zero(t, result.Reclaimed)
			if kind == "reserved" || kind == "orphan" {
				expiryCounters(t, p, 1, 128, 0)
			} else {
				expiryCounters(t, p, 1, 0, 4)
			}
			_, err = os.Lstat(filepath.Join(p.root, p.reservation.ObjectName))
			require.NoError(t, err)
		})
	}
}
func TestContentExpiryPGConcurrentHistoricalEpochAndMissingFile(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprint(missing), func(t *testing.T) {
			p := setupContentPublishPGWithRetention(t, []byte("body"), 1, 2)
			_, err := p.publish(t)
			require.NoError(t, err)
			_, err = p.pool.Exec(t.Context(), `UPDATE execution_content_streams SET recovery_epoch=recovery_epoch+1 WHERE org_id=$1 AND project_id=$2`, p.reservation.OrgID, p.reservation.ProjectID)
			require.NoError(t, err)
			expiryWait(t, p)
			// Simulate an earlier durable file phase whose DB commit never happened.
			if missing {
				require.NoError(t, p.files.RemoveExpired(t.Context(), p.receipt))
			}
			neighbour := filepath.Join(p.root, p.reservation.ObjectName+".retained")
			require.NoError(t, os.WriteFile(neighbour, []byte("retain"), 0600))
			expiryCounters(t, p, 1, 0, 4)
			candidate := expiredContentObject{ref: p.reservation.ContentRef, org: p.reservation.OrgID, project: p.reservation.ProjectID}
			var wg sync.WaitGroup
			type outcome struct {
				saved   int64
				removed bool
				err     error
			}
			results := make(chan outcome, 2)
			for range 2 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					n, removed, err := p.service.reclaimExpiredContentObject(t.Context(), p.pool, p.files, candidate)
					results <- outcome{n, removed, err}
				}()
			}
			wg.Wait()
			close(results)
			var removed int
			var saved int64
			for r := range results {
				require.NoError(t, r.err)
				if r.removed {
					removed++
				}
				saved += r.saved
			}
			require.Equal(t, 1, removed)
			require.EqualValues(t, 4, saved)
			expiryCounters(t, p, 0, 0, 0)
			require.NoError(t, p.files.CheckReady(t.Context()))
			raw, err := os.ReadFile(neighbour)
			require.NoError(t, err)
			require.Equal(t, "retain", string(raw))
		})
	}
}
func TestContentExpiryPGDatabaseRollbackKeepsChargeUntilNextBatch(t *testing.T) {
	p := setupContentPublishPGWithRetention(t, []byte("body"), 1, 2)
	_, err := p.publish(t)
	require.NoError(t, err)
	expiryWait(t, p)
	_, err = p.pool.Exec(t.Context(), `CREATE FUNCTION synthetic_expiry_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic rollback'; END $$; CREATE TRIGGER synthetic_expiry_failure BEFORE UPDATE OF stored_bytes ON execution_content_policies FOR EACH ROW EXECUTE FUNCTION synthetic_expiry_failure()`)
	require.NoError(t, err)
	result, err := p.service.ReclaimExpiredContent(t.Context(), p.files, 10)
	require.Error(t, err)
	require.Zero(t, result.Reclaimed)
	expiryCounters(t, p, 1, 0, 4)
	require.NoFileExists(t, filepath.Join(p.root, p.reservation.ObjectName))
	_, err = p.pool.Exec(t.Context(), `DROP TRIGGER synthetic_expiry_failure ON execution_content_policies; DROP FUNCTION synthetic_expiry_failure()`)
	require.NoError(t, err)
	result, err = p.service.ReclaimExpiredContent(t.Context(), p.files, 10)
	require.NoError(t, err)
	require.Equal(t, 1, result.Reclaimed)
	expiryCounters(t, p, 0, 0, 0)
}
func TestContentExpiryPGIndependentPoolAndLimits(t *testing.T) {
	p := setupContentPublishPGWithRetention(t, []byte("body"), 1, 2)
	for _, limit := range []int{0, 1001} {
		_, err := p.service.ReclaimExpiredContent(t.Context(), p.files, limit)
		require.Error(t, err)
	}
	tx, err := p.pool.Begin(t.Context())
	require.NoError(t, err)
	defer tx.Rollback(context.Background())
	copied := *p.service
	copied.db = tx
	_, err = copied.ReclaimExpiredContent(t.Context(), p.files, 1)
	require.ErrorIs(t, err, ErrContentJournalTransaction)
	_, err = p.service.ReclaimExpiredContent(t.Context(), nil, 1)
	require.Error(t, err)
	// A later policy disable does not shorten the original body's retention.
	_, err = p.publish(t)
	require.NoError(t, err)
	disabled := contentcapture.PolicyConfig{Mode: contentcapture.ModeDisabled, SupportedProtocols: []string{p.manifest.Protocol}, ObservationPoints: []string{p.manifest.ObservationPoint}, AllowedPurposes: []string{}}
	wire, err := json.Marshal(disabled)
	require.NoError(t, err)
	_, err = p.pool.Exec(t.Context(), `UPDATE execution_content_policies SET desired_version=2,effective_version=2,desired_config=$3,effective_config=$3 WHERE org_id=$1 AND project_id=$2`, p.reservation.OrgID, p.reservation.ProjectID, wire)
	require.NoError(t, err)
	result, err := p.service.ReclaimExpiredContent(t.Context(), p.files, 1)
	require.NoError(t, err)
	require.Zero(t, result.Reclaimed)
	expiryWait(t, p)
	result, err = p.service.ReclaimExpiredContent(t.Context(), p.files, 1)
	require.NoError(t, err)
	require.Equal(t, 1, result.Reclaimed)
}

func TestContentExpiryPGMultipleProjectsBoundedBatch(t *testing.T) {
	p := setupContentPublishPGWithRetention(t, []byte("body"), 1, 3)
	_, err := p.publish(t)
	require.NoError(t, err)
	org := p.reservation.OrgID
	var project int64
	require.NoError(t, p.pool.QueryRow(t.Context(), `INSERT INTO projects(org_id,name,slug,status,budget_period) VALUES($1,'Expiry second','expiry-second','active','none') RETURNING id`, org).Scan(&project))
	_, err = p.pool.Exec(t.Context(), `INSERT INTO execution_content_streams(org_id,project_id,source_instance) VALUES($1,$2,$3)`, org, project, p.service.SourceInstance())
	require.NoError(t, err)
	wire, err := json.Marshal(p.reservation.Effective.Config)
	require.NoError(t, err)
	_, err = p.pool.Exec(t.Context(), `INSERT INTO execution_content_policies(org_id,project_id,desired_version,effective_version,desired_config,effective_config,effective_at,application_status,application_reason) VALUES($1,$2,1,1,$3,$3,clock_timestamp(),'applied','none')`, org, project, wire)
	require.NoError(t, err)
	m := p.manifest
	m.OrgID = fmt.Sprint(org)
	m.ProjectID = fmt.Sprint(project)
	m.RequestID = "expiry-second-project"
	m.SourceFactID = m.FactID()
	m.EventID = m.EventIDForRevision()
	m.ContentRef = "uc1_" + strings.Repeat("e", 64)
	request := expiryRequest(p)
	request.OrgID = org
	request.ProjectID = project
	request.SourceFactID = m.SourceFactID
	request.ContentRef = m.ContentRef
	reservation, err := expiryPolicy(t, p).Reserve(t.Context(), p.files, request)
	require.NoError(t, err)
	receipt, err := p.files.SaveReservation(t.Context(), reservation, strings.NewReader("body"), p.limits)
	require.NoError(t, err)
	m.Capture.PolicyRef = fmt.Sprintf("content-policy:%d:%d", org, project)
	m.Capture.PolicyEffectiveAt = reservation.EffectiveAt
	m.CapturedAt = reservation.ExpiresAt.Add(-3 * time.Second).UTC().Format("2006-01-02T15:04:05.000000Z")
	m.ExpiresAt = reservation.ExpiresAt.UTC().Format("2006-01-02T15:04:05.000000Z")
	_, err = p.service.PublishReservedContent(t.Context(), p.files, reservation, receipt, m, p.limits)
	require.NoError(t, err)
	second := p
	second.reservation = reservation
	expiryWait(t, second)
	firstResult, err := p.service.ReclaimExpiredContent(t.Context(), p.files, 1)
	require.NoError(t, err)
	require.Equal(t, 1, firstResult.Examined)
	require.Equal(t, 1, firstResult.Reclaimed)
	expiryCounters(t, p, 0, 0, 0)
	expiryCounters(t, second, 1, 0, 4)
	require.FileExists(t, filepath.Join(p.root, reservation.ObjectName))
	secondResult, err := p.service.ReclaimExpiredContent(t.Context(), p.files, 1)
	require.NoError(t, err)
	require.Equal(t, 1, secondResult.Examined)
	require.Equal(t, 1, secondResult.Reclaimed)
	require.EqualValues(t, 8, firstResult.ReleasedBytes+secondResult.ReleasedBytes)
	expiryCounters(t, p, 0, 0, 0)
	expiryCounters(t, second, 0, 0, 0)
	require.NoFileExists(t, filepath.Join(p.root, p.reservation.ObjectName))
	require.NoFileExists(t, filepath.Join(p.root, reservation.ObjectName))
}
