package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/agentcap"
	"github.com/uniroute/unirouter/internal/executioncontext"
	"github.com/uniroute/unirouter/internal/models"
)

type captureExecutionFunc func(context.Context, executioncontext.Snapshot) error

func (f captureExecutionFunc) CaptureAuthenticated(ctx context.Context, s executioncontext.Snapshot) error {
	return f(ctx, s)
}

func TestExecutionCapturePreservesValidatedIdentityAndIgnoresCallerLabels(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/embeddings", "/v1/responses", "/v1/messages", "/v1/media/jobs"} {
		t.Run(path, func(t *testing.T) {
			project, owner, app, credential := int64(2), int64(3), int64(4), int64(5)
			key := &models.ApiKey{ID: 1, OrgID: 1, ProjectID: &project, OwnerUserID: &owner, WorkloadApplicationID: &app, AccessCredentialID: &credential, CredentialRef: "credential-original", KeyType: "service", AuthEpoch: 7, ExecutionIdentity: &executioncontext.Identity{GrantID: "9", GrantRevision: "2", SponsorUserID: "10", ActualHuman: "unknown", AuthorityRead: "live_boundary"}}
			r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"synthetic","actor":"forged","workflow_id":"forged"}`))
			r.Header.Set("X-Actor", "forged")
			r.Header.Set("Authorization", "Bearer never-copy-this")
			ctx := context.WithValue(r.Context(), CtxAPIKey, key)
			ctx = context.WithValue(ctx, CtxRequestID, "server-request")
			ctx = context.WithValue(ctx, CtxAuthKind, AuthKindAgentCapability)
			ctx = context.WithValue(ctx, CtxAgentCapability, &agentcap.Claims{WorkflowID: "validated-workflow", AgentID: "validated-agent", RootAgentID: "validated-root", RootAuthorityVersion: "validated-version", Lineage: []string{"validated-root"}})
			count := 0
			next := false
			h := CaptureExecutionContext(captureExecutionFunc(func(_ context.Context, s executioncontext.Snapshot) error {
				count++
				require.NoError(t, s.Validate())
				require.Equal(t, "3", s.Identity.OwnerUserID)
				require.Equal(t, "4", s.Core.WorkloadApplicationID)
				require.Equal(t, "5", s.Core.CredentialID)
				require.Equal(t, "10", s.Identity.SponsorUserID)
				require.Equal(t, "unknown", s.Identity.ActualHuman)
				require.Equal(t, "validated-workflow", s.Identity.WorkflowID)
				return nil
			}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next = true
				s, ok := executioncontext.FromContext(r.Context())
				require.True(t, ok)
				require.Equal(t, "server-request", s.Core.RequestID)
				w.WriteHeader(204)
			}))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r.WithContext(ctx))
			require.Equal(t, 204, w.Code)
			require.Equal(t, 1, count)
			require.True(t, next)
		})
	}
}

func TestExecutionCaptureFailurePreventsDispatchAndDisabledIsTransparent(t *testing.T) {
	project := int64(2)
	key := &models.ApiKey{ID: 1, OrgID: 1, ProjectID: &project, KeyType: "shared"}
	for _, path := range []string{"/v1/chat/completions", "/v1/messages"} {
		r := httptest.NewRequest(http.MethodPost, path, nil)
		ctx := context.WithValue(r.Context(), CtxAPIKey, key)
		ctx = context.WithValue(ctx, CtxRequestID, "capture-failure")
		ctx = context.WithValue(ctx, CtxAuthKind, AuthKindAPIKey)
		next := 0
		h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { next++; w.WriteHeader(204) })
		w := httptest.NewRecorder()
		CaptureExecutionContext(captureExecutionFunc(func(context.Context, executioncontext.Snapshot) error { return errors.New("synthetic unavailable") }))(h).ServeHTTP(w, r.WithContext(ctx))
		require.Equal(t, 503, w.Code)
		require.Zero(t, next)
		require.NotContains(t, w.Body.String(), "synthetic unavailable")
		w = httptest.NewRecorder()
		CaptureExecutionContext(nil)(h).ServeHTTP(w, r)
		require.Equal(t, 204, w.Code)
		require.Equal(t, 1, next)
	}
}
