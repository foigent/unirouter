package provenancepassport

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/uniroute/unirouter/internal/completedfeed"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/telemetryexport"
)

var passportIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,159}$`)

// Evaluate builds one deterministic passport from exact Source bytes. It does
// not read Feed delivery state, create a Cursor/Checkpoint, call a collector,
// register a Connector, or mutate runtime state.
func Evaluate(body []byte) (Report, error) {
	request, err := decodeRequest(body)
	if err != nil {
		return Report{}, err
	}
	sourceBytes, err := validateRequest(request)
	if err != nil {
		return Report{}, err
	}
	exactSource := append([]byte(nil), sourceBytes...)
	snapshot, err := completedfeed.DecodeSnapshot(sourceBytes)
	if err != nil {
		return Report{}, contractError(ErrInvalid, "source_snapshot_invalid")
	}
	projected, projectionErr := completedfeed.BuildReferenceEnvelopeV1(
		sourceBytes,
		request.SourceInstance,
	)
	if projectionErr != nil {
		return Report{}, contractError(ErrInvalid, "reference_projection_invalid")
	}
	if !bytes.Equal(sourceBytes, exactSource) ||
		projected.SourceHash != eventconnector.HashSourceBytes(exactSource) {
		return Report{}, contractError(ErrConflict, "source_bytes_changed")
	}
	fieldGroups, gaps, fidelity, dataPolicy, timelineComplete, err := extractPassportEvidence(projected.Envelope)
	if err != nil {
		return Report{}, err
	}
	binding := completedfeed.ExpectedReferenceRuntimeBindingV1()
	semanticHash, err := semanticHash(request)
	if err != nil {
		return Report{}, err
	}
	return Report{
		ReportVersion:     ReportVersion,
		ContractVersion:   ContractVersion,
		PassportID:        request.PassportID,
		InputPayloadHash:  eventconnector.SHA256Reference(body),
		InputSemanticHash: semanticHash,
		Source: SourceEvidence{
			Profile:             request.SourceProfile,
			SchemaID:            binding.SourceSchemaID,
			SchemaSHA256:        binding.SourceSchemaDigest,
			SourceInstance:      request.SourceInstance,
			SourceRevision:      request.SourceRevision,
			RequestRef:          snapshot.RequestID,
			TerminalStatus:      snapshot.Terminal.Status,
			SourceBytes:         len(sourceBytes),
			SourceHash:          projected.SourceHash,
			EnvelopeHash:        projected.EnvelopeHash,
			IdempotencyKey:      projected.IdempotencyKey,
			ExactBytesPreserved: true,
			AdapterFidelity:     fidelity,
			RawPayloadPresent:   dataPolicy.rawPayloadPresent,
			PayloadCaptureMode:  dataPolicy.payloadCaptureMode,
			TimelineComplete:    timelineComplete,
		},
		FieldGroups: fieldGroups,
		CaptureGaps: gaps,
		Compatibility: FeedCompatibility{
			Endpoint:                "/api/v1/integrations/xinsight/completed-executions",
			PageSchema:              completedfeed.PageSchemaVersion,
			ConnectorID:             binding.ConnectorID,
			ConnectorVersion:        binding.ConnectorVersion,
			EventSchemaRef:          binding.EventSchemaRef,
			EventSchemaVersion:      binding.EventSchemaVersion,
			ExistingDTOUnchanged:    true,
			SourceBytesUnchanged:    true,
			EventIdentityUnchanged:  true,
			FeedWireUnchanged:       true,
			PageBehaviorUnchanged:   true,
			CursorBehaviorUnchanged: true,
			CredentialUnchanged:     true,
		},
		Protocol: ProtocolEvidence{
			Repository:    "https://github.com/foigent/event-connector-spec",
			Version:       ProductProtocolVersion,
			Commit:        ProductProtocolCommit,
			ReleaseDigest: ProductProtocolDigest,
		},
		ReferenceConnector: referenceConnectorAttestation(binding),
		SecondConnector:    secondConnectorAttestation(),
		OpenInference: ProjectionEvidence{
			Profile:             OpenInferenceProfile,
			ContractVersion:     telemetryexport.SchemaVersion,
			GoldenFixtureSHA256: request.OpenInferenceEvidence,
			CreatesSourceTruth:  false,
			IsConnectorRuntime:  false,
			RawContentIncluded:  false,
		},
		ReplayStatus: ReplayAccepted,
		RuntimeBoundary: RuntimeBoundary{
			ProductRegistryConnectorIDs: []string{"unirouter.completedfeed.reference-v1"},
			SecondConnectorRegistered:   false,
			SecondConnectorRuntimeOwned: false,
			SynchronousDependency:       false,
			RuntimeMutation:             RuntimeMutationNone,
		},
	}, nil
}

type extractedDataPolicy struct {
	payloadCaptureMode string
	rawPayloadPresent  bool
}

func decodeRequest(body []byte) (Request, error) {
	if len(body) == 0 || len(body) > MaxInputBytes {
		return Request{}, contractError(ErrInvalid, "input_size_invalid")
	}
	if strictErr := eventconnector.ValidateStrictJSON(body, eventconnector.StrictJSONOptions{RejectNull: true}); strictErr != nil {
		if strictErr.Code == "duplicate_json_key" {
			return Request{}, contractError(ErrInvalid, "duplicate_json_key")
		}
		return Request{}, contractError(ErrInvalid, "input_json_invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var request Request
	if err := decoder.Decode(&request); err != nil {
		return Request{}, contractError(ErrInvalid, "input_shape_invalid")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Request{}, contractError(ErrInvalid, "trailing_json")
	}
	return request, nil
}

func validateRequest(request Request) ([]byte, error) {
	if request.ContractVersion != ContractVersion {
		return nil, contractError(ErrInvalid, "contract_version_unsupported")
	}
	if !passportIDPattern.MatchString(request.PassportID) {
		return nil, contractError(ErrInvalid, "passport_id_invalid")
	}
	if request.SourceProfile != SourceProfile {
		return nil, contractError(ErrInvalid, "source_profile_unsupported")
	}
	if request.SourceRevision != SourceRevision {
		return nil, contractError(ErrInvalid, "source_revision_unsupported")
	}
	if _, opaqueErr := eventconnector.NewOpaqueString(
		"source_instance",
		request.SourceInstance,
		1,
		128,
		true,
	); opaqueErr != nil {
		return nil, contractError(ErrInvalid, "source_instance_invalid")
	}
	if request.OpenInferenceEvidence != OpenInferenceGoldenDigest {
		return nil, contractError(ErrInvalid, "openinference_evidence_mismatch")
	}
	sourceBytes, err := base64.StdEncoding.Strict().DecodeString(request.SourceSnapshotBase64)
	if err != nil || base64.StdEncoding.EncodeToString(sourceBytes) != request.SourceSnapshotBase64 {
		return nil, contractError(ErrInvalid, "source_snapshot_base64_invalid")
	}
	if len(sourceBytes) == 0 || len(sourceBytes) > completedfeed.MaxSnapshotBytes {
		return nil, contractError(ErrInvalid, "source_snapshot_size_invalid")
	}
	return sourceBytes, nil
}

func extractPassportEvidence(
	envelope map[string]any,
) (FieldGroups, []CaptureGapEvidence, string, extractedDataPolicy, bool, error) {
	provenance, ok := envelope["provenance"].(map[string]any)
	if !ok {
		return FieldGroups{}, nil, "", extractedDataPolicy{}, false,
			contractError(ErrInvalid, "provenance_missing")
	}
	fidelity, ok := provenance["adapter_fidelity"].(string)
	if !ok || fidelity == "" {
		return FieldGroups{}, nil, "", extractedDataPolicy{}, false,
			contractError(ErrInvalid, "adapter_fidelity_missing")
	}
	rawGroups, ok := provenance["field_groups"].(map[string]any)
	if !ok {
		return FieldGroups{}, nil, "", extractedDataPolicy{}, false,
			contractError(ErrInvalid, "field_groups_missing")
	}
	groups := FieldGroups{
		Direct:   make([]FieldEvidence, 0),
		Derived:  make([]FieldEvidence, 0),
		Declared: make([]FieldEvidence, 0),
		Missing:  make([]FieldEvidence, 0),
	}
	for field, rawClass := range rawGroups {
		classification, ok := rawClass.(string)
		if !ok {
			return FieldGroups{}, nil, "", extractedDataPolicy{}, false,
				contractError(ErrInvalid, "field_group_invalid")
		}
		evidence := FieldEvidence{
			Field:          field,
			Classification: classification,
			EvidencePath:   "envelope." + field,
		}
		switch classification {
		case "direct":
			groups.Direct = append(groups.Direct, evidence)
		case "derived":
			groups.Derived = append(groups.Derived, evidence)
		case "declared":
			groups.Declared = append(groups.Declared, evidence)
		default:
			return FieldGroups{}, nil, "", extractedDataPolicy{}, false,
				contractError(ErrInvalid, "field_group_unsupported")
		}
	}
	sortFieldEvidence(groups.Direct)
	sortFieldEvidence(groups.Derived)
	sortFieldEvidence(groups.Declared)

	payload, ok := envelope["payload"].(map[string]any)
	if !ok {
		return FieldGroups{}, nil, "", extractedDataPolicy{}, false,
			contractError(ErrInvalid, "payload_missing")
	}
	rawGaps, ok := payload["capture_gaps"].([]any)
	if !ok || len(rawGaps) != 4 {
		return FieldGroups{}, nil, "", extractedDataPolicy{}, false,
			contractError(ErrInvalid, "capture_gap_set_invalid")
	}
	gaps := make([]CaptureGapEvidence, 0, len(rawGaps))
	seen := make(map[string]struct{}, len(rawGaps))
	for _, rawGap := range rawGaps {
		gapMap, ok := rawGap.(map[string]any)
		if !ok {
			return FieldGroups{}, nil, "", extractedDataPolicy{}, false,
				contractError(ErrInvalid, "capture_gap_invalid")
		}
		gap, err := parseGap(gapMap)
		if err != nil {
			return FieldGroups{}, nil, "", extractedDataPolicy{}, false, err
		}
		if _, duplicate := seen[gap.AffectedClaim]; duplicate {
			return FieldGroups{}, nil, "", extractedDataPolicy{}, false,
				contractError(ErrInvalid, "capture_gap_duplicate")
		}
		seen[gap.AffectedClaim] = struct{}{}
		gaps = append(gaps, gap)
		groups.Missing = append(groups.Missing, FieldEvidence{
			Field:          "capture_gap." + gap.AffectedClaim,
			Classification: "missing",
			EvidencePath:   "envelope.payload.capture_gaps." + gap.AffectedClaim,
			Reason:         gap.ReasonCode,
		})
	}
	sortFieldEvidence(groups.Missing)
	sort.Slice(gaps, func(left, right int) bool {
		return gaps[left].AffectedClaim < gaps[right].AffectedClaim
	})

	dataPolicyMap, ok := envelope["data_policy"].(map[string]any)
	if !ok {
		return FieldGroups{}, nil, "", extractedDataPolicy{}, false,
			contractError(ErrInvalid, "data_policy_missing")
	}
	payloadCaptureMode, captureOK := dataPolicyMap["payload_capture_mode"].(string)
	rawPayloadPresent, rawOK := dataPolicyMap["raw_payload_present"].(bool)
	timelineComplete, timelineOK := payload["timeline_complete"].(bool)
	if !captureOK || !rawOK || !timelineOK {
		return FieldGroups{}, nil, "", extractedDataPolicy{}, false,
			contractError(ErrInvalid, "passport_fact_missing")
	}
	return groups, gaps, fidelity, extractedDataPolicy{
		payloadCaptureMode: payloadCaptureMode,
		rawPayloadPresent:  rawPayloadPresent,
	}, timelineComplete, nil
}

func parseGap(gap map[string]any) (CaptureGapEvidence, error) {
	required := func(name string) (string, bool) {
		value, ok := gap[name].(string)
		return value, ok && value != ""
	}
	gapID, gapIDOK := required("gap_id")
	claim, claimOK := required("affected_claim")
	gapType, gapTypeOK := required("gap_type")
	reason, reasonOK := required("reason_code")
	severity, severityOK := required("severity")
	observedAt, observedOK := required("observed_at")
	captureMode, captureOK := required("payload_capture_mode")
	rawProvenance, provenanceOK := gap["field_provenance"].(map[string]any)
	if !gapIDOK || !claimOK || !gapTypeOK || !reasonOK || !severityOK ||
		!observedOK || !captureOK || !provenanceOK {
		return CaptureGapEvidence{}, contractError(ErrInvalid, "capture_gap_shape_invalid")
	}
	fieldProvenance := make(map[string]string, len(rawProvenance))
	for field, rawClass := range rawProvenance {
		classification, ok := rawClass.(string)
		if !ok {
			return CaptureGapEvidence{}, contractError(ErrInvalid, "capture_gap_provenance_invalid")
		}
		fieldProvenance[field] = classification
	}
	return CaptureGapEvidence{
		GapID:              gapID,
		AffectedClaim:      claim,
		GapType:            gapType,
		ReasonCode:         reason,
		Severity:           severity,
		ObservedAt:         observedAt,
		PayloadCaptureMode: captureMode,
		FieldProvenance:    fieldProvenance,
	}, nil
}

func sortFieldEvidence(values []FieldEvidence) {
	sort.Slice(values, func(left, right int) bool {
		return values[left].Field < values[right].Field
	})
}

func semanticHash(request Request) (string, error) {
	encoded, err := json.Marshal(request)
	if err != nil {
		return "", contractError(ErrInvalid, "semantic_encoding_failed")
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", contractError(ErrInvalid, "semantic_encoding_failed")
	}
	canonical, canonicalErr := eventconnector.CanonicalJSON(value)
	if canonicalErr != nil {
		return "", contractError(ErrInvalid, "semantic_canonicalization_failed")
	}
	return eventconnector.SHA256Reference(canonical), nil
}

func referenceConnectorAttestation(
	binding completedfeed.ReferenceRuntimeBinding,
) ConnectorAttestation {
	return ConnectorAttestation{
		ConnectorID:               binding.ConnectorID,
		ConnectorVersion:          binding.ConnectorVersion,
		SourceProduct:             binding.SourceProduct,
		SourceSchema:              binding.SourceSchemaID,
		EventSchema:               binding.EventSchemaRef,
		ManifestSHA256:            binding.ManifestDigest,
		SourceMappingSHA256:       ReferenceMappingDigest,
		CompatibilitySHA256:       ReferenceCompatDigest,
		ConformanceContractSHA256: ReferenceContractDigest,
		VectorBundleSHA256:        ReferenceVectorDigest,
		IndependentReports:        []string{ReferenceGoReportDigest, ReferenceTSReportDigest},
		ConformanceCases:          30,
		ConformanceClasses:        14,
		Status:                    "pass",
		RuntimeRegistered:         true,
		ProductRuntimeOwned:       true,
		CredentialOwned:           true,
		CheckpointOwned:           false,
		CoreVendorBranchRequired:  false,
	}
}

func secondConnectorAttestation() ConnectorAttestation {
	return ConnectorAttestation{
		ConnectorID:               LiteLLMConnectorID,
		ConnectorVersion:          LiteLLMConnectorVersion,
		SourceProduct:             "litellm",
		SourceSchema:              "foigent://sources/litellm/spend-log/v1",
		EventSchema:               "foigent://events/model/request-completed/v2",
		ManifestSHA256:            LiteLLMManifestDigest,
		SourceMappingSHA256:       LiteLLMMappingDigest,
		CompatibilitySHA256:       LiteLLMCompatDigest,
		ConformanceContractSHA256: LiteLLMContractDigest,
		VectorBundleSHA256:        LiteLLMVectorDigest,
		IndependentReports:        []string{LiteLLMGoReportDigest, LiteLLMTSReportDigest},
		ConformanceCases:          20,
		ConformanceClasses:        11,
		Status:                    "pass",
		RuntimeRegistered:         false,
		ProductRuntimeOwned:       false,
		CredentialOwned:           false,
		CheckpointOwned:           false,
		CoreVendorBranchRequired:  false,
	}
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
		"api_key",
		"authorization",
		"source_snapshot_base64",
	} {
		if strings.Contains(lower, restricted) {
			return true
		}
	}
	return false
}
