package runtimechange

import (
	"errors"
	"testing"
)

func TestLegacyRuntimeChangeWritersAreRetiredBeforeDatabaseAccess(t *testing.T) {
	var manager *Manager
	ctx := t.Context()

	tests := []struct {
		name string
		call func() error
	}{
		{name: "CreateDraft", call: func() error {
			_, err := manager.CreateDraft(ctx, DraftRequest{}, 0)
			return err
		}},
		{name: "Validate", call: func() error {
			_, err := manager.Validate(ctx, "", 0, 0)
			return err
		}},
		{name: "AdvanceShadow", call: func() error {
			_, err := manager.AdvanceShadow(ctx, "", 0, 0)
			return err
		}},
		{name: "StartCanary", call: func() error {
			_, err := manager.StartCanary(ctx, "", 0, 0, 0)
			return err
		}},
		{name: "Promote", call: func() error {
			_, err := manager.Promote(ctx, "", 0, 0)
			return err
		}},
		{name: "Rollback", call: func() error {
			_, err := manager.Rollback(ctx, "", 0, 0)
			return err
		}},
		{name: "Reject", call: func() error {
			_, err := manager.Reject(ctx, "", 0, 0, "")
			return err
		}},
		{name: "ActivateInitial", call: func() error {
			_, err := manager.ActivateInitial(ctx, "", 0, 0, 0)
			return err
		}},
		{name: "ReplaceUnavailableStable", call: func() error {
			_, err := manager.ReplaceUnavailableStable(ctx, "", 0, "", 0, 0, 0)
			return err
		}},
		{name: "ActivateInitialDeploymentStable", call: func() error {
			_, err := manager.ActivateInitialDeploymentStable(ctx, InitialDeploymentActivationRequest{})
			return err
		}},
		{name: "StartDeploymentCanary", call: func() error {
			_, err := manager.StartDeploymentCanary(ctx, DeploymentLifecycleRequest{}, 0)
			return err
		}},
		{name: "PromoteDeploymentCanary", call: func() error {
			_, err := manager.PromoteDeploymentCanary(ctx, DeploymentLifecycleRequest{})
			return err
		}},
		{name: "RollbackDeploymentRelease", call: func() error {
			_, err := manager.RollbackDeploymentRelease(ctx, DeploymentLifecycleRequest{})
			return err
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.call()
			if !errors.Is(err, ErrLegacyRuntimeChangeRetired) {
				t.Fatalf("error = %v, want ErrLegacyRuntimeChangeRetired", err)
			}
			if err != ErrLegacyRuntimeChangeRetired {
				t.Fatalf("error identity = %v, want stable sentinel", err)
			}
		})
	}
}
