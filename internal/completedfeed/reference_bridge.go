package completedfeed

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	foigentEnvelopeSpecVersion = "foigent.event-envelope.v1"
	foigentEventType           = "model.request.completed"
	foigentEventSchemaRef      = "foigent://events/model/request-completed/v1"
	foigentConnectorID         = "foigent.unirouter.completed-execution"
	foigentConnectorVersion    = "1.0.0"
	foigentIdempotencyDomain   = "foigent.event-idempotency.v1"
)

var foigentSharedGapOrder = [...]string{
	"call_log",
	"client_ip_presence",
	"request_trace",
	"upstream_error_presence",
}

// ReferenceEnvelopeV1Result is the Phase 2A compatibility representation of a
// current completed-execution snapshot. It is deliberately not wired into the
// live Feed: the existing page, credential, checkpoint and persistence paths
// remain authoritative until a separately authorized runtime transition.
type ReferenceEnvelopeV1Result struct {
	HashInput       map[string]any
	CanonicalBytes  []byte
	Envelope        map[string]any
	EnvelopeBytes   []byte
	EnvelopeHash    string
	IdempotencyKey  string
	SourceHash      string
	SourceRequestID string
}

// BuildReferenceEnvelopeV1 maps exact current Source bytes to the shared
// Foigent Event Envelope V1. Delivery cursor, page time, retry state and
// ingestion time are intentionally absent.
func BuildReferenceEnvelopeV1(snapshotBytes []byte, sourceInstance string) (ReferenceEnvelopeV1Result, error) {
	if !utf8.Valid(snapshotBytes) {
		return ReferenceEnvelopeV1Result{}, errors.New("completed-execution source bytes must be valid UTF-8")
	}
	if err := rejectReferenceDuplicateJSONKeys(snapshotBytes); err != nil {
		return ReferenceEnvelopeV1Result{}, err
	}
	if err := validateBoundedText("producer.instance", sourceInstance, 1, 128); err != nil {
		return ReferenceEnvelopeV1Result{}, err
	}
	if !utf8.ValidString(sourceInstance) {
		return ReferenceEnvelopeV1Result{}, errors.New("producer.instance must be valid UTF-8")
	}
	snapshot, err := DecodeSnapshot(snapshotBytes)
	if err != nil {
		return ReferenceEnvelopeV1Result{}, err
	}
	terminalAt := snapshot.Terminal.AvailableAt.UTC()
	if !terminalAt.Equal(terminalAt.Truncate(time.Microsecond)) {
		return ReferenceEnvelopeV1Result{}, errors.New("completed-execution terminal time loses sub-microsecond precision")
	}
	terminalText := terminalAt.Format("2006-01-02T15:04:05.000000Z")
	orgRef := strconv.FormatInt(snapshot.OrgID, 10)
	projectRef := strconv.FormatInt(snapshot.ProjectID, 10)
	idempotencyKey, err := referenceIdempotencyKey(
		"unirouter",
		sourceInstance,
		"",
		orgRef,
		projectRef,
		foigentEventType,
		snapshot.RequestID,
	)
	if err != nil {
		return ReferenceEnvelopeV1Result{}, err
	}
	sourceDigest := sha256.Sum256(snapshotBytes)
	sourceHash := "sha256:" + hex.EncodeToString(sourceDigest[:])

	sourceGaps := make(map[string]CaptureGap, len(snapshot.CaptureGaps))
	for _, gap := range snapshot.CaptureGaps {
		sourceGaps[gap.Source] = gap
	}
	gaps := make([]any, 0, len(foigentSharedGapOrder))
	for _, claim := range foigentSharedGapOrder {
		sourceGap, ok := sourceGaps[claim]
		if !ok {
			return ReferenceEnvelopeV1Result{}, fmt.Errorf("completed-execution source gap %q is missing", claim)
		}
		gaps = append(gaps, referenceCaptureGap(
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
	if snapshot.Usage.EstimateVersion != "" {
		usage["estimate_version"] = snapshot.Usage.EstimateVersion
	}

	hashInput := map[string]any{
		"spec_version":   foigentEnvelopeSpecVersion,
		"event_id":       snapshot.RequestID,
		"event_type":     foigentEventType,
		"schema_ref":     foigentEventSchemaRef,
		"schema_version": "1",
		"producer": map[string]any{
			"product":           "unirouter",
			"instance":          sourceInstance,
			"connector_id":      foigentConnectorID,
			"connector_version": foigentConnectorVersion,
		},
		"scope": map[string]any{
			"org_ref":     orgRef,
			"project_ref": projectRef,
		},
		"subject": map[string]any{
			"type": "request",
			"ref":  snapshot.RequestID,
		},
		"actor": map[string]any{"type": "system"},
		"time": map[string]any{
			"occurred_at": terminalText,
			"terminal_at": terminalText,
		},
		"correlation": map[string]any{"request_ref": snapshot.RequestID},
		"identity": map[string]any{
			"idempotency_key": idempotencyKey,
			"source_revision": SnapshotRevision,
		},
		"integrity": map[string]any{"source_hash": sourceHash},
		"data_policy": map[string]any{
			"payload_capture_mode": "metadata_only",
			"raw_payload_present":  false,
		},
		"provenance": map[string]any{
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
		"payload": map[string]any{
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
	}
	canonicalBytes, err := canonicalReferenceJSON(hashInput)
	if err != nil {
		return ReferenceEnvelopeV1Result{}, err
	}
	envelopeDigest := sha256.Sum256(canonicalBytes)
	envelopeHash := "sha256:" + hex.EncodeToString(envelopeDigest[:])
	envelope := cloneReferenceMap(hashInput)
	envelope["integrity"].(map[string]any)["envelope_hash"] = envelopeHash
	envelopeBytes, err := canonicalReferenceJSON(envelope)
	if err != nil {
		return ReferenceEnvelopeV1Result{}, err
	}
	return ReferenceEnvelopeV1Result{
		HashInput:       hashInput,
		CanonicalBytes:  canonicalBytes,
		Envelope:        envelope,
		EnvelopeBytes:   envelopeBytes,
		EnvelopeHash:    envelopeHash,
		IdempotencyKey:  idempotencyKey,
		SourceHash:      sourceHash,
		SourceRequestID: snapshot.RequestID,
	}, nil
}

func rejectReferenceDuplicateJSONKeys(payload []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := walkReferenceJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err != nil {
			return fmt.Errorf("decode completed-execution source JSON: %w", err)
		}
		return errors.New("completed-execution source JSON contains trailing content")
	}
	return nil
}

func walkReferenceJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("decode completed-execution source JSON: %w", err)
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("decode completed-execution source JSON object key: %w", err)
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("completed-execution source JSON object key is not a string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("completed-execution source JSON contains duplicate key %q", key)
			}
			seen[key] = struct{}{}
			if err := walkReferenceJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("decode completed-execution source JSON object: %w", err)
		}
		if end != json.Delim('}') {
			return errors.New("completed-execution source JSON object is not closed")
		}
	case '[':
		for decoder.More() {
			if err := walkReferenceJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("decode completed-execution source JSON array: %w", err)
		}
		if end != json.Delim(']') {
			return errors.New("completed-execution source JSON array is not closed")
		}
	default:
		return errors.New("completed-execution source JSON contains an unexpected delimiter")
	}
	return nil
}

func referenceCaptureGap(requestID, orgRef, projectRef, observedAt string, gap CaptureGap) map[string]any {
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

func referenceIdempotencyKey(parts ...string) (string, error) {
	var input bytes.Buffer
	input.WriteString(foigentIdempotencyDomain)
	for _, part := range parts {
		if !utf8.ValidString(part) {
			return "", errors.New("Foigent idempotency component must be valid UTF-8")
		}
		if uint64(len(part)) > uint64(^uint32(0)) {
			return "", errors.New("Foigent idempotency component exceeds uint32 bytes")
		}
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(part)))
		input.Write(length[:])
		input.WriteString(part)
	}
	digest := sha256.Sum256(input.Bytes())
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func cloneReferenceMap(input map[string]any) map[string]any {
	cloned := make(map[string]any, len(input))
	for key, value := range input {
		if nested, ok := value.(map[string]any); ok {
			cloned[key] = cloneReferenceMap(nested)
			continue
		}
		cloned[key] = value
	}
	return cloned
}

func canonicalReferenceJSON(value any) ([]byte, error) {
	var output bytes.Buffer
	if err := appendCanonicalReferenceJSON(&output, value); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func appendCanonicalReferenceJSON(output *bytes.Buffer, value any) error {
	switch typed := value.(type) {
	case nil:
		output.WriteString("null")
	case bool:
		if typed {
			output.WriteString("true")
		} else {
			output.WriteString("false")
		}
	case string:
		appendCanonicalReferenceString(output, typed)
	case int:
		output.WriteString(strconv.Itoa(typed))
	case int64:
		output.WriteString(strconv.FormatInt(typed, 10))
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(left, right int) bool {
			return compareUTF16(keys[left], keys[right]) < 0
		})
		output.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				output.WriteByte(',')
			}
			appendCanonicalReferenceString(output, key)
			output.WriteByte(':')
			if err := appendCanonicalReferenceJSON(output, typed[key]); err != nil {
				return err
			}
		}
		output.WriteByte('}')
	case []any:
		output.WriteByte('[')
		for index, item := range typed {
			if index > 0 {
				output.WriteByte(',')
			}
			if err := appendCanonicalReferenceJSON(output, item); err != nil {
				return err
			}
		}
		output.WriteByte(']')
	default:
		return fmt.Errorf("unsupported canonical Foigent JSON type %T", value)
	}
	return nil
}

func appendCanonicalReferenceString(output *bytes.Buffer, value string) {
	output.WriteByte('"')
	for _, character := range value {
		switch character {
		case '"', '\\':
			output.WriteByte('\\')
			output.WriteRune(character)
		case '\b':
			output.WriteString(`\b`)
		case '\t':
			output.WriteString(`\t`)
		case '\n':
			output.WriteString(`\n`)
		case '\f':
			output.WriteString(`\f`)
		case '\r':
			output.WriteString(`\r`)
		default:
			if character < 0x20 {
				fmt.Fprintf(output, `\u%04x`, character)
			} else {
				output.WriteRune(character)
			}
		}
	}
	output.WriteByte('"')
}

func compareUTF16(left, right string) int {
	leftUnits := utf16.Encode([]rune(left))
	rightUnits := utf16.Encode([]rune(right))
	limit := len(leftUnits)
	if len(rightUnits) < limit {
		limit = len(rightUnits)
	}
	for index := 0; index < limit; index++ {
		switch {
		case leftUnits[index] < rightUnits[index]:
			return -1
		case leftUnits[index] > rightUnits[index]:
			return 1
		}
	}
	switch {
	case len(leftUnits) < len(rightUnits):
		return -1
	case len(leftUnits) > len(rightUnits):
		return 1
	default:
		return 0
	}
}
