//go:build integration

package admin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/sourcefacts"
	"github.com/uniroute/unirouter/internal/testutil"
	"github.com/uniroute/unirouter/internal/xinsightconnector"
)

// Build the real supported predecessor, rather than downgrading a current
// fixture or inserting a fabricated completed setup row.
func normalSetupM127Fixture(t *testing.T) normalSetupFixture {
	t.Helper()
	pool := testutil.SetupMigratedThroughTestDB(t, schemaauthority.XInsightCommunicationHead)
	f := sourceControlFixture{xInsightFeedAuthorizationFixture: newXInsightFeedAuthorizationFixtureWithPool(t, false, pool)}
	var err error
	f.source, err = sourcefacts.New(pool, sourcefacts.Options{Enabled: true, ContentEnabled: true, SourceInstance: "synthetic-source-control", CursorSecret: syntheticSourceControlSecret})
	require.NoError(t, err)
	require.NoError(t, f.source.CheckReady(t.Context()))
	f.handler = NewHandler(pool, syntheticSourceControlSecret, []byte("0123456789abcdef01234567"), nil)
	f.handler.SetSourceFactsController(f.source)
	return newNormalSetupFixtureFromSource(t, f)
}

// Use the actual consent and native redeem journal. The peer response is a
// synthetic fixture; no external XI or production credential participates.
func pairNormalSetup(t *testing.T, f normalSetupFixture) {
	t.Helper()
	put := f.put(f.preview(t))
	w := f.human(t, "reconcile-consent", put, f.handler.CreateXInsightSetup)
	require.Equal(t, 202, w.Code, w.Body.String())
	prepared, err := f.journal.PrepareRedeem(t.Context(), cp2bInstallationID)
	require.NoError(t, err)
	operation := cp2bPrepareOperation()
	operation.ProjectID = prepared.ControlProjectID
	wire, err := operation.CanonicalJSON()
	require.NoError(t, err)
	installation, err := json.Marshal(map[string]any{
		"installation_id": cp2bInstallationID, "deployment_id": cp2bDeploymentID,
		"project_id": prepared.ControlProjectID, "final_event_scope": operation.EventScope,
	})
	require.NoError(t, err)
	result, err := json.Marshal(map[string]json.RawMessage{"installation": installation, "operation": wire})
	require.NoError(t, err)
	require.NoError(t, f.journal.FinalizeRedeemFromOrigin(t.Context(), prepared, result, f.peer.URL))
	executor, err := NewXInsightConnectorActivationExecutor(f.handler, cp2bBindingResolver{})
	require.NoError(t, err)
	counterpart := &cp2bCounterpart{}
	agent, err := xinsightconnector.NewTransactionalAgent(cp2bInstallationID, counterpart, f.journal, executor)
	require.NoError(t, err)
	runV2 := func(o xinsightconnector.ControlOperationV2) xinsightconnector.ControlResultV2 {
		counterpart.operation = canonicalCP2BOperation(t, o)
		processed, e := agent.RunOnce(t.Context())
		require.NoError(t, e)
		require.True(t, processed)
		return decodeCP2BResult(t, counterpart.acks[len(counterpart.acks)-1])
	}
	preparedResult := runV2(operation)
	priorSHA, err := preparedResult.CanonicalSHA256()
	require.NoError(t, err)
	registration := cp2bReceiverRegistration(t, auth.ReceiverFeedTokenPrefix+strings.Repeat("ab", 32))
	register := cp2bRegisterOperation(operation, priorSHA, registration)
	register.ProjectID = prepared.ControlProjectID
	registerRequest := register.Request.(xinsightconnector.RegisterReceiverRequestV2)
	registerRequest.SourceInstance = f.source.SourceInstance()
	register.Request = registerRequest
	registered := runV2(register)
	priorSHA, err = registered.CanonicalSHA256()
	require.NoError(t, err)
	resume := cp2bResumeOperation(t, register, registered, priorSHA)
	resume.ProjectID = prepared.ControlProjectID
	resumeRequest := resume.Request.(xinsightconnector.ResumeSourceRequestV2)
	resumeRequest.SourceInstance = f.source.SourceInstance()
	resume.Request = resumeRequest
	resumed := runV2(resume)
	priorSHA, err = resumed.CanonicalSHA256()
	require.NoError(t, err)
	var sourceBinding string
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT source_binding_sha256 FROM xinsight_connector_installations WHERE installation_id=$1`, cp2bInstallationID).Scan(&sourceBinding))
	for _, name := range []string{"05-policy-pair-operation", "07-source-enroll-pair-operation"} {
		o := loadAdminV3(t, name)
		if name == "05-policy-pair-operation" {
			o.OperationID = "xco_EEEEEEEEEEEEEEEEEEEEEE"
		} else {
			o.OperationID = "xco_FFFFFFFFFFFFFFFFFFFFFF"
		}
		o.ProjectID = prepared.ControlProjectID
		o.PredecessorOperationID, o.PredecessorResultSHA256 = resume.OperationID, priorSHA
		q := o.Request.(xinsightconnector.DeliverMachineRequest)
		q.SourceInstance, q.SourceBindingSHA256 = f.source.SourceInstance(), sourceBinding
		q.Registration.AllowedIP = "127.0.0.1/32"
		q.RegistrationSHA256 = xinsightconnector.PublicCanonicalSHA256(q.Registration)
		q.CustodyReceipt.RegistrationSHA256 = q.RegistrationSHA256
		q.CustodyReceiptSHA256 = xinsightconnector.PublicCanonicalSHA256(q.CustodyReceipt)
		o.Request = q
		counterpart.operation, err = o.CanonicalJSON()
		require.NoError(t, err)
		processed, e := agent.RunOnce(t.Context())
		require.NoError(t, e)
		require.True(t, processed)
		terminal, e := xinsightconnector.DecodeControlResultV3(counterpart.acks[len(counterpart.acks)-1])
		require.NoError(t, e)
		require.Equal(t, "completed", terminal.Status)
	}
	// The real controller authorizes at most one policy grant per transaction.
	// Advance the declared bounded phases, rather than fabricating progress.
	for phase := range max(1, len(put.PolicyAccess)) {
		var beforeState, beforeProgress string
		require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT setup_state,setup_progress::text FROM xinsight_connector_installations WHERE installation_id=$1`, cp2bInstallationID).Scan(&beforeState, &beforeProgress))
		require.NoError(t, f.handler.AdvanceXInsightSetups(t.Context(), 32))
		if phase == 0 && len(put.PolicyAccess) > 1 {
			var afterState, afterProgress string
			require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT setup_state,setup_progress::text FROM xinsight_connector_installations WHERE installation_id=$1`, cp2bInstallationID).Scan(&afterState, &afterProgress))
			require.Equal(t, beforeState, afterState, "first real grant keeps provisioning state")
			require.NotEqual(t, beforeProgress, afterProgress, "changed progress must persist even when state is unchanged")
		}
	}
	var state string
	var grants int
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT setup_state FROM xinsight_connector_installations WHERE installation_id=$1`, cp2bInstallationID).Scan(&state))
	require.Equal(t, "waiting_data", state)
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM xinsight_policy_delegations)+(SELECT count(*) FROM xinsight_source_enrollments)`).Scan(&grants))
	require.Positive(t, grants, "exercise a paired installation with durable actual grants")
}

func normalSetupRow(t *testing.T, f normalSetupFixture) (string, string) {
	t.Helper()
	var row, revision string
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT to_jsonb(i)::text,xmin::text FROM xinsight_connector_installations i WHERE installation_id=$1`, cp2bInstallationID).Scan(&row, &revision))
	return row, revision
}

// Every committed original row stays in this comparison. Only the known M128
// schema row/new columns are projected away, as in the shipped upgrade owner.
func normalSetupOriginalTables(t *testing.T, f normalSetupFixture) map[string]string {
	t.Helper()
	var names []string
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT array_agg(tablename ORDER BY tablename) FROM pg_tables WHERE schemaname='public'`).Scan(&names))
	result := make(map[string]string, len(names))
	for _, name := range names {
		expr, where := "to_jsonb(t)", ""
		if name == "model_deployments" {
			expr += "-'upstream_release_observation'-'upstream_observation_revision'"
		}
		if name == "schema_migrations" {
			where = " WHERE version<>'" + schemaauthority.UpstreamReleaseObservationHead + "'"
		}
		var raw string
		require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT COALESCE(jsonb_agg(`+expr+` ORDER BY (`+expr+`)::text),'[]')::text FROM `+pgx.Identifier{name}.Sanitize()+` t`+where).Scan(&raw))
		hash := sha256.Sum256([]byte(raw))
		result[name] = hex.EncodeToString(hash[:])
	}
	return result
}

func TestXInsightNormalSetupPairedReconciliationPreservesM127AndM128RowsIntegration(t *testing.T) {
	f := normalSetupM127Fixture(t)
	pairNormalSetup(t, f)
	before := normalSetupOriginalTables(t, f)
	row, revision := normalSetupRow(t, f)
	for cycle := 0; cycle < 3; cycle++ {
		require.NoError(t, f.handler.AdvanceXInsightSetups(t.Context(), 32))
		current, currentRevision := normalSetupRow(t, f)
		require.Equal(t, row, current, "unchanged reconciliation must preserve every field, including updated_at")
		require.Equal(t, revision, currentRevision, "a semantic no-op must not issue a row UPDATE")
	}
	require.Equal(t, before, normalSetupOriginalTables(t, f))
	require.NoError(t, migrate.RunExisting(t.Context(), f.pool, testutil.MigrationsDir(t)))
	var head string
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT max(version) FROM schema_migrations`).Scan(&head))
	require.Equal(t, schemaauthority.UpstreamReleaseObservationHead, head)
	require.Equal(t, before, normalSetupOriginalTables(t, f), "actual populated predecessor rows survive M128")
	for cycle := 0; cycle < 3; cycle++ {
		require.NoError(t, f.handler.AdvanceXInsightSetups(t.Context(), 32))
		require.Equal(t, before, normalSetupOriginalTables(t, f), "new-writer setup cycles must preserve all original table facts")
		current, currentRevision := normalSetupRow(t, f)
		require.Equal(t, row, current)
		require.Equal(t, revision, currentRevision)
	}
}

func TestXInsightNormalSetupPairedReconciliationRetainsTransitionsAndFreshAuthorityIntegration(t *testing.T) {
	f := newNormalSetupFixture(t)
	pairNormalSetup(t, f)
	_, err := f.pool.Exec(t.Context(), `UPDATE xinsight_connector_installations SET setup_state='provisioning' WHERE installation_id=$1`, cp2bInstallationID)
	require.NoError(t, err)
	before, _ := normalSetupRow(t, f)
	require.NoError(t, f.handler.AdvanceXInsightSetups(t.Context(), 32))
	after, revision := normalSetupRow(t, f)
	require.NotEqual(t, before, after, "a real state transition must still persist")
	var state string
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT setup_state FROM xinsight_connector_installations WHERE installation_id=$1`, cp2bInstallationID).Scan(&state))
	require.Equal(t, "waiting_data", state)
	require.NoError(t, f.handler.AdvanceXInsightSetups(t.Context(), 32))
	current, currentRevision := normalSetupRow(t, f)
	require.Equal(t, after, current)
	require.Equal(t, revision, currentRevision)
	_, err = f.pool.Exec(t.Context(), `UPDATE users SET status='disabled' WHERE id=$1`, f.actorID)
	require.NoError(t, err)
	require.Error(t, f.handler.CheckXInsightSetupControlAuthority(t.Context(), cp2bInstallationID))
	require.NoError(t, f.handler.AdvanceXInsightSetups(t.Context(), 32))
	var issue string
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT setup_state,setup_progress->>'issue' FROM xinsight_connector_installations WHERE installation_id=$1`, cp2bInstallationID).Scan(&state, &issue))
	require.Equal(t, "needs_action", state)
	require.Equal(t, "human_authority_changed", issue)
}

func TestXInsightNormalSetupRecoveryOracleActualPairedM128Integration(t *testing.T) {
	f := normalSetupM127Fixture(t)
	pairNormalSetup(t, f)
	type observation struct {
		Row         map[string]json.RawMessage `json:"row"`
		StableSHA   string                     `json:"stable_sha256"`
		ProgressSHA string                     `json:"progress_sha256"`
		Clock       string                     `json:"clock"`
	}
	observe := func() observation {
		var raw []byte
		require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT jsonb_build_object('row',to_jsonb(t),
 'stable_sha256',encode(sha256(convert_to((to_jsonb(t)-'updated_at')::text,'UTF8')),'hex'),
 'progress_sha256',encode(sha256(convert_to(setup_progress::text,'UTF8')),'hex'),'clock',clock_timestamp())
 FROM xinsight_connector_installations t WHERE installation_id=$1`, cp2bInstallationID).Scan(&raw))
		var out observation
		require.NoError(t, json.Unmarshal(raw, &out))
		return out
	}
	original := observe()
	before := normalSetupOriginalTables(t, f)
	require.NoError(t, migrate.RunExisting(t.Context(), f.pool, testutil.MigrationsDir(t)))
	var start time.Time
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT clock_timestamp()`).Scan(&start))
	// With the old-code overlay this invokes the actual old controller's
	// no-op UPDATE after M128; no terminal/grant/progress row is fabricated.
	require.NoError(t, f.handler.AdvanceXInsightSetups(t.Context(), 32))
	current := observe()
	expectOldTick := os.Getenv("RECOVERY_EXPECT_OLD_SETUP_TICK") == "1"
	if expectOldTick {
		require.NotEqual(t, string(original.Row["updated_at"]), string(current.Row["updated_at"]))
	} else {
		require.Equal(t, string(original.Row["updated_at"]), string(current.Row["updated_at"]))
		require.Equal(t, before, normalSetupOriginalTables(t, f))
	}
	plan := map[string]any{
		"allowed_row": map[string]any{
			"installation_id": original.Row["installation_id"], "org_id": original.Row["org_id"], "project_id": original.Row["project_id"],
			"updated_at": original.Row["updated_at"], "setup_state": original.Row["setup_state"],
			"setup_progress_sha256": original.ProgressSHA, "all_other_columns_sha256": original.StableSHA,
		},
		// This local DB boundary tests timestamp arithmetic, not Docker startup.
		"writer": map[string]string{"started_at": start.UTC().Format(time.RFC3339Nano)},
	}
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	script := `import importlib.util,json,sys
from pathlib import Path
s=importlib.util.spec_from_file_location("actual_recovery",Path(sys.argv[1])/"deploy/customer/recovery_resume.py")
m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
v=json.loads(sys.stdin.read())
try:
 m.setup_delta(v["source"],v["current"],v["plan"]); accepted=True
except m.RecoveryError: accepted=False
print(json.dumps({"accepted":accepted,"projection":m.conditional_row_projection(v["plan"]["allowed_row"]["installation_id"])}))`
	oracle := func(value observation) (bool, string) {
		input, err := json.Marshal(map[string]any{"source": original, "current": value, "plan": plan})
		require.NoError(t, err)
		command := exec.CommandContext(t.Context(), "/usr/bin/python3", "-I", "-B", "-c", script, repo)
		command.Env = []string{"PATH=" + os.Getenv("PATH")}
		command.Stdin = bytes.NewReader(input)
		var out, diagnostics bytes.Buffer
		command.Stdout, command.Stderr = &out, &diagnostics
		require.NoError(t, command.Run(), "recovery oracle failed: %s", diagnostics.String())
		var result struct {
			Accepted   bool   `json:"accepted"`
			Projection string `json:"projection"`
		}
		require.NoError(t, json.Unmarshal(out.Bytes(), &result))
		return result.Accepted, result.Projection
	}
	accepted, projection := oracle(current)
	require.Equal(t, expectOldTick, accepted, "the current fix needs no recovery; the exact old timestamp-only drift is recognized")
	var projected string
	require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT COALESCE(jsonb_agg(`+projection+` ORDER BY (`+projection+`)::text),'[]')::text FROM xinsight_connector_installations t`).Scan(&projected))
	// Execute the actual production CASE expression on PostgreSQL, and compare
	// to the retained real row with just the one permitted field removed.
	originalRow := make(map[string]json.RawMessage, len(original.Row))
	for k, v := range original.Row {
		if k != "updated_at" {
			originalRow[k] = v
		}
	}
	want, err := json.Marshal([]map[string]json.RawMessage{originalRow})
	require.NoError(t, err)
	require.JSONEq(t, string(want), projected)
	bad := current
	bad.Row = make(map[string]json.RawMessage, len(current.Row))
	for k, v := range current.Row {
		bad.Row[k] = v
	}
	bad.Row["control_fence"] = json.RawMessage(`999`)
	accepted, _ = oracle(bad)
	require.False(t, accepted, "a changed authority field never becomes an allowed timestamp delta")
	bad = current
	bad.Clock = "2000-01-01T00:00:00Z"
	accepted, _ = oracle(bad)
	require.False(t, accepted, "a future timestamp must fail")
}

func TestRecoverySessionGCActualPairedUpgradeAndCleanupIntegration(t *testing.T) {
	for _, scenario := range []string{"expired", "live", "at_cutoff", "after_cutoff", "mixed"} {
		t.Run(scenario, func(t *testing.T) {
			f := normalSetupM127Fixture(t)
			pairNormalSetup(t, f)
			// Only this test's newly isolated synthetic database is changed.
			_, err := f.pool.Exec(t.Context(), `DELETE FROM admin_sessions`)
			require.NoError(t, err)
			_, err = f.pool.Exec(t.Context(), `INSERT INTO admin_sessions(user_id,sid_hash,issued_at,expires_at)
VALUES($1,decode(repeat('31',32),'hex'),now()-interval '2 hours',now()-interval '1 hour')`, f.actorID)
			require.NoError(t, err)
			require.NoError(t, migrate.RunExisting(t.Context(), f.pool, testutil.MigrationsDir(t)))
			require.NoError(t, f.handler.AdvanceXInsightSetups(t.Context(), 32))
			var started time.Time
			require.NoError(t, f.pool.QueryRow(t.Context(), `SELECT clock_timestamp()`).Scan(&started))
			cutoff := started.UTC().Truncate(time.Microsecond).Format("2006-01-02T15:04:05.000000-07:00")
			if started.Nanosecond() == 0 {
				cutoff = started.UTC().Format("2006-01-02T15:04:05-07:00")
			}
			if scenario == "live" || scenario == "mixed" {
				if scenario == "live" {
					_, err = f.pool.Exec(t.Context(), `UPDATE admin_sessions SET expires_at=now()+interval '1 hour'`)
				} else {
					_, err = f.pool.Exec(t.Context(), `INSERT INTO admin_sessions(user_id,sid_hash,issued_at,expires_at)
VALUES($1,decode(repeat('32',32),'hex'),now()-interval '1 hour',now()+interval '1 hour')`, f.actorID)
				}
				require.NoError(t, err)
			}
			if scenario == "at_cutoff" || scenario == "after_cutoff" {
				expires := started
				if scenario == "after_cutoff" {
					expires = expires.Add(time.Microsecond)
				}
				_, err = f.pool.Exec(t.Context(), `UPDATE admin_sessions SET expires_at=$1`, expires)
				require.NoError(t, err)
			}
			conn, err := f.pool.Acquire(t.Context())
			require.NoError(t, err)
			defer conn.Release()
			_, err = conn.Exec(t.Context(), `CREATE TEMP TABLE original_admin_sessions AS TABLE admin_sessions`)
			require.NoError(t, err)
			_, file, _, ok := runtime.Caller(0)
			require.True(t, ok)
			repo := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
			script := `import importlib.util,json,sys
from pathlib import Path
s=importlib.util.spec_from_file_location("actual_recovery",Path(sys.argv[1])/"deploy/customer/recovery_resume.py")
m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
v=json.loads(sys.stdin.read())
if v["action"]=="query":print(json.dumps({"query":m.session_probe_query(v["cutoff"])}))
else:
 try:
  missing,reference,proof=m.session_gc_delta(v["source"],v["current"],v["plan"],v["source"]["table"])
  print(json.dumps({"accepted":True,"reference":reference,"where":m.session_source_projection(missing)[1],"proof":proof}))
 except m.RecoveryError:print(json.dumps({"accepted":False}))`
			bridge := func(input map[string]any) map[string]json.RawMessage {
				wire, e := json.Marshal(input)
				require.NoError(t, e)
				command := exec.CommandContext(t.Context(), "/usr/bin/python3", "-I", "-B", "-c", script, repo)
				command.Env = []string{"PATH=" + os.Getenv("PATH")}
				command.Stdin = bytes.NewReader(wire)
				var out, diagnostic bytes.Buffer
				command.Stdout, command.Stderr = &out, &diagnostic
				require.NoError(t, command.Run(), "oracle process failed: %s", diagnostic.String())
				var result map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(out.Bytes(), &result))
				return result
			}
			var query string
			require.NoError(t, json.Unmarshal(bridge(map[string]any{"action": "query", "cutoff": cutoff})["query"], &query))
			// Execute the unchanged production multi-statement READ ONLY query
			// through native simple protocol, retaining only its hash observations.
			probe := func(text string) map[string]json.RawMessage {
				results, e := conn.Conn().PgConn().Exec(t.Context(), text).ReadAll()
				require.NoError(t, e)
				var raw []byte
				for _, r := range results {
					if len(r.Rows) == 1 && len(r.Rows[0]) == 1 {
						raw = r.Rows[0][0]
					}
				}
				var result map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(raw, &result))
				return result
			}
			sourceQuery := strings.Replace(query, "FROM admin_sessions t", "FROM original_admin_sessions t", 1)
			original := probe(sourceQuery)
			plan := map[string]any{"writer": map[string]string{"started_at": started.UTC().Format(time.RFC3339Nano)},
				"allowed_session_gc": map[string]any{"table": "admin_sessions", "source_table": original["table"], "max_removed": 1, "expires_before": cutoff}}
			input := func(current map[string]json.RawMessage) map[string]any {
				return map[string]any{"action": "delta", "source": original, "current": current, "plan": plan}
			}
			// Invoke the actual production cleanup; no SQL imitation of deletion.
			require.NoError(t, f.handler.CleanupOldData(t.Context(), 30))
			current := probe(query)
			result := bridge(input(current))
			accepted := string(result["accepted"]) == "true"
			require.Equal(t, scenario == "expired" || scenario == "live", accepted)
			if !accepted {
				return
			}
			var where string
			require.NoError(t, json.Unmarshal(result["where"], &where))
			projected := probe(strings.Replace(sourceQuery, "FROM original_admin_sessions t", "FROM original_admin_sessions t"+where, 1))
			require.JSONEq(t, string(result["reference"]), string(projected["table"]), "expected fingerprint must be derived from original rows")
			require.JSONEq(t, string(result["reference"]), string(current["table"]))
			if scenario == "live" {
				_, err = f.pool.Exec(t.Context(), `UPDATE admin_sessions SET revoked_at=now()`)
				require.NoError(t, err)
				require.Equal(t, "false", string(bridge(input(probe(query)))["accepted"]), "a retained live row change refuses")
				_, err = f.pool.Exec(t.Context(), `DELETE FROM admin_sessions`)
				require.NoError(t, err)
				require.Equal(t, "false", string(bridge(input(probe(query)))["accepted"]), "missing live row refuses")
			} else {
				_, err = f.pool.Exec(t.Context(), `INSERT INTO admin_sessions(user_id,sid_hash,issued_at,expires_at)
VALUES($1,decode(repeat('33',32),'hex'),now()-interval '2 hours',now()-interval '1 hour')`, f.actorID)
				require.NoError(t, err)
				require.Equal(t, "false", string(bridge(input(probe(query)))["accepted"]), "a newly added expired row refuses")
			}
		})
	}
}
