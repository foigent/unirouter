package contentcapture

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uniroute/unirouter/internal/db"
)

type policyTestRow struct {
	values []any
	err    error
}

func (r policyTestRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return errors.New("synthetic row column mismatch")
	}
	for i, value := range r.values {
		target := reflect.ValueOf(dest[i]).Elem()
		if value == nil {
			target.Set(reflect.Zero(target.Type()))
		} else {
			target.Set(reflect.ValueOf(value))
		}
	}
	return nil
}

type policyReadDB struct {
	db.DBQuerier
	row   pgx.Row
	calls int
	sql   string
}

func (d *policyReadDB) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	d.calls++
	d.sql = sql
	return d.row
}

type policyWriteTx struct {
	pgx.Tx
	rows        []pgx.Row
	calls       []string
	updates     int
	updateArgs  []any
	updateCount int64
}

func (t *policyWriteTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	t.calls = append(t.calls, sql)
	if len(t.rows) == 0 {
		return policyTestRow{err: errors.New("unexpected query")}
	}
	row := t.rows[0]
	t.rows = t.rows[1:]
	return row
}
func (t *policyWriteTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	t.calls = append(t.calls, sql)
	if strings.HasPrefix(sql, "UPDATE") {
		t.updates++
		t.updateArgs = args
		if t.updateCount == 0 {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}
func policyStoreTestOptions() PolicyStoreOptions {
	return PolicyStoreOptions{SourceInstance: "synthetic-store", DefaultMode: ModeMetadataOnly, SupportedProtocols: []string{"openai.chat"}, ObservationPoints: []string{"client_request", "provider_response"}, Limits: testPolicyView().Limits, FullContentEnabled: true}
}
func policyAbsentRow(created time.Time) pgx.Row {
	return policyTestRow{values: []any{created, nil, nil, nil, nil, nil, nil, nil}}
}
func policyStoredValues(view PolicyView) []any {
	d, _ := json.Marshal(view.Desired.Config)
	e, _ := json.Marshal(view.Effective.Config)
	dv, _ := policyID(view.Desired.PolicyVersion)
	ev, _ := policyID(view.Effective.PolicyVersion)
	di, ei := int64(dv), int64(ev)
	at, _ := time.Parse(time.RFC3339Nano, view.EffectiveAt)
	return []any{&di, &ei, d, e, &at, &view.ApplicationStatus, &view.ApplicationReason}
}

func TestPolicyStoreReadDefaultIsStableAndDoesNotWrite(t *testing.T) {
	created := time.Date(2025, 1, 2, 3, 4, 5, 123456000, time.UTC)
	database := &policyReadDB{row: policyAbsentRow(created)}
	options := policyStoreTestOptions()
	options.DefaultMode = ModeDisabled
	s, err := NewPolicyStore(database, options)
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Get(t.Context(), 1, 2, "not_configured")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Get(t.Context(), 1, 2, "not_configured")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) || first.EffectiveAt != "2025-01-02T03:04:05.123456Z" || first.Desired.PolicyVersion != "1" || first.Effective.Config.Mode != ModeDisabled {
		t.Fatalf("unstable or expanded default: %#v", first)
	}
	if first.Desired.Config.QuotaBytes != nil || first.Desired.Config.RetentionSeconds != nil {
		t.Fatal("invented content retention or quota")
	}
	if database.calls != 2 || strings.Contains(database.sql, "FOR UPDATE") {
		t.Fatal("read took write lock")
	}
	if _, err := s.EffectiveForCapture(t.Context(), 1, 2, "ready"); !errors.Is(err, ErrPolicyInactive) {
		t.Fatal("disabled default allowed capture")
	}
	options.SupportedProtocols[0] = "mutated"
	if s.defaultConfig.SupportedProtocols[0] != "openai.chat" || s.options.SupportedProtocols[0] != "openai.chat" {
		t.Fatal("options alias caller memory")
	}
}

func TestPolicyStorePutLocksOnlyPolicyAndKeepsPendingEffective(t *testing.T) {
	s, err := NewPolicyStore(&policyReadDB{}, policyStoreTestOptions())
	if err != nil {
		t.Fatal(err)
	}
	current := testPolicyView()
	created := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	tx := &policyWriteTx{rows: []pgx.Row{policyTestRow{values: []any{created}}, policyTestRow{values: policyStoredValues(current)}}, updateCount: 1}
	put := PolicyPut{SchemaVersion: PolicyPutSchema, ExpectedPolicyVersion: "1", Desired: testFullPolicy()}
	next, err := s.PutTx(t.Context(), tx, 1, 2, put, PolicyApplication{StorageStatus: "unavailable", Now: created.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if next.Desired.PolicyVersion != "2" || next.Effective.PolicyVersion != "1" || next.ApplicationStatus != "pending" || next.ApplicationReason != "storage_unavailable" || next.EffectiveAt != current.EffectiveAt {
		t.Fatalf("wrong transition: %#v", next)
	}
	if len(tx.calls) != 4 || !strings.Contains(tx.calls[1], "ON CONFLICT") || !strings.Contains(tx.calls[2], "FOR UPDATE") || strings.Contains(tx.calls[0], "FOR ") {
		t.Fatalf("lock sequence: %#v", tx.calls)
	}
	if tx.updateArgs[2] != int64(2) || tx.updateArgs[3] != int64(1) {
		t.Fatal("did not persist distinct versions")
	}
	for _, sql := range tx.calls {
		if strings.Contains(sql, "UPDATE projects") || strings.Contains(sql, "FOR SHARE") {
			t.Fatal("project lock escalation")
		}
	}
}

func TestPolicyStoreCASAndFeatureGateFailClosed(t *testing.T) {
	options := policyStoreTestOptions()
	options.FullContentEnabled = false
	s, err := NewPolicyStore(&policyReadDB{}, options)
	if err != nil {
		t.Fatal(err)
	}
	tx := &policyWriteTx{}
	put := PolicyPut{SchemaVersion: PolicyPutSchema, ExpectedPolicyVersion: "1", Desired: testFullPolicy()}
	if _, err := s.PutTx(t.Context(), tx, 1, 2, put, PolicyApplication{StorageStatus: "ready", ApplyNow: true, Now: time.Now()}); !errors.Is(err, ErrInvalidPolicy) || len(tx.calls) != 0 {
		t.Fatal("feature gate allowed writes")
	}
	s.options.FullContentEnabled = true
	if _, err := s.PutTx(t.Context(), tx, 1, 2, put, PolicyApplication{StorageStatus: "unavailable", ApplyNow: true, Now: time.Now()}); !errors.Is(err, ErrInvalidPolicy) || len(tx.calls) != 0 {
		t.Fatal("accepted unverified immediate application")
	}
	current := testPolicyView()
	current.Desired.PolicyVersion = "2"
	current.Effective.PolicyVersion = "2"
	tx.rows = []pgx.Row{policyTestRow{values: []any{time.Now()}}, policyTestRow{values: policyStoredValues(current)}}
	if _, err := s.PutTx(t.Context(), tx, 1, 2, put, PolicyApplication{StorageStatus: "ready", ApplyNow: true, Now: time.Now()}); !errors.Is(err, ErrPolicyVersionConflict) || tx.updates != 0 {
		t.Fatalf("stale desired CAS mutated: %v", err)
	}
}

func TestPolicyStoreFreshEffectiveAndStrictPersistedDecode(t *testing.T) {
	options := policyStoreTestOptions()
	view := testPolicyView()
	view.Desired = PolicyVersion{PolicyVersion: "3", Config: testFullPolicy()}
	view.Effective = PolicyVersion{PolicyVersion: "2", Config: testFullPolicy()}
	view.ApplicationStatus = "pending"
	view.ApplicationReason = "awaiting_activation"
	row := append([]any{time.Now()}, policyStoredValues(view)...)
	database := &policyReadDB{row: policyTestRow{values: row}}
	s, err := NewPolicyStore(database, options)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.EffectiveForCapture(t.Context(), 1, 2, "ready")
	if err != nil || got.Effective.PolicyVersion != "2" {
		t.Fatalf("used desired instead of effective: %v", err)
	}
	if _, err := s.EffectiveForCapture(t.Context(), 1, 2, "unavailable"); !errors.Is(err, ErrPolicyInactive) {
		t.Fatal("used unavailable storage")
	}
	s.options.FullContentEnabled = false
	if _, err := s.EffectiveForCapture(t.Context(), 1, 2, "ready"); !errors.Is(err, ErrPolicyInactive) {
		t.Fatal("stored full bypassed feature gate")
	}
	s.options.FullContentEnabled = true
	database.row = policyAbsentRow(time.Now())
	got, err = s.EffectiveForCapture(t.Context(), 1, 2, "not_configured")
	if err != nil || got.Effective.Config.Mode != ModeMetadataOnly {
		t.Fatal("cached old full policy")
	}
	row[3] = []byte(`{"mode":"metadata_only","mode":"disabled"}`)
	database.row = policyTestRow{values: row}
	if _, err := s.Get(t.Context(), 1, 2, "ready"); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatal("accepted corrupt stored config")
	}
	database.row = policyTestRow{err: pgx.ErrNoRows}
	if _, err := s.Get(t.Context(), 1, 2, "ready"); !errors.Is(err, ErrPolicyProjectNotFound) {
		t.Fatal("invented absent project")
	}
}

func TestPolicyStoreConstructorRequiresRealCapabilities(t *testing.T) {
	for _, change := range []func(*PolicyStoreOptions){func(o *PolicyStoreOptions) { o.DefaultMode = "" }, func(o *PolicyStoreOptions) { o.DefaultMode = ModeFullContent }, func(o *PolicyStoreOptions) { o.SupportedProtocols = nil }, func(o *PolicyStoreOptions) { o.ObservationPoints = nil }, func(o *PolicyStoreOptions) { o.Limits.MaxReadBytes = 1 }} {
		o := policyStoreTestOptions()
		change(&o)
		if _, err := NewPolicyStore(&policyReadDB{}, o); err == nil {
			t.Fatal("accepted incomplete capabilities")
		}
	}
}
