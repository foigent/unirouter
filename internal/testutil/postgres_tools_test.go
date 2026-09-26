package testutil

import (
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestPostgresToolPasswordStaysOutOfArguments(t *testing.T) {
	for _, dsn := range []string{
		"postgres://tester:synthetic%21secret@127.0.0.1:56132/example?sslmode=verify-full&application_name=recovery-fixture",
		"postgres://tester@127.0.0.1:56132/example?password=synthetic%21secret&sslmode=verify-full&application_name=recovery-fixture",
	} {
		config, err := pgxpool.ParseConfig(dsn)
		require.NoError(t, err)
		command := PostgresToolCommand(t, t.Context(), "pg_dump", config, "--format=custom")
		require.NotContains(t, strings.Join(command.Args, " "), "synthetic!secret")
		require.NotContains(t, strings.Join(command.Args, " "), "synthetic%21secret")
		u, err := url.Parse(command.Args[2])
		require.NoError(t, err)
		_, hasPassword := u.User.Password()
		require.False(t, hasPassword)
		require.Empty(t, u.Query().Get("password"))
		require.Equal(t, "verify-full", u.Query().Get("sslmode"))
		require.Equal(t, "recovery-fixture", u.Query().Get("application_name"))
		require.Contains(t, command.Env, "PGPASSWORD=synthetic!secret")
	}
}
