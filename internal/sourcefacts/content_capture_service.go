package sourcefacts

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"mime"
	"slices"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/modeloperation"
)

type ContentCaptureIdentity = contentcapture.Observation

type ContentCaptureServiceConfig struct {
	Policy                                contentcapture.PolicyStoreOptions
	Engine                                contentcapture.EngineOptions
	LeaseOwner                            string
	LeaseTTL, StartTimeout, HealthTimeout time.Duration
}

// ContentCaptureService is internal capture authority for authenticated public
// callers. Its methods must be called outside financial transactions. It does
// not grant source credentials, issue reads, retry a request, or release quota.
type ContentCaptureService struct {
	source *Service
	pool   *pgxpool.Pool
	policy *contentcapture.PolicyStore
	files  *contentcapture.FileStore
	engine *contentcapture.Engine
	config ContentCaptureServiceConfig
	// Fixed production functions; package tests may simulate lost replies after
	// these real transactions commit. No public injection/configuration surface.
	reserve        func(context.Context, *contentcapture.FileStore, contentcapture.ReservationRequest) (contentcapture.Reservation, error)
	appendManifest func(context.Context, ContentManifest, int64) (ContentAppendResult, error)
	publish        func(context.Context, *contentcapture.FileStore, contentcapture.Reservation, contentcapture.StoredReceipt, ContentManifest, contentcapture.StoreLimits) (ContentAppendResult, error)
}

func NewContentCaptureService(source *Service, files *contentcapture.FileStore, c ContentCaptureServiceConfig) (*ContentCaptureService, error) {
	if source == nil || !source.ContentEnabled() || c.Policy.SourceInstance != source.SourceInstance() || !contentText(c.LeaseOwner, 128) || c.LeaseTTL <= 0 || c.StartTimeout <= 0 || c.HealthTimeout <= 0 {
		return nil, errors.New("content capture requires enabled source and finite owner configuration")
	}
	pool, ok := source.db.(*pgxpool.Pool)
	if !ok || pool == nil {
		return nil, ErrContentJournalTransaction
	}
	policy, err := contentcapture.NewPolicyStore(pool, c.Policy)
	if err != nil {
		return nil, err
	}
	s := &ContentCaptureService{source: source, pool: pool, policy: policy, files: files, config: c, reserve: policy.Reserve, appendManifest: source.AppendContentManifest, publish: source.PublishReservedContent}
	if files != nil {
		s.engine, err = contentcapture.NewEngine(files, c.Engine)
		if err != nil {
			return nil, err
		}
	}
	if c.Policy.FullContentEnabled && s.engine == nil {
		return nil, errors.New("full content capture requires verified storage and engine")
	}
	return s, nil
}

func (s *ContentCaptureService) PolicyStore() *contentcapture.PolicyStore {
	if s == nil {
		return nil
	}
	return s.policy
}

// CachePersistenceAllowed is a fresh, authenticated policy read for the exact
// response cache. Cache hits and writes are optional; an unavailable policy
// observation therefore safely disables the optimization for this request.
func (s *ContentCaptureService) CachePersistenceAllowed(ctx context.Context, principal contentcapture.CapturePrincipal, orgID, projectID int64) (bool, error) {
	if s == nil || s.policy == nil {
		return false, ErrContentJournalDisabled
	}
	ready, err := s.Readiness(ctx)
	if err != nil {
		return false, err
	}
	view, scoped, err := s.policy.ViewForCapture(ctx, principal, orgID, projectID, ready.StorageStatus)
	if err != nil {
		return false, err
	}
	if scoped != nil && scoped.ExecutionStorageMode != contentcapture.ExecutionStorageTemporary {
		return false, nil
	}
	config := view.Effective.Config
	return config.Mode == contentcapture.ModeFullContent &&
		slices.Contains(config.SupportedProtocols, string(modeloperation.IDChatCompletions)) &&
		slices.Contains(config.ObservationPoints, "provider_response") &&
		slices.Contains(config.ObservationPoints, "client_response"), nil
}
func (s *ContentCaptureService) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}
	return s.engine.Close(ctx)
}

func (s *ContentCaptureService) Readiness(ctx context.Context) (contentcapture.PolicyApplication, error) {
	status := "not_configured"
	if s != nil && s.files != nil {
		status = "ready"
		if s.files.CheckReady(ctx) != nil {
			status = "unavailable"
		}
	}
	return contentcapture.PolicyApplication{StorageStatus: status, ApplyNow: status == "ready", Now: time.Now().UTC()}, nil
}

// Begin freezes one observation's actual identity and effective policy. A nil
// Capture with nil error means an accurate not_captured manifest was published.
// Callers ignore errors for request execution; capture failures never authorize
// a provider retry or replace the existing HTTP/financial result.
func (s *ContentCaptureService) Begin(ctx context.Context, id ContentCaptureIdentity) (*contentcapture.Capture, error) {
	if s == nil || !s.source.ContentEnabled() {
		return nil, ErrContentJournalDisabled
	}
	if ctx == nil {
		return nil, errors.New("content capture context is required")
	}
	ctx, cancel := context.WithTimeout(ctx, s.config.StartTimeout)
	defer cancel()
	ready, _ := s.Readiness(ctx)
	view, scoped, err := s.policy.ViewForCapture(ctx, id.Principal, id.OrgID, id.ProjectID, ready.StorageStatus)
	if err != nil {
		return nil, err
	}
	if view.SourceInstance != s.source.SourceInstance() {
		return nil, ErrContentJournalScope
	}
	m, err := captureInitialManifest(id, view)
	if err != nil {
		return nil, err
	}
	config := view.Effective.Config
	reason := ""
	switch config.Mode {
	case contentcapture.ModeDisabled:
		reason = "policy_disabled"
	case contentcapture.ModeMetadataOnly:
		reason = "policy_metadata_only"
	default:
		if !slices.Contains(config.SupportedProtocols, id.Protocol) {
			reason = "unsupported_protocol"
		} else if !slices.Contains(config.ObservationPoints, id.ObservationPoint) {
			reason = "scope_excluded"
		} else if s.engine == nil || ready.StorageStatus != "ready" {
			reason = "capture_unavailable"
		}
	}
	if reason != "" {
		epoch, err := s.initialGapEpoch(ctx, id.OrgID, id.ProjectID)
		if err != nil {
			return nil, err
		}
		return nil, s.writeCaptureGap(m, epoch, reason)
	}
	epoch, err := s.ensureStream(ctx, id.OrgID, id.ProjectID)
	if err != nil {
		return nil, err
	}
	capture, startErr := s.engine.Start(ctx, id.ProjectID, func(prepareCtx context.Context) (contentcapture.CaptureJob, error) {
		prepared := &preparedContentCapture{service: s, initial: m, epoch: epoch, mediaType: captureMediaType(id.MediaType)}
		job := contentcapture.CaptureJob{Finalize: prepared.finish}
		var nonce [32]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			prepared.safeGap = true
			return job, err
		}
		leaseTTL := s.config.LeaseTTL
		if *config.RetentionSeconds <= int64(leaseTTL/time.Second) {
			leaseTTL = time.Duration(*config.RetentionSeconds) * time.Second
		}
		input := contentcapture.ReservationRequest{OrgID: id.OrgID, ProjectID: id.ProjectID, ContentRef: "uc1_" + hex.EncodeToString(nonce[:]), SourceFactID: m.SourceFactID, Revision: 2, ExpectedPolicyVersion: view.Effective.PolicyVersion, ExpectedRecoveryEpoch: epoch, Protocol: id.Protocol, ObservationPoint: id.ObservationPoint, MaxSavedBytes: *config.MaxSavedBytes, LeaseOwner: s.config.LeaseOwner, LeaseTTL: leaseTTL}
		if scoped != nil {
			input.Principal = id.Principal
			input.ExpectedPolicyRef = scoped.PolicyRef
			input.ExpectedPolicyVector = scoped.Vector
		}
		reservation, err := s.reserve(prepareCtx, s.files, input)
		if err != nil {
			prepared.safeGap = knownCaptureRejection(err)
			prepared.rejectReason = captureRejectionReason(err)
			return job, err
		}
		prepared.reservation = reservation
		prepared.initial.Capture = capturePolicy(reservation.Effective, reservation.EffectiveAt, view.PolicyRef)
		limits := contentcapture.StoreLimits{MaxSavedBytes: reservation.ReservedBytes, MaxReadBytes: view.Limits.MaxReadBytes, MaxBufferBytes: view.Limits.MaxBufferBytes}
		prepared.limits = limits
		job.Reservation = reservation
		job.Limits = limits
		pending, err := s.appendManifest(prepareCtx, prepared.initial, epoch)
		if err != nil {
			return job, err
		}
		prepared.pendingHash = pending.SourceSHA256
		return job, nil
	})
	if capture == nil && startErr != nil {
		// No prepare callback ran, so the adapter still owns this precise gap.
		reason := "capture_unavailable"
		if errors.Is(startErr, contentcapture.ErrCaptureCapacity) {
			reason = "queue_full"
		}
		return nil, errors.Join(startErr, s.writeCaptureGap(m, epoch, reason))
	}
	return capture, startErr
}

func captureInitialManifest(id ContentCaptureIdentity, v contentcapture.PolicyView) (ContentManifest, error) {
	m := ContentManifest{SchemaVersion: ContentManifestVersion, SourceInstance: v.SourceInstance, OrgID: strconv.FormatInt(id.OrgID, 10), ProjectID: strconv.FormatInt(id.ProjectID, 10), RequestID: id.RequestID, Profile: ContentProfile, Revision: "1", OccurredAt: contentReadTime(id.OccurredAt), RecordedAt: contentReadTime(time.Now()), Protocol: id.Protocol, ObservationPoint: id.ObservationPoint, AttemptID: id.AttemptID, ContentItemID: "body-0", Capture: capturePolicy(v.Effective, v.EffectiveAt, v.PolicyRef)}
	if id.OccurredAt.IsZero() || (oneOf(id.ObservationPoint, "client_request", "client_response") && id.AttemptID != "") {
		return m, errors.New("invalid content observation identity")
	}
	// Disabled and metadata records cannot claim an in-progress body.
	if v.Effective.Config.Mode == contentcapture.ModeDisabled {
		m.Capture.Completeness = "not_captured"
		m.Capture.Reason = "policy_disabled"
	}
	if v.Effective.Config.Mode == contentcapture.ModeMetadataOnly {
		m.Capture.Completeness = "not_captured"
		m.Capture.Reason = "policy_metadata_only"
	}
	m.SourceFactID = m.FactID()
	m.EventID = m.EventIDForRevision()
	return m, m.Validate()
}

func capturePolicy(v contentcapture.PolicyVersion, effectiveAt, ref string) ContentCapture {
	return ContentCapture{Mode: v.Config.Mode, PolicyRef: ref, PolicyVersion: v.PolicyVersion, PolicyEffectiveAt: effectiveAt, Completeness: "pending", Reason: "capture_in_progress"}
}

func captureMediaType(value string) string {
	media, _, err := mime.ParseMediaType(value)
	if err != nil || len(media) > 255 || !contentMediaPattern.MatchString(media) {
		return "application/octet-stream"
	}
	return media
}

func knownCaptureRejection(err error) bool {
	return errors.Is(err, contentcapture.ErrReservationQuota) || errors.Is(err, contentcapture.ErrPolicyInactive) || errors.Is(err, contentcapture.ErrPolicyVersionConflict) || errors.Is(err, contentcapture.ErrReservationScope) || errors.Is(err, contentcapture.ErrReservationUnconfigured) || errors.Is(err, contentcapture.ErrStoreBinding)
}

func captureRejectionReason(err error) string {
	if errors.Is(err, contentcapture.ErrReservationQuota) {
		return "limit_exceeded"
	}
	return "capture_unavailable"
}

type preparedContentCapture struct {
	service                              *ContentCaptureService
	initial                              ContentManifest
	epoch                                int64
	reservation                          contentcapture.Reservation
	limits                               contentcapture.StoreLimits
	pendingHash, mediaType, rejectReason string
	safeGap                              bool
}

func (p *preparedContentCapture) finish(ctx context.Context, o contentcapture.CaptureOutcome) error {
	if p.pendingHash == "" {
		if p.safeGap {
			reason := p.rejectReason
			if reason == "" {
				reason = "capture_unavailable"
			}
			return p.service.writeCaptureGap(p.initial, p.epoch, reason)
		}
		return errors.Join(o.PrepareError, p.service.recordCaptureHealth(p.initial, p.epoch))
	}
	m := p.initial
	m.Revision = "2"
	m.PreviousRevision = "1"
	m.PreviousHash = p.pendingHash
	m.CorrectionReason = "capture_finished"
	m.RecordedAt = contentReadTime(time.Now())
	m.EventID = m.EventIDForRevision()
	m.Capture.Completeness = "not_captured"
	m.Capture.Reason = o.Reason
	var err error
	if o.Receipt != nil {
		m.Capture.Completeness = "partial"
		if o.Reason == "none" {
			m.Capture.Completeness = "complete"
		}
		m.ContentRef = p.reservation.ContentRef
		m.Representation = &ContentRepresentation{MediaType: p.mediaType, Encoding: "identity", ByteDomain: ContentByteDomain, SavedBytes: o.Receipt.SavedBytes, Digest: o.Receipt.SHA256}
		m.CapturedAt = contentReadTime(time.Now())
		m.ExpiresAt = contentReadTime(p.reservation.ExpiresAt)
		_, err = p.service.publish(ctx, p.service.files, p.reservation, *o.Receipt, m, p.limits)
	} else {
		if !oneOf(m.Capture.Reason, "limit_exceeded", "queue_full", "storage_write_failed") {
			m.Capture.Reason = "capture_unavailable"
		}
		_, err = p.service.appendManifest(ctx, m, p.epoch)
	}
	if err != nil {
		return errors.Join(err, p.service.recordCaptureHealth(m, p.epoch))
	}
	return nil
}

func (s *ContentCaptureService) writeCaptureGap(m ContentManifest, epoch int64, reason string) error {
	m.Capture.Completeness = "not_captured"
	m.Capture.Reason = reason
	ctx, cancel := context.WithTimeout(context.Background(), s.config.HealthTimeout)
	defer cancel()
	_, err := s.appendManifest(ctx, m, epoch)
	if err != nil {
		return errors.Join(err, s.recordCaptureHealth(m, epoch))
	}
	return nil
}

// initialGapEpoch freezes the existing stream epoch without a redundant write
// transaction. The independent appender still locks and checks this exact epoch;
// a restore between this read and append is rejected, never silently rebound.
// First use retains the committed initializer so a failed append can still leave
// its durable health gap against the initialized stream, including lost replies.
func (s *ContentCaptureService) initialGapEpoch(ctx context.Context, org, project int64) (int64, error) {
	if !s.source.ContentEnabled() {
		return 0, ErrContentJournalDisabled
	}
	var source string
	var epoch int64
	err := s.pool.QueryRow(ctx, `SELECT source_instance,recovery_epoch FROM execution_content_streams WHERE org_id=$1 AND project_id=$2`, org, project).Scan(&source, &epoch)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.ensureStream(ctx, org, project)
	}
	if err != nil {
		return 0, err
	}
	if source != s.source.SourceInstance() || epoch <= 0 {
		return 0, ErrContentJournalScope
	}
	return epoch, nil
}

func (s *ContentCaptureService) ensureStream(ctx context.Context, org, project int64) (int64, error) {
	if !s.source.ContentEnabled() {
		return 0, ErrContentJournalDisabled
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(context.Background())
	_, err = tx.Exec(ctx, `INSERT INTO execution_content_streams(org_id,project_id,source_instance) VALUES($1,$2,$3) ON CONFLICT(org_id,project_id) DO NOTHING`, org, project, s.source.SourceInstance())
	if err != nil {
		return 0, err
	}
	var source string
	var epoch int64
	err = tx.QueryRow(ctx, `SELECT source_instance,recovery_epoch FROM execution_content_streams WHERE org_id=$1 AND project_id=$2 FOR UPDATE`, org, project).Scan(&source, &epoch)
	if err != nil {
		return 0, err
	}
	if source != s.source.SourceInstance() || epoch <= 0 {
		return 0, ErrContentJournalScope
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return epoch, nil
}
