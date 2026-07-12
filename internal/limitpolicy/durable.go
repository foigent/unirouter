package limitpolicy

import (
	"fmt"

	"github.com/uniroute/unirouter/internal/workloadadmission"
)

type DurablePolicyReservation struct {
	OperationID    string `json:"operation_id"`
	PolicyRef      string `json:"policy_ref"`
	RuntimeRef     string `json:"runtime_ref"`
	Fingerprint    string `json:"fingerprint"`
	InputTokens    int64  `json:"input_tokens"`
	ReservedTokens int64  `json:"reserved_tokens"`
	TPMLimit       int64  `json:"tpm_limit"`
	Fallback       bool   `json:"fallback"`
	Mode           string `json:"mode"`
}

// DurableReservation is a terminal-only snapshot. It cannot be exchanged for
// new policy or workload admission.
type DurableReservation struct {
	OperationID string                                `json:"operation_id"`
	Items       []DurablePolicyReservation            `json:"items"`
	Idempotent  bool                                  `json:"idempotent"`
	Workload    *workloadadmission.DurableReservation `json:"workload,omitempty"`
}

func (r *Reservation) Durable() *DurableReservation {
	if r == nil {
		return nil
	}
	items := make([]DurablePolicyReservation, 0, len(r.Items))
	for _, item := range r.Items {
		items = append(items, DurablePolicyReservation{
			OperationID: item.OperationID, PolicyRef: item.PolicyRef,
			RuntimeRef: item.RuntimeRef, Fingerprint: item.Fingerprint,
			InputTokens: item.InputTokens, ReservedTokens: item.ReservedTokens,
			TPMLimit: item.TPMLimit, Fallback: item.Fallback, Mode: item.Mode,
		})
	}
	return &DurableReservation{
		OperationID: r.OperationID,
		Items:       items,
		Idempotent:  r.Idempotent,
		Workload:    r.workload.Durable(),
	}
}

func RestoreDurableReservation(snapshot *DurableReservation) (*Reservation, error) {
	if snapshot == nil || snapshot.OperationID == "" || len(snapshot.OperationID) > 64 {
		return nil, fmt.Errorf("invalid durable policy reservation")
	}
	items := make([]policyReservation, 0, len(snapshot.Items))
	for _, item := range snapshot.Items {
		items = append(items, policyReservation{
			OperationID: item.OperationID, PolicyRef: item.PolicyRef,
			RuntimeRef: item.RuntimeRef, Fingerprint: item.Fingerprint,
			InputTokens: item.InputTokens, ReservedTokens: item.ReservedTokens,
			TPMLimit: item.TPMLimit, Fallback: item.Fallback, Mode: item.Mode,
		})
	}
	workload, err := workloadadmission.RestoreDurableReservation(snapshot.Workload)
	if err != nil {
		return nil, err
	}
	reservation := &Reservation{
		OperationID: snapshot.OperationID,
		Items:       items,
		Idempotent:  snapshot.Idempotent,
		workload:    workload,
	}
	if !validReservation(reservation) {
		return nil, fmt.Errorf("invalid durable policy reservation")
	}
	return reservation, nil
}
