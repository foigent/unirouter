package sourcefacts

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/billing"
	"github.com/uniroute/unirouter/internal/executioncontext"
	"github.com/uniroute/unirouter/internal/executionevidence"
	"github.com/uniroute/unirouter/internal/modelrouting"
)

func (s *Service) observation(org, project int64, request, profile, subjectKind, subjectID, kind string, at time.Time) Observation {
	d := Observation{SchemaVersion: ObservationVersion, SourceInstance: s.options.SourceInstance, OrgID: executioncontext.ID(org), ProjectID: executioncontext.ID(project), RequestID: request, Profile: profile, SubjectKind: subjectKind, SubjectID: subjectID, Kind: kind, Revision: "1", OccurredAt: executioncontext.Time(at), RecordedAt: executioncontext.Time(at), Coverage: "observed", CaptureExpected: []string{kind}}
	d.SourceFactID = d.factID()
	d.EventID = d.eventID()
	return d
}

// MaterializeDispatchTx is called by the existing coordinator after its final
// route claim. The three frozen records prove registration, never network I/O.
// Their durable outbox insert shares that transaction; source publication occurs
// later in the original worker, without changing Authorize's RR snapshot.
func (s *Service) MaterializeDispatchTx(ctx context.Context, tx pgx.Tx, plan modelrouting.Plan, route modelrouting.Route, start billing.ProviderAttemptStart, evidenceHash string, at time.Time) error {
	if !s.ObservationsEnabled() {
		return nil
	}
	if tx == nil || !validHash(evidenceHash) {
		return errors.New("dispatch source requires exact transaction and evidence")
	}
	var project int64
	var contextWire []byte
	if e := tx.QueryRow(ctx, `SELECT project_id,source_context FROM freeze_idempotency WHERE org_id=$1 AND request_id=$2`, start.OrgID, start.RequestID).Scan(&project, &contextWire); e != nil {
		return e
	}
	if project <= 0 {
		return errors.New("dispatch source project is unavailable")
	}
	contextRef := ""
	if len(contextWire) > 0 {
		var snapshot executioncontext.Snapshot
		if e := json.Unmarshal(contextWire, &snapshot); e != nil {
			return e
		}
		if e := snapshot.Validate(); e != nil {
			return e
		}
		if snapshot.Core.OrgID != executioncontext.ID(start.OrgID) || snapshot.Core.ProjectID != executioncontext.ID(project) || snapshot.Core.RequestID != start.RequestID {
			return errors.New("dispatch source context scope differs")
		}
		contextRef = (Document{SourceInstance: s.options.SourceInstance, OrgID: snapshot.Core.OrgID, ProjectID: snapshot.Core.ProjectID, RequestID: start.RequestID, Profile: ContextProfile, Stage: "authenticated"}).factID()
	}
	makeObservation := func(profile, kind string) Observation {
		d := s.observation(start.OrgID, project, start.RequestID, profile, "provider_attempt", start.AttemptID, kind, at)
		d.ContextRef = contextRef
		d.RouteEvidenceHash = evidenceHash
		if contextRef == "" {
			d.Coverage = "partial"
		}
		return d
	}
	registered := makeObservation(AttemptProfile, "attempt_registered")
	registered.CaptureExpected = []string{"attempt_registered", "provider_response_received", "provider_rejected", "provider_outcome_unknown", "provider_response_unusable", "financial_result", "delivery_started", "local_delivery", "output_held", "round_trip_measurement"}
	registered.Attempt = &AttemptObservation{AttemptID: start.AttemptID, Operation: plan.ModelOperationID, RouteSequence: strconv.Itoa(start.RouteSequence), RouteTotal: strconv.Itoa(start.RouteTotal), Provider: "registered", LocalDelivery: "not_started", Financial: "reserved", EvidenceRef: start.AttemptID}

	evidence := executionevidence.EvidenceV1{}
	if start.RouteEvidence != nil {
		evidence = *start.RouteEvidence
	}
	if start.PublicRouteEvidence != nil {
		evidence = start.PublicRouteEvidence.EvidenceV1
	}
	capability := makeObservation(CapabilityProfile, "selection")
	modelRef := plan.PublicModel
	if plan.PublicModelID > 0 {
		modelRef = "model:" + executioncontext.ID(plan.PublicModelID)
	}
	ref := func(id, version string) ProfileReference {
		if id == "" {
			id = "not_available"
		}
		if version == "" {
			version = "not_available"
		}
		return ProfileReference{ID: id, Version: version}
	}
	c := &CapabilityObservation{ModelRef: modelRef, Provider: route.Provider, Operation: plan.ModelOperationID, Surface: start.Endpoint, Protocol: ref(evidence.ProtocolProfileID, evidence.ProtocolProfileVersion), ExecutionProfile: ref(plan.ExecutionProfileID, plan.ExecutionProfileVersion), ReleaseRef: evidence.RuntimeReleaseRef, EvidenceState: "partial"}
	c.RequiredCapabilities = append([]string{}, plan.RequiredCapabilities...)
	slices.Sort(c.RequiredCapabilities)
	c.Capabilities = make([]CapabilityReference, 0, len(evidence.CapabilityEvidence))
	c.CapabilityEvidenceHash = evidence.CapabilityEvidenceHash
	for _, entry := range evidence.CapabilityEvidence {
		verified := ""
		if entry.VerifiedAt != nil {
			verified = executioncontext.Time(*entry.VerifiedAt)
		}
		c.Capabilities = append(c.Capabilities, CapabilityReference{CapabilityID: entry.CapabilityID, SupportState: entry.SupportState, EvidenceState: entry.EvidenceState, EvidenceRefSHA256: fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(entry.EvidenceRef))), VerifiedAt: verified, Revision: executioncontext.ID(entry.Revision)})
	}
	slices.SortFunc(c.Capabilities, func(a, b CapabilityReference) int { return strings.Compare(a.CapabilityID, b.CapabilityID) })
	if plan.ReleaseExecution != nil {
		c.ExecutionProfile.SHA256 = plan.ReleaseExecution.ProfileDefinitionDigest
	}
	if evidence.SlotVersion > 0 {
		c.SlotVersion = executioncontext.ID(evidence.SlotVersion)
	}
	if evidence.CredentialProbeID > 0 {
		c.ProbeRef = "probe:" + executioncontext.ID(evidence.CredentialProbeID)
	}
	if start.PublicRouteEvidence != nil {
		c.BindingRef = "public-binding:" + executioncontext.ID(start.PublicRouteEvidence.PublicExecutionBindingID)
	}
	if route.ProductionApprovalID > 0 {
		c.ApprovalRef = "approval:" + executioncontext.ID(route.ProductionApprovalID)
	}
	if c.Protocol.ID != "not_available" && c.ExecutionProfile.ID != "not_available" && c.ReleaseRef != "" {
		c.EvidenceState = "available"
	}
	capability.Capability = c
	origin, e := DisclosureOrigin(route.BaseURL)
	if e != nil {
		return e
	}
	destination := makeObservation(DestinationProfile, "dispatch_registered")
	destination.CaptureExpected = []string{"dispatch_registered", "dispatch_result"}
	destination.Destination = &DestinationObservation{Provider: route.Provider, Origin: origin, RegionSource: "not_available", Dispatch: "registered", PolicyEvidence: "not_observed", InputBindingState: "unknown"}
	return s.enqueueDispatchTx(ctx, tx, start.AttemptID, []Observation{registered, capability, destination})
}
