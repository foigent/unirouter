package billing

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// TerminalRecord is the allowlisted billing-terminal envelope available to
// asynchronous product read models. It deliberately carries no prompt,
// response, credential, network address, or raw upstream error.
type TerminalRecord struct {
	BillingMode          string
	BillingModeSource    string
	EventKey             string
	RequestID            string
	OrgID                int64
	ProjectID            int64
	Status               string
	Model                string
	PromptTokens         int
	UncachedPromptTokens int
	CacheReadTokens      int
	CacheCreationTokens  int
	CompletionTokens     int
	UsageSource          string
	UsageAuthority       string
	UsagePurpose         string
	UsageFinality        string
	UsageContractVersion string
	UsageEstimateVersion string
	CacheUsageSchema     string
	CacheUsageStatus     string
	CalculatedCostMicros int64
	ChargedCostMicros    int64
	PricingVersion       string
	OccurredAt           time.Time
}

// TerminalMaterializer joins the usage journal transaction so an outbox retry
// can never publish a feed item without the corresponding durable financial
// terminal, or vice versa.
type TerminalMaterializer interface {
	MaterializeTerminalTx(context.Context, pgx.Tx, TerminalRecord) error
}

func (b *Billing) SetTerminalMaterializer(materializer TerminalMaterializer) {
	if b != nil {
		b.terminalMaterializer = materializer
	}
}
