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
		expectedTerminal string
	}{
		{
			fixture:          "valid-settled.json",
			canonicalBytes:   4596,
			envelopeHash:     "sha256:0d11f98f981427b7bcf175e82294511ca42981c0ecea8e02f841c3777743d1d2",
			idempotencyKey:   "sha256:c2822f0d940c01de77e2e06af1a5f6e1fd10fc66269be20fceb0939150c17b2e",
			expectedTerminal: TerminalSettled,
		},
		{
			fixture:          "valid-cancelled.json",
			canonicalBytes:   4629,
			envelopeHash:     "sha256:c31445c779ffec9dae8cbc7f1f0344722a4e0fb696a366b2e321f58d0d07fbab",
			idempotencyKey:   "sha256:d342a560ade558edc557f2cd645a526178958540a012ceabe9ca7047e916796c",
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
		"high_watermark",
		"generated_at",
		"ingested_at",
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
	idempotency, err := referenceIdempotencyKey(
		"unirouter",
		"unirouter-development",
		"",
		"101",
		"202",
		foigentEventType,
		"req_e\u0301",
	)
	if err != nil {
		t.Fatalf("build opaque identity: %v", err)
	}
	if idempotency != "sha256:fd256a506369a0fa05dad3ee68f2e577918b97810778e1802d11950112275447" {
		t.Fatalf("idempotency=%s", idempotency)
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
