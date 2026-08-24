package enterprisefinance

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	RateSourceOfficialImport = "official_import"
	RateSourceLegacyModel    = "legacy_model_prices"
	RateSourceContract       = "supplier_contract"
	RateSourceUnitTokens     = "tokens"
	RateSourceUnitComponents = "versioned_rate_card_components.v1"
)

var rateSourceSHA256Pattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type RateSourceEvidence struct {
	OrgID            int64     `json:"org_id"`
	EvidenceRef      string    `json:"evidence_ref"`
	SourceKind       string    `json:"source_kind"`
	SourceURL        string    `json:"source_url"`
	SourceRevision   string    `json:"source_revision"`
	FetchedAt        time.Time `json:"fetched_at"`
	PayloadSHA256    string    `json:"payload_sha256"`
	TransformVersion string    `json:"transform_version"`
	Currency         string    `json:"currency"`
	UnitContract     string    `json:"unit_contract"`
	LastSuccessAt    time.Time `json:"last_success_at"`
	StaleAfter       time.Time `json:"stale_after"`
	ImportedBy       string    `json:"imported_by"`
	CreatedAt        time.Time `json:"created_at,omitempty"`
}

type OfficialRateDraftImport struct {
	Card     RateCard           `json:"card"`
	Evidence RateSourceEvidence `json:"evidence"`
}

type RequestPricingAuthorityStatus struct {
	Authority           string     `json:"authority"`
	SelectionKey        string     `json:"selection_key"`
	ContractPriority    []string   `json:"contract_priority"`
	State               string     `json:"state"`
	MissingBehavior     string     `json:"missing_behavior"`
	ProviderAmbiguity   string     `json:"provider_ambiguity"`
	ActiveCardCount     int        `json:"active_card_count"`
	DraftCardCount      int        `json:"draft_card_count"`
	SourceEvidenceCount int        `json:"source_evidence_count"`
	StaleSourceCount    int        `json:"stale_source_count"`
	LastSourceSuccessAt *time.Time `json:"last_source_success_at,omitempty"`
	OfficialSyncMode    string     `json:"official_sync_mode"`
	ActiveOnImport      bool       `json:"active_on_import"`
	LegacyCompatibility string     `json:"legacy_compatibility"`
	PublicSourceFailure string     `json:"public_source_failure"`
	RuntimeSelectionEnv string     `json:"runtime_selection_env"`
	MigrationDefault    string     `json:"migration_default"`
	StrictModeValue     string     `json:"strict_mode_value"`
	CurrentMode         string     `json:"current_mode"`
	StrictActive        bool       `json:"strict_active"`
}

func ValidateRateSourceEvidence(evidence RateSourceEvidence) error {
	if evidence.OrgID <= 0 || blank(evidence.EvidenceRef) || blank(evidence.SourceURL) ||
		blank(evidence.SourceRevision) || blank(evidence.TransformVersion) || blank(evidence.ImportedBy) {
		return invalid("rate_source_identity")
	}
	if (evidence.Currency != "CNY" && evidence.Currency != "USD") ||
		(evidence.UnitContract != RateSourceUnitTokens && evidence.UnitContract != RateSourceUnitComponents) {
		return invalid("rate_source_currency_unit")
	}
	if !rateSourceSHA256Pattern.MatchString(evidence.PayloadSHA256) {
		return invalid("rate_source_payload_sha256")
	}
	if !validTime(evidence.FetchedAt) || !validTime(evidence.LastSuccessAt) || !validTime(evidence.StaleAfter) ||
		evidence.LastSuccessAt.Before(evidence.FetchedAt) || !evidence.StaleAfter.After(evidence.LastSuccessAt) {
		return invalid("rate_source_freshness")
	}
	parsed, err := url.Parse(strings.TrimSpace(evidence.SourceURL))
	if err != nil {
		return invalid("rate_source_url")
	}
	switch evidence.SourceKind {
	case RateSourceOfficialImport:
		if parsed.Scheme != "https" || parsed.Host == "" {
			return invalid("official_rate_source_url")
		}
	case RateSourceLegacyModel:
		if parsed.Scheme != "unirouter" {
			return invalid("legacy_rate_source_url")
		}
	case RateSourceContract:
		if parsed.Scheme == "" {
			return invalid("contract_rate_source_url")
		}
	default:
		return invalid("rate_source_kind")
	}
	return nil
}

// ImportOfficialRateCardDraft records caller-supplied source evidence and a
// provider-list draft atomically. It performs no network request and never
// approves or activates the imported rate.
func (s *PostgresStore) ImportOfficialRateCardDraft(ctx context.Context, request OfficialRateDraftImport, actorRef string) (RateCardRecord, error) {
	actorRef = strings.TrimSpace(actorRef)
	request.Card.ContractVersion = ContractVersion
	request.Card.State = RateDraft
	request.Card.Version = 1
	request.Card.Layer = RateProviderList
	request.Card.CreatedBy = actorRef
	request.Card.ApprovedBy = ""
	request.Card.SourceEvidenceRef = strings.TrimSpace(request.Evidence.EvidenceRef)
	request.Evidence.OrgID = request.Card.OrgID
	request.Evidence.SourceKind = RateSourceOfficialImport
	request.Evidence.ImportedBy = actorRef
	request.Evidence.UnitContract = rateSourceUnitContractForCard(request.Card)
	if actorRef == "" {
		return RateCardRecord{}, invalid("rate_source_actor")
	}
	if err := ValidateRateCard(request.Card); err != nil {
		return RateCardRecord{}, err
	}
	currency, err := rateCardCurrency(request.Card)
	if err != nil {
		return RateCardRecord{}, err
	}
	request.Evidence.Currency = currency
	if err := ValidateRateSourceEvidence(request.Evidence); err != nil {
		return RateCardRecord{}, err
	}
	// Current runtime settlement is CNY-only. Foreign-currency official list
	// prices may be retained as immutable review evidence, but approval still
	// fails closed through ValidateRequestPricingRateCard until an explicit FX or
	// multi-currency settlement authority exists.
	if currency == "CNY" {
		if err := ValidateRequestPricingRateCard(request.Card); err != nil {
			return RateCardRecord{}, err
		}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return RateCardRecord{}, fmt.Errorf("begin official rate draft import: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := insertRateSourceEvidence(ctx, tx, request.Evidence); err != nil {
		return RateCardRecord{}, err
	}
	record, err := insertRateCardVersion(ctx, tx, request.Card, "", nil, nil)
	if err != nil {
		return RateCardRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RateCardRecord{}, fmt.Errorf("commit official rate draft import: %w", err)
	}
	record.SourceEvidence = &request.Evidence
	return record, nil
}

func rateCardCurrency(card RateCard) (string, error) {
	currency := ""
	visit := func(components []RateComponent) error {
		for _, component := range components {
			value := strings.TrimSpace(component.Amount.Currency)
			if currency == "" {
				currency = value
				continue
			}
			if value != currency {
				return invalid("rate_source_mixed_currency")
			}
		}
		return nil
	}
	if err := visit(card.Components); err != nil {
		return "", err
	}
	for _, band := range card.DailyPriceBands {
		if err := visit(band.Components); err != nil {
			return "", err
		}
	}
	if currency != "CNY" && currency != "USD" {
		return "", invalid("rate_source_currency")
	}
	return currency, nil
}

func rateSourceUnitContract(components []RateComponent) string {
	for _, component := range components {
		for _, meter := range supportedRequestUnitMeters {
			if component.Meter == meter {
				return RateSourceUnitComponents
			}
		}
	}
	return RateSourceUnitTokens
}

func rateSourceUnitContractForCard(card RateCard) string {
	if rateSourceUnitContract(card.Components) == RateSourceUnitComponents {
		return RateSourceUnitComponents
	}
	for _, band := range card.DailyPriceBands {
		if rateSourceUnitContract(band.Components) == RateSourceUnitComponents {
			return RateSourceUnitComponents
		}
	}
	return RateSourceUnitTokens
}

func (s *PostgresStore) ListRateSourceEvidence(ctx context.Context, orgID int64) ([]RateSourceEvidence, error) {
	if orgID <= 0 {
		return nil, invalid("rate_source_list")
	}
	rows, err := s.db.Query(ctx, `
		SELECT org_id,evidence_ref,source_kind,source_url,source_revision,fetched_at,payload_sha256,
		       transform_version,currency,unit_contract,last_success_at,stale_after,imported_by,created_at
		FROM enterprise_finance_rate_source_evidence
		WHERE org_id=$1 ORDER BY last_success_at DESC,evidence_ref`, orgID)
	if err != nil {
		return nil, fmt.Errorf("list rate source evidence: %w", err)
	}
	defer rows.Close()
	items := make([]RateSourceEvidence, 0)
	for rows.Next() {
		var item RateSourceEvidence
		if err := rows.Scan(
			&item.OrgID, &item.EvidenceRef, &item.SourceKind, &item.SourceURL, &item.SourceRevision,
			&item.FetchedAt, &item.PayloadSHA256, &item.TransformVersion, &item.Currency,
			&item.UnitContract, &item.LastSuccessAt, &item.StaleAfter, &item.ImportedBy, &item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan rate source evidence: %w", err)
		}
		item = normalizeRateSourceEvidence(item)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rate source evidence: %w", err)
	}
	return items, nil
}

func (s *PostgresStore) RequestPricingAuthorityStatus(ctx context.Context, orgID int64, at time.Time) (RequestPricingAuthorityStatus, error) {
	records, err := s.ListLatestRateCards(ctx, orgID)
	if err != nil {
		return RequestPricingAuthorityStatus{}, err
	}
	at = at.UTC().Truncate(time.Microsecond)
	status := RequestPricingAuthorityStatus{
		Authority: RequestPricingAuthority, SelectionKey: RequestPricingKey,
		ContractPriority: []string{RateSupplierContract, RateProviderList},
		State:            "no_active_rate", MissingBehavior: "fail_closed", ProviderAmbiguity: "fail_closed",
		OfficialSyncMode: "evidence_backed_draft_import", ActiveOnImport: false,
		LegacyCompatibility: "explicit_draft_bootstrap_only",
		PublicSourceFailure: "keep_current_active_rate",
		RuntimeSelectionEnv: "REQUEST_PRICING_AUTHORITY",
		MigrationDefault:    "legacy",
		StrictModeValue:     "rate_card",
	}
	seenEvidence := make(map[string]RateSourceEvidence)
	for _, record := range records {
		if record.Card.Layer != RateSupplierContract && record.Card.Layer != RateProviderList {
			continue
		}
		switch record.Card.State {
		case RateDraft:
			status.DraftCardCount++
		case RateActive:
			if !at.Before(record.Card.EffectiveFrom) && (record.Card.EffectiveTo == nil || at.Before(*record.Card.EffectiveTo)) && ValidateRequestPricingRateCard(record.Card) == nil {
				status.ActiveCardCount++
			}
		}
		if record.SourceEvidence != nil {
			seenEvidence[record.SourceEvidence.EvidenceRef] = *record.SourceEvidence
		}
	}
	status.SourceEvidenceCount = len(seenEvidence)
	for _, evidence := range seenEvidence {
		if !at.Before(evidence.StaleAfter) {
			status.StaleSourceCount++
		}
		if status.LastSourceSuccessAt == nil || evidence.LastSuccessAt.After(*status.LastSourceSuccessAt) {
			value := evidence.LastSuccessAt
			status.LastSourceSuccessAt = &value
		}
	}
	if status.ActiveCardCount > 0 {
		status.State = "active_rate_present"
	}
	return status, nil
}

func (s *PostgresStore) loadRateSourceEvidence(ctx context.Context, orgID int64, evidenceRef string) (RateSourceEvidence, error) {
	var item RateSourceEvidence
	err := s.db.QueryRow(ctx, `
		SELECT org_id,evidence_ref,source_kind,source_url,source_revision,fetched_at,payload_sha256,
		       transform_version,currency,unit_contract,last_success_at,stale_after,imported_by,created_at
		FROM enterprise_finance_rate_source_evidence
		WHERE org_id=$1 AND evidence_ref=$2`, orgID, strings.TrimSpace(evidenceRef)).Scan(
		&item.OrgID, &item.EvidenceRef, &item.SourceKind, &item.SourceURL, &item.SourceRevision,
		&item.FetchedAt, &item.PayloadSHA256, &item.TransformVersion, &item.Currency,
		&item.UnitContract, &item.LastSuccessAt, &item.StaleAfter, &item.ImportedBy, &item.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return RateSourceEvidence{}, ErrNotFound
	}
	if err != nil {
		return RateSourceEvidence{}, fmt.Errorf("load rate source evidence: %w", err)
	}
	return normalizeRateSourceEvidence(item), nil
}

func insertRateSourceEvidence(ctx context.Context, tx pgx.Tx, evidence RateSourceEvidence) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO enterprise_finance_rate_source_evidence (
		  org_id,evidence_ref,source_kind,source_url,source_revision,fetched_at,payload_sha256,
		  transform_version,currency,unit_contract,last_success_at,stale_after,imported_by
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		evidence.OrgID, strings.TrimSpace(evidence.EvidenceRef), evidence.SourceKind,
		strings.TrimSpace(evidence.SourceURL), strings.TrimSpace(evidence.SourceRevision), evidence.FetchedAt,
		evidence.PayloadSHA256, strings.TrimSpace(evidence.TransformVersion), evidence.Currency,
		evidence.UnitContract, evidence.LastSuccessAt, evidence.StaleAfter, strings.TrimSpace(evidence.ImportedBy),
	)
	if err != nil {
		return fmt.Errorf("insert rate source evidence: %w", err)
	}
	return nil
}

func normalizeRateSourceEvidence(item RateSourceEvidence) RateSourceEvidence {
	item.FetchedAt = item.FetchedAt.UTC()
	item.LastSuccessAt = item.LastSuccessAt.UTC()
	item.StaleAfter = item.StaleAfter.UTC()
	item.CreatedAt = item.CreatedAt.UTC()
	return item
}
