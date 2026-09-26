package main

import (
	"context"
	"errors"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/mediajob"
	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelauthz"
	"github.com/uniroute/unirouter/internal/modelgov"
	"strings"
)

// Share the production admission and dispatch policy with local integration tests.
// These callbacks deliberately retain the original authorization and error mapping.
func newMediaExecutionAuthorizer(modelAuthorizationEvaluator *modelauthz.Evaluator) mediajob.ExecutionModelAuthorizer {
	return mediajob.ExecutionModelAuthorizerFunc(func(
		ctx context.Context,
		q db.DBQuerier,
		input mediajob.ExecutionAuthorization,
	) error {
		access, accessErr := modelgov.CheckAccess(
			ctx, q, input.Scope.OrgID, input.Scope.ProjectID, input.RequestedModel,
		)
		if accessErr != nil {
			if errors.Is(accessErr, modelgov.ErrModelRetired) || errors.Is(accessErr, modelgov.ErrModelDenied) ||
				errors.Is(accessErr, modelgov.ErrModelRequired) {
				return mediajob.ErrForbidden
			}
			return mediajob.ErrUnavailable
		}
		routingModel := strings.TrimSpace(access.ResolvedModel)
		if routingModel == "" {
			routingModel = input.RequestedModel
		}
		if routingModel != input.RoutingModel {
			return mediajob.ErrForbidden
		}
		decision, decisionErr := modelAuthorizationEvaluator.EvaluateWithQuerier(ctx, q, modelauthz.Input{
			OrgID: input.Scope.OrgID, ProjectID: input.Scope.ProjectID,
			APIKeyID: input.APIKeyID, AccessCredentialID: input.AccessCredentialID,
			OwnerUserID: input.OwnerUserID, Endpoint: "/v1/media/jobs",
			RequestedModel: input.RequestedModel, ResolvedModel: routingModel,
		})
		if decisionErr != nil {
			return mediajob.ErrUnavailable
		}
		if !decision.Allowed {
			return mediajob.ErrForbidden
		}
		return nil
	})
}

func newMediaSubmissionAuthorizer(pool db.DBQuerier, modelAuthorizationEvaluator *modelauthz.Evaluator) mediajob.ModelAuthorizer {
	return mediajob.ModelAuthorizerFunc(func(
		ctx context.Context,
		scope mediajob.Scope,
		requestedModel string,
	) (mediajob.ModelAuthorization, error) {
		access, accessErr := modelgov.CheckAccess(
			ctx, pool, scope.OrgID, scope.ProjectID, requestedModel,
		)
		switch {
		case errors.Is(accessErr, modelgov.ErrModelRetired),
			errors.Is(accessErr, modelgov.ErrModelDenied),
			errors.Is(accessErr, modelgov.ErrModelRequired):
			return mediajob.ModelAuthorization{}, mediajob.ErrForbidden
		case accessErr != nil:
			return mediajob.ModelAuthorization{}, mediajob.ErrUnavailable
		default:
			routingModel := access.ResolvedModel
			if routingModel == "" {
				routingModel = requestedModel
			}
			apiKey := middleware.GetAPIKeyFromContext(ctx)
			if apiKey == nil {
				return mediajob.ModelAuthorization{}, mediajob.ErrForbidden
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
				return mediajob.ModelAuthorization{}, mediajob.ErrUnavailable
			}
			if !decision.Allowed {
				return mediajob.ModelAuthorization{}, mediajob.ErrForbidden
			}
			return mediajob.ModelAuthorization{RoutingModel: routingModel}, nil
		}
	})
}
