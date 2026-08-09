package middleware

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/payloadpolicy"
	"github.com/uniroute/unirouter/internal/testutil"
)

type stagedRequestBody struct {
	started chan struct{}
	release chan struct{}
	sent    bool
}

func (b *stagedRequestBody) Read(target []byte) (int, error) {
	if b.sent {
		return 0, io.EOF
	}
	b.sent = true
	close(b.started)
	<-b.release
	return copy(target, []byte(`{"model":"company-chat","messages":[]}`)), nil
}

func TestGatewayAuthAcrossBufferedBodyRejectsAuthorityRevokedDuringUpload(t *testing.T) {
	var authorityActive atomic.Bool
	authorityActive.Store(true)
	var authorityReads atomic.Int64
	database := &testutil.MockDB{QueryRowFn: func(_ context.Context, query string, _ ...any) pgx.Row {
		if strings.Contains(query, "billing_provider_attempts") {
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*bool) = false
				return nil
			}}
		}
		authorityReads.Add(1)
		return &testutil.MockRow{ScanFn: func(dest ...any) error {
			if !authorityActive.Load() {
				return pgx.ErrNoRows
			}
			*dest[0].(*int64) = 41
			*dest[1].(*int64) = 7
			*dest[2].(*int64) = 11
			*dest[3].(*string) = "active"
			*dest[4].(*string) = ""
			*dest[5].(*string) = "company-chat"
			*dest[6].(*int) = 60
			*dest[7].(*int) = 10
			*dest[8].(**int) = nil
			*dest[9].(*string) = "ur-body"
			*dest[10].(*string) = "body revalidation"
			*dest[11].(*int64) = 0
			*dest[12].(*string) = "shared"
			*dest[13].(*string) = ""
			*dest[14].(*int64) = 1
			*dest[15].(*string) = "active"
			*dest[16].(*string) = "/v1/chat/completions"
			*dest[17].(*sql.NullInt64) = sql.NullInt64{}
			return nil
		}}
	}}

	budget := payloadpolicy.MustNewBudget(payloadpolicy.DefaultLimits())
	authenticate := GatewayAuthWithAPIKeyHashSecret(database, "post-body-test-secret")
	retain := payloadpolicy.RequestMiddleware(budget, payloadpolicy.ProfileChatRequest, GetOrgIDFromContext)
	var reachedSink atomic.Bool
	handler := GatewayAuthAcrossBufferedBody(authenticate, retain)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reachedSink.Store(true)
		w.WriteHeader(http.StatusNoContent)
	}))

	body := &stagedRequestBody{started: make(chan struct{}), release: make(chan struct{})}
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", body)
	request.Header.Set("Authorization", "Bearer ur-body-revalidation-secret")
	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(recorder, request)
		close(done)
	}()

	<-body.started
	authorityActive.Store(false)
	close(body.release)
	<-done

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if reachedSink.Load() {
		t.Fatal("request reached paid sink with a superseded authorization snapshot")
	}
	if got := authorityReads.Load(); got != 2 {
		t.Fatalf("live authority reads=%d, want exactly one before and one after body", got)
	}
}
