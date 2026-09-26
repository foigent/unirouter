package testutil

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresToolCommand preserves the test connection's URL options while keeping
// its password out of process arguments and command-error diagnostics.
func PostgresToolCommand(t *testing.T, ctx context.Context, program string, config *pgxpool.Config, args ...string) *exec.Cmd {
	t.Helper()
	u, err := url.Parse(config.ConnString())
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		t.Fatal("PostgreSQL recovery tests require a PostgreSQL URL")
	}
	u.User = url.User(config.ConnConfig.User)
	query := u.Query()
	query.Del("password")
	u.RawQuery = query.Encode()
	command := exec.CommandContext(ctx, program, append([]string{"--dbname", u.String()}, args...)...)
	command.Env = append(os.Environ(), "PGPASSWORD="+config.ConnConfig.Password)
	return command
}
