package sourceevidence

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
)

const (
	ConformanceProjectionSchemaVersion = "unirouter.source-evidence.conformance-projection.v1"
	ConformanceFixtureAuthority        = "synthetic_conformance_only"
	neutralVerificationSealDomain      = "unirouter.source-evidence.neutral-whole-verification.v1"
	packageVerificationSealDomain      = "unirouter.source-evidence.package-verification.v1"
	productAcknowledgementSealDomain   = "unirouter.source-evidence.product-acknowledgement-verification.v1"
)

type ConformanceProjectionProductAuthority struct {
	ComponentVerificationReceipt ArtifactReference `json:"component_verification_receipt"`
	InstalledRelease             ArtifactReference `json:"installed_release"`
	VerifierRelease              ContractReference `json:"verifier_release"`
	IntegrationControlPinSHA256  string            `json:"integration_control_pin_sha256"`
}

type ConformanceProjectionNeutralChain struct {
	CampaignManifestSHA256         string `json:"campaign_manifest_sha256"`
	UniRouterAcknowledgementSHA256 string `json:"unirouter_acknowledgement_sha256"`
	XInsightAcknowledgementSHA256  string `json:"xinsight_acknowledgement_sha256"`
	AuthorizationSHA256            string `json:"authorization_sha256"`
	AuthorizationSignatureSHA256   string `json:"authorization_signature_sha256"`
	AuthorizationTrustSetSHA256    string `json:"authorization_trust_set_sha256"`
}

type ConformanceProjectionSourceAuthority struct {
	SourceBindingSHA256 string                 `json:"source_binding_sha256"`
	Signer              ProductSigningIdentity `json:"signer"`
}

type ConformanceProjectionPackageManifest struct {
	SchemaVersion string `json:"schema_version"`
	SHA256        string `json:"sha256"`
	CreatedAt     string `json:"created_at"`
}

type ConformanceProjectionPackageClaim struct {
	Scope                       string `json:"scope"`
	State                       string `json:"state"`
	NestedExecutionEvidenceV2   string `json:"nested_execution_evidence_v2"`
	RuntimeCloseMarkerAvailable bool   `json:"runtime_close_marker_available"`
}

type ConformanceProjectionPackage struct {
	SHA256   string                               `json:"sha256"`
	Bytes    int64                                `json:"bytes"`
	Manifest ConformanceProjectionPackageManifest `json:"manifest"`
	Claim    ConformanceProjectionPackageClaim    `json:"claim"`
}

// ConformanceProjection is the product-owned, payload-neutral terminal view of
// one independently verified component package. It does not add claims to the
// signed package and does not validate another product's receipt substance.
type ConformanceProjection struct {
	SchemaVersion      string                                `json:"schema_version"`
	Product            string                                `json:"product"`
	ComponentContract  ContractReference                     `json:"component_contract"`
	ProductAuthority   ConformanceProjectionProductAuthority `json:"product_authority"`
	NeutralChain       ConformanceProjectionNeutralChain     `json:"neutral_chain"`
	SourceAuthority    ConformanceProjectionSourceAuthority  `json:"source_authority"`
	Package            ConformanceProjectionPackage          `json:"package"`
	CapabilityBoundary string                                `json:"capability_boundary"`
}

type ConformanceProjectionInputs struct {
	ComponentContractBody  []byte
	ProductAcknowledgement ProductAcknowledgementArtifacts
	NeutralInputs          NeutralPreauthorizationInputs
	NeutralVerification    NeutralWholeVerification
	PackageVerification    PackageVerification
}

func BuildConformanceProjection(inputs ConformanceProjectionInputs) (ConformanceProjection, []byte, error) {
	if !productAcknowledgementArtifactsVerified(inputs.ProductAcknowledgement) {
		return ConformanceProjection{}, nil, errors.New("conformance projection product acknowledgement is not an unmodified verifier output")
	}
	if !neutralWholeVerificationVerified(inputs.NeutralVerification) {
		return ConformanceProjection{}, nil, errors.New("conformance projection neutral authority is not an unmodified pinned-verifier output")
	}
	if !packageVerificationVerified(inputs.PackageVerification) {
		return ConformanceProjection{}, nil, errors.New("conformance projection package identity is not an unmodified package-verifier output")
	}
	contract, err := DecodeComponentContractIndex(inputs.ComponentContractBody)
	if err != nil {
		return ConformanceProjection{}, nil, err
	}
	acknowledgement := inputs.ProductAcknowledgement
	receipt, err := DecodeComponentVerificationReceipt(acknowledgement.ReceiptBody)
	if err != nil {
		return ConformanceProjection{}, nil, err
	}
	receiptBody, err := CanonicalJSON(acknowledgement.Receipt)
	if err != nil || !bytes.Equal(receiptBody, acknowledgement.ReceiptBody) {
		return ConformanceProjection{}, nil, errors.New("conformance projection component receipt is not the exact verified product receipt")
	}
	var componentAck ComponentAcknowledgement
	if err := decodeStrict(acknowledgement.AckBody, &componentAck); err != nil {
		return ConformanceProjection{}, nil, fmt.Errorf("decode conformance projection component acknowledgement: %w", err)
	}
	ackBody, err := CanonicalJSON(acknowledgement.Ack)
	if err != nil || !bytes.Equal(ackBody, acknowledgement.AckBody) || componentAck != acknowledgement.Ack {
		return ConformanceProjection{}, nil, errors.New("conformance projection acknowledgement is not the exact verified product acknowledgement")
	}
	if err := validateConformanceProductAuthority(inputs, contract, receipt, componentAck); err != nil {
		return ConformanceProjection{}, nil, err
	}
	if err := validateConformanceNeutralChain(inputs, receipt, componentAck); err != nil {
		return ConformanceProjection{}, nil, err
	}
	packageVerification := inputs.PackageVerification
	if err := ValidatePackageManifest(packageVerification.Manifest); err != nil {
		return ConformanceProjection{}, nil, err
	}
	manifestBody, err := CanonicalJSON(packageVerification.Manifest)
	if err != nil || !bytes.Equal(manifestBody, packageVerification.manifestBody) || ContentSHA256(packageVerification.manifestBody) != packageVerification.ManifestSHA256 {
		return ConformanceProjection{}, nil, errors.New("conformance projection package manifest is not the exact verified manifest")
	}
	createdAt, err := parseCanonicalTime("conformance projection package created_at", packageVerification.Manifest.CreatedAt)
	if err != nil {
		return ConformanceProjection{}, nil, err
	}
	binding, err := ValidateCaptureAuthorization(inputs.NeutralVerification, inputs.NeutralInputs, acknowledgement.Verification, createdAt)
	if err != nil {
		return ConformanceProjection{}, nil, err
	}
	manifest := packageVerification.Manifest
	if !hashRefPattern.MatchString(packageVerification.PackageSHA256) || packageVerification.PackageBytes <= 0 || packageVerification.PackageBytes > MaxPackageBytes || !hashRefPattern.MatchString(packageVerification.ManifestSHA256) {
		return ConformanceProjection{}, nil, errors.New("conformance projection package verification identity is invalid")
	}
	if manifest.AuthorizationManifestSHA256 != binding.AuthorizationManifestSHA256 || manifest.AuthorizationStatementSHA256 != binding.AuthorizationStatementSHA256 || manifest.AuthorizationSignatureSHA256 != binding.AuthorizationSignatureSHA256 || manifest.SourceBindingSHA256 != binding.SourceBindingSHA256 {
		return ConformanceProjection{}, nil, errors.New("conformance projection package does not match the verified capture authorization")
	}
	signing := receipt.SourceEvidenceSigning
	if manifest.Purpose != signing.Purpose || manifest.Domain != signing.Domain || manifest.SigningKeyRef != signing.SigningKeyRef || manifest.SignerPublicKeyID != signing.SignerPublicKeyID || manifest.TrustSetSHA256 != signing.TrustSetSHA256 {
		return ConformanceProjection{}, nil, errors.New("conformance projection package signer does not match product authority")
	}

	value := ConformanceProjection{
		SchemaVersion:     ConformanceProjectionSchemaVersion,
		Product:           "unirouter",
		ComponentContract: receipt.ComponentContract,
		ProductAuthority: ConformanceProjectionProductAuthority{
			ComponentVerificationReceipt: ArtifactReference{SchemaVersion: VerificationReceiptSchemaVersion, SHA256: ContentSHA256(acknowledgement.ReceiptBody)},
			InstalledRelease:             receipt.InstalledRelease,
			VerifierRelease:              receipt.VerifierRelease,
			IntegrationControlPinSHA256:  receipt.IntegrationControlPinSHA256,
		},
		NeutralChain: ConformanceProjectionNeutralChain{
			CampaignManifestSHA256:         inputs.NeutralVerification.CampaignManifestSHA256,
			UniRouterAcknowledgementSHA256: inputs.NeutralVerification.AcknowledgementSHA256["unirouter"],
			XInsightAcknowledgementSHA256:  inputs.NeutralVerification.AcknowledgementSHA256["xinsight"],
			AuthorizationSHA256:            inputs.NeutralVerification.AuthorizationSHA256,
			AuthorizationSignatureSHA256:   inputs.NeutralVerification.AuthorizationSignatureSHA256,
			AuthorizationTrustSetSHA256:    inputs.NeutralVerification.AuthorizationTrustSetSHA256,
		},
		SourceAuthority: ConformanceProjectionSourceAuthority{SourceBindingSHA256: receipt.SourceBindingSHA256, Signer: signing},
		Package: ConformanceProjectionPackage{
			SHA256: packageVerification.PackageSHA256,
			Bytes:  packageVerification.PackageBytes,
			Manifest: ConformanceProjectionPackageManifest{
				SchemaVersion: manifest.SchemaVersion,
				SHA256:        packageVerification.ManifestSHA256,
				CreatedAt:     manifest.CreatedAt,
			},
			Claim: ConformanceProjectionPackageClaim{
				Scope: manifest.ClaimScope, State: manifest.ClaimState,
				NestedExecutionEvidenceV2:   manifest.NestedExecutionEvidenceV2,
				RuntimeCloseMarkerAvailable: acknowledgement.Verification.Manifest.SourceEvidenceSigning.RuntimeCloseMarkerAvailable,
			},
		},
		CapabilityBoundary: inputs.NeutralVerification.CapabilityBoundary,
	}
	if err := ValidateConformanceProjection(value); err != nil {
		return ConformanceProjection{}, nil, err
	}
	body, err := CanonicalJSON(value)
	if err != nil {
		return ConformanceProjection{}, nil, err
	}
	return value, body, nil
}

func verificationProvenanceSeal(domain string, value any) ([sha256.Size]byte, error) {
	body, err := CanonicalJSON(value)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	input := make([]byte, 0, len(domain)+1+len(body))
	input = append(input, domain...)
	input = append(input, 0)
	input = append(input, body...)
	return sha256.Sum256(input), nil
}

func DecodeConformanceProjection(body []byte) (ConformanceProjection, error) {
	var value ConformanceProjection
	if err := decodeStrict(body, &value); err != nil {
		return ConformanceProjection{}, fmt.Errorf("decode conformance projection: %w", err)
	}
	canonical, err := CanonicalJSON(value)
	if err != nil {
		return ConformanceProjection{}, err
	}
	if !bytes.Equal(canonical, body) {
		return ConformanceProjection{}, errors.New("conformance projection is not canonical JSON")
	}
	if err := ValidateConformanceProjection(value); err != nil {
		return ConformanceProjection{}, err
	}
	return value, nil
}

func ValidateConformanceProjection(value ConformanceProjection) error {
	if value.SchemaVersion != ConformanceProjectionSchemaVersion || value.Product != "unirouter" || value.ComponentContract.ID != ProducerContractID || !hashRefPattern.MatchString(value.ComponentContract.SHA256) || value.CapabilityBoundary != NeutralCapabilityBoundary {
		return errors.New("conformance projection root identity is invalid")
	}
	authority := value.ProductAuthority
	if authority.ComponentVerificationReceipt.SchemaVersion != VerificationReceiptSchemaVersion || !hashRefPattern.MatchString(authority.ComponentVerificationReceipt.SHA256) || authority.InstalledRelease.SchemaVersion != InstalledReleaseSchemaVersion || !hashRefPattern.MatchString(authority.InstalledRelease.SHA256) || authority.VerifierRelease.ID != ComponentVerifierID || !hashRefPattern.MatchString(authority.VerifierRelease.SHA256) || !hashRefPattern.MatchString(authority.IntegrationControlPinSHA256) {
		return errors.New("conformance projection product authority is invalid")
	}
	chain := value.NeutralChain
	for _, digest := range []string{chain.CampaignManifestSHA256, chain.UniRouterAcknowledgementSHA256, chain.XInsightAcknowledgementSHA256, chain.AuthorizationSHA256, chain.AuthorizationSignatureSHA256, chain.AuthorizationTrustSetSHA256} {
		if !hashRefPattern.MatchString(digest) {
			return errors.New("conformance projection neutral chain is invalid")
		}
	}
	source := value.SourceAuthority
	if !hashRefPattern.MatchString(source.SourceBindingSHA256) || source.Signer.Purpose != SignaturePurpose || source.Signer.Domain != SignatureDomain || !validSourceEvidenceKeyID(source.Signer.SigningKeyRef) || !signerPublicKeyIDPattern.MatchString(source.Signer.SignerPublicKeyID) || !hashRefPattern.MatchString(source.Signer.TrustSetSHA256) {
		return errors.New("conformance projection Source authority is invalid")
	}
	packageIdentity := value.Package
	if !hashRefPattern.MatchString(packageIdentity.SHA256) || packageIdentity.Bytes <= 0 || packageIdentity.Bytes > MaxPackageBytes || packageIdentity.Manifest.SchemaVersion != PackageManifestSchemaVersion || !hashRefPattern.MatchString(packageIdentity.Manifest.SHA256) {
		return errors.New("conformance projection package identity is invalid")
	}
	if _, err := parseCanonicalTime("conformance projection package created_at", packageIdentity.Manifest.CreatedAt); err != nil {
		return err
	}
	claim := packageIdentity.Claim
	if claim.Scope != SourceFactsSchemaVersion || claim.State != "complete" || claim.NestedExecutionEvidenceV2 != "partial" || claim.RuntimeCloseMarkerAvailable {
		return errors.New("conformance projection package claim is invalid")
	}
	return nil
}

func VerifyConformanceProjection(body []byte, inputs ConformanceProjectionInputs) (ConformanceProjection, error) {
	actual, err := DecodeConformanceProjection(body)
	if err != nil {
		return ConformanceProjection{}, err
	}
	_, expectedBody, err := BuildConformanceProjection(inputs)
	if err != nil {
		return ConformanceProjection{}, err
	}
	if !bytes.Equal(body, expectedBody) {
		return ConformanceProjection{}, errors.New("conformance projection does not match exact verified product lifecycle inputs")
	}
	return actual, nil
}

func validateConformanceProductAuthority(inputs ConformanceProjectionInputs, contract ComponentContractIndex, receipt ComponentVerificationReceipt, ack ComponentAcknowledgement) error {
	verification := inputs.ProductAcknowledgement.Verification
	if err := ValidateInstalledReleaseIdentity(verification.InstalledRelease); err != nil {
		return err
	}
	if err := ValidateSourceScopeReceipt(verification.ScopeReceipt); err != nil {
		return err
	}
	if err := ValidateVerifierRelease(verification.VerifierRelease); err != nil {
		return err
	}
	if err := ValidateRequestSet(verification.RequestSet); err != nil {
		return err
	}
	if err := ValidateSourceScopeForRequestSet(verification.SourceScope, verification.RequestSet); err != nil {
		return err
	}
	componentContractSHA := ContentSHA256(inputs.ComponentContractBody)
	verificationContractBody, err := CanonicalJSON(verification.ComponentContract)
	if err != nil || !bytes.Equal(verificationContractBody, inputs.ComponentContractBody) {
		return errors.New("conformance projection component contract is not the exact product-verified contract")
	}
	installedBody, err := CanonicalJSON(verification.InstalledRelease)
	if err != nil {
		return err
	}
	scopeReceiptBody, err := CanonicalJSON(verification.ScopeReceipt)
	if err != nil {
		return err
	}
	verifierReleaseBody, err := CanonicalJSON(verification.VerifierRelease)
	if err != nil {
		return err
	}
	requestSetBody, err := CanonicalJSON(verification.RequestSet)
	if err != nil {
		return err
	}
	sourceScopeBody, err := CanonicalJSON(verification.SourceScope)
	if err != nil {
		return err
	}
	signing := verification.Manifest.SourceEvidenceSigning
	if receipt.CampaignID != verification.Manifest.CampaignID || receipt.CampaignManifestSHA256 != ContentSHA256(inputs.NeutralInputs.ManifestBody) || receipt.ComponentContract != (ContractReference{ID: ProducerContractID, SHA256: componentContractSHA}) || receipt.InstalledRelease != (ArtifactReference{SchemaVersion: InstalledReleaseSchemaVersion, SHA256: ContentSHA256(installedBody)}) || receipt.ComponentScopeReceipt != (ArtifactReference{SchemaVersion: SourceScopeReceiptSchemaVersion, SHA256: ContentSHA256(scopeReceiptBody)}) || receipt.VerifierRelease != (ContractReference{ID: ComponentVerifierID, SHA256: ContentSHA256(verifierReleaseBody)}) || receipt.RequestSetSHA256 != ContentSHA256(requestSetBody) || receipt.SourceBindingSHA256 != ContentSHA256(sourceScopeBody) {
		return errors.New("conformance projection product receipt does not match exact product verification inputs")
	}
	if receipt.SourceEvidenceSigning != (ProductSigningIdentity{Purpose: signing.Purpose, Domain: signing.Domain, SigningKeyRef: signing.SigningKeyRef, SignerPublicKeyID: signing.SignerPublicKeyID, TrustSetSHA256: signing.TrustSetSHA256}) || receipt.SourceEvidenceSigning.TrustSetSHA256 != verification.SourceTrustSetSHA {
		return errors.New("conformance projection Source signer does not match exact product verification inputs")
	}
	if contract.SchemaVersion != ProducerContractID || contract.IntegrationControl.PinSHA256 != receipt.IntegrationControlPinSHA256 || verification.VerifierRelease.ProducerContractSHA != componentContractSHA || verification.VerifierRelease.IntegrationControlPinSHA256 != receipt.IntegrationControlPinSHA256 {
		return errors.New("conformance projection product contract or verifier authority conflicts")
	}
	if ack.SchemaVersion != ComponentAcknowledgementSchema || ack.Product != "unirouter" || ack.CampaignManifestSHA256 != receipt.CampaignManifestSHA256 || ack.ComponentContract != receipt.ComponentContract || ack.ComponentScopeReceipt != receipt.ComponentScopeReceipt || ack.ProductVerificationReceipt != (ArtifactReference{SchemaVersion: VerificationReceiptSchemaVersion, SHA256: ContentSHA256(inputs.ProductAcknowledgement.ReceiptBody)}) || ack.VerifierRelease != receipt.VerifierRelease || ack.Status != ComponentStatusAccepted || ack.VerifiedAt != receipt.VerifiedAt {
		return errors.New("conformance projection product acknowledgement conflicts with its verification receipt")
	}
	return nil
}

func validateConformanceNeutralChain(inputs ConformanceProjectionInputs, receipt ComponentVerificationReceipt, ack ComponentAcknowledgement) error {
	report := inputs.NeutralVerification
	if !report.Valid || report.CapabilityBoundary != NeutralCapabilityBoundary || len(report.AcknowledgementSHA256) != 2 || !bytes.Equal(inputs.NeutralInputs.UniRouterAckBody, inputs.ProductAcknowledgement.AckBody) {
		return errors.New("conformance projection neutral whole verification is invalid")
	}
	if report.CampaignManifestSHA256 != ContentSHA256(inputs.NeutralInputs.ManifestBody) || report.AcknowledgementSHA256["unirouter"] != ContentSHA256(inputs.NeutralInputs.UniRouterAckBody) || report.AcknowledgementSHA256["xinsight"] != ContentSHA256(inputs.NeutralInputs.XInsightAckBody) || report.AuthorizationSHA256 != ContentSHA256(inputs.NeutralInputs.AuthorizationBody) || report.AuthorizationSignatureSHA256 != ContentSHA256(inputs.NeutralInputs.AuthorizationSignature) || report.AuthorizationTrustSetSHA256 != ContentSHA256(inputs.NeutralInputs.AuthorizationTrustSet) {
		return errors.New("conformance projection neutral chain does not match exact N1-verified inputs")
	}
	if report.CampaignManifestSHA256 != receipt.CampaignManifestSHA256 || report.CampaignManifestSHA256 != ack.CampaignManifestSHA256 || report.SourceEvidenceTrustSetSHA256 != receipt.SourceEvidenceSigning.TrustSetSHA256 {
		return errors.New("conformance projection neutral chain conflicts with product authority")
	}
	return nil
}
