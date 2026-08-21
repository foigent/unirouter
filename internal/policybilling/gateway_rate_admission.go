package policybilling

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/uniroute/unirouter/internal/gatewayerror"
	"github.com/uniroute/unirouter/internal/limitpolicy"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/payloadpolicy"
)

type earlyRateAdmissionContextKey struct{}

// earlyRateAdmissionMarker is deliberately package-private. Freeze trusts it
// only when it is bound to the live authenticated subject and server-owned
// operation identity carried by the same request context.
type earlyRateAdmissionMarker struct {
	operationID    string
	requestedModel string
	orgID          int64
	apiKeyID       int64
	rateReceipt    limitpolicy.RateAdmissionReceipt
}

// EarlyRateAdmissionEvidence is the topology-neutral record emitted when the
// pre-governance rate boundary denies a request. It intentionally contains no
// model, selector, policy ref, credential ref, body, or provider detail.
type EarlyRateAdmissionEvidence struct {
	OperationID   string
	OrgID         int64
	APIKeyID      int64
	Result        string
	ErrorCode     string
	PolicyVersion string
}

type EarlyRateAdmissionEvidenceSink interface {
	// RecordEarlyRateAdmission must be bounded and must not add model or policy
	// topology to the supplied safe evidence shape.
	RecordEarlyRateAdmission(context.Context, EarlyRateAdmissionEvidence)
}

// rateAdmissionRuntime is deliberately narrower than Runtime: only the
// authoritative limit-policy service may issue an opaque receipt after a
// successful rate-only finalization. Gateway admission fails closed when a
// runtime does not implement this contract.
type rateAdmissionRuntime interface {
	FinalizeRateAdmission(context.Context, *limitpolicy.Reservation, string) (limitpolicy.RateAdmissionReceipt, error)
}

func (b *Billing) SetEarlyRateAdmissionEvidenceSink(sink EarlyRateAdmissionEvidenceSink) {
	if b == nil {
		return
	}
	b.earlyEvidenceMu.Lock()
	b.earlyRateEvidenceSink = sink
	b.earlyEvidenceMu.Unlock()
}

// GatewayRateAdmission consumes the request-rate portion of the distributed
// policy before model governance or any paid execution preparation. The body
// must already have passed retained-body accounting and strict JSON ingress.
//
// Each HTTP attempt has a new server operation identity, so an idempotency
// replay is intentionally another authenticated RPS/RPM attempt. The distinct
// rate-operation suffix prevents the later token/concurrency reservation from
// sharing a terminal Redis operation key with this phase.
func (b *Billing) GatewayRateAdmission(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if b == nil || b.runtime == nil || next == nil {
			b.writeEarlyRateError(w, r, r.Context(), limitpolicy.ErrStateUnavailable)
			return
		}
		apiKey := middleware.GetAPIKeyFromContext(r.Context())
		identity := middleware.GetRequestIdentityFromContext(r.Context())
		if !validEarlyRateIdentity(r.Context(), apiKey, identity.OperationID) {
			b.writeEarlyRateError(w, r, r.Context(), limitpolicy.ErrStateUnavailable)
			return
		}
		body, retained := payloadpolicy.BodyFromContext(r.Context())
		if !retained {
			b.writeEarlyRateError(w, r, r.Context(), limitpolicy.ErrStateUnavailable)
			return
		}
		rateRuntime, valid := b.runtime.(rateAdmissionRuntime)
		if !valid {
			b.writeEarlyRateError(w, r, r.Context(), limitpolicy.ErrStateUnavailable)
			return
		}
		requestedModel, modelErr := exactRequestedModel(body)

		rateOperationID, valid := limitpolicy.RateAdmissionOperationID(identity.OperationID)
		if !valid {
			b.writeEarlyRateError(w, r, r.Context(), limitpolicy.ErrStateUnavailable)
			return
		}
		admissionContext, _ := limitpolicy.ContextWithDecisionCollector(r.Context())
		reservation, err := b.runtime.Reserve(admissionContext, limitpolicy.AdmissionRequest{
			OperationID:    rateOperationID,
			Subject:        admissionSubject(r.Context(), apiKey, ""),
			RequestedModel: requestedModel,
			Phase:          limitpolicy.AdmissionPhaseUnscopedRate,
		})
		if err != nil {
			b.writeEarlyRateError(w, r, admissionContext, err)
			return
		}
		if reservation == nil {
			b.writeEarlyRateError(w, r, admissionContext, limitpolicy.ErrStateUnavailable)
			return
		}
		// Rate-window entries are the intended durable effect. Finalization makes
		// the per-operation handle terminal immediately and owns no TPM,
		// concurrency, or workload reservation that could leak into execution.
		rateReceipt, err := rateRuntime.FinalizeRateAdmission(admissionContext, reservation, identity.OperationID)
		if err != nil {
			b.writeEarlyRateError(w, r, admissionContext, limitpolicy.ErrStateUnavailable)
			return
		}
		if modelErr != nil {
			gatewayerror.Write(w, r.URL.Path, identity.OperationID, http.StatusBadRequest,
				"validation_error.invalid_request", "A non-empty model string is required")
			return
		}

		marker := earlyRateAdmissionMarker{
			operationID:    identity.OperationID,
			requestedModel: requestedModel,
			orgID:          middleware.GetOrgIDFromContext(r.Context()),
			apiKeyID:       middleware.GetKeyIDFromContext(r.Context()),
			rateReceipt:    rateReceipt,
		}
		ctx := context.WithValue(admissionContext, earlyRateAdmissionContextKey{}, marker)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func exactRequestedModel(body []byte) (string, error) {
	var document map[string]json.RawMessage
	if err := json.Unmarshal(body, &document); err != nil || document == nil {
		return "", errors.New("request body is not an object")
	}
	raw, exists := document["model"]
	if !exists {
		return "", errors.New("model is missing")
	}
	var model string
	if err := json.Unmarshal(raw, &model); err != nil || strings.TrimSpace(model) == "" {
		return "", errors.New("model is invalid")
	}
	return strings.TrimSpace(model), nil
}

func validEarlyRateIdentity(ctx context.Context, apiKey *models.ApiKey, operationID string) bool {
	return ctx != nil && apiKey != nil && apiKey.ID > 0 && apiKey.OrgID > 0 &&
		operationID != "" && len(operationID) <= 64-len(":rate") &&
		middleware.GetOrgIDFromContext(ctx) == apiKey.OrgID &&
		middleware.GetKeyIDFromContext(ctx) == apiKey.ID
}

func admissionSubject(ctx context.Context, apiKey *models.ApiKey, model string) limitpolicy.Subject {
	subject := limitpolicy.Subject{
		OrgID: middleware.GetOrgIDFromContext(ctx), ProjectID: middleware.GetProjectIDFromContext(ctx),
		APIKeyID: middleware.GetKeyIDFromContext(ctx), Model: strings.TrimSpace(model),
		BaseRPSLimit: int64(apiKey.QPSLimit), BaseRPMLimit: int64(apiKey.RPMLimit),
		CredentialConcurrencyLimit: intPointer64(apiKey.CredentialConcurrencyLimit),
		GrantRPSLimit:              intPointer64(apiKey.GrantRPSLimit), GrantRPMLimit: intPointer64(apiKey.GrantRPMLimit),
		GrantConcurrencyLimit: intPointer64(apiKey.GrantConcurrencyLimit),
	}
	if apiKey.AccessCredentialID != nil {
		subject.AccessCredentialID = *apiKey.AccessCredentialID
		subject.CredentialRef = apiKey.CredentialRef
	}
	if apiKey.AccessGrantID != nil {
		subject.AccessGrantID = *apiKey.AccessGrantID
	}
	return subject
}

func admissionPhaseFromContext(ctx context.Context, apiKey *models.ApiKey, operationID string) (string, string, limitpolicy.RateAdmissionReceipt, bool) {
	if ctx == nil {
		return "", "", limitpolicy.RateAdmissionReceipt{}, false
	}
	marker, marked := ctx.Value(earlyRateAdmissionContextKey{}).(earlyRateAdmissionMarker)
	if !marked {
		return limitpolicy.AdmissionPhaseFull, "", limitpolicy.RateAdmissionReceipt{}, true
	}
	authorizedRequestedModel := strings.TrimSpace(middleware.GetModelAliasFromContext(ctx))
	if !validEarlyRateIdentity(ctx, apiKey, operationID) || marker.operationID != operationID ||
		marker.orgID != apiKey.OrgID || marker.apiKeyID != apiKey.ID ||
		authorizedRequestedModel == "" || authorizedRequestedModel != marker.requestedModel {
		return "", "", limitpolicy.RateAdmissionReceipt{}, false
	}
	return limitpolicy.AdmissionPhaseModelExecution, authorizedRequestedModel, marker.rateReceipt, true
}

func (b *Billing) writeEarlyRateError(w http.ResponseWriter, r *http.Request, ctx context.Context, err error) {
	status := http.StatusServiceUnavailable
	code := errorCode(err)
	message := "Shared limit-policy state is temporarily unavailable"
	switch code {
	case ErrorCodeRPSExceeded:
		status = http.StatusTooManyRequests
		message = "Shared request-per-second limit exceeded"
		w.Header().Set("Retry-After", "1")
	case ErrorCodeRPMExceeded:
		status = http.StatusTooManyRequests
		message = "Shared request-per-minute limit exceeded"
		w.Header().Set("Retry-After", "60")
	case ErrorCodeConflict:
		status = http.StatusConflict
		message = "Operation identity conflicts with an existing limit reservation"
	default:
		w.Header().Set("Retry-After", "1")
	}
	identity := middleware.GetRequestIdentityFromContext(r.Context())
	policyVersion := ""
	if collector := limitpolicy.DecisionCollectorFromContext(ctx); collector != nil {
		_, policyVersion = collector.Snapshot()
		if policyVersion != "" {
			w.Header().Set("X-UniRouter-Limit-Policy-Version", policyVersion)
		}
	}
	b.recordEarlyRateEvidence(ctx, EarlyRateAdmissionEvidence{
		OperationID: identity.OperationID,
		OrgID:       middleware.GetOrgIDFromContext(r.Context()),
		APIKeyID:    middleware.GetKeyIDFromContext(r.Context()),
		Result:      "denied", ErrorCode: code, PolicyVersion: policyVersion,
	})
	gatewayerror.Write(w, r.URL.Path, identity.OperationID, status, code, message)
}

func (b *Billing) recordEarlyRateEvidence(ctx context.Context, evidence EarlyRateAdmissionEvidence) {
	if b != nil {
		b.earlyEvidenceMu.RLock()
		sink := b.earlyRateEvidenceSink
		b.earlyEvidenceMu.RUnlock()
		if sink != nil {
			sink.RecordEarlyRateAdmission(ctx, evidence)
			return
		}
	}
	log.Printf(
		"[limit-policy] early_gateway_admission operation_id=%s org_id=%d api_key_id=%d result=%s error_code=%s policy_version=%s",
		evidence.OperationID, evidence.OrgID, evidence.APIKeyID, evidence.Result, evidence.ErrorCode, evidence.PolicyVersion,
	)
}
