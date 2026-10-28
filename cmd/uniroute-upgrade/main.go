package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/signing"
	"github.com/uniroute/unirouter/internal/strictjson"
	"github.com/uniroute/unirouter/internal/upgradebundle"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: uniroute-upgrade <pack|verify>")
	}
	switch args[0] {
	case "pack":
		return runPack(args[1:], stdout)
	case "verify":
		return runVerify(args[1:], stdout)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

type repeatedFlag []string

func (values *repeatedFlag) String() string { return strings.Join(*values, ",") }
func (values *repeatedFlag) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func runPack(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("pack", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	keyID := fs.String("key-id", "", "trusted upgrade signing key identifier")
	privateKeyPath := fs.String("private-key", "", "Ed25519 private-key file")
	expectedArtifacts := fs.String("expected-artifacts", "", "optional exact previously reviewed artifact JSON array")
	releaseSequence := fs.Int64("release-sequence", 0, "monotonic release sequence")
	version := fs.String("version", "", "release version")
	commit := fs.String("commit", "", "exact source commit")
	createdAtText := fs.String("created-at", "", "RFC3339 manifest creation time")
	expiresAtText := fs.String("expires-at", "", "RFC3339 metadata expiry time")
	noExpiry := fs.Bool("no-expiry", false, "explicitly create a v2 manifest with no expiry")
	targetOS := fs.String("target-os", "", "target operating system")
	targetArch := fs.String("target-arch", "", "target architecture")
	minCurrentSequence := fs.Int64("min-current-sequence", 0, "minimum accepted installed release sequence")
	maxCurrentSequence := fs.Int64("max-current-sequence", -1, "maximum accepted installed release sequence")
	minCurrentSchema := fs.String("min-current-schema", "", "minimum accepted current database schema")
	targetSchema := fs.String("target-schema", "", "target database schema")
	provenanceSHA256 := fs.String("provenance-sha256", "", "optional SHA-256 of build provenance")
	outPath := fs.String("out", "", "new .tar.gz upgrade bundle")
	var artifacts repeatedFlag
	fs.Var(&artifacts, "artifact", "repeatable payload/path=/source/file mapping")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !signing.ValidKeyID(*keyID) || *privateKeyPath == "" || *version == "" || *commit == "" || *createdAtText == "" || *targetOS == "" || *targetArch == "" || *minCurrentSchema == "" || *targetSchema == "" || *outPath == "" || len(artifacts) == 0 {
		return fmt.Errorf("all signing, release, platform, schema, output and at least one --artifact option are required")
	}
	createdAt, err := time.Parse(time.RFC3339, *createdAtText)
	if err != nil {
		return fmt.Errorf("--created-at must be RFC3339: %w", err)
	}
	expirySupplied := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "expires-at" {
			expirySupplied = true
		}
	})
	if *noExpiry && expirySupplied {
		return fmt.Errorf("--no-expiry conflicts with --expires-at")
	}
	var expiresAt time.Time
	if !*noExpiry {
		if !expirySupplied || *expiresAtText == "" {
			return fmt.Errorf("--expires-at or explicit --no-expiry is required")
		}
		expiresAt, err = time.Parse(time.RFC3339, *expiresAtText)
		if err != nil {
			return fmt.Errorf("--expires-at must be RFC3339: %w", err)
		}
	}
	sources, err := parseArtifacts(artifacts)
	if err != nil {
		return err
	}
	manifest, err := upgradebundle.CreateManifest(upgradebundle.ManifestOptions{
		KeyID:              *keyID,
		ReleaseSequence:    *releaseSequence,
		Version:            *version,
		Commit:             *commit,
		CreatedAt:          createdAt,
		ExpiresAt:          expiresAt,
		NoExpiry:           *noExpiry,
		TargetOS:           *targetOS,
		TargetArch:         *targetArch,
		MinCurrentSequence: *minCurrentSequence,
		MaxCurrentSequence: *maxCurrentSequence,
		MinCurrentSchema:   *minCurrentSchema,
		TargetSchema:       *targetSchema,
		ProvenanceSHA256:   *provenanceSHA256,
	}, sources)
	if err != nil {
		return fmt.Errorf("create signed manifest: %w", err)
	}
	if *expectedArtifacts != "" {
		if err := checkExpectedArtifacts(*expectedArtifacts, manifest.Artifacts); err != nil {
			return err
		}
	}
	privateRaw, err := os.ReadFile(*privateKeyPath)
	if err != nil {
		return fmt.Errorf("read private key: %w", err)
	}
	privateKey, err := signing.ParsePrivateKey(string(privateRaw))
	if err != nil {
		return fmt.Errorf("parse private key: %w", err)
	}
	if err := writeNewBundle(*outPath, manifest, privateKey, sources); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "package=%s\nrelease_sequence=%d\nversion=%s\ncommit=%s\ntarget=%s/%s\ntarget_schema=%s\n", *outPath, manifest.ReleaseSequence, manifest.Version, manifest.Commit, manifest.TargetOS, manifest.TargetArch, manifest.TargetSchema)
	return err
}

// A selected/previously reviewed byte inventory must match before any private
// key is read or output is created. Pack's later stream rehash remains required.
// This ordinary Artifact array is data, never signing or publication authority.
func checkExpectedArtifacts(path string, actual []upgradebundle.Artifact) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 128*1024 {
		return fmt.Errorf("expected artifact inventory must be a bounded regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("cannot read expected artifact inventory")
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return fmt.Errorf("expected artifact inventory changed")
	}
	raw, err := io.ReadAll(io.LimitReader(file, 128*1024+1))
	if err != nil || len(raw) > 128*1024 || strictjson.Validate(raw) != nil {
		return fmt.Errorf("invalid expected artifact inventory")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var expected []upgradebundle.Artifact
	if err := decoder.Decode(&expected); err != nil || len(expected) == 0 || len(expected) > 64 {
		return fmt.Errorf("invalid expected artifact inventory")
	}
	sort.Slice(expected, func(i, j int) bool { return expected[i].Path < expected[j].Path })
	if len(expected) != len(actual) {
		return fmt.Errorf("assembled artifacts differ from expected inventory")
	}
	for index, row := range expected {
		if row != actual[index] {
			return fmt.Errorf("assembled artifacts differ from expected inventory")
		}
	}
	return nil
}

func runVerify(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	publicKeysPath := fs.String("public-keys", "", "JSON public-key ring file")
	packagePath := fs.String("package", "", "signed upgrade bundle")
	targetOS := fs.String("target-os", runtime.GOOS, "current operating system")
	targetArch := fs.String("target-arch", runtime.GOARCH, "current architecture")
	currentSequence := fs.Int64("current-sequence", -1, "current installed release sequence")
	currentSchema := fs.String("current-schema", "", "current database schema")
	atText := fs.String("at", "", "optional RFC3339 verification time")
	maxPayloadBytes := fs.Int64("max-payload-bytes", 0, "optional payload byte limit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *publicKeysPath == "" || *packagePath == "" || *targetOS == "" || *targetArch == "" || *currentSequence < 0 || *currentSchema == "" {
		return fmt.Errorf("--public-keys, --package, --target-os, --target-arch, --current-sequence and --current-schema are required")
	}
	publicKeysRaw, err := os.ReadFile(*publicKeysPath)
	if err != nil {
		return fmt.Errorf("read public-key ring: %w", err)
	}
	ring, err := signing.ParsePublicKeyRing(string(publicKeysRaw), "UPGRADE_PUBLIC_KEYS")
	if err != nil {
		return err
	}
	now := time.Now()
	if strings.TrimSpace(*atText) != "" {
		now, err = time.Parse(time.RFC3339, *atText)
		if err != nil {
			return fmt.Errorf("--at must be RFC3339: %w", err)
		}
	}
	file, err := os.Open(*packagePath)
	if err != nil {
		return fmt.Errorf("open upgrade package: %w", err)
	}
	verification, verifyErr := upgradebundle.Verify(file, upgradebundle.VerifyOptions{
		PublicKeys:      ring,
		Now:             now,
		TargetOS:        *targetOS,
		TargetArch:      *targetArch,
		CurrentSequence: *currentSequence,
		CurrentSchema:   *currentSchema,
		MaxPayloadBytes: *maxPayloadBytes,
	})
	closeErr := file.Close()
	if verifyErr != nil {
		return fmt.Errorf("verify upgrade package: %w", verifyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close upgrade package: %w", closeErr)
	}
	manifest := verification.Manifest
	// Verify accepts only this canonical encoding. Bind consumers' parsed
	// artifact lists to the exact signed manifest, not only its release identity.
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("encode verified manifest: %w", err)
	}
	_, err = fmt.Fprintf(stdout, "verified=true\npackage_sha256=%s\nmanifest_sha256=%x\nrelease_sequence=%d\nversion=%s\ncommit=%s\ntarget=%s/%s\ntarget_schema=%s\npayload_bytes=%d\n", verification.BundleSHA256, sha256.Sum256(manifestJSON), manifest.ReleaseSequence, manifest.Version, manifest.Commit, manifest.TargetOS, manifest.TargetArch, manifest.TargetSchema, verification.PayloadBytes)
	return err
}

func parseArtifacts(values []string) ([]upgradebundle.Source, error) {
	sources := make([]upgradebundle.Source, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		bundlePath, filePath, ok := strings.Cut(value, "=")
		bundlePath = strings.TrimSpace(bundlePath)
		filePath = strings.TrimSpace(filePath)
		if !ok || bundlePath == "" || filePath == "" {
			return nil, fmt.Errorf("--artifact must use payload/path=/source/file")
		}
		if seen[bundlePath] {
			return nil, fmt.Errorf("duplicate --artifact path %q", bundlePath)
		}
		seen[bundlePath] = true
		sources = append(sources, upgradebundle.Source{BundlePath: bundlePath, FilePath: filePath})
	}
	return sources, nil
}

func writeNewBundle(path string, manifest upgradebundle.Manifest, privateKey []byte, sources []upgradebundle.Source) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create upgrade package: %w", err)
	}
	removePartial := func(cause error) error {
		_ = file.Close()
		if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
			return fmt.Errorf("%v; remove partial package: %w", cause, removeErr)
		}
		return cause
	}
	if err := upgradebundle.Pack(file, manifest, privateKey, sources); err != nil {
		return removePartial(fmt.Errorf("pack upgrade package: %w", err))
	}
	if err := file.Sync(); err != nil {
		return removePartial(fmt.Errorf("sync upgrade package: %w", err))
	}
	if err := file.Close(); err != nil {
		if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
			return fmt.Errorf("close upgrade package: %v; remove partial package: %w", err, removeErr)
		}
		return fmt.Errorf("close upgrade package: %w", err)
	}
	return nil
}
