//go:build integration

package testdb

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestDropDiagnosticsDistinguishAcquireAndExecuteIntegration(t *testing.T) {
	adminDSN, targetDSN, name, err := TempDatabaseDSNs(DSN())
	require.NoError(t, err)
	cfg, err := pgxpool.ParseConfig(adminDSN)
	require.NoError(t, err)
	cfg.MaxConns = 1
	admin, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	defer admin.Close()
	if err := admin.Ping(t.Context()); err != nil {
		Unavailable(t, "synthetic PostgreSQL is unavailable")
	}

	held, err := admin.Acquire(t.Context())
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	err = Drop(ctx, admin, name)
	cancel()
	held.Release()
	var diagnostic *dropError
	require.ErrorAs(t, err, &diagnostic)
	require.Equal(t, "acquire", diagnostic.phase)
	require.True(t, errors.Is(err, context.DeadlineExceeded))

	_, err = admin.Exec(t.Context(), `CREATE DATABASE `+QuoteIdent(name))
	require.NoError(t, err)
	target, err := pgxpool.New(t.Context(), targetDSN)
	require.NoError(t, err)
	defer func() {
		target.Close()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		require.NoError(t, Drop(cleanupCtx, admin, name))
	}()
	require.NoError(t, target.Ping(t.Context()))
	ctx, cancel = context.WithTimeout(t.Context(), 100*time.Millisecond)
	err = Drop(ctx, admin, name)
	cancel()
	require.ErrorAs(t, err, &diagnostic)
	require.Equal(t, "execute", diagnostic.phase)
	require.True(t, errors.Is(err, context.DeadlineExceeded))
}
