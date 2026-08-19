package admin

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/enterprisefinance"
	"github.com/uniroute/unirouter/internal/middleware"
)

const enterpriseFinanceMutationMaxBytes = 64 << 10

type createFiscalPeriodRequest struct {
	OrgID     int64     `json:"org_id"`
	PeriodRef string    `json:"period_ref"`
	StartAt   time.Time `json:"start_at"`
	EndAt     time.Time `json:"end_at"`
}

type transitionFiscalPeriodRequest struct {
	OrgID           int64  `json:"org_id"`
	ToState         string `json:"to_state"`
	ExpectedVersion int64  `json:"expected_version"`
	Reason          string `json:"reason"`
}

type createCostCenterRequest struct {
	OrgID          int64      `json:"org_id"`
	CostCenterRef  string     `json:"cost_center_ref"`
	Version        int64      `json:"version"`
	Name           string     `json:"name"`
	LegalEntityRef string     `json:"legal_entity_ref"`
	OwnerUnitRef   string     `json:"owner_unit_ref"`
	SourceRef      string     `json:"source_ref"`
	SourceRevision string     `json:"source_revision"`
	ValidFrom      time.Time  `json:"valid_from"`
	ValidTo        *time.Time `json:"valid_to"`
}

type createBudgetRequest struct {
	OrgID           int64                    `json:"org_id"`
	BudgetRef       string                   `json:"budget_ref"`
	Scope           enterprisefinance.Scope  `json:"scope"`
	FiscalPeriodRef string                   `json:"fiscal_period_ref"`
	Amount          enterprisefinance.Amount `json:"amount"`
}

type approveBudgetRequest struct {
	OrgID           int64 `json:"org_id"`
	ExpectedVersion int64 `json:"expected_version"`
}

type createRateCardRequest struct {
	OrgID             int64                              `json:"org_id"`
	RateCardRef       string                             `json:"rate_card_ref"`
	ProviderRef       string                             `json:"provider_ref"`
	ModelRef          string                             `json:"model_ref"`
	Layer             string                             `json:"layer"`
	EffectiveFrom     time.Time                          `json:"effective_from"`
	EffectiveTo       *time.Time                         `json:"effective_to"`
	Components        []enterprisefinance.RateComponent  `json:"components"`
	DailyPriceBands   []enterprisefinance.DailyPriceBand `json:"daily_price_bands"`
	SourceEvidenceRef string                             `json:"source_evidence_ref"`
}

type importOfficialRateCardRequest struct {
	OrgID            int64                              `json:"org_id"`
	RateCardRef      string                             `json:"rate_card_ref"`
	ProviderRef      string                             `json:"provider_ref"`
	ModelRef         string                             `json:"model_ref"`
	EffectiveFrom    time.Time                          `json:"effective_from"`
	EffectiveTo      *time.Time                         `json:"effective_to"`
	Components       []enterprisefinance.RateComponent  `json:"components"`
	DailyPriceBands  []enterprisefinance.DailyPriceBand `json:"daily_price_bands"`
	EvidenceRef      string                             `json:"evidence_ref"`
	SourceURL        string                             `json:"source_url"`
	SourceRevision   string                             `json:"source_revision"`
	FetchedAt        time.Time                          `json:"fetched_at"`
	PayloadSHA256    string                             `json:"payload_sha256"`
	TransformVersion string                             `json:"transform_version"`
	StaleAfter       time.Time                          `json:"stale_after"`
}

type bootstrapLegacyRateCardsRequest struct {
	OrgID int64 `json:"org_id"`
}

type requestPricingPreflightModel struct {
	ModelRef string `json:"model_ref"`
}

type requestPricingPreflightRequest struct {
	OrgID  int64                          `json:"org_id"`
	Models []requestPricingPreflightModel `json:"models"`
}

type rateCardTransitionRequest struct {
	OrgID                     int64  `json:"org_id"`
	ExpectedVersion           int64  `json:"expected_version"`
	SupersedesRateCardRef     string `json:"supersedes_rate_card_ref"`
	ExpectedSupersededVersion int64  `json:"expected_superseded_version"`
}

type allocationResultRequest struct {
	RequestRef   string                   `json:"request_ref"`
	Target       enterprisefinance.Scope  `json:"target"`
	State        string                   `json:"state"`
	RuleRef      string                   `json:"rule_ref"`
	RuleRevision string                   `json:"rule_revision"`
	Amount       enterprisefinance.Amount `json:"amount"`
}

type createAllocationRunRequest struct {
	OrgID            int64                     `json:"org_id"`
	AllocationRunRef string                    `json:"allocation_run_ref"`
	FiscalPeriodRef  string                    `json:"fiscal_period_ref"`
	SourceRevision   string                    `json:"source_revision"`
	RuleSetRef       string                    `json:"rule_set_ref"`
	RuleSetRevision  string                    `json:"rule_set_revision"`
	SupersedesRunRef string                    `json:"supersedes_run_ref"`
	Results          []allocationResultRequest `json:"results"`
}

type createAdjustmentJournalRequest struct {
	OrgID                  int64                    `json:"org_id"`
	JournalRef             string                   `json:"journal_ref"`
	FiscalPeriodRef        string                   `json:"fiscal_period_ref"`
	RequestRef             string                   `json:"request_ref"`
	EntryType              string                   `json:"entry_type"`
	ReversalOfJournalRef   string                   `json:"reversal_of_journal_ref"`
	Target                 enterprisefinance.Scope  `json:"target"`
	Amount                 enterprisefinance.Amount `json:"amount"`
	Reason                 string                   `json:"reason"`
	SourceAllocationRunRef string                   `json:"source_allocation_run_ref"`
}

type approveAdjustmentJournalRequest struct {
	OrgID           int64 `json:"org_id"`
	ExpectedVersion int64 `json:"expected_version"`
}

func enterpriseFinanceActorRef(r *http.Request) (string, error) {
	userID := middleware.GetUserIDFromContext(r.Context())
	if userID <= 0 {
		return "", errors.New("authenticated finance actor is required")
	}
	return fmt.Sprintf("user:%d", userID), nil
}

func (h *Handler) SetRequestPricingAuthority(authority string) {
	if h != nil {
		h.requestPricingAuthority = strings.TrimSpace(authority)
	}
}

func (h *Handler) effectiveRequestPricingAuthority() string {
	if h == nil || strings.TrimSpace(h.requestPricingAuthority) == "" {
		return "legacy"
	}
	return h.requestPricingAuthority
}

func decodeEnterpriseFinanceJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, enterpriseFinanceMutationMaxBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid enterprise finance payload"})
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Only one enterprise finance object is allowed"})
		return false
	}
	return true
}

func enterpriseFinanceStore(h *Handler) (*enterprisefinance.PostgresStore, error) {
	return enterprisefinance.NewPostgresStore(h.db)
}

func writeEnterpriseFinanceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, enterprisefinance.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": "Enterprise finance record not found"})
	case errors.Is(err, enterprisefinance.ErrConflict),
		errors.Is(err, enterprisefinance.ErrOverlappingPeriod),
		errors.Is(err, enterprisefinance.ErrMakerChecker),
		errors.Is(err, enterprisefinance.ErrClosedPeriod),
		errors.Is(err, enterprisefinance.ErrCloseNotReady),
		errors.Is(err, enterprisefinance.ErrRequestPricingNotReady),
		errors.Is(err, enterprisefinance.ErrRequestPricingAmbiguous):
		writeJSON(w, http.StatusConflict, map[string]interface{}{"error": err.Error()})
	case errors.Is(err, enterprisefinance.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Enterprise finance operation failed"})
	}
}

func (h *Handler) ListEnterpriseFiscalPeriods(w http.ResponseWriter, r *http.Request) {
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	store, _ := enterpriseFinanceStore(h)
	items, err := store.ListFiscalPeriods(r.Context(), orgID)
	if err != nil {
		writeEnterpriseFinanceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": items, "total": len(items), "org_id": orgID})
}

func (h *Handler) CreateEnterpriseFiscalPeriod(w http.ResponseWriter, r *http.Request) {
	var request createFiscalPeriodRequest
	if !decodeEnterpriseFinanceJSON(w, r, &request) {
		return
	}
	orgID, err := resolveActorOrganization(r.Context(), request.OrgID)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	actorRef, err := enterpriseFinanceActorRef(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "Authenticated finance actor required"})
		return
	}
	store, _ := enterpriseFinanceStore(h)
	record, err := store.CreateFiscalPeriod(r.Context(), orgID, enterprisefinance.FiscalPeriod{
		PeriodRef: request.PeriodRef, StartAt: request.StartAt, EndAt: request.EndAt,
		State: enterprisefinance.PeriodOpen,
	}, actorRef)
	if err != nil {
		writeEnterpriseFinanceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, record)
}

func (h *Handler) TransitionEnterpriseFiscalPeriod(w http.ResponseWriter, r *http.Request) {
	var request transitionFiscalPeriodRequest
	if !decodeEnterpriseFinanceJSON(w, r, &request) {
		return
	}
	orgID, err := resolveActorOrganization(r.Context(), request.OrgID)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	actorRef, err := enterpriseFinanceActorRef(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "Authenticated finance actor required"})
		return
	}
	store, _ := enterpriseFinanceStore(h)
	record, err := store.TransitionFiscalPeriod(
		r.Context(), orgID, r.PathValue("period_ref"), strings.TrimSpace(request.ToState),
		request.ExpectedVersion, actorRef, request.Reason,
	)
	if err != nil {
		writeEnterpriseFinanceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (h *Handler) ListEnterpriseCostCenters(w http.ResponseWriter, r *http.Request) {
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	effectiveAt := time.Now().UTC().Truncate(time.Microsecond)
	if raw := strings.TrimSpace(r.URL.Query().Get("effective_at")); raw != "" {
		parsed, parseErr := time.Parse(time.RFC3339Nano, raw)
		if parseErr != nil || parsed.Nanosecond()%1_000 != 0 {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "effective_at must be RFC3339 with at most microsecond precision"})
			return
		}
		effectiveAt = parsed.UTC()
	}
	store, _ := enterpriseFinanceStore(h)
	items, err := store.ListEffectiveCostCenters(r.Context(), orgID, effectiveAt)
	if err != nil {
		writeEnterpriseFinanceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": items, "total": len(items), "org_id": orgID, "effective_at": effectiveAt,
	})
}

func (h *Handler) CreateEnterpriseCostCenterVersion(w http.ResponseWriter, r *http.Request) {
	var request createCostCenterRequest
	if !decodeEnterpriseFinanceJSON(w, r, &request) {
		return
	}
	orgID, err := resolveActorOrganization(r.Context(), request.OrgID)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	actorRef, err := enterpriseFinanceActorRef(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "Authenticated finance actor required"})
		return
	}
	store, _ := enterpriseFinanceStore(h)
	record, err := store.PutCostCenterVersion(r.Context(), enterprisefinance.CostCenterVersion{
		OrgID: orgID, CostCenterRef: request.CostCenterRef, Version: request.Version,
		Name: request.Name, LegalEntityRef: request.LegalEntityRef, OwnerUnitRef: request.OwnerUnitRef,
		SourceRef: request.SourceRef, SourceRevision: request.SourceRevision,
		ValidFrom: request.ValidFrom, ValidTo: request.ValidTo, CreatedBy: actorRef,
	})
	if err != nil {
		writeEnterpriseFinanceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, record)
}

func (h *Handler) ListEnterpriseBudgets(w http.ResponseWriter, r *http.Request) {
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	store, _ := enterpriseFinanceStore(h)
	items, err := store.ListLatestBudgets(r.Context(), orgID)
	if err != nil {
		writeEnterpriseFinanceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": items, "total": len(items), "org_id": orgID,
		"default_runtime_effect": enterprisefinance.BudgetDefaultEffect,
		"wallet_ledger_mutation": "none",
	})
}

func (h *Handler) ListEnterpriseBudgetPositions(w http.ResponseWriter, r *http.Request) {
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	store, _ := enterpriseFinanceStore(h)
	items, err := store.GetBudgetPositions(r.Context(), orgID)
	if err != nil {
		writeEnterpriseFinanceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": items, "total": len(items), "org_id": orgID,
		"committed_evidence": enterprisefinance.EvidenceNotModeled,
		"forecast_evidence":  enterprisefinance.EvidenceNotModeled,
	})
}

func (h *Handler) CreateEnterpriseBudgetDraft(w http.ResponseWriter, r *http.Request) {
	var request createBudgetRequest
	if !decodeEnterpriseFinanceJSON(w, r, &request) {
		return
	}
	orgID, err := resolveActorOrganization(r.Context(), request.OrgID)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	actorRef, err := enterpriseFinanceActorRef(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "Authenticated finance actor required"})
		return
	}
	if strings.TrimSpace(request.BudgetRef) == "" {
		request.BudgetRef, err = newEnterpriseFinanceRef("budget")
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Budget reference generation failed"})
			return
		}
	}
	store, _ := enterpriseFinanceStore(h)
	record, err := store.CreateBudgetDraft(r.Context(), enterprisefinance.BudgetPlan{
		ContractVersion: enterprisefinance.ContractVersion, BudgetRef: request.BudgetRef,
		OrgID: orgID, Scope: request.Scope, FiscalPeriodRef: request.FiscalPeriodRef,
		Amount: request.Amount, Version: 1,
	}, actorRef)
	if err != nil {
		writeEnterpriseFinanceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, record)
}

func (h *Handler) ApproveEnterpriseBudget(w http.ResponseWriter, r *http.Request) {
	var request approveBudgetRequest
	if !decodeEnterpriseFinanceJSON(w, r, &request) {
		return
	}
	orgID, err := resolveActorOrganization(r.Context(), request.OrgID)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	actorRef, err := enterpriseFinanceActorRef(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "Authenticated finance actor required"})
		return
	}
	store, _ := enterpriseFinanceStore(h)
	record, err := store.ApproveBudget(
		r.Context(), orgID, r.PathValue("budget_ref"), request.ExpectedVersion, actorRef,
	)
	if err != nil {
		writeEnterpriseFinanceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (h *Handler) ListEnterpriseRateCards(w http.ResponseWriter, r *http.Request) {
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	store, _ := enterpriseFinanceStore(h)
	items, err := store.ListLatestRateCards(r.Context(), orgID)
	if err != nil {
		writeEnterpriseFinanceError(w, err)
		return
	}
	authority, err := store.RequestPricingAuthorityStatus(r.Context(), orgID, time.Now().UTC().Truncate(time.Microsecond))
	if err != nil {
		writeEnterpriseFinanceError(w, err)
		return
	}
	authority.CurrentMode = h.effectiveRequestPricingAuthority()
	authority.StrictActive = authority.CurrentMode == "rate_card"
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": items, "total": len(items), "org_id": orgID,
		"request_pricing_integration": "versioned_rate_card_authority",
		"request_pricing_authority":   authority,
	})
}

func (h *Handler) CreateEnterpriseRateCardDraft(w http.ResponseWriter, r *http.Request) {
	var request createRateCardRequest
	if !decodeEnterpriseFinanceJSON(w, r, &request) {
		return
	}
	orgID, err := resolveActorOrganization(r.Context(), request.OrgID)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	actorRef, err := enterpriseFinanceActorRef(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "Authenticated finance actor required"})
		return
	}
	if strings.TrimSpace(request.RateCardRef) == "" {
		request.RateCardRef, err = newEnterpriseFinanceRef("rate")
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rate Card reference generation failed"})
			return
		}
	}
	store, _ := enterpriseFinanceStore(h)
	record, err := store.CreateRateCardDraft(r.Context(), enterprisefinance.RateCard{
		ContractVersion: enterprisefinance.ContractVersion, RateCardRef: request.RateCardRef,
		OrgID: orgID, ProviderRef: request.ProviderRef, ModelRef: request.ModelRef,
		Layer: request.Layer, EffectiveFrom: request.EffectiveFrom, EffectiveTo: request.EffectiveTo,
		Components: request.Components, DailyPriceBands: request.DailyPriceBands,
		SourceEvidenceRef: request.SourceEvidenceRef,
	}, actorRef)
	if err != nil {
		writeEnterpriseFinanceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, record)
}

// ImportOfficialEnterpriseRateCardDraft accepts already-retrieved, hashed
// source evidence. UniRouter performs no network fetch here and always writes a
// provider_list draft that still requires independent approval and activation.
func (h *Handler) ImportOfficialEnterpriseRateCardDraft(w http.ResponseWriter, r *http.Request) {
	var request importOfficialRateCardRequest
	if !decodeEnterpriseFinanceJSON(w, r, &request) {
		return
	}
	orgID, err := resolveActorOrganization(r.Context(), request.OrgID)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	actorRef, err := enterpriseFinanceActorRef(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "Authenticated finance actor required"})
		return
	}
	if strings.TrimSpace(request.RateCardRef) == "" {
		request.RateCardRef, err = newEnterpriseFinanceRef("rate")
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rate Card reference generation failed"})
			return
		}
	}
	if strings.TrimSpace(request.EvidenceRef) == "" {
		request.EvidenceRef, err = newEnterpriseFinanceRef("rate_source")
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Rate source reference generation failed"})
			return
		}
	}
	fetchedAt := request.FetchedAt.UTC().Truncate(time.Microsecond)
	staleAfter := request.StaleAfter.UTC().Truncate(time.Microsecond)
	store, _ := enterpriseFinanceStore(h)
	record, err := store.ImportOfficialRateCardDraft(r.Context(), enterprisefinance.OfficialRateDraftImport{
		Card: enterprisefinance.RateCard{
			RateCardRef: request.RateCardRef, OrgID: orgID, ProviderRef: request.ProviderRef,
			ModelRef: request.ModelRef, EffectiveFrom: request.EffectiveFrom.UTC().Truncate(time.Microsecond),
			EffectiveTo: normalizeOptionalEnterpriseTime(request.EffectiveTo), Components: request.Components,
			DailyPriceBands: request.DailyPriceBands,
		},
		Evidence: enterprisefinance.RateSourceEvidence{
			EvidenceRef: request.EvidenceRef, SourceURL: request.SourceURL, SourceRevision: request.SourceRevision,
			FetchedAt: fetchedAt, PayloadSHA256: request.PayloadSHA256, TransformVersion: request.TransformVersion,
			LastSuccessAt: fetchedAt, StaleAfter: staleAfter,
		},
	}, actorRef)
	if err != nil {
		writeEnterpriseFinanceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"record": record, "import_state": "draft", "automatic_activation": false,
		"network_fetch_performed": false,
	})
}

func (h *Handler) BootstrapLegacyEnterpriseRateCardDrafts(w http.ResponseWriter, r *http.Request) {
	var request bootstrapLegacyRateCardsRequest
	if !decodeEnterpriseFinanceJSON(w, r, &request) {
		return
	}
	orgID, err := resolveActorOrganization(r.Context(), request.OrgID)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	actorRef, err := enterpriseFinanceActorRef(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "Authenticated finance actor required"})
		return
	}
	store, _ := enterpriseFinanceStore(h)
	result, err := store.BootstrapLegacyProviderListDrafts(r.Context(), orgID, actorRef)
	if err != nil {
		writeEnterpriseFinanceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (h *Handler) PreflightEnterpriseRateCardAuthority(w http.ResponseWriter, r *http.Request) {
	var request requestPricingPreflightRequest
	if !decodeEnterpriseFinanceJSON(w, r, &request) {
		return
	}
	orgID, err := resolveActorOrganization(r.Context(), request.OrgID)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	if len(request.Models) == 0 || len(request.Models) > 1000 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "models must contain 1 to 1000 explicit model_ref values"})
		return
	}
	store, _ := enterpriseFinanceStore(h)
	now := time.Now().UTC().Truncate(time.Microsecond)
	type result struct {
		ModelRef        string `json:"model_ref"`
		State           string `json:"state"`
		RateCardRef     string `json:"rate_card_ref,omitempty"`
		RateCardVersion int64  `json:"rate_card_version,omitempty"`
		Layer           string `json:"layer,omitempty"`
		ProviderRef     string `json:"provider_ref,omitempty"`
		PricingVersion  string `json:"pricing_version,omitempty"`
		Error           string `json:"error,omitempty"`
	}
	items := make([]result, 0, len(request.Models))
	seen := make(map[string]struct{}, len(request.Models))
	ready := true
	for _, model := range request.Models {
		modelRef := strings.TrimSpace(model.ModelRef)
		if modelRef == "" {
			ready = false
			items = append(items, result{State: "not_ready", Error: "model_ref is required"})
			continue
		}
		if _, duplicate := seen[modelRef]; duplicate {
			continue
		}
		seen[modelRef] = struct{}{}
		quote, resolveErr := store.ResolveRequestPriceQuote(r.Context(), orgID, modelRef, now)
		if resolveErr != nil {
			ready = false
			items = append(items, result{ModelRef: modelRef, State: "not_ready", Error: resolveErr.Error()})
			continue
		}
		items = append(items, result{
			ModelRef: modelRef, State: "ready", RateCardRef: quote.RateCardRef,
			RateCardVersion: quote.RateCardVersion, Layer: quote.Layer, ProviderRef: quote.ProviderRef,
			PricingVersion: quote.PricingVersion,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"authority": "versioned_rate_card", "org_id": orgID, "ready": ready,
		"checked_at": now, "models": items,
		"switch_instruction": "Set REQUEST_PRICING_AUTHORITY=rate_card only when ready=true, then restart UniRouter.",
	})
}

func newEnterpriseFinanceRef(prefix string) (string, error) {
	var entropy [12]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", err
	}
	return strings.TrimSpace(prefix) + "_" + hex.EncodeToString(entropy[:]), nil
}

func normalizeOptionalEnterpriseTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := value.UTC().Truncate(time.Microsecond)
	return &normalized
}

func (h *Handler) ApproveEnterpriseRateCard(w http.ResponseWriter, r *http.Request) {
	var request rateCardTransitionRequest
	if !decodeEnterpriseFinanceJSON(w, r, &request) {
		return
	}
	orgID, err := resolveActorOrganization(r.Context(), request.OrgID)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	actorRef, err := enterpriseFinanceActorRef(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "Authenticated finance actor required"})
		return
	}
	store, _ := enterpriseFinanceStore(h)
	record, err := store.ApproveRateCard(
		r.Context(), orgID, r.PathValue("rate_card_ref"), request.ExpectedVersion, actorRef,
	)
	if err != nil {
		writeEnterpriseFinanceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (h *Handler) ActivateEnterpriseRateCard(w http.ResponseWriter, r *http.Request) {
	var request rateCardTransitionRequest
	if !decodeEnterpriseFinanceJSON(w, r, &request) {
		return
	}
	orgID, err := resolveActorOrganization(r.Context(), request.OrgID)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	actorRef, err := enterpriseFinanceActorRef(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "Authenticated finance actor required"})
		return
	}
	store, _ := enterpriseFinanceStore(h)
	record, err := store.ActivateRateCardWithSupersede(
		r.Context(), orgID, r.PathValue("rate_card_ref"), request.ExpectedVersion,
		enterprisefinance.RateCardActivationOptions{
			SupersedesRateCardRef:     request.SupersedesRateCardRef,
			ExpectedSupersededVersion: request.ExpectedSupersededVersion,
		},
		actorRef, time.Now().UTC().Truncate(time.Microsecond),
	)
	if err != nil {
		writeEnterpriseFinanceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (h *Handler) GetEnterprisePricingSnapshot(w http.ResponseWriter, r *http.Request) {
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	store, _ := enterpriseFinanceStore(h)
	record, err := store.GetPricingSnapshot(r.Context(), orgID, r.PathValue("request_ref"))
	if err != nil {
		writeEnterpriseFinanceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (h *Handler) ListEnterpriseAllocationRuns(w http.ResponseWriter, r *http.Request) {
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	periodRef := strings.TrimSpace(r.URL.Query().Get("period_ref"))
	store, _ := enterpriseFinanceStore(h)
	items, err := store.ListAllocationRuns(r.Context(), orgID, periodRef)
	if err != nil {
		writeEnterpriseFinanceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": items, "total": len(items), "org_id": orgID, "fiscal_period_ref": periodRef,
		"ledger_mutation":      enterprisefinance.LedgerMutationNone,
		"customer_debt_action": enterprisefinance.CustomerDebtNone,
	})
}

func (h *Handler) GetEnterpriseAllocationRun(w http.ResponseWriter, r *http.Request) {
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	store, _ := enterpriseFinanceStore(h)
	record, err := store.GetAllocationRun(r.Context(), orgID, r.PathValue("allocation_run_ref"))
	if err != nil {
		writeEnterpriseFinanceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (h *Handler) CreateEnterpriseAllocationRun(w http.ResponseWriter, r *http.Request) {
	var request createAllocationRunRequest
	if !decodeEnterpriseFinanceJSON(w, r, &request) {
		return
	}
	orgID, err := resolveActorOrganization(r.Context(), request.OrgID)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	actorRef, err := enterpriseFinanceActorRef(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "Authenticated finance actor required"})
		return
	}
	results := make([]enterprisefinance.AllocationResult, 0, len(request.Results))
	for _, item := range request.Results {
		results = append(results, enterprisefinance.AllocationResult{
			ContractVersion:  enterprisefinance.ContractVersion,
			AllocationRunRef: request.AllocationRunRef, RequestRef: item.RequestRef,
			FiscalPeriodRef: request.FiscalPeriodRef, SourceRevision: request.SourceRevision,
			Target: item.Target, State: item.State, RuleRef: item.RuleRef, RuleRevision: item.RuleRevision,
			Amount: item.Amount, LedgerMutation: enterprisefinance.LedgerMutationNone,
			CustomerDebtAction: enterprisefinance.CustomerDebtNone,
		})
	}
	store, _ := enterpriseFinanceStore(h)
	result, err := store.ApplyAllocationRun(r.Context(), enterprisefinance.AllocationRunRequest{
		ContractVersion: enterprisefinance.ContractVersion, AllocationRunRef: request.AllocationRunRef,
		OrgID: orgID, FiscalPeriodRef: request.FiscalPeriodRef, SourceRevision: request.SourceRevision,
		RuleSetRef: request.RuleSetRef, RuleSetRevision: request.RuleSetRevision,
		SupersedesRunRef: request.SupersedesRunRef, CreatedBy: actorRef, Results: results,
	})
	if err != nil {
		writeEnterpriseFinanceError(w, err)
		return
	}
	status := http.StatusCreated
	if result.Result == enterprisefinance.AllocationApplyDuplicate {
		status = http.StatusOK
	}
	writeJSON(w, status, result)
}

func (h *Handler) GetEnterpriseAllocationSummary(w http.ResponseWriter, r *http.Request) {
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	store, _ := enterpriseFinanceStore(h)
	summary, err := store.GetReconciliationSummary(r.Context(), orgID, r.URL.Query().Get("period_ref"))
	if err != nil {
		writeEnterpriseFinanceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (h *Handler) ListEnterpriseAdjustmentJournals(w http.ResponseWriter, r *http.Request) {
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	store, _ := enterpriseFinanceStore(h)
	items, err := store.ListLatestAdjustmentJournals(r.Context(), orgID, r.URL.Query().Get("period_ref"))
	if err != nil {
		writeEnterpriseFinanceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": items, "total": len(items), "org_id": orgID,
		"gl_export_state": enterprisefinance.GLExportDisabled,
	})
}

func (h *Handler) CreateEnterpriseAdjustmentJournal(w http.ResponseWriter, r *http.Request) {
	var request createAdjustmentJournalRequest
	if !decodeEnterpriseFinanceJSON(w, r, &request) {
		return
	}
	orgID, err := resolveActorOrganization(r.Context(), request.OrgID)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	actorRef, err := enterpriseFinanceActorRef(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "Authenticated finance actor required"})
		return
	}
	store, _ := enterpriseFinanceStore(h)
	record, err := store.CreateAdjustmentJournalDraft(r.Context(), enterprisefinance.AdjustmentJournal{
		JournalRef: request.JournalRef, OrgID: orgID, FiscalPeriodRef: request.FiscalPeriodRef,
		RequestRef: request.RequestRef, EntryType: request.EntryType,
		ReversalOfJournalRef: request.ReversalOfJournalRef, Target: request.Target,
		Amount: request.Amount, Reason: request.Reason,
		SourceAllocationRunRef: request.SourceAllocationRunRef, CreatedBy: actorRef,
	})
	if err != nil {
		writeEnterpriseFinanceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, record)
}

func (h *Handler) ApproveEnterpriseAdjustmentJournal(w http.ResponseWriter, r *http.Request) {
	var request approveAdjustmentJournalRequest
	if !decodeEnterpriseFinanceJSON(w, r, &request) {
		return
	}
	orgID, err := resolveActorOrganization(r.Context(), request.OrgID)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	actorRef, err := enterpriseFinanceActorRef(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"error": "Authenticated finance actor required"})
		return
	}
	store, _ := enterpriseFinanceStore(h)
	record, err := store.ApproveAdjustmentJournal(
		r.Context(), orgID, r.PathValue("journal_ref"), request.ExpectedVersion, actorRef,
	)
	if err != nil {
		writeEnterpriseFinanceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func parseEnterpriseFinanceLimit(raw string, fallback int) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 || value > 500 {
		return 0, errors.New("invalid limit")
	}
	return value, nil
}
