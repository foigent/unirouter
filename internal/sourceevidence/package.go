package sourceevidence

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/signing"
)

const (
	PackageManifestSchemaVersion  = "unirouter.source-evidence.component-manifest.v1"
	PackageSignatureSchemaVersion = "unirouter.source-evidence.detached-signature.v1"
	TrustSetSchemaVersion         = "unirouter.source-evidence.trust-set.v1"
	ProducerContractID            = "unirouter.source-evidence.component-package.v1"
	SignaturePurpose              = "source_evidence"
	SignatureDomain               = "unirouter.source-evidence.component.v1"
	SignatureAlgorithm            = "ed25519"
	requestSetMediaType           = "application/vnd.unirouter.source-evidence.request-set.v1+json"
	sourceScopeMediaType          = "application/vnd.unirouter.source-evidence.source-scope.v1+json"
	sourceFactsMediaType          = "application/vnd.unirouter.poc-source-facts.v1+json"
	compatibilityMediaType        = "application/vnd.unirouter.execution-evidence.bundle.v2+json; completeness=partial"

	manifestPath    = "manifest.json"
	signaturePath   = "manifest.sig"
	requestSetPath  = "payload/request-set.json"
	sourceScopePath = "payload/source-scope.json"
	sourceFactsPath = "payload/source-facts.json"

	maxManifestBytes  = 128 << 10
	maxSignatureBytes = 16 << 10
	MaxPackageBytes   = 32 << 20
	maxExpandedBytes  = 32 << 20
	maxPackageEntries = MaxRequests + 5
)

var signatureDomainPrefix = []byte(SignatureDomain + "\x00" + SignaturePurpose + "\x00")

type PackageArtifact struct {
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	MediaType string `json:"media_type"`
}

type PackageManifest struct {
	SchemaVersion                string            `json:"schema_version"`
	ContractID                   string            `json:"contract_id"`
	Product                      string            `json:"product"`
	Purpose                      string            `json:"purpose"`
	Domain                       string            `json:"domain"`
	SigningKeyRef                string            `json:"signing_key_ref"`
	SignerPublicKeyID            string            `json:"signer_public_key_id"`
	TrustSetSHA256               string            `json:"trust_set_sha256"`
	SignatureAlgorithm           string            `json:"signature_algorithm"`
	CreatedAt                    string            `json:"created_at"`
	ClaimScope                   string            `json:"claim_scope"`
	ClaimState                   string            `json:"claim_state"`
	NestedExecutionEvidenceV2    string            `json:"nested_execution_evidence_v2"`
	RequestSetSHA256             string            `json:"request_set_sha256"`
	AuthorizationManifestSHA256  string            `json:"authorization_manifest_sha256"`
	AuthorizationStatementSHA256 string            `json:"authorization_statement_sha256"`
	AuthorizationSignatureSHA256 string            `json:"authorization_signature_sha256"`
	SourceBindingSHA256          string            `json:"source_binding_sha256"`
	SourceFactsSHA256            string            `json:"source_facts_sha256"`
	CompatibilityProfileID       string            `json:"compatibility_profile_id"`
	CompatibilityProfileSHA256   string            `json:"compatibility_profile_sha256"`
	CompatibilitySchemaSHA256    string            `json:"compatibility_schema_sha256"`
	Artifacts                    []PackageArtifact `json:"artifacts"`
}

type PackageSignature struct {
	SchemaVersion     string `json:"schema_version"`
	Purpose           string `json:"purpose"`
	Domain            string `json:"domain"`
	SigningKeyRef     string `json:"signing_key_ref"`
	SignerPublicKeyID string `json:"signer_public_key_id"`
	TrustSetSHA256    string `json:"trust_set_sha256"`
	Algorithm         string `json:"algorithm"`
	Value             string `json:"value"`
}

type TrustSet struct {
	SchemaVersion string        `json:"schema_version"`
	Purpose       string        `json:"purpose"`
	Domain        string        `json:"domain"`
	Keys          []TrustSetKey `json:"keys"`
}

type TrustSetKey struct {
	SigningKeyRef     string `json:"signing_key_ref"`
	SignerPublicKeyID string `json:"signer_public_key_id"`
	Algorithm         string `json:"algorithm"`
	PublicKey         string `json:"public_key"`
}

func ParseTrustSet(body []byte) (map[string]ed25519.PublicKey, string, error) {
	var trustSet TrustSet
	if err := decodeStrict(body, &trustSet); err != nil {
		return nil, "", fmt.Errorf("decode source_evidence trust set: %w", err)
	}
	if trustSet.SchemaVersion != TrustSetSchemaVersion || trustSet.Purpose != SignaturePurpose || trustSet.Domain != SignatureDomain || len(trustSet.Keys) == 0 || len(trustSet.Keys) > 32 {
		return nil, "", errors.New("source_evidence trust set identity is invalid")
	}
	publicKeys := make(map[string]ed25519.PublicKey, len(trustSet.Keys))
	last := ""
	for index, entry := range trustSet.Keys {
		if !validSourceEvidenceKeyID(entry.SigningKeyRef) || entry.SigningKeyRef <= last || entry.Algorithm != SignatureAlgorithm {
			return nil, "", fmt.Errorf("source_evidence trust set keys[%d] is invalid or not strictly sorted", index)
		}
		publicKey, err := signing.ParsePublicKey(entry.PublicKey)
		if err != nil {
			return nil, "", fmt.Errorf("source_evidence trust set key %q: %w", entry.SigningKeyRef, err)
		}
		canonical, err := signing.EncodePublicKey(publicKey)
		if err != nil || canonical != entry.PublicKey || entry.SignerPublicKeyID != signerPublicKeyID(publicKey) {
			return nil, "", fmt.Errorf("source_evidence trust set key %q has a noncanonical public identity", entry.SigningKeyRef)
		}
		publicKeys[entry.SigningKeyRef] = publicKey
		last = entry.SigningKeyRef
	}
	pin, canonical, err := TrustPin(publicKeys)
	if err != nil {
		return nil, "", err
	}
	canonicalBody, err := CanonicalJSON(canonical)
	if err != nil {
		return nil, "", err
	}
	declaredBody, err := CanonicalJSON(trustSet)
	if err != nil {
		return nil, "", err
	}
	if !bytes.Equal(canonicalBody, declaredBody) {
		return nil, "", errors.New("source_evidence trust set does not match its canonical purpose-specific projection")
	}
	return publicKeys, pin, nil
}

type PackageOptions struct {
	SigningKeyRef string
	PublicKeys    map[string]ed25519.PublicKey
	PrivateKey    ed25519.PrivateKey
	CreatedAt     time.Time
	RequestSet    RequestSet
	Authorization NeutralAuthorizationBinding
	Generation    Generation
}

type PackageVerification struct {
	PackageSHA256        string                `json:"package_sha256"`
	PackageBytes         int64                 `json:"package_bytes"`
	ManifestSHA256       string                `json:"manifest_sha256"`
	Manifest             PackageManifest       `json:"manifest"`
	RequestSet           RequestSet            `json:"request_set"`
	SourceScope          SourceScope           `json:"source_scope"`
	SourceFacts          SourceFacts           `json:"source_facts"`
	CompatibilityMembers []CompatibilityBundle `json:"compatibility_members"`
	manifestBody         []byte
	verificationSeal     [32]byte
}

type packageContent struct {
	path      string
	mediaType string
	body      []byte
}

func TrustPin(publicKeys map[string]ed25519.PublicKey) (string, TrustSet, error) {
	if len(publicKeys) == 0 || len(publicKeys) > 32 {
		return "", TrustSet{}, errors.New("source_evidence public trust set must contain 1..32 keys")
	}
	keyIDs := make([]string, 0, len(publicKeys))
	for keyID := range publicKeys {
		keyIDs = append(keyIDs, keyID)
	}
	sort.Strings(keyIDs)
	trustSet := TrustSet{SchemaVersion: TrustSetSchemaVersion, Purpose: SignaturePurpose, Domain: SignatureDomain, Keys: make([]TrustSetKey, 0, len(keyIDs))}
	for _, keyID := range keyIDs {
		if !validSourceEvidenceKeyID(keyID) || len(publicKeys[keyID]) != ed25519.PublicKeySize {
			return "", TrustSet{}, fmt.Errorf("source_evidence trust set contains invalid key %q", keyID)
		}
		encoded, err := signing.EncodePublicKey(publicKeys[keyID])
		if err != nil {
			return "", TrustSet{}, err
		}
		trustSet.Keys = append(trustSet.Keys, TrustSetKey{
			SigningKeyRef:     keyID,
			SignerPublicKeyID: signerPublicKeyID(publicKeys[keyID]),
			Algorithm:         SignatureAlgorithm,
			PublicKey:         encoded,
		})
	}
	body, err := json.Marshal(trustSet)
	if err != nil {
		return "", TrustSet{}, err
	}
	return hashBytes(body), trustSet, nil
}

func Pack(writer io.Writer, options PackageOptions) (PackageManifest, error) {
	if writer == nil {
		return PackageManifest{}, errors.New("source evidence package writer is required")
	}
	if !validSourceEvidenceKeyID(options.SigningKeyRef) || len(options.PrivateKey) != ed25519.PrivateKeySize {
		return PackageManifest{}, errors.New("source_evidence signing key identity is invalid")
	}
	publicKey, ok := options.PublicKeys[options.SigningKeyRef]
	if !ok || !bytes.Equal(publicKey, options.PrivateKey.Public().(ed25519.PublicKey)) {
		return PackageManifest{}, errors.New("source_evidence private key does not match the pinned public trust set")
	}
	if err := ValidateRequestSet(options.RequestSet); err != nil {
		return PackageManifest{}, err
	}
	if err := ValidateSourceScopeForRequestSet(options.Generation.SourceScope, options.RequestSet); err != nil {
		return PackageManifest{}, err
	}
	if err := ValidateNeutralAuthorizationBinding(options.Authorization, options.Generation.SourceScope); err != nil {
		return PackageManifest{}, err
	}
	if err := ValidateSourceFacts(options.Generation.SourceFacts); err != nil {
		return PackageManifest{}, err
	}
	if len(options.Generation.CompatibilityMembers) != len(options.RequestSet.Requests) {
		return PackageManifest{}, errors.New("source evidence compatibility member count does not match request set")
	}
	if err := validatePackageBindings(options.RequestSet, options.Generation.SourceScope, options.Generation.SourceFacts, options.Generation.CompatibilityMembers); err != nil {
		return PackageManifest{}, err
	}
	createdAt := options.CreatedAt.UTC()
	if createdAt.IsZero() {
		return PackageManifest{}, errors.New("source evidence package created_at is required")
	}
	generatedAt, _ := time.Parse(time.RFC3339Nano, options.Generation.SourceFacts.GeneratedAt)
	if createdAt.Before(generatedAt) {
		return PackageManifest{}, errors.New("source evidence package cannot predate generated Source facts")
	}
	trustPin, _, err := TrustPin(options.PublicKeys)
	if err != nil {
		return PackageManifest{}, err
	}
	contents, requestSetSHA, sourceFactsSHA, err := packageContents(options.RequestSet, options.Generation)
	if err != nil {
		return PackageManifest{}, err
	}
	artifacts := make([]PackageArtifact, 0, len(contents))
	for _, content := range contents {
		artifacts = append(artifacts, PackageArtifact{Path: content.path, Size: int64(len(content.body)), SHA256: hashBytes(content.body), MediaType: content.mediaType})
	}
	manifest := PackageManifest{
		SchemaVersion:                PackageManifestSchemaVersion,
		ContractID:                   ProducerContractID,
		Product:                      "unirouter",
		Purpose:                      SignaturePurpose,
		Domain:                       SignatureDomain,
		SigningKeyRef:                options.SigningKeyRef,
		SignerPublicKeyID:            signerPublicKeyID(publicKey),
		TrustSetSHA256:               trustPin,
		SignatureAlgorithm:           SignatureAlgorithm,
		CreatedAt:                    canonicalTime(createdAt),
		ClaimScope:                   SourceFactsSchemaVersion,
		ClaimState:                   "complete",
		NestedExecutionEvidenceV2:    "partial",
		RequestSetSHA256:             requestSetSHA,
		AuthorizationManifestSHA256:  options.Authorization.AuthorizationManifestSHA256,
		AuthorizationStatementSHA256: options.Authorization.AuthorizationStatementSHA256,
		AuthorizationSignatureSHA256: options.Authorization.AuthorizationSignatureSHA256,
		SourceBindingSHA256:          options.Authorization.SourceBindingSHA256,
		SourceFactsSHA256:            sourceFactsSHA,
		CompatibilityProfileID:       CompatibilityProfileID,
		CompatibilityProfileSHA256:   CompatibilityProfileManifestSHA256,
		CompatibilitySchemaSHA256:    CompatibilityBundleSchemaSHA256,
		Artifacts:                    artifacts,
	}
	if err := ValidatePackageManifest(manifest); err != nil {
		return PackageManifest{}, err
	}
	manifestBody, err := json.Marshal(manifest)
	if err != nil {
		return PackageManifest{}, err
	}
	signed := append(append([]byte(nil), signatureDomainPrefix...), manifestBody...)
	signature := PackageSignature{
		SchemaVersion:     PackageSignatureSchemaVersion,
		Purpose:           SignaturePurpose,
		Domain:            SignatureDomain,
		SigningKeyRef:     options.SigningKeyRef,
		SignerPublicKeyID: signerPublicKeyID(publicKey),
		TrustSetSHA256:    trustPin,
		Algorithm:         SignatureAlgorithm,
		Value:             base64.StdEncoding.EncodeToString(ed25519.Sign(options.PrivateKey, signed)),
	}
	signatureBody, err := json.Marshal(signature)
	if err != nil {
		return PackageManifest{}, err
	}
	entries := []packageContent{
		{path: manifestPath, mediaType: "application/vnd.unirouter.source-evidence.component-manifest.v1+json", body: manifestBody},
		{path: signaturePath, mediaType: "application/vnd.unirouter.source-evidence.detached-signature.v1+json", body: signatureBody},
	}
	entries = append(entries, contents...)
	if err := writeCanonicalContainer(writer, createdAt, entries); err != nil {
		return PackageManifest{}, err
	}
	return manifest, nil
}

func Verify(reader io.Reader, publicKeys map[string]ed25519.PublicKey) (PackageVerification, error) {
	if reader == nil {
		return PackageVerification{}, errors.New("source evidence package reader is required")
	}
	packageBody, err := io.ReadAll(io.LimitReader(reader, MaxPackageBytes+1))
	if err != nil {
		return PackageVerification{}, err
	}
	if len(packageBody) == 0 || len(packageBody) > MaxPackageBytes {
		return PackageVerification{}, errors.New("source evidence package size is invalid")
	}
	trustPin, _, err := TrustPin(publicKeys)
	if err != nil {
		return PackageVerification{}, err
	}
	compressedReader := bytes.NewReader(packageBody)
	gzipReader, err := gzip.NewReader(compressedReader)
	if err != nil {
		return PackageVerification{}, fmt.Errorf("open source evidence package: %w", err)
	}
	gzipReader.Multistream(false)
	tarReader := tar.NewReader(gzipReader)
	entries := make(map[string][]byte)
	entryOrder := []string{}
	var expandedBytes int64
	for len(entryOrder) <= maxPackageEntries {
		header, nextErr := tarReader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			_ = gzipReader.Close()
			return PackageVerification{}, fmt.Errorf("read source evidence package: %w", nextErr)
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			_ = gzipReader.Close()
			return PackageVerification{}, errors.New("source evidence package contains a non-file entry")
		}
		name, normalizeErr := normalizePackagePath(header.Name)
		if normalizeErr != nil || name != header.Name || header.Size < 0 || header.Size > MaxContractBytes || expandedBytes > maxExpandedBytes-header.Size {
			_ = gzipReader.Close()
			return PackageVerification{}, errors.New("source evidence package contains an unsafe entry")
		}
		expandedBytes += header.Size
		if _, duplicate := entries[name]; duplicate {
			_ = gzipReader.Close()
			return PackageVerification{}, errors.New("source evidence package contains duplicate entries")
		}
		body, readErr := io.ReadAll(io.LimitReader(tarReader, header.Size+1))
		if readErr != nil || int64(len(body)) != header.Size {
			_ = gzipReader.Close()
			return PackageVerification{}, errors.New("source evidence package entry size is invalid")
		}
		entries[name] = body
		entryOrder = append(entryOrder, name)
	}
	if len(entryOrder) > maxPackageEntries {
		_ = gzipReader.Close()
		return PackageVerification{}, errors.New("source evidence package has too many entries")
	}
	trailingTarBytes, err := io.ReadAll(io.LimitReader(gzipReader, 1))
	if err != nil {
		_ = gzipReader.Close()
		return PackageVerification{}, fmt.Errorf("finish source evidence gzip stream: %w", err)
	}
	if len(trailingTarBytes) != 0 {
		_ = gzipReader.Close()
		return PackageVerification{}, errors.New("source evidence package contains bytes after the canonical tar end")
	}
	if err := gzipReader.Close(); err != nil {
		return PackageVerification{}, err
	}
	if compressedReader.Len() != 0 {
		return PackageVerification{}, errors.New("source evidence package contains a concatenated gzip member or trailing bytes")
	}
	if len(entryOrder) < 4 || entryOrder[0] != manifestPath || entryOrder[1] != signaturePath {
		return PackageVerification{}, errors.New("source evidence package metadata order is invalid")
	}
	manifest, err := decodeCanonicalManifest(entries[manifestPath])
	if err != nil {
		return PackageVerification{}, err
	}
	signature, err := decodeCanonicalSignature(entries[signaturePath])
	if err != nil {
		return PackageVerification{}, err
	}
	if signature.Purpose != manifest.Purpose || signature.Domain != manifest.Domain || signature.SigningKeyRef != manifest.SigningKeyRef || signature.SignerPublicKeyID != manifest.SignerPublicKeyID || signature.TrustSetSHA256 != manifest.TrustSetSHA256 || signature.Algorithm != manifest.SignatureAlgorithm || manifest.TrustSetSHA256 != trustPin {
		return PackageVerification{}, errors.New("source evidence signature envelope does not match manifest and pinned trust set")
	}
	publicKey, ok := publicKeys[manifest.SigningKeyRef]
	if !ok {
		return PackageVerification{}, errors.New("source evidence signing key is not in the pinned trust set")
	}
	if manifest.SignerPublicKeyID != signerPublicKeyID(publicKey) {
		return PackageVerification{}, errors.New("source evidence signer public-key identity does not match the pinned key")
	}
	signatureBytes, err := base64.StdEncoding.DecodeString(signature.Value)
	if err != nil || len(signatureBytes) != ed25519.SignatureSize {
		return PackageVerification{}, errors.New("source evidence signature encoding is invalid")
	}
	signed := append(append([]byte(nil), signatureDomainPrefix...), entries[manifestPath]...)
	if !ed25519.Verify(publicKey, signed, signatureBytes) {
		return PackageVerification{}, errors.New("source evidence signature verification failed")
	}
	if len(manifest.Artifacts)+2 != len(entries) {
		return PackageVerification{}, errors.New("source evidence package artifact count does not match manifest")
	}
	for index, artifact := range manifest.Artifacts {
		if entryOrder[index+2] != artifact.Path {
			return PackageVerification{}, errors.New("source evidence package artifact order does not match manifest")
		}
		body, ok := entries[artifact.Path]
		if !ok || int64(len(body)) != artifact.Size || hashBytes(body) != artifact.SHA256 {
			return PackageVerification{}, fmt.Errorf("source evidence artifact %q does not match signed identity", artifact.Path)
		}
	}
	requestSet, err := DecodeRequestSet(entries[requestSetPath])
	if err != nil {
		return PackageVerification{}, err
	}
	if len(manifest.Artifacts) != len(requestSet.Requests)+3 {
		return PackageVerification{}, errors.New("source evidence package contains artifacts outside the frozen product-local contract")
	}
	sourceScope, err := DecodeSourceScope(entries[sourceScopePath])
	if err != nil {
		return PackageVerification{}, err
	}
	if err := ValidateSourceScopeForRequestSet(sourceScope, requestSet); err != nil {
		return PackageVerification{}, err
	}
	sourceFacts, err := DecodeSourceFacts(entries[sourceFactsPath])
	if err != nil {
		return PackageVerification{}, err
	}
	requestSetSHA, _ := SemanticSHA256(requestSet)
	sourceBindingSHA, _ := SemanticSHA256(sourceScope)
	sourceFactsSHA, _ := SemanticSHA256(sourceFacts)
	if requestSetSHA != manifest.RequestSetSHA256 || requestSetSHA != sourceFacts.RequestSetSHA256 || sourceBindingSHA != manifest.SourceBindingSHA256 || sourceBindingSHA != sourceFacts.SourceBindingSHA256 || sourceFactsSHA != manifest.SourceFactsSHA256 || requestSet.CampaignID != sourceFacts.CampaignID || requestSet.SourceInstance != sourceFacts.Source.Instance {
		return PackageVerification{}, errors.New("source evidence package request set, Source facts, and manifest bindings conflict")
	}
	members := make([]CompatibilityBundle, 0, len(requestSet.Requests))
	for index, selection := range requestSet.Requests {
		memberPath := compatibilityPath(index, selection)
		member, decodeErr := DecodeCompatibilityBundle(entries[memberPath])
		if decodeErr != nil {
			return PackageVerification{}, decodeErr
		}
		if member.RequestID != selection.RequestID || member.OrgID != selection.OrgID || member.ProjectID == nil || *member.ProjectID != selection.ProjectID || member.SourceInstance != requestSet.SourceInstance {
			return PackageVerification{}, errors.New("source evidence compatibility member identity conflicts with request set")
		}
		members = append(members, member)
	}
	if err := validatePackageBindings(requestSet, sourceScope, sourceFacts, members); err != nil {
		return PackageVerification{}, err
	}
	createdAt, _ := time.Parse(time.RFC3339Nano, manifest.CreatedAt)
	canonicalEntries := make([]packageContent, 0, len(entryOrder))
	for _, name := range entryOrder {
		canonicalEntries = append(canonicalEntries, packageContent{path: name, body: entries[name]})
	}
	var canonicalPackage bytes.Buffer
	if err := writeCanonicalContainer(&canonicalPackage, createdAt, canonicalEntries); err != nil {
		return PackageVerification{}, fmt.Errorf("reconstruct canonical source evidence package: %w", err)
	}
	if !bytes.Equal(canonicalPackage.Bytes(), packageBody) {
		return PackageVerification{}, errors.New("source evidence package is not the single canonical container representation")
	}
	verification := PackageVerification{
		PackageSHA256:        hashBytes(packageBody),
		PackageBytes:         int64(len(packageBody)),
		ManifestSHA256:       hashBytes(entries[manifestPath]),
		Manifest:             manifest,
		RequestSet:           requestSet,
		SourceScope:          sourceScope,
		SourceFacts:          sourceFacts,
		CompatibilityMembers: members,
		manifestBody:         append([]byte(nil), entries[manifestPath]...),
	}
	seal, err := verificationProvenanceSeal(packageVerificationSealDomain, verification)
	if err != nil {
		return PackageVerification{}, err
	}
	verification.verificationSeal = seal
	return verification, nil
}

func packageVerificationVerified(value PackageVerification) bool {
	seal, err := verificationProvenanceSeal(packageVerificationSealDomain, value)
	return err == nil && seal == value.verificationSeal
}

func validatePackageBindings(requestSet RequestSet, sourceScope SourceScope, sourceFacts SourceFacts, members []CompatibilityBundle) error {
	if len(sourceFacts.Requests) != len(requestSet.Requests) || len(members) != len(requestSet.Requests) {
		return errors.New("source evidence request set, Source facts, and compatibility member counts conflict")
	}
	requestSetSHA, err := SemanticSHA256(requestSet)
	if err != nil {
		return err
	}
	if sourceFacts.RequestSetSHA256 != requestSetSHA || sourceFacts.CampaignID != requestSet.CampaignID || sourceFacts.Source.Instance != requestSet.SourceInstance {
		return errors.New("source evidence request set and Source facts binding conflicts")
	}
	sourceBindingSHA, err := SemanticSHA256(sourceScope)
	if err != nil {
		return err
	}
	if sourceFacts.SourceBindingSHA256 != sourceBindingSHA || sourceFacts.Source.Product != sourceScope.Product || sourceFacts.Source.Instance != sourceScope.SourceInstance || sourceFacts.Source.IdentityAuthority != sourceScope.IdentityAuthority || sourceFacts.Source.RuntimeObservation != sourceScope.RuntimeObservation || sourceFacts.Source.FeedSnapshotSchema != sourceScope.FeedSnapshotSchema {
		return errors.New("source evidence Source scope and Source identity conflict")
	}
	for index, selection := range requestSet.Requests {
		facts := sourceFacts.Requests[index]
		if facts.ScenarioID != selection.ScenarioID || facts.RequestID != selection.RequestID || facts.OrgID != selection.OrgID || facts.ProjectID != selection.ProjectID {
			return fmt.Errorf("source evidence Source facts request %d does not match frozen request-set order", index)
		}
		expected, projectErr := projectCompatibilityMember(sourceFacts, facts)
		if projectErr != nil {
			return projectErr
		}
		expectedBody, marshalErr := CanonicalJSON(expected)
		if marshalErr != nil {
			return marshalErr
		}
		actualBody, marshalErr := CanonicalJSON(members[index])
		if marshalErr != nil {
			return marshalErr
		}
		if !bytes.Equal(expectedBody, actualBody) {
			return fmt.Errorf("source evidence compatibility member %d is not the deterministic partial projection of Source facts", index)
		}
	}
	return nil
}

func ValidatePackageManifest(value PackageManifest) error {
	if value.SchemaVersion != PackageManifestSchemaVersion || value.ContractID != ProducerContractID || value.Product != "unirouter" || value.Purpose != SignaturePurpose || value.Domain != SignatureDomain || !validSourceEvidenceKeyID(value.SigningKeyRef) || !signerPublicKeyIDPattern.MatchString(value.SignerPublicKeyID) || !hashRefPattern.MatchString(value.TrustSetSHA256) || value.SignatureAlgorithm != SignatureAlgorithm {
		return errors.New("source evidence component manifest signing identity is invalid")
	}
	if _, err := parseCanonicalTime("source evidence manifest created_at", value.CreatedAt); err != nil {
		return err
	}
	if value.ClaimScope != SourceFactsSchemaVersion || value.ClaimState != "complete" || value.NestedExecutionEvidenceV2 != "partial" || !hashRefPattern.MatchString(value.RequestSetSHA256) || !hashRefPattern.MatchString(value.AuthorizationManifestSHA256) || !hashRefPattern.MatchString(value.AuthorizationStatementSHA256) || !hashRefPattern.MatchString(value.AuthorizationSignatureSHA256) || !hashRefPattern.MatchString(value.SourceBindingSHA256) || !hashRefPattern.MatchString(value.SourceFactsSHA256) || value.CompatibilityProfileID != CompatibilityProfileID || value.CompatibilityProfileSHA256 != CompatibilityProfileManifestSHA256 || value.CompatibilitySchemaSHA256 != CompatibilityBundleSchemaSHA256 {
		return errors.New("source evidence component manifest completeness or contract binding is invalid")
	}
	if len(value.Artifacts) < 4 || len(value.Artifacts) > MaxRequests+3 {
		return errors.New("source evidence component manifest artifact count is invalid")
	}
	last := ""
	seenRequest := false
	seenScope := false
	seenFacts := false
	for index, artifact := range value.Artifacts {
		name, err := normalizePackagePath(artifact.Path)
		expectedMediaType, mediaErr := expectedArtifactMediaType(artifact.Path)
		if err != nil || mediaErr != nil || name != artifact.Path || artifact.Path <= last || artifact.Size <= 0 || artifact.Size > MaxContractBytes || !hashRefPattern.MatchString(artifact.SHA256) || artifact.MediaType != expectedMediaType {
			return fmt.Errorf("source evidence manifest artifacts[%d] is invalid or not sorted", index)
		}
		last = artifact.Path
		seenRequest = seenRequest || artifact.Path == requestSetPath
		seenScope = seenScope || artifact.Path == sourceScopePath
		seenFacts = seenFacts || artifact.Path == sourceFactsPath
	}
	if !seenRequest || !seenScope || !seenFacts {
		return errors.New("source evidence component manifest is missing request set, Source scope, or Source facts")
	}
	return nil
}

func packageContents(requestSet RequestSet, generation Generation) ([]packageContent, string, string, error) {
	requestSetBody, err := CanonicalJSON(requestSet)
	if err != nil {
		return nil, "", "", err
	}
	sourceScopeBody, err := CanonicalJSON(generation.SourceScope)
	if err != nil {
		return nil, "", "", err
	}
	sourceFactsBody, err := CanonicalJSON(generation.SourceFacts)
	if err != nil {
		return nil, "", "", err
	}
	requestSetSHA, err := SemanticSHA256(requestSet)
	if err != nil {
		return nil, "", "", err
	}
	sourceFactsSHA, err := SemanticSHA256(generation.SourceFacts)
	if err != nil {
		return nil, "", "", err
	}
	contents := []packageContent{
		{path: requestSetPath, mediaType: requestSetMediaType, body: requestSetBody},
		{path: sourceScopePath, mediaType: sourceScopeMediaType, body: sourceScopeBody},
		{path: sourceFactsPath, mediaType: sourceFactsMediaType, body: sourceFactsBody},
	}
	for index, member := range generation.CompatibilityMembers {
		if err := ValidateCompatibilityBundle(member); err != nil {
			return nil, "", "", err
		}
		body, marshalErr := CanonicalJSON(member)
		if marshalErr != nil {
			return nil, "", "", marshalErr
		}
		contents = append(contents, packageContent{
			path:      compatibilityPath(index, requestSet.Requests[index]),
			mediaType: compatibilityMediaType,
			body:      body,
		})
	}
	sort.Slice(contents, func(left, right int) bool { return contents[left].path < contents[right].path })
	return contents, requestSetSHA, sourceFactsSHA, nil
}

func expectedArtifactMediaType(name string) (string, error) {
	switch {
	case name == requestSetPath:
		return requestSetMediaType, nil
	case name == sourceScopePath:
		return sourceScopeMediaType, nil
	case name == sourceFactsPath:
		return sourceFactsMediaType, nil
	case strings.HasPrefix(name, "payload/compatibility/") && strings.HasSuffix(name, ".json"):
		return compatibilityMediaType, nil
	default:
		return "", errors.New("source evidence manifest contains an unsupported artifact path")
	}
}

func compatibilityPath(index int, selection RequestSelection) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s", selection.OrgID, selection.RequestID)))
	return fmt.Sprintf("payload/compatibility/%03d-%s.json", index+1, hex.EncodeToString(digest[:8]))
}

func normalizePackagePath(value string) (string, error) {
	if value == manifestPath || value == signaturePath {
		return value, nil
	}
	if value == "" || len(value) > 240 || strings.Contains(value, "\\") || strings.ContainsRune(value, '\x00') || strings.HasPrefix(value, "/") {
		return "", errors.New("source evidence package path is unsafe")
	}
	clean := path.Clean(value)
	if clean != value || clean == "payload" || !strings.HasPrefix(clean, "payload/") || strings.Contains(clean, "../") {
		return "", errors.New("source evidence artifacts must use normalized payload paths")
	}
	return clean, nil
}

func writeCanonicalContainer(writer io.Writer, modTime time.Time, entries []packageContent) error {
	if writer == nil || modTime.IsZero() || len(entries) < 4 || len(entries) > maxPackageEntries {
		return errors.New("canonical source evidence container inputs are invalid")
	}
	gzipWriter := gzip.NewWriter(writer)
	gzipWriter.Name = ""
	gzipWriter.Comment = ""
	gzipWriter.ModTime = modTime.UTC()
	gzipWriter.OS = 255
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		if err := writePackageEntry(tarWriter, entry.path, entry.body, modTime.UTC()); err != nil {
			_ = tarWriter.Close()
			_ = gzipWriter.Close()
			return err
		}
	}
	if err := tarWriter.Close(); err != nil {
		_ = gzipWriter.Close()
		return err
	}
	return gzipWriter.Close()
}

func writePackageEntry(writer *tar.Writer, name string, body []byte, modTime time.Time) error {
	header := &tar.Header{Name: name, Mode: 0o600, Size: int64(len(body)), ModTime: modTime, Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}
	if err := writer.WriteHeader(header); err != nil {
		return err
	}
	_, err := writer.Write(body)
	return err
}

func decodeCanonicalManifest(body []byte) (PackageManifest, error) {
	if len(body) == 0 || len(body) > maxManifestBytes {
		return PackageManifest{}, errors.New("source evidence manifest size is invalid")
	}
	var manifest PackageManifest
	if err := decodeStrict(body, &manifest); err != nil {
		return PackageManifest{}, fmt.Errorf("decode source evidence manifest: %w", err)
	}
	canonical, _ := json.Marshal(manifest)
	if !bytes.Equal(canonical, body) {
		return PackageManifest{}, errors.New("source evidence manifest is not canonical JSON")
	}
	if err := ValidatePackageManifest(manifest); err != nil {
		return PackageManifest{}, err
	}
	return manifest, nil
}

func decodeCanonicalSignature(body []byte) (PackageSignature, error) {
	if len(body) == 0 || len(body) > maxSignatureBytes {
		return PackageSignature{}, errors.New("source evidence signature size is invalid")
	}
	var signature PackageSignature
	if err := decodeStrict(body, &signature); err != nil {
		return PackageSignature{}, fmt.Errorf("decode source evidence signature: %w", err)
	}
	canonical, _ := json.Marshal(signature)
	if !bytes.Equal(canonical, body) {
		return PackageSignature{}, errors.New("source evidence signature is not canonical JSON")
	}
	if signature.SchemaVersion != PackageSignatureSchemaVersion || signature.Purpose != SignaturePurpose || signature.Domain != SignatureDomain || !validSourceEvidenceKeyID(signature.SigningKeyRef) || !signerPublicKeyIDPattern.MatchString(signature.SignerPublicKeyID) || !hashRefPattern.MatchString(signature.TrustSetSHA256) || signature.Algorithm != SignatureAlgorithm || strings.TrimSpace(signature.Value) == "" {
		return PackageSignature{}, errors.New("source evidence signature envelope is invalid")
	}
	return signature, nil
}

func validSourceEvidenceKeyID(value string) bool {
	const prefix = "source-evidence-"
	suffix := strings.TrimPrefix(value, prefix)
	return signing.ValidKeyID(value) && suffix != value && len(suffix) >= 1 && len(suffix) <= 48
}

func signerPublicKeyID(publicKey ed25519.PublicKey) string {
	digest := sha256.Sum256(publicKey)
	return "ed25519:sha256:" + hex.EncodeToString(digest[:])
}
