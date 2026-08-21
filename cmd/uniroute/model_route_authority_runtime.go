package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/modelrouteauthority"
	"github.com/uniroute/unirouter/internal/modelrouting"
)

func enforceModelRouteAuthorityStartup(
	ctx context.Context,
	cfg *config.Config,
	database modelrouteauthority.Database,
	customerOrgID int64,
) (modelrouting.AuthorityCheckReport, error) {
	if cfg == nil {
		return modelrouting.AuthorityCheckReport{}, fmt.Errorf("model route authority config is required")
	}
	mode := modelrouting.Mode(cfg.ModelRouteAuthority)
	if mode == modelrouting.ModeLegacy {
		return modelrouting.AuthorityCheckReport{Mode: mode, CustomerOrgID: customerOrgID, Ready: true}, nil
	}
	if customerOrgID <= 0 {
		return modelrouting.AuthorityCheckReport{}, fmt.Errorf("model route authority %s requires a ready customer baseline", mode)
	}
	service, err := modelrouteauthority.New(database, cfg.RoutingPolicyConfig())
	if err != nil {
		return modelrouting.AuthorityCheckReport{}, err
	}
	report, err := service.Check(ctx, modelrouting.AuthorityCheckRequest{
		Mode: mode, CustomerOrgID: customerOrgID,
	})
	if err != nil {
		return report, fmt.Errorf("model route authority %s startup check: %w", mode, err)
	}
	return report, nil
}

func isModelRouteAuthorityNotReady(err error) bool {
	return errors.Is(err, modelrouting.ErrAuthorityNotReady) || errors.Is(err, modelrouteauthority.ErrApplyIncomplete)
}
