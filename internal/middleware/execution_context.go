package middleware

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/uniroute/unirouter/internal/executioncontext"
	"github.com/uniroute/unirouter/internal/executionmeasurement"
	"github.com/uniroute/unirouter/internal/guardrail"
	"github.com/uniroute/unirouter/internal/modeloperation"
)

type ExecutionContextCapturer interface {
	CaptureAuthenticated(context.Context, executioncontext.Snapshot) error
}

// CaptureExecutionContext belongs immediately inside the second authentication
// pass, before admission. It does not read any request body or caller tags.
func CaptureExecutionContext(capturer ExecutionContextCapturer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if capturer == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			operation, ok := modeloperation.LookupRequest(r.Method, r.URL.Path)
			if !ok || !operation.Paid {
				next.ServeHTTP(w, r)
				return
			}
			key := GetAPIKeyFromContext(r.Context())
			if key == nil || key.ProjectID == nil || *key.ProjectID <= 0 {
				next.ServeHTTP(w, r)
				return
			}
			snapshot := executioncontext.Snapshot{
				SchemaVersion: executioncontext.Version,
				Core: executioncontext.Core{
					RequestID: GetRequestIDFromContext(r.Context()), OrgID: executioncontext.ID(key.OrgID),
					ProjectID: executioncontext.ID(*key.ProjectID), APIKeyID: executioncontext.ID(key.ID),
					KeyType: key.KeyType, CredentialRef: key.CredentialRef,
					AuthKind: GetAuthKindFromContext(r.Context()), AuthEpoch: strconv.FormatInt(key.AuthEpoch, 10),
					Operation: string(operation.ID), Protocol: operation.ProtocolFamily + "." + operation.ProtocolVersion,
					CapturedAt: executioncontext.Time(time.Now()),
				},
				Identity: executioncontext.Identity{ActualHuman: "unknown", AuthorityRead: "live_key"},
			}
			if key.ExecutionIdentity != nil {
				snapshot.Identity = *key.ExecutionIdentity
			}
			if key.OwnerUserID != nil {
				snapshot.Identity.OwnerUserID = executioncontext.ID(*key.OwnerUserID)
			}
			if key.WorkloadApplicationID != nil {
				snapshot.Core.WorkloadApplicationID = executioncontext.ID(*key.WorkloadApplicationID)
			}
			if key.AccessCredentialID != nil {
				snapshot.Core.CredentialID = executioncontext.ID(*key.AccessCredentialID)
			}
			if claims := GetAgentCapabilityFromContext(r.Context()); claims != nil {
				snapshot.Identity.WorkflowID = claims.WorkflowID
				snapshot.Identity.RootAgentID = claims.RootAgentID
				snapshot.Identity.AgentID = claims.AgentID
				snapshot.Identity.ParentAgentID = claims.ParentAgentID
				snapshot.Identity.Lineage = append([]string(nil), claims.Lineage...)
				snapshot.Identity.AuthorityVersion = claims.RootAuthorityVersion
			}
			if err := snapshot.Validate(); err != nil {
				writeGatewayRequestError(w, r, http.StatusServiceUnavailable, "source_facts.capture_unavailable", "Execution context is temporarily unavailable")
				return
			}
			ctx := executioncontext.WithSnapshot(r.Context(), snapshot)
			var measurements *executionmeasurement.Collector
			measurementSink, enabled := capturer.(interface {
				ObservationsEnabled() bool
				CaptureMeasurements(context.Context, executioncontext.Snapshot, []executionmeasurement.Sample) error
			})
			if enabled && measurementSink.ObservationsEnabled() {
				ctx, measurements = executionmeasurement.Start(ctx)
			}
			if err := capturer.CaptureAuthenticated(ctx, snapshot); err != nil {
				writeGatewayRequestError(w, r, http.StatusServiceUnavailable, "source_facts.capture_unavailable", "Execution context is temporarily unavailable")
				return
			}
			if sink, ok := capturer.(interface {
				CaptureGatewayNoRoute(context.Context, executioncontext.Snapshot) error
			}); ok {
				ctx = WithGatewayNoRouteCapture(ctx, func(callCtx context.Context) error {
					return sink.CaptureGatewayNoRoute(callCtx, snapshot)
				})
			}
			if measurements != nil {
				defer func() {
					if err := measurementSink.CaptureMeasurements(ctx, snapshot, measurements.Samples()); err != nil {
						log.Printf("[source-observations] optional request measurements unavailable")
					}
				}()
			}
			if sink, ok := capturer.(interface {
				ObservationsEnabled() bool
				CapturePolicies(context.Context, executioncontext.Snapshot, []guardrail.SourcePolicyObservation, bool) error
			}); ok && sink.ObservationsEnabled() {
				var buffer *guardrail.SourcePolicyBuffer
				ctx, buffer = guardrail.StartSourceObservations(ctx)
				defer func() {
					values, overflow := buffer.Close()
					if err := sink.CapturePolicies(ctx, snapshot, values, overflow); err != nil {
						log.Printf("[source-observations] optional policy observations unavailable")
					}
				}()
			}
			if sink, ok := capturer.(interface {
				ObservationsEnabled() bool
				CaptureAdmissionRejection(context.Context, executioncontext.Snapshot, int) error
			}); ok && sink.ObservationsEnabled() {
				original := w
				w = wrapSourceAdmissionWriter(original, func(status int) error {
					return sink.CaptureAdmissionRejection(ctx, snapshot, status)
				}, func() {
					writeGatewayRequestError(original, r, http.StatusServiceUnavailable, "source_facts.capture_unavailable", "Execution context is temporarily unavailable")
				})
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
