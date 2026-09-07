package runtimegeneration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestIngressRecoveryNeverReplaysUncertainOrRejectedResults(t *testing.T) {
	for _, failure := range []error{io.EOF, context.Canceled, context.DeadlineExceeded,
		&pgconn.PgError{Code: "23505"}, &pgconn.PgError{Code: "40P01"},
		fmt.Errorf("ingress commit ambiguous: %w", io.ErrUnexpectedEOF)} {
		calls := 0
		_, err := recoverIngressSerialization(t.Context(), func(context.Context) (string, error) {
			calls++
			return "", failure
		})
		require.ErrorIs(t, err, failure)
		require.Equal(t, 1, calls)
	}
}

func TestIngressRecoveryRespectsCallerDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()
	_, err := recoverIngressSerialization(ctx, func(context.Context) (string, error) {
		return "", &pgconn.PgError{Code: "40001"}
	})
	require.True(t, errors.Is(err, context.DeadlineExceeded))
}
