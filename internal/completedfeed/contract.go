package completedfeed

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	PageSchemaVersion     = "xinsight.completed-execution-feed.v1"
	SnapshotSchemaVersion = "unirouter.completed-execution.v1"
	SnapshotRevision      = "1"
	MoneySchemaVersion    = "micro-cny.v1"
	MoneyCurrency         = "CNY"
	MoneyAmountScale      = int64(1_000_000)
	MaxPageItems          = 50
	MaxPageBytes          = 8 << 20
	MaxSnapshotBytes      = 64 << 10
	MaxCursorBytes        = 512
)

const (
	TerminalSettled   = "settled"
	TerminalCancelled = "cancelled"
)

const captureGapReason = "not_available_at_billing_terminal"

var requiredCaptureGaps = [...]string{
	"call_log",
	"request_trace",
	"client_ip_presence",
	"upstream_error_presence",
}

type Snapshot struct {
	SchemaVersion string       `json:"schema_version"`
	RequestID     string       `json:"request_id"`
	OrgID         int64        `json:"org_id"`
	ProjectID     int64        `json:"project_id"`
	Terminal      Terminal     `json:"terminal"`
	Usage         Usage        `json:"usage"`
	Money         Money        `json:"money"`
	CaptureGaps   []CaptureGap `json:"capture_gaps"`
}

type Terminal struct {
	Status      string    `json:"status"`
	AvailableAt time.Time `json:"available_at"`
}

type Usage struct {
	Model                string `json:"model"`
	PromptTokens         int    `json:"prompt_tokens"`
	UncachedPromptTokens int    `json:"uncached_prompt_tokens"`
	CacheReadTokens      int    `json:"cache_read_tokens"`
	CacheCreationTokens  int    `json:"cache_creation_tokens"`
	CompletionTokens     int    `json:"completion_tokens"`
	Source               string `json:"source"`
	Authority            string `json:"authority"`
	Purpose              string `json:"purpose"`
	Finality             string `json:"finality"`
	ContractVersion      string `json:"contract_version"`
	EstimateVersion      string `json:"estimate_version,omitempty"`
	CacheUsageSchema     string `json:"cache_usage_schema"`
	CacheUsageStatus     string `json:"cache_usage_status"`
}

type Money struct {
	Schema               string `json:"schema"`
	Currency             string `json:"currency"`
	AmountScale          int64  `json:"amount_scale"`
	CalculatedCostMicros string `json:"calculated_cost_micros"`
	ChargedCostMicros    string `json:"charged_cost_micros"`
	PricingVersion       string `json:"pricing_version"`
}

type CaptureGap struct {
	Source string `json:"source"`
	Reason string `json:"reason"`
}

type FeedItem struct {
	RequestID           string          `json:"request_id"`
	TerminalAvailableAt time.Time       `json:"terminal_available_at"`
	Revision            string          `json:"revision"`
	TimelineComplete    bool            `json:"timeline_complete"`
	SnapshotSHA256      string          `json:"snapshot_sha256"`
	Snapshot            json.RawMessage `json:"snapshot"`
}

type FeedPage struct {
	SchemaVersion  string     `json:"schema_version"`
	SourceInstance string     `json:"source_instance"`
	GeneratedAt    time.Time  `json:"generated_at"`
	Items          []FeedItem `json:"items"`
	NextCursor     string     `json:"next_cursor"`
	HighWatermark  string     `json:"high_watermark"`
	HasMore        bool       `json:"has_more"`
}

type ErrorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

type SnapshotInput struct {
	RequestID            string
	OrgID                int64
	ProjectID            int64
	TerminalStatus       string
	TerminalAvailableAt  time.Time
	Model                string
	PromptTokens         int
	UncachedPromptTokens int
	CacheReadTokens      int
	CacheCreationTokens  int
	CompletionTokens     int
	UsageSource          string
	UsageAuthority       string
	UsagePurpose         string
	UsageFinality        string
	UsageContractVersion string
	UsageEstimateVersion string
	CacheUsageSchema     string
	CacheUsageStatus     string
	CalculatedCostMicros int64
	ChargedCostMicros    int64
	PricingVersion       string
}

func BuildSnapshot(input SnapshotInput) (Snapshot, json.RawMessage, string, error) {
	snapshot := Snapshot{
		SchemaVersion: SnapshotSchemaVersion,
		RequestID:     input.RequestID,
		OrgID:         input.OrgID,
		ProjectID:     input.ProjectID,
		Terminal: Terminal{
			Status:      input.TerminalStatus,
			AvailableAt: input.TerminalAvailableAt.UTC(),
		},
		Usage: Usage{
			Model:                input.Model,
			PromptTokens:         input.PromptTokens,
			UncachedPromptTokens: input.UncachedPromptTokens,
			CacheReadTokens:      input.CacheReadTokens,
			CacheCreationTokens:  input.CacheCreationTokens,
			CompletionTokens:     input.CompletionTokens,
			Source:               input.UsageSource,
			Authority:            input.UsageAuthority,
			Purpose:              input.UsagePurpose,
			Finality:             input.UsageFinality,
			ContractVersion:      input.UsageContractVersion,
			EstimateVersion:      input.UsageEstimateVersion,
			CacheUsageSchema:     input.CacheUsageSchema,
			CacheUsageStatus:     input.CacheUsageStatus,
		},
		Money: Money{
			Schema:               MoneySchemaVersion,
			Currency:             MoneyCurrency,
			AmountScale:          MoneyAmountScale,
			CalculatedCostMicros: strconv.FormatInt(input.CalculatedCostMicros, 10),
			ChargedCostMicros:    strconv.FormatInt(input.ChargedCostMicros, 10),
			PricingVersion:       input.PricingVersion,
		},
		CaptureGaps: make([]CaptureGap, 0, len(requiredCaptureGaps)),
	}
	for _, source := range requiredCaptureGaps {
		snapshot.CaptureGaps = append(snapshot.CaptureGaps, CaptureGap{
			Source: source,
			Reason: captureGapReason,
		})
	}
	if err := ValidateSnapshot(snapshot); err != nil {
		return Snapshot{}, nil, "", err
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return Snapshot{}, nil, "", fmt.Errorf("marshal completed-execution snapshot: %w", err)
	}
	if len(payload) > MaxSnapshotBytes {
		return Snapshot{}, nil, "", fmt.Errorf("completed-execution snapshot exceeds %d bytes", MaxSnapshotBytes)
	}
	digest := sha256.Sum256(payload)
	return snapshot, json.RawMessage(payload), "sha256:" + hex.EncodeToString(digest[:]), nil
}

func DecodeSnapshot(payload []byte) (Snapshot, error) {
	if len(payload) == 0 {
		return Snapshot{}, errors.New("completed-execution snapshot is empty")
	}
	if len(payload) > MaxSnapshotBytes {
		return Snapshot{}, fmt.Errorf("completed-execution snapshot exceeds %d bytes", MaxSnapshotBytes)
	}
	if err := rejectProhibitedKeys(payload); err != nil {
		return Snapshot{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var snapshot Snapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return Snapshot{}, fmt.Errorf("decode completed-execution snapshot: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Snapshot{}, errors.New("completed-execution snapshot contains trailing content")
	}
	if err := ValidateSnapshot(snapshot); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func ValidateSnapshot(snapshot Snapshot) error {
	if snapshot.SchemaVersion != SnapshotSchemaVersion {
		return fmt.Errorf("completed-execution schema_version must be %q", SnapshotSchemaVersion)
	}
	if err := validateBoundedText("request_id", snapshot.RequestID, 1, 64); err != nil {
		return err
	}
	if snapshot.OrgID <= 0 || snapshot.ProjectID <= 0 {
		return errors.New("completed-execution org_id and project_id must be positive")
	}
	if snapshot.Terminal.Status != TerminalSettled && snapshot.Terminal.Status != TerminalCancelled {
		return errors.New("completed-execution terminal status is unsupported")
	}
	if snapshot.Terminal.AvailableAt.IsZero() {
		return errors.New("completed-execution terminal available_at is required")
	}
	_, offset := snapshot.Terminal.AvailableAt.Zone()
	if offset != 0 {
		return errors.New("completed-execution terminal available_at must use UTC")
	}
	if err := validateBoundedText("usage.model", snapshot.Usage.Model, 1, 100); err != nil {
		return err
	}
	for name, value := range map[string]int{
		"usage.prompt_tokens":          snapshot.Usage.PromptTokens,
		"usage.uncached_prompt_tokens": snapshot.Usage.UncachedPromptTokens,
		"usage.cache_read_tokens":      snapshot.Usage.CacheReadTokens,
		"usage.cache_creation_tokens":  snapshot.Usage.CacheCreationTokens,
		"usage.completion_tokens":      snapshot.Usage.CompletionTokens,
	} {
		if value < 0 {
			return fmt.Errorf("%s must be nonnegative", name)
		}
	}
	if snapshot.Usage.UncachedPromptTokens+snapshot.Usage.CacheReadTokens+snapshot.Usage.CacheCreationTokens != snapshot.Usage.PromptTokens {
		return errors.New("completed-execution prompt token partition is invalid")
	}
	for name, bounded := range map[string]struct {
		value string
		max   int
	}{
		"usage.source":             {snapshot.Usage.Source, 32},
		"usage.authority":          {snapshot.Usage.Authority, 64},
		"usage.purpose":            {snapshot.Usage.Purpose, 64},
		"usage.finality":           {snapshot.Usage.Finality, 64},
		"usage.contract_version":   {snapshot.Usage.ContractVersion, 80},
		"usage.estimate_version":   {snapshot.Usage.EstimateVersion, 80},
		"usage.cache_usage_schema": {snapshot.Usage.CacheUsageSchema, 80},
		"usage.cache_usage_status": {snapshot.Usage.CacheUsageStatus, 40},
		"money.pricing_version":    {snapshot.Money.PricingVersion, 80},
	} {
		if err := validateBoundedText(name, bounded.value, 0, bounded.max); err != nil {
			return err
		}
	}
	if snapshot.Money.Schema != MoneySchemaVersion ||
		snapshot.Money.Currency != MoneyCurrency ||
		snapshot.Money.AmountScale != MoneyAmountScale {
		return errors.New("completed-execution money identity is invalid")
	}
	if err := validateCanonicalAmount("money.calculated_cost_micros", snapshot.Money.CalculatedCostMicros); err != nil {
		return err
	}
	if err := validateCanonicalAmount("money.charged_cost_micros", snapshot.Money.ChargedCostMicros); err != nil {
		return err
	}
	if len(snapshot.CaptureGaps) != len(requiredCaptureGaps) {
		return errors.New("completed-execution capture_gaps must contain the fixed V1 set")
	}
	seen := make(map[string]struct{}, len(snapshot.CaptureGaps))
	for _, gap := range snapshot.CaptureGaps {
		if gap.Reason != captureGapReason {
			return errors.New("completed-execution capture gap reason is unsupported")
		}
		seen[gap.Source] = struct{}{}
	}
	for _, source := range requiredCaptureGaps {
		if _, ok := seen[source]; !ok {
			return errors.New("completed-execution capture_gaps is missing a required source")
		}
	}
	if len(seen) != len(requiredCaptureGaps) {
		return errors.New("completed-execution capture_gaps contains a duplicate or unsupported source")
	}
	return nil
}

func validateCanonicalAmount(name, value string) error {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 || strconv.FormatInt(parsed, 10) != value {
		return fmt.Errorf("%s must be a canonical nonnegative integer string", name)
	}
	return nil
}

func validateBoundedText(name, value string, minLength, maxLength int) error {
	if len(value) < minLength || len(value) > maxLength {
		return fmt.Errorf("%s must contain between %d and %d bytes", name, minLength, maxLength)
	}
	for _, char := range value {
		if unicode.IsControl(char) {
			return fmt.Errorf("%s must not contain control characters", name)
		}
	}
	return nil
}

func rejectProhibitedKeys(payload []byte) error {
	var value interface{}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(&value); err != nil {
		return fmt.Errorf("decode completed-execution snapshot: %w", err)
	}
	var walk func(interface{}) bool
	walk = func(current interface{}) bool {
		switch typed := current.(type) {
		case map[string]interface{}:
			for key, child := range typed {
				switch strings.ToLower(key) {
				case "prompt", "response", "authorization", "api_key", "credential", "ip_address", "upstream_error":
					return true
				}
				if walk(child) {
					return true
				}
			}
		case []interface{}:
			for _, child := range typed {
				if walk(child) {
					return true
				}
			}
		}
		return false
	}
	if walk(value) {
		return errors.New("completed-execution snapshot contains a prohibited raw field")
	}
	return nil
}
