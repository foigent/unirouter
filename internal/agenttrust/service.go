package agenttrust

import "errors"

// Apply mutates only an isolated in-memory conformance ledger. It is not
// production authority, policy, release, receipt, or revocation state.
func (s *Service) Apply(body []byte) (Report, error) {
	if s == nil || s.store == nil {
		return Report{}, contractError(ErrInvalid, "store_missing")
	}
	report, err := Evaluate(body)
	if err != nil {
		return Report{}, err
	}
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	if current, exists := s.store.reviews[report.ReviewID]; exists {
		if current.semanticHash != report.InputSemanticHash {
			return Report{}, contractError(ErrConflict, "review_identity_conflict")
		}
		duplicate := current.report
		duplicate.ReplayStatus = ReplayDuplicateNoop
		return duplicate, nil
	}
	report.ReplayStatus = ReplayAccepted
	s.store.reviews[report.ReviewID] = storedReview{
		semanticHash: report.InputSemanticHash,
		report:       report,
	}
	return report, nil
}

func IsConflict(err error) bool {
	return errors.Is(err, ErrConflict)
}
