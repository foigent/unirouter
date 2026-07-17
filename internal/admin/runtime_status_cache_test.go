package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/auth"
	"github.com/uniroute/unirouter/internal/testutil"
)

func TestRuntimeStatusUsesBoundedSnapshot(t *testing.T) {
	var queryCalls atomic.Int32
	database := &testutil.MockDB{
		QueryRowFn: func(_ context.Context, query string, _ ...any) pgx.Row {
			queryCalls.Add(1)
			if strings.Contains(query, "SELECT 1") {
				return &testutil.MockRow{ScanFn: func(dest ...any) error {
					*(dest[0].(*int)) = 1
					return nil
				}}
			}
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*(dest[0].(*int64)) = 0
				*(dest[1].(*int64)) = 0
				*(dest[2].(*int64)) = 0
				*(dest[3].(*int64)) = 0
				*(dest[4].(*float64)) = 0
				*(dest[5].(*float64)) = 0
				return nil
			}}
		},
	}
	h := NewHandler(database, "jwt-secret", []byte("0123456789abcdef01234567"), nil)
	h.runtimeHostTelemetryProvider = func() runtimeHostTelemetry { return runtimeHostTelemetry{} }

	responses := make([]runtimeStatusResponse, 0, 2)
	for range 2 {
		req := withTestBillingActor(httptest.NewRequest(http.MethodGet, "/api/v1/runtime/status", nil))
		rec := httptest.NewRecorder()
		h.RuntimeStatus(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
		}
		var response runtimeStatusResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		responses = append(responses, response)
	}

	if got := queryCalls.Load(); got != 2 {
		t.Fatalf("two reads inside the snapshot TTL made %d database queries; want one collection (2 queries)", got)
	}
	if !responses[0].GeneratedAt.Equal(responses[1].GeneratedAt) {
		t.Fatalf("two reads inside the snapshot TTL used different snapshots: %s and %s", responses[0].GeneratedAt, responses[1].GeneratedAt)
	}
}

func TestRuntimeStatusCoalescesConcurrentRefreshAndReportsFreshness(t *testing.T) {
	baseTime := time.Date(2026, time.July, 17, 9, 0, 0, 0, time.UTC)
	var clockNanos atomic.Int64
	clockNanos.Store(baseTime.UnixNano())
	now := func() time.Time { return time.Unix(0, clockNanos.Load()).UTC() }

	h := NewHandler(nil, "jwt-secret", []byte("0123456789abcdef01234567"), nil)
	h.runtimeStatusCache.ttl = 5 * time.Second
	h.runtimeStatusCache.now = now
	var collectorCalls atomic.Int32
	collectorEntered := make(chan struct{}, 1)
	releaseCollector := make(chan struct{})
	h.runtimeStatusCollector = func(ctx context.Context, _ int64) (runtimeStatusResponse, error) {
		collectorCalls.Add(1)
		select {
		case collectorEntered <- struct{}{}:
		default:
		}
		select {
		case <-releaseCollector:
			return runtimeStatusResponse{Status: "operational"}, nil
		case <-ctx.Done():
			return runtimeStatusResponse{}, ctx.Err()
		}
	}

	const requestCount = 4
	start := make(chan struct{})
	results := make(chan runtimeStatusTestResult, requestCount)
	for range requestCount {
		go func() {
			<-start
			results <- invokeRuntimeStatus(h, "/api/v1/runtime/status", auth.RoleBillingAdmin)
		}()
	}
	close(start)
	waitForRuntimeStatusSignal(t, collectorEntered, "first runtime collector entry")
	close(releaseCollector)

	var generatedAt time.Time
	for range requestCount {
		result := <-results
		if result.code != http.StatusOK {
			t.Fatalf("concurrent runtime status returned %d body=%s", result.code, result.body)
		}
		if generatedAt.IsZero() {
			generatedAt = result.response.GeneratedAt
		} else if !generatedAt.Equal(result.response.GeneratedAt) {
			t.Fatalf("concurrent reads did not share a snapshot: %s != %s", generatedAt, result.response.GeneratedAt)
		}
	}
	if got := collectorCalls.Load(); got != 1 {
		t.Fatalf("four concurrent same-org reads invoked collector %d times; want 1", got)
	}
	if !generatedAt.Equal(baseTime) {
		t.Fatalf("snapshot generation time = %s; want injected time %s", generatedAt, baseTime)
	}

	clockNanos.Add(int64(3 * time.Second))
	cached := invokeRuntimeStatus(h, "/api/v1/runtime/status", auth.RoleBillingAdmin)
	if cached.code != http.StatusOK || cached.response.DataFreshnessSeconds != 3 {
		t.Fatalf("cached response code/freshness = %d/%d; want 200/3 body=%s", cached.code, cached.response.DataFreshnessSeconds, cached.body)
	}
	if got := collectorCalls.Load(); got != 1 {
		t.Fatalf("fresh cache entry invoked collector again: %d", got)
	}

	clockNanos.Add(int64(3 * time.Second))
	refreshed := invokeRuntimeStatus(h, "/api/v1/runtime/status", auth.RoleBillingAdmin)
	if refreshed.code != http.StatusOK {
		t.Fatalf("post-expiry refresh returned %d body=%s", refreshed.code, refreshed.body)
	}
	if got := collectorCalls.Load(); got != 2 {
		t.Fatalf("post-expiry read invoked collector %d times total; want 2", got)
	}
	if !refreshed.response.GeneratedAt.Equal(baseTime.Add(6*time.Second)) || refreshed.response.DataFreshnessSeconds != 0 {
		t.Fatalf("unexpected refreshed timestamp/freshness: %#v", refreshed.response)
	}
}

func TestRuntimeStatusSnapshotCacheBoundsOrganizationKeys(t *testing.T) {
	baseTime := time.Date(2026, time.July, 17, 9, 0, 0, 0, time.UTC)
	var clockNanos atomic.Int64
	clockNanos.Store(baseTime.UnixNano())
	cache := &runtimeStatusSnapshotCache{
		ttl:                time.Minute,
		maxEntries:         2,
		refreshConcurrency: 1,
		now:                func() time.Time { return time.Unix(0, clockNanos.Load()).UTC() },
	}
	var callsMu sync.Mutex
	callsByOrg := make(map[int64]int)
	collector := func(_ context.Context, orgID int64) (runtimeStatusResponse, error) {
		callsMu.Lock()
		callsByOrg[orgID]++
		callsMu.Unlock()
		return runtimeStatusResponse{
			Status: "operational",
			CapacityHealth: runtimeCapacityHealth{
				Traffic: runtimeTrafficStatus{Requests: orgID},
			},
		}, nil
	}

	for orgID := int64(1); orgID <= 3; orgID++ {
		response, err := cache.get(context.Background(), orgID, collector)
		if err != nil {
			t.Fatalf("collect org %d: %v", orgID, err)
		}
		if response.CapacityHealth.Traffic.Requests != orgID {
			t.Fatalf("org %d received another organization's traffic snapshot: %#v", orgID, response.CapacityHealth.Traffic)
		}
		clockNanos.Add(int64(time.Second))
	}

	cache.mu.RLock()
	_, hasOrg1 := cache.entries[1]
	_, hasOrg2 := cache.entries[2]
	_, hasOrg3 := cache.entries[3]
	entryCount := len(cache.entries)
	cache.mu.RUnlock()
	if entryCount != 2 || hasOrg1 || !hasOrg2 || !hasOrg3 {
		t.Fatalf("bounded cache entries = %#v; want only orgs 2 and 3", cache.entries)
	}

	if _, err := cache.get(context.Background(), 1, collector); err != nil {
		t.Fatalf("refresh evicted org 1: %v", err)
	}
	callsMu.Lock()
	org1Calls := callsByOrg[1]
	callsMu.Unlock()
	if org1Calls != 2 {
		t.Fatalf("evicted org 1 collector calls = %d; want 2", org1Calls)
	}
	cache.mu.RLock()
	entryCount = len(cache.entries)
	cache.mu.RUnlock()
	if entryCount != 2 {
		t.Fatalf("cache grew past configured bound: %d", entryCount)
	}
}

func TestRuntimeStatusSnapshotCacheBoundsCrossOrganizationRefreshes(t *testing.T) {
	cache := &runtimeStatusSnapshotCache{
		ttl:                time.Minute,
		maxEntries:         8,
		refreshConcurrency: 2,
	}
	collectorEntered := make(chan int64, 3)
	releaseCollector := make(chan struct{})
	var active atomic.Int32
	var maxActive atomic.Int32
	collector := func(ctx context.Context, orgID int64) (runtimeStatusResponse, error) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			observed := maxActive.Load()
			if current <= observed || maxActive.CompareAndSwap(observed, current) {
				break
			}
		}
		collectorEntered <- orgID
		select {
		case <-releaseCollector:
			return runtimeStatusResponse{Status: "operational"}, nil
		case <-ctx.Done():
			return runtimeStatusResponse{}, ctx.Err()
		}
	}

	results := make(chan error, 2)
	for orgID := int64(1); orgID <= 2; orgID++ {
		go func(id int64) {
			_, err := cache.get(context.Background(), id, collector)
			results <- err
		}(orgID)
	}
	waitForRuntimeStatusSignal(t, collectorEntered, "first cross-org collector entry")
	waitForRuntimeStatusSignal(t, collectorEntered, "second cross-org collector entry")

	if _, err := cache.get(context.Background(), 3, collector); !errors.Is(err, errRuntimeStatusRefreshSaturated) {
		t.Fatalf("third cross-org refresh error = %v; want %v", err, errRuntimeStatusRefreshSaturated)
	}
	close(releaseCollector)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("admitted cross-org refresh failed: %v", err)
		}
	}
	if _, err := cache.get(context.Background(), 3, collector); err != nil {
		t.Fatalf("refresh after slot release failed: %v", err)
	}
	if got := maxActive.Load(); got != 2 {
		t.Fatalf("maximum active cross-org collectors = %d; want 2", got)
	}
}

func TestRuntimeStatusSnapshotWaitHonorsCancellation(t *testing.T) {
	cache := &runtimeStatusSnapshotCache{ttl: time.Minute, refreshConcurrency: 1}
	collectorEntered := make(chan struct{}, 1)
	releaseCollector := make(chan struct{})
	var calls atomic.Int32
	collector := func(ctx context.Context, _ int64) (runtimeStatusResponse, error) {
		calls.Add(1)
		collectorEntered <- struct{}{}
		select {
		case <-releaseCollector:
			return runtimeStatusResponse{Status: "operational"}, nil
		case <-ctx.Done():
			return runtimeStatusResponse{}, ctx.Err()
		}
	}

	firstDone := make(chan error, 1)
	go func() {
		_, err := cache.get(context.Background(), 1, collector)
		firstDone <- err
	}()
	waitForRuntimeStatusSignal(t, collectorEntered, "shared collector entry")

	waitCtx, cancel := context.WithCancel(context.Background())
	waitDone := make(chan error, 1)
	go func() {
		_, err := cache.get(waitCtx, 1, collector)
		waitDone <- err
	}()
	cancel()
	select {
	case err := <-waitDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled waiter error = %v; want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled snapshot waiter did not return")
	}

	close(releaseCollector)
	if err := <-firstDone; err != nil {
		t.Fatalf("shared collector failed after waiter cancellation: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("waiter cancellation changed collector count to %d; want 1", got)
	}
}

func TestRuntimeStatusRefreshSaturationReturnsRetryableServiceUnavailable(t *testing.T) {
	h := NewHandler(nil, "jwt-secret", []byte("0123456789abcdef01234567"), nil)
	h.runtimeStatusCache.refreshConcurrency = 1
	collectorEntered := make(chan struct{}, 1)
	releaseCollector := make(chan struct{})
	h.runtimeStatusCollector = func(ctx context.Context, _ int64) (runtimeStatusResponse, error) {
		collectorEntered <- struct{}{}
		select {
		case <-releaseCollector:
			return runtimeStatusResponse{Status: "operational"}, nil
		case <-ctx.Done():
			return runtimeStatusResponse{}, ctx.Err()
		}
	}

	firstResult := make(chan runtimeStatusTestResult, 1)
	go func() {
		firstResult <- invokeRuntimeStatus(h, "/api/v1/runtime/status?org_id=1", auth.RolePlatformAdmin)
	}()
	waitForRuntimeStatusSignal(t, collectorEntered, "first handler collector entry")

	saturated := invokeRuntimeStatus(h, "/api/v1/runtime/status?org_id=2", auth.RolePlatformAdmin)
	if saturated.code != http.StatusServiceUnavailable || saturated.header.Get("Retry-After") != "1" {
		t.Fatalf("saturated response = %d Retry-After=%q body=%s", saturated.code, saturated.header.Get("Retry-After"), saturated.body)
	}
	if saturated.body != "{\"error\":\"Runtime status temporarily unavailable\"}\n" {
		t.Fatalf("unexpected saturated response body: %s", saturated.body)
	}

	close(releaseCollector)
	if first := <-firstResult; first.code != http.StatusOK {
		t.Fatalf("admitted refresh returned %d body=%s", first.code, first.body)
	}
	retry := invokeRuntimeStatus(h, "/api/v1/runtime/status?org_id=2", auth.RolePlatformAdmin)
	if retry.code != http.StatusOK {
		t.Fatalf("retry after slot release returned %d body=%s", retry.code, retry.body)
	}
}

func TestMonitoringGetRejectsOversizedResponseBodies(t *testing.T) {
	oversizedBody := bytes.Repeat([]byte("x"), runtimeMonitoringMaxBodyBytes+1)
	streamedClient := &http.Client{Transport: runtimeStatusRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Header:        make(http.Header),
			Body:          io.NopCloser(bytes.NewReader(oversizedBody)),
			ContentLength: -1,
			Request:       req,
		}, nil
	})}
	if _, err := monitoringGetWithClient(context.Background(), streamedClient, "http://monitor.invalid/api/health"); !errors.Is(err, errMonitoringResponseTooLarge) {
		t.Fatalf("streamed oversized response error = %v; want %v", err, errMonitoringResponseTooLarge)
	}
	if got := monitoringFailureReason(errMonitoringResponseTooLarge, "probe_failed"); got != "response_too_large" {
		t.Fatalf("oversized response reason = %q", got)
	}

	trackingBody := &runtimeStatusTrackingBody{reader: strings.NewReader("{}")}
	advertisedClient := &http.Client{Transport: runtimeStatusRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Header:        make(http.Header),
			Body:          trackingBody,
			ContentLength: runtimeMonitoringMaxBodyBytes + 1,
			Request:       req,
		}, nil
	})}
	if _, err := monitoringGetWithClient(context.Background(), advertisedClient, "http://monitor.invalid/api/health"); !errors.Is(err, errMonitoringResponseTooLarge) {
		t.Fatalf("advertised oversized response error = %v; want %v", err, errMonitoringResponseTooLarge)
	}
	if trackingBody.reads.Load() != 0 || trackingBody.closes.Load() != 1 {
		t.Fatalf("advertised oversized body reads/closes = %d/%d; want 0/1", trackingBody.reads.Load(), trackingBody.closes.Load())
	}

	validBody := []byte(`{"database":"ok"}`)
	validClient := &http.Client{Transport: runtimeStatusRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Header:        make(http.Header),
			Body:          io.NopCloser(bytes.NewReader(validBody)),
			ContentLength: int64(len(validBody)),
			Request:       req,
		}, nil
	})}
	result, err := monitoringGetWithClient(context.Background(), validClient, "http://monitor.invalid/api/health")
	if err != nil {
		t.Fatalf("valid monitoring response failed: %v", err)
	}
	if result.StatusCode != http.StatusOK || !bytes.Equal(result.Body, validBody) {
		t.Fatalf("valid monitoring result = %#v", result)
	}
}

type runtimeStatusTestResult struct {
	code     int
	header   http.Header
	body     string
	response runtimeStatusResponse
}

func invokeRuntimeStatus(h *Handler, target, role string) runtimeStatusTestResult {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req = withTestActor(req, 1, 1, role)
	rec := httptest.NewRecorder()
	h.RuntimeStatus(rec, req)
	result := runtimeStatusTestResult{code: rec.Code, header: rec.Header().Clone(), body: rec.Body.String()}
	if rec.Code == http.StatusOK {
		_ = json.Unmarshal(rec.Body.Bytes(), &result.response)
	}
	return result
}

func waitForRuntimeStatusSignal[T any](t *testing.T, signal <-chan T, description string) T {
	t.Helper()
	select {
	case value := <-signal:
		return value
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
		var zero T
		return zero
	}
}

type runtimeStatusRoundTripFunc func(*http.Request) (*http.Response, error)

func (f runtimeStatusRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type runtimeStatusTrackingBody struct {
	reader io.Reader
	reads  atomic.Int32
	closes atomic.Int32
}

func (b *runtimeStatusTrackingBody) Read(p []byte) (int, error) {
	b.reads.Add(1)
	return b.reader.Read(p)
}

func (b *runtimeStatusTrackingBody) Close() error {
	b.closes.Add(1)
	return nil
}
