package completedfeed

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReferenceEnvelopeGoldenCompatibility(t *testing.T) {
	testCases := []struct {
		fixture          string
		canonicalBytes   int
		envelopeHash     string
		idempotencyKey   string
		sourceHash       string
		expectedTerminal string
	}{
		{
			fixture:          "valid-settled.json",
			canonicalBytes:   4596,
			envelopeHash:     "sha256:0d11f98f981427b7bcf175e82294511ca42981c0ecea8e02f841c3777743d1d2",
			idempotencyKey:   "sha256:c2822f0d940c01de77e2e06af1a5f6e1fd10fc66269be20fceb0939150c17b2e",
			sourceHash:       "sha256:5e9eda0a2630d52c5589d38b8ec480604c47c2cc77391b75a57b2a159db6dd27",
			expectedTerminal: TerminalSettled,
		},
		{
			fixture:          "valid-cancelled.json",
			canonicalBytes:   4629,
			envelopeHash:     "sha256:c31445c779ffec9dae8cbc7f1f0344722a4e0fb696a366b2e321f58d0d07fbab",
			idempotencyKey:   "sha256:d342a560ade558edc557f2cd645a526178958540a012ceabe9ca7047e916796c",
			sourceHash:       "sha256:31cbfeb23968c07d158b591c28efb3dd40cffba1fd6d1fd4202f43d63bfe9f2d",
			expectedTerminal: TerminalCancelled,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.fixture, func(t *testing.T) {
			payload := readReferenceFixture(t, testCase.fixture)
			result, err := BuildReferenceEnvelopeV1(payload, "unirouter-development")
			if err != nil {
				t.Fatalf("build Reference Envelope: %v", err)
			}
			if len(result.CanonicalBytes) != testCase.canonicalBytes {
				t.Fatalf("canonical bytes=%d want=%d", len(result.CanonicalBytes), testCase.canonicalBytes)
			}
			if result.EnvelopeHash != testCase.envelopeHash {
				t.Fatalf("envelope hash=%s want=%s", result.EnvelopeHash, testCase.envelopeHash)
			}
			if result.IdempotencyKey != testCase.idempotencyKey {
				t.Fatalf("idempotency=%s want=%s", result.IdempotencyKey, testCase.idempotencyKey)
			}
			if result.SourceHash != testCase.sourceHash {
				t.Fatalf("source hash=%s want=%s", result.SourceHash, testCase.sourceHash)
			}
			digest := sha256.Sum256(result.CanonicalBytes)
			if got := "sha256:" + hex.EncodeToString(digest[:]); got != result.EnvelopeHash {
				t.Fatalf("canonical digest=%s envelope hash=%s", got, result.EnvelopeHash)
			}
			integrity := result.Envelope["integrity"].(map[string]any)
			if integrity["envelope_hash"] != result.EnvelopeHash ||
				integrity["source_hash"] != result.SourceHash {
				t.Fatalf("integrity=%#v", integrity)
			}
			payloadMap := result.Envelope["payload"].(map[string]any)
			terminal := payloadMap["terminal"].(map[string]any)
			if terminal["status"] != testCase.expectedTerminal {
				t.Fatalf("terminal status=%v want=%s", terminal["status"], testCase.expectedTerminal)
			}
			if len(payloadMap["capture_gaps"].([]any)) != 4 {
				t.Fatalf("capture gaps=%d want=4", len(payloadMap["capture_gaps"].([]any)))
			}
		})
	}
}

func TestReferenceEnvelopeExcludesDeliveryAndOperationalState(t *testing.T) {
	result, err := BuildReferenceEnvelopeV1(readReferenceFixture(t, "valid-settled.json"), "unirouter-development")
	if err != nil {
		t.Fatalf("build Reference Envelope: %v", err)
	}
	encoded := string(result.EnvelopeBytes)
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
		if strings.Contains(encoded, prohibited) {
			t.Fatalf("Envelope contains operational field %q", prohibited)
		}
	}
}

func TestReferenceEnvelopeRejectsSubMicrosecondTerminal(t *testing.T) {
	payload := readReferenceFixture(t, "valid-settled.json")
	payload = []byte(strings.Replace(
		string(payload),
		"2026-07-25T04:05:06Z",
		"2026-07-25T04:05:06.000000001Z",
		1,
	))
	if _, err := BuildReferenceEnvelopeV1(payload, "unirouter-development"); err == nil ||
		!strings.Contains(err.Error(), "sub-microsecond") {
		t.Fatalf("error=%v want sub-microsecond rejection", err)
	}
}

func TestReferenceEnvelopePreservesOpaqueNonNFCBytesInIdentity(t *testing.T) {
	payload := readReferenceFixture(t, "valid-settled.json")
	payload = []byte(strings.Replace(
		string(payload),
		"req_feed_v1_settled",
		"req_e\u0301",
		1,
	))
	result, err := BuildReferenceEnvelopeV1(payload, "unirouter-development")
	if err != nil {
		t.Fatalf("build opaque Reference Envelope: %v", err)
	}
	if result.SourceRequestID != "req_e\u0301" ||
		result.Envelope["event_id"] != "req_e\u0301" ||
		hex.EncodeToString([]byte(result.SourceRequestID)) != "7265715f65cc81" {
		t.Fatalf("opaque request ID changed: %q", result.SourceRequestID)
	}
	if result.IdempotencyKey != "sha256:fd256a506369a0fa05dad3ee68f2e577918b97810778e1802d11950112275447" {
		t.Fatalf("idempotency=%s", result.IdempotencyKey)
	}
	digest := sha256.Sum256(payload)
	if got := "sha256:" + hex.EncodeToString(digest[:]); result.SourceHash != got {
		t.Fatalf("source hash=%s want=%s", result.SourceHash, got)
	}
}

func TestReferenceEnvelopePreservesOpaqueNonNFCSourceInstance(t *testing.T) {
	sourceInstance := "unirouter-e\u0301"
	result, err := BuildReferenceEnvelopeV1(
		readReferenceFixture(t, "valid-settled.json"),
		sourceInstance,
	)
	if err != nil {
		t.Fatalf("build opaque Source instance: %v", err)
	}
	producer := result.Envelope["producer"].(map[string]any)
	if producer["instance"] != sourceInstance {
		t.Fatalf("producer.instance=%q want=%q", producer["instance"], sourceInstance)
	}
}

func TestReferenceEnvelopeRejectsInvalidSourceUTF8(t *testing.T) {
	payload := readReferenceFixture(t, "valid-settled.json")
	payload = append([]byte(nil), payload...)
	index := strings.Index(string(payload), "req_feed_v1_settled")
	if index < 0 {
		t.Fatal("fixture request_id not found")
	}
	payload[index] = 0xff
	if _, err := BuildReferenceEnvelopeV1(payload, "unirouter-development"); err == nil ||
		!strings.Contains(err.Error(), "valid UTF-8") {
		t.Fatalf("error=%v want invalid UTF-8 rejection", err)
	}
}

func TestReferenceEnvelopeRejectsNestedDuplicateJSONKey(t *testing.T) {
	payload := readReferenceFixture(t, "valid-settled.json")
	payload = []byte(strings.Replace(
		string(payload),
		`"status": "settled"`,
		`"status": "settled", "status": "settled"`,
		1,
	))
	if _, err := BuildReferenceEnvelopeV1(payload, "unirouter-development"); err == nil ||
		!strings.Contains(err.Error(), `duplicate key "status"`) {
		t.Fatalf("error=%v want duplicate-key rejection", err)
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
