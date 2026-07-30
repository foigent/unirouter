package admin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/uniroute/unirouter/internal/agenttrust"
)

// AgentTrustOfflineContract exposes only immutable, synthetic, metadata-only
// Agent authority and Governance Digital Twin evidence.
func (h *Handler) AgentTrustOfflineContract(w http.ResponseWriter, _ *http.Request) {
	setNoStoreHeaders(w)
	schema, fixtures, golden, err := agenttrust.ArtifactDigests()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error": map[string]string{
				"code":    "agent_trust_contract_unavailable",
				"message": "The offline Agent Trust contract is unavailable.",
			},
		})
		return
	}
	request, err := agenttrust.SampleRequestBytes()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error": map[string]string{
				"code":    "agent_trust_fixture_unavailable",
				"message": "The synthetic Agent Trust fixture is unavailable.",
			},
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"contract_version": agenttrust.ContractVersion,
		"report_version":   agenttrust.ReportVersion,
		"receipt_version":  agenttrust.ReceiptVersion,
		"sample_request":   json.RawMessage(request),
		"artifacts": map[string]interface{}{
			"schema":        schema,
			"fixtures":      fixtures,
			"golden_report": golden,
		},
		"authority_boundary": map[string]interface{}{
			"mode":                             "offline_preview",
			"persistent":                       false,
			"signing_key_accepted":             false,
			"fixture_public_key_runtime_trust": false,
			"synchronous_runtime_dependency":   false,
			"runtime_enforcement_authorized":   false,
			"runtime_mutation":                 agenttrust.RuntimeMutationNone,
		},
	})
}

// PreviewAgentTrust verifies one bounded trust review without key ingestion,
// provider traffic, database writes, policy promotion, or runtime revocation.
func (h *Handler) PreviewAgentTrust(w http.ResponseWriter, r *http.Request) {
	setNoStoreHeaders(w)
	r.Body = http.MaxBytesReader(w, r.Body, agenttrust.MaxInputBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]interface{}{
			"error": map[string]string{
				"code":    "input_size_invalid",
				"message": "Offline Agent Trust evidence exceeds the bounded preview size.",
			},
		})
		return
	}
	report, err := agenttrust.Evaluate(body)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, agenttrust.ErrConflict) {
			status = http.StatusConflict
		}
		code := "agent_trust_invalid"
		var contractErr *agenttrust.ContractError
		if errors.As(err, &contractErr) {
			code = contractErr.Code
		}
		writeJSON(w, status, map[string]interface{}{
			"error": map[string]string{
				"code":    code,
				"message": "Offline Agent Trust evidence did not satisfy the strict contract.",
			},
		})
		return
	}
	writeJSON(w, http.StatusOK, report)
}
