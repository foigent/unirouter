package sourceevidence

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProductContractIndexAndN1PinBindExactLocalArtifacts(t *testing.T) {
	contractRoot := filepath.Join(repositoryRoot(t), "contracts", "source-evidence", "v1")
	pinBody, err := os.ReadFile(filepath.Join(contractRoot, "unirouter.integration-control.n1-pin.v1.json"))
	require.NoError(t, err)
	pin, err := DecodeIntegrationControlN1Pin(pinBody)
	require.NoError(t, err)
	require.Equal(t, "12f1ede8ae85e9d311a8150694eea54ff6f02950", pin.SourceCommit)
	require.Equal(t, "c9e5a77dd0416654d8e46efb09f18a6ecca026f8", pin.ReleaseCommit)

	indexBody, err := os.ReadFile(filepath.Join(contractRoot, "unirouter.source-evidence.component-package.v1.json"))
	require.NoError(t, err)
	index, err := DecodeComponentContractIndex(indexBody)
	require.NoError(t, err)
	require.Equal(t, ContentSHA256(pinBody), index.IntegrationControl.PinSHA256)
	require.Len(t, index.Artifacts, len(requiredContractArtifactSchemas))
	for _, artifact := range index.Artifacts {
		body, readErr := os.ReadFile(filepath.Join(contractRoot, artifact.Path))
		require.NoErrorf(t, readErr, "read indexed contract %s", artifact.Path)
		require.Equalf(t, ContentSHA256(body), artifact.SHA256, "indexed digest drift for %s", artifact.Path)
	}
}

func TestPreparedReceiptsAreCanonicalBoundedAndHonest(t *testing.T) {
	requestSet := validRequestSet(t)
	sourceScope := SourceScopeForRequestSet(requestSet)
	protocol := ProtocolFacts{
		PinVersion: "pin-v1", ProtocolVersion: "2.0.2", ReleaseDigest: "sha256:" + strings.Repeat("1", 64),
		PinSHA256: "sha256:" + strings.Repeat("2", 64), Components: []ProtocolComponent{{Name: "schema", SHA256: "sha256:" + strings.Repeat("3", 64)}},
	}
	releaseFacts := ReleaseFacts{
		ReleaseSequence: 8, Version: "1.1.8-source-evidence", Commit: strings.Repeat("a", 40),
		PackageSHA256: strings.Repeat("b", 64), TrustedKeyID: "release-key-1", RecordedSchema: "000059_enterprise_access_provisioning",
		CurrentSchema: "000059_enterprise_access_provisioning", RecordedAt: "2026-08-10T11:00:00Z",
	}
	snapshot := SnapshotFacts{
		Isolation: "repeatable read", ReadOnly: true, TransactionObservedAt: "2026-08-10T11:01:00Z",
		TransactionIDAssigned: false, SelectionMode: "bounded_tenant_request_set",
	}
	release, err := BuildInstalledReleaseIdentity(releaseFacts, snapshot)
	require.NoError(t, err)
	scope, err := BuildSourceScopeReceipt(requestSet, sourceScope, protocol, release, snapshot)
	require.NoError(t, err)
	require.Equal(t, zeroReceiptEffects(), scope.Effects)
	require.Equal(t, SourceRuntimeObservation, scope.RuntimeObservation)
	require.NoError(t, ValidatePreparedBindings(requestSet, sourceScope, protocol, release, scope))

	releaseBody, err := CanonicalJSON(release)
	require.NoError(t, err)
	decodedRelease, err := DecodeInstalledReleaseIdentity(releaseBody)
	require.NoError(t, err)
	require.Equal(t, release, decodedRelease)
	scopeBody, err := CanonicalJSON(scope)
	require.NoError(t, err)
	decodedScope, err := DecodeSourceScopeReceipt(scopeBody)
	require.NoError(t, err)
	require.Equal(t, scope, decodedScope)

	_, err = DecodeInstalledReleaseIdentity(append([]byte(" "), releaseBody...))
	require.ErrorContains(t, err, "not canonical")
	release.CanonicalCommit = strings.Repeat("A", 40)
	require.ErrorContains(t, ValidateInstalledReleaseIdentity(release), "invalid")
}

func TestPinnedN1ExactReleaseAndWholeCompatibility(t *testing.T) {
	root := strings.TrimSpace(os.Getenv("UNIROUTE_INTEGRATION_CONTROL_N1_ROOT"))
	if root == "" {
		t.Skip("set UNIROUTE_INTEGRATION_CONTROL_N1_ROOT to the immutable N1 release root")
	}
	binaryName := "foigent-integration-control-v0.1.0-candidate.1-" + runtime.GOOS + "-" + runtime.GOARCH
	pinBody := readContractFixture(t, "unirouter.integration-control.n1-pin.v1.json")
	releaseBody, err := os.ReadFile(filepath.Join(root, "releases", "v0.1.0-candidate.1", "foigent.integration-control.release.v1.json"))
	require.NoError(t, err)
	verifier, err := NewPinnedNeutralVerifier(context.Background(), pinBody, releaseBody, filepath.Join(root, "dist", binaryName))
	require.NoError(t, err)
	require.Equal(t, ContentSHA256(pinBody), verifier.Identity().PinSHA256)
	require.Equal(t, ContentSHA256(releaseBody), verifier.Identity().ReleaseManifestSHA256)

	fixture := func(name string) []byte {
		body, readErr := os.ReadFile(filepath.Join(root, "fixtures", "v1", "valid", name))
		require.NoError(t, readErr)
		return body
	}
	inputs := NeutralPreauthorizationInputs{
		ManifestBody: fixture("manifest.json"), UniRouterAckBody: fixture("unirouter-ack.json"), XInsightAckBody: fixture("xinsight-ack.json"),
		AuthorizationBody: fixture("authorization.json"), AuthorizationSignature: fixture("authorization-signature.json"), AuthorizationTrustSet: fixture("authorization-trust-set.json"),
	}
	report, err := verifier.VerifyWhole(context.Background(), inputs)
	require.NoError(t, err)
	require.True(t, report.Valid)
	require.Equal(t, NeutralCapabilityBoundary, report.CapabilityBoundary)

	tampered := inputs
	tampered.AuthorizationSignature = bytes.Replace(tampered.AuthorizationSignature, []byte(`"value":"`), []byte(`"value":"A`), 1)
	_, err = verifier.VerifyWhole(context.Background(), tampered)
	require.Error(t, err)

	wrongPin := append([]byte(nil), pinBody...)
	wrongPin = bytes.Replace(wrongPin, []byte(verifier.Identity().BinarySHA256), []byte("sha256:"+strings.Repeat("0", 64)), 1)
	_, err = NewPinnedNeutralVerifier(context.Background(), wrongPin, releaseBody, filepath.Join(root, "dist", binaryName))
	require.Error(t, err)
}

func TestVerifierReleaseAndComponentReceiptRejectAuthorityDrift(t *testing.T) {
	verifierRelease := VerifierRelease{
		SchemaVersion: VerifierReleaseSchemaVersion, VerifierID: ComponentVerifierID, ProducerContractID: ProducerContractID,
		ProducerContractSHA: "sha256:" + strings.Repeat("1", 64), SourceCommit: strings.Repeat("a", 40), SourceTree: strings.Repeat("b", 40),
		BinarySHA256: "sha256:" + strings.Repeat("2", 64), IntegrationControlPinSHA256: "sha256:" + strings.Repeat("3", 64), BuiltAt: "2026-08-10T11:00:00Z",
	}
	require.NoError(t, ValidateVerifierRelease(verifierRelease))
	verifierRelease.SourceTree = strings.Repeat("B", 40)
	require.Error(t, ValidateVerifierRelease(verifierRelease))

	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x11}, ed25519.SeedSize))
	publicKeys := map[string]ed25519.PublicKey{"source-evidence-test": privateKey.Public().(ed25519.PublicKey)}
	trustPin, _, err := TrustPin(publicKeys)
	require.NoError(t, err)
	receipt := ComponentVerificationReceipt{
		SchemaVersion: VerificationReceiptSchemaVersion, Product: "unirouter", CampaignID: "campaign", Status: ComponentStatusAccepted,
		CampaignManifestSHA256: "sha256:" + strings.Repeat("4", 64), ComponentContract: ContractReference{ID: ProducerContractID, SHA256: "sha256:" + strings.Repeat("5", 64)},
		InstalledRelease:      ArtifactReference{SchemaVersion: InstalledReleaseSchemaVersion, SHA256: "sha256:" + strings.Repeat("6", 64)},
		ComponentScopeReceipt: ArtifactReference{SchemaVersion: SourceScopeReceiptSchemaVersion, SHA256: "sha256:" + strings.Repeat("7", 64)},
		VerifierRelease:       ContractReference{ID: ComponentVerifierID, SHA256: "sha256:" + strings.Repeat("8", 64)},
		RequestSetSHA256:      "sha256:" + strings.Repeat("9", 64), SourceBindingSHA256: "sha256:" + strings.Repeat("a", 64),
		SourceEvidenceSigning:       ProductSigningIdentity{Purpose: SignaturePurpose, Domain: SignatureDomain, SigningKeyRef: "source-evidence-test", SignerPublicKeyID: signerPublicKeyID(publicKeys["source-evidence-test"]), TrustSetSHA256: trustPin},
		IntegrationControlPinSHA256: "sha256:" + strings.Repeat("b", 64), IntegrationControlReleaseSHA256: "sha256:" + strings.Repeat("c", 64), IntegrationControlBinarySHA256: "sha256:" + strings.Repeat("d", 64),
		Checks: append([]string(nil), requiredComponentVerificationChecks...), VerifiedAt: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano), Effects: zeroReceiptEffects(),
	}
	require.NoError(t, ValidateComponentVerificationReceipt(receipt))
	receipt.SourceEvidenceSigning.Domain = "unirouter.source-evidence.other.v1"
	require.Error(t, ValidateComponentVerificationReceipt(receipt))
}

func TestProductCompatibleConformanceFixturesCloseNeutralAndPackageChain(t *testing.T) {
	root := strings.TrimSpace(os.Getenv("UNIROUTE_INTEGRATION_CONTROL_N1_ROOT"))
	if root == "" {
		t.Skip("set UNIROUTE_INTEGRATION_CONTROL_N1_ROOT to the immutable N1 release root")
	}
	contractRoot := filepath.Join(repositoryRoot(t), "contracts", "source-evidence", "v1")
	fixtureRoot := filepath.Join(contractRoot, "fixtures", "conformance")
	validFixture := func(name string) []byte {
		body, err := os.ReadFile(filepath.Join(fixtureRoot, "valid", name))
		require.NoError(t, err)
		return body
	}
	invalidFixture := func(name string) []byte {
		body, err := os.ReadFile(filepath.Join(fixtureRoot, "invalid", name))
		require.NoError(t, err)
		return body
	}
	pinBody, err := os.ReadFile(filepath.Join(contractRoot, "unirouter.integration-control.n1-pin.v1.json"))
	require.NoError(t, err)
	releaseBody, err := os.ReadFile(filepath.Join(root, "releases", "v0.1.0-candidate.1", "foigent.integration-control.release.v1.json"))
	require.NoError(t, err)
	binaryName := "foigent-integration-control-v0.1.0-candidate.1-" + runtime.GOOS + "-" + runtime.GOARCH
	verifier, err := NewPinnedNeutralVerifier(context.Background(), pinBody, releaseBody, filepath.Join(root, "dist", binaryName))
	require.NoError(t, err)
	verifierRelease, err := DecodeVerifierRelease(validFixture("verifier-release.json"))
	require.NoError(t, err)
	productInputs := ProductManifestInputs{
		ManifestBody: validFixture("manifest.json"), RequestSetBody: validFixture("request-set.json"), SourceScopeBody: validFixture("source-scope.json"),
		SourceTrustSetBody: validFixture("source-evidence-trust-set.json"), ComponentContractBody: readContractFixture(t, "unirouter.source-evidence.component-package.v1.json"),
		InstalledReleaseBody: validFixture("installed-release.json"), ScopeReceiptBody: validFixture("scope-receipt.json"), VerifierReleaseBody: validFixture("verifier-release.json"),
		VerifierBinarySHA256: verifierRelease.BinarySHA256,
	}
	preparedRelease, err := DecodeInstalledReleaseIdentity(productInputs.InstalledReleaseBody)
	require.NoError(t, err)
	preparedScope, err := DecodeSourceScopeReceipt(productInputs.ScopeReceiptBody)
	require.NoError(t, err)
	preparedSource, err := DecodeCanonicalSourceScope(productInputs.SourceScopeBody)
	require.NoError(t, err)
	prepared := PreparedComponentInputs{SourceScope: preparedSource, InstalledRelease: preparedRelease, ScopeReceipt: preparedScope}
	built, err := verifier.BuildProductAcknowledgement(context.Background(), productInputs, prepared, time.Date(2030, 1, 1, 0, 1, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Equal(t, validFixture("component-verification-receipt.json"), built.ReceiptBody)
	require.Equal(t, validFixture("unirouter-ack.json"), built.AckBody)
	drifted := prepared
	drifted.InstalledRelease.ReleaseSequence++
	_, err = verifier.BuildProductAcknowledgement(context.Background(), productInputs, drifted, time.Date(2030, 1, 1, 0, 1, 0, 0, time.UTC))
	require.ErrorContains(t, err, "current read-only product preparation")
	acknowledgement, err := verifier.VerifyProductAcknowledgement(context.Background(), productInputs, validFixture("component-verification-receipt.json"), validFixture("unirouter-ack.json"))
	require.NoError(t, err)
	require.Equal(t, SourceRuntimeObservation, acknowledgement.Verification.SourceScope.RuntimeObservation)
	preauthorization := NeutralPreauthorizationInputs{
		ManifestBody: productInputs.ManifestBody, UniRouterAckBody: validFixture("unirouter-ack.json"), XInsightAckBody: validFixture("xinsight-ack.json"),
		AuthorizationBody: validFixture("authorization.json"), AuthorizationSignature: validFixture("authorization-signature.json"), AuthorizationTrustSet: validFixture("authorization-trust-set.json"),
	}
	whole, err := verifier.VerifyWhole(context.Background(), preauthorization)
	require.NoError(t, err)
	binding, err := ValidateCaptureAuthorization(whole, preauthorization, acknowledgement.Verification, time.Date(2030, 1, 1, 0, 4, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Equal(t, whole.CampaignManifestSHA256, binding.AuthorizationManifestSHA256)

	publicKeys, trustSHA, err := ParseTrustSet(productInputs.SourceTrustSetBody)
	require.NoError(t, err)
	require.Equal(t, whole.SourceEvidenceTrustSetSHA256, trustSHA)
	packageVerification, err := Verify(bytes.NewReader(validFixture("source-evidence-package.tar.gz")), publicKeys)
	require.NoError(t, err)
	require.Equal(t, whole.CampaignManifestSHA256, packageVerification.Manifest.AuthorizationManifestSHA256)
	require.Equal(t, whole.AuthorizationSHA256, packageVerification.Manifest.AuthorizationStatementSHA256)
	require.Equal(t, whole.AuthorizationSignatureSHA256, packageVerification.Manifest.AuthorizationSignatureSHA256)
	require.Equal(t, "complete", packageVerification.Manifest.ClaimState)
	require.Equal(t, "partial", packageVerification.Manifest.NestedExecutionEvidenceV2)

	var expected struct {
		SchemaVersion                  string `json:"schema_version"`
		ProducerContractID             string `json:"producer_contract_id"`
		ProducerContractSHA256         string `json:"producer_contract_sha256"`
		SignatureDomain                string `json:"signature_domain"`
		IntegrationControlPinSHA256    string `json:"integration_control_pin_sha256"`
		CampaignManifestSHA256         string `json:"campaign_manifest_sha256"`
		UniRouterAcknowledgementSHA256 string `json:"unirouter_acknowledgement_sha256"`
		XInsightAcknowledgementSHA256  string `json:"xinsight_acknowledgement_sha256"`
		AuthorizationSHA256            string `json:"authorization_sha256"`
		AuthorizationSignatureSHA256   string `json:"authorization_signature_sha256"`
		AuthorizationTrustSetSHA256    string `json:"authorization_trust_set_sha256"`
		SourceEvidenceTrustSetSHA256   string `json:"source_evidence_trust_set_sha256"`
		PackageSHA256                  string `json:"package_sha256"`
		PackageBytes                   int64  `json:"package_bytes"`
		ClaimScope                     string `json:"claim_scope"`
		PackageScopeState              string `json:"package_scope_state"`
		NestedExecutionEvidenceV2      string `json:"nested_execution_evidence_v2"`
		RuntimeCloseMarkerAvailable    bool   `json:"runtime_close_marker_available"`
		CapabilityBoundary             string `json:"capability_boundary"`
	}
	require.NoError(t, decodeStrict(validFixture("expected-verification.json"), &expected))
	require.Equal(t, "unirouter.source-evidence.conformance-projection.v1", expected.SchemaVersion)
	require.Equal(t, ProducerContractID, expected.ProducerContractID)
	require.Equal(t, ContentSHA256(productInputs.ComponentContractBody), expected.ProducerContractSHA256)
	require.Equal(t, SignatureDomain, expected.SignatureDomain)
	require.Equal(t, verifier.Identity().PinSHA256, expected.IntegrationControlPinSHA256)
	require.Equal(t, whole.CampaignManifestSHA256, expected.CampaignManifestSHA256)
	require.Equal(t, whole.AcknowledgementSHA256["unirouter"], expected.UniRouterAcknowledgementSHA256)
	require.Equal(t, whole.AcknowledgementSHA256["xinsight"], expected.XInsightAcknowledgementSHA256)
	require.Equal(t, whole.AuthorizationSHA256, expected.AuthorizationSHA256)
	require.Equal(t, whole.AuthorizationSignatureSHA256, expected.AuthorizationSignatureSHA256)
	require.Equal(t, whole.AuthorizationTrustSetSHA256, expected.AuthorizationTrustSetSHA256)
	require.Equal(t, whole.SourceEvidenceTrustSetSHA256, expected.SourceEvidenceTrustSetSHA256)
	require.Equal(t, packageVerification.PackageSHA256, expected.PackageSHA256)
	require.Equal(t, packageVerification.PackageBytes, expected.PackageBytes)
	require.Equal(t, packageVerification.Manifest.ClaimScope, expected.ClaimScope)
	require.Equal(t, packageVerification.Manifest.ClaimState, expected.PackageScopeState)
	require.Equal(t, packageVerification.Manifest.NestedExecutionEvidenceV2, expected.NestedExecutionEvidenceV2)
	require.False(t, expected.RuntimeCloseMarkerAvailable)
	require.Equal(t, NeutralCapabilityBoundary, expected.CapabilityBoundary)

	wrongManifest := productInputs
	wrongManifest.ManifestBody = invalidFixture("manifest.wrong-source-binding.json")
	_, err = verifier.VerifyProductManifest(context.Background(), wrongManifest)
	require.Error(t, err)
	_, err = verifier.VerifyProductAcknowledgement(context.Background(), productInputs, validFixture("component-verification-receipt.json"), invalidFixture("unirouter-ack.wrong-contract.json"))
	require.Error(t, err)
	wrongAuthorization := preauthorization
	wrongAuthorization.AuthorizationSignature = invalidFixture("authorization-signature.wrong-purpose.json")
	_, err = verifier.VerifyWhole(context.Background(), wrongAuthorization)
	require.Error(t, err)
	wrongAuthorization = preauthorization
	wrongAuthorization.AuthorizationTrustSet = invalidFixture("authorization-trust-set.wrong-key.json")
	_, err = verifier.VerifyWhole(context.Background(), wrongAuthorization)
	require.Error(t, err)
	wrongSourceTrust := productInputs
	wrongSourceTrust.SourceTrustSetBody = invalidFixture("source-evidence-trust-set.wrong-key.json")
	_, err = verifier.VerifyProductManifest(context.Background(), wrongSourceTrust)
	require.Error(t, err)
	_, err = Verify(bytes.NewReader(invalidFixture("source-evidence-package.tampered.tar.gz")), publicKeys)
	require.Error(t, err)
	_, err = Verify(bytes.NewReader(invalidFixture("source-evidence-package.trailing-bytes.tar.gz")), publicKeys)
	require.ErrorContains(t, err, "trailing bytes")
	for name, errorMatch := range map[string]string{
		"source-evidence-package.member-tampered.tar.gz":    "signed identity",
		"source-evidence-package.member-missing.tar.gz":     "artifact count",
		"source-evidence-package.member-extra.tar.gz":       "artifact",
		"source-evidence-package.member-duplicate.tar.gz":   "duplicate entries",
		"source-evidence-package.member-path-unsafe.tar.gz": "unsafe entry",
	} {
		_, err = Verify(bytes.NewReader(invalidFixture(name)), publicKeys)
		require.ErrorContains(t, err, errorMatch, name)
	}
}

func TestConformanceIndexBindsEveryPortableFixture(t *testing.T) {
	root := filepath.Join(repositoryRoot(t), "contracts", "source-evidence", "v1", "fixtures", "conformance")
	body, err := os.ReadFile(filepath.Join(root, "index.json"))
	require.NoError(t, err)
	var index struct {
		SchemaVersion  string `json:"schema_version"`
		ContractID     string `json:"producer_contract_id"`
		ContractSHA256 string `json:"producer_contract_sha256"`
		Fixtures       []struct {
			Path     string `json:"path"`
			SHA256   string `json:"sha256"`
			Expected string `json:"expected"`
		} `json:"fixtures"`
	}
	require.NoError(t, decodeStrict(body, &index))
	require.Equal(t, "unirouter.source-evidence.conformance-index.v1", index.SchemaVersion)
	require.Equal(t, ProducerContractID, index.ContractID)
	contractBody, err := os.ReadFile(filepath.Join(repositoryRoot(t), "contracts", "source-evidence", "v1", "unirouter.source-evidence.component-package.v1.json"))
	require.NoError(t, err)
	require.Equal(t, ContentSHA256(contractBody), index.ContractSHA256)
	require.NotEmpty(t, index.Fixtures)
	last := ""
	for _, fixture := range index.Fixtures {
		require.Greater(t, fixture.Path, last)
		require.Contains(t, []string{"accepted", "rejected"}, fixture.Expected)
		fixtureBody, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(fixture.Path)))
		require.NoError(t, readErr)
		require.Equal(t, ContentSHA256(fixtureBody), fixture.SHA256)
		last = fixture.Path
	}
	for _, prohibited := range []string{"private-key", "private_key", "seed"} {
		require.NotContains(t, strings.ToLower(string(body)), prohibited)
	}
}
