package sourcefacts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/completedfeed"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/executioncontext"
	"github.com/uniroute/unirouter/internal/schemaauthority"
)

const RetentionWindow = 7 * 24 * time.Hour

type Options struct {
	Enabled        bool
	SourceInstance string
	CursorSecret   string
	I01Enabled     bool
}

type Service struct {
	db        db.DBQuerier
	options   Options
	readSlots chan struct{}
}

func New(database db.DBQuerier, options Options) (*Service, error) {
	if options.Enabled {
		if database == nil || len(options.CursorSecret) < 32 {
			return nil, errors.New("source facts require database and cursor secret")
		}
		if _, e := eventconnector.NewOpaqueString("source_instance", options.SourceInstance, 1, 128, true); e != nil {
			return nil, e
		}
	}
	return &Service{db: database, options: options, readSlots: make(chan struct{}, 8)}, nil
}

func (s *Service) Enabled() bool { return s != nil && s.options.Enabled }

// CheckReady is read-only. Enabling capture on a predecessor schema or under
// a different producer identity is rejected before serving any requests.
func (s *Service) CheckReady(ctx context.Context) error {
	if !s.Enabled() {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var head string
	if e := s.db.QueryRow(ctx, `SELECT COALESCE(MAX(version),'') FROM schema_migrations`).Scan(&head); e != nil {
		return e
	}
	if head != schemaauthority.SourceFactsExpandHead {
		return errors.New("source facts capture requires M116")
	}
	var mismatch bool
	if e := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM execution_source_streams WHERE source_instance<>$1 UNION ALL SELECT 1 FROM execution_source_connections WHERE source_instance<>$1)`, s.options.SourceInstance).Scan(&mismatch); e != nil {
		return e
	}
	if mismatch {
		return errors.New("source facts producer instance changed")
	}
	_, e := descriptors()
	return e
}

func (s *Service) document(org, project int64, request, profile, stage, occurred, recorded string) Document {
	d := Document{SchemaVersion: Version, SourceInstance: s.options.SourceInstance, OrgID: executioncontext.ID(org), ProjectID: executioncontext.ID(project), RequestID: request, Profile: profile, Stage: stage, Revision: "1", OccurredAt: occurred, RecordedAt: recorded, CaptureStatus: "available"}
	d.SourceFactID = d.factID()
	d.EventID = d.eventID()
	return d
}

func (s *Service) contexts(org, project int64, request, stage, occurred, recorded string, snapshot *executioncontext.Snapshot) []Document {
	core := s.document(org, project, request, ContextProfile, stage, occurred, recorded)
	identity := s.document(org, project, request, IdentityProfile, stage, occurred, recorded)
	if snapshot == nil {
		core.CaptureStatus = "not_captured"
		identity.CaptureStatus = "not_captured"
	} else {
		core.Core = &snapshot.Core
		identity.Identity = &snapshot.Identity
	}
	return []Document{core, identity}
}

func (s *Service) CaptureAuthenticated(ctx context.Context, snapshot executioncontext.Snapshot) error {
	if !s.Enabled() {
		return nil
	}
	if e := snapshot.Validate(); e != nil {
		return e
	}
	org, _ := strconv.ParseInt(snapshot.Core.OrgID, 10, 64)
	project, _ := strconv.ParseInt(snapshot.Core.ProjectID, 10, 64)
	tx, e := s.db.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = s.PublishTx(ctx, tx, s.contexts(org, project, snapshot.Core.RequestID, "authenticated", snapshot.Core.CapturedAt, snapshot.Core.CapturedAt, &snapshot)); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func (s *Service) MaterializeReservationTx(ctx context.Context, tx pgx.Tx, req billing.FreezeRequest, basis billing.SourceBasis) error {
	if !s.Enabled() || req.ProjectID <= 0 {
		return nil
	}
	var snapshot *executioncontext.Snapshot
	var contextBytes []byte
	if captured, ok := executioncontext.FromContext(ctx); ok {
		if e := captured.Validate(); e != nil {
			return e
		}
		if captured.Core.RequestID != req.RequestID || captured.Core.OrgID != executioncontext.ID(req.OrgID) || captured.Core.ProjectID != executioncontext.ID(req.ProjectID) || captured.Core.APIKeyID != executioncontext.ID(req.APIKeyID) {
			return errors.New("reservation context binding mismatch")
		}
		snapshot = &captured
		contextBytes, _ = json.Marshal(captured)
	}
	basisBytes, e := json.Marshal(basis)
	if e != nil {
		return e
	}
	if len(basisBytes) > 32768 {
		return errors.New("cost basis exceeds reservation bound")
	}
	var created time.Time
	e = tx.QueryRow(ctx, `UPDATE freeze_idempotency SET source_context=$3,source_cost_basis=$4
	 WHERE org_id=$1 AND request_id=$2 AND source_context IS NULL AND source_cost_basis IS NULL RETURNING created_at`, req.OrgID, req.RequestID, contextBytes, basisBytes).Scan(&created)
	if e != nil {
		return fmt.Errorf("persist reservation source snapshot: %w", e)
	}
	return s.PublishTx(ctx, tx, s.contexts(req.OrgID, req.ProjectID, req.RequestID, "admission", executioncontext.Time(created), executioncontext.Time(created), snapshot))
}

func (s *Service) MaterializeTerminalTx(ctx context.Context, tx pgx.Tx, record billing.TerminalRecord) error {
	if !s.Enabled() || record.ProjectID <= 0 {
		return nil
	}
	var contextBytes, basisBytes []byte
	e := tx.QueryRow(ctx, `SELECT source_context,source_cost_basis FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, record.OrgID, record.RequestID).Scan(&contextBytes, &basisBytes)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	var snapshot *executioncontext.Snapshot
	if len(contextBytes) > 0 {
		var captured executioncontext.Snapshot
		if e = json.Unmarshal(contextBytes, &captured); e != nil {
			return e
		}
		if e = captured.Validate(); e != nil {
			return e
		}
		if captured.Core.RequestID != record.RequestID || captured.Core.OrgID != executioncontext.ID(record.OrgID) || captured.Core.ProjectID != executioncontext.ID(record.ProjectID) {
			return errors.New("terminal context binding mismatch")
		}
		snapshot = &captured
	}
	var basis *billing.SourceBasis
	if len(basisBytes) > 0 {
		basis = &billing.SourceBasis{}
		if e = json.Unmarshal(basisBytes, basis); e != nil {
			return e
		}
	} else {
		basis, e = billing.ReadPersistedSourceBasis(ctx, tx, record.OrgID, record.RequestID)
		if e != nil {
			return e
		}
	}
	_, _, snapshotHash, e := completedfeed.BuildSnapshot(completedfeed.SnapshotInput{
		RequestID: record.RequestID, OrgID: record.OrgID, ProjectID: record.ProjectID, TerminalStatus: record.Status, TerminalAvailableAt: record.OccurredAt.UTC().Truncate(time.Microsecond), Model: record.Model,
		PromptTokens: record.PromptTokens, UncachedPromptTokens: record.UncachedPromptTokens, CacheReadTokens: record.CacheReadTokens, CacheCreationTokens: record.CacheCreationTokens, CompletionTokens: record.CompletionTokens,
		UsageSource: record.UsageSource, UsageAuthority: record.UsageAuthority, UsagePurpose: record.UsagePurpose, UsageFinality: record.UsageFinality, UsageContractVersion: record.UsageContractVersion, UsageEstimateVersion: record.UsageEstimateVersion,
		CacheUsageSchema: record.CacheUsageSchema, CacheUsageStatus: record.CacheUsageStatus, CalculatedCostMicros: record.CalculatedCostMicros, ChargedCostMicros: record.ChargedCostMicros, PricingVersion: record.PricingVersion,
	})
	if e != nil {
		return e
	}
	status := "not_published_by_i01"
	if s.options.I01Enabled {
		var retainedHash string
		if e = tx.QueryRow(ctx, `SELECT snapshot_sha256 FROM completed_execution_feed_items WHERE org_id=$1 AND request_id=$2`, record.OrgID, record.RequestID).Scan(&retainedHash); e != nil {
			return e
		}
		if retainedHash != snapshotHash {
			return errors.New("I01 source hash differs from terminal")
		}
		status = "published_by_i01"
	}
	instant := executioncontext.Time(record.OccurredAt)
	documents := s.contexts(record.OrgID, record.ProjectID, record.RequestID, "terminal", instant, instant, snapshot)
	cost := s.document(record.OrgID, record.ProjectID, record.RequestID, CostProfile, "terminal", instant, instant)
	cost.Cost = &CostDocument{Model: record.Model, TerminalStatus: record.Status, Basis: basis, BasisStatus: "available", JournalBillingMode: record.BillingMode, JournalModeSource: record.BillingModeSource,
		Reference: EconomicReference{Source: "completed_execution", SourceInstance: s.options.SourceInstance, OrgID: cost.OrgID, ProjectID: cost.ProjectID, RequestID: record.RequestID, Revision: "1", SnapshotSHA256: snapshotHash, PublicationStatus: status, CalculatedAmountPath: "money.calculated_cost_micros", ChargedAmountPath: "money.charged_cost_micros"}}
	if basis == nil {
		cost.Cost.BasisStatus = "not_captured"
	} else if len(basisBytes) == 0 {
		cost.Cost.BasisStatus = "persisted_legacy"
	}
	return s.PublishTx(ctx, tx, append(documents, cost))
}

// PublishTx is called only after the business/V1 locks. All profile state locks
// precede sequence allocation, and the caller commits the entire transaction.
func (s *Service) PublishTx(ctx context.Context, tx pgx.Tx, documents []Document) error {
	if !s.Enabled() {
		return nil
	}
	if tx == nil || len(documents) == 0 || len(documents) > 3 {
		return errors.New("invalid source publication transaction")
	}
	documents = slices.Clone(documents)
	slices.SortFunc(documents, func(a, b Document) int { return strings.Compare(a.Profile, b.Profile) })
	encoded := make([][]byte, len(documents))
	first := documents[0]
	for i, d := range documents {
		if d.SourceInstance != s.options.SourceInstance || d.OrgID != first.OrgID || d.ProjectID != first.ProjectID || d.RequestID != first.RequestID || (i > 0 && d.Profile == documents[i-1].Profile) {
			return errors.New("mixed source publication scope")
		}
		var e error
		encoded[i], e = Encode(d)
		if e != nil {
			return e
		}
	}
	// Pipeline the bounded (at most three profiles) read phase, preserving the
	// same SQL lock order. All locks precede the shared publication timestamp
	// and replay/predecessor reads. No transaction or writer authority changes.
	readBatch := &pgx.Batch{}
	eventIDs, predecessorFacts, predecessorRevisions := []string{}, []string{}, []int64{}
	for _, d := range documents {
		readBatch.Queue(`INSERT INTO execution_source_streams(source_instance,org_id,project_id,profile) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, d.SourceInstance, d.OrgID, d.ProjectID, d.Profile)
		readBatch.Queue(`SELECT high_sequence FROM execution_source_streams WHERE source_instance=$1 AND org_id=$2 AND project_id=$3 AND profile=$4 FOR UPDATE`, d.SourceInstance, d.OrgID, d.ProjectID, d.Profile)
		eventIDs = append(eventIDs, d.EventID)
		if d.Revision != "1" {
			previous, _ := strconv.ParseInt(d.PreviousRevision, 10, 64) // Encode validated canonical int64.
			predecessorFacts = append(predecessorFacts, d.SourceFactID)
			predecessorRevisions = append(predecessorRevisions, previous)
		}
	}
	readBatch.Queue(`SELECT clock_timestamp()`)
	readBatch.Queue(`SELECT event_id,source_fact_id,revision,source_hash FROM execution_source_facts
	 WHERE event_id=ANY($1::text[]) OR (source_fact_id,revision) IN
	 (SELECT unnest($2::text[]),unnest($3::bigint[]))`, eventIDs, predecessorFacts, predecessorRevisions)
	var published time.Time
	existing, predecessors := map[string]string{}, map[string]string{}
	if e := func() (err error) {
		results := tx.SendBatch(ctx, readBatch)
		defer func() {
			if closeErr := results.Close(); err == nil {
				err = closeErr
			}
		}()
		for range documents {
			if _, err = results.Exec(); err != nil {
				return err
			}
			var sequence int64
			if err = results.QueryRow().Scan(&sequence); err != nil {
				return err
			}
		}
		if err = results.QueryRow().Scan(&published); err != nil {
			return err
		}
		rows, err := results.Query()
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var event, fact, hash string
			var revision int64
			if err = rows.Scan(&event, &fact, &revision, &hash); err != nil {
				return err
			}
			existing[event] = hash
			predecessors[fact+":"+strconv.FormatInt(revision, 10)] = hash
		}
		return rows.Err()
	}(); e != nil {
		return e
	}
	writeBatch := &pgx.Batch{}
	for i, d := range documents {
		hash := eventconnector.HashSourceBytes(encoded[i])
		if retainedHash, exists := existing[d.EventID]; exists {
			if retainedHash != hash {
				return errors.New("source fact revision content conflict")
			}
			continue
		}
		if d.Revision != "1" {
			retainedHash, exists := predecessors[d.SourceFactID+":"+d.PreviousRevision]
			if !exists {
				return errors.New("source revision predecessor unavailable")
			}
			if retainedHash != d.PreviousHash {
				return errors.New("source revision predecessor differs")
			}
		}
		writeBatch.Queue(`WITH allocated AS (
		 UPDATE execution_source_streams SET high_sequence=high_sequence+1
		 WHERE source_instance=$1 AND org_id=$2 AND project_id=$3 AND profile=$4 RETURNING high_sequence)
		 INSERT INTO execution_source_facts(source_instance,org_id,project_id,profile,sequence,event_id,source_fact_id,revision,request_id,stage,document,source_hash,published_at)
		 SELECT $1,$2,$3,$4,high_sequence,$5,$6,$7,$8,$9,$10,$11,$12 FROM allocated`, d.SourceInstance, d.OrgID, d.ProjectID, d.Profile, d.EventID, d.SourceFactID, d.Revision, d.RequestID, d.Stage, encoded[i], hash, published)
	}
	if writeBatch.Len() == 0 {
		return nil
	}
	return func() (err error) {
		results := tx.SendBatch(ctx, writeBatch)
		defer func() {
			if closeErr := results.Close(); err == nil {
				err = closeErr
			}
		}()
		for range writeBatch.Len() {
			tag, err := results.Exec()
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return errors.New("source stream allocation missing")
			}
		}
		return nil
	}()
}
