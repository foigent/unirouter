package gateway

import (
	"context"
	"log"
	"net/http"

	"github.com/uniroute/unirouter/internal/policybilling"
)

// RecordEarlyRateAdmission is the topology-neutral evidence adapter for the
// pre-governance policy boundary. It preserves the normal execution trace
// event type without adding a model, selector, policy ref, or request body.
func (relay *Relay) RecordEarlyRateAdmission(ctx context.Context, evidence policybilling.EarlyRateAdmissionEvidence) {
	status := http.StatusServiceUnavailable
	switch evidence.ErrorCode {
	case policybilling.ErrorCodeRPSExceeded, policybilling.ErrorCodeRPMExceeded:
		status = http.StatusTooManyRequests
	case policybilling.ErrorCodeConflict:
		status = http.StatusConflict
	}
	log.Printf(
		"[limit-policy] early_gateway_admission operation_id=%s org_id=%d api_key_id=%d result=%s error_code=%s policy_version=%s",
		evidence.OperationID, evidence.OrgID, evidence.APIKeyID, evidence.Result, evidence.ErrorCode, evidence.PolicyVersion,
	)
	relay.recordTraceEvent(
		ctx, evidence.OperationID, "limit_policy.admission", "admission", "warning", "", "", nil, status, 0,
		map[string]interface{}{
			"limit_policy_result":      evidence.Result,
			"limit_policy_error_code":  evidence.ErrorCode,
			"limit_policy_set_version": evidence.PolicyVersion,
			"admission_phase":          "rate",
			"pre_governance":           true,
		},
	)
}

var _ policybilling.EarlyRateAdmissionEvidenceSink = (*Relay)(nil)
