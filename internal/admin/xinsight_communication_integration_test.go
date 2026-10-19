//go:build integration

package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
	"github.com/uniroute/unirouter/internal/xinsightconnector"
)

func TestXInsightCommunicationAuthorizationLocalStatusAndIdempotencyIntegration(t *testing.T) {
	f := newNormalSetupFixture(t)
	ctx := context.Background()
	put := f.put(f.preview(t))
	require.Equal(t, 202, f.human(t, "create", put, f.handler.CreateXInsightSetup).Code)
	owner := runtimegeneration.Identity{Instance: "uri_" + strings.Repeat("6", 32), Artifact: "sha256:" + strings.Repeat("6", 64)}
	ledger, e := schemaauthority.ExpectedLedger(testutil.MigrationsDir(t))
	require.NoError(t, e)
	_, _, e = runtimegeneration.Renew(ctx, f.pool, owner, runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads()), ledger, false)
	require.NoError(t, e)
	store := xinsightconnector.NewCommunicationStore(f.pool, 1000, owner.Instance, owner.Artifact)
	calls, probes := 0, 0
	f.handler.xInsightSetup.status = func(context.Context, string, string) ([]byte, error) {
		probes++
		return nil, xinsightconnector.ErrUnavailable
	}
	f.handler.SetXInsightCommunication(store, func(_ context.Context, p *xinsightconnector.CommunicationPermit, id string) (string, bool, error) {
		calls++
		require.Equal(t, cp2bInstallationID, id)
		return "empty", false, nil
	})
	request := func(body any, handler http.HandlerFunc) *httptest.ResponseRecorder {
		r := f.request(t, "communication", body)
		r.SetPathValue("project_id", stringInt64(f.projectID))
		w := httptest.NewRecorder()
		handler(w, r)
		return w
	}
	get := request(nil, f.handler.GetXInsightCommunication)
	require.Equal(t, 200, get.Code, get.Body.String())
	var state xinsightconnector.CommunicationState
	require.NoError(t, json.Unmarshal(get.Body.Bytes(), &state))
	require.False(t, state.AutomaticEnabled)
	sync := xInsightCommunicationSync{IdempotencyKey: "one", ExpectedRevision: state.Revision, InstallationID: cp2bInstallationID}
	require.Equal(t, 200, request(sync, f.handler.SyncXInsightCommunicationOnce).Code)
	require.Equal(t, 200, request(sync, f.handler.SyncXInsightCommunicationOnce).Code)
	require.Equal(t, 1, calls)
	enabled := true
	update := xInsightCommunicationUpdate{AutomaticEnabled: &enabled, CadenceMS: 60000, ExpectedRevision: 1, InstallationID: cp2bInstallationID}
	require.Equal(t, 200, request(update, f.handler.UpdateXInsightCommunication).Code)
	sync.ExpectedRevision = 2
	sync.IdempotencyKey = "must-not-run-automatic"
	require.Equal(t, 409, request(sync, f.handler.SyncXInsightCommunicationOnce).Code)
	require.Equal(t, 1, calls)
	// Existing receipt replay remains available while automatic is enabled.
	sync.ExpectedRevision = 1
	sync.IdempotencyKey = "one"
	require.Equal(t, 200, request(sync, f.handler.SyncXInsightCommunicationOnce).Code)
	require.Equal(t, 1, calls)
	require.Equal(t, 409, request(update, f.handler.UpdateXInsightCommunication).Code, "stale revision fails closed")
	enabled = false
	update.ExpectedRevision = 2
	_, e = f.pool.Exec(ctx, `UPDATE projects SET status='disabled' WHERE id=$1`, f.projectID)
	require.NoError(t, e)
	require.Equal(t, 200, request(update, f.handler.UpdateXInsightCommunication).Code, "safety pause remains available")
	enabled = true
	update.ExpectedRevision = 3
	require.Equal(t, 403, request(update, f.handler.UpdateXInsightCommunication).Code, "disabled project cannot expand")
	_, e = f.pool.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, f.actorID)
	require.NoError(t, e)
	enabled = false
	require.Equal(t, 403, request(update, f.handler.UpdateXInsightCommunication).Code, "actor status is rechecked in mutation transaction")
	require.Zero(t, probes, "status pages and policy GET never contact XI")
	var auditCount int
	require.NoError(t, f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action='data_exchange.xinsight.communication.once'`).Scan(&auditCount))
	require.Equal(t, 1, auditCount, "no per-request or replay audit stream")
}
