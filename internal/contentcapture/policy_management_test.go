package contentcapture

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestPolicyManagementPreservesDeploymentStop(t *testing.T) {
	options := policyStoreTestOptions()
	options.DefaultMode = ModeDisabled
	s, err := NewPolicyStore(&policyReadDB{}, options)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{ModeMetadataOnly, ModeFullContent} {
		t.Run(mode, func(t *testing.T) {
			view := testPolicyView()
			if mode == ModeFullContent {
				view.Desired.Config = testFullPolicy()
				view.Effective.Config = testFullPolicy()
			}
			row := append([]any{time.Now()}, policyStoredValues(view)...)
			tx := &policyWriteTx{rows: []pgx.Row{policyTestRow{values: row}}}
			if _, err := s.GetTx(t.Context(), tx, 1, 2, "ready"); !errors.Is(err, ErrPolicyInactive) {
				t.Fatalf("reported stored %s as operational: %v", mode, err)
			}
			if tx.updates != 0 {
				t.Fatal("GET rewrote stored policy")
			}
			tx = &policyWriteTx{}
			put := PolicyPut{SchemaVersion: PolicyPutSchema, ExpectedPolicyVersion: "1", Desired: view.Desired.Config}
			if _, err := s.PutTx(t.Context(), tx, 1, 2, put, PolicyApplication{StorageStatus: "ready", ApplyNow: true, Now: time.Now()}); !errors.Is(err, ErrInvalidPolicy) || len(tx.calls) != 0 {
				t.Fatalf("deployment stop allowed PUT: %v", err)
			}
		})
	}
	// An unchanged disabled default remains readable without materialization.
	tx := &policyWriteTx{rows: []pgx.Row{policyAbsentRow(time.Now())}}
	view, err := s.GetTx(t.Context(), tx, 1, 2, "not_configured")
	if err != nil || view.Effective.Config.Mode != ModeDisabled || tx.updates != 0 {
		t.Fatalf("disabled default unreadable or mutated: %v", err)
	}
}
