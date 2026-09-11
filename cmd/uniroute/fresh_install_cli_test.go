package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/uniroute/unirouter/internal/installstate"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
)

func TestFreshPublicationOrdinaryPrefixRecoveryBoundaries(t *testing.T) {
	p := publication.Plan{ExpectedLedger: []string{"M106"}, ApplyMigrations: []string{"M107", "M108"}, ProposedLedger: []string{"M106", "M107", "M108"}}
	if err := ordinaryPublicationPrefix(p, []string{"M106", "M107"}); err != nil {
		t.Fatal(err)
	}
	for _, ledger := range [][]string{nil, {"M106"}, {"M106", "M108"}, {"M106", "M107", "M108"}, {"M106", "M107", "M108", "M109"}, {"foreign", "M107"}} {
		if err := ordinaryPublicationPrefix(p, ledger); err == nil {
			t.Fatalf("accepted unowned movement: %v", ledger)
		}
	}
}

func TestFreshPlanFilesPreserveOrphansAndRefuseReplacement(t *testing.T) {
	dir := t.TempDir()
	ref := installstate.Reference{Publication: "000108_example", PlanDigest: installstate.SHA([]byte("plan")), FileSHA256: installstate.SHA([]byte("original"))}
	if err := persistFreshPublication(dir, ref, []byte("original")); err != nil {
		t.Fatal(err)
	}
	if err := persistFreshPublication(dir, ref, []byte("original")); err != nil {
		t.Fatal(err)
	}
	if err := persistFreshPublication(dir, ref, []byte("replacement")); err == nil {
		t.Fatal("overwrote immutable Plan")
	}
	path := freshPublicationPath(dir, ref)
	wire, err := readFreshFile(path, 64)
	if err != nil || string(wire) != "original" {
		t.Fatalf("original Plan lost: %v", err)
	}
	if _, err := readFreshFile(path, 2); err == nil {
		t.Fatal("accepted oversized Plan")
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readFreshFile(link, 64); err == nil {
		t.Fatal("followed Plan symlink")
	}
}
