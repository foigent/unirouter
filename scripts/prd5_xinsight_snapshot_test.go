//go:build ignore

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/uniroute/unirouter/internal/snapshotpublish"
)

type snapshotRowScannerFunc func(dest ...any) error

func (f snapshotRowScannerFunc) Scan(dest ...any) error {
	return f(dest...)
}

func TestLoadApprovalManifestHashesExactBytes(t *testing.T) {
	payload := []byte(`{"schema_version":"unirouter.prd5.xinsight.approval.v1","source_instance":"gate-a","requests":[{"scenario_label":"successful_non_stream","request_id":"req-1","approval_status":"approved","required":true}]}
`)
	path := filepath.Join(t.TempDir(), "approval.json")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	manifest, digest, err := loadApprovalManifest(path)
	if err != nil {
		t.Fatalf("loadApprovalManifest: %v", err)
	}
	if manifest.SourceInstance != "gate-a" {
		t.Fatalf("source_instance=%q", manifest.SourceInstance)
	}
	const expectedDigest = "sha256:b57c4809fce1d16f56c1688251c92779fd023a2309141f61d0134d5983b414c6"
	if digest != expectedDigest {
		t.Fatalf("digest=%q want=%q", digest, expectedDigest)
	}
	if !strings.HasPrefix(digest, "sha256:") || len(digest) != len("sha256:")+64 {
		t.Fatalf("unexpected digest format: %q", digest)
	}
}

func TestLoadApprovalManifestRejectsTrailingJSONObject(t *testing.T) {
	path := filepath.Join(t.TempDir(), "approval.json")
	payload := []byte(`{"schema_version":"unirouter.prd5.xinsight.approval.v1","requests":[]} {}`)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, err := loadApprovalManifest(path)
	if err == nil || !strings.Contains(err.Error(), "exactly one JSON object") {
		t.Fatalf("expected trailing object rejection, got %v", err)
	}
}

func TestLoadApprovalManifestRejectsDuplicateKeysAtEveryObjectDepthWithoutEcho(t *testing.T) {
	for _, payload := range []string{
		`{"schema_version":"unirouter.prd5.xinsight.approval.v1","source_instance":"internal-dev","source_instance":"private-source-canary","requests":[]}`,
		`{"schema_version":"unirouter.prd5.xinsight.approval.v1","source_instance":"internal-dev","requests":[{"scenario_label":"success","request_id":"req-1","request_id":"private-request-canary","approval_status":"approved","required":true}]}`,
	} {
		path := filepath.Join(t.TempDir(), "approval.json")
		if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
			t.Fatal(err)
		}
		_, _, err := loadApprovalManifest(path)
		if err == nil || !strings.Contains(err.Error(), "duplicate_key") {
			t.Fatalf("expected duplicate-key rejection, got %v", err)
		}
		for _, canary := range []string{"private-source-canary", "private-request-canary"} {
			if strings.Contains(err.Error(), canary) {
				t.Fatalf("duplicate value leaked: %v", err)
			}
		}
	}
}

func TestRunSnapshotRejectsManifestSourceInstanceOverrideBeforeDSN(t *testing.T) {
	path := filepath.Join(t.TempDir(), "approval.json")
	payload := []byte(`{"schema_version":"unirouter.prd5.xinsight.approval.v1","source_instance":"approved-gate-a","requests":[{"scenario_label":"successful_non_stream","request_id":"req-1","approval_status":"approved","required":true}]}`)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	err := runPRD5Snapshot([]string{
		"--approval-manifest", path,
		"--source-instance", "different-environment",
	})
	if err == nil || !strings.Contains(err.Error(), "source_instance_mismatch") {
		t.Fatalf("expected source instance mismatch before DSN lookup, got %v", err)
	}
}

func TestRunSnapshotRequiresManifestSourceInstanceBeforeDSN(t *testing.T) {
	path := filepath.Join(t.TempDir(), "approval.json")
	payload := []byte(`{"schema_version":"unirouter.prd5.xinsight.approval.v1","requests":[{"scenario_label":"successful_non_stream","request_id":"req-1","approval_status":"approved","required":true}]}`)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	err := runPRD5Snapshot([]string{"--approval-manifest", path})
	if err == nil || !strings.Contains(err.Error(), "strict_manifest_invalid: source_instance") {
		t.Fatalf("expected missing source instance rejection before DSN lookup, got %v", err)
	}
}

func TestNewExchangeIDFormatAndUniqueness(t *testing.T) {
	pattern := regexp.MustCompile(`^uxchg_[0-9a-f]{32}$`)
	seen := map[string]bool{}
	for i := 0; i < 128; i++ {
		value, err := newExchangeID()
		if err != nil {
			t.Fatalf("newExchangeID: %v", err)
		}
		if !pattern.MatchString(value) {
			t.Fatalf("unexpected exchange_id: %q", value)
		}
		if seen[value] {
			t.Fatalf("duplicate exchange_id: %q", value)
		}
		seen[value] = true
	}
}

func TestSnapshotEnvelopeJSONOmitsManifestAndArtifactHashesInDirectMode(t *testing.T) {
	payload, err := json.Marshal(snapshotFile{
		SchemaVersion:  prd5SnapshotSchemaVersion,
		ExchangeID:     "uxchg_0123456789abcdef0123456789abcdef",
		Source:         "unirouter",
		SourceInstance: "gate-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(payload)
	if !strings.Contains(encoded, `"exchange_id":"uxchg_0123456789abcdef0123456789abcdef"`) {
		t.Fatalf("missing exchange_id: %s", encoded)
	}
	if strings.Contains(encoded, "approval_manifest_sha256") || strings.Contains(encoded, "artifact_sha256") {
		t.Fatalf("unexpected hash in direct-mode snapshot envelope: %s", encoded)
	}
}

func TestSnapshotV2SerializesExactMicroCNYContract(t *testing.T) {
	if prd5SnapshotSchemaVersion != "unirouter.prd5.xinsight.snapshot.v2" {
		t.Fatalf("snapshot schema=%q", prd5SnapshotSchemaVersion)
	}
	query := callLogsQuery(2)
	if !strings.Contains(query, "cost_micros::text") || strings.Contains(query, "cost_cents") || !strings.Contains(query, "IN ($1, $2)") {
		t.Fatalf("unexpected call-log query: %s", query)
	}

	row, err := scanSnapshotCallLog(snapshotCallLogScanner(t, "7"), "synthetic-local")
	if err != nil {
		t.Fatal(err)
	}
	if row.CostMicros != "7" || row.Currency != "CNY" || row.AmountScale != 1_000_000 || row.MoneySchema != "micro-cny.v1" {
		t.Fatalf("mapped money contract=%#v", row)
	}
	payload, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["cost_micros"] != "7" || decoded["currency"] != "CNY" || decoded["amount_scale"] != float64(1_000_000) || decoded["money_schema"] != "micro-cny.v1" {
		t.Fatalf("money contract=%#v", decoded)
	}
	if _, exists := decoded["cost_cents"]; exists {
		t.Fatalf("snapshot v2 must not serialize cost_cents: %#v", decoded)
	}
}

func TestStrictIPPresenceProjectionReturnsBooleanOnly(t *testing.T) {
	callQuery := strictCallLogsQuery(1)
	const detailProjection = "ip_address IS NOT NULL AND ip_address <> '' AS ip_address_present"
	if strings.Count(callQuery, detailProjection) != 1 || strings.Contains(callQuery, "ip_address::") || strings.Contains(callQuery, "ip_address AS") {
		t.Fatalf("strict detail query does not reduce IP to one presence boolean: %s", callQuery)
	}

	aggregateQuery := strictAggregateQuery(1)
	const aggregateProjection = "(cl.ip_address IS NOT NULL AND cl.ip_address <> '') AS ip_address_present"
	if strings.Count(aggregateQuery, aggregateProjection) != 1 ||
		strings.Count(aggregateQuery, "COUNT(r.id) FILTER (WHERE r.ip_address_present)") != 1 ||
		strings.Contains(aggregateQuery, "cl.ip_address::") || strings.Contains(aggregateQuery, "cl.ip_address AS") {
		t.Fatalf("strict aggregate query does not reduce IP to a bounded presence count: %s", aggregateQuery)
	}

	values := validCallLogScanValues(1, "req-1", 7)
	values[23] = true
	row, err := scanSnapshotCallLogStrict(snapshotRowScannerFunc(func(dest ...any) error {
		if len(dest) <= 23 {
			t.Fatalf("scanner destination count=%d", len(dest))
		}
		if _, ok := dest[23].(*bool); !ok {
			t.Fatalf("IP presence destination type=%T want *bool", dest[23])
		}
		return assignFakeScan(values, dest)
	}), "internal-dev")
	if err != nil {
		t.Fatal(err)
	}
	if !row.IPAddressWasPresent || !row.IPAddressRedacted {
		t.Fatalf("presence proof=%t/%t", row.IPAddressWasPresent, row.IPAddressRedacted)
	}
}

func TestStrictCallLogScanFailureUsesSafeNonEchoingError(t *testing.T) {
	const canary = "private-source-value-canary"
	_, err := scanSnapshotCallLogStrict(snapshotRowScannerFunc(func(...any) error {
		return errors.New("database conversion failed for " + canary)
	}), "internal-dev")
	if err == nil || err.Error() != "strict_source_read_failed: call_logs row" {
		t.Fatalf("unexpected strict scan error: %v", err)
	}
	if strings.Contains(err.Error(), canary) {
		t.Fatalf("source value leaked: %v", err)
	}
}

func TestSnapshotV2RejectsNegativeMicroCNYAtProducerBoundary(t *testing.T) {
	_, err := scanSnapshotCallLog(snapshotCallLogScanner(t, "-1"), "synthetic-local")
	if err == nil || !strings.Contains(err.Error(), "canonical non-negative integer text") {
		t.Fatalf("expected negative cost rejection, got %v", err)
	}
}

func snapshotCallLogScanner(t *testing.T, costMicros string) snapshotRowScannerFunc {
	t.Helper()
	return func(dest ...any) error {
		if len(dest) != 38 {
			t.Fatalf("scan destinations=%d want=38", len(dest))
		}
		*dest[0].(*int64) = 41
		*dest[1].(*string) = "req-v2-contract"
		*dest[2].(*int64) = 7
		*dest[6].(*string) = "personal"
		*dest[7].(*string) = "synthetic-model"
		*dest[8].(*string) = "synthetic-model"
		*dest[9].(*string) = "synthetic-provider"
		*dest[10].(*string) = "synthetic-channel"
		*dest[15].(*string) = "api"
		*dest[16].(*string) = "authoritative"
		*dest[17].(*string) = costMicros
		*dest[25].(*time.Time) = time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
		return nil
	}
}

func TestFormatHandoffNotePreservesApprovalManifestMode(t *testing.T) {
	snapshot := snapshotFile{
		SchemaVersion:          prd5SnapshotSchemaVersion,
		ExchangeID:             "uxchg_0123456789abcdef0123456789abcdef",
		ApprovalManifestSHA256: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		GeneratedAt:            "2026-07-10T00:00:00Z",
		Source:                 "unirouter",
		SourceInstance:         "gate-a",
		RequestIDs:             []string{"req-1"},
		Scenarios:              []snapshotScenario{{Label: "successful_non_stream", RequestID: "req-1"}},
		Safety:                 snapshotSafety{SelectionMode: "approved_request_id_manifest"},
	}

	note := formatHandoffNote("snapshot.json", "handoff.md", "approval.json", false, snapshot)
	for _, expected := range []string{
		"Exchange ID: " + snapshot.ExchangeID,
		"Approval Manifest SHA-256: " + snapshot.ApprovalManifestSHA256,
		"--approval-manifest 'approval.json'",
		"(exchange_id, artifact.sha256)",
	} {
		if !strings.Contains(note, expected) {
			t.Fatalf("handoff note missing %q:\n%s", expected, note)
		}
	}
	if strings.Contains(note, "  --request-id ") || strings.Contains(note, "  --scenario ") {
		t.Fatalf("manifest handoff command widened to direct selection:\n%s", note)
	}
}

type fakeSnapshotRows struct {
	values [][]any
	next   int
	err    error
}

func (r *fakeSnapshotRows) Next() bool {
	if r.next >= len(r.values) {
		return false
	}
	r.next++
	return true
}

func (r *fakeSnapshotRows) Scan(dest ...any) error {
	if r.next == 0 || r.next > len(r.values) {
		return errors.New("fake rows scan outside active row")
	}
	return assignFakeScan(r.values[r.next-1], dest)
}

func (r *fakeSnapshotRows) Err() error   { return r.err }
func (r *fakeSnapshotRows) Close() error { return nil }

func assignFakeScan(values []any, dest []any) error {
	if len(values) != len(dest) {
		return fmt.Errorf("fake scan values=%d destinations=%d", len(values), len(dest))
	}
	for idx, value := range values {
		target := reflect.ValueOf(dest[idx])
		if target.Kind() != reflect.Ptr || target.IsNil() {
			return fmt.Errorf("fake scan destination %d is not a pointer", idx)
		}
		if value == nil {
			target.Elem().Set(reflect.Zero(target.Elem().Type()))
			continue
		}
		source := reflect.ValueOf(value)
		if !source.Type().AssignableTo(target.Elem().Type()) {
			return fmt.Errorf("fake scan value %d type=%s target=%s", idx, source.Type(), target.Elem().Type())
		}
		target.Elem().Set(source)
	}
	return nil
}

type fakeStrictQueryResult struct {
	contains string
	rows     snapshotRows
	err      error
}

type fakeStrictTx struct {
	isolation     string
	readOnly      string
	modeErr       error
	queryResults  []fakeStrictQueryResult
	queryIndex    int
	queries       []string
	commitErr     error
	commitCalls   int
	rollbackCalls int
}

func (t *fakeStrictTx) QueryContext(_ context.Context, query string, _ ...any) (snapshotRows, error) {
	t.queries = append(t.queries, query)
	if t.queryIndex >= len(t.queryResults) {
		return nil, errors.New("unexpected fake query")
	}
	result := t.queryResults[t.queryIndex]
	t.queryIndex++
	if result.contains != "" && !strings.Contains(query, result.contains) {
		return nil, fmt.Errorf("fake query %d missing marker %q", t.queryIndex, result.contains)
	}
	return result.rows, result.err
}

func (t *fakeStrictTx) QueryRowContext(_ context.Context, query string, _ ...any) snapshotRowScanner {
	t.queries = append(t.queries, query)
	return snapshotRowScannerFunc(func(dest ...any) error {
		if t.modeErr != nil {
			return t.modeErr
		}
		return assignFakeScan([]any{t.isolation, t.readOnly}, dest)
	})
}

func (t *fakeStrictTx) Commit() error {
	t.commitCalls++
	return t.commitErr
}

func (t *fakeStrictTx) Rollback() error {
	t.rollbackCalls++
	return nil
}

type fakeStrictDB struct {
	tx           *fakeStrictTx
	beginErr     error
	beginOptions sql.TxOptions
	beginCalls   int
	closeCalls   int
}

func (d *fakeStrictDB) BeginStrict(_ context.Context, options *sql.TxOptions) (strictReadTx, error) {
	d.beginCalls++
	if options != nil {
		d.beginOptions = *options
	}
	if d.beginErr != nil {
		return nil, d.beginErr
	}
	return d.tx, nil
}

func (d *fakeStrictDB) Close() error {
	d.closeCalls++
	return nil
}

func exactSchemaRows() [][]any {
	const contract = `
call_logs.id:int8:true
call_logs.request_id:varchar:true
call_logs.org_id:int8:true
call_logs.project_id:int8:false
call_logs.api_key_id:int8:false
call_logs.owner_user_id:int8:false
call_logs.key_type:varchar:true
call_logs.model:varchar:true
call_logs.requested_model:varchar:true
call_logs.provider:varchar:true
call_logs.channel:varchar:true
call_logs.prompt_tokens:int4:true
call_logs.completion_tokens:int4:true
call_logs.total_tokens:int4:true
call_logs.estimated_tokens:int4:true
call_logs.usage_source:varchar:true
call_logs.usage_confidence:numeric(4,3):true
call_logs.cost_micros:int8:true
call_logs.latency_ms:int4:true
call_logs.ttft_ms:int4:true
call_logs.status_code:int4:true
call_logs.upstream_status_code:int4:false
call_logs.upstream_error:text:false
call_logs.ip_address:varchar:true
call_logs.is_stream:bool:true
call_logs.created_at:timestamptz:true
call_logs.trace_id:varchar:false
call_logs.session_id:varchar:false
call_logs.upstream_request_id:varchar:false
call_logs.schema_version:varchar:true
call_logs.capture_mode:varchar:true
call_logs.evidence_ref:varchar:false
call_logs.source_event_hash:varchar:false
call_logs.actor_type:varchar:false
call_logs.actor_ref:varchar:false
call_logs.on_behalf_of:varchar:false
call_logs.payload_capture_mode:varchar:true
call_logs.payload_ref:varchar:false
request_trace_events.id:int8:true
request_trace_events.request_id:varchar:true
request_trace_events.org_id:int8:true
request_trace_events.project_id:int8:false
request_trace_events.api_key_id:int8:false
request_trace_events.owner_user_id:int8:false
request_trace_events.key_type:varchar:true
request_trace_events.event_type:varchar:true
request_trace_events.stage:varchar:true
request_trace_events.severity:varchar:true
request_trace_events.model:varchar:true
request_trace_events.requested_model:varchar:true
request_trace_events.provider:varchar:true
request_trace_events.channel:varchar:true
request_trace_events.status_code:int4:false
request_trace_events.latency_ms:int4:true
request_trace_events.metadata:jsonb:true
request_trace_events.created_at:timestamptz:true
request_trace_events.trace_id:varchar:false
request_trace_events.span_id:varchar:false
request_trace_events.parent_span_id:varchar:false
request_trace_events.schema_version:varchar:true
request_trace_events.source_component:varchar:true
request_trace_events.capture_mode:varchar:true
request_trace_events.capture_gap:varchar:false
request_trace_events.evidence_ref:varchar:false
request_trace_events.upstream_request_id:varchar:false
request_trace_events.decision_kind:varchar:false
request_trace_events.decision_result:varchar:false
request_trace_events.decision_reason:varchar:false
request_trace_events.actor_type:varchar:false
request_trace_events.actor_ref:varchar:false
request_trace_events.on_behalf_of:varchar:false
request_trace_events.session_id:varchar:false
request_trace_events.payload_capture_mode:varchar:true
request_trace_events.payload_ref:varchar:false
request_trace_events.source_event_hash:varchar:false
`
	rows := make([][]any, 0, 75)
	for _, line := range strings.Fields(contract) {
		parts := strings.Split(line, ":")
		relationColumn := strings.Split(parts[0], ".")
		rows = append(rows, []any{relationColumn[0], relationColumn[1], parts[1], parts[2] == "true"})
	}
	return rows
}

func validCallLogScanValues(id int64, requestID string, orgID int64) []any {
	return []any{
		id, requestID, orgID,
		sql.NullInt64{}, sql.NullInt64{}, sql.NullInt64{},
		"service", "model-v1", "model-v1", "provider-v1", "channel-v1",
		int64(1), int64(1), int64(2), int64(0), "upstream", "1.000", "7",
		int64(20), int64(5), int64(200), sql.NullInt64{}, false, false, false,
		time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
		sql.NullString{}, sql.NullString{}, sql.NullString{},
		"unirouter.evidence.v1", "native", sql.NullString{}, sql.NullString{},
		sql.NullString{}, sql.NullString{}, sql.NullString{}, "metadata_only", sql.NullString{},
	}
}

func validTraceEventScanValues(id int64, requestID string, orgID int64) []any {
	return []any{
		id, requestID, orgID,
		sql.NullInt64{}, sql.NullInt64{}, sql.NullInt64{},
		"service", "provider.complete", "provider", "info",
		"model-v1", "model-v1", "provider-v1", "channel-v1",
		sql.NullInt64{Int64: 200, Valid: true}, int64(20), "{}",
		time.Date(2026, 7, 20, 0, 0, 1, 0, time.UTC),
		sql.NullString{}, sql.NullString{}, sql.NullString{},
		"unirouter.evidence.v1", "provider", "native",
		sql.NullString{}, sql.NullString{}, sql.NullString{},
		sql.NullString{}, sql.NullString{}, sql.NullString{},
		sql.NullString{}, sql.NullString{}, sql.NullString{}, sql.NullString{},
		"metadata_only", sql.NullString{}, sql.NullString{},
	}
}

func validStrictDBForRequests(requestIDs []string) *fakeStrictDB {
	aggregateRows := make([][]any, 0, len(requestIDs))
	callRows := make([][]any, 0, len(requestIDs))
	traceRows := make([][]any, 0, len(requestIDs))
	for idx, requestID := range requestIDs {
		aggregateRows = append(aggregateRows, []any{
			idx + 1, int64(1), int64(1), int64(1), sql.NullInt64{Int64: 7, Valid: true}, int64(0), int64(0), int64(0),
		})
		callRows = append(callRows, validCallLogScanValues(int64(idx+1), requestID, 7))
		traceRows = append(traceRows, validTraceEventScanValues(int64(idx+101), requestID, 7))
	}
	tx := &fakeStrictTx{
		isolation: "repeatable read",
		readOnly:  "on",
		queryResults: []fakeStrictQueryResult{
			{contains: "pg_catalog.pg_class", rows: &fakeSnapshotRows{values: exactSchemaRows()}},
			{contains: "WITH selected", rows: &fakeSnapshotRows{values: aggregateRows}},
			{contains: "FROM public.call_logs", rows: &fakeSnapshotRows{values: callRows}},
			{contains: "FROM public.request_trace_events", rows: &fakeSnapshotRows{values: traceRows}},
		},
	}
	return &fakeStrictDB{tx: tx}
}

func strictScenarios(requestIDs []string) []snapshotScenario {
	scenarios := make([]snapshotScenario, 0, len(requestIDs))
	for idx, requestID := range requestIDs {
		scenarios = append(scenarios, snapshotScenario{Label: fmt.Sprintf("scenario-%02d", idx+1), RequestID: requestID})
	}
	sort.Slice(scenarios, func(i, j int) bool {
		if scenarios[i].RequestID == scenarios[j].RequestID {
			return scenarios[i].Label < scenarios[j].Label
		}
		return scenarios[i].RequestID < scenarios[j].RequestID
	})
	return scenarios
}

func loadValidStrictSnapshotForTest(t *testing.T, db strictSnapshotDB, requestIDs []string) (snapshotFile, error) {
	t.Helper()
	return loadStrictSnapshot(
		context.Background(), db, "internal-dev", "approved_request_id_manifest",
		"uxchg_0123456789abcdef0123456789abcdef",
		"sha256:"+strings.Repeat("a", 64), requestIDs, strictScenarios(requestIDs),
		"2026-07-20T00:00:00Z",
	)
}

type fakeSnapshotStage struct {
	identity     snapshotpublish.Identity
	data         []byte
	writeLimit   int
	writeErrAt   int
	writeEINTRAt int
	zeroWriteAt  int
	writeCalls   int
	syncErr      error
	statCalls    int
	statErrAt    map[int]error
	statValueAt  map[int]snapshotpublish.Identity
	readErr      error
	corruptRead  bool
	commitErr    error
	commitCalls  int
	closeErr     error
	closeCalls   int
}

type faultInjectingSnapshotStage struct {
	snapshotpublish.Stage
	failAt    string
	statCalls int
}

type closeDiagnosticSnapshotStage struct {
	snapshotpublish.Stage
}

type pathBoundCommitSentinelStage struct {
	*fakeSnapshotStage
	destination string
}

func (s *pathBoundCommitSentinelStage) Commit() error {
	s.commitCalls++
	if s.commitErr != nil {
		return s.commitErr
	}
	return os.WriteFile(s.destination, s.data, 0o600)
}

func (s *closeDiagnosticSnapshotStage) Close() error {
	_ = s.Stage.Close()
	return errors.New("injected post-commit close diagnostic")
}

func (s *faultInjectingSnapshotStage) Write(payload []byte) (int, error) {
	if s.failAt == "write" {
		return 0, errors.New("injected write failure")
	}
	return s.Stage.Write(payload)
}

func (s *faultInjectingSnapshotStage) Sync() error {
	if s.failAt == "fsync" {
		return errors.New("injected fsync failure")
	}
	return s.Stage.Sync()
}

func (s *faultInjectingSnapshotStage) Stat() (snapshotpublish.Identity, error) {
	s.statCalls++
	if s.failAt == fmt.Sprintf("stat_%d", s.statCalls) {
		return snapshotpublish.Identity{}, errors.New("injected stat failure")
	}
	identity, err := s.Stage.Stat()
	if err == nil && s.failAt == "length" && s.statCalls == 2 {
		identity.Size++
	}
	return identity, err
}

func (s *faultInjectingSnapshotStage) ReadAt(payload []byte, offset int64) (int, error) {
	if s.failAt == "read" {
		return 0, errors.New("injected read failure")
	}
	n, err := s.Stage.ReadAt(payload, offset)
	if s.failAt == "sha" && n > 0 {
		payload[0] ^= 0xff
	}
	return n, err
}

func (s *faultInjectingSnapshotStage) Commit() error {
	if s.failAt == "link" {
		return snapshotpublish.ErrCommitFailed
	}
	return s.Stage.Commit()
}

func newFakeSnapshotStage() *fakeSnapshotStage {
	return &fakeSnapshotStage{
		identity: snapshotpublish.Identity{
			Device: 1, Inode: 2, UID: uint32(os.Geteuid()), Mode: 0o600, Nlink: 0, Size: 0, Regular: true,
		},
		statErrAt:   map[int]error{},
		statValueAt: map[int]snapshotpublish.Identity{},
	}
}

func (s *fakeSnapshotStage) Backend() string { return "fake/anonymous" }

func (s *fakeSnapshotStage) Write(payload []byte) (int, error) {
	s.writeCalls++
	if s.writeEINTRAt == s.writeCalls {
		return -1, syscall.EINTR
	}
	if s.writeErrAt == s.writeCalls {
		return 0, errors.New("injected write")
	}
	if s.zeroWriteAt == s.writeCalls {
		return 0, nil
	}
	n := len(payload)
	if s.writeLimit > 0 && n > s.writeLimit {
		n = s.writeLimit
	}
	s.data = append(s.data, payload[:n]...)
	s.identity.Size = int64(len(s.data))
	return n, nil
}

func (s *fakeSnapshotStage) ReadAt(payload []byte, offset int64) (int, error) {
	if s.readErr != nil {
		return 0, s.readErr
	}
	if offset < 0 || offset > int64(len(s.data)) {
		return 0, io.EOF
	}
	n := copy(payload, s.data[offset:])
	if s.corruptRead && n > 0 {
		payload[0] ^= 0xff
	}
	if n < len(payload) {
		return n, io.EOF
	}
	return n, nil
}

func (s *fakeSnapshotStage) Sync() error { return s.syncErr }

func (s *fakeSnapshotStage) Stat() (snapshotpublish.Identity, error) {
	s.statCalls++
	if err := s.statErrAt[s.statCalls]; err != nil {
		return snapshotpublish.Identity{}, err
	}
	if identity, ok := s.statValueAt[s.statCalls]; ok {
		return identity, nil
	}
	return s.identity, nil
}

func (s *fakeSnapshotStage) Commit() error {
	s.commitCalls++
	return s.commitErr
}

func (s *fakeSnapshotStage) Close() error {
	s.closeCalls++
	return s.closeErr
}

func writeStrictManifestFixture(t *testing.T, requests []approvalManifestRequest) string {
	return writeStrictManifestFixtureWithSource(t, "internal-dev", requests)
}

func writeStrictManifestFixtureWithSource(t *testing.T, sourceInstance string, requests []approvalManifestRequest) string {
	t.Helper()
	payload, err := json.Marshal(approvalManifest{
		SchemaVersion:  prd5ApprovalManifestSchemaVersion,
		SourceInstance: sourceInstance,
		Requests:       requests,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "approval-fixture.json")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func oneStrictManifestRequest(requestID string) approvalManifestRequest {
	return approvalManifestRequest{
		ScenarioLabel: "successful-non-stream", RequestID: requestID, ApprovalStatus: "approved", Required: true,
	}
}

func validPureStrictSnapshot(t *testing.T) (snapshotFile, []strictSourceAggregate) {
	t.Helper()
	requestIDs := []string{"req-1"}
	call, err := scanSnapshotCallLogStrict(snapshotRowScannerFunc(func(dest ...any) error {
		return assignFakeScan(validCallLogScanValues(1, requestIDs[0], 7), dest)
	}), "internal-dev")
	if err != nil {
		t.Fatal(err)
	}
	trace, err := scanSnapshotTraceEvent(snapshotRowScannerFunc(func(dest ...any) error {
		return assignFakeScan(validTraceEventScanValues(101, requestIDs[0], 7), dest)
	}), "internal-dev", true)
	if err != nil {
		t.Fatal(err)
	}
	aggregates := []strictSourceAggregate{{Ordinal: 1, CallLogs: 1, TraceEvents: 1, DistinctOrgs: 1, OrgID: 7}}
	snapshot := newStrictSnapshotFile(
		"internal-dev", "approved_request_id_manifest", "uxchg_0123456789abcdef0123456789abcdef",
		"sha256:"+strings.Repeat("a", 64), requestIDs, strictScenarios(requestIDs), "2026-07-20T00:00:00Z",
		[]snapshotCallLog{call}, []snapshotTraceEvent{trace}, nil,
	)
	return snapshot, aggregates
}

func bindingForSnapshot(snapshot snapshotFile) strictSnapshotBinding {
	return strictSnapshotBinding{
		SourceInstance: snapshot.SourceInstance, SelectionMode: snapshot.Safety.SelectionMode,
		ExchangeID: snapshot.ExchangeID, ApprovalManifestSHA256: snapshot.ApprovalManifestSHA256,
		RequestIDs: append([]string(nil), snapshot.RequestIDs...),
		Scenarios:  append([]snapshotScenario(nil), snapshot.Scenarios...),
	}
}

func sha256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return fmt.Sprintf("%x", sum[:])
}

func TestStrictSchemaPreflightAcceptsExactSnapshotV2Relations(t *testing.T) {
	tx := &fakeStrictTx{queryResults: []fakeStrictQueryResult{{rows: &fakeSnapshotRows{values: exactSchemaRows()}}}}
	if err := preflightSnapshotV2Schema(context.Background(), tx); err != nil {
		t.Fatalf("preflightSnapshotV2Schema: %v", err)
	}
	if tx.queryIndex != 1 {
		t.Fatalf("schema queries=%d want=1", tx.queryIndex)
	}
}

func TestStrictSchemaContractMatchesIndependentFixtureBidirectionally(t *testing.T) {
	type fixtureColumn struct {
		sourceType string
		notNull    bool
	}
	fixture := map[string]map[string]fixtureColumn{}
	for _, row := range exactSchemaRows() {
		relation, column := row[0].(string), row[1].(string)
		if fixture[relation] == nil {
			fixture[relation] = map[string]fixtureColumn{}
		}
		if _, duplicate := fixture[relation][column]; duplicate {
			t.Fatalf("duplicate independent fixture column %s.%s", relation, column)
		}
		fixture[relation][column] = fixtureColumn{sourceType: row[2].(string), notNull: row[3].(bool)}
	}
	if len(fixture) != len(strictSnapshotSchema) {
		t.Fatalf("relation inventory fixture=%d production=%d", len(fixture), len(strictSnapshotSchema))
	}
	for relation, fixtureColumns := range fixture {
		productionColumns, ok := strictSnapshotSchema[relation]
		if !ok || len(productionColumns) != len(fixtureColumns) {
			t.Fatalf("column inventory %s fixture=%d production=%d", relation, len(fixtureColumns), len(productionColumns))
		}
		for column, expected := range fixtureColumns {
			actual, ok := productionColumns[column]
			if !ok || len(actual.types) != 1 || !actual.types[expected.sourceType] || actual.requireNotNull != expected.notNull {
				t.Fatalf("schema contract drift %s.%s fixture=%#v production=%#v", relation, column, expected, actual)
			}
		}
	}
	for relation, productionColumns := range strictSnapshotSchema {
		fixtureColumns, ok := fixture[relation]
		if !ok {
			t.Fatalf("production relation absent from fixture: %s", relation)
		}
		for column := range productionColumns {
			if _, ok := fixtureColumns[column]; !ok {
				t.Fatalf("production column absent from fixture: %s.%s", relation, column)
			}
		}
	}
}

func TestStrictSchemaPreflightRejectsMissingRelationOrRequiredColumnBeforeSourceRead(t *testing.T) {
	base := exactSchemaRows()
	for _, tc := range []struct {
		name string
		keep func([]any) bool
		want string
	}{
		{name: "relation", keep: func(row []any) bool { return row[0].(string) != "request_trace_events" }, want: "missing_relation"},
		{name: "column", keep: func(row []any) bool { return !(row[0] == "call_logs" && row[1] == "request_id") }, want: "missing_column"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var values [][]any
			for _, row := range base {
				if tc.keep(row) {
					values = append(values, row)
				}
			}
			tx := &fakeStrictTx{queryResults: []fakeStrictQueryResult{{rows: &fakeSnapshotRows{values: values}}}}
			err := preflightSnapshotV2Schema(context.Background(), tx)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %s, got %v", tc.want, err)
			}
			if tx.queryIndex != 1 {
				t.Fatalf("unexpected source query count=%d", tx.queryIndex)
			}
		})
	}
}

func TestStrictSchemaPreflightRejectsIncompatibleColumnType(t *testing.T) {
	values := exactSchemaRows()
	for _, row := range values {
		if row[0] == "call_logs" && row[1] == "org_id" {
			row[2] = "text"
		}
	}
	tx := &fakeStrictTx{queryResults: []fakeStrictQueryResult{{rows: &fakeSnapshotRows{values: values}}}}
	err := preflightSnapshotV2Schema(context.Background(), tx)
	if err == nil || !strings.Contains(err.Error(), "incompatible_column: call_logs.org_id") {
		t.Fatalf("expected incompatible type, got %v", err)
	}
}

func TestRunStrictSnapshotSchemaFailurePublishesNoOutput(t *testing.T) {
	requestID := "private-schema-canary"
	manifestPath := writeStrictManifestFixture(t, []approvalManifestRequest{oneStrictManifestRequest(requestID)})
	destination := filepath.Join(t.TempDir(), "snapshot.json")
	rows := exactSchemaRows()
	rows = rows[:len(rows)-1]
	tx := &fakeStrictTx{
		isolation: "repeatable read", readOnly: "on",
		queryResults: []fakeStrictQueryResult{{rows: &fakeSnapshotRows{values: rows}}},
	}
	db := &fakeStrictDB{tx: tx}
	assertStrictFailurePublishesNothing(t, manifestPath, destination, "strict_schema_", requestID, db)
}

func TestLoadStrictSnapshotUsesOneServerConfirmedReadOnlyRepeatableReadTransaction(t *testing.T) {
	requestIDs := []string{"req-1"}
	db := validStrictDBForRequests(requestIDs)
	snapshot, err := loadValidStrictSnapshotForTest(t, db, requestIDs)
	if err != nil {
		t.Fatalf("loadStrictSnapshot: %v", err)
	}
	if snapshot.Counts.CallLogs != 1 || snapshot.Counts.TraceEvents != 1 || snapshot.Counts.CaptureGaps != 0 {
		t.Fatalf("unexpected counts: %#v", snapshot.Counts)
	}
	if db.beginCalls != 1 || db.beginOptions.Isolation != sql.LevelRepeatableRead || !db.beginOptions.ReadOnly {
		t.Fatalf("transaction options=%#v beginCalls=%d", db.beginOptions, db.beginCalls)
	}
	if db.tx.commitCalls != 1 || db.tx.rollbackCalls != 0 || db.tx.queryIndex != 4 {
		t.Fatalf("tx state commit=%d rollback=%d queryIndex=%d", db.tx.commitCalls, db.tx.rollbackCalls, db.tx.queryIndex)
	}
}

func TestLoadStrictSnapshotRejectsUnconfirmedIsolationOrReadOnlyMode(t *testing.T) {
	for _, tc := range []struct {
		name, isolation, readOnly, want string
	}{
		{name: "isolation", isolation: "read committed", readOnly: "on", want: "isolation"},
		{name: "read_only", isolation: "repeatable read", readOnly: "off", want: "read_only"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := validStrictDBForRequests([]string{"req-1"})
			db.tx.isolation = tc.isolation
			db.tx.readOnly = tc.readOnly
			_, err := loadValidStrictSnapshotForTest(t, db, []string{"req-1"})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %s failure, got %v", tc.want, err)
			}
			if db.tx.queryIndex != 0 || db.tx.commitCalls != 0 || db.tx.rollbackCalls != 1 {
				t.Fatalf("unexpected tx state: %#v", db.tx)
			}
		})
	}
}

func TestLoadStrictSnapshotRollsBackOnAnyGateOrReadFailure(t *testing.T) {
	for _, tc := range []struct {
		name  string
		alter func(*fakeStrictDB)
	}{
		{name: "gate", alter: func(db *fakeStrictDB) {
			db.tx.queryResults[1].rows = &fakeSnapshotRows{values: [][]any{{1, int64(0), int64(0), int64(0), sql.NullInt64{}, int64(0), int64(0), int64(0)}}}
		}},
		{name: "second_table_read", alter: func(db *fakeStrictDB) { db.tx.queryResults[3].err = errors.New("injected") }},
		{name: "commit", alter: func(db *fakeStrictDB) { db.tx.commitErr = errors.New("injected") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := validStrictDBForRequests([]string{"req-1"})
			tc.alter(db)
			_, err := loadValidStrictSnapshotForTest(t, db, []string{"req-1"})
			if err == nil {
				t.Fatal("expected failure")
			}
			if db.tx.rollbackCalls != 1 {
				t.Fatalf("rollback calls=%d", db.tx.rollbackCalls)
			}
		})
	}
}

func TestStrictCapsAcceptOneAndTwentyApprovedRequests(t *testing.T) {
	for _, count := range []int{1, 20} {
		manifest := approvalManifest{SchemaVersion: prd5ApprovalManifestSchemaVersion, SourceInstance: "internal-dev"}
		for idx := 0; idx < count; idx++ {
			manifest.Requests = append(manifest.Requests, approvalManifestRequest{
				ScenarioLabel: fmt.Sprintf("scenario-%02d", idx), RequestID: fmt.Sprintf("req-%02d", idx), ApprovalStatus: "approved", Required: true,
			})
		}
		ids, scenarios, err := approvedManifestSelection(manifest)
		if err != nil || len(ids) != count || len(scenarios) != count {
			t.Fatalf("count=%d ids=%d scenarios=%d err=%v", count, len(ids), len(scenarios), err)
		}
	}
}

func TestStrictCapsAcceptsExactlyTwoHundredRows(t *testing.T) {
	aggregates := []strictSourceAggregate{{Ordinal: 1, CallLogs: 1, TraceEvents: 199, DistinctOrgs: 1, OrgID: 7}}
	if err := validateStrictSourceAggregates(aggregates, 1); err != nil {
		t.Fatalf("exact 200 rejected: %v", err)
	}
}

func TestStrictCapsRejectsZeroTwentyOneAndTwoHundredOneWithoutOutput(t *testing.T) {
	for _, count := range []int{0, 21} {
		manifest := approvalManifest{SchemaVersion: prd5ApprovalManifestSchemaVersion, SourceInstance: "internal-dev"}
		for idx := 0; idx < count; idx++ {
			manifest.Requests = append(manifest.Requests, approvalManifestRequest{
				ScenarioLabel: fmt.Sprintf("scenario-%02d", idx), RequestID: fmt.Sprintf("req-%02d", idx), ApprovalStatus: "approved", Required: true,
			})
		}
		if _, _, err := approvedManifestSelection(manifest); err == nil || !strings.Contains(err.Error(), "strict_request_limit") {
			t.Fatalf("count=%d expected request limit, got %v", count, err)
		}
	}
	aggregates := []strictSourceAggregate{{Ordinal: 1, CallLogs: 1, TraceEvents: 200, DistinctOrgs: 1, OrgID: 7}}
	if err := validateStrictSourceAggregates(aggregates, 1); err == nil || !strings.Contains(err.Error(), "strict_row_limit") {
		t.Fatalf("expected 201 rejection, got %v", err)
	}
	t.Setenv("UNIROUTE_PRD5_MAX_ROWS", "999999")
	if err := validateStrictSourceAggregates(aggregates, 1); err == nil {
		t.Fatal("environment variable widened immutable cap")
	}
}

func TestStrictApprovalManifestAcceptsApprovedRequiredNotesFreeUniqueRowsAndExactByteHash(t *testing.T) {
	payload := []byte(`{"schema_version":"unirouter.prd5.xinsight.approval.v1","source_instance":"internal-dev","requests":[{"scenario_label":"success","request_id":"req-1","approval_status":"approved","required":true}]}`)
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, digest, err := loadApprovalManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	ids, scenarios, err := approvedManifestSelection(manifest)
	if err != nil || len(ids) != 1 || len(scenarios) != 1 {
		t.Fatalf("selection ids=%v scenarios=%v err=%v", ids, scenarios, err)
	}
	if digest != "sha256:"+sha256Hex(payload) {
		t.Fatalf("digest=%s", digest)
	}
}

func TestStrictApprovalManifestRejectsPendingUnavailableRequiredFalseNotesWhitespacePlaceholderEmptyAndDuplicates(t *testing.T) {
	base := oneStrictManifestRequest("private-manifest-canary")
	for _, tc := range []struct {
		name   string
		mutate func(*approvalManifest)
	}{
		{name: "pending", mutate: func(m *approvalManifest) { m.Requests[0].ApprovalStatus = "pending" }},
		{name: "unavailable", mutate: func(m *approvalManifest) { m.Requests[0].ApprovalStatus = "unavailable" }},
		{name: "required_false", mutate: func(m *approvalManifest) { m.Requests[0].Required = false }},
		{name: "notes", mutate: func(m *approvalManifest) { m.Requests[0].Notes = " private-note-canary " }},
		{name: "placeholder", mutate: func(m *approvalManifest) { m.Requests[0].RequestID = "TBD" }},
		{name: "unknown_request", mutate: func(m *approvalManifest) { m.Requests[0].RequestID = "UnKnOwN" }},
		{name: "credential_request", mutate: func(m *approvalManifest) { m.Requests[0].RequestID = "apikey:private-canary" }},
		{name: "placeholder_label", mutate: func(m *approvalManifest) { m.Requests[0].ScenarioLabel = "TODO" }},
		{name: "unknown_label", mutate: func(m *approvalManifest) { m.Requests[0].ScenarioLabel = "unknown" }},
		{name: "credential_label", mutate: func(m *approvalManifest) { m.Requests[0].ScenarioLabel = "apikey:private-canary" }},
		{name: "empty", mutate: func(m *approvalManifest) { m.Requests[0].ScenarioLabel = "" }},
		{name: "duplicate_request", mutate: func(m *approvalManifest) {
			m.Requests = append(m.Requests, m.Requests[0])
			m.Requests[1].ScenarioLabel = "second"
		}},
		{name: "duplicate_label", mutate: func(m *approvalManifest) {
			m.Requests = append(m.Requests, m.Requests[0])
			m.Requests[1].RequestID = "req-2"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manifest := approvalManifest{SchemaVersion: prd5ApprovalManifestSchemaVersion, SourceInstance: "internal-dev", Requests: []approvalManifestRequest{base}}
			tc.mutate(&manifest)
			_, _, err := approvedManifestSelection(manifest)
			if err == nil {
				t.Fatal("expected manifest rejection")
			}
			for _, canary := range []string{"private-manifest-canary", "private-note-canary", "private-canary"} {
				if strings.Contains(err.Error(), canary) {
					t.Fatalf("manifest value leaked: %v", err)
				}
			}
		})
	}
}

func TestStrictApprovalManifestRejectsSourceInstanceNormalizationOrMismatch(t *testing.T) {
	for _, value := range []string{" internal-dev", "internal dev", "internal/dev", "internal-dev ", "TBD", "none", "unknown", "UNKNOWN", "unavailable"} {
		if err := validateStrictSourceInstance(value); err == nil {
			t.Fatalf("source instance accepted after normalization: %q", value)
		}
	}
	manifestPath := writeStrictManifestFixture(t, []approvalManifestRequest{oneStrictManifestRequest("req-1")})
	err := runPRD5Snapshot([]string{"--approval-manifest", manifestPath, "--source-instance", "different"})
	if err == nil || !strings.Contains(err.Error(), "source_instance_mismatch") {
		t.Fatalf("expected mismatch, got %v", err)
	}
}

func TestStrictUnknownSourceInstanceManifestAndBuiltSnapshotFailClosedBeforeSourceRead(t *testing.T) {
	manifestPath := writeStrictManifestFixtureWithSource(t, "unknown", []approvalManifestRequest{oneStrictManifestRequest("req-1")})
	prepareCalls, envCalls, dbCalls := 0, 0, 0
	deps := defaultStrictSnapshotDeps()
	deps.prepareOutput = func(string) (snapshotpublish.Stage, error) { prepareCalls++; return newFakeSnapshotStage(), nil }
	deps.lookupEnv = func(string) string { envCalls++; return "synthetic" }
	deps.openDB = func(string) (strictSnapshotDB, error) {
		dbCalls++
		return validStrictDBForRequests([]string{"req-1"}), nil
	}
	err := runPRD5SnapshotWithDeps([]string{"--approval-manifest", manifestPath, "--output", filepath.Join(t.TempDir(), "snapshot.json")}, deps)
	if err == nil || !strings.Contains(err.Error(), "source_instance") || prepareCalls != 0 || envCalls != 0 || dbCalls != 0 {
		t.Fatalf("manifest err=%v prepare=%d env=%d db=%d", err, prepareCalls, envCalls, dbCalls)
	}
	snapshot, _ := validPureStrictSnapshot(t)
	snapshot.SourceInstance = "UNKNOWN"
	if err := validateStrictSnapshotValuePolicy(snapshot); err == nil || !strings.Contains(err.Error(), "source_or_time") {
		t.Fatalf("built snapshot unknown source accepted: %v", err)
	}
}

func TestRunStrictSnapshotRejectsDirectIDSelectionBeforeSourceReadOrOutput(t *testing.T) {
	manifestPath := writeStrictManifestFixture(t, []approvalManifestRequest{oneStrictManifestRequest("req-1")})
	for _, selector := range [][]string{
		{"--request-id", "req-2"}, {"--request-id", ""}, {"--request-ids", " "}, {"--scenario", ""},
	} {
		prepareCalls, envCalls := 0, 0
		destination := filepath.Join(t.TempDir(), "snapshot.json")
		stage := newFakeSnapshotStage()
		deps := defaultStrictSnapshotDeps()
		deps.prepareOutput = func(string) (snapshotpublish.Stage, error) { prepareCalls++; return stage, nil }
		deps.lookupEnv = func(string) string { envCalls++; return "synthetic" }
		args := append([]string{"--approval-manifest", manifestPath, "--output", destination}, selector...)
		assertTopLevelStrictFailurePublishesNothing(
			t, args, destination, "direct_id_selection", "req-2", deps, 0, func() int { return stage.commitCalls },
		)
		if prepareCalls != 0 || envCalls != 0 {
			t.Fatalf("selector=%q prepare=%d env=%d", selector, prepareCalls, envCalls)
		}
	}
}

func TestStrictSourceIntegrityAcceptsEveryRequestCoveredByOnePositiveOrgAndAtMostOneCallLog(t *testing.T) {
	aggregates := []strictSourceAggregate{
		{Ordinal: 1, CallLogs: 1, TraceEvents: 0, DistinctOrgs: 1, OrgID: 7},
		{Ordinal: 2, CallLogs: 0, TraceEvents: 1, DistinctOrgs: 1, OrgID: 7},
	}
	if err := validateStrictSourceAggregates(aggregates, 2); err != nil {
		t.Fatal(err)
	}
}

func TestStrictSourceIntegrityAllowsMultipleTraceRowsOnlyWithinTheSameOrg(t *testing.T) {
	aggregates := []strictSourceAggregate{{Ordinal: 1, CallLogs: 1, TraceEvents: 198, DistinctOrgs: 1, OrgID: 7}}
	if err := validateStrictSourceAggregates(aggregates, 1); err != nil {
		t.Fatal(err)
	}
}

func TestStrictSourceIntegrityRejectsMissingCoverageZeroOrMixedOrgCrossOrgCollisionAndDuplicateCallLog(t *testing.T) {
	for _, tc := range []struct {
		name       string
		aggregates []strictSourceAggregate
		want       string
	}{
		{name: "missing", aggregates: []strictSourceAggregate{{Ordinal: 1}}, want: "missing_coverage"},
		{name: "zero_org", aggregates: []strictSourceAggregate{{Ordinal: 1, CallLogs: 1, DistinctOrgs: 1}}, want: "invalid_organization"},
		{name: "mixed_global", aggregates: []strictSourceAggregate{{Ordinal: 1, CallLogs: 1, DistinctOrgs: 1, OrgID: 7}, {Ordinal: 2, TraceEvents: 1, DistinctOrgs: 1, OrgID: 8}}, want: "multiple_organizations"},
		{name: "collision", aggregates: []strictSourceAggregate{{Ordinal: 1, CallLogs: 1, TraceEvents: 1, DistinctOrgs: 2, OrgID: 7}}, want: "cross_org_collision"},
		{name: "duplicate", aggregates: []strictSourceAggregate{{Ordinal: 1, CallLogs: 2, DistinctOrgs: 1, OrgID: 7}}, want: "duplicate_call_log"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateStrictSourceAggregates(tc.aggregates, len(tc.aggregates))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %s, got %v", tc.want, err)
			}
		})
	}
}

func TestReconcileStrictSnapshotAcceptsExactSetsCountsGapsAndTableSourceRefs(t *testing.T) {
	snapshot, aggregates := validPureStrictSnapshot(t)
	if err := reconcileStrictSnapshot(bindingForSnapshot(snapshot), aggregates, snapshot); err != nil {
		t.Fatal(err)
	}
}

func TestStrictIPPresenceAggregateDetailReconcilesExactly(t *testing.T) {
	for _, tc := range []struct {
		name         string
		aggregate    int64
		present      bool
		redacted     bool
		wantAccepted bool
	}{
		{name: "zero_matches_false_false", aggregate: 0, wantAccepted: true},
		{name: "one_matches_true_true", aggregate: 1, present: true, redacted: true, wantAccepted: true},
		{name: "aggregate_zero_detail_true", aggregate: 0, present: true, redacted: true},
		{name: "aggregate_one_detail_false", aggregate: 1},
		{name: "detail_mismatch", aggregate: 1, present: true, redacted: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot, aggregates := validPureStrictSnapshot(t)
			snapshot.CallLogs[0].IPAddressWasPresent = tc.present
			snapshot.CallLogs[0].IPAddressRedacted = tc.redacted
			aggregates[0].IPAddressPresentRows = tc.aggregate
			err := reconcileStrictSnapshot(bindingForSnapshot(snapshot), aggregates, snapshot)
			if tc.wantAccepted {
				if err != nil {
					t.Fatalf("matching IP aggregate rejected: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "strict_reconciliation_failed: ip_") {
				t.Fatalf("IP reconciliation mismatch accepted: %v", err)
			}
		})
	}

	requestIDs := []string{"req-1", "req-2"}
	snapshot, err := loadValidStrictSnapshotForTest(t, validStrictDBForRequests(requestIDs), requestIDs)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.CallLogs[0].IPAddressWasPresent = true
	snapshot.CallLogs[0].IPAddressRedacted = true
	aggregates := []strictSourceAggregate{
		{Ordinal: 1, CallLogs: 1, TraceEvents: 1, DistinctOrgs: 1, OrgID: 7, IPAddressPresentRows: 0},
		{Ordinal: 2, CallLogs: 1, TraceEvents: 1, DistinctOrgs: 1, OrgID: 7, IPAddressPresentRows: 1},
	}
	if err := reconcileStrictSnapshot(bindingForSnapshot(snapshot), aggregates, snapshot); err == nil || !strings.Contains(err.Error(), "ip_presence_count") {
		t.Fatalf("per-request presence swap accepted despite matching global total: %v", err)
	}
}

func TestLoadStrictSnapshotAcceptsPositiveIPPresenceWithExactReconciliation(t *testing.T) {
	db := validStrictDBForRequests([]string{"req-1"})
	aggregateRows := db.tx.queryResults[1].rows.(*fakeSnapshotRows)
	aggregateRows.values[0][5] = int64(1)
	callRows := db.tx.queryResults[2].rows.(*fakeSnapshotRows)
	callRows.values[0][23] = true

	snapshot, err := loadValidStrictSnapshotForTest(t, db, []string{"req-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.CallLogs) != 1 || !snapshot.CallLogs[0].IPAddressWasPresent || !snapshot.CallLogs[0].IPAddressRedacted {
		t.Fatalf("loaded proof=%#v", snapshot.CallLogs)
	}
	if db.tx.commitCalls != 1 || db.tx.rollbackCalls != 0 || db.tx.queryIndex != 4 {
		t.Fatalf("transaction commit=%d rollback=%d queryIndex=%d", db.tx.commitCalls, db.tx.rollbackCalls, db.tx.queryIndex)
	}
}

func TestStrictIPPresenceAggregateMismatchPublishesNothingWithoutEcho(t *testing.T) {
	for _, tc := range []struct {
		name      string
		aggregate int64
		detail    bool
	}{
		{name: "aggregate_true_detail_false", aggregate: 1, detail: false},
		{name: "aggregate_false_detail_true", aggregate: 0, detail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requestID := "private-ip-reconciliation-canary"
			manifestPath := writeStrictManifestFixture(t, []approvalManifestRequest{oneStrictManifestRequest(requestID)})
			destination := filepath.Join(t.TempDir(), "snapshot.json")
			db := validStrictDBForRequests([]string{requestID})
			aggregateRows := db.tx.queryResults[1].rows.(*fakeSnapshotRows)
			aggregateRows.values[0][5] = tc.aggregate
			callRows := db.tx.queryResults[2].rows.(*fakeSnapshotRows)
			callRows.values[0][23] = tc.detail
			assertStrictFailurePublishesNothing(t, manifestPath, destination, "strict_reconciliation_failed: ip_presence_count", requestID, db)
			if db.tx.commitCalls != 0 || db.tx.rollbackCalls != 1 {
				t.Fatalf("transaction commit=%d rollback=%d", db.tx.commitCalls, db.tx.rollbackCalls)
			}
		})
	}
}

func TestReconcileStrictSnapshotAcceptsOneSourceTableAndOneExactGeneratedGapAtBoundary(t *testing.T) {
	snapshot, aggregates := validPureStrictSnapshot(t)
	snapshot.TraceEvents = nil
	aggregates[0].TraceEvents = 0
	snapshot.CaptureGaps = buildCaptureGaps(snapshot.RequestIDs, snapshot.CallLogs, nil)
	snapshot.Counts.TraceEvents = 0
	snapshot.Counts.CaptureGaps = 1
	if err := reconcileStrictSnapshot(bindingForSnapshot(snapshot), aggregates, snapshot); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileStrictSnapshotRejectsOutsideSetRowInventedOrMissingScenario(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*snapshotFile)
	}{
		{name: "outside_row", mutate: func(s *snapshotFile) { s.CallLogs[0].RequestID = "outside-private-canary" }},
		{name: "invented_scenario", mutate: func(s *snapshotFile) { s.Scenarios[0].Label = "invented" }},
		{name: "missing_scenario", mutate: func(s *snapshotFile) { s.Scenarios = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot, aggregates := validPureStrictSnapshot(t)
			binding := bindingForSnapshot(snapshot)
			tc.mutate(&snapshot)
			err := reconcileStrictSnapshot(binding, aggregates, snapshot)
			if err == nil || !strings.Contains(err.Error(), "strict_reconciliation_failed") {
				t.Fatalf("expected reconciliation failure, got %v", err)
			}
			if strings.Contains(err.Error(), "outside-private-canary") {
				t.Fatalf("outside value leaked: %v", err)
			}
		})
	}
}

func TestReconcileStrictSnapshotRejectsMissingExtraGapCountOrTableSourceRefMismatch(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*snapshotFile)
	}{
		{name: "extra_gap", mutate: func(s *snapshotFile) { s.CaptureGaps = append(s.CaptureGaps, snapshotCaptureGap{Generated: true}) }},
		{name: "count", mutate: func(s *snapshotFile) { s.Counts.CallLogs++ }},
		{name: "source_ref", mutate: func(s *snapshotFile) { s.CallLogs[0].SourceRef = "unirouter:internal-dev:request_trace_events:1" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot, aggregates := validPureStrictSnapshot(t)
			binding := bindingForSnapshot(snapshot)
			err := func() error {
				tc.mutate(&snapshot)
				return reconcileStrictSnapshot(binding, aggregates, snapshot)
			}()
			if err == nil || !strings.Contains(err.Error(), "strict_reconciliation_failed") {
				t.Fatalf("expected reconciliation failure, got %v", err)
			}
		})
	}
}

func TestReconcileStrictSnapshotRejectsMissingGapAndSelfConsistentSourceBindingRewrite(t *testing.T) {
	snapshot, aggregates := validPureStrictSnapshot(t)
	binding := bindingForSnapshot(snapshot)
	snapshot.TraceEvents = nil
	aggregates[0].TraceEvents = 0
	snapshot.Counts.TraceEvents = 0
	snapshot.Counts.CaptureGaps = 0
	if err := reconcileStrictSnapshot(binding, aggregates, snapshot); err == nil || !strings.Contains(err.Error(), "capture_gap") {
		t.Fatalf("missing gap accepted: %v", err)
	}

	snapshot, aggregates = validPureStrictSnapshot(t)
	binding = bindingForSnapshot(snapshot)
	snapshot.SourceInstance = "rewritten-source"
	snapshot.CallLogs[0].SourceRef = sourceRef(snapshot.SourceInstance, "call_logs", snapshot.CallLogs[0].ID)
	snapshot.TraceEvents[0].SourceRef = sourceRef(snapshot.SourceInstance, "request_trace_events", snapshot.TraceEvents[0].ID)
	if err := reconcileStrictSnapshot(binding, aggregates, snapshot); err == nil || !strings.Contains(err.Error(), "manifest_binding") {
		t.Fatalf("self-consistent source rewrite accepted: %v", err)
	}
}

func TestStrictPayloadContractAcceptsEmptyRefAndAllowedModes(t *testing.T) {
	for _, ref := range []sql.NullString{{}, {String: "", Valid: true}} {
		for _, mode := range []string{"none", "metadata_only", "hash_only"} {
			payloadRef, actualMode, err := validateStrictPayloadContract("call_logs", ref, mode)
			if err != nil || payloadRef != nil || actualMode != mode {
				t.Fatalf("ref=%#v mode=%s payload=%v actual=%s err=%v", ref, mode, payloadRef, actualMode, err)
			}
			encoded, err := json.Marshal(snapshotCallLog{PayloadRef: payloadRef, PayloadCaptureMode: actualMode})
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(encoded, []byte(`"payload_ref"`)) {
				t.Fatalf("payload_ref was serialized: %s", encoded)
			}
		}
	}
}

func TestStrictPayloadContractRejectsWhitespaceOrNonEmptyRefAndEmptyOrUnsupportedMode(t *testing.T) {
	privateSentinel := "private-payload-ref-canary"
	for _, tc := range []struct {
		ref  sql.NullString
		mode string
	}{
		{ref: sql.NullString{String: " ", Valid: true}, mode: "metadata_only"},
		{ref: sql.NullString{String: privateSentinel, Valid: true}, mode: "metadata_only"},
		{mode: ""}, {mode: "disabled"}, {mode: "raw"}, {mode: "Metadata_Only"},
	} {
		_, _, err := validateStrictPayloadContract("request_trace_events", tc.ref, tc.mode)
		if err == nil {
			t.Fatalf("expected rejection for %#v", tc)
		}
		if strings.Contains(err.Error(), privateSentinel) {
			t.Fatalf("payload ref leaked: %v", err)
		}
	}
}

func TestStrictPayloadCaptureReasonIsRegressionLocked(t *testing.T) {
	if prd5PayloadCaptureReason != "raw prompt/response export is disabled for PRD5 Gate A snapshots" {
		t.Fatalf("payload reason changed: %q", prd5PayloadCaptureReason)
	}
	snapshot, _ := validPureStrictSnapshot(t)
	if snapshot.CallLogs[0].PayloadCaptureReason != prd5PayloadCaptureReason || snapshot.TraceEvents[0].PayloadCaptureReason != prd5PayloadCaptureReason {
		t.Fatal("row payload reasons are not locked to the producer constant")
	}
}

func TestStrictValuePolicyAcceptsAllowlistedBoundedCodeScalarsAndProjectOpaqueRefs(t *testing.T) {
	snapshot, _ := validPureStrictSnapshot(t)
	if err := validateStrictSnapshotValuePolicy(snapshot); err != nil {
		t.Fatalf("valid strict snapshot rejected: %v", err)
	}
	for key, rule := range strictMetadataRules {
		if err := validateStrictMetadataValue(rule, safeMetadataValue(rule)); err != nil {
			t.Fatalf("metadata key %s safe value rejected: %v", key, err)
		}
	}
}

func TestStrictMetadataMirrorsReceiverCodeGrammarAndNumericIdentifierBoundary(t *testing.T) {
	for _, tc := range []struct {
		name  string
		rule  strictMetadataRule
		value interface{}
	}{
		{name: "code_slash", rule: strictMetadataRule{kind: metadataCode}, value: "code/v1"},
		{name: "code_list_pipe", rule: strictMetadataRule{kind: metadataCodeList}, value: "a|b"},
	} {
		if err := validateStrictMetadataValue(tc.rule, tc.value); err == nil {
			t.Fatalf("receiver-incompatible metadata accepted: %s=%v", tc.name, tc.value)
		}
	}
	if err := validateStrictMetadataValue(strictMetadataRule{kind: metadataCode}, "code:v1"); err != nil {
		t.Fatalf("receiver-safe code rejected: %v", err)
	}
	if err := validateStrictMetadataValue(strictMetadataRule{kind: metadataCodeList}, "a,b"); err != nil {
		t.Fatalf("receiver-safe code list rejected: %v", err)
	}

	integerRule := strictMetadataRule{kind: metadataInteger}
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{value: "999999999", want: true},
		{value: "1000000000", want: false},
		{value: "999999999999999", want: false},
		{value: "1000000000000000", want: true},
	} {
		err := validateStrictMetadataValue(integerRule, json.Number(tc.value))
		if (err == nil) != tc.want {
			t.Fatalf("integer %s accepted=%t want=%t err=%v", tc.value, err == nil, tc.want, err)
		}
	}
	decimalRule := strictMetadataRule{kind: metadataDecimal}
	if err := validateStrictMetadataValue(decimalRule, json.Number("1000000000.0")); err == nil {
		t.Fatal("integer-like decimal in receiver blocked range accepted")
	}
	if err := validateStrictMetadataValue(decimalRule, json.Number("1000000000.5")); err != nil {
		t.Fatalf("non-integer decimal rejected contrary to receiver semantics: %v", err)
	}
}

func TestStrictMetadataAcceptsFieldSpecificServerOwnedGrammars(t *testing.T) {
	const detectorVersion = "unirouter.local-pii-secrets.v1.2.3.4"
	const limitPolicyChain = "builtin:rps@1#global-org=allow/allowed,builtin:tpm@2#org-7=allow/allowed"

	if class := blockedStringClass(detectorVersion); class != "ip" {
		t.Fatalf("fixture must reproduce generic IP false positive, class=%q", class)
	}
	if class := blockedStringClass(limitPolicyChain); class != "email" {
		t.Fatalf("fixture must reproduce generic email false positive, class=%q", class)
	}

	payload, err := json.Marshal(map[string]any{
		"guardrail_detector_version": detectorVersion,
		"limit_policy_chain":         limitPolicyChain,
	})
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := strictMetadata(string(payload))
	if err != nil {
		t.Fatalf("server-owned metadata grammars rejected: %v", err)
	}
	if metadata["guardrail_detector_version"] != detectorVersion || metadata["limit_policy_chain"] != limitPolicyChain {
		t.Fatalf("field-specific metadata changed: %#v", metadata)
	}
}

func TestStrictMetadataFieldSpecificGrammarsRejectRealSensitiveLookalikesWithSafeDiagnostics(t *testing.T) {
	tests := []struct {
		name, field, value, family, class string
	}{
		{
			name: "detector_exact_ip", field: "guardrail_detector_version", value: "127.0.0.1",
			family: "guardrail_detector_version", class: "ip",
		},
		{
			name: "detector_embedded_ip", field: "guardrail_detector_version", value: "detector.127.0.0.1",
			family: "guardrail_detector_version", class: "ip",
		},
		{
			name: "detector_email", field: "guardrail_detector_version", value: "ops@example.com",
			family: "guardrail_detector_version", class: "email",
		},
		{
			name: "detector_credential", field: "guardrail_detector_version", value: "token=private-detector-canary",
			family: "guardrail_detector_version", class: "credential",
		},
		{
			name: "chain_email", field: "limit_policy_chain", value: "ops@example.com",
			family: "limit_policy_chain", class: "email",
		},
		{
			name: "chain_ip", field: "limit_policy_chain", value: "builtin:rps@1#127.0.0.1=allow/allowed",
			family: "limit_policy_chain", class: "ip",
		},
		{
			name: "chain_credential", field: "limit_policy_chain", value: "builtin:rps@1#global=allow/token=private-chain-canary",
			family: "limit_policy_chain", class: "credential",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := json.Marshal(map[string]string{tc.field: tc.value})
			if err != nil {
				t.Fatal(err)
			}
			_, err = strictMetadata(string(payload))
			if err == nil {
				t.Fatal("sensitive lookalike accepted")
			}
			message := err.Error()
			for _, marker := range []string{
				"field=" + tc.field,
				"family=" + tc.family,
				"class=" + tc.class,
				"count=1",
			} {
				if !strings.Contains(message, marker) {
					t.Fatalf("diagnostic missing %q: %v", marker, err)
				}
			}
			if strings.Contains(message, tc.value) ||
				strings.Contains(message, "private-detector-canary") ||
				strings.Contains(message, "private-chain-canary") {
				t.Fatalf("metadata value leaked: %v", err)
			}
		})
	}
}

func TestStrictMetadataDocumentAndUnknownKeyDiagnosticsNeverEchoInput(t *testing.T) {
	const unknownKey = "private_metadata_key_canary"
	_, err := strictMetadata(`{"` + unknownKey + `":"private_metadata_value_canary"}`)
	if err == nil ||
		!strings.Contains(err.Error(), "field=unknown family=unknown_key class=unknown_key count=1") ||
		strings.Contains(err.Error(), unknownKey) ||
		strings.Contains(err.Error(), "private_metadata_value_canary") {
		t.Fatalf("unsafe unknown-key diagnostic: %v", err)
	}

	_, err = strictMetadata(" {\"usage_source\":\"upstream\"}")
	if err == nil ||
		!strings.Contains(err.Error(), "field=metadata family=document class=noncanonical_json count=1") ||
		strings.Contains(err.Error(), "upstream") {
		t.Fatalf("unsafe document diagnostic: %v", err)
	}
}

func TestStrictSafetyUsesExactDualBooleanIPHandlingDeclaration(t *testing.T) {
	const exact = "raw IP addresses are not exported; only ip_address_was_present and ip_address_redacted boolean fields are retained"
	snapshot, _ := validPureStrictSnapshot(t)
	if strictIPHandling != exact || snapshot.Safety.IPHandling != exact || snapshot.Safety.ErrorHandling != strictErrorHandling || snapshot.Safety.MetadataHandling != strictMetadataHandling {
		t.Fatalf("strict safety declarations=%#v", snapshot.Safety)
	}
}

func TestStrictSafetyRejectsMutationOmissionAndLegacyStrictDeclaration(t *testing.T) {
	const legacyStrict = "raw IP addresses are not exported; only ip_address_was_present is retained"
	for _, value := range []string{
		"",
		legacyStrict,
		"raw IP addresses are not exported; only ip_address_redacted and ip_address_was_present boolean fields are retained",
		strictIPHandling + ".",
	} {
		snapshot, _ := validPureStrictSnapshot(t)
		snapshot.Safety.IPHandling = value
		if err := validateStrictSnapshotValuePolicy(snapshot); err == nil || err.Error() != "strict_value_policy: snapshot.safety_text" {
			t.Fatalf("strict safety mutation %q accepted: %v", value, err)
		}
	}
}

func TestHistoricalDirectIPHandlingDeclarationRemainsSingleBoolean(t *testing.T) {
	_, testPath, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("test source path unavailable")
	}
	producerSource, err := os.ReadFile(filepath.Join(filepath.Dir(testPath), "prd5_xinsight_snapshot.go"))
	if err != nil {
		t.Fatal(err)
	}
	legacy := []byte("raw IP addresses are not exported; only ip_address_was_present is retained")
	strict := []byte("raw IP addresses are not exported; only ip_address_was_present and ip_address_redacted boolean fields are retained")
	if count := bytes.Count(producerSource, legacy); count != 1 {
		t.Fatalf("historical direct declaration count=%d want=1", count)
	}
	if count := bytes.Count(producerSource, strict); count != 1 {
		t.Fatalf("strict dual-boolean declaration count=%d want=1", count)
	}
}

func TestStrictMetadataSanitizerCannotMutateOrDrop(t *testing.T) {
	metadata, err := strictMetadata(`{"usage_source":"upstream","amount_scale":1000000}`)
	if err != nil || metadata["usage_source"] != "upstream" {
		t.Fatalf("strict metadata sanitizer round trip metadata=%#v err=%v", metadata, err)
	}
	if err := requireStrictMetadataSanitizerEquality(map[string]interface{}{"usage_source": "upstream"}, func(map[string]interface{}) map[string]interface{} {
		return map[string]interface{}{}
	}); err == nil || !strings.Contains(err.Error(), "sanitizer_mismatch") {
		t.Fatalf("silent sanitizer drop was not rejected: %v", err)
	}
}

func safeMetadataValue(rule strictMetadataRule) interface{} {
	switch rule.kind {
	case metadataCode:
		return "code-v1"
	case metadataCodeList:
		return "code-v1,code-v2"
	case metadataGuardrailDetectorVersion:
		return "unirouter.local-pii-secrets.v1.2.3.4"
	case metadataLimitPolicyChain:
		return "builtin:rps@1#global-org=allow/allowed"
	case metadataBool:
		return true
	case metadataFalseBool:
		return false
	case metadataInteger:
		return json.Number("1")
	case metadataDecimal:
		return json.Number("1.25")
	case metadataConfidence:
		return json.Number("0.75")
	case metadataPercent:
		return json.Number("50")
	case metadataCurrency:
		return "CNY"
	case metadataMoneySchema:
		return "micro-cny.v1"
	case metadataAmountScale:
		return json.Number("1000000")
	case metadataPayloadMode:
		return "metadata_only"
	case metadataPayloadReason:
		return "payload.metadata_only"
	default:
		return nil
	}
}

func TestStrictValuePolicyRejectsUnknownKeysNestedValuesAndFreeText(t *testing.T) {
	for _, metadata := range []string{
		`{"unknown_private_key":"code"}`,
		`{"usage_source":{"nested":"value"}}`,
		`{"usage_source":["value"]}`,
		`{"usage_source":null}`,
		`{"usage_source":" leading"}`,
		`{"usage_source":"free text"}`,
	} {
		err := func() error { _, err := strictMetadata(metadata); return err }()
		if err == nil || !strings.Contains(err.Error(), "strict_value_policy") {
			t.Fatalf("metadata accepted: %s err=%v", metadata, err)
		}
		if strings.Contains(err.Error(), "unknown_private_key") {
			t.Fatalf("dynamic metadata key leaked: %v", err)
		}
	}
}

func TestStrictValuePolicyRejectsEmailPhoneIPURLCredentialCustomerIDAndRawErrorWithoutEcho(t *testing.T) {
	values := []string{
		"private@example.test", "+1 (555) 123-4567", "192.0.2.10", "https://private.example.test",
		"token=private-credential-canary", "ghp_privatecredentialcanary", "customer_id_private_canary", "raw error private canary",
		"trace:2001:db8::1", "user:13800138000", "user:555-123-4567", "authorization:private-canary",
		"apikey:private-canary", "api_key:private-canary", "api-key:private-canary", "access_token=private-canary",
		"refresh-token:private-canary", "-----BEGIN TEST PRIVATE KEY-----", "code/v1", "a|b",
	}
	for _, value := range values {
		err := validateStrictCodeValue(value, 256, false)
		if err == nil {
			t.Fatalf("blocked value accepted: %q", value)
		}
		if strings.Contains(err.Error(), value) {
			t.Fatalf("blocked value echoed: %v", err)
		}
	}
}

func TestStrictUsageConfidenceEnforcesCanonicalNumericFourThreeUnitInterval(t *testing.T) {
	for _, value := range []string{"0.000", "0.001", "0.999", "1.000"} {
		if err := validateStrictUnitDecimal(value); err != nil {
			t.Fatalf("canonical NUMERIC(4,3) confidence rejected: %q err=%v", value, err)
		}
	}
	for _, value := range []string{
		"-0.001", "1.001", "0", ".500", "0.5", "00.500", "1.00", "0." + strings.Repeat("0", 300) + "1",
	} {
		if err := validateStrictUnitDecimal(value); err == nil {
			t.Fatalf("noncanonical or out-of-range confidence accepted: %q", value)
		}
	}
}

func TestStrictMetadataRejectsInsteadOfDroppingOrTruncating(t *testing.T) {
	longValue := strings.Repeat("a", 257)
	for _, metadata := range []string{
		`{"unknown":"value"}`,
		`{"usage_source":"upstream","usage_source":"gateway"}`,
		`{"usage_source":" value "}`,
		fmt.Sprintf(`{"usage_source":%q}`, longValue),
	} {
		if _, err := strictMetadata(metadata); err == nil {
			t.Fatalf("metadata was silently sanitized: %s", metadata)
		}
	}
}

func TestStrictHTTPStatusFieldsRejectOutOfRangeBoundaries(t *testing.T) {
	for _, value := range []int64{-1, 0, 99, 600} {
		snapshot, _ := validPureStrictSnapshot(t)
		snapshot.CallLogs[0].UpstreamStatusCode = &value
		if err := validateStrictSnapshotValuePolicy(snapshot); err == nil {
			t.Fatalf("call_log upstream status %d accepted", value)
		}
		snapshot, _ = validPureStrictSnapshot(t)
		snapshot.TraceEvents[0].StatusCode = &value
		if err := validateStrictSnapshotValuePolicy(snapshot); err == nil {
			t.Fatalf("trace status %d accepted", value)
		}
	}
	for _, value := range []int64{100, 599} {
		snapshot, _ := validPureStrictSnapshot(t)
		snapshot.CallLogs[0].UpstreamStatusCode = &value
		snapshot.TraceEvents[0].StatusCode = &value
		if err := validateStrictSnapshotValuePolicy(snapshot); err != nil {
			t.Fatalf("status %d rejected: %v", value, err)
		}
	}
}

func TestStrictIPProofAcceptsFalseFalseAndTrueTrueRejectsMismatches(t *testing.T) {
	for _, tc := range []struct {
		name       string
		present    bool
		redacted   bool
		wantAccept bool
	}{
		{name: "no_ip", present: false, redacted: false, wantAccept: true},
		{name: "redacted_ip", present: true, redacted: true, wantAccept: true},
		{name: "presence_without_redaction", present: true, redacted: false},
		{name: "redaction_without_presence", present: false, redacted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot, _ := validPureStrictSnapshot(t)
			snapshot.CallLogs[0].IPAddressWasPresent = tc.present
			snapshot.CallLogs[0].IPAddressRedacted = tc.redacted
			helperErr := validateStrictIPProof(tc.present, tc.redacted)
			policyErr := validateStrictSnapshotValuePolicy(snapshot)
			if tc.wantAccept {
				if helperErr != nil || policyErr != nil {
					t.Fatalf("allowed proof rejected: helper=%v policy=%v", helperErr, policyErr)
				}
				return
			}
			if helperErr == nil || helperErr.Error() != "strict_ip_proof_mismatch" || policyErr == nil || policyErr.Error() != "strict_ip_proof_mismatch" {
				t.Fatalf("mismatch not rejected statically: helper=%v policy=%v", helperErr, policyErr)
			}
		})
	}
}

func TestStrictIPProofMismatchPublishesNothingWithoutEcho(t *testing.T) {
	for _, tc := range []struct {
		name, privateCanary string
		present, redacted   bool
	}{
		{name: "presence_without_redaction", privateCanary: "private-ip-proof-present-canary", present: true, redacted: false},
		{name: "redaction_without_presence", privateCanary: "private-ip-proof-redacted-canary", present: false, redacted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot, _ := validPureStrictSnapshot(t)
			snapshot.CallLogs[0].IPAddressWasPresent = tc.present
			snapshot.CallLogs[0].IPAddressRedacted = tc.redacted
			evidenceRef := tc.privateCanary
			snapshot.CallLogs[0].EvidenceRef = &evidenceRef
			stage := newFakeSnapshotStage()
			var runErr error
			stdout := captureStdout(t, func() {
				runErr = validateStrictSnapshotValuePolicy(snapshot)
				if runErr == nil {
					_, runErr = publishStrictSnapshot(stage, snapshot)
				}
			})
			if runErr == nil || runErr.Error() != "strict_ip_proof_mismatch" {
				t.Fatalf("mismatch error=%v", runErr)
			}
			if strings.Contains(runErr.Error(), tc.privateCanary) || len(stdout) != 0 || stage.commitCalls != 0 || len(stage.data) != 0 {
				t.Fatalf("mismatch leaked or published: err=%v stdout=%q commits=%d bytes=%d", runErr, stdout, stage.commitCalls, len(stage.data))
			}
		})
	}
}

func TestStrictRawIPSentinelIsNeverEchoedHashedOrSerialized(t *testing.T) {
	type result struct {
		row       snapshotCallLog
		rowJSON   []byte
		output    []byte
		outputSHA string
	}
	build := func(rawSentinel string) result {
		t.Helper()
		values := validCallLogScanValues(1, "req-1", 7)
		// This is the test-only source boundary: production receives only the
		// boolean result of the SQL presence expression, never rawSentinel.
		values[23] = rawSentinel != ""
		row, err := scanSnapshotCallLogStrict(snapshotRowScannerFunc(func(dest ...any) error {
			return assignFakeScan(values, dest)
		}), "internal-dev")
		if err != nil {
			t.Fatal(err)
		}
		snapshot := newStrictSnapshotFile(
			"internal-dev", "approved_request_id_manifest", "uxchg_0123456789abcdef0123456789abcdef",
			"sha256:"+strings.Repeat("a", 64), []string{"req-1"}, strictScenarios([]string{"req-1"}),
			"2026-07-20T00:00:00Z", []snapshotCallLog{row}, nil, nil,
		)
		if err := validateStrictSnapshotValuePolicy(snapshot); err != nil {
			t.Fatal(err)
		}
		rowJSON, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		stage := newFakeSnapshotStage()
		var publishResult strictPublishResult
		stdout := captureStdout(t, func() { publishResult, err = publishStrictSnapshot(stage, snapshot) })
		if err != nil || !publishResult.Published || len(stdout) != 0 {
			t.Fatalf("publish result=%#v err=%v stdout=%q", publishResult, err, stdout)
		}
		return result{row: row, rowJSON: rowJSON, output: append([]byte(nil), stage.data...), outputSHA: publishResult.SHA256}
	}

	sentinels := []string{"198.51.100.17", "2001:db8::17"}
	results := []result{build(sentinels[0]), build(sentinels[1])}
	if !reflect.DeepEqual(results[0].row, results[1].row) || results[0].row.SourceEventHash != results[1].row.SourceEventHash ||
		!bytes.Equal(results[0].output, results[1].output) || results[0].outputSHA != results[1].outputSHA {
		t.Fatalf("producer-visible row/bytes/hash depend on raw sentinel")
	}
	for _, sentinel := range sentinels {
		rawDigest := sha256Hex([]byte(sentinel))
		for _, visible := range [][]byte{results[0].rowJSON, results[1].rowJSON, results[0].output, results[1].output} {
			if bytes.Contains(visible, []byte(sentinel)) || bytes.Contains(visible, []byte(rawDigest)) {
				t.Fatalf("raw sentinel or its digest reached producer-visible bytes")
			}
		}
		if results[0].row.SourceEventHash == rawDigest || results[1].row.SourceEventHash == rawDigest || results[0].outputSHA == rawDigest || results[1].outputSHA == rawDigest {
			t.Fatal("raw sentinel digest became a producer-visible hash")
		}
	}
}

func TestStrictArtifactContainsOnlyApprovedIPBooleanFields(t *testing.T) {
	snapshot, _ := validPureStrictSnapshot(t)
	snapshot.CallLogs[0].IPAddressWasPresent = true
	snapshot.CallLogs[0].IPAddressRedacted = true
	if err := validateStrictSnapshotValuePolicy(snapshot); err != nil {
		t.Fatal(err)
	}

	isIPRelatedField := func(key string) bool {
		key = strings.ToLower(key)
		return strings.Contains(key, "ip_address") || strings.Contains(key, "raw_ip") ||
			strings.HasPrefix(key, "ip_") || strings.HasSuffix(key, "_ip") ||
			strings.Contains(key, "client_address") || strings.Contains(key, "remote_addr")
	}
	var reflected []string
	var allJSONTags []string
	typeOfCallLog := reflect.TypeOf(snapshotCallLog{})
	for idx := 0; idx < typeOfCallLog.NumField(); idx++ {
		tag := strings.Split(typeOfCallLog.Field(idx).Tag.Get("json"), ",")[0]
		allJSONTags = append(allJSONTags, tag)
		if isIPRelatedField(tag) {
			reflected = append(reflected, tag)
		}
	}
	sort.Strings(allJSONTags)
	const exactCallLogJSONContractSHA256 = "0c6afe35f0905ddd131caf9d55af8f84a27f7fdd5d943bb9634886fea99ed3f0"
	if len(allJSONTags) != 45 || sha256Hex([]byte(strings.Join(allJSONTags, "\n")+"\n")) != exactCallLogJSONContractSHA256 {
		t.Fatalf("snapshotCallLog JSON contract changed: count=%d digest=%s", len(allJSONTags), sha256Hex([]byte(strings.Join(allJSONTags, "\n")+"\n")))
	}
	sort.Strings(reflected)
	want := []string{"ip_address_redacted", "ip_address_was_present"}
	if !reflect.DeepEqual(reflected, want) {
		t.Fatalf("IP JSON tags=%v want=%v", reflected, want)
	}

	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	callLogs := decoded["call_logs"].([]any)
	callLog := callLogs[0].(map[string]any)
	var actual []string
	for key, value := range callLog {
		if isIPRelatedField(key) {
			actual = append(actual, key)
			if value != true {
				t.Fatalf("IP proof %s=%v want=true", key, value)
			}
		}
	}
	sort.Strings(actual)
	if !reflect.DeepEqual(actual, want) {
		t.Fatalf("artifact IP fields=%v want=%v", actual, want)
	}
	for _, forbiddenAlias := range []string{"ip_address", "raw_ip", "raw_ip_address", "client_ip", "source_ip", "remote_ip", "client_address", "remote_address"} {
		if _, exists := callLog[forbiddenAlias]; exists {
			t.Fatalf("forbidden raw-IP alias serialized: %s", forbiddenAlias)
		}
	}
}

func TestStrictRawErrorPresenceStillBlocksBeforeArtifact(t *testing.T) {
	snapshot, _ := validPureStrictSnapshot(t)
	snapshot.CallLogs[0].UpstreamErrorPresent = true
	err := validateStrictSnapshotValuePolicy(snapshot)
	if err == nil || !strings.Contains(err.Error(), "strict_source_blocked") {
		t.Fatalf("raw error presence did not block: %v", err)
	}
}

func TestStrictIPPresenceAggregateCountsAreBounded(t *testing.T) {
	for _, count := range []int64{0, 1} {
		aggregate := strictSourceAggregate{Ordinal: 1, CallLogs: 1, DistinctOrgs: 1, OrgID: 7, IPAddressPresentRows: count}
		if err := validateStrictSourceAggregates([]strictSourceAggregate{aggregate}, 1); err != nil {
			t.Fatalf("presence count %d rejected: %v", count, err)
		}
	}
	for _, aggregate := range []strictSourceAggregate{
		{Ordinal: 1, CallLogs: 1, DistinctOrgs: 1, OrgID: 7, IPAddressPresentRows: -1},
		{Ordinal: 1, CallLogs: 1, DistinctOrgs: 1, OrgID: 7, IPAddressPresentRows: 2},
		{Ordinal: 1, TraceEvents: 1, DistinctOrgs: 1, OrgID: 7, IPAddressPresentRows: 1},
	} {
		if err := validateStrictSourceAggregates([]strictSourceAggregate{aggregate}, 1); err == nil || err.Error() != "strict_source_aggregate_invalid: ip_presence_count" {
			t.Fatalf("out-of-range presence aggregate accepted: %#v err=%v", aggregate, err)
		}
	}
}

func TestStrictAggregateStillBlocksRawErrorAndOtherBlockedValueClassesBeforeDetail(t *testing.T) {
	for _, aggregate := range []strictSourceAggregate{
		{Ordinal: 1, CallLogs: 1, DistinctOrgs: 1, OrgID: 7, RawErrorRows: 1},
		{Ordinal: 1, CallLogs: 1, DistinctOrgs: 1, OrgID: 7, BlockedValueRows: 1},
	} {
		if err := validateStrictSourceAggregates([]strictSourceAggregate{aggregate}, 1); err == nil || !strings.Contains(err.Error(), "strict_source_blocked") {
			t.Fatalf("blocked aggregate accepted: %#v err=%v", aggregate, err)
		}
	}
	query := strictAggregateQuery(1)
	for _, marker := range []string{"blocked_value_present", "ip_address_present", "raw_error_present", "ghp_", "customer"} {
		if !strings.Contains(query, marker) {
			t.Fatalf("aggregate query missing safety marker %q", marker)
		}
	}
	if strings.Contains(query, "metadata::text") {
		t.Fatal("aggregate preflight must not apply field-agnostic detectors to serialized metadata")
	}
	for _, receiverMarker := range []string{
		"api[_-]?key", "authorization", "access[_-]?token", "refresh[_-]?token", "-----begin", "private[[:space:]]key",
		"aiza", "([+][0-9]{1,3}", "[0-9]{3,4}[ .-][0-9]{4}",
	} {
		if count := strings.Count(query, receiverMarker); count != 2 {
			t.Fatalf("aggregate receiver marker %q count=%d want=2", receiverMarker, count)
		}
	}
}

func TestStrictValuePolicyCoversEveryExportableStringReferenceAndMetadataPath(t *testing.T) {
	if len(strictMetadataRules) != 96 {
		t.Fatalf("metadata policy unexpectedly incomplete: %d rules", len(strictMetadataRules))
	}
	snapshot, _ := validPureStrictSnapshot(t)
	snapshot.CaptureGaps = []snapshotCaptureGap{{
		RequestID: snapshot.RequestIDs[0], Source: "call_logs", Gap: strictGapClass,
		CandidateGaps: []string{strictGapClass}, Reason: strictCallGapReason, Generated: true,
	}}
	paths := collectExportableStringPaths(reflect.TypeOf(snapshot), nil)
	if len(paths) < 65 {
		t.Fatalf("reflection coverage unexpectedly small: %d", len(paths))
	}
	for _, privateSentinel := range []string{"token=private-field-canary", "apikey:private-field-canary"} {
		for idx, path := range paths {
			copySnapshot := cloneSnapshotForTest(t, snapshot)
			setExportableStringPath(reflect.ValueOf(&copySnapshot).Elem(), path, privateSentinel)
			err := validateStrictSnapshotValuePolicy(copySnapshot)
			if err == nil || strings.Contains(err.Error(), privateSentinel) {
				t.Fatalf("string surface mutation %d path=%v err=%v", idx, path, err)
			}
		}
		for key := range snapshot.Notes {
			copySnapshot := cloneSnapshotForTest(t, snapshot)
			copySnapshot.Notes[key] = privateSentinel
			if err := validateStrictSnapshotValuePolicy(copySnapshot); err == nil || strings.Contains(err.Error(), privateSentinel) {
				t.Fatalf("notes surface %s err=%v", key, err)
			}
		}
		for key := range strictMetadataRules {
			copySnapshot := cloneSnapshotForTest(t, snapshot)
			copySnapshot.TraceEvents[0].Metadata = map[string]interface{}{key: privateSentinel}
			copySnapshot.TraceEvents[0].MetadataKeys = []string{key}
			if err := validateStrictSnapshotValuePolicy(copySnapshot); err == nil || strings.Contains(err.Error(), privateSentinel) {
				t.Fatalf("metadata surface %s err=%v", key, err)
			}
		}
	}
}

func cloneSnapshotForTest(t *testing.T, snapshot snapshotFile) snapshotFile {
	t.Helper()
	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var clone snapshotFile
	if err := json.Unmarshal(payload, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}

func collectExportableStringPaths(valueType reflect.Type, prefix []int) [][]int {
	var paths [][]int
	for idx := 0; idx < valueType.NumField(); idx++ {
		fieldType := valueType.Field(idx).Type
		path := append(append([]int(nil), prefix...), idx)
		switch {
		case fieldType.Kind() == reflect.String:
			paths = append(paths, path)
		case fieldType.Kind() == reflect.Ptr && fieldType.Elem().Kind() == reflect.String:
			paths = append(paths, path)
		case fieldType.Kind() == reflect.Struct:
			paths = append(paths, collectExportableStringPaths(fieldType, path)...)
		case fieldType.Kind() == reflect.Slice && fieldType.Elem().Kind() == reflect.String:
			paths = append(paths, path)
		case fieldType.Kind() == reflect.Slice && fieldType.Elem().Kind() == reflect.Struct:
			paths = append(paths, collectExportableStringPaths(fieldType.Elem(), append(path, -1))...)
		}
	}
	return paths
}

func setExportableStringPath(root reflect.Value, path []int, value string) {
	current := root
	for _, step := range path {
		if step == -1 {
			current = current.Index(0)
			continue
		}
		current = current.Field(step)
	}
	switch current.Kind() {
	case reflect.String:
		current.SetString(value)
	case reflect.Ptr:
		pointer := reflect.New(current.Type().Elem())
		pointer.Elem().SetString(value)
		current.Set(pointer)
	case reflect.Slice:
		current.Set(reflect.ValueOf([]string{value}))
	default:
		panic("unsupported exportable string path")
	}
}

func TestStrictSourceEventHashAcceptsRawLowercase64Hex(t *testing.T) {
	want := strings.Repeat("a", 64)
	actual, err := normalizeStrictSourceEventHash(sql.NullString{String: want, Valid: true}, struct{}{})
	if err != nil || actual != want {
		t.Fatalf("hash=%q err=%v", actual, err)
	}
}

func TestStrictSourceEventHashUsesValidLowercaseFallbackWhenSourceEmpty(t *testing.T) {
	for _, source := range []sql.NullString{{}, {String: "", Valid: true}} {
		hash, err := normalizeStrictSourceEventHash(source, map[string]string{"fixture": "safe"})
		if err != nil || !strictEventHashPattern.MatchString(hash) {
			t.Fatalf("source=%#v hash=%q err=%v", source, hash, err)
		}
	}
}

func TestStrictSourceEventHashRejectsUppercasePrefixedNonHexAndLengths63Or65(t *testing.T) {
	values := []string{
		strings.Repeat("A", 64), "sha256:" + strings.Repeat("a", 64), strings.Repeat("g", 64),
		strings.Repeat("a", 63), strings.Repeat("a", 65), " " + strings.Repeat("a", 64),
	}
	for _, value := range values {
		_, err := normalizeStrictSourceEventHash(sql.NullString{String: value, Valid: true}, struct{}{})
		if err == nil || strings.Contains(err.Error(), value) {
			t.Fatalf("value length=%d err=%v", len(value), err)
		}
	}
}

func TestStrictOutputAnonymousStageEveryPreCommitFailureCreatesNoDirectoryEntry(t *testing.T) {
	for _, tc := range []struct {
		name      string
		configure func(*fakeSnapshotStage)
	}{
		{name: "initial_stat", configure: func(s *fakeSnapshotStage) { s.statErrAt[1] = errors.New("injected") }},
		{name: "write", configure: func(s *fakeSnapshotStage) { s.writeErrAt = 1 }},
		{name: "zero_write", configure: func(s *fakeSnapshotStage) { s.zeroWriteAt = 1 }},
		{name: "fsync", configure: func(s *fakeSnapshotStage) { s.syncErr = errors.New("injected") }},
		{name: "post_write_stat", configure: func(s *fakeSnapshotStage) { s.statErrAt[2] = errors.New("injected") }},
		{name: "length", configure: func(s *fakeSnapshotStage) {
			bad := s.identity
			bad.Size = 999
			s.statValueAt[2] = bad
		}},
		{name: "read", configure: func(s *fakeSnapshotStage) { s.readErr = errors.New("injected") }},
		{name: "sha", configure: func(s *fakeSnapshotStage) { s.corruptRead = true }},
		{name: "final_stat", configure: func(s *fakeSnapshotStage) { s.statErrAt[3] = errors.New("injected") }},
		{name: "link", configure: func(s *fakeSnapshotStage) { s.commitErr = snapshotpublish.ErrCommitFailed }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			destination := filepath.Join(t.TempDir(), "snapshot.json")
			stage := newFakeSnapshotStage()
			tc.configure(stage)
			_, err := publishStrictSnapshot(stage, snapshotFile{SchemaVersion: prd5SnapshotSchemaVersion})
			if err == nil {
				t.Fatal("expected precommit failure")
			}
			if _, statErr := os.Lstat(destination); !errors.Is(statErr, fs.ErrNotExist) {
				t.Fatalf("destination exists: %v", statErr)
			}
			if tc.name != "link" && stage.commitCalls != 0 {
				t.Fatalf("commit called for pre-link failure: %d", stage.commitCalls)
			}
		})
	}
}

func TestStrictOutputRealAnonymousStageEveryPostPreparePreCommitFailureLeavesParentUnchanged(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("real anonymous publication is accepted on Linux only")
	}
	for _, failAt := range []string{"stat_1", "write", "fsync", "stat_2", "length", "read", "sha", "stat_3", "link"} {
		t.Run(failAt, func(t *testing.T) {
			parent := t.TempDir()
			if err := os.Chmod(parent, 0o700); err != nil {
				t.Fatal(err)
			}
			destination := filepath.Join(parent, "snapshot.json")
			realStage, err := snapshotpublish.Prepare(destination)
			if err != nil {
				t.Fatalf("required Linux backend must execute, not skip: %v", err)
			}
			stage := &faultInjectingSnapshotStage{Stage: realStage, failAt: failAt}
			var publishErr error
			stdout := captureStdout(t, func() {
				_, publishErr = publishStrictSnapshot(stage, snapshotFile{SchemaVersion: prd5SnapshotSchemaVersion})
			})
			if publishErr == nil {
				t.Fatal("expected injected precommit failure")
			}
			if len(stdout) != 0 {
				t.Fatalf("stdout was not empty: %q", stdout)
			}
			if _, err := os.Lstat(destination); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("destination exists after %s: %v", failAt, err)
			}
			entries, err := os.ReadDir(parent)
			if err != nil || len(entries) != 0 {
				t.Fatalf("parent entries after %s: %v err=%v", failAt, entries, err)
			}
		})
	}
}

func captureStdout(t *testing.T, run func()) []byte {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = write
	run()
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = original
	payload, err := io.ReadAll(read)
	if closeErr := read.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func assertStrictFailurePublishesNothing(
	t *testing.T,
	manifestPath, destination, want, privateCanary string,
	db *fakeStrictDB,
) {
	t.Helper()
	stage := &pathBoundCommitSentinelStage{fakeSnapshotStage: newFakeSnapshotStage(), destination: destination}
	prepareCalls := 0
	deps := defaultStrictSnapshotDeps()
	deps.prepareOutput = func(path string) (snapshotpublish.Stage, error) {
		prepareCalls++
		if path != destination {
			t.Fatalf("prepared path=%q want=%q", path, destination)
		}
		return stage, nil
	}
	deps.lookupEnv = func(string) string { return "synthetic-test-dsn" }
	deps.openDB = func(string) (strictSnapshotDB, error) { return db, nil }
	deps.now = func() time.Time { return time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC) }
	deps.newExchangeID = func() (string, error) { return "uxchg_0123456789abcdef0123456789abcdef", nil }
	assertTopLevelStrictFailurePublishesNothing(
		t,
		[]string{"--approval-manifest", manifestPath, "--output", destination},
		destination,
		want,
		privateCanary,
		deps,
		1,
		func() int { return stage.commitCalls },
	)
	if prepareCalls != 1 {
		t.Fatalf("prepare=%d", prepareCalls)
	}
}

func assertTopLevelStrictFailurePublishesNothing(
	t *testing.T,
	args []string,
	destination, want, privateCanary string,
	deps strictSnapshotDeps,
	expectedPrepareCalls int,
	commitCalls func() int,
) {
	t.Helper()
	prepareCalls := 0
	originalPrepare := deps.prepareOutput
	deps.prepareOutput = func(path string) (snapshotpublish.Stage, error) {
		prepareCalls++
		return originalPrepare(path)
	}
	var runErr error
	stdout := captureStdout(t, func() { runErr = runPRD5SnapshotWithDeps(args, deps) })
	if runErr == nil || !strings.Contains(runErr.Error(), want) {
		t.Fatalf("expected %q failure, got %v", want, runErr)
	}
	if privateCanary != "" && strings.Contains(runErr.Error(), privateCanary) {
		t.Fatalf("private canary leaked: %v", runErr)
	}
	if len(stdout) != 0 {
		t.Fatalf("stdout=%q", stdout)
	}
	if prepareCalls != expectedPrepareCalls || commitCalls() != 0 {
		t.Fatalf("prepare=%d commit=%d", prepareCalls, commitCalls())
	}
	if _, err := os.Lstat(destination); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("destination exists: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(destination))
	if err != nil || len(entries) != 0 {
		t.Fatalf("destination parent entries=%v err=%v", entries, err)
	}
}

func TestStrictG02ThroughG09RepresentativeFailuresPublishNothing(t *testing.T) {
	const privateCanary = "token=private-run-canary"
	for _, tc := range []struct {
		name, want string
		mutate     func(*fakeStrictDB)
	}{
		{name: "g02_transaction", want: "strict_transaction_mode_invalid", mutate: func(db *fakeStrictDB) { db.tx.isolation = "read committed" }},
		{name: "g03_row_cap", want: "strict_row_limit", mutate: func(db *fakeStrictDB) {
			db.tx.queryResults[1].rows = &fakeSnapshotRows{values: [][]any{{1, int64(1), int64(200), int64(1), sql.NullInt64{Int64: 7, Valid: true}, int64(0), int64(0), int64(0)}}}
		}},
		{name: "g05_duplicate_call", want: "strict_source_duplicate_call_log", mutate: func(db *fakeStrictDB) {
			db.tx.queryResults[1].rows = &fakeSnapshotRows{values: [][]any{{1, int64(2), int64(1), int64(1), sql.NullInt64{Int64: 7, Valid: true}, int64(0), int64(0), int64(0)}}}
		}},
		{name: "g06_reconciliation", want: "strict_reconciliation_failed", mutate: func(db *fakeStrictDB) {
			db.tx.queryResults[2].rows = &fakeSnapshotRows{}
		}},
		{name: "g07_payload", want: "strict_payload_contract", mutate: func(db *fakeStrictDB) {
			values := validCallLogScanValues(1, "req-1", 7)
			values[37] = sql.NullString{String: privateCanary, Valid: true}
			db.tx.queryResults[2].rows = &fakeSnapshotRows{values: [][]any{values}}
		}},
		{name: "g08_value_policy", want: "strict_value_policy", mutate: func(db *fakeStrictDB) {
			values := validCallLogScanValues(1, "req-1", 7)
			values[7] = privateCanary
			db.tx.queryResults[2].rows = &fakeSnapshotRows{values: [][]any{values}}
		}},
		{name: "g09_event_hash", want: "strict_event_hash_invalid", mutate: func(db *fakeStrictDB) {
			values := validCallLogScanValues(1, "req-1", 7)
			values[32] = sql.NullString{String: "INVALID-HASH-" + privateCanary, Valid: true}
			db.tx.queryResults[2].rows = &fakeSnapshotRows{values: [][]any{values}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manifestPath := writeStrictManifestFixture(t, []approvalManifestRequest{oneStrictManifestRequest("req-1")})
			destination := filepath.Join(t.TempDir(), "snapshot.json")
			db := validStrictDBForRequests([]string{"req-1"})
			tc.mutate(db)
			assertStrictFailurePublishesNothing(t, manifestPath, destination, tc.want, privateCanary, db)
		})
	}
}

func TestStrictOutputWriteAllRecoversFromInterruptedAndShortWrites(t *testing.T) {
	stage := newFakeSnapshotStage()
	stage.writeEINTRAt = 1
	stage.writeLimit = 7
	snapshot := snapshotFile{SchemaVersion: prd5SnapshotSchemaVersion, Source: "ordinary-test-fixture"}
	result, err := publishStrictSnapshot(stage, snapshot)
	if err != nil || !result.Published || stage.commitCalls != 1 || stage.writeCalls < 3 {
		t.Fatalf("result=%#v err=%v commits=%d writes=%d", result, err, stage.commitCalls, stage.writeCalls)
	}
	if result.Length != int64(len(stage.data)) || result.SHA256 != sha256Hex(stage.data) {
		t.Fatalf("result=%#v data_len=%d", result, len(stage.data))
	}
}

func TestStrictOutputUnsupportedAnonymousPublicationFailsBeforeDSNAndOutput(t *testing.T) {
	manifestPath := writeStrictManifestFixture(t, []approvalManifestRequest{oneStrictManifestRequest("req-1")})
	destination := filepath.Join(t.TempDir(), "snapshot.json")
	prepareCalls, envCalls, dbCalls := 0, 0, 0
	deps := defaultStrictSnapshotDeps()
	deps.prepareOutput = func(string) (snapshotpublish.Stage, error) {
		prepareCalls++
		return nil, snapshotpublish.ErrUnsupported
	}
	deps.lookupEnv = func(string) string { envCalls++; return "synthetic" }
	deps.openDB = func(string) (strictSnapshotDB, error) { dbCalls++; return nil, errors.New("must not run") }
	assertTopLevelStrictFailurePublishesNothing(
		t,
		[]string{"--approval-manifest", manifestPath, "--output", destination},
		destination,
		"strict_output_unsupported",
		"req-1",
		deps,
		1,
		func() int { return 0 },
	)
	if prepareCalls != 1 || envCalls != 0 || dbCalls != 0 {
		t.Fatalf("prepare=%d env=%d db=%d", prepareCalls, envCalls, dbCalls)
	}
}

func TestStrictOutputRejectsDestinationOptionsBeforePreparation(t *testing.T) {
	for _, tc := range []struct {
		name, output, handoff string
		overwrite             bool
	}{
		{name: "stdout", output: "-"},
		{name: "empty", output: ""},
		{name: "relative", output: "snapshot.json"},
		{name: "unnormalized", output: "/private/tmp/../tmp/snapshot.json"},
		{name: "overwrite", output: "/private/tmp/snapshot.json", overwrite: true},
		{name: "handoff", output: "/private/tmp/snapshot.json", handoff: "/private/tmp/handoff.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateStrictOutputOptions(tc.output, tc.handoff, tc.overwrite); err == nil {
				t.Fatal("invalid strict output options accepted")
			}
		})
	}
}

func TestStrictOutputRealParentBoundariesFailWithoutEntry(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("real parent descriptor checks are Linux-only")
	}
	base := t.TempDir()
	validParent := filepath.Join(base, "valid")
	if err := os.Mkdir(validParent, 0o700); err != nil {
		t.Fatal(err)
	}
	wrongModeParent := filepath.Join(base, "wrong-mode")
	if err := os.Mkdir(wrongModeParent, 0o755); err != nil {
		t.Fatal(err)
	}
	symlinkParent := filepath.Join(base, "symlink-parent")
	if err := os.Symlink(validParent, symlinkParent); err != nil {
		t.Fatal(err)
	}
	for _, parent := range []string{filepath.Join(base, "missing"), wrongModeParent, symlinkParent} {
		destination := filepath.Join(parent, "snapshot.json")
		stage, err := snapshotpublish.Prepare(destination)
		if stage != nil || err == nil {
			t.Fatalf("parent=%s stage=%v err=%v", filepath.Base(parent), stage, err)
		}
		if _, statErr := os.Lstat(destination); !errors.Is(statErr, fs.ErrNotExist) {
			t.Fatalf("parent=%s destination exists: %v", filepath.Base(parent), statErr)
		}
	}
}

func TestStrictOutputRealNonLinuxImplementationIsUnsupported(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("Linux is covered by required real backend tests")
	}
	stage, err := snapshotpublish.Prepare(filepath.Join(t.TempDir(), "snapshot.json"))
	if stage != nil || !errors.Is(err, snapshotpublish.ErrUnsupported) {
		t.Fatalf("stage=%v err=%v", stage, err)
	}
}

func TestStrictOutputAtomicNoReplacePreservesExistingFileAndSymlink(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("real anonymous publication is accepted on Linux only")
	}
	for _, kind := range []string{"file", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			parent := t.TempDir()
			if err := os.Chmod(parent, 0o700); err != nil {
				t.Fatal(err)
			}
			destination := filepath.Join(parent, "snapshot.json")
			if kind == "file" {
				if err := os.WriteFile(destination, []byte("winner-bytes"), 0o640); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Symlink("winner-target", destination); err != nil {
				t.Fatal(err)
			}
			before := lstatSignature(t, destination)
			stage, err := snapshotpublish.Prepare(destination)
			if stage != nil || !errors.Is(err, fs.ErrExist) {
				t.Fatalf("stage=%v err=%v", stage, err)
			}
			after := lstatSignature(t, destination)
			if before != after {
				t.Fatalf("winner identity changed: before=%s after=%s", before, after)
			}
			if kind == "file" {
				payload, err := os.ReadFile(destination)
				if err != nil || string(payload) != "winner-bytes" {
					t.Fatalf("winner bytes=%q err=%v", payload, err)
				}
			} else if target, err := os.Readlink(destination); err != nil || target != "winner-target" {
				t.Fatalf("symlink target=%q err=%v", target, err)
			}
		})
	}
}

func TestStrictOutputAtomicNoReplaceRacePreservesWinner(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("real anonymous publication is accepted on Linux only")
	}
	for _, kind := range []string{"file", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			parent := t.TempDir()
			if err := os.Chmod(parent, 0o700); err != nil {
				t.Fatal(err)
			}
			destination := filepath.Join(parent, "snapshot.json")
			stage, err := snapshotpublish.Prepare(destination)
			if err != nil {
				t.Fatalf("Prepare: %v", err)
			}
			if kind == "file" {
				if err := os.WriteFile(destination, []byte("race-winner"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Symlink("race-target", destination); err != nil {
				t.Fatal(err)
			}
			before := lstatSignature(t, destination)
			_, err = publishStrictSnapshot(stage, snapshotFile{SchemaVersion: prd5SnapshotSchemaVersion})
			if err == nil || !strings.Contains(err.Error(), "target_exists") {
				t.Fatalf("expected EEXIST commit, got %v", err)
			}
			if after := lstatSignature(t, destination); after != before {
				t.Fatalf("race winner changed: before=%s after=%s", before, after)
			}
			if kind == "file" {
				payload, readErr := os.ReadFile(destination)
				if readErr != nil || string(payload) != "race-winner" {
					t.Fatalf("race winner bytes=%q err=%v", payload, readErr)
				}
			} else if target, readErr := os.Readlink(destination); readErr != nil || target != "race-target" {
				t.Fatalf("race winner symlink target=%q err=%v", target, readErr)
			}
		})
	}
}

func TestStrictOutputAnonymousStageAtomicLinkPublishesExactRegularOwner0600LinkCountOneBytes(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("real anonymous publication is accepted on Linux only")
	}
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(parent, "snapshot.json")
	stage, err := snapshotpublish.Prepare(destination)
	if err != nil {
		t.Fatalf("Linux O_TMPFILE preflight must not skip or downgrade: %v", err)
	}
	before, err := stage.Stat()
	if err != nil || before.Nlink != 0 || !before.Regular {
		t.Fatalf("anonymous identity=%#v err=%v", before, err)
	}
	if _, err := os.Lstat(destination); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("destination visible before commit: %v", err)
	}
	snapshot := snapshotFile{SchemaVersion: prd5SnapshotSchemaVersion, Source: "ordinary-test-fixture"}
	result, err := publishStrictSnapshot(stage, snapshot)
	if err != nil || !result.Published {
		t.Fatalf("publish result=%#v err=%v", result, err)
	}
	var expected bytes.Buffer
	encoder := json.NewEncoder(&expected)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(snapshot); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(destination)
	if err != nil || !bytes.Equal(payload, expected.Bytes()) {
		t.Fatalf("payload mismatch err=%v", err)
	}
	info, err := os.Lstat(destination)
	if err != nil {
		t.Fatal(err)
	}
	stat := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 || uint64(stat.Ino) != before.Inode {
		t.Fatalf("published identity mode=%v stat=%#v before=%#v", info.Mode(), stat, before)
	}
	if result.Length != int64(len(payload)) || result.SHA256 != sha256Hex(payload) {
		t.Fatalf("result=%#v payload_len=%d", result, len(payload))
	}
}

func TestStrictOutputLinkSuccessIsCommitWithoutPostCommitFailureDowngrade(t *testing.T) {
	stage := newFakeSnapshotStage()
	stage.closeErr = errors.New("post-commit close diagnostic")
	result, err := publishStrictSnapshot(stage, snapshotFile{SchemaVersion: prd5SnapshotSchemaVersion})
	if err != nil || !result.Published || stage.commitCalls != 1 || stage.closeCalls != 1 {
		t.Fatalf("result=%#v err=%v commit=%d close=%d", result, err, stage.commitCalls, stage.closeCalls)
	}
}

func TestStrictOutputRealLinkSuccessRemainsPublishedWhenCloseReportsDiagnostic(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("real anonymous publication is accepted on Linux only")
	}
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(parent, "snapshot.json")
	realStage, err := snapshotpublish.Prepare(destination)
	if err != nil {
		t.Fatalf("required Linux backend must execute, not skip: %v", err)
	}
	snapshot := snapshotFile{SchemaVersion: prd5SnapshotSchemaVersion, Source: "ordinary-test-fixture"}
	result, err := publishStrictSnapshot(&closeDiagnosticSnapshotStage{Stage: realStage}, snapshot)
	if err != nil || !result.Published {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	payload, err := os.ReadFile(destination)
	if err != nil || result.Length != int64(len(payload)) || result.SHA256 != sha256Hex(payload) {
		t.Fatalf("published target result=%#v bytes=%d err=%v", result, len(payload), err)
	}
}

func TestStrictOutputNeverUsesNamedTempRenameDirectFinalCreateOrCleanup(t *testing.T) {
	parent := t.TempDir()
	destination := filepath.Join(parent, "snapshot.json")
	stage := newFakeSnapshotStage()
	result, err := publishStrictSnapshot(stage, snapshotFile{SchemaVersion: prd5SnapshotSchemaVersion})
	if err != nil || !result.Published || stage.commitCalls != 1 {
		t.Fatalf("result=%#v err=%v commits=%d", result, err, stage.commitCalls)
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("fake state machine created a named entry: %v", entries)
	}
	if _, err := os.Lstat(destination); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("direct final-name creation occurred: %v", err)
	}
}

func lstatSignature(t *testing.T, path string) string {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	stat := info.Sys().(*syscall.Stat_t)
	return fmt.Sprintf("dev=%d ino=%d mode=%d nlink=%d uid=%d size=%d", stat.Dev, stat.Ino, stat.Mode, stat.Nlink, stat.Uid, stat.Size)
}
