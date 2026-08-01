package enterprisefinance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

type canonicalRateComponent struct {
	Meter        string `json:"meter"`
	UnitQuantity string `json:"unit_quantity"`
	ValueMinor   string `json:"value_minor"`
	Currency     string `json:"currency"`
	Scale        int64  `json:"scale"`
}

func RateComponentIdentity(component RateComponent) (string, error) {
	probe := RateCard{
		ContractVersion: ContractVersion, RateCardRef: "identity-probe", OrgID: 1,
		ProviderRef: "identity-probe", ModelRef: "identity-probe", Layer: RateProviderList,
		Version: 1, State: RateDraft, EffectiveFrom: validIdentityTime(), CreatedBy: "identity-probe",
		Components: []RateComponent{component},
	}
	if err := ValidateRateCard(probe); err != nil {
		return "", err
	}
	wire, err := json.Marshal(canonicalRateComponent{
		Meter: strings.TrimSpace(component.Meter), UnitQuantity: component.UnitQuantity,
		ValueMinor: component.Amount.ValueMinor, Currency: component.Amount.Currency, Scale: component.Amount.Scale,
	})
	if err != nil {
		return "", err
	}
	return sha256Identity(wire), nil
}

type canonicalPricingSnapshot struct {
	ContractVersion string           `json:"contract_version"`
	RequestRef      string           `json:"request_ref"`
	OrgID           int64            `json:"org_id"`
	ProjectRef      string           `json:"project_ref"`
	PricedAt        string           `json:"priced_at"`
	Bindings        []PricingBinding `json:"bindings"`
}

type canonicalAllocationResult struct {
	RequestRef         string `json:"request_ref"`
	SourceRevision     string `json:"source_revision"`
	State              string `json:"state"`
	TargetKind         string `json:"target_kind"`
	TargetRef          string `json:"target_ref"`
	RuleRef            string `json:"rule_ref"`
	RuleRevision       string `json:"rule_revision"`
	ValueMinor         string `json:"value_minor"`
	Currency           string `json:"currency"`
	Scale              int64  `json:"scale"`
	LedgerMutation     string `json:"ledger_mutation"`
	CustomerDebtAction string `json:"customer_debt_action"`
}

type canonicalAllocationRun struct {
	ContractVersion  string                      `json:"contract_version"`
	AllocationRunRef string                      `json:"allocation_run_ref"`
	OrgID            int64                       `json:"org_id"`
	FiscalPeriodRef  string                      `json:"fiscal_period_ref"`
	SourceRevision   string                      `json:"source_revision"`
	RuleSetRef       string                      `json:"rule_set_ref"`
	RuleSetRevision  string                      `json:"rule_set_revision"`
	SupersedesRunRef string                      `json:"supersedes_run_ref"`
	Results          []canonicalAllocationResult `json:"results"`
}

// AllocationRunIdentity is a product-local identity. It is deliberately not a
// Foigent Event Envelope hash and therefore cannot redefine shared protocol
// canonicalization.
func AllocationRunIdentity(request AllocationRunRequest) (string, error) {
	if err := ValidateAllocationRun(request); err != nil {
		return "", err
	}
	results := make([]canonicalAllocationResult, 0, len(request.Results))
	for _, result := range request.Results {
		results = append(results, canonicalAllocationResult{
			RequestRef:     strings.TrimSpace(result.RequestRef),
			SourceRevision: strings.TrimSpace(result.SourceRevision), State: result.State,
			TargetKind: result.Target.Kind, TargetRef: strings.TrimSpace(result.Target.Ref),
			RuleRef: strings.TrimSpace(result.RuleRef), RuleRevision: strings.TrimSpace(result.RuleRevision),
			ValueMinor: result.Amount.ValueMinor, Currency: result.Amount.Currency, Scale: result.Amount.Scale,
			LedgerMutation: result.LedgerMutation, CustomerDebtAction: result.CustomerDebtAction,
		})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].RequestRef < results[j].RequestRef })
	wire, err := json.Marshal(canonicalAllocationRun{
		ContractVersion:  request.ContractVersion,
		AllocationRunRef: strings.TrimSpace(request.AllocationRunRef), OrgID: request.OrgID,
		FiscalPeriodRef: strings.TrimSpace(request.FiscalPeriodRef),
		SourceRevision:  strings.TrimSpace(request.SourceRevision), RuleSetRef: strings.TrimSpace(request.RuleSetRef),
		RuleSetRevision: strings.TrimSpace(request.RuleSetRevision), SupersedesRunRef: strings.TrimSpace(request.SupersedesRunRef),
		Results: results,
	})
	if err != nil {
		return "", err
	}
	return sha256Identity(wire), nil
}

func PricingSnapshotIdentity(snapshot PricingSnapshot) (string, error) {
	probe := snapshot
	probe.SnapshotHash = "sha256:" + strings.Repeat("0", 64)
	if err := ValidatePricingSnapshot(probe); err != nil {
		return "", err
	}
	bindings := append([]PricingBinding{}, snapshot.Bindings...)
	sort.Slice(bindings, func(i, j int) bool {
		if bindings[i].Meter != bindings[j].Meter {
			return bindings[i].Meter < bindings[j].Meter
		}
		if bindings[i].RateCardRef != bindings[j].RateCardRef {
			return bindings[i].RateCardRef < bindings[j].RateCardRef
		}
		if bindings[i].RateCardVersion != bindings[j].RateCardVersion {
			return bindings[i].RateCardVersion < bindings[j].RateCardVersion
		}
		return bindings[i].RateComponentHash < bindings[j].RateComponentHash
	})
	wire, err := json.Marshal(canonicalPricingSnapshot{
		ContractVersion: snapshot.ContractVersion,
		RequestRef:      strings.TrimSpace(snapshot.RequestRef), OrgID: snapshot.OrgID,
		ProjectRef: strings.TrimSpace(snapshot.ProjectRef),
		PricedAt:   snapshot.PricedAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
		Bindings:   bindings,
	})
	if err != nil {
		return "", err
	}
	return sha256Identity(wire), nil
}

func sha256Identity(wire []byte) string {
	digest := sha256.Sum256(wire)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func validIdentityTime() time.Time {
	return time.Unix(0, 0).UTC()
}
