package limitpolicy

import (
	"encoding/json"
	"testing"

	"github.com/uniroute/unirouter/internal/workloadadmission"
)

func TestP6F20DurablePolicyReservationRoundTrip(t *testing.T) {
	workload, err := workloadadmission.RestoreDurableReservation(&workloadadmission.DurableReservation{
		OperationID: "op_p6f20_policy",
		OrgID:       7,
		Class:       workloadadmission.ClassStream,
		Weight:      4,
		Member:      "operation-member",
		Fingerprint: "workload-fingerprint",
	})
	if err != nil {
		t.Fatal(err)
	}
	original := &Reservation{
		OperationID: "op_p6f20_policy",
		Items: []policyReservation{{
			OperationID:    "op_p6f20_policy",
			PolicyRef:      "policy:org:7",
			RuntimeRef:     "runtime:policy:7",
			Fingerprint:    "policy-fingerprint",
			InputTokens:    10,
			ReservedTokens: 30,
			TPMLimit:       100,
			Mode:           ModeEnforce,
		}},
		workload: workload,
	}
	payload, err := json.Marshal(original.Durable())
	if err != nil {
		t.Fatal(err)
	}
	var snapshot DurableReservation
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		t.Fatal(err)
	}
	restored, err := RestoreDurableReservation(&snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if restored.OperationID != original.OperationID || len(restored.Items) != 1 || restored.workload == nil {
		t.Fatalf("restored reservation = %#v", restored)
	}
	item := restored.Items[0]
	if item.PolicyRef != "policy:org:7" || item.RuntimeRef != "runtime:policy:7" ||
		item.Fingerprint != "policy-fingerprint" || item.InputTokens != 10 ||
		item.ReservedTokens != 30 || item.TPMLimit != 100 || item.Mode != ModeEnforce {
		t.Fatalf("restored policy item = %#v", item)
	}
}
