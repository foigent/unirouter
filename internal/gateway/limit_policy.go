package gateway

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/uniroute/unirouter/internal/limitpolicy"
	"github.com/uniroute/unirouter/internal/policybilling"
)

func contextWithLimitPolicyCollector(ctx context.Context) (context.Context, *limitpolicy.DecisionCollector) {
	return limitpolicy.ContextWithDecisionCollector(ctx)
}

func (relay *Relay) recordLimitPolicyAdmission(ctx context.Context, w http.ResponseWriter, collector *limitpolicy.DecisionCollector, requestID, model, requestedModel string, latencyMs int) {
	decisions, policySetVersion := collector.Snapshot()
	if len(decisions) == 0 {
		return
	}
	if policySetVersion != "" {
		w.Header().Set("X-UniRouter-Limit-Policy-Version", policySetVersion)
	}
	result, reason, enforceCount, shadowCount, chain := summarizeLimitPolicyDecisions(decisions)
	workloadClass, workloadWeight, workloadWaitMS, workloadQueueDepth := summarizeWorkloadDecision(decisions)
	if workloadClass != "" {
		w.Header().Set("X-UniRouter-Workload-Class", workloadClass)
	}
	severity := "info"
	if result != "allowed" {
		severity = "warning"
	}
	relay.recordTraceEvent(ctx, requestID, "limit_policy.admission", "admission", severity, model, requestedModel, nil, 0, latencyMs, map[string]interface{}{
		"limit_policy_set_version":    policySetVersion,
		"limit_policy_result":         result,
		"limit_policy_reason":         reason,
		"limit_policy_decision_count": len(decisions),
		"limit_policy_enforce_count":  enforceCount,
		"limit_policy_shadow_count":   shadowCount,
		"limit_policy_chain":          strings.Join(chain, ","),
		"workload_class":              workloadClass,
		"workload_weight":             workloadWeight,
		"workload_wait_ms":            workloadWaitMS,
		"workload_queue_depth":        workloadQueueDepth,
	})
}

func summarizeWorkloadDecision(decisions []limitpolicy.Decision) (class string, weight, waitMS, queueDepth int64) {
	for _, decision := range decisions {
		if decision.WorkloadClass == "" {
			continue
		}
		class = decision.WorkloadClass
		weight = decision.WorkloadWeight
		waitMS = decision.WaitMS
		queueDepth = decision.QueueDepth
	}
	return
}

func summarizeLimitPolicyDecisions(decisions []limitpolicy.Decision) (result, reason string, enforceCount, shadowCount int, chain []string) {
	result = "allowed"
	reason = limitpolicy.ReasonAllowed
	chain = make([]string, 0, len(decisions))
	for _, decision := range decisions {
		if decision.Mode == limitpolicy.ModeShadow {
			shadowCount++
		} else if decision.Mode == limitpolicy.ModeEnforce {
			enforceCount++
		}
		if decision.Decision == limitpolicy.DecisionDeny {
			result = "rejected"
			reason = decision.Reason
		} else if result != "rejected" && decision.Decision == limitpolicy.DecisionWouldDeny {
			result = "shadow_would_deny"
			reason = decision.Reason
		} else if result == "allowed" && decision.Decision == limitpolicy.DecisionAllow && decision.Reason != limitpolicy.ReasonAllowed {
			result = "degraded_allow"
			reason = decision.Reason
		}
		chain = append(chain, fmt.Sprintf("%s@%d#%s=%s/%s", decision.PolicyRef, decision.PolicyVersion, decision.SelectorHash, decision.Decision, decision.Reason))
	}
	return result, reason, enforceCount, shadowCount, chain
}

func writeLimitPolicyRuntimeRejection(w http.ResponseWriter, errorCode string) bool {
	switch errorCode {
	case policybilling.ErrorCodeRPSExceeded:
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusTooManyRequests, "rate_limit_error", errorCode, "Shared request-per-second limit exceeded")
	case policybilling.ErrorCodeRPMExceeded:
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "rate_limit_error", errorCode, "Shared request-per-minute limit exceeded")
	case policybilling.ErrorCodeTPMExceeded:
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "rate_limit_error", errorCode, "Shared token-per-minute reservation limit exceeded")
	case policybilling.ErrorCodeConcurrencyExceeded:
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusTooManyRequests, "rate_limit_error", errorCode, "Shared concurrent request limit exceeded")
	case policybilling.ErrorCodeWorkloadCapacity:
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusTooManyRequests, "rate_limit_error", errorCode, "Weighted workload capacity is temporarily full")
	case policybilling.ErrorCodeWorkloadConfig:
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusServiceUnavailable, "server_error", errorCode, "Workload admission configuration does not match active replicas")
	case policybilling.ErrorCodeStateUnavailable:
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusServiceUnavailable, "server_error", errorCode, "Shared limit-policy state is temporarily unavailable")
	case policybilling.ErrorCodeConflict:
		writeError(w, http.StatusConflict, "limit_policy_error", errorCode, "Operation identity conflicts with an existing limit reservation")
	default:
		return false
	}
	return true
}
