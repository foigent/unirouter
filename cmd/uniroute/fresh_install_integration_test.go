//go:build integration

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/installstate"
	"github.com/uniroute/unirouter/internal/migrate"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

// These subprocess tests execute the real installer owner and original hooks.
// Their artifact is explicitly synthetic; native backend-image acceptance is
// separately required by the runbook, not inferred from a Go test executable.
func freshTestConfig(t *testing.T, dsn string) *config.Config {
	t.Helper()
	priorVersion, priorCommit, priorTree := buildVersion, buildCommit, buildTree
	t.Cleanup(func() { buildVersion, buildCommit, buildTree = priorVersion, priorCommit, priorTree })
	buildVersion, buildCommit, buildTree = "1.4.1", strings.Repeat("a", 40), strings.Repeat("b", 40)
	return &config.Config{DB_DSN: dsn, DBMaxConnections: 1, DBMaxIdleConns: 0, MigrationsDir: testutil.MigrationsDir(t), RuntimeEnvironment: "synthetic-fresh-test", RuntimeVersion: buildVersion, RuntimeCommit: buildCommit, RuntimeTree: buildTree, RuntimeImageDigest: "sha256:" + strings.Repeat("c", 64)}
}

func TestFreshInstallOwnerSubprocess(t *testing.T) {
	if os.Getenv("UR_FRESH_OWNER_CHILD") != "1" {
		t.Skip("subprocess helper")
	}
	cfg := freshTestConfig(t, os.Getenv("UR_FRESH_CHILD_DSN"))
	wire, err := readFreshFile(os.Getenv("UR_FRESH_CHILD_PLAN"), installstate.MaximumBytes)
	require.NoError(t, err)
	p, err := installstate.DecodePlan(wire)
	require.NoError(t, err)
	a, err := freshArtifact(cfg)
	require.NoError(t, err)
	require.Equal(t, p.Artifact, a)
	poolCfg, err := pgxpool.ParseConfig(cfg.DB_DSN)
	require.NoError(t, err)
	poolCfg.MaxConns = 1
	poolCfg.MinConns = 0
	pool, err := pgxpool.NewWithConfig(t.Context(), poolCfg)
	require.NoError(t, err)
	defer pool.Close()
	stop := os.Getenv("UR_FRESH_CHILD_STOP")
	observe := func(phase, version string) {
		if phase+":"+version != stop {
			return
		}
		require.NoError(t, os.WriteFile(os.Getenv("UR_FRESH_CHILD_EVENT"), []byte(stop), 0o600))
		process, err := os.FindProcess(os.Getpid())
		require.NoError(t, err)
		require.NoError(t, process.Kill()) // Actual SIGKILL: no rollback/defer/receipt callback.
		select {}
	}
	err = migrate.WithFreshOwner(t.Context(), pool, cfg.MigrationsDir, p, true, func(owner *migrate.FreshOwner) error {
		return executeFreshInstallObserved(t.Context(), owner, cfg, p, os.Getenv("UR_FRESH_CHILD_EVIDENCE"), observe)
	})
	require.NoError(t, err)
}

func TestFreshInstallProcessRecoveryIntegration(t *testing.T) {
	for _, tool := range []string{"pg_dump", "psql"} {
		_, err := exec.LookPath(tool)
		require.NoError(t, err, "check native prerequisites before creating the test database")
	}
	pool := testutil.SetupIsolatedTestDB(t) // Existing empty-DB helper; no migration/template/seed shortcut.
	ctx := t.Context()
	cfg := freshTestConfig(t, pool.Config().ConnConfig.ConnString())
	a, err := freshArtifact(cfg)
	require.NoError(t, err)
	p, err := planFreshInstall(ctx, pool, cfg.RuntimeEnvironment, a)
	require.NoError(t, err)
	stale := p
	stale.InstallationID = "ufi_" + strings.Repeat("d", 32)
	stale.Digest = stale.CalculateDigest()
	root, err := os.MkdirTemp(os.Getenv("UR_FRESH_EVIDENCE_PARENT"), "ur-fresh-recovery-")
	require.NoError(t, err)
	t.Logf("fresh integration evidence: %s", root)
	planPath := filepath.Join(root, "outer.json")
	wire, err := json.Marshal(p)
	require.NoError(t, err)
	require.NoError(t, writeFreshFile(planPath, wire))
	evidence := filepath.Join(root, "plans")
	require.NoError(t, prepareFreshEvidence(evidence, p, false))
	before, err := installstate.Read(ctx, pool)
	require.NoError(t, err)
	require.False(t, before.HasLedger)
	_, err = checkFreshInstall(ctx, pool, p)
	require.NoError(t, err)
	after, err := installstate.Read(ctx, pool)
	require.NoError(t, err)
	require.Equal(t, before, after, "plan/check must not create even schema_migrations")
	declarations, err := publication.CompiledDeclarations()
	require.NoError(t, err)
	id := func(prefix string) string {
		for _, d := range declarations {
			if strings.HasPrefix(d.PublicationID, prefix) {
				return d.PublicationID
			}
		}
		t.Fatalf("missing compiled publication %s", prefix)
		return ""
	}
	run := func(stop string, wantSuccess bool) {
		t.Helper()
		label := strings.ReplaceAll(stop, ":", "-")
		if label == "" {
			label = "complete"
		}
		logPath := filepath.Join(root, label+".log")
		log, err := os.OpenFile(logPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		require.NoError(t, err)
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFreshInstallOwnerSubprocess$", "-test.v")
		command.Env = append(os.Environ(), "UR_FRESH_OWNER_CHILD=1", "UR_FRESH_CHILD_DSN="+cfg.DB_DSN, "UR_FRESH_CHILD_PLAN="+planPath, "UR_FRESH_CHILD_EVIDENCE="+evidence, "UR_FRESH_CHILD_STOP="+stop, "UR_FRESH_CHILD_EVENT="+filepath.Join(root, label+".event"))
		command.Stdout = log
		command.Stderr = log
		err = command.Run()
		require.NoError(t, log.Close())
		if wantSuccess {
			require.NoError(t, err, "see %s", logPath)
		} else {
			require.Error(t, err, "expected abrupt process termination; see %s", logPath)
			event, readErr := os.ReadFile(filepath.Join(root, label+".event"))
			require.NoError(t, readErr)
			require.Equal(t, stop, string(event))
		}
	}
	assertHead := func(head string) installstate.Snapshot {
		t.Helper()
		s, err := installstate.Read(ctx, pool)
		require.NoError(t, err)
		require.NotNil(t, s.Record)
		if head == "" {
			require.Empty(t, s.Ledger)
		} else {
			require.Equal(t, head, s.Ledger[len(s.Ledger)-1])
		}
		_, err = installstate.RequireOrdinary(ctx, pool)
		require.Error(t, err, "incomplete owner must refuse ordinary admission")
		return s
	}
	run("claimed:", false)
	assertHead("")
	require.Error(t, migrate.WithFreshOwner(ctx, pool, cfg.MigrationsDir, stale, true, func(*migrate.FreshOwner) error { return nil }), "stale read-only Plan cannot claim another installation")
	run("baseline_committed:"+publication.BaselineMigrationM100, false)
	assertHead(publication.BaselineMigrationM100)
	run("plan_durable:"+id("000101"), false)
	s := assertHead(publication.BaselineMigrationM100)
	require.Empty(t, s.Record.References, "durable orphan is not adopted evidence")
	run("plan_adopted:"+id("000101"), false)
	s = assertHead(publication.BaselineMigrationM100)
	require.Len(t, s.Record.References, 1)
	require.False(t, s.Record.References[0].Committed)
	run("publication_uncommitted:"+id("000108"), false)
	s = assertHead(a.Ledger[slices.Index(a.Ledger, id("000108"))-1])
	var p0 installstate.Reference
	for _, ref := range s.Record.References {
		if ref.Publication == id("000108") {
			p0 = ref
		}
	}
	require.NotEmpty(t, p0.PlanDigest)
	require.False(t, p0.Committed)
	p0Path := freshPublicationPath(evidence, p0)
	p0Bytes, err := os.ReadFile(p0Path)
	require.NoError(t, err)
	require.NoError(t, os.Rename(p0Path, p0Path+".unavailable-original"))
	err = migrate.WithFreshOwner(ctx, pool, cfg.MigrationsDir, p, true, func(owner *migrate.FreshOwner) error { return executeFreshInstall(ctx, owner, cfg, p, evidence) })
	require.Error(t, err, "missing adopted Plan must stop resume")
	require.NoError(t, os.Rename(p0Path+".unavailable-original", p0Path))
	run("publication_committed:"+id("000108"), false)
	s = assertHead(id("000108"))
	original, err := os.ReadFile(p0Path)
	require.NoError(t, err)
	require.Equal(t, p0Bytes, original)
	var successor installstate.Reference
	for _, ref := range s.Record.References {
		if ref.Supersedes == p0.PlanDigest {
			successor = ref
		}
	}
	require.True(t, successor.Committed)
	require.NotEqual(t, p0.PlanDigest, successor.PlanDigest)
	run("publication_uncommitted:"+id("000110"), false)
	assertHead(a.Ledger[slices.Index(a.Ledger, id("000110"))-1])
	run("publication_uncommitted:"+id("000113"), false)
	assertHead(a.Ledger[slices.Index(a.Ledger, id("000113"))-1])
	run("publication_committed:"+id("000113"), false)
	s = assertHead(id("000113"))
	require.Error(t, schemaauthority.VerifyRuntimeLedger(a.Ledger, s.Ledger), "M126 must not serve an incomplete pre-retirement installation")
	_, err = verifyRuntimeAuthority(ctx, pool, a.Ledger)
	require.Error(t, err, "including SKIP_MIGRATION must reject active M113")
	run("expand_committed:"+schemaauthority.ObservationExpandHead, false)
	assertHead(schemaauthority.ObservationExpandHead)
	// Terminate the ACTUAL lock-owning PostgreSQL backend. A surviving Go
	// object cannot re-acquire a connection and continue under stale proof.
	oneCfg := pool.Config().Copy()
	oneCfg.MaxConns = 1
	oneCfg.MinConns = 0
	one, err := pgxpool.NewWithConfig(ctx, oneCfg)
	require.NoError(t, err)
	err = migrate.WithFreshOwner(ctx, one, cfg.MigrationsDir, p, true, func(owner *migrate.FreshOwner) error {
		var pid int32
		if err := owner.Read(ctx, func(conn *pgxpool.Conn) error { return conn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid) }); err != nil {
			return err
		}
		var terminated bool
		require.NoError(t, pool.QueryRow(ctx, `SELECT pg_terminate_backend($1)`, pid).Scan(&terminated))
		require.True(t, terminated)
		_, err := owner.Snapshot(ctx)
		return err
	})
	require.Error(t, err)
	one.Close()
	assertHead(schemaauthority.ObservationExpandHead)
	run("expand_committed:"+schemaauthority.BudgetSnapshotExpandHead, false)
	s = assertHead(schemaauthority.BudgetSnapshotExpandHead)
	require.Equal(t, "active", s.Record.State, "M118 certifies budgets but cannot complete the current installation")
	var originalBudgetProof string
	require.NoError(t, pool.QueryRow(ctx, `SELECT to_jsonb(c)::text FROM execution_source_budget_coverage c WHERE ended_at IS NULL`).Scan(&originalBudgetProof))
	_, err = runtimegeneration.ReadBudgetCoverage(ctx, pool)
	require.NoError(t, err, "M118 original certification must be valid before the next expansion")
	run("expand_committed:"+schemaauthority.ContentCaptureExpandHead, false)
	s = assertHead(schemaauthority.ContentCaptureExpandHead)
	require.Equal(t, "active", s.Record.State, "M119 cannot complete the current installation")
	run("expand_committed:"+schemaauthority.ContentReclaimExpandHead, false)
	var retainedBudgetProof string
	require.NoError(t, pool.QueryRow(ctx, `SELECT to_jsonb(c)::text FROM execution_source_budget_coverage c WHERE ended_at IS NULL`).Scan(&retainedBudgetProof))
	require.Equal(t, originalBudgetProof, retainedBudgetProof, "M119 must not rewrite the original budget certification")
	s = assertHead(schemaauthority.ContentReclaimExpandHead)
	require.Equal(t, "active", s.Record.State, "M120 cannot complete the current installation")
	for _, head := range []string{schemaauthority.PreDispatchExpandHead, schemaauthority.ScopedContentExpandHead} {
		run("expand_committed:"+head, false)
		s = assertHead(head)
		require.Equal(t, "active", s.Record.State, "an installation below M123 must remain incomplete")
	}
	run("expand_committed:"+schemaauthority.XInsightPolicyExpandHead, false)
	s = assertHead(schemaauthority.XInsightPolicyExpandHead)
	require.Equal(t, "active", s.Record.State, "M123 cannot complete the current M124 installation")
	for _, head := range []string{schemaauthority.XInsightAdoptionExpandHead, schemaauthority.XInsightCredentialExpandHead} {
		run("expand_committed:"+head, false)
		s = assertHead(head)
		require.Equal(t, "active", s.Record.State, "pre-retirement installation remains incomplete")
	}
	run("expand_committed:"+schemaauthority.CommercialLicenseRetirementHead, false)
	s = assertHead(schemaauthority.CommercialLicenseRetirementHead)
	require.Equal(t, "active", s.Record.State, "M126 cannot complete the M127 installation")
	run("expand_committed:"+schemaauthority.XInsightCommunicationHead, false)
	s = assertHead(schemaauthority.XInsightCommunicationHead)
	require.Equal(t, "active", s.Record.State, "M127 cannot complete the M128 installation")
	run("expand_committed:"+schemaauthority.UpstreamReleaseObservationHead, false)
	s, err = installstate.RequireOrdinary(ctx, pool)
	require.NoError(t, err)
	require.Equal(t, "completed", s.Record.State)
	require.Equal(t, a.Ledger, s.Ledger)
	_, err = verifyRuntimeAuthority(ctx, pool, a.Ledger)
	require.NoError(t, err)
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM runtime_generations`).Scan(&count))
	require.Zero(t, count, "installer must not fabricate a serving generation")
	coverage, err := runtimegeneration.ReadBudgetCoverage(ctx, pool)
	require.NoError(t, err)
	run("", true)
	unchanged, err := runtimegeneration.ReadBudgetCoverage(ctx, pool)
	require.NoError(t, err)
	require.Equal(t, coverage, unchanged, "completed retry must not certify again")
	proof, _ := json.MarshalIndent(s, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(root, "completed.json"), proof, 0o600))
	verifyFreshRestore(t, pool, p, root)
}

func restoreFreshBackup(t *testing.T, source *pgxpool.Pool, root string) *pgxpool.Pool {
	t.Helper()
	for _, tool := range []string{"pg_dump", "psql"} {
		_, err := exec.LookPath(tool)
		require.NoError(t, err, "native fixture must provide PostgreSQL tools before expensive execution")
	}
	restored := testutil.SetupIsolatedTestDB(t)
	pgEnv := func(pool *pgxpool.Pool) []string {
		c := pool.Config().ConnConfig
		sslmode := "require"
		if c.TLSConfig == nil {
			sslmode = "disable"
		}
		return append(os.Environ(), "PGHOST="+c.Host, fmt.Sprintf("PGPORT=%d", c.Port), "PGUSER="+c.User, "PGPASSWORD="+c.Password, "PGDATABASE="+c.Database, "PGSSLMODE="+sslmode)
	}
	dumpPath := filepath.Join(root, "restorable.sql")
	dump := exec.CommandContext(t.Context(), "pg_dump", "--no-owner", "--no-privileges", "--file", dumpPath)
	dump.Env = pgEnv(source)
	log, err := os.OpenFile(filepath.Join(root, "restore.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	defer log.Close()
	dump.Stdout = log
	dump.Stderr = log
	require.NoError(t, dump.Run(), "see restore.log")
	restore := exec.CommandContext(t.Context(), "psql", "-X", "--set", "ON_ERROR_STOP=1", "--file", dumpPath)
	restore.Env = pgEnv(restored)
	restore.Stdout = log
	restore.Stderr = log
	require.NoError(t, restore.Run(), "see restore.log")
	rootDir := filepath.Dir(testutil.MigrationsDir(t))
	rebase := exec.CommandContext(t.Context(), "psql", "-X", "--set", "ON_ERROR_STOP=1", "--file", filepath.Join(rootDir, "scripts", "source-budget-restore-rebase.sql"))
	rebase.Env = pgEnv(restored)
	rebase.Stdout = log
	rebase.Stderr = log
	require.NoError(t, rebase.Run(), "see restore.log")
	return restored
}

func verifyFreshRestore(t *testing.T, source *pgxpool.Pool, p installstate.Plan, root string) {
	t.Helper()
	restored := restoreFreshBackup(t, source, root)
	_, err := verifyRuntimeAuthority(t.Context(), restored, p.Artifact.Ledger)
	require.NoError(t, err, "historical origin must not pin restored DB OID or name")
	s, err := checkFreshInstall(t.Context(), restored, p)
	require.NoError(t, err)
	require.Equal(t, p.Digest, s.Record.Plan.Digest)
	coverage, err := runtimegeneration.ReadBudgetCoverage(t.Context(), restored)
	require.NoError(t, err)
	require.Equal(t, "restore_rebase", coverage.Reason)
	var n int
	require.NoError(t, restored.QueryRow(t.Context(), `SELECT count(*) FROM execution_source_budget_coverage WHERE reason='initial_certification'`).Scan(&n))
	require.Equal(t, 1, n)
	// A disconnected ordinary writer cannot make a completed DB fresh again.
	conn, err := restored.Acquire(t.Context())
	require.NoError(t, err)
	_, err = installstate.RequireOrdinary(t.Context(), conn)
	require.NoError(t, err)
	require.NoError(t, conn.Conn().Close(t.Context()))
	conn.Release()
	tx, err := restored.BeginTx(t.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	require.NoError(t, err)
	_, err = installstate.FreshShape(t.Context(), tx)
	require.Error(t, err)
	require.NoError(t, tx.Rollback(t.Context()))
}

func TestFreshInstallRefusesMixedAndAlteredEmptyDatabasesIntegration(t *testing.T) {
	for _, sql := range []string{`CREATE TABLE unrelated(id int)`, `CREATE SCHEMA extra`, `CREATE FUNCTION public.unrelated() RETURNS int LANGUAGE sql AS 'SELECT 1'`, `CREATE TABLE schema_migrations(version VARCHAR(64) PRIMARY KEY,applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()); ALTER TABLE schema_migrations ADD COLUMN unrelated int`} {
		t.Run(fmt.Sprintf("case-%s", installstate.SHA([]byte(sql))[:8]), func(t *testing.T) {
			pool := testutil.SetupIsolatedTestDB(t)
			_, err := pool.Exec(t.Context(), sql)
			require.NoError(t, err)
			tx, err := pool.BeginTx(t.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
			require.NoError(t, err)
			defer tx.Rollback(t.Context())
			_, err = installstate.FreshShape(t.Context(), tx)
			require.Error(t, err)
		})
	}
}

func TestFreshInstallClaimsOnlyOriginalEmptyLedgerIntegration(t *testing.T) {
	pool := testutil.SetupIsolatedTestDB(t)
	cfg := freshTestConfig(t, pool.Config().ConnConfig.ConnString())
	_, err := pool.Exec(t.Context(), `CREATE TABLE schema_migrations(version VARCHAR(64) PRIMARY KEY,applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`)
	require.NoError(t, err)
	before := freshEmptyLedgerSnapshot(t, pool)
	planPath := filepath.Join(t.TempDir(), "original.json")
	require.NoError(t, runFreshInstallCLI(t.Context(), []string{"plan", "--out", planPath}, cfg))
	require.NoError(t, runFreshInstallCLI(t.Context(), []string{"check", "--plan", planPath}, cfg))
	require.Equal(t, before, freshEmptyLedgerSnapshot(t, pool), "formal plan/check must leave the original empty ledger unchanged")
	wire, err := readFreshFile(planPath, installstate.MaximumBytes)
	require.NoError(t, err)
	p, err := installstate.DecodePlan(wire)
	require.NoError(t, err)
	require.Equal(t, "empty_original_ledger", p.FreshShape)
	_, err = installstate.RequireOrdinary(t.Context(), pool)
	require.Error(t, err)
	err = migrate.WithFreshOwner(t.Context(), pool, cfg.MigrationsDir, p, false, func(owner *migrate.FreshOwner) error {
		s, err := owner.Snapshot(t.Context())
		if err != nil {
			return err
		}
		require.Empty(t, s.Ledger)
		require.Equal(t, "active", s.Record.State)
		return nil
	})
	require.NoError(t, err)
}

// Capture the state these admission commands could mutate, including the
// disabled policy and collation details that an empty row count cannot prove.
func freshEmptyLedgerSnapshot(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var snapshot string
	err := pool.QueryRow(t.Context(), `SELECT jsonb_build_object(
 'ledger',(SELECT COALESCE(jsonb_agg(to_jsonb(m) ORDER BY version),'[]'::jsonb) FROM public.schema_migrations m),
 'comment',obj_description('public.schema_migrations'::regclass,'pg_class'),
 'class',(SELECT to_jsonb(c) FROM pg_class c WHERE c.oid='public.schema_migrations'::regclass),
 'columns',(SELECT jsonb_agg(to_jsonb(a) ORDER BY attnum) FROM pg_attribute a WHERE attrelid='public.schema_migrations'::regclass AND attnum>0),
 'policies',(SELECT COALESCE(jsonb_agg(to_jsonb(p) ORDER BY oid),'[]'::jsonb) FROM pg_policy p WHERE polrelid='public.schema_migrations'::regclass),
 'constraints',(SELECT jsonb_agg(to_jsonb(c) ORDER BY oid) FROM pg_constraint c WHERE conrelid='public.schema_migrations'::regclass),
 'indexes',(SELECT jsonb_agg(to_jsonb(i) ORDER BY indexrelid) FROM pg_index i WHERE indrelid='public.schema_migrations'::regclass),
 'public_objects',(SELECT jsonb_agg(to_jsonb(c) ORDER BY oid) FROM pg_class c WHERE relnamespace='public'::regnamespace)
 )::text`).Scan(&snapshot)
	require.NoError(t, err)
	return snapshot
}

func TestFreshInstallPlanCheckRefuseEmptyLedgerPolicyAndCollationDriftIntegration(t *testing.T) {
	for _, tc := range []struct{ name, drift string }{
		{"disabled_policy", `CREATE POLICY retained_policy ON public.schema_migrations USING (true)`},
		{"explicit_collation", `ALTER TABLE public.schema_migrations ALTER COLUMN version TYPE VARCHAR(64) COLLATE "C"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := testutil.SetupIsolatedTestDB(t)
			cfg := freshTestConfig(t, pool.Config().ConnConfig.ConnString())
			_, err := pool.Exec(t.Context(), `CREATE TABLE public.schema_migrations(version VARCHAR(64) PRIMARY KEY,applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`)
			require.NoError(t, err)
			planPath := filepath.Join(t.TempDir(), "original.json")
			require.NoError(t, runFreshInstallCLI(t.Context(), []string{"plan", "--out", planPath}, cfg))
			original, err := readFreshFile(planPath, installstate.MaximumBytes)
			require.NoError(t, err)
			// Deliberate negative fixture only, never a positive bootstrap step.
			_, err = pool.Exec(t.Context(), tc.drift)
			require.NoError(t, err)
			var enabled, forced bool
			require.NoError(t, pool.QueryRow(t.Context(), `SELECT relrowsecurity,relforcerowsecurity FROM pg_class WHERE oid='public.schema_migrations'::regclass`).Scan(&enabled, &forced))
			require.False(t, enabled)
			require.False(t, forced)
			before := freshEmptyLedgerSnapshot(t, pool)
			rejectedPath := filepath.Join(t.TempDir(), "rejected.json")
			err = runFreshInstallCLI(t.Context(), []string{"plan", "--out", rejectedPath}, cfg)
			require.ErrorContains(t, err, "not the original migration-owner table")
			_, err = os.Stat(rejectedPath)
			require.True(t, os.IsNotExist(err), "rejected plan must not create output")
			require.Equal(t, before, freshEmptyLedgerSnapshot(t, pool))
			err = runFreshInstallCLI(t.Context(), []string{"check", "--plan", planPath}, cfg)
			require.ErrorContains(t, err, "not the original migration-owner table")
			require.Equal(t, before, freshEmptyLedgerSnapshot(t, pool))
			after, err := readFreshFile(planPath, installstate.MaximumBytes)
			require.NoError(t, err)
			require.Equal(t, original, after, "check must preserve the original Plan")
		})
	}
}

func TestFreshInstallNativeBinaryIntegration(t *testing.T) {
	binary := os.Getenv("UR_FRESH_TEST_BINARY")
	image := os.Getenv("UR_FRESH_TEST_IMAGE_DIGEST")
	if binary == "" || image == "" {
		if os.Getenv("REQUIRE_FRESH_NATIVE") == "1" {
			t.Fatal("native fixture must supply its exact backend executable and inspected image digest")
		}
		t.Skip("native image executable was not supplied")
	}
	pool := testutil.SetupIsolatedTestDB(t)
	ctx := t.Context()
	root, err := os.MkdirTemp(os.Getenv("UR_FRESH_EVIDENCE_PARENT"), "ur-fresh-native-")
	require.NoError(t, err)
	t.Logf("native backend evidence: %s", root)
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "DB_DSN=" + pool.Config().ConnConfig.ConnString(), "DB_MAX_CONNECTIONS=1", "DB_MAX_IDLE_CONNECTIONS=0", "JWT_SECRET=" + strings.Repeat("s", 32), "API_KEY_HASH_SECRET=" + strings.Repeat("s", 32), "ENCRYPTION_KEY=0123456789abcdef0123456789abcdef", "MODEL_ROUTE_AUTHORITY=legacy", "MODEL_ROUTE_COMPATIBILITY_ENABLED=true", "RUNTIME_ENVIRONMENT=development", "RUNTIME_IMAGE_DIGEST=" + image, "MIGRATIONS_DIR=" + testutil.MigrationsDir(t)}
	run := func(label string, success bool, args ...string) {
		t.Helper()
		log, err := os.OpenFile(filepath.Join(root, label+".log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		require.NoError(t, err)
		command := exec.CommandContext(ctx, binary, args...)
		command.Env = env
		command.Stdout = log
		command.Stderr = log
		err = command.Run()
		require.NoError(t, log.Close())
		if success {
			require.NoError(t, err, "see %s/%s.log", root, label)
		} else {
			require.Error(t, err, "unexpected admission; see %s/%s.log", root, label)
			wire, readErr := os.ReadFile(filepath.Join(root, label+".log"))
			require.NoError(t, readErr)
			require.Contains(t, string(wire), "empty database requires the explicit fresh-install owner", "an unrelated configuration error is not admission evidence")
		}
	}
	planPath := filepath.Join(root, "outer.json")
	run("empty-startup-refused", false)
	run("empty-baseline-refused", false, "capability-contract-publication", "install-baseline")
	run("plan", true, "fresh-install", "plan", "--out", planPath)
	run("check", true, "fresh-install", "check", "--plan", planPath)
	s, err := installstate.Read(ctx, pool)
	require.NoError(t, err)
	require.False(t, s.HasLedger)
	run("apply", true, "fresh-install", "apply", "--plan", planPath, "--evidence-dir", filepath.Join(root, "plans"))
	run("completed-check", true, "fresh-install", "check", "--plan", planPath)
	ledger, err := schemaauthority.ExpectedLedger(testutil.MigrationsDir(t))
	require.NoError(t, err)
	_, err = verifyRuntimeAuthority(ctx, pool, ledger)
	require.NoError(t, err)
	s, err = installstate.RequireOrdinary(ctx, pool)
	require.NoError(t, err)
	require.Equal(t, image, s.Record.Plan.Artifact.ImageDigest)
	file, err := os.ReadFile(binary)
	require.NoError(t, err)
	require.Equal(t, "sha256:"+installstate.SHA(file), s.Record.Plan.Artifact.ExecutableDigest)
	// The native command consumes all exact embedded declarations and migration
	// bytes. First-customer login, canonical activation and actual restart remain
	// separate required runtime cases in the original F fixture.
}
