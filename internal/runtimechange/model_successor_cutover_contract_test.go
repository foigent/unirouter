package runtimechange

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func validModelSuccessorCutoverMutation() ModelSuccessorCutoverMutation {
	return ModelSuccessorCutoverMutation{
		ReleaseRef: "rcr_glm52_successor", ExpectedReleaseVersion: 2,
		StableRequestModel:       "glm-5.2",
		PredecessorPublicModelID: 11, PredecessorModelName: "glm-5.2",
		ExpectedPredecessorLifecycle: "ga", ExpectedPredecessorApprovedCatalog: true,
		SuccessorPublicModelID: 12, SuccessorModelName: "glm-5.2-20260902",
		ExpectedSuccessorLifecycle: "staging", ExpectedSuccessorApprovedCatalog: false,
		ExpectedModelSets: []ModelSuccessorExpectedModelSet{},
		Reason:            "first cutover",
	}
}

func TestModelSuccessorClaimRetryableErrorPreservesSQLState(t *testing.T) {
	for _, code := range []string{"40001", "40P01"} {
		err := &deploymentLifecycleReceiptClaimRetryableError{
			operation: "claim model successor cutover receipt",
			cause:     &pgconn.PgError{Code: code},
		}
		if !isDeploymentLifecycleReceiptRecoveryCandidate(err) {
			t.Fatalf("SQLSTATE %s was hidden by successor claim wrapper: %v", code, err)
		}
		var recovered interface{ SQLState() string }
		if !errors.As(err, &recovered) || recovered.SQLState() != code {
			t.Fatalf("SQLSTATE %s cause chain = %#v", code, recovered)
		}
	}
}

func TestNormalizeModelSuccessorCutoverMutationLimitsV1ToFirstCutover(t *testing.T) {
	valid := validModelSuccessorCutoverMutation()
	if _, err := NormalizeModelSuccessorCutoverMutation(valid); err != nil {
		t.Fatalf("valid mutation rejected: %v", err)
	}
	for _, mutate := range []func(*ModelSuccessorCutoverMutation){
		func(value *ModelSuccessorCutoverMutation) { value.StableRequestModel = "existing-alias" },
		func(value *ModelSuccessorCutoverMutation) { value.SuccessorModelName = value.StableRequestModel },
		func(value *ModelSuccessorCutoverMutation) { value.ExpectedPredecessorApprovedCatalog = false },
		func(value *ModelSuccessorCutoverMutation) { value.ExpectedSuccessorLifecycle = "ga" },
		func(value *ModelSuccessorCutoverMutation) { value.ExpectedSuccessorApprovedCatalog = true },
		func(value *ModelSuccessorCutoverMutation) { value.ExpectedModelSets = nil },
		func(value *ModelSuccessorCutoverMutation) {
			value.ExpectedModelSets = []ModelSuccessorExpectedModelSet{{ModelSetID: 7}, {ModelSetID: 7}}
		},
	} {
		candidate := valid
		mutate(&candidate)
		if _, err := NormalizeModelSuccessorCutoverMutation(candidate); err == nil {
			t.Fatalf("invalid first-cutover mutation accepted: %+v", candidate)
		}
	}
}

func TestNormalizeModelSuccessorCutoverMutationSeparatesNeverActivatedStagingPredecessor(t *testing.T) {
	valid := validModelSuccessorCutoverMutation()
	valid.PredecessorAuthorityMode = ModelSuccessorPredecessorNeverActivated
	valid.ExpectedPredecessorLifecycle = "staging"
	valid.ExpectedPredecessorApprovedCatalog = false
	if normalized, err := NormalizeModelSuccessorCutoverMutation(valid); err != nil {
		t.Fatalf("valid staging predecessor mutation rejected: %v", err)
	} else if normalized.PredecessorAuthorityMode != ModelSuccessorPredecessorNeverActivated {
		t.Fatalf("staging authority mode changed: %q", normalized.PredecessorAuthorityMode)
	}

	for _, mutate := range []func(*ModelSuccessorCutoverMutation){
		func(value *ModelSuccessorCutoverMutation) { value.ExpectedPredecessorLifecycle = "ga" },
		func(value *ModelSuccessorCutoverMutation) { value.ExpectedPredecessorApprovedCatalog = true },
		func(value *ModelSuccessorCutoverMutation) { value.PredecessorAuthorityMode = "unknown" },
	} {
		candidate := valid
		mutate(&candidate)
		if _, err := NormalizeModelSuccessorCutoverMutation(candidate); err == nil {
			t.Fatalf("invalid staging predecessor mutation accepted: %+v", candidate)
		}
	}
}

func TestModelSuccessorContractIdentitiesCannotCrossReplayNamespaces(t *testing.T) {
	published := validModelSuccessorCutoverMutation()
	publishedIdentity := successorContractIdentity(published)
	if publishedIdentity.lifecycleVersion != ModelSuccessorLifecycleReceiptContractVersion ||
		publishedIdentity.childVersion != ModelSuccessorCutoverReceiptContractVersion ||
		publishedIdentity.operationScope != modelSuccessorLifecycleOperationScope {
		t.Fatalf("published successor identity drifted: %+v", publishedIdentity)
	}

	staging := published
	staging.PredecessorAuthorityMode = ModelSuccessorPredecessorNeverActivated
	staging.ExpectedPredecessorLifecycle = "staging"
	staging.ExpectedPredecessorApprovedCatalog = false
	stagingIdentity := successorContractIdentity(staging)
	if stagingIdentity.lifecycleVersion != StagingModelSuccessorLifecycleReceiptContractVersion ||
		stagingIdentity.childVersion != StagingModelSuccessorCutoverReceiptContractVersion ||
		stagingIdentity.operationScope != stagingModelSuccessorLifecycleOperationScope {
		t.Fatalf("staging successor identity drifted: %+v", stagingIdentity)
	}
	if stagingIdentity.operationScope == publishedIdentity.operationScope ||
		stagingIdentity.lifecycleVersion == publishedIdentity.lifecycleVersion ||
		stagingIdentity.childVersion == publishedIdentity.childVersion {
		t.Fatalf("successor identities share a replay dimension: published=%+v staging=%+v", publishedIdentity, stagingIdentity)
	}
}

func TestRepeatedSuccessorRequiresExactCurrentAuthority(t *testing.T) {
	valid := validModelSuccessorCutoverMutation()
	valid.PredecessorAuthorityMode = ModelSuccessorPredecessorPrimaryAlias
	valid.PredecessorModelName = "glm-5.2-prior"
	valid.ExpectedPrimaryAliasID = 7
	valid.ExpectedPredecessorSlotVersion = 3
	valid.ExpectedPredecessorReleaseRef = "rcr_prior"
	if _, err := NormalizeModelSuccessorCutoverMutation(valid); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*ModelSuccessorCutoverMutation){
		func(m *ModelSuccessorCutoverMutation) { m.ExpectedPrimaryAliasID = 0 },
		func(m *ModelSuccessorCutoverMutation) { m.ExpectedPredecessorSlotVersion = 0 },
		func(m *ModelSuccessorCutoverMutation) { m.ExpectedPredecessorReleaseRef = "invalid" },
		func(m *ModelSuccessorCutoverMutation) { m.PredecessorModelName = m.StableRequestModel },
		func(m *ModelSuccessorCutoverMutation) {
			m.PredecessorAuthorityMode = ModelSuccessorPredecessorPublished
		},
	} {
		invalid := valid
		mutate(&invalid)
		if _, err := NormalizeModelSuccessorCutoverMutation(invalid); err == nil {
			t.Fatal("accepted incomplete or cross-mode authority")
		}
	}
	identity := successorContractIdentity(valid)
	if identity.childVersion != RepeatedModelSuccessorCutoverReceiptContractVersion ||
		identity.lifecycleVersion != RepeatedModelSuccessorLifecycleReceiptContractVersion ||
		identity.operationScope == modelSuccessorLifecycleOperationScope || identity.operationScope == stagingModelSuccessorLifecycleOperationScope {
		t.Fatalf("repeated successor identity: %+v", identity)
	}
}

func TestNormalizeModelSuccessorCutoverCommandRequiresDurableIdentity(t *testing.T) {
	command := ModelSuccessorCutoverCommand{
		Mutation: validModelSuccessorCutoverMutation(), CustomerOrgID: 1, ActorUserID: 2, ActorOrgID: 1,
		IdempotencyKeyHash: strings.Repeat("a", 64),
		RequestFingerprint: "hmac-sha256:" + strings.Repeat("b", 64),
		ServerOperationID:  "op_successor",
	}
	if _, err := NormalizeModelSuccessorCutoverCommand(command); err != nil {
		t.Fatalf("valid command rejected: %v", err)
	}
	command.RequestFingerprint = "sha256:" + strings.Repeat("b", 64)
	if _, err := NormalizeModelSuccessorCutoverCommand(command); err == nil {
		t.Fatal("non-HMAC request fingerprint accepted")
	}
}
