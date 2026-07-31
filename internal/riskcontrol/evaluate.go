package riskcontrol

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/agenttrust"
	"github.com/uniroute/unirouter/internal/completedfeed"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/governancesim"
	"github.com/uniroute/unirouter/internal/provenancepassport"
)

var (
	portableIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,159}$`)
	digestPattern     = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	unsignedPattern   = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
)

// Evaluate composes immutable local evidence into advice and a reviewable
// proposal. It does not read runtime policy, call a provider or risk service,
// persist evidence, send shadow traffic, promote policy, or enforce a result.
func Evaluate(body []byte) (Report, error) {
	request, err := decodeRequest(body)
	if err != nil {
		return Report{}, err
	}
	evaluatedAt, err := validateRequestShape(request)
	if err != nil {
		return Report{}, err
	}

	agentBody, err := json.Marshal(request.AgentTrust)
	if err != nil {
		return Report{}, contractError(ErrInvalid, "agent_trust_encoding_failed")
	}
	agentReport, err := agenttrust.Evaluate(agentBody)
	if err != nil {
		return Report{}, contractError(ErrInvalid, "agent_trust_invalid")
	}
	passportBody, err := json.Marshal(request.ProvenancePassport)
	if err != nil {
		return Report{}, contractError(ErrInvalid, "provenance_passport_encoding_failed")
	}
	passportReport, err := provenancepassport.Evaluate(passportBody)
	if err != nil {
		return Report{}, contractError(ErrInvalid, "provenance_passport_invalid")
	}
	sourceScope, err := validateEvidenceBinding(request, agentReport, passportReport)
	if err != nil {
		return Report{}, err
	}

	agentReportHash, err := stableJSONHash(agentReport)
	if err != nil {
		return Report{}, contractError(ErrInvalid, "agent_trust_report_hash_failed")
	}
	passportReportHash, err := stableJSONHash(passportReport)
	if err != nil {
		return Report{}, contractError(ErrInvalid, "provenance_passport_report_hash_failed")
	}
	bindingHash, err := canonicalHash(request.EvidenceBinding)
	if err != nil {
		return Report{}, contractError(ErrInvalid, "evidence_binding_hash_failed")
	}
	inputSemanticHash, err := canonicalHash(request)
	if err != nil {
		return Report{}, contractError(ErrInvalid, "input_canonicalization_failed")
	}

	dimensions := classifyRisk(agentReport, passportReport)
	advice := decideAdvice(agentReport, passportReport, dimensions)
	assertion, err := buildAssertion(
		request,
		evaluatedAt,
		agentReport,
		agentReportHash,
		passportReportHash,
		bindingHash,
		advice,
		dimensions,
	)
	if err != nil {
		return Report{}, err
	}
	proposal, err := buildProposal(
		request,
		agentReport,
		passportReport,
		assertion,
		agentReportHash,
		passportReportHash,
	)
	if err != nil {
		return Report{}, err
	}

	report := Report{
		ReportVersion:       ReportVersion,
		ContractVersion:     ContractVersion,
		ReviewID:            request.ReviewID,
		TenantRef:           request.TenantRef,
		InputPayloadHash:    hash(body),
		InputSemanticHash:   inputSemanticHash,
		EvidenceBindingHash: bindingHash,
		Evidence: EvidenceSummary{
			AgentTrustDecision:            agentReport.Decision,
			AgentTrustReportHash:          agentReportHash,
			ReceiptPayloadHash:            agentReport.ReceiptVerification.PayloadHash,
			ReceiptCryptographicallyValid: agentReport.ReceiptVerification.CryptographicallyValid,
			ReceiptRuntimeTrusted:         agentReport.ReceiptVerification.RuntimeTrusted,
			ProvenancePassportHash:        passportReportHash,
			SourceHash:                    passportReport.Source.SourceHash,
			EnvelopeHash:                  passportReport.Source.EnvelopeHash,
			TerminalStatus:                passportReport.Source.TerminalStatus,
			CaptureGapCount:               len(passportReport.CaptureGaps),
			GovernanceRequestHash:         agentReport.SignedReceipt.Payload.GovernanceRequestHash,
			GovernanceOverallRisk:         agentReport.DigitalTwin.OverallRisk,
			SourceScope:                   sourceScope,
		},
		Advice:          advice,
		Dimensions:      dimensions,
		Assertion:       assertion,
		Proposal:        proposal,
		ReplayStatus:    ReplayAccepted,
		RuntimeBoundary: offlineRuntimeBoundary(),
	}
	if containsRestrictedContent(report) {
		return Report{}, contractError(ErrConflict, "restricted_content_exposed")
	}
	return report, nil
}

func decodeRequest(body []byte) (ReviewRequest, error) {
	if len(body) == 0 || len(body) > MaxInputBytes {
		return ReviewRequest{}, contractError(ErrInvalid, "input_size_invalid")
	}
	if strictErr := eventconnector.ValidateStrictJSON(
		body,
		eventconnector.StrictJSONOptions{RejectNull: true},
	); strictErr != nil {
		if strictErr.Code == "duplicate_json_key" {
			return ReviewRequest{}, contractError(ErrInvalid, "duplicate_json_key")
		}
		return ReviewRequest{}, contractError(ErrInvalid, "input_json_invalid")
	}
	var request ReviewRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return ReviewRequest{}, contractError(ErrInvalid, "input_shape_invalid")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return ReviewRequest{}, contractError(ErrInvalid, "trailing_json")
	}
	return request, nil
}

func validateRequestShape(request ReviewRequest) (time.Time, error) {
	if request.ContractVersion != ContractVersion {
		return time.Time{}, contractError(ErrInvalid, "contract_version_unsupported")
	}
	if !portableIDPattern.MatchString(request.ReviewID) {
		return time.Time{}, contractError(ErrInvalid, "review_id_invalid")
	}
	evaluatedAt, err := parseUTCMicros(request.EvaluatedAt)
	if err != nil {
		return time.Time{}, contractError(ErrInvalid, "evaluated_at_invalid")
	}
	if !digestPattern.MatchString(request.TenantRef) {
		return time.Time{}, contractError(ErrInvalid, "tenant_ref_invalid")
	}
	if request.Audience != ReviewAudience {
		return time.Time{}, contractError(ErrInvalid, "audience_invalid")
	}
	if request.PolicyVersion != PolicyVersion {
		return time.Time{}, contractError(ErrInvalid, "policy_version_unsupported")
	}
	if _, err := parsePositiveInt64(request.RiskEpoch); err != nil {
		return time.Time{}, contractError(ErrInvalid, "risk_epoch_invalid")
	}
	if request.AssertionTTLSeconds != AssertionTTL {
		return time.Time{}, contractError(ErrInvalid, "assertion_ttl_unsupported")
	}
	if request.AgentTrust.TenantRef != request.TenantRef {
		return time.Time{}, contractError(ErrConflict, "tenant_binding_mismatch")
	}
	if request.AgentTrust.EvaluatedAt != request.EvaluatedAt {
		return time.Time{}, contractError(ErrConflict, "evaluation_time_mismatch")
	}
	if request.ProvenancePassport.PassportID == "" {
		return time.Time{}, contractError(ErrInvalid, "provenance_passport_missing")
	}
	return evaluatedAt, nil
}

func validateEvidenceBinding(
	request ReviewRequest,
	agentReport agenttrust.Report,
	passportReport provenancepassport.Report,
) (SourceScopeEvidence, error) {
	binding := request.EvidenceBinding
	for _, digest := range []string{
		binding.ProvenanceRef,
		binding.AgentTrustInputSemanticHash,
		binding.AgentTrustReceiptPayloadHash,
		binding.PassportInputSemanticHash,
		binding.PassportSourceHash,
		binding.PassportEnvelopeHash,
		binding.PassportSourceScopeHash,
	} {
		if !digestPattern.MatchString(digest) {
			return SourceScopeEvidence{},
				contractError(ErrInvalid, "evidence_binding_digest_invalid")
		}
	}
	if binding.ProvenanceRef != request.AgentTrust.ProvenanceRef ||
		binding.ProvenanceRef != agentReport.SignedReceipt.Payload.ProvenanceRef {
		return SourceScopeEvidence{},
			contractError(ErrConflict, "provenance_ref_binding_mismatch")
	}
	if binding.AgentTrustInputSemanticHash != agentReport.InputSemanticHash {
		return SourceScopeEvidence{},
			contractError(ErrConflict, "agent_trust_semantic_hash_mismatch")
	}
	if binding.AgentTrustReceiptPayloadHash != agentReport.ReceiptVerification.PayloadHash {
		return SourceScopeEvidence{},
			contractError(ErrConflict, "agent_trust_receipt_hash_mismatch")
	}
	if binding.PassportInputSemanticHash != passportReport.InputSemanticHash {
		return SourceScopeEvidence{},
			contractError(ErrConflict, "passport_semantic_hash_mismatch")
	}
	if binding.PassportSourceHash != passportReport.Source.SourceHash {
		return SourceScopeEvidence{},
			contractError(ErrConflict, "passport_source_hash_mismatch")
	}
	if binding.PassportEnvelopeHash != passportReport.Source.EnvelopeHash {
		return SourceScopeEvidence{},
			contractError(ErrConflict, "passport_envelope_hash_mismatch")
	}
	sourceScope, err := extractSourceScope(request.ProvenancePassport)
	if err != nil {
		return SourceScopeEvidence{}, err
	}
	if binding.PassportSourceScopeHash != sourceScope.ScopeHash {
		return SourceScopeEvidence{},
			contractError(ErrConflict, "passport_source_scope_hash_mismatch")
	}
	if !agentReport.ReceiptVerification.CryptographicallyValid ||
		agentReport.ReceiptVerification.RuntimeTrusted ||
		agentReport.RuntimeEnforcementAuthorized ||
		agentReport.RuntimeMutation != agenttrust.RuntimeMutationNone {
		return SourceScopeEvidence{},
			contractError(ErrConflict, "agent_trust_runtime_boundary_invalid")
	}
	if !passportReport.Source.ExactBytesPreserved ||
		passportReport.RuntimeBoundary.SynchronousDependency ||
		passportReport.RuntimeBoundary.RuntimeMutation != provenancepassport.RuntimeMutationNone {
		return SourceScopeEvidence{},
			contractError(ErrConflict, "provenance_runtime_boundary_invalid")
	}
	return sourceScope, nil
}

func extractSourceScope(
	request provenancepassport.Request,
) (SourceScopeEvidence, error) {
	sourceBytes, err := base64.StdEncoding.Strict().DecodeString(
		request.SourceSnapshotBase64,
	)
	if err != nil {
		return SourceScopeEvidence{},
			contractError(ErrInvalid, "passport_source_scope_invalid")
	}
	snapshot, err := completedfeed.DecodeSnapshot(sourceBytes)
	if err != nil {
		return SourceScopeEvidence{},
			contractError(ErrInvalid, "passport_source_scope_invalid")
	}
	scope := SourceScopeEvidence{
		OrgRef:              strconv.FormatInt(snapshot.OrgID, 10),
		ProjectRef:          strconv.FormatInt(snapshot.ProjectID, 10),
		SourceInstance:      request.SourceInstance,
		TenantBindingStatus: ScopeBindingUnverified,
	}
	scopeHash, err := canonicalHash(struct {
		OrgRef         string `json:"org_ref"`
		ProjectRef     string `json:"project_ref"`
		SourceInstance string `json:"source_instance"`
	}{
		OrgRef: scope.OrgRef, ProjectRef: scope.ProjectRef,
		SourceInstance: scope.SourceInstance,
	})
	if err != nil {
		return SourceScopeEvidence{},
			contractError(ErrInvalid, "passport_source_scope_hash_failed")
	}
	scope.ScopeHash = scopeHash
	return scope, nil
}

func classifyRisk(
	agentReport agenttrust.Report,
	passportReport provenancepassport.Report,
) RiskDimensions {
	dimensions := RiskDimensions{
		Authority:  RiskLow,
		Change:     RiskLow,
		Provenance: RiskLow,
		Financial:  RiskLow,
	}
	switch agentReport.Decision {
	case agenttrust.DecisionDeny:
		dimensions.Authority = RiskHigh
	case agenttrust.DecisionShadowOnly:
		dimensions.Authority = RiskElevated
	}
	switch agentReport.DigitalTwin.OverallRisk {
	case governancesim.RiskBreaking:
		dimensions.Change = RiskHigh
	case governancesim.RiskReviewRequired:
		dimensions.Change = RiskElevated
	}
	if len(passportReport.CaptureGaps) > 0 || !passportReport.Source.TimelineComplete {
		dimensions.Provenance = RiskKnownGap
	}
	switch passportReport.Source.TerminalStatus {
	case "settled", "cancelled":
		dimensions.Financial = RiskLow
	default:
		dimensions.Financial = RiskHigh
	}
	return dimensions
}

func decideAdvice(
	agentReport agenttrust.Report,
	passportReport provenancepassport.Report,
	dimensions RiskDimensions,
) string {
	if agentReport.Decision == agenttrust.DecisionDeny ||
		dimensions.Authority == RiskHigh ||
		dimensions.Change == RiskHigh ||
		dimensions.Financial == RiskHigh {
		return AdviceDeny
	}
	if agentReport.Decision == agenttrust.DecisionShadowOnly ||
		dimensions.Change == RiskElevated ||
		dimensions.Provenance == RiskKnownGap ||
		len(passportReport.CaptureGaps) > 0 {
		return AdviceShadowOnly
	}
	return AdviceAllowBaseline
}

func buildAssertion(
	request ReviewRequest,
	evaluatedAt time.Time,
	agentReport agenttrust.Report,
	agentReportHash string,
	passportReportHash string,
	bindingHash string,
	advice string,
	dimensions RiskDimensions,
) (BoundedRiskAssertion, error) {
	ttl, _ := strconv.ParseInt(request.AssertionTTLSeconds, 10, 64)
	assertion := BoundedRiskAssertion{
		AssertionVersion:                    AssertionVersion,
		AssertionID:                         request.ReviewID + ".assertion",
		TenantRef:                           request.TenantRef,
		Audience:                            request.Audience,
		SubjectRef:                          request.AgentTrust.SubjectAgentRef,
		IssuedAt:                            request.EvaluatedAt,
		ExpiresAt:                           formatUTCMicros(evaluatedAt.Add(time.Duration(ttl) * time.Second)),
		RiskEpoch:                           request.RiskEpoch,
		Scope:                               AssertionScope,
		Advice:                              advice,
		Dimensions:                          dimensions,
		EvidenceBindingHash:                 bindingHash,
		AgentTrustReportHash:                agentReportHash,
		ProvenancePassportReportHash:        passportReportHash,
		GovernanceRequestHash:               agentReport.SignedReceipt.Payload.GovernanceRequestHash,
		AuthorityEffect:                     "downscope_only",
		SignatureState:                      "not_issued_offline",
		MissingEvidenceCanIncreasePrivilege: false,
		SynchronousDependency:               false,
		RuntimeEnforcementAuthorized:        false,
	}
	assertionHash, err := canonicalHash(assertion)
	if err != nil {
		return BoundedRiskAssertion{}, contractError(ErrInvalid, "risk_assertion_hash_failed")
	}
	assertion.AssertionHash = assertionHash
	return assertion, nil
}

func buildProposal(
	request ReviewRequest,
	agentReport agenttrust.Report,
	passportReport provenancepassport.Report,
	assertion BoundedRiskAssertion,
	agentReportHash string,
	passportReportHash string,
) (ControlProposal, error) {
	controls := make([]ProposedControl, 0, 4)
	if agentReport.Decision != agenttrust.DecisionAllow {
		controls = append(controls, ProposedControl{
			ControlID:      "authority-downscope-review",
			TargetOwner:    "unirouter.iam",
			ProposedEffect: "preserve_or_reduce_current_authority_only",
			RationaleCode:  "agent_trust_not_allow",
			EvidenceRefs:   []string{agentReportHash, assertion.AssertionHash},
		})
	}
	if agentReport.DigitalTwin.OverallRisk != governancesim.RiskUnchanged &&
		agentReport.DigitalTwin.OverallRisk != governancesim.RiskCompatible {
		controls = append(controls, ProposedControl{
			ControlID:      "route-candidate-shadow-review",
			TargetOwner:    "unirouter.routing",
			ProposedEffect: "evaluate_candidate_off_path_without_client_delivery",
			RationaleCode:  "governance_change_requires_review",
			EvidenceRefs: []string{
				agentReport.DigitalTwin.InputSemanticHash,
				agentReport.SignedReceipt.Payload.GovernanceRequestHash,
			},
		})
	}
	if len(passportReport.CaptureGaps) > 0 || !passportReport.Source.TimelineComplete {
		controls = append(controls, ProposedControl{
			ControlID:      "capture-gap-resolution-review",
			TargetOwner:    "unirouter.evidence",
			ProposedEffect: "review_declared_capture_gaps_before_any_promotion",
			RationaleCode:  "source_evidence_has_declared_gaps",
			EvidenceRefs:   []string{passportReportHash, passportReport.Source.SourceHash},
		})
	}
	controls = append(controls, ProposedControl{
		ControlID:      "tenant-source-scope-binding-review",
		TargetOwner:    "unirouter.iam",
		ProposedEffect: "verify_product_local_tenant_to_source_scope_mapping",
		RationaleCode:  "tenant_source_scope_binding_unverified",
		EvidenceRefs: []string{
			assertion.EvidenceBindingHash,
			passportReport.Source.SourceHash,
		},
	})
	if assertion.Advice == AdviceDeny && passportReport.Source.TerminalStatus != "settled" &&
		passportReport.Source.TerminalStatus != "cancelled" {
		controls = append(controls, ProposedControl{
			ControlID:      "financial-finality-review",
			TargetOwner:    "unirouter.billing",
			ProposedEffect: "hold_promotion_until_terminal_financial_state",
			RationaleCode:  "financial_finality_unknown",
			EvidenceRefs:   []string{passportReportHash},
		})
	}
	sort.Slice(controls, func(left, right int) bool {
		return controls[left].ControlID < controls[right].ControlID
	})

	state := ProposalNotCreated
	lifecycle := make([]ProposalLifecycleStep, 0, 3)
	if len(controls) > 0 {
		state = ProposalSimulatedReviewRequired
		lifecycle = append(lifecycle,
			ProposalLifecycleStep{
				State: "evidence_derived_draft", OccurredAt: request.EvaluatedAt,
				EvidenceRef: assertion.EvidenceBindingHash,
			},
			ProposalLifecycleStep{
				State: "offline_simulated", OccurredAt: request.EvaluatedAt,
				EvidenceRef: agentReport.DigitalTwin.InputSemanticHash,
			},
			ProposalLifecycleStep{
				State: "human_review_required", OccurredAt: request.EvaluatedAt,
				EvidenceRef: assertion.AssertionHash,
			},
		)
	}
	proposal := ControlProposal{
		ProposalVersion: ProposalVersion,
		ProposalID:      request.ReviewID + ".proposal",
		State:           state,
		Trigger:         "bounded_evidence_review",
		Controls:        controls,
		Lifecycle:       lifecycle,
		Simulation: SimulationEvidence{
			ContractVersion:   agentReport.DigitalTwin.ContractVersion,
			InputSemanticHash: agentReport.DigitalTwin.InputSemanticHash,
			EvaluationMode:    agentReport.DigitalTwin.EvaluationMode,
			OverallRisk:       agentReport.DigitalTwin.OverallRisk,
			ReleaseDecision:   agentReport.DigitalTwin.ReleaseDecision,
			RuntimeMutation:   agentReport.DigitalTwin.RuntimeMutation,
			ReusedDirectly:    true,
		},
		HumanReviewRequired:        len(controls) > 0,
		SelfPromoted:               false,
		RuntimePromotionAuthorized: false,
		PromotionGate:              PromotionGate,
	}
	proposalHash, err := canonicalHash(proposal)
	if err != nil {
		return ControlProposal{}, contractError(ErrInvalid, "control_proposal_hash_failed")
	}
	proposal.ProposalHash = proposalHash
	return proposal, nil
}

func offlineRuntimeBoundary() RuntimeBoundary {
	return RuntimeBoundary{
		Mode:                         "offline_advice_and_shadow_evidence",
		Persistent:                   false,
		RemoteRiskDependency:         false,
		ProviderOrShadowTraffic:      false,
		CredentialsAccepted:          false,
		RuntimePolicyRead:            false,
		RuntimeEnforcementAuthorized: false,
		RuntimePromotionAuthorized:   false,
		RuntimeMutation:              RuntimeMutationNone,
		ExternalDecisionGate:         PromotionGate,
	}
}

func canonicalHash(value any) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return "", err
	}
	normalized, err := normalizeCanonicalNumbers(decoded)
	if err != nil {
		return "", err
	}
	canonical, connectorErr := eventconnector.CanonicalJSON(normalized)
	if connectorErr != nil {
		return "", connectorErr
	}
	return hash(canonical), nil
}

// stableJSONHash binds reports emitted by already-versioned dependencies. Their
// byte-stable Go JSON reports may intentionally contain JSON null for a
// revoked empty authority slice, which RFC 8785 canonical event profiles reject.
func stableJSONHash(value any) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return hash(body), nil
}

func normalizeCanonicalNumbers(value any) (any, error) {
	switch typed := value.(type) {
	case json.Number:
		parsed, err := strconv.ParseInt(string(typed), 10, 64)
		if err != nil {
			return nil, errors.New("canonical integers required")
		}
		return parsed, nil
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			normalized, err := normalizeCanonicalNumbers(item)
			if err != nil {
				return nil, err
			}
			result[key] = normalized
		}
		return result, nil
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			normalized, err := normalizeCanonicalNumbers(item)
			if err != nil {
				return nil, err
			}
			result[index] = normalized
		}
		return result, nil
	default:
		return value, nil
	}
}

func hash(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func parseUTCMicros(value string) (time.Time, error) {
	if eventconnector.ValidateCanonicalMicrosecondTime(value) != nil {
		return time.Time{}, errors.New("six-digit UTC required")
	}
	return time.Parse("2006-01-02T15:04:05.000000Z", value)
}

func formatUTCMicros(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000000Z")
}

func parsePositiveInt64(value string) (int64, error) {
	if !unsignedPattern.MatchString(value) {
		return 0, errors.New("canonical positive int64 required")
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, errors.New("canonical positive int64 required")
	}
	return parsed, nil
}

func containsRestrictedContent(report Report) bool {
	body, err := json.Marshal(report)
	if err != nil {
		return true
	}
	lower := strings.ToLower(string(body))
	for _, restricted := range []string{
		"prompt_body",
		"response_body",
		"raw_prompt",
		"raw_response",
		"source_snapshot_base64",
		"private_key",
		"credential_value",
		"authorization",
	} {
		if strings.Contains(lower, restricted) {
			return true
		}
	}
	return false
}

func errorCode(err error) string {
	var contractErr *ContractError
	if errors.As(err, &contractErr) {
		return contractErr.Code
	}
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%T", err)
}
