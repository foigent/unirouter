package completedfeed

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/uniroute/unirouter/internal/eventconnector"
)

const (
	neutralReferenceLocalAdapterID = "unirouter.completedfeed.reference-v1"
	neutralProtocolVersion         = "1.0.0"
	neutralProtocolCommit          = "c09bc93cc7750e2333752655d6b4e69802e6769d"
	neutralProtocolReleaseDigest   = "sha256:aaeb5b37b94a1fd603048d77b2180fa88be1ddbd6eeea6a1369edf0cea9c5e2b"
	neutralManifestDigest          = "sha256:a3bc97bd5d5c7aab6452de9518be0b00f62776877deb431a91c175da5287de79"
	neutralEnvelopeSchemaDigest    = "sha256:6942e85d9b5d8b08ff9828b970c317596394184ef29908cbf2b8fc792187cf26"
	neutralSourceSchemaDigest      = "sha256:4ae2897d4f8470c38624dfe21c54d72eaf6d995d2c9578c4420c94d244424fb2"
	neutralEventSchemaDigest       = "sha256:ea0ea7354f3d9f435bb86a34eb28284e4297e5a2d1a3d22a23058c7dcc514dc6"
	neutralConnectorID             = "foigent.unirouter.completed-execution"
	neutralConnectorVersion        = "1.0.0"
	neutralEventType               = "model.request.completed"
	neutralEventSchemaRef          = "foigent://events/model/request-completed/v1"
)

var neutralSharedGapOrder = [...]string{
	"call_log",
	"client_ip_presence",
	"request_trace",
	"upstream_error_presence",
}

type completedExecutionReferenceAdapter struct {
	descriptor eventconnector.Descriptor
}

func newCompletedExecutionReferenceAdapter() (completedExecutionReferenceAdapter, *eventconnector.ConnectorError) {
	descriptor, err := eventconnector.NewDescriptor(neutralReferenceDescriptorBinding())
	if err != nil {
		return completedExecutionReferenceAdapter{}, err
	}
	return completedExecutionReferenceAdapter{descriptor: descriptor}, nil
}

func neutralReferenceDescriptorBinding() eventconnector.DescriptorBinding {
	return eventconnector.DescriptorBinding{
		ProtocolVersion:      neutralProtocolVersion,
		ProtocolCommit:       neutralProtocolCommit,
		ReleaseDigest:        neutralProtocolReleaseDigest,
		ManifestDigest:       neutralManifestDigest,
		EnvelopeSpecVersion:  eventconnector.EventEnvelopeSpecVersion,
		EnvelopeSchemaDigest: neutralEnvelopeSchemaDigest,
		SourceProduct:        "unirouter",
		ConnectorID:          neutralConnectorID,
		ConnectorVersion:     neutralConnectorVersion,
		SourceSchema: eventconnector.SchemaBinding{
			SchemaID: SnapshotSchemaVersion,
			SHA256:   neutralSourceSchemaDigest,
		},
		EmittedEvents: []eventconnector.EventSchemaBinding{
			{
				EventType:     neutralEventType,
				SchemaRef:     neutralEventSchemaRef,
				SchemaVersion: "1",
				SHA256:        neutralEventSchemaDigest,
			},
		},
	}
}

func (adapter completedExecutionReferenceAdapter) Descriptor() eventconnector.Descriptor {
	return adapter.descriptor
}

func (adapter completedExecutionReferenceAdapter) Project(
	artifact eventconnector.SourceArtifact,
) (eventconnector.EventDraft, *eventconnector.ConnectorError) {
	sourceBytes := artifact.Bytes()
	estimateVersion, estimateVersionPresent, err := neutralEstimateVersion(sourceBytes)
	if err != nil {
		return eventconnector.EventDraft{}, referenceAdapterError(err)
	}
	snapshot, err := DecodeSnapshot(sourceBytes)
	if err != nil {
		return eventconnector.EventDraft{}, referenceAdapterError(err)
	}
	if numericErr := neutralValidateSnapshotNumerics(snapshot); numericErr != nil {
		return eventconnector.EventDraft{}, numericErr
	}
	terminalText, connectorErr := eventconnector.FormatMicrosecondTime(snapshot.Terminal.AvailableAt)
	if connectorErr != nil {
		return eventconnector.EventDraft{}, connectorErr
	}
	requestID, connectorErr := eventconnector.NewOpaqueString("event_id", snapshot.RequestID, 1, 128, true)
	if connectorErr != nil {
		return eventconnector.EventDraft{}, connectorErr
	}
	eventType, connectorErr := eventconnector.NewProtocolString("event_type", neutralEventType)
	if connectorErr != nil {
		return eventconnector.EventDraft{}, connectorErr
	}
	schemaRef, connectorErr := eventconnector.NewProtocolString("schema_ref", neutralEventSchemaRef)
	if connectorErr != nil {
		return eventconnector.EventDraft{}, connectorErr
	}
	schemaVersion, connectorErr := eventconnector.NewProtocolString("schema_version", "1")
	if connectorErr != nil {
		return eventconnector.EventDraft{}, connectorErr
	}
	subjectType, connectorErr := eventconnector.NewProtocolString("subject.type", "request")
	if connectorErr != nil {
		return eventconnector.EventDraft{}, connectorErr
	}
	sourceRevision, connectorErr := eventconnector.NewOpaqueString(
		"source_revision",
		artifact.Revision(),
		1,
		64,
		true,
	)
	if connectorErr != nil {
		return eventconnector.EventDraft{}, connectorErr
	}

	orgRef := strconv.FormatInt(snapshot.OrgID, 10)
	projectRef := strconv.FormatInt(snapshot.ProjectID, 10)
	sourceGaps := make(map[string]CaptureGap, len(snapshot.CaptureGaps))
	for _, gap := range snapshot.CaptureGaps {
		sourceGaps[gap.Source] = gap
	}
	gaps := make([]any, 0, len(neutralSharedGapOrder))
	for _, claim := range neutralSharedGapOrder {
		sourceGap, ok := sourceGaps[claim]
		if !ok {
			return eventconnector.EventDraft{}, eventconnector.NewConnectorError(
				"source",
				"missing_capture_gap",
				"normalize",
				"project_completed_execution",
				false,
				"Source Snapshot is missing a required Capture Gap",
				"operator_repair",
				fmt.Errorf("missing completed-execution Capture Gap %q", claim),
			)
		}
		gaps = append(gaps, neutralReferenceCaptureGap(
			snapshot.RequestID,
			orgRef,
			projectRef,
			terminalText,
			sourceGap,
		))
	}

	usage := map[string]any{
		"model":                  snapshot.Usage.Model,
		"prompt_tokens":          strconv.Itoa(snapshot.Usage.PromptTokens),
		"uncached_prompt_tokens": strconv.Itoa(snapshot.Usage.UncachedPromptTokens),
		"cache_read_tokens":      strconv.Itoa(snapshot.Usage.CacheReadTokens),
		"cache_creation_tokens":  strconv.Itoa(snapshot.Usage.CacheCreationTokens),
		"completion_tokens":      strconv.Itoa(snapshot.Usage.CompletionTokens),
		"source":                 snapshot.Usage.Source,
		"authority":              snapshot.Usage.Authority,
		"purpose":                snapshot.Usage.Purpose,
		"finality":               snapshot.Usage.Finality,
		"contract_version":       snapshot.Usage.ContractVersion,
		"cache_usage_schema":     snapshot.Usage.CacheUsageSchema,
		"cache_usage_status":     snapshot.Usage.CacheUsageStatus,
	}
	if estimateVersionPresent {
		usage["estimate_version"] = estimateVersion
	}

	return eventconnector.EventDraft{
		EventID:       requestID,
		EventType:     eventType,
		SchemaRef:     schemaRef,
		SchemaVersion: schemaVersion,
		Scope: eventconnector.EventScope{
			OrgRef:     orgRef,
			ProjectRef: projectRef,
		},
		Subject: eventconnector.EventSubject{
			Type: subjectType,
			Ref:  requestID,
		},
		Actor: map[string]any{"type": "system"},
		Time: map[string]any{
			"occurred_at": terminalText,
			"terminal_at": terminalText,
		},
		Correlation:    map[string]any{"request_ref": snapshot.RequestID},
		SourceRevision: sourceRevision,
		DataPolicy: map[string]any{
			"payload_capture_mode": "metadata_only",
			"raw_payload_present":  false,
		},
		Provenance: map[string]any{
			"adapter_fidelity": "F2",
			"field_groups": map[string]any{
				"actor":                     "declared",
				"correlation":               "direct",
				"data_policy":               "declared",
				"identity":                  "direct",
				"payload.capture_gaps":      "direct",
				"payload.money":             "direct",
				"payload.terminal":          "direct",
				"payload.timeline_complete": "direct",
				"payload.usage":             "direct",
				"scope":                     "direct",
				"subject":                   "derived",
				"time.occurred_at":          "derived",
				"time.terminal_at":          "direct",
			},
		},
		Payload: map[string]any{
			"terminal": map[string]any{"status": snapshot.Terminal.Status},
			"usage":    usage,
			"money": map[string]any{
				"schema":                 snapshot.Money.Schema,
				"currency":               snapshot.Money.Currency,
				"amount_scale":           snapshot.Money.AmountScale,
				"calculated_cost_micros": snapshot.Money.CalculatedCostMicros,
				"charged_cost_micros":    snapshot.Money.ChargedCostMicros,
				"pricing_version":        snapshot.Money.PricingVersion,
			},
			"timeline_complete": true,
			"capture_gaps":      gaps,
		},
	}, nil
}

// BuildNeutralReferenceEnvelopeV1 is the Phase 2B shadow path. It has no
// production caller; the Phase 2A bridge remains the independent compatibility
// oracle until a separately authorized runtime phase.
func BuildNeutralReferenceEnvelopeV1(
	snapshotBytes []byte,
	sourceInstance string,
) (ReferenceEnvelopeV1Result, *eventconnector.ConnectorError) {
	event, err := buildNeutralReferenceEventV1(snapshotBytes, sourceInstance)
	if err != nil {
		return ReferenceEnvelopeV1Result{}, err
	}
	return ReferenceEnvelopeV1Result{
		HashInput:       event.HashInput(),
		CanonicalBytes:  event.HashInputBytes(),
		Envelope:        event.Envelope(),
		EnvelopeBytes:   event.EnvelopeBytes(),
		EnvelopeHash:    event.EnvelopeHash(),
		IdempotencyKey:  event.IdempotencyKey(),
		SourceHash:      event.SourceHash(),
		SourceRequestID: event.EventID(),
	}, nil
}

func buildNeutralReferenceEventV1(
	snapshotBytes []byte,
	sourceInstance string,
) (eventconnector.ImmutableEvent, *eventconnector.ConnectorError) {
	adapter, err := newCompletedExecutionReferenceAdapter()
	if err != nil {
		return eventconnector.ImmutableEvent{}, err
	}
	artifact, err := eventconnector.NewSourceArtifact(
		adapter.Descriptor(),
		sourceInstance,
		SnapshotRevision,
		snapshotBytes,
		eventconnector.StrictJSONOptions{RejectNull: true},
	)
	if err != nil {
		return eventconnector.ImmutableEvent{}, err
	}
	event, err := eventconnector.ProjectAndFinalize(adapter, artifact)
	if err != nil {
		return eventconnector.ImmutableEvent{}, err
	}
	return event, nil
}

func neutralEstimateVersion(payload []byte) (string, bool, error) {
	var source struct {
		Usage struct {
			EstimateVersion json.RawMessage `json:"estimate_version"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(payload, &source); err != nil {
		return "", false, err
	}
	if source.Usage.EstimateVersion == nil {
		return "", false, nil
	}
	var value string
	if err := json.Unmarshal(source.Usage.EstimateVersion, &value); err != nil {
		return "", false, err
	}
	return value, true, nil
}

func neutralReferenceCaptureGap(
	requestID string,
	orgRef string,
	projectRef string,
	observedAt string,
	gap CaptureGap,
) map[string]any {
	return map[string]any{
		"gap_id":          requestID + ":gap:" + gap.Source,
		"parent_event_id": requestID,
		"scope": map[string]any{
			"org_ref":     orgRef,
			"project_ref": projectRef,
		},
		"correlation":          map[string]any{"request_ref": requestID},
		"affected_claim":       gap.Source,
		"gap_type":             "not_available_from_source",
		"reason_code":          gap.Reason,
		"severity":             "warning",
		"observed_at":          observedAt,
		"payload_capture_mode": "none",
		"field_provenance": map[string]any{
			"affected_claim":       "direct",
			"reason_code":          "direct",
			"gap_id":               "derived",
			"parent_event_id":      "derived",
			"scope":                "derived",
			"correlation":          "derived",
			"observed_at":          "derived",
			"gap_type":             "declared",
			"severity":             "declared",
			"payload_capture_mode": "declared",
		},
	}
}

func referenceAdapterError(err error) *eventconnector.ConnectorError {
	if err == nil {
		return nil
	}
	category := "source"
	code := "invalid_source_snapshot"
	message := "Source Snapshot is invalid"
	if strings.Contains(err.Error(), "prohibited raw field") {
		category = "privacy"
		code = "prohibited_raw_field"
		message = "Source payload contains a prohibited raw field"
	}
	return eventconnector.NewConnectorError(
		category,
		code,
		"validate",
		"map_source_snapshot",
		false,
		message,
		"operator_repair",
		err,
	)
}

func neutralValidateSnapshotNumerics(snapshot Snapshot) *eventconnector.ConnectorError {
	values := []int{
		snapshot.Usage.PromptTokens,
		snapshot.Usage.UncachedPromptTokens,
		snapshot.Usage.CacheReadTokens,
		snapshot.Usage.CacheCreationTokens,
		snapshot.Usage.CompletionTokens,
	}
	for _, value := range values {
		if value < 0 || eventconnector.ValidateInt64Decimal(strconv.Itoa(value)) != nil {
			return eventconnector.NewConnectorError(
				"contract",
				"invalid_int64_decimal",
				"validate",
				"map_source_snapshot",
				false,
				"Source token counter is outside the shared int64 decimal profile",
				"operator_repair",
				nil,
			)
		}
	}
	prompt := snapshot.Usage.PromptTokens
	uncached := snapshot.Usage.UncachedPromptTokens
	cacheRead := snapshot.Usage.CacheReadTokens
	cacheCreation := snapshot.Usage.CacheCreationTokens
	if uncached > prompt ||
		cacheRead > prompt-uncached ||
		cacheCreation != prompt-uncached-cacheRead {
		return eventconnector.NewConnectorError(
			"source",
			"invalid_prompt_token_partition",
			"validate",
			"map_source_snapshot",
			false,
			"Source prompt token partition is invalid",
			"operator_repair",
			nil,
		)
	}
	return nil
}

// DecodeNeutralReferenceFeedErrorV1 maps the one pinned negative Feed seed to
// the shared error taxonomy. It is a conformance adapter, not a runtime caller.
func DecodeNeutralReferenceFeedErrorV1(payload []byte) *eventconnector.ConnectorError {
	if err := eventconnector.ValidateStrictJSON(
		payload,
		eventconnector.StrictJSONOptions{RejectNull: true},
	); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var response ErrorResponse
	if err := decoder.Decode(&response); err != nil {
		return eventconnector.NewConnectorError(
			"contract",
			"invalid_feed_error",
			"decode",
			"decode_feed_error",
			false,
			"Feed error payload is invalid",
			"operator_repair",
			err,
		)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return eventconnector.NewConnectorError(
			"contract",
			"invalid_feed_error",
			"decode",
			"decode_feed_error",
			false,
			"Feed error payload contains trailing content",
			"operator_repair",
			err,
		)
	}
	if response.Error.Code != "completed_execution_feed.cursor_expired" ||
		response.Error.Message != "Cursor is outside the retained replay window" ||
		response.Error.Retryable {
		return eventconnector.NewConnectorError(
			"contract",
			"unsupported_feed_error",
			"validate",
			"map_feed_error",
			false,
			"Feed error is outside the pinned Reference profile",
			"operator_repair",
			nil,
		)
	}
	return eventconnector.NewConnectorError(
		"cursor",
		response.Error.Code,
		"fetch",
		"fetch_page",
		false,
		response.Error.Message,
		"operator_repair",
		nil,
	)
}

var _ eventconnector.SourceAdapter = completedExecutionReferenceAdapter{}
