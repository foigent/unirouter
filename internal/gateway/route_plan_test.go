package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/routingpolicy"
	"github.com/uniroute/unirouter/internal/testutil"
)

type routePlanRow struct {
	id                   int64
	name                 string
	provider             string
	baseURL              string
	priority             int
	weight               int
	channelStatus        string
	circuitState         string
	halfOpenUntil        *time.Time
	degradationThreshold int
	keyID                int64
	keySecret            string
	keyStatus            string
	keyCooldown          *time.Time
	allowedModels        string
	dbNow                time.Time
}

func TestChannelQueryUsesExactCanonicalModelMembership(t *testing.T) {
	for _, required := range []string{"regexp_split_to_table", "[,，;；\\r\\n]+", "BTRIM(configured.model) = $1"} {
		if !strings.Contains(channelQuery, required) {
			t.Fatalf("channelQuery missing %q: %s", required, channelQuery)
		}
	}
	if strings.Contains(channelQuery, " LIKE ") {
		t.Fatalf("channelQuery must not interpret model identifiers as LIKE patterns: %s", channelQuery)
	}
}

func (row routePlanRow) scan(dest ...any) error {
	*dest[0].(*int64) = row.id
	*dest[1].(*string) = row.name
	*dest[2].(*string) = row.provider
	*dest[3].(*string) = row.baseURL
	*dest[4].(*string) = "gpt-route-plan"
	*dest[5].(*json.RawMessage) = json.RawMessage(`{}`)
	*dest[6].(*string) = "openai_compatible"
	*dest[7].(*json.RawMessage) = json.RawMessage(`{}`)
	*dest[8].(*int) = 128000
	*dest[9].(*int) = 4096
	*dest[10].(*int) = row.priority
	*dest[11].(*string) = row.channelStatus
	*dest[12].(*string) = row.circuitState
	*dest[13].(**time.Time) = row.halfOpenUntil
	*dest[14].(*int) = 0
	*dest[15].(*int64) = row.keyID
	*dest[16].(*string) = row.keySecret
	*dest[17].(*string) = row.keyStatus
	*dest[18].(**time.Time) = row.keyCooldown
	*dest[19].(*int) = row.degradationThreshold
	*dest[20].(*int) = row.weight
	*dest[21].(*int64) = 1
	*dest[22].(*int64) = 1
	*dest[23].(**time.Time) = nil
	*dest[24].(*string) = ""
	*dest[25].(*int64) = 1
	*dest[26].(*string) = row.allowedModels
	*dest[27].(**time.Time) = nil
	*dest[28].(*time.Time) = row.dbNow
	return nil
}

func routePlanRows(rows ...routePlanRow) *testutil.MockRows {
	result := &testutil.MockRows{Rows: make([]func(dest ...any) error, 0, len(rows))}
	for _, row := range rows {
		row := row
		result.Rows = append(result.Rows, row.scan)
	}
	return result
}

func TestP6F08DeterministicWeightedRoutePlanUsesOneSnapshot(t *testing.T) {
	dbNow := time.Date(2026, 7, 12, 3, 0, 0, 0, time.UTC)
	rows := []routePlanRow{
		{id: 1, name: "weight-100", provider: "openai", baseURL: "https://one.invalid", priority: 10, weight: 100, channelStatus: "active", circuitState: "healthy", degradationThreshold: 4, keyID: 11, keySecret: "secret-one", keyStatus: "active", dbNow: dbNow},
		{id: 2, name: "weight-300", provider: "azure", baseURL: "https://two.invalid", priority: 10, weight: 300, channelStatus: "active", circuitState: "healthy", degradationThreshold: 4, keyID: 22, keySecret: "secret-two", keyStatus: "active", dbNow: dbNow},
		{id: 3, name: "later-priority", provider: "anthropic", baseURL: "https://three.invalid", priority: 20, weight: 1000, channelStatus: "active", circuitState: "healthy", degradationThreshold: 4, keyID: 33, keySecret: "secret-three", keyStatus: "active", dbNow: dbNow},
	}
	var queryCount atomic.Int32
	db := &testutil.MockDB{QueryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
		queryCount.Add(1)
		return routePlanRows(rows...), nil
	}}
	router := NewRouter(db)
	router.now = func() time.Time { return dbNow }

	firstByChannel := map[int64]int{}
	for i := 0; i < 2000; i++ {
		selectionKey := fmt.Sprintf("server-request-%d", i)
		plan, err := router.RouteAllWithKey(context.Background(), "gpt-route-plan", selectionKey)
		if err != nil {
			t.Fatalf("route plan %d: %v", i, err)
		}
		if len(plan) != 3 || plan[2].Channel.ID != 3 {
			t.Fatalf("lower priority tier crossed higher priority candidates: %#v", plan)
		}
		firstByChannel[plan[0].Channel.ID]++
		repeat, err := router.RouteAllWithKey(context.Background(), "gpt-route-plan", selectionKey)
		if err != nil || repeat[0].Channel.ID != plan[0].Channel.ID || repeat[1].Channel.ID != plan[1].Channel.ID {
			t.Fatalf("same request ID produced a different route plan: first=%#v repeat=%#v err=%v", plan, repeat, err)
		}
	}
	if queryCount.Load() != 1 {
		t.Fatalf("expected one immutable snapshot query, got %d", queryCount.Load())
	}
	if firstByChannel[2] < 1350 || firstByChannel[2] > 1650 {
		t.Fatalf("300:100 static weight distribution outside deterministic fixture tolerance: %#v", firstByChannel)
	}
	if firstByChannel[1]+firstByChannel[2] != 2000 || firstByChannel[3] != 0 {
		t.Fatalf("unexpected first-choice distribution: %#v", firstByChannel)
	}
}

func TestP6F08SnapshotLastKnownGoodIsBoundedThenFailsClosed(t *testing.T) {
	current := time.Date(2026, 7, 12, 3, 0, 0, 0, time.UTC)
	row := routePlanRow{id: 1, name: "stable", provider: "openai", baseURL: "https://stable.invalid", priority: 10, weight: 100, channelStatus: "active", circuitState: "healthy", degradationThreshold: 4, keyID: 11, keySecret: "secret", keyStatus: "active", dbNow: current}
	queryCount := 0
	db := &testutil.MockDB{QueryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
		queryCount++
		if queryCount == 1 {
			return routePlanRows(row), nil
		}
		return nil, fmt.Errorf("postgres unavailable")
	}}
	policy := routingpolicy.Default()
	policy.SnapshotRefreshSeconds = 1
	policy.SnapshotStaleSeconds = 3
	router, err := NewRouterWithPolicy(db, policy)
	if err != nil {
		t.Fatal(err)
	}
	router.now = func() time.Time { return current }

	live, err := router.RouteAllWithKey(context.Background(), "gpt-route-plan", "request-live")
	if err != nil || live[0].SnapshotSource != "live" || live[0].SnapshotDegraded {
		t.Fatalf("unexpected live snapshot: %#v err=%v", live, err)
	}
	current = current.Add(2 * time.Second)
	lkg, err := router.RouteAllWithKey(context.Background(), "gpt-route-plan", "request-lkg")
	if err != nil || lkg[0].SnapshotSource != "last_known_good" || !lkg[0].SnapshotDegraded {
		t.Fatalf("expected bounded last-known-good snapshot: %#v err=%v", lkg, err)
	}
	current = current.Add(2 * time.Second)
	if _, err := router.RouteAllWithKey(context.Background(), "gpt-route-plan", "request-closed"); err == nil || !strings.Contains(err.Error(), "postgres unavailable") {
		t.Fatalf("stale snapshot did not fail closed: %v", err)
	}
}

func TestP6F08PreviewExplainsExclusionsWithoutCredentials(t *testing.T) {
	dbNow := time.Date(2026, 7, 12, 3, 0, 0, 0, time.UTC)
	cooldown := dbNow.Add(time.Minute)
	db := &testutil.MockDB{QueryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
		return routePlanRows(
			routePlanRow{id: 1, name: "eligible", provider: "openai", baseURL: "https://secret-host.invalid", priority: 10, weight: 100, channelStatus: "active", circuitState: "healthy", degradationThreshold: 4, keyID: 11, keySecret: "encrypted-provider-secret", keyStatus: "active", dbNow: dbNow},
			routePlanRow{id: 2, name: "disabled", provider: "azure", baseURL: "https://disabled.invalid", priority: 10, weight: 100, channelStatus: "disabled", circuitState: "healthy", degradationThreshold: 4, keyID: 22, keySecret: "other-secret", keyStatus: "active", dbNow: dbNow},
			routePlanRow{id: 3, name: "cooling", provider: "anthropic", baseURL: "https://cooling.invalid", priority: 10, weight: 100, channelStatus: "active", circuitState: "healthy", degradationThreshold: 4, keyID: 33, keySecret: "third-secret", keyStatus: "active", keyCooldown: &cooldown, dbNow: dbNow},
			routePlanRow{id: 4, name: "no-key", provider: "custom", baseURL: "https://no-key.invalid", priority: 10, weight: 100, channelStatus: "active", circuitState: "healthy", degradationThreshold: 4, dbNow: dbNow},
			routePlanRow{id: 5, name: "wrong-model", provider: "custom", baseURL: "https://wrong-model.invalid", priority: 10, weight: 100, channelStatus: "active", circuitState: "healthy", degradationThreshold: 4, keyID: 55, keySecret: "fifth-secret", keyStatus: "active", allowedModels: "another-model", dbNow: dbNow},
		), nil
	}}
	router := NewRouter(db)
	router.now = func() time.Time { return dbNow }
	preview, err := router.Preview(context.Background(), "gpt-route-plan")
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Candidates) != 1 || len(preview.Exclusions) != 4 {
		t.Fatalf("unexpected preview: %#v", preview)
	}
	reasons := map[string]bool{}
	for _, exclusion := range preview.Exclusions {
		reasons[exclusion.Reason] = true
	}
	for _, expected := range []string{"channel_inactive", "key_cooldown", "no_key", "key_model_not_allowed"} {
		if !reasons[expected] {
			t.Fatalf("missing exclusion reason %q in %#v", expected, reasons)
		}
	}
	encoded, _ := json.Marshal(preview)
	for _, forbidden := range []string{"encrypted-provider-secret", "other-secret", "fifth-secret", "secret-host.invalid", "cooling.invalid", "wrong-model.invalid"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("routing preview leaked private route material %q: %s", forbidden, encoded)
		}
	}
}

func TestP6F08SnapshotCacheAndModelInputAreBounded(t *testing.T) {
	current := time.Date(2026, 7, 12, 3, 0, 0, 0, time.UTC)
	row := routePlanRow{id: 1, name: "bounded", provider: "openai", baseURL: "https://bounded.invalid", priority: 10, weight: 100, channelStatus: "active", circuitState: "healthy", degradationThreshold: 4, keyID: 11, keySecret: "secret", keyStatus: "active", dbNow: current}
	queryCount := 0
	db := &testutil.MockDB{QueryFn: func(context.Context, string, ...any) (pgx.Rows, error) {
		queryCount++
		row.dbNow = current
		return routePlanRows(row), nil
	}}
	policy := routingpolicy.Default()
	policy.MaxSnapshotCount = 2
	router, err := NewRouterWithPolicy(db, policy)
	if err != nil {
		t.Fatal(err)
	}
	router.now = func() time.Time { return current }
	for _, model := range []string{"model-one", "model-two", "model-three"} {
		if _, err := router.RouteAllWithKey(context.Background(), model, "request-"+model); err != nil {
			t.Fatalf("route %s: %v", model, err)
		}
		current = current.Add(time.Second)
	}
	if len(router.cache) != 2 || router.cache["model-one"] != nil {
		t.Fatalf("snapshot cache was not oldest-first bounded: %#v", router.cache)
	}
	if _, err := router.RouteAllWithKey(context.Background(), strings.Repeat("x", maxRoutingModelLength+1), "long-model"); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("overlong model did not fail before query: %v", err)
	}
	if queryCount != 3 {
		t.Fatalf("overlong model reached PostgreSQL: query_count=%d", queryCount)
	}
}
