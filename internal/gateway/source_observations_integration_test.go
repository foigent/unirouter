//go:build integration

package gateway_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/sourcefacts"
)

func TestSourceObservationsOriginalPublicDispatchRollbackAndCaptureIntegration(t *testing.T) {
	for _, operation := range []modeloperation.ID{modeloperation.IDResponses, modeloperation.IDMessages} {
		t.Run(string(operation), func(t *testing.T) {
			f := gateway.NewSourceFactsGatewayFixture(t, operation)
			source, journal, raw := sourceGatewayAttachProfiles(t, f, true)
			handler := sourceGatewayHandler(t, f, f.Relay, source)
			invoke := func() *httptest.ResponseRecorder {
				r := httptest.NewRequest(http.MethodPost, f.Path, strings.NewReader(string(f.Body)))
				if operation == modeloperation.IDMessages {
					r.Header.Set("x-api-key", raw)
					r.Header.Set("anthropic-version", "2023-06-01")
				} else {
					r.Header.Set("Authorization", "Bearer "+raw)
				}
				r.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				return w
			}
			// Required durability now lives in the original attempt transaction's
			// frozen outbox insert; publication itself is recovered by that worker.
			_, err := f.Pool.Exec(t.Context(), `CREATE FUNCTION source_test_refuse_dispatch() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event_type='evidence.source_dispatch.v1' THEN RAISE EXCEPTION 'synthetic dispatch source unavailable'; END IF; RETURN NEW; END $$;
			CREATE TRIGGER source_test_refuse_dispatch BEFORE INSERT ON internal_outbox_events FOR EACH ROW EXECUTE FUNCTION source_test_refuse_dispatch()`)
			require.NoError(t, err)
			failed := invoke()
			require.NotEqual(t, http.StatusOK, failed.Code)
			require.NotEqual(t, http.StatusUnauthorized, failed.Code, failed.Body.String())
			require.NotEqual(t, http.StatusForbidden, failed.Code, failed.Body.String())
			require.Zero(t, f.ProviderCalls())
			var count int
			require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT count(*) FROM billing_provider_attempts`).Scan(&count))
			require.Zero(t, count)
			require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT count(*) FROM execution_source_facts WHERE profile=ANY($1)`, []string{sourcefacts.AttemptProfile, sourcefacts.CapabilityProfile, sourcefacts.DestinationProfile}).Scan(&count))
			require.Zero(t, count)
			_, err = f.Pool.Exec(t.Context(), `DROP TRIGGER source_test_refuse_dispatch ON internal_outbox_events; DROP FUNCTION source_test_refuse_dispatch()`)
			require.NoError(t, err)
			response := invoke()
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			require.EqualValues(t, 1, f.ProviderCalls())
			request := response.Header().Get(middleware.OperationIDHeader)
			require.NotEmpty(t, request)
			_, err = journal.DrainOnce(t.Context())
			require.NoError(t, err)
			rows, err := f.Pool.Query(t.Context(), `SELECT document FROM execution_source_facts WHERE request_id=$1 AND profile=ANY($2) ORDER BY profile`, request, sourcefacts.ObservationProfiles())
			require.NoError(t, err)
			defer rows.Close()
			found := map[string]bool{}
			kinds := map[string]bool{}
			for rows.Next() {
				var wire []byte
				require.NoError(t, rows.Scan(&wire))
				d, err := sourcefacts.DecodeObservation(wire)
				require.NoError(t, err)
				_, err = sourcefacts.ObservationEvent(d, wire)
				require.NoError(t, err)
				require.NotEmpty(t, d.ContextRef)
				if d.Profile != sourcefacts.RuntimeProfile {
					require.NotEmpty(t, d.RouteEvidenceHash)
				}
				switch d.Profile {
				case sourcefacts.AttemptProfile:
					if d.Kind == "attempt_registered" {
						require.Equal(t, "registered", d.Attempt.Provider)
						require.Equal(t, "reserved", d.Attempt.Financial)
					}
				case sourcefacts.CapabilityProfile:
					require.NotEmpty(t, d.Capability.ReleaseRef)
					require.NotEmpty(t, d.Capability.BindingRef)
					require.NotEmpty(t, d.Capability.Capabilities)
				case sourcefacts.DestinationProfile:
					if d.Kind == "dispatch_registered" {
						require.Equal(t, "registered", d.Destination.Dispatch)
					} else {
						require.Equal(t, "response_observed", d.Destination.Dispatch)
					}
					require.Equal(t, "unknown", d.Destination.InputBindingState)
					require.NotContains(t, d.Destination.Origin, "?")
				}
				found[d.Profile] = true
				kinds[d.Kind] = true
			}
			require.NoError(t, rows.Err())
			require.Len(t, found, 4)
			for _, kind := range []string{"request_started", "attempt_registered", "selection", "dispatch_registered", "provider_response_received", "delivery_started", "local_delivery", "request_terminal", "financial_result", "response_measurement", "round_trip_measurement", "dispatch_result"} {
				require.True(t, kinds[kind], "missing positive original boundary %s", kind)
			}
		})
	}
}

func invokeObservationGateway(t *testing.T, f gateway.SourceFactsGatewayFixture, source *sourcefacts.Service, raw string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, f.Path, strings.NewReader(string(f.Body)))
	r.Header.Set("Content-Type", "application/json")
	if f.Operation == modeloperation.IDMessages {
		r.Header.Set("x-api-key", raw)
		r.Header.Set("anthropic-version", "2023-06-01")
	} else {
		r.Header.Set("Authorization", "Bearer "+raw)
	}
	w := httptest.NewRecorder()
	sourceGatewayHandler(t, f, f.Relay, source).ServeHTTP(w, r)
	return w
}

func TestSourceObservationsActualMonotonicStreamBoundariesIntegration(t *testing.T) {
	for _, operation := range []modeloperation.ID{modeloperation.IDResponses, modeloperation.IDMessages} {
		for _, mode := range []string{"complete", "interrupted"} {
			t.Run(string(operation)+"/"+mode, func(t *testing.T) {
				f := gateway.NewSourceFactsGatewayStreamFixture(t, operation, "1.3.0", mode)
				source, _, raw := sourceGatewayAttachProfiles(t, f, true)
				response := invokeObservationGateway(t, f, source, raw)
				require.Equal(t, 200, response.Code, response.Body.String())
				require.EqualValues(t, 1, f.ProviderCalls())
				request := response.Header().Get(middleware.OperationIDHeader)
				rows, err := f.Pool.Query(t.Context(), `SELECT document FROM execution_source_facts WHERE request_id=$1 AND stage IN ('response_measurement','first_output_measurement','stream_measurement','round_trip_measurement') ORDER BY stage`, request)
				require.NoError(t, err)
				defer rows.Close()
				measurements := map[string]sourcefacts.Measurement{}
				for rows.Next() {
					var wire []byte
					require.NoError(t, rows.Scan(&wire))
					d, err := sourcefacts.DecodeObservation(wire)
					require.NoError(t, err)
					require.NotNil(t, d.Measurement)
					measurements[d.Measurement.Kind] = *d.Measurement
				}
				require.NoError(t, rows.Err())
				require.Len(t, measurements, 4)
				require.Equal(t, "not_applicable", measurements["request_response_complete"].State)
				for _, kind := range []string{"stream_first_valid_output", "attempt_http_round_trip"} {
					m := measurements[kind]
					require.Equal(t, "observed", m.State)
					require.NotNil(t, m.Value)
					n, err := strconv.ParseInt(*m.Value, 10, 64)
					require.NoError(t, err)
					require.GreaterOrEqual(t, n, int64(0))
				}
				complete := measurements["stream_complete"]
				if mode == "complete" {
					require.Equal(t, "observed", complete.State)
					require.NotNil(t, complete.Value)
					first, _ := strconv.ParseInt(*measurements["stream_first_valid_output"].Value, 10, 64)
					last, _ := strconv.ParseInt(*complete.Value, 10, 64)
					require.GreaterOrEqual(t, last, first)
				} else {
					require.Equal(t, "not_observed", complete.State)
					require.Nil(t, complete.Value)
				}
			})
		}
	}
}

func TestSourceObservationsOptionalFailureCannotChangeOriginalMoneyOrDeliveryIntegration(t *testing.T) {
	f := gateway.NewSourceFactsGatewayFixture(t, modeloperation.IDResponses)
	source, journal, raw := sourceGatewayAttachProfiles(t, f, true)
	_, err := f.Pool.Exec(t.Context(), `CREATE FUNCTION source_test_refuse_optional() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.stage IN ('provider_response_received','response_measurement') THEN RAISE EXCEPTION 'synthetic optional observation unavailable'; END IF; RETURN NEW; END $$;
	CREATE TRIGGER source_test_refuse_optional BEFORE INSERT ON execution_source_facts FOR EACH ROW EXECUTE FUNCTION source_test_refuse_optional()`)
	require.NoError(t, err)
	response := invokeObservationGateway(t, f, source, raw)
	require.Equal(t, 200, response.Code, response.Body.String())
	require.EqualValues(t, 1, f.ProviderCalls())
	request := response.Header().Get(middleware.OperationIDHeader)
	_, err = journal.DrainOnce(t.Context())
	require.NoError(t, err)
	var reservation, delivery string
	var terminal, missing int
	require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT status FROM freeze_idempotency WHERE request_id=$1`, request).Scan(&reservation))
	require.Equal(t, "settled", reservation)
	require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT outcome FROM request_delivery_receipts WHERE request_id=$1 ORDER BY sequence DESC LIMIT 1`, request).Scan(&delivery))
	require.Equal(t, "completed", delivery)
	require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT count(*) FROM usage_finalization_journal WHERE request_id=$1 AND event_type='settled'`, request).Scan(&terminal))
	require.Equal(t, 1, terminal)
	require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT count(*) FROM execution_source_facts WHERE request_id=$1 AND stage IN ('provider_response_received','response_measurement')`, request).Scan(&missing))
	require.Zero(t, missing)
	require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT count(*) FROM execution_source_facts WHERE request_id=$1 AND stage='financial_result'`, request).Scan(&terminal))
	require.Equal(t, 1, terminal)
}

func TestSourceObservationsDispatchPublicationFailurePreservesResultAndFrozenOrigin(t *testing.T) {
	f := gateway.NewSourceFactsGatewayFixture(t, modeloperation.IDResponses)
	source, journal, raw := sourceGatewayAttachProfiles(t, f, true)
	ctx := t.Context()
	var originalOrigin string
	require.NoError(t, f.Pool.QueryRow(ctx, `SELECT base_url FROM provider_connections WHERE id=$1`, f.ConnectionID).Scan(&originalOrigin))
	originalOrigin, err := sourcefacts.DisclosureOrigin(originalOrigin)
	require.NoError(t, err)
	_, err = f.Pool.Exec(ctx, `CREATE FUNCTION refuse_dispatch_publication() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.stage IN ('attempt_registered','selection','dispatch_registered') THEN RAISE EXCEPTION 'synthetic publication fault after durable dispatch'; END IF; RETURN NEW; END$$; CREATE TRIGGER refuse_dispatch_publication BEFORE INSERT ON execution_source_facts FOR EACH ROW EXECUTE FUNCTION refuse_dispatch_publication()`)
	require.NoError(t, err)
	response := invokeObservationGateway(t, f, source, raw)
	require.Equal(t, 200, response.Code, response.Body.String())
	require.EqualValues(t, 1, f.ProviderCalls())
	request := response.Header().Get(middleware.OperationIDHeader)
	var resultBytes []byte
	require.NoError(t, f.Pool.QueryRow(ctx, `SELECT document FROM execution_source_facts WHERE request_id=$1 AND stage='dispatch_result'`, request).Scan(&resultBytes))
	result, err := sourcefacts.DecodeObservation(resultBytes)
	require.NoError(t, err)
	require.Equal(t, originalOrigin, result.Destination.Origin, "measurement uses pending frozen input")
	require.Equal(t, "response_observed", result.Destination.Dispatch)
	_, err = f.Pool.Exec(ctx, `UPDATE provider_connections SET base_url='https://changed.synthetic.invalid',config_revision=config_revision+1 WHERE id=$1`, f.ConnectionID)
	require.NoError(t, err)
	_, err = journal.DrainOnce(ctx)
	require.ErrorContains(t, err, "synthetic publication fault after durable dispatch")
	var status, delivery string
	var pending, registered, settled int
	require.NoError(t, f.Pool.QueryRow(ctx, `SELECT status FROM freeze_idempotency WHERE request_id=$1`, request).Scan(&status))
	require.Equal(t, "settled", status)
	require.NoError(t, f.Pool.QueryRow(ctx, `SELECT outcome FROM request_delivery_receipts WHERE request_id=$1 ORDER BY sequence DESC LIMIT 1`, request).Scan(&delivery))
	require.Equal(t, "completed", delivery)
	require.NoError(t, f.Pool.QueryRow(ctx, `SELECT count(*) FROM usage_finalization_journal WHERE request_id=$1 AND event_type='settled'`, request).Scan(&settled))
	require.Equal(t, 1, settled)
	require.NoError(t, f.Pool.QueryRow(ctx, `SELECT count(*) FROM internal_outbox_events WHERE request_id=$1 AND event_type='evidence.source_dispatch.v1' AND payload->>'materialized'='false'`, request).Scan(&pending))
	require.Equal(t, 1, pending)
	require.NoError(t, f.Pool.QueryRow(ctx, `SELECT count(*) FROM execution_source_facts WHERE request_id=$1 AND stage IN ('attempt_registered','selection','dispatch_registered')`, request).Scan(&registered))
	require.Zero(t, registered, "failed publication rolls back all three records")
	_, err = f.Pool.Exec(ctx, `DROP TRIGGER refuse_dispatch_publication ON execution_source_facts; DROP FUNCTION refuse_dispatch_publication(); UPDATE internal_outbox_events SET available_at=clock_timestamp() WHERE event_type='evidence.source_dispatch.v1'`)
	require.NoError(t, err)
	_, err = journal.DrainOnce(ctx)
	require.NoError(t, err)
	var lateBytes, retained []byte
	require.NoError(t, f.Pool.QueryRow(ctx, `SELECT document FROM execution_source_facts WHERE request_id=$1 AND stage='dispatch_registered'`, request).Scan(&lateBytes))
	late, err := sourcefacts.DecodeObservation(lateBytes)
	require.NoError(t, err)
	require.Equal(t, originalOrigin, late.Destination.Origin)
	require.Equal(t, "registered", late.Destination.Dispatch)
	require.NoError(t, f.Pool.QueryRow(ctx, `SELECT document FROM execution_source_facts WHERE event_id=$1`, result.EventID).Scan(&retained))
	require.Equal(t, resultBytes, retained, "late registration never downgrades an already known result")
}

func TestSourceObservationsPendingDestinationSurvivesConnectionChangeDuringDo(t *testing.T) {
	f := gateway.NewSourceFactsGatewayFixture(t, modeloperation.IDResponses)
	source, _, raw := sourceGatewayAttachProfiles(t, f, true)
	var original string
	require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT base_url FROM provider_connections WHERE id=$1`, f.ConnectionID).Scan(&original))
	original, err := sourcefacts.DisclosureOrigin(original)
	require.NoError(t, err)
	f.BeforeProvider(func() {
		_, err := f.Pool.Exec(t.Context(), `UPDATE provider_connections SET base_url='https://changed.synthetic.invalid',config_revision=config_revision+1 WHERE id=$1`, f.ConnectionID)
		require.NoError(t, err)
	})
	response := invokeObservationGateway(t, f, source, raw)
	// The original health fence rejects the changed connection revision after
	// Do. Preserve that behavior; an upstream response is not customer delivery.
	require.Equal(t, 503, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "server_error.execution_state_unavailable")
	require.EqualValues(t, 1, f.ProviderCalls())
	request := response.Header().Get(middleware.OperationIDHeader)
	var wire []byte
	require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT document FROM execution_source_facts WHERE request_id=$1 AND stage='dispatch_result'`, request).Scan(&wire))
	document, err := sourcefacts.DecodeObservation(wire)
	require.NoError(t, err)
	require.Equal(t, original, document.Destination.Origin)
	require.Equal(t, "response_observed", document.Destination.Dispatch)
	var registered int
	require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT count(*) FROM execution_source_facts WHERE request_id=$1 AND stage='dispatch_registered'`, request).Scan(&registered))
	require.Zero(t, registered, "the observation came from the frozen pending event")
}
