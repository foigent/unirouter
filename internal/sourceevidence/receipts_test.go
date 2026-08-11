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
	pin, err := DecodeIntegrationControlN1Pin(pinBody)
	require.NoError(t, err)
	releaseBody, err := os.ReadFile(filepath.Join(root, "releases", "v0.1.0-candidate.1", "foigent.integration-control.release.v1.json"))
	require.NoError(t, err)
	binaryName := "foigent-integration-control-v0.1.0-candidate.1-" + runtime.GOOS + "-" + runtime.GOARCH
	verifier, err := NewPinnedNeutralVerifier(context.Background(), pinBody, releaseBody, filepath.Join(root, "dist", binaryName))
	require.NoError(t, err)
	runtimeArtifact, ok := selectedN1Artifact(pin, runtime.GOOS, runtime.GOARCH)
	require.True(t, ok)
	require.Equal(t, runtimeArtifact.SHA256, verifier.Identity().BinarySHA256)
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
	require.Equal(t, runtimeArtifact.SHA256, built.Receipt.IntegrationControlBinarySHA256)
	require.Equal(t, ContentSHA256(built.ReceiptBody), built.Ack.ProductVerificationReceipt.SHA256)
	acknowledgement, err := verifier.VerifyProductAcknowledgement(context.Background(), productInputs, built.ReceiptBody, built.AckBody)
	require.NoError(t, err)
	require.Equal(t, SourceRuntimeObservation, acknowledgement.Verification.SourceScope.RuntimeObservation)

	otherBinarySHA := ""
	for _, artifact := range pin.Artifacts {
		if artifact.SHA256 != runtimeArtifact.SHA256 {
			otherBinarySHA = artifact.SHA256
			break
		}
	}
	require.NotEmpty(t, otherBinarySHA)
	wrongBinaryReceipt := built.Receipt
	wrongBinaryReceipt.IntegrationControlBinarySHA256 = otherBinarySHA
	wrongBinaryReceiptBody, err := CanonicalJSON(wrongBinaryReceipt)
	require.NoError(t, err)
	_, err = verifier.VerifyProductAcknowledgement(context.Background(), productInputs, wrongBinaryReceiptBody, built.AckBody)
	require.ErrorContains(t, err, "does not match exact product evidence")
	wrongReceiptAck := built.Ack
	wrongReceiptAck.ProductVerificationReceipt.SHA256 = otherBinarySHA
	wrongReceiptAckBody, err := CanonicalJSON(wrongReceiptAck)
	require.NoError(t, err)
	_, err = verifier.VerifyProductAcknowledgement(context.Background(), productInputs, built.ReceiptBody, wrongReceiptAckBody)
	require.ErrorContains(t, err, "does not match exact product evidence")

	canonicalReceiptBody := validFixture("component-verification-receipt.json")
	canonicalReceipt, err := DecodeComponentVerificationReceipt(canonicalReceiptBody)
	require.NoError(t, err)
	canonicalAckBody := validFixture("unirouter-ack.json")
	var canonicalAck ComponentAcknowledgement
	require.NoError(t, decodeStrict(canonicalAckBody, &canonicalAck))
	require.Equal(t, ContentSHA256(canonicalReceiptBody), canonicalAck.ProductVerificationReceipt.SHA256)
	canonicalBinaryIsPinned := false
	for _, artifact := range pin.Artifacts {
		if artifact.SHA256 == canonicalReceipt.IntegrationControlBinarySHA256 {
			canonicalBinaryIsPinned = true
			break
		}
	}
	require.True(t, canonicalBinaryIsPinned)
	drifted := prepared
	drifted.InstalledRelease.ReleaseSequence++
	_, err = verifier.BuildProductAcknowledgement(context.Background(), productInputs, drifted, time.Date(2030, 1, 1, 0, 1, 0, 0, time.UTC))
	require.ErrorContains(t, err, "current read-only product preparation")
	preauthorization := NeutralPreauthorizationInputs{
		ManifestBody: productInputs.ManifestBody, UniRouterAckBody: canonicalAckBody, XInsightAckBody: validFixture("xinsight-ack.json"),
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
	require.Equal(t, "sha256:b54f1cc5d5523cdfd1b4cccbb44183b22d59d1f830bbb2991a9333e33783e774", packageVerification.ManifestSHA256)

	canonicalProductAcknowledgement := ProductAcknowledgementArtifacts{
		Verification: acknowledgement.Verification,
		Receipt:      canonicalReceipt,
		ReceiptBody:  canonicalReceiptBody,
		Ack:          canonicalAck,
		AckBody:      canonicalAckBody,
	}
	require.NoError(t, sealProductAcknowledgementArtifacts(&canonicalProductAcknowledgement))
	projectionInputs := ConformanceProjectionInputs{
		ComponentContractBody:  productInputs.ComponentContractBody,
		ProductAcknowledgement: canonicalProductAcknowledgement,
		NeutralInputs:          preauthorization,
		NeutralVerification:    whole,
		PackageVerification:    packageVerification,
	}
	projection, projectionBody, err := BuildConformanceProjection(projectionInputs)
	require.NoError(t, err)
	require.Equal(t, validFixture("expected-verification.json"), projectionBody)
	require.Equal(t, ConformanceProjectionSchemaVersion, projection.SchemaVersion)
	require.Equal(t, ProducerContractID, projection.ComponentContract.ID)
	require.Equal(t, ContentSHA256(productInputs.ComponentContractBody), projection.ComponentContract.SHA256)
	require.Equal(t, verifier.Identity().PinSHA256, projection.ProductAuthority.IntegrationControlPinSHA256)
	require.Equal(t, whole.CampaignManifestSHA256, projection.NeutralChain.CampaignManifestSHA256)
	require.Equal(t, whole.AcknowledgementSHA256["unirouter"], projection.NeutralChain.UniRouterAcknowledgementSHA256)
	require.Equal(t, whole.AcknowledgementSHA256["xinsight"], projection.NeutralChain.XInsightAcknowledgementSHA256)
	require.Equal(t, whole.SourceEvidenceTrustSetSHA256, projection.SourceAuthority.Signer.TrustSetSHA256)
	require.Equal(t, packageVerification.ManifestSHA256, projection.Package.Manifest.SHA256)
	require.Equal(t, packageVerification.Manifest.CreatedAt, projection.Package.Manifest.CreatedAt)
	require.False(t, projection.Package.Claim.RuntimeCloseMarkerAvailable)
	require.Equal(t, NeutralCapabilityBoundary, projection.CapabilityBoundary)
	decodedProjection, err := DecodeConformanceProjection(projectionBody)
	require.NoError(t, err)
	require.Equal(t, projection, decodedProjection)
	verifiedProjection, err := VerifyConformanceProjection(projectionBody, projectionInputs)
	require.NoError(t, err)
	require.Equal(t, projection, verifiedProjection)
	t.Run("verification outputs reject caller mutation", func(t *testing.T) {
		mutated := projectionInputs
		mutated.NeutralVerification.AuthorizationSHA256 = "sha256:" + strings.Repeat("0", 64)
		_, _, mutationErr := BuildConformanceProjection(mutated)
		require.ErrorContains(t, mutationErr, "not an unmodified pinned-verifier output")

		mutated = projectionInputs
		mutated.NeutralVerification.AcknowledgementSHA256 = map[string]string{
			"unirouter": projectionInputs.NeutralVerification.AcknowledgementSHA256["unirouter"],
			"xinsight":  "sha256:" + strings.Repeat("0", 64),
		}
		_, _, mutationErr = BuildConformanceProjection(mutated)
		require.ErrorContains(t, mutationErr, "not an unmodified pinned-verifier output")

		mutated = projectionInputs
		mutated.PackageVerification.PackageSHA256 = "sha256:" + strings.Repeat("0", 64)
		_, _, mutationErr = BuildConformanceProjection(mutated)
		require.ErrorContains(t, mutationErr, "not an unmodified package-verifier output")

		mutated = projectionInputs
		mutated.PackageVerification.PackageBytes++
		_, _, mutationErr = BuildConformanceProjection(mutated)
		require.ErrorContains(t, mutationErr, "not an unmodified package-verifier output")

		mutated = projectionInputs
		mutated.ProductAcknowledgement.Receipt.SourceBindingSHA256 = "sha256:" + strings.Repeat("0", 64)
		_, _, mutationErr = BuildConformanceProjection(mutated)
		require.ErrorContains(t, mutationErr, "not an unmodified verifier output")
	})
	_, err = DecodeConformanceProjection(append(append([]byte(nil), projectionBody...), '\n'))
	require.ErrorContains(t, err, "not canonical")
	for _, name := range []string{
		"conformance-projection.authority-drift.json",
		"conformance-projection.neutral-drift.json",
		"conformance-projection.package-manifest-drift.json",
		"conformance-projection.signer-drift.json",
	} {
		driftedBody := invalidFixture(name)
		_, err = DecodeConformanceProjection(driftedBody)
		require.NoError(t, err, name)
		_, err = VerifyConformanceProjection(driftedBody, projectionInputs)
		require.ErrorContains(t, err, "does not match exact verified product lifecycle inputs", name)
	}
	_, err = DecodeConformanceProjection(invalidFixture("conformance-projection.false-complete.json"))
	require.ErrorContains(t, err, "package claim is invalid")

	_, err = ValidateCaptureAuthorization(whole, preauthorization, acknowledgement.Verification, time.Date(2030, 1, 1, 0, 2, 0, 0, time.UTC))
	require.NoError(t, err, "authorization not_before is inclusive")
	_, err = ValidateCaptureAuthorization(whole, preauthorization, acknowledgement.Verification, time.Date(2030, 1, 1, 1, 0, 0, 0, time.UTC))
	require.ErrorContains(t, err, "outside the verified neutral authorization window", "authorization not_after is exclusive")

	wrongManifest := productInputs
	wrongManifest.ManifestBody = invalidFixture("manifest.wrong-source-binding.json")
	_, err = verifier.VerifyProductManifest(context.Background(), wrongManifest)
	require.Error(t, err)
	_, err = verifier.VerifyAcknowledgement(context.Background(), productInputs.ManifestBody, invalidFixture("unirouter-ack.wrong-contract.json"))
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
		Projection     struct {
			SchemaVersion string `json:"schema_version"`
			SchemaSHA256  string `json:"schema_sha256"`
		} `json:"projection_contract"`
		FixtureAuthority string `json:"fixture_authority"`
		Fixtures         []struct {
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
	projectionSchemaBody, err := os.ReadFile(filepath.Join(repositoryRoot(t), "contracts", "source-evidence", "v1", "unirouter.source-evidence.conformance-projection.v1.schema.json"))
	require.NoError(t, err)
	require.Equal(t, ConformanceProjectionSchemaVersion, index.Projection.SchemaVersion)
	require.Equal(t, ContentSHA256(projectionSchemaBody), index.Projection.SchemaSHA256)
	require.Equal(t, ConformanceFixtureAuthority, index.FixtureAuthority)
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
