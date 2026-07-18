//go:build ignore

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

type snapshotRowScannerFunc func(dest ...any) error

func (f snapshotRowScannerFunc) Scan(dest ...any) error {
	return f(dest...)
}

func TestLoadApprovalManifestHashesExactBytes(t *testing.T) {
	payload := []byte(`{"schema_version":"unirouter.prd5.xinsight.approval.v1","source_instance":"gate-a","requests":[{"scenario_label":"successful_non_stream","request_id":"req-1","approval_status":"approved","required":true}]}
`)
	path := filepath.Join(t.TempDir(), "approval.json")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	manifest, digest, err := loadApprovalManifest(path)
	if err != nil {
		t.Fatalf("loadApprovalManifest: %v", err)
	}
	if manifest.SourceInstance != "gate-a" {
		t.Fatalf("source_instance=%q", manifest.SourceInstance)
	}
	const expectedDigest = "sha256:b57c4809fce1d16f56c1688251c92779fd023a2309141f61d0134d5983b414c6"
	if digest != expectedDigest {
		t.Fatalf("digest=%q want=%q", digest, expectedDigest)
	}
	if !strings.HasPrefix(digest, "sha256:") || len(digest) != len("sha256:")+64 {
		t.Fatalf("unexpected digest format: %q", digest)
	}
}

func TestLoadApprovalManifestRejectsTrailingJSONObject(t *testing.T) {
	path := filepath.Join(t.TempDir(), "approval.json")
	payload := []byte(`{"schema_version":"unirouter.prd5.xinsight.approval.v1","requests":[]} {}`)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, err := loadApprovalManifest(path)
	if err == nil || !strings.Contains(err.Error(), "exactly one JSON object") {
		t.Fatalf("expected trailing object rejection, got %v", err)
	}
}

func TestRunSnapshotRejectsManifestSourceInstanceOverrideBeforeDSN(t *testing.T) {
	path := filepath.Join(t.TempDir(), "approval.json")
	payload := []byte(`{"schema_version":"unirouter.prd5.xinsight.approval.v1","source_instance":"approved-gate-a","requests":[{"scenario_label":"successful_non_stream","request_id":"req-1","approval_status":"approved","required":true}]}`)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	err := runPRD5Snapshot([]string{
		"--approval-manifest", path,
		"--source-instance", "different-environment",
	})
	if err == nil || !strings.Contains(err.Error(), "must match approval manifest source_instance") {
		t.Fatalf("expected source instance mismatch before DSN lookup, got %v", err)
	}
}

func TestRunSnapshotRequiresManifestSourceInstanceBeforeDSN(t *testing.T) {
	path := filepath.Join(t.TempDir(), "approval.json")
	payload := []byte(`{"schema_version":"unirouter.prd5.xinsight.approval.v1","requests":[{"scenario_label":"successful_non_stream","request_id":"req-1","approval_status":"approved","required":true}]}`)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	err := runPRD5Snapshot([]string{"--approval-manifest", path})
	if err == nil || !strings.Contains(err.Error(), "approval manifest source_instance is required") {
		t.Fatalf("expected missing source instance rejection before DSN lookup, got %v", err)
	}
}

func TestNewExchangeIDFormatAndUniqueness(t *testing.T) {
	pattern := regexp.MustCompile(`^uxchg_[0-9a-f]{32}$`)
	seen := map[string]bool{}
	for i := 0; i < 128; i++ {
		value, err := newExchangeID()
		if err != nil {
			t.Fatalf("newExchangeID: %v", err)
		}
		if !pattern.MatchString(value) {
			t.Fatalf("unexpected exchange_id: %q", value)
		}
		if seen[value] {
			t.Fatalf("duplicate exchange_id: %q", value)
		}
		seen[value] = true
	}
}

func TestSnapshotEnvelopeJSONOmitsManifestAndArtifactHashesInDirectMode(t *testing.T) {
	payload, err := json.Marshal(snapshotFile{
		SchemaVersion:  prd5SnapshotSchemaVersion,
		ExchangeID:     "uxchg_0123456789abcdef0123456789abcdef",
		Source:         "unirouter",
		SourceInstance: "gate-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(payload)
	if !strings.Contains(encoded, `"exchange_id":"uxchg_0123456789abcdef0123456789abcdef"`) {
		t.Fatalf("missing exchange_id: %s", encoded)
	}
	if strings.Contains(encoded, "approval_manifest_sha256") || strings.Contains(encoded, "artifact_sha256") {
		t.Fatalf("unexpected hash in direct-mode snapshot envelope: %s", encoded)
	}
}

func TestSnapshotV2SerializesExactMicroCNYContract(t *testing.T) {
	if prd5SnapshotSchemaVersion != "unirouter.prd5.xinsight.snapshot.v2" {
		t.Fatalf("snapshot schema=%q", prd5SnapshotSchemaVersion)
	}
	query := callLogsQuery(2)
	if !strings.Contains(query, "cost_micros::text") || strings.Contains(query, "cost_cents") || !strings.Contains(query, "IN ($1, $2)") {
		t.Fatalf("unexpected call-log query: %s", query)
	}

	row, err := scanSnapshotCallLog(snapshotCallLogScanner(t, "7"), "synthetic-local")
	if err != nil {
		t.Fatal(err)
	}
	if row.CostMicros != "7" || row.Currency != "CNY" || row.AmountScale != 1_000_000 || row.MoneySchema != "micro-cny.v1" {
		t.Fatalf("mapped money contract=%#v", row)
	}
	payload, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["cost_micros"] != "7" || decoded["currency"] != "CNY" || decoded["amount_scale"] != float64(1_000_000) || decoded["money_schema"] != "micro-cny.v1" {
		t.Fatalf("money contract=%#v", decoded)
	}
	if _, exists := decoded["cost_cents"]; exists {
		t.Fatalf("snapshot v2 must not serialize cost_cents: %#v", decoded)
	}
}

func TestSnapshotV2RejectsNegativeMicroCNYAtProducerBoundary(t *testing.T) {
	_, err := scanSnapshotCallLog(snapshotCallLogScanner(t, "-1"), "synthetic-local")
	if err == nil || !strings.Contains(err.Error(), "canonical non-negative integer text") {
		t.Fatalf("expected negative cost rejection, got %v", err)
	}
}

func snapshotCallLogScanner(t *testing.T, costMicros string) snapshotRowScannerFunc {
	t.Helper()
	return func(dest ...any) error {
		if len(dest) != 38 {
			t.Fatalf("scan destinations=%d want=38", len(dest))
		}
		*dest[0].(*int64) = 41
		*dest[1].(*string) = "req-v2-contract"
		*dest[2].(*int64) = 7
		*dest[6].(*string) = "personal"
		*dest[7].(*string) = "synthetic-model"
		*dest[8].(*string) = "synthetic-model"
		*dest[9].(*string) = "synthetic-provider"
		*dest[10].(*string) = "synthetic-channel"
		*dest[15].(*string) = "api"
		*dest[16].(*string) = "authoritative"
		*dest[17].(*string) = costMicros
		*dest[25].(*time.Time) = time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
		return nil
	}
}

func TestFormatHandoffNotePreservesApprovalManifestMode(t *testing.T) {
	snapshot := snapshotFile{
		SchemaVersion:          prd5SnapshotSchemaVersion,
		ExchangeID:             "uxchg_0123456789abcdef0123456789abcdef",
		ApprovalManifestSHA256: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		GeneratedAt:            "2026-07-10T00:00:00Z",
		Source:                 "unirouter",
		SourceInstance:         "gate-a",
		RequestIDs:             []string{"req-1"},
		Scenarios:              []snapshotScenario{{Label: "successful_non_stream", RequestID: "req-1"}},
		Safety:                 snapshotSafety{SelectionMode: "approved_request_id_manifest"},
	}

	note := formatHandoffNote("snapshot.json", "handoff.md", "approval.json", false, snapshot)
	for _, expected := range []string{
		"Exchange ID: " + snapshot.ExchangeID,
		"Approval Manifest SHA-256: " + snapshot.ApprovalManifestSHA256,
		"--approval-manifest 'approval.json'",
		"(exchange_id, artifact.sha256)",
	} {
		if !strings.Contains(note, expected) {
			t.Fatalf("handoff note missing %q:\n%s", expected, note)
		}
	}
	if strings.Contains(note, "  --request-id ") || strings.Contains(note, "  --scenario ") {
		t.Fatalf("manifest handoff command widened to direct selection:\n%s", note)
	}
}
