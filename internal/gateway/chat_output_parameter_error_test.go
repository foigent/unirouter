package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/models"
	"github.com/uniroute/unirouter/internal/payloadpolicy"
)

func TestOutputParameterDiagnosticDisclosure(t *testing.T) {
	valid := &unsupportedOutputParameterError{parameter: "max_tokens", accepted: []string{"max_completion_tokens"}, profile: "private-profile-marker"}
	wrapped := fmt.Errorf("outer: %w: %w", errModelExecutionProfileRequestInvalid, valid)
	require.ErrorIs(t, wrapped, errModelExecutionProfileRequestInvalid)
	var cause *unsupportedOutputParameterError
	require.ErrorAs(t, wrapped, &cause)
	detail := publicOutputParameterError(wrapped)
	require.NotNil(t, detail)
	require.Equal(t, "unsupported_output_parameter", detail.Code)
	require.Equal(t, "max_tokens", detail.Parameter)
	require.Equal(t, []string{"max_completion_tokens"}, detail.AcceptedParameters)
	require.NotContains(t, detail.Message, "private-profile-marker")
	for _, bad := range []error{
		valid, fmt.Errorf("%w: private request max_tokens", errModelExecutionProfileRequestInvalid),
		errors.New("private-profile-marker max_tokens is not supported"),
		fmt.Errorf("%w: %w", errModelExecutionProfileRequestInvalid, (*unsupportedOutputParameterError)(nil)),
	} {
		require.Nil(t, publicOutputParameterError(bad))
	}
	for _, fields := range []struct {
		field    string
		accepted []string
	}{
		{"private", []string{"max_completion_tokens"}}, {"max_tokens", nil},
		{"max_tokens", []string{"private"}}, {"max_tokens", []string{"max_tokens"}},
		{"max_tokens", []string{"max_completion_tokens", "private"}},
		{"max_tokens", []string{"max_completion_tokens", "max_completion_tokens"}},
	} {
		err := fmt.Errorf("%w: %w", errModelExecutionProfileRequestInvalid, &unsupportedOutputParameterError{parameter: fields.field, accepted: fields.accepted, profile: "private"})
		require.Nil(t, publicOutputParameterError(err))
		code, message, ok := modelExecutionLimitErrorDetails(err)
		require.True(t, ok)
		require.Equal(t, "validation_error.model_execution_profile", code)
		require.Equal(t, "Request is incompatible with the selected model execution profile", message)
	}
}

func TestOutputParameterMigrationPreservesAdmissionAndNoDispatch(t *testing.T) {
	const first = `{"model":"public-model","stream":true,"enable_thinking":true,"stream_options":{"include_usage":true},"tool_choice":"auto","max_completion_tokens":8192,"messages":[{"role":"user","content":"private-prompt-marker"}],"tools":[{"type":"function","function":{"name":"read","description":"Read a fixture","parameters":{"type":"object"}}}]}`
	continuation := strings.Replace(first, `"private-prompt-marker"}]`, `"private-prompt-marker"},{"role":"assistant","content":null,"reasoning_content":"thought","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"private-prompt-marker"}]`, 1)
	for _, tc := range []struct {
		name, body, field, accepted string
		status                      int
		pass                        bool
	}{
		{"first", first, "", "", 200, true}, {"continuation", continuation, "", "", 200, true},
		{"legacy", strings.Replace(first, "max_completion_tokens", "max_tokens", 1), "max_tokens", "max_completion_tokens", 200, false},
		{"both equal", strings.Replace(first, `"max_completion_tokens":8192`, `"max_completion_tokens":8192,"max_tokens":8192`, 1), "max_tokens", "max_completion_tokens", 200, false},
		{"both differ", strings.Replace(first, `"max_completion_tokens":8192`, `"max_completion_tokens":8192,"max_tokens":4096`, 1), "", "", 400, false},
		{"glm reverse", strings.Replace(preflightToolRequest, "max_tokens", "max_completion_tokens", 1), "max_completion_tokens", "max_tokens", 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			relay, p, c := newPreflightFixture(t)
			if tc.name != "glm reverse" {
				p.plan = providerCompletionTestPlan(t)
			}
			if tc.pass {
				_, err := validateChatExecutionProfileRequest([]byte(tc.body), p.plan)
				require.NoError(t, err)
			}
			w, result := runChatPreflight(t, relay, tc.body)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.pass, result.LocallyAdmissible, w.Body.String())
			if tc.field != "" {
				require.Equal(t, []string{"validation_error.model_execution_profile"}, result.Reasons)
				require.Len(t, result.ParameterErrors, 1)
				require.Equal(t, tc.field, result.ParameterErrors[0].Parameter)
				require.Equal(t, []string{tc.accepted}, result.ParameterErrors[0].AcceptedParameters)
				require.Contains(t, result.ParameterErrors[0].Message, "Remove "+tc.field+" and use "+tc.accepted)
			} else {
				require.Empty(t, result.ParameterErrors)
			}
			require.Zero(t, c.authorizeCount.Load())
			require.Zero(t, c.renewCount.Load())
			require.Zero(t, c.releaseCount.Load())
			if tc.pass {
				return
			}
			// Chat keeps its existing rejection trace, but cannot reserve or dispatch.
			relay.db = executionTestDB()
			rec := httptest.NewRecorder()
			relay.HandleChatCompletion(rec, executionRequest("POST", "/v1/chat/completions", "migration-negative", tc.body))
			require.Equal(t, 400, rec.Code, rec.Body.String())
			if tc.field != "" {
				require.Contains(t, rec.Body.String(), "Remove "+tc.field+" and use "+tc.accepted)
				require.Contains(t, rec.Body.String(), `"code":"validation_error.model_execution_profile"`)
			}
			require.NotContains(t, rec.Body.String(), "private-prompt-marker")
			require.Zero(t, c.authorizeCount.Load())
		})
	}
}

func TestOutputParameterMigrationAuthorizationPrecedesDisclosure(t *testing.T) {
	for _, preflight := range []bool{true, false} {
		relay, p, c := newPreflightFixture(t)
		p.plan = providerCompletionTestPlan(t)
		req := executionRequest("POST", "/v1/chat/completions", "migration-denied", preflightToolRequest)
		req = req.WithContext(context.WithValue(req.Context(), middleware.CtxAPIKey, &models.ApiKey{AllowedModels: "other"}))
		rec := httptest.NewRecorder()
		if preflight {
			// Use real retained-payload ingress before the caller's model scope check.
			payloadpolicy.RequestMiddleware(payloadpolicy.MustNewBudget(payloadpolicy.DefaultLimits()), payloadpolicy.ProfileChatRequest, nil)(http.HandlerFunc(relay.HandleChatPreflight)).ServeHTTP(rec, req)
		} else {
			relay.db = executionTestDB()
			relay.HandleChatCompletion(rec, req)
		}
		require.Equal(t, 403, rec.Code, rec.Body.String())
		require.NotContains(t, rec.Body.String(), "accepted_parameters")
		require.NotContains(t, rec.Body.String(), "Remove max_tokens")
		require.Empty(t, p.requests)
		require.Zero(t, c.authorizeCount.Load())
	}
}
