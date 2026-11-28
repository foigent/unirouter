package mediajob

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/executionevidence"
	"github.com/uniroute/unirouter/internal/modelexecution"
	"github.com/uniroute/unirouter/internal/modeloperation"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/modelrouting"
)

// CanonicalRoutes shares the route planner and its locked release, deployment
// and provider credential checks. The job lease remains the dispatch owner.
type mediaDispatchRoute struct {
	modelrouting.Route
	profileID, profileVersion string
}

type CanonicalRoutes struct {
	Planner     *modelrouting.Planner
	Coordinator *modelexecution.Coordinator
}

func (c *CanonicalRoutes) plan(ctx context.Context, job Job, request NormalizedRequest) (modelrouting.Plan, modelrouting.Route, error) {
	if c == nil || c.Planner == nil || c.Coordinator == nil {
		return modelrouting.Plan{}, modelrouting.Route{}, domainError(ErrUnavailable, "canonical_media_route_unavailable")
	}
	plan, err := c.Planner.Plan(ctx, modelrouting.Request{
		Mode: modelrouting.ModeCanonical, CustomerOrgID: job.Scope.OrgID, PublicModel: job.RoutingModel,
		ModelOperationID: string(modeloperation.IDMediaJobs), SelectionKey: job.JobID,
	})
	if err != nil {
		if errors.Is(err, modelrouting.ErrNoRoute) {
			return modelrouting.Plan{}, modelrouting.Route{}, domainError(ErrForbidden, "canonical_media_route_ineligible")
		}
		return modelrouting.Plan{}, modelrouting.Route{}, domainError(ErrUnavailable, "canonical_media_route_unavailable")
	}
	p := plan.Definition.Profile()
	if !modelprofile.MediaJobProfile(p) || plan.ReleaseExecution == nil ||
		plan.PublicOutputLimit != 1 || len(request.Prompt) > plan.PublicContextLimit {
		return modelrouting.Plan{}, modelrouting.Route{}, domainError(ErrForbidden, "canonical_media_release_limits")
	}
	requestedOperation := request.Operation
	if requestedOperation == "" {
		requestedOperation = OperationTextToImage
	}
	if endpointprotocol.MediaJobOperation(p.ProtocolProfile) != requestedOperation {
		return modelrouting.Plan{}, modelrouting.Route{}, domainError(ErrInvalid, "media_operation_profile_mismatch")
	}
	if p.ProtocolProfile == endpointprotocol.ProfileOpenAIImageGPT &&
		!endpointprotocol.SupportsCanonicalFlareImageFormat(request.OutputFormat) {
		return modelrouting.Plan{}, modelrouting.Route{}, domainError(ErrInvalid, "canonical_image_output_format_unsupported")
	}
	if p.APISurface == modelprofile.APISurfaceOpenAIImages {
		switch p.ProtocolProfile {
		case endpointprotocol.ProfileGLMImage:
			if !endpointprotocol.SupportsGLMImageControls(request.Size, request.Quality, request.Background, request.OutputFormat) {
				return modelrouting.Plan{}, modelrouting.Route{}, domainError(ErrInvalid, "published_image_controls_unsupported")
			}
		case endpointprotocol.ProfileDashScopeQwenImage, endpointprotocol.ProfileMiniMaxH3Image:
			if !endpointprotocol.SupportsSquarePNGImageControls(request.Size, request.Quality, request.Background, request.OutputFormat) {
				return modelrouting.Plan{}, modelrouting.Route{}, domainError(ErrInvalid, "published_image_controls_unsupported")
			}
		}
	}
	for _, route := range plan.Routes {
		if route.RequiresProbe || route.PublicEvidence == nil {
			continue
		}
		if _, err := executionevidence.NormalizeV2(*route.PublicEvidence); err != nil {
			return modelrouting.Plan{}, modelrouting.Route{}, domainError(ErrUnavailable, "canonical_media_route_invalid")
		}
		if job.CanonicalRoute != nil && !sameCanonicalMediaRoute(*job.CanonicalRoute, *route.PublicEvidence) {
			continue
		}
		return plan, route, nil
	}
	return modelrouting.Plan{}, modelrouting.Route{}, domainError(ErrForbidden, "canonical_media_route_changed")
}

// The planner hash includes its database clock. Compare the exact accepted
// route fields while keeping both original snapshots in the durable record.
func sameCanonicalMediaRoute(accepted, candidate executionevidence.EvidenceV2) bool {
	acceptedDigest, err := executionevidence.DigestV2(accepted)
	if err != nil {
		return false
	}
	candidate, err = executionevidence.NormalizeV2(candidate)
	if err != nil {
		return false
	}
	candidate.SnapshotHash = accepted.SnapshotHash
	currentDigest, err := executionevidence.DigestV2(candidate)
	return err == nil && acceptedDigest == currentDigest
}

func (c *CanonicalRoutes) revalidate(ctx context.Context, tx pgx.Tx, job Job, plan modelrouting.Plan, route modelrouting.Route) error {
	if c == nil || job.CanonicalRoute == nil {
		return domainError(ErrForbidden, "canonical_media_route_missing")
	}
	status, err := c.Coordinator.RevalidateMediaJobRoute(ctx, tx, plan, route, modelexecution.AttemptIdentity{
		OrgID: job.Scope.OrgID, RequestID: job.RequestID, AttemptID: job.JobID, APIKeyID: job.APIKeyID,
		Endpoint: modeloperation.PathMediaJobs, RouteSequence: 1, RouteTotal: 1,
		AccessCredentialID: nullableIDValue(job.AccessCredentialID), CredentialRef: job.CredentialRef,
		AccessGrantID: nullableIDValue(job.AccessGrantID),
	})
	if err != nil {
		return domainError(ErrUnavailable, "canonical_media_route_unavailable")
	}
	if status != modelexecution.Applied {
		return domainError(ErrForbidden, "canonical_media_route_changed")
	}
	return nil
}
