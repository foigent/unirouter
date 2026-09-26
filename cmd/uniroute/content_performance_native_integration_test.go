//go:build integration

package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"runtime/trace"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/limitpolicy"
	"github.com/uniroute/unirouter/internal/outbox"
	"github.com/uniroute/unirouter/internal/testutil"
	"github.com/uniroute/unirouter/internal/workloadadmission"
)

// These are the exact 1.4.6 workload scripts. The offline real-Service test
// below detects drift; no script, key, arguments or arbitrary error is exported.
const contentPerformanceReserveSHA = "e55bed2735d2743308feb385671fde84dc6c42eb"
const contentPerformanceReleaseSHA = "080deedf342d8f208467d1646e929664550f9a76"
const contentPerformanceNativeLimit = 6020
const contentPerformanceDecisionLimit = 64

type contentPerformanceSafeDecision struct {
	Kind     string                          `json:"kind"`
	Decision string                          `json:"decision"`
	Reason   string                          `json:"reason"`
	Mode     string                          `json:"mode"`
	Workload *contentPerformanceSafeWorkload `json:"workload,omitempty"`
}
type contentPerformanceSafeWorkload struct {
	Class          string `json:"class"`
	Weight         int64  `json:"weight"`
	WaitMS         int64  `json:"wait_ms"`
	QueueDepth     int64  `json:"queue_depth"`
	GlobalCapacity int64  `json:"global_capacity"`
	GlobalUsed     int64  `json:"global_used"`
	// The collector does not carry OrgUsed. Absence must never become numeric 0.
	OrgUsedAvailable bool `json:"org_used_available"`
}
type contentPerformanceDecisionEvidence struct {
	CollectorPresent bool                             `json:"collector_present"`
	WorkloadPresent  bool                             `json:"workload_present"`
	Truncated        bool                             `json:"truncated"`
	Decisions        []contentPerformanceSafeDecision `json:"decisions"`
}
type contentPerformanceCommandInterval struct {
	StartNS       int64  `json:"start_ns"`
	EndNS         int64  `json:"end_ns"`
	GlobalUsed    int64  `json:"global_used"`
	OrgUsed       int64  `json:"org_used"`
	RedisNowMS    *int64 `json:"redis_now_ms,omitempty"`
	LeaseExpiryMS *int64 `json:"lease_expiry_ms,omitempty"`
}
type contentPerformanceNativeRow struct {
	OperationID       string                              `json:"operation_id"`
	Marker            string                              `json:"marker"`
	Decisions         *contentPerformanceDecisionEvidence `json:"admission_decisions,omitempty"`
	ReserveInFlight   int64                               `json:"reserve_in_flight_at_snapshot"`
	ReleaseInFlight   int64                               `json:"release_in_flight_at_snapshot"`
	ReserveCalls      int64                               `json:"reserve_calls"`
	ReserveWaits      int64                               `json:"reserve_wait_results"`
	ReserveDenials    int64                               `json:"reserve_denial_results"`
	ReserveIdempotent int64                               `json:"reserve_idempotent_results"`
	ReserveUnknown    int64                               `json:"reserve_unknown_results"`
	ReserveCacheMiss  int64                               `json:"reserve_script_cache_misses"`
	ReleaseCalls      int64                               `json:"release_calls"`
	ReleaseErrors     int64                               `json:"release_error_results"`
	ReleaseUnknown    int64                               `json:"release_unknown_results"`
	ReleaseCacheMiss  int64                               `json:"release_script_cache_misses"`
	ReleaseConflicts  int64                               `json:"release_conflict_results"`
	ReleaseAcks       int64                               `json:"release_ack_results"`
	FirstGrant        *contentPerformanceCommandInterval  `json:"first_observed_grant,omitempty"`
	FirstReleaseAck   *contentPerformanceCommandInterval  `json:"first_observed_release_ack,omitempty"`
	FirstErrorClass   string                              `json:"first_error_class,omitempty"`
	Status            string                              `json:"status"`
	RightCensored     bool                                `json:"right_censored"`
}
type contentPerformanceNativeEntry struct {
	owner *contentPerformanceNativeDiagnostics
	row   contentPerformanceNativeRow
	orgID int64
	ctx   context.Context
	task  *trace.Task
}
type contentPerformanceNativeDiagnostics struct {
	mu           sync.Mutex
	start        time.Time
	entries      map[string]*contentPerformanceNativeEntry // private hashed member -> known operation
	closed       bool
	overflow     bool
	unassociated int64
	malformed    int64
	late         int64
}

func newContentPerformanceNativeDiagnostics(enabled bool) *contentPerformanceNativeDiagnostics {
	if !enabled {
		return nil
	}
	return &contentPerformanceNativeDiagnostics{start: time.Now(), entries: make(map[string]*contentPerformanceNativeEntry)}
}
func contentPerformanceSyntheticOperation(id string) bool {
	if len(id) != 35 || !strings.HasPrefix(id, "op_") {
		return false
	}
	for _, c := range id[3:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func contentPerformanceMember(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:16])
}
func (d *contentPerformanceNativeDiagnostics) begin(ctx context.Context, id, marker string) (context.Context, *contentPerformanceNativeEntry) {
	if d == nil || !contentPerformanceSyntheticOperation(id) || contentPerformanceMarker(marker) == "" {
		return ctx, nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	member := contentPerformanceMember(id)
	if d.closed {
		d.late++
		return ctx, nil
	}
	if _, exists := d.entries[member]; exists {
		return ctx, nil
	}
	if len(d.entries) >= contentPerformanceNativeLimit {
		d.overflow = true
		return ctx, nil
	}
	taskCtx, task := trace.NewTask(ctx, "cc13.operation")
	trace.Log(taskCtx, "operation", id)
	trace.Log(taskCtx, "marker", marker)
	entry := &contentPerformanceNativeEntry{owner: d, row: contentPerformanceNativeRow{OperationID: id, Marker: marker}, ctx: taskCtx, task: task}
	d.entries[member] = entry
	return taskCtx, entry
}
func (e *contentPerformanceNativeEntry) bind(orgID int64) {
	if e == nil || orgID <= 0 {
		return
	}
	d := e.owner
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.closed && e.orgID == 0 {
		e.orgID = orgID
	}
}
func contentPerformanceSafeEnum(value string, allowed ...string) string {
	for _, v := range allowed {
		if value == v {
			return v
		}
	}
	return "other_redacted"
}
func contentPerformanceCollectDecisions(ctx context.Context) contentPerformanceDecisionEvidence {
	c := limitpolicy.DecisionCollectorFromContext(ctx)
	result := contentPerformanceDecisionEvidence{CollectorPresent: c != nil, Decisions: []contentPerformanceSafeDecision{}}
	values, _ := c.Snapshot()
	result.Truncated = len(values) > contentPerformanceDecisionLimit
	for _, v := range values[:min(len(values), contentPerformanceDecisionLimit)] {
		row := contentPerformanceSafeDecision{Kind: "policy", Decision: contentPerformanceSafeEnum(v.Decision, "allow", "deny", "would_deny"), Mode: contentPerformanceSafeEnum(v.Mode, "enforce", "shadow", "disabled"), Reason: contentPerformanceSafeEnum(v.Reason, "allowed", "rps_exceeded", "rpm_exceeded", "tpm_exceeded", "concurrency_exceeded", "state_unavailable", "last_known_safe", "operation_conflict", "policy_chain_too_large", "policy_snapshot_stale", "workload_capacity_exceeded", "workload_queue_full", "workload_configuration_conflict")}
		if v.PolicyRef == "builtin:workload-admission-v2" {
			row.Kind = "workload"
			result.WorkloadPresent = true
			row.Workload = &contentPerformanceSafeWorkload{Class: contentPerformanceSafeEnum(v.WorkloadClass, "interactive", "stream", "embedding"), Weight: v.WorkloadWeight, WaitMS: v.WaitMS, QueueDepth: v.QueueDepth, GlobalCapacity: v.Limits.Concurrency, GlobalUsed: v.Observed.Concurrency}
		}
		result.Decisions = append(result.Decisions, row)
	}
	return result
}
func (e *contentPerformanceNativeEntry) collect(ctx context.Context) {
	if e == nil {
		return
	}
	evidence := contentPerformanceCollectDecisions(ctx)
	d := e.owner
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.closed {
		e.row.Decisions = &evidence
	}
}
func (e *contentPerformanceNativeEntry) decisions() *contentPerformanceDecisionEvidence {
	if e == nil {
		return nil
	}
	d := e.owner
	d.mu.Lock()
	defer d.mu.Unlock()
	if e.row.Decisions == nil {
		return nil
	}
	copy := *e.row.Decisions
	copy.Decisions = append([]contentPerformanceSafeDecision{}, copy.Decisions...)
	return &copy
}
func (e *contentPerformanceNativeEntry) mark(name, result string) {
	if e != nil {
		trace.Log(e.ctx, name, result)
	}
}
func (d *contentPerformanceNativeDiagnostics) endTasks() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	d.closed = true
	for _, e := range d.entries {
		e.task.End()
	}
}
func (d *contentPerformanceNativeDiagnostics) evidence(joined bool) map[string]any {
	d.endTasks()
	d.mu.Lock()
	defer d.mu.Unlock()
	rows := make([]contentPerformanceNativeRow, 0, len(d.entries))
	for _, e := range d.entries {
		row := e.row
		switch {
		case row.FirstGrant != nil && row.FirstReleaseAck != nil && row.FirstReleaseAck.EndNS < row.FirstGrant.StartNS:
			row.Status = "release_ack_precedes_observed_grant"
			row.RightCensored = true
		case row.FirstGrant != nil && row.FirstReleaseAck != nil:
			row.Status = "grant_and_release_ack_observed_atomic_release_unknown"
		case row.FirstGrant != nil:
			row.Status = "grant_observed_no_release_ack"
			row.RightCensored = true
		case row.ReserveUnknown > 0 || row.ReserveInFlight > 0:
			row.Status = "acquisition_unknown"
			row.RightCensored = true
		case row.ReserveIdempotent > 0:
			row.Status = "prior_acquisition_not_observed"
			row.RightCensored = row.FirstReleaseAck == nil
		default:
			row.Status = "no_grant_observed"
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].OperationID < rows[j].OperationID })
	return map[string]any{"schema": "cc13.native-diagnostics.v1", "clock": "monotonic_since_diagnostic_window_start", "window_started_at": d.start.UTC().Format(time.RFC3339Nano), "window_end_ns": time.Since(d.start).Nanoseconds(), "terminal_worker_joined": joined, "operation_limit": contentPerformanceNativeLimit, "operation_overflow": d.overflow, "unassociated_known_script_calls": d.unassociated, "malformed_known_script_calls": d.malformed, "late_calls": d.late, "rows": rows, "boundary": "command intervals include client pool/IO/retries; release code 0 is only an ack, not proof or timestamp of first atomic release; missing acknowledgements are censored; no Redis keys/arguments, SQL or credentials"}
}
func (d *contentPerformanceNativeDiagnostics) outboxRegion(name string, next outbox.Handler) outbox.Handler {
	if d == nil {
		return next
	}
	return func(ctx context.Context, event outbox.Event) error {
		d.mu.Lock()
		entry := d.entries[contentPerformanceMember(event.RequestID)]
		known := entry != nil && entry.row.OperationID == event.RequestID && entry.orgID == event.OrgID && !d.closed
		d.mu.Unlock()
		if !known {
			return next(ctx, event)
		}
		region := trace.StartRegion(entry.ctx, name)
		defer region.End()
		return next(ctx, event) // Preserve the worker's original context and payload.
	}
}
func (d *contentPerformanceNativeDiagnostics) DialHook(next redis.DialHook) redis.DialHook {
	return next
}
func (d *contentPerformanceNativeDiagnostics) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func contentPerformanceCommandFamily(cmd redis.Cmder) string {
	args := cmd.Args()
	if len(args) < 3 {
		return ""
	}
	value, ok := args[1].(string)
	if !ok {
		return ""
	}
	switch cmd.Name() {
	case "evalsha":
	case "eval":
		sum := sha1.Sum([]byte(value))
		value = hex.EncodeToString(sum[:])
	default:
		return ""
	}
	switch value {
	case contentPerformanceReserveSHA:
		return "reserve"
	case contentPerformanceReleaseSHA:
		return "release"
	}
	return ""
}
func contentPerformanceExactKeys(args []interface{}, orgID int64) bool {
	if len(args) < 17 || args[2] != 14 {
		return false
	}
	prefix := "workload:{admission-v1}:"
	suffixes := []string{"leases", "lease-weights", "lease-orgs", "global-total", "org-totals", "queue:interactive", "queue:stream", "queue:embedding", "queue-deadlines", "metadata", "fairness", "fairness:org:" + strconv.FormatInt(orgID, 10), "config-version", "queue-org-counts"}
	for i, suffix := range suffixes {
		if args[3+i] != prefix+suffix {
			return false
		}
	}
	return true
}
func contentPerformanceNumericResult(cmd redis.Cmder, count int) ([]int64, bool) {
	value, ok := cmd.(*redis.Cmd)
	if !ok {
		return nil, false
	}
	raw, ok := value.Val().([]interface{})
	if !ok || len(raw) != count {
		return nil, false
	}
	result := make([]int64, count)
	for i, v := range raw {
		number, ok := v.(int64)
		if !ok || number < 0 {
			return nil, false
		}
		result[i] = number
	}
	return result, true
}
func (d *contentPerformanceNativeDiagnostics) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		family := contentPerformanceCommandFamily(cmd)
		if family == "" {
			return next(ctx, cmd)
		}
		args := cmd.Args()
		want := 35
		if family == "release" {
			want = 20
		}
		d.mu.Lock()
		if d.closed {
			d.late++
			d.mu.Unlock()
			return next(ctx, cmd)
		}
		if len(args) != want {
			d.malformed++
			d.mu.Unlock()
			return next(ctx, cmd)
		}
		member, ok := args[17].(string)
		entry := d.entries[member]
		if !ok || entry == nil || entry.orgID == 0 {
			d.unassociated++
			d.mu.Unlock()
			return next(ctx, cmd)
		}
		if !contentPerformanceExactKeys(args, entry.orgID) {
			d.malformed++
			d.mu.Unlock()
			return next(ctx, cmd)
		}
		if family == "reserve" {
			entry.row.ReserveCalls++
			entry.row.ReserveInFlight++
		} else {
			entry.row.ReleaseCalls++
			entry.row.ReleaseInFlight++
		}
		d.mu.Unlock()
		region := trace.StartRegion(entry.ctx, "workload."+family)
		defer region.End()
		start := time.Since(d.start).Nanoseconds()
		err := next(ctx, cmd)
		end := time.Since(d.start).Nanoseconds()
		observedErr := err
		if observedErr == nil {
			observedErr = cmd.Err()
		}
		count := 8
		if family == "release" {
			count = 3
		}
		values, valid := contentPerformanceNumericResult(cmd, count)
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.closed {
			d.late++
			return err
		}
		row := &entry.row
		if family == "reserve" {
			row.ReserveInFlight--
		} else {
			row.ReleaseInFlight--
		}
		if observedErr != nil {
			if redis.HasErrorPrefix(observedErr, "NOSCRIPT") {
				if family == "reserve" {
					row.ReserveCacheMiss++
				} else {
					row.ReleaseCacheMiss++
				}
			} else {
				if family == "reserve" {
					row.ReserveUnknown++
				} else {
					row.ReleaseErrors++
				}
				if row.FirstErrorClass == "" {
					row.FirstErrorClass = contentPerformanceErrorClass(observedErr)
				}
			}
			return err
		}
		if !valid {
			if family == "reserve" {
				row.ReserveUnknown++
			} else {
				row.ReleaseUnknown++
			}
			return err
		}
		interval := &contentPerformanceCommandInterval{StartNS: start, EndNS: end, GlobalUsed: values[1], OrgUsed: values[2]}
		if family == "release" {
			switch values[0] {
			case 0:
				row.ReleaseAcks++
				if row.FirstReleaseAck == nil {
					row.FirstReleaseAck = interval
				}
			case 2:
				row.ReleaseConflicts++
			default:
				row.ReleaseUnknown++
			}
		} else {
			switch values[0] {
			case 0:
				interval.RedisNowMS = &values[3]
				interval.LeaseExpiryMS = &values[7]
				if row.FirstGrant == nil {
					row.FirstGrant = interval
				}
			case 1:
				row.ReserveWaits++
			case 3:
				row.ReserveIdempotent++
			case 2, 4, 5, 6:
				row.ReserveDenials++
			default:
				row.ReserveUnknown++
			}
		}
		return err
	}
}

func contentPerformanceNativeTraceActive() bool { return trace.IsEnabled() }

type contentPerformanceDatabaseBinding struct {
	Name string `json:"name"`
	OID  int64  `json:"oid"`
}

func contentPerformanceReadDatabaseBinding(ctx context.Context, pool db.DBQuerier) (contentPerformanceDatabaseBinding, error) {
	var binding contentPerformanceDatabaseBinding
	err := pool.QueryRow(ctx, `SELECT current_database(), (SELECT oid::bigint FROM pg_database WHERE datname=current_database())`).Scan(&binding.Name, &binding.OID)
	if err == nil && (binding.Name == "" || binding.OID <= 0) {
		err = errors.New("invalid diagnostic database identity")
	}
	return binding, err
}

func contentPerformanceNativeTestEntry(d *contentPerformanceNativeDiagnostics, n int) *contentPerformanceNativeEntry {
	_, entry := d.begin(context.Background(), fmt.Sprintf("op_%032x", n), fmt.Sprintf("r0-plain-%d", n%3000))
	entry.bind(7)
	return entry
}
func contentPerformanceNativeTestCommand(entry *contentPerformanceNativeEntry, family string) *redis.Cmd {
	hash := contentPerformanceReserveSHA
	count := 18
	if family == "release" {
		hash = contentPerformanceReleaseSHA
		count = 3
	}
	args := []interface{}{"evalsha", hash, 14}
	for _, key := range []string{"leases", "lease-weights", "lease-orgs", "global-total", "org-totals", "queue:interactive", "queue:stream", "queue:embedding", "queue-deadlines", "metadata", "fairness", "fairness:org:7", "config-version", "queue-org-counts"} {
		args = append(args, "workload:{admission-v1}:"+key)
	}
	args = append(args, contentPerformanceMember(entry.row.OperationID))
	for i := 1; i < count; i++ {
		args = append(args, "private-argument-must-not-export")
	}
	return redis.NewCmd(context.Background(), args...)
}
func contentPerformanceNativeTestReply(code int64) []interface{} {
	return []interface{}{code, int64(2), int64(1), int64(1800000000000), int64(5), int64(25), int64(3), int64(1800000060000)}
}

func TestContentPerformanceNativeDiagnosticsOffline(t *testing.T) {
	t.Run("default_off_and_safe_identity", func(t *testing.T) {
		require.Nil(t, newContentPerformanceNativeDiagnostics(false))
		d := newContentPerformanceNativeDiagnostics(true)
		defer d.endTasks()
		for _, id := range []string{"secret-token", "op_short", "op_" + strings.Repeat("X", 32)} {
			_, entry := d.begin(t.Context(), id, "r0-plain-0")
			require.Nil(t, entry)
		}
		_, entry := d.begin(t.Context(), "op_"+strings.Repeat("a", 32), "secret-header")
		require.Nil(t, entry)
		require.Empty(t, d.entries)
	})
	t.Run("collector_numeric_whitelist_no_org_used_or_secrets", func(t *testing.T) {
		missing := contentPerformanceCollectDecisions(t.Context())
		require.False(t, missing.CollectorPresent)
		require.False(t, missing.WorkloadPresent)
		ctx, _ := limitpolicy.ContextWithDecisionCollector(t.Context())
		limitpolicy.RecordSupplementalDecision(ctx, limitpolicy.Decision{PolicyRef: "builtin:workload-admission-v2", PolicyName: "secret-name", SelectorHash: "secret-selector", WorkloadClass: "interactive", WorkloadWeight: 1, WaitMS: 51, QueueDepth: 4, Limits: limitpolicy.Limits{Concurrency: 100}, Observed: limitpolicy.Usage{Concurrency: 10}, Decision: "deny", Reason: "workload_capacity_exceeded", Mode: "enforce"})
		limitpolicy.RecordSupplementalDecision(ctx, limitpolicy.Decision{PolicyRef: "secret-ref", Decision: "secret-decision", Reason: "secret-reason", Mode: "secret-mode"})
		result := contentPerformanceCollectDecisions(ctx)
		require.True(t, result.WorkloadPresent)
		require.EqualValues(t, 51, result.Decisions[0].Workload.WaitMS)
		wire, err := json.Marshal(result)
		require.NoError(t, err)
		require.NotContains(t, string(wire), "secret")
		require.NotContains(t, string(wire), `"org_used":`)
		require.Contains(t, string(wire), `"org_used_available":false`)
		for i := 0; i < 100; i++ {
			limitpolicy.RecordSupplementalDecision(ctx, limitpolicy.Decision{})
		}
		result = contentPerformanceCollectDecisions(ctx)
		require.True(t, result.Truncated)
		require.Len(t, result.Decisions, contentPerformanceDecisionLimit)
	})
	t.Run("hook_preserves_context_command_result_and_error", func(t *testing.T) {
		d := newContentPerformanceNativeDiagnostics(true)
		defer d.endTasks()
		entry := contentPerformanceNativeTestEntry(d, 1)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		sentinel := errors.New("private credential SQL error")
		command := contentPerformanceNativeTestCommand(entry, "reserve")
		calls := 0
		reply := contentPerformanceNativeTestReply(0)
		next := func(actual context.Context, cmd redis.Cmder) error {
			calls++
			require.Same(t, ctx, actual)
			require.Same(t, command, cmd)
			command.SetVal(reply)
			return sentinel
		}
		require.Same(t, sentinel, d.ProcessHook(next)(ctx, command))
		require.Equal(t, 1, calls)
		require.Equal(t, reply, command.Val())
		require.EqualValues(t, 1, entry.row.ReserveUnknown)
		wire, err := json.Marshal(d.evidence(true))
		require.NoError(t, err)
		for _, secret := range []string{"private credential SQL error", "private-argument-must-not-export", "SQL error", "workload:{", contentPerformanceReserveSHA} {
			require.NotContains(t, string(wire), secret)
		}
	})
	t.Run("exact_scripts_keys_members_and_malformed_replies", func(t *testing.T) {
		d := newContentPerformanceNativeDiagnostics(true)
		defer d.endTasks()
		entry := contentPerformanceNativeTestEntry(d, 2)
		calls := 0
		next := func(_ context.Context, cmd redis.Cmder) error {
			calls++
			cmd.(*redis.Cmd).SetVal([]interface{}{"private result"})
			return nil
		}
		invoke := d.ProcessHook(next)
		command := contentPerformanceNativeTestCommand(entry, "reserve")
		require.NoError(t, invoke(t.Context(), command))
		require.EqualValues(t, 1, entry.row.ReserveUnknown)
		command = contentPerformanceNativeTestCommand(entry, "reserve")
		command.Args()[1] = "unrecognized-sha"
		require.NoError(t, invoke(t.Context(), command))
		command = contentPerformanceNativeTestCommand(entry, "reserve")
		command.Args()[17] = "unregistered-member"
		require.NoError(t, invoke(t.Context(), command))
		command = contentPerformanceNativeTestCommand(entry, "release")
		command.Args()[3] = "wrong-key"
		require.NoError(t, invoke(t.Context(), command))
		require.Equal(t, 4, calls)
		require.EqualValues(t, 1, entry.row.ReserveCalls)
		require.EqualValues(t, 1, d.unassociated)
		require.EqualValues(t, 1, d.malformed)
	})
	t.Run("constant_space_ack_uncertainty_and_censoring", func(t *testing.T) {
		d := newContentPerformanceNativeDiagnostics(true)
		entry := contentPerformanceNativeTestEntry(d, 3)
		reserve := contentPerformanceNativeTestCommand(entry, "reserve")
		release := contentPerformanceNativeTestCommand(entry, "release")
		require.NoError(t, d.ProcessHook(func(_ context.Context, cmd redis.Cmder) error {
			cmd.(*redis.Cmd).SetVal(contentPerformanceNativeTestReply(0))
			return nil
		})(t.Context(), reserve))
		ack := d.ProcessHook(func(_ context.Context, cmd redis.Cmder) error {
			cmd.(*redis.Cmd).SetVal([]interface{}{int64(0), int64(0), int64(0)})
			return nil
		})
		for i := 0; i < 1000; i++ {
			require.NoError(t, ack(t.Context(), release))
		}
		first := entry.row.FirstReleaseAck
		require.NotNil(t, first)
		require.EqualValues(t, 1000, entry.row.ReleaseAcks)
		pending := contentPerformanceNativeTestEntry(d, 4)
		require.NoError(t, d.ProcessHook(func(_ context.Context, cmd redis.Cmder) error {
			cmd.(*redis.Cmd).SetVal(contentPerformanceNativeTestReply(0))
			return nil
		})(t.Context(), contentPerformanceNativeTestCommand(pending, "reserve")))
		evidence := d.evidence(false)
		rows := evidence["rows"].([]contentPerformanceNativeRow)
		require.Equal(t, "grant_and_release_ack_observed_atomic_release_unknown", rows[0].Status)
		require.False(t, rows[0].RightCensored)
		require.True(t, rows[1].RightCensored)
		require.Same(t, first, entry.row.FirstReleaseAck)
		require.False(t, evidence["terminal_worker_joined"].(bool))
		require.NoError(t, ack(t.Context(), release))
		require.EqualValues(t, 1000, entry.row.ReleaseAcks)
		require.EqualValues(t, 1, d.late)
	})
	t.Run("inflight_at_close_is_unknown_and_does_not_mutate_snapshot", func(t *testing.T) {
		d := newContentPerformanceNativeDiagnostics(true)
		entry := contentPerformanceNativeTestEntry(d, 7)
		started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
		command := contentPerformanceNativeTestCommand(entry, "reserve")
		go func() {
			defer close(done)
			_ = d.ProcessHook(func(_ context.Context, cmd redis.Cmder) error {
				close(started)
				<-release
				cmd.(*redis.Cmd).SetVal(contentPerformanceNativeTestReply(0))
				return nil
			})(context.Background(), command)
		}()
		<-started
		evidence := d.evidence(false)
		rows := evidence["rows"].([]contentPerformanceNativeRow)
		require.EqualValues(t, 1, rows[0].ReserveInFlight)
		require.Equal(t, "acquisition_unknown", rows[0].Status)
		require.True(t, rows[0].RightCensored)
		close(release)
		<-done
		require.Nil(t, rows[0].FirstGrant)
		require.Nil(t, entry.row.FirstGrant)
		require.EqualValues(t, 1, d.late)
	})
	t.Run("reserve_wrapper_captures_existing_collector_transparently", func(t *testing.T) {
		d := newContentPerformanceNativeDiagnostics(true)
		defer d.endTasks()
		entry := contentPerformanceNativeTestEntry(d, 8)
		phase := &contentPerformancePhase{start: time.Now(), native: entry}
		ctx, collector := limitpolicy.ContextWithDecisionCollector(context.WithValue(t.Context(), contentPerformancePhaseKey{}, phase))
		limitpolicy.RecordSupplementalDecision(ctx, limitpolicy.Decision{Decision: "deny", Reason: "policy_snapshot_stale", Mode: "enforce"})
		before, _ := collector.Snapshot()
		service := &limitpolicy.Service{}
		req := limitpolicy.AdmissionRequest{OperationID: entry.row.OperationID, Subject: limitpolicy.Subject{OrgID: 7}}
		original, originalErr := service.Reserve(ctx, req)
		result, err := (contentPerformancePolicyRuntime{service}).Reserve(ctx, req)
		require.Same(t, originalErr, err)
		require.Equal(t, original, result)
		after, _ := collector.Snapshot()
		require.Equal(t, before, after)
		evidence := phase.snapshot()["admission_decisions"].(*contentPerformanceDecisionEvidence)
		require.True(t, evidence.CollectorPresent)
		require.Len(t, evidence.Decisions, 1)
		require.False(t, evidence.WorkloadPresent)
	})
	t.Run("registry_is_bounded", func(t *testing.T) {
		d := newContentPerformanceNativeDiagnostics(true)
		defer d.endTasks()
		for i := 0; i < contentPerformanceNativeLimit+1; i++ {
			d.begin(t.Context(), fmt.Sprintf("op_%032x", i), "r0-plain-1")
		}
		require.Len(t, d.entries, contentPerformanceNativeLimit)
		require.True(t, d.overflow)
	})
	t.Run("real_workload_scripts_and_detached_release", func(t *testing.T) {
		server := miniredis.RunT(t)
		client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1, ContextTimeoutEnabled: true})
		defer client.Close()
		d := newContentPerformanceNativeDiagnostics(true)
		defer d.endTasks()
		entry := contentPerformanceNativeTestEntry(d, 5)
		client.AddHook(d)
		service, err := workloadadmission.NewService(client, workloadadmission.DefaultOptions())
		require.NoError(t, err)
		reservation, decision, err := service.Reserve(t.Context(), workloadadmission.Request{OperationID: entry.row.OperationID, OrgID: 7, Class: workloadadmission.ClassInteractive, LeaseTTL: time.Minute})
		require.NoError(t, err)
		require.Equal(t, "allow", decision.Decision)
		require.NotNil(t, entry.row.FirstGrant, "exact production reserve script fingerprint drifted")
		require.NoError(t, service.Release(context.Background(), reservation))
		first := entry.row.FirstReleaseAck
		require.NotNil(t, first, "exact production release script fingerprint drifted")
		require.NoError(t, service.Release(context.Background(), reservation))
		require.Same(t, first, entry.row.FirstReleaseAck)
		require.EqualValues(t, 2, entry.row.ReleaseAcks)
		require.EqualValues(t, 1, entry.row.ReserveCacheMiss)
		require.EqualValues(t, 1, entry.row.ReleaseCacheMiss)
		require.Zero(t, entry.row.ReserveUnknown)
		require.Zero(t, d.malformed)
		require.Zero(t, d.unassociated)
	})
	t.Run("outbox_original_context_and_native_trace_survive_handler_end", func(t *testing.T) {
		var buffer bytes.Buffer
		require.NoError(t, trace.Start(&buffer))
		defer trace.Stop()
		d := newContentPerformanceNativeDiagnostics(true)
		entry := contentPerformanceNativeTestEntry(d, 6)
		entry.mark("handler_end", "none")
		workerCtx, cancel := context.WithCancel(t.Context())
		defer cancel()
		event := outbox.Event{RequestID: entry.row.OperationID, OrgID: 7, Payload: json.RawMessage(`{"private":"not-exported"}`)}
		calls := 0
		wrapped := d.outboxRegion("terminal_recovery", func(ctx context.Context, actual outbox.Event) error {
			calls++
			require.Same(t, workerCtx, ctx)
			require.Equal(t, event, actual)
			return nil
		})
		require.NoError(t, wrapped(workerCtx, event))
		require.Equal(t, 1, calls)
		d.endTasks()
		trace.Stop()
		for _, text := range []string{"cc13.operation", "handler_end", "terminal_recovery", entry.row.OperationID} {
			require.Contains(t, buffer.String(), text)
		}
		require.NotContains(t, buffer.String(), "not-exported")
	})
	t.Run("database_binding_one_readonly_query_no_connection_data", func(t *testing.T) {
		calls := 0
		pool := &testutil.MockDB{QueryRowFn: func(ctx context.Context, query string, args ...any) pgx.Row {
			calls++
			require.Same(t, t.Context(), ctx)
			require.Equal(t, `SELECT current_database(), (SELECT oid::bigint FROM pg_database WHERE datname=current_database())`, query)
			require.Empty(t, args)
			return &testutil.MockRow{ScanFn: func(dest ...any) error {
				*dest[0].(*string) = "exact_synthetic_db"
				*dest[1].(*int64) = 12345
				return nil
			}}
		}}
		binding, err := contentPerformanceReadDatabaseBinding(t.Context(), pool)
		require.NoError(t, err)
		require.Equal(t, 1, calls)
		wire, err := json.Marshal(binding)
		require.NoError(t, err)
		require.JSONEq(t, `{"name":"exact_synthetic_db","oid":12345}`, string(wire))
	})
}
