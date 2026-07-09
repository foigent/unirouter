//go:build ignore

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

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
