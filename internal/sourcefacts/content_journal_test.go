package sourcefacts

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"testing"
)

// Embedding pgx.Tx implements DBQuerier; Begin must never be called by the C
// appender, or it would merely open a savepoint in somebody else's transaction.
type syntheticFinancialTransaction struct{ pgx.Tx }

func (syntheticFinancialTransaction) Begin(context.Context) (pgx.Tx, error) {
	panic("financial transaction was reused")
}
func TestContentJournalRejectsTransactionAndDisabledUnboundCapture(t *testing.T) {
	m := syntheticContentManifest(nil)
	s := &Service{db: syntheticFinancialTransaction{}, options: Options{Enabled: true, ContentEnabled: true, SourceInstance: m.SourceInstance}}
	if _, err := s.AppendContentManifest(t.Context(), m, 1); !errors.Is(err, ErrContentJournalTransaction) {
		t.Fatalf("financial transaction gate: %v", err)
	}
	if _, err := s.AppendContentManifest(t.Context(), m, 0); !errors.Is(err, ErrContentJournalScope) {
		t.Fatalf("missing epoch: %v", err)
	}
	m.SourceInstance = "other"
	if _, err := s.AppendContentManifest(t.Context(), m, 1); !errors.Is(err, ErrContentJournalScope) {
		t.Fatalf("scope gate: %v", err)
	}
	s.options.ContentEnabled = false
	if _, err := s.AppendContentManifest(t.Context(), m, 1); !errors.Is(err, ErrContentJournalDisabled) {
		t.Fatalf("disabled gate: %v", err)
	}
	var absent *Service
	if _, err := absent.AppendContentManifest(t.Context(), m, 1); !errors.Is(err, ErrContentJournalDisabled) {
		t.Fatalf("nil service gate: %v", err)
	}
}
