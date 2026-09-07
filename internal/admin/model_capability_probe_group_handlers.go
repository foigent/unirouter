package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/uniroute/unirouter/internal/middleware"
	"github.com/uniroute/unirouter/internal/modelcapabilityqualification"
)

var sharedProbeDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

var physicalProbeRefPattern = regexp.MustCompile(`^op_[0-9a-f]{32}$`)

type capabilityProbeGroupRequest struct {
	CredentialBindingID int64    `json:"credential_binding_id"`
	PlanDigest          string   `json:"plan_digest"`
	CapabilityIDs       []string `json:"capability_ids"`
}
type sharedProbeFact struct {
	CapabilityProbeID    int64  `json:"capability_probe_id"`
	CapabilityID         string `json:"capability_id"`
	SuccessAtObservation bool   `json:"success_at_observation"`
	StatusCode           int    `json:"status_code"`
	ErrorSummary         string `json:"error_summary,omitempty"`
}
type capabilityProbeGroupReadback struct {
	Observation sharedProbeMetadata `json:"observation"`
	Facts       []sharedProbeFact   `json:"facts"`
	// This is one physical response. Never add its usage once per fact.
	ResponseSummary       json.RawMessage                    `json:"response_summary"`
	CurrentQualifications []modelCapabilityQualificationView `json:"current_qualifications"`
}

func capabilityGroupPath(r *http.Request) (int64, string, error) {
	id, e := strconv.ParseInt(r.PathValue("deploymentID"), 10, 64)
	op := r.PathValue("operationID")
	if e != nil || id <= 0 || op == "" || len(op) > 80 || strings.ContainsAny(op, "\x00\r\n") {
		return 0, "", errModelCapabilityInvalid
	}
	return id, op, nil
}
func (h *Handler) GetCanonicalCapabilityProbePlan(w http.ResponseWriter, r *http.Request) {
	id, op, e := capabilityGroupPath(r)
	if e != nil {
		writeModelCapabilityError(w, e)
		return
	}
	binding, e := strconv.ParseInt(r.URL.Query().Get("credential_binding_id"), 10, 64)
	if e != nil || binding <= 0 {
		writeModelCapabilityError(w, errModelCapabilityInvalid)
		return
	}
	org, e := resolveActorOrganization(r.Context(), 0)
	if e != nil {
		writeOrganizationScopeError(w, e)
		return
	}
	plan, e := h.loadCapabilityProbePlan(r.Context(), org, middleware.GetUserIDFromContext(r.Context()), id, op, binding)
	if e != nil {
		writeModelCapabilityError(w, e)
		return
	}
	writeJSON(w, http.StatusOK, plan)
}
func (h *Handler) ProbeCanonicalCapabilityGroup(w http.ResponseWriter, r *http.Request) {
	id, op, e := capabilityGroupPath(r)
	if e != nil {
		writeModelCapabilityError(w, e)
		return
	}
	body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, modelCapabilityProbeMaxRequestBytes))
	var input capabilityProbeGroupRequest
	if e != nil || decodeStrictModelCapabilityObject(body, &input) != nil || input.CredentialBindingID <= 0 || !sharedProbeDigestPattern.MatchString(input.PlanDigest) || len(input.CapabilityIDs) == 0 || len(input.CapabilityIDs) > 50 {
		writeModelCapabilityError(w, errModelCapabilityInvalid)
		return
	}
	for i, c := range input.CapabilityIDs {
		if c == "" || len(c) > 80 || (i > 0 && c <= input.CapabilityIDs[i-1]) {
			writeModelCapabilityError(w, errModelCapabilityInvalid)
			return
		}
	}
	org, e := resolveActorOrganization(r.Context(), 0)
	if e != nil {
		writeOrganizationScopeError(w, e)
		return
	}
	result, e := h.probeCapabilityGroup(r.Context(), org, middleware.GetUserIDFromContext(r.Context()), id, op, input.CredentialBindingID, input.PlanDigest, input.CapabilityIDs, requestAuditMeta(r, org, 0))
	if e != nil {
		if result.Observation.SourceRef != "" {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": map[string]any{"code": "capability_probe_group_unconfirmed", "message": "Probe outcome is unconfirmed. Read this observation; do not automatically resend.", "source_ref": result.Observation.SourceRef, "dispatch_state": result.Observation.DispatchState}})
			return
		}
		writeModelCapabilityError(w, e)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (h *Handler) GetCanonicalCapabilityProbeGroup(w http.ResponseWriter, r *http.Request) {
	id, op, e := capabilityGroupPath(r)
	ref := r.PathValue("sourceRef")
	if e != nil || !physicalProbeRefPattern.MatchString(ref) {
		writeModelCapabilityError(w, errModelCapabilityInvalid)
		return
	}
	org, e := resolveActorOrganization(r.Context(), 0)
	if e != nil {
		writeOrganizationScopeError(w, e)
		return
	}
	result, e := h.loadCapabilityProbeGroup(r.Context(), org, id, op, ref)
	if e != nil {
		writeModelCapabilityError(w, e)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) loadCapabilityProbeGroup(ctx context.Context, orgID, deploymentID int64, operationID, ref string) (capabilityProbeGroupReadback, error) {
	result := capabilityProbeGroupReadback{Facts: []sharedProbeFact{}, CurrentQualifications: []modelCapabilityQualificationView{}}
	rows, err := h.db.Query(ctx, `SELECT capability_probe_id,capability_id,success,status_code,COALESCE(error_summary,''),response_summary FROM model_capability_probes WHERE customer_org_id=$1 AND deployment_id=$2 AND operation_id=$3 AND source_kind=$4 AND source_ref=$5 ORDER BY capability_id LIMIT 51`, orgID, deploymentID, operationID, modelcapabilityqualification.ProbeSourceKind, ref)
	if err != nil {
		return result, err
	}
	metadata := []sharedProbeMetadata{}
	ids := []string{}
	var responseDigest string
	for rows.Next() {
		var fact sharedProbeFact
		var summary json.RawMessage
		if err = rows.Scan(&fact.CapabilityProbeID, &fact.CapabilityID, &fact.SuccessAtObservation, &fact.StatusCode, &fact.ErrorSummary, &summary); err != nil {
			rows.Close()
			return result, err
		}
		var parsed struct {
			Shared *sharedProbeMetadata `json:"shared_observation"`
		}
		if json.Unmarshal(summary, &parsed) != nil || parsed.Shared == nil || parsed.Shared.SourceRef != ref {
			rows.Close()
			return result, errModelCapabilityConflict
		}
		var canonical any
		if json.Unmarshal(summary, &canonical) != nil {
			rows.Close()
			return result, errModelCapabilityConflict
		}
		digest := probeDigest(canonical)
		if responseDigest != "" && responseDigest != digest {
			rows.Close()
			return result, errModelCapabilityConflict
		}
		responseDigest = digest
		result.ResponseSummary = summary
		result.Facts = append(result.Facts, fact)
		metadata = append(metadata, *parsed.Shared)
		ids = append(ids, fact.CapabilityID)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	if len(metadata) > 50 {
		return result, errModelCapabilityConflict
	}
	if err = validateSharedProbeRows(metadata, ids); err != nil {
		return result, err
	}
	result.Observation = metadata[0]
	for _, id := range ids {
		view, e := h.loadModelCapabilityQualificationView(ctx, orgID, deploymentID, operationID, id)
		if e != nil {
			return result, e
		}
		result.CurrentQualifications = append(result.CurrentQualifications, view)
	}
	return result, nil
}
