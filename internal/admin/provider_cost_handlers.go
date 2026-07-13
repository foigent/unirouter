package admin

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/middleware"
)

type providerCostReconciliationMutation struct {
	OrgID       int64  `json:"org_id"`
	RequestID   string `json:"request_id"`
	AttemptID   string `json:"attempt_id"`
	Action      string `json:"action"`
	Reason      string `json:"reason"`
	EvidenceRef string `json:"evidence_ref"`
}

type pendingProviderCost struct {
	OrgID             int64     `json:"org_id"`
	RequestID         string    `json:"request_id"`
	AttemptID         string    `json:"attempt_id"`
	APIKeyID          int64     `json:"api_key_id"`
	Provider          string    `json:"provider"`
	Channel           string    `json:"channel"`
	State             string    `json:"state"`
	OutcomeCertainty  string    `json:"outcome_certainty"`
	CalculatedCost    *int64    `json:"calculated_cost,omitempty"`
	ChargedCost       *int64    `json:"charged_cost,omitempty"`
	UpstreamRequestID *string   `json:"upstream_request_id,omitempty"`
	StartedAt         time.Time `json:"started_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func (h *Handler) ListPendingProviderCosts(w http.ResponseWriter, r *http.Request) {
	orgID, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("org_id")), 10, 64)
	if err != nil || orgID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "A positive org_id is required"})
		return
	}
	rows, err := h.db.Query(r.Context(),
		`SELECT org_id, request_id, attempt_id, api_key_id, provider, channel_name,
		        state, outcome_certainty, calculated_cost, charged_cost,
		        upstream_request_id, started_at, updated_at
		 FROM billing_provider_attempts
		 WHERE org_id=$1 AND state IN (
		   'dispatch_started','response_accepted','outcome_unknown','response_unusable','funding_pending',
		   'delivery_pending','delivery_failed'
		 )
		 ORDER BY updated_at, request_id, attempt_id LIMIT 100`, orgID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Pending provider costs unavailable"})
		return
	}
	defer rows.Close()
	items := make([]pendingProviderCost, 0)
	for rows.Next() {
		var item pendingProviderCost
		var calculatedCost, chargedCost sql.NullInt64
		var upstreamRequestID sql.NullString
		if err := rows.Scan(&item.OrgID, &item.RequestID, &item.AttemptID, &item.APIKeyID,
			&item.Provider, &item.Channel, &item.State, &item.OutcomeCertainty,
			&calculatedCost, &chargedCost, &upstreamRequestID,
			&item.StartedAt, &item.UpdatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Pending provider costs unavailable"})
			return
		}
		if calculatedCost.Valid {
			item.CalculatedCost = &calculatedCost.Int64
		}
		if chargedCost.Valid {
			item.ChargedCost = &chargedCost.Int64
		}
		if upstreamRequestID.Valid {
			item.UpstreamRequestID = &upstreamRequestID.String
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Pending provider costs unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"data": items, "org_id": orgID, "limit": 100})
}

func (h *Handler) ReconcileProviderCost(w http.ResponseWriter, r *http.Request) {
	if h.providerCostReconciler == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"error": "Provider cost reconciliation is unavailable"})
		return
	}
	var mutation providerCostReconciliationMutation
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&mutation); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid reconciliation payload"})
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Only one reconciliation object is allowed"})
		return
	}
	request := billing.ProviderReconciliationRequest{
		OrgID: mutation.OrgID, RequestID: mutation.RequestID, AttemptID: mutation.AttemptID,
		Action: mutation.Action, ActorUserID: middleware.GetUserIDFromContext(r.Context()),
		Reason: mutation.Reason, EvidenceRef: mutation.EvidenceRef,
	}
	result, err := h.providerCostReconciler.ReconcileProviderAttempt(r.Context(), request)
	if err != nil {
		switch {
		case errors.Is(err, billing.ErrProviderReconciliationConflict):
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Reconciliation conflicts with existing durable evidence"})
		case errors.Is(err, billing.ErrProviderReconciliationUnsafe), errors.Is(err, billing.ErrSettlementFundingPending):
			writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Reconciliation action is not financially authorized"})
		case errors.Is(err, billing.ErrProviderReconciliationInvalid):
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "Invalid reconciliation request"})
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "Reconciliation failed"})
		}
		return
	}
	h.recordAuditForOrgProjectFromRequest(r, result.OrgID, 0,
		"provider_cost."+strings.TrimSpace(result.Action), "provider_cost_reconciliation",
		fmt.Sprintf("%s/%s", result.RequestID, result.AttemptID), map[string]interface{}{
			"action": result.Action, "api_key_id": result.APIKeyID,
			"terminal_transaction_id": result.TerminalTransactionID,
			"customer_charged_cost":   result.CustomerChargedCost,
			"customer_refund":         result.CustomerRefund, "evidence_ref": mutation.EvidenceRef,
		})
	writeJSON(w, http.StatusOK, result)
}
