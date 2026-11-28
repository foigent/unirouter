package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/modelqualification"
	"github.com/uniroute/unirouter/internal/upstreamrelease"
)

type mediaProbeAssetTransport struct{ handler *Handler }

func (t mediaProbeAssetTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.handler.sendModelProbe(req, canonicalImageProbeHTTPClient)
}

type asyncOperationProbeRead struct {
	sourceRef string
	fence     int64
}

type asyncOperationProbeTask struct {
	sourceRef, digest, taskID string
	readUntil, nextRead       time.Time
	result                    []byte
	fence                     int64
	operationRevision         int64
}

func asyncOperationProbeProtocol(snapshot canonicalOperationProbeSnapshot) endpointprotocol.AsyncMediaProfile {
	profile, err := endpointprotocol.NewRegistry().Resolve(snapshot.ProfileID, snapshot.ProfileVersion)
	if err != nil {
		return nil
	}
	async, _ := profile.(endpointprotocol.AsyncMediaProfile)
	return async
}

func validAsyncOperationProbeRef(ref string) bool {
	const prefix = "op_"
	if len(ref) != len(prefix)+32 || !strings.HasPrefix(ref, prefix) || ref != strings.ToLower(ref) {
		return false
	}
	_, err := hex.DecodeString(ref[len(prefix):])
	return err == nil
}

func asyncOperationProbeDigest(s canonicalOperationProbeSnapshot) string {
	wire, _ := json.Marshal([]any{s.CustomerOrgID, s.DeploymentID, s.ConnectionID, s.OperationID,
		s.ConnectionRevision, s.DeploymentRevision, s.OperationRevision, s.CredentialBindingID, s.CredentialRevision,
		s.ProfileID, s.ProfileVersion, s.ProfileDefinitionDigest, s.BaseURL, s.UpstreamModel,
		s.LinkCreatedAt.UTC(), upstreamrelease.Identity(s.UpstreamObservation)})
	return fmt.Sprintf("%x", sha256.Sum256(wire))
}

func (h *Handler) mediaProbeTime() time.Time {
	if h.asyncMediaProbeNow != nil {
		return h.asyncMediaProbeNow().UTC()
	}
	return time.Now().UTC()
}

func pendingAsyncOperationProbe(s canonicalOperationProbeSnapshot, ref, reason string) canonicalOperationProbeResponse {
	return canonicalOperationProbeResponse{DeploymentID: s.DeploymentID, OperationID: s.OperationID,
		CredentialBindingID: s.CredentialBindingID, Current: true, Pending: true, ProbeRunRef: ref,
		ReasonCode: reason, ProviderReasonCode: reason}
}

func (h *Handler) claimAsyncOperationProbe(ctx context.Context, s canonicalOperationProbeSnapshot,
	sourceRef, resumeRef string, discovery bool, now time.Time) (task asyncOperationProbeTask, fresh bool, err error) {
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return task, false, err
	}
	defer tx.Rollback(ctx)
	// Lock the exact operation row while classifying or starting its existing
	// probe. The transaction is committed before any provider HTTP call.
	var revision int64
	err = tx.QueryRow(ctx, `SELECT revision FROM model_deployment_operations
		WHERE deployment_id=$1 AND operation_id=$2 FOR UPDATE`, s.DeploymentID, s.OperationID).Scan(&revision)
	if err != nil {
		return task, false, err
	}
	if revision != s.OperationRevision {
		return task, false, errCanonicalOperationProbeConflict
	}
	identity := asyncOperationProbeDigest(s)
	query := `SELECT source_ref,identity_digest,COALESCE(task_id,''),read_until,next_read_at,read_fence,operation_revision,result
		  FROM model_deployment_async_probe_tasks
		 WHERE org_id=$1 AND deployment_id=$2 AND operation_id=$3
		   AND (operation_revision=$4 OR ($5<>'' AND source_ref=$5))
		 ORDER BY (source_ref=$5) DESC LIMIT 1 FOR UPDATE`
	err = tx.QueryRow(ctx, query, s.CustomerOrgID, s.DeploymentID, s.OperationID, s.OperationRevision, resumeRef).Scan(
		&task.sourceRef, &task.digest, &task.taskID, &task.readUntil, &task.nextRead, &task.fence, &task.operationRevision, &task.result)
	if errors.Is(err, pgx.ErrNoRows) {
		if resumeRef != "" {
			return task, false, errCanonicalOperationProbeConflict
		}
		if s.OperationEvidenceState == "live_probed" {
			// Advancing evidence in our own transaction does not create a
			// fresh paid intent when an operator retries the same button.
			previousRevision := s.OperationRevision - 1
			pinned := s
			pinned.OperationRevision = previousRevision
			err = tx.QueryRow(ctx, `SELECT result FROM model_deployment_async_probe_tasks
				WHERE org_id=$1 AND deployment_id=$2 AND operation_id=$3
				  AND operation_revision=$4 AND identity_digest=$5 AND result IS NOT NULL`,
				s.CustomerOrgID, s.DeploymentID, s.OperationID, previousRevision, asyncOperationProbeDigest(pinned)).Scan(&task.result)
			if err == nil {
				return task, false, tx.Commit(ctx)
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return task, false, err
			}
		}
		if discovery {
			return task, false, errCanonicalOperationProbeNotFound
		}
		var previous bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM model_deployment_async_probe_tasks
			WHERE org_id=$1 AND deployment_id=$2 AND operation_id=$3 AND result IS NULL AND read_until>$4)`,
			s.CustomerOrgID, s.DeploymentID, s.OperationID, now).Scan(&previous)
		if err != nil {
			return task, false, err
		}
		if previous {
			return task, false, errCanonicalOperationProbeConflict
		}
		task = asyncOperationProbeTask{sourceRef: sourceRef, digest: identity, readUntil: now.Add(endpointprotocol.MediaTaskReadWindow)}
		_, err = tx.Exec(ctx, `INSERT INTO model_deployment_async_probe_tasks
			(org_id,deployment_id,operation_id,operation_revision,source_ref,identity_digest,read_until,next_read_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, s.CustomerOrgID, s.DeploymentID, s.OperationID, s.OperationRevision, sourceRef, identity, task.readUntil, now)
		if err != nil {
			return task, false, err
		}
		return task, true, tx.Commit(ctx)
	}
	if err != nil {
		return task, false, err
	}
	if resumeRef != "" && task.sourceRef != resumeRef {
		return task, false, errCanonicalOperationProbeConflict
	}
	if task.digest != identity {
		// A completed probe can itself advance the operation revision. An
		// explicit receipt read still verifies every other original identity.
		if len(task.result) == 0 || resumeRef == "" {
			return task, false, errCanonicalOperationProbeConflict
		}
		pinned := s
		pinned.OperationRevision = task.operationRevision
		if task.digest != asyncOperationProbeDigest(pinned) {
			return task, false, errCanonicalOperationProbeConflict
		}
	}
	if len(task.result) == 0 && task.fence >= endpointprotocol.MediaTaskMaxStatusReads {
		task.nextRead = task.readUntil // Keep the receipt available until its actual deadline.
	}
	task.fence = 0
	if resumeRef != "" && len(task.result) == 0 && task.taskID != "" && !now.Before(task.nextRead) && now.Before(task.readUntil) {
		task.nextRead = endpointprotocol.NextMediaTaskStatusRead(now, task.readUntil)
		err = tx.QueryRow(ctx, `UPDATE model_deployment_async_probe_tasks SET read_fence=read_fence+1,next_read_at=$2
			WHERE source_ref=$1 RETURNING read_fence`, task.sourceRef, task.nextRead).Scan(&task.fence)
		if err != nil {
			return task, false, err
		}
	}
	return task, false, tx.Commit(ctx)
}

func (h *Handler) probeAsyncCanonicalOperation(ctx context.Context, s canonicalOperationProbeSnapshot, request canonicalOperationProbeRequest,
	audit auditMeta, sourceRef string, profile endpointprotocol.AsyncMediaProfile) (canonicalOperationProbeResponse, error) {
	if request.ProbeText != "" {
		return canonicalOperationProbeResponse{}, errCanonicalOperationProbeInvalid
	}
	task, fresh, err := h.claimAsyncOperationProbe(ctx, s, sourceRef, request.ResumeProbeRef, request.DiscoverAsyncProbe, h.mediaProbeTime())
	if err != nil {
		return canonicalOperationProbeResponse{}, err
	}
	if len(task.result) != 0 {
		var result canonicalOperationProbeResponse
		err = json.Unmarshal(task.result, &result)
		return result, err
	}
	if !h.mediaProbeTime().Before(task.readUntil) {
		response := pendingAsyncOperationProbe(s, task.sourceRef, "provider_task_read_window_expired")
		response.Pending, response.Current, response.Stale = false, false, true
		return response, nil
	}
	if !fresh && task.fence == 0 {
		reason := "provider_task_not_due"
		if request.DiscoverAsyncProbe && !h.mediaProbeTime().Before(task.nextRead) {
			reason = "provider_task_status_available"
		}
		if task.taskID == "" {
			reason = "provider_task_submission_unconfirmed"
		}
		return pendingAsyncOperationProbe(s, task.sourceRef, reason), nil
	}
	origin, err := modelqualification.NormalizeHTTPSOrigin(s.BaseURL)
	if err != nil {
		return canonicalOperationProbeResponse{}, errCanonicalOperationProbeConflict
	}
	secret, err := auth.DecryptKey(s.EncryptedSecret, h.encKey)
	if err != nil || secret == "" {
		return canonicalOperationProbeResponse{}, errCanonicalOperationProbeConflict
	}
	probeCtx, cancel := context.WithTimeout(ctx, canonicalImageProbeTimeout)
	defer cancel()
	input := endpointprotocol.BuildInput{BaseURL: origin, APIKey: secret, RequestID: task.sourceRef, RequestedModel: s.UpstreamModel, MappedModel: s.UpstreamModel}
	if fresh {
		// The reserved row prevents repeating the POST even if the response or
		// its acknowledgment is lost. Only a confirmed ID can be polled.
		input.Body, err = canonicalOperationProbePayloadForSnapshot(s, "")
		if err != nil {
			return canonicalOperationProbeResponse{}, err
		}
		req, binding, err := profile.BuildRequest(probeCtx, input)
		if err != nil {
			return canonicalOperationProbeResponse{}, err
		}
		status, body, err := h.readAsyncMediaProbeResponse(req)
		if err != nil {
			return pendingAsyncOperationProbe(s, task.sourceRef, "provider_task_submission_unconfirmed"), nil
		}
		outcome, err := profile.ParseResponse(probeCtx, binding, status, body)
		if err != nil || outcome.MediaTaskReference() == "" {
			return pendingAsyncOperationProbe(s, task.sourceRef, "provider_task_submission_unconfirmed"), nil
		}
		// Complete the small receipt write when the caller disconnects after
		// the vendor response. No request or secret is stored in this row.
		saveCtx, saveCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer saveCancel()
		saved, err := h.db.Exec(saveCtx, `UPDATE model_deployment_async_probe_tasks SET task_id=$2,next_read_at=$3
			WHERE source_ref=$1 AND task_id IS NULL`, task.sourceRef, outcome.MediaTaskReference(), h.mediaProbeTime().Add(endpointprotocol.MediaTaskPollInterval))
		if err != nil || saved.RowsAffected() != 1 {
			return canonicalOperationProbeResponse{}, errCanonicalOperationProbeConflict
		}
		return pendingAsyncOperationProbe(s, task.sourceRef, "provider_task_accepted"), nil
	}
	req, err := profile.BuildStatusRequest(probeCtx, input, task.taskID)
	if err != nil {
		return canonicalOperationProbeResponse{}, errCanonicalOperationProbeConflict
	}
	started := time.Now()
	status, body, err := h.readAsyncMediaProbeResponse(req)
	if err != nil {
		return pendingAsyncOperationProbe(s, task.sourceRef, "provider_task_read_unavailable"), nil
	}
	outcome, err := profile.ParseStatusResponse(s.UpstreamModel, task.taskID, status, body)
	if err != nil {
		return pendingAsyncOperationProbe(s, task.sourceRef, "provider_task_read_unverified"), nil
	}
	if outcome.ProviderState == endpointprotocol.MediaTaskPending {
		return pendingAsyncOperationProbe(s, task.sourceRef, "provider_task_pending"), nil
	}
	result := canonicalOperationProbeOutcome{StatusCode: status, ReasonCode: "provider_media_task_failed", ResponseBytes: len(body)}
	if outcome.ProviderState == endpointprotocol.MediaTaskUnavailable {
		result.ReasonCode = "provider_task_unavailable"
	}
	if outcome.ProviderState == endpointprotocol.MediaTaskCompleted {
		assetClient := &http.Client{Transport: mediaProbeAssetTransport{handler: h}}
		if _, err := endpointprotocol.DownloadMP4(probeCtx, assetClient, profile, outcome.ResultURL); err != nil {
			return pendingAsyncOperationProbe(s, task.sourceRef, "provider_media_asset_unverified"), nil
		}
		result.ProviderSuccess, result.ReasonCode, result.OutputCount, result.Units = true, "ok", outcome.OutputCount, outcome.Units
	}
	result.LatencyMS = time.Since(started).Milliseconds()
	return h.persistCanonicalOperationProbe(ctx, s.Actor.UserID, s, result, task.sourceRef, audit, &asyncOperationProbeRead{sourceRef: task.sourceRef, fence: task.fence})
}

func (h *Handler) readAsyncMediaProbeResponse(req *http.Request) (int, []byte, error) {
	res, err := h.sendModelProbe(req, canonicalImageProbeHTTPClient)
	if err != nil {
		return 0, nil, err
	}
	if res == nil || res.Body == nil {
		return 0, nil, endpointprotocol.ErrInvalidResponse
	}
	defer res.Body.Close()
	const maxBytes = 128 << 10
	body, err := io.ReadAll(io.LimitReader(res.Body, maxBytes+1))
	if err != nil || len(body) > maxBytes {
		return 0, nil, endpointprotocol.ErrInvalidResponse
	}
	return res.StatusCode, body, nil
}
