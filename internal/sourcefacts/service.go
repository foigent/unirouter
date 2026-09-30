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
	budgetassets "github.com/uniroute/unirouter/contracts/source-budgets/v1"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/completedfeed"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/executioncontext"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
)

const RetentionWindow = 7 * 24 * time.Hour

type Options struct {
	ContentEnabled         bool
	BudgetSnapshotsEnabled bool
	FinancialJournalReady  func() bool
	ObservationsEnabled    bool
	Enabled                bool
	SourceInstance         string
	CursorSecret           string
	I01Enabled             bool
}

type Service struct {
	db        db.DBQuerier
	options   Options
	readSlots chan struct{}
}

func New(database db.DBQuerier, options Options) (*Service, error) {
	if (options.ContentEnabled || options.ObservationsEnabled || options.BudgetSnapshotsEnabled) && !options.Enabled {
		return nil, errors.New("source observations, budgets and content require source facts enablement")
	}
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

func (s *Service) ContentEnabled() bool { return s.Enabled() && s.options.ContentEnabled }

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
	if head != schemaauthority.SourceFactsExpandHead && head != schemaauthority.ObservationExpandHead && head != schemaauthority.BudgetSnapshotExpandHead && head != schemaauthority.ContentCaptureExpandHead && head != schemaauthority.ContentReclaimExpandHead && head != schemaauthority.PreDispatchExpandHead && head != schemaauthority.ScopedContentExpandHead && head != schemaauthority.XInsightPolicyExpandHead {
		return errors.New("source facts capture requires M116 through M123")
	}
	if s.ObservationsEnabled() && head != schemaauthority.ObservationExpandHead && head != schemaauthority.BudgetSnapshotExpandHead && head != schemaauthority.ContentCaptureExpandHead && head != schemaauthority.ContentReclaimExpandHead && head != schemaauthority.PreDispatchExpandHead && head != schemaauthority.ScopedContentExpandHead && head != schemaauthority.XInsightPolicyExpandHead {
		return errors.New("source observations require M117 through M123")
	}
	if s.ContentEnabled() && head != schemaauthority.ContentCaptureExpandHead && head != schemaauthority.ContentReclaimExpandHead && head != schemaauthority.PreDispatchExpandHead && head != schemaauthority.ScopedContentExpandHead && head != schemaauthority.XInsightPolicyExpandHead {
		return errors.New("content capture requires M119 through M123")
	}
	if s.BudgetsEnabled() {
		if err := budgetassets.Verify(); err != nil {
			return err
		}
		if !s.budgetJournalReady() {
			return errors.New("budget snapshots require the original durable financial journal")
		}
		if _, err := runtimegeneration.ReadBudgetCoverage(ctx, s.db); err != nil {
			return err
		}
	}
	var mismatch bool
	if e := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM execution_source_streams WHERE source_instance<>$1 UNION ALL SELECT 1 FROM execution_source_connections WHERE source_instance<>$1)`, s.options.SourceInstance).Scan(&mismatch); e != nil {
		return e
	}
	if mismatch {
		return errors.New("source facts producer instance changed")
	}
	if s.ContentEnabled() {
		if e := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM execution_content_streams WHERE source_instance<>$1)`, s.options.SourceInstance).Scan(&mismatch); e != nil {
			return e
		}
		if mismatch {
			return errors.New("content producer instance changed")
		}
	}
	if s.ObservationsEnabled() {
		if _, e := observationDescriptors(); e != nil {
			return e
		}
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
		return captureError("begin", e)
	}
	defer tx.Rollback(ctx)
	var observations []Observation
	if s.ObservationsEnabled() {
		at, _ := time.Parse(time.RFC3339Nano, snapshot.Core.CapturedAt)
		d := s.observation(org, project, snapshot.Core.RequestID, RuntimeProfile, "request", snapshot.Core.RequestID, "request_started", at)
		d.ContextRef = (Document{SourceInstance: s.options.SourceInstance, OrgID: snapshot.Core.OrgID, ProjectID: snapshot.Core.ProjectID, RequestID: snapshot.Core.RequestID, Profile: ContextProfile, Stage: "authenticated"}).factID()
		d.CaptureExpected = []string{"request_started", "request_terminal", "delivery_started", "local_delivery", "response_measurement", "first_output_measurement", "stream_measurement"}
		d.Runtime = &RuntimeObservation{Operation: snapshot.Core.Operation, StreamMode: "not_observed", Admission: "authenticated", Provider: "not_started", LocalDelivery: "not_started", Financial: "not_created", Capture: "observed"}
		observations = append(observations, d)
	}
	if e = s.publishTogetherTx(ctx, tx, s.contexts(org, project, snapshot.Core.RequestID, "authenticated", snapshot.Core.CapturedAt, snapshot.Core.CapturedAt, &snapshot), observations); e != nil {
		return e
	}
	return captureError("commit", tx.Commit(ctx))
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
	var boundAttempt string
	e := tx.QueryRow(ctx, `SELECT source_context,source_cost_basis,COALESCE(delivery_attempt_id,'') FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, record.OrgID, record.RequestID).Scan(&contextBytes, &basisBytes, &boundAttempt)
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
	var observations []Observation
	if s.ObservationsEnabled() {
		d := s.observation(record.OrgID, record.ProjectID, record.RequestID, RuntimeProfile, "request", record.RequestID, "request_terminal", record.OccurredAt)
		operation := "not_available"
		if snapshot != nil {
			operation = snapshot.Core.Operation
			d.ContextRef = documents[0].SourceFactID
		}
		// This journal owns the financial outcome only. Delivery and Provider
		// observations are captured at their original, separate write boundaries.
		d.Coverage = "partial"
		d.Runtime = &RuntimeObservation{Operation: operation, StreamMode: "not_observed", Admission: "unknown", Provider: "unknown", LocalDelivery: "unknown", Financial: record.Status, Capture: "partial", EvidenceRef: record.EventKey}
		observations = append(observations, d)
		if boundAttempt != "" {
			// The reservation's original delivery binding identifies the charged
			// attempt. Never guess from the newest attempt or customer display.
			var attemptOperation, routeHash string
			var sequence, total int
			if e = tx.QueryRow(ctx, `SELECT COALESCE(route_model_operation_id,''),COALESCE(route_evidence_hash,''),route_sequence,route_total FROM billing_provider_attempts WHERE org_id=$1 AND project_id=$2 AND request_id=$3 AND attempt_id=$4`, record.OrgID, record.ProjectID, record.RequestID, boundAttempt).Scan(&attemptOperation, &routeHash, &sequence, &total); e != nil {
				return e
			}
			if attemptOperation == "" {
				attemptOperation = "not_available"
			}
			attempt := s.observation(record.OrgID, record.ProjectID, record.RequestID, AttemptProfile, "provider_attempt", boundAttempt, "financial_result", record.OccurredAt)
			attempt.ContextRef = d.ContextRef
			attempt.RouteEvidenceHash = routeHash
			attempt.Coverage = "partial"
			attempt.Attempt = &AttemptObservation{AttemptID: boundAttempt, Operation: attemptOperation, RouteSequence: strconv.Itoa(sequence), RouteTotal: strconv.Itoa(total), Provider: "unknown", LocalDelivery: "unknown", Financial: record.Status, EvidenceRef: record.EventKey}
			observations = append(observations, attempt)
		}
	}
	return s.publishTogetherTx(ctx, tx, append(documents, cost), observations)
}

// sourcePublication contains only metadata from a successfully encoded A or B
// document. Callers cannot supply arbitrary unvalidated source bytes.
type sourcePublication struct {
	SourceInstance, OrgID, ProjectID, RequestID, Profile, Stage     string
	EventID, SourceFactID, Revision, PreviousRevision, PreviousHash string
	Wire                                                            []byte
}

func publicationA(d Document) (sourcePublication, error) {
	wire, e := Encode(d)
	return sourcePublication{d.SourceInstance, d.OrgID, d.ProjectID, d.RequestID, d.Profile, d.Stage, d.EventID, d.SourceFactID, d.Revision, d.PreviousRevision, d.PreviousHash, wire}, e
}

func publicationB(d Observation) (sourcePublication, error) {
	wire, e := EncodeObservation(d)
	return sourcePublication{d.SourceInstance, d.OrgID, d.ProjectID, d.RequestID, d.Profile, d.Kind, d.EventID, d.SourceFactID, d.Revision, d.PreviousRevision, d.PreviousHash, wire}, e
}

func (s *Service) ObservationsEnabled() bool { return s.Enabled() && s.options.ObservationsEnabled }

// PublishTx preserves the A-only entrypoint and its original bounds. A and B
// terminal writers use publishTogetherTx so every source lock is sorted once.
func (s *Service) PublishTx(ctx context.Context, tx pgx.Tx, documents []Document) error {
	return s.publishTogetherTx(ctx, tx, documents, nil)
}

func (s *Service) publishTogetherTx(ctx context.Context, tx pgx.Tx, documents []Document, observations []Observation) error {
	if !s.Enabled() {
		return nil
	}
	if len(documents) > 3 {
		return errors.New("invalid A source publication size")
	}
	records := make([]sourcePublication, 0, len(documents)+len(observations))
	seen := map[string]bool{}
	for _, d := range documents {
		if seen[d.Profile] {
			return errors.New("mixed source publication scope")
		}
		seen[d.Profile] = true
		p, e := publicationA(d)
		if e != nil {
			return e
		}
		records = append(records, p)
	}
	if s.ObservationsEnabled() {
		for _, d := range observations {
			p, e := publicationB(d)
			if e != nil {
				return e
			}
			records = append(records, p)
		}
	}
	return s.publishRecordsTx(ctx, tx, records)
}

// publishRecordsTx runs after business/V1 locks and before the existing budget
// tail. Mixed A/B publications share one total profile order and one transaction.
func (s *Service) publishRecordsTx(ctx context.Context, tx pgx.Tx, records []sourcePublication) error {
	if tx == nil || len(records) == 0 {
		return errors.New("invalid source publication transaction")
	}
	documents := slices.Clone(records)
	slices.SortFunc(documents, func(a, b sourcePublication) int {
		if n := strings.Compare(a.Profile, b.Profile); n != 0 {
			return n
		}
		return strings.Compare(a.EventID, b.EventID)
	})
	encoded := make([][]byte, len(documents))
	first := documents[0]
	events := map[string]bool{}
	for i, d := range documents {
		if d.SourceInstance != s.options.SourceInstance || d.OrgID != first.OrgID || d.ProjectID != first.ProjectID || d.RequestID != first.RequestID || events[d.EventID] {
			return errors.New("mixed source publication scope")
		}
		events[d.EventID] = true
		encoded[i] = d.Wire
	}
	// Pipeline the finite publication batch, preserving the same SQL lock order. All locks precede the shared publication timestamp
	// and replay/predecessor reads. No transaction or writer authority changes.
	readBatch := &pgx.Batch{}
	eventIDs, predecessorFacts, predecessorRevisions := []string{}, []string{}, []int64{}
	profiles := 0
	for i, d := range documents {
		if i == 0 || documents[i-1].Profile != d.Profile {
			profiles++
			readBatch.Queue(`INSERT INTO execution_source_streams(source_instance,org_id,project_id,profile) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, d.SourceInstance, d.OrgID, d.ProjectID, d.Profile)
			readBatch.Queue(`SELECT high_sequence FROM execution_source_streams WHERE source_instance=$1 AND org_id=$2 AND project_id=$3 AND profile=$4 FOR UPDATE`, d.SourceInstance, d.OrgID, d.ProjectID, d.Profile)
		}
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
				err = captureError("batch_close", closeErr)
			}
		}()
		for range profiles {
			if _, err = results.Exec(); err != nil {
				return captureError("stream_insert", err)
			}
			var sequence int64
			if err = results.QueryRow().Scan(&sequence); err != nil {
				return captureError("stream_lock", err)
			}
		}
		if err = results.QueryRow().Scan(&published); err != nil {
			return captureError("publication_clock", err)
		}
		rows, err := results.Query()
		if err != nil {
			return captureError("replay_predecessor_read", err)
		}
		defer rows.Close()
		for rows.Next() {
			var event, fact, hash string
			var revision int64
			if err = rows.Scan(&event, &fact, &revision, &hash); err != nil {
				return captureError("replay_predecessor_read", err)
			}
			existing[event] = hash
			predecessors[fact+":"+strconv.FormatInt(revision, 10)] = hash
		}
		return captureError("replay_predecessor_read", rows.Err())
	}(); e != nil {
		return e
	}
	pending := make([]sourcePublication, 0, len(documents))
	pendingHashes := make([]string, 0, len(documents))
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
		pending = append(pending, d)
		pendingHashes = append(pendingHashes, hash)
	}
	// Locks and predecessor reads above cover the whole batch before any
	// sequence is allocated. Replays consume no sequence; each profile's new
	// events retain the original (profile,event_id) order and exact wire bytes.
	writeBatch := &pgx.Batch{}
	writeCounts := []int64{}
	for start := 0; start < len(pending); {
		end := start + 1
		for end < len(pending) && pending[end].Profile == pending[start].Profile {
			end++
		}
		group := pending[start:end]
		eventIDs, factIDs, stages, hashes := []string{}, []string{}, []string{}, []string{}
		revisions, wires := []int64{}, [][]byte{}
		for i, d := range group {
			revision, _ := strconv.ParseInt(d.Revision, 10, 64) // Encode validated canonical int64.
			eventIDs = append(eventIDs, d.EventID)
			factIDs = append(factIDs, d.SourceFactID)
			revisions = append(revisions, revision)
			stages = append(stages, d.Stage)
			wires = append(wires, d.Wire)
			hashes = append(hashes, pendingHashes[start+i])
		}
		d := group[0]
		writeBatch.Queue(`WITH allocated AS (
		 UPDATE execution_source_streams SET high_sequence=high_sequence+cardinality($6::text[])
		 WHERE source_instance=$1 AND org_id=$2 AND project_id=$3 AND profile=$4 RETURNING high_sequence)
		 INSERT INTO execution_source_facts(source_instance,org_id,project_id,profile,sequence,event_id,source_fact_id,revision,request_id,stage,document,source_hash,published_at)
		 SELECT $1,$2,$3,$4,allocated.high_sequence-cardinality($6::text[])+item.ordinality,
		 item.event_id,item.fact_id,item.revision,$5,item.stage,item.document,item.source_hash,$12
		 FROM allocated CROSS JOIN unnest($6::text[],$7::text[],$8::bigint[],$9::text[],$10::bytea[],$11::text[])
		 WITH ORDINALITY AS item(event_id,fact_id,revision,stage,document,source_hash,ordinality)
		 ORDER BY item.ordinality`, d.SourceInstance, d.OrgID, d.ProjectID, d.Profile, d.RequestID, eventIDs, factIDs, revisions, stages, wires, hashes, published)
		writeCounts = append(writeCounts, int64(len(group)))
		start = end
	}
	if writeBatch.Len() == 0 {
		return nil
	}
	return func() (err error) {
		results := tx.SendBatch(ctx, writeBatch)
		defer func() {
			if closeErr := results.Close(); err == nil {
				err = captureError("batch_close", closeErr)
			}
		}()
		for _, count := range writeCounts {
			tag, err := results.Exec()
			if err != nil {
				return captureError("sequence_and_fact_write", err)
			}
			if tag.RowsAffected() != count {
				return errors.New("source stream allocation missing")
			}
		}
		return nil
	}()
}

func (s *Service) BudgetsEnabled() bool { return s.Enabled() && s.options.BudgetSnapshotsEnabled }
func (s *Service) budgetJournalReady() bool {
	return s != nil && s.options.FinancialJournalReady != nil && s.options.FinancialJournalReady()
}
