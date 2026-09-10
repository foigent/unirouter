//go:build integration

package gateway_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/gateway"
	"github.com/uniroute/unirouter/internal/guardrail"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/sourcefacts"
)

type sourceSyntheticDetector struct{ failure, stream bool }

func (sourceSyntheticDetector) Version() string { return "1.0.0" }
func (sourceSyntheticDetector) StreamCapability() guardrail.StreamCapability {
	return guardrail.StreamCapability{Synchronous: true}
}
func (d sourceSyntheticDetector) Detect(_ guardrail.Context, text guardrail.Text, _ json.RawMessage) ([]guardrail.Finding, error) {
	if d.failure {
		return nil, errors.New("synthetic detector unavailable")
	}
	if d.stream && !strings.Contains(text.Content, "answer") {
		return nil, nil
	}
	if text.Content == "" {
		return nil, nil
	}
	return []guardrail.Finding{{Category: "synthetic-private-category", Start: 0, End: 1, ConfidenceBucket: "high"}}, nil
}

func TestSourcePolicyOriginalGatewayEffectsAndOptionalFailureIntegration(t *testing.T) {
	for _, tc := range []struct {
		name, stage, mode, action, failure, effect, outcome string
		status                                              int
		calls                                               int64
		broken                                              bool
	}{
		{"shadow-match", "input", "shadow", "block", "fail_closed", "", "match", 200, 1, false},
		{"input-block", "input", "enforce", "block", "fail_closed", "input_blocked", "block", 400, 0, false},
		{"input-replace", "input", "enforce", "redact", "fail_closed", "input_replaced", "replace", 200, 1, false},
		{"output-block", "nonstream_output", "enforce", "block", "fail_closed", "output_blocked", "block", 403, 1, false},
		{"output-replace", "nonstream_output", "enforce", "redact", "fail_closed", "output_replaced", "replace", 200, 1, false},
		{"fail-closed", "input", "enforce", "block", "fail_closed", "input_blocked", "block", 400, 0, true},
		{"degraded-allow", "input", "enforce", "block", "degraded_allow", "", "error", 200, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := gateway.NewSourceFactsGatewayFixture(t, modeloperation.IDResponses)
			source, journal, raw := sourceGatewayAttachProfiles(t, f, true)
			service, err := guardrail.NewService(f.Pool, nil, []byte(strings.Repeat("s", 32)), guardrail.DefaultOptions())
			require.NoError(t, err)
			require.NoError(t, service.RegisterDetector("external:source-synthetic", sourceSyntheticDetector{failure: tc.broken}))
			_, err = f.Pool.Exec(t.Context(), `INSERT INTO guardrail_policies(org_id,name,stage,detector_ref,action,mode,failure_mode,config) VALUES($1,'synthetic-source',$2,'external:source-synthetic',$3,$4,$5,'{}')`, f.OrgID, tc.stage, tc.action, tc.mode, tc.failure)
			require.NoError(t, err)
			require.NoError(t, service.Refresh(t.Context()))
			f.Relay.SetGuardrail(service)
			response := invokeObservationGateway(t, f, source, raw)
			require.Equal(t, tc.status, response.Code, response.Body.String())
			require.Equal(t, tc.calls, f.ProviderCalls())
			request := response.Header().Get(middleware.OperationIDHeader)
			require.NotEmpty(t, request)
			_, err = journal.DrainOnce(t.Context())
			require.NoError(t, err)
			var wire []byte
			require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT document FROM execution_source_facts WHERE request_id=$1 AND profile=$2`, request, sourcefacts.PolicyProfile).Scan(&wire))
			d, err := sourcefacts.DecodeObservation(wire)
			require.NoError(t, err)
			require.Equal(t, tc.outcome, d.Policy.Outcome)
			require.Equal(t, tc.mode, d.Policy.Mode)
			if tc.effect == "" {
				require.Empty(t, d.Policy.Effects)
			} else {
				require.Contains(t, d.Policy.Effects, tc.effect)
			}
			require.Equal(t, tc.broken, d.Policy.Degraded)
			for _, private := range []string{"synthetic-private-category", "fingerprints", "match_count", "categories", "synthetic answer"} {
				require.NotContains(t, string(wire), private)
			}
			if tc.calls == 0 {
				var attempts int
				require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT count(*) FROM billing_provider_attempts WHERE request_id=$1`, request).Scan(&attempts))
				require.Zero(t, attempts)
			}
			if tc.name == "output-block" {
				require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT document FROM execution_source_facts WHERE request_id=$1 AND stage='output_held' AND profile=$2`, request, sourcefacts.RuntimeProfile).Scan(&wire))
				held, err := sourcefacts.DecodeObservation(wire)
				require.NoError(t, err)
				require.Equal(t, "pending", held.Runtime.Financial)
				require.Equal(t, "not_started", held.Runtime.LocalDelivery)
			}
			// Optional source failures cannot overturn a completed business path.
			if tc.name == "shadow-match" {
				_, err = f.Pool.Exec(t.Context(), `CREATE FUNCTION reject_optional_source_policy() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.profile='unirouter.policy-observation.v1' THEN RAISE EXCEPTION 'synthetic policy source failure';END IF;RETURN NEW;END $$;CREATE TRIGGER reject_optional_source_policy BEFORE INSERT ON execution_source_facts FOR EACH ROW EXECUTE FUNCTION reject_optional_source_policy()`)
				require.NoError(t, err)
				response = invokeObservationGateway(t, f, source, raw)
				require.Equal(t, http.StatusOK, response.Code, response.Body.String())
				require.EqualValues(t, 2, f.ProviderCalls())
			}
		})
	}
}

func TestSourcePolicyOriginalStreamTerminationSummaryIntegration(t *testing.T) {
	f := gateway.NewSourceFactsGatewayStreamFixture(t, modeloperation.IDResponses, "1.3.0", "complete")
	source, journal, raw := sourceGatewayAttachProfiles(t, f, true)
	service, err := guardrail.NewService(f.Pool, nil, []byte(strings.Repeat("s", 32)), guardrail.DefaultOptions())
	require.NoError(t, err)
	require.NoError(t, service.RegisterDetector("external:source-synthetic", sourceSyntheticDetector{stream: true}))
	_, err = f.Pool.Exec(t.Context(), `INSERT INTO guardrail_policies(org_id,name,stage,detector_ref,action,mode,failure_mode,config) VALUES($1,'synthetic-stream','stream_output','external:source-synthetic','block','enforce','fail_closed','{}')`, f.OrgID)
	require.NoError(t, err)
	require.NoError(t, service.Refresh(t.Context()))
	f.Relay.SetGuardrail(service)
	response := invokeObservationGateway(t, f, source, raw)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.EqualValues(t, 1, f.ProviderCalls())
	require.NotContains(t, response.Body.String(), `"delta":" answer"`)
	request := response.Header().Get(middleware.OperationIDHeader)
	_, err = journal.DrainOnce(t.Context())
	require.NoError(t, err)
	var wire []byte
	require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT document FROM execution_source_facts WHERE request_id=$1 AND profile=$2`, request, sourcefacts.PolicyProfile).Scan(&wire))
	doc, err := sourcefacts.DecodeObservation(wire)
	require.NoError(t, err)
	require.Equal(t, "stream_result", doc.Kind)
	require.Equal(t, "partial", doc.Coverage)
	require.Contains(t, doc.Policy.DetectorOutcomes, "match")
	require.Contains(t, doc.Policy.Effects, "stream_terminated")
	require.Contains(t, doc.Policy.Effects, "partial_output")
	var count int
	require.NoError(t, f.Pool.QueryRow(t.Context(), `SELECT count(*) FROM execution_source_facts WHERE request_id=$1 AND profile=$2`, request, sourcefacts.PolicyProfile).Scan(&count))
	require.Equal(t, 1, count)
}
