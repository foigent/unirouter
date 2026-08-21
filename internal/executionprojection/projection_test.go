package executionprojection

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/executionevidence"
)

func TestBindSourceEventHashIncludesImmutableEvidenceHash(t *testing.T) {
	left := legacyCompact("sha256:" + repeat("a", 64))
	right := legacyCompact("sha256:" + repeat("b", 64))
	base := repeat("c", 64)
	leftHash, err := BindSourceEventHash(base, Projection{Source: SourceProviderAttempt, Compact: &left})
	if err != nil {
		t.Fatal(err)
	}
	rightHash, err := BindSourceEventHash(base, Projection{Source: SourceProviderAttempt, Compact: &right})
	if err != nil {
		t.Fatal(err)
	}
	if leftHash == rightHash {
		t.Fatal("route evidence hash did not affect source-event hash")
	}
}

func TestBindSourceEventHashIncludesEvidenceV2PublicAuthority(t *testing.T) {
	compact := executionevidence.CompactProjection{
		ContractVersion: executionevidence.ContractVersionV2,
		AuthorityMode:   executionevidence.ModeCanonical,
		EvidenceHash:    "sha256:" + repeat("a", 64),
		PublicModelID:   1, DeploymentID: 2, ConnectionID: 3,
		CredentialBindingID: 4, ModelOperationID: "openai.responses.v1",
		RuntimeReleaseID: 5, SnapshotHash: "mrs1_" + repeat("b", 64),
		PublicExecutionBindingID: 6, DeliveryContractVersion: executionevidence.PublicDeliveryContractV1,
		ChargeContractVersion:      executionevidence.PublicChargeContractV1,
		IdempotencyContractVersion: executionevidence.PublicIdempotencyContractV1,
	}
	left, err := BindSourceEventHash(repeat("c", 64), Projection{Source: SourceProviderAttempt, Compact: &compact})
	if err != nil {
		t.Fatal(err)
	}
	compact.PublicExecutionBindingID++
	right, err := BindSourceEventHash(repeat("c", 64), Projection{Source: SourceProviderAttempt, Compact: &compact})
	if err != nil {
		t.Fatal(err)
	}
	if left == right {
		t.Fatal("V2 public execution binding did not affect source-event hash")
	}
}

func TestNormalizeRequiresModeCompleteCompactIdentity(t *testing.T) {
	legacy := legacyCompact("sha256:" + repeat("d", 64))
	if _, err := Normalize(Projection{Source: SourceProviderAttempt, Compact: &legacy}); err != nil {
		t.Fatal(err)
	}
	legacy.LegacyChannelID = 0
	if _, err := Normalize(Projection{Source: SourceProviderAttempt, Compact: &legacy}); err == nil {
		t.Fatal("partial legacy projection was accepted")
	}
	if _, err := Normalize(Projection{Source: SourceResponseCache}); err != nil {
		t.Fatal(err)
	}
}

func TestResolveNeverRelabelsIncompleteProviderEventAsNoAttempt(t *testing.T) {
	_, err := Resolve(context.Background(), emptyQuerier{}, ResolveInput{
		OrgID: 1, RequestID: "request-1", NoAttemptSource: SourceNoProviderAttempt,
		AttemptRequired: true,
	})
	if !errors.Is(err, ErrExpectedAttemptMissing) {
		t.Fatalf("missing exact provider attempt error = %v", err)
	}
}

type emptyQuerier struct{}

func (emptyQuerier) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("query must not run without exact attempt_id")
}

func legacyCompact(hash string) executionevidence.CompactProjection {
	return executionevidence.CompactProjection{
		ContractVersion: executionevidence.ContractVersion,
		AuthorityMode:   executionevidence.ModeLegacy, EvidenceHash: hash,
		ModelOperationID: "openai.chat_completions.v1", LegacyChannelID: 21,
	}
}

func repeat(value string, count int) string {
	result := ""
	for index := 0; index < count; index++ {
		result += value
	}
	return result
}
