package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uniroute/unirouter/internal/signing"
	"github.com/uniroute/unirouter/internal/snapshotpublish"
	"github.com/uniroute/unirouter/internal/sourceevidence"
)

const maxInputBytes = 32 << 20

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: uniroute-source-evidence <validate-request-set|prepare|acknowledge|capture|verify>")
	}
	switch args[0] {
	case "validate-request-set":
		return runValidateRequestSet(args[1:], stdout)
	case "prepare":
		return runPrepare(ctx, args[1:], stdout)
	case "acknowledge":
		return runAcknowledge(ctx, args[1:], stdout)
	case "capture":
		return runCapture(ctx, args[1:], stdout)
	case "verify":
		return runVerify(args[1:], stdout)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runPrepare(ctx context.Context, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("prepare", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	requestSetPath := flags.String("request-set", "", "canonical frozen request-set JSON")
	protocolPinPath := flags.String("protocol-pin", "", "reviewed UniRouter protocol pin JSON")
	dsnEnv := flags.String("dsn-env", "UNIROUTE_SOURCE_EVIDENCE_DSN", "environment variable containing the read-only PostgreSQL DSN")
	releaseOutput := flags.String("release-out", "", "new private installed-release receipt")
	sourceScopeOutput := flags.String("source-scope-out", "", "new private canonical Source-scope projection")
	scopeReceiptOutput := flags.String("scope-receipt-out", "", "new private Source-scope receipt")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("prepare does not accept positional arguments")
	}
	for name, value := range map[string]string{
		"--request-set": *requestSetPath, "--protocol-pin": *protocolPinPath,
		"--release-out": *releaseOutput, "--source-scope-out": *sourceScopeOutput, "--scope-receipt-out": *scopeReceiptOutput,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	if *releaseOutput == *sourceScopeOutput || *releaseOutput == *scopeReceiptOutput || *sourceScopeOutput == *scopeReceiptOutput {
		return errors.New("prepare output paths must be distinct")
	}
	requestSetBody, err := readBoundedFile(*requestSetPath, sourceevidence.MaxContractBytes)
	if err != nil {
		return err
	}
	requestSet, err := sourceevidence.DecodeCanonicalRequestSet(requestSetBody)
	if err != nil {
		return err
	}
	protocolPin, err := readBoundedFile(*protocolPinPath, 1<<20)
	if err != nil {
		return err
	}
	pool, err := openReadOnlyPool(ctx, *dsnEnv)
	if err != nil {
		return err
	}
	defer pool.Close()
	prepared, err := sourceevidence.PrepareComponentInputs(ctx, pool, requestSet, protocolPin)
	if err != nil {
		return err
	}
	releaseBody, err := sourceevidence.CanonicalJSON(prepared.InstalledRelease)
	if err != nil {
		return err
	}
	sourceScopeBody, err := sourceevidence.CanonicalJSON(prepared.SourceScope)
	if err != nil {
		return err
	}
	scopeReceiptBody, err := sourceevidence.CanonicalJSON(prepared.ScopeReceipt)
	if err != nil {
		return err
	}
	if err := publishPrivateOutputs([]privateOutput{
		{path: *releaseOutput, body: releaseBody},
		{path: *sourceScopeOutput, body: sourceScopeBody},
		{path: *scopeReceiptOutput, body: scopeReceiptBody},
	}); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout,
		"prepared=true\nrelease_schema=%s\nrelease_sha256=%s\nsource_scope_schema=%s\nsource_binding_sha256=%s\nscope_receipt_schema=%s\nscope_receipt_sha256=%s\ndatabase_mutation=none\nadditional_gateway_calls=0\nprovider_calls=0\nfeed_calls=0\nupstream_calls=0\n",
		prepared.InstalledRelease.SchemaVersion, sourceevidence.ContentSHA256(releaseBody), prepared.SourceScope.SchemaVersion, sourceevidence.ContentSHA256(sourceScopeBody), prepared.ScopeReceipt.SchemaVersion, sourceevidence.ContentSHA256(scopeReceiptBody),
	)
	return err
}

func runAcknowledge(ctx context.Context, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("acknowledge", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	paths := addProductManifestFlags(flags)
	n1PinPath := flags.String("integration-control-pin", "", "pinned product-local N1 identity")
	n1ReleasePath := flags.String("integration-control-release", "", "exact N1 release manifest")
	n1BinaryPath := flags.String("integration-control-bin", "", "local executable matching the N1 pin")
	protocolPinPath := flags.String("protocol-pin", "", "reviewed UniRouter protocol pin JSON")
	dsnEnv := flags.String("dsn-env", "UNIROUTE_SOURCE_EVIDENCE_DSN", "environment variable containing the read-only PostgreSQL DSN")
	verifiedAtText := flags.String("verified-at", "", "canonical RFC3339 product verification time")
	receiptOutput := flags.String("verification-receipt-out", "", "new private product verification receipt")
	ackOutput := flags.String("ack-out", "", "new private neutral component acknowledgement")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("acknowledge does not accept positional arguments")
	}
	for name, value := range map[string]string{
		"--integration-control-pin": *n1PinPath, "--integration-control-release": *n1ReleasePath, "--integration-control-bin": *n1BinaryPath,
		"--protocol-pin": *protocolPinPath,
		"--verified-at":  *verifiedAtText, "--verification-receipt-out": *receiptOutput, "--ack-out": *ackOutput,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	if *receiptOutput == *ackOutput {
		return errors.New("acknowledgement outputs must be distinct")
	}
	verifiedAt, err := time.Parse(time.RFC3339Nano, *verifiedAtText)
	if err != nil || verifiedAt.Location() != time.UTC || verifiedAt.Format(time.RFC3339Nano) != *verifiedAtText {
		return errors.New("--verified-at must be canonical UTC RFC3339")
	}
	inputs, err := readProductManifestInputs(paths)
	if err != nil {
		return err
	}
	verifier, err := loadPinnedNeutralVerifier(ctx, *n1PinPath, *n1ReleasePath, *n1BinaryPath)
	if err != nil {
		return err
	}
	// Reject an invalid neutral manifest before consulting the product database.
	if _, err := verifier.VerifyProductManifest(ctx, inputs); err != nil {
		return err
	}
	requestSet, err := sourceevidence.DecodeCanonicalRequestSet(inputs.RequestSetBody)
	if err != nil {
		return err
	}
	protocolPin, err := readBoundedFile(*protocolPinPath, 1<<20)
	if err != nil {
		return err
	}
	pool, err := openReadOnlyPool(ctx, *dsnEnv)
	if err != nil {
		return err
	}
	prepared, err := sourceevidence.PrepareComponentInputs(ctx, pool, requestSet, protocolPin)
	pool.Close()
	if err != nil {
		return err
	}
	artifacts, err := verifier.BuildProductAcknowledgement(ctx, inputs, prepared, verifiedAt)
	if err != nil {
		return err
	}
	if err := publishPrivateOutputs([]privateOutput{{path: *receiptOutput, body: artifacts.ReceiptBody}, {path: *ackOutput, body: artifacts.AckBody}}); err != nil {
		return err
	}
	identity := verifier.Identity()
	_, err = fmt.Fprintf(stdout,
		"acknowledged=true\ncampaign_manifest_sha256=%s\nverification_receipt_sha256=%s\nacknowledgement_sha256=%s\nintegration_control_pin_sha256=%s\nintegration_control_release_sha256=%s\nintegration_control_binary_sha256=%s\ncapability_boundary=offline_files_only\n",
		artifacts.Receipt.CampaignManifestSHA256, sourceevidence.ContentSHA256(artifacts.ReceiptBody), sourceevidence.ContentSHA256(artifacts.AckBody), identity.PinSHA256, identity.ReleaseManifestSHA256, identity.BinarySHA256,
	)
	return err
}

func runValidateRequestSet(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("validate-request-set", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	requestSetPath := flags.String("request-set", "", "frozen unirouter.source-evidence.request-set.v1 JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*requestSetPath) == "" {
		return errors.New("--request-set is required")
	}
	body, err := readBoundedFile(*requestSetPath, sourceevidence.MaxContractBytes)
	if err != nil {
		return err
	}
	requestSet, err := sourceevidence.DecodeRequestSet(body)
	if err != nil {
		return err
	}
	digest, err := sourceevidence.SemanticSHA256(requestSet)
	if err != nil {
		return err
	}
	sourceScope := sourceevidence.SourceScopeForRequestSet(requestSet)
	sourceBindingSHA, err := sourceevidence.SemanticSHA256(sourceScope)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "valid=true\nschema_version=%s\nsemantic_sha256=%s\nsource_scope_schema=%s\nsource_binding_sha256=%s\nrequests=%d\n", requestSet.SchemaVersion, digest, sourceScope.SchemaVersion, sourceBindingSHA, len(requestSet.Requests))
	return err
}

func runCapture(ctx context.Context, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("capture", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	productPaths := addProductManifestFlags(flags)
	protocolPinPath := flags.String("protocol-pin", "", "reviewed UniRouter protocol pin JSON")
	n1PinPath := flags.String("integration-control-pin", "", "pinned product-local N1 identity")
	n1ReleasePath := flags.String("integration-control-release", "", "exact N1 release manifest")
	n1BinaryPath := flags.String("integration-control-bin", "", "local executable matching the N1 pin")
	uniRouterAckPath := flags.String("unirouter-ack", "", "exact accepted UniRouter component acknowledgement")
	xInsightAckPath := flags.String("xinsight-ack", "", "exact accepted X-Insight component acknowledgement")
	verificationReceiptPath := flags.String("verification-receipt", "", "exact UniRouter component verification receipt")
	authorizationStatementPath := flags.String("authorization-statement", "", "frozen foigent.integration-campaign-authorization.v1 bytes")
	authorizationSignaturePath := flags.String("authorization-signature", "", "detached neutral authorization signature bytes")
	authorizationTrustSetPath := flags.String("authorization-trust-set", "", "canonical neutral authorization trust set")
	dsnEnv := flags.String("dsn-env", "UNIROUTE_SOURCE_EVIDENCE_DSN", "environment variable containing the read-only PostgreSQL DSN")
	signingKeyRef := flags.String("signing-key-ref", "", "source_evidence operational signing key reference")
	privateKeyPath := flags.String("private-key", "", "source_evidence Ed25519 private-key file")
	createdAtText := flags.String("created-at", "", "RFC3339 package creation time")
	outputPath := flags.String("out", "", "new private signed component package on supported Linux/ext4 storage")
	if err := flags.Parse(args); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"--protocol-pin": *protocolPinPath, "--integration-control-pin": *n1PinPath,
		"--integration-control-release": *n1ReleasePath, "--integration-control-bin": *n1BinaryPath,
		"--unirouter-ack": *uniRouterAckPath, "--xinsight-ack": *xInsightAckPath, "--verification-receipt": *verificationReceiptPath,
		"--signing-key-ref":         *signingKeyRef,
		"--authorization-statement": *authorizationStatementPath,
		"--authorization-signature": *authorizationSignaturePath,
		"--authorization-trust-set": *authorizationTrustSetPath, "--private-key": *privateKeyPath,
		"--created-at": *createdAtText, "--out": *outputPath,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	createdAt, err := time.Parse(time.RFC3339Nano, *createdAtText)
	if err != nil || createdAt.Location() != time.UTC || createdAt.Format(time.RFC3339Nano) != *createdAtText {
		return errors.New("--created-at must be canonical UTC RFC3339")
	}
	productInputs, err := readProductManifestInputs(productPaths)
	if err != nil {
		return err
	}
	verifier, err := loadPinnedNeutralVerifier(ctx, *n1PinPath, *n1ReleasePath, *n1BinaryPath)
	if err != nil {
		return err
	}
	verificationReceipt, err := readBoundedFile(*verificationReceiptPath, sourceevidence.MaxContractBytes)
	if err != nil {
		return err
	}
	uniRouterAck, err := readBoundedFile(*uniRouterAckPath, sourceevidence.MaxContractBytes)
	if err != nil {
		return err
	}
	productAcknowledgement, err := verifier.VerifyProductAcknowledgement(ctx, productInputs, verificationReceipt, uniRouterAck)
	if err != nil {
		return err
	}
	xInsightAck, err := readBoundedFile(*xInsightAckPath, sourceevidence.MaxContractBytes)
	if err != nil {
		return err
	}
	authorizationStatement, err := readBoundedFile(*authorizationStatementPath, sourceevidence.MaxContractBytes)
	if err != nil {
		return err
	}
	authorizationSignature, err := readBoundedFile(*authorizationSignaturePath, 1<<20)
	if err != nil {
		return err
	}
	authorizationTrustSet, err := readBoundedFile(*authorizationTrustSetPath, sourceevidence.MaxContractBytes)
	if err != nil {
		return err
	}
	preauthorization := sourceevidence.NeutralPreauthorizationInputs{
		ManifestBody: productInputs.ManifestBody, UniRouterAckBody: uniRouterAck, XInsightAckBody: xInsightAck,
		AuthorizationBody: authorizationStatement, AuthorizationSignature: authorizationSignature, AuthorizationTrustSet: authorizationTrustSet,
	}
	wholeVerification, err := verifier.VerifyWhole(ctx, preauthorization)
	if err != nil {
		return err
	}
	authorizationBinding, err := sourceevidence.ValidateCaptureAuthorization(wholeVerification, preauthorization, productAcknowledgement.Verification, createdAt)
	if err != nil {
		return err
	}
	protocolPin, err := readBoundedFile(*protocolPinPath, 1<<20)
	if err != nil {
		return err
	}
	protocol, err := sourceevidence.ParseProtocolPin(protocolPin)
	if err != nil {
		return err
	}
	if *signingKeyRef != productAcknowledgement.Receipt.SourceEvidenceSigning.SigningKeyRef {
		return errors.New("capture signing key reference does not match the verified neutral manifest")
	}
	privateKeyBody, err := readBoundedFile(*privateKeyPath, 16<<10)
	if err != nil {
		return err
	}
	privateKey, err := signing.ParsePrivateKeyBytes(privateKeyBody)
	clear(privateKeyBody)
	if err != nil {
		return err
	}
	defer clear(privateKey)
	pinnedPublicKey, ok := productAcknowledgement.Verification.SourcePublicKeys[*signingKeyRef]
	if !ok || !bytes.Equal(pinnedPublicKey, privateKey.Public().(ed25519.PublicKey)) {
		return errors.New("capture private key does not match the verified Source-evidence signing authority")
	}
	pool, err := openReadOnlyPool(ctx, *dsnEnv)
	if err != nil {
		return err
	}
	defer pool.Close()
	generation, err := sourceevidence.Generate(ctx, pool, productAcknowledgement.Verification.RequestSet, sourceevidence.GenerateOptions{
		ProtocolPin: protocolPin,
	})
	if err != nil {
		return err
	}
	if err := sourceevidence.ValidateGenerationAgainstPrepared(generation, productAcknowledgement.Verification.RequestSet, protocol, productAcknowledgement.Verification.InstalledRelease, productAcknowledgement.Verification.ScopeReceipt); err != nil {
		return err
	}
	var packageBuffer bytes.Buffer
	manifest, err := sourceevidence.Pack(&packageBuffer, sourceevidence.PackageOptions{
		SigningKeyRef: *signingKeyRef, PublicKeys: productAcknowledgement.Verification.SourcePublicKeys, PrivateKey: privateKey,
		CreatedAt: createdAt, RequestSet: productAcknowledgement.Verification.RequestSet, Authorization: authorizationBinding, Generation: generation,
	})
	if err != nil {
		return err
	}
	verification, err := sourceevidence.Verify(bytes.NewReader(packageBuffer.Bytes()), productAcknowledgement.Verification.SourcePublicKeys)
	if err != nil {
		return fmt.Errorf("independent in-memory package verification failed: %w", err)
	}
	stage, err := snapshotpublish.Prepare(*outputPath)
	if err != nil {
		return fmt.Errorf("prepare strict private output: %w", err)
	}
	defer func() { _ = stage.Close() }()
	written, err := stage.Write(packageBuffer.Bytes())
	if err != nil || written != packageBuffer.Len() {
		return errors.New("write strict private output failed")
	}
	if err := stage.Sync(); err != nil {
		return errors.New("sync strict private output failed")
	}
	identity, err := stage.Stat()
	if err != nil || !identity.Regular || identity.Nlink != 0 || identity.Size != int64(packageBuffer.Len()) || identity.Mode&0o777 != 0o600 {
		return errors.New("strict private output identity check failed")
	}
	if err := stage.Commit(); err != nil {
		return fmt.Errorf("publish strict private output: %w", err)
	}
	_, err = fmt.Fprintf(stdout,
		"captured=true\npackage_sha256=%s\npackage_bytes=%d\nmanifest_schema=%s\nclaim_scope=%s\nnested_execution_evidence_v2=%s\nneutral_authorization_state=VERIFIED_N1\nauthorization_manifest_sha256=%s\nauthorization_statement_sha256=%s\nauthorization_signature_sha256=%s\nrequests=%d\npublication_backend=%s\n",
		verification.PackageSHA256, verification.PackageBytes, manifest.SchemaVersion,
		manifest.ClaimScope, manifest.NestedExecutionEvidenceV2,
		manifest.AuthorizationManifestSHA256, manifest.AuthorizationStatementSHA256, manifest.AuthorizationSignatureSHA256,
		len(productAcknowledgement.Verification.RequestSet.Requests), stage.Backend(),
	)
	return err
}

func runVerify(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	packagePath := flags.String("package", "", "signed source evidence component package")
	publicKeysPath := flags.String("public-keys", "", "source_evidence public trust-set JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*packagePath) == "" || strings.TrimSpace(*publicKeysPath) == "" {
		return errors.New("--package and --public-keys are required")
	}
	publicKeysBody, err := readBoundedFile(*publicKeysPath, 1<<20)
	if err != nil {
		return err
	}
	publicKeys, _, err := sourceevidence.ParseTrustSet(publicKeysBody)
	if err != nil {
		return err
	}
	file, err := os.Open(*packagePath)
	if err != nil {
		return fmt.Errorf("open source evidence package: %w", err)
	}
	verification, verifyErr := sourceevidence.Verify(file, publicKeys)
	closeErr := file.Close()
	if verifyErr != nil {
		return verifyErr
	}
	if closeErr != nil {
		return closeErr
	}
	encoded, err := json.Marshal(struct {
		Verified          bool   `json:"verified"`
		PackageSHA256     string `json:"package_sha256"`
		PackageBytes      int64  `json:"package_bytes"`
		Purpose           string `json:"purpose"`
		SigningKeyRef     string `json:"signing_key_ref"`
		SignerPublicKeyID string `json:"signer_public_key_id"`
		TrustSetSHA256    string `json:"trust_set_sha256"`
		ClaimScope        string `json:"claim_scope"`
		ClaimState        string `json:"claim_state"`
		Compatibility     string `json:"nested_execution_evidence_v2"`
		NeutralState      string `json:"neutral_authorization_state"`
		Requests          int    `json:"requests"`
	}{
		Verified:          true,
		PackageSHA256:     verification.PackageSHA256,
		PackageBytes:      verification.PackageBytes,
		Purpose:           verification.Manifest.Purpose,
		SigningKeyRef:     verification.Manifest.SigningKeyRef,
		SignerPublicKeyID: verification.Manifest.SignerPublicKeyID,
		TrustSetSHA256:    verification.Manifest.TrustSetSHA256,
		ClaimScope:        verification.Manifest.ClaimScope,
		ClaimState:        verification.Manifest.ClaimState,
		Compatibility:     verification.Manifest.NestedExecutionEvidenceV2,
		NeutralState:      "PACKAGE_BINDINGS_PRESENT_NOT_REVERIFIED",
		Requests:          len(verification.RequestSet.Requests),
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "%s\n", encoded)
	return err
}

type productManifestPaths struct {
	manifest          *string
	requestSet        *string
	sourceScope       *string
	sourceTrustSet    *string
	componentContract *string
	installedRelease  *string
	scopeReceipt      *string
	verifierRelease   *string
}

func addProductManifestFlags(flags *flag.FlagSet) productManifestPaths {
	return productManifestPaths{
		manifest:          flags.String("authorization-manifest", "", "canonical foigent.integration-campaign-manifest.v1 bytes"),
		requestSet:        flags.String("request-set", "", "canonical frozen request-set JSON"),
		sourceScope:       flags.String("source-scope", "", "canonical product Source-scope projection"),
		sourceTrustSet:    flags.String("source-evidence-trust-set", "", "canonical product Source-evidence trust set"),
		componentContract: flags.String("component-contract", "", "immutable product component contract index"),
		installedRelease:  flags.String("installed-release", "", "exact product installed-release receipt"),
		scopeReceipt:      flags.String("scope-receipt", "", "exact product Source-scope receipt"),
		verifierRelease:   flags.String("verifier-release", "", "exact product component-verifier release receipt"),
	}
}

func readProductManifestInputs(paths productManifestPaths) (sourceevidence.ProductManifestInputs, error) {
	values := map[string]*string{
		"--authorization-manifest":    paths.manifest,
		"--request-set":               paths.requestSet,
		"--source-scope":              paths.sourceScope,
		"--source-evidence-trust-set": paths.sourceTrustSet,
		"--component-contract":        paths.componentContract,
		"--installed-release":         paths.installedRelease,
		"--scope-receipt":             paths.scopeReceipt,
		"--verifier-release":          paths.verifierRelease,
	}
	for name, value := range values {
		if value == nil || strings.TrimSpace(*value) == "" {
			return sourceevidence.ProductManifestInputs{}, fmt.Errorf("%s is required", name)
		}
	}
	read := func(path *string) ([]byte, error) {
		return readBoundedFile(*path, sourceevidence.MaxContractBytes)
	}
	manifest, err := read(paths.manifest)
	if err != nil {
		return sourceevidence.ProductManifestInputs{}, err
	}
	requestSet, err := read(paths.requestSet)
	if err != nil {
		return sourceevidence.ProductManifestInputs{}, err
	}
	sourceScope, err := read(paths.sourceScope)
	if err != nil {
		return sourceevidence.ProductManifestInputs{}, err
	}
	sourceTrust, err := read(paths.sourceTrustSet)
	if err != nil {
		return sourceevidence.ProductManifestInputs{}, err
	}
	contract, err := read(paths.componentContract)
	if err != nil {
		return sourceevidence.ProductManifestInputs{}, err
	}
	release, err := read(paths.installedRelease)
	if err != nil {
		return sourceevidence.ProductManifestInputs{}, err
	}
	scope, err := read(paths.scopeReceipt)
	if err != nil {
		return sourceevidence.ProductManifestInputs{}, err
	}
	verifier, err := read(paths.verifierRelease)
	if err != nil {
		return sourceevidence.ProductManifestInputs{}, err
	}
	verifierBinarySHA, err := currentExecutableSHA256()
	if err != nil {
		return sourceevidence.ProductManifestInputs{}, err
	}
	return sourceevidence.ProductManifestInputs{
		ManifestBody: manifest, RequestSetBody: requestSet, SourceScopeBody: sourceScope, SourceTrustSetBody: sourceTrust,
		ComponentContractBody: contract, InstalledReleaseBody: release, ScopeReceiptBody: scope, VerifierReleaseBody: verifier,
		VerifierBinarySHA256: verifierBinarySHA,
	}, nil
}

func loadPinnedNeutralVerifier(ctx context.Context, pinPath, releasePath, binaryPath string) (*sourceevidence.PinnedNeutralVerifier, error) {
	pinBody, err := readBoundedFile(pinPath, sourceevidence.MaxContractBytes)
	if err != nil {
		return nil, err
	}
	releaseBody, err := readBoundedFile(releasePath, sourceevidence.MaxContractBytes)
	if err != nil {
		return nil, err
	}
	return sourceevidence.NewPinnedNeutralVerifier(ctx, pinBody, releaseBody, binaryPath)
}

func openReadOnlyPool(ctx context.Context, dsnEnvironment string) (*pgxpool.Pool, error) {
	dsnEnvironment = strings.TrimSpace(dsnEnvironment)
	dsn := strings.TrimSpace(os.Getenv(dsnEnvironment))
	if dsnEnvironment == "" || dsn == "" {
		return nil, fmt.Errorf("%s must contain the read-only PostgreSQL DSN", dsnEnvironment)
	}
	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("parse read-only PostgreSQL DSN failed")
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 0
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, errors.New("open read-only PostgreSQL pool failed")
	}
	return pool, nil
}

func currentExecutableSHA256() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", errors.New("resolve current component verifier executable failed")
	}
	body, err := readBoundedFile(path, 256<<20)
	if err != nil {
		return "", errors.New("read current component verifier executable failed")
	}
	return sourceevidence.ContentSHA256(body), nil
}

type privateOutput struct {
	path string
	body []byte
}

func publishPrivateOutputs(outputs []privateOutput) error {
	if len(outputs) == 0 || len(outputs) > 4 {
		return errors.New("private output set is invalid")
	}
	stages := make([]snapshotpublish.Stage, 0, len(outputs))
	defer func() {
		for _, stage := range stages {
			_ = stage.Close()
		}
	}()
	for _, output := range outputs {
		stage, err := snapshotpublish.Prepare(output.path)
		if err != nil {
			return fmt.Errorf("prepare strict private output: %w", err)
		}
		stages = append(stages, stage)
		written, err := stage.Write(output.body)
		if err != nil || written != len(output.body) {
			return errors.New("write strict private output failed")
		}
		if err := stage.Sync(); err != nil {
			return errors.New("sync strict private output failed")
		}
		identity, err := stage.Stat()
		if err != nil || !identity.Regular || identity.Nlink != 0 || identity.Size != int64(len(output.body)) || identity.Mode&0o777 != 0o600 {
			return errors.New("strict private output identity check failed")
		}
	}
	for _, stage := range stages {
		if err := stage.Commit(); err != nil {
			return fmt.Errorf("publish strict private output: %w", err)
		}
	}
	return nil
}

func readBoundedFile(path string, maximum int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	body, readErr := io.ReadAll(io.LimitReader(file, maximum+1))
	closeErr := file.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(body) == 0 || int64(len(body)) > maximum {
		return nil, fmt.Errorf("%s exceeds bounded input size", path)
	}
	return body, nil
}
