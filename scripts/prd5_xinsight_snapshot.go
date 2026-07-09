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
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/uniroute/unirouter/internal/traceutil"
)

const prd5SnapshotSchemaVersion = "unirouter.prd5.xinsight.snapshot.v1"
const prd5ApprovalManifestSchemaVersion = "unirouter.prd5.xinsight.approval.v1"

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
	CostCents              int64   `json:"cost_cents"`
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
	PayloadRef             *string `json:"payload_ref"`
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
	PayloadRef             *string                `json:"payload_ref"`
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
	var requestIDs []string
	var scenarios []snapshotScenario
	if approvalManifestPath != "" {
		if len(requestIDFlags) > 0 || strings.TrimSpace(requestIDsCSV) != "" || len(scenarioFlags) > 0 {
			return errors.New("--approval-manifest cannot be combined with --request-id, --request-ids, or --scenario")
		}
		manifest, manifestSHA256, err := loadApprovalManifest(approvalManifestPath)
		if err != nil {
			return err
		}
		approvalManifestSHA256 = manifestSHA256
		manifestSourceInstance := strings.TrimSpace(manifest.SourceInstance)
		if manifestSourceInstance == "" {
			return errors.New("approval manifest source_instance is required")
		}
		if sourceInstanceFlagSet && strings.TrimSpace(sourceInstance) != manifestSourceInstance {
			return errors.New("--source-instance must match approval manifest source_instance")
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
	sourceInstance = sanitizeSourceInstance(sourceInstance)
	if err := validateScenarios(requestIDs, scenarios); err != nil {
		return err
	}
	handoffNotePath = strings.TrimSpace(handoffNotePath)
	if handoffNotePath == "-" {
		return errors.New("--handoff-note must be a Markdown file path, not stdout")
	}

	dsn := strings.TrimSpace(os.Getenv(dsnEnv))
	if dsn == "" && dsnEnv != "DB_DSN" {
		dsn = strings.TrimSpace(os.Getenv("DB_DSN"))
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

	exchangeID, err := newExchangeID()
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
		},
	}
	return snapshot, nil
}

func loadCallLogs(ctx context.Context, db *sql.DB, sourceInstance string, requestIDs []string) ([]snapshotCallLog, error) {
	query := fmt.Sprintf(`
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
  cost_cents,
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
ORDER BY created_at, id`, placeholders(len(requestIDs)))

	rows, err := db.QueryContext(ctx, query, argsFromStrings(requestIDs)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []snapshotCallLog
	for rows.Next() {
		var row snapshotCallLog
		var projectID, apiKeyID, ownerUserID, upstreamStatusCode sql.NullInt64
		var traceID, sessionID, upstreamRequestID, evidenceRef, sourceEventHash sql.NullString
		var actorType, actorRef, onBehalfOf, payloadRef sql.NullString
		var createdAt time.Time
		if err := rows.Scan(
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
			&row.CostCents,
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
			return nil, err
		}
		row.ProjectID = nullableInt64(projectID)
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
		row.PayloadCaptureReason = "raw prompt/response export is disabled for PRD5 Gate A snapshots"
		row.DerivedSourceComponent = "logging"
		row.SourceRef = sourceRef(sourceInstance, "call_logs", row.ID)
		if sourceEventHash.Valid && sourceEventHash.String != "" {
			row.SourceEventHash = sourceEventHash.String
		} else {
			row.SourceEventHash = stableHash(row)
		}
		logs = append(logs, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return logs, nil
}

func loadTraceEvents(ctx context.Context, db *sql.DB, sourceInstance string, requestIDs []string) ([]snapshotTraceEvent, error) {
	query := fmt.Sprintf(`
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
FROM request_trace_events
WHERE request_id IN (%s)
ORDER BY created_at, id`, placeholders(len(requestIDs)))

	rows, err := db.QueryContext(ctx, query, argsFromStrings(requestIDs)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []snapshotTraceEvent
	for rows.Next() {
		var row snapshotTraceEvent
		var projectID, apiKeyID, ownerUserID, statusCode sql.NullInt64
		var traceID, spanID, parentSpanID, captureGap, evidenceRef, upstreamRequestID sql.NullString
		var decisionKind, decisionResult, decisionReason, actorType, actorRef, onBehalfOf, sessionID sql.NullString
		var payloadRef, sourceEventHash sql.NullString
		var metadataText string
		var createdAt time.Time
		if err := rows.Scan(
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
			return nil, err
		}

		metadata, err := sanitizedMetadata(metadataText)
		if err != nil {
			return nil, fmt.Errorf("trace event %d metadata: %w", row.ID, err)
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
		row.PayloadCaptureReason = "raw prompt/response export is disabled for PRD5 Gate A snapshots"
		row.SourceRef = sourceRef(sourceInstance, "request_trace_events", row.ID)
		if sourceEventHash.Valid && sourceEventHash.String != "" {
			row.SourceEventHash = sourceEventHash.String
		} else {
			row.SourceEventHash = stableHash(row)
		}
		events = append(events, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
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
				Reason:        "no call_logs row matched this request_id in the selected snapshot",
				Generated:     true,
			})
		}
		if !seenTrace[requestID] {
			candidates := []string{"not_available_from_source"}
			reason := "no request_trace_events row matched this request_id and no call_logs row is present in the selected snapshot"
			if seenCallLog[requestID] {
				candidates = []string{"sampled_out", "retention_expired", "not_instrumented"}
				reason = "call_logs row exists but no request_trace_events row matched this request_id; possible trace sampling, trace retention cleanup, or non-instrumented path"
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
		return approvalManifest{}, "", err
	}

	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var manifest approvalManifest
	if err := decoder.Decode(&manifest); err != nil {
		return approvalManifest{}, "", err
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return approvalManifest{}, "", errors.New("approval manifest must contain exactly one JSON object")
		}
		return approvalManifest{}, "", fmt.Errorf("approval manifest trailing data: %w", err)
	}
	if strings.TrimSpace(manifest.SchemaVersion) != prd5ApprovalManifestSchemaVersion {
		return approvalManifest{}, "", fmt.Errorf("approval manifest schema_version must be %q", prd5ApprovalManifestSchemaVersion)
	}
	return manifest, sha256Digest(payload), nil
}

func approvedManifestSelection(manifest approvalManifest) ([]string, []snapshotScenario, error) {
	var requestIDFlags repeatedFlag
	var scenarios []snapshotScenario
	seenLabels := map[string]bool{}
	for idx, row := range manifest.Requests {
		status := strings.ToLower(strings.TrimSpace(row.ApprovalStatus))
		switch status {
		case "approved":
			label := strings.TrimSpace(row.ScenarioLabel)
			requestID := strings.TrimSpace(row.RequestID)
			if label == "" {
				return nil, nil, fmt.Errorf("approval manifest row %d approved entry must include scenario_label", idx+1)
			}
			if placeholderRequestID(requestID) {
				return nil, nil, fmt.Errorf("approval manifest row %d approved entry must include a concrete request_id", idx+1)
			}
			if seenLabels[label] {
				return nil, nil, fmt.Errorf("approval manifest scenario_label %q is duplicated", label)
			}
			seenLabels[label] = true
			requestIDFlags = append(requestIDFlags, requestID)
			scenarios = append(scenarios, snapshotScenario{Label: label, RequestID: requestID})
		case "pending", "unavailable":
			continue
		default:
			return nil, nil, fmt.Errorf("approval manifest row %d has unsupported approval_status %q", idx+1, row.ApprovalStatus)
		}
	}
	requestIDs := normalizedRequestIDs(requestIDFlags, "")
	if len(requestIDs) == 0 {
		return nil, nil, errors.New("approval manifest contains no approved request IDs")
	}
	sort.Slice(scenarios, func(i, j int) bool {
		if scenarios[i].RequestID == scenarios[j].RequestID {
			return scenarios[i].Label < scenarios[j].Label
		}
		return scenarios[i].RequestID < scenarios[j].RequestID
	})
	return requestIDs, scenarios, nil
}

func placeholderRequestID(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", "tbd", "todo", "n/a", "na", "none", "unavailable":
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
			return fmt.Errorf("scenario %q references request_id %q, but that request_id is not selected for export", scenario.Label, scenario.RequestID)
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
