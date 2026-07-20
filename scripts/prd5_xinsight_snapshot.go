//go:build ignore

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/uniroute/unirouter/internal/snapshotpublish"
	"github.com/uniroute/unirouter/internal/traceutil"
)

const prd5SnapshotSchemaVersion = "unirouter.prd5.xinsight.snapshot.v2"
const prd5ApprovalManifestSchemaVersion = "unirouter.prd5.xinsight.approval.v1"

const (
	maxApprovedRequestIDs    = 20
	maxSnapshotRows          = 200
	prd5PayloadCaptureReason = "raw prompt/response export is disabled for PRD5 Gate A snapshots"
	strictGapClass           = "not_available_from_source"
	strictIPHandling         = "raw IP addresses are not exported; only ip_address_was_present and ip_address_redacted boolean fields are retained"
	strictErrorHandling      = "raw upstream_error text is not exported; only upstream_error_present is retained"
	strictMetadataHandling   = "request_trace_events.metadata is re-sanitized through internal/traceutil.SanitizeMetadata before export"
	strictCallGapReason      = "no call_logs row matched this request_id in the selected snapshot"
	strictTraceGapNoCall     = "no request_trace_events row matched this request_id and no call_logs row is present in the selected snapshot"
	strictTraceGapWithCall   = "call_logs row exists but no request_trace_events row matched this request_id; possible trace sampling, trace retention cleanup, or non-instrumented path"
)

var errDuplicateJSONKey = errors.New("duplicate JSON object key")

type repeatedFlag []string

func (f *repeatedFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *repeatedFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	*f = append(*f, value)
	return nil
}

type snapshotFile struct {
	SchemaVersion          string                 `json:"schema_version"`
	ExchangeID             string                 `json:"exchange_id"`
	ApprovalManifestSHA256 string                 `json:"approval_manifest_sha256,omitempty"`
	GeneratedAt            string                 `json:"generated_at"`
	Source                 string                 `json:"source"`
	SourceInstance         string                 `json:"source_instance"`
	RequestIDs             []string               `json:"request_ids"`
	Scenarios              []snapshotScenario     `json:"scenarios"`
	Safety                 snapshotSafety         `json:"safety"`
	Counts                 snapshotCounts         `json:"counts"`
	CaptureGaps            []snapshotCaptureGap   `json:"capture_gaps"`
	CallLogs               []snapshotCallLog      `json:"call_logs"`
	TraceEvents            []snapshotTraceEvent   `json:"request_trace_events"`
	Notes                  map[string]interface{} `json:"notes"`
}

type snapshotScenario struct {
	Label     string `json:"label"`
	RequestID string `json:"request_id"`
}

type approvalManifest struct {
	SchemaVersion  string                    `json:"schema_version"`
	SourceInstance string                    `json:"source_instance"`
	Requests       []approvalManifestRequest `json:"requests"`
}

type approvalManifestRequest struct {
	ScenarioLabel  string `json:"scenario_label"`
	RequestID      string `json:"request_id"`
	ApprovalStatus string `json:"approval_status"`
	Required       bool   `json:"required"`
	Notes          string `json:"notes,omitempty"`
}

type snapshotSafety struct {
	SelectionMode        string `json:"selection_mode"`
	RawPromptsIncluded   bool   `json:"raw_prompts_included"`
	RawResponsesIncluded bool   `json:"raw_responses_included"`
	APIKeysIncluded      bool   `json:"api_keys_included"`
	IPHandling           string `json:"ip_handling"`
	ErrorHandling        string `json:"error_handling"`
	MetadataHandling     string `json:"metadata_handling"`
}

type snapshotCounts struct {
	RequestedRequestIDs int `json:"requested_request_ids"`
	CallLogs            int `json:"call_logs"`
	TraceEvents         int `json:"request_trace_events"`
	CaptureGaps         int `json:"capture_gaps"`
}

type strictSnapshotBinding struct {
	SourceInstance         string
	SelectionMode          string
	ExchangeID             string
	ApprovalManifestSHA256 string
	RequestIDs             []string
	Scenarios              []snapshotScenario
}

type snapshotCaptureGap struct {
	RequestID     string   `json:"request_id"`
	Source        string   `json:"source"`
	Gap           string   `json:"capture_gap"`
	CandidateGaps []string `json:"candidate_capture_gaps,omitempty"`
	Reason        string   `json:"reason"`
	Generated     bool     `json:"generated_by_snapshot_tool"`
}

type snapshotCallLog struct {
	SourceRef              string  `json:"source_ref"`
	SourceEventHash        string  `json:"source_event_hash,omitempty"`
	ID                     int64   `json:"id"`
	RequestID              string  `json:"request_id"`
	OrgID                  int64   `json:"org_id"`
	ProjectID              *int64  `json:"project_id"`
	APIKeyID               *int64  `json:"api_key_id"`
	OwnerUserID            *int64  `json:"owner_user_id"`
	KeyType                string  `json:"key_type"`
	Model                  string  `json:"model"`
	RequestedModel         string  `json:"requested_model"`
	Provider               string  `json:"provider"`
	Channel                string  `json:"channel"`
	PromptTokens           int64   `json:"prompt_tokens"`
	CompletionTokens       int64   `json:"completion_tokens"`
	TotalTokens            int64   `json:"total_tokens"`
	EstimatedTokens        int64   `json:"estimated_tokens"`
	UsageSource            string  `json:"usage_source"`
	UsageConfidence        string  `json:"usage_confidence"`
	CostMicros             string  `json:"cost_micros"`
	Currency               string  `json:"currency"`
	AmountScale            int64   `json:"amount_scale"`
	MoneySchema            string  `json:"money_schema"`
	LatencyMS              int64   `json:"latency_ms"`
	TTFTMS                 int64   `json:"ttft_ms"`
	StatusCode             int64   `json:"status_code"`
	UpstreamStatusCode     *int64  `json:"upstream_status_code"`
	UpstreamErrorPresent   bool    `json:"upstream_error_present"`
	IPAddressWasPresent    bool    `json:"ip_address_was_present"`
	IPAddressRedacted      bool    `json:"ip_address_redacted"`
	IsStream               bool    `json:"is_stream"`
	CreatedAt              string  `json:"created_at"`
	TraceID                *string `json:"trace_id"`
	SessionID              *string `json:"session_id"`
	UpstreamRequestID      *string `json:"upstream_request_id"`
	EvidenceRef            *string `json:"evidence_ref"`
	ActorType              *string `json:"actor_type"`
	ActorRef               *string `json:"actor_ref"`
	OnBehalfOf             *string `json:"on_behalf_of"`
	PayloadRef             *string `json:"payload_ref,omitempty"`
	PayloadCaptureMode     string  `json:"payload_capture_mode"`
	PayloadCaptureReason   string  `json:"payload_capture_reason"`
	DerivedCaptureMode     string  `json:"capture_mode"`
	DerivedSchemaVersion   string  `json:"schema_version"`
	DerivedSourceComponent string  `json:"source_component"`
}

type snapshotTraceEvent struct {
	SourceRef              string                 `json:"source_ref"`
	SourceEventHash        string                 `json:"source_event_hash,omitempty"`
	ID                     int64                  `json:"id"`
	RequestID              string                 `json:"request_id"`
	OrgID                  int64                  `json:"org_id"`
	ProjectID              *int64                 `json:"project_id"`
	APIKeyID               *int64                 `json:"api_key_id"`
	OwnerUserID            *int64                 `json:"owner_user_id"`
	KeyType                string                 `json:"key_type"`
	EventType              string                 `json:"event_type"`
	Stage                  string                 `json:"stage"`
	Severity               string                 `json:"severity"`
	Model                  string                 `json:"model"`
	RequestedModel         string                 `json:"requested_model"`
	Provider               string                 `json:"provider"`
	Channel                string                 `json:"channel"`
	StatusCode             *int64                 `json:"status_code"`
	LatencyMS              int64                  `json:"latency_ms"`
	Metadata               map[string]interface{} `json:"metadata"`
	MetadataKeys           []string               `json:"metadata_keys"`
	CreatedAt              string                 `json:"created_at"`
	TraceID                *string                `json:"trace_id"`
	SpanID                 *string                `json:"span_id"`
	ParentSpanID           *string                `json:"parent_span_id"`
	CaptureGap             *string                `json:"capture_gap"`
	EvidenceRef            *string                `json:"evidence_ref"`
	UpstreamRequestID      *string                `json:"upstream_request_id"`
	DecisionKind           *string                `json:"decision_kind"`
	DecisionResult         *string                `json:"decision_result"`
	DecisionReason         *string                `json:"decision_reason"`
	ActorType              *string                `json:"actor_type"`
	ActorRef               *string                `json:"actor_ref"`
	OnBehalfOf             *string                `json:"on_behalf_of"`
	SessionID              *string                `json:"session_id"`
	PayloadRef             *string                `json:"payload_ref,omitempty"`
	PayloadCaptureMode     string                 `json:"payload_capture_mode"`
	PayloadCaptureReason   string                 `json:"payload_capture_reason"`
	DerivedCaptureMode     string                 `json:"capture_mode"`
	DerivedSchemaVersion   string                 `json:"schema_version"`
	DerivedSourceComponent string                 `json:"source_component"`
}

func main() {
	if err := runPRD5Snapshot(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintf(os.Stderr, "snapshot failed: %v\n", err)
		os.Exit(1)
	}
}

func runPRD5Snapshot(args []string) error {
	return runPRD5SnapshotWithDeps(args, defaultStrictSnapshotDeps())
}

type strictSnapshotDeps struct {
	prepareOutput func(string) (snapshotpublish.Stage, error)
	lookupEnv     func(string) string
	openDB        func(string) (strictSnapshotDB, error)
	now           func() time.Time
	newExchangeID func() (string, error)
}

func defaultStrictSnapshotDeps() strictSnapshotDeps {
	return strictSnapshotDeps{
		prepareOutput: snapshotpublish.Prepare,
		lookupEnv:     os.Getenv,
		openDB:        openStrictSnapshotDB,
		now:           time.Now,
		newExchangeID: newExchangeID,
	}
}

type strictSnapshotDB interface {
	BeginStrict(context.Context, *sql.TxOptions) (strictReadTx, error)
	Close() error
}

type strictReadTx interface {
	QueryContext(context.Context, string, ...any) (snapshotRows, error)
	QueryRowContext(context.Context, string, ...any) snapshotRowScanner
	Commit() error
	Rollback() error
}

type snapshotRows interface {
	snapshotRowScanner
	Next() bool
	Err() error
	Close() error
}

type sqlStrictSnapshotDB struct {
	db *sql.DB
}

type sqlStrictReadTx struct {
	tx *sql.Tx
}

type sqlSnapshotRows struct {
	rows *sql.Rows
}

func openStrictSnapshotDB(dsn string) (strictSnapshotDB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	return &sqlStrictSnapshotDB{db: db}, nil
}

func (d *sqlStrictSnapshotDB) BeginStrict(ctx context.Context, options *sql.TxOptions) (strictReadTx, error) {
	tx, err := d.db.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &sqlStrictReadTx{tx: tx}, nil
}

func (d *sqlStrictSnapshotDB) Close() error {
	return d.db.Close()
}

func (t *sqlStrictReadTx) QueryContext(ctx context.Context, query string, args ...any) (snapshotRows, error) {
	rows, err := t.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return &sqlSnapshotRows{rows: rows}, nil
}

func (t *sqlStrictReadTx) QueryRowContext(ctx context.Context, query string, args ...any) snapshotRowScanner {
	return t.tx.QueryRowContext(ctx, query, args...)
}

func (t *sqlStrictReadTx) Commit() error {
	return t.tx.Commit()
}

func (t *sqlStrictReadTx) Rollback() error {
	return t.tx.Rollback()
}

func (r *sqlSnapshotRows) Next() bool {
	return r.rows.Next()
}

func (r *sqlSnapshotRows) Scan(dest ...any) error {
	return r.rows.Scan(dest...)
}

func (r *sqlSnapshotRows) Err() error {
	return r.rows.Err()
}

func (r *sqlSnapshotRows) Close() error {
	return r.rows.Close()
}

func runPRD5SnapshotWithDeps(args []string, deps strictSnapshotDeps) error {
	fs := flag.NewFlagSet("prd5_xinsight_snapshot", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var requestIDFlags repeatedFlag
	var scenarioFlags repeatedFlag
	var requestIDsCSV string
	var outputPath string
	var handoffNotePath string
	var approvalManifestPath string
	var dsnEnv string
	var sourceInstance string
	var overwrite bool

	fs.Var(&requestIDFlags, "request-id", "request_id to export; repeat for multiple requests")
	fs.StringVar(&requestIDsCSV, "request-ids", "", "comma-separated request_id list")
	fs.Var(&scenarioFlags, "scenario", "label=request_id scenario marker; repeat for multiple scenarios")
	fs.StringVar(&outputPath, "output", "-", "output JSON path, or - for stdout")
	fs.StringVar(&handoffNotePath, "handoff-note", "", "optional Markdown handoff note path; DSN and credentials are never written")
	fs.StringVar(&approvalManifestPath, "approval-manifest", "", "optional JSON approval manifest containing approved scenario request IDs")
	fs.StringVar(&dsnEnv, "dsn-env", "UNIROUTE_PRD5_SNAPSHOT_DSN", "environment variable containing the read-only Postgres DSN; falls back to DB_DSN")
	fs.StringVar(&sourceInstance, "source-instance", "local-controlled", "non-secret source instance label used in source_ref")
	fs.BoolVar(&overwrite, "overwrite", false, "overwrite output path if it already exists")

	if err := fs.Parse(args); err != nil {
		return err
	}

	sourceInstanceFlagSet := flagWasSet(fs, "source-instance")
	approvalManifestPath = strings.TrimSpace(approvalManifestPath)
	selectionMode := "explicit_request_id_only"
	approvalManifestSHA256 := ""
	strictMode := approvalManifestPath != ""
	var requestIDs []string
	var scenarios []snapshotScenario
	if strictMode {
		if flagWasSet(fs, "request-id") || flagWasSet(fs, "request-ids") || flagWasSet(fs, "scenario") {
			return errors.New("strict_manifest_invalid: direct_id_selection")
		}
		manifest, manifestSHA256, err := loadApprovalManifest(approvalManifestPath)
		if err != nil {
			return err
		}
		approvalManifestSHA256 = manifestSHA256
		manifestSourceInstance := manifest.SourceInstance
		if err := validateStrictSourceInstance(manifestSourceInstance); err != nil {
			return err
		}
		if sourceInstanceFlagSet && sourceInstance != manifestSourceInstance {
			return errors.New("strict_manifest_invalid: source_instance_mismatch")
		}
		requestIDs, scenarios, err = approvedManifestSelection(manifest)
		if err != nil {
			return err
		}
		sourceInstance = manifestSourceInstance
		selectionMode = "approved_request_id_manifest"
	} else {
		var err error
		requestIDs = normalizedRequestIDs(requestIDFlags, requestIDsCSV)
		if len(requestIDs) == 0 {
			return errors.New("at least one --request-id, --request-ids, or --approval-manifest value is required; broad snapshots are intentionally unsupported")
		}
		scenarios, err = parseScenarios(scenarioFlags)
		if err != nil {
			return err
		}
	}
	if !strictMode {
		sourceInstance = sanitizeSourceInstance(sourceInstance)
	}
	if err := validateScenarios(requestIDs, scenarios); err != nil {
		return err
	}
	handoffNotePath = strings.TrimSpace(handoffNotePath)
	if handoffNotePath == "-" {
		return errors.New("--handoff-note must be a Markdown file path, not stdout")
	}

	if strictMode {
		if err := validateStrictOutputOptions(outputPath, handoffNotePath, overwrite); err != nil {
			return err
		}
		stage, err := deps.prepareOutput(outputPath)
		if err != nil {
			switch {
			case errors.Is(err, snapshotpublish.ErrTargetExists):
				return errors.New("strict_output_target_exists")
			case errors.Is(err, snapshotpublish.ErrUnsupported):
				return errors.New("strict_output_unsupported")
			default:
				return errors.New("strict_output_preflight_failed")
			}
		}
		defer func() { _ = stage.Close() }()

		dsn := strings.TrimSpace(deps.lookupEnv(dsnEnv))
		if dsn == "" && dsnEnv != "DB_DSN" {
			dsn = strings.TrimSpace(deps.lookupEnv("DB_DSN"))
		}
		if dsn == "" {
			return errors.New("strict_dsn_unavailable")
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		db, err := deps.openDB(dsn)
		if err != nil {
			return errors.New("strict_database_open_failed")
		}
		defer func() { _ = db.Close() }()

		exchangeID, err := deps.newExchangeID()
		if err != nil {
			return errors.New("strict_exchange_id_failed")
		}
		snapshot, err := loadStrictSnapshot(
			ctx,
			db,
			sourceInstance,
			selectionMode,
			exchangeID,
			approvalManifestSHA256,
			requestIDs,
			scenarios,
			deps.now().UTC().Format(time.RFC3339Nano),
		)
		if err != nil {
			return err
		}
		_, err = publishStrictSnapshot(stage, snapshot)
		return err
	}

	dsn := strings.TrimSpace(deps.lookupEnv(dsnEnv))
	if dsn == "" && dsnEnv != "DB_DSN" {
		dsn = strings.TrimSpace(deps.lookupEnv("DB_DSN"))
	}
	if dsn == "" {
		return fmt.Errorf("%s or DB_DSN must be set to a read-only UniRouter Postgres DSN", dsnEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return err
	}

	exchangeID, err := deps.newExchangeID()
	if err != nil {
		return err
	}
	snapshot, err := loadSnapshot(ctx, db, sourceInstance, selectionMode, exchangeID, approvalManifestSHA256, requestIDs, scenarios)
	if err != nil {
		return err
	}

	if err := writeSnapshot(outputPath, overwrite, snapshot); err != nil {
		return err
	}
	if handoffNotePath != "" {
		return writeHandoffNote(handoffNotePath, overwrite, outputPath, approvalManifestPath, snapshot)
	}
	return nil
}

func validateStrictOutputOptions(outputPath, handoffNotePath string, overwrite bool) error {
	if outputPath == "" || outputPath == "-" || !filepath.IsAbs(outputPath) || filepath.Clean(outputPath) != outputPath {
		return errors.New("strict_output_invalid: absolute_normalized_private_path_required")
	}
	if overwrite {
		return errors.New("strict_output_invalid: overwrite_prohibited")
	}
	if handoffNotePath != "" {
		return errors.New("strict_output_invalid: handoff_note_prohibited")
	}
	return nil
}

type strictSchemaColumn struct {
	types          map[string]bool
	requireNotNull bool
}

func strictColumn(sourceType string, requireNotNull bool) strictSchemaColumn {
	return strictSchemaColumn{types: map[string]bool{sourceType: true}, requireNotNull: requireNotNull}
}

var strictSnapshotSchema = map[string]map[string]strictSchemaColumn{
	"call_logs": {
		"id": strictColumn("int8", true), "request_id": strictColumn("varchar", true),
		"org_id": strictColumn("int8", true), "project_id": strictColumn("int8", false),
		"api_key_id": strictColumn("int8", false), "owner_user_id": strictColumn("int8", false),
		"key_type": strictColumn("varchar", true), "model": strictColumn("varchar", true),
		"requested_model": strictColumn("varchar", true), "provider": strictColumn("varchar", true),
		"channel": strictColumn("varchar", true), "prompt_tokens": strictColumn("int4", true),
		"completion_tokens": strictColumn("int4", true), "total_tokens": strictColumn("int4", true),
		"estimated_tokens": strictColumn("int4", true), "usage_source": strictColumn("varchar", true),
		"usage_confidence": strictColumn("numeric(4,3)", true), "cost_micros": strictColumn("int8", true),
		"latency_ms": strictColumn("int4", true), "ttft_ms": strictColumn("int4", true),
		"status_code": strictColumn("int4", true), "upstream_status_code": strictColumn("int4", false),
		"upstream_error": strictColumn("text", false), "ip_address": strictColumn("varchar", true),
		"is_stream": strictColumn("bool", true), "created_at": strictColumn("timestamptz", true),
		"trace_id": strictColumn("varchar", false), "session_id": strictColumn("varchar", false),
		"upstream_request_id": strictColumn("varchar", false), "schema_version": strictColumn("varchar", true),
		"capture_mode": strictColumn("varchar", true), "evidence_ref": strictColumn("varchar", false),
		"source_event_hash": strictColumn("varchar", false), "actor_type": strictColumn("varchar", false),
		"actor_ref": strictColumn("varchar", false), "on_behalf_of": strictColumn("varchar", false),
		"payload_capture_mode": strictColumn("varchar", true), "payload_ref": strictColumn("varchar", false),
	},
	"request_trace_events": {
		"id": strictColumn("int8", true), "request_id": strictColumn("varchar", true),
		"org_id": strictColumn("int8", true), "project_id": strictColumn("int8", false),
		"api_key_id": strictColumn("int8", false), "owner_user_id": strictColumn("int8", false),
		"key_type": strictColumn("varchar", true), "event_type": strictColumn("varchar", true),
		"stage": strictColumn("varchar", true), "severity": strictColumn("varchar", true),
		"model": strictColumn("varchar", true), "requested_model": strictColumn("varchar", true),
		"provider": strictColumn("varchar", true), "channel": strictColumn("varchar", true),
		"status_code": strictColumn("int4", false), "latency_ms": strictColumn("int4", true),
		"metadata": strictColumn("jsonb", true), "created_at": strictColumn("timestamptz", true),
		"trace_id": strictColumn("varchar", false), "span_id": strictColumn("varchar", false),
		"parent_span_id": strictColumn("varchar", false), "schema_version": strictColumn("varchar", true),
		"source_component": strictColumn("varchar", true), "capture_mode": strictColumn("varchar", true),
		"capture_gap": strictColumn("varchar", false), "evidence_ref": strictColumn("varchar", false),
		"upstream_request_id": strictColumn("varchar", false), "decision_kind": strictColumn("varchar", false),
		"decision_result": strictColumn("varchar", false), "decision_reason": strictColumn("varchar", false),
		"actor_type": strictColumn("varchar", false), "actor_ref": strictColumn("varchar", false),
		"on_behalf_of": strictColumn("varchar", false), "session_id": strictColumn("varchar", false),
		"payload_capture_mode": strictColumn("varchar", true), "payload_ref": strictColumn("varchar", false),
		"source_event_hash": strictColumn("varchar", false),
	},
}

const strictSchemaPreflightQuery = `
SELECT
  c.relname,
  a.attname,
  CASE
    WHEN t.typname = 'numeric' THEN pg_catalog.format_type(a.atttypid, a.atttypmod)
    ELSE t.typname
  END AS source_type,
  a.attnotnull
FROM pg_catalog.pg_class c
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid
JOIN pg_catalog.pg_type t ON t.oid = a.atttypid
WHERE n.nspname = 'public'
  AND c.relname IN ('call_logs', 'request_trace_events')
  AND c.relkind IN ('r', 'p')
  AND a.attnum > 0
  AND NOT a.attisdropped
ORDER BY c.relname, a.attname`

func preflightSnapshotV2Schema(ctx context.Context, tx strictReadTx) error {
	rows, err := tx.QueryContext(ctx, strictSchemaPreflightQuery)
	if err != nil {
		return errors.New("strict_schema_preflight_failed")
	}
	defer func() { _ = rows.Close() }()

	seen := make(map[string]map[string]bool, len(strictSnapshotSchema))
	for rows.Next() {
		var relation, column, sourceType string
		var notNull bool
		if err := rows.Scan(&relation, &column, &sourceType, &notNull); err != nil {
			return errors.New("strict_schema_preflight_failed")
		}
		relationSpec, expectedRelation := strictSnapshotSchema[relation]
		if !expectedRelation {
			continue
		}
		columnSpec, expectedColumn := relationSpec[column]
		if !expectedColumn {
			continue
		}
		if !columnSpec.types[sourceType] || (columnSpec.requireNotNull && !notNull) {
			return fmt.Errorf("strict_schema_incompatible_column: %s.%s", relation, column)
		}
		if seen[relation] == nil {
			seen[relation] = map[string]bool{}
		}
		seen[relation][column] = true
	}
	if rows.Err() != nil {
		return errors.New("strict_schema_preflight_failed")
	}
	for relation, columns := range strictSnapshotSchema {
		if seen[relation] == nil {
			return fmt.Errorf("strict_schema_missing_relation: %s", relation)
		}
		for column := range columns {
			if !seen[relation][column] {
				return fmt.Errorf("strict_schema_missing_column: %s.%s", relation, column)
			}
		}
	}
	return nil
}

type strictSourceAggregate struct {
	Ordinal              int
	CallLogs             int64
	TraceEvents          int64
	DistinctOrgs         int64
	OrgID                int64
	IPAddressPresentRows int64
	RawErrorRows         int64
	BlockedValueRows     int64
}

func strictAggregateQuery(requestIDCount int) string {
	selected := make([]string, requestIDCount)
	for i := range selected {
		selected[i] = fmt.Sprintf("($%d::text, %d)", i+1, i+1)
	}
	return fmt.Sprintf(`
WITH selected(request_id, ord) AS (
  VALUES %s
), source_rows AS (
  SELECT
    cl.request_id::text AS request_id,
    cl.org_id,
    cl.id,
    'call_logs'::text AS source_name,
    (cl.ip_address IS NOT NULL AND cl.ip_address <> '') AS ip_address_present,
    (cl.upstream_error IS NOT NULL AND cl.upstream_error <> '') AS raw_error_present,
    (
      lower(concat_ws('|', cl.request_id, cl.key_type, cl.model, cl.requested_model,
        cl.provider, cl.channel, cl.usage_source, cl.trace_id, cl.session_id,
        cl.upstream_request_id, cl.schema_version, cl.capture_mode, cl.evidence_ref,
        cl.source_event_hash, cl.actor_type, cl.actor_ref, cl.on_behalf_of,
        cl.payload_capture_mode, cl.payload_ref)) ~
        '(@|://|www[.]|bearer[[:space:]]+|basic[[:space:]]+|sk-[a-z0-9_-]{12,}|akia|aiza|ghp_|github_pat_|xox[baprs]-|(api[_-]?key|authorization|password|secret|access[_-]?token|refresh[_-]?token)[[:space:]]*[:=]|token[=:]|client_secret|private_key|access_key_id|-----begin[[:space:]][a-z ]+private[[:space:]]key-----|dsn=|customer|tenant|account[_-]?id|(^|[^0-9])1[3-9][0-9]{9}([^0-9]|$)|(^|[^a-z0-9])([+][0-9]{1,3}[ .-]?)?([(][0-9]{2,4}[)][ .-]?|[0-9]{2,4}[ .-])[0-9]{3,4}[ .-][0-9]{4}($|[^a-z0-9])|([0-9]{1,3}\.){3}[0-9]{1,3}|([0-9a-f]{1,4}:){2,})'
      OR concat_ws('|', cl.key_type, cl.model, cl.requested_model, cl.provider,
        cl.channel, cl.usage_source, cl.schema_version, cl.capture_mode,
        cl.payload_capture_mode) ~ '[[:space:]]'
    ) AS blocked_value_present
  FROM public.call_logs cl
  JOIN selected s ON s.request_id = cl.request_id
  UNION ALL
  SELECT
    te.request_id::text,
    te.org_id,
    te.id,
    'request_trace_events'::text,
    false,
    false,
    (
      lower(concat_ws('|', te.request_id, te.key_type, te.event_type, te.stage,
        te.severity, te.model, te.requested_model, te.provider, te.channel,
        te.metadata::text, te.trace_id, te.span_id, te.parent_span_id,
        te.schema_version, te.source_component, te.capture_mode, te.capture_gap,
        te.evidence_ref, te.upstream_request_id, te.decision_kind,
        te.decision_result, te.decision_reason, te.actor_type, te.actor_ref,
        te.on_behalf_of, te.session_id, te.payload_capture_mode, te.payload_ref,
        te.source_event_hash)) ~
        '(@|://|www[.]|bearer[[:space:]]+|basic[[:space:]]+|sk-[a-z0-9_-]{12,}|akia|aiza|ghp_|github_pat_|xox[baprs]-|(api[_-]?key|authorization|password|secret|access[_-]?token|refresh[_-]?token)[[:space:]]*[:=]|token[=:]|client_secret|private_key|access_key_id|-----begin[[:space:]][a-z ]+private[[:space:]]key-----|dsn=|customer|tenant|account[_-]?id|(^|[^0-9])1[3-9][0-9]{9}([^0-9]|$)|(^|[^a-z0-9])([+][0-9]{1,3}[ .-]?)?([(][0-9]{2,4}[)][ .-]?|[0-9]{2,4}[ .-])[0-9]{3,4}[ .-][0-9]{4}($|[^a-z0-9])|([0-9]{1,3}\.){3}[0-9]{1,3}|([0-9a-f]{1,4}:){2,})'
      OR concat_ws('|', te.key_type, te.event_type, te.stage, te.severity,
        te.model, te.requested_model, te.provider, te.channel,
        te.schema_version, te.source_component, te.capture_mode,
        te.decision_kind, te.decision_result, te.decision_reason,
        te.payload_capture_mode) ~ '[[:space:]]'
    )
  FROM public.request_trace_events te
  JOIN selected s ON s.request_id = te.request_id
)
SELECT
  s.ord,
  COUNT(r.id) FILTER (WHERE r.source_name = 'call_logs'),
  COUNT(r.id) FILTER (WHERE r.source_name = 'request_trace_events'),
  COUNT(DISTINCT r.org_id),
  MIN(r.org_id),
  COUNT(r.id) FILTER (WHERE r.ip_address_present),
  COUNT(r.id) FILTER (WHERE r.raw_error_present),
  COUNT(r.id) FILTER (WHERE r.blocked_value_present)
FROM selected s
LEFT JOIN source_rows r ON r.request_id = s.request_id
GROUP BY s.ord, s.request_id
ORDER BY s.ord`, strings.Join(selected, ", "))
}

func loadStrictSourceAggregates(ctx context.Context, tx strictReadTx, requestIDs []string) ([]strictSourceAggregate, error) {
	rows, err := tx.QueryContext(ctx, strictAggregateQuery(len(requestIDs)), argsFromStrings(requestIDs)...)
	if err != nil {
		return nil, errors.New("strict_source_aggregate_failed")
	}
	defer func() { _ = rows.Close() }()

	aggregates := make([]strictSourceAggregate, 0, len(requestIDs))
	for rows.Next() {
		var aggregate strictSourceAggregate
		var orgID sql.NullInt64
		if err := rows.Scan(
			&aggregate.Ordinal,
			&aggregate.CallLogs,
			&aggregate.TraceEvents,
			&aggregate.DistinctOrgs,
			&orgID,
			&aggregate.IPAddressPresentRows,
			&aggregate.RawErrorRows,
			&aggregate.BlockedValueRows,
		); err != nil {
			return nil, errors.New("strict_source_aggregate_failed")
		}
		if orgID.Valid {
			aggregate.OrgID = orgID.Int64
		}
		aggregates = append(aggregates, aggregate)
	}
	if rows.Err() != nil {
		return nil, errors.New("strict_source_aggregate_failed")
	}
	return aggregates, nil
}

func validateStrictSourceAggregates(aggregates []strictSourceAggregate, requestCount int) error {
	if len(aggregates) != requestCount {
		return errors.New("strict_source_aggregate_incomplete")
	}
	var boundOrgID int64
	var totalRows int64
	var gapCount int64
	for idx, aggregate := range aggregates {
		if aggregate.CallLogs < 0 || aggregate.TraceEvents < 0 || aggregate.CallLogs > maxSnapshotRows || aggregate.TraceEvents > maxSnapshotRows {
			return errors.New("strict_row_limit: aggregate_count_out_of_range")
		}
		if aggregate.Ordinal != idx+1 {
			return errors.New("strict_source_aggregate_incomplete")
		}
		if aggregate.CallLogs+aggregate.TraceEvents == 0 {
			return fmt.Errorf("strict_source_missing_coverage: request_index=%d", idx+1)
		}
		if aggregate.DistinctOrgs != 1 {
			return fmt.Errorf("strict_source_cross_org_collision: request_index=%d", idx+1)
		}
		if aggregate.OrgID <= 0 {
			return fmt.Errorf("strict_source_invalid_organization: request_index=%d", idx+1)
		}
		if boundOrgID == 0 {
			boundOrgID = aggregate.OrgID
		} else if aggregate.OrgID != boundOrgID {
			return errors.New("strict_source_multiple_organizations")
		}
		if aggregate.CallLogs > 1 {
			return fmt.Errorf("strict_source_duplicate_call_log: request_index=%d", idx+1)
		}
		if aggregate.IPAddressPresentRows < 0 || aggregate.IPAddressPresentRows > aggregate.CallLogs {
			return errors.New("strict_source_aggregate_invalid: ip_presence_count")
		}
		if aggregate.RawErrorRows > 0 {
			return fmt.Errorf("strict_source_blocked: class=raw_error count=%d", aggregate.RawErrorRows)
		}
		if aggregate.BlockedValueRows > 0 {
			return fmt.Errorf("strict_source_blocked: class=blocked_value count=%d", aggregate.BlockedValueRows)
		}
		if aggregate.CallLogs == 0 {
			gapCount++
		}
		if aggregate.TraceEvents == 0 {
			gapCount++
		}
		rowCount := aggregate.CallLogs + aggregate.TraceEvents
		if totalRows+rowCount > maxSnapshotRows {
			return fmt.Errorf("strict_row_limit: count=%d limit=%d", totalRows+rowCount, maxSnapshotRows)
		}
		totalRows += rowCount
	}
	if totalRows+gapCount > maxSnapshotRows {
		return fmt.Errorf("strict_row_limit: count=%d limit=%d", totalRows+gapCount, maxSnapshotRows)
	}
	return nil
}

func loadStrictSnapshot(
	ctx context.Context,
	db strictSnapshotDB,
	sourceInstance string,
	selectionMode string,
	exchangeID string,
	approvalManifestSHA256 string,
	requestIDs []string,
	scenarios []snapshotScenario,
	generatedAt string,
) (snapshotFile, error) {
	if len(requestIDs) < 1 || len(requestIDs) > maxApprovedRequestIDs {
		return snapshotFile{}, fmt.Errorf("strict_request_limit: count=%d limit=%d", len(requestIDs), maxApprovedRequestIDs)
	}
	tx, err := db.BeginStrict(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return snapshotFile{}, errors.New("strict_transaction_begin_failed")
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var isolation, readOnly string
	if err := tx.QueryRowContext(ctx, `SELECT current_setting('transaction_isolation'), current_setting('transaction_read_only')`).Scan(&isolation, &readOnly); err != nil {
		return snapshotFile{}, errors.New("strict_transaction_confirmation_failed")
	}
	if isolation != "repeatable read" {
		return snapshotFile{}, errors.New("strict_transaction_mode_invalid: isolation")
	}
	if readOnly != "on" {
		return snapshotFile{}, errors.New("strict_transaction_mode_invalid: read_only")
	}
	if err := preflightSnapshotV2Schema(ctx, tx); err != nil {
		return snapshotFile{}, err
	}
	aggregates, err := loadStrictSourceAggregates(ctx, tx, requestIDs)
	if err != nil {
		return snapshotFile{}, err
	}
	if err := validateStrictSourceAggregates(aggregates, len(requestIDs)); err != nil {
		return snapshotFile{}, err
	}
	callLogs, err := loadStrictCallLogs(ctx, tx, sourceInstance, requestIDs)
	if err != nil {
		return snapshotFile{}, err
	}
	traceEvents, err := loadStrictTraceEvents(ctx, tx, sourceInstance, requestIDs)
	if err != nil {
		return snapshotFile{}, err
	}
	gaps := buildCaptureGaps(requestIDs, callLogs, traceEvents)
	snapshot := newStrictSnapshotFile(
		sourceInstance,
		selectionMode,
		exchangeID,
		approvalManifestSHA256,
		requestIDs,
		scenarios,
		generatedAt,
		callLogs,
		traceEvents,
		gaps,
	)
	if err := validateStrictSnapshotValuePolicy(snapshot); err != nil {
		return snapshotFile{}, err
	}
	binding := strictSnapshotBinding{
		SourceInstance: sourceInstance, SelectionMode: selectionMode, ExchangeID: exchangeID,
		ApprovalManifestSHA256: approvalManifestSHA256,
		RequestIDs:             append([]string(nil), requestIDs...), Scenarios: append([]snapshotScenario(nil), scenarios...),
	}
	if err := reconcileStrictSnapshot(binding, aggregates, snapshot); err != nil {
		return snapshotFile{}, err
	}
	if err := tx.Commit(); err != nil {
		return snapshotFile{}, errors.New("strict_transaction_commit_failed")
	}
	committed = true
	return snapshot, nil
}

func newStrictSnapshotFile(
	sourceInstance, selectionMode, exchangeID, approvalManifestSHA256 string,
	requestIDs []string,
	scenarios []snapshotScenario,
	generatedAt string,
	callLogs []snapshotCallLog,
	traceEvents []snapshotTraceEvent,
	gaps []snapshotCaptureGap,
) snapshotFile {
	return snapshotFile{
		SchemaVersion:          prd5SnapshotSchemaVersion,
		ExchangeID:             exchangeID,
		ApprovalManifestSHA256: approvalManifestSHA256,
		GeneratedAt:            generatedAt,
		Source:                 "unirouter",
		SourceInstance:         sourceInstance,
		RequestIDs:             append([]string(nil), requestIDs...),
		Scenarios:              append([]snapshotScenario(nil), scenarios...),
		Safety: snapshotSafety{
			SelectionMode:        selectionMode,
			RawPromptsIncluded:   false,
			RawResponsesIncluded: false,
			APIKeysIncluded:      false,
			IPHandling:           strictIPHandling,
			ErrorHandling:        strictErrorHandling,
			MetadataHandling:     strictMetadataHandling,
		},
		Counts: snapshotCounts{
			RequestedRequestIDs: len(requestIDs),
			CallLogs:            len(callLogs),
			TraceEvents:         len(traceEvents),
			CaptureGaps:         len(gaps),
		},
		CaptureGaps: append([]snapshotCaptureGap{}, gaps...),
		CallLogs:    append([]snapshotCallLog{}, callLogs...),
		TraceEvents: append([]snapshotTraceEvent{}, traceEvents...),
		Notes: map[string]interface{}{
			"request_id_join": "request_id is the intended join key between call_logs and request_trace_events",
			"missing_rows":    "missing call logs or trace events are represented as capture gaps and should not be fabricated by importers",
			"payload_policy":  "UniRouter raw prompt/response capture remains disabled for this snapshot path",
			"money_contract":  "cost_micros is canonical non-negative integer micro-CNY text; currency=CNY, amount_scale=1000000, money_schema=micro-cny.v1; cost_cents is absent",
		},
	}
}

type strictPublishResult struct {
	Published bool
	Length    int64
	SHA256    string
}

func publishStrictSnapshot(stage snapshotpublish.Stage, snapshot snapshotFile) (strictPublishResult, error) {
	defer func() { _ = stage.Close() }()
	initial, err := stage.Stat()
	if err != nil || !validStrictAnonymousIdentity(initial, 0) {
		return strictPublishResult{}, errors.New("strict_output_stage_invalid")
	}

	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(snapshot); err != nil {
		return strictPublishResult{}, errors.New("strict_output_encode_failed")
	}
	payload := encoded.Bytes()
	expectedSHA := rawSHA256(payload)
	if err := writeAllStrict(stage, payload); err != nil {
		return strictPublishResult{}, err
	}
	if err := stage.Sync(); err != nil {
		return strictPublishResult{}, errors.New("strict_output_fsync_failed")
	}
	written, err := stage.Stat()
	if err != nil || !sameStrictIdentity(initial, written) || !validStrictAnonymousIdentity(written, int64(len(payload))) {
		return strictPublishResult{}, errors.New("strict_output_stage_invariant_failed")
	}
	readback := make([]byte, len(payload)+1)
	n, readErr := stage.ReadAt(readback, 0)
	if n != len(payload) || !errors.Is(readErr, io.EOF) {
		return strictPublishResult{}, errors.New("strict_output_readback_failed")
	}
	readback = readback[:n]
	if !bytes.Equal(readback, payload) || rawSHA256(readback) != expectedSHA {
		return strictPublishResult{}, errors.New("strict_output_sha256_mismatch")
	}
	finalIdentity, err := stage.Stat()
	if err != nil || !sameStrictIdentity(initial, finalIdentity) || !validStrictAnonymousIdentity(finalIdentity, int64(len(payload))) {
		return strictPublishResult{}, errors.New("strict_output_precommit_invariant_failed")
	}
	if err := stage.Commit(); err != nil {
		if errors.Is(err, snapshotpublish.ErrTargetExists) {
			return strictPublishResult{}, errors.New("strict_output_target_exists")
		}
		return strictPublishResult{}, errors.New("strict_output_atomic_commit_failed")
	}
	return strictPublishResult{Published: true, Length: int64(len(payload)), SHA256: expectedSHA}, nil
}

func writeAllStrict(stage snapshotpublish.Stage, payload []byte) error {
	for written := 0; written < len(payload); {
		n, err := stage.Write(payload[written:])
		if errors.Is(err, syscall.EINTR) {
			// The Linux syscall wrapper reports n=-1 for an interrupted write.
			// Do not interpret that sentinel as a byte count; retry the anonymous
			// descriptor write only. A positive partial count is retained if an
			// injected/test implementation reports one with EINTR.
			if n > 0 {
				if n > len(payload)-written {
					return errors.New("strict_output_write_invalid_count")
				}
				written += n
			}
			continue
		}
		if n < 0 || n > len(payload)-written {
			return errors.New("strict_output_write_invalid_count")
		}
		written += n
		if err != nil {
			return errors.New("strict_output_write_failed")
		}
		if n == 0 {
			return errors.New("strict_output_write_no_progress")
		}
	}
	return nil
}

func rawSHA256(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func sameStrictIdentity(left, right snapshotpublish.Identity) bool {
	return left.Device == right.Device && left.Inode == right.Inode
}

func validStrictAnonymousIdentity(identity snapshotpublish.Identity, expectedSize int64) bool {
	return identity.Regular && !identity.Directory && identity.UID == uint32(os.Geteuid()) && identity.Mode == 0o600 && identity.Nlink == 0 && identity.Size == expectedSize
}

func loadSnapshot(ctx context.Context, db *sql.DB, sourceInstance string, selectionMode string, exchangeID string, approvalManifestSHA256 string, requestIDs []string, scenarios []snapshotScenario) (snapshotFile, error) {
	callLogs, err := loadCallLogs(ctx, db, sourceInstance, requestIDs)
	if err != nil {
		return snapshotFile{}, err
	}
	traceEvents, err := loadTraceEvents(ctx, db, sourceInstance, requestIDs)
	if err != nil {
		return snapshotFile{}, err
	}

	gaps := buildCaptureGaps(requestIDs, callLogs, traceEvents)
	snapshot := snapshotFile{
		SchemaVersion:          prd5SnapshotSchemaVersion,
		ExchangeID:             exchangeID,
		ApprovalManifestSHA256: approvalManifestSHA256,
		GeneratedAt:            time.Now().UTC().Format(time.RFC3339Nano),
		Source:                 "unirouter",
		SourceInstance:         sourceInstance,
		RequestIDs:             requestIDs,
		Scenarios:              scenarios,
		Safety: snapshotSafety{
			SelectionMode:        selectionMode,
			RawPromptsIncluded:   false,
			RawResponsesIncluded: false,
			APIKeysIncluded:      false,
			IPHandling:           "raw IP addresses are not exported; only ip_address_was_present is retained",
			ErrorHandling:        "raw upstream_error text is not exported; only upstream_error_present is retained",
			MetadataHandling:     "request_trace_events.metadata is re-sanitized through internal/traceutil.SanitizeMetadata before export",
		},
		Counts: snapshotCounts{
			RequestedRequestIDs: len(requestIDs),
			CallLogs:            len(callLogs),
			TraceEvents:         len(traceEvents),
			CaptureGaps:         len(gaps),
		},
		CaptureGaps: gaps,
		CallLogs:    callLogs,
		TraceEvents: traceEvents,
		Notes: map[string]interface{}{
			"request_id_join": "request_id is the intended join key between call_logs and request_trace_events",
			"missing_rows":    "missing call logs or trace events are represented as capture gaps and should not be fabricated by importers",
			"payload_policy":  "UniRouter raw prompt/response capture remains disabled for this snapshot path",
			"money_contract":  "cost_micros is canonical non-negative integer micro-CNY text; currency=CNY, amount_scale=1000000, money_schema=micro-cny.v1; cost_cents is absent",
		},
	}
	return snapshot, nil
}

func loadCallLogs(ctx context.Context, db *sql.DB, sourceInstance string, requestIDs []string) ([]snapshotCallLog, error) {
	rows, err := db.QueryContext(ctx, callLogsQuery(len(requestIDs)), argsFromStrings(requestIDs)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []snapshotCallLog
	for rows.Next() {
		row, err := scanSnapshotCallLog(rows, sourceInstance)
		if err != nil {
			return nil, err
		}
		logs = append(logs, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return logs, nil
}

func loadStrictCallLogs(ctx context.Context, tx strictReadTx, sourceInstance string, requestIDs []string) ([]snapshotCallLog, error) {
	rows, err := tx.QueryContext(ctx, strictCallLogsQuery(len(requestIDs)), argsFromStrings(requestIDs)...)
	if err != nil {
		return nil, errors.New("strict_source_read_failed: call_logs")
	}
	defer func() { _ = rows.Close() }()

	logs := make([]snapshotCallLog, 0)
	for rows.Next() {
		row, err := scanSnapshotCallLogStrict(rows, sourceInstance)
		if err != nil {
			return nil, err
		}
		logs = append(logs, row)
	}
	if rows.Err() != nil {
		return nil, errors.New("strict_source_read_failed: call_logs")
	}
	return logs, nil
}

func callLogsQuery(requestIDCount int) string {
	return fmt.Sprintf(`
SELECT
  id,
  request_id,
  org_id,
  project_id,
  api_key_id,
  owner_user_id,
  key_type,
  model,
  requested_model,
  provider,
  channel,
  prompt_tokens,
  completion_tokens,
  total_tokens,
  estimated_tokens,
  usage_source,
  usage_confidence::text,
  cost_micros::text,
  latency_ms,
  ttft_ms,
  status_code,
  upstream_status_code,
  upstream_error IS NOT NULL AND upstream_error <> '' AS upstream_error_present,
  ip_address IS NOT NULL AND ip_address <> '' AS ip_address_present,
  is_stream,
  created_at,
  trace_id,
  session_id,
  upstream_request_id,
  schema_version,
  capture_mode,
  evidence_ref,
  source_event_hash,
  actor_type,
  actor_ref,
  on_behalf_of,
  payload_capture_mode,
  payload_ref
FROM call_logs
WHERE request_id IN (%s)
ORDER BY created_at, id`, placeholders(requestIDCount))
}

func strictCallLogsQuery(requestIDCount int) string {
	return strings.Replace(callLogsQuery(requestIDCount), "FROM call_logs", "FROM public.call_logs", 1)
}

type snapshotRowScanner interface {
	Scan(dest ...any) error
}

func scanSnapshotCallLog(scanner snapshotRowScanner, sourceInstance string) (snapshotCallLog, error) {
	return scanSnapshotCallLogMode(scanner, sourceInstance, false)
}

func scanSnapshotCallLogStrict(scanner snapshotRowScanner, sourceInstance string) (snapshotCallLog, error) {
	return scanSnapshotCallLogMode(scanner, sourceInstance, true)
}

func scanSnapshotCallLogMode(scanner snapshotRowScanner, sourceInstance string, strict bool) (snapshotCallLog, error) {
	var row snapshotCallLog
	var projectID, apiKeyID, ownerUserID, upstreamStatusCode sql.NullInt64
	var traceID, sessionID, upstreamRequestID, evidenceRef, sourceEventHash sql.NullString
	var actorType, actorRef, onBehalfOf, payloadRef sql.NullString
	var createdAt time.Time
	if err := scanner.Scan(
		&row.ID,
		&row.RequestID,
		&row.OrgID,
		&projectID,
		&apiKeyID,
		&ownerUserID,
		&row.KeyType,
		&row.Model,
		&row.RequestedModel,
		&row.Provider,
		&row.Channel,
		&row.PromptTokens,
		&row.CompletionTokens,
		&row.TotalTokens,
		&row.EstimatedTokens,
		&row.UsageSource,
		&row.UsageConfidence,
		&row.CostMicros,
		&row.LatencyMS,
		&row.TTFTMS,
		&row.StatusCode,
		&upstreamStatusCode,
		&row.UpstreamErrorPresent,
		&row.IPAddressWasPresent,
		&row.IsStream,
		&createdAt,
		&traceID,
		&sessionID,
		&upstreamRequestID,
		&row.DerivedSchemaVersion,
		&row.DerivedCaptureMode,
		&evidenceRef,
		&sourceEventHash,
		&actorType,
		&actorRef,
		&onBehalfOf,
		&row.PayloadCaptureMode,
		&payloadRef,
	); err != nil {
		if strict {
			return snapshotCallLog{}, errors.New("strict_source_read_failed: call_logs row")
		}
		return snapshotCallLog{}, err
	}
	if err := validateCostMicros(row.CostMicros); err != nil {
		if strict {
			return snapshotCallLog{}, errors.New("strict_value_policy: call_logs.cost_micros canonical_integer")
		}
		return snapshotCallLog{}, err
	}
	row.ProjectID = nullableInt64(projectID)
	row.Currency = "CNY"
	row.AmountScale = 1_000_000
	row.MoneySchema = "micro-cny.v1"
	row.APIKeyID = nullableInt64(apiKeyID)
	row.OwnerUserID = nullableInt64(ownerUserID)
	row.UpstreamStatusCode = nullableInt64(upstreamStatusCode)
	row.IPAddressRedacted = row.IPAddressWasPresent
	row.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
	row.TraceID = nullableStringSQL(traceID)
	row.SessionID = nullableStringSQL(sessionID)
	row.UpstreamRequestID = nullableStringSQL(upstreamRequestID)
	row.EvidenceRef = nullableStringSQL(evidenceRef)
	row.ActorType = nullableStringSQL(actorType)
	row.ActorRef = nullableStringSQL(actorRef)
	row.OnBehalfOf = nullableStringSQL(onBehalfOf)
	if strict {
		payload, mode, err := validateStrictPayloadContract("call_logs", payloadRef, row.PayloadCaptureMode)
		if err != nil {
			return snapshotCallLog{}, err
		}
		row.PayloadRef = payload
		row.PayloadCaptureMode = mode
	} else {
		row.PayloadRef = nullableStringSQL(payloadRef)
		if row.PayloadCaptureMode == "" {
			row.PayloadCaptureMode = "metadata_only"
		}
		if row.DerivedSchemaVersion == "" {
			row.DerivedSchemaVersion = prd5SnapshotSchemaVersion
		}
		if row.DerivedCaptureMode == "" {
			row.DerivedCaptureMode = "db_import"
		}
	}
	row.PayloadCaptureReason = prd5PayloadCaptureReason
	row.DerivedSourceComponent = "logging"
	row.SourceRef = sourceRef(sourceInstance, "call_logs", row.ID)
	if strict {
		if row.UpstreamErrorPresent {
			return snapshotCallLog{}, errors.New("strict_source_blocked: class=raw_error count=1")
		}
		if err := validateStrictIPProof(row.IPAddressWasPresent, row.IPAddressRedacted); err != nil {
			return snapshotCallLog{}, err
		}
		hash, err := normalizeStrictSourceEventHash(sourceEventHash, row)
		if err != nil {
			return snapshotCallLog{}, err
		}
		row.SourceEventHash = hash
	} else if sourceEventHash.Valid && sourceEventHash.String != "" {
		row.SourceEventHash = sourceEventHash.String
	} else {
		row.SourceEventHash = stableHash(row)
	}
	return row, nil
}

func validateCostMicros(value string) error {
	if value == "0" {
		return nil
	}
	if value == "" || value[0] < '1' || value[0] > '9' {
		return errors.New("cost_micros must be canonical non-negative integer text")
	}
	for i := 1; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return errors.New("cost_micros must be canonical non-negative integer text")
		}
	}
	return nil
}

func traceEventsQuery(requestIDCount int, strict bool) string {
	relation := "request_trace_events"
	if strict {
		relation = "public.request_trace_events"
	}
	return fmt.Sprintf(`
SELECT
  id,
  request_id,
  org_id,
  project_id,
  api_key_id,
  owner_user_id,
  key_type,
  event_type,
  stage,
  severity,
  model,
  requested_model,
  provider,
  channel,
  status_code,
  latency_ms,
  metadata::text,
  created_at,
  trace_id,
  span_id,
  parent_span_id,
  schema_version,
  source_component,
  capture_mode,
  capture_gap,
  evidence_ref,
  upstream_request_id,
  decision_kind,
  decision_result,
  decision_reason,
  actor_type,
  actor_ref,
  on_behalf_of,
  session_id,
  payload_capture_mode,
  payload_ref,
  source_event_hash
FROM %s
WHERE request_id IN (%s)
ORDER BY created_at, id`, relation, placeholders(requestIDCount))
}

func loadTraceEvents(ctx context.Context, db *sql.DB, sourceInstance string, requestIDs []string) ([]snapshotTraceEvent, error) {
	rows, err := db.QueryContext(ctx, traceEventsQuery(len(requestIDs), false), argsFromStrings(requestIDs)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []snapshotTraceEvent
	for rows.Next() {
		row, err := scanSnapshotTraceEvent(rows, sourceInstance, false)
		if err != nil {
			return nil, err
		}
		events = append(events, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func loadStrictTraceEvents(ctx context.Context, tx strictReadTx, sourceInstance string, requestIDs []string) ([]snapshotTraceEvent, error) {
	rows, err := tx.QueryContext(ctx, traceEventsQuery(len(requestIDs), true), argsFromStrings(requestIDs)...)
	if err != nil {
		return nil, errors.New("strict_source_read_failed: request_trace_events")
	}
	defer func() { _ = rows.Close() }()

	events := make([]snapshotTraceEvent, 0)
	for rows.Next() {
		row, err := scanSnapshotTraceEvent(rows, sourceInstance, true)
		if err != nil {
			return nil, err
		}
		events = append(events, row)
	}
	if rows.Err() != nil {
		return nil, errors.New("strict_source_read_failed: request_trace_events")
	}
	return events, nil
}

func scanSnapshotTraceEvent(scanner snapshotRowScanner, sourceInstance string, strict bool) (snapshotTraceEvent, error) {
	var row snapshotTraceEvent
	var projectID, apiKeyID, ownerUserID, statusCode sql.NullInt64
	var traceID, spanID, parentSpanID, captureGap, evidenceRef, upstreamRequestID sql.NullString
	var decisionKind, decisionResult, decisionReason, actorType, actorRef, onBehalfOf, sessionID sql.NullString
	var payloadRef, sourceEventHash sql.NullString
	var metadataText string
	var createdAt time.Time
	if err := scanner.Scan(
		&row.ID,
		&row.RequestID,
		&row.OrgID,
		&projectID,
		&apiKeyID,
		&ownerUserID,
		&row.KeyType,
		&row.EventType,
		&row.Stage,
		&row.Severity,
		&row.Model,
		&row.RequestedModel,
		&row.Provider,
		&row.Channel,
		&statusCode,
		&row.LatencyMS,
		&metadataText,
		&createdAt,
		&traceID,
		&spanID,
		&parentSpanID,
		&row.DerivedSchemaVersion,
		&row.DerivedSourceComponent,
		&row.DerivedCaptureMode,
		&captureGap,
		&evidenceRef,
		&upstreamRequestID,
		&decisionKind,
		&decisionResult,
		&decisionReason,
		&actorType,
		&actorRef,
		&onBehalfOf,
		&sessionID,
		&row.PayloadCaptureMode,
		&payloadRef,
		&sourceEventHash,
	); err != nil {
		if strict {
			return snapshotTraceEvent{}, errors.New("strict_source_read_failed: request_trace_events row")
		}
		return snapshotTraceEvent{}, err
	}

	var metadata map[string]interface{}
	var err error
	if strict {
		metadata, err = strictMetadata(metadataText)
	} else {
		metadata, err = sanitizedMetadata(metadataText)
	}
	if err != nil {
		if strict {
			return snapshotTraceEvent{}, err
		}
		return snapshotTraceEvent{}, err
	}
	row.ProjectID = nullableInt64(projectID)
	row.APIKeyID = nullableInt64(apiKeyID)
	row.OwnerUserID = nullableInt64(ownerUserID)
	row.StatusCode = nullableInt64(statusCode)
	row.Metadata = metadata
	row.MetadataKeys = sortedKeys(metadata)
	row.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
	row.TraceID = nullableStringSQL(traceID)
	row.SpanID = nullableStringSQL(spanID)
	row.ParentSpanID = nullableStringSQL(parentSpanID)
	row.CaptureGap = nullableStringSQL(captureGap)
	row.EvidenceRef = nullableStringSQL(evidenceRef)
	row.UpstreamRequestID = nullableStringSQL(upstreamRequestID)
	row.DecisionKind = nullableStringSQL(decisionKind)
	row.DecisionResult = nullableStringSQL(decisionResult)
	row.DecisionReason = nullableStringSQL(decisionReason)
	row.ActorType = nullableStringSQL(actorType)
	row.ActorRef = nullableStringSQL(actorRef)
	row.OnBehalfOf = nullableStringSQL(onBehalfOf)
	row.SessionID = nullableStringSQL(sessionID)
	if strict {
		payload, mode, err := validateStrictPayloadContract("request_trace_events", payloadRef, row.PayloadCaptureMode)
		if err != nil {
			return snapshotTraceEvent{}, err
		}
		row.PayloadRef = payload
		row.PayloadCaptureMode = mode
	} else {
		row.PayloadRef = nullableStringSQL(payloadRef)
		if row.PayloadCaptureMode == "" {
			row.PayloadCaptureMode = "metadata_only"
		}
		if row.DerivedCaptureMode == "" {
			row.DerivedCaptureMode = "db_import"
		}
		if row.DerivedSchemaVersion == "" {
			row.DerivedSchemaVersion = prd5SnapshotSchemaVersion
		}
		if row.DerivedSourceComponent == "" {
			row.DerivedSourceComponent = sourceComponentForTrace(row.Stage)
		}
	}
	row.PayloadCaptureReason = prd5PayloadCaptureReason
	row.SourceRef = sourceRef(sourceInstance, "request_trace_events", row.ID)
	if strict {
		hash, err := normalizeStrictSourceEventHash(sourceEventHash, row)
		if err != nil {
			return snapshotTraceEvent{}, err
		}
		row.SourceEventHash = hash
	} else if sourceEventHash.Valid && sourceEventHash.String != "" {
		row.SourceEventHash = sourceEventHash.String
	} else {
		row.SourceEventHash = stableHash(row)
	}
	return row, nil
}

func sanitizedMetadata(metadataText string) (map[string]interface{}, error) {
	if strings.TrimSpace(metadataText) == "" {
		return map[string]interface{}{}, nil
	}
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(metadataText), &raw); err != nil {
		return nil, err
	}
	return traceutil.SanitizeMetadata(raw), nil
}

var (
	strictSourceInstancePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,159}$`)
	strictOpaquePattern         = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*$`)
	strictCodePattern           = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:=,+-]*$`)
	strictCodeListPattern       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:=,+-]*$`)
	strictEventHashPattern      = regexp.MustCompile(`^[a-f0-9]{64}$`)
	strictManifestHashPattern   = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	strictExchangeIDPattern     = regexp.MustCompile(`^uxchg_[a-f0-9]{32}$`)
	strictIntegerPattern        = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
	strictDecimalPattern        = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`)
	strictUnitConfidencePattern = regexp.MustCompile(`^(0\.[0-9]{3}|1\.000)$`)
	strictIPv4Pattern           = regexp.MustCompile(`(?:^|[^0-9])(?:[0-9]{1,3}\.){3}[0-9]{1,3}(?:$|[^0-9])`)
	strictEmbeddedPhonePattern  = regexp.MustCompile(`(?:^|[^0-9])1[3-9][0-9]{9}(?:$|[^0-9])`)
	strictGenericPhonePattern   = regexp.MustCompile(`(?:^|[^A-Za-z0-9])(?:\+[0-9]{1,3}[ .-]?)?(?:\([0-9]{2,4}\)[ .-]?|[0-9]{2,4}[ .-])[0-9]{3,4}[ .-][0-9]{4}(?:$|[^A-Za-z0-9])`)
	strictCredentialPattern     = regexp.MustCompile(`(?i)(?:\bbearer\s+|\bbasic\s+|\b(?:api[_-]?key|authorization|password|secret|access[_-]?token|refresh[_-]?token)\s*[:=]|-----BEGIN [A-Z ]+PRIVATE KEY-----|\bsk-[A-Za-z0-9_-]{12,})`)
)

func validateStrictPayloadContract(table string, payloadRef sql.NullString, mode string) (*string, string, error) {
	path := "request_trace_events"
	if table == "call_logs" {
		path = "call_logs"
	}
	if payloadRef.Valid && payloadRef.String != "" {
		return nil, "", fmt.Errorf("strict_payload_contract: %s.payload_ref nonempty", path)
	}
	switch mode {
	case "none", "metadata_only", "hash_only":
		return nil, mode, nil
	default:
		return nil, "", fmt.Errorf("strict_payload_contract: %s.payload_capture_mode unsupported", path)
	}
}

func normalizeStrictSourceEventHash(source sql.NullString, fallback any) (string, error) {
	if source.Valid && source.String != "" {
		if !strictEventHashPattern.MatchString(source.String) {
			return "", errors.New("strict_event_hash_invalid: source")
		}
		return source.String, nil
	}
	hash := stableHash(fallback)
	if !strictEventHashPattern.MatchString(hash) {
		return "", errors.New("strict_event_hash_invalid: fallback")
	}
	return hash, nil
}

func blockedStringClass(value string) string {
	if !utf8.ValidString(value) {
		return "invalid_utf8"
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return "control"
		}
	}
	lower := strings.ToLower(value)
	if strings.Contains(value, "@") {
		return "email"
	}
	if containsIPLiteral(value) {
		return "ip"
	}
	if strings.Contains(lower, "://") || strings.Contains(lower, "www.") {
		return "url"
	}
	if strictCredentialPattern.MatchString(value) {
		return "credential"
	}
	for _, marker := range []string{
		"bearer ", "basic ", "sk-", "akia", "-----begin", "token=", "token:",
		"secret=", "secret:", "password=", "password:", "api_key=", "api-key=", "apikey=",
		"ghp_", "github_pat_", "xoxb-", "xoxp-", "xoxa-", "xoxr-", "aiza", "client_secret",
		"private_key", "access_key_id", "dsn=",
	} {
		if strings.Contains(lower, marker) {
			return "credential"
		}
	}
	for _, marker := range []string{"customer", "tenant", "account_id", "account-id"} {
		if strings.Contains(lower, marker) {
			return "customer_identifier"
		}
	}
	if looksPhoneLike(value) || strictEmbeddedPhonePattern.MatchString(value) || strictGenericPhonePattern.MatchString(value) {
		return "phone"
	}
	return ""
}

func containsIPLiteral(value string) bool {
	if net.ParseIP(value) != nil || strictIPv4Pattern.MatchString(value) {
		return true
	}
	start := -1
	for idx, r := range value {
		isHexOrColon := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') || r == ':'
		if isHexOrColon {
			if start < 0 {
				start = idx
			}
			continue
		}
		if start >= 0 {
			candidate := value[start:idx]
			if strings.Count(candidate, ":") >= 2 && net.ParseIP(candidate) != nil {
				return true
			}
			start = -1
		}
	}
	if start >= 0 {
		candidate := value[start:]
		return strings.Count(candidate, ":") >= 2 && net.ParseIP(candidate) != nil
	}
	return false
}

func looksPhoneLike(value string) bool {
	digits := 0
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r == '+', r == '-', r == '(', r == ')', r == ' ', r == '.':
		default:
			return false
		}
	}
	return digits >= 7
}

func validateStrictOpaqueValue(value string, maxBytes int, allowEmpty bool) error {
	if value == "" {
		if allowEmpty {
			return nil
		}
		return errors.New("empty")
	}
	if len(value) > maxBytes || blockedStringClass(value) != "" || !strictOpaquePattern.MatchString(value) {
		return errors.New("unsafe")
	}
	return nil
}

func validateStrictCodeValue(value string, maxBytes int, allowEmpty bool) error {
	if value == "" {
		if allowEmpty {
			return nil
		}
		return errors.New("empty")
	}
	if len(value) > maxBytes || blockedStringClass(value) != "" || !strictCodePattern.MatchString(value) {
		return errors.New("unsafe")
	}
	if strings.HasPrefix(value, "/") || strings.Contains(value, "//") || strings.Contains(value, "..") || strings.Contains(value, "\\") {
		return errors.New("unsafe")
	}
	return nil
}

func validateStrictCodeListValue(value string, maxBytes int) error {
	if value == "" || len(value) > maxBytes || blockedStringClass(value) != "" || !strictCodeListPattern.MatchString(value) {
		return errors.New("unsafe")
	}
	if strings.Contains(value, "//") || strings.Contains(value, "..") || strings.Contains(value, "\\") {
		return errors.New("unsafe")
	}
	return nil
}

type strictMetadataKind int

const (
	metadataCode strictMetadataKind = iota
	metadataCodeList
	metadataBool
	metadataFalseBool
	metadataInteger
	metadataDecimal
	metadataConfidence
	metadataPercent
	metadataCurrency
	metadataMoneySchema
	metadataAmountScale
	metadataPayloadMode
	metadataPayloadReason
)

type strictMetadataRule struct {
	kind strictMetadataKind
}

func buildStrictMetadataRules() map[string]strictMetadataRule {
	rules := map[string]strictMetadataRule{}
	add := func(kind strictMetadataKind, keys ...string) {
		for _, key := range keys {
			rules[key] = strictMetadataRule{kind: kind}
		}
	}
	add(metadataCode,
		"billing_status", "cache_scope", "cache_usage_schema", "cache_usage_status", "pricing_version",
		"effective_config_version", "error_code", "failure_kind", "mapped_model", "routing_config_version",
		"routing_policy_version", "routing_snapshot_version", "routing_snapshot_source", "routing_selection_reason",
		"retry_decision", "stream_policy_version", "usage_source", "limit_policy_set_version", "limit_policy_result",
		"limit_policy_reason", "guardrail_policy_set_version", "guardrail_policy_ref", "guardrail_policy_version",
		"guardrail_detector_ref", "guardrail_detector_version", "guardrail_mode", "guardrail_action",
		"guardrail_outcome", "guardrail_failure_mode", "guardrail_decision",
	)
	add(metadataCodeList, "routing_exclusion_reasons", "limit_policy_chain", "guardrail_categories", "guardrail_finding_fingerprints")
	add(metadataBool,
		"cache_usage_authoritative", "cache_hit", "is_stream", "routing_snapshot_degraded", "routing_requires_probe",
		"retryable", "settle_cancelled", "stream", "guardrail_mandatory", "guardrail_transformed", "guardrail_degraded",
	)
	add(metadataFalseBool, "payload_ref_present", "raw_payload_stored")
	add(metadataInteger,
		"uncached_prompt_tokens", "cache_read_tokens", "cache_creation_tokens", "cache_read_cost_micros",
		"cache_creation_cost_micros", "prepaid_topup_cents", "prepaid_topup_micros", "auth_failure_cooldown_seconds",
		"circuit_breaker_recover_seconds", "circuit_breaker_threshold", "charged_cost_cents", "actual_cost_micros",
		"charged_cost_micros", "projected_charge_micros", "reserved_amount_micros", "completion_tokens", "cost_micros",
		"estimated_tokens", "freeze_amount_cents", "freeze_amount_micros", "max_retry_count", "prompt_tokens",
		"rate_limit_cooldown_seconds", "route_count", "route_index", "routing_candidate_count", "routing_excluded_count",
		"routing_priority", "routing_weight", "stream_max_sse_line_bytes", "total_tokens", "ttft_ms",
		"provider_key_failure_threshold", "limit_policy_decision_count", "limit_policy_enforce_count",
		"limit_policy_shadow_count", "guardrail_match_count",
	)
	add(metadataDecimal, "cache_read_cost_cents", "cache_creation_cost_cents", "cost_cents", "cache_read_ratio", "cache_creation_ratio")
	add(metadataConfidence, "usage_confidence")
	add(metadataPercent, "circuit_breaker_half_open_percent")
	add(metadataCurrency, "currency")
	add(metadataMoneySchema, "money_schema")
	add(metadataAmountScale, "amount_scale")
	add(metadataPayloadMode, "payload_capture_mode")
	add(metadataPayloadReason, "payload_policy_reason")
	return rules
}

var strictMetadataRules = buildStrictMetadataRules()

func strictMetadata(metadataText string) (map[string]interface{}, error) {
	if metadataText == "" {
		return map[string]interface{}{}, nil
	}
	if strings.TrimSpace(metadataText) != metadataText {
		return nil, errors.New("strict_value_policy: request_trace_events.metadata class=noncanonical_json")
	}
	if err := rejectDuplicateJSONKeys([]byte(metadataText)); errors.Is(err, errDuplicateJSONKey) {
		return nil, errors.New("strict_value_policy: request_trace_events.metadata class=duplicate_key")
	}
	decoder := json.NewDecoder(strings.NewReader(metadataText))
	decoder.UseNumber()
	var raw map[string]interface{}
	if err := decoder.Decode(&raw); err != nil || raw == nil {
		return nil, errors.New("strict_value_policy: request_trace_events.metadata class=invalid_object")
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("strict_value_policy: request_trace_events.metadata class=trailing_data")
	}
	for key, value := range raw {
		rule, ok := strictMetadataRules[key]
		if !ok {
			return nil, errors.New("strict_value_policy: request_trace_events.metadata class=unknown_key")
		}
		if err := validateStrictMetadataValue(rule, value); err != nil {
			return nil, errors.New("strict_value_policy: request_trace_events.metadata class=invalid_value")
		}
	}
	if err := requireStrictMetadataSanitizerEquality(raw, traceutil.SanitizeMetadata); err != nil {
		return nil, err
	}
	return raw, nil
}

func requireStrictMetadataSanitizerEquality(raw map[string]interface{}, sanitize func(map[string]interface{}) map[string]interface{}) error {
	comparable := make(map[string]interface{}, len(raw))
	for key, value := range raw {
		number, ok := value.(json.Number)
		if !ok {
			comparable[key] = value
			continue
		}
		if strictIntegerPattern.MatchString(number.String()) {
			parsed, err := strconv.ParseInt(number.String(), 10, 64)
			if err != nil {
				return errors.New("strict_value_policy: request_trace_events.metadata class=sanitizer_mismatch")
			}
			comparable[key] = parsed
			continue
		}
		parsed, err := strconv.ParseFloat(number.String(), 64)
		if err != nil || math.IsInf(parsed, 0) || math.IsNaN(parsed) {
			return errors.New("strict_value_policy: request_trace_events.metadata class=sanitizer_mismatch")
		}
		comparable[key] = parsed
	}
	if sanitized := sanitize(comparable); !reflect.DeepEqual(sanitized, comparable) {
		return errors.New("strict_value_policy: request_trace_events.metadata class=sanitizer_mismatch")
	}
	return nil
}

func validateStrictMetadataValue(rule strictMetadataRule, value interface{}) error {
	switch rule.kind {
	case metadataCode:
		text, ok := value.(string)
		if !ok {
			return errors.New("type")
		}
		return validateStrictCodeValue(text, 256, false)
	case metadataCodeList:
		text, ok := value.(string)
		if !ok {
			return errors.New("type")
		}
		return validateStrictCodeListValue(text, 256)
	case metadataBool:
		_, ok := value.(bool)
		if !ok {
			return errors.New("type")
		}
		return nil
	case metadataFalseBool:
		boolean, ok := value.(bool)
		if !ok || boolean {
			return errors.New("blocked")
		}
		return nil
	case metadataInteger, metadataAmountScale:
		number, ok := value.(json.Number)
		if !ok || !strictIntegerPattern.MatchString(number.String()) {
			return errors.New("type")
		}
		parsed, err := strconv.ParseUint(number.String(), 10, 63)
		if err != nil {
			return errors.New("range")
		}
		if receiverBlocksNumericMetadata(float64(parsed)) {
			return errors.New("numeric_identifier_like_value")
		}
		if rule.kind == metadataAmountScale && parsed != 1_000_000 {
			return errors.New("exact")
		}
		return nil
	case metadataDecimal, metadataConfidence, metadataPercent:
		number, ok := value.(json.Number)
		if !ok || len(number.String()) > 64 || !strictDecimalPattern.MatchString(number.String()) {
			return errors.New("type")
		}
		parsed, err := strconv.ParseFloat(number.String(), 64)
		if err != nil || math.IsInf(parsed, 0) || math.IsNaN(parsed) || parsed < 0 {
			return errors.New("range")
		}
		if receiverBlocksNumericMetadata(parsed) {
			return errors.New("numeric_identifier_like_value")
		}
		if rule.kind == metadataConfidence && parsed > 1 {
			return errors.New("range")
		}
		if rule.kind == metadataPercent && parsed > 100 {
			return errors.New("range")
		}
		return nil
	case metadataCurrency:
		text, ok := value.(string)
		if !ok || text != "CNY" {
			return errors.New("exact")
		}
		return nil
	case metadataMoneySchema:
		text, ok := value.(string)
		if !ok || text != "micro-cny.v1" {
			return errors.New("exact")
		}
		return nil
	case metadataPayloadMode:
		text, ok := value.(string)
		if !ok || (text != "none" && text != "metadata_only" && text != "hash_only") {
			return errors.New("exact")
		}
		return nil
	case metadataPayloadReason:
		text, ok := value.(string)
		if !ok || !oneOf(text, "payload.none", "payload.metadata_only", "payload.hash_only") {
			return errors.New("exact")
		}
		return nil
	default:
		return errors.New("rule")
	}
}

func receiverBlocksNumericMetadata(value float64) bool {
	absolute := math.Abs(value)
	return math.Trunc(absolute) == absolute && absolute >= 1_000_000_000 && absolute < 1_000_000_000_000_000
}

func validateStrictSnapshotValuePolicy(snapshot snapshotFile) error {
	if snapshot.SchemaVersion != prd5SnapshotSchemaVersion || snapshot.Source != "unirouter" || snapshot.Safety.SelectionMode != "approved_request_id_manifest" {
		return errors.New("strict_value_policy: snapshot.constant")
	}
	if !strictExchangeIDPattern.MatchString(snapshot.ExchangeID) || !strictManifestHashPattern.MatchString(snapshot.ApprovalManifestSHA256) {
		return errors.New("strict_value_policy: snapshot.identity")
	}
	if validateStrictTimestamp(snapshot.GeneratedAt) != nil || validateStrictSourceInstance(snapshot.SourceInstance) != nil {
		return errors.New("strict_value_policy: snapshot.source_or_time")
	}
	if snapshot.Safety.RawPromptsIncluded || snapshot.Safety.RawResponsesIncluded || snapshot.Safety.APIKeysIncluded {
		return errors.New("strict_value_policy: snapshot.safety")
	}
	expectedSafety := newStrictSnapshotFile("s", "approved_request_id_manifest", "uxchg_00000000000000000000000000000000", "sha256:"+strings.Repeat("0", 64), nil, nil, "2026-01-01T00:00:00Z", nil, nil, nil).Safety
	if snapshot.Safety.IPHandling != expectedSafety.IPHandling || snapshot.Safety.ErrorHandling != expectedSafety.ErrorHandling || snapshot.Safety.MetadataHandling != expectedSafety.MetadataHandling {
		return errors.New("strict_value_policy: snapshot.safety_text")
	}
	for _, requestID := range snapshot.RequestIDs {
		if validateStrictOpaqueValue(requestID, 64, false) != nil {
			return errors.New("strict_value_policy: snapshot.request_ids")
		}
	}
	for _, scenario := range snapshot.Scenarios {
		if validateStrictCodeValue(scenario.Label, 120, false) != nil || validateStrictOpaqueValue(scenario.RequestID, 64, false) != nil {
			return errors.New("strict_value_policy: snapshot.scenarios")
		}
	}
	if err := validateStrictNotes(snapshot.Notes); err != nil {
		return err
	}
	for _, gap := range snapshot.CaptureGaps {
		if validateStrictCaptureGap(gap) != nil {
			return errors.New("strict_value_policy: capture_gaps")
		}
	}
	for _, row := range snapshot.CallLogs {
		if err := validateStrictCallLogValues(row); err != nil {
			return err
		}
	}
	for _, row := range snapshot.TraceEvents {
		if err := validateStrictTraceEventValues(row); err != nil {
			return err
		}
	}
	return nil
}

func validateStrictCaptureGap(gap snapshotCaptureGap) error {
	if validateStrictOpaqueValue(gap.RequestID, 64, false) != nil || gap.Gap != strictGapClass || !gap.Generated {
		return errors.New("invalid gap")
	}
	switch gap.Source {
	case "call_logs":
		if gap.Reason != strictCallGapReason || !reflect.DeepEqual(gap.CandidateGaps, []string{strictGapClass}) {
			return errors.New("invalid call gap")
		}
	case "request_trace_events":
		withoutCall := gap.Reason == strictTraceGapNoCall && reflect.DeepEqual(gap.CandidateGaps, []string{strictGapClass})
		withCall := gap.Reason == strictTraceGapWithCall && reflect.DeepEqual(gap.CandidateGaps, []string{"sampled_out", "retention_expired", "not_instrumented"})
		if !withoutCall && !withCall {
			return errors.New("invalid trace gap")
		}
	default:
		return errors.New("invalid gap source")
	}
	return nil
}

func validateStrictTimestamp(value string) error {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.UTC().Format(time.RFC3339Nano) != value {
		return errors.New("invalid timestamp")
	}
	return nil
}

func validateStrictNotes(notes map[string]interface{}) error {
	expected := map[string]string{
		"request_id_join": "request_id is the intended join key between call_logs and request_trace_events",
		"missing_rows":    "missing call logs or trace events are represented as capture gaps and should not be fabricated by importers",
		"payload_policy":  "UniRouter raw prompt/response capture remains disabled for this snapshot path",
		"money_contract":  "cost_micros is canonical non-negative integer micro-CNY text; currency=CNY, amount_scale=1000000, money_schema=micro-cny.v1; cost_cents is absent",
	}
	if len(notes) != len(expected) {
		return errors.New("strict_value_policy: snapshot.notes")
	}
	for key, exact := range expected {
		value, ok := notes[key].(string)
		if !ok || value != exact {
			return errors.New("strict_value_policy: snapshot.notes")
		}
	}
	return nil
}

func validateStrictCallLogValues(row snapshotCallLog) error {
	if row.ID <= 0 || row.OrgID <= 0 || optionalPositive(row.ProjectID) != nil || optionalPositive(row.APIKeyID) != nil || optionalPositive(row.OwnerUserID) != nil {
		return errors.New("strict_value_policy: call_logs.numeric_identity")
	}
	if validateStrictOpaqueValue(row.SourceRef, 256, false) != nil || validateStrictOpaqueValue(row.RequestID, 64, false) != nil || !strictEventHashPattern.MatchString(row.SourceEventHash) {
		return errors.New("strict_value_policy: call_logs.identity")
	}
	if !oneOf(row.KeyType, "unknown", "shared", "personal", "service") {
		return errors.New("strict_value_policy: call_logs.key_type")
	}
	for _, check := range []struct {
		value string
		max   int
	}{
		{row.Model, 100}, {row.RequestedModel, 100}, {row.Provider, 50}, {row.Channel, 255},
		{row.UsageSource, 20}, {row.DerivedCaptureMode, 40}, {row.DerivedSchemaVersion, 40}, {row.DerivedSourceComponent, 40},
	} {
		if validateStrictCodeValue(check.value, check.max, false) != nil {
			return errors.New("strict_value_policy: call_logs.code")
		}
	}
	if validateStrictUnitDecimal(row.UsageConfidence) != nil {
		return errors.New("strict_value_policy: call_logs.usage_confidence")
	}
	if validateCostMicros(row.CostMicros) != nil || row.Currency != "CNY" || row.AmountScale != 1_000_000 || row.MoneySchema != "micro-cny.v1" {
		return errors.New("strict_value_policy: call_logs.money")
	}
	if row.PromptTokens < 0 || row.CompletionTokens < 0 || row.TotalTokens < 0 || row.EstimatedTokens < 0 || row.LatencyMS < 0 || row.TTFTMS < 0 || !validHTTPStatus(row.StatusCode) || !validOptionalHTTPStatus(row.UpstreamStatusCode) {
		return errors.New("strict_value_policy: call_logs.nonnegative")
	}
	if row.UpstreamErrorPresent {
		return errors.New("strict_source_blocked: call_logs.raw_error_presence")
	}
	if err := validateStrictIPProof(row.IPAddressWasPresent, row.IPAddressRedacted); err != nil {
		return err
	}
	if validateStrictTimestamp(row.CreatedAt) != nil {
		return errors.New("strict_value_policy: call_logs.created_at")
	}
	for _, check := range []struct {
		value *string
		max   int
	}{
		{row.TraceID, 64}, {row.SessionID, 128}, {row.UpstreamRequestID, 128}, {row.EvidenceRef, 255},
		{row.ActorType, 40}, {row.ActorRef, 128}, {row.OnBehalfOf, 128},
	} {
		if validateStrictOptionalOpaque(check.value, check.max) != nil {
			return errors.New("strict_value_policy: call_logs.reference")
		}
	}
	if row.PayloadRef != nil || !oneOf(row.PayloadCaptureMode, "none", "metadata_only", "hash_only") || row.PayloadCaptureReason != prd5PayloadCaptureReason {
		return errors.New("strict_payload_contract: call_logs.final")
	}
	return nil
}

func validateStrictTraceEventValues(row snapshotTraceEvent) error {
	if row.ID <= 0 || row.OrgID <= 0 || optionalPositive(row.ProjectID) != nil || optionalPositive(row.APIKeyID) != nil || optionalPositive(row.OwnerUserID) != nil {
		return errors.New("strict_value_policy: request_trace_events.numeric_identity")
	}
	if validateStrictOpaqueValue(row.SourceRef, 256, false) != nil || validateStrictOpaqueValue(row.RequestID, 64, false) != nil || !strictEventHashPattern.MatchString(row.SourceEventHash) {
		return errors.New("strict_value_policy: request_trace_events.identity")
	}
	if !oneOf(row.KeyType, "unknown", "shared", "personal", "service") || !oneOf(row.Severity, "info", "warning", "error") {
		return errors.New("strict_value_policy: request_trace_events.enum")
	}
	for _, check := range []struct {
		value string
		max   int
	}{
		{row.EventType, 80}, {row.Stage, 40}, {row.DerivedCaptureMode, 40}, {row.DerivedSchemaVersion, 40}, {row.DerivedSourceComponent, 40},
	} {
		if validateStrictCodeValue(check.value, check.max, false) != nil {
			return errors.New("strict_value_policy: request_trace_events.code")
		}
	}
	for _, check := range []struct {
		value string
		max   int
	}{
		{row.Model, 100}, {row.RequestedModel, 100}, {row.Provider, 50}, {row.Channel, 255},
	} {
		if validateStrictCodeValue(check.value, check.max, true) != nil {
			return errors.New("strict_value_policy: request_trace_events.optional_code")
		}
	}
	if row.LatencyMS < 0 || !validOptionalHTTPStatus(row.StatusCode) || validateStrictTimestamp(row.CreatedAt) != nil {
		return errors.New("strict_value_policy: request_trace_events.time")
	}
	for _, check := range []struct {
		value *string
		max   int
	}{
		{row.TraceID, 64}, {row.SpanID, 64}, {row.ParentSpanID, 64}, {row.CaptureGap, 80},
		{row.EvidenceRef, 255}, {row.UpstreamRequestID, 128}, {row.DecisionKind, 40},
		{row.DecisionResult, 40}, {row.DecisionReason, 120}, {row.ActorType, 40},
		{row.ActorRef, 128}, {row.OnBehalfOf, 128}, {row.SessionID, 128},
	} {
		if validateStrictOptionalOpaque(check.value, check.max) != nil {
			return errors.New("strict_value_policy: request_trace_events.reference")
		}
	}
	if row.PayloadRef != nil || !oneOf(row.PayloadCaptureMode, "none", "metadata_only", "hash_only") || row.PayloadCaptureReason != prd5PayloadCaptureReason {
		return errors.New("strict_payload_contract: request_trace_events.final")
	}
	if len(row.MetadataKeys) != len(row.Metadata) {
		return errors.New("strict_value_policy: request_trace_events.metadata_keys")
	}
	expectedKeys := sortedKeys(row.Metadata)
	for idx := range expectedKeys {
		if row.MetadataKeys[idx] != expectedKeys[idx] {
			return errors.New("strict_value_policy: request_trace_events.metadata_keys")
		}
		rule, ok := strictMetadataRules[expectedKeys[idx]]
		if !ok || validateStrictMetadataValue(rule, row.Metadata[expectedKeys[idx]]) != nil {
			return errors.New("strict_value_policy: request_trace_events.metadata")
		}
	}
	return nil
}

func validateStrictOptionalOpaque(value *string, maxBytes int) error {
	if value == nil {
		return nil
	}
	return validateStrictOpaqueValue(*value, maxBytes, false)
}

func validateStrictUnitDecimal(value string) error {
	if !strictUnitConfidencePattern.MatchString(value) {
		return errors.New("invalid decimal")
	}
	return nil
}

func optionalPositive(value *int64) error {
	if value != nil && *value <= 0 {
		return errors.New("nonpositive")
	}
	return nil
}

func validHTTPStatus(value int64) bool {
	return value >= 100 && value <= 599
}

func validOptionalHTTPStatus(value *int64) bool {
	return value == nil || validHTTPStatus(*value)
}

func validateStrictIPProof(ipAddressWasPresent, ipAddressRedacted bool) error {
	if ipAddressWasPresent != ipAddressRedacted {
		return errors.New("strict_ip_proof_mismatch")
	}
	return nil
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func reconcileStrictSnapshot(binding strictSnapshotBinding, aggregates []strictSourceAggregate, snapshot snapshotFile) error {
	requestIDs, scenarios := binding.RequestIDs, binding.Scenarios
	if snapshot.SourceInstance != binding.SourceInstance || snapshot.Safety.SelectionMode != binding.SelectionMode || snapshot.ExchangeID != binding.ExchangeID || snapshot.ApprovalManifestSHA256 != binding.ApprovalManifestSHA256 {
		return errors.New("strict_reconciliation_failed: manifest_binding")
	}
	if len(snapshot.RequestIDs) != len(requestIDs) || len(snapshot.Scenarios) != len(scenarios) || len(aggregates) != len(requestIDs) {
		return errors.New("strict_reconciliation_failed: top_level_set")
	}
	selected := make(map[string]int, len(requestIDs))
	for idx, requestID := range requestIDs {
		if snapshot.RequestIDs[idx] != requestID {
			return errors.New("strict_reconciliation_failed: request_set")
		}
		selected[requestID] = idx
	}
	for idx := range scenarios {
		if snapshot.Scenarios[idx] != scenarios[idx] {
			return errors.New("strict_reconciliation_failed: scenario_set")
		}
	}

	callCounts := make([]int64, len(requestIDs))
	traceCounts := make([]int64, len(requestIDs))
	ipAddressPresentCounts := make([]int64, len(requestIDs))
	ipAddressRedactedCounts := make([]int64, len(requestIDs))
	callIDs := map[int64]bool{}
	traceIDs := map[int64]bool{}
	for _, row := range snapshot.CallLogs {
		idx, ok := selected[row.RequestID]
		if !ok || row.ID <= 0 || callIDs[row.ID] || row.SourceRef != sourceRef(binding.SourceInstance, "call_logs", row.ID) || row.OrgID != aggregates[idx].OrgID {
			return errors.New("strict_reconciliation_failed: call_logs")
		}
		callIDs[row.ID] = true
		callCounts[idx]++
		if validateStrictIPProof(row.IPAddressWasPresent, row.IPAddressRedacted) != nil {
			return errors.New("strict_reconciliation_failed: ip_proof")
		}
		if row.IPAddressWasPresent {
			ipAddressPresentCounts[idx]++
		}
		if row.IPAddressRedacted {
			ipAddressRedactedCounts[idx]++
		}
	}
	for _, row := range snapshot.TraceEvents {
		idx, ok := selected[row.RequestID]
		if !ok || row.ID <= 0 || traceIDs[row.ID] || row.SourceRef != sourceRef(binding.SourceInstance, "request_trace_events", row.ID) || row.OrgID != aggregates[idx].OrgID {
			return errors.New("strict_reconciliation_failed: request_trace_events")
		}
		traceIDs[row.ID] = true
		traceCounts[idx]++
	}
	for idx, aggregate := range aggregates {
		if callCounts[idx] != aggregate.CallLogs || traceCounts[idx] != aggregate.TraceEvents {
			return errors.New("strict_reconciliation_failed: aggregate_detail_count")
		}
		if ipAddressPresentCounts[idx] != aggregate.IPAddressPresentRows || ipAddressRedactedCounts[idx] != aggregate.IPAddressPresentRows {
			return errors.New("strict_reconciliation_failed: ip_presence_count")
		}
	}

	expectedGaps := buildCaptureGaps(requestIDs, snapshot.CallLogs, snapshot.TraceEvents)
	if len(snapshot.CaptureGaps) != len(expectedGaps) {
		return errors.New("strict_reconciliation_failed: capture_gap_count")
	}
	for idx := range expectedGaps {
		actual, expected := snapshot.CaptureGaps[idx], expectedGaps[idx]
		if actual.RequestID != expected.RequestID || actual.Source != expected.Source || actual.Gap != expected.Gap || actual.Reason != expected.Reason || actual.Generated != expected.Generated || strings.Join(actual.CandidateGaps, "\x00") != strings.Join(expected.CandidateGaps, "\x00") {
			return errors.New("strict_reconciliation_failed: capture_gap")
		}
	}
	if snapshot.Counts.RequestedRequestIDs != len(snapshot.RequestIDs) || snapshot.Counts.CallLogs != len(snapshot.CallLogs) || snapshot.Counts.TraceEvents != len(snapshot.TraceEvents) || snapshot.Counts.CaptureGaps != len(snapshot.CaptureGaps) {
		return errors.New("strict_reconciliation_failed: counts")
	}
	if len(snapshot.CallLogs)+len(snapshot.TraceEvents)+len(snapshot.CaptureGaps) > maxSnapshotRows {
		return errors.New("strict_reconciliation_failed: row_limit")
	}
	return nil
}

func buildCaptureGaps(requestIDs []string, callLogs []snapshotCallLog, traceEvents []snapshotTraceEvent) []snapshotCaptureGap {
	seenCallLog := make(map[string]bool, len(callLogs))
	for _, row := range callLogs {
		seenCallLog[row.RequestID] = true
	}
	seenTrace := make(map[string]bool, len(traceEvents))
	for _, row := range traceEvents {
		seenTrace[row.RequestID] = true
	}

	var gaps []snapshotCaptureGap
	for _, requestID := range requestIDs {
		if !seenCallLog[requestID] {
			gaps = append(gaps, snapshotCaptureGap{
				RequestID:     requestID,
				Source:        "call_logs",
				Gap:           "not_available_from_source",
				CandidateGaps: []string{"not_available_from_source"},
				Reason:        strictCallGapReason,
				Generated:     true,
			})
		}
		if !seenTrace[requestID] {
			candidates := []string{"not_available_from_source"}
			reason := strictTraceGapNoCall
			if seenCallLog[requestID] {
				candidates = []string{"sampled_out", "retention_expired", "not_instrumented"}
				reason = strictTraceGapWithCall
			}
			gaps = append(gaps, snapshotCaptureGap{
				RequestID:     requestID,
				Source:        "request_trace_events",
				Gap:           "not_available_from_source",
				CandidateGaps: candidates,
				Reason:        reason,
				Generated:     true,
			})
		}
	}
	return gaps
}

func writeSnapshot(outputPath string, overwrite bool, snapshot snapshotFile) error {
	var w io.Writer = os.Stdout
	var file *os.File
	if outputPath != "-" {
		parent := filepath.Dir(outputPath)
		if parent != "." {
			if err := os.MkdirAll(parent, 0o700); err != nil {
				return err
			}
		}
		flags := os.O_WRONLY | os.O_CREATE
		if overwrite {
			flags |= os.O_TRUNC
		} else {
			flags |= os.O_EXCL
		}
		f, err := os.OpenFile(outputPath, flags, 0o600)
		if err != nil {
			return err
		}
		defer f.Close()
		file = f
		w = f
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(snapshot); err != nil {
		if file != nil {
			_ = file.Close()
		}
		return err
	}
	return nil
}

func writeHandoffNote(notePath string, overwrite bool, snapshotOutputPath string, approvalManifestPath string, snapshot snapshotFile) error {
	parent := filepath.Dir(notePath)
	if parent != "." {
		if err := os.MkdirAll(parent, 0o700); err != nil {
			return err
		}
	}
	flags := os.O_WRONLY | os.O_CREATE
	if overwrite {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_EXCL
	}
	f, err := os.OpenFile(notePath, flags, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(formatHandoffNote(snapshotOutputPath, notePath, approvalManifestPath, overwrite, snapshot))
	return err
}

func formatHandoffNote(snapshotOutputPath string, notePath string, approvalManifestPath string, overwrite bool, snapshot snapshotFile) string {
	var b strings.Builder
	b.WriteString("# PRD5 X-Insight Snapshot Handoff Note\n\n")
	b.WriteString("Status: Generated\n")
	b.WriteString(fmt.Sprintf("Generated At: %s\n", snapshot.GeneratedAt))
	b.WriteString(fmt.Sprintf("Source: %s\n", snapshot.Source))
	b.WriteString(fmt.Sprintf("Source Instance: %s\n", snapshot.SourceInstance))
	b.WriteString(fmt.Sprintf("Schema Version: %s\n", snapshot.SchemaVersion))
	b.WriteString(fmt.Sprintf("Exchange ID: %s\n", snapshot.ExchangeID))
	if snapshot.ApprovalManifestSHA256 == "" {
		b.WriteString("Approval Manifest SHA-256: not applicable (direct explicit request IDs)\n")
	} else {
		b.WriteString(fmt.Sprintf("Approval Manifest SHA-256: %s\n", snapshot.ApprovalManifestSHA256))
	}
	if snapshotOutputPath == "-" {
		b.WriteString("Snapshot Output: stdout\n")
	} else {
		b.WriteString(fmt.Sprintf("Snapshot Output: %s\n", snapshotOutputPath))
	}
	b.WriteString(fmt.Sprintf("Selection Mode: %s\n", snapshot.Safety.SelectionMode))
	b.WriteString("\n## Counts\n\n")
	b.WriteString("| Field | Count |\n")
	b.WriteString("| --- | ---: |\n")
	b.WriteString(fmt.Sprintf("| requested_request_ids | %d |\n", snapshot.Counts.RequestedRequestIDs))
	b.WriteString(fmt.Sprintf("| call_logs | %d |\n", snapshot.Counts.CallLogs))
	b.WriteString(fmt.Sprintf("| request_trace_events | %d |\n", snapshot.Counts.TraceEvents))
	b.WriteString(fmt.Sprintf("| capture_gaps | %d |\n", snapshot.Counts.CaptureGaps))

	b.WriteString("\n## Capture Gaps\n\n")
	if len(snapshot.CaptureGaps) == 0 {
		b.WriteString("No generated capture gaps for the selected request IDs.\n")
	} else {
		b.WriteString("| Request ID | Source | Capture Gap | Candidate Gaps | Reason |\n")
		b.WriteString("| --- | --- | --- | --- | --- |\n")
		for _, gap := range snapshot.CaptureGaps {
			b.WriteString(fmt.Sprintf("| `%s` | `%s` | `%s` | `%s` | %s |\n",
				gap.RequestID,
				gap.Source,
				gap.Gap,
				strings.Join(gap.CandidateGaps, ", "),
				gap.Reason,
			))
		}
	}

	b.WriteString("\n## Selected Request IDs\n\n")
	for _, requestID := range snapshot.RequestIDs {
		b.WriteString(fmt.Sprintf("- `%s`\n", requestID))
	}
	if len(snapshot.RequestIDs) == 0 {
		b.WriteString("- none\n")
	}

	b.WriteString("\n## Scenarios\n\n")
	if len(snapshot.Scenarios) == 0 {
		b.WriteString("No scenario labels were provided.\n")
	} else {
		b.WriteString("| Label | Request ID |\n")
		b.WriteString("| --- | --- |\n")
		for _, scenario := range snapshot.Scenarios {
			b.WriteString(fmt.Sprintf("| `%s` | `%s` |\n", scenario.Label, scenario.RequestID))
		}
	}

	b.WriteString("\n## Safety Confirmation\n\n")
	b.WriteString(fmt.Sprintf("- Raw prompts included: `%t`\n", snapshot.Safety.RawPromptsIncluded))
	b.WriteString(fmt.Sprintf("- Raw responses included: `%t`\n", snapshot.Safety.RawResponsesIncluded))
	b.WriteString(fmt.Sprintf("- API keys included: `%t`\n", snapshot.Safety.APIKeysIncluded))
	b.WriteString(fmt.Sprintf("- IP handling: %s\n", snapshot.Safety.IPHandling))
	b.WriteString(fmt.Sprintf("- Error handling: %s\n", snapshot.Safety.ErrorHandling))
	b.WriteString(fmt.Sprintf("- Metadata handling: %s\n", snapshot.Safety.MetadataHandling))
	b.WriteString("- DSN, credentials, tokens, raw prompts, raw responses, raw IP addresses, and raw upstream error text are intentionally omitted from this note.\n")

	b.WriteString("\n## Command Shape Without DSN\n\n")
	b.WriteString("Set `UNIROUTE_PRD5_SNAPSHOT_DSN` out of band. Do not write the DSN to the shared folder.\n\n")
	b.WriteString("```bash\n")
	b.WriteString("UNIROUTE_PRD5_SNAPSHOT_DSN=<read-only postgres dsn> \\\n")
	b.WriteString("go run scripts/prd5_xinsight_snapshot.go \\\n")
	b.WriteString(fmt.Sprintf("  --source-instance %s \\\n", shellQuote(snapshot.SourceInstance)))
	if snapshot.Safety.SelectionMode == "approved_request_id_manifest" && approvalManifestPath != "" {
		b.WriteString(fmt.Sprintf("  --approval-manifest %s \\\n", shellQuote(approvalManifestPath)))
	} else {
		for _, requestID := range snapshot.RequestIDs {
			b.WriteString(fmt.Sprintf("  --request-id %s \\\n", shellQuote(requestID)))
		}
		for _, scenario := range snapshot.Scenarios {
			b.WriteString(fmt.Sprintf("  --scenario %s \\\n", shellQuote(scenario.Label+"="+scenario.RequestID)))
		}
	}
	if snapshotOutputPath != "" {
		b.WriteString(fmt.Sprintf("  --output %s \\\n", shellQuote(snapshotOutputPath)))
	}
	b.WriteString(fmt.Sprintf("  --handoff-note %s", shellQuote(notePath)))
	if overwrite {
		b.WriteString(" \\\n  --overwrite")
	}
	b.WriteString("\n```\n")

	b.WriteString("\n## X-Insight Gate A Use\n\n")
	b.WriteString("Re-delivering this unchanged snapshot keeps the embedded exchange ID. Regenerating the snapshot creates a new exchange ID, even if the selected request IDs are unchanged. X-Insight computes the snapshot artifact hash after receipt and reconciles on `(exchange_id, artifact.sha256)`.\n\n")
	b.WriteString("X-Insight should run discovery against the snapshot first, compare the report with the PRD5 snapshot contract, and treat missing `call_logs` or `request_trace_events` joins as capture gaps rather than inferred data.\n")
	return b.String()
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func normalizedRequestIDs(flags repeatedFlag, csv string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		out = append(out, value)
	}
	for _, value := range flags {
		add(value)
	}
	for _, value := range strings.Split(csv, ",") {
		add(value)
	}
	sort.Strings(out)
	return out
}

func flagWasSet(fs *flag.FlagSet, name string) bool {
	wasSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			wasSet = true
		}
	})
	return wasSet
}

func loadApprovalManifest(path string) (approvalManifest, string, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return approvalManifest{}, "", errors.New("strict_manifest_invalid: unreadable")
	}

	if err := rejectDuplicateJSONKeys(payload); errors.Is(err, errDuplicateJSONKey) {
		return approvalManifest{}, "", errors.New("strict_manifest_invalid: duplicate_key")
	}

	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var manifest approvalManifest
	if err := decoder.Decode(&manifest); err != nil {
		return approvalManifest{}, "", errors.New("strict_manifest_invalid: malformed_json")
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return approvalManifest{}, "", errors.New("approval manifest must contain exactly one JSON object")
		}
		return approvalManifest{}, "", errors.New("strict_manifest_invalid: trailing_data")
	}
	if manifest.SchemaVersion != prd5ApprovalManifestSchemaVersion {
		return approvalManifest{}, "", errors.New("strict_manifest_invalid: schema_version")
	}
	return manifest, sha256Digest(payload), nil
}

func rejectDuplicateJSONKeys(payload []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := consumeJSONValueWithoutDuplicateKeys(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}

func consumeJSONValueWithoutDuplicateKeys(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("non-string object key")
			}
			if _, exists := seen[key]; exists {
				return errDuplicateJSONKey
			}
			seen[key] = struct{}{}
			if err := consumeJSONValueWithoutDuplicateKeys(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return errors.New("unterminated object")
		}
		return nil
	case '[':
		for decoder.More() {
			if err := consumeJSONValueWithoutDuplicateKeys(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return errors.New("unterminated array")
		}
		return nil
	default:
		return errors.New("unexpected delimiter")
	}
}

func approvedManifestSelection(manifest approvalManifest) ([]string, []snapshotScenario, error) {
	if len(manifest.Requests) < 1 || len(manifest.Requests) > maxApprovedRequestIDs {
		return nil, nil, fmt.Errorf("strict_request_limit: count=%d limit=%d", len(manifest.Requests), maxApprovedRequestIDs)
	}
	requestIDs := make([]string, 0, len(manifest.Requests))
	scenarios := make([]snapshotScenario, 0, len(manifest.Requests))
	seenRequestIDs := map[string]bool{}
	seenLabels := map[string]bool{}
	for idx, row := range manifest.Requests {
		if row.ApprovalStatus != "approved" {
			return nil, nil, fmt.Errorf("strict_manifest_invalid: row_index=%d field=approval_status class=not_approved", idx+1)
		}
		if !row.Required {
			return nil, nil, fmt.Errorf("strict_manifest_invalid: row_index=%d field=required class=false", idx+1)
		}
		if row.Notes != "" {
			return nil, nil, fmt.Errorf("strict_manifest_invalid: row_index=%d field=notes class=nonempty", idx+1)
		}
		if placeholderIdentifier(row.RequestID) || validateStrictOpaqueValue(row.RequestID, 64, false) != nil {
			return nil, nil, fmt.Errorf("strict_manifest_invalid: row_index=%d field=request_id class=unsafe", idx+1)
		}
		if placeholderIdentifier(row.ScenarioLabel) || validateStrictCodeValue(row.ScenarioLabel, 120, false) != nil {
			return nil, nil, fmt.Errorf("strict_manifest_invalid: row_index=%d field=scenario_label class=unsafe", idx+1)
		}
		if seenRequestIDs[row.RequestID] {
			return nil, nil, fmt.Errorf("strict_manifest_invalid: row_index=%d field=request_id class=duplicate", idx+1)
		}
		if seenLabels[row.ScenarioLabel] {
			return nil, nil, fmt.Errorf("strict_manifest_invalid: row_index=%d field=scenario_label class=duplicate", idx+1)
		}
		seenRequestIDs[row.RequestID] = true
		seenLabels[row.ScenarioLabel] = true
		requestIDs = append(requestIDs, row.RequestID)
		scenarios = append(scenarios, snapshotScenario{Label: row.ScenarioLabel, RequestID: row.RequestID})
	}
	sort.Strings(requestIDs)
	sort.Slice(scenarios, func(i, j int) bool {
		if scenarios[i].RequestID == scenarios[j].RequestID {
			return scenarios[i].Label < scenarios[j].Label
		}
		return scenarios[i].RequestID < scenarios[j].RequestID
	})
	return requestIDs, scenarios, nil
}

func validateStrictSourceInstance(value string) error {
	if placeholderIdentifier(value) || len(value) > 160 || !strictSourceInstancePattern.MatchString(value) || blockedStringClass(value) != "" {
		return errors.New("strict_manifest_invalid: source_instance")
	}
	return nil
}

func placeholderIdentifier(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", "tbd", "todo", "n/a", "na", "none", "unknown", "unavailable":
		return true
	default:
		return false
	}
}

func parseScenarios(flags repeatedFlag) ([]snapshotScenario, error) {
	var scenarios []snapshotScenario
	for _, value := range flags {
		label, requestID, ok := strings.Cut(value, "=")
		if !ok {
			return nil, fmt.Errorf("scenario %q must use label=request_id", value)
		}
		label = strings.TrimSpace(label)
		requestID = strings.TrimSpace(requestID)
		if label == "" || requestID == "" {
			return nil, fmt.Errorf("scenario %q must include non-empty label and request_id", value)
		}
		scenarios = append(scenarios, snapshotScenario{Label: label, RequestID: requestID})
	}
	sort.Slice(scenarios, func(i, j int) bool {
		if scenarios[i].RequestID == scenarios[j].RequestID {
			return scenarios[i].Label < scenarios[j].Label
		}
		return scenarios[i].RequestID < scenarios[j].RequestID
	})
	return scenarios, nil
}

func validateScenarios(requestIDs []string, scenarios []snapshotScenario) error {
	selected := make(map[string]bool, len(requestIDs))
	for _, requestID := range requestIDs {
		selected[requestID] = true
	}
	for _, scenario := range scenarios {
		if !selected[scenario.RequestID] {
			return errors.New("scenario references an unselected request_id")
		}
	}
	return nil
}

func placeholders(count int) string {
	parts := make([]string, count)
	for i := range parts {
		parts[i] = fmt.Sprintf("$%d", i+1)
	}
	return strings.Join(parts, ", ")
}

func argsFromStrings(values []string) []interface{} {
	args := make([]interface{}, len(values))
	for i, value := range values {
		args[i] = value
	}
	return args
}

func nullableInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	v := value.Int64
	return &v
}

func nullableStringSQL(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	v := value.String
	return &v
}

func sortedKeys(values map[string]interface{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func stableHash(value interface{}) string {
	payload, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func sha256Digest(payload []byte) string {
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func newExchangeID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate exchange_id: %w", err)
	}
	return "uxchg_" + hex.EncodeToString(value[:]), nil
}

func sourceRef(sourceInstance, table string, id int64) string {
	return fmt.Sprintf("unirouter:%s:%s:%d", sourceInstance, table, id)
}

func sanitizeSourceInstance(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "local-controlled"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-._")
	if out == "" {
		return "local-controlled"
	}
	return out
}

func sourceComponentForTrace(stage string) string {
	switch strings.TrimSpace(strings.ToLower(stage)) {
	case "auth":
		return "auth"
	case "billing":
		return "billing"
	case "routing", "route":
		return "routing"
	case "provider", "upstream":
		return "provider"
	case "logging", "log":
		return "logging"
	case "gateway", "received", "response":
		return "gateway"
	default:
		return "trace"
	}
}
