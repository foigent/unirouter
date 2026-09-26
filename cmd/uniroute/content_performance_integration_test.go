//go:build integration

package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/admin"
	"github.com/uniroute/unirouter/internal/agentcap"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/capbilling"
	"github.com/uniroute/unirouter/internal/completedfeed"
	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/filter"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/limitpolicy"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelexecution"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/payloadpolicy"
	"github.com/uniroute/unirouter/internal/policybilling"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/sourcefacts"
	"github.com/uniroute/unirouter/internal/testutil"
)

// This opt-in fixture measures the real Chat handler. The load generator and
// synthetic HTTP provider run in another process; they are excluded from RSS.
// It is not a production listener or an installed-artifact qualification.
func TestContentHTTPPerformanceHarness(t *testing.T) {
	if os.Getenv("UR_CONTENT_PERFORMANCE") != "1" {
		t.Skip("explicit synthetic performance fixture only")
	}
	mode := os.Getenv("UR_CONTENT_PERFORMANCE_MODE")
	require.Contains(t, []string{"disabled", "metadata_only", "full_content"}, mode)
	output := os.Getenv("UR_CONTENT_PERFORMANCE_OUTPUT")
	require.True(t, filepath.IsAbs(output))
	native := newContentPerformanceNativeDiagnostics(os.Getenv("UR_CONTENT_PERFORMANCE_NATIVE_DIAGNOSTICS") == "1")
	if native != nil {
		require.Equal(t, "disabled", mode)
		require.True(t, contentPerformanceNativeTraceActive(), "native diagnostics require -test.trace")
		t.Cleanup(native.endTasks)
	}
	upstream, err := url.Parse(os.Getenv("UR_CONTENT_PERFORMANCE_PROVIDER"))
	require.NoError(t, err)
	require.Equal(t, "http", upstream.Scheme)
	require.Equal(t, "127.0.0.1", upstream.Hostname())
	require.Empty(t, upstream.User)
	var pgDiagnostics *contentPerformancePG
	if native != nil {
		pgDiagnostics = newContentPerformancePG()
	}
	pool := contentPerformanceRuntimePool(t, testutil.SetupMigratedTestDB(t), pgDiagnostics)
	if pgDiagnostics != nil {
		// Registered before all runtime owners: LIFO runs this after their joins,
		// before pool/clone cleanup, including failure exits. No diagnostic goroutine.
		t.Cleanup(func() {
			if err := contentPerformanceWrite(filepath.Join(output, "pg-client.json"), pgDiagnostics.snapshot()); err != nil {
				t.Errorf("CC13 pg-client diagnostic write: %s", contentPerformanceErrorClass(err))
			}
		})
	}
	var nativeDatabase contentPerformancePGDatabase
	if native != nil {
		bindingCtx, bindingCancel := context.WithTimeout(t.Context(), 5*time.Second)
		nativeDatabase, err = contentPerformanceReadPGDatabase(bindingCtx, pool)
		bindingCancel()
		require.NoError(t, err) // Diagnostic startup binding, before any warmup/load.
	}
	sourceHTTPIsolateRedisIdentities(t, pool)
	org, owner, project := seedPRD2P0OrgProjectUser(t, pool, "developer")
	testutil.NewTestWalletCents(t, pool, org, 10000000)
	testutil.NewTestModelPrice(t, pool, "gpt-4o", 2, 10)
	seedPRD2P0ModelGovernance(t, pool, org, project)
	seedPRD2P0Channel(t, pool, upstream.String(), "gpt-4o")
	// The same public synthetic key as the existing source-HTTP fixture.
	raw := "ur-synthetic-source-" + strings.Repeat("a", 32)
	var keyID int64
	var qps, rpm int
	err = pool.QueryRow(t.Context(), `INSERT INTO api_keys(org_id,project_id,owner_user_id,key_type,name,key_hash,key_prefix,allowed_models,status,qps_limit,rpm_limit) VALUES($1,$2,$3,'shared','synthetic-source',$4,'ur-synthetic','company-chat','active',6020,6020) RETURNING id,qps_limit,rpm_limit`, org, project, owner, auth.HashAPIKey(raw, prd2E2EJWTSecret)).Scan(&keyID, &qps, &rpm)
	require.NoError(t, err)
	// One bounded round has 20 warmup + 2*200*15 = 6020 operations.
	// This synthetic key allowance removes rate-policy interference; it is not
	// a capacity claim or a production default. RETURNING is the database value.
	require.Equal(t, 6020, qps)
	require.Equal(t, 6020, rpm)
	const secret = "synthetic-source-http-cursor-secret-only"
	source, err := sourcefacts.New(pool, sourcefacts.Options{Enabled: true, ContentEnabled: true, ObservationsEnabled: true, SourceInstance: "synthetic-http", CursorSecret: secret, I01Enabled: true})
	require.NoError(t, err)
	v1, err := completedfeed.NewService(pool, secret, source.SourceInstance(), true)
	require.NoError(t, err)
	b := billing.NewBilling(pool, nil).WithMode(billing.BillingModeCostObservation)
	b.SetReservationMaterializer(source)
	b.SetTerminalMaterializer(billing.TerminalMaterializers{v1, source})
	journal, err := outbox.New(pool, outbox.DefaultOptions())
	require.NoError(t, err)
	b.SetOutbox(journal)
	require.NoError(t, journal.RegisterHandler(outbox.EventSourceDispatch, source.ProcessDispatchEvent))
	for _, kind := range []string{outbox.EventBillingUsagePrepared, outbox.EventBillingSettled, outbox.EventBillingCancelled} {
		require.NoError(t, journal.RegisterHandler(kind, native.outboxRegion("financial_journal", b.ProcessUsageJournalEvent)))
	}
	require.NotEmpty(t, os.Getenv("REDIS_ADDR"))
	redisDB, err := strconv.Atoi(os.Getenv("REDIS_DB"))
	require.NoError(t, err)
	rdb := redis.NewClient(&redis.Options{Addr: os.Getenv("REDIS_ADDR"), Password: os.Getenv("REDIS_PASSWORD"), DB: redisDB})
	t.Cleanup(func() { _ = rdb.Close() })
	if native != nil {
		rdb.AddHook(native)
	}
	capabilities, err := agentcap.NewService(pool, rdb, prd2E2EJWTSecret)
	require.NoError(t, err)
	limits, err := limitpolicy.NewService(pool, rdb, limitpolicy.DefaultOptions())
	require.NoError(t, err)
	require.NoError(t, limits.Refresh(t.Context()))
	stopPolicy, policyDone := contentPerformanceStartOwner(t.Context(), limits.RunRefreshLoop)
	t.Cleanup(func() { contentPerformanceStopAndJoin(stopPolicy, policyDone) })
	limitSubject := limitpolicy.Subject{OrgID: org, ProjectID: project, APIKeyID: keyID, Model: "company-chat", BaseRPSLimit: int64(qps), BaseRPMLimit: int64(rpm)}
	initialPolicy, err := limits.Readiness(t.Context(), limitSubject)
	require.NoError(t, err)
	require.Equal(t, limitpolicy.ReadinessReady, initialPolicy.State)
	hooks, err := newRuntimeTerminalHooks(pool, journal, capabilities, limits)
	require.NoError(t, err)
	require.NoError(t, journal.RegisterHandler(outbox.EventRuntimeTerminal, native.outboxRegion("terminal_recovery", hooks.ProcessEvent)))
	require.NoError(t, journal.RegisterHandler(outbox.EventRuntimeTerminalV1, native.outboxRegion("terminal_recovery", hooks.ProcessEvent)))
	audit := admin.NewHandler(pool, prd2E2EJWTSecret, prd2E2EEncryptionKey, nil)
	require.NoError(t, journal.RegisterHandler(outbox.EventAudit, audit.ProcessAuditOutbox))
	phaseHooks := contentPerformanceTerminalHooks{hooks}
	capBilling := capbilling.New(contentPerformanceCoreBilling{b}, capabilities)
	capBilling.SetTerminalHooks(phaseHooks)
	shared := policybilling.New(capBilling, contentPerformancePolicyRuntime{limits})
	shared.SetTerminalHooks(phaseHooks)
	relay := gateway.NewRelay(pool, rdb, gateway.NewRouter(pool), contentPerformanceBilling{shared}, filter.NewDFAFilter(nil), prd2E2EEncryptionKey)
	relay.SetOutbox(journal)
	require.NoError(t, journal.RegisterHandler(outbox.EventCallLog, relay.ProcessCallLogOutbox))
	require.NoError(t, journal.RegisterHandler(outbox.EventTrace, relay.ProcessTraceOutbox))
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.Chmod(root, 0700))
	binding, err := contentcapture.InitializeFileStore(root, "synthetic-http-pg")
	require.NoError(t, err)
	private, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.Chmod(private, 0700))
	tokenFile := filepath.Join(private, "binding")
	require.NoError(t, os.WriteFile(tokenFile, []byte(binding.Token), 0600))
	cfg := &config.Config{SourceContentEnabled: true, SourceContentFullEnabled: true, SourceContentStorageRoot: root, SourceContentStorageIdentity: binding.Identity, SourceContentStorageTokenFile: tokenFile, CompletedExecutionFeedSourceInstance: source.SourceInstance(), PayloadCaptureMode: contentcapture.ModeMetadataOnly, StreamMaxDurationSecs: 1800}
	runtime, err := newContentRuntime(cfg, source)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, runtime.close(ctx, true))
	})
	// All modes retain exactly the production engine/queue/start/finalize limits.
	// Only legal policy fields differ. Full reservations cover the 8 KiB body.
	retention, quota, maximum := int64(3600), int64(512<<20), int64(64<<10)
	expiry := os.Getenv("UR_CONTENT_PERFORMANCE_EXPIRY") == "1"
	if expiry {
		require.Equal(t, "full_content", mode)
		retention = 10
	}
	policy := contentcapture.PolicyConfig{Mode: mode, SupportedProtocols: runtime.options.Policy.SupportedProtocols, ObservationPoints: runtime.options.Policy.ObservationPoints, AllowedPurposes: []string{}}
	if mode == "full_content" {
		policy.AllowedPurposes = []string{"human_review"}
		policy.RetentionSeconds = &retention
		policy.QuotaBytes = &quota
		policy.MaxSavedBytes = &maximum
		policy.OnLimit = "record_capture_gap"
	}
	require.NoError(t, policy.Validate())
	wire, err := json.Marshal(policy)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `INSERT INTO execution_content_policies(org_id,project_id,desired_version,effective_version,desired_config,effective_config,effective_at,application_status,application_reason) VALUES($1,$2,1,1,$3,$3,clock_timestamp(),'applied','none')`, org, project, wire)
	require.NoError(t, err)
	relay.SetContentCapture(runtime.service)
	routing := routingpolicy.Default()
	planner, err := modelrouting.NewPlanner(pool, routing)
	require.NoError(t, err)
	execution, err := modelexecution.ConfigFromRoutingPolicy(routing)
	require.NoError(t, err)
	coordinator, err := modelexecution.New(pool, b, execution)
	require.NoError(t, err)
	b.SetCommittedObservationSink(source)
	coordinator.SetDispatchMaterializer(source)
	require.NoError(t, relay.SetModelExecutionRuntime(modelrouting.ModeLegacy, planner, coordinator, routing))
	guarded := middleware.IdempotencyGuard(middleware.NewPostgresIdempotencyStore(pool), prd2E2EJWTSecret, newGatewayChatHandler(pool, relay))
	retained := payloadpolicy.RequestMiddlewareWithMax(payloadpolicy.MustNewBudget(payloadpolicy.DefaultLimits()), 8*payloadpolicy.MiB, middleware.GetOrgIDFromContext)
	requests := &contentPerformanceRequests{seen: map[string]bool{}, http: map[string]int{}, native: native}
	capturedHandler := middleware.CaptureExecutionContext(contentPerformanceCapture{sourceCaptureDiagnostics{Service: source, t: t}})(middleware.CaptureContent(runtime.service)(middleware.StrictJSONIngress(guarded)))
	observed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := middleware.GetAPIKeyFromContext(r.Context())
		validScope := key != nil && key.OrgID == org && key.ProjectID != nil && *key.ProjectID == project
		contentPerformancePhaseFrom(r.Context()).mark("authentication_complete", "none")
		requests.record(middleware.GetRequestIDFromContext(r.Context()), validScope)
		capturedHandler.ServeHTTP(w, r)
	})
	authenticated := middleware.GatewayAuthAcrossBufferedBody(middleware.GatewayAuthWithAgentCapabilities(pool, prd2E2EJWTSecret, capabilities), retained)(observed)
	handler := middleware.OperationIdentityWithSecret(prd2E2EJWTSecret)(contentPerformanceObserveHTTP(requests, authenticated))
	before := contentPerformanceSnapshot(t, pool, root)
	require.NoError(t, contentPerformanceWrite(filepath.Join(output, "before.json"), before))
	workerCtx, stopWorker := context.WithCancel(t.Context())
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); journal.Run(workerCtx) }()
	// Cleanup is an owner-exit barrier, not the one-minute success budget.
	// It must complete before the earlier registered pool/DB cleanup can run.
	t.Cleanup(func() { contentPerformanceStopAndJoin(stopWorker, workerDone) })
	done := make(chan struct{}, 1)
	mux := http.NewServeMux()
	mux.Handle("POST /v1/chat/completions", handler)
	// Test-only control, on the same ephemeral loopback listener. No database
	// mutation or file path is accepted from the caller.
	mux.HandleFunc("POST /__fixture/finish", func(w http.ResponseWriter, r *http.Request) {
		select {
		case done <- struct{}{}:
		default:
		}
		w.WriteHeader(http.StatusAccepted)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	readyEvidence := map[string]any{"url": server.URL, "pid": os.Getpid(), "mode": mode, "retention_seconds": retention, "engine": runtime.options.Engine, "outbox": outbox.DefaultOptions(), "limit_options": limitpolicy.DefaultOptions(), "limit_subject": limitSubject, "api_key_limits": map[string]int{"qps_limit": qps, "rpm_limit": rpm}, "policy_readiness": initialPolicy, "pool": contentPerformancePoolStats(pool)}
	if native != nil {
		readyEvidence["diagnostic_database"] = nativeDatabase
	}
	require.NoError(t, contentPerformanceWrite(filepath.Join(output, "ready.json"), readyEvidence))
	select {
	case <-done:
	case <-time.After(8 * time.Minute):
		t.Fatal("load driver did not finish within fixture envelope")
	}
	server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	// The worker remains live while the exact request set catches up. Every
	// query below uses the original total shutdown budget, never a fresh timeout.
	stopAtDeadline := context.AfterFunc(ctx, stopWorker)
	defer stopAtDeadline()
	expected, identityErr := requests.snapshot()
	// Persist one bounded snapshot before drain polling, within the original
	// total shutdown deadline. Auxiliary sampling does not change success gates.
	traceCtx, cancelTrace := context.WithTimeout(ctx, 5*time.Second)
	beforeTrace := contentPerformanceTraceDiagnostics(traceCtx, pool, org, project, expected)
	traceWriteErr := contentPerformanceWrite(filepath.Join(output, "trace-before-wait.json"), beforeTrace)
	cancelTrace()
	httpStatuses := requests.statuses()
	finalPolicy, policyErr := limits.Readiness(ctx, limitSubject)
	if policyErr == nil && finalPolicy.State != limitpolicy.ReadinessReady {
		policyErr = errors.New("policy refresh unavailable at load completion")
	}
	policyEvidence := map[string]any{"initial": initialPolicy, "final": finalPolicy, "error_class": contentPerformanceErrorClass(policyErr), "snapshot_advanced": finalPolicy.SnapshotLoadedAt.After(initialPolicy.SnapshotLoadedAt)}
	censusErr := contentPerformanceWrite(filepath.Join(output, "http-census.json"), requests.evidence())
	policyWriteErr := contentPerformanceWrite(filepath.Join(output, "policy-refresh.json"), policyEvidence)
	poolAfterHTTP := contentPerformancePoolStats(pool)
	// Retain a baseline even if capture Close itself consumes the budget.
	beforeOutbox := contentPerformanceOutboxDiagnostics(ctx, pool)
	beforePoints := contentPerformancePointDiagnostics(ctx, pool)
	beforeFinance := contentPerformanceFinancialDiagnostics(ctx, pool, org, project, expected)
	beforeOutbox["phase"] = "after_http_before_capture_close"
	beforePoints["phase"] = "after_http_before_capture_close"
	captureErr := runtime.service.Close(ctx)
	readState := func(ctx context.Context) (contentPerformanceDrainState, error) {
		return contentPerformanceReadDrainState(ctx, pool, org, project, expected, httpStatuses)
	}
	var state contentPerformanceDrainState
	finishErr := errors.Join(identityErr, captureErr, censusErr, policyErr, policyWriteErr)
	if finishErr == nil {
		state, finishErr = contentPerformanceWaitDrain(ctx, outbox.DefaultOptions().PollInterval, readState)
	}
	stopWorker()
	joinErr := contentPerformanceJoinWorker(ctx, workerDone)
	var finalState *contentPerformanceDrainState
	var recheckErr error
	if joinErr == nil {
		value, readErr := readState(ctx)
		recheckErr = readErr
		if readErr == nil {
			finalState = &value
		}
		if recheckErr == nil && !finalState.ready() {
			recheckErr = errors.New("post-stop projection or outbox mismatch")
		}
	}
	contentPerformanceStopAndJoin(stopPolicy, policyDone)
	afterOutbox := contentPerformanceOutboxDiagnostics(ctx, pool)
	afterPoints := contentPerformancePointDiagnostics(ctx, pool)
	afterFinance := contentPerformanceFinancialDiagnostics(ctx, pool, org, project, expected)
	// Keep both pre-wait evidence and final error envelopes. Expired-context
	// diagnostics report unavailable instead of inventing empty observations.
	report := map[string]any{"expected_requests": len(expected), "wait_state": state, "post_stop_state": finalState, "finish_error_class": contentPerformanceErrorClass(finishErr), "join_error_class": contentPerformanceErrorClass(joinErr), "recheck_error_class": contentPerformanceErrorClass(recheckErr), "policy_refresh_joined": true, "pool_after_http": poolAfterHTTP, "pool_after_wait": contentPerformancePoolStats(pool)}
	outputs := []struct {
		name  string
		value any
	}{
		{"financial-before-wait.json", beforeFinance}, {"financial-after-wait.json", afterFinance},
		{"before-worker-stop.json", beforeOutbox}, {"capture-points-before-wait.json", beforePoints},
		{"after-drain-diagnostics.json", afterOutbox}, {"capture-points.json", afterPoints},
	}
	writeFailed := false
	for _, item := range outputs {
		if err := contentPerformanceWrite(filepath.Join(output, item.name), item.value); err != nil {
			writeFailed = true
			t.Logf("CC13 diagnostic artifact %s: %s", item.name, contentPerformanceErrorClass(err))
		}
	}
	diagnosticsComplete := afterOutbox["complete"] == true && afterPoints["complete"] == true && afterFinance["complete"] == true
	report["trace_before_wait_complete"] = beforeTrace["complete"]
	report["trace_before_wait_error_class"] = beforeTrace["error_class"]
	report["trace_before_wait_truncated"] = beforeTrace["truncated"]
	report["trace_before_wait_write_error_class"] = contentPerformanceErrorClass(traceWriteErr)
	if native != nil {
		nativeErr := contentPerformanceWrite(filepath.Join(output, "workload-lifecycle.json"), native.evidence(joinErr == nil))
		report["native_diagnostic_write_error_class"] = contentPerformanceErrorClass(nativeErr)
		if nativeErr != nil {
			t.Logf("CC13 native diagnostic artifact: %s", contentPerformanceErrorClass(nativeErr))
		}
	}
	report["diagnostics_complete"] = diagnosticsComplete
	report["diagnostic_write_failed"] = writeFailed
	reportErr := contentPerformanceWrite(filepath.Join(output, "shutdown.json"), report)
	// No Fatal occurs before all diagnostic writes and stop/join are attempted.
	t.Logf("CC13 shutdown: finish=%s join=%s recheck=%s report_write=%s", contentPerformanceErrorClass(finishErr), contentPerformanceErrorClass(joinErr), contentPerformanceErrorClass(recheckErr), contentPerformanceErrorClass(reportErr))
	if finishErr != nil || joinErr != nil || recheckErr != nil || writeFailed || reportErr != nil || !diagnosticsComplete {
		t.Fatal("CC13 shutdown incomplete; see safe diagnostic artifacts")
	}
	after := contentPerformanceSnapshot(t, pool, root, ctx)
	require.NoError(t, contentPerformanceWrite(filepath.Join(output, "after.json"), after))
	if expiry {
		var latest time.Time
		require.NoError(t, pool.QueryRow(ctx, `SELECT max(expires_at) FROM execution_content_objects WHERE state='ready'`).Scan(&latest))
		if delay := time.Until(latest) + time.Millisecond; delay > 0 {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
		var usageBefore, usageAfter syscall.Rusage
		require.NoError(t, syscall.Getrusage(syscall.RUSAGE_SELF, &usageBefore))
		start := time.Now()
		batchCtx, batchCancel := context.WithTimeout(ctx, 30*time.Second)
		reclaimed, err := source.ReclaimExpiredContent(batchCtx, runtime.files, 1000)
		batchCancel()
		cleanupSeconds := time.Since(start).Seconds()
		require.NoError(t, syscall.Getrusage(syscall.RUSAGE_SELF, &usageAfter))
		cpuSeconds := float64(usageAfter.Utime.Sec-usageBefore.Utime.Sec+usageAfter.Stime.Sec-usageBefore.Stime.Sec) + float64(usageAfter.Utime.Usec-usageBefore.Utime.Usec+usageAfter.Stime.Usec-usageBefore.Stime.Usec)/1e6
		peakRSS := usageAfter.Maxrss
		if goruntime.GOOS != "darwin" {
			peakRSS *= 1024
		}
		end := contentPerformanceSnapshot(t, pool, root, ctx)
		require.NoError(t, contentPerformanceWrite(filepath.Join(output, "expiry.json"), map[string]any{"result": reclaimed, "seconds": cleanupSeconds, "cpu_seconds": cpuSeconds, "lifetime_peak_rss_bytes": peakRSS, "after": end, "error": fmt.Sprint(err)}))
		require.NoError(t, err)
		require.EqualValues(t, after["body_files"], reclaimed.Reclaimed)
		require.EqualValues(t, after["stored_bytes"], reclaimed.ReleasedBytes)
		for _, name := range after["ready_object_names"].([]string) {
			_, statErr := os.Lstat(filepath.Join(root, name))
			require.True(t, os.IsNotExist(statErr), "exact previously-ready file must be absent")
		}
		require.Equal(t, after["manifest_sha256"], end["manifest_sha256"])
		require.EqualValues(t, 0, end["stored_bytes"])
		require.EqualValues(t, 0, end["reserved_bytes"])
	}
}

func contentPerformanceWrite(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func contentPerformanceSnapshot(t *testing.T, pool *pgxpool.Pool, root string, parents ...context.Context) map[string]any {
	t.Helper()
	parent := t.Context()
	if len(parents) > 0 {
		parent = parents[0]
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	result := map[string]any{}
	for name, sql := range map[string]string{
		"db_bytes":                         `SELECT pg_database_size(current_database())`,
		"metadata_relation_bytes":          `SELECT COALESCE(sum(pg_total_relation_size(oid)),0)::bigint FROM pg_class WHERE relnamespace='public'::regnamespace AND relkind='r' AND relname LIKE 'execution_content_%'`,
		"manifest_count":                   `SELECT count(*) FROM execution_content_manifests`,
		"manifest_document_bytes":          `SELECT COALESCE(sum(octet_length(document)),0)::bigint FROM execution_content_manifests`,
		"stored_bytes":                     `SELECT COALESCE(sum(stored_bytes),0)::bigint FROM execution_content_policies`,
		"reserved_bytes":                   `SELECT COALESCE(sum(reserved_bytes),0)::bigint FROM execution_content_policies`,
		"settled_requests":                 `SELECT count(*) FROM freeze_idempotency WHERE status='settled'`,
		"economic_items":                   `SELECT count(*) FROM completed_execution_feed_items`,
		"invalid_request_observation_sets": `SELECT count(*) FROM (SELECT request_id,count(*) AS n,count(DISTINCT convert_from(document,'UTF8')::jsonb->>'observation_point') AS points,bool_and((convert_from(document,'UTF8')::jsonb->>'observation_point')=ANY(ARRAY['client_request','client_response','provider_request','provider_response'])) AS legal FROM (SELECT DISTINCT ON(source_fact_id) request_id,document FROM execution_content_manifests ORDER BY source_fact_id,revision DESC) m GROUP BY request_id) r WHERE n<>4 OR points<>4 OR NOT legal`,
		"outbox_remaining":                 `SELECT count(*) FROM internal_outbox_events`,
		"provider_attempts":                `SELECT count(*) FROM billing_provider_attempts`,
	} {
		var n int64
		require.NoError(t, pool.QueryRow(ctx, sql).Scan(&n))
		result[name] = n
	}
	var lsn string
	require.NoError(t, pool.QueryRow(ctx, `SELECT pg_current_wal_lsn()::text`).Scan(&lsn))
	result["wal_lsn"] = lsn
	groups := func(sql string) map[string]int64 {
		rows, err := pool.Query(ctx, sql)
		require.NoError(t, err)
		defer rows.Close()
		v := map[string]int64{}
		for rows.Next() {
			var key string
			var n int64
			require.NoError(t, rows.Scan(&key, &n))
			v[key] = n
		}
		require.NoError(t, rows.Err())
		return v
	}
	result["object_states"] = groups(`SELECT state,count(*) FROM execution_content_objects GROUP BY state`)
	result["latest_capture_states"] = groups(`SELECT concat_ws('/',(convert_from(document,'UTF8')::jsonb)->'capture'->>'completeness',(convert_from(document,'UTF8')::jsonb)->'capture'->>'reason'),count(*) FROM (SELECT DISTINCT ON(source_fact_id) document FROM execution_content_manifests ORDER BY source_fact_id,revision DESC) m GROUP BY 1`)
	rows, err := pool.Query(ctx, `SELECT document FROM execution_content_manifests ORDER BY org_id,project_id,sequence`)
	require.NoError(t, err)
	digest := sha256.New()
	for rows.Next() {
		var data []byte
		require.NoError(t, rows.Scan(&data))
		fmt.Fprintf(digest, "%d:", len(data))
		digest.Write(data)
	}
	require.NoError(t, rows.Err())
	rows.Close()
	result["manifest_sha256"] = hex.EncodeToString(digest.Sum(nil))
	files, err := pool.Query(ctx, `SELECT relative_path,saved_bytes FROM execution_content_objects WHERE state='ready'`)
	require.NoError(t, err)
	var actual, objects, allocated int64
	names := []string{}
	for files.Next() {
		var name string
		var saved int64
		require.NoError(t, files.Scan(&name, &saved))
		require.Len(t, name, 64)
		_, err := hex.DecodeString(name)
		require.NoError(t, err)
		info, err := os.Lstat(filepath.Join(root, name))
		require.NoError(t, err)
		require.True(t, info.Mode().IsRegular())
		require.Equal(t, saved, info.Size())
		names = append(names, name)
		actual += info.Size()
		stat, ok := info.Sys().(*syscall.Stat_t)
		require.True(t, ok)
		allocated += stat.Blocks * 512
		objects++
	}
	require.NoError(t, files.Err())
	files.Close()
	result["ready_object_names"] = names
	result["body_actual_bytes"] = actual
	result["body_files"] = objects
	result["body_allocated_bytes"] = allocated
	return result
}

// Diagnostic-only reads after the load window. No payload, last_error text,
// credential, event key or DSN is exported. Raw rows remain in the test DB.
func contentPerformanceOutboxDiagnostics(ctx context.Context, pool db.DBQuerier) map[string]any {
	result := map[string]any{"boundary": "independent read statements; zero claimed is not zero remaining", "complete": false, "error_class": "none"}
	fail := func(err error) map[string]any {
		result["error_class"] = contentPerformanceErrorClass(err)
		return result
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	var total, settled, economic int64
	var observed time.Time
	if err := pool.QueryRow(ctx, `SELECT statement_timestamp(),(SELECT count(*) FROM internal_outbox_events),(SELECT count(*) FROM freeze_idempotency WHERE status='settled'),(SELECT count(*) FROM completed_execution_feed_items)`).Scan(&observed, &total, &settled, &economic); err != nil {
		return fail(err)
	}
	result["observed_at"] = observed
	result["remaining"] = total
	result["settled_requests"] = settled
	result["economic_items"] = economic
	rows, err := pool.Query(ctx, `SELECT event_type,attempts,available_at>statement_timestamp(),COALESCE(claimed_until>statement_timestamp(),false),dead_lettered_at IS NOT NULL,CASE WHEN COALESCE(last_error,'')='' THEN 'none' WHEN last_error='context canceled' THEN 'context_cancelled' WHEN last_error='context deadline exceeded' THEN 'deadline_exceeded' ELSE 'other_redacted' END,count(*) FROM internal_outbox_events GROUP BY 1,2,3,4,5,6 ORDER BY 1,2,3,4,5,6 LIMIT 65`)
	if err != nil {
		return fail(err)
	}
	defer rows.Close()
	groups := []map[string]any{}
	for rows.Next() {
		var kind, errorClass string
		var attempts int
		var delayed, leased, dead bool
		var count int64
		if err := rows.Scan(&kind, &attempts, &delayed, &leased, &dead, &errorClass, &count); err != nil {
			return fail(err)
		}
		// All event types are from this synthetic fixture's registered product lanes.
		if !contentPerformanceKnownEvent(kind) {
			kind = "other_redacted"
		}
		groups = append(groups, map[string]any{"event_type": kind, "attempts": attempts, "available_in_future": delayed, "lease_in_future": leased, "dead_lettered": dead, "error_class": errorClass, "count": count, "eligible_by_time": !delayed && !leased && !dead})
		result["groups_truncated"] = len(groups) > 64
		result["groups"] = groups[:min(len(groups), 64)]
	}
	if err := rows.Err(); err != nil {
		return fail(err)
	}
	result["groups_truncated"] = len(groups) > 64
	if len(groups) > 64 {
		groups = groups[:64]
	}
	result["groups"] = groups
	result["complete"] = true
	return result
}
func contentPerformanceKnownEvent(kind string) bool {
	switch kind {
	case outbox.EventSourceDispatch, outbox.EventBillingUsagePrepared, outbox.EventBillingSettled, outbox.EventBillingCancelled, outbox.EventRuntimeTerminal, outbox.EventRuntimeTerminalV1, outbox.EventAudit, outbox.EventCallLog, outbox.EventTrace:
		return true
	}
	return false
}

func contentPerformancePointDiagnostics(ctx context.Context, pool db.DBQuerier) map[string]any {
	result := map[string]any{"sample_limit": 256, "complete": false, "error_class": "none", "boundary": "typed synthetic manifests; no body bytes; bounded sample"}
	points := []sourcefacts.ContentManifest{}
	fail := func(err error) map[string]any {
		result["error_class"] = contentPerformanceErrorClass(err)
		result["truncated"] = len(points) > 256
		result["points"] = points[:min(len(points), 256)]
		return result
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	// Prefer incomplete captures, then stable identities. The bounded sample is
	// explicitly not an exhaustive manifest export during the full load test.
	rows, err := pool.Query(ctx, `SELECT document FROM (SELECT DISTINCT ON(org_id,project_id,source_fact_id) org_id,project_id,source_fact_id,document FROM execution_content_manifests ORDER BY org_id,project_id,source_fact_id,revision DESC) m ORDER BY ((convert_from(document,'UTF8')::jsonb->'capture'->>'completeness')='complete'),org_id,project_id,source_fact_id LIMIT 257`)
	if err != nil {
		return fail(err)
	}
	defer rows.Close()
	for rows.Next() {
		var document []byte
		if err := rows.Scan(&document); err != nil {
			return fail(err)
		}
		var point sourcefacts.ContentManifest
		if err := json.Unmarshal(document, &point); err != nil {
			return fail(err)
		}
		if err := point.Validate(); err != nil {
			return fail(err)
		}
		points = append(points, point)
	}
	if err := rows.Err(); err != nil {
		return fail(err)
	}
	truncated := len(points) > 256
	if truncated {
		points = points[:256]
	}
	result["truncated"] = truncated
	result["points"] = points
	result["complete"] = true
	return result
}

// Test-only request census, collected after the actual authentication boundary.
// The driver still checks its own expected request count and operation IDs.
type contentPerformanceRequests struct {
	native  *contentPerformanceNativeDiagnostics
	mu      sync.Mutex
	seen    map[string]bool
	invalid bool
	http    map[string]int
	headers map[string]bool
	phases  map[string]*contentPerformancePhase
}

func (r *contentPerformanceRequests) record(id string, scopeOK bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !scopeOK || id == "" || len(id) > 64 || r.seen[id] {
		r.invalid = true
		return
	}
	r.seen[id] = true
}
func (r *contentPerformanceRequests) snapshot() ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]string, 0, len(r.seen)+len(r.http))
	if r.http != nil {
		for id := range r.http {
			ids = append(ids, id)
		}
		for id := range r.seen {
			if _, ok := r.http[id]; !ok {
				r.invalid = true
			}
		}
	} else {
		for id := range r.seen {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if r.invalid || len(ids) == 0 {
		return ids, errors.New("invalid observed request identities")
	}
	return ids, nil
}

func contentPerformanceStartOwner(parent context.Context, run func(context.Context)) (context.CancelFunc, <-chan struct{}) {
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() { defer close(done); run(ctx) }()
	return cancel, done
}
func (r *contentPerformanceRequests) recordHTTP(id string, status int, headerWritten bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id == "" || len(id) > 64 {
		r.invalid = true
		return
	}
	if _, duplicate := r.http[id]; duplicate {
		r.invalid = true
		return
	}
	r.http[id] = status
	if r.headers == nil {
		r.headers = map[string]bool{}
	}
	r.headers[id] = headerWritten
}
func (r *contentPerformanceRequests) statuses() map[string]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := make(map[string]int, len(r.http))
	for id, status := range r.http {
		copy[id] = status
	}
	return copy
}
func (r *contentPerformanceRequests) evidence() []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]string, 0, len(r.http))
	for id := range r.http {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	rows := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		row := map[string]any{"operation_id": id, "http_status": r.http[id], "authenticated_scope": r.seen[id], "final_header_written": r.headers[id]}
		if phase := r.phases[id]; phase != nil {
			row["timing"] = phase.snapshot()
		}
		rows = append(rows, row)
	}
	return rows
}

// Preserve the live transport and its optional flush support; never buffer SSE.
type contentPerformanceResponse struct {
	http.ResponseWriter
	status int
}

func (w *contentPerformanceResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *contentPerformanceResponse) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	if status >= 200 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *contentPerformanceResponse) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}

type contentPerformanceFlusher struct{ *contentPerformanceResponse }

func (w *contentPerformanceFlusher) FlushError() error {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}
func (w *contentPerformanceFlusher) Flush() { _ = w.FlushError() }
func contentPerformanceWrapResponse(w *contentPerformanceResponse) http.ResponseWriter {
	if _, ok := w.ResponseWriter.(http.Flusher); ok {
		return &contentPerformanceFlusher{w}
	}
	if _, ok := w.ResponseWriter.(interface{ FlushError() error }); ok {
		return &contentPerformanceFlusher{w}
	}
	return w
}

// These classifications describe cleanup, not benchmark acceptance. Any
// non-200/semantic error still fails the driver even when no finance was created.
type contentPerformanceDrainState struct {
	Outbox            int64 `json:"outbox_remaining"`
	DeadLetters       int64 `json:"dead_letters"`
	Expected          int64 `json:"expected_requests"`
	Unsettled         int64 `json:"unsettled_or_missing_requests"`
	MissingProjection int64 `json:"missing_or_wrong_terminal_projections"`
	NoFinancial       int64 `json:"rejected_without_financial_record"`
	Settled           int64 `json:"settled_requests"`
	Cancelled         int64 `json:"cancelled_requests"`
	PendingAttempts   int64 `json:"requests_with_pending_attempts"`
	Attempts          int64 `json:"provider_attempts"`
	HTTPFailures      int64 `json:"http_non_200_requests"`
}

func (s contentPerformanceDrainState) ready() bool {
	return s.Expected > 0 && s.Outbox == 0 && s.DeadLetters == 0 && s.Unsettled == 0 && s.MissingProjection == 0 && s.PendingAttempts == 0
}
func contentPerformanceReadDrainState(ctx context.Context, pool db.DBQuerier, org, project int64, expected []string, outcomes ...map[string]int) (contentPerformanceDrainState, error) {
	var s contentPerformanceDrainState
	statuses := make([]int, len(expected))
	for i, id := range expected {
		statuses[i] = http.StatusOK
		if len(outcomes) > 0 {
			statuses[i] = outcomes[0][id]
		}
	}
	// One statement snapshot, exact identities. Absence is exempt only for a
	// rejected HTTP response with no freeze, attempt OR terminal projection.
	// Wrong-project rows are explicitly mismatches, never treated as absence.
	err := pool.QueryRow(ctx, `WITH expected AS (SELECT * FROM unnest($3::text[],$4::int[]) AS e(request_id,http_status)), facts AS (
 SELECT e.*, f.status, f.project_id AS freeze_project, c.terminal_status, c.project_id AS feed_project,
 (SELECT count(*) FROM billing_provider_attempts a WHERE a.org_id=$1 AND a.request_id=e.request_id) AS attempts,
 EXISTS(SELECT 1 FROM billing_provider_attempts a WHERE a.org_id=$1 AND a.request_id=e.request_id AND
 a.state NOT IN ('settled','reconciled','rejected','released_undispatched')) AS pending_attempt,
 EXISTS(SELECT 1 FROM billing_provider_attempts a WHERE a.org_id=$1 AND a.request_id=e.request_id AND a.project_id IS DISTINCT FROM $2::bigint) AS wrong_attempt_scope
 FROM expected e LEFT JOIN freeze_idempotency f ON f.org_id=$1 AND f.request_id=e.request_id
 LEFT JOIN completed_execution_feed_items c ON c.org_id=$1 AND c.request_id=e.request_id)
 SELECT (SELECT count(*) FROM internal_outbox_events),
 (SELECT count(*) FROM internal_outbox_events WHERE dead_lettered_at IS NOT NULL), count(*),
 count(*) FILTER(WHERE wrong_attempt_scope OR (status IS NULL AND (http_status<400 OR attempts>0)) OR
 (status IS NOT NULL AND (status NOT IN ('settled','cancelled') OR freeze_project IS DISTINCT FROM $2::bigint OR (http_status=200 AND status<>'settled')))),
 count(*) FILTER(WHERE (status IN ('settled','cancelled') AND (terminal_status IS DISTINCT FROM status OR feed_project IS DISTINCT FROM $2::bigint)) OR (status IS NULL AND terminal_status IS NOT NULL)),
 count(*) FILTER(WHERE http_status>=400 AND status IS NULL AND attempts=0 AND terminal_status IS NULL),
 count(*) FILTER(WHERE status='settled'), count(*) FILTER(WHERE status='cancelled'),
 count(*) FILTER(WHERE pending_attempt), COALESCE(sum(attempts),0), count(*) FILTER(WHERE http_status<>200)
 FROM facts`, org, project, expected, statuses).Scan(&s.Outbox, &s.DeadLetters, &s.Expected, &s.Unsettled, &s.MissingProjection, &s.NoFinancial, &s.Settled, &s.Cancelled, &s.PendingAttempts, &s.Attempts, &s.HTTPFailures)
	return s, err
}

// Safe exact-operation facts for joining to the driver's HTTP and synthetic
// Provider observations. Never infer remote calls from a missing DB row.
func contentPerformanceFinancialDiagnostics(ctx context.Context, pool db.DBQuerier, org, project int64, expected []string) map[string]any {
	result := map[string]any{"complete": false, "records": []map[string]any{}}
	if err := ctx.Err(); err != nil {
		result["error_class"] = contentPerformanceErrorClass(err)
		return result
	}
	rows, err := pool.Query(ctx, `SELECT e.request_id,COALESCE(f.status,'absent'),COALESCE(f.project_id=$2,false),
 COALESCE(c.terminal_status,'absent'),COALESCE(c.project_id=$2,false),
 ARRAY(SELECT a.state FROM billing_provider_attempts a WHERE a.org_id=$1 AND a.request_id=e.request_id ORDER BY a.attempt_id),
 ARRAY(SELECT a.attempt_id FROM billing_provider_attempts a WHERE a.org_id=$1 AND a.request_id=e.request_id ORDER BY a.attempt_id),
 EXISTS(SELECT 1 FROM billing_provider_attempts a WHERE a.org_id=$1 AND a.request_id=e.request_id AND a.project_id IS DISTINCT FROM $2::bigint)
 FROM unnest($3::text[]) AS e(request_id)
 LEFT JOIN freeze_idempotency f ON f.org_id=$1 AND f.request_id=e.request_id
 LEFT JOIN completed_execution_feed_items c ON c.org_id=$1 AND c.request_id=e.request_id ORDER BY e.request_id`, org, project, expected)
	if err != nil {
		result["error_class"] = contentPerformanceErrorClass(err)
		return result
	}
	defer rows.Close()
	records := []map[string]any{}
	for rows.Next() {
		var id, freeze, terminal string
		var freezeScope, feedScope, wrongAttemptScope bool
		var states, attemptIDs []string
		if err = rows.Scan(&id, &freeze, &freezeScope, &terminal, &feedScope, &states, &attemptIDs, &wrongAttemptScope); err != nil {
			break
		}
		records = append(records, map[string]any{"operation_id": id, "freeze_status": freeze, "freeze_scope_matches": freezeScope, "terminal_status": terminal, "terminal_scope_matches": feedScope, "attempt_states": states, "attempt_ids": attemptIDs, "wrong_attempt_scope": wrongAttemptScope})
	}
	if err == nil {
		err = rows.Err()
	}
	result["records"] = records
	if err != nil {
		result["error_class"] = contentPerformanceErrorClass(err)
		return result
	}
	result["complete"] = true
	return result
}

func contentPerformanceWaitDrain(ctx context.Context, poll time.Duration, read func(context.Context) (contentPerformanceDrainState, error)) (contentPerformanceDrainState, error) {
	var last contentPerformanceDrainState
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return last, err
		}
		next, err := read(ctx)
		if err != nil {
			return last, err
		}
		last = next
		if err := ctx.Err(); err != nil {
			return last, err
		}
		if last.DeadLetters > 0 {
			return last, errors.New("outbox dead letter blocks completion")
		}
		if last.ready() {
			return last, nil
		}
		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-ticker.C:
		}
	}
}
func contentPerformanceStopAndJoin(stop context.CancelFunc, done <-chan struct{}) {
	stop()
	<-done
}
func contentPerformanceJoinWorker(ctx context.Context, done <-chan struct{}) error {
	// Prefer an already joined worker even if the deadline expired concurrently.
	select {
	case <-done:
		return nil
	default:
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func contentPerformanceErrorClass(err error) string {
	if err == nil {
		return "none"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	if errors.Is(err, context.Canceled) {
		return "context_cancelled"
	}
	return "other_redacted"
}

func TestContentPerformanceShutdownOffline(t *testing.T) {
	t.Run("request_census_rejects_duplicates_wrong_scope_and_empty", func(t *testing.T) {
		for _, kind := range []string{"normal", "duplicate", "wrong_scope", "empty"} {
			r := &contentPerformanceRequests{seen: map[string]bool{}}
			if kind != "empty" {
				r.record("request-b", true)
				r.record("request-a", kind != "wrong_scope")
			}
			if kind == "duplicate" {
				r.record("request-a", true)
			}
			ids, err := r.snapshot()
			if kind == "normal" {
				require.NoError(t, err)
				require.Equal(t, []string{"request-a", "request-b"}, ids)
			} else {
				require.Error(t, err)
			}
		}
	})
	t.Run("live_worker_can_finish_at_existing_poll_interval", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			poll := outbox.DefaultOptions().PollInterval
			start := time.Now()
			reads := 0
			state, err := contentPerformanceWaitDrain(ctx, poll, func(context.Context) (contentPerformanceDrainState, error) {
				reads++
				if reads == 1 {
					return contentPerformanceDrainState{Expected: 2, Outbox: 1, MissingProjection: 1}, nil
				}
				return contentPerformanceDrainState{Expected: 2}, nil
			})
			require.NoError(t, err)
			require.True(t, state.ready())
			require.Equal(t, 2, reads)
			require.Equal(t, poll, time.Since(start))
		})
	})
	for _, kind := range []string{"nonempty_ineligible", "missing_projection", "missing_freeze", "empty_census", "dead_letter", "query_error", "cancel_during_read", "deadline_during_read"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
				defer cancel()
				start := time.Now()
				state, err := contentPerformanceWaitDrain(ctx, outbox.DefaultOptions().PollInterval, func(ctx context.Context) (contentPerformanceDrainState, error) {
					s := contentPerformanceDrainState{Expected: 1}
					switch kind {
					case "nonempty_ineligible":
						s.Outbox = 1
					case "missing_projection":
						s.MissingProjection = 1
					case "missing_freeze":
						s.Unsettled = 1
					case "empty_census":
						s.Expected = 0
					case "dead_letter":
						s.Outbox = 1
						s.DeadLetters = 1
					case "query_error":
						return s, errors.New("postgres://synthetic-secret must not escape")
					case "cancel_during_read":
						cancel()
					case "deadline_during_read":
						<-ctx.Done()
					}
					return s, nil
				})
				require.Error(t, err)
				require.LessOrEqual(t, time.Since(start), time.Minute)
				if kind != "cancel_during_read" && kind != "deadline_during_read" {
					require.False(t, state.ready())
				}
				if kind == "query_error" {
					require.Equal(t, "other_redacted", contentPerformanceErrorClass(err))
				}
			})
		})
	}
	t.Run("expired_diagnostics_never_open_a_fresh_query_budget", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		pool := &testutil.MockDB{QueryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
			t.Fatal("query after cancellation")
			return nil, nil
		}, QueryRowFn: func(context.Context, string, ...any) pgx.Row { t.Fatal("query after cancellation"); return nil }}
		for _, result := range []map[string]any{contentPerformanceOutboxDiagnostics(ctx, pool), contentPerformancePointDiagnostics(ctx, pool)} {
			require.Equal(t, false, result["complete"])
			require.Equal(t, "context_cancelled", result["error_class"])
		}
	})
	t.Run("query_failures_return_safe_artifacts_without_fatal", func(t *testing.T) {
		poison := errors.New("postgres://user:synthetic-secret@host/body")
		pool := &testutil.MockDB{QueryFn: func(context.Context, string, ...any) (pgx.Rows, error) { return nil, poison }, QueryRowFn: func(context.Context, string, ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(...any) error { return poison }}
		}}
		for i, result := range []map[string]any{contentPerformanceOutboxDiagnostics(t.Context(), pool), contentPerformancePointDiagnostics(t.Context(), pool)} {
			require.Equal(t, false, result["complete"])
			require.Equal(t, "other_redacted", result["error_class"])
			path := filepath.Join(t.TempDir(), fmt.Sprintf("diagnostic-%d.json", i))
			require.NoError(t, contentPerformanceWrite(path, result))
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			require.NotContains(t, string(data), "synthetic-secret")
			require.NotContains(t, string(data), "postgres://")
		}
	})
	t.Run("partial_classification_survives_later_row_failure", func(t *testing.T) {
		rows := &testutil.MockRows{Rows: []func(...any) error{
			func(d ...any) error {
				*d[0].(*string) = outbox.EventTrace
				*d[1].(*int) = 1
				*d[2].(*bool) = false
				*d[3].(*bool) = true
				*d[4].(*bool) = false
				*d[5].(*string) = "none"
				*d[6].(*int64) = 1
				return nil
			},
			func(...any) error { return errors.New("synthetic-secret row failure") },
		}}
		pool := &testutil.MockDB{QueryFn: func(context.Context, string, ...any) (pgx.Rows, error) { return rows, nil }, QueryRowFn: func(context.Context, string, ...any) pgx.Row {
			return &testutil.MockRow{ScanFn: func(d ...any) error {
				*d[0].(*time.Time) = time.Now()
				*d[1].(*int64) = 2
				*d[2].(*int64) = 1
				*d[3].(*int64) = 1
				return nil
			}}
		}}
		result := contentPerformanceOutboxDiagnostics(t.Context(), pool)
		require.Equal(t, false, result["complete"])
		require.Equal(t, "other_redacted", result["error_class"])
		require.Len(t, result["groups"], 1)
		require.True(t, rows.Closed)
		data, err := json.Marshal(result)
		require.NoError(t, err)
		require.NotContains(t, string(data), "synthetic-secret")
	})
	for _, kind := range []string{"cancelled", "deadline_expired"} {
		t.Run("cleanup_barrier_"+kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
				defer cancel()
				if kind == "cancelled" {
					cancel()
				} else {
					<-ctx.Done()
				}
				stopped := make(chan struct{})
				release := make(chan struct{})
				workerDone := make(chan struct{})
				resourcesReleased := make(chan struct{})
				require.Error(t, contentPerformanceJoinWorker(ctx, workerDone), "expired completion budget must report not joined")
				go func() { <-stopped; <-release; close(workerDone) }()
				go func() { contentPerformanceStopAndJoin(func() { close(stopped) }, workerDone); close(resourcesReleased) }()
				synctest.Wait()
				select {
				case <-stopped:
				default:
					t.Fatal("cleanup did not stop owner")
				}
				select {
				case <-resourcesReleased:
					t.Fatal("resource cleanup ran before owner exit")
				default:
				}
				close(release)
				synctest.Wait()
				select {
				case <-workerDone:
				default:
					t.Fatal("owner did not exit")
				}
				select {
				case <-resourcesReleased:
				default:
					t.Fatal("cleanup did not resume after owner exit")
				}
			})
		})
	}
	t.Run("join_is_bounded_and_never_claims_success_on_timeout", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			done := make(chan struct{})
			require.ErrorIs(t, contentPerformanceJoinWorker(ctx, done), context.DeadlineExceeded)
			close(done)
			require.NoError(t, contentPerformanceJoinWorker(ctx, done))
		})
	})
}

// These are isolated SQL predicate/worker negative tests, not fabricated
// financial acceptance evidence. Compile locally; execute only in an approved
// PG window. No Redis, HTTP server or external provider is required.
func TestContentPerformanceShutdownPG(t *testing.T) {
	for _, kind := range []string{"empty_and_exact", "future_lease", "delayed_available", "dead_letter", "cancelled_handler", "wrong_request_same_count", "wrong_project", "wrong_terminal", "wrong_freeze_project", "wrong_freeze_status", "rejected_without_finance", "rejected_pending_finance", "rejected_cancelled", "success_without_finance"} {
		t.Run(kind, func(t *testing.T) {
			pool := testutil.SetupMigratedTestDB(t)
			ctx := t.Context()
			org := testutil.NewTestOrg(t, pool)
			var project, otherProject int64
			require.NoError(t, pool.QueryRow(ctx, `INSERT INTO projects(org_id,name,slug) VALUES($1,'shutdown fixture','shutdown') RETURNING id`, org).Scan(&project))
			require.NoError(t, pool.QueryRow(ctx, `INSERT INTO projects(org_id,name,slug) VALUES($1,'other fixture','other') RETURNING id`, org).Scan(&otherProject))
			const request = "synthetic-shutdown-request"
			_, err := pool.Exec(ctx, `INSERT INTO freeze_idempotency(org_id,project_id,request_id,status) VALUES($1,$2,$3,'settled')`, org, project, request)
			require.NoError(t, err)
			if kind == "wrong_freeze_project" {
				_, err = pool.Exec(ctx, `UPDATE freeze_idempotency SET project_id=$2 WHERE org_id=$1 AND request_id=$3`, org, otherProject, request)
				require.NoError(t, err)
			}
			if kind == "wrong_freeze_status" {
				_, err = pool.Exec(ctx, `UPDATE freeze_idempotency SET status='cancelled' WHERE org_id=$1 AND request_id=$2`, org, request)
				require.NoError(t, err)
			}
			projected, projectionProject, terminal := request, project, "settled"
			if kind == "wrong_request_same_count" {
				projected = "unrelated-synthetic-request"
			}
			if kind == "wrong_project" {
				projectionProject = otherProject
			}
			if kind == "wrong_terminal" {
				terminal = "cancelled"
			}
			hash := sha256.Sum256([]byte("{}"))
			_, err = pool.Exec(ctx, `INSERT INTO completed_execution_feed_items(event_key,org_id,project_id,request_id,terminal_status,terminal_available_at,snapshot,snapshot_sha256) VALUES('synthetic-shutdown-feed',$1,$2,$3,$4,clock_timestamp(),$5,$6)`, org, projectionProject, projected, terminal, []byte("{}"), "sha256:"+hex.EncodeToString(hash[:]))
			require.NoError(t, err)
			service, err := outbox.New(pool, outbox.DefaultOptions())
			require.NoError(t, err)
			var eventID int64
			switch kind {
			case "future_lease", "delayed_available", "dead_letter", "cancelled_handler":
				require.NoError(t, pool.QueryRow(ctx, `INSERT INTO internal_outbox_events(event_key,event_type,priority,org_id,request_id) VALUES('synthetic-shutdown-event',$1,$2,$3,$4) RETURNING id`, outbox.EventTrace, outbox.PriorityTraceInfo, org, request).Scan(&eventID))
			}
			switch kind {
			case "future_lease":
				_, err = pool.Exec(ctx, `UPDATE internal_outbox_events SET claimed_by='synthetic-stopped-worker',claimed_until=clock_timestamp()+interval '1 hour' WHERE id=$1`, eventID)
			case "delayed_available":
				_, err = pool.Exec(ctx, `UPDATE internal_outbox_events SET available_at=clock_timestamp()+interval '1 hour' WHERE id=$1`, eventID)
			case "dead_letter":
				_, err = pool.Exec(ctx, `UPDATE internal_outbox_events SET dead_lettered_at=clock_timestamp() WHERE id=$1`, eventID)
			case "cancelled_handler":
				workerCtx, cancel := context.WithCancel(ctx)
				require.NoError(t, service.RegisterHandler(outbox.EventTrace, func(context.Context, outbox.Event) error { cancel(); return context.Canceled }))
				claimed, drainErr := service.DrainOnce(workerCtx)
				require.Equal(t, 1, claimed)
				require.Error(t, drainErr)
				cancel()
			}
			require.NoError(t, err)
			claimed, err := service.DrainOnce(ctx)
			require.NoError(t, err)
			require.Zero(t, claimed, "zero claimed may leave ineligible rows")
			statuses := map[string]int{request: 200}
			if strings.HasPrefix(kind, "rejected_") {
				statuses[request] = 429
			}
			if kind == "rejected_without_finance" || kind == "success_without_finance" {
				_, err = pool.Exec(ctx, `DELETE FROM completed_execution_feed_items WHERE org_id=$1 AND request_id=$2`, org, request)
				require.NoError(t, err)
				_, err = pool.Exec(ctx, `DELETE FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, org, request)
				require.NoError(t, err)
			}
			if kind == "rejected_pending_finance" {
				_, err = pool.Exec(ctx, `UPDATE freeze_idempotency SET status='frozen' WHERE org_id=$1 AND request_id=$2`, org, request)
				require.NoError(t, err)
			}
			if kind == "rejected_cancelled" {
				_, err = pool.Exec(ctx, `UPDATE freeze_idempotency SET status='cancelled' WHERE org_id=$1 AND request_id=$2`, org, request)
				require.NoError(t, err)
				_, err = pool.Exec(ctx, `UPDATE completed_execution_feed_items SET terminal_status='cancelled' WHERE org_id=$1 AND request_id=$2`, org, request)
				require.NoError(t, err)
			}
			state, err := contentPerformanceReadDrainState(ctx, pool, org, project, []string{request}, statuses)
			require.NoError(t, err)
			require.Equal(t, kind == "empty_and_exact" || kind == "rejected_without_finance" || kind == "rejected_cancelled", state.ready())
			if eventID > 0 {
				require.EqualValues(t, 1, state.Outbox)
				diagnostic := contentPerformanceOutboxDiagnostics(ctx, pool)
				require.Equal(t, true, diagnostic["complete"])
				groups := diagnostic["groups"].([]map[string]any)
				require.Len(t, groups, 1)
				require.Equal(t, false, groups[0]["eligible_by_time"])
				// Zero-claim observation must not mutate retry/lease/dead-letter state.
				var attempts int
				require.NoError(t, pool.QueryRow(ctx, `SELECT attempts FROM internal_outbox_events WHERE id=$1`, eventID).Scan(&attempts))
				if kind == "cancelled_handler" {
					require.Equal(t, 1, attempts)
					require.Equal(t, true, groups[0]["lease_in_future"])
				} else {
					require.Zero(t, attempts)
				}
			} else if strings.HasPrefix(kind, "rejected_") || kind == "success_without_finance" {
				if kind == "rejected_without_finance" {
					require.EqualValues(t, 1, state.NoFinancial)
				}
				if kind == "rejected_pending_finance" || kind == "success_without_finance" {
					require.EqualValues(t, 1, state.Unsettled)
				}
			} else if strings.HasPrefix(kind, "wrong_freeze_") {
				require.EqualValues(t, 1, state.Unsettled)
			} else if kind != "empty_and_exact" {
				require.EqualValues(t, 1, state.MissingProjection)
			}
		})
	}
}

func TestContentPerformanceCensusOffline(t *testing.T) {
	t.Run("auth_rejection_is_in_outer_census", func(t *testing.T) {
		r := &contentPerformanceRequests{seen: map[string]bool{}, http: map[string]int{}}
		r.record("admitted", true)
		r.recordHTTP("admitted", 200, true)
		r.recordHTTP("blocked", 409, true)
		ids, err := r.snapshot()
		require.NoError(t, err)
		require.Equal(t, []string{"admitted", "blocked"}, ids)
		require.Equal(t, []map[string]any{{"operation_id": "admitted", "http_status": 200, "authenticated_scope": true, "final_header_written": true}, {"operation_id": "blocked", "http_status": 409, "authenticated_scope": false, "final_header_written": true}}, r.evidence())
		r.recordHTTP("blocked", 409, true)
		_, err = r.snapshot()
		require.Error(t, err)
	})
	t.Run("rejected_finance_and_pending_are_not_blanket_exempt", func(t *testing.T) {
		require.True(t, (contentPerformanceDrainState{Expected: 1, NoFinancial: 1, HTTPFailures: 1}).ready())
		require.True(t, (contentPerformanceDrainState{Expected: 1, Cancelled: 1, HTTPFailures: 1}).ready())
		for _, s := range []contentPerformanceDrainState{{Expected: 1, HTTPFailures: 1, Unsettled: 1}, {Expected: 1, HTTPFailures: 1, PendingAttempts: 1}, {Expected: 1, HTTPFailures: 1, MissingProjection: 1}} {
			require.False(t, s.ready())
		}
	})
	t.Run("refresh_owner_cancel_and_join_before_dependency_release", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			exited := false
			stop, done := contentPerformanceStartOwner(t.Context(), func(ctx context.Context) { <-ctx.Done(); time.Sleep(time.Second); exited = true })
			contentPerformanceStopAndJoin(stop, done)
			require.True(t, exited)
			contentPerformanceStopAndJoin(stop, done)
		})
	})
	t.Run("live_sse_flush_arrives_before_handler_returns", func(t *testing.T) {
		release := make(chan struct{})
		status := make(chan int, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			captured := &contentPerformanceResponse{ResponseWriter: w}
			wrapped := contentPerformanceWrapResponse(captured)
			wrapped.Header().Set("Content-Type", "text/event-stream")
			_, err := io.WriteString(wrapped, "data: first\n\n")
			if err != nil {
				return
			}
			if err := http.NewResponseController(wrapped).Flush(); err != nil {
				return
			}
			<-release
			wrapped.WriteHeader(503)
			status <- captured.status
		}))
		defer server.Close()
		defer close(release)
		resp, err := server.Client().Get(server.URL)
		require.NoError(t, err)
		defer resp.Body.Close()
		line, err := bufio.NewReader(resp.Body).ReadString('\n')
		require.NoError(t, err)
		require.Equal(t, "data: first\n", line)
		// Unblock before server.Close; first final status must remain 200.
		release <- struct{}{}
		require.Equal(t, 200, <-status)
	})
	t.Run("flush_error_and_optional_interfaces_are_preserved", func(t *testing.T) {
		plain := &contentPerformancePlainWriter{}
		_, ok := contentPerformanceWrapResponse(&contentPerformanceResponse{ResponseWriter: plain}).(http.Flusher)
		require.False(t, ok)
		raw := &contentPerformanceFailingFlush{contentPerformancePlainWriter: plain}
		wrapped := contentPerformanceWrapResponse(&contentPerformanceResponse{ResponseWriter: raw})
		require.ErrorIs(t, http.NewResponseController(wrapped).Flush(), io.ErrClosedPipe)
	})
}

type contentPerformancePlainWriter struct{}

func (*contentPerformancePlainWriter) Header() http.Header         { return http.Header{} }
func (*contentPerformancePlainWriter) Write(p []byte) (int, error) { return len(p), nil }
func (*contentPerformancePlainWriter) WriteHeader(int)             {}

type contentPerformanceFailingFlush struct{ *contentPerformancePlainWriter }

func (*contentPerformanceFailingFlush) FlushError() error { return io.ErrClosedPipe }

// Preserve the original isolated-database cleanup, but use the same business
// pool constructor and limits as main. Register this cleanup before any owner:
// later owner cleanups run first, then this pool closes, then the clone drops.
func contentPerformanceRuntimePool(t *testing.T, preparation *pgxpool.Pool, diagnostics ...*contentPerformancePG) *pgxpool.Pool {
	t.Helper()
	dsn := preparation.Config().ConnString()
	preparation.Close()
	maxConns, minConns := config.LoadDBPoolLimits()
	postgres, err := db.NewPostgres(dsn, maxConns, minConns)
	require.NoError(t, err)
	// pgx snapshots its tracer on construction. Copy the actual production
	// constructor's config, close that pool, then add only a fixture tracer.
	// This is NOT a second concurrent pool or a production constructor change.
	if len(diagnostics) == 1 && diagnostics[0] != nil {
		cfg := postgres.Pool.Config()
		require.Nil(t, cfg.ConnConfig.Tracer, "do not replace an existing tracer")
		postgres.Close()
		cfg.ConnConfig.Tracer = diagnostics[0]
		instrumented, err := pgxpool.NewWithConfig(context.Background(), cfg)
		require.NoError(t, err)
		postgres.Pool = instrumented
	}
	t.Cleanup(postgres.Close)
	return postgres.Pool
}

// Aggregate local pgx counters only: no DSN, SQL text, backend identity or secret.
// Cumulative counters are compared with ready.json; this adds no pool acquisition.
func contentPerformancePoolStats(pool *pgxpool.Pool) map[string]any {
	st := pool.Stat()
	cfg := pool.Config()
	constructor := "db.NewPostgres"
	if _, ok := cfg.ConnConfig.Tracer.(*contentPerformancePG); ok {
		constructor = "db.NewPostgres config + test-only pgx tracer"
	}
	return map[string]any{"constructor": constructor, "limits_source": "config.LoadDBPoolLimits",
		"max_conns": st.MaxConns(), "min_conns": cfg.MinConns,
		"max_conn_lifetime_ns": cfg.MaxConnLifetime.Nanoseconds(), "max_conn_idle_time_ns": cfg.MaxConnIdleTime.Nanoseconds(),
		"health_check_period_ns": cfg.HealthCheckPeriod.Nanoseconds(),
		"total_conns":            st.TotalConns(), "acquired_conns": st.AcquiredConns(), "idle_conns": st.IdleConns(),
		"constructing_conns": st.ConstructingConns(), "acquire_count": st.AcquireCount(),
		"acquire_duration_ns": st.AcquireDuration().Nanoseconds(), "empty_acquire_count": st.EmptyAcquireCount(),
		"empty_acquire_wait_ns": st.EmptyAcquireWaitTime().Nanoseconds(), "canceled_acquire_count": st.CanceledAcquireCount(),
		"new_conns_count": st.NewConnsCount()}
}

// One statement snapshot, bounded rows and exact current-run scope. Retain
// pending trace envelopes too: a worker may first encounter their error later.
func contentPerformanceTraceDiagnostics(ctx context.Context, pool db.DBQuerier, org, project int64, expected []string) map[string]any {
	result := map[string]any{"complete": false, "error_class": "none", "org_id": org, "project_id": project, "phase": "after_http_before_drain_poll", "boundary": "one SQL snapshot; pending is not final failure", "traces": []any{}, "pending_trace_outbox": []any{}}
	if err := ctx.Err(); err != nil {
		result["error_class"] = contentPerformanceErrorClass(err)
		return result
	}
	if org <= 0 || project <= 0 || len(expected) == 0 || len(expected) > 6020 {
		result["error_class"] = "invalid_scope"
		return result
	}
	var wire []byte
	err := pool.QueryRow(ctx, `WITH scope AS (SELECT unnest($3::text[]) AS request_id),
 trace_sample AS (
  SELECT t.request_id AS operation_id,t.event_type,t.latency_ms,t.created_at,t.attempt_id,t.route_evidence_source,t.route_channel_id
  FROM request_trace_events t JOIN scope s ON s.request_id=t.request_id
  WHERE t.org_id=$1 AND t.project_id=$2 ORDER BY t.request_id,t.created_at,t.id LIMIT 4097
 ), pending_sample AS (
  SELECT e.id AS outbox_id,e.request_id AS operation_id,e.attempts,(e.last_error<>'') AS had_error,
   e.payload->>'event_type' AS event_type,e.payload->>'attempt_id' AS attempt_id,
   e.payload->>'provider_attempt_stage' AS provider_attempt_stage,e.payload->>'route_channel_id' AS selected_channel_id,
   e.payload#>>'{route_projection,source}' AS projection_source,
   e.payload->>'latency_ms' AS latency_ms,e.payload->>'occurred_at' AS occurred_at
  FROM internal_outbox_events e JOIN scope s ON s.request_id=e.request_id
  WHERE e.org_id=$1 AND e.event_type='evidence.trace.v1'
   AND e.payload->>'org_id'=$1::text AND e.payload->>'project_id'=$2::text
   AND e.payload->>'request_id'=e.request_id
  ORDER BY (e.last_error<>'') DESC,e.id LIMIT 1025
 ) SELECT jsonb_build_object(
 'observed_at',statement_timestamp(),
 'traces',COALESCE((SELECT jsonb_agg(to_jsonb(t) ORDER BY t.operation_id,t.created_at) FROM trace_sample t),'[]'::jsonb),
 'pending_trace_outbox',COALESCE((SELECT jsonb_agg(to_jsonb(p) ORDER BY p.had_error DESC,p.outbox_id) FROM pending_sample p),'[]'::jsonb),
 'truncated',(SELECT count(*)>4096 FROM trace_sample) OR (SELECT count(*)>1024 FROM pending_sample))`, org, project, expected).Scan(&wire)
	if err != nil {
		result["error_class"] = contentPerformanceErrorClass(err)
		return result
	}
	var snapshot map[string]any
	if err = json.Unmarshal(wire, &snapshot); err != nil {
		result["error_class"] = "decode_failed"
		return result
	}
	for _, key := range []string{"observed_at", "traces", "pending_trace_outbox", "truncated"} {
		result[key] = snapshot[key]
	}
	result["complete"] = snapshot["truncated"] == false
	if snapshot["truncated"] == true {
		result["error_class"] = "row_limit"
	}
	return result
}

func TestContentPerformanceTraceDiagnosticsOffline(t *testing.T) {
	for _, scenario := range []struct {
		name            string
		fail, truncated bool
	}{{"complete", false, false}, {"query_failure", true, false}, {"truncated", false, true}} {
		t.Run(scenario.name, func(t *testing.T) {
			calls := 0
			pool := &testutil.MockDB{QueryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
				calls++
				require.Contains(t, sql, "t.project_id=$2")
				require.Contains(t, sql, "e.payload->>'project_id'=$2::text")
				require.NotContains(t, sql, "SELECT *")
				require.NotContains(t, sql, "metadata")
				require.Equal(t, []any{int64(1), int64(2), []string{"op_test"}}, args)
				return &testutil.MockRow{ScanFn: func(d ...any) error {
					if scenario.fail {
						return errors.New("secret raw SQL credential")
					}
					*d[0].(*[]byte) = []byte(fmt.Sprintf(`{"traces":[{"operation_id":"op_test"}],"pending_trace_outbox":[],"truncated":%t}`, scenario.truncated))
					return nil
				}}
			}}
			result := contentPerformanceTraceDiagnostics(t.Context(), pool, 1, 2, []string{"op_test"})
			require.Equal(t, 1, calls)
			require.Equal(t, !scenario.fail && !scenario.truncated, result["complete"])
			if scenario.truncated {
				require.Equal(t, "row_limit", result["error_class"])
			}
			file := filepath.Join(t.TempDir(), "trace-before-wait.json")
			require.NoError(t, contentPerformanceWrite(file, result))
			wire, err := os.ReadFile(file)
			require.NoError(t, err)
			require.NotContains(t, string(wire), "secret")
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	pool := &testutil.MockDB{QueryRowFn: func(context.Context, string, ...any) pgx.Row {
		t.Fatal("expired/invalid scope must not query")
		return nil
	}}
	require.Equal(t, false, contentPerformanceTraceDiagnostics(ctx, pool, 1, 2, []string{"op_test"})["complete"])
	require.Equal(t, "invalid_scope", contentPerformanceTraceDiagnostics(t.Context(), pool, 1, 2, nil)["error_class"])
}

func TestContentPerformanceTraceDiagnosticsPG(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	org, _, project := seedPRD2P0OrgProjectUser(t, pool, "developer")
	for _, request := range []string{"op_trace_snapshot", "op_excluded"} {
		_, err := pool.Exec(t.Context(), `INSERT INTO request_trace_events(org_id,project_id,request_id,event_type,stage,metadata) VALUES($1,$2,$3,'routing.dispatch_authority_failed','routing','{"secret":"must-not-export"}')`, org, project, request)
		require.NoError(t, err)
		payload := fmt.Sprintf(`{"org_id":%d,"project_id":%d,"request_id":%q,"event_type":"routing.dispatch_authority_failed","provider_attempt_stage":"pre_dispatch","route_channel_id":201,"route_projection":{"source":"no_provider_attempt"},"metadata":{"secret":"must-not-export"}}`, org, project, request)
		_, err = pool.Exec(t.Context(), `INSERT INTO internal_outbox_events(event_key,event_type,priority,org_id,request_id,payload,attempts,last_error) VALUES($1,'evidence.trace.v1',1,$2,$3,$4::jsonb,1,'secret-error')`, request, org, request, payload)
		require.NoError(t, err)
	}
	for _, scope := range []struct {
		name    string
		project int64
		ids     []string
		want    int
	}{
		{"exact", project, []string{"op_trace_snapshot"}, 1},
		{"wrong_project", project + 100000, []string{"op_trace_snapshot"}, 0},
		{"wrong_operation", project, []string{"op_absent"}, 0},
	} {
		t.Run(scope.name, func(t *testing.T) {
			result := contentPerformanceTraceDiagnostics(t.Context(), pool, org, scope.project, scope.ids)
			require.Equal(t, true, result["complete"], "%v", result)
			require.Len(t, result["traces"], scope.want)
			require.Len(t, result["pending_trace_outbox"], scope.want)
			wire, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(wire), "secret")
			require.NotContains(t, string(wire), "must-not-export")
			require.NotContains(t, string(wire), "op_excluded")
		})
	}
}
