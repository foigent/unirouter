//go:build integration

package sourcefacts

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/executioncontext"
	"github.com/uniroute/unirouter/internal/migrate"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestObservationExpansionConstraintIdentity(t *testing.T) {
	pool, _, _, _ := setupSourceDB(t)
	rows, err := pool.Query(t.Context(), `SELECT conrelid::regclass::text, conname FROM pg_constraint WHERE conrelid IN ('execution_source_streams'::regclass,'execution_source_connections'::regclass,'execution_source_credentials'::regclass) AND contype='c' AND pg_get_constraintdef(oid) LIKE '%profile%' ORDER BY 1,2`)
	require.NoError(t, err)
	defer rows.Close()
	var found []string
	for rows.Next() {
		var table, constraint string
		require.NoError(t, rows.Scan(&table, &constraint))
		found = append(found, table+":"+constraint)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{"execution_source_connections:execution_source_connections_profiles_check", "execution_source_credentials:execution_source_credentials_profiles_check", "execution_source_streams:execution_source_streams_profile_check"}, found)
}

func observationDown(t *testing.T, pool *pgxpool.Pool) error {
	t.Helper()
	wire, err := os.ReadFile(filepath.Join(testutil.MigrationsDir(t), schemaauthority.ObservationExpandHead+".down.sql"))
	require.NoError(t, err)
	conn, err := pool.Acquire(t.Context())
	require.NoError(t, err)
	defer conn.Release()
	_, err = conn.Exec(t.Context(), string(wire))
	if err != nil {
		_, _ = conn.Exec(context.Background(), "ROLLBACK")
	}
	return err
}

func TestObservationExpandPreservesAAndRefusesDataBearingDown(t *testing.T) {
	pool := testutil.SetupPreRetirementTestDB(t)
	require.NoError(t, budgetDown(t, pool), "establish the real M117 predecessor before any fixture project exists")
	pool, s, org, project := setupSourceDBWithPool(t, pool)
	snapshot := testSnapshot()
	snapshot.Core.OrgID = executioncontext.ID(org)
	snapshot.Core.ProjectID = executioncontext.ID(project)
	require.NoError(t, s.CaptureAuthenticated(t.Context(), snapshot))
	readA := func() string {
		var result string
		require.NoError(t, pool.QueryRow(t.Context(), `SELECT jsonb_agg(to_jsonb(f) ORDER BY profile,sequence)::text FROM execution_source_facts f WHERE profile=ANY($1)`, Profiles()).Scan(&result))
		return result
	}
	originalA := readA()
	require.NoError(t, observationDown(t, pool))
	require.Equal(t, originalA, readA())
	require.NoError(t, s.CheckReady(t.Context()))
	s.options.ObservationsEnabled = true
	require.ErrorContains(t, s.CheckReady(t.Context()), "M117")
	dir := testutil.MigrationsDir(t)
	artifact, err := testutil.PreRetirementLedger(t)
	require.NoError(t, err)
	predecessor := assertSourceFixtureLedger(t, pool, "000116_execution_source_facts")
	require.NoError(t, schemaauthority.VerifyRuntimeLedger(artifact, predecessor))
	require.Error(t, schemaauthority.VerifyRuntimeLedger(predecessor, append(append([]string(nil), predecessor...), schemaauthority.ObservationExpandHead)))
	require.NoError(t, migrate.RunRuntime(t.Context(), pool, testutil.PreRetirementMigrationsDir(t)))
	require.ErrorContains(t, s.CheckReady(t.Context()), "M117")
	wire, err := os.ReadFile(filepath.Join(dir, schemaauthority.ObservationExpandHead+".up.sql"))
	require.NoError(t, err)
	hooks := migrate.CompatibleExpandHooks{Version: schemaauthority.ObservationExpandHead, ExpectedSHA256: fmt.Sprintf("sha256:%x", sha256.Sum256(wire)), Before: func(context.Context, *pgxpool.Conn) error { return nil }, After: func(context.Context, pgx.Tx) error { return errors.New("injected expansion rollback") }}
	require.ErrorContains(t, migrate.RunCompatibleExpand(t.Context(), pool, dir, hooks), "injected expansion rollback")
	require.Equal(t, originalA, readA())
	require.ErrorContains(t, s.CheckReady(t.Context()), "M117")
	hooks.After = func(context.Context, pgx.Tx) error { return nil }
	require.NoError(t, migrate.RunCompatibleExpand(t.Context(), pool, dir, hooks))
	require.NoError(t, migrate.RunCompatibleExpand(t.Context(), pool, dir, hooks))
	require.NoError(t, s.CheckReady(t.Context()))
	require.Equal(t, originalA, readA())
	snapshot.Core.RequestID = "new-b-authenticated"
	require.NoError(t, s.CaptureAuthenticated(t.Context(), snapshot))
	before := readA()
	require.ErrorContains(t, observationDown(t, pool), "unsafe down refused")
	require.Equal(t, before, readA())
	require.NoError(t, s.CheckReady(t.Context()))
	var count int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM execution_source_facts WHERE profile=$1`, RuntimeProfile).Scan(&count))
	require.Equal(t, 1, count)
}
