package runtimechange

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestNormalizeDeploymentLifecycleRequest(t *testing.T) {
	valid := DeploymentLifecycleRequest{
		ReleaseRef: " RCR_CANARY_001 ", PublicModelID: 7,
		ExpectedReleaseVersion: 2, ExpectedSlotVersion: 3,
		ActorUserID: 11, CustomerOrgID: 13,
	}
	normalized, err := normalizeDeploymentLifecycleRequest(valid)
	if err != nil {
		t.Fatalf("normalize valid request: %v", err)
	}
	if normalized.ReleaseRef != "rcr_canary_001" {
		t.Fatalf("ReleaseRef = %q", normalized.ReleaseRef)
	}

	for name, edit := range map[string]func(*DeploymentLifecycleRequest){
		"release":         func(value *DeploymentLifecycleRequest) { value.ReleaseRef = "bad" },
		"public model":    func(value *DeploymentLifecycleRequest) { value.PublicModelID = 0 },
		"release version": func(value *DeploymentLifecycleRequest) { value.ExpectedReleaseVersion = 0 },
		"slot version":    func(value *DeploymentLifecycleRequest) { value.ExpectedSlotVersion = 0 },
		"actor":           func(value *DeploymentLifecycleRequest) { value.ActorUserID = 0 },
		"organization":    func(value *DeploymentLifecycleRequest) { value.CustomerOrgID = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			request := valid
			edit(&request)
			_, err := normalizeDeploymentLifecycleRequest(request)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestLifecycleWriteErrorClassifiesConcurrency(t *testing.T) {
	err := lifecycleWriteError("transition", &pgconn.PgError{Code: "40001"})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("serialization error = %v, want ErrConflict", err)
	}
	err = lifecycleWriteError("transition", &pgconn.PgError{Code: "40P01"})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("deadlock error = %v, want ErrConflict", err)
	}
	err = lifecycleWriteError("transition", &pgconn.PgError{Code: "08006"})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("connection error = %v, want ErrUnavailable", err)
	}
}

func TestDeploymentLifecycleReceiptClaimClassifiesActorTenantMismatch(t *testing.T) {
	err := deploymentLifecycleReceiptClaimWriteError("claim", &pgconn.PgError{
		Code:           "23503",
		ConstraintName: "model_deployment_lifecycle_receipts_actor_tenant_fk",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("actor tenant mismatch = %v, want ErrConflict", err)
	}
	err = deploymentLifecycleReceiptClaimWriteError("claim", &pgconn.PgError{
		Code:           "23503",
		ConstraintName: "some_other_constraint",
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("other foreign key = %v, want ErrUnavailable", err)
	}
}

func TestStartDeploymentCanaryIsRetiredBeforeDatabaseAccess(t *testing.T) {
	manager := &Manager{}
	request := DeploymentLifecycleRequest{
		ReleaseRef: "rcr_canary_001", PublicModelID: 7,
		ExpectedReleaseVersion: 2, ExpectedSlotVersion: 3,
		ActorUserID: 11, CustomerOrgID: 13,
	}
	for _, basisPoints := range []int{0, 5001} {
		_, err := manager.StartDeploymentCanary(t.Context(), request, basisPoints)
		if !errors.Is(err, ErrLegacyRuntimeChangeRetired) {
			t.Fatalf("basisPoints %d error = %v, want ErrLegacyRuntimeChangeRetired", basisPoints, err)
		}
	}
}
