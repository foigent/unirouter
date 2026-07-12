package workloadadmission

import "fmt"

// DurableReservation is the server-owned replay handle persisted by P6-F20.
// It contains no bearer credential and cannot admit new work.
type DurableReservation struct {
	OperationID string `json:"operation_id"`
	OrgID       int64  `json:"org_id"`
	Class       string `json:"class"`
	Weight      int64  `json:"weight"`
	Member      string `json:"member"`
	Fingerprint string `json:"fingerprint"`
	Fallback    bool   `json:"fallback"`
	Idempotent  bool   `json:"idempotent"`
}

func (r *Reservation) Durable() *DurableReservation {
	if r == nil {
		return nil
	}
	return &DurableReservation{
		OperationID: r.OperationID,
		OrgID:       r.OrgID,
		Class:       r.Class,
		Weight:      r.Weight,
		Member:      r.member,
		Fingerprint: r.fingerprint,
		Fallback:    r.Fallback,
		Idempotent:  r.Idempotent,
	}
}

func RestoreDurableReservation(snapshot *DurableReservation) (*Reservation, error) {
	if snapshot == nil {
		return nil, nil
	}
	if snapshot.OperationID == "" || snapshot.OrgID <= 0 || snapshot.Member == "" || snapshot.Fingerprint == "" || snapshot.Weight <= 0 {
		return nil, fmt.Errorf("invalid durable workload reservation")
	}
	return &Reservation{
		OperationID: snapshot.OperationID,
		OrgID:       snapshot.OrgID,
		Class:       snapshot.Class,
		Weight:      snapshot.Weight,
		member:      snapshot.Member,
		fingerprint: snapshot.Fingerprint,
		Fallback:    snapshot.Fallback,
		Idempotent:  snapshot.Idempotent,
	}, nil
}
