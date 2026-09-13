package testdb

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestDropErrorRetainsCauseWithoutSensitiveDetails(t *testing.T) {
	for _, tc := range []struct {
		cause        error
		class, state string
	}{
		{context.DeadlineExceeded, "deadline", "none"},
		{context.Canceled, "canceled", "none"},
		{&pgconn.PgError{Code: "55P03", Message: "synthetic-sensitive-value", Detail: "synthetic-sensitive-detail"}, "postgres", "55P03"},
		{errors.New("synthetic-sensitive-value"), "other", "none"},
	} {
		err := newDropError("execute", time.Now(), tc.cause)
		if !errors.Is(err, tc.cause) {
			t.Fatal("original error chain lost")
		}
		if strings.Contains(err.Error(), "synthetic-sensitive") {
			t.Fatal("sensitive error details leaked")
		}
		for _, want := range []string{"phase=execute", "class=" + tc.class, "sqlstate=" + tc.state} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("missing safe diagnostic %q", want)
			}
		}
	}
}
