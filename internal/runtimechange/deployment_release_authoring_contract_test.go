package runtimechange

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func validDeploymentReleaseAuthoringCommand() DeploymentReleaseAuthoringCommand {
	return DeploymentReleaseAuthoringCommand{
		Mutation: DeploymentReleaseAuthoringMutation{
			PublicModelID: 41,
			Baseline: DeploymentReleaseBaseline{
				Kind: DeploymentReleaseBaselineInitial,
			},
			Selections: []DeploymentReleaseSelection{
				{DeploymentID: 7, OperationID: "openai.chat_completions.v1"},
				{DeploymentID: 8, OperationID: "openai.embeddings.v1"},
				{DeploymentID: 9, OperationID: "openai.responses.v1"},
				{DeploymentID: 10, OperationID: "anthropic.messages.v1"},
			},
			Reason: " author exact candidate ",
		},
		CustomerOrgID: 3, ActorUserID: 5, ActorOrgID: 3,
		IdempotencyKeyHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		RequestFingerprint: "hmac-sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		ServerOperationID:  " op_authoring ",
	}
}

func TestNormalizeDeploymentReleaseAuthoringCommandKeepsOnlyExactCallerFields(t *testing.T) {
	command := validDeploymentReleaseAuthoringCommand()
	original := command
	original.Mutation.Selections = append([]DeploymentReleaseSelection(nil), command.Mutation.Selections...)
	normalized, err := NormalizeDeploymentReleaseAuthoringCommand(command)
	if err != nil {
		t.Fatalf("NormalizeDeploymentReleaseAuthoringCommand: %v", err)
	}
	if normalized.Mutation.Reason != "author exact candidate" || normalized.ServerOperationID != "op_authoring" {
		t.Fatalf("normalized text = %+v", normalized)
	}
	if !reflect.DeepEqual(normalized.Mutation.Selections, command.Mutation.Selections) {
		t.Fatalf("selection order changed: %+v", normalized.Mutation.Selections)
	}
	normalized.Mutation.Selections[0].OperationID = "changed"
	if !reflect.DeepEqual(command, original) {
		t.Fatalf("normalization mutated caller: got=%+v want=%+v", command, original)
	}
}

func TestNormalizeDeploymentReleaseAuthoringCommandRejectsWeakBaselineAndSelections(t *testing.T) {
	tests := []func(*DeploymentReleaseAuthoringCommand){
		func(command *DeploymentReleaseAuthoringCommand) { command.Mutation.PublicModelID = 0 },
		func(command *DeploymentReleaseAuthoringCommand) { command.Mutation.Baseline.ExpectedSlotVersion = 1 },
		func(command *DeploymentReleaseAuthoringCommand) {
			command.Mutation.Baseline = DeploymentReleaseBaseline{
				Kind: DeploymentReleaseBaselineStable, ExpectedSlotVersion: 4,
				StableReleaseRef: "", ExpectedStableReleaseVersion: 2,
			}
		},
		func(command *DeploymentReleaseAuthoringCommand) { command.Mutation.Selections = nil },
		func(command *DeploymentReleaseAuthoringCommand) {
			command.Mutation.Selections[1] = command.Mutation.Selections[0]
		},
		func(command *DeploymentReleaseAuthoringCommand) {
			command.Mutation.Selections[0].OperationID = "system.models.list.v1"
		},
		func(command *DeploymentReleaseAuthoringCommand) { command.Mutation.Reason = "\n" },
		func(command *DeploymentReleaseAuthoringCommand) { command.ActorOrgID = 0 },
		func(command *DeploymentReleaseAuthoringCommand) { command.IdempotencyKeyHash = "raw-key" },
		func(command *DeploymentReleaseAuthoringCommand) { command.RequestFingerprint = "sha256:not-hmac" },
	}
	for index, mutate := range tests {
		command := validDeploymentReleaseAuthoringCommand()
		mutate(&command)
		if _, err := NormalizeDeploymentReleaseAuthoringCommand(command); !errors.Is(err, ErrInvalid) {
			t.Fatalf("case %d error=%v, want ErrInvalid", index, err)
		}
	}
}

func TestNormalizeDeploymentReleaseAuthoringCommandAcceptsExactReferencedBaseline(t *testing.T) {
	for _, kind := range []DeploymentReleaseBaselineKind{
		DeploymentReleaseBaselineStable,
		DeploymentReleaseBaselineLegacyCutover,
	} {
		command := validDeploymentReleaseAuthoringCommand()
		command.Mutation.Baseline = DeploymentReleaseBaseline{
			Kind: kind, ExpectedSlotVersion: 9,
			StableReleaseRef: " RCR_STABLE-ONE ", ExpectedStableReleaseVersion: 4,
		}
		normalized, err := NormalizeDeploymentReleaseAuthoringCommand(command)
		if err != nil {
			t.Fatalf("NormalizeDeploymentReleaseAuthoringCommand %s: %v", kind, err)
		}
		if normalized.Mutation.Baseline.StableReleaseRef != "rcr_stable-one" {
			t.Fatalf("%s stable ref=%q", kind, normalized.Mutation.Baseline.StableReleaseRef)
		}
	}
}

func TestNormalizeDeploymentReleaseAuthoringCommandRejectsWeakLegacyCutoverBaseline(t *testing.T) {
	for index, mutate := range []func(*DeploymentReleaseBaseline){
		func(baseline *DeploymentReleaseBaseline) { baseline.ExpectedSlotVersion = 0 },
		func(baseline *DeploymentReleaseBaseline) { baseline.StableReleaseRef = "" },
		func(baseline *DeploymentReleaseBaseline) { baseline.ExpectedStableReleaseVersion = 0 },
	} {
		command := validDeploymentReleaseAuthoringCommand()
		command.Mutation.Baseline = DeploymentReleaseBaseline{
			Kind: DeploymentReleaseBaselineLegacyCutover, ExpectedSlotVersion: 9,
			StableReleaseRef: "rcr_legacy-one", ExpectedStableReleaseVersion: 4,
		}
		mutate(&command.Mutation.Baseline)
		if _, err := NormalizeDeploymentReleaseAuthoringCommand(command); !errors.Is(err, ErrInvalid) {
			t.Fatalf("case %d error=%v, want ErrInvalid", index, err)
		}
	}
}

func TestDeploymentReleaseAuthoringClaimRetryableErrorPreservesSQLState(t *testing.T) {
	for _, code := range []string{"40001", "40P01"} {
		cause := &pgconn.PgError{Code: code}
		err := &deploymentReleaseAuthoringClaimRetryableError{
			operation: "claim deployment release authoring receipt", cause: cause,
		}
		if !isDeploymentLifecycleReceiptRecoveryCandidate(err) {
			t.Fatalf("SQLSTATE %s was hidden by retryable wrapper: %v", code, err)
		}
		var recovered interface{ SQLState() string }
		if !errors.As(err, &recovered) || recovered.SQLState() != code {
			t.Fatalf("SQLSTATE %s cause chain = %#v", code, recovered)
		}
	}
}

func TestDeploymentReleaseAuthoringReceiptV1DoesNotInventM82ReleaseFields(t *testing.T) {
	release := DeploymentRelease{
		ID: 7, ReleaseRef: "rcr_exact", PublicModelID: 9, ModelName: "model",
		ExecutionProfileID: "kimi.k3.api", ExecutionProfileVersion: "1.1.0",
		PublicContextLimit: 262144, PublicOutputLimit: 32768,
		CandidateVersion: DeploymentCandidateVersion,
	}
	receiptJSON, err := json.Marshal(DeploymentReleaseAuthoringReceipt{
		ContractVersion: DeploymentReleaseAuthoringReceiptContractVersion,
		ReceiptID:       11, Release: release,
	})
	if err != nil {
		t.Fatalf("marshal authoring receipt: %v", err)
	}
	for _, forbidden := range []string{
		"execution_profile_id", "execution_profile_version",
		"public_context_limit", "public_output_limit",
	} {
		if strings.Contains(string(receiptJSON), forbidden) {
			t.Fatalf("v1 authoring receipt invented M82 field %q: %s", forbidden, receiptJSON)
		}
	}
	releaseJSON, err := json.Marshal(release)
	if err != nil {
		t.Fatalf("marshal exact release: %v", err)
	}
	if !strings.Contains(string(releaseJSON), `"execution_profile_id":"kimi.k3.api"`) {
		t.Fatalf("exact release omitted sealed execution profile: %s", releaseJSON)
	}
}
