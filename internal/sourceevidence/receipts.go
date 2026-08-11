package sourceevidence

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	IntegrationControlN1PinSchemaVersion = "unirouter.integration-control.n1-pin.v1"
	InstalledReleaseSchemaVersion        = "unirouter.release-state-identity.v1"
	SourceScopeReceiptSchemaVersion      = "unirouter.source-evidence-scope-receipt.v1"
	VerifierReleaseSchemaVersion         = "unirouter.source-evidence.component-verifier-release.v1"
	VerificationReceiptSchemaVersion     = "unirouter.source-evidence.component-verification-receipt.v1"
	ComponentVerifierID                  = "unirouter.source-evidence.component-verifier.v1"
	ComponentAcknowledgementSchema       = "foigent.integration-campaign-component-ack.v1"
	NeutralManifestSchemaVersion         = "foigent.integration-campaign-manifest.v1"
	NeutralAuthorizationSchemaVersion    = "foigent.integration-campaign-authorization.v1"
	NeutralCapabilityBoundary            = "offline_files_only"
	ComponentStatusAccepted              = "accepted"
)

var requiredContractArtifactSchemas = []string{
	CompatibilityBundleSchema,
	IntegrationControlN1PinSchemaVersion,
	InstalledReleaseSchemaVersion,
	PackageManifestSchemaVersion,
	PackageSignatureSchemaVersion,
	ProducerContractID,
	RequestSetSchemaVersion,
	SourceFactsSchemaVersion,
	SourceScopeReceiptSchemaVersion,
	SourceScopeSchemaVersion,
	TrustSetSchemaVersion,
	VerificationReceiptSchemaVersion,
	VerifierReleaseSchemaVersion,
}

// ContractArtifact binds one immutable product-owned schema or compatibility
// contract into the producer contract index. The index body itself is the
// artifact referenced by neutral M as ProducerContractID.
type ContractArtifact struct {
	SchemaVersion string `json:"schema_version"`
	Path          string `json:"path"`
	SHA256        string `json:"sha256"`
}

type IntegrationControlReference struct {
	PinSchemaVersion string `json:"pin_schema_version"`
	PinSHA256        string `json:"pin_sha256"`
}

type ComponentContractIndex struct {
	SchemaVersion      string                      `json:"schema_version"`
	Product            string                      `json:"product"`
	Profile            string                      `json:"profile"`
	SignaturePurpose   string                      `json:"signature_purpose"`
	SignatureDomain    string                      `json:"signature_domain"`
	ClaimScope         string                      `json:"claim_scope"`
	RuntimeObservation string                      `json:"runtime_observation"`
	IntegrationControl IntegrationControlReference `json:"integration_control"`
	Artifacts          []ContractArtifact          `json:"artifacts"`
}

type InstalledReleaseIdentity struct {
	SchemaVersion   string `json:"schema_version"`
	ReleaseSequence int64  `json:"release_sequence"`
	Version         string `json:"version"`
	CanonicalCommit string `json:"canonical_commit"`
	PackageSHA256   string `json:"package_sha256"`
	TrustedKeyID    string `json:"trusted_key_id"`
	DatabaseSchema  string `json:"database_schema"`
	RecordedAt      string `json:"recorded_at"`
	ObservedAt      string `json:"observed_at"`
}

type ReceiptEffects struct {
	DatabaseMutation       string `json:"database_mutation"`
	AdditionalGatewayCalls int    `json:"additional_gateway_calls"`
	ProviderCalls          int    `json:"provider_calls"`
	FeedCalls              int    `json:"feed_calls"`
	UpstreamCalls          int    `json:"upstream_calls"`
}

type SourceScopeReceipt struct {
	SchemaVersion         string         `json:"schema_version"`
	Product               string         `json:"product"`
	CampaignID            string         `json:"campaign_id"`
	Status                string         `json:"status"`
	SourceBindingSHA256   string         `json:"source_binding_sha256"`
	RequestSetSHA256      string         `json:"request_set_sha256"`
	ReleaseIdentitySHA256 string         `json:"release_identity_sha256"`
	ProtocolPinSHA256     string         `json:"protocol_pin_sha256"`
	SourceInstance        string         `json:"source_instance"`
	IdentityAuthority     string         `json:"identity_authority"`
	RuntimeObservation    string         `json:"runtime_observation"`
	RequestCount          int            `json:"request_count"`
	MaxRequests           int            `json:"max_requests"`
	MaxFacts              int            `json:"max_facts"`
	ObservedAt            string         `json:"observed_at"`
	DatabaseIsolation     string         `json:"database_isolation"`
	DatabaseReadOnly      bool           `json:"database_read_only"`
	TransactionIDAssigned bool           `json:"transaction_id_assigned"`
	Effects               ReceiptEffects `json:"effects"`
}

// VerifierRelease is a build-time product receipt. It is generated only for an
// exact reviewed UniRouter release; synthetic tests may use synthetic facts.
type VerifierRelease struct {
	SchemaVersion               string `json:"schema_version"`
	VerifierID                  string `json:"verifier_id"`
	ProducerContractID          string `json:"producer_contract_id"`
	ProducerContractSHA         string `json:"producer_contract_sha256"`
	SourceCommit                string `json:"source_commit"`
	SourceTree                  string `json:"source_tree"`
	BinarySHA256                string `json:"binary_sha256"`
	IntegrationControlPinSHA256 string `json:"integration_control_pin_sha256"`
	BuiltAt                     string `json:"built_at"`
}

type ArtifactReference struct {
	SchemaVersion string `json:"schema_version"`
	SHA256        string `json:"sha256"`
}

type ContractReference struct {
	ID     string `json:"id"`
	SHA256 string `json:"sha256"`
}

type ProductSigningIdentity struct {
	Purpose           string `json:"purpose"`
	Domain            string `json:"domain"`
	SigningKeyRef     string `json:"signing_key_ref"`
	SignerPublicKeyID string `json:"signer_public_key_id"`
	TrustSetSHA256    string `json:"trust_set_sha256"`
}

type ComponentVerificationReceipt struct {
	SchemaVersion                   string                 `json:"schema_version"`
	Product                         string                 `json:"product"`
	CampaignID                      string                 `json:"campaign_id"`
	Status                          string                 `json:"status"`
	CampaignManifestSHA256          string                 `json:"campaign_manifest_sha256"`
	ComponentContract               ContractReference      `json:"component_contract"`
	InstalledRelease                ArtifactReference      `json:"installed_release"`
	ComponentScopeReceipt           ArtifactReference      `json:"component_scope_receipt"`
	VerifierRelease                 ContractReference      `json:"verifier_release"`
	RequestSetSHA256                string                 `json:"request_set_sha256"`
	SourceBindingSHA256             string                 `json:"source_binding_sha256"`
	SourceEvidenceSigning           ProductSigningIdentity `json:"source_evidence_signing"`
	IntegrationControlPinSHA256     string                 `json:"integration_control_pin_sha256"`
	IntegrationControlReleaseSHA256 string                 `json:"integration_control_release_sha256"`
	IntegrationControlBinarySHA256  string                 `json:"integration_control_binary_sha256"`
	Checks                          []string               `json:"checks"`
	VerifiedAt                      string                 `json:"verified_at"`
	Effects                         ReceiptEffects         `json:"effects"`
}

var requiredComponentVerificationChecks = []string{
	"campaign_manifest_n1_verified",
	"component_contract_exact",
	"installed_release_exact",
	"request_set_exact",
	"source_binding_exact",
	"source_evidence_signer_exact",
	"source_scope_receipt_exact",
	"verifier_release_exact",
}

func DecodeComponentContractIndex(body []byte) (ComponentContractIndex, error) {
	var value ComponentContractIndex
	if err := decodeStrict(body, &value); err != nil {
		return ComponentContractIndex{}, fmt.Errorf("decode source evidence component contract index: %w", err)
	}
	canonical, err := CanonicalJSON(value)
	if err != nil {
		return ComponentContractIndex{}, err
	}
	if string(canonical) != string(body) {
		return ComponentContractIndex{}, errors.New("source evidence component contract index is not canonical JSON")
	}
	if err := ValidateComponentContractIndex(value); err != nil {
		return ComponentContractIndex{}, err
	}
	return value, nil
}

func ValidateComponentContractIndex(value ComponentContractIndex) error {
	if value.SchemaVersion != ProducerContractID || value.Product != "unirouter" || value.Profile != "poc_source_evidence" || value.SignaturePurpose != SignaturePurpose || value.SignatureDomain != SignatureDomain || value.ClaimScope != SourceFactsSchemaVersion || value.RuntimeObservation != SourceRuntimeObservation {
		return errors.New("source evidence component contract index identity is invalid")
	}
	if value.IntegrationControl.PinSchemaVersion != IntegrationControlN1PinSchemaVersion || !hashRefPattern.MatchString(value.IntegrationControl.PinSHA256) {
		return errors.New("source evidence component contract index N1 pin is invalid")
	}
	if len(value.Artifacts) != len(requiredContractArtifactSchemas) {
		return errors.New("source evidence component contract index artifact set is incomplete")
	}
	seen := make([]string, 0, len(value.Artifacts))
	lastPath := ""
	for index, artifact := range value.Artifacts {
		if !portableWithin(artifact.SchemaVersion, 200) || !portableWithin(artifact.Path, 240) || artifact.Path <= lastPath || strings.HasPrefix(artifact.Path, "/") || strings.Contains(artifact.Path, "..") || !hashRefPattern.MatchString(artifact.SHA256) {
			return fmt.Errorf("source evidence component contract index artifacts[%d] is invalid or not sorted", index)
		}
		seen = append(seen, artifact.SchemaVersion)
		lastPath = artifact.Path
	}
	sort.Strings(seen)
	expected := append([]string(nil), requiredContractArtifactSchemas...)
	sort.Strings(expected)
	for index := range expected {
		if seen[index] != expected[index] {
			return errors.New("source evidence component contract index artifact identities are incomplete")
		}
	}
	return nil
}

func DecodeInstalledReleaseIdentity(body []byte) (InstalledReleaseIdentity, error) {
	var value InstalledReleaseIdentity
	if err := decodeCanonicalProductReceipt(body, &value); err != nil {
		return InstalledReleaseIdentity{}, fmt.Errorf("decode installed release identity: %w", err)
	}
	if err := ValidateInstalledReleaseIdentity(value); err != nil {
		return InstalledReleaseIdentity{}, err
	}
	return value, nil
}

func ValidateInstalledReleaseIdentity(value InstalledReleaseIdentity) error {
	if value.SchemaVersion != InstalledReleaseSchemaVersion || value.ReleaseSequence <= 0 || !portableWithin(value.Version, 128) || value.CanonicalCommit != strings.ToLower(value.CanonicalCommit) || !gitObjectPattern.MatchString(value.CanonicalCommit) || !hashRefPattern.MatchString(value.PackageSHA256) || !metadataKeyPattern.MatchString(value.TrustedKeyID) || !portableWithin(value.DatabaseSchema, 128) {
		return errors.New("installed release identity is invalid")
	}
	recordedAt, err := parseCanonicalTime("installed release recorded_at", value.RecordedAt)
	if err != nil {
		return err
	}
	observedAt, err := parseCanonicalTime("installed release observed_at", value.ObservedAt)
	if err != nil {
		return err
	}
	if observedAt.Before(recordedAt) {
		return errors.New("installed release observation predates its durable record")
	}
	return nil
}

func DecodeSourceScopeReceipt(body []byte) (SourceScopeReceipt, error) {
	var value SourceScopeReceipt
	if err := decodeCanonicalProductReceipt(body, &value); err != nil {
		return SourceScopeReceipt{}, fmt.Errorf("decode Source scope receipt: %w", err)
	}
	if err := ValidateSourceScopeReceipt(value); err != nil {
		return SourceScopeReceipt{}, err
	}
	return value, nil
}

func ValidateSourceScopeReceipt(value SourceScopeReceipt) error {
	if value.SchemaVersion != SourceScopeReceiptSchemaVersion || value.Product != "unirouter" || value.Status != ComponentStatusAccepted || !portableWithin(value.CampaignID, 128) || !hashRefPattern.MatchString(value.SourceBindingSHA256) || !hashRefPattern.MatchString(value.RequestSetSHA256) || !hashRefPattern.MatchString(value.ReleaseIdentitySHA256) || !hashRefPattern.MatchString(value.ProtocolPinSHA256) || !sourceInstancePattern.MatchString(value.SourceInstance) || value.IdentityAuthority != SourceIdentityAuthority || value.RuntimeObservation != SourceRuntimeObservation || value.RequestCount < 1 || value.RequestCount > MaxRequests || value.MaxRequests != MaxRequests || value.MaxFacts != MaxFacts || value.DatabaseIsolation != "repeatable read" || !value.DatabaseReadOnly || value.TransactionIDAssigned || value.Effects != zeroReceiptEffects() {
		return errors.New("Source scope receipt identity, bounds, or zero-effect evidence is invalid")
	}
	_, err := parseCanonicalTime("Source scope receipt observed_at", value.ObservedAt)
	return err
}

func DecodeVerifierRelease(body []byte) (VerifierRelease, error) {
	var value VerifierRelease
	if err := decodeCanonicalProductReceipt(body, &value); err != nil {
		return VerifierRelease{}, fmt.Errorf("decode component verifier release: %w", err)
	}
	if err := ValidateVerifierRelease(value); err != nil {
		return VerifierRelease{}, err
	}
	return value, nil
}

func ValidateVerifierRelease(value VerifierRelease) error {
	if value.SchemaVersion != VerifierReleaseSchemaVersion || value.VerifierID != ComponentVerifierID || value.ProducerContractID != ProducerContractID || !hashRefPattern.MatchString(value.ProducerContractSHA) || value.SourceCommit != strings.ToLower(value.SourceCommit) || !gitObjectPattern.MatchString(value.SourceCommit) || value.SourceTree != strings.ToLower(value.SourceTree) || !gitObjectPattern.MatchString(value.SourceTree) || !hashRefPattern.MatchString(value.BinarySHA256) || !hashRefPattern.MatchString(value.IntegrationControlPinSHA256) {
		return errors.New("component verifier release identity is invalid")
	}
	_, err := parseCanonicalTime("component verifier release built_at", value.BuiltAt)
	return err
}

func DecodeComponentVerificationReceipt(body []byte) (ComponentVerificationReceipt, error) {
	var value ComponentVerificationReceipt
	if err := decodeCanonicalProductReceipt(body, &value); err != nil {
		return ComponentVerificationReceipt{}, fmt.Errorf("decode component verification receipt: %w", err)
	}
	if err := ValidateComponentVerificationReceipt(value); err != nil {
		return ComponentVerificationReceipt{}, err
	}
	return value, nil
}

func ValidateComponentVerificationReceipt(value ComponentVerificationReceipt) error {
	if value.SchemaVersion != VerificationReceiptSchemaVersion || value.Product != "unirouter" || value.Status != ComponentStatusAccepted || !portableWithin(value.CampaignID, 128) || !hashRefPattern.MatchString(value.CampaignManifestSHA256) || value.ComponentContract.ID != ProducerContractID || !hashRefPattern.MatchString(value.ComponentContract.SHA256) || value.InstalledRelease.SchemaVersion != InstalledReleaseSchemaVersion || !hashRefPattern.MatchString(value.InstalledRelease.SHA256) || value.ComponentScopeReceipt.SchemaVersion != SourceScopeReceiptSchemaVersion || !hashRefPattern.MatchString(value.ComponentScopeReceipt.SHA256) || value.VerifierRelease.ID != ComponentVerifierID || !hashRefPattern.MatchString(value.VerifierRelease.SHA256) || !hashRefPattern.MatchString(value.RequestSetSHA256) || !hashRefPattern.MatchString(value.SourceBindingSHA256) || value.SourceEvidenceSigning.Purpose != SignaturePurpose || value.SourceEvidenceSigning.Domain != SignatureDomain || !validSourceEvidenceKeyID(value.SourceEvidenceSigning.SigningKeyRef) || !signerPublicKeyIDPattern.MatchString(value.SourceEvidenceSigning.SignerPublicKeyID) || !hashRefPattern.MatchString(value.SourceEvidenceSigning.TrustSetSHA256) || !hashRefPattern.MatchString(value.IntegrationControlPinSHA256) || !hashRefPattern.MatchString(value.IntegrationControlReleaseSHA256) || !hashRefPattern.MatchString(value.IntegrationControlBinarySHA256) || value.Effects != zeroReceiptEffects() {
		return errors.New("component verification receipt identity or zero-effect evidence is invalid")
	}
	if len(value.Checks) != len(requiredComponentVerificationChecks) {
		return errors.New("component verification receipt checks are incomplete")
	}
	for index := range requiredComponentVerificationChecks {
		if value.Checks[index] != requiredComponentVerificationChecks[index] {
			return errors.New("component verification receipt checks are not canonical")
		}
	}
	_, err := parseCanonicalTime("component verification receipt verified_at", value.VerifiedAt)
	return err
}

func BuildInstalledReleaseIdentity(release ReleaseFacts, snapshot SnapshotFacts) (InstalledReleaseIdentity, error) {
	value := InstalledReleaseIdentity{
		SchemaVersion:   InstalledReleaseSchemaVersion,
		ReleaseSequence: release.ReleaseSequence,
		Version:         release.Version,
		CanonicalCommit: release.Commit,
		PackageSHA256:   "sha256:" + release.PackageSHA256,
		TrustedKeyID:    release.TrustedKeyID,
		DatabaseSchema:  release.CurrentSchema,
		RecordedAt:      release.RecordedAt,
		ObservedAt:      snapshot.TransactionObservedAt,
	}
	if err := ValidateInstalledReleaseIdentity(value); err != nil {
		return InstalledReleaseIdentity{}, err
	}
	return value, nil
}

func BuildSourceScopeReceipt(requestSet RequestSet, sourceScope SourceScope, protocol ProtocolFacts, release InstalledReleaseIdentity, snapshot SnapshotFacts) (SourceScopeReceipt, error) {
	if err := ValidateRequestSet(requestSet); err != nil {
		return SourceScopeReceipt{}, err
	}
	if err := ValidateSourceScopeForRequestSet(sourceScope, requestSet); err != nil {
		return SourceScopeReceipt{}, err
	}
	if err := ValidateProtocolFacts(protocol); err != nil {
		return SourceScopeReceipt{}, err
	}
	if err := ValidateInstalledReleaseIdentity(release); err != nil {
		return SourceScopeReceipt{}, err
	}
	requestSetSHA, err := SemanticSHA256(requestSet)
	if err != nil {
		return SourceScopeReceipt{}, err
	}
	sourceBindingSHA, err := SemanticSHA256(sourceScope)
	if err != nil {
		return SourceScopeReceipt{}, err
	}
	releaseSHA, err := SemanticSHA256(release)
	if err != nil {
		return SourceScopeReceipt{}, err
	}
	value := SourceScopeReceipt{
		SchemaVersion:         SourceScopeReceiptSchemaVersion,
		Product:               "unirouter",
		CampaignID:            requestSet.CampaignID,
		Status:                ComponentStatusAccepted,
		SourceBindingSHA256:   sourceBindingSHA,
		RequestSetSHA256:      requestSetSHA,
		ReleaseIdentitySHA256: releaseSHA,
		ProtocolPinSHA256:     protocol.PinSHA256,
		SourceInstance:        sourceScope.SourceInstance,
		IdentityAuthority:     sourceScope.IdentityAuthority,
		RuntimeObservation:    sourceScope.RuntimeObservation,
		RequestCount:          len(requestSet.Requests),
		MaxRequests:           requestSet.Limits.MaxRequests,
		MaxFacts:              requestSet.Limits.MaxFacts,
		ObservedAt:            snapshot.TransactionObservedAt,
		DatabaseIsolation:     snapshot.Isolation,
		DatabaseReadOnly:      snapshot.ReadOnly,
		TransactionIDAssigned: snapshot.TransactionIDAssigned,
		Effects:               zeroReceiptEffects(),
	}
	if err := ValidateSourceScopeReceipt(value); err != nil {
		return SourceScopeReceipt{}, err
	}
	return value, nil
}

func ValidatePreparedBindings(requestSet RequestSet, sourceScope SourceScope, protocol ProtocolFacts, release InstalledReleaseIdentity, scope SourceScopeReceipt) error {
	if err := ValidateSourceScopeForRequestSet(sourceScope, requestSet); err != nil {
		return err
	}
	expected, err := BuildSourceScopeReceipt(requestSet, sourceScope, protocol, release, SnapshotFacts{
		Isolation:             scope.DatabaseIsolation,
		ReadOnly:              scope.DatabaseReadOnly,
		TransactionObservedAt: scope.ObservedAt,
		TransactionIDAssigned: scope.TransactionIDAssigned,
		SelectionMode:         "bounded_tenant_request_set",
	})
	if err != nil {
		return err
	}
	if expected != scope {
		return errors.New("Source scope receipt does not match the exact request, Source, release, and protocol inputs")
	}
	return nil
}

func zeroReceiptEffects() ReceiptEffects {
	return ReceiptEffects{DatabaseMutation: "none"}
}

func decodeCanonicalProductReceipt(body []byte, destination any) error {
	if len(body) == 0 || len(body) > MaxContractBytes {
		return errors.New("product receipt size is invalid")
	}
	if err := decodeStrict(body, destination); err != nil {
		return err
	}
	canonical, err := CanonicalJSON(destination)
	if err != nil {
		return err
	}
	if string(canonical) != string(body) {
		return errors.New("product receipt is not canonical JSON")
	}
	return nil
}

func receiptReference(schema string, value any) (ArtifactReference, error) {
	digest, err := SemanticSHA256(value)
	if err != nil {
		return ArtifactReference{}, err
	}
	return ArtifactReference{SchemaVersion: schema, SHA256: digest}, nil
}

func canonicalReceiptTime(value time.Time) (string, error) {
	if value.IsZero() {
		return "", errors.New("receipt time is required")
	}
	return canonicalTime(value.UTC()), nil
}
