package admin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/uniroute/unirouter/internal/provenancepassport"
)

// ProvenancePassportOfflineContract exposes only immutable fixture and
// authority identities for the read-only passport workspace.
func (h *Handler) ProvenancePassportOfflineContract(w http.ResponseWriter, _ *http.Request) {
	setNoStoreHeaders(w)
	schema, fixtures, golden, err := provenancepassport.ArtifactDigests()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error": map[string]string{
				"code":    "provenance_passport_contract_unavailable",
				"message": "The offline Provenance Passport contract is unavailable.",
			},
		})
		return
	}
	request, err := provenancepassport.SampleRequestBytes()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error": map[string]string{
				"code":    "provenance_passport_fixture_unavailable",
				"message": "The synthetic Provenance Passport fixture is unavailable.",
			},
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"contract_version": provenancepassport.ContractVersion,
		"report_version":   provenancepassport.ReportVersion,
		"sample_request":   json.RawMessage(request),
		"artifacts": map[string]interface{}{
			"schema":        schema,
			"fixtures":      fixtures,
			"golden_report": golden,
		},
		"authority_boundary": map[string]interface{}{
			"mode":                           "offline_preview",
			"persistent":                     false,
			"second_connector_registered":    false,
			"second_connector_runtime_owned": false,
			"credentials_accepted":           false,
			"cursor_or_checkpoint_accepted":  false,
			"synchronous_runtime_dependency": false,
			"runtime_mutation":               provenancepassport.RuntimeMutationNone,
		},
	})
}

// PreviewProvenancePassport maps exact fixture Source bytes without Feed,
// delivery, credential, Connector registry or runtime mutation.
func (h *Handler) PreviewProvenancePassport(w http.ResponseWriter, r *http.Request) {
	setNoStoreHeaders(w)
	r.Body = http.MaxBytesReader(w, r.Body, provenancepassport.MaxInputBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]interface{}{
			"error": map[string]string{
				"code":    "input_size_invalid",
				"message": "Offline Provenance Passport evidence exceeds the bounded preview size.",
			},
		})
		return
	}
	report, err := provenancepassport.Evaluate(body)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, provenancepassport.ErrConflict) {
			status = http.StatusConflict
		}
		code := "provenance_passport_invalid"
		var contractErr *provenancepassport.ContractError
		if errors.As(err, &contractErr) {
			code = contractErr.Code
		}
		writeJSON(w, status, map[string]interface{}{
			"error": map[string]string{
				"code":    code,
				"message": "Offline Provenance Passport evidence did not satisfy the strict contract.",
			},
		})
		return
	}
	writeJSON(w, http.StatusOK, report)
}
