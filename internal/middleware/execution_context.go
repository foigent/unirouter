package middleware

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/uniroute/unirouter/internal/executioncontext"
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
			if err := capturer.CaptureAuthenticated(ctx, snapshot); err != nil {
				writeGatewayRequestError(w, r, http.StatusServiceUnavailable, "source_facts.capture_unavailable", "Execution context is temporarily unavailable")
				return
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
