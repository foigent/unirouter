//go:build integration

package sourcefacts

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/installstate"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

func reclaimInput(t *testing.T, p contentPublishPG) UnpublishedReclaimRequest {
	t.Helper()
	target, err := installstate.ReadTarget(t.Context(), p.pool, "synthetic-offline-reclaim")
	require.NoError(t, err)
	ledger, err := schemaauthority.ExpectedLedger(testutil.MigrationsDir(t))
	require.NoError(t, err)
	t.Logf("synthetic offline selection database=%s oid=%d system=%s root=%s source=%s org=%d project=%d ref=%s final=%s temp=%s; Reserve/Save returned; no backend/reader/capture/media/maintenance owner was started by this fixture; no owner restart path is configured", target.Database, target.DatabaseOID, target.SystemIdentifier, p.root, p.service.SourceInstance(), p.reservation.OrgID, p.reservation.ProjectID, p.reservation.ContentRef, p.reservation.ObjectName, "."+p.reservation.ObjectName+".tmp")
	return UnpublishedReclaimRequest{Target: target, ExpectedLedger: ledger, SourceInstance: p.service.SourceInstance(), OrgID: p.reservation.OrgID, ProjectID: p.reservation.ProjectID, ContentRef: p.reservation.ContentRef, AllOwnersStopped: true, RestartsProhibited: true, OfflineEvidenceSHA256: "sha256:" + strings.Repeat("a", 64)}
}
func reclaimSnapshot(t *testing.T, p contentPublishPG) string {
	t.Helper()
	var snapshot string
	require.NoError(t, p.pool.QueryRow(t.Context(), `SELECT jsonb_build_object('objects',(SELECT jsonb_agg(to_jsonb(o) ORDER BY content_ref) FROM execution_content_objects o),'policies',(SELECT jsonb_agg(to_jsonb(p) ORDER BY org_id,project_id) FROM execution_content_policies p),'manifests',(SELECT jsonb_agg(to_jsonb(m) ORDER BY org_id,project_id,sequence) FROM execution_content_manifests m),'streams',(SELECT jsonb_agg(to_jsonb(s) ORDER BY org_id,project_id) FROM execution_content_streams s))::text`).Scan(&snapshot))
	return fmt.Sprintf("%x", sha256.Sum256([]byte(snapshot)))
}
func TestUnpublishedReclaimPGInterruptedPathsCheckAndDedup(t *testing.T) {
	for _, kind := range []string{"before_pending", "temp", "final", "same_inode"} {
		t.Run(kind, func(t *testing.T) {
			p := setupContentPublishPG(t, []byte("synthetic interrupted bytes"), 1)
			remaining := int64(0)
			var aside string
			if kind == "before_pending" {
				aside = filepath.Join(p.root, p.reservation.ObjectName)
				request := expiryRequest(p)
				request.SourceFactID = "uf1_" + strings.Repeat("d", 64)
				reservation, err := expiryPolicy(t, p).Reserve(t.Context(), p.files, request)
				require.NoError(t, err)
				p.reservation = reservation
				remaining = 128
			}
			final := filepath.Join(p.root, p.reservation.ObjectName)
			temp := filepath.Join(p.root, "."+p.reservation.ObjectName+".tmp")
			if kind == "temp" {
				require.NoError(t, os.Rename(final, temp))
			}
			if kind == "same_inode" {
				require.NoError(t, os.Link(final, temp))
			}
			side := filepath.Join(p.root, "unrelated-side-file")
			require.NoError(t, os.WriteFile(side, []byte("keep"), 0600))
			input := reclaimInput(t, p)
			input.CheckOnly = true
			before := reclaimSnapshot(t, p)
			result, err := ReclaimUnpublishedContent(t.Context(), p.pool, p.files, input)
			require.NoError(t, err)
			require.Equal(t, "eligible", result.Status)
			require.Zero(t, result.ReleasedBytes)
			require.Equal(t, before, reclaimSnapshot(t, p))
			input.CheckOnly = false
			result, err = ReclaimUnpublishedContent(t.Context(), p.pool, p.files, input)
			require.NoError(t, err)
			require.EqualValues(t, 128, result.ReleasedBytes)
			require.NoFileExists(t, final)
			require.NoFileExists(t, temp)
			require.FileExists(t, side)
			if aside != "" {
				require.FileExists(t, aside)
			}
			require.NoError(t, p.files.CheckReady(t.Context()))
			var state string
			var marked bool
			var original, reserved, stored int64
			require.NoError(t, p.pool.QueryRow(t.Context(), `SELECT o.state,o.reclaimed_at IS NOT NULL,o.reserved_bytes,p.reserved_bytes,p.stored_bytes FROM execution_content_objects o JOIN execution_content_policies p USING(org_id,project_id) WHERE o.content_ref=$1`, input.ContentRef).Scan(&state, &marked, &original, &reserved, &stored))
			require.Equal(t, "orphan", state)
			require.True(t, marked)
			require.EqualValues(t, 128, original)
			require.Equal(t, remaining, reserved)
			require.Zero(t, stored)
			result, err = ReclaimUnpublishedContent(t.Context(), p.pool, p.files, input)
			require.NoError(t, err)
			require.Equal(t, "already_reclaimed", result.Status)
			require.Zero(t, result.ReleasedBytes)
			request := expiryRequest(p)
			request.ContentRef = "uc1_" + strings.Repeat("e", 64)
			_, err = expiryPolicy(t, p).Reserve(t.Context(), p.files, request)
			require.ErrorIs(t, err, contentcapture.ErrReservationConflict)
		})
	}
}
func TestUnpublishedReclaimPGNotCapturedFullChainOldEpochAndHistory(t *testing.T) {
	p := setupContentPublishPG(t, []byte("interrupted"), 1)
	pending := p.manifest
	pending.ContentRef = ""
	pending.Representation = nil
	pending.CapturedAt = ""
	pending.ExpiresAt = ""
	pending.Capture.Completeness = "pending"
	pending.Capture.Reason = "capture_in_progress"
	first, err := p.service.AppendContentManifest(t.Context(), pending, 1)
	require.NoError(t, err)
	terminal := pending
	terminal.Revision = "2"
	terminal.PreviousRevision = "1"
	terminal.PreviousHash = first.SourceSHA256
	terminal.CorrectionReason = "capture_failed"
	terminal.EventID = terminal.EventIDForRevision()
	terminal.Capture.Completeness = "not_captured"
	terminal.Capture.Reason = "storage_write_failed"
	_, err = p.service.AppendContentManifest(t.Context(), terminal, 1)
	require.NoError(t, err)
	_, err = p.pool.Exec(t.Context(), `UPDATE execution_content_streams SET recovery_epoch=2 WHERE org_id=$1 AND project_id=$2`, p.reservation.OrgID, p.reservation.ProjectID)
	require.NoError(t, err)
	history := func() string {
		var s string
		require.NoError(t, p.pool.QueryRow(t.Context(), `SELECT jsonb_agg(to_jsonb(m) ORDER BY sequence)::text FROM execution_content_manifests m`).Scan(&s))
		return s
	}
	before := history()
	result, err := ReclaimUnpublishedContent(t.Context(), p.pool, p.files, reclaimInput(t, p))
	require.NoError(t, err)
	require.EqualValues(t, 128, result.ReleasedBytes)
	require.Equal(t, before, history())
	var epoch int64
	require.NoError(t, p.pool.QueryRow(t.Context(), `SELECT recovery_epoch FROM execution_content_objects WHERE content_ref=$1`, p.reservation.ContentRef).Scan(&epoch))
	require.EqualValues(t, 1, epoch)
}
func TestUnpublishedReclaimPGRejectsAmbiguousAndPublished(t *testing.T) {
	for _, kind := range []string{"published_receipt_lost", "unknown_orphan", "duplicate_mapping", "wrong_binding", "wrong_source", "wrong_database", "retained_history", "missing_M120", "missing_middle", "quota_mismatch"} {
		t.Run(kind, func(t *testing.T) {
			p := setupContentPublishPG(t, []byte("keep"), 1)
			input := reclaimInput(t, p)
			var err error
			switch kind {
			case "published_receipt_lost":
				_, err = p.publish(t)
			case "unknown_orphan":
				_, err = p.pool.Exec(t.Context(), `UPDATE execution_content_objects SET state='orphan' WHERE content_ref=$1`, input.ContentRef)
			case "wrong_binding":
				_, err = p.pool.Exec(t.Context(), `UPDATE execution_content_objects SET storage_binding_token=repeat('f',64) WHERE content_ref=$1`, input.ContentRef)
			case "wrong_source":
				input.SourceInstance = "another"
			case "wrong_database":
				input.Target.DatabaseOID++
			case "retained_history":
				_, err = p.pool.Exec(t.Context(), `UPDATE execution_content_streams SET high_sequence=1,retained_after_sequence=1 WHERE org_id=$1 AND project_id=$2`, input.OrgID, input.ProjectID)
			case "missing_M120":
				require.NoError(t, sourceBudgetFixtureDownIfPresent(t, p.pool, schemaauthority.PreDispatchExpandHead))
				wire, e := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), schemaauthority.ContentReclaimExpandHead+".down.sql"))
				require.NoError(t, e)
				_, err = p.pool.Exec(t.Context(), string(wire))
			case "missing_middle":
				_, err = p.pool.Exec(t.Context(), `DELETE FROM schema_migrations WHERE version=$1`, schemaauthority.ContentCaptureExpandHead)
			case "quota_mismatch":
				_, err = p.pool.Exec(t.Context(), `UPDATE execution_content_policies SET reserved_bytes=reserved_bytes+1 WHERE org_id=$1 AND project_id=$2`, input.OrgID, input.ProjectID)
			case "duplicate_mapping":
				request := expiryRequest(p)
				request.SourceFactID = "uf1_" + strings.Repeat("d", 64)
				other, e := expiryPolicy(t, p).Reserve(t.Context(), p.files, request)
				require.NoError(t, e)
				_, err = p.pool.Exec(t.Context(), `UPDATE execution_content_objects SET relative_path=$2 WHERE content_ref=$1`, other.ContentRef, p.reservation.ObjectName)
			}
			require.NoError(t, err)
			before := reclaimSnapshot(t, p)
			_, err = ReclaimUnpublishedContent(t.Context(), p.pool, p.files, input)
			require.Error(t, err)
			require.Equal(t, before, reclaimSnapshot(t, p))
			require.FileExists(t, filepath.Join(p.root, p.reservation.ObjectName))
		})
	}
}
func TestUnpublishedReclaimPGDatabaseFailureReentryAndConcurrentOnce(t *testing.T) {
	p := setupContentPublishPG(t, []byte("interrupted"), 1)
	input := reclaimInput(t, p)
	_, err := p.pool.Exec(t.Context(), `CREATE FUNCTION synthetic_fail_reclaim() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic quota failure'; END $$; CREATE TRIGGER synthetic_reclaim_failure BEFORE UPDATE ON execution_content_policies FOR EACH ROW EXECUTE FUNCTION synthetic_fail_reclaim()`)
	require.NoError(t, err)
	before := reclaimSnapshot(t, p)
	_, err = ReclaimUnpublishedContent(t.Context(), p.pool, p.files, input)
	require.Error(t, err)
	require.Equal(t, before, reclaimSnapshot(t, p))
	require.NoFileExists(t, filepath.Join(p.root, p.reservation.ObjectName))
	_, err = p.pool.Exec(t.Context(), `DROP TRIGGER synthetic_reclaim_failure ON execution_content_policies; DROP FUNCTION synthetic_fail_reclaim()`)
	require.NoError(t, err)
	var wg sync.WaitGroup
	results := make([]UnpublishedReclaimResult, 2)
	errs := make([]error, 2)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = ReclaimUnpublishedContent(context.Background(), p.pool, p.files, input)
		}()
	}
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.EqualValues(t, 128, results[0].ReleasedBytes+results[1].ReleasedBytes)
	expiryCounters(t, p, 1, 0, 0)
}

func TestUnpublishedReclaimPGRestoreRetainsMarkerAndDownRefuses(t *testing.T) {
	p := setupContentPublishPG(t, []byte("interrupted"), 1)
	input := reclaimInput(t, p)
	_, err := ReclaimUnpublishedContent(t.Context(), p.pool, p.files, input)
	require.NoError(t, err)
	object := func() string {
		var row string
		require.NoError(t, p.pool.QueryRow(t.Context(), `SELECT to_jsonb(o)::text FROM execution_content_objects o WHERE content_ref=$1`, input.ContentRef).Scan(&row))
		return fmt.Sprintf("%x", sha256.Sum256([]byte(row)))
	}
	before := object()
	helper, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), "..", "scripts", "source-budget-restore-rebase.sql"))
	require.NoError(t, err)
	_, err = p.pool.Exec(t.Context(), string(helper))
	require.NoError(t, err)
	require.Equal(t, before, object())
	down, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), schemaauthority.ContentReclaimExpandHead+".down.sql"))
	require.NoError(t, err)
	conn, err := p.pool.Acquire(t.Context())
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), string(down))
	require.ErrorContains(t, err, "unsafe down refused")
	_, err = conn.Exec(t.Context(), "ROLLBACK")
	require.NoError(t, err)
	conn.Release()
	require.Equal(t, before, object())
	// A later unexpected file must never be removed under an old release marker.
	path := filepath.Join(p.root, p.reservation.ObjectName)
	require.NoError(t, os.WriteFile(path, []byte("unexpected"), 0600))
	_, err = ReclaimUnpublishedContent(t.Context(), p.pool, p.files, input)
	require.Error(t, err)
	require.FileExists(t, path)
	require.Equal(t, before, object())
}
