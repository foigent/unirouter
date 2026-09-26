//go:build integration

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/limitpolicy"
	"github.com/uniroute/unirouter/internal/mediajob"
	"github.com/uniroute/unirouter/internal/modelrouting"
	"github.com/uniroute/unirouter/internal/sourcefacts"
	"github.com/uniroute/unirouter/internal/testutil"
)

// Run the real main binary: merely calling Worker.Run in a fixture cannot
// prove that the production assembly waits for source readiness.
func TestSourceMediaMainStartupBoundaries(t *testing.T) {
	root, err := filepath.Abs("../..")
	require.NoError(t, err)
	binary := filepath.Join(t.TempDir(), "uniroute-source-startup")
	build := exec.CommandContext(t.Context(), "go", "build", "-race", "-o", binary, ".")
	build.Env = os.Environ()
	output, err := build.CombinedOutput()
	require.NoError(t, err, string(output))
	binaryFile, err := os.Open(binary)
	require.NoError(t, err)
	binaryHash := sha256.New()
	_, err = io.Copy(binaryHash, binaryFile)
	require.NoError(t, err)
	require.NoError(t, binaryFile.Close())
	for _, mode := range []string{"blocked", "mismatch", "media_off", "sources_off", "a_only", "ab_on", "b_without_a", "dispatch_off", "dispatch_mismatch"} {
		t.Run(mode, func(t *testing.T) {
			dispatch := strings.HasPrefix(mode, "dispatch_")
			mediaEnabled := mode != "media_off" && !dispatch
			aEnabled := mode != "sources_off" && mode != "b_without_a" && !dispatch
			bEnabled := mode != "sources_off" && mode != "a_only" && !dispatch
			refused := mode == "blocked" || mode == "mismatch" || mode == "b_without_a" || mode == "dispatch_mismatch"
			pool := testutil.SetupMigratedTestDB(t)
			ctx := t.Context()
			var org, project, key int64
			require.NoError(t, pool.QueryRow(ctx, `INSERT INTO orgs(name,status,org_type) VALUES('synthetic-startup','active','standard') RETURNING id`).Scan(&org))
			require.NoError(t, pool.QueryRow(ctx, `INSERT INTO projects(org_id,name,slug,status,budget_period) VALUES($1,'Synthetic startup','startup','active','none') RETURNING id`, org).Scan(&project))
			require.NoError(t, pool.QueryRow(ctx, `INSERT INTO api_keys(org_id,project_id,name,key_hash,key_prefix,status,allowed_models) VALUES($1,$2,'Synthetic startup',repeat('e',64),'ur-startup','active','gpt-image-1') RETURNING id`, org, project).Scan(&key))
			store, err := mediajob.NewPostgresStore(pool, mediajob.ExecutionModelAuthorizerFunc(func(context.Context, db.DBQuerier, mediajob.ExecutionAuthorization) error { return nil }))
			require.NoError(t, err)
			source, err := sourcefacts.New(pool, sourcefacts.Options{Enabled: true, ObservationsEnabled: dispatch, SourceInstance: "synthetic-startup", CursorSecret: strings.Repeat("s", 32)})
			require.NoError(t, err)
			store.SetSourceMaterializer(source)
			now := time.Now().UTC().Truncate(time.Microsecond)
			jobs := []mediajob.Job{}
			for _, suffix := range []string{"a", "b", "c"} {
				job, _, err := store.CreateOrGet(ctx, mediajob.CreateRecord{AllowCreate: true, ExpectedLimitSubject: limitpolicy.Subject{OrgID: org, ProjectID: project, APIKeyID: key, Model: "gpt-image-1", BaseRPSLimit: 10, BaseRPMLimit: 60}, Job: mediajob.Job{ContractVersion: mediajob.ContractVersion, JobID: "mediajob_" + strings.Repeat(suffix, 32), Scope: mediajob.Scope{OrgID: org, ProjectID: project}, APIKeyID: key, RequestID: "synthetic-startup-" + suffix, Operation: mediajob.OperationTextToImage, ProfileID: endpointprotocol.ProfileOpenAIImageGPT, RequestedModel: "gpt-image-1", RoutingModel: "gpt-image-1", RequestSHA256: "sha256:" + strings.Repeat(suffix, 64), IdempotencyHash: "hmac-sha256:" + strings.Repeat(suffix, 64), RequestCiphertext: "synthetic-never-dispatched", Lifecycle: mediajob.Lifecycle{Admission: mediajob.AdmissionAccepted, Queue: mediajob.QueueQueued, Lease: mediajob.LeaseUnleased, Execution: mediajob.ExecutionPending, Outcome: mediajob.OutcomePending, Cancellation: mediajob.CancellationNotRequested, Object: mediajob.ObjectPending, Settlement: mediajob.SettlementNotEligible}, MaxAttempts: 3, NextAttemptAt: now.Add(-time.Hour), CreatedAt: now.Add(-8 * 24 * time.Hour), UpdatedAt: now.Add(-8 * 24 * time.Hour)}})
				require.NoError(t, err)
				jobs = append(jobs, job)
			}
			// These exact fixture rows represent the two independent immediate loops.
			_, err = pool.Exec(ctx, `UPDATE async_media_jobs SET queue_state='leased',lease_state='leased',execution_state='running',attempt=1,lease_owner='synthetic-expired-owner',lease_fence=1,lease_expires_at=NOW()-INTERVAL '1 minute',started_at=NOW()-INTERVAL '2 minutes' WHERE job_id=$1`, jobs[1].JobID)
			require.NoError(t, err)
			_, err = pool.Exec(ctx, `UPDATE async_media_jobs SET queue_state='terminal',lease_state='released',execution_state='terminal',provider_outcome='succeeded',mapped_model='gpt-image-1',request_ciphertext=NULL,object_state='persisted',settlement_state='charge_fact_recorded',attempt=1,started_at=created_at,terminal_at=created_at,object_key='synthetic/object',receipt_key='synthetic/receipt',output_sha256='sha256:'||repeat('f',64),output_media_type='image/png',output_size_bytes=8,output_persisted_at=created_at,output_expires_at=created_at+INTERVAL '7 days' WHERE job_id=$1`, jobs[2].JobID)
			require.NoError(t, err)
			if dispatch {
				b := billing.NewBilling(pool, nil).WithMode(billing.BillingModeCostObservation)
				frozen, err := b.Freeze(ctx, billing.FreezeRequest{RequestID: "synthetic-dispatch-startup", OrgID: org, ProjectID: project, APIKeyID: key, Model: "synthetic-unpriced", EstimatedTokens: 30, ReservedCompletionTokens: 10, SafetyFactor: "1"})
				require.NoError(t, err)
				require.True(t, frozen.Success)
				tx, err := pool.Begin(ctx)
				require.NoError(t, err)
				defer tx.Rollback(ctx)
				err = source.MaterializeDispatchTx(ctx, tx, modelrouting.Plan{PublicModel: "synthetic-unpriced", ModelOperationID: "chat.completions", RequiredCapabilities: []string{}}, modelrouting.Route{Provider: "synthetic", BaseURL: "https://frozen.synthetic.invalid/v1"}, billing.ProviderAttemptStart{RequestID: "synthetic-dispatch-startup", AttemptID: "synthetic-startup-attempt", OrgID: org, APIKeyID: key, RouteSequence: 1, RouteTotal: 1, Endpoint: "/v1/chat/completions"}, "sha256:"+strings.Repeat("f", 64), now)
				require.NoError(t, err)
				require.NoError(t, tx.Commit(ctx))
			}
			snapshot := func() string {
				var value []byte
				require.NoError(t, pool.QueryRow(ctx, `SELECT jsonb_build_object('jobs',(SELECT jsonb_agg(to_jsonb(j) ORDER BY job_id) FROM async_media_jobs j),'source_count',(SELECT count(*) FROM execution_source_facts),'journal_count',(SELECT count(*) FROM usage_finalization_journal),'charge_count',(SELECT count(*) FROM async_media_charge_facts),'outbox',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM internal_outbox_events e))`).Scan(&value))
				require.True(t, json.Valid(value))
				return string(value)
			}
			before := snapshot()
			var objectRequests atomic.Int64
			objects := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { objectRequests.Add(1); w.WriteHeader(204) }))
			defer objects.Close()
			sourceInstance := "synthetic-startup"
			if mode == "mismatch" || mode == "dispatch_mismatch" {
				sourceInstance = "synthetic-other-source"
			}
			blocker, err := pool.Begin(ctx)
			require.NoError(t, err)
			defer blocker.Rollback(context.Background())
			if mode == "blocked" {
				_, err = blocker.Exec(ctx, `LOCK TABLE execution_source_streams IN ACCESS EXCLUSIVE MODE`)
				require.NoError(t, err)
			}
			logfile, err := os.Create(filepath.Join(t.TempDir(), "main.log"))
			require.NoError(t, err)
			defer logfile.Close()
			command := exec.Command(binary)
			command.Dir = root
			command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "DB_DSN=" + pool.Config().ConnConfig.ConnString(), "REDIS_ADDR=" + os.Getenv("REDIS_ADDR"), "REDIS_PASSWORD=" + os.Getenv("REDIS_PASSWORD"), "JWT_SECRET=" + strings.Repeat("s", 32), "API_KEY_HASH_SECRET=" + strings.Repeat("s", 32), "ENCRYPTION_KEY=0123456789abcdef0123456789abcdef", "MODEL_ROUTE_AUTHORITY=legacy", "MODEL_ROUTE_COMPATIBILITY_ENABLED=true", "RUNTIME_ENVIRONMENT=development", "SKIP_MIGRATION=true", "MIGRATIONS_DIR=" + filepath.Join(root, "migrations"), "SOURCE_FACTS_ENABLED=true", "SOURCE_OBSERVATIONS_ENABLED=true", "COMPLETED_EXECUTION_FEED_SOURCE_INSTANCE=" + sourceInstance, "ASYNC_MEDIA_ENABLED=true", "ASYNC_MEDIA_ADMISSION_ENABLED=false", "ASYNC_MEDIA_S3_BUCKET=synthetic-source-startup", "ASYNC_MEDIA_S3_ENDPOINT=" + objects.URL, "ASYNC_MEDIA_S3_FORCE_PATH_STYLE=true", "AWS_ACCESS_KEY_ID=synthetic-only", "AWS_SECRET_ACCESS_KEY=synthetic-only", "AWS_EC2_METADATA_DISABLED=true", "LISTEN_PORT=18597"}
			for index, entry := range command.Env {
				for name, enabled := range map[string]bool{"ASYNC_MEDIA_ENABLED": mediaEnabled, "SOURCE_FACTS_ENABLED": aEnabled, "SOURCE_OBSERVATIONS_ENABLED": bEnabled} {
					if strings.HasPrefix(entry, name+"=") {
						command.Env[index] = name + "=" + strconv.FormatBool(enabled)
					}
				}
				if strings.HasPrefix(entry, "LISTEN_PORT=") {
					command.Env[index] = "LISTEN_PORT=0"
				}
			}
			// Synthetic local runtime identity is bound to the exact race binary.
			// This is not a Docker image or a released artifact identity.
			command.Env = append(command.Env, "RUNTIME_IMAGE_DIGEST=sha256:"+hex.EncodeToString(binaryHash.Sum(nil)))
			command.Stdout = logfile
			command.Stderr = logfile
			require.NoError(t, command.Start())
			done := make(chan error, 1)
			reaped := false
			go func() { done <- command.Wait() }()
			t.Cleanup(func() {
				if !reaped {
					_ = command.Process.Kill()
					<-done
				}
			})
			if mode == "blocked" {
				require.Eventually(t, func() bool {
					var blocked bool
					err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query LIKE '%SELECT EXISTS(SELECT 1 FROM execution_source_streams%')`).Scan(&blocked)
					return err == nil && blocked
				}, 10*time.Second, 25*time.Millisecond)
				require.JSONEq(t, before, snapshot())
				require.Zero(t, objectRequests.Load())
			}
			readLog := func() string {
				wire, err := os.ReadFile(logfile.Name())
				require.NoError(t, err)
				return string(wire)
			}
			if !refused {
				deadline := time.NewTimer(15 * time.Second)
				defer deadline.Stop()
				tick := time.NewTicker(25 * time.Millisecond)
				defer tick.Stop()
				ready := false
				for !ready {
					select {
					case err := <-done:
						reaped = true
						t.Fatalf("main exited before ready: %v\n%s", err, readLog())
					case <-deadline.C:
						t.Fatalf("main did not become ready\n%s", readLog())
					case <-tick.C:
						ready = strings.Contains(readLog(), "UniRoute server starting on")
					}
				}
				if mediaEnabled {
					require.Eventually(t, func() bool {
						var terminal bool
						err := pool.QueryRow(ctx, `SELECT execution_state='terminal' FROM async_media_jobs WHERE job_id=$1`, jobs[1].JobID).Scan(&terminal)
						return err == nil && terminal && objectRequests.Load() > 0
					}, 5*time.Second, 25*time.Millisecond, "original job and purge loops must both run after readiness")
				} else {
					var jobsBefore, jobsAfter map[string]json.RawMessage
					require.NoError(t, json.Unmarshal([]byte(before), &jobsBefore))
					require.NoError(t, json.Unmarshal([]byte(snapshot()), &jobsAfter))
					require.JSONEq(t, string(jobsBefore["jobs"]), string(jobsAfter["jobs"]))
					require.Zero(t, objectRequests.Load())
				}
				if dispatch {
					require.Eventually(t, func() bool {
						var records, pending int
						err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM execution_source_facts WHERE request_id='synthetic-dispatch-startup'),(SELECT count(*) FROM internal_outbox_events WHERE event_type='evidence.source_dispatch.v1')`).Scan(&records, &pending)
						return err == nil && records == 3 && pending == 0
					}, 5*time.Second, 25*time.Millisecond, "actual main must recover and acknowledge frozen dispatch with A/B disabled")
				}
				require.NoError(t, command.Process.Signal(syscall.SIGTERM))
			}
			select {
			case err := <-done:
				reaped = true
				if refused {
					require.Error(t, err)
				} else {
					require.NoError(t, err, readLog())
				}
			case <-time.After(15 * time.Second):
				_ = command.Process.Kill()
				<-done
				reaped = true
				t.Fatal("main did not stop after readiness refusal or graceful shutdown")
			}
			bytes, err := os.ReadFile(logfile.Name())
			require.NoError(t, err)
			if refused {
				if mode == "b_without_a" {
					require.Contains(t, string(bytes), "SOURCE_OBSERVATIONS_ENABLED requires SOURCE_FACTS_ENABLED", string(bytes))
				} else if mode == "dispatch_mismatch" {
					require.Contains(t, string(bytes), "Source dispatch recovery preflight refused", string(bytes))
				} else {
					require.Contains(t, string(bytes), "Source facts preflight refused", string(bytes))
				}
				require.NotContains(t, string(bytes), "Async-media Image V1 enabled")
				require.JSONEq(t, before, snapshot())
				require.Zero(t, objectRequests.Load(), "no object read/write/delete before source readiness")
			} else {
				require.Equal(t, map[bool]int{true: 1, false: 0}[mediaEnabled], strings.Count(string(bytes), "Async-media Image V1 enabled:"))
				require.Contains(t, string(bytes), "Shutting down server...")
				require.NotContains(t, string(bytes), "Async-media worker did not stop")
			}
		})
	}
}
