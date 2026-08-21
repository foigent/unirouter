package runtimechange

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestNormalizeInitialDeploymentActivationRequest(t *testing.T) {
	valid := InitialDeploymentActivationRequest{
		ReleaseRef: " RCR_INITIAL_001 ", PublicModelID: 7,
		ExpectedReleaseVersion: 2, ActorUserID: 11, CustomerOrgID: 13,
	}
	normalized, err := normalizeInitialDeploymentActivationRequest(valid)
	if err != nil {
		t.Fatalf("normalize valid request: %v", err)
	}
	if normalized.ReleaseRef != "rcr_initial_001" {
		t.Fatalf("ReleaseRef = %q", normalized.ReleaseRef)
	}

	for name, edit := range map[string]func(*InitialDeploymentActivationRequest){
		"release":         func(value *InitialDeploymentActivationRequest) { value.ReleaseRef = "bad" },
		"public model":    func(value *InitialDeploymentActivationRequest) { value.PublicModelID = 0 },
		"release version": func(value *InitialDeploymentActivationRequest) { value.ExpectedReleaseVersion = 0 },
		"actor":           func(value *InitialDeploymentActivationRequest) { value.ActorUserID = 0 },
		"organization":    func(value *InitialDeploymentActivationRequest) { value.CustomerOrgID = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			request := valid
			edit(&request)
			_, err := normalizeInitialDeploymentActivationRequest(request)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestInitialDeploymentActivationWriteErrorClassifiesStateRaces(t *testing.T) {
	for _, code := range []string{"40001", "40P01", "23505"} {
		err := initialDeploymentActivationWriteError("initial activation", &pgconn.PgError{Code: code})
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("PostgreSQL %s error = %v, want ErrConflict", code, err)
		}
	}
	for _, code := range []string{"23514", "08006"} {
		err := initialDeploymentActivationWriteError("initial activation", &pgconn.PgError{Code: code})
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("PostgreSQL %s error = %v, want ErrUnavailable", code, err)
		}
	}
}

func TestActivateInitialDeploymentStableIsRetiredBeforeDatabaseAccess(t *testing.T) {
	manager := &Manager{}
	_, err := manager.ActivateInitialDeploymentStable(t.Context(), InitialDeploymentActivationRequest{
		ReleaseRef: "rcr_initial_001", PublicModelID: 7,
		ExpectedReleaseVersion: 0, ActorUserID: 11, CustomerOrgID: 13,
	})
	if !errors.Is(err, ErrLegacyRuntimeChangeRetired) {
		t.Fatalf("error = %v, want ErrLegacyRuntimeChangeRetired", err)
	}
}
