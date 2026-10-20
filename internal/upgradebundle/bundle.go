package upgradebundle

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"time"
)

const (
	ManifestSchemaVersion            = "unirouter.upgrade-manifest.v1"
	NonExpiringManifestSchemaVersion = "unirouter.upgrade-manifest.v2"
	NeverExpires                     = "never"
	SignatureSchemaVersion           = "unirouter.detached-signature.v1"
	Product                          = "unirouter"
	SignatureAlgorithm               = "ed25519"
	ProvenanceArtifactPath           = "payload/provenance/build-provenance.json"

	manifestPath  = "manifest.json"
	signaturePath = "manifest.sig"

	maxManifestBytes  = 128 * 1024
	maxSignatureBytes = 16 * 1024
	maxArchiveEntries = 66
	defaultMaxBytes   = int64(8 * 1024 * 1024 * 1024)
)

type Artifact struct {
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	MediaType string `json:"media_type,omitempty"`
}

type Manifest struct {
	SchemaVersion      string     `json:"schema_version"`
	Product            string     `json:"product"`
	KeyID              string     `json:"key_id"`
	SignatureAlgorithm string     `json:"signature_algorithm"`
	ReleaseSequence    int64      `json:"release_sequence"`
	Version            string     `json:"version"`
	Commit             string     `json:"commit"`
	CreatedAt          string     `json:"created_at"`
	ExpiresAt          string     `json:"expires_at"`
	TargetOS           string     `json:"target_os"`
	TargetArch         string     `json:"target_arch"`
	MinCurrentSequence int64      `json:"min_current_sequence"`
	MaxCurrentSequence int64      `json:"max_current_sequence"`
	MinCurrentSchema   string     `json:"min_current_schema"`
	TargetSchema       string     `json:"target_schema"`
	ProvenanceSHA256   string     `json:"provenance_sha256,omitempty"`
	Artifacts          []Artifact `json:"artifacts"`
}

type Signature struct {
	SchemaVersion string `json:"schema_version"`
	KeyID         string `json:"key_id"`
	Algorithm     string `json:"algorithm"`
	Value         string `json:"value"`
}

type Source struct {
	BundlePath string
	FilePath   string
	MediaType  string
}

type ManifestOptions struct {
	KeyID              string
	ReleaseSequence    int64
	Version            string
	Commit             string
	CreatedAt          time.Time
	ExpiresAt          time.Time
	NoExpiry           bool
	TargetOS           string
	TargetArch         string
	MinCurrentSequence int64
	MaxCurrentSequence int64
	MinCurrentSchema   string
	TargetSchema       string
	ProvenanceSHA256   string
}

type VerifyOptions struct {
	PublicKeys      map[string]ed25519.PublicKey
	Now             time.Time
	TargetOS        string
	TargetArch      string
	CurrentSequence int64
	CurrentSchema   string
	MaxPayloadBytes int64
}

type Verification struct {
	Manifest     Manifest `json:"manifest"`
	BundleSHA256 string   `json:"bundle_sha256"`
	PayloadBytes int64    `json:"payload_bytes"`
}

func CreateManifest(options ManifestOptions, sources []Source) (Manifest, error) {
	if options.NoExpiry && !options.ExpiresAt.IsZero() {
		return Manifest{}, fmt.Errorf("no-expiry conflicts with expires_at")
	}
	if !options.NoExpiry && options.ExpiresAt.IsZero() {
		return Manifest{}, fmt.Errorf("expires_at or explicit no-expiry is required")
	}
	schemaVersion := ManifestSchemaVersion
	expiresAt := options.ExpiresAt.UTC().Format(time.RFC3339)
	if options.NoExpiry {
		schemaVersion, expiresAt = NonExpiringManifestSchemaVersion, NeverExpires
	}
	artifacts := make([]Artifact, 0, len(sources))
	seen := make(map[string]bool, len(sources))
	for _, source := range sources {
		bundlePath, err := normalizePayloadPath(source.BundlePath)
		if err != nil {
			return Manifest{}, err
		}
		if seen[bundlePath] {
			return Manifest{}, fmt.Errorf("duplicate artifact path %s", bundlePath)
		}
		seen[bundlePath] = true
		info, err := os.Stat(source.FilePath)
		if err != nil {
			return Manifest{}, fmt.Errorf("stat artifact %s: %w", source.FilePath, err)
		}
		if !info.Mode().IsRegular() {
			return Manifest{}, fmt.Errorf("artifact source %s is not a regular file", source.FilePath)
		}
		digest, size, err := hashFile(source.FilePath)
		if err != nil {
			return Manifest{}, err
		}
		artifacts = append(artifacts, Artifact{Path: bundlePath, Size: size, SHA256: digest, MediaType: strings.TrimSpace(source.MediaType)})
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	manifest := Manifest{
		SchemaVersion:      schemaVersion,
		Product:            Product,
		KeyID:              strings.TrimSpace(options.KeyID),
		SignatureAlgorithm: SignatureAlgorithm,
		ReleaseSequence:    options.ReleaseSequence,
		Version:            strings.TrimSpace(options.Version),
		Commit:             strings.TrimSpace(options.Commit),
		CreatedAt:          options.CreatedAt.UTC().Format(time.RFC3339),
		ExpiresAt:          expiresAt,
		TargetOS:           strings.TrimSpace(options.TargetOS),
		TargetArch:         strings.TrimSpace(options.TargetArch),
		MinCurrentSequence: options.MinCurrentSequence,
		MaxCurrentSequence: options.MaxCurrentSequence,
		MinCurrentSchema:   strings.TrimSpace(options.MinCurrentSchema),
		TargetSchema:       strings.TrimSpace(options.TargetSchema),
		ProvenanceSHA256:   strings.ToLower(strings.TrimSpace(options.ProvenanceSHA256)),
		Artifacts:          artifacts,
	}
	if err := ValidateManifest(manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func Pack(w io.Writer, manifest Manifest, privateKey ed25519.PrivateKey, sources []Source) error {
	if err := ValidateManifest(manifest); err != nil {
		return err
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		return fmt.Errorf("upgrade signing key must be an Ed25519 private key")
	}
	byPath := make(map[string]Source, len(sources))
	for _, source := range sources {
		bundlePath, err := normalizePayloadPath(source.BundlePath)
		if err != nil {
			return err
		}
		if _, exists := byPath[bundlePath]; exists {
			return fmt.Errorf("duplicate source path %s", bundlePath)
		}
		byPath[bundlePath] = source
	}
	if len(byPath) != len(manifest.Artifacts) {
		return fmt.Errorf("source count does not match signed artifact count")
	}
	manifestBytes, err := canonicalManifest(manifest)
	if err != nil {
		return err
	}
	signature := Signature{
		SchemaVersion: SignatureSchemaVersion,
		KeyID:         manifest.KeyID,
		Algorithm:     SignatureAlgorithm,
		Value:         base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, manifestBytes)),
	}
	signatureBytes, err := json.Marshal(signature)
	if err != nil {
		return err
	}
	createdAt, _ := time.Parse(time.RFC3339, manifest.CreatedAt)
	gz := gzip.NewWriter(w)
	gz.Name = ""
	gz.Comment = ""
	gz.ModTime = createdAt
	gz.OS = 255
	tw := tar.NewWriter(gz)
	closeWithError := func(cause error) error {
		_ = tw.Close()
		_ = gz.Close()
		return cause
	}
	if err := writeTarBytes(tw, manifestPath, manifestBytes, createdAt); err != nil {
		return closeWithError(err)
	}
	if err := writeTarBytes(tw, signaturePath, signatureBytes, createdAt); err != nil {
		return closeWithError(err)
	}
	for _, artifact := range manifest.Artifacts {
		source, ok := byPath[artifact.Path]
		if !ok {
			return closeWithError(fmt.Errorf("missing source for %s", artifact.Path))
		}
		file, err := os.Open(source.FilePath)
		if err != nil {
			return closeWithError(fmt.Errorf("open artifact %s: %w", source.FilePath, err))
		}
		header := &tar.Header{Name: artifact.Path, Mode: 0o644, Size: artifact.Size, ModTime: createdAt, Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}
		if err := tw.WriteHeader(header); err != nil {
			file.Close()
			return closeWithError(err)
		}
		hash := sha256.New()
		written, copyErr := io.Copy(io.MultiWriter(tw, hash), file)
		closeErr := file.Close()
		if copyErr != nil {
			return closeWithError(copyErr)
		}
		if closeErr != nil {
			return closeWithError(closeErr)
		}
		if written != artifact.Size || hex.EncodeToString(hash.Sum(nil)) != artifact.SHA256 {
			return closeWithError(fmt.Errorf("artifact %s changed while packing", artifact.Path))
		}
	}
	if err := tw.Close(); err != nil {
		_ = gz.Close()
		return err
	}
	return gz.Close()
}

func Verify(r io.Reader, options VerifyOptions) (Verification, error) {
	if options.Now.IsZero() {
		options.Now = time.Now()
	}
	if options.MaxPayloadBytes <= 0 {
		options.MaxPayloadBytes = defaultMaxBytes
	}
	packageHash := sha256.New()
	buffered := bufio.NewReader(io.TeeReader(r, packageHash))
	gz, err := gzip.NewReader(buffered)
	if err != nil {
		return Verification{}, fmt.Errorf("open upgrade gzip: %w", err)
	}
	gz.Multistream(false)
	tr := tar.NewReader(gz)
	manifestHeader, err := tr.Next()
	if err != nil {
		return Verification{}, fmt.Errorf("read manifest entry: %w", err)
	}
	if err := validateMetadataHeader(manifestHeader, manifestPath, maxManifestBytes); err != nil {
		return Verification{}, err
	}
	manifestBytes, err := readExactEntry(tr, manifestHeader.Size, maxManifestBytes)
	if err != nil {
		return Verification{}, err
	}
	manifest, err := decodeManifest(manifestBytes)
	if err != nil {
		return Verification{}, err
	}
	signatureHeader, err := tr.Next()
	if err != nil {
		return Verification{}, fmt.Errorf("read signature entry: %w", err)
	}
	if err := validateMetadataHeader(signatureHeader, signaturePath, maxSignatureBytes); err != nil {
		return Verification{}, err
	}
	signatureBytes, err := readExactEntry(tr, signatureHeader.Size, maxSignatureBytes)
	if err != nil {
		return Verification{}, err
	}
	signature, err := decodeSignature(signatureBytes)
	if err != nil {
		return Verification{}, err
	}
	if signature.SchemaVersion != SignatureSchemaVersion || signature.Algorithm != SignatureAlgorithm || signature.KeyID != manifest.KeyID {
		return Verification{}, fmt.Errorf("upgrade signature metadata does not match the manifest")
	}
	publicKey, ok := options.PublicKeys[manifest.KeyID]
	if !ok || len(publicKey) != ed25519.PublicKeySize {
		return Verification{}, fmt.Errorf("upgrade signing key %q is not trusted", manifest.KeyID)
	}
	decodedSignature, err := base64.StdEncoding.DecodeString(signature.Value)
	if err != nil || len(decodedSignature) != ed25519.SignatureSize {
		return Verification{}, fmt.Errorf("upgrade signature is not a valid Ed25519 signature")
	}
	if !ed25519.Verify(publicKey, manifestBytes, decodedSignature) {
		return Verification{}, fmt.Errorf("upgrade manifest signature verification failed")
	}
	// Only trusted metadata may drive platform, expiry, sequence or schema
	// decisions. Structural parsing above is deliberately non-authoritative.
	if err := validatePreflight(manifest, options); err != nil {
		return Verification{}, err
	}

	expected := make(map[string]Artifact, len(manifest.Artifacts))
	for _, artifact := range manifest.Artifacts {
		expected[artifact.Path] = artifact
	}
	seen := map[string]bool{manifestPath: true, signaturePath: true}
	var payloadBytes int64
	entryCount := 2
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Verification{}, fmt.Errorf("read upgrade entry: %w", err)
		}
		entryCount++
		if entryCount > maxArchiveEntries {
			return Verification{}, fmt.Errorf("upgrade archive contains too many entries")
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return Verification{}, fmt.Errorf("upgrade entry %q is not a regular file", header.Name)
		}
		cleanName, err := normalizePayloadPath(header.Name)
		if err != nil || cleanName != header.Name {
			return Verification{}, fmt.Errorf("unsafe upgrade entry path %q", header.Name)
		}
		if seen[header.Name] {
			return Verification{}, fmt.Errorf("duplicate upgrade entry %q", header.Name)
		}
		seen[header.Name] = true
		artifact, ok := expected[header.Name]
		if !ok {
			return Verification{}, fmt.Errorf("undeclared upgrade payload %q", header.Name)
		}
		if header.Size != artifact.Size {
			return Verification{}, fmt.Errorf("upgrade payload %q size does not match manifest", header.Name)
		}
		if header.Size < 0 || payloadBytes > options.MaxPayloadBytes-header.Size {
			return Verification{}, fmt.Errorf("upgrade payload exceeds the configured size bound")
		}
		hash := sha256.New()
		read, err := io.Copy(hash, tr)
		if err != nil {
			return Verification{}, fmt.Errorf("hash upgrade payload %q: %w", header.Name, err)
		}
		if read != artifact.Size || hex.EncodeToString(hash.Sum(nil)) != artifact.SHA256 {
			return Verification{}, fmt.Errorf("upgrade payload %q hash verification failed", header.Name)
		}
		payloadBytes += read
	}
	for artifactPath := range expected {
		if !seen[artifactPath] {
			return Verification{}, fmt.Errorf("declared upgrade payload %q is missing", artifactPath)
		}
	}
	if _, err := io.Copy(io.Discard, gz); err != nil {
		return Verification{}, fmt.Errorf("finish upgrade gzip verification: %w", err)
	}
	if err := gz.Close(); err != nil {
		return Verification{}, fmt.Errorf("close upgrade gzip: %w", err)
	}
	if _, err := buffered.Peek(1); err != io.EOF {
		if err == nil {
			return Verification{}, fmt.Errorf("upgrade bundle contains trailing data or another gzip stream")
		}
		return Verification{}, fmt.Errorf("inspect upgrade bundle trailer: %w", err)
	}
	return Verification{Manifest: manifest, BundleSHA256: hex.EncodeToString(packageHash.Sum(nil)), PayloadBytes: payloadBytes}, nil
}

func ValidateManifest(manifest Manifest) error {
	if (manifest.SchemaVersion != ManifestSchemaVersion && manifest.SchemaVersion != NonExpiringManifestSchemaVersion) || manifest.Product != Product {
		return fmt.Errorf("unsupported upgrade manifest schema or product")
	}
	if !validKeyID(manifest.KeyID) || manifest.SignatureAlgorithm != SignatureAlgorithm {
		return fmt.Errorf("invalid upgrade signing metadata")
	}
	if manifest.ReleaseSequence <= 0 || manifest.MinCurrentSequence < 0 || manifest.MaxCurrentSequence < manifest.MinCurrentSequence || manifest.ReleaseSequence <= manifest.MaxCurrentSequence {
		return fmt.Errorf("invalid upgrade release sequence window")
	}
	if !normalizedBounded(manifest.Version, 128) || manifest.Version == "local" || manifest.Version == "unknown" ||
		!validSourceCommit(manifest.Commit) ||
		!validPlatformIdentity(manifest.TargetOS) || !validPlatformIdentity(manifest.TargetArch) {
		return fmt.Errorf("upgrade version, commit, OS and architecture are required")
	}
	createdAt, err := time.Parse(time.RFC3339, manifest.CreatedAt)
	if err != nil {
		return fmt.Errorf("upgrade created_at must be RFC3339")
	}
	if manifest.SchemaVersion == NonExpiringManifestSchemaVersion {
		if manifest.ExpiresAt != NeverExpires {
			return fmt.Errorf("upgrade v2 expires_at must be exactly never")
		}
	} else {
		expiresAt, err := time.Parse(time.RFC3339, manifest.ExpiresAt)
		if err != nil || !expiresAt.After(createdAt) {
			return fmt.Errorf("upgrade expires_at must be RFC3339 and after created_at")
		}
	}
	if !validSchemaVersion(manifest.MinCurrentSchema) || !validSchemaVersion(manifest.TargetSchema) || manifest.TargetSchema < manifest.MinCurrentSchema {
		return fmt.Errorf("invalid upgrade schema window")
	}
	if manifest.ProvenanceSHA256 != "" && !validSHA256(manifest.ProvenanceSHA256) {
		return fmt.Errorf("invalid provenance_sha256")
	}
	if len(manifest.Artifacts) == 0 || len(manifest.Artifacts) > maxArchiveEntries-2 {
		return fmt.Errorf("upgrade manifest must declare a bounded non-empty artifact set")
	}
	seen := make(map[string]bool, len(manifest.Artifacts))
	previous := ""
	provenanceBound := manifest.ProvenanceSHA256 == ""
	for _, artifact := range manifest.Artifacts {
		clean, err := normalizePayloadPath(artifact.Path)
		if err != nil || clean != artifact.Path || seen[artifact.Path] {
			return fmt.Errorf("invalid or duplicate artifact path %q", artifact.Path)
		}
		if previous != "" && artifact.Path <= previous {
			return fmt.Errorf("upgrade artifacts must be sorted by path")
		}
		previous = artifact.Path
		seen[artifact.Path] = true
		if artifact.Size < 0 || !validSHA256(artifact.SHA256) {
			return fmt.Errorf("invalid artifact metadata for %q", artifact.Path)
		}
		if artifact.MediaType != "" && !normalizedBounded(artifact.MediaType, 128) {
			return fmt.Errorf("invalid artifact media type for %q", artifact.Path)
		}
		if artifact.Path == ProvenanceArtifactPath {
			if manifest.ProvenanceSHA256 == "" || artifact.SHA256 != manifest.ProvenanceSHA256 {
				return fmt.Errorf("build provenance artifact must match provenance_sha256")
			}
			provenanceBound = true
		}
	}
	if !provenanceBound {
		return fmt.Errorf("provenance_sha256 requires %s", ProvenanceArtifactPath)
	}
	return nil
}

func validatePreflight(manifest Manifest, options VerifyOptions) error {
	if err := ValidateManifest(manifest); err != nil {
		return err
	}
	now := options.Now.UTC()
	createdAt, _ := time.Parse(time.RFC3339, manifest.CreatedAt)
	if createdAt.After(now.Add(5 * time.Minute)) {
		return fmt.Errorf("upgrade manifest creation time is in the future")
	}
	if manifest.SchemaVersion == ManifestSchemaVersion {
		expiresAt, _ := time.Parse(time.RFC3339, manifest.ExpiresAt)
		if !expiresAt.After(now) {
			return fmt.Errorf("upgrade manifest metadata has expired")
		}
	}
	if options.TargetOS != manifest.TargetOS || options.TargetArch != manifest.TargetArch {
		return fmt.Errorf("upgrade targets %s/%s, current platform is %s/%s", manifest.TargetOS, manifest.TargetArch, options.TargetOS, options.TargetArch)
	}
	if options.CurrentSequence < manifest.MinCurrentSequence || options.CurrentSequence > manifest.MaxCurrentSequence {
		return fmt.Errorf("current release sequence %d is outside signed window %d-%d", options.CurrentSequence, manifest.MinCurrentSequence, manifest.MaxCurrentSequence)
	}
	if manifest.ReleaseSequence <= options.CurrentSequence {
		return fmt.Errorf("upgrade release sequence %d does not advance current sequence %d", manifest.ReleaseSequence, options.CurrentSequence)
	}
	if options.CurrentSchema < manifest.MinCurrentSchema || options.CurrentSchema > manifest.TargetSchema {
		return fmt.Errorf("current schema %q is incompatible with signed range %q-%q", options.CurrentSchema, manifest.MinCurrentSchema, manifest.TargetSchema)
	}
	return nil
}

func canonicalManifest(manifest Manifest) ([]byte, error) {
	if err := ValidateManifest(manifest); err != nil {
		return nil, err
	}
	return json.Marshal(manifest)
}

func decodeManifest(raw []byte) (Manifest, error) {
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("parse upgrade manifest: %w", err)
	}
	if err := requireEOF(decoder); err != nil {
		return Manifest{}, fmt.Errorf("parse upgrade manifest: %w", err)
	}
	canonical, err := json.Marshal(manifest)
	if err != nil {
		return Manifest{}, fmt.Errorf("canonicalize upgrade manifest: %w", err)
	}
	if !bytes.Equal(raw, canonical) {
		return Manifest{}, fmt.Errorf("upgrade manifest is not canonical JSON")
	}
	return manifest, nil
}

func decodeSignature(raw []byte) (Signature, error) {
	var signature Signature
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&signature); err != nil {
		return Signature{}, fmt.Errorf("parse upgrade signature: %w", err)
	}
	if err := requireEOF(decoder); err != nil {
		return Signature{}, fmt.Errorf("parse upgrade signature: %w", err)
	}
	canonical, err := json.Marshal(signature)
	if err != nil || !bytes.Equal(raw, canonical) {
		return Signature{}, fmt.Errorf("upgrade signature is not canonical JSON")
	}
	return signature, nil
}

func normalizePayloadPath(value string) (string, error) {
	if value == "" || len(value) > 240 || strings.Contains(value, "\\") || strings.ContainsRune(value, '\x00') || strings.HasPrefix(value, "/") {
		return "", fmt.Errorf("unsafe upgrade payload path %q", value)
	}
	clean := path.Clean(value)
	if clean != value || clean == "payload" || !strings.HasPrefix(clean, "payload/") || strings.Contains(clean, "../") {
		return "", fmt.Errorf("upgrade artifact must use a normalized payload/ path: %q", value)
	}
	return clean, nil
}

func validateMetadataHeader(header *tar.Header, expected string, maxBytes int64) error {
	if header.Name != expected || (header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA) || header.Size < 0 || header.Size > maxBytes {
		return fmt.Errorf("invalid upgrade metadata entry %q", header.Name)
	}
	return nil
}

func readExactEntry(r io.Reader, size, maxBytes int64) ([]byte, error) {
	if size < 0 || size > maxBytes {
		return nil, fmt.Errorf("upgrade metadata exceeds size bound")
	}
	data, err := io.ReadAll(io.LimitReader(r, size+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != size {
		return nil, fmt.Errorf("upgrade metadata size mismatch")
	}
	return data, nil
}

func writeTarBytes(w *tar.Writer, name string, data []byte, modTime time.Time) error {
	header := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), ModTime: modTime, Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}
	if err := w.WriteHeader(header); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
}

func hashFile(filePath string) (string, int64, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", 0, fmt.Errorf("open artifact %s: %w", filePath, err)
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, fmt.Errorf("hash artifact %s: %w", filePath, err)
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validSourceCommit(value string) bool {
	if (len(value) != 40 && len(value) != 64) || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func normalizedBounded(value string, maxBytes int) bool {
	if value == "" || len(value) > maxBytes || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func validPlatformIdentity(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

func validSchemaVersion(value string) bool {
	if len(value) < 6 || len(value) > 128 {
		return false
	}
	for index, r := range value {
		if index < 6 {
			if r < '0' || r > '9' {
				return false
			}
			continue
		}
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func validKeyID(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func requireEOF(decoder *json.Decoder) error {
	var extra interface{}
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}
	return fmt.Errorf("unexpected trailing JSON value")
}
