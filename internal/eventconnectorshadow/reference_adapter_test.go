package eventconnectorshadow

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/completedfeed"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/eventconnector/pullv1"
)

func TestPhase2BThreePathReferenceDifferential(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		fixture        string
		requestID      string
		terminal       string
		canonicalBytes int
		envelopeHash   string
		idempotencyKey string
		sourceHash     string
	}{
		{
			fixture:        "valid-settled.json",
			requestID:      "req_feed_v1_settled",
			terminal:       completedfeed.TerminalSettled,
			canonicalBytes: 4596,
			envelopeHash:   "sha256:0d11f98f981427b7bcf175e82294511ca42981c0ecea8e02f841c3777743d1d2",
			idempotencyKey: "sha256:c2822f0d940c01de77e2e06af1a5f6e1fd10fc66269be20fceb0939150c17b2e",
			sourceHash:     "sha256:5e9eda0a2630d52c5589d38b8ec480604c47c2cc77391b75a57b2a159db6dd27",
		},
		{
			fixture:        "valid-cancelled.json",
			requestID:      "req_feed_v1_cancelled",
			terminal:       completedfeed.TerminalCancelled,
			canonicalBytes: 4629,
			envelopeHash:   "sha256:c31445c779ffec9dae8cbc7f1f0344722a4e0fb696a366b2e321f58d0d07fbab",
			idempotencyKey: "sha256:d342a560ade558edc557f2cd645a526178958540a012ceabe9ca7047e916796c",
			sourceHash:     "sha256:31cbfeb23968c07d158b591c28efb3dd40cffba1fd6d1fd4202f43d63bfe9f2d",
		},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.fixture, func(t *testing.T) {
			t.Parallel()

			payload := readReferenceFixture(t, testCase.fixture)
			exactSource := append([]byte(nil), payload...)

			// Path 1: current production DTO and decoder.
			snapshot, err := completedfeed.DecodeSnapshot(payload)
			if err != nil {
				t.Fatalf("decode current Source DTO: %v", err)
			}
			if snapshot.RequestID != testCase.requestID ||
				snapshot.Terminal.Status != testCase.terminal ||
				len(snapshot.CaptureGaps) != 4 {
				t.Fatalf("legacy Source facts drifted: %#v", snapshot)
			}

			// Path 2: Phase 2A compatibility bridge.
			compatibility, err := completedfeed.BuildReferenceEnvelopeV1(payload, "unirouter-development")
			if err != nil {
				t.Fatalf("build Phase 2A Reference Envelope: %v", err)
			}

			// Path 3: Phase 2B neutral kernel and completed-execution Adapter.
			neutral, connectorErr := BuildNeutralReferenceEnvelopeV1(payload, "unirouter-development")
			if connectorErr != nil {
				t.Fatalf("build Phase 2B neutral Reference Envelope: %v", connectorErr)
			}

			if !bytes.Equal(payload, exactSource) {
				t.Fatal("Source bytes were modified in place")
			}
			if !bytes.Equal(neutral.CanonicalBytes, compatibility.CanonicalBytes) ||
				!bytes.Equal(neutral.EnvelopeBytes, compatibility.EnvelopeBytes) ||
				!reflect.DeepEqual(neutral.HashInput, compatibility.HashInput) ||
				!reflect.DeepEqual(neutral.Envelope, compatibility.Envelope) {
				t.Fatal("Phase 2A and Phase 2B Envelope bytes or objects differ")
			}
			if len(neutral.CanonicalBytes) != testCase.canonicalBytes ||
				neutral.EnvelopeHash != testCase.envelopeHash ||
				neutral.IdempotencyKey != testCase.idempotencyKey ||
				neutral.SourceHash != testCase.sourceHash ||
				neutral.SourceRequestID != testCase.requestID {
				t.Fatalf("neutral vector drift: %#v", neutral)
			}
			if eventconnector.HashSourceBytes(exactSource) != neutral.SourceHash ||
				eventconnector.HashCanonicalEnvelopeBytes(neutral.CanonicalBytes) != neutral.EnvelopeHash {
				t.Fatal("Source and Envelope integrity domains are not exact")
			}
			if compatibility.EnvelopeHash != neutral.EnvelopeHash ||
				compatibility.IdempotencyKey != neutral.IdempotencyKey ||
				compatibility.SourceHash != neutral.SourceHash {
				t.Fatal("Phase 2A and Phase 2B integrity identities differ")
			}
			gaps := neutral.Envelope["payload"].(map[string]any)["capture_gaps"].([]any)
			for index, expectedClaim := range neutralSharedGapOrder {
				if got := gaps[index].(map[string]any)["affected_claim"]; got != expectedClaim {
					t.Fatalf("capture_gaps[%d]=%v want=%s", index, got, expectedClaim)
				}
			}
		})
	}
}

func TestPhase2BOpaqueUTF8Differential(t *testing.T) {
	t.Parallel()

	payload := readReferenceFixture(t, "valid-settled.json")
	payload = []byte(strings.Replace(
		string(payload),
		"req_feed_v1_settled",
		"req_e\u0301",
		1,
	))
	sourceInstance := "unirouter-development"
	compatibility, err := completedfeed.BuildReferenceEnvelopeV1(payload, sourceInstance)
	if err != nil {
		t.Fatalf("build Phase 2A opaque Envelope: %v", err)
	}
	neutral, connectorErr := BuildNeutralReferenceEnvelopeV1(payload, sourceInstance)
	if connectorErr != nil {
		t.Fatalf("build Phase 2B opaque Envelope: %v", connectorErr)
	}
	if !bytes.Equal(neutral.CanonicalBytes, compatibility.CanonicalBytes) ||
		!bytes.Equal(neutral.EnvelopeBytes, compatibility.EnvelopeBytes) {
		t.Fatal("opaque UTF-8 differential bytes differ")
	}
	if neutral.SourceRequestID != "req_e\u0301" ||
		neutral.IdempotencyKey != "sha256:fd256a506369a0fa05dad3ee68f2e577918b97810778e1802d11950112275447" {
		t.Fatalf(
			"opaque identity changed: request=%q idempotency=%s",
			neutral.SourceRequestID,
			neutral.IdempotencyKey,
		)
	}

	sourceInstance = "unirouter-e\u0301"
	compatibility, err = completedfeed.BuildReferenceEnvelopeV1(payload, sourceInstance)
	if err != nil {
		t.Fatalf("build Phase 2A opaque Source instance: %v", err)
	}
	neutral, connectorErr = BuildNeutralReferenceEnvelopeV1(payload, sourceInstance)
	if connectorErr != nil {
		t.Fatalf("build Phase 2B opaque Source instance: %v", connectorErr)
	}
	if !bytes.Equal(neutral.EnvelopeBytes, compatibility.EnvelopeBytes) {
		t.Fatal("opaque Source instance differential bytes differ")
	}
	producer := neutral.Envelope["producer"].(map[string]any)
	if producer["instance"] != sourceInstance {
		t.Fatalf("opaque producer instance=%q want=%q", producer["instance"], sourceInstance)
	}
}

func TestPhase2BNeutralProducerInstanceRejectsUnicodeControls(t *testing.T) {
	t.Parallel()

	payload := readReferenceFixture(t, "valid-settled.json")
	for _, sourceInstance := range []string{
		"unirouter\x00development",
		"unirouter\ndevelopment",
		"unirouter\u0085development",
	} {
		sourceInstance := sourceInstance
		t.Run(strconv.QuoteToASCII(sourceInstance), func(t *testing.T) {
			t.Parallel()

			_, err := BuildNeutralReferenceEnvelopeV1(payload, sourceInstance)
			if err == nil ||
				err.Category != "contract" ||
				err.Code != "invalid_opaque_string" ||
				err.Phase != "validate" ||
				err.Operation != "validate_opaque_string" {
				t.Fatalf("error=%#v", err)
			}
		})
	}
}

func TestPhase2BNegativeReferenceConformance(t *testing.T) {
	t.Parallel()

	valid := readReferenceFixture(t, "valid-settled.json")
	testCases := []struct {
		name      string
		payload   func() []byte
		category  string
		code      string
		phase     string
		operation string
	}{
		{
			name: "invalid-utf8",
			payload: func() []byte {
				payload := append([]byte(nil), valid...)
				index := bytes.Index(payload, []byte("req_feed_v1_settled"))
				payload[index] = 0xff
				return payload
			},
			category:  "contract",
			code:      "invalid_utf8",
			phase:     "decode",
			operation: "decode_source_json",
		},
		{
			name: "unpaired-surrogate",
			payload: func() []byte {
				return []byte(strings.Replace(
					string(valid),
					"req_feed_v1_settled",
					`req_\ud800`,
					1,
				))
			},
			category:  "contract",
			code:      "invalid_unicode_escape",
			phase:     "decode",
			operation: "decode_source_json",
		},
		{
			name: "nested-duplicate-key",
			payload: func() []byte {
				return []byte(strings.Replace(
					string(valid),
					`"status": "settled"`,
					`"status": "settled", "status": "settled"`,
					1,
				))
			},
			category:  "contract",
			code:      "duplicate_json_key",
			phase:     "decode",
			operation: "decode_source_json",
		},
		{
			name: "lossy-null",
			payload: func() []byte {
				return []byte(strings.Replace(
					string(valid),
					`"source": "provider"`,
					`"source": null`,
					1,
				))
			},
			category:  "contract",
			code:      "invalid_source_json",
			phase:     "decode",
			operation: "decode_source_json",
		},
		{
			name: "sub-microsecond-loss",
			payload: func() []byte {
				return []byte(strings.Replace(
					string(valid),
					"2026-07-25T04:05:06Z",
					"2026-07-25T04:05:06.000000001Z",
					1,
				))
			},
			category:  "contract",
			code:      "timestamp_precision_loss",
			phase:     "normalize",
			operation: "normalize_timestamp",
		},
		{
			name:      "privacy-redaction-rejected",
			payload:   func() []byte { return readReferenceFixture(t, "redaction-rejected.json") },
			category:  "privacy",
			code:      "prohibited_raw_field",
			phase:     "validate",
			operation: "map_source_snapshot",
		},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			_, err := BuildNeutralReferenceEnvelopeV1(testCase.payload(), "unirouter-development")
			if err == nil ||
				err.Category != testCase.category ||
				err.Code != testCase.code ||
				err.Phase != testCase.phase ||
				err.Operation != testCase.operation ||
				err.Retryable ||
				err.Recovery != "operator_repair" {
				t.Fatalf("error=%#v", err)
			}
			if validationErr := err.Validate(); validationErr != nil {
				t.Fatalf("invalid Connector Error: %v", validationErr)
			}
		})
	}

	cursorErr := DecodeNeutralReferenceFeedErrorV1(
		readReferenceFixture(t, "error-cursor-expired.json"),
	)
	if cursorErr.Category != "cursor" ||
		cursorErr.Code != "completed_execution_feed.cursor_expired" ||
		cursorErr.Phase != "fetch" ||
		cursorErr.Operation != "fetch_page" ||
		cursorErr.Retryable ||
		cursorErr.SafeMessage != "Cursor is outside the retained replay window" ||
		cursorErr.Recovery != "operator_repair" {
		t.Fatalf("cursor error=%#v", cursorErr)
	}
	if err := cursorErr.Validate(); err != nil {
		t.Fatalf("invalid cursor Connector Error: %v", err)
	}
}

func TestPhase2BReferenceDeliveryIsolation(t *testing.T) {
	t.Parallel()

	event, err := buildNeutralReferenceEventV1(
		readReferenceFixture(t, "valid-settled.json"),
		"unirouter-development",
	)
	if err != nil {
		t.Fatalf("build neutral Event: %v", err)
	}
	eventBytes := event.EnvelopeBytes()
	envelopeHash := event.EnvelopeHash()
	idempotencyKey := event.IdempotencyKey()
	batches := []pullv1.EventBatch{
		{
			BatchVersion:     pullv1.EventBatchVersion,
			ConnectorID:      neutralConnectorID,
			ConnectorVersion: neutralConnectorVersion,
			ProducerInstance: "unirouter-development",
			GeneratedAt:      "2026-07-25T04:05:07.000000Z",
			Events:           []eventconnector.ImmutableEvent{event},
			NextCursor:       "cursor-a",
			HighWatermark:    "high-a",
			HasMore:          true,
		},
		{
			BatchVersion:     pullv1.EventBatchVersion,
			ConnectorID:      neutralConnectorID,
			ConnectorVersion: neutralConnectorVersion,
			ProducerInstance: "unirouter-development",
			GeneratedAt:      "2026-07-25T04:06:08.000000Z",
			Events:           []eventconnector.ImmutableEvent{event},
			NextCursor:       "cursor-b",
			HighWatermark:    "high-b",
			HasMore:          false,
		},
	}
	for index, batch := range batches {
		if err := batch.Validate(); err != nil {
			t.Fatalf("batch %d validation: %v", index, err)
		}
	}
	if !bytes.Equal(event.EnvelopeBytes(), eventBytes) ||
		event.EnvelopeHash() != envelopeHash ||
		event.IdempotencyKey() != idempotencyKey {
		t.Fatal("Batch delivery state changed Reference Event bytes, hash or idempotency")
	}
	for _, prohibited := range []string{
		"source_cursor",
		"next_cursor",
		"cursor",
		"high_watermark",
		"generated_at",
		"has_more",
		"page",
		"checkpoint",
		"acknowledged_sequence",
		"ingested_at",
		"ingestion",
		"retry",
	} {
		if bytes.Contains(eventBytes, []byte(strconv.Quote(prohibited))) {
			t.Fatalf("Reference Event contains delivery field %q", prohibited)
		}
	}
}

func TestPhase2BReferenceRegistryBinding(t *testing.T) {
	t.Parallel()

	payload, err := os.ReadFile(filepath.Join("..", "..", "contracts", "foigent", "connector-registry.pin.v1.json"))
	if err != nil {
		t.Fatalf("read registry pin: %v", err)
	}
	registry, connectorErr := eventconnector.DecodeRegistryPin(payload)
	if connectorErr != nil {
		t.Fatalf("decode registry pin: %v", connectorErr)
	}
	registered, connectorErr := registry.Descriptor(neutralReferenceLocalAdapterID)
	if connectorErr != nil {
		t.Fatalf("resolve registered descriptor: %v", connectorErr)
	}
	if !reflect.DeepEqual(registered.Binding(), neutralReferenceDescriptorBinding()) {
		t.Fatalf(
			"registered descriptor=%#v\nadapter descriptor=%#v",
			registered.Binding(),
			neutralReferenceDescriptorBinding(),
		)
	}
}

func TestPhase2BNoProductionRuntimeCallOrBridgeMutation(t *testing.T) {
	t.Parallel()

	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	bridgePath := filepath.Join(repositoryRoot, "internal", "completedfeed", "reference_bridge.go")
	bridge, err := os.ReadFile(bridgePath)
	if err != nil {
		t.Fatalf("read Phase 2A bridge: %v", err)
	}
	if got, want := eventconnector.HashSourceBytes(bridge), "sha256:d838efc41acbca4b86b04b96ade87031e9028f4568aba039bbcbb6ec14a983c5"; got != want {
		t.Fatalf("Phase 2A bridge bytes changed: %s want=%s", got, want)
	}
	if bytes.Contains(bridge, []byte("eventconnector")) ||
		bytes.Contains(bridge, []byte("NeutralReference")) {
		t.Fatal("Phase 2A bridge now depends on the Phase 2B path")
	}
}

func TestPhase2BProductionEntryDependencyGraphMatchesBase(t *testing.T) {
	const (
		baseCommit = "55302a569ddabc52132b86264b0fc0b047e6a600"
		baseTree   = "af11c18123194a14043a7aa21f0cd1c6acb16f30"
		baseDigest = "sha256:49e50af7c867f6fadf1f2e755a1d90312db6254b00f97b43a2dc4b65b69b2a63"
		template   = `{{if not .Standard}}{{.ImportPath}}	{{join .Imports ","}}{{end}}`
	)

	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}

	command := exec.Command("go", "list", "-deps", "-f", template, "./cmd/...")
	command.Dir = repositoryRoot
	command.Env = append(
		os.Environ(),
		"GOOS=linux",
		"GOARCH=amd64",
		"CGO_ENABLED=0",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("resolve production dependency graph: %v\n%s", err, output)
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	filtered := lines[:0]
	for _, line := range lines {
		if line != "" {
			filtered = append(filtered, line)
		}
	}
	sort.Strings(filtered)
	graph := []byte(strings.Join(filtered, "\n") + "\n")
	if got := eventconnector.HashSourceBytes(graph); got != baseDigest {
		t.Fatalf(
			"linux/amd64 production entry dependency graph differs from base %s tree %s: %s want=%s",
			baseCommit,
			baseTree,
			got,
			baseDigest,
		)
	}
	for _, shadowImport := range []string{
		"github.com/uniroute/unirouter/internal/eventconnector\t",
		"github.com/uniroute/unirouter/internal/eventconnectorshadow\t",
	} {
		if bytes.Contains(graph, []byte(shadowImport)) {
			t.Fatalf("production dependency closure includes Phase 2B package %q", strings.TrimSuffix(shadowImport, "\t"))
		}
	}
}

func readReferenceFixture(t *testing.T, name string) []byte {
	t.Helper()

	path := filepath.Join("..", "..", "contracts", "completed-execution", "v1", "fixtures", name)
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return payload
}
