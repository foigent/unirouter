package sourceevidence

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	maximumIntegrationControlBinaryBytes = 8 << 20
	maximumIntegrationControlOutputBytes = 1 << 20
	integrationControlTimeout            = 10 * time.Second
)

type IntegrationControlArtifactPin struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	SizeBytes    int64  `json:"size_bytes"`
	SHA256       string `json:"sha256"`
}

type IntegrationControlN1Pin struct {
	SchemaVersion         string                          `json:"schema_version"`
	NeutralReleaseSchema  string                          `json:"neutral_release_schema_version"`
	SourceCommit          string                          `json:"source_commit"`
	SourceTree            string                          `json:"source_tree"`
	ReleaseCommit         string                          `json:"release_commit"`
	ReleaseTree           string                          `json:"release_tree"`
	ReleaseManifestSHA256 string                          `json:"release_manifest_sha256"`
	ContractsSHA256       string                          `json:"contracts_sha256"`
	ValidFixturesSHA256   string                          `json:"valid_fixtures_sha256"`
	InvalidFixturesSHA256 string                          `json:"invalid_fixtures_sha256"`
	CapabilityBoundary    string                          `json:"capability_boundary"`
	Artifacts             []IntegrationControlArtifactPin `json:"artifacts"`
}

type integrationControlReleaseProjection struct {
	SchemaVersion         string `json:"schema_version"`
	SourceCommit          string `json:"source_commit"`
	SourceTree            string `json:"source_tree"`
	ContractsSHA256       string `json:"contracts_sha256"`
	ValidFixturesSHA256   string `json:"valid_fixtures_sha256"`
	InvalidFixturesSHA256 string `json:"invalid_fixtures_sha256"`
	CapabilityBoundary    string `json:"capability_boundary"`
	Artifacts             []struct {
		OS           string `json:"os"`
		Architecture string `json:"architecture"`
		SizeBytes    int64  `json:"size_bytes"`
		SHA256       string `json:"sha256"`
	} `json:"artifacts"`
}

type NeutralVerifierIdentity struct {
	PinSHA256             string `json:"pin_sha256"`
	ReleaseManifestSHA256 string `json:"release_manifest_sha256"`
	BinarySHA256          string `json:"binary_sha256"`
	SourceCommit          string `json:"source_commit"`
	SourceTree            string `json:"source_tree"`
	ReleaseCommit         string `json:"release_commit"`
	ReleaseTree           string `json:"release_tree"`
	OS                    string `json:"os"`
	Architecture          string `json:"architecture"`
}

type PinnedNeutralVerifier struct {
	pin          IntegrationControlN1Pin
	identity     NeutralVerifierIdentity
	binary       []byte
	releaseBytes []byte
}

type n1ManifestVerification struct {
	Valid                  bool   `json:"valid"`
	CampaignManifestSHA256 string `json:"campaign_manifest_sha256"`
}

type n1AcknowledgementVerification struct {
	Valid                 bool   `json:"valid"`
	Product               string `json:"product"`
	AcknowledgementSHA256 string `json:"acknowledgement_sha256"`
}

type NeutralWholeVerification struct {
	Valid                        bool              `json:"valid"`
	CampaignManifestSHA256       string            `json:"campaign_manifest_sha256"`
	AcknowledgementSHA256        map[string]string `json:"acknowledgement_sha256"`
	AuthorizationSHA256          string            `json:"authorization_sha256"`
	AuthorizationSignatureSHA256 string            `json:"authorization_signature_sha256"`
	AuthorizationTrustSetSHA256  string            `json:"authorization_trust_set_sha256"`
	SourceEvidenceTrustSetSHA256 string            `json:"source_evidence_trust_set_sha256"`
	CapabilityBoundary           string            `json:"capability_boundary"`
}

type neutralManifestProjection struct {
	SchemaVersion string            `json:"schema_version"`
	CampaignID    string            `json:"campaign_id"`
	FrozenAt      string            `json:"frozen_at"`
	SourceBinding ArtifactReference `json:"source_binding"`
	RequestSet    struct {
		SchemaVersion string `json:"schema_version"`
		SHA256        string `json:"sha256"`
		MaxRequests   int    `json:"max_requests"`
		MaxFacts      int    `json:"max_facts"`
	} `json:"request_set"`
	Components []struct {
		Product      string            `json:"product"`
		Role         string            `json:"role"`
		Contract     ContractReference `json:"contract"`
		Release      ArtifactReference `json:"release"`
		ScopeReceipt ArtifactReference `json:"scope_receipt"`
	} `json:"components"`
	SourceEvidenceSigning struct {
		Purpose                     string `json:"purpose"`
		Domain                      string `json:"domain"`
		SigningKeyRef               string `json:"signing_key_ref"`
		SignerPublicKeyID           string `json:"signer_public_key_id"`
		TrustSetSHA256              string `json:"trust_set_sha256"`
		ClaimScope                  string `json:"claim_scope"`
		PackageScopeState           string `json:"package_scope_state"`
		NestedExecutionEvidenceV2   string `json:"nested_execution_evidence_v2"`
		RuntimeCloseMarkerAvailable bool   `json:"runtime_close_marker_available"`
	} `json:"source_evidence_signing"`
}

type neutralAuthorizationProjection struct {
	SchemaVersion          string `json:"schema_version"`
	CampaignID             string `json:"campaign_id"`
	CampaignManifestSHA256 string `json:"campaign_manifest_sha256"`
	AuthorizationWindow    struct {
		NotBefore string `json:"not_before"`
		NotAfter  string `json:"not_after"`
	} `json:"authorization_window"`
	ConfirmedAt string `json:"confirmed_at"`
}

type ComponentAcknowledgement struct {
	SchemaVersion              string            `json:"schema_version"`
	Product                    string            `json:"product"`
	CampaignManifestSHA256     string            `json:"campaign_manifest_sha256"`
	ComponentContract          ContractReference `json:"component_contract"`
	ComponentScopeReceipt      ArtifactReference `json:"component_scope_receipt"`
	ProductVerificationReceipt ArtifactReference `json:"product_verification_receipt"`
	VerifierRelease            ContractReference `json:"verifier_release"`
	Status                     string            `json:"status"`
	VerifiedAt                 string            `json:"verified_at"`
}

type ProductManifestInputs struct {
	ManifestBody          []byte
	RequestSetBody        []byte
	SourceScopeBody       []byte
	SourceTrustSetBody    []byte
	ComponentContractBody []byte
	InstalledReleaseBody  []byte
	ScopeReceiptBody      []byte
	VerifierReleaseBody   []byte
	VerifierBinarySHA256  string
}

type ProductManifestVerification struct {
	Manifest          neutralManifestProjection
	RequestSet        RequestSet
	SourceScope       SourceScope
	ComponentContract ComponentContractIndex
	InstalledRelease  InstalledReleaseIdentity
	ScopeReceipt      SourceScopeReceipt
	VerifierRelease   VerifierRelease
	SourcePublicKeys  map[string]ed25519.PublicKey
	SourceTrustSetSHA string
}

type ProductAcknowledgementArtifacts struct {
	Verification ProductManifestVerification
	Receipt      ComponentVerificationReceipt
	ReceiptBody  []byte
	Ack          ComponentAcknowledgement
	AckBody      []byte
}

type NeutralPreauthorizationInputs struct {
	ManifestBody           []byte
	UniRouterAckBody       []byte
	XInsightAckBody        []byte
	AuthorizationBody      []byte
	AuthorizationSignature []byte
	AuthorizationTrustSet  []byte
}

func DecodeIntegrationControlN1Pin(body []byte) (IntegrationControlN1Pin, error) {
	var value IntegrationControlN1Pin
	if err := decodeCanonicalProductReceipt(body, &value); err != nil {
		return IntegrationControlN1Pin{}, fmt.Errorf("decode integration-control N1 pin: %w", err)
	}
	if err := ValidateIntegrationControlN1Pin(value); err != nil {
		return IntegrationControlN1Pin{}, err
	}
	return value, nil
}

func ValidateIntegrationControlN1Pin(value IntegrationControlN1Pin) error {
	if value.SchemaVersion != IntegrationControlN1PinSchemaVersion || value.NeutralReleaseSchema != "foigent.integration-control.release.v1" || value.SourceCommit != strings.ToLower(value.SourceCommit) || !gitObjectPattern.MatchString(value.SourceCommit) || value.SourceTree != strings.ToLower(value.SourceTree) || !gitObjectPattern.MatchString(value.SourceTree) || value.ReleaseCommit != strings.ToLower(value.ReleaseCommit) || !gitObjectPattern.MatchString(value.ReleaseCommit) || value.ReleaseTree != strings.ToLower(value.ReleaseTree) || !gitObjectPattern.MatchString(value.ReleaseTree) || !hashRefPattern.MatchString(value.ReleaseManifestSHA256) || !hashRefPattern.MatchString(value.ContractsSHA256) || !hashRefPattern.MatchString(value.ValidFixturesSHA256) || !hashRefPattern.MatchString(value.InvalidFixturesSHA256) || value.CapabilityBoundary != NeutralCapabilityBoundary || len(value.Artifacts) < 1 || len(value.Artifacts) > 8 {
		return errors.New("integration-control N1 pin identity is invalid")
	}
	last := ""
	for index, artifact := range value.Artifacts {
		identity := artifact.OS + "/" + artifact.Architecture
		if !portableWithin(artifact.OS, 40) || !portableWithin(artifact.Architecture, 40) || identity <= last || artifact.SizeBytes <= 0 || artifact.SizeBytes > maximumIntegrationControlBinaryBytes || !hashRefPattern.MatchString(artifact.SHA256) {
			return fmt.Errorf("integration-control N1 pin artifacts[%d] is invalid or unsorted", index)
		}
		last = identity
	}
	return nil
}

func NewPinnedNeutralVerifier(ctx context.Context, pinBody, releaseBody []byte, executablePath string) (*PinnedNeutralVerifier, error) {
	pin, err := DecodeIntegrationControlN1Pin(pinBody)
	if err != nil {
		return nil, err
	}
	pinSHA := ContentSHA256(pinBody)
	releaseSHA := ContentSHA256(releaseBody)
	if releaseSHA != pin.ReleaseManifestSHA256 {
		return nil, errors.New("integration-control release manifest does not match the product pin")
	}
	var release integrationControlReleaseProjection
	if err := json.Unmarshal(releaseBody, &release); err != nil {
		return nil, fmt.Errorf("decode pinned integration-control release projection: %w", err)
	}
	if release.SchemaVersion != pin.NeutralReleaseSchema || release.SourceCommit != pin.SourceCommit || release.SourceTree != pin.SourceTree || release.ContractsSHA256 != pin.ContractsSHA256 || release.ValidFixturesSHA256 != pin.ValidFixturesSHA256 || release.InvalidFixturesSHA256 != pin.InvalidFixturesSHA256 || release.CapabilityBoundary != pin.CapabilityBoundary {
		return nil, errors.New("integration-control release manifest contradicts the product pin")
	}
	artifact, ok := selectedN1Artifact(pin, runtime.GOOS, runtime.GOARCH)
	if !ok {
		return nil, fmt.Errorf("integration-control N1 pin has no %s/%s executable", runtime.GOOS, runtime.GOARCH)
	}
	matchedReleaseArtifact := false
	for _, candidate := range release.Artifacts {
		if candidate.OS == artifact.OS && candidate.Architecture == artifact.Architecture && candidate.SizeBytes == artifact.SizeBytes && candidate.SHA256 == artifact.SHA256 {
			matchedReleaseArtifact = true
			break
		}
	}
	if !matchedReleaseArtifact {
		return nil, errors.New("integration-control executable pin is absent from the exact release manifest")
	}
	file, err := os.Open(executablePath)
	if err != nil {
		return nil, fmt.Errorf("open pinned integration-control executable: %w", err)
	}
	identity, statErr := file.Stat()
	binary, readErr := io.ReadAll(io.LimitReader(file, maximumIntegrationControlBinaryBytes+1))
	closeErr := file.Close()
	if statErr != nil || !identity.Mode().IsRegular() || identity.Size() != artifact.SizeBytes || readErr != nil || closeErr != nil || int64(len(binary)) != artifact.SizeBytes || ContentSHA256(binary) != artifact.SHA256 {
		return nil, errors.New("integration-control executable does not match the pinned content identity")
	}
	verifier := &PinnedNeutralVerifier{
		pin: pin,
		identity: NeutralVerifierIdentity{
			PinSHA256: pinSHA, ReleaseManifestSHA256: releaseSHA, BinarySHA256: artifact.SHA256,
			SourceCommit: pin.SourceCommit, SourceTree: pin.SourceTree, ReleaseCommit: pin.ReleaseCommit, ReleaseTree: pin.ReleaseTree,
			OS: artifact.OS, Architecture: artifact.Architecture,
		},
		binary:       binary,
		releaseBytes: bytes.Clone(releaseBody),
	}
	var report struct {
		Valid         bool   `json:"valid"`
		ReleaseSHA256 string `json:"release_sha256"`
	}
	if err := verifier.runJSON(ctx, []string{"release", "verify", "--release", "@release"}, map[string][]byte{"release": releaseBody}, &report); err != nil {
		return nil, fmt.Errorf("pinned integration-control release verification failed: %w", err)
	}
	if !report.Valid || report.ReleaseSHA256 != releaseSHA {
		return nil, errors.New("pinned integration-control release report identity is invalid")
	}
	return verifier, nil
}

func (verifier *PinnedNeutralVerifier) Identity() NeutralVerifierIdentity {
	return verifier.identity
}

func (verifier *PinnedNeutralVerifier) VerifyManifest(ctx context.Context, body []byte) (string, error) {
	var report n1ManifestVerification
	if err := verifier.runJSON(ctx, []string{"manifest", "verify", "--manifest", "@manifest"}, map[string][]byte{"manifest": body}, &report); err != nil {
		return "", err
	}
	expected := ContentSHA256(body)
	if !report.Valid || report.CampaignManifestSHA256 != expected {
		return "", errors.New("integration-control manifest report identity is invalid")
	}
	return expected, nil
}

func (verifier *PinnedNeutralVerifier) VerifyAcknowledgement(ctx context.Context, manifestBody, acknowledgementBody []byte) (string, error) {
	var report n1AcknowledgementVerification
	if err := verifier.runJSON(ctx, []string{"ack", "verify", "--manifest", "@manifest", "--ack", "@ack"}, map[string][]byte{"manifest": manifestBody, "ack": acknowledgementBody}, &report); err != nil {
		return "", err
	}
	expected := ContentSHA256(acknowledgementBody)
	if !report.Valid || report.Product != "unirouter" || report.AcknowledgementSHA256 != expected {
		return "", errors.New("integration-control acknowledgement report identity is invalid")
	}
	return expected, nil
}

func (verifier *PinnedNeutralVerifier) VerifyWhole(ctx context.Context, inputs NeutralPreauthorizationInputs) (NeutralWholeVerification, error) {
	var report NeutralWholeVerification
	err := verifier.runJSON(ctx, []string{
		"authorization", "verify",
		"--manifest", "@manifest", "--unirouter-ack", "@unirouter-ack", "--xinsight-ack", "@xinsight-ack",
		"--authorization", "@authorization", "--signature", "@signature", "--trust-set", "@trust-set",
	}, map[string][]byte{
		"manifest": inputs.ManifestBody, "unirouter-ack": inputs.UniRouterAckBody, "xinsight-ack": inputs.XInsightAckBody,
		"authorization": inputs.AuthorizationBody, "signature": inputs.AuthorizationSignature, "trust-set": inputs.AuthorizationTrustSet,
	}, &report)
	if err != nil {
		return NeutralWholeVerification{}, err
	}
	expectedAck := map[string]string{"unirouter": ContentSHA256(inputs.UniRouterAckBody), "xinsight": ContentSHA256(inputs.XInsightAckBody)}
	if !report.Valid || report.CampaignManifestSHA256 != ContentSHA256(inputs.ManifestBody) || report.AuthorizationSHA256 != ContentSHA256(inputs.AuthorizationBody) || report.AuthorizationSignatureSHA256 != ContentSHA256(inputs.AuthorizationSignature) || report.AuthorizationTrustSetSHA256 != ContentSHA256(inputs.AuthorizationTrustSet) || report.AcknowledgementSHA256["unirouter"] != expectedAck["unirouter"] || report.AcknowledgementSHA256["xinsight"] != expectedAck["xinsight"] || len(report.AcknowledgementSHA256) != 2 || report.CapabilityBoundary != NeutralCapabilityBoundary {
		return NeutralWholeVerification{}, errors.New("integration-control whole-verification report identity is invalid")
	}
	return report, nil
}

func (verifier *PinnedNeutralVerifier) VerifyProductManifest(ctx context.Context, inputs ProductManifestInputs) (ProductManifestVerification, error) {
	manifestSHA, err := verifier.VerifyManifest(ctx, inputs.ManifestBody)
	if err != nil {
		return ProductManifestVerification{}, err
	}
	var manifest neutralManifestProjection
	if err := json.Unmarshal(inputs.ManifestBody, &manifest); err != nil {
		return ProductManifestVerification{}, fmt.Errorf("decode verified neutral manifest projection: %w", err)
	}
	if manifest.SchemaVersion != NeutralManifestSchemaVersion || manifestSHA != ContentSHA256(inputs.ManifestBody) || manifest.RequestSet.SchemaVersion != RequestSetSchemaVersion || manifest.RequestSet.MaxRequests != MaxRequests || manifest.RequestSet.MaxFacts != MaxFacts || manifest.SourceBinding.SchemaVersion != SourceScopeSchemaVersion || len(manifest.Components) != 2 {
		return ProductManifestVerification{}, errors.New("verified neutral manifest product projection is invalid")
	}
	requestSet, err := DecodeCanonicalRequestSet(inputs.RequestSetBody)
	if err != nil {
		return ProductManifestVerification{}, err
	}
	sourceScope, err := DecodeCanonicalSourceScope(inputs.SourceScopeBody)
	if err != nil {
		return ProductManifestVerification{}, err
	}
	if err := ValidateSourceScopeForRequestSet(sourceScope, requestSet); err != nil {
		return ProductManifestVerification{}, err
	}
	contract, err := DecodeComponentContractIndex(inputs.ComponentContractBody)
	if err != nil {
		return ProductManifestVerification{}, err
	}
	installed, err := DecodeInstalledReleaseIdentity(inputs.InstalledReleaseBody)
	if err != nil {
		return ProductManifestVerification{}, err
	}
	scope, err := DecodeSourceScopeReceipt(inputs.ScopeReceiptBody)
	if err != nil {
		return ProductManifestVerification{}, err
	}
	verifierRelease, err := DecodeVerifierRelease(inputs.VerifierReleaseBody)
	if err != nil {
		return ProductManifestVerification{}, err
	}
	if !hashRefPattern.MatchString(inputs.VerifierBinarySHA256) || inputs.VerifierBinarySHA256 != verifierRelease.BinarySHA256 {
		return ProductManifestVerification{}, errors.New("component verifier release does not match the executing product verifier binary")
	}
	publicKeys, sourceTrustSHA, err := ParseTrustSet(inputs.SourceTrustSetBody)
	if err != nil {
		return ProductManifestVerification{}, err
	}
	if ContentSHA256(inputs.SourceTrustSetBody) != sourceTrustSHA {
		return ProductManifestVerification{}, errors.New("source_evidence trust set must use its exact canonical product bytes")
	}
	if manifest.CampaignID != requestSet.CampaignID || manifest.SourceBinding.SHA256 != ContentSHA256(inputs.SourceScopeBody) || manifest.RequestSet.SHA256 != ContentSHA256(inputs.RequestSetBody) {
		return ProductManifestVerification{}, errors.New("neutral manifest request set or Source binding does not match exact product bytes")
	}
	if contract.IntegrationControl.PinSHA256 != verifier.identity.PinSHA256 || verifierRelease.IntegrationControlPinSHA256 != verifier.identity.PinSHA256 || verifierRelease.ProducerContractSHA != ContentSHA256(inputs.ComponentContractBody) {
		return ProductManifestVerification{}, errors.New("product contract or verifier release does not bind the pinned integration-control release")
	}
	component := manifest.Components[0]
	if component.Product != "unirouter" || component.Role != "source_evidence_producer" || component.Contract.ID != ProducerContractID || component.Contract.SHA256 != ContentSHA256(inputs.ComponentContractBody) || component.Release.SchemaVersion != InstalledReleaseSchemaVersion || component.Release.SHA256 != ContentSHA256(inputs.InstalledReleaseBody) || component.ScopeReceipt.SchemaVersion != SourceScopeReceiptSchemaVersion || component.ScopeReceipt.SHA256 != ContentSHA256(inputs.ScopeReceiptBody) {
		return ProductManifestVerification{}, errors.New("neutral manifest UniRouter component does not match exact product artifacts")
	}
	if scope.CampaignID != requestSet.CampaignID || scope.SourceBindingSHA256 != ContentSHA256(inputs.SourceScopeBody) || scope.RequestSetSHA256 != ContentSHA256(inputs.RequestSetBody) || scope.ReleaseIdentitySHA256 != ContentSHA256(inputs.InstalledReleaseBody) {
		return ProductManifestVerification{}, errors.New("product Source scope receipt does not match manifest inputs")
	}
	signing := manifest.SourceEvidenceSigning
	publicKey, ok := publicKeys[signing.SigningKeyRef]
	if !ok || signing.Purpose != SignaturePurpose || signing.Domain != SignatureDomain || signing.SignerPublicKeyID != signerPublicKeyID(publicKey) || signing.TrustSetSHA256 != sourceTrustSHA || signing.ClaimScope != SourceFactsSchemaVersion || signing.PackageScopeState != "complete" || signing.NestedExecutionEvidenceV2 != "partial" || signing.RuntimeCloseMarkerAvailable {
		return ProductManifestVerification{}, errors.New("neutral manifest Source-evidence signing authority does not match the product trust set")
	}
	frozenAt, err := parseCanonicalTime("neutral manifest frozen_at", manifest.FrozenAt)
	if err != nil {
		return ProductManifestVerification{}, err
	}
	installedObserved, _ := time.Parse(time.RFC3339Nano, installed.ObservedAt)
	scopeObserved, _ := time.Parse(time.RFC3339Nano, scope.ObservedAt)
	verifierBuilt, _ := time.Parse(time.RFC3339Nano, verifierRelease.BuiltAt)
	if frozenAt.Before(installedObserved) || frozenAt.Before(scopeObserved) || frozenAt.Before(verifierBuilt) {
		return ProductManifestVerification{}, errors.New("neutral manifest was frozen before its UniRouter release, scope, or verifier evidence")
	}
	return ProductManifestVerification{
		Manifest: manifest, RequestSet: requestSet, SourceScope: sourceScope, ComponentContract: contract,
		InstalledRelease: installed, ScopeReceipt: scope, VerifierRelease: verifierRelease,
		SourcePublicKeys: publicKeys, SourceTrustSetSHA: sourceTrustSHA,
	}, nil
}

func (verifier *PinnedNeutralVerifier) BuildProductAcknowledgement(ctx context.Context, inputs ProductManifestInputs, prepared PreparedComponentInputs, verifiedAt time.Time) (ProductAcknowledgementArtifacts, error) {
	verification, err := verifier.VerifyProductManifest(ctx, inputs)
	if err != nil {
		return ProductAcknowledgementArtifacts{}, err
	}
	if err := validateAuthoritativePreparedEvidence(inputs, verification, prepared); err != nil {
		return ProductAcknowledgementArtifacts{}, err
	}
	return verifier.buildProductAcknowledgement(ctx, inputs, verification, verifiedAt)
}

func (verifier *PinnedNeutralVerifier) buildProductAcknowledgement(ctx context.Context, inputs ProductManifestInputs, verification ProductManifestVerification, verifiedAt time.Time) (ProductAcknowledgementArtifacts, error) {
	verifiedAtText, err := canonicalReceiptTime(verifiedAt)
	if err != nil {
		return ProductAcknowledgementArtifacts{}, err
	}
	frozenAt, _ := time.Parse(time.RFC3339Nano, verification.Manifest.FrozenAt)
	if verifiedAt.Before(frozenAt) {
		return ProductAcknowledgementArtifacts{}, errors.New("component verification cannot predate the neutral manifest freeze")
	}
	component := verification.Manifest.Components[0]
	signing := verification.Manifest.SourceEvidenceSigning
	receipt := ComponentVerificationReceipt{
		SchemaVersion: VerificationReceiptSchemaVersion, Product: "unirouter", CampaignID: verification.Manifest.CampaignID,
		Status: ComponentStatusAccepted, CampaignManifestSHA256: ContentSHA256(inputs.ManifestBody),
		ComponentContract: component.Contract, InstalledRelease: component.Release, ComponentScopeReceipt: component.ScopeReceipt,
		VerifierRelease:  ContractReference{ID: ComponentVerifierID, SHA256: ContentSHA256(inputs.VerifierReleaseBody)},
		RequestSetSHA256: ContentSHA256(inputs.RequestSetBody), SourceBindingSHA256: ContentSHA256(inputs.SourceScopeBody),
		SourceEvidenceSigning:       ProductSigningIdentity{Purpose: signing.Purpose, Domain: signing.Domain, SigningKeyRef: signing.SigningKeyRef, SignerPublicKeyID: signing.SignerPublicKeyID, TrustSetSHA256: signing.TrustSetSHA256},
		IntegrationControlPinSHA256: verifier.identity.PinSHA256, IntegrationControlReleaseSHA256: verifier.identity.ReleaseManifestSHA256, IntegrationControlBinarySHA256: verifier.identity.BinarySHA256,
		Checks: append([]string(nil), requiredComponentVerificationChecks...), VerifiedAt: verifiedAtText, Effects: zeroReceiptEffects(),
	}
	if err := ValidateComponentVerificationReceipt(receipt); err != nil {
		return ProductAcknowledgementArtifacts{}, err
	}
	receiptBody, err := CanonicalJSON(receipt)
	if err != nil {
		return ProductAcknowledgementArtifacts{}, err
	}
	ack := ComponentAcknowledgement{
		SchemaVersion: ComponentAcknowledgementSchema, Product: "unirouter", CampaignManifestSHA256: receipt.CampaignManifestSHA256,
		ComponentContract: component.Contract, ComponentScopeReceipt: component.ScopeReceipt,
		ProductVerificationReceipt: ArtifactReference{SchemaVersion: VerificationReceiptSchemaVersion, SHA256: ContentSHA256(receiptBody)},
		VerifierRelease:            receipt.VerifierRelease, Status: ComponentStatusAccepted, VerifiedAt: verifiedAtText,
	}
	ackBody, err := CanonicalJSON(ack)
	if err != nil {
		return ProductAcknowledgementArtifacts{}, err
	}
	if _, err := verifier.VerifyAcknowledgement(ctx, inputs.ManifestBody, ackBody); err != nil {
		return ProductAcknowledgementArtifacts{}, err
	}
	return ProductAcknowledgementArtifacts{Verification: verification, Receipt: receipt, ReceiptBody: receiptBody, Ack: ack, AckBody: ackBody}, nil
}

func (verifier *PinnedNeutralVerifier) VerifyProductAcknowledgement(ctx context.Context, inputs ProductManifestInputs, receiptBody, acknowledgementBody []byte) (ProductAcknowledgementArtifacts, error) {
	receipt, err := DecodeComponentVerificationReceipt(receiptBody)
	if err != nil {
		return ProductAcknowledgementArtifacts{}, err
	}
	verification, err := verifier.VerifyProductManifest(ctx, inputs)
	if err != nil {
		return ProductAcknowledgementArtifacts{}, err
	}
	verifiedAt, _ := time.Parse(time.RFC3339Nano, receipt.VerifiedAt)
	expected, err := verifier.buildProductAcknowledgement(ctx, inputs, verification, verifiedAt)
	if err != nil {
		return ProductAcknowledgementArtifacts{}, err
	}
	if !bytes.Equal(receiptBody, expected.ReceiptBody) || !bytes.Equal(acknowledgementBody, expected.AckBody) {
		return ProductAcknowledgementArtifacts{}, errors.New("UniRouter acknowledgement or product verification receipt does not match exact product evidence")
	}
	return expected, nil
}

func validateAuthoritativePreparedEvidence(inputs ProductManifestInputs, verification ProductManifestVerification, prepared PreparedComponentInputs) error {
	if err := ValidateSourceScopeForRequestSet(prepared.SourceScope, verification.RequestSet); err != nil {
		return err
	}
	if err := ValidateInstalledReleaseIdentity(prepared.InstalledRelease); err != nil {
		return err
	}
	if err := ValidateSourceScopeReceipt(prepared.ScopeReceipt); err != nil {
		return err
	}
	preparedSourceScope, err := CanonicalJSON(prepared.SourceScope)
	if err != nil {
		return err
	}
	preparedRelease, err := CanonicalJSON(prepared.InstalledRelease)
	if err != nil {
		return err
	}
	preparedScopeReceipt, err := CanonicalJSON(prepared.ScopeReceipt)
	if err != nil {
		return err
	}
	if !bytes.Equal(inputs.SourceScopeBody, preparedSourceScope) || !bytes.Equal(inputs.InstalledReleaseBody, preparedRelease) || !bytes.Equal(inputs.ScopeReceiptBody, preparedScopeReceipt) {
		return errors.New("neutral manifest product receipts do not match the current read-only product preparation")
	}
	return nil
}

func ValidateCaptureAuthorization(report NeutralWholeVerification, inputs NeutralPreauthorizationInputs, verification ProductManifestVerification, createdAt time.Time) (NeutralAuthorizationBinding, error) {
	if report.SourceEvidenceTrustSetSHA256 != verification.SourceTrustSetSHA {
		return NeutralAuthorizationBinding{}, errors.New("neutral authorization report does not bind the product Source-evidence trust set")
	}
	var authorization neutralAuthorizationProjection
	if err := json.Unmarshal(inputs.AuthorizationBody, &authorization); err != nil {
		return NeutralAuthorizationBinding{}, fmt.Errorf("decode verified neutral authorization projection: %w", err)
	}
	if authorization.SchemaVersion != NeutralAuthorizationSchemaVersion || authorization.CampaignID != verification.Manifest.CampaignID || authorization.CampaignManifestSHA256 != report.CampaignManifestSHA256 {
		return NeutralAuthorizationBinding{}, errors.New("neutral authorization product projection does not match the verified campaign")
	}
	notBefore, err := parseCanonicalTime("neutral authorization not_before", authorization.AuthorizationWindow.NotBefore)
	if err != nil {
		return NeutralAuthorizationBinding{}, err
	}
	notAfter, err := parseCanonicalTime("neutral authorization not_after", authorization.AuthorizationWindow.NotAfter)
	if err != nil {
		return NeutralAuthorizationBinding{}, err
	}
	confirmedAt, err := parseCanonicalTime("neutral authorization confirmed_at", authorization.ConfirmedAt)
	if err != nil {
		return NeutralAuthorizationBinding{}, err
	}
	createdAt = createdAt.UTC()
	if createdAt.Before(notBefore) || !createdAt.Before(notAfter) || createdAt.Before(confirmedAt) {
		return NeutralAuthorizationBinding{}, errors.New("source evidence capture time is outside the verified neutral authorization window")
	}
	return NeutralAuthorizationBinding{
		AuthorizationManifestSHA256:  report.CampaignManifestSHA256,
		AuthorizationStatementSHA256: report.AuthorizationSHA256,
		AuthorizationSignatureSHA256: report.AuthorizationSignatureSHA256,
		SourceBindingSHA256:          verification.ScopeReceipt.SourceBindingSHA256,
	}, nil
}

func (verifier *PinnedNeutralVerifier) runJSON(ctx context.Context, arguments []string, inputs map[string][]byte, output any) error {
	if verifier == nil || len(verifier.binary) == 0 {
		return errors.New("pinned integration-control verifier is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, integrationControlTimeout)
	defer cancel()
	executable, err := os.CreateTemp("", "uniroute-integration-control-n1-")
	if err != nil {
		return err
	}
	executablePath := executable.Name()
	defer func() { _ = os.Remove(executablePath) }()
	if err := executable.Chmod(0o700); err != nil {
		_ = executable.Close()
		return err
	}
	if written, err := executable.Write(verifier.binary); err != nil || written != len(verifier.binary) {
		_ = executable.Close()
		return errors.New("write pinned integration-control executable snapshot failed")
	}
	if err := executable.Sync(); err != nil {
		_ = executable.Close()
		return err
	}
	if err := executable.Close(); err != nil {
		return err
	}

	keys := make([]string, 0, len(inputs))
	for key := range inputs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	readers := make([]*os.File, 0, len(keys))
	writers := make([]*os.File, 0, len(keys))
	paths := make(map[string]string, len(keys))
	for _, key := range keys {
		reader, writer, pipeErr := os.Pipe()
		if pipeErr != nil {
			closeFiles(readers)
			closeFiles(writers)
			return pipeErr
		}
		paths[key] = fmt.Sprintf("/dev/fd/%d", 3+len(readers))
		readers = append(readers, reader)
		writers = append(writers, writer)
	}
	resolvedArguments := append([]string(nil), arguments...)
	for index, argument := range resolvedArguments {
		if strings.HasPrefix(argument, "@") {
			pathValue, ok := paths[strings.TrimPrefix(argument, "@")]
			if !ok {
				closeFiles(readers)
				closeFiles(writers)
				return errors.New("integration-control input argument is unresolved")
			}
			resolvedArguments[index] = pathValue
		}
	}
	command := exec.CommandContext(ctx, executablePath, resolvedArguments...)
	command.Env = []string{"LANG=C", "LC_ALL=C", "PATH=/usr/bin:/bin"}
	command.ExtraFiles = readers
	var stdout, stderr boundedBuffer
	stdout.limit = maximumIntegrationControlOutputBytes
	stderr.limit = maximumIntegrationControlOutputBytes
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		closeFiles(readers)
		closeFiles(writers)
		return err
	}
	closeFiles(readers)
	var writes sync.WaitGroup
	writeErrors := make(chan error, len(keys))
	for index, key := range keys {
		writer := writers[index]
		body := bytes.Clone(inputs[key])
		writes.Add(1)
		go func() {
			defer writes.Done()
			_, writeErr := writer.Write(body)
			closeErr := writer.Close()
			if writeErr != nil {
				writeErrors <- writeErr
			} else if closeErr != nil {
				writeErrors <- closeErr
			}
		}()
	}
	waitErr := command.Wait()
	writes.Wait()
	close(writeErrors)
	if waitErr != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("pinned integration-control verifier timed out: %w", ctx.Err())
		}
		return fmt.Errorf("pinned integration-control verifier rejected input: %w: %s", waitErr, strings.TrimSpace(stderr.String()))
	}
	for writeErr := range writeErrors {
		if writeErr != nil {
			return fmt.Errorf("stream exact input to integration-control verifier: %w", writeErr)
		}
	}
	if stdout.exceeded || stderr.exceeded {
		return errors.New("pinned integration-control verifier exceeded bounded output")
	}
	reportBody := bytes.TrimSuffix(stdout.Bytes(), []byte("\n"))
	if err := decodeStrict(reportBody, output); err != nil {
		return fmt.Errorf("decode pinned integration-control report: %w", err)
	}
	return nil
}

type boundedBuffer struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (buffer *boundedBuffer) Write(body []byte) (int, error) {
	if buffer.exceeded {
		return len(body), nil
	}
	remaining := buffer.limit - buffer.Len()
	if len(body) > remaining {
		buffer.exceeded = true
		if remaining > 0 {
			_, _ = buffer.Buffer.Write(body[:remaining])
		}
		return len(body), nil
	}
	return buffer.Buffer.Write(body)
}

func selectedN1Artifact(pin IntegrationControlN1Pin, operatingSystem, architecture string) (IntegrationControlArtifactPin, bool) {
	for _, artifact := range pin.Artifacts {
		if artifact.OS == operatingSystem && artifact.Architecture == architecture {
			return artifact, true
		}
	}
	return IntegrationControlArtifactPin{}, false
}

func DecodeCanonicalRequestSet(body []byte) (RequestSet, error) {
	value, err := DecodeRequestSet(body)
	if err != nil {
		return RequestSet{}, err
	}
	canonical, err := CanonicalJSON(value)
	if err != nil || !bytes.Equal(canonical, body) {
		return RequestSet{}, errors.New("request set must use exact product canonical JSON before neutral binding")
	}
	return value, nil
}

func DecodeCanonicalSourceScope(body []byte) (SourceScope, error) {
	value, err := DecodeSourceScope(body)
	if err != nil {
		return SourceScope{}, err
	}
	canonical, err := CanonicalJSON(value)
	if err != nil || !bytes.Equal(canonical, body) {
		return SourceScope{}, errors.New("Source scope must use exact product canonical JSON before neutral binding")
	}
	return value, nil
}

func closeFiles(files []*os.File) {
	for _, file := range files {
		_ = file.Close()
	}
}
