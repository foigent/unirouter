package agenttrust

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/agentcap"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/governancesim"
	"github.com/uniroute/unirouter/internal/signing"
)

var (
	portableIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,159}$`)
	digestPattern     = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	unsignedPattern   = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
)

type authorityAnalysis struct {
	tree         []AuthorityNodeReport
	diffs        []AttenuationChange
	budgets      []BudgetProof
	timeline     []RevocationRecord
	stateByAgent map[string]string
	snapshotHash string
}

type reviewAnalysis struct {
	authority authorityAnalysis
	twin      governancesim.Report
	decision  string
	payload   DecisionReceiptPayload
}

type receiptSignatureInput struct {
	Algorithm    string                 `json:"algorithm"`
	KeyID        string                 `json:"key_id"`
	KeyVersion   string                 `json:"key_version"`
	TrustProfile string                 `json:"trust_profile"`
	Payload      DecisionReceiptPayload `json:"payload"`
}

// Evaluate verifies one metadata-only trust review without reading runtime
// authority, making a provider call, or changing policy/release state.
func Evaluate(body []byte) (Report, error) {
	request, err := decodeRequest(body)
	if err != nil {
		return Report{}, err
	}
	analysis, err := analyze(request)
	if err != nil {
		return Report{}, err
	}
	verification, err := verifyReceipt(request, analysis.payload)
	if err != nil {
		return Report{}, err
	}
	semanticHash, err := canonicalHash(request)
	if err != nil {
		return Report{}, contractError(ErrInvalid, "canonicalization_failed")
	}
	return Report{
		ReportVersion:                ReportVersion,
		ContractVersion:              ContractVersion,
		ReviewID:                     request.ReviewID,
		TenantRef:                    request.TenantRef,
		InputPayloadHash:             hash(body),
		InputSemanticHash:            semanticHash,
		AuthoritySnapshotHash:        analysis.authority.snapshotHash,
		EffectiveAuthorityTree:       analysis.authority.tree,
		AttenuationDiffs:             analysis.authority.diffs,
		AncestorBudgets:              analysis.authority.budgets,
		RevocationTimeline:           analysis.authority.timeline,
		DigitalTwin:                  digitalTwinProof(analysis.twin),
		SignedReceipt:                request.SignedReceipt,
		ReceiptVerification:          verification,
		Decision:                     analysis.decision,
		ReplayStatus:                 ReplayAccepted,
		SynchronousRuntimeDependency: false,
		RuntimeEnforcementAuthorized: false,
		RuntimeMutation:              RuntimeMutationNone,
	}, nil
}

// BuildReceiptPayload returns the exact canonical metadata payload that a
// separately authorized signer may sign. It never reads or creates key
// material.
func BuildReceiptPayload(request ReviewRequest) (DecisionReceiptPayload, error) {
	analysis, err := analyze(request)
	if err != nil {
		return DecisionReceiptPayload{}, err
	}
	return analysis.payload, nil
}

// SignDecisionReceipt signs an already-built payload with caller-owned
// Ed25519 material. No signing key is stored or returned.
func SignDecisionReceipt(
	payload DecisionReceiptPayload,
	keyID string,
	keyVersion string,
	privateKey ed25519.PrivateKey,
	trustProfile string,
) (SignedDecisionReceipt, error) {
	if !signing.ValidKeyID(keyID) {
		return SignedDecisionReceipt{}, contractError(ErrInvalid, "receipt_key_id_invalid")
	}
	if _, err := parsePositiveInt64(keyVersion); err != nil {
		return SignedDecisionReceipt{}, contractError(ErrInvalid, "receipt_key_version_invalid")
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		return SignedDecisionReceipt{}, contractError(ErrInvalid, "receipt_private_key_invalid")
	}
	if trustProfile != "fixture_local" {
		return SignedDecisionReceipt{}, contractError(ErrInvalid, "receipt_trust_profile_invalid")
	}
	signingInput := receiptSignatureInput{
		Algorithm: "Ed25519", KeyID: keyID, KeyVersion: keyVersion,
		TrustProfile: trustProfile, Payload: payload,
	}
	canonical, err := canonicalBytes(signingInput)
	if err != nil {
		return SignedDecisionReceipt{}, contractError(ErrInvalid, "receipt_canonicalization_failed")
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	return SignedDecisionReceipt{
		Payload:      payload,
		Algorithm:    "Ed25519",
		KeyID:        keyID,
		KeyVersion:   keyVersion,
		PublicKey:    base64.StdEncoding.EncodeToString(publicKey),
		Signature:    base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, canonical)),
		TrustProfile: trustProfile,
	}, nil
}

func decodeRequest(body []byte) (ReviewRequest, error) {
	if len(body) == 0 || len(body) > MaxInputBytes {
		return ReviewRequest{}, contractError(ErrInvalid, "input_size_invalid")
	}
	if strictErr := eventconnector.ValidateStrictJSON(body, eventconnector.StrictJSONOptions{}); strictErr != nil {
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

func analyze(request ReviewRequest) (reviewAnalysis, error) {
	if request.ContractVersion != ContractVersion {
		return reviewAnalysis{}, contractError(ErrInvalid, "contract_version_unsupported")
	}
	if !portableIDPattern.MatchString(request.ReviewID) {
		return reviewAnalysis{}, contractError(ErrInvalid, "review_id_invalid")
	}
	evaluatedAt, err := parseUTCMicros(request.EvaluatedAt)
	if err != nil {
		return reviewAnalysis{}, contractError(ErrInvalid, "evaluated_at_invalid")
	}
	if !digestPattern.MatchString(request.TenantRef) || !digestPattern.MatchString(request.SubjectAgentRef) ||
		!digestPattern.MatchString(request.ProvenanceRef) {
		return reviewAnalysis{}, contractError(ErrInvalid, "portable_identity_invalid")
	}
	if request.Audience != ReviewAudience {
		return reviewAnalysis{}, contractError(ErrInvalid, "audience_invalid")
	}
	authority, err := evaluateAuthority(request.Authority, request.SubjectAgentRef, evaluatedAt)
	if err != nil {
		return reviewAnalysis{}, err
	}
	twinBody, err := json.Marshal(request.DigitalTwin)
	if err != nil {
		return reviewAnalysis{}, contractError(ErrInvalid, "digital_twin_encoding_failed")
	}
	twin, err := governancesim.Evaluate(twinBody)
	if err != nil {
		return reviewAnalysis{}, contractError(ErrInvalid, "digital_twin_invalid")
	}
	decision := DecisionAllow
	if authority.stateByAgent[request.SubjectAgentRef] != NodeActive || twin.OverallRisk == governancesim.RiskBreaking {
		decision = DecisionDeny
	} else if twin.OverallRisk == governancesim.RiskReviewRequired {
		decision = DecisionShadowOnly
	}
	payload, err := buildReceiptPayload(request, authority.snapshotHash, twin, decision)
	if err != nil {
		return reviewAnalysis{}, err
	}
	return reviewAnalysis{authority: authority, twin: twin, decision: decision, payload: payload}, nil
}

func evaluateAuthority(snapshot AuthoritySnapshot, subjectRef string, evaluatedAt time.Time) (authorityAnalysis, error) {
	if !digestPattern.MatchString(snapshot.SnapshotRef) || !digestPattern.MatchString(snapshot.RootAgentRef) {
		return authorityAnalysis{}, contractError(ErrInvalid, "authority_snapshot_identity_invalid")
	}
	if len(snapshot.Nodes) == 0 || len(snapshot.Nodes) > 64 || len(snapshot.Revocations) > 64 {
		return authorityAnalysis{}, contractError(ErrInvalid, "authority_snapshot_size_invalid")
	}
	nodeByRef := make(map[string]AuthorityNode, len(snapshot.Nodes))
	reportByRef := make(map[string]AuthorityNodeReport, len(snapshot.Nodes))
	stateByRef := make(map[string]string, len(snapshot.Nodes))
	tree := make([]AuthorityNodeReport, 0, len(snapshot.Nodes))
	diffs := make([]AttenuationChange, 0, len(snapshot.Nodes)-1)
	budgets := make([]BudgetProof, 0, len(snapshot.Nodes))

	for index, node := range snapshot.Nodes {
		if !digestPattern.MatchString(node.AgentRef) || !digestPattern.MatchString(node.AuthorityVersion) {
			return authorityAnalysis{}, contractError(ErrInvalid, "authority_node_identity_invalid")
		}
		if _, duplicate := nodeByRef[node.AgentRef]; duplicate {
			return authorityAnalysis{}, contractError(ErrInvalid, "authority_node_duplicate")
		}
		issuedAt, issuedErr := parseUTCMicros(node.IssuedAt)
		expiresAt, expiresErr := parseUTCMicros(node.ExpiresAt)
		if issuedErr != nil || expiresErr != nil || !issuedAt.Before(expiresAt) || evaluatedAt.Before(issuedAt) {
			return authorityAnalysis{}, contractError(ErrInvalid, "authority_node_time_invalid")
		}
		normalized := node.Limits.Normalize()
		if !reflect.DeepEqual(normalized, node.Limits) || node.Limits.ValidateShape() != nil {
			return authorityAnalysis{}, contractError(ErrInvalid, "authority_limits_invalid")
		}
		for _, endpoint := range node.Limits.AllowedEndpoints {
			if !supportedAgentEndpoint(endpoint) {
				return authorityAnalysis{}, contractError(ErrInvalid, "authority_endpoint_invalid")
			}
		}
		var lineage []string
		state := NodeActive
		if index == 0 {
			if node.AgentRef != snapshot.RootAgentRef || node.ParentAgentRef != "" || node.Depth != 0 {
				return authorityAnalysis{}, contractError(ErrInvalid, "authority_root_invalid")
			}
			lineage = []string{node.AgentRef}
		} else {
			parent, exists := nodeByRef[node.ParentAgentRef]
			parentReport := reportByRef[node.ParentAgentRef]
			if !exists || node.Depth != parent.Depth+1 || node.Depth > agentcap.MaxAgentDepth ||
				!node.Limits.IsAttenuationOf(parent.Limits) || node.Limits.MaxDepth < node.Depth {
				return authorityAnalysis{}, contractError(ErrInvalid, "authority_attenuation_invalid")
			}
			parentExpiry, _ := parseUTCMicros(parent.ExpiresAt)
			if expiresAt.After(parentExpiry) {
				return authorityAnalysis{}, contractError(ErrInvalid, "authority_ttl_widened")
			}
			lineage = append(slices.Clone(parentReport.Lineage), node.AgentRef)
			if parentReport.State != NodeActive {
				state = NodeAncestorRevoked
			}
			diffs = append(diffs, attenuationDiff(parent, node))
		}
		revokedAt := time.Time{}
		if node.RevokedAt != "" {
			parsed, revokeErr := parseUTCMicros(node.RevokedAt)
			if revokeErr != nil || parsed.Before(issuedAt) || parsed.After(evaluatedAt) {
				return authorityAnalysis{}, contractError(ErrInvalid, "authority_revoked_at_invalid")
			}
			revokedAt = parsed
		}
		if !revokedAt.IsZero() {
			state = NodeRevoked
		} else if !evaluatedAt.Before(expiresAt) {
			state = NodeExpired
		}
		remaining := int64(expiresAt.Sub(evaluatedAt) / time.Second)
		if remaining < 0 {
			remaining = 0
		}
		effective := node.Limits
		if state != NodeActive {
			effective = agentcap.Limits{}
		}
		nodeReport := AuthorityNodeReport{
			AgentRef:            node.AgentRef,
			ParentAgentRef:      node.ParentAgentRef,
			Depth:               node.Depth,
			State:               state,
			Lineage:             lineage,
			EffectiveLimits:     effective,
			RemainingTTLSeconds: strconv.FormatInt(remaining, 10),
		}
		nodeByRef[node.AgentRef] = node
		reportByRef[node.AgentRef] = nodeReport
		stateByRef[node.AgentRef] = state
		tree = append(tree, nodeReport)
		budgets = append(budgets, BudgetProof{
			AgentRef:            node.AgentRef,
			Lineage:             slices.Clone(lineage),
			EffectiveLimits:     node.Limits,
			AncestorBudgetRefs:  slices.Clone(lineage),
			RemainingTTLSeconds: strconv.FormatInt(remaining, 10),
		})
	}
	if _, exists := nodeByRef[subjectRef]; !exists {
		return authorityAnalysis{}, contractError(ErrInvalid, "subject_agent_missing")
	}

	timeline := slices.Clone(snapshot.Revocations)
	if !slices.IsSortedFunc(timeline, func(a, b RevocationRecord) int {
		if a.RevokedAt == b.RevokedAt {
			return strings.Compare(a.EventRef, b.EventRef)
		}
		return strings.Compare(a.RevokedAt, b.RevokedAt)
	}) {
		return authorityAnalysis{}, contractError(ErrInvalid, "revocation_timeline_order_invalid")
	}
	revocationByAgent := make(map[string]RevocationRecord, len(timeline))
	for _, record := range timeline {
		node, exists := nodeByRef[record.AgentRef]
		if !exists || !digestPattern.MatchString(record.EventRef) || record.Reason == "" || len(record.Reason) > 128 ||
			node.RevokedAt == "" || node.RevokedAt != record.RevokedAt {
			return authorityAnalysis{}, contractError(ErrInvalid, "revocation_timeline_invalid")
		}
		if _, duplicate := revocationByAgent[record.AgentRef]; duplicate {
			return authorityAnalysis{}, contractError(ErrInvalid, "revocation_timeline_duplicate")
		}
		revocationByAgent[record.AgentRef] = record
	}
	for index := range tree {
		node := snapshot.Nodes[index]
		record, exists := revocationByAgent[node.AgentRef]
		if (node.RevokedAt != "") != exists {
			return authorityAnalysis{}, contractError(ErrInvalid, "revocation_evidence_missing")
		}
		if exists {
			tree[index].RevocationEventRef = record.EventRef
			reportByRef[node.AgentRef] = tree[index]
		}
	}
	snapshotHash, err := canonicalHash(snapshot)
	if err != nil {
		return authorityAnalysis{}, contractError(ErrInvalid, "authority_snapshot_canonicalization_failed")
	}
	return authorityAnalysis{
		tree: tree, diffs: diffs, budgets: budgets, timeline: timeline,
		stateByAgent: stateByRef, snapshotHash: snapshotHash,
	}, nil
}

func supportedAgentEndpoint(endpoint string) bool {
	switch endpoint {
	case "/v1/chat/completions", "/v1/embeddings", "/v1/models":
		return true
	default:
		return false
	}
}

func attenuationDiff(parent, child AuthorityNode) AttenuationChange {
	fields := make([]string, 0, 8)
	if child.Limits.MaxDepth < parent.Limits.MaxDepth {
		fields = append(fields, "max_depth")
	}
	if child.Limits.MaxChildren < parent.Limits.MaxChildren {
		fields = append(fields, "max_children")
	}
	if child.Limits.MaxDescendants < parent.Limits.MaxDescendants {
		fields = append(fields, "max_descendants")
	}
	if child.Limits.CostBudgetCents < parent.Limits.CostBudgetCents {
		fields = append(fields, "cost_budget_cents")
	}
	if child.Limits.TokenBudget < parent.Limits.TokenBudget {
		fields = append(fields, "token_budget")
	}
	if child.Limits.RPMLimit < parent.Limits.RPMLimit {
		fields = append(fields, "rpm_limit")
	}
	if child.Limits.RPSLimit < parent.Limits.RPSLimit {
		fields = append(fields, "rps_limit")
	}
	if child.Limits.ConcurrencyLimit < parent.Limits.ConcurrencyLimit {
		fields = append(fields, "concurrency_limit")
	}
	return AttenuationChange{
		AgentRef:         child.AgentRef,
		ParentAgentRef:   parent.AgentRef,
		ReducedFields:    fields,
		RemovedModels:    setDifference(parent.Limits.AllowedModels, child.Limits.AllowedModels),
		RemovedEndpoints: setDifference(parent.Limits.AllowedEndpoints, child.Limits.AllowedEndpoints),
	}
}

func setDifference(parent, child []string) []string {
	result := make([]string, 0)
	for _, value := range parent {
		if !slices.Contains(child, value) {
			result = append(result, value)
		}
	}
	return result
}

func buildReceiptPayload(
	request ReviewRequest,
	authorityHash string,
	twin governancesim.Report,
	decision string,
) (DecisionReceiptPayload, error) {
	governanceHash, err := canonicalHash(request.DigitalTwin)
	if err != nil {
		return DecisionReceiptPayload{}, contractError(ErrInvalid, "governance_request_canonicalization_failed")
	}
	baselinePolicyHash, err := canonicalHash(request.DigitalTwin.Baseline.Policy)
	if err != nil {
		return DecisionReceiptPayload{}, contractError(ErrInvalid, "baseline_policy_canonicalization_failed")
	}
	candidatePolicyHash, err := canonicalHash(request.DigitalTwin.Candidate.Policy)
	if err != nil {
		return DecisionReceiptPayload{}, contractError(ErrInvalid, "candidate_policy_canonicalization_failed")
	}
	baselineRouteHash, err := canonicalHash(request.DigitalTwin.Baseline.RouteSnapshot)
	if err != nil {
		return DecisionReceiptPayload{}, contractError(ErrInvalid, "baseline_route_canonicalization_failed")
	}
	candidateRouteHash, err := canonicalHash(request.DigitalTwin.Candidate.RouteSnapshot)
	if err != nil {
		return DecisionReceiptPayload{}, contractError(ErrInvalid, "candidate_route_canonicalization_failed")
	}
	packSetHash, err := canonicalHash(struct {
		Baseline  []governancesim.ProviderPackSnapshot `json:"baseline"`
		Candidate []governancesim.ProviderPackSnapshot `json:"candidate"`
	}{
		Baseline:  request.DigitalTwin.Baseline.ProviderPacks,
		Candidate: request.DigitalTwin.Candidate.ProviderPacks,
	})
	if err != nil {
		return DecisionReceiptPayload{}, contractError(ErrInvalid, "provider_pack_set_canonicalization_failed")
	}
	return DecisionReceiptPayload{
		ReceiptVersion:          ReceiptVersion,
		ReceiptID:               request.ReviewID + ".receipt",
		ReviewID:                request.ReviewID,
		TenantRef:               request.TenantRef,
		Audience:                request.Audience,
		Decision:                decision,
		DecidedAt:               request.EvaluatedAt,
		SubjectRef:              request.SubjectAgentRef,
		AuthoritySnapshotHash:   authorityHash,
		GovernanceRequestHash:   governanceHash,
		BaselinePolicyHash:      baselinePolicyHash,
		CandidatePolicyHash:     candidatePolicyHash,
		BaselineRouteHash:       baselineRouteHash,
		CandidateRouteHash:      candidateRouteHash,
		ProviderPackSetHash:     packSetHash,
		ProvenanceRef:           request.ProvenanceRef,
		SynchronousDependency:   false,
		RuntimeActionAuthorized: false,
	}, nil
}

func verifyReceipt(request ReviewRequest, expected DecisionReceiptPayload) (ReceiptVerification, error) {
	receipt := request.SignedReceipt
	if receipt.Payload != expected {
		return ReceiptVerification{}, contractError(ErrInvalid, "receipt_payload_mismatch")
	}
	if receipt.Algorithm != "Ed25519" || !signing.ValidKeyID(receipt.KeyID) {
		return ReceiptVerification{}, contractError(ErrInvalid, "receipt_signature_profile_invalid")
	}
	if _, err := parsePositiveInt64(receipt.KeyVersion); err != nil {
		return ReceiptVerification{}, contractError(ErrInvalid, "receipt_key_version_invalid")
	}
	if receipt.TrustProfile != "fixture_local" {
		return ReceiptVerification{}, contractError(ErrInvalid, "receipt_trust_profile_invalid")
	}
	publicKeyBytes, err := base64.StdEncoding.DecodeString(receipt.PublicKey)
	if err != nil || len(publicKeyBytes) != ed25519.PublicKeySize ||
		base64.StdEncoding.EncodeToString(publicKeyBytes) != receipt.PublicKey {
		return ReceiptVerification{}, contractError(ErrInvalid, "receipt_public_key_invalid")
	}
	signature, err := base64.StdEncoding.DecodeString(receipt.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize ||
		base64.StdEncoding.EncodeToString(signature) != receipt.Signature {
		return ReceiptVerification{}, contractError(ErrInvalid, "receipt_signature_invalid")
	}
	signingInput := receiptSignatureInput{
		Algorithm: receipt.Algorithm, KeyID: receipt.KeyID, KeyVersion: receipt.KeyVersion,
		TrustProfile: receipt.TrustProfile, Payload: receipt.Payload,
	}
	canonical, err := canonicalBytes(signingInput)
	if err != nil {
		return ReceiptVerification{}, contractError(ErrInvalid, "receipt_canonicalization_failed")
	}
	if !ed25519.Verify(ed25519.PublicKey(publicKeyBytes), canonical, signature) {
		return ReceiptVerification{}, contractError(ErrInvalid, "receipt_signature_mismatch")
	}
	payloadCanonical, err := canonicalBytes(receipt.Payload)
	if err != nil {
		return ReceiptVerification{}, contractError(ErrInvalid, "receipt_canonicalization_failed")
	}
	return ReceiptVerification{
		ReceiptVersion:         receipt.Payload.ReceiptVersion,
		ReceiptID:              receipt.Payload.ReceiptID,
		Algorithm:              receipt.Algorithm,
		KeyID:                  receipt.KeyID,
		KeyVersion:             receipt.KeyVersion,
		PayloadHash:            hash(payloadCanonical),
		SignatureValid:         true,
		TenantBound:            receipt.Payload.TenantRef == request.TenantRef,
		AudienceBound:          receipt.Payload.Audience == request.Audience,
		DecisionBound:          receipt.Payload.Decision == expected.Decision,
		CryptographicallyValid: true,
		TrustProfile:           receipt.TrustProfile,
		RuntimeTrusted:         false,
	}, nil
}

func digitalTwinProof(report governancesim.Report) DigitalTwinProof {
	return DigitalTwinProof{
		ContractVersion:       report.ContractVersion,
		InputSemanticHash:     report.InputSemanticHash,
		EvaluationMode:        report.EvaluationMode,
		OverallRisk:           report.OverallRisk,
		RouteChanges:          slices.Clone(report.RouteChanges),
		ProviderPackChanges:   slices.Clone(report.ProviderPackChanges),
		ReleaseDecision:       report.Release.Decision,
		RuntimeMutation:       report.RuntimeMutation,
		SynchronousDependency: false,
	}
}

func canonicalHash(value any) (string, error) {
	canonical, err := canonicalBytes(value)
	if err != nil {
		return "", err
	}
	return hash(canonical), nil
}

func canonicalBytes(value any) ([]byte, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	normalized, err := normalizeCanonicalNumbers(decoded)
	if err != nil {
		return nil, err
	}
	canonical, connectorErr := eventconnector.CanonicalJSON(normalized)
	if connectorErr != nil {
		return nil, connectorErr
	}
	return canonical, nil
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
