package admin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/uniroute/unirouter/internal/upstreamrelease"
	"slices"
	"sort"

	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelcapabilityqualification"
)

const sharedProbeVersion = "unirouter.shared-capability-observation.v1"

type capabilityProbeGroup struct {
	PlanDigest       string   `json:"plan_digest"`
	RequestDigest    string   `json:"request_digest"`
	CapabilityIDs    []string `json:"capability_ids"`
	ProbeContract    string   `json:"probe_contract"`
	ProviderRequests int      `json:"planned_provider_requests"`
	snapshots        []modelCapabilitySnapshot
	body             []byte
}
type capabilityProbePlan struct {
	DeploymentID         int64                  `json:"deployment_id"`
	OperationID          string                 `json:"operation_id"`
	CredentialBindingID  int64                  `json:"credential_binding_id"`
	Groups               []capabilityProbeGroup `json:"groups"`
	SeparateCapabilities []string               `json:"separate_capabilities"`
}
type sharedProbeMetadata struct {
	Version            string   `json:"version"`
	SourceRef          string   `json:"source_ref"`
	PlanDigest         string   `json:"plan_digest"`
	RequestDigest      string   `json:"request_digest"`
	CapabilityIDs      []string `json:"capability_ids"`
	AnchorCapabilityID string   `json:"anchor_capability_id"`
	DispatchState      string   `json:"dispatch_state"`
	OutcomeAmbiguous   bool     `json:"outcome_ambiguous"`
}
type capabilityProbeGroupResult struct {
	Observation sharedProbeMetadata          `json:"observation"`
	Results     []modelCapabilityProbeResult `json:"results"`
}

func probeDigest(v any) string {
	b, _ := json.Marshal(v)
	d := sha256.Sum256(b)
	return hex.EncodeToString(d[:])
}
func probeRequestDigest(body []byte) string {
	d := sha256.Sum256(body)
	return hex.EncodeToString(d[:])
}

// Only hashes leave this function. Secrets and server-owned request bytes never
// enter the public plan; exact identity and each support revision still pin it.
func probeMemberPin(s modelCapabilitySnapshot) string {
	return probeDigest([]any{upstreamrelease.Identity(s.UpstreamObservation), s.CustomerOrgID, s.DeploymentID, s.PublicModelID, s.ConnectionID,
		s.CredentialBindingID, s.Provider, s.BaseURL, s.AdapterType, string(s.AdapterConfig),
		s.ConnectionRevision, s.UpstreamModel, s.DeploymentRevision, s.OperationID, s.OperationRevision,
		s.ProtocolProfileID, s.ProtocolProfileVersion, s.CapabilityID, s.CapabilitySupportRevision,
		s.ExecutionProfileID, s.ExecutionProfileVersion, s.Definition.Digest(), s.ProbeContractID,
		s.ProbeContractVersion, s.CredentialRevision, s.CredentialLinkCreatedAt, s.Actor})
}
func sameProbeGroupInputs(a, b modelCapabilitySnapshot) bool {
	b.CapabilityID = a.CapabilityID
	b.CapabilitySupportRevision = a.CapabilitySupportRevision
	return sameModelCapabilitySnapshot(a, b) && canonicalOperationProbeActorMatches(a.Actor, b.Actor)
}
func groupCapabilitySnapshots(snapshots []modelCapabilitySnapshot) ([]capabilityProbeGroup, []string, error) {
	groups := []capabilityProbeGroup{}
	separate := []string{}
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].CapabilityID < snapshots[j].CapabilityID })
	seen := map[string]bool{}
	for _, s := range snapshots {
		if seen[s.CapabilityID] {
			return nil, nil, errModelCapabilityInvalid
		}
		seen[s.CapabilityID] = true
		contract, err := modelcapabilityqualification.SharedStreamProbeContract(modelCapabilityProbeBindingFromSnapshot(s))
		if err != nil {
			separate = append(separate, s.CapabilityID)
			continue
		}
		body, err := buildModelCapabilityProbeRequestBody(s)
		if err != nil {
			return nil, nil, err
		}
		found := -1
		for i, g := range groups {
			if g.ProbeContract == contract && sameProbeGroupInputs(g.snapshots[0], s) && bytes.Equal(g.body, body) {
				found = i
				break
			}
		}
		if found < 0 {
			groups = append(groups, capabilityProbeGroup{ProbeContract: contract, RequestDigest: probeRequestDigest(body), ProviderRequests: 1, body: body})
			found = len(groups) - 1
		}
		groups[found].CapabilityIDs = append(groups[found].CapabilityIDs, s.CapabilityID)
		groups[found].snapshots = append(groups[found].snapshots, s)
	}
	for i, g := range groups {
		pins := []string{}
		for _, s := range g.snapshots {
			pins = append(pins, probeMemberPin(s))
		}
		groups[i].PlanDigest = probeDigest([]any{sharedProbeVersion, g.ProbeContract, g.RequestDigest, pins})
	}
	return groups, separate, nil
}

func (h *Handler) loadCapabilityProbePlan(ctx context.Context, orgID, actorID, deploymentID int64, operationID string, bindingID int64) (capabilityProbePlan, error) {
	plan := capabilityProbePlan{DeploymentID: deploymentID, OperationID: operationID, CredentialBindingID: bindingID, Groups: []capabilityProbeGroup{}, SeparateCapabilities: []string{}}
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return plan, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ`); err != nil {
		return plan, err
	}
	if err = validateCanonicalOperationProbeCustomerOrg(ctx, tx, orgID); err != nil {
		return plan, err
	}
	actor, err := lockCanonicalOperationProbeActor(ctx, tx, actorID, orgID)
	if err != nil {
		return plan, err
	}
	rows, err := tx.Query(ctx, `SELECT c.capability_id FROM model_deployment_capabilities c JOIN model_deployments d ON d.id=c.deployment_id WHERE d.customer_org_id=$1 AND d.id=$2 AND c.operation_id=$3 AND c.support_state='enabled' ORDER BY c.capability_id LIMIT 51`, orgID, deploymentID, operationID)
	if err != nil {
		return plan, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return plan, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return plan, err
	}
	if len(ids) == 0 {
		return plan, errModelCapabilityNotFound
	}
	if len(ids) > 50 {
		return plan, errModelCapabilityUnsupported
	}
	snapshots := []modelCapabilitySnapshot{}
	for _, id := range ids {
		s, e := lockModelCapabilitySnapshotRows(ctx, tx, orgID, deploymentID, operationID, id, bindingID)
		if errors.Is(e, errModelCapabilityNotFound) || errors.Is(e, errModelCapabilityUnsupported) {
			plan.SeparateCapabilities = append(plan.SeparateCapabilities, id)
			continue
		}
		if e != nil {
			return plan, e
		}
		s.Actor = actor
		if e = validateModelCapabilitySnapshot(s); e != nil {
			if errors.Is(e, errModelCapabilityUnsupported) {
				plan.SeparateCapabilities = append(plan.SeparateCapabilities, id)
				continue
			}
			return plan, e
		}
		snapshots = append(snapshots, s)
	}
	groups, separate, err := groupCapabilitySnapshots(snapshots)
	if err != nil {
		return plan, err
	}
	plan.Groups = groups
	plan.SeparateCapabilities = append(plan.SeparateCapabilities, separate...)
	sort.Strings(plan.SeparateCapabilities)
	if err = tx.Commit(ctx); err != nil {
		return plan, err
	}
	return plan, nil
}

func (h *Handler) probeCapabilityGroup(ctx context.Context, orgID, actorID, deploymentID int64, operationID string, bindingID int64, digest string, members []string, audit auditMeta) (capabilityProbeGroupResult, error) {
	result := capabilityProbeGroupResult{}
	plan, err := h.loadCapabilityProbePlan(ctx, orgID, actorID, deploymentID, operationID, bindingID)
	if err != nil {
		return result, err
	}
	var selected *capabilityProbeGroup
	for i := range plan.Groups {
		g := &plan.Groups[i]
		if g.PlanDigest == digest && slices.Equal(g.CapabilityIDs, members) {
			selected = g
			break
		}
	}
	if selected == nil {
		return result, errModelCapabilityConflict
	}
	sourceRef, err := middleware.NewOperationID()
	if err != nil {
		return result, err
	}
	outcome := h.executeModelCapabilityProbe(ctx, selected.snapshots[0], sourceRef)
	state := outcome.DispatchState
	if state == "" {
		state = "not_started"
	}
	result.Observation = sharedProbeMetadata{Version: sharedProbeVersion, SourceRef: sourceRef, PlanDigest: digest, RequestDigest: selected.RequestDigest, CapabilityIDs: slices.Clone(members), AnchorCapabilityID: members[0], DispatchState: state, OutcomeAmbiguous: outcome.OutcomeAmbiguous}
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ`); err != nil {
		return result, err
	}
	root, err := lockCanonicalOperationProbeRootCurrent(ctx, tx, orgID)
	if err != nil {
		return result, err
	}
	actor, present, active, err := lockCanonicalOperationProbeActorRow(ctx, tx, actorID)
	if err != nil {
		return result, err
	}
	current := root && present && active && canonicalOperationProbeActorAllowed(actor, orgID) && canonicalOperationProbeActorMatches(selected.snapshots[0].Actor, actor)
	// Lock/check the complete group before ANY member can project qualification.
	for _, s := range selected.snapshots {
		now, e := lockModelCapabilitySnapshotRows(ctx, tx, orgID, deploymentID, operationID, s.CapabilityID, bindingID)
		if e != nil && !errors.Is(e, errModelCapabilityNotFound) {
			return result, e
		}
		current = current && e == nil && sameModelCapabilitySnapshot(s, now) && validateModelCapabilitySnapshot(now) == nil
	}
	for _, s := range selected.snapshots {
		row, e := h.persistModelCapabilityProbeTx(ctx, tx, actorID, s, outcome, sourceRef, audit, &result.Observation, current)
		if e != nil {
			return capabilityProbeGroupResult{Observation: result.Observation}, e
		}
		result.Results = append(result.Results, row)
	}
	if err = tx.Commit(ctx); err != nil {
		return capabilityProbeGroupResult{Observation: result.Observation}, fmt.Errorf("commit shared capability observation: %w", err)
	}
	if current && outcome.ContractSuccess && h.routingController != nil {
		h.routingController.Invalidate()
	}
	return result, nil
}

// A group is resolved only by the complete immutable set, never by latest rows.
func validateSharedProbeRows(metadata []sharedProbeMetadata, capabilities []string) error {
	if len(metadata) == 0 || len(metadata) != len(capabilities) {
		return errModelCapabilityNotFound
	}
	first := metadata[0]
	if first.Version != sharedProbeVersion || !physicalProbeRefPattern.MatchString(first.SourceRef) || !sharedProbeDigestPattern.MatchString(first.PlanDigest) || !sharedProbeDigestPattern.MatchString(first.RequestDigest) || len(first.CapabilityIDs) != len(metadata) || first.AnchorCapabilityID != first.CapabilityIDs[0] {
		return errModelCapabilityConflict
	}
	if first.DispatchState != "not_started" && first.DispatchState != "started" && first.DispatchState != "response_received" {
		return errModelCapabilityConflict
	}
	for i, m := range metadata {
		if probeDigest(m) != probeDigest(first) || capabilities[i] != first.CapabilityIDs[i] || (i > 0 && capabilities[i] <= capabilities[i-1]) {
			return errModelCapabilityConflict
		}
	}
	return nil
}
