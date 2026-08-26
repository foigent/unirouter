package modelrouting

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"slices"
	"time"

	"github.com/uniroute/unirouter/internal/executionevidence"
)

func adapterDigest(raw json.RawMessage) string {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if len(raw) == 0 || decoder.Decode(&value) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return digestBytes(raw)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return digestBytes(raw)
	}
	return digestBytes(canonical)
}

func legacyCandidateDigest(version, model string, overrides []legacyOverride) string {
	value := struct {
		ContractVersion string           `json:"contract_version"`
		ModelName       string           `json:"model_name"`
		Overrides       []legacyOverride `json:"overrides"`
	}{ContractVersion: version, ModelName: model, Overrides: overrides}
	body, _ := json.Marshal(value)
	return digestBytes(body)
}

func manifestDigest(items []manifestItem) string {
	type route struct {
		DeploymentID               int64  `json:"deployment_id"`
		ModelOperationID           string `json:"model_operation_id"`
		ExpectedConnectionRevision int64  `json:"expected_connection_revision"`
		ExpectedDeploymentRevision int64  `json:"expected_deployment_revision"`
		ExpectedOperationRevision  int64  `json:"expected_operation_revision"`
		ProtocolProfileID          string `json:"protocol_profile_id"`
		ProtocolProfileVersion     string `json:"protocol_profile_version"`
		PublicPath                 string `json:"public_path,omitempty"`
		ExecutorContractVersion    string `json:"executor_contract_version,omitempty"`
		PublicExecutionBindingID   int64  `json:"public_execution_binding_id,omitempty"`
		DeliveryContractVersion    string `json:"delivery_contract_version,omitempty"`
		ChargeContractVersion      string `json:"charge_contract_version,omitempty"`
		IdempotencyContractVersion string `json:"idempotency_contract_version,omitempty"`
		Admission                  string `json:"admission"`
		Priority                   *int   `json:"priority,omitempty"`
		Weight                     *int   `json:"weight,omitempty"`
	}
	normalized := make([]route, 0, len(items))
	for _, item := range items {
		normalized = append(normalized, route{
			DeploymentID: item.DeploymentID, ModelOperationID: item.ModelOperationID,
			ExpectedConnectionRevision: item.ExpectedConnectionRevision,
			ExpectedDeploymentRevision: item.ExpectedDeploymentRevision,
			ExpectedOperationRevision:  item.ExpectedOperationRevision,
			ProtocolProfileID:          item.ProtocolProfileID, ProtocolProfileVersion: item.ProtocolProfileVersion,
			PublicPath: item.PublicPath, ExecutorContractVersion: item.ExecutorContractVersion,
			PublicExecutionBindingID:   item.PublicExecutionBindingID,
			DeliveryContractVersion:    item.DeliveryContractVersion,
			ChargeContractVersion:      item.ChargeContractVersion,
			IdempotencyContractVersion: item.IdempotencyContractVersion,
			Admission:                  item.Admission, Priority: item.Priority, Weight: item.Weight,
		})
	}
	body, _ := json.Marshal(normalized)
	return digestBytes(body)
}

func snapshotDigest(value snapshot) (string, error) {
	type safeBinding struct {
		ID, LegacyID, Revision int64
		Status                 string
		Cooldown               *time.Time
		AllowedModels          string
		LinkedAt               time.Time
		Probe                  probeSnapshot
	}
	type safeLegacy struct {
		ID, RoutingRevision, CircuitRevision   int64
		Name, Provider, BaseURL, AdapterType   string
		AdapterDigest, Upstream, Status        string
		CircuitState, ProbeToken               string
		Priority, Weight, FailCount, Threshold int
		MaxContext, DefaultMaxOutput           int
		HalfOpen, ProbeUntil                   *time.Time
		Bindings                               []safeBinding
	}
	type safeCanonical struct {
		DeploymentID, PublicModelID, ConnectionID, LegacyChannelID int64
		ConnectionRevision, DeploymentRevision, CircuitRevision    int64
		OperationRevision                                          int64
		Name, Provider, BaseURL, AdapterType, AdapterDigest        string
		ConnectionState, DeploymentState, Upstream, CircuitState   string
		LegacyPublicModel                                          string
		Priority, Weight, FailCount, Threshold                     int
		MaxContext, DefaultMaxOutput                               int
		HalfOpen, ProbeUntil                                       *time.Time
		ProbeToken                                                 string
		OperationID, OperationSupport, OperationEvidence           string
		OperationEvidenceRef                                       string
		ProtocolProfileID, ProtocolProfileVersion                  string
		PublicPath, ExecutorContractVersion                        string `json:",omitempty"`
		QualificationState                                         string `json:",omitempty"`
		ProductionApprovalID                                       int64  `json:",omitempty"`
		ProductionQualified                                        bool   `json:",omitempty"`
		PublicExecutionBindingID                                   int64  `json:",omitempty"`
		DeliveryContractVersion                                    string `json:",omitempty"`
		ChargeContractVersion                                      string `json:",omitempty"`
		IdempotencyContractVersion                                 string `json:",omitempty"`
		PublicQualificationState                                   string `json:",omitempty"`
		PublicExecutionQualified                                   bool   `json:",omitempty"`
		OperationVerified                                          *time.Time
		Bindings                                                   []safeBinding
		Capabilities                                               []capabilitySnapshot
		Manifest                                                   *manifestItem
	}
	type safeSnapshot struct {
		Mode                    Mode
		DBNow                   time.Time
		CustomerOrgID           int64
		PublicModelID           int64
		PublicModel             string
		Lifecycle               string
		Operation               string
		CapabilityRequirements  []CapabilityRequirement
		PublicContextLimit      int
		PublicOutputLimit       int
		ExecutionProfileID      string
		ExecutionProfileVersion string
		Slot                    slotSnapshot
		Manifest                []manifestItem
		LegacyRelease           releaseSnapshot
		LegacyManifest          []manifestItem
		LegacyOverrides         []legacyOverride
		Legacy                  []safeLegacy
		Canonical               []safeCanonical
	}

	safe := safeSnapshot{
		Mode: value.Mode, DBNow: value.DBNow.UTC(), CustomerOrgID: value.CustomerOrgID,
		PublicModelID: value.PublicModelID, PublicModel: value.PublicModel,
		Lifecycle: value.PublicModelLifecycle, Operation: value.RequestedOperationID,
		CapabilityRequirements: cloneCapabilityRequirements(value.CapabilityRequirements), Slot: value.Slot,
		PublicContextLimit: value.PublicContextLimit, PublicOutputLimit: value.PublicOutputLimit,
		ExecutionProfileID: value.ExecutionProfileID, ExecutionProfileVersion: value.ExecutionProfileVersion,
		Manifest: slices.Clone(value.Manifest), LegacyOverrides: slices.Clone(value.LegacyOverrides),
		LegacyRelease: value.LegacyRelease, LegacyManifest: slices.Clone(value.LegacyManifest),
	}
	for _, item := range value.Legacy {
		candidate := safeLegacy{
			ID: item.ID, RoutingRevision: item.RoutingRevision, CircuitRevision: item.CircuitRevision,
			Name: item.Name, Provider: item.Provider, BaseURL: item.BaseURL, AdapterType: item.AdapterType,
			AdapterDigest: item.AdapterConfigDigest, Upstream: item.UpstreamModel, Status: item.Status,
			CircuitState: item.CircuitState, ProbeToken: item.HalfOpenProbeToken,
			Priority: item.Priority, Weight: item.Weight, FailCount: item.FailCount,
			MaxContext: item.MaxContext, DefaultMaxOutput: item.DefaultMaxOutput,
			Threshold: item.DegradationThreshold, HalfOpen: item.HalfOpenUntil, ProbeUntil: item.HalfOpenProbeUntil,
		}
		for _, binding := range item.Bindings {
			candidate.Bindings = append(candidate.Bindings, safeBinding{
				ID: binding.ID, LegacyID: binding.ID, Revision: binding.StateRevision,
				Status: binding.Status, Cooldown: binding.CooldownUntil, AllowedModels: binding.AllowedModels,
			})
		}
		safe.Legacy = append(safe.Legacy, candidate)
	}
	for _, item := range value.Canonical {
		candidate := safeCanonical{
			DeploymentID: item.DeploymentID, PublicModelID: item.PublicModelID, ConnectionID: item.ConnectionID,
			LegacyChannelID: item.LegacyChannelID, ConnectionRevision: item.ConnectionRevision,
			DeploymentRevision: item.DeploymentRevision, CircuitRevision: item.CircuitRevision,
			OperationRevision: item.OperationRevision, Name: item.Name, Provider: item.Provider,
			BaseURL: item.BaseURL, AdapterType: item.AdapterType, AdapterDigest: item.AdapterConfigDigest,
			ConnectionState: item.ConnectionState, DeploymentState: item.DeploymentState,
			Upstream: item.UpstreamModel, CircuitState: item.CircuitState,
			LegacyPublicModel: item.LegacyPublicModel, Priority: item.Priority, Weight: item.Weight,
			FailCount: item.FailCount, Threshold: item.DegradationThreshold, HalfOpen: item.HalfOpenUntil,
			MaxContext: item.MaxContext, DefaultMaxOutput: item.DefaultMaxOutput,
			ProbeUntil: item.HalfOpenProbeUntil, ProbeToken: item.HalfOpenProbeToken,
			OperationID: item.OperationID, OperationSupport: item.OperationSupportState,
			OperationEvidence: item.OperationEvidenceState, OperationEvidenceRef: item.OperationEvidenceRef,
			OperationVerified: item.OperationVerifiedAt,
			ProtocolProfileID: item.ProtocolProfileID, ProtocolProfileVersion: item.ProtocolProfileVersion,
			PublicPath: item.PublicPath, ExecutorContractVersion: item.ExecutorContractVersion,
			QualificationState:         item.QualificationState,
			ProductionApprovalID:       item.ProductionApprovalID,
			ProductionQualified:        item.ProductionQualified,
			PublicExecutionBindingID:   item.PublicExecutionBindingID,
			DeliveryContractVersion:    item.DeliveryContractVersion,
			ChargeContractVersion:      item.ChargeContractVersion,
			IdempotencyContractVersion: item.IdempotencyContractVersion,
			PublicQualificationState:   item.PublicQualificationState,
			PublicExecutionQualified:   item.PublicExecutionQualified,
			Manifest:                   item.Manifest,
		}
		for _, binding := range item.Bindings {
			candidate.Bindings = append(candidate.Bindings, safeBinding{
				ID: binding.ID, LegacyID: binding.LegacyKeyID, Revision: binding.StateRevision,
				Status: binding.Status, Cooldown: binding.CooldownUntil,
				LinkedAt: binding.LinkedAt, Probe: binding.Probe,
			})
		}
		capabilityIDs := make([]string, 0, len(item.Capabilities))
		for id := range item.Capabilities {
			capabilityIDs = append(capabilityIDs, id)
		}
		slices.Sort(capabilityIDs)
		for _, id := range capabilityIDs {
			candidate.Capabilities = append(candidate.Capabilities, item.Capabilities[id])
		}
		safe.Canonical = append(safe.Canonical, candidate)
	}
	body, err := json.Marshal(safe)
	if err != nil {
		return "", err
	}
	return "mrs1_" + hex.EncodeToString(sha256Sum(body)), nil
}

func capabilityEvidenceDigest(required []string, available map[string]capabilitySnapshot) string {
	items := capabilityEvidenceEntries(required, available)
	// Reuse the evidence authority's UTC/microsecond normalization, including
	// for PostgreSQL timestamps scanned in a non-UTC process timezone.
	digest, err := executionevidence.CapabilityDigest(items)
	if err != nil {
		// An empty digest cannot pass Normalize/NormalizeV2 at dispatch. Never
		// turn invalid capability evidence into the valid empty-list digest.
		return ""
	}
	return digest
}

func capabilityEvidenceEntries(required []string, available map[string]capabilitySnapshot) []CapabilityEvidenceEntry {
	ids := slices.Clone(required)
	slices.Sort(ids)
	items := make([]CapabilityEvidenceEntry, 0, len(ids))
	for _, id := range ids {
		item := available[id]
		items = append(items, CapabilityEvidenceEntry{
			CapabilityID: id, SupportState: item.SupportState,
			EvidenceState: item.EvidenceState, EvidenceRef: item.EvidenceRef,
			VerifiedAt: item.VerifiedAt, Revision: item.Revision,
		})
	}
	return items
}

func digestBytes(value []byte) string {
	return "sha256:" + hex.EncodeToString(sha256Sum(value))
}

func sha256Sum(value []byte) []byte {
	sum := sha256.Sum256(value)
	return sum[:]
}
