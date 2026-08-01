package admin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/uniroute/unirouter/internal/governancesim"
)

// GovernanceSimulationOfflineContract exposes only immutable local contract
// identities and credential-free current Provider Pack metadata.
func (h *Handler) GovernanceSimulationOfflineContract(w http.ResponseWriter, _ *http.Request) {
	setNoStoreHeaders(w)
	schema, fixtures, golden, err := governancesim.ArtifactDigests()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error": map[string]string{
				"code":    "governance_simulation_contract_unavailable",
				"message": "The offline governance simulation contract is unavailable.",
			},
		})
		return
	}
	packs, err := governancesim.CurrentProviderPackSnapshots()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error": map[string]string{
				"code":    "provider_pack_catalog_unavailable",
				"message": "The credential-free Provider Pack catalog is unavailable.",
			},
		})
		return
	}
	sample, err := governancesim.SampleRequestBytes()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error": map[string]string{
				"code":    "governance_simulation_fixture_unavailable",
				"message": "The synthetic governance fixture is unavailable.",
			},
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"contract_version":  governancesim.ContractVersion,
		"report_version":    governancesim.ReportVersion,
		"release_version":   governancesim.ReleaseVersion,
		"policy_version":    "routing-config.v2",
		"selection_version": "routing-policy.v1",
		"evaluation_modes": []string{
			governancesim.ModeShadow,
			governancesim.ModeCanary,
			governancesim.ModeEnforced,
		},
		"release_stages": []string{
			governancesim.StageDraft,
			governancesim.StageValidated,
			governancesim.StageShadow,
			governancesim.StageCanary,
			governancesim.StageExternalGate,
		},
		"risk_classes": []string{
			governancesim.RiskUnchanged,
			governancesim.RiskCompatible,
			governancesim.RiskReviewRequired,
			governancesim.RiskBreaking,
		},
		"current_provider_packs": packs,
		"sample_request":         json.RawMessage(sample),
		"artifacts": map[string]interface{}{
			"schema":        schema,
			"fixtures":      fixtures,
			"golden_report": golden,
		},
		"authority_boundary": map[string]interface{}{
			"mode":                         "offline_preview",
			"persistent":                   false,
			"credentials_accepted":         false,
			"provider_network_access":      false,
			"runtime_promotion_authorized": false,
			"runtime_mutation":             governancesim.RuntimeMutationNone,
		},
	})
}

// PreviewGovernanceSimulation evaluates one supplied immutable snapshot pair
// in memory. It cannot promote routing, policy or Provider Pack state.
func (h *Handler) PreviewGovernanceSimulation(w http.ResponseWriter, r *http.Request) {
	setNoStoreHeaders(w)
	r.Body = http.MaxBytesReader(w, r.Body, governancesim.MaxInputBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]interface{}{
			"error": map[string]string{
				"code":    "input_size_invalid",
				"message": "Offline governance evidence exceeds the bounded preview size.",
			},
		})
		return
	}
	report, err := governancesim.Evaluate(body)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, governancesim.ErrConflict) {
			status = http.StatusConflict
		}
		code := "governance_simulation_invalid"
		var contractErr *governancesim.ContractError
		if errors.As(err, &contractErr) {
			code = contractErr.Code
		}
		writeJSON(w, status, map[string]interface{}{
			"error": map[string]string{
				"code":    code,
				"message": "Offline governance evidence did not satisfy the strict contract.",
			},
		})
		return
	}
	writeJSON(w, http.StatusOK, report)
}
