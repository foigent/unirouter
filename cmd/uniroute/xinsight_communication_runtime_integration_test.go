//go:build integration

package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
	"github.com/uniroute/unirouter/internal/xinsightconnector"
)

type communicationTransport func(*http.Request) (*http.Response, error)

func (f communicationTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type communicationUnusedExecutor struct{}

func (communicationUnusedExecutor) ExecutePhaseTx(context.Context, xinsightconnector.PhaseProductTx, xinsightconnector.PhaseExecutionInput) (xinsightconnector.ControlResultV2, error) {
	return xinsightconnector.ControlResultV2{}, xinsightconnector.ErrInvalid
}

// Real installation, redeem journal, HTTP counterpart and transactional Agent.
// Only the remote XI service is synthetic. No resident auto loop is needed to
// redeem a new manual connection and reach its first bounded control request.
func TestXInsightCommunicationFiniteInitializationAndCadenceIntegration(t *testing.T) {
	ctx := context.Background()
	pool := testutil.SetupMigratedTestDB(t)
	const id = "xci_AAAAAAAAAAAAAAAAAAAAAA"
	const deployment = "xid_ZZZZZZZZZZZZZZZZZZZZZZ"
	const invitation = "xiiv1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	var org, project, actor int64
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs(name,status,org_type) VALUES('manual-init','active','standard') RETURNING id`).Scan(&org))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO users(org_id,username,password_hash,role,status) VALUES($1,'manual-init','hash','platform_admin','active') RETURNING id`, org).Scan(&actor))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO projects(org_id,name,slug,status,created_by) VALUES($1,'manual-init','manual-init','active',$2) RETURNING id`, org, actor).Scan(&project))
	manager, e := xinsightconnector.NewManager(pool, xinsightconnector.CommitAuthorizerFunc(func(context.Context, pgx.Tx, int64, int64, int64, string) error { return nil }), runtimeCredentialSource{})
	require.NoError(t, e)
	_, _, e = manager.CreateInstallation(ctx, xinsightconnector.Consent{InstallationID: id, XDeploymentID: deployment, InvitationSHA256: runtimeCredentialHash([]byte(invitation)), OrgID: org, ProjectID: project, EventScope: xinsightconnector.EventScopeModelCompleted, IdempotencyKey: "manual-init", ConsentedByUserID: actor})
	require.NoError(t, e)
	journal, e := xinsightconnector.NewPostgresPhaseJournal(pool, []byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, e)
	require.NoError(t, journal.StageInvitation(ctx, id, invitation))
	owner := runtimegeneration.Identity{Instance: "uri_" + strings.Repeat("3", 32), Artifact: "sha256:" + strings.Repeat("3", 64)}
	ledger, e := schemaauthority.ExpectedLedger(testutil.MigrationsDir(t))
	require.NoError(t, e)
	_, _, e = runtimegeneration.Renew(ctx, pool, owner, runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads()), ledger, false)
	require.NoError(t, e)
	store := xinsightconnector.NewCommunicationStore(pool, 1000, owner.Instance, owner.Artifact)
	requests := []string{}
	client := &http.Client{Transport: communicationTransport(func(r *http.Request) (*http.Response, error) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		body := ""
		status := 204
		if r.Method == http.MethodPost {
			prepared, e := journal.PrepareRedeem(ctx, id)
			require.NoError(t, e)
			raw, e := os.ReadFile("../../internal/xinsightconnector/testdata/control-v2/valid/01-prepare-paused-source-operation.json")
			require.NoError(t, e)
			op, e := xinsightconnector.DecodeControlOperationV2(raw)
			require.NoError(t, e)
			op.ProjectID = prepared.ControlProjectID
			operation, e := op.CanonicalJSON()
			require.NoError(t, e)
			wire, e := json.Marshal(map[string]any{"installation": map[string]any{"installation_id": id, "deployment_id": deployment, "project_id": prepared.ControlProjectID, "final_event_scope": []string{xinsightconnector.EventScopeModelCompleted}}, "operation": json.RawMessage(operation)})
			require.NoError(t, e)
			body = string(wire)
			status = 200
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	counterpart := func(_ context.Context, d xinsightconnector.RuntimeInstallationDescriptor) (*xinsightconnector.HTTPControlCounterpart, error) {
		return xinsightconnector.NewHTTPControlCounterpart("https://control.example.test", d, journal, client, time.Second)
	}
	s, e := newXInsightConnectorRuntimeSupervisor(true, time.Second, journal, func(c context.Context, d xinsightconnector.RuntimeInstallationDescriptor) (xInsightConnectorRuntimeRedeemer, error) {
		return counterpart(c, d)
	}, func(c context.Context, d xinsightconnector.RuntimeInstallationDescriptor) (xinsightconnector.ControlCounterpart, error) {
		return counterpart(c, d)
	}, newXInsightConnectorTransactionalAgentFactory(journal, communicationUnusedExecutor{}), nil)
	require.NoError(t, e)
	s.communication = store
	localAdvances := 0
	s.setupAdvance = func(context.Context, int) error { localAdvances++; return nil }
	require.NoError(t, s.runCycle(ctx))
	require.Empty(t, requests, "manual connection has zero automatic HTTP")
	tx, e := pool.Begin(ctx)
	require.NoError(t, e)
	permit, _, e := store.ClaimTx(ctx, tx, org, project, actor, 1, id, "initialize", "manual")
	require.NoError(t, e)
	require.NoError(t, tx.Commit(ctx))
	code, success, e := s.syncCommunicationOnce(ctx, permit, id)
	require.NoError(t, e)
	require.Equal(t, "partial", code)
	require.True(t, success)
	require.NoError(t, permit.Finish(ctx, code, success))
	require.Equal(t, []string{"POST /api/v1/connector-control/invitations/redeem", "GET /api/v1/connector-control/operations"}, requests)
	st, e := store.Read(ctx, org, project)
	require.NoError(t, e)
	require.False(t, st.AutomaticEnabled)
	require.Equal(t, "partial", st.LastRun.State, "empty first Poll is not completed initialization")
	require.Equal(t, 2, st.LastRun.Units)
	require.Equal(t, 2, st.LastRun.Requests)
	require.NoError(t, s.runCycle(ctx))
	require.Len(t, requests, 2)
	require.Greater(t, localAdvances, 0, "local setup continues while manual")
	tx, e = pool.Begin(ctx)
	require.NoError(t, e)
	require.NoError(t, store.UpdateTx(ctx, tx, org, project, actor, 1, id, true, 60000))
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, s.runCycle(ctx))
	require.Len(t, requests, 3)
	require.NoError(t, s.runCycle(ctx))
	require.Len(t, requests, 3, "no second HTTP request before persisted cadence")
}
