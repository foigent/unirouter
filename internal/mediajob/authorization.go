package mediajob

import (
	"context"
	"errors"
	"strings"

	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelauthz"
	"github.com/uniroute/unirouter/internal/modelgov"
)

// Share the production admission and dispatch policy with local integration tests.
// These callbacks deliberately retain the original authorization and error mapping.
func NewExecutionAuthorizer(modelAuthorizationEvaluator *modelauthz.Evaluator) ExecutionModelAuthorizer {
	return ExecutionModelAuthorizerFunc(func(
		ctx context.Context,
		q db.DBQuerier,
		input ExecutionAuthorization,
	) error {
		access, accessErr := modelgov.CheckAccess(
			ctx, q, input.Scope.OrgID, input.Scope.ProjectID, input.RequestedModel,
		)
		if accessErr != nil {
			if errors.Is(accessErr, modelgov.ErrModelRetired) || errors.Is(accessErr, modelgov.ErrModelDenied) ||
				errors.Is(accessErr, modelgov.ErrModelRequired) {
				return ErrForbidden
			}
			return ErrUnavailable
		}
		routingModel := strings.TrimSpace(access.ResolvedModel)
		if routingModel == "" {
			routingModel = input.RequestedModel
		}
		if routingModel != input.RoutingModel {
			return ErrForbidden
		}
		decision, decisionErr := modelAuthorizationEvaluator.EvaluateWithQuerier(ctx, q, modelauthz.Input{
			OrgID: input.Scope.OrgID, ProjectID: input.Scope.ProjectID,
			APIKeyID: input.APIKeyID, AccessCredentialID: input.AccessCredentialID,
			OwnerUserID: input.OwnerUserID, Endpoint: "/v1/media/jobs",
			RequestedModel: input.RequestedModel, ResolvedModel: routingModel,
		})
		if decisionErr != nil {
			return ErrUnavailable
		}
		if !decision.Allowed {
			return ErrForbidden
		}
		return nil
	})
}

func NewSubmissionAuthorizer(pool db.DBQuerier, modelAuthorizationEvaluator *modelauthz.Evaluator) ModelAuthorizer {
	return ModelAuthorizerFunc(func(
		ctx context.Context,
		scope Scope,
		requestedModel string,
	) (ModelAuthorization, error) {
		access, accessErr := modelgov.CheckAccess(
			ctx, pool, scope.OrgID, scope.ProjectID, requestedModel,
		)
		switch {
		case errors.Is(accessErr, modelgov.ErrModelRetired),
			errors.Is(accessErr, modelgov.ErrModelDenied),
			errors.Is(accessErr, modelgov.ErrModelRequired):
			return ModelAuthorization{}, ErrForbidden
		case accessErr != nil:
			return ModelAuthorization{}, ErrUnavailable
		default:
			routingModel := access.ResolvedModel
			if routingModel == "" {
				routingModel = requestedModel
			}
			apiKey := middleware.GetAPIKeyFromContext(ctx)
			if apiKey == nil {
				return ModelAuthorization{}, ErrForbidden
			}
			input := modelauthz.Input{
				OrgID: scope.OrgID, ProjectID: scope.ProjectID, APIKeyID: apiKey.ID,
				Endpoint: "/v1/media/jobs", RequestedModel: requestedModel, ResolvedModel: routingModel,
			}
			if apiKey.AccessCredentialID != nil {
				input.AccessCredentialID = *apiKey.AccessCredentialID
			}
			if apiKey.OwnerUserID != nil {
				input.OwnerUserID = *apiKey.OwnerUserID
			}
			decision, decisionErr := modelAuthorizationEvaluator.Evaluate(ctx, input)
			if decisionErr != nil {
				return ModelAuthorization{}, ErrUnavailable
			}
			if !decision.Allowed {
				return ModelAuthorization{}, ErrForbidden
			}
			return ModelAuthorization{RoutingModel: routingModel}, nil
		}
	})
}
