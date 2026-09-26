//go:build integration

package sourcefacts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/contentcapture"
)

type captureServicePG struct {
	pool     *pgxpool.Pool
	service  *ContentCaptureService
	identity ContentCaptureIdentity
	config   contentcapture.PolicyConfig
	root     string
}

func setupCaptureServicePG(t *testing.T, mode string, limit int64, protocols ...string) captureServicePG {
	t.Helper()
	pool, source, org, project := setupContentJournal(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	binding, err := contentcapture.InitializeFileStore(root, "synthetic-capture-service")
	require.NoError(t, err)
	files, err := contentcapture.OpenFileStore(root, binding)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, files.Close()) })
	config := contentcapture.PolicyConfig{Mode: mode, SupportedProtocols: []string{"openai.chat"}, ObservationPoints: []string{"client_request", "provider_request", "provider_response", "client_response"}, AllowedPurposes: []string{}}
	if len(protocols) > 0 {
		config.SupportedProtocols = protocols
	}
	if mode == contentcapture.ModeFullContent {
		retention, quota := int64(3600), limit*8
		config.RetentionSeconds = &retention
		config.QuotaBytes = &quota
		config.MaxSavedBytes = &limit
		config.AllowedPurposes = []string{ContentHumanReview}
		config.OnLimit = "record_capture_gap"
	}
	wire, err := json.Marshal(config)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `INSERT INTO execution_content_policies(org_id,project_id,desired_version,effective_version,desired_config,effective_config,effective_at,application_status,application_reason) VALUES($1,$2,1,1,$3,$3,clock_timestamp(),'applied','none')`, org, project, wire)
	require.NoError(t, err)
	options := ContentCaptureServiceConfig{Policy: contentcapture.PolicyStoreOptions{SourceInstance: source.SourceInstance(), DefaultMode: contentcapture.ModeMetadataOnly, FullContentEnabled: true, SupportedProtocols: config.SupportedProtocols, ObservationPoints: config.ObservationPoints, Limits: contentcapture.PolicyLimits{MaxSavedBytes: limit, MaxReadBytes: limit, MaxBufferBytes: min(limit, 32*1024), MaxManifestBytes: 65536, MaxManifestItems: 100}}, Engine: contentcapture.EngineOptions{Queue: contentcapture.QueueLimits{MaxItems: 1024, MaxBytes: max(limit, 4096), MaxProjectBytes: max(limit, 4096), MaxChunkBytes: min(limit, 32*1024)}, MaxActive: 8, MaxProjectActive: 8, PrepareTimeout: 5 * time.Second, FinalizeTimeout: 5 * time.Second, MaxLifetime: 30 * time.Second}, LeaseOwner: "synthetic-capture-owner", LeaseTTL: time.Minute, StartTimeout: 5 * time.Second, HealthTimeout: 5 * time.Second}
	service, err := NewContentCaptureService(source, files, options)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, service.Close(ctx))
	})
	return captureServicePG{pool, service, ContentCaptureIdentity{OrgID: org, ProjectID: project, RequestID: "synthetic-capture", Protocol: "openai.chat", ObservationPoint: "client_request", MediaType: "application/json; charset=utf-8", OccurredAt: time.Now().UTC()}, config, root}
}

func (p captureServicePG) manifests(t *testing.T) []ContentManifest {
	t.Helper()
	rows, err := p.pool.Query(t.Context(), `SELECT document FROM execution_content_manifests WHERE org_id=$1 AND project_id=$2 ORDER BY sequence`, p.identity.OrgID, p.identity.ProjectID)
	require.NoError(t, err)
	defer rows.Close()
	var values []ContentManifest
	for rows.Next() {
		var wire []byte
		require.NoError(t, rows.Scan(&wire))
		m, err := DecodeContentManifest(wire)
		require.NoError(t, err)
		values = append(values, m)
	}
	require.NoError(t, rows.Err())
	return values
}

func waitCaptureService(t *testing.T, c *contentcapture.Capture) contentcapture.CaptureResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	result, err := c.Wait(ctx)
	require.NoError(t, err)
	return result
}

func (p captureServicePG) counts(t *testing.T, objects, reserved, stored, manifests, health int64) {
	t.Helper()
	var gotObjects, gotReserved, gotStored, gotManifests, gotHealth int64
	require.NoError(t, p.pool.QueryRow(t.Context(), `SELECT reserved_bytes,stored_bytes,(SELECT count(*) FROM execution_content_objects WHERE org_id=$1 AND project_id=$2),(SELECT count(*) FROM execution_content_manifests WHERE org_id=$1 AND project_id=$2),(SELECT count(*) FROM execution_content_health WHERE org_id=$1 AND project_id=$2) FROM execution_content_policies WHERE org_id=$1 AND project_id=$2`, p.identity.OrgID, p.identity.ProjectID).Scan(&gotReserved, &gotStored, &gotObjects, &gotManifests, &gotHealth))
	require.Equal(t, []int64{objects, reserved, stored, manifests, health}, []int64{gotObjects, gotReserved, gotStored, gotManifests, gotHealth})
}

func TestContentCaptureServicePGTransitions(t *testing.T) {
	for _, kind := range []string{"zero", "binary", "limit", "cancel_zero", "disable_after_start", "large"} {
		t.Run(kind, func(t *testing.T) {
			limit := int64(128)
			if kind == "large" {
				limit = 17*1024*1024 + 1
			}
			p := setupCaptureServicePG(t, contentcapture.ModeFullContent, limit)
			c, err := p.service.Begin(t.Context(), p.identity)
			require.NoError(t, err)
			require.NotNil(t, c)
			before := p.manifests(t)
			require.Len(t, before, 1)
			require.Equal(t, "pending", before[0].Capture.Completeness)
			p.counts(t, 1, limit, 0, 1, 0)
			body := []byte{0, 255, 1, 10}
			reason := "none"
			switch kind {
			case "zero":
				body = []byte{}
			case "cancel_zero":
				body = nil
				reason = "request_cancelled"
			case "limit":
				body = bytes.Repeat([]byte{9}, int(limit)+1)
			case "disable_after_start":
				config := contentcapture.PolicyConfig{Mode: contentcapture.ModeDisabled, SupportedProtocols: p.config.SupportedProtocols, ObservationPoints: p.config.ObservationPoints, AllowedPurposes: []string{}}
				wire, err := json.Marshal(config)
				require.NoError(t, err)
				_, err = p.pool.Exec(t.Context(), `UPDATE execution_content_policies SET desired_version=2,effective_version=2,desired_config=$3,effective_config=$3,effective_at=clock_timestamp() WHERE org_id=$1 AND project_id=$2`, p.identity.OrgID, p.identity.ProjectID, wire)
				require.NoError(t, err)
			case "large":
				body = bytes.Repeat([]byte{255}, int(limit))
			}
			err = c.Offer(t.Context(), body)
			if kind == "limit" {
				require.ErrorIs(t, err, contentcapture.ErrStoreLimit)
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, c.Finish(reason))
			result := waitCaptureService(t, c)
			require.NoError(t, result.FinalizeError)
			values := p.manifests(t)
			require.Len(t, values, 2)
			require.NoError(t, ValidateContentPredecessor(values[1], values[0]))
			require.Equal(t, values[0].SourceFactID, values[1].SourceFactID)
			require.Equal(t, values[0].OccurredAt, values[1].OccurredAt)
			require.Equal(t, "1", values[1].Capture.PolicyVersion)
			if kind == "cancel_zero" {
				require.Equal(t, "not_captured", values[1].Capture.Completeness)
				require.Equal(t, "capture_unavailable", values[1].Capture.Reason)
				require.Empty(t, values[1].ContentRef)
				p.counts(t, 1, limit, 0, 2, 0)
				return
			}
			expected := body
			if kind == "limit" {
				expected = body[:limit]
				require.Equal(t, "partial", values[1].Capture.Completeness)
			} else {
				require.Equal(t, "complete", values[1].Capture.Completeness)
			}
			require.NotNil(t, result.Outcome.Receipt)
			f, err := p.service.files.OpenVerified(t.Context(), *result.Outcome.Receipt, contentcapture.StoreLimits{MaxSavedBytes: limit, MaxReadBytes: limit, MaxBufferBytes: min(limit, 32*1024)})
			require.NoError(t, err)
			got, err := io.ReadAll(f)
			require.NoError(t, err)
			require.NoError(t, f.Close())
			require.Equal(t, expected, got)
			p.counts(t, 1, 0, int64(len(expected)), 2, 0)
		})
	}
}

func TestContentCaptureServicePGNoBodyModesAndQuota(t *testing.T) {
	for _, mode := range []string{contentcapture.ModeDisabled, contentcapture.ModeMetadataOnly, "quota", "unsupported", "scope"} {
		t.Run(mode, func(t *testing.T) {
			policyMode := mode
			if mode == "quota" || mode == "unsupported" || mode == "scope" {
				policyMode = contentcapture.ModeFullContent
			}
			p := setupCaptureServicePG(t, policyMode, 128)
			if mode == "quota" {
				_, err := p.pool.Exec(t.Context(), `UPDATE execution_content_policies SET stored_bytes=1024 WHERE org_id=$1 AND project_id=$2`, p.identity.OrgID, p.identity.ProjectID)
				require.NoError(t, err)
			}
			if mode == "unsupported" {
				p.identity.Protocol = "unsupported.protocol"
			}
			if mode == "scope" {
				p.config.ObservationPoints = []string{"client_response"}
				wire, err := json.Marshal(p.config)
				require.NoError(t, err)
				_, err = p.pool.Exec(t.Context(), `UPDATE execution_content_policies SET desired_config=$3,effective_config=$3 WHERE org_id=$1 AND project_id=$2`, p.identity.OrgID, p.identity.ProjectID, wire)
				require.NoError(t, err)
			}
			c, err := p.service.Begin(t.Context(), p.identity)
			if mode == "quota" {
				require.ErrorIs(t, err, contentcapture.ErrReservationQuota)
				result := waitCaptureService(t, c)
				require.NoError(t, result.FinalizeError)
			} else {
				require.NoError(t, err)
				require.Nil(t, c)
			}
			values := p.manifests(t)
			require.Len(t, values, 1)
			require.Equal(t, "not_captured", values[0].Capture.Completeness)
			require.Empty(t, values[0].ContentRef)
			stored := int64(0)
			if mode == "quota" {
				stored = 1024
				require.Equal(t, "limit_exceeded", values[0].Capture.Reason)
			}
			p.counts(t, 0, 0, stored, 1, 0)
		})
	}
}

// Real PostgreSQL commits occur before the injected lost response. This tests
// the adapter's ambiguity boundary, not an actual network fault injector.
func TestContentCaptureServicePGLostCommitRepliesNeverRetry(t *testing.T) {
	for _, stage := range []string{"reserve", "pending", "publish"} {
		t.Run(stage, func(t *testing.T) {
			p := setupCaptureServicePG(t, contentcapture.ModeFullContent, 128)
			lost := errors.New("synthetic response lost after actual commit")
			var calls atomic.Int32
			switch stage {
			case "reserve":
				original := p.service.reserve
				p.service.reserve = func(ctx context.Context, f *contentcapture.FileStore, r contentcapture.ReservationRequest) (contentcapture.Reservation, error) {
					calls.Add(1)
					v, err := original(ctx, f, r)
					if err == nil {
						return contentcapture.Reservation{}, lost
					}
					return v, err
				}
			case "pending":
				original := p.service.appendManifest
				p.service.appendManifest = func(ctx context.Context, m ContentManifest, e int64) (ContentAppendResult, error) {
					calls.Add(1)
					v, err := original(ctx, m, e)
					if err == nil {
						return ContentAppendResult{}, lost
					}
					return v, err
				}
			case "publish":
				original := p.service.publish
				p.service.publish = func(ctx context.Context, f *contentcapture.FileStore, r contentcapture.Reservation, v contentcapture.StoredReceipt, m ContentManifest, l contentcapture.StoreLimits) (ContentAppendResult, error) {
					calls.Add(1)
					out, err := original(ctx, f, r, v, m, l)
					if err == nil {
						return ContentAppendResult{}, lost
					}
					return out, err
				}
			}
			c, err := p.service.Begin(t.Context(), p.identity)
			require.NotNil(t, c)
			if stage == "publish" {
				require.NoError(t, err)
				require.NoError(t, c.Offer(t.Context(), []byte("abc")))
				require.NoError(t, c.Finish("none"))
			} else {
				require.ErrorIs(t, err, lost)
			}
			result := waitCaptureService(t, c)
			require.ErrorIs(t, result.FinalizeError, lost)
			require.EqualValues(t, 1, calls.Load())
			count, reserved, stored := int64(0), int64(128), int64(0)
			if stage == "pending" {
				count = 1
			}
			if stage == "publish" {
				count = 2
				reserved = 0
				stored = 3
			}
			p.counts(t, 1, reserved, stored, count, 1)
			var wire []byte
			require.NoError(t, p.pool.QueryRow(t.Context(), `SELECT detail FROM execution_content_health WHERE org_id=$1 AND project_id=$2`, p.identity.OrgID, p.identity.ProjectID).Scan(&wire))
			require.NotContains(t, string(wire), lost.Error())
			var fields map[string]any
			require.NoError(t, json.Unmarshal(wire, &fields))
			require.Len(t, fields, 3)
			var health contentCaptureHealth
			require.NoError(t, json.Unmarshal(wire, &health))
			require.Equal(t, "1", health.RecoveryEpoch)
		})
	}
}

func TestContentCaptureServicePGConflictAndEpoch(t *testing.T) {
	t.Run("same_fact", func(t *testing.T) {
		p := setupCaptureServicePG(t, contentcapture.ModeFullContent, 128)
		type started struct {
			c   *contentcapture.Capture
			err error
		}
		out := make(chan started, 2)
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); c, err := p.service.Begin(t.Context(), p.identity); out <- started{c, err} }()
		}
		wg.Wait()
		close(out)
		var succeeded, conflicted int
		for v := range out {
			require.NotNil(t, v.c)
			if v.err == nil {
				succeeded++
				require.NoError(t, v.c.Finish("none"))
				require.NoError(t, waitCaptureService(t, v.c).FinalizeError)
			} else {
				conflicted++
				require.ErrorIs(t, v.err, contentcapture.ErrReservationConflict)
				require.ErrorIs(t, waitCaptureService(t, v.c).FinalizeError, contentcapture.ErrReservationConflict)
			}
		}
		require.Equal(t, 1, succeeded)
		require.Equal(t, 1, conflicted)
		p.counts(t, 1, 0, 0, 2, 1)
	})
	t.Run("epoch", func(t *testing.T) {
		p := setupCaptureServicePG(t, contentcapture.ModeFullContent, 128)
		c, err := p.service.Begin(t.Context(), p.identity)
		require.NoError(t, err)
		_, err = p.pool.Exec(t.Context(), `UPDATE execution_content_streams SET recovery_epoch=2 WHERE org_id=$1 AND project_id=$2`, p.identity.OrgID, p.identity.ProjectID)
		require.NoError(t, err)
		require.NoError(t, c.Offer(t.Context(), []byte("saved")))
		require.NoError(t, c.Finish("none"))
		result := waitCaptureService(t, c)
		require.ErrorIs(t, result.FinalizeError, ErrContentEpochChanged)
		require.NotNil(t, result.Outcome.Receipt)
		p.counts(t, 1, 128, 0, 1, 0)
	})
}

func TestContentCaptureServicePGHealthReadAndFailure(t *testing.T) {
	p := setupCaptureServicePG(t, contentcapture.ModeFullContent, 128)
	m, err := captureInitialManifest(p.identity, mustCapturePolicy(t, p))
	require.NoError(t, err)
	require.NoError(t, p.service.recordCaptureHealth(m, 1))
	for _, epoch := range []int64{1, 2} {
		tx, err := p.pool.Begin(t.Context())
		require.NoError(t, err)
		known, err := contentKnownHealthGap(t.Context(), tx, p.identity.OrgID, p.identity.ProjectID, epoch)
		require.NoError(t, err)
		require.Equal(t, epoch == 1, known)
		require.NoError(t, tx.Rollback(t.Context()))
	}
	_, err = p.pool.Exec(t.Context(), `UPDATE execution_content_health SET detail=$3 WHERE org_id=$1 AND project_id=$2`, p.identity.OrgID, p.identity.ProjectID, []byte("malformed"))
	require.NoError(t, err)
	_, err = p.pool.Exec(t.Context(), `CREATE FUNCTION synthetic_capture_health_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic health failure'; END $$; CREATE TRIGGER synthetic_capture_health_failure BEFORE INSERT OR UPDATE ON execution_content_health FOR EACH ROW EXECUTE FUNCTION synthetic_capture_health_failure()`)
	require.NoError(t, err)
	require.Error(t, p.service.recordCaptureHealth(m, 1))
	var count int
	require.NoError(t, p.pool.QueryRow(t.Context(), `SELECT count(*) FROM execution_content_health WHERE org_id=$1 AND project_id=$2`, p.identity.OrgID, p.identity.ProjectID).Scan(&count))
	require.Equal(t, 1, count)
	tx, err := p.pool.Begin(t.Context())
	require.NoError(t, err)
	known, err := contentKnownHealthGap(t.Context(), tx, p.identity.OrgID, p.identity.ProjectID, 1)
	require.NoError(t, err)
	require.False(t, known, "failed health write cannot certify the malformed prior state")
	require.NoError(t, tx.Rollback(t.Context()))
}

func mustCapturePolicy(t *testing.T, p captureServicePG) contentcapture.PolicyView {
	t.Helper()
	v, err := p.service.policy.Get(t.Context(), p.identity.OrgID, p.identity.ProjectID, "ready")
	require.NoError(t, err)
	return v
}

func TestContentCaptureServicePGQueueAndStorageGaps(t *testing.T) {
	t.Run("active_capacity", func(t *testing.T) {
		p := setupCaptureServicePG(t, contentcapture.ModeFullContent, 128)
		var captures []*contentcapture.Capture
		for i := 0; i < 8; i++ {
			id := p.identity
			id.RequestID = fmt.Sprintf("synthetic-capacity-%d", i)
			c, err := p.service.Begin(t.Context(), id)
			require.NoError(t, err)
			captures = append(captures, c)
		}
		c, err := p.service.Begin(t.Context(), p.identity)
		require.Nil(t, c)
		require.ErrorIs(t, err, contentcapture.ErrCaptureCapacity)
		for _, c := range captures {
			require.NoError(t, c.Finish("none"))
			require.NoError(t, waitCaptureService(t, c).FinalizeError)
		}
		p.counts(t, 8, 0, 0, 17, 0)
		var gap int
		require.NoError(t, p.pool.QueryRow(t.Context(), `SELECT count(*) FROM execution_content_manifests WHERE org_id=$1 AND project_id=$2 AND request_id=$3`, p.identity.OrgID, p.identity.ProjectID, p.identity.RequestID).Scan(&gap))
		require.Equal(t, 1, gap)
	})
	t.Run("file_failure", func(t *testing.T) {
		p := setupCaptureServicePG(t, contentcapture.ModeFullContent, 128)
		c, err := p.service.Begin(t.Context(), p.identity)
		require.NoError(t, err)
		var name string
		require.NoError(t, p.pool.QueryRow(t.Context(), `SELECT relative_path FROM execution_content_objects WHERE org_id=$1 AND project_id=$2`, p.identity.OrgID, p.identity.ProjectID).Scan(&name))
		require.NoError(t, os.WriteFile(filepath.Join(p.root, name), []byte("preexisting-test-object"), 0600))
		require.NoError(t, c.Offer(t.Context(), []byte("new content")))
		require.NoError(t, c.Finish("none"))
		result := waitCaptureService(t, c)
		require.NoError(t, result.FinalizeError)
		require.Error(t, result.Outcome.StorageError)
		values := p.manifests(t)
		require.Len(t, values, 2)
		require.Equal(t, "not_captured", values[1].Capture.Completeness)
		require.Equal(t, "storage_write_failed", values[1].Capture.Reason)
		require.Empty(t, values[1].ContentRef)
		p.counts(t, 1, 128, 0, 2, 0)
	})
	t.Run("pending_rollback", func(t *testing.T) {
		p := setupCaptureServicePG(t, contentcapture.ModeFullContent, 128)
		_, err := p.pool.Exec(t.Context(), `CREATE FUNCTION synthetic_pending_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic pending failure'; END $$; CREATE TRIGGER synthetic_pending_failure BEFORE INSERT ON execution_content_manifests FOR EACH ROW EXECUTE FUNCTION synthetic_pending_failure()`)
		require.NoError(t, err)
		c, err := p.service.Begin(t.Context(), p.identity)
		require.Error(t, err)
		require.NotNil(t, c)
		require.Error(t, waitCaptureService(t, c).FinalizeError)
		p.counts(t, 1, 128, 0, 0, 1)
	})
}
