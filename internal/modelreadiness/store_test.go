package modelreadiness

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/accessgrantboundary"
	"github.com/uniroute/unirouter/internal/accessscope"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestInspectCallerFailsClosedBeforeResolvingAnUnboundedCredentialSet(t *testing.T) {
	var boundaryReads atomic.Int64
	database := &testutil.MockDB{
		QueryRowFn: func(_ context.Context, query string, _ ...any) pgx.Row {
			switch {
			case strings.Contains(query, "FROM access_grants ag"):
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*dest[0].(*int64) = 41
					*dest[1].(*string) = "active"
					*dest[2].(*string) = accessscope.ModeProjectDefault
					*dest[3].(*[]byte) = []byte(`[]`)
					*dest[4].(*[]byte) = []byte(`[]`)
					*dest[5].(*time.Time) = time.Now().UTC().Add(-time.Hour)
					return nil
				}}
			case strings.Contains(query, "FROM api_keys ak"):
				boundaryReads.Add(1)
				return &testutil.MockRow{ScanFn: func(...any) error { return accessgrantboundary.ErrInactive }}
			default:
				t.Fatalf("unexpected QueryRow: %s", query)
				return &testutil.MockRow{ScanFn: func(...any) error { return pgx.ErrNoRows }}
			}
		},
		QueryFn: func(_ context.Context, query string, args ...any) (pgx.Rows, error) {
			if !strings.Contains(query, "LIMIT $4") || len(args) != 4 || args[3] != MaxCredentialCandidates+1 {
				t.Fatalf("credential query is not bounded: query=%s args=%#v", query, args)
			}
			rows := make([]func(...any) error, MaxCredentialCandidates+1)
			for index := range rows {
				keyID := int64(index + 1)
				rows[index] = func(dest ...any) error {
					*dest[0].(*int64) = keyID
					return nil
				}
			}
			return &testutil.MockRows{Rows: rows}, nil
		},
	}

	snapshot := InspectCaller(context.Background(), database, CallerInput{
		OrgID: 7, ProjectID: 9, Caller: "self", ActorUserID: 11,
		RequestedModel: "company-chat", ResolvedModel: "provider-chat", Endpoint: "/v1/chat/completions",
	})
	if snapshot.CredentialError == nil || !strings.Contains(snapshot.CredentialError.Error(), "candidate limit exceeded") {
		t.Fatalf("credential error=%v, want bounded fail-closed result", snapshot.CredentialError)
	}
	if got := boundaryReads.Load(); got != 0 {
		t.Fatalf("over-limit candidate set performed %d deep boundary reads; want zero", got)
	}
}
