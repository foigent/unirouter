package enterprisefinance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/money"
)

const legacyRateTransformVersion = "legacy-model-prices-to-rate-card.v1"

type LegacyRateBootstrapResult struct {
	OrgID                 int64    `json:"org_id"`
	CreatedDraftCount     int      `json:"created_draft_count"`
	ExistingDraftCount    int      `json:"existing_draft_count"`
	SkippedModelCount     int      `json:"skipped_model_count"`
	SkippedModels         []string `json:"skipped_models,omitempty"`
	State                 string   `json:"state"`
	ApprovalRequired      bool     `json:"approval_required"`
	ActivationRequired    bool     `json:"activation_required"`
	LegacyRuntimeFallback bool     `json:"legacy_runtime_fallback"`
}

type legacyModelPrice struct {
	Model              string
	Provider           string
	Input              string
	Output             string
	CacheReadRatio     string
	CacheCreationRatio string
	OrgMultiplier      string
	UpdatedAt          time.Time
}

// BootstrapLegacyProviderListDrafts is the only compatibility bridge from
// model_prices. It produces evidence-backed provider-list drafts for explicit
// review; it never approves, activates or permits a runtime fallback.
func (s *PostgresStore) BootstrapLegacyProviderListDrafts(ctx context.Context, orgID int64, actorRef string) (LegacyRateBootstrapResult, error) {
	actorRef = strings.TrimSpace(actorRef)
	if orgID <= 0 || actorRef == "" {
		return LegacyRateBootstrapResult{}, invalid("legacy_rate_bootstrap_identity")
	}
	var orgStatus, orgType string
	if err := s.db.QueryRow(ctx, `SELECT status,org_type FROM orgs WHERE id=$1`, orgID).Scan(&orgStatus, &orgType); err != nil {
		return LegacyRateBootstrapResult{}, fmt.Errorf("load legacy rate bootstrap organization: %w", err)
	}
	if orgStatus != "active" || orgType != "customer" {
		return LegacyRateBootstrapResult{}, invalid("legacy_rate_bootstrap_customer")
	}
	rows, err := s.db.Query(ctx, `
		SELECT mp.model_name,mp.provider,mp.input_price_cents::text,mp.output_price_cents::text,
		       COALESCE(mp.cache_read_ratio,1)::text,COALESCE(mp.cache_creation_ratio,1)::text,
		       COALESCE(omp.multiplier,1)::text,GREATEST(mp.updated_at,COALESCE(omp.updated_at,mp.updated_at))
		FROM model_prices mp
		LEFT JOIN org_model_prices omp ON omp.org_id=$1 AND omp.model_name=mp.model_name
		ORDER BY mp.model_name`, orgID)
	if err != nil {
		return LegacyRateBootstrapResult{}, fmt.Errorf("list legacy model prices: %w", err)
	}
	prices := make([]legacyModelPrice, 0)
	for rows.Next() {
		var item legacyModelPrice
		if err := rows.Scan(
			&item.Model, &item.Provider, &item.Input, &item.Output,
			&item.CacheReadRatio, &item.CacheCreationRatio, &item.OrgMultiplier, &item.UpdatedAt,
		); err != nil {
			rows.Close()
			return LegacyRateBootstrapResult{}, fmt.Errorf("scan legacy model price: %w", err)
		}
		prices = append(prices, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return LegacyRateBootstrapResult{}, fmt.Errorf("iterate legacy model prices: %w", err)
	}
	rows.Close()

	result := LegacyRateBootstrapResult{
		OrgID: orgID, State: "drafts_only", ApprovalRequired: true,
		ActivationRequired: true, LegacyRuntimeFallback: false,
	}
	bootstrapAt := time.Now().UTC().Truncate(time.Microsecond)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return LegacyRateBootstrapResult{}, fmt.Errorf("begin legacy rate bootstrap: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, item := range prices {
		item.Model = strings.TrimSpace(item.Model)
		item.Provider = strings.TrimSpace(item.Provider)
		if item.Model == "" || item.Provider == "" {
			result.SkippedModelCount++
			result.SkippedModels = append(result.SkippedModels, item.Model+":provider_ref_missing")
			continue
		}
		components, err := legacyPriceComponents(item)
		if err != nil {
			result.SkippedModelCount++
			result.SkippedModels = append(result.SkippedModels, item.Model+":invalid_legacy_price")
			continue
		}
		payload, err := json.Marshal(struct {
			Model              string `json:"model"`
			Provider           string `json:"provider"`
			Input              string `json:"input_cents_per_thousand"`
			Output             string `json:"output_cents_per_thousand"`
			CacheReadRatio     string `json:"cache_read_ratio"`
			CacheCreationRatio string `json:"cache_creation_ratio"`
			OrgMultiplier      string `json:"org_multiplier"`
			UpdatedAt          string `json:"updated_at"`
		}{
			item.Model, item.Provider, item.Input, item.Output, item.CacheReadRatio,
			item.CacheCreationRatio, item.OrgMultiplier, item.UpdatedAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
		})
		if err != nil {
			return LegacyRateBootstrapResult{}, err
		}
		digest := sha256.Sum256(payload)
		digestHex := hex.EncodeToString(digest[:])
		evidenceRef := "legacy_model_prices_" + digestHex[:24]
		rateCardRef := "legacy_provider_list_" + digestHex[:24]
		var exists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS(SELECT 1 FROM enterprise_finance_rate_card_versions
			WHERE org_id=$1 AND rate_card_ref=$2)`, orgID, rateCardRef).Scan(&exists); err != nil {
			return LegacyRateBootstrapResult{}, fmt.Errorf("check legacy rate draft: %w", err)
		}
		if exists {
			result.ExistingDraftCount++
			continue
		}
		evidence := RateSourceEvidence{
			OrgID: orgID, EvidenceRef: evidenceRef, SourceKind: RateSourceLegacyModel,
			SourceURL:      "unirouter://model_prices/" + url.PathEscape(item.Model),
			SourceRevision: item.UpdatedAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
			FetchedAt:      bootstrapAt, PayloadSHA256: "sha256:" + digestHex,
			TransformVersion: legacyRateTransformVersion, Currency: "CNY", UnitContract: RateSourceUnitTokens,
			LastSuccessAt: bootstrapAt, StaleAfter: bootstrapAt.Add(24 * time.Hour), ImportedBy: actorRef,
		}
		if err := ValidateRateSourceEvidence(evidence); err != nil {
			return LegacyRateBootstrapResult{}, err
		}
		if err := insertRateSourceEvidence(ctx, tx, evidence); err != nil {
			return LegacyRateBootstrapResult{}, err
		}
		card := RateCard{
			ContractVersion: ContractVersion, RateCardRef: rateCardRef, OrgID: orgID,
			ProviderRef: item.Provider, ModelRef: item.Model, Layer: RateProviderList,
			Version: 1, State: RateDraft, EffectiveFrom: bootstrapAt,
			Components: components, SourceEvidenceRef: evidenceRef, CreatedBy: actorRef,
		}
		if err := ValidateRequestPricingRateCard(card); err != nil {
			return LegacyRateBootstrapResult{}, err
		}
		if _, err := insertRateCardVersion(ctx, tx, card, "", nil, nil); err != nil {
			return LegacyRateBootstrapResult{}, err
		}
		result.CreatedDraftCount++
	}
	if err := tx.Commit(ctx); err != nil {
		return LegacyRateBootstrapResult{}, fmt.Errorf("commit legacy rate bootstrap: %w", err)
	}
	return result, nil
}

func legacyPriceComponents(item legacyModelPrice) ([]RateComponent, error) {
	input, err := money.PriceMicrosPerMillionFromInternal(item.Input, "1", item.OrgMultiplier)
	if err != nil {
		return nil, err
	}
	output, err := money.PriceMicrosPerMillionFromInternal(item.Output, "1", item.OrgMultiplier)
	if err != nil {
		return nil, err
	}
	cacheRead, err := money.PriceMicrosPerMillionFromInternal(item.Input, item.CacheReadRatio, item.OrgMultiplier)
	if err != nil {
		return nil, err
	}
	cacheCreation, err := money.PriceMicrosPerMillionFromInternal(item.Input, item.CacheCreationRatio, item.OrgMultiplier)
	if err != nil {
		return nil, err
	}
	values := []struct {
		meter string
		price int64
	}{
		{MeterInputTokens, input},
		{MeterOutputTokens, output},
		{MeterCacheReadTokens, cacheRead},
		{MeterCacheCreationTokens, cacheCreation},
	}
	components := make([]RateComponent, 0, len(values))
	for _, value := range values {
		components = append(components, RateComponent{
			Meter: value.meter, UnitQuantity: strconv.FormatInt(requestPricingBasis, 10),
			Amount: Amount{ValueMinor: strconv.FormatInt(value.price, 10), Currency: "CNY", Scale: requestPricingScale},
		})
	}
	return components, nil
}
