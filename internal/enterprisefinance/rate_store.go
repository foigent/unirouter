package enterprisefinance

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	PricingPersistApplied   = "applied"
	PricingPersistDuplicate = "duplicate_noop"
)

type RateCardRecord struct {
	ID                int64               `json:"id"`
	Card              RateCard            `json:"card"`
	SourceEvidence    *RateSourceEvidence `json:"source_evidence,omitempty"`
	ActivatedBy       string              `json:"activated_by,omitempty"`
	SupersedesVersion *int64              `json:"supersedes_version,omitempty"`
	ActivatedAt       *time.Time          `json:"activated_at,omitempty"`
	CreatedAt         time.Time           `json:"created_at"`
}

type PricingPersistResult struct {
	Result       string          `json:"result"`
	Snapshot     PricingSnapshot `json:"snapshot"`
	BindingCount int             `json:"binding_count"`
	CreatedAt    time.Time       `json:"created_at"`
}

// RateCardActivationOptions makes replacement authority explicit. A caller
// may name one exact current active card and version; activation never guesses
// which card to retire.
type RateCardActivationOptions struct {
	SupersedesRateCardRef     string `json:"supersedes_rate_card_ref,omitempty"`
	ExpectedSupersededVersion int64  `json:"expected_superseded_version,omitempty"`
}

func (s *PostgresStore) CreateRateCardDraft(ctx context.Context, card RateCard, actorRef string) (RateCardRecord, error) {
	actorRef = strings.TrimSpace(actorRef)
	card.ContractVersion = ContractVersion
	card.State = RateDraft
	card.Version = 1
	card.CreatedBy = actorRef
	card.ApprovedBy = ""
	card.DailyPriceBands = canonicalDailyPriceBands(card.DailyPriceBands)
	if actorRef == "" {
		return RateCardRecord{}, invalid("rate_card_creator")
	}
	if err := ValidateRateCard(card); err != nil {
		return RateCardRecord{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return RateCardRecord{}, fmt.Errorf("begin rate-card draft: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	record, err := insertRateCardVersion(ctx, tx, card, "", nil, nil)
	if err != nil {
		return RateCardRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RateCardRecord{}, fmt.Errorf("commit rate-card draft: %w", err)
	}
	return record, nil
}

func (s *PostgresStore) ApproveRateCard(ctx context.Context, orgID int64, rateCardRef string, expectedVersion int64, approverRef string) (RateCardRecord, error) {
	rateCardRef, approverRef = strings.TrimSpace(rateCardRef), strings.TrimSpace(approverRef)
	if orgID <= 0 || rateCardRef == "" || expectedVersion <= 0 || approverRef == "" {
		return RateCardRecord{}, invalid("rate_card_approval")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return RateCardRecord{}, fmt.Errorf("begin rate-card approval: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := loadLatestRateCard(ctx, tx, orgID, rateCardRef, true)
	if err != nil {
		return RateCardRecord{}, err
	}
	if current.Card.Version != expectedVersion || current.Card.State != RateDraft {
		return RateCardRecord{}, ErrConflict
	}
	if current.Card.CreatedBy == approverRef {
		return RateCardRecord{}, ErrMakerChecker
	}
	next := current.Card
	next.Version++
	next.State = RateApproved
	next.ApprovedBy = approverRef
	if err := ValidateRateCard(next); err != nil {
		return RateCardRecord{}, err
	}
	if err := ValidateRequestPricingRateCard(next); err != nil {
		return RateCardRecord{}, err
	}
	record, err := insertRateCardVersion(ctx, tx, next, "", &current.Card.Version, nil)
	if err != nil {
		return RateCardRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RateCardRecord{}, fmt.Errorf("commit rate-card approval: %w", err)
	}
	return record, nil
}

func (s *PostgresStore) ActivateRateCard(ctx context.Context, orgID int64, rateCardRef string, expectedVersion int64, actorRef string, activatedAt time.Time) (RateCardRecord, error) {
	return s.ActivateRateCardWithSupersede(
		ctx, orgID, rateCardRef, expectedVersion, RateCardActivationOptions{}, actorRef, activatedAt,
	)
}

// ActivateRateCardWithSupersede atomically retires one exact conflicting
// request-pricing card and activates its replacement. It rejects missing,
// unnecessary or ambiguous replacement declarations and leaves both cards
// unchanged on any failure.
func (s *PostgresStore) ActivateRateCardWithSupersede(
	ctx context.Context,
	orgID int64,
	rateCardRef string,
	expectedVersion int64,
	options RateCardActivationOptions,
	actorRef string,
	activatedAt time.Time,
) (RateCardRecord, error) {
	rateCardRef, actorRef = strings.TrimSpace(rateCardRef), strings.TrimSpace(actorRef)
	options.SupersedesRateCardRef = strings.TrimSpace(options.SupersedesRateCardRef)
	if orgID <= 0 || rateCardRef == "" || expectedVersion <= 0 || actorRef == "" || !validTime(activatedAt) {
		return RateCardRecord{}, invalid("rate_card_activation")
	}
	if (options.SupersedesRateCardRef == "") != (options.ExpectedSupersededVersion == 0) ||
		options.ExpectedSupersededVersion < 0 || options.SupersedesRateCardRef == rateCardRef {
		return RateCardRecord{}, invalid("rate_card_supersede_identity")
	}
	activatedAt = activatedAt.UTC().Truncate(time.Microsecond)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return RateCardRecord{}, fmt.Errorf("begin rate-card activation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, fmt.Sprintf("enterprise-finance-rate:%d", orgID)); err != nil {
		return RateCardRecord{}, fmt.Errorf("lock rate-card activation: %w", err)
	}
	current, err := loadLatestRateCard(ctx, tx, orgID, rateCardRef, true)
	if err != nil {
		return RateCardRecord{}, err
	}
	if current.Card.Version != expectedVersion || current.Card.State != RateApproved {
		return RateCardRecord{}, ErrConflict
	}
	if err := ValidateRequestPricingRateCard(current.Card); err != nil {
		return RateCardRecord{}, err
	}
	if activatedAt.Before(current.Card.EffectiveFrom) ||
		(current.Card.EffectiveTo != nil && !activatedAt.Before(*current.Card.EffectiveTo)) {
		return RateCardRecord{}, fmt.Errorf("%w: candidate Rate Card is not effective at activation time", ErrRequestPricingNotReady)
	}
	if current.Card.Layer == RateProviderList {
		var sourceKind, sourceURL, sourceRevision, payloadSHA256, transformVersion, currency, unitContract, importedBy string
		var fetchedAt, lastSuccessAt, staleAfter time.Time
		err := tx.QueryRow(ctx, `
			SELECT source_kind,source_url,source_revision,fetched_at,payload_sha256,
			       transform_version,currency,unit_contract,last_success_at,stale_after,imported_by
			FROM enterprise_finance_rate_source_evidence
			WHERE org_id=$1 AND evidence_ref=$2`, orgID, strings.TrimSpace(current.Card.SourceEvidenceRef),
		).Scan(
			&sourceKind, &sourceURL, &sourceRevision, &fetchedAt, &payloadSHA256,
			&transformVersion, &currency, &unitContract, &lastSuccessAt, &staleAfter, &importedBy,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return RateCardRecord{}, fmt.Errorf("%w: provider-list source evidence is missing", ErrRequestPricingNotReady)
		}
		if err != nil {
			return RateCardRecord{}, fmt.Errorf("load provider-list source evidence: %w", err)
		}
		if sourceKind != RateSourceOfficialImport && sourceKind != RateSourceLegacyModel {
			return RateCardRecord{}, fmt.Errorf("%w: provider-list source evidence kind %s", ErrRequestPricingNotReady, sourceKind)
		}
		if err := ValidateRateSourceEvidence(RateSourceEvidence{
			OrgID: orgID, EvidenceRef: current.Card.SourceEvidenceRef, SourceKind: sourceKind,
			SourceURL: sourceURL, SourceRevision: sourceRevision, FetchedAt: fetchedAt.UTC(),
			PayloadSHA256: payloadSHA256, TransformVersion: transformVersion, Currency: currency,
			UnitContract: unitContract, LastSuccessAt: lastSuccessAt.UTC(), StaleAfter: staleAfter.UTC(),
			ImportedBy: importedBy,
		}); err != nil {
			return RateCardRecord{}, fmt.Errorf("%w: invalid provider-list source evidence: %v", ErrRequestPricingNotReady, err)
		}
		if !activatedAt.Before(staleAfter.UTC()) {
			return RateCardRecord{}, fmt.Errorf("%w: provider-list source evidence is stale at activation time", ErrRequestPricingNotReady)
		}
	}
	if current.Card.Layer == RateSupplierContract || current.Card.Layer == RateProviderList {
		rows, err := tx.Query(ctx, `
			SELECT other.rate_card_ref,other.version,other.effective_from,other.effective_to
			FROM enterprise_finance_rate_card_versions other
			WHERE other.org_id=$1
			  AND other.rate_card_ref<>$2
			  AND other.model_ref=$3 AND other.layer=$4
			  AND other.state='active'
			  AND other.version=(
			    SELECT MAX(latest.version) FROM enterprise_finance_rate_card_versions latest
			    WHERE latest.org_id=other.org_id AND latest.rate_card_ref=other.rate_card_ref
			  )
			  AND other.effective_from < COALESCE($5,'infinity'::timestamptz)
			  AND COALESCE(other.effective_to,'infinity'::timestamptz) > $6`,
			orgID, rateCardRef, current.Card.ModelRef, current.Card.Layer,
			current.Card.EffectiveTo, activatedAt,
		)
		if err != nil {
			return RateCardRecord{}, fmt.Errorf("check request-pricing provider ambiguity: %w", err)
		}
		type activeCollision struct {
			ref       string
			version   int64
			effective time.Time
			expires   *time.Time
		}
		collisions := make([]activeCollision, 0, 2)
		for rows.Next() {
			var collision activeCollision
			if err := rows.Scan(&collision.ref, &collision.version, &collision.effective, &collision.expires); err != nil {
				rows.Close()
				return RateCardRecord{}, fmt.Errorf("scan request-pricing provider ambiguity: %w", err)
			}
			collisions = append(collisions, collision)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return RateCardRecord{}, fmt.Errorf("iterate request-pricing provider ambiguity: %w", err)
		}
		rows.Close()

		switch len(collisions) {
		case 0:
			if options.SupersedesRateCardRef != "" {
				return RateCardRecord{}, fmt.Errorf("%w: supersedes declaration is unnecessary", ErrConflict)
			}
		case 1:
			collision := collisions[0]
			if options.SupersedesRateCardRef == "" {
				return RateCardRecord{}, fmt.Errorf("%w: activation requires an explicit supersedes declaration", ErrRequestPricingAmbiguous)
			}
			if collision.ref != options.SupersedesRateCardRef || collision.version != options.ExpectedSupersededVersion {
				return RateCardRecord{}, fmt.Errorf("%w: activation requires the exact conflicting active Rate Card", ErrConflict)
			}
			if activatedAt.Before(collision.effective) ||
				(collision.expires != nil && !activatedAt.Before(*collision.expires)) {
				return RateCardRecord{}, fmt.Errorf("%w: conflicting Rate Card is not current at activation time", ErrConflict)
			}
			superseded, err := loadLatestRateCard(ctx, tx, orgID, collision.ref, true)
			if err != nil {
				return RateCardRecord{}, err
			}
			if superseded.Card.Version != options.ExpectedSupersededVersion || superseded.Card.State != RateActive ||
				superseded.Card.ModelRef != current.Card.ModelRef || superseded.Card.Layer != current.Card.Layer {
				return RateCardRecord{}, ErrConflict
			}
			retired := superseded.Card
			retired.Version++
			retired.State = RateRetired
			if err := ValidateRateCard(retired); err != nil {
				return RateCardRecord{}, err
			}
			if _, err := insertRateCardVersion(ctx, tx, retired, actorRef, &superseded.Card.Version, &activatedAt); err != nil {
				return RateCardRecord{}, err
			}
		default:
			return RateCardRecord{}, fmt.Errorf("%w: model_ref %s has %d overlapping active cards at layer %s",
				ErrRequestPricingAmbiguous, current.Card.ModelRef, len(collisions), current.Card.Layer)
		}
	} else if options.SupersedesRateCardRef != "" {
		return RateCardRecord{}, invalid("rate_card_supersede_layer")
	}
	var collisions int
	err = tx.QueryRow(ctx, `
		SELECT COUNT(DISTINCT other.id)
		FROM enterprise_finance_rate_card_versions other
		JOIN enterprise_finance_rate_components other_component
		  ON other_component.rate_card_version_id=other.id AND other_component.org_id=other.org_id
		JOIN enterprise_finance_rate_components candidate_component
		  ON candidate_component.rate_card_version_id=$1
		 AND candidate_component.org_id=$2
		 AND candidate_component.meter=other_component.meter
		WHERE other.org_id=$2
		  AND other.rate_card_ref<>$3
		  AND other.provider_ref=$4 AND other.model_ref=$5 AND other.layer=$6
		  AND other.state='active'
		  AND other.version=(
		    SELECT MAX(latest.version) FROM enterprise_finance_rate_card_versions latest
		    WHERE latest.org_id=other.org_id AND latest.rate_card_ref=other.rate_card_ref
		  )
		  AND other.effective_from < COALESCE($8,'infinity'::timestamptz)
		  AND COALESCE(other.effective_to,'infinity'::timestamptz) > $7`,
		current.ID, orgID, rateCardRef, current.Card.ProviderRef, current.Card.ModelRef,
		current.Card.Layer, current.Card.EffectiveFrom, current.Card.EffectiveTo,
	).Scan(&collisions)
	if err != nil {
		return RateCardRecord{}, fmt.Errorf("check rate-card activation collision: %w", err)
	}
	if collisions != 0 {
		return RateCardRecord{}, ErrConflict
	}
	next := current.Card
	next.Version++
	next.State = RateActive
	record, err := insertRateCardVersion(ctx, tx, next, actorRef, &current.Card.Version, &activatedAt)
	if err != nil {
		return RateCardRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RateCardRecord{}, fmt.Errorf("commit rate-card activation: %w", err)
	}
	return record, nil
}

func (s *PostgresStore) ListLatestRateCards(ctx context.Context, orgID int64) ([]RateCardRecord, error) {
	if orgID <= 0 {
		return nil, invalid("rate_card_list")
	}
	rows, err := s.db.Query(ctx, `
		SELECT DISTINCT ON (rate_card_ref)
		  id,rate_card_ref,version,provider_ref,model_ref,layer,state,effective_from,effective_to,
		  source_evidence_ref,daily_price_bands,created_by,approved_by,activated_by,supersedes_version,activated_at,created_at
		FROM enterprise_finance_rate_card_versions
		WHERE org_id=$1 ORDER BY rate_card_ref,version DESC`, orgID)
	if err != nil {
		return nil, fmt.Errorf("list latest rate cards: %w", err)
	}
	metas := make([]RateCardRecord, 0)
	for rows.Next() {
		var record RateCardRecord
		var dailyPriceBandsJSON []byte
		record.Card.ContractVersion = ContractVersion
		record.Card.OrgID = orgID
		if err := rows.Scan(
			&record.ID, &record.Card.RateCardRef, &record.Card.Version, &record.Card.ProviderRef,
			&record.Card.ModelRef, &record.Card.Layer, &record.Card.State,
			&record.Card.EffectiveFrom, &record.Card.EffectiveTo, &record.Card.SourceEvidenceRef, &dailyPriceBandsJSON,
			&record.Card.CreatedBy, &record.Card.ApprovedBy, &record.ActivatedBy,
			&record.SupersedesVersion, &record.ActivatedAt, &record.CreatedAt,
		); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan latest rate card: %w", err)
		}
		record.Card.DailyPriceBands, err = unmarshalDailyPriceBands(dailyPriceBandsJSON)
		if err != nil {
			rows.Close()
			return nil, err
		}
		metas = append(metas, normalizeRateCardRecord(record))
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate latest rate cards: %w", err)
	}
	rows.Close()
	for i := range metas {
		components, err := loadRateComponents(ctx, s.db, metas[i].ID)
		if err != nil {
			return nil, err
		}
		metas[i].Card.Components = components
	}
	evidence, err := s.ListRateSourceEvidence(ctx, orgID)
	if err != nil {
		return nil, err
	}
	byRef := make(map[string]RateSourceEvidence, len(evidence))
	for _, item := range evidence {
		byRef[item.EvidenceRef] = item
	}
	for i := range metas {
		if item, ok := byRef[metas[i].Card.SourceEvidenceRef]; ok {
			value := item
			metas[i].SourceEvidence = &value
		}
	}
	return metas, nil
}

func (s *PostgresStore) PersistPricingSnapshot(ctx context.Context, snapshot PricingSnapshot) (PricingPersistResult, error) {
	if err := ValidatePricingSnapshot(snapshot); err != nil {
		return PricingPersistResult{}, err
	}
	computed, err := PricingSnapshotIdentity(snapshot)
	if err != nil {
		return PricingPersistResult{}, err
	}
	if snapshot.SnapshotHash != computed {
		return PricingPersistResult{}, fmt.Errorf("%w: pricing snapshot hash mismatch", ErrConflict)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return PricingPersistResult{}, fmt.Errorf("begin pricing snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, fmt.Sprintf("enterprise-finance-pricing:%d:%s", snapshot.OrgID, snapshot.RequestRef)); err != nil {
		return PricingPersistResult{}, fmt.Errorf("lock pricing snapshot: %w", err)
	}
	var existingHash string
	var existingCreatedAt time.Time
	err = tx.QueryRow(ctx, `
		SELECT snapshot_hash,created_at FROM enterprise_finance_pricing_snapshots
		WHERE org_id=$1 AND request_ref=$2`, snapshot.OrgID, strings.TrimSpace(snapshot.RequestRef),
	).Scan(&existingHash, &existingCreatedAt)
	if err == nil {
		if existingHash != snapshot.SnapshotHash {
			return PricingPersistResult{}, ErrConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return PricingPersistResult{}, fmt.Errorf("commit duplicate pricing snapshot: %w", err)
		}
		return PricingPersistResult{
			Result: PricingPersistDuplicate, Snapshot: snapshot,
			BindingCount: len(snapshot.Bindings), CreatedAt: existingCreatedAt.UTC(),
		}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return PricingPersistResult{}, fmt.Errorf("check pricing snapshot replay: %w", err)
	}
	for _, binding := range snapshot.Bindings {
		matches, err := exactPricingBindingMatches(ctx, tx, snapshot, binding)
		if err != nil {
			return PricingPersistResult{}, err
		}
		if !matches {
			return PricingPersistResult{}, fmt.Errorf("%w: pricing binding is not an exact active component", ErrConflict)
		}
	}
	var snapshotID int64
	var createdAt time.Time
	err = tx.QueryRow(ctx, `
		INSERT INTO enterprise_finance_pricing_snapshots (
		  snapshot_hash,org_id,request_ref,project_ref,priced_at
		) VALUES ($1,$2,$3,$4,$5) RETURNING id,created_at`,
		snapshot.SnapshotHash, snapshot.OrgID, strings.TrimSpace(snapshot.RequestRef),
		strings.TrimSpace(snapshot.ProjectRef), snapshot.PricedAt,
	).Scan(&snapshotID, &createdAt)
	if err != nil {
		return PricingPersistResult{}, fmt.Errorf("insert pricing snapshot: %w", err)
	}
	for _, binding := range snapshot.Bindings {
		if _, err := tx.Exec(ctx, `
			INSERT INTO enterprise_finance_pricing_bindings (
			  pricing_snapshot_id,org_id,meter,quantity,rate_card_ref,rate_card_version,
			  rate_component_hash,charge_minor,currency,amount_scale
			) VALUES ($1,$2,$3,$4::numeric,$5,$6,$7,$8::numeric,$9,$10)`,
			snapshotID, snapshot.OrgID, binding.Meter, binding.Quantity,
			binding.RateCardRef, binding.RateCardVersion, binding.RateComponentHash,
			binding.Charge.ValueMinor, binding.Charge.Currency, binding.Charge.Scale,
		); err != nil {
			return PricingPersistResult{}, fmt.Errorf("insert pricing binding: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return PricingPersistResult{}, fmt.Errorf("commit pricing snapshot: %w", err)
	}
	return PricingPersistResult{
		Result: PricingPersistApplied, Snapshot: snapshot,
		BindingCount: len(snapshot.Bindings), CreatedAt: createdAt.UTC(),
	}, nil
}

func exactPricingBindingMatches(ctx context.Context, tx pgx.Tx, snapshot PricingSnapshot, binding PricingBinding) (bool, error) {
	var cardID int64
	var dailyPriceBandsJSON []byte
	err := tx.QueryRow(ctx, `
		SELECT id,daily_price_bands
		FROM enterprise_finance_rate_card_versions
		WHERE org_id=$1 AND rate_card_ref=$2 AND version=$3 AND state='active'
		  AND effective_from <= $4 AND (effective_to IS NULL OR effective_to > $4)`,
		snapshot.OrgID, strings.TrimSpace(binding.RateCardRef), binding.RateCardVersion, snapshot.PricedAt,
	).Scan(&cardID, &dailyPriceBandsJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load pricing binding authority: %w", err)
	}
	components, err := loadRateComponents(ctx, tx, cardID)
	if err != nil {
		return false, err
	}
	bands, err := unmarshalDailyPriceBands(dailyPriceBandsJSON)
	if err != nil {
		return false, err
	}
	if band, scheduled, selectErr := selectDailyPriceBand(bands, snapshot.PricedAt); selectErr != nil {
		return false, fmt.Errorf("validate pricing binding UTC schedule: %w", selectErr)
	} else if scheduled {
		components = band.Components
	}
	for _, component := range components {
		if component.Meter != binding.Meter {
			continue
		}
		identity, identityErr := RateComponentIdentity(component)
		if identityErr != nil {
			return false, identityErr
		}
		return identity == binding.RateComponentHash, nil
	}
	return false, nil
}

func (s *PostgresStore) GetPricingSnapshot(ctx context.Context, orgID int64, requestRef string) (PricingPersistResult, error) {
	requestRef = strings.TrimSpace(requestRef)
	if orgID <= 0 || requestRef == "" {
		return PricingPersistResult{}, invalid("pricing_snapshot_query")
	}
	var snapshotID int64
	var result PricingPersistResult
	result.Result = PricingPersistApplied
	result.Snapshot.ContractVersion = ContractVersion
	result.Snapshot.OrgID = orgID
	err := s.db.QueryRow(ctx, `
		SELECT id,snapshot_hash,request_ref,project_ref,priced_at,created_at
		FROM enterprise_finance_pricing_snapshots WHERE org_id=$1 AND request_ref=$2`, orgID, requestRef,
	).Scan(
		&snapshotID, &result.Snapshot.SnapshotHash, &result.Snapshot.RequestRef,
		&result.Snapshot.ProjectRef, &result.Snapshot.PricedAt, &result.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return PricingPersistResult{}, ErrNotFound
	}
	if err != nil {
		return PricingPersistResult{}, fmt.Errorf("load pricing snapshot: %w", err)
	}
	rows, err := s.db.Query(ctx, `
		SELECT meter,quantity::text,rate_card_ref,rate_card_version,rate_component_hash,
		       charge_minor::text,currency,amount_scale
		FROM enterprise_finance_pricing_bindings
		WHERE pricing_snapshot_id=$1 AND org_id=$2
		ORDER BY meter,rate_card_ref,rate_card_version`, snapshotID, orgID)
	if err != nil {
		return PricingPersistResult{}, fmt.Errorf("load pricing bindings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var binding PricingBinding
		if err := rows.Scan(
			&binding.Meter, &binding.Quantity, &binding.RateCardRef, &binding.RateCardVersion,
			&binding.RateComponentHash, &binding.Charge.ValueMinor,
			&binding.Charge.Currency, &binding.Charge.Scale,
		); err != nil {
			return PricingPersistResult{}, fmt.Errorf("scan pricing binding: %w", err)
		}
		result.Snapshot.Bindings = append(result.Snapshot.Bindings, binding)
	}
	if err := rows.Err(); err != nil {
		return PricingPersistResult{}, fmt.Errorf("iterate pricing bindings: %w", err)
	}
	result.Snapshot.PricedAt = result.Snapshot.PricedAt.UTC()
	result.CreatedAt = result.CreatedAt.UTC()
	result.BindingCount = len(result.Snapshot.Bindings)
	return result, nil
}

func insertRateCardVersion(ctx context.Context, tx pgx.Tx, card RateCard, activatedBy string, supersedes *int64, activatedAt *time.Time) (RateCardRecord, error) {
	var record RateCardRecord
	record.Card = card
	record.ActivatedBy = strings.TrimSpace(activatedBy)
	record.SupersedesVersion = supersedes
	record.ActivatedAt = activatedAt
	dailyPriceBandsJSON, err := marshalDailyPriceBands(card.DailyPriceBands)
	if err != nil {
		return RateCardRecord{}, err
	}
	record.Card.DailyPriceBands = canonicalDailyPriceBands(card.DailyPriceBands)
	err = tx.QueryRow(ctx, `
		INSERT INTO enterprise_finance_rate_card_versions (
		  org_id,rate_card_ref,version,provider_ref,model_ref,layer,state,effective_from,effective_to,
		  source_evidence_ref,daily_price_bands,created_by,approved_by,activated_by,supersedes_version,activated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12,$13,$14,$15,$16)
		RETURNING id,created_at`,
		card.OrgID, strings.TrimSpace(card.RateCardRef), card.Version,
		strings.TrimSpace(card.ProviderRef), strings.TrimSpace(card.ModelRef), card.Layer, card.State,
		card.EffectiveFrom, card.EffectiveTo, strings.TrimSpace(card.SourceEvidenceRef), dailyPriceBandsJSON,
		strings.TrimSpace(card.CreatedBy), strings.TrimSpace(card.ApprovedBy), record.ActivatedBy,
		supersedes, activatedAt,
	).Scan(&record.ID, &record.CreatedAt)
	if err != nil {
		return RateCardRecord{}, fmt.Errorf("insert rate-card version: %w", err)
	}
	for _, component := range card.Components {
		identity, err := RateComponentIdentity(component)
		if err != nil {
			return RateCardRecord{}, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO enterprise_finance_rate_components (
			  rate_card_version_id,org_id,meter,unit_quantity,amount_minor,currency,amount_scale,component_hash
			) VALUES ($1,$2,$3,$4::numeric,$5::numeric,$6,$7,$8)`,
			record.ID, card.OrgID, component.Meter, component.UnitQuantity,
			component.Amount.ValueMinor, component.Amount.Currency, component.Amount.Scale, identity,
		); err != nil {
			return RateCardRecord{}, fmt.Errorf("insert rate component %q: %w", component.Meter, err)
		}
	}
	return normalizeRateCardRecord(record), nil
}

type rateComponentQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func loadRateComponents(ctx context.Context, queryer rateComponentQueryer, rateCardVersionID int64) ([]RateComponent, error) {
	rows, err := queryer.Query(ctx, `
		SELECT meter,unit_quantity::text,amount_minor::text,currency,amount_scale
		FROM enterprise_finance_rate_components
		WHERE rate_card_version_id=$1 ORDER BY meter`, rateCardVersionID)
	if err != nil {
		return nil, fmt.Errorf("load rate components: %w", err)
	}
	defer rows.Close()
	components := make([]RateComponent, 0)
	for rows.Next() {
		var component RateComponent
		if err := rows.Scan(
			&component.Meter, &component.UnitQuantity, &component.Amount.ValueMinor,
			&component.Amount.Currency, &component.Amount.Scale,
		); err != nil {
			return nil, fmt.Errorf("scan rate component: %w", err)
		}
		components = append(components, component)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rate components: %w", err)
	}
	return components, nil
}

func loadLatestRateCard(ctx context.Context, tx pgx.Tx, orgID int64, rateCardRef string, lock bool) (RateCardRecord, error) {
	query := `
		SELECT id,rate_card_ref,version,provider_ref,model_ref,layer,state,effective_from,effective_to,
		       source_evidence_ref,daily_price_bands,created_by,approved_by,activated_by,supersedes_version,activated_at,created_at
		FROM enterprise_finance_rate_card_versions
		WHERE org_id=$1 AND rate_card_ref=$2 ORDER BY version DESC LIMIT 1`
	if lock {
		query += ` FOR UPDATE`
	}
	var record RateCardRecord
	var dailyPriceBandsJSON []byte
	record.Card.ContractVersion = ContractVersion
	record.Card.OrgID = orgID
	err := tx.QueryRow(ctx, query, orgID, rateCardRef).Scan(
		&record.ID, &record.Card.RateCardRef, &record.Card.Version, &record.Card.ProviderRef,
		&record.Card.ModelRef, &record.Card.Layer, &record.Card.State,
		&record.Card.EffectiveFrom, &record.Card.EffectiveTo, &record.Card.SourceEvidenceRef, &dailyPriceBandsJSON,
		&record.Card.CreatedBy, &record.Card.ApprovedBy, &record.ActivatedBy,
		&record.SupersedesVersion, &record.ActivatedAt, &record.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return RateCardRecord{}, ErrNotFound
	}
	if err != nil {
		return RateCardRecord{}, fmt.Errorf("load latest rate card: %w", err)
	}
	record.Card.DailyPriceBands, err = unmarshalDailyPriceBands(dailyPriceBandsJSON)
	if err != nil {
		return RateCardRecord{}, err
	}
	record.Card.Components, err = loadRateComponents(ctx, tx, record.ID)
	if err != nil {
		return RateCardRecord{}, err
	}
	return normalizeRateCardRecord(record), nil
}

func normalizeRateCardRecord(record RateCardRecord) RateCardRecord {
	record.Card.EffectiveFrom = record.Card.EffectiveFrom.UTC()
	if record.Card.EffectiveTo != nil {
		value := record.Card.EffectiveTo.UTC()
		record.Card.EffectiveTo = &value
	}
	if record.ActivatedAt != nil {
		value := record.ActivatedAt.UTC()
		record.ActivatedAt = &value
	}
	record.CreatedAt = record.CreatedAt.UTC()
	return record
}
