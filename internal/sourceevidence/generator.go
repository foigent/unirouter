package sourceevidence

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/completedfeed"
	"github.com/uniroute/unirouter/internal/eventconnector"
)

type TxStarter interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

type GenerateOptions struct {
	ProtocolPin []byte
	Now         func() time.Time
}

type Generation struct {
	SourceScope          SourceScope
	SourceFacts          SourceFacts
	CompatibilityMembers []CompatibilityBundle
}

type PreparedComponentInputs struct {
	SourceScope      SourceScope
	InstalledRelease InstalledReleaseIdentity
	ScopeReceipt     SourceScopeReceipt
}

type protocolPinDocument struct {
	PinVersion      string            `json:"pin_version"`
	Product         string            `json:"product"`
	ProtocolVersion string            `json:"protocol_version"`
	ReleaseDigest   string            `json:"release_digest"`
	Components      map[string]string `json:"components"`
	Authority       protocolAuthority `json:"authority"`
}

type protocolAuthority struct {
	Repository   string `json:"repository"`
	RepositoryID int64  `json:"repository_id"`
	Tag          string `json:"tag"`
}

func ParseProtocolPin(body []byte) (ProtocolFacts, error) {
	if len(body) == 0 || len(body) > 1<<20 {
		return ProtocolFacts{}, errors.New("protocol pin size is invalid")
	}
	if strictErr := eventconnector.ValidateStrictJSON(body, eventconnector.StrictJSONOptions{}); strictErr != nil {
		return ProtocolFacts{}, fmt.Errorf("protocol pin strict JSON: %s", strictErr.Code)
	}
	var document protocolPinDocument
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return ProtocolFacts{}, fmt.Errorf("decode protocol pin: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ProtocolFacts{}, errors.New("protocol pin contains trailing content")
	}
	if document.Product != "unirouter" || document.PinVersion == "" || document.ProtocolVersion == "" || !hashRefPattern.MatchString(document.ReleaseDigest) || len(document.Components) == 0 || len(document.Components) > 64 || !strings.HasPrefix(document.Authority.Repository, "https://") || document.Authority.RepositoryID <= 0 || !portableWithin(document.Authority.Tag, 64) {
		return ProtocolFacts{}, errors.New("protocol pin identity is invalid")
	}
	components := make([]ProtocolComponent, 0, len(document.Components))
	for name, digest := range document.Components {
		components = append(components, ProtocolComponent{Name: name, SHA256: digest})
	}
	sort.Slice(components, func(left, right int) bool { return components[left].Name < components[right].Name })
	result := ProtocolFacts{
		PinVersion:      document.PinVersion,
		ProtocolVersion: document.ProtocolVersion,
		ReleaseDigest:   document.ReleaseDigest,
		PinSHA256:       hashBytes(body),
		Components:      components,
	}
	if err := ValidateProtocolFacts(result); err != nil {
		return ProtocolFacts{}, err
	}
	return result, nil
}

// PrepareComponentInputs emits only product-owned preauthorization facts. It
// uses the same read-only repeatable-read authority as capture but does not
// inspect request execution rows and cannot create Source facts or traffic.
func PrepareComponentInputs(ctx context.Context, database TxStarter, requestSet RequestSet, protocolPin []byte) (PreparedComponentInputs, error) {
	if database == nil {
		return PreparedComponentInputs{}, errors.New("source evidence database is required")
	}
	if err := ValidateRequestSet(requestSet); err != nil {
		return PreparedComponentInputs{}, err
	}
	sourceScope := SourceScopeForRequestSet(requestSet)
	if err := ValidateSourceScopeForRequestSet(sourceScope, requestSet); err != nil {
		return PreparedComponentInputs{}, err
	}
	protocol, err := ParseProtocolPin(protocolPin)
	if err != nil {
		return PreparedComponentInputs{}, err
	}
	tx, err := database.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return PreparedComponentInputs{}, fmt.Errorf("begin read-only repeatable-read component preparation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	release, snapshot, err := readSnapshotIdentity(ctx, tx)
	if err != nil {
		return PreparedComponentInputs{}, err
	}
	installedRelease, err := BuildInstalledReleaseIdentity(release, snapshot)
	if err != nil {
		return PreparedComponentInputs{}, err
	}
	scopeReceipt, err := BuildSourceScopeReceipt(requestSet, sourceScope, protocol, installedRelease, snapshot)
	if err != nil {
		return PreparedComponentInputs{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PreparedComponentInputs{}, fmt.Errorf("commit read-only component preparation: %w", err)
	}
	return PreparedComponentInputs{SourceScope: sourceScope, InstalledRelease: installedRelease, ScopeReceipt: scopeReceipt}, nil
}

func ValidateGenerationAgainstPrepared(generation Generation, requestSet RequestSet, protocol ProtocolFacts, installed InstalledReleaseIdentity, scope SourceScopeReceipt) error {
	if err := ValidatePreparedBindings(requestSet, generation.SourceScope, protocol, installed, scope); err != nil {
		return err
	}
	current := generation.SourceFacts.Release
	if installed.ReleaseSequence != current.ReleaseSequence || installed.Version != current.Version || installed.CanonicalCommit != current.Commit || installed.PackageSHA256 != "sha256:"+current.PackageSHA256 || installed.TrustedKeyID != current.TrustedKeyID || installed.DatabaseSchema != current.CurrentSchema || installed.RecordedAt != current.RecordedAt {
		return errors.New("installed release changed after neutral preauthorization")
	}
	if generation.SourceFacts.RequestSetSHA256 != scope.RequestSetSHA256 || generation.SourceFacts.SourceBindingSHA256 != scope.SourceBindingSHA256 || generation.SourceFacts.Protocol.PinSHA256 != scope.ProtocolPinSHA256 {
		return errors.New("captured Source facts do not match the preauthorized product scope receipt")
	}
	return nil
}

func Generate(ctx context.Context, database TxStarter, requestSet RequestSet, options GenerateOptions) (Generation, error) {
	if database == nil {
		return Generation{}, errors.New("source evidence database is required")
	}
	if err := ValidateRequestSet(requestSet); err != nil {
		return Generation{}, err
	}
	sourceScope := SourceScopeForRequestSet(requestSet)
	if err := ValidateSourceScopeForRequestSet(sourceScope, requestSet); err != nil {
		return Generation{}, err
	}
	protocol, err := ParseProtocolPin(options.ProtocolPin)
	if err != nil {
		return Generation{}, err
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	tx, err := database.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Generation{}, fmt.Errorf("begin read-only repeatable-read Source snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	release, snapshot, err := readSnapshotIdentity(ctx, tx)
	if err != nil {
		return Generation{}, err
	}
	requestSetSHA, err := SemanticSHA256(requestSet)
	if err != nil {
		return Generation{}, fmt.Errorf("hash request set: %w", err)
	}
	sourceBindingSHA, err := SemanticSHA256(sourceScope)
	if err != nil {
		return Generation{}, fmt.Errorf("hash Source scope: %w", err)
	}
	requests := make([]RequestFacts, len(requestSet.Requests))
	byIdentity := make(map[string]*RequestFacts, len(requestSet.Requests))
	for index, selected := range requestSet.Requests {
		requests[index] = RequestFacts{
			ScenarioID:       selected.ScenarioID,
			RequestID:        selected.RequestID,
			OrgID:            selected.OrgID,
			ProjectID:        selected.ProjectID,
			TraceEvents:      []TraceFact{},
			ProviderAttempts: []ProviderAttemptFact{},
			CaptureGaps:      []SourceCaptureGap{},
		}
		byIdentity[requestIdentity(selected.OrgID, selected.RequestID)] = &requests[index]
	}
	orgIDs, requestIDs := selectionArrays(requestSet.Requests)
	if err := readCallLogs(ctx, tx, orgIDs, requestIDs, byIdentity, requestSet.SourceInstance); err != nil {
		return Generation{}, err
	}
	if err := readTraceEvents(ctx, tx, orgIDs, requestIDs, byIdentity, requestSet.SourceInstance); err != nil {
		return Generation{}, err
	}
	if err := readProviderAttempts(ctx, tx, orgIDs, requestIDs, byIdentity, requestSet.SourceInstance); err != nil {
		return Generation{}, err
	}
	if err := readCompletedFeed(ctx, tx, orgIDs, requestIDs, byIdentity, requestSet.SourceInstance); err != nil {
		return Generation{}, err
	}
	for index := range requests {
		addAbsenceGaps(&requests[index])
	}

	counts, feedRange, err := summarizeFacts(requests)
	if err != nil {
		return Generation{}, err
	}
	generatedAt := now().UTC()
	observedAt, err := time.Parse(time.RFC3339Nano, snapshot.TransactionObservedAt)
	if err != nil {
		return Generation{}, err
	}
	if generatedAt.Before(observedAt) {
		generatedAt = observedAt
	}
	facts := SourceFacts{
		SchemaVersion:       SourceFactsSchemaVersion,
		GeneratedAt:         canonicalTime(generatedAt),
		RequestSetSHA256:    requestSetSHA,
		SourceBindingSHA256: sourceBindingSHA,
		CampaignID:          requestSet.CampaignID,
		Source: SourceIdentity{
			Product:            "unirouter",
			Instance:           requestSet.SourceInstance,
			IdentityAuthority:  SourceIdentityAuthority,
			RuntimeObservation: SourceRuntimeObservation,
			FeedSnapshotSchema: completedfeed.SnapshotSchemaVersion,
		},
		Release:   release,
		Protocol:  protocol,
		Snapshot:  snapshot,
		Requests:  requests,
		FeedRange: feedRange,
		Counts:    counts,
		Completeness: SourceCompleteness{
			ClaimScope:                    SourceFactsSchemaVersion,
			State:                         "complete",
			BoundedRequestSetComplete:     true,
			ExecutionEvidenceV2Projection: "partial",
			RuntimeCloseMarkerAvailable:   false,
		},
		Safety: SourceSafety{
			DatabaseMutation: "none",
		},
	}
	if err := ValidateSourceFacts(facts); err != nil {
		return Generation{}, fmt.Errorf("validate generated Source facts: %w", err)
	}
	members := make([]CompatibilityBundle, 0, len(facts.Requests))
	for _, request := range facts.Requests {
		member, err := projectCompatibilityMember(facts, request)
		if err != nil {
			return Generation{}, err
		}
		members = append(members, member)
	}
	if err := tx.Commit(ctx); err != nil {
		return Generation{}, fmt.Errorf("commit read-only Source snapshot: %w", err)
	}
	return Generation{SourceScope: sourceScope, SourceFacts: facts, CompatibilityMembers: members}, nil
}

func readSnapshotIdentity(ctx context.Context, tx pgx.Tx) (ReleaseFacts, SnapshotFacts, error) {
	var (
		releaseSequence int64
		version         string
		commit          string
		packageSHA      string
		keyID           string
		recordedSchema  string
		recordedAt      time.Time
	)
	if err := tx.QueryRow(ctx, `
		SELECT state.release_sequence, state.version, state.commit, state.package_sha256,
		       state.trusted_key_id, state.target_schema, history.recorded_at
		FROM release_state state
		JOIN release_history history
		  ON history.release_sequence = state.release_sequence
		 AND history.version = state.version
		 AND history.commit = state.commit
		 AND history.package_sha256 = state.package_sha256
		 AND history.trusted_key_id = state.trusted_key_id
		 AND history.target_schema = state.target_schema
		 AND history.manifest = state.manifest
		WHERE state.singleton = TRUE`,
	).Scan(&releaseSequence, &version, &commit, &packageSHA, &keyID, &recordedSchema, &recordedAt); err != nil {
		return ReleaseFacts{}, SnapshotFacts{}, fmt.Errorf("read exact release state/history binding: %w", err)
	}
	var currentSchema string
	if err := tx.QueryRow(ctx, `SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1`).Scan(&currentSchema); err != nil {
		return ReleaseFacts{}, SnapshotFacts{}, fmt.Errorf("read current schema: %w", err)
	}
	if commit != strings.ToLower(commit) || !gitObjectPattern.MatchString(commit) {
		return ReleaseFacts{}, SnapshotFacts{}, errors.New("durable release commit must use canonical lowercase Git identity")
	}
	if !hashPattern.MatchString(packageSHA) || packageSHA != strings.ToLower(packageSHA) || !portableWithin(version, 128) || !metadataKeyPattern.MatchString(keyID) || !portableWithin(keyID, 64) || !portableWithin(recordedSchema, 128) || releaseSequence <= 0 {
		return ReleaseFacts{}, SnapshotFacts{}, errors.New("durable release state identity is invalid")
	}
	if currentSchema != recordedSchema {
		return ReleaseFacts{}, SnapshotFacts{}, errors.New("durable release state schema does not match current schema")
	}
	var (
		transactionReadOnly bool
		isolation           string
		observedAt          time.Time
		txIDAssigned        bool
	)
	if err := tx.QueryRow(ctx, `
		SELECT current_setting('transaction_read_only')::boolean,
		       current_setting('transaction_isolation'),
		       transaction_timestamp(),
		       txid_current_if_assigned() IS NOT NULL`,
	).Scan(&transactionReadOnly, &isolation, &observedAt, &txIDAssigned); err != nil {
		return ReleaseFacts{}, SnapshotFacts{}, fmt.Errorf("read transaction evidence: %w", err)
	}
	return ReleaseFacts{
			ReleaseSequence: releaseSequence,
			Version:         version,
			Commit:          commit,
			PackageSHA256:   packageSHA,
			TrustedKeyID:    keyID,
			RecordedSchema:  recordedSchema,
			CurrentSchema:   currentSchema,
			RecordedAt:      canonicalTime(recordedAt),
		}, SnapshotFacts{
			Isolation:             isolation,
			ReadOnly:              transactionReadOnly,
			TransactionObservedAt: canonicalTime(observedAt),
			TransactionIDAssigned: txIDAssigned,
			SelectionMode:         "bounded_tenant_request_set",
		}, nil
}

func readCallLogs(ctx context.Context, tx pgx.Tx, orgIDs []int64, requestIDs []string, selected map[string]*RequestFacts, sourceInstance string) error {
	rows, err := tx.Query(ctx, `
		SELECT cl.id, cl.org_id, cl.request_id, cl.project_id,
		       cl.model, cl.requested_model, cl.provider, cl.channel,
		       cl.prompt_tokens, cl.completion_tokens, cl.total_tokens,
		       cl.cost_micros, cl.usage_source, cl.usage_authority, cl.usage_finality,
		       cl.status_code, cl.upstream_status_code, cl.latency_ms,
		       cl.trace_id, cl.attempt_id, cl.actor_type,
		       cl.payload_capture_mode,
		       (cl.upstream_error IS NOT NULL AND btrim(cl.upstream_error) <> ''),
		       (btrim(cl.ip_address) <> ''), cl.created_at
		FROM call_logs cl
		JOIN unnest($1::bigint[], $2::text[]) selected(org_id, request_id)
		  ON selected.org_id = cl.org_id AND selected.request_id = cl.request_id
		ORDER BY cl.org_id, cl.request_id, cl.id
		LIMIT $3`, orgIDs, requestIDs, MaxRequests+1)
	if err != nil {
		return fmt.Errorf("read bounded call logs: %w", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		if count > MaxRequests {
			return errors.New("bounded request set resolved to too many call logs")
		}
		var (
			id, orgID, promptTokens, completionTokens, totalTokens, costMicros, statusCode, latencyMS                    int64
			requestID, model, requestedModel, provider, channel, usageSource, usageAuthority, usageFinality, payloadMode string
			projectID, upstreamStatus                                                                                    sql.NullInt64
			traceID, attemptID, actorType                                                                                sql.NullString
			upstreamErrorSeen, ipSeen                                                                                    bool
			createdAt                                                                                                    time.Time
		)
		if err := rows.Scan(&id, &orgID, &requestID, &projectID, &model, &requestedModel, &provider, &channel, &promptTokens, &completionTokens, &totalTokens, &costMicros, &usageSource, &usageAuthority, &usageFinality, &statusCode, &upstreamStatus, &latencyMS, &traceID, &attemptID, &actorType, &payloadMode, &upstreamErrorSeen, &ipSeen, &createdAt); err != nil {
			return fmt.Errorf("scan call log: %w", err)
		}
		request, ok := selected[requestIdentity(orgID, requestID)]
		if !ok || !projectID.Valid || projectID.Int64 != request.ProjectID || request.CallLog != nil {
			return errors.New("call log tenant/project identity is ambiguous or duplicated")
		}
		fact := CallLogFact{
			SourceRef:          fmt.Sprintf("unirouter:%s:call_logs:%d", sourceInstance, id),
			ID:                 id,
			Model:              model,
			RequestedModel:     requestedModel,
			Provider:           provider,
			Channel:            channel,
			PromptTokens:       promptTokens,
			CompletionTokens:   completionTokens,
			TotalTokens:        totalTokens,
			CostMicros:         strconv.FormatInt(costMicros, 10),
			Currency:           "CNY",
			AmountScale:        1_000_000,
			UsageSource:        usageSource,
			UsageAuthority:     usageAuthority,
			UsageFinality:      usageFinality,
			StatusCode:         statusCode,
			UpstreamStatusCode: nullInt64Pointer(upstreamStatus),
			LatencyMS:          latencyMS,
			TraceID:            traceID.String,
			AttemptID:          attemptID.String,
			ActorType:          canonicalSourceActorType(actorType.String),
			PayloadCaptureMode: payloadMode,
			UpstreamErrorSeen:  upstreamErrorSeen,
			IPAddressSeen:      ipSeen,
			CreatedAt:          canonicalTime(createdAt),
		}
		fact.FactSHA256, err = semanticFactHash(fact)
		if err != nil {
			return err
		}
		request.CallLog = &fact
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read call logs: %w", err)
	}
	return nil
}

func readTraceEvents(ctx context.Context, tx pgx.Tx, orgIDs []int64, requestIDs []string, selected map[string]*RequestFacts, sourceInstance string) error {
	rows, err := tx.Query(ctx, `
		SELECT te.id, te.org_id, te.request_id, te.project_id,
		       te.event_type, te.stage, te.severity, te.model, te.requested_model,
		       te.provider, te.channel, te.status_code, te.latency_ms,
		       te.trace_id, te.span_id, te.parent_span_id, te.attempt_id,
		       te.actor_type,
		       te.decision_kind, te.decision_result, te.decision_reason,
		       te.capture_gap, te.payload_capture_mode,
		       ARRAY(SELECT key FROM jsonb_object_keys(te.metadata) AS key ORDER BY key),
		       te.created_at
		FROM request_trace_events te
		JOIN unnest($1::bigint[], $2::text[]) selected(org_id, request_id)
		  ON selected.org_id = te.org_id AND selected.request_id = te.request_id
		ORDER BY te.org_id, te.request_id, te.created_at, te.id
		LIMIT $3`, orgIDs, requestIDs, MaxTraceFacts+1)
	if err != nil {
		return fmt.Errorf("read bounded request trace events: %w", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		if count > MaxTraceFacts {
			return fmt.Errorf("request trace facts exceed %d", MaxTraceFacts)
		}
		var (
			id, orgID, latencyMS                                                                         int64
			requestID, eventType, stage, severity, model, requestedModel, provider, channel, payloadMode string
			projectID, statusCode                                                                        sql.NullInt64
			traceID, spanID, parentSpanID, attemptID, actorType                                          sql.NullString
			decisionKind, decisionResult, decisionReason, captureGap                                     sql.NullString
			metadataKeys                                                                                 []string
			createdAt                                                                                    time.Time
		)
		if err := rows.Scan(&id, &orgID, &requestID, &projectID, &eventType, &stage, &severity, &model, &requestedModel, &provider, &channel, &statusCode, &latencyMS, &traceID, &spanID, &parentSpanID, &attemptID, &actorType, &decisionKind, &decisionResult, &decisionReason, &captureGap, &payloadMode, &metadataKeys, &createdAt); err != nil {
			return fmt.Errorf("scan request trace event: %w", err)
		}
		request, ok := selected[requestIdentity(orgID, requestID)]
		if !ok || !projectID.Valid || projectID.Int64 != request.ProjectID {
			return errors.New("request trace tenant/project identity does not match frozen request set")
		}
		fact := TraceFact{
			SourceRef:          fmt.Sprintf("unirouter:%s:request_trace_events:%d", sourceInstance, id),
			ID:                 id,
			EventType:          eventType,
			Stage:              stage,
			CompatibilityStage: compatibilityStage(stage, eventType),
			Severity:           severity,
			Model:              model,
			RequestedModel:     requestedModel,
			Provider:           provider,
			Channel:            channel,
			StatusCode:         nullInt64Pointer(statusCode),
			LatencyMS:          latencyMS,
			TraceID:            traceID.String,
			SpanID:             spanID.String,
			ParentSpanID:       parentSpanID.String,
			AttemptID:          attemptID.String,
			ActorType:          canonicalSourceActorType(actorType.String),
			DecisionKind:       decisionKind.String,
			DecisionResult:     decisionResult.String,
			DecisionReason:     decisionReason.String,
			CaptureGap:         captureGap.String,
			MetadataKeys:       metadataKeys,
			PayloadCaptureMode: payloadMode,
			CreatedAt:          canonicalTime(createdAt),
		}
		fact.FactSHA256, err = semanticFactHash(fact)
		if err != nil {
			return err
		}
		request.TraceEvents = append(request.TraceEvents, fact)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read request trace events: %w", err)
	}
	return nil
}

func readProviderAttempts(ctx context.Context, tx pgx.Tx, orgIDs []int64, requestIDs []string, selected map[string]*RequestFacts, sourceInstance string) error {
	rows, err := tx.Query(ctx, `
		SELECT pa.org_id, pa.request_id, pa.attempt_id, pa.project_id,
		       pa.api_key_id, pa.access_credential_id, pa.credential_ref, pa.access_grant_id,
		       pa.channel_id, pa.channel_key_id, pa.route_sequence, pa.route_total,
		       pa.endpoint, pa.provider, pa.state, pa.outcome_certainty, pa.retry_authorized,
		       pa.http_status, pa.failure_kind, pa.calculated_cost_micros, pa.charged_cost_micros,
		       pa.started_at, pa.response_received_at, pa.financial_resolved_at, pa.updated_at
		FROM billing_provider_attempts pa
		JOIN unnest($1::bigint[], $2::text[]) selected(org_id, request_id)
		  ON selected.org_id = pa.org_id AND selected.request_id = pa.request_id
		ORDER BY pa.org_id, pa.request_id, pa.started_at, pa.attempt_id
		LIMIT $3`, orgIDs, requestIDs, MaxProviderAttempts+1)
	if err != nil {
		return fmt.Errorf("read bounded provider attempts: %w", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		if count > MaxProviderAttempts {
			return fmt.Errorf("provider attempt facts exceed %d", MaxProviderAttempts)
		}
		var (
			orgID, apiKeyID, channelID, channelKeyID                                              int64
			routeSequence, routeTotal                                                             int
			requestID, attemptID, endpoint, provider, state, certainty                            string
			projectID, accessCredentialID, accessGrantID, httpStatus, calculatedCost, chargedCost sql.NullInt64
			credentialRef, failureKind                                                            sql.NullString
			retryAuthorized                                                                       bool
			startedAt, updatedAt                                                                  time.Time
			responseReceivedAt, financialResolvedAt                                               sql.NullTime
		)
		if err := rows.Scan(&orgID, &requestID, &attemptID, &projectID, &apiKeyID, &accessCredentialID, &credentialRef, &accessGrantID, &channelID, &channelKeyID, &routeSequence, &routeTotal, &endpoint, &provider, &state, &certainty, &retryAuthorized, &httpStatus, &failureKind, &calculatedCost, &chargedCost, &startedAt, &responseReceivedAt, &financialResolvedAt, &updatedAt); err != nil {
			return fmt.Errorf("scan provider attempt: %w", err)
		}
		request, ok := selected[requestIdentity(orgID, requestID)]
		if !ok || !projectID.Valid || projectID.Int64 != request.ProjectID {
			return errors.New("provider attempt tenant/project identity does not match frozen request set")
		}
		accessBindingType, credentialCorrelation, err := providerAttemptAccessBinding(apiKeyID, channelID, channelKeyID, accessCredentialID, credentialRef, accessGrantID)
		if err != nil {
			return err
		}
		fact := ProviderAttemptFact{
			SourceRef:                          fmt.Sprintf("unirouter:%s:billing_provider_attempts:%d:%s", sourceInstance, orgID, attemptID),
			AttemptID:                          attemptID,
			AccessBindingType:                  accessBindingType,
			LogicalCredentialCorrelationSHA256: credentialCorrelation,
			RouteSequence:                      routeSequence,
			RouteTotal:                         routeTotal,
			Endpoint:                           endpoint,
			Provider:                           provider,
			State:                              state,
			OutcomeCertainty:                   certainty,
			RetryAuthorized:                    retryAuthorized,
			HTTPStatus:                         nullInt64Pointer(httpStatus),
			FailureKind:                        canonicalProviderFailureKind(failureKind.String),
			CalculatedCostMicros:               nullInt64StringPointer(calculatedCost),
			ChargedCostMicros:                  nullInt64StringPointer(chargedCost),
			StartedAt:                          canonicalTime(startedAt),
			ResponseReceivedAt:                 nullTimeString(responseReceivedAt),
			FinancialResolvedAt:                nullTimeString(financialResolvedAt),
			UpdatedAt:                          canonicalTime(updatedAt),
		}
		fact.FactSHA256, err = semanticFactHash(fact)
		if err != nil {
			return err
		}
		request.ProviderAttempts = append(request.ProviderAttempts, fact)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read provider attempts: %w", err)
	}
	return nil
}

func providerAttemptAccessBinding(apiKeyID, channelID, channelKeyID int64, accessCredentialID sql.NullInt64, credentialRef sql.NullString, accessGrantID sql.NullInt64) (string, string, error) {
	if apiKeyID <= 0 || channelID <= 0 || channelKeyID <= 0 {
		return "", "", errors.New("provider attempt durable binding contains an invalid internal locator")
	}
	logicalFieldsPresent := accessCredentialID.Valid || credentialRef.Valid || accessGrantID.Valid
	if !logicalFieldsPresent {
		return ProviderAttemptAccessBindingLegacy, "", nil
	}
	if !accessCredentialID.Valid || accessCredentialID.Int64 <= 0 || !credentialRef.Valid || !accessGrantID.Valid || accessGrantID.Int64 <= 0 {
		return "", "", errors.New("provider attempt durable logical credential binding is incomplete")
	}
	correlation, err := logicalCredentialCorrelation(credentialRef.String)
	if err != nil {
		return "", "", fmt.Errorf("provider attempt durable logical credential binding: %w", err)
	}
	return ProviderAttemptAccessBindingLogical, correlation, nil
}

func readCompletedFeed(ctx context.Context, tx pgx.Tx, orgIDs []int64, requestIDs []string, selected map[string]*RequestFacts, sourceInstance string) error {
	rows, err := tx.Query(ctx, `
		SELECT fi.id, fi.org_id, fi.project_id, fi.request_id, fi.terminal_status,
		       fi.terminal_available_at, fi.revision, fi.snapshot, fi.snapshot_sha256
		FROM completed_execution_feed_items fi
		JOIN unnest($1::bigint[], $2::text[]) selected(org_id, request_id)
		  ON selected.org_id = fi.org_id AND selected.request_id = fi.request_id
		ORDER BY fi.org_id, fi.request_id, fi.id
		LIMIT $3`, orgIDs, requestIDs, MaxRequests+1)
	if err != nil {
		return fmt.Errorf("read bounded completed feed items: %w", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		if count > MaxRequests {
			return errors.New("bounded request set resolved to too many completed feed items")
		}
		var (
			id, orgID, projectID                             int64
			requestID, terminalStatus, revision, snapshotSHA string
			terminalAt                                       time.Time
			snapshotBytes                                    []byte
		)
		if err := rows.Scan(&id, &orgID, &projectID, &requestID, &terminalStatus, &terminalAt, &revision, &snapshotBytes, &snapshotSHA); err != nil {
			return fmt.Errorf("scan completed feed item: %w", err)
		}
		request, ok := selected[requestIdentity(orgID, requestID)]
		if !ok || projectID != request.ProjectID || request.CompletedFeed != nil {
			return errors.New("completed feed tenant/project identity is ambiguous or duplicated")
		}
		digest := sha256.Sum256(snapshotBytes)
		if snapshotSHA != "sha256:"+hex.EncodeToString(digest[:]) {
			return errors.New("completed feed snapshot hash does not match stored bytes")
		}
		snapshot, err := completedfeed.DecodeSnapshot(snapshotBytes)
		if err != nil {
			return fmt.Errorf("decode completed feed snapshot: %w", err)
		}
		if snapshot.RequestID != requestID || snapshot.OrgID != orgID || snapshot.ProjectID != projectID || snapshot.Terminal.Status != terminalStatus || canonicalTime(snapshot.Terminal.AvailableAt) != canonicalTime(terminalAt) {
			return errors.New("completed feed snapshot identity does not match relational binding")
		}
		fact := CompletedFeedFact{
			SourceRef:           fmt.Sprintf("unirouter:%s:completed_execution_feed_items:%d", sourceInstance, id),
			SequenceID:          id,
			TerminalStatus:      terminalStatus,
			TerminalAvailableAt: canonicalTime(terminalAt),
			Revision:            revision,
			SnapshotSHA256:      snapshotSHA,
			ContentSummary: FeedContentSummary{
				Model:                snapshot.Usage.Model,
				PromptTokens:         snapshot.Usage.PromptTokens,
				CompletionTokens:     snapshot.Usage.CompletionTokens,
				UsageAuthority:       snapshot.Usage.Authority,
				UsageFinality:        snapshot.Usage.Finality,
				CalculatedCostMicros: snapshot.Money.CalculatedCostMicros,
				ChargedCostMicros:    snapshot.Money.ChargedCostMicros,
				PricingVersion:       snapshot.Money.PricingVersion,
			},
		}
		fact.FactSHA256, err = semanticFactHash(fact)
		if err != nil {
			return err
		}
		request.CompletedFeed = &fact
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read completed feed items: %w", err)
	}
	return nil
}

func addAbsenceGaps(request *RequestFacts) {
	if request.CallLog == nil {
		request.CaptureGaps = append(request.CaptureGaps, sourceCaptureGapContract["call_log_unavailable"])
	}
	if len(request.TraceEvents) == 0 {
		request.CaptureGaps = append(request.CaptureGaps, sourceCaptureGapContract["request_trace_unavailable"])
	}
	if len(request.ProviderAttempts) == 0 {
		request.CaptureGaps = append(request.CaptureGaps, sourceCaptureGapContract["provider_attempt_unavailable"])
	}
	if request.CompletedFeed == nil {
		request.CaptureGaps = append(request.CaptureGaps, sourceCaptureGapContract["completed_feed_item_unavailable"])
	}
}

func summarizeFacts(requests []RequestFacts) (SourceFactCounts, FeedRangeFacts, error) {
	counts := SourceFactCounts{RequestedRequests: len(requests)}
	feedRange := FeedRangeFacts{Selection: "bounded_request_set", CursorClaim: "not_part_of_source_facts", HighWatermarkClaim: "not_part_of_source_facts"}
	for _, request := range requests {
		if request.CallLog != nil {
			counts.CallLogs++
		}
		counts.TraceEvents += len(request.TraceEvents)
		counts.ProviderAttempts += len(request.ProviderAttempts)
		counts.CaptureGaps += len(request.CaptureGaps)
		if request.CompletedFeed != nil {
			counts.CompletedFeedItems++
			sequence := request.CompletedFeed.SequenceID
			if feedRange.MinSequenceID == nil || sequence < *feedRange.MinSequenceID {
				value := sequence
				feedRange.MinSequenceID = &value
			}
			if feedRange.MaxSequenceID == nil || sequence > *feedRange.MaxSequenceID {
				value := sequence
				feedRange.MaxSequenceID = &value
			}
		}
	}
	counts.TotalFacts = counts.CallLogs + counts.TraceEvents + counts.ProviderAttempts + counts.CompletedFeedItems + counts.CaptureGaps
	feedRange.SelectedItems = counts.CompletedFeedItems
	if counts.TraceEvents > MaxTraceFacts || counts.ProviderAttempts > MaxProviderAttempts || counts.TotalFacts > MaxFacts {
		return SourceFactCounts{}, FeedRangeFacts{}, fmt.Errorf("bounded Source fact count %d exceeds %d", counts.TotalFacts, MaxFacts)
	}
	return counts, feedRange, nil
}

func projectCompatibilityMember(source SourceFacts, request RequestFacts) (CompatibilityBundle, error) {
	traceFacts := make([]CompatibilityTraceFact, 0, len(request.TraceEvents))
	traceHashes := make([]string, 0, len(request.TraceEvents))
	coverage := make(map[string][]string, len(compatibilityStages))
	for _, trace := range request.TraceEvents {
		hash := strings.TrimPrefix(trace.FactSHA256, "sha256:")
		traceHashes = append(traceHashes, hash)
		coverage[trace.CompatibilityStage] = append(coverage[trace.CompatibilityStage], hash)
		traceFacts = append(traceFacts, CompatibilityTraceFact{
			SourceRef:          trace.SourceRef,
			SourceEventHash:    hash,
			EventType:          trace.EventType,
			Stage:              trace.Stage,
			Severity:           trace.Severity,
			CreatedAt:          trace.CreatedAt,
			TraceID:            trace.TraceID,
			SpanID:             trace.SpanID,
			ParentSpanID:       trace.ParentSpanID,
			AttemptID:          trace.AttemptID,
			ActorType:          trace.ActorType,
			Model:              trace.Model,
			RequestedModel:     trace.RequestedModel,
			Provider:           trace.Provider,
			Channel:            trace.Channel,
			StatusCode:         trace.StatusCode,
			LatencyMS:          trace.LatencyMS,
			DecisionKind:       trace.DecisionKind,
			DecisionResult:     trace.DecisionResult,
			DecisionReason:     trace.DecisionReason,
			CaptureGap:         trace.CaptureGap,
			PayloadCaptureMode: "metadata_only",
			Metadata:           map[string]any{},
		})
	}
	callFacts := []CompatibilityCallLogFact{}
	callHashes := []string{}
	if request.CallLog != nil {
		call := request.CallLog
		hash := strings.TrimPrefix(call.FactSHA256, "sha256:")
		callHashes = append(callHashes, hash)
		coverage["usage"] = append(coverage["usage"], hash)
		coverage["logging"] = append(coverage["logging"], hash)
		callFacts = append(callFacts, CompatibilityCallLogFact{
			SourceRef:          call.SourceRef,
			SourceEventHash:    hash,
			CreatedAt:          call.CreatedAt,
			TraceID:            call.TraceID,
			AttemptID:          call.AttemptID,
			ActorType:          call.ActorType,
			Model:              call.Model,
			RequestedModel:     call.RequestedModel,
			Provider:           call.Provider,
			Channel:            call.Channel,
			StatusCode:         call.StatusCode,
			UpstreamStatusCode: call.UpstreamStatusCode,
			LatencyMS:          call.LatencyMS,
			PromptTokens:       call.PromptTokens,
			CompletionTokens:   call.CompletionTokens,
			CostMicros:         call.CostMicros,
			Currency:           call.Currency,
			AmountScale:        call.AmountScale,
			UsageSource:        call.UsageSource,
			UsageAuthority:     call.UsageAuthority,
			UsageFinality:      call.UsageFinality,
			PayloadCaptureMode: "metadata_only",
		})
	}
	sort.Strings(traceHashes)
	sort.Strings(callHashes)
	stageCoverage := make([]CompatibilityStageCoverage, 0, len(compatibilityStages))
	for _, stage := range compatibilityStages {
		hashes := normalizedHashes(coverage[stage])
		status := "gap"
		if len(hashes) > 0 {
			status = "present"
		}
		stageCoverage = append(stageCoverage, CompatibilityStageCoverage{Stage: stage, Status: status, SourceEventHashes: hashes})
	}
	gaps := []CompatibilityGap{{Code: CompatibilityGapRuntimeCloseMarker, Stage: "source", Reason: "runtime close marker is unavailable; closed_at is the snapshot cutoff, not a runtime closure claim; expected hashes are limited to persisted facts visible in the frozen repeatable-read snapshot"}}
	for _, gap := range request.CaptureGaps {
		gaps = append(gaps, CompatibilityGap{Code: gap.Code, Stage: "source", Reason: gap.Reason})
	}
	projectID := request.ProjectID
	bundle := CompatibilityBundle{
		SchemaVersion: CompatibilityBundleSchema,
		Profile: CompatibilityProfileRef{
			ID:                 CompatibilityProfileID,
			ManifestVersion:    CompatibilityProfileManifestVersion,
			ManifestSHA256:     CompatibilityProfileManifestSHA256,
			BundleSchemaSHA256: CompatibilityBundleSchemaSHA256,
		},
		SourceInstance: source.Source.Instance,
		RequestID:      request.RequestID,
		OrgID:          request.OrgID,
		ProjectID:      &projectID,
		ClosedAt:       source.GeneratedAt,
		Completeness: CompatibilityCompleteness{
			State:                    "partial",
			SourceProfileComplete:    false,
			ExpectedTraceEventHashes: append([]string(nil), traceHashes...),
			ExpectedCallLogHashes:    append([]string(nil), callHashes...),
			ObservedTraceEventHashes: append([]string(nil), traceHashes...),
			ObservedCallLogHashes:    append([]string(nil), callHashes...),
			MissingEventHashes:       []string{},
		},
		StageCoverage: stageCoverage,
		TraceEvents:   traceFacts,
		CallLogs:      callFacts,
		CaptureGaps:   gaps,
	}
	if err := ValidateCompatibilityBundle(bundle); err != nil {
		return CompatibilityBundle{}, fmt.Errorf("validate compatibility member for %s: %w", request.RequestID, err)
	}
	return bundle, nil
}

func selectionArrays(requests []RequestSelection) ([]int64, []string) {
	orgIDs := make([]int64, len(requests))
	requestIDs := make([]string, len(requests))
	for index, request := range requests {
		orgIDs[index] = request.OrgID
		requestIDs[index] = request.RequestID
	}
	return orgIDs, requestIDs
}

func requestIdentity(orgID int64, requestID string) string {
	return strconv.FormatInt(orgID, 10) + "\x00" + requestID
}

func semanticFactHash(value any) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal Source fact: %w", err)
	}
	digest := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func compatibilityStage(stage, eventType string) string {
	normalized := strings.ToLower(stage + "." + eventType)
	switch {
	case strings.Contains(normalized, "retry") || strings.Contains(normalized, "fallback"):
		return "retry_fallback"
	case strings.Contains(normalized, "identity") || strings.Contains(normalized, "auth") || strings.Contains(normalized, "admission"):
		return "identity"
	case strings.Contains(normalized, "route") || strings.Contains(normalized, "routing"):
		return "routing"
	case strings.Contains(normalized, "provider") || strings.Contains(normalized, "upstream"):
		return "provider"
	case strings.Contains(normalized, "bill") || strings.Contains(normalized, "settle") || strings.Contains(normalized, "wallet") || strings.Contains(normalized, "cost"):
		return "billing"
	case strings.Contains(normalized, "usage") || strings.Contains(normalized, "token"):
		return "usage"
	default:
		return "logging"
	}
}

func nullInt64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}

func nullInt64StringPointer(value sql.NullInt64) *string {
	if !value.Valid {
		return nil
	}
	result := strconv.FormatInt(value.Int64, 10)
	return &result
}

func nullTimeString(value sql.NullTime) string {
	if !value.Valid {
		return ""
	}
	return canonicalTime(value.Time)
}
