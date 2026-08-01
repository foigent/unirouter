package riskcontrol

import "encoding/json"

// Apply stores only in caller-owned memory for deterministic replay tests. No
// production route wires this service to persistence.
func (s *Service) Apply(body []byte) (Report, error) {
	report, err := Evaluate(body)
	if err != nil {
		return Report{}, err
	}
	if s == nil || s.store == nil {
		return Report{}, contractError(ErrInvalid, "memory_store_missing")
	}
	var request ReviewRequest
	if err := json.Unmarshal(body, &request); err != nil {
		return Report{}, contractError(ErrInvalid, "input_shape_invalid")
	}
	key := request.TenantRef + "\x00" + request.ReviewID
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	if current, exists := s.store.reviews[key]; exists {
		if current.semanticHash != report.InputSemanticHash {
			return Report{}, contractError(ErrConflict, "review_identity_conflict")
		}
		duplicate := current.report
		duplicate.ReplayStatus = ReplayDuplicateNoop
		return duplicate, nil
	}
	s.store.reviews[key] = storedReview{
		semanticHash: report.InputSemanticHash,
		report:       report,
	}
	return report, nil
}
