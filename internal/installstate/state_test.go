package installstate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/schemaauthority"
)

func testRecord(t *testing.T) Record {
	t.Helper()
	dir := filepath.Join("..", "..", "migrations")
	ledger, err := schemaauthority.ExpectedLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := Artifact{Version: "1.4.1", Commit: strings.Repeat("a", 40), Tree: strings.Repeat("b", 40), ImageDigest: "sha256:" + strings.Repeat("c", 64), ExecutableDigest: "sha256:" + strings.Repeat("d", 64), ManifestDigest: strings.Repeat("e", 64), Ledger: ledger, Files: map[string]string{}}
	for _, id := range ledger {
		wire, err := os.ReadFile(filepath.Join(dir, id+".up.sql"))
		if err != nil {
			t.Fatal(err)
		}
		a.Files[id+".up.sql"] = SHA(wire)
		if strings.Contains(string(wire), "-- unirouter:capability-contract-publication-v1") {
			a.Publications = append(a.Publications, id)
		}
	}
	p := Plan{SchemaVersion: Schema, InstallationID: "ufi_" + strings.Repeat("1", 32), Target: Target{Environment: "synthetic", SystemIdentifier: "123", Database: "fresh", DatabaseOID: 1234, Owner: "test", Role: "test"}, Artifact: a, FreshShape: "empty_database"}
	p.Digest = p.CalculateDigest()
	return Record{Plan: p, State: "active", References: []Reference{}}
}

func TestFreshEnvelopeStrictCanonicalAndHistoricalBinding(t *testing.T) {
	r := testRecord(t)
	wire, err := r.CommentBytes()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := ParseComment(string(wire))
	if err != nil || decoded.Plan.Digest != r.Plan.Digest {
		t.Fatalf("canonical round trip: %v", err)
	}
	for _, invalid := range []string{string(wire) + " ", Prefix + `{"plan":{},"plan":{}}`, strings.Replace(string(wire), `"state":"active"`, `"state":"active","unknown":true`, 1), "unirouter.fresh-install.v2\n{}"} {
		if _, err := ParseComment(invalid); err == nil {
			t.Fatal("accepted malformed recognized envelope")
		}
	}
	if got, err := ParseComment("existing customer annotation"); err != nil || got != nil {
		t.Fatalf("foreign comment became installation authority: %v", err)
	}
	for i, id := range r.Plan.Artifact.Publications {
		r.References = append(r.References, Reference{Publication: id, PlanDigest: SHA([]byte(fmt.Sprint(i))), FileSHA256: strings.Repeat("f", 64), Committed: true})
	}
	r.State = "completed"
	r.Completion = &Completion{LedgerSHA256: LedgerSHA(r.Plan.Artifact.Ledger), MigrationSHA256: "sha256:" + r.Plan.Artifact.Files[r.Plan.Artifact.Ledger[len(r.Plan.Artifact.Ledger)-1]+".up.sql"], ManifestSHA256: r.Plan.Artifact.ManifestDigest}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	r.Completion.ManifestSHA256 = strings.Repeat("0", 64)
	if err := r.Validate(); err == nil {
		t.Fatal("accepted altered historical completion")
	}
}

func TestFreshEnvelopeSupersessionNeverRewritesCommittedPlan(t *testing.T) {
	r := testRecord(t)
	id := r.Plan.Artifact.Publications[0]
	p0 := Reference{Publication: id, PlanDigest: strings.Repeat("a", 64), FileSHA256: strings.Repeat("b", 64)}
	p1 := Reference{Publication: id, PlanDigest: strings.Repeat("c", 64), FileSHA256: strings.Repeat("d", 64), Supersedes: p0.PlanDigest}
	r.References = []Reference{p0, p1}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	r.References[0].Committed = true
	if err := r.Validate(); err == nil {
		t.Fatal("replaced a committed Plan")
	}
	r.References[0].Committed = false
	r.References[1].Supersedes = ""
	if err := r.Validate(); err == nil {
		t.Fatal("lost supersession binding")
	}
	r = testRecord(t)
	r.Plan.Artifact.ExecutableDigest = ""
	r.Plan.Digest = r.Plan.CalculateDigest()
	wire, _ := json.Marshal(r.Plan)
	if _, err := DecodePlan(wire); err == nil {
		t.Fatal("accepted missing executable identity")
	}
}

func TestFreshCompletionRetainsHistoricalM118AndSeparatesM119(t *testing.T) {
	for _, head := range []string{schemaauthority.BudgetSnapshotExpandHead, schemaauthority.ContentCaptureExpandHead, schemaauthority.ContentReclaimExpandHead} {
		t.Run(head, func(t *testing.T) {
			r := testRecord(t)
			for r.Plan.Artifact.Ledger[len(r.Plan.Artifact.Ledger)-1] != head {
				last := r.Plan.Artifact.Ledger[len(r.Plan.Artifact.Ledger)-1]
				r.Plan.Artifact.Ledger = r.Plan.Artifact.Ledger[:len(r.Plan.Artifact.Ledger)-1]
				delete(r.Plan.Artifact.Files, last+".up.sql")
			}
			r.Plan.Digest = r.Plan.CalculateDigest()
			for i, id := range r.Plan.Artifact.Publications {
				r.References = append(r.References, Reference{Publication: id, PlanDigest: SHA([]byte(fmt.Sprint(i))), FileSHA256: strings.Repeat("f", 64), Committed: true})
			}
			r.State = "completed"
			r.Completion = &Completion{LedgerSHA256: LedgerSHA(r.Plan.Artifact.Ledger), MigrationSHA256: "sha256:" + r.Plan.Artifact.Files[head+".up.sql"], ManifestSHA256: r.Plan.Artifact.ManifestDigest}
			wire, err := r.CommentBytes()
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := ParseComment(string(wire))
			if err != nil || decoded.Completion.MigrationSHA256 != r.Completion.MigrationSHA256 {
				t.Fatalf("historical completion changed: %v", err)
			}
			if head == schemaauthority.ContentCaptureExpandHead || head == schemaauthority.ContentReclaimExpandHead {
				r.Completion.MigrationSHA256 = "sha256:" + r.Plan.Artifact.Files[schemaauthority.BudgetSnapshotExpandHead+".up.sql"]
				if err := r.Validate(); err == nil {
					t.Fatal("M118 proof prematurely completes M119 installation")
				}
			}
		})
	}
}
