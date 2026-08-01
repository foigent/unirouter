package admin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/uniroute/unirouter/internal/riskcontrol"
)

// RiskControlOfflineContract exposes only immutable synthetic evidence and the
// fail-closed no-enforcement boundary for advice and proposal review.
func (h *Handler) RiskControlOfflineContract(w http.ResponseWriter, _ *http.Request) {
	setNoStoreHeaders(w)
	schema, fixtures, golden, err := riskcontrol.ArtifactDigests()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error": map[string]string{
				"code":    "risk_control_contract_unavailable",
				"message": "The offline risk-control contract is unavailable.",
			},
		})
		return
	}
	request, err := riskcontrol.SampleRequestBytes()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error": map[string]string{
				"code":    "risk_control_fixture_unavailable",
				"message": "The synthetic risk-control fixture is unavailable.",
			},
		})
		return
	}
	conformance, err := riskcontrol.RunConformance()
	if err != nil || conformance.Summary.Failed != 0 {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error": map[string]string{
				"code":    "risk_control_conformance_failed",
				"message": "The offline risk-control evidence is not conformant.",
			},
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"contract_version":  riskcontrol.ContractVersion,
		"report_version":    riskcontrol.ReportVersion,
		"assertion_version": riskcontrol.AssertionVersion,
		"proposal_version":  riskcontrol.ProposalVersion,
		"policy_version":    riskcontrol.PolicyVersion,
		"sample_request":    json.RawMessage(request),
		"artifacts": map[string]interface{}{
			"schema":        schema,
			"fixtures":      fixtures,
			"golden_report": golden,
			"dependencies":  conformance.Dependencies,
		},
		"authority_boundary": map[string]interface{}{
			"mode":                           "offline_advice_and_shadow_evidence",
			"persistent":                     false,
			"remote_risk_dependency":         false,
			"provider_or_shadow_traffic":     false,
			"credentials_accepted":           false,
			"runtime_policy_read":            false,
			"runtime_enforcement_authorized": false,
			"runtime_promotion_authorized":   false,
			"human_review_required":          true,
			"external_decision_gate":         riskcontrol.PromotionGate,
			"runtime_mutation":               riskcontrol.RuntimeMutationNone,
		},
	})
}

// PreviewRiskControl composes B8/B10/B11 fixture evidence without provider
// traffic, persistence, signing, policy reads, promotion, or enforcement.
func (h *Handler) PreviewRiskControl(w http.ResponseWriter, r *http.Request) {
	setNoStoreHeaders(w)
	r.Body = http.MaxBytesReader(w, r.Body, riskcontrol.MaxInputBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]interface{}{
			"error": map[string]string{
				"code":    "input_size_invalid",
				"message": "Offline risk-control evidence exceeds the bounded preview size.",
			},
		})
		return
	}
	report, err := riskcontrol.Evaluate(body)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, riskcontrol.ErrConflict) {
			status = http.StatusConflict
		}
		code := "risk_control_invalid"
		var contractErr *riskcontrol.ContractError
		if errors.As(err, &contractErr) {
			code = contractErr.Code
		}
		writeJSON(w, status, map[string]interface{}{
			"error": map[string]string{
				"code":    code,
				"message": "Offline risk-control evidence did not satisfy the strict contract.",
			},
		})
		return
	}
	writeJSON(w, http.StatusOK, report)
}
