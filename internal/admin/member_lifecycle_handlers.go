package admin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/uniroute/unirouter/internal/memberlifecycle"
)

// MemberLifecycleOfflineContract exposes only immutable fixtures and
// IdP-neutral, non-sensitive lifecycle semantics.
func (h *Handler) MemberLifecycleOfflineContract(w http.ResponseWriter, _ *http.Request) {
	setNoStoreHeaders(w)
	schema, fixtures, golden, err := memberlifecycle.ArtifactDigests()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error": map[string]string{
				"code":    "member_lifecycle_contract_unavailable",
				"message": "The offline member lifecycle contract is unavailable.",
			},
		})
		return
	}
	events, err := memberlifecycle.SampleEventsBytes()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error": map[string]string{
				"code":    "member_lifecycle_fixture_unavailable",
				"message": "The synthetic member lifecycle fixture is unavailable.",
			},
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"contract_version": memberlifecycle.ContractVersion,
		"report_version":   memberlifecycle.ReportVersion,
		"operations": []string{
			memberlifecycle.OperationCreate,
			memberlifecycle.OperationUpdate,
			memberlifecycle.OperationSuspend,
			memberlifecycle.OperationDeprovision,
		},
		"sample_events": json.RawMessage(events),
		"artifacts": map[string]interface{}{
			"schema":        schema,
			"fixtures":      fixtures,
			"golden_report": golden,
		},
		"authority_boundary": map[string]interface{}{
			"mode":                      "offline_preview",
			"persistent":                false,
			"directory_vendor_selected": false,
			"idp_network_access":        false,
			"credentials_accepted":      false,
			"runtime_mutation":          memberlifecycle.RuntimeMutationNone,
		},
	})
}

// PreviewMemberLifecycle evaluates one synthetic or sanitized lifecycle event
// without changing users, sessions, memberships, keys, capabilities, or IdP
// state.
func (h *Handler) PreviewMemberLifecycle(w http.ResponseWriter, r *http.Request) {
	setNoStoreHeaders(w)
	r.Body = http.MaxBytesReader(w, r.Body, memberlifecycle.MaxInputBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]interface{}{
			"error": map[string]string{
				"code":    "input_size_invalid",
				"message": "Offline lifecycle evidence exceeds the bounded preview size.",
			},
		})
		return
	}
	report, err := memberlifecycle.Evaluate(body)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, memberlifecycle.ErrConflict) {
			status = http.StatusConflict
		}
		code := "member_lifecycle_invalid"
		var contractErr *memberlifecycle.ContractError
		if errors.As(err, &contractErr) {
			code = contractErr.Code
		}
		writeJSON(w, status, map[string]interface{}{
			"error": map[string]string{
				"code":    code,
				"message": "Offline lifecycle evidence did not satisfy the strict contract.",
			},
		})
		return
	}
	writeJSON(w, http.StatusOK, report)
}
