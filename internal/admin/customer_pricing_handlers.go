package admin

import (
	"net/http"
	"strconv"
	"time"

	"github.com/uniroute/unirouter/internal/enterprisefinance"
)

type customerPriceTableDraft struct {
	OrgID         int64                                  `json:"org_id"`
	Ref           string                                 `json:"table_ref"`
	Version       int64                                  `json:"version"`
	EffectiveFrom time.Time                              `json:"effective_from"`
	EffectiveTo   *time.Time                             `json:"effective_to,omitempty"`
	Entries       []enterprisefinance.CustomerPriceEntry `json:"entries"`
}

type customerPriceBindingRequest struct {
	OrgID            int64  `json:"org_id"`
	ProjectID        int64  `json:"project_id"`
	TableRef         string `json:"table_ref"`
	TableVersion     int64  `json:"table_version"`
	ExpectedRevision *int64 `json:"expected_revision"`
}

func customerPricingWriteScope(w http.ResponseWriter, r *http.Request, requestedOrg int64) (int64, string, bool) {
	orgID, err := resolveActorOrganization(r.Context(), requestedOrg)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return 0, "", false
	}
	actor, err := enterpriseFinanceActorRef(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Authenticated finance actor required"})
		return 0, "", false
	}
	return orgID, actor, true
}

func customerPricingVersion(w http.ResponseWriter, r *http.Request) (int64, bool) {
	version, err := strconv.ParseInt(r.PathValue("version"), 10, 64)
	if err != nil || version <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Positive price table version required"})
		return 0, false
	}
	return version, true
}

func (h *Handler) CreateCustomerPriceTable(w http.ResponseWriter, r *http.Request) {
	var request customerPriceTableDraft
	if !decodeEnterpriseFinanceJSON(w, r, &request) {
		return
	}
	orgID, actor, ok := customerPricingWriteScope(w, r, request.OrgID)
	if !ok {
		return
	}
	store, _ := enterpriseFinanceStore(h)
	table, err := store.CreateCustomerPriceTable(r.Context(), enterprisefinance.CustomerPriceTable{
		OrgID: orgID, Ref: request.Ref, Version: request.Version,
		EffectiveFrom: request.EffectiveFrom, EffectiveTo: request.EffectiveTo, Entries: request.Entries,
	}, actor)
	if err != nil {
		writeEnterpriseFinanceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, table)
}

func (h *Handler) GetCustomerPriceTable(w http.ResponseWriter, r *http.Request) {
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	version, ok := customerPricingVersion(w, r)
	if !ok {
		return
	}
	store, _ := enterpriseFinanceStore(h)
	table, err := store.GetCustomerPriceTable(r.Context(), orgID, r.PathValue("table_ref"), version)
	if err != nil {
		writeEnterpriseFinanceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, table)
}

func (h *Handler) ApproveCustomerPriceTable(w http.ResponseWriter, r *http.Request) {
	var request struct {
		OrgID int64 `json:"org_id"`
	}
	if !decodeEnterpriseFinanceJSON(w, r, &request) {
		return
	}
	orgID, actor, ok := customerPricingWriteScope(w, r, request.OrgID)
	if !ok {
		return
	}
	version, ok := customerPricingVersion(w, r)
	if !ok {
		return
	}
	store, _ := enterpriseFinanceStore(h)
	if err := store.ApproveCustomerPriceTable(r.Context(), orgID, r.PathValue("table_ref"), version, actor); err != nil {
		writeEnterpriseFinanceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) BindCustomerPriceTable(w http.ResponseWriter, r *http.Request) {
	var request customerPriceBindingRequest
	if !decodeEnterpriseFinanceJSON(w, r, &request) {
		return
	}
	if request.ExpectedRevision == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expected_revision is required"})
		return
	}
	orgID, actor, ok := customerPricingWriteScope(w, r, request.OrgID)
	if !ok {
		return
	}
	store, _ := enterpriseFinanceStore(h)
	binding, err := store.BindCustomerPriceTable(r.Context(), enterprisefinance.CustomerPriceBinding{
		OrgID: orgID, ProjectID: request.ProjectID, TableRef: request.TableRef, TableVersion: request.TableVersion,
	}, *request.ExpectedRevision, actor)
	if err != nil {
		writeEnterpriseFinanceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, binding)
}

func (h *Handler) GetCustomerPriceBinding(w http.ResponseWriter, r *http.Request) {
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	var projectID int64
	if value := r.URL.Query().Get("project_id"); value != "" {
		projectID, err = strconv.ParseInt(value, 10, 64)
		if err != nil || projectID < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "project_id must be nonnegative"})
			return
		}
	}
	store, _ := enterpriseFinanceStore(h)
	binding, err := store.GetCustomerPriceBinding(r.Context(), orgID, projectID)
	if err != nil {
		writeEnterpriseFinanceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, binding)
}

func (h *Handler) GetCustomerChargeQuote(w http.ResponseWriter, r *http.Request) {
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	requestID := r.PathValue("request_ref")
	if requestID == "" || len(requestID) > 64 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request reference"})
		return
	}
	store, _ := enterpriseFinanceStore(h)
	receipt, err := store.GetCustomerChargeQuote(r.Context(), orgID, requestID)
	if err != nil {
		writeEnterpriseFinanceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, receipt)
}

func (h *Handler) ListCustomerChargeQuoteOverruns(w http.ResponseWriter, r *http.Request) {
	orgID, err := resolveRequestOrganization(r)
	if err != nil {
		writeOrganizationScopeError(w, err)
		return
	}
	query := r.URL.Query()
	limit, err := parseEnterpriseFinanceLimit(query.Get("limit"), 50)
	if err != nil {
		writeEnterpriseFinanceError(w, enterprisefinance.ErrInvalid)
		return
	}
	after := enterprisefinance.CustomerChargeQuoteOverrunCursor{RequestID: query.Get("after_request_id")}
	if raw := query.Get("after_provider_cost_pending_at"); raw != "" {
		after.ProviderPendingAt, err = time.Parse(time.RFC3339Nano, raw)
		if err != nil || after.ProviderPendingAt.IsZero() {
			writeEnterpriseFinanceError(w, enterprisefinance.ErrInvalid)
			return
		}
	}
	store, _ := enterpriseFinanceStore(h)
	page, err := store.ListCustomerChargeQuoteOverruns(r.Context(), orgID, after, limit)
	if err != nil {
		writeEnterpriseFinanceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}
