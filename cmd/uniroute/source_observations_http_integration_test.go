//go:build integration

package main

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/sourcefacts"
)

func sourceHTTPIsolateRedisIdentities(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	// Separate disposable PG databases share test Redis. Original workload
	// admission buckets use org ID, while legacy rate buckets use API-key ID.
	// Distinct synthetic IDs retain both policies without flushing shared state
	// or releasing a legitimate pending-delivery lease merely to satisfy a test.
	// The original Freeze ledger retains an int4 API-key field, so stay within
	// that supported range even though the current api_keys PK is bigint.
	start := 100_000_000 + time.Now().UnixMilli()%1_000_000_000
	_, err := pool.Exec(t.Context(), `SELECT setval(pg_get_serial_sequence(name,'id'),$1,false) FROM unnest($2::text[]) AS name`, start, []string{"orgs", "api_keys"})
	require.NoError(t, err)
}

// Checks B against actual protocol output and original dispatch count. The A
// fixture separately retains its original identity, finance and replay checks.
func sourceHTTPAssertObservations(t *testing.T, pool *pgxpool.Pool, request, operation string, attempts int) {
	t.Helper()
	rows, err := pool.Query(t.Context(), `SELECT document FROM execution_source_facts WHERE request_id=$1 AND profile=ANY($2) ORDER BY profile,stage`, request, sourcefacts.ObservationProfiles())
	require.NoError(t, err)
	defer rows.Close()
	kinds := map[string]int{}
	measurements := map[string]sourcefacts.Measurement{}
	for rows.Next() {
		var wire []byte
		require.NoError(t, rows.Scan(&wire))
		require.NotContains(t, string(wire), "source-private-prompt")
		require.NotContains(t, string(wire), "synthetic answer")
		d, err := sourcefacts.DecodeObservation(wire)
		require.NoError(t, err)
		_, err = sourcefacts.ObservationEvent(d, wire)
		require.NoError(t, err)
		require.NotEmpty(t, d.ContextRef)
		kinds[d.Kind]++
		if d.Kind == "request_context_done" {
			require.Equal(t, "chat_stream_cancel", operation)
			require.Equal(t, "context_cancelled", d.Runtime.ContextDoneReason)
			require.Equal(t, "unknown", d.Runtime.Provider)
			require.Equal(t, "unknown", d.Runtime.LocalDelivery)
		}
		if d.Measurement != nil {
			measurements[d.Measurement.Kind] = *d.Measurement
			if d.Measurement.State == "observed" {
				require.NotNil(t, d.Measurement.Value)
				n, err := strconv.ParseInt(*d.Measurement.Value, 10, 64)
				require.NoError(t, err)
				require.GreaterOrEqual(t, n, int64(0))
			} else {
				require.Nil(t, d.Measurement.Value)
			}
		}
	}
	require.NoError(t, rows.Err())
	if operation == "chat_stream_cancel" {
		require.Equal(t, 1, kinds["request_context_done"])
	}
	for _, kind := range []string{"request_started", "request_terminal", "response_measurement", "first_output_measurement", "stream_measurement"} {
		require.Equal(t, 1, kinds[kind], kind)
	}
	for _, kind := range []string{"attempt_registered", "selection", "dispatch_registered", "round_trip_measurement", "dispatch_result"} {
		require.Equal(t, attempts, kinds[kind], kind)
	}
	if attempts > 0 {
		require.Equal(t, 1, kinds["financial_result"], "only the final attempt binds the original financial terminal")
		require.Equal(t, "observed", measurements["attempt_http_round_trip"].State)
	} else {
		require.Zero(t, kinds["financial_result"])
	}
	if attempts == 2 {
		require.Equal(t, 1, kinds["provider_rejected"])
	}
	if strings.HasPrefix(operation, "chat_stream") {
		require.Equal(t, "not_applicable", measurements["request_response_complete"].State)
		require.Equal(t, "observed", measurements["stream_first_valid_output"].State)
		state := "not_observed"
		if operation == "chat_stream" || operation == "chat_stream_tools" {
			state = "observed"
		}
		require.Equal(t, state, measurements["stream_complete"].State)
	} else {
		require.Equal(t, "observed", measurements["request_response_complete"].State)
		require.Equal(t, "not_applicable", measurements["stream_first_valid_output"].State)
		require.Equal(t, "not_applicable", measurements["stream_complete"].State)
	}
}
