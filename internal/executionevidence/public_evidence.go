package executionevidence

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	PublicDeliveryContractV1          = "unirouter.public-nonstream-delivery.v1"
	PublicResponsesDeliveryContractV2 = "unirouter.public-responses-delivery.v2"
	PublicMessagesDeliveryContractV2  = "unirouter.public-anthropic-messages-delivery.v2"
	PublicMessagesToolsDeliveryV1     = "unirouter.public-anthropic-messages-tools-delivery.v1"
	PublicChargeContractV1            = "unirouter.provider-final-token-charge.v1"
	PublicIdempotencyContractV1       = "unirouter.public-at-most-once.v1"
)

// EvidenceV2 is the explicit public-execution extension. EvidenceV1 is
// embedded so the selected credential, its own probe, and canonical release
// identity retain the exact V1 semantics; only V2 may carry the immutable
// per-release-item public contract authority.
type EvidenceV2 struct {
	EvidenceV1
	PublicExecutionBindingID   int64  `json:"public_execution_binding_id"`
	DeliveryContractVersion    string `json:"delivery_contract_version"`
	ChargeContractVersion      string `json:"charge_contract_version"`
	IdempotencyContractVersion string `json:"idempotency_contract_version"`
}

func NormalizeV2(value EvidenceV2) (EvidenceV2, error) {
	if value.ContractVersion != ContractVersionV2 {
		return EvidenceV2{}, invalid("contract_version")
	}
	base := value.EvidenceV1
	base.ContractVersion = ContractVersionV1
	normalizedBase, err := normalizeEvidence(base, true)
	if err != nil {
		return EvidenceV2{}, err
	}
	value.DeliveryContractVersion = strings.TrimSpace(value.DeliveryContractVersion)
	value.ChargeContractVersion = strings.TrimSpace(value.ChargeContractVersion)
	value.IdempotencyContractVersion = strings.TrimSpace(value.IdempotencyContractVersion)
	if normalizedBase.AuthorityMode != ModeCanonical ||
		normalizedBase.ExecutionContractSource != ContractSourceCanonicalProfile ||
		(normalizedBase.ModelOperationID != "openai.responses.v1" &&
			normalizedBase.ModelOperationID != "anthropic.messages.v1") ||
		value.PublicExecutionBindingID <= 0 ||
		!validPublicContractBundle(
			normalizedBase.ModelOperationID,
			value.DeliveryContractVersion,
			value.ChargeContractVersion,
			value.IdempotencyContractVersion,
		) {
		return EvidenceV2{}, invalid("public_execution_bundle")
	}
	normalizedBase.ContractVersion = ContractVersionV2
	value.EvidenceV1 = normalizedBase
	return value, nil
}

func validPublicContractBundle(operationID, delivery, charge, idempotency string) bool {
	if charge != PublicChargeContractV1 || idempotency != PublicIdempotencyContractV1 {
		return false
	}
	switch delivery {
	case PublicDeliveryContractV1:
		return operationID == "openai.responses.v1" || operationID == "anthropic.messages.v1"
	case PublicResponsesDeliveryContractV2:
		return operationID == "openai.responses.v1"
	case PublicMessagesDeliveryContractV2:
		return operationID == "anthropic.messages.v1"
	case PublicMessagesToolsDeliveryV1:
		return operationID == "anthropic.messages.v1"
	default:
		return false
	}
}

func NormalizeV2ForDispatch(value EvidenceV2) (EvidenceV2, error) {
	normalized, err := NormalizeV2(value)
	if err != nil {
		return EvidenceV2{}, err
	}
	if normalized.RequiresProbe && normalized.ProbeLeaseTokenHash == "" {
		return EvidenceV2{}, invalid("probe_lease_token_hash")
	}
	return normalized, nil
}

func DigestV2(value EvidenceV2) (string, error) {
	normalized, err := NormalizeV2(value)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("%w: marshal v2: %v", ErrInvalid, err)
	}
	return digestBytes(body), nil
}

// RouteEvidenceV1 returns the normalized underlying route identity using the
// historical V1 contract marker. It is only for shared revalidation logic;
// public dispatch receipts and idempotency comparisons must use DigestV2.
func (value EvidenceV2) RouteEvidenceV1() (EvidenceV1, error) {
	normalized, err := NormalizeV2(value)
	if err != nil {
		return EvidenceV1{}, err
	}
	base := normalized.EvidenceV1
	base.ContractVersion = ContractVersionV1
	return base, nil
}

func CompactV2(value EvidenceV2) (CompactProjection, error) {
	normalized, err := NormalizeV2(value)
	if err != nil {
		return CompactProjection{}, err
	}
	digest, err := DigestV2(normalized)
	if err != nil {
		return CompactProjection{}, err
	}
	return NormalizeCompact(CompactProjection{
		ContractVersion: ContractVersionV2, AuthorityMode: normalized.AuthorityMode,
		EvidenceHash: digest, PublicModelID: normalized.PublicModelID,
		DeploymentID: normalized.DeploymentID, ConnectionID: normalized.ConnectionID,
		CredentialBindingID: normalized.CredentialBindingID,
		ModelOperationID:    normalized.ModelOperationID,
		RuntimeReleaseID:    normalized.RuntimeReleaseID, SnapshotHash: normalized.SnapshotHash,
		PublicExecutionBindingID:   normalized.PublicExecutionBindingID,
		DeliveryContractVersion:    normalized.DeliveryContractVersion,
		ChargeContractVersion:      normalized.ChargeContractVersion,
		IdempotencyContractVersion: normalized.IdempotencyContractVersion,
	})
}
