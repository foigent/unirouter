package middleware

import (
	"context"
	"net/http"

	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/db"
)

type xInsightMachineContextKey struct{}

type XInsightMachineIdentity struct {
	KeyID           int64
	OrgID           int64
	AnchorProjectID int64
	InstallationID  string
	XDeploymentID   string
	SourceInstance  string
	Purpose         string
}

func XInsightMachineFromContext(ctx context.Context) (XInsightMachineIdentity, bool) {
	identity, ok := ctx.Value(xInsightMachineContextKey{}).(XInsightMachineIdentity)
	return identity, ok && identity.KeyID > 0
}

// XInsightMachineAuth is a separate service-only gate. It deliberately does
// not populate a UR human user ID or the gateway API-key context.
func XInsightMachineAuth(q db.DBQuerier, secret, purpose string, resolver ClientIPResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			raw, err := extractUniqueBearerCredential(r)
			if err != nil {
				writeGatewayAuthError(w, 401, "auth_error.invalid_api_key", "Invalid machine credential")
				return
			}
			hash, prefix, err := auth.HashXInsightMachineCredential(raw, secret, purpose)
			if err != nil || q == nil {
				writeGatewayAuthError(w, 401, "auth_error.invalid_api_key", "Invalid machine credential")
				return
			}
			var identity XInsightMachineIdentity
			var allowedIP string
			var orgStatus, projectStatus, desired, observed string
			err = q.QueryRow(r.Context(), `SELECT ak.id,ak.org_id,mc.anchor_project_id,mc.installation_id,
 mc.x_deployment_id,mc.source_instance,mc.purpose,ak.allowed_ips,o.status,p.status,
 i.desired_state,i.observed_state
 FROM api_keys ak JOIN xinsight_machine_credentials mc ON mc.api_key_id=ak.id
 JOIN xinsight_connector_installations i ON i.installation_id=mc.installation_id
 JOIN orgs o ON o.id=mc.org_id JOIN projects p ON p.id=mc.anchor_project_id AND p.org_id=mc.org_id
 WHERE ak.key_hash=$1 AND ak.key_prefix=$2 AND ak.purpose=$3 AND mc.purpose=$3
 AND ak.verifier_scheme=$4 AND ak.status='feed_active' AND ak.key_type='service'
 AND ak.org_id=mc.org_id AND ak.project_id=mc.anchor_project_id
 AND i.org_id=mc.org_id AND i.project_id=mc.anchor_project_id
 AND i.x_deployment_id=mc.x_deployment_id AND i.source_instance=mc.source_instance`,
				hash, prefix, purpose, auth.ReceiverFeedVerifierScheme).Scan(&identity.KeyID, &identity.OrgID,
				&identity.AnchorProjectID, &identity.InstallationID, &identity.XDeploymentID, &identity.SourceInstance,
				&identity.Purpose, &allowedIP, &orgStatus, &projectStatus, &desired, &observed)
			if err != nil {
				writeGatewayAuthError(w, 401, "auth_error.invalid_api_key", "Invalid machine credential")
				return
			}
			if orgStatus != "active" || projectStatus != "active" || desired != "active" || observed == "disconnected" {
				writeGatewayAuthError(w, 403, "auth_error.machine_binding_inactive", "Machine binding is inactive")
				return
			}
			if !checkIPWhitelist(resolver.Resolve(r), allowedIP) {
				writeGatewayAuthError(w, 403, "auth_error.ip_not_allowed", "Client IP is not allowed")
				return
			}
			ctx := context.WithValue(r.Context(), xInsightMachineContextKey{}, identity)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
