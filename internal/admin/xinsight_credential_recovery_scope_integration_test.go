//go:build integration

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/customercontext"
	"github.com/uniroute/unirouter/internal/middleware"
)

type recoveryScopeOutboundTripwire struct{ calls *atomic.Int32 }

func (t recoveryScopeOutboundTripwire) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls.Add(1)
	return nil, errors.New("unexpected XI outbound")
}

func TestXInsightCredentialRecoveryScopeTechnicalOrgPostgresIntegration(t *testing.T) {
	f := newNormalSetupFixture(t)
	ctx := context.Background()
	preview := f.preview(t)
	require.Equal(t, http.StatusAccepted, f.human(t, "create-recovery-scope", f.put(preview), f.handler.CreateXInsightSetup).Code)
	prepared, err := f.journal.PrepareRedeem(ctx, cp2bInstallationID)
	require.NoError(t, err)
	require.NotEmpty(t, prepared.ControlProjectID)
	_, err = f.pool.Exec(ctx, `UPDATE xinsight_connector_control_bindings SET
 invitation_token_enc=NULL,redeem_result_sha256=$2,redeemed_origin=$3,
 created_at=NOW()-INTERVAL '25 hours',control_token_expires_at=NOW()-INTERVAL '1 hour'
 WHERE installation_id=$1`, cp2bInstallationID, cp2bSHA("synthetic-completed-redeem"), f.peer.URL)
	require.NoError(t, err)
	f.handler.SetXInsightCredentialJournal(f.journal)

	var technicalOrg, technicalActor, foreignOrg, foreignActor int64
	require.NoError(t, f.pool.QueryRow(ctx, `INSERT INTO orgs(name,status,org_type)
VALUES ('recovery-technical-org','active','partner') RETURNING id`).Scan(&technicalOrg))
	require.NoError(t, f.pool.QueryRow(ctx, `INSERT INTO users(org_id,username,password_hash,role,organization_role,status)
VALUES ($1,'recovery-technical-admin','hash','platform_admin','platform_admin','active') RETURNING id`, technicalOrg).Scan(&technicalActor))
	require.NoError(t, f.pool.QueryRow(ctx, `INSERT INTO orgs(name,status,org_type)
VALUES ('recovery-foreign-org','active','customer') RETURNING id`).Scan(&foreignOrg))
	require.NoError(t, f.pool.QueryRow(ctx, `INSERT INTO users(org_id,username,password_hash,role,organization_role,status)
VALUES ($1,'recovery-foreign-admin','hash','org_admin','org_admin','active') RETURNING id`, foreignOrg).Scan(&foreignActor))
	require.NotEqual(t, technicalOrg, f.orgID)

	// The tripwire proves scope discovery and denied preview use only local
	// saved setup and current principal facts, without sending the ticket.
	var outbound atomic.Int32
	f.handler.xInsightSetup.client.Transport = recoveryScopeOutboundTripwire{calls: &outbound}
	call := func(actor int64, customerOrg int64, pathOrg int64, scope bool) *httptest.ResponseRecorder {
		t.Helper()
		method, path, body := http.MethodGet,
			"/api/v2/xinsight/setups/"+cp2bInstallationID+"/credential-recovery-scope", ""
		if !scope {
			method = http.MethodPost
			path = "/api/v2/organizations/" + stringInt64(pathOrg) + "/xinsight/setups/" + cp2bInstallationID + "/credential-recovery/preview"
			body = `{"recovery_ticket":"xirt_` + strings.Repeat("A", 43) + `"}`
		}
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r = withIntegrationCommittingActor(t, f.pool, r, actor)
		r = r.WithContext(context.WithValue(r.Context(), middleware.CtxCustomerContext,
			customercontext.Snapshot{State: customercontext.StateReady, OrganizationID: customerOrg}))
		r.SetPathValue("installation_id", cp2bInstallationID)
		if !scope {
			r.SetPathValue("org_id", stringInt64(pathOrg))
		}
		w := httptest.NewRecorder()
		if scope {
			f.handler.GetXInsightCredentialRecoveryScope(w, r)
		} else {
			f.handler.PreviewXInsightCredentialRecovery(w, r)
		}
		return w
	}
	good := call(technicalActor, f.orgID, 0, true)
	require.Equal(t, http.StatusOK, good.Code, good.Body.String())
	var resolved struct {
		InstallationID string `json:"installation_id"`
		OrgID          int64  `json:"org_id"`
	}
	require.NoError(t, json.Unmarshal(good.Body.Bytes(), &resolved))
	require.Equal(t, cp2bInstallationID, resolved.InstallationID)
	require.Equal(t, f.orgID, resolved.OrgID)
	require.Equal(t, "no-store", good.Header().Get("Cache-Control"))
	require.NotContains(t, good.Body.String(), "xirt_")
	require.Zero(t, outbound.Load())

	// Neither a foreign organization administrator nor a platform actor whose
	// resolved customer scope differs from the original business org may locate
	// it or send the ticket to XI.
	require.Equal(t, http.StatusNotFound, call(foreignActor, foreignOrg, 0, true).Code)
	require.Equal(t, http.StatusNotFound, call(technicalActor, foreignOrg, 0, true).Code)
	require.NotEqual(t, http.StatusOK, call(foreignActor, foreignOrg, f.orgID, false).Code)
	require.NotEqual(t, http.StatusOK, call(technicalActor, foreignOrg, f.orgID, false).Code)
	require.Zero(t, outbound.Load())
}
