package workloadadmission

import (
	"encoding/json"
	"testing"
)

func TestP6F20DurableWorkloadReservationRoundTrip(t *testing.T) {
	original := &Reservation{
		OperationID: "op_p6f20_workload",
		OrgID:       7,
		Class:       ClassStream,
		Weight:      4,
		member:      "operation-member",
		fingerprint: "reservation-fingerprint",
		Idempotent:  true,
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
	if restored.OperationID != original.OperationID || restored.OrgID != original.OrgID ||
		restored.Class != original.Class || restored.Weight != original.Weight ||
		restored.member != original.member || restored.fingerprint != original.fingerprint ||
		restored.Idempotent != original.Idempotent {
		t.Fatalf("restored reservation = %#v", restored)
	}
}
