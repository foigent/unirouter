// Package executionevidence owns the bounded, secret-free route identity that
// is persisted before an upstream dispatch. It deliberately contains no
// database, gateway, billing, or provider implementation policy.
package executionevidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	ContractVersionV1 = "unirouter.execution-route.v1"
	ContractVersionV2 = "unirouter.execution-route.v2"
	// ContractVersion remains the historical V1 identity for existing callers.
	// Public execution must opt into EvidenceV2 explicitly.
	ContractVersion = ContractVersionV1
)

type AuthorityMode string

const (
	ModeLegacy    AuthorityMode = "legacy"
	ModeVerify    AuthorityMode = "verify"
	ModeCanonical AuthorityMode = "canonical"
)

type ExecutionContractSource string

const (
	ContractSourceCanonicalProfile ExecutionContractSource = "canonical_profile"
	ContractSourceLegacyAdapter    ExecutionContractSource = "legacy_adapter"
)

var (
	ErrInvalid = errors.New("executionevidence: invalid evidence")
	shaPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	mrsPattern = regexp.MustCompile(`^mrs1_[0-9a-f]{64}$`)
)

type CapabilityEntry struct {
	CapabilityID  string     `json:"capability_id"`
	SupportState  string     `json:"support_state"`
	EvidenceState string     `json:"evidence_state"`
	EvidenceRef   string     `json:"evidence_ref"`
	VerifiedAt    *time.Time `json:"verified_at,omitempty"`
	Revision      int64      `json:"revision"`
}

// EvidenceV1 is the immutable execution identity persisted before dispatch.
// Encrypted credentials, prompts, response bodies, and full provider URLs are
// intentionally absent. In legacy_adapter mode, ModelOperationID describes the
// requested wire operation; compatibility-only legacy.any/probe profiles must
// never be copied into the execution contract fields.
type EvidenceV1 struct {
	ContractVersion         string                  `json:"contract_version"`
	AuthorityMode           AuthorityMode           `json:"authority_mode"`
	ExecutionContractSource ExecutionContractSource `json:"execution_contract_source"`

	PublicModelID           int64     `json:"public_model_id,omitempty"`
	DeploymentID            int64     `json:"deployment_id,omitempty"`
	ConnectionID            int64     `json:"connection_id,omitempty"`
	CredentialBindingID     int64     `json:"credential_binding_id,omitempty"`
	ConnectionRevision      int64     `json:"connection_revision,omitempty"`
	DeploymentRevision      int64     `json:"deployment_revision,omitempty"`
	CircuitRevision         int64     `json:"circuit_revision,omitempty"`
	CredentialStateRevision int64     `json:"credential_state_revision,omitempty"`
	CredentialLinkedAt      time.Time `json:"credential_linked_at,omitempty"`

	ModelOperationID       string     `json:"model_operation_id"`
	OperationRevision      int64      `json:"operation_revision,omitempty"`
	ProtocolProfileID      string     `json:"protocol_profile_id,omitempty"`
	ProtocolProfileVersion string     `json:"protocol_profile_version,omitempty"`
	OperationEvidenceState string     `json:"operation_evidence_state,omitempty"`
	OperationEvidenceRef   string     `json:"operation_evidence_ref,omitempty"`
	OperationVerifiedAt    *time.Time `json:"operation_verified_at,omitempty"`

	CapabilityEvidenceHash  string            `json:"capability_evidence_hash,omitempty"`
	CapabilityEvidence      []CapabilityEntry `json:"capability_evidence,omitempty"`
	CredentialProbeID       int64             `json:"credential_probe_id,omitempty"`
	CredentialProbeTestedAt time.Time         `json:"credential_probe_tested_at,omitempty"`

	RuntimeReleaseID  int64  `json:"runtime_release_id,omitempty"`
	RuntimeReleaseRef string `json:"runtime_release_ref,omitempty"`
	RuntimeStage      string `json:"runtime_stage,omitempty"`
	SlotVersion       int64  `json:"slot_version,omitempty"`
	SnapshotHash      string `json:"snapshot_hash,omitempty"`

	LegacyChannelID               int64  `json:"legacy_channel_id,omitempty"`
	LegacyChannelKeyID            int64  `json:"legacy_channel_key_id,omitempty"`
	LegacyRoutingRevision         int64  `json:"legacy_routing_revision,omitempty"`
	LegacyCircuitRevision         int64  `json:"legacy_circuit_revision,omitempty"`
	LegacyCredentialStateRevision int64  `json:"legacy_credential_state_revision,omitempty"`
	RequiresProbe                 bool   `json:"requires_probe"`
	ProbeLeaseTokenHash           string `json:"probe_lease_token_hash,omitempty"`
}

// CompactProjection is the query-oriented subset copied into logs and traces.
// ProviderAttempt retains the complete evidence as the dispatch authority.
type CompactProjection struct {
	ContractVersion            string        `json:"contract_version"`
	AuthorityMode              AuthorityMode `json:"authority_mode"`
	EvidenceHash               string        `json:"evidence_hash"`
	PublicModelID              int64         `json:"public_model_id,omitempty"`
	DeploymentID               int64         `json:"deployment_id,omitempty"`
	ConnectionID               int64         `json:"connection_id,omitempty"`
	CredentialBindingID        int64         `json:"credential_binding_id,omitempty"`
	ModelOperationID           string        `json:"model_operation_id"`
	RuntimeReleaseID           int64         `json:"runtime_release_id,omitempty"`
	SnapshotHash               string        `json:"snapshot_hash,omitempty"`
	LegacyChannelID            int64         `json:"legacy_channel_id,omitempty"`
	PublicExecutionBindingID   int64         `json:"public_execution_binding_id,omitempty"`
	DeliveryContractVersion    string        `json:"delivery_contract_version,omitempty"`
	ChargeContractVersion      string        `json:"charge_contract_version,omitempty"`
	IdempotencyContractVersion string        `json:"idempotency_contract_version,omitempty"`
}

// NormalizeCompact validates the query projection without pretending that it
// can replace the complete immutable EvidenceV1 receipt. The evidence hash is
// still the digest of that complete receipt; this helper only protects
// consumers from accepting a partial or mode-inconsistent projection.
func NormalizeCompact(value CompactProjection) (CompactProjection, error) {
	value.ContractVersion = strings.TrimSpace(value.ContractVersion)
	value.EvidenceHash = strings.TrimSpace(value.EvidenceHash)
	value.ModelOperationID = strings.TrimSpace(value.ModelOperationID)
	value.SnapshotHash = strings.TrimSpace(value.SnapshotHash)
	value.DeliveryContractVersion = strings.TrimSpace(value.DeliveryContractVersion)
	value.ChargeContractVersion = strings.TrimSpace(value.ChargeContractVersion)
	value.IdempotencyContractVersion = strings.TrimSpace(value.IdempotencyContractVersion)
	if !isKnownContractVersion(value.ContractVersion) || !shaPattern.MatchString(value.EvidenceHash) ||
		!validBounded(value.ModelOperationID, 80) || value.ModelOperationID == "legacy.any.v1" {
		return CompactProjection{}, invalid("compact_identity")
	}
	canonicalPresent := value.PublicModelID > 0 && value.DeploymentID > 0 && value.ConnectionID > 0 &&
		value.CredentialBindingID > 0 && value.RuntimeReleaseID > 0 && mrsPattern.MatchString(value.SnapshotHash)
	canonicalEmpty := value.PublicModelID == 0 && value.DeploymentID == 0 && value.ConnectionID == 0 &&
		value.CredentialBindingID == 0 && value.RuntimeReleaseID == 0 && value.SnapshotHash == ""
	publicPresent := value.PublicExecutionBindingID > 0 && validPublicContractBundle(
		value.ModelOperationID,
		value.DeliveryContractVersion,
		value.ChargeContractVersion,
		value.IdempotencyContractVersion,
	)
	publicEmpty := value.PublicExecutionBindingID == 0 && value.DeliveryContractVersion == "" &&
		value.ChargeContractVersion == "" && value.IdempotencyContractVersion == ""
	if value.ContractVersion == ContractVersionV1 && (!publicEmpty || isPublicOperation(value.ModelOperationID)) {
		return CompactProjection{}, invalid("compact_v1_public_bundle")
	}
	if value.ContractVersion == ContractVersionV2 && (!publicPresent ||
		value.AuthorityMode != ModeCanonical ||
		(value.ModelOperationID != "openai.responses.v1" && value.ModelOperationID != "anthropic.messages.v1")) {
		return CompactProjection{}, invalid("compact_v2_public_bundle")
	}
	switch value.AuthorityMode {
	case ModeLegacy:
		if value.LegacyChannelID <= 0 || !canonicalEmpty {
			return CompactProjection{}, invalid("compact_legacy_bundle")
		}
	case ModeVerify:
		if value.LegacyChannelID <= 0 || !canonicalPresent {
			return CompactProjection{}, invalid("compact_verify_bundle")
		}
	case ModeCanonical:
		if value.LegacyChannelID != 0 || !canonicalPresent {
			return CompactProjection{}, invalid("compact_canonical_bundle")
		}
	default:
		return CompactProjection{}, invalid("compact_authority_mode")
	}
	return value, nil
}

func Normalize(value EvidenceV1) (EvidenceV1, error) {
	return normalizeEvidence(value, false)
}

func normalizeEvidence(value EvidenceV1, allowPublicOperations bool) (EvidenceV1, error) {
	value.ContractVersion = strings.TrimSpace(value.ContractVersion)
	value.ModelOperationID = strings.TrimSpace(value.ModelOperationID)
	value.ProtocolProfileID = strings.TrimSpace(value.ProtocolProfileID)
	value.ProtocolProfileVersion = strings.TrimSpace(value.ProtocolProfileVersion)
	value.OperationEvidenceState = strings.TrimSpace(value.OperationEvidenceState)
	value.OperationEvidenceRef = strings.TrimSpace(value.OperationEvidenceRef)
	value.CapabilityEvidenceHash = strings.TrimSpace(value.CapabilityEvidenceHash)
	value.RuntimeReleaseRef = strings.TrimSpace(value.RuntimeReleaseRef)
	value.RuntimeStage = strings.TrimSpace(value.RuntimeStage)
	value.SnapshotHash = strings.TrimSpace(value.SnapshotHash)
	value.ProbeLeaseTokenHash = strings.TrimSpace(value.ProbeLeaseTokenHash)
	value.CredentialLinkedAt = canonicalTime(value.CredentialLinkedAt)
	value.CredentialProbeTestedAt = canonicalTime(value.CredentialProbeTestedAt)
	value.OperationVerifiedAt = canonicalTimePointer(value.OperationVerifiedAt)

	if value.ContractVersion != ContractVersion {
		return EvidenceV1{}, invalid("contract_version")
	}
	if !allowPublicOperations && isPublicOperation(value.ModelOperationID) {
		return EvidenceV1{}, invalid("v1_public_operation")
	}
	if !validBounded(value.ModelOperationID, 80) || value.ModelOperationID == "legacy.any.v1" {
		return EvidenceV1{}, invalid("model_operation_id")
	}
	if value.AuthorityMode != ModeLegacy && value.AuthorityMode != ModeVerify && value.AuthorityMode != ModeCanonical {
		return EvidenceV1{}, invalid("authority_mode")
	}
	if value.ExecutionContractSource != ContractSourceCanonicalProfile && value.ExecutionContractSource != ContractSourceLegacyAdapter {
		return EvidenceV1{}, invalid("execution_contract_source")
	}
	if (!value.RequiresProbe && value.ProbeLeaseTokenHash != "") ||
		(value.ProbeLeaseTokenHash != "" && !shaPattern.MatchString(value.ProbeLeaseTokenHash)) {
		return EvidenceV1{}, invalid("probe_lease_token_hash")
	}

	legacyPresent := value.LegacyChannelID > 0 && value.LegacyChannelKeyID > 0 &&
		value.LegacyRoutingRevision > 0 && value.LegacyCircuitRevision > 0 &&
		value.LegacyCredentialStateRevision > 0
	legacyEmpty := value.LegacyChannelID == 0 && value.LegacyChannelKeyID == 0 &&
		value.LegacyRoutingRevision == 0 && value.LegacyCircuitRevision == 0 &&
		value.LegacyCredentialStateRevision == 0
	canonicalPresent := value.PublicModelID > 0 && value.DeploymentID > 0 && value.ConnectionID > 0 &&
		value.CredentialBindingID > 0 && value.ConnectionRevision > 0 && value.DeploymentRevision > 0 &&
		value.CircuitRevision > 0 && value.CredentialStateRevision > 0 && !value.CredentialLinkedAt.IsZero() &&
		value.CredentialProbeID > 0 && !value.CredentialProbeTestedAt.IsZero() &&
		value.RuntimeReleaseID > 0 && validBounded(value.RuntimeReleaseRef, 80) &&
		validRuntimeStage(value.RuntimeStage) && value.SlotVersion > 0 && mrsPattern.MatchString(value.SnapshotHash)
	canonicalEmpty := value.PublicModelID == 0 && value.DeploymentID == 0 && value.ConnectionID == 0 &&
		value.CredentialBindingID == 0 && value.ConnectionRevision == 0 && value.DeploymentRevision == 0 &&
		value.CircuitRevision == 0 && value.CredentialStateRevision == 0 && value.CredentialLinkedAt.IsZero() &&
		value.CredentialProbeID == 0 && value.CredentialProbeTestedAt.IsZero() &&
		value.RuntimeReleaseID == 0 && value.RuntimeReleaseRef == "" && value.RuntimeStage == "" &&
		value.SlotVersion == 0 && value.SnapshotHash == ""

	switch value.AuthorityMode {
	case ModeLegacy:
		if !legacyPresent || !canonicalEmpty || value.ExecutionContractSource != ContractSourceLegacyAdapter {
			return EvidenceV1{}, invalid("legacy_bundle")
		}
	case ModeVerify:
		if !legacyPresent || !canonicalPresent {
			return EvidenceV1{}, invalid("verify_bundle")
		}
	case ModeCanonical:
		if !legacyEmpty || !canonicalPresent || value.ExecutionContractSource != ContractSourceCanonicalProfile {
			return EvidenceV1{}, invalid("canonical_bundle")
		}
	}

	switch value.ExecutionContractSource {
	case ContractSourceLegacyAdapter:
		if value.AuthorityMode == ModeCanonical || value.OperationRevision != 0 || value.ProtocolProfileID != "" ||
			value.ProtocolProfileVersion != "" || value.OperationEvidenceState != "" ||
			value.OperationEvidenceRef != "" || value.OperationVerifiedAt != nil ||
			value.CapabilityEvidenceHash != "" || len(value.CapabilityEvidence) != 0 {
			return EvidenceV1{}, invalid("legacy_adapter_contract")
		}
	case ContractSourceCanonicalProfile:
		if value.AuthorityMode == ModeLegacy || value.OperationRevision <= 0 ||
			!validBounded(value.ProtocolProfileID, 100) || !validBounded(value.ProtocolProfileVersion, 40) ||
			value.ProtocolProfileID == "legacy.any.v1" || value.ProtocolProfileID == "legacy.channel_model_probe" ||
			!validBounded(value.OperationEvidenceState, 32) || !validBounded(value.OperationEvidenceRef, 512) ||
			value.OperationVerifiedAt == nil {
			return EvidenceV1{}, invalid("canonical_profile_contract")
		}
		entries, digest, err := normalizeCapabilities(value.CapabilityEvidence)
		if err != nil || value.CapabilityEvidenceHash != digest {
			return EvidenceV1{}, invalid("capability_evidence")
		}
		value.CapabilityEvidence = entries
	}

	return value, nil
}

// NormalizeForDispatch applies the additional invariant that a half-open
// attempt already owns a hashed lease token. Planner evidence may be normalized
// before the coordinator has claimed that token; persisted attempt evidence may
// not.
func NormalizeForDispatch(value EvidenceV1) (EvidenceV1, error) {
	normalized, err := Normalize(value)
	if err != nil {
		return EvidenceV1{}, err
	}
	if normalized.RequiresProbe && normalized.ProbeLeaseTokenHash == "" {
		return EvidenceV1{}, invalid("probe_lease_token_hash")
	}
	return normalized, nil
}

func Digest(value EvidenceV1) (string, error) {
	normalized, err := Normalize(value)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("%w: marshal: %v", ErrInvalid, err)
	}
	return digestBytes(body), nil
}

func CapabilityDigest(entries []CapabilityEntry) (string, error) {
	normalized, digest, err := normalizeCapabilities(entries)
	if err != nil {
		return "", err
	}
	_ = normalized
	return digest, nil
}

func Compact(value EvidenceV1) (CompactProjection, error) {
	normalized, err := Normalize(value)
	if err != nil {
		return CompactProjection{}, err
	}
	digest, err := Digest(normalized)
	if err != nil {
		return CompactProjection{}, err
	}
	return NormalizeCompact(CompactProjection{
		ContractVersion: normalized.ContractVersion, AuthorityMode: normalized.AuthorityMode,
		EvidenceHash: digest, PublicModelID: normalized.PublicModelID,
		DeploymentID: normalized.DeploymentID, ConnectionID: normalized.ConnectionID,
		CredentialBindingID: normalized.CredentialBindingID, ModelOperationID: normalized.ModelOperationID,
		RuntimeReleaseID: normalized.RuntimeReleaseID, SnapshotHash: normalized.SnapshotHash,
		LegacyChannelID: normalized.LegacyChannelID,
	})
}

func normalizeCapabilities(input []CapabilityEntry) ([]CapabilityEntry, string, error) {
	if len(input) > 64 {
		return nil, "", invalid("capability_count")
	}
	entries := slices.Clone(input)
	if entries == nil {
		entries = []CapabilityEntry{}
	}
	for index := range entries {
		entry := &entries[index]
		entry.CapabilityID = strings.TrimSpace(entry.CapabilityID)
		entry.SupportState = strings.TrimSpace(entry.SupportState)
		entry.EvidenceState = strings.TrimSpace(entry.EvidenceState)
		entry.EvidenceRef = strings.TrimSpace(entry.EvidenceRef)
		entry.VerifiedAt = canonicalTimePointer(entry.VerifiedAt)
		if !validBounded(entry.CapabilityID, 80) || entry.SupportState != "enabled" ||
			!validBounded(entry.EvidenceState, 32) || !validBounded(entry.EvidenceRef, 512) ||
			entry.VerifiedAt == nil || entry.Revision <= 0 {
			return nil, "", invalid("capability_entry")
		}
	}
	slices.SortFunc(entries, func(left, right CapabilityEntry) int {
		return strings.Compare(left.CapabilityID, right.CapabilityID)
	})
	for index := 1; index < len(entries); index++ {
		if entries[index-1].CapabilityID == entries[index].CapabilityID {
			return nil, "", invalid("duplicate_capability")
		}
	}
	body, err := json.Marshal(entries)
	if err != nil {
		return nil, "", fmt.Errorf("%w: capability marshal: %v", ErrInvalid, err)
	}
	return entries, digestBytes(body), nil
}

func validRuntimeStage(value string) bool {
	return value == "active" || value == "canary"
}

func isKnownContractVersion(value string) bool {
	return value == ContractVersionV1 || value == ContractVersionV2
}

func isPublicOperation(value string) bool {
	return value == "openai.responses.v1" || value == "anthropic.messages.v1"
}

func validBounded(value string, limit int) bool {
	return value != "" && len(value) <= limit && value == strings.TrimSpace(value) && !strings.ContainsAny(value, "\x00\r\n")
}

func canonicalTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Time{}
	}
	return value.UTC().Truncate(time.Microsecond)
}

func canonicalTimePointer(value *time.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}
	normalized := canonicalTime(*value)
	return &normalized
}

func digestBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func invalid(field string) error {
	return fmt.Errorf("%w: %s", ErrInvalid, field)
}

func IsDigest(value string) bool {
	return shaPattern.MatchString(value)
}
