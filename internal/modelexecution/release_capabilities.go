package modelexecution

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/releaseexecution"
)

func revalidatePinnedCapabilities(ctx context.Context, tx pgx.Tx, request preparedRequest) (Status, error) {
	for _, entry := range request.evidence.CapabilityEvidence {
		var found *releaseexecution.CapabilityPin
		for index := range request.plan.ReleaseExecution.CapabilityPins {
			pin := &request.plan.ReleaseExecution.CapabilityPins[index]
			if pin.DeploymentID == request.evidence.DeploymentID && pin.OperationID == request.underlying.id && pin.CapabilityID == entry.CapabilityID {
				found = pin
				break
			}
		}
		if found == nil {
			return Stale, nil
		}
		decision, err := releaseexecution.ReadCapabilityDecision(ctx, tx, releaseexecution.CapabilityTarget{
			CustomerOrgID: request.identity.OrgID, PublicModelID: request.plan.PublicModelID, DeploymentID: request.evidence.DeploymentID,
			OperationID: request.underlying.id, ExecutionProfileID: request.plan.ExecutionProfileID, ExecutionProfileVersion: request.plan.ExecutionProfileVersion,
			ProtocolProfileID: request.underlying.profileID, ProtocolProfileVersion: request.underlying.profileVersion}, found.ApprovalID, found, true)
		if err != nil {
			if errors.Is(err, releaseexecution.ErrCapabilityUnavailable) {
				return Stale, nil
			}
			return Stale, err
		}
		actual := decision.Entry
		if actual.CapabilityID != entry.CapabilityID || actual.SupportState != entry.SupportState || actual.EvidenceState != entry.EvidenceState || actual.EvidenceRef != entry.EvidenceRef || actual.Revision != entry.Revision || !sameTimePointer(actual.VerifiedAt, entry.VerifiedAt) {
			return Stale, nil
		}
	}
	return Applied, nil
}
