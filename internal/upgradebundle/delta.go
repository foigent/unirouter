package upgradebundle

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"time"
)

// This transport profile applies only to the bounded community release entry.
const CommunityBundleLimit = int64(512 << 20)
const CommunityDeltaLimit = int64(32 << 20)
const communityExpandedLimit = CommunityBundleLimit + maxManifestBytes + maxSignatureBytes + maxArchiveEntries*1024 + 1024

type DeltaReport struct {
	BasisSHA256  string `json:"basis_sha256"`
	TargetSHA256 string `json:"target_sha256"`
	ReusedBytes  int64  `json:"reused_member_bytes"`
	NewBytes     int64  `json:"new_member_bytes"`
}

type deltaArchive struct {
	buffer   *bufio.Reader
	gzip     *gzip.Reader
	tar      *tar.Reader
	metadata archiveMetadata
}

func openDeltaArchive(input io.ReadSeeker, options VerifyOptions) (*deltaArchive, error) {
	if options.Now.IsZero() {
		options.Now = time.Now()
	}
	if _, err := input.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	buffer := bufio.NewReader(input)
	compressed, err := gzip.NewReader(buffer)
	if err != nil {
		return nil, err
	}
	compressed.Multistream(false)
	archive := tar.NewReader(io.LimitReader(compressed, communityExpandedLimit))
	metadata, err := readSignedMetadata(archive, options)
	if err != nil {
		_ = compressed.Close()
		return nil, err
	}
	return &deltaArchive{buffer, compressed, archive, metadata}, nil
}

// Full input archives have already passed Verify. The sidecar must supply
// exactly the requested changed entries, in signed manifest order.
func (archive *deltaArchive) openArtifact(artifact Artifact, skipEarlier bool) (io.ReadCloser, error) {
	for {
		header, err := archive.tar.Next()
		if err != nil {
			return nil, fmt.Errorf("read artifact %s: %w", artifact.Path, err)
		}
		if skipEarlier && header.Name < artifact.Path {
			continue
		}
		if header.Name != artifact.Path || header.Size != artifact.Size ||
			header.Typeflag != tar.TypeReg || header.Format != tar.FormatUSTAR {
			return nil, fmt.Errorf("unexpected archive entry for %s", artifact.Path)
		}
		return io.NopCloser(archive.tar), nil
	}
}

func (archive *deltaArchive) finish() error {
	if _, err := archive.tar.Next(); err != io.EOF {
		return fmt.Errorf("unexpected additional delta entries or malformed trailer")
	}
	var extra [1]byte
	if _, err := io.ReadFull(archive.gzip, extra[:]); err != io.EOF {
		return fmt.Errorf("unexpected delta data after the tar")
	}
	if _, err := archive.buffer.Peek(1); err != io.EOF {
		return fmt.Errorf("unexpected data after the delta gzip")
	}
	return nil
}

func deltaBasis(basis io.ReadSeeker, options VerifyOptions) (Verification, VerifyOptions, error) {
	if options.Now.IsZero() {
		options.Now = time.Now()
	}
	options.MaxPayloadBytes = CommunityBundleLimit
	verified, err := verifyArchive(basis, options, communityExpandedLimit)
	if err != nil {
		return Verification{}, VerifyOptions{}, fmt.Errorf("verify delta basis: %w", err)
	}
	options.CurrentSequence = verified.Manifest.ReleaseSequence
	options.CurrentSchema = verified.Manifest.TargetSchema
	return verified, options, nil
}

func deltaMembers(basis, target Manifest) (map[string]bool, DeltaReport, error) {
	if basis.KeyID != target.KeyID {
		return nil, DeltaReport{}, fmt.Errorf("delta requires the same trusted release key")
	}
	basisByPath := make(map[string]Artifact, len(basis.Artifacts))
	for _, artifact := range basis.Artifacts {
		basisByPath[artifact.Path] = artifact
	}
	reuse := make(map[string]bool, len(target.Artifacts))
	var report DeltaReport
	for _, artifact := range target.Artifacts {
		if artifact.Size > CommunityBundleLimit-report.ReusedBytes-report.NewBytes {
			return nil, DeltaReport{}, fmt.Errorf("delta target payload exceeds the community limit")
		}
		if previous, ok := basisByPath[artifact.Path]; ok && previous == artifact {
			reuse[artifact.Path] = true
			report.ReusedBytes += artifact.Size
		} else {
			report.NewBytes += artifact.Size
		}
	}
	if report.ReusedBytes == 0 {
		return nil, DeltaReport{}, fmt.Errorf("delta has no reusable bytes")
	}
	return reuse, report, nil
}

// CreateDelta copies the original signed target metadata and only changed
// members. The signed full target remains the release authority.
func CreateDelta(w io.Writer, basis, target io.ReadSeeker, options VerifyOptions) (DeltaReport, error) {
	verifiedBasis, targetOptions, err := deltaBasis(basis, options)
	if err != nil {
		return DeltaReport{}, err
	}
	verifiedTarget, err := verifyArchive(target, targetOptions, communityExpandedLimit)
	if err != nil {
		return DeltaReport{}, fmt.Errorf("verify delta target: %w", err)
	}
	archive, err := openDeltaArchive(target, targetOptions)
	if err != nil {
		return DeltaReport{}, err
	}
	defer archive.gzip.Close()
	reuse, report, err := deltaMembers(verifiedBasis.Manifest, archive.metadata.manifest)
	if err != nil {
		return DeltaReport{}, err
	}
	var changed []Artifact
	for _, artifact := range archive.metadata.manifest.Artifacts {
		if !reuse[artifact.Path] {
			changed = append(changed, artifact)
		}
	}
	if err := writeSignedBundle(w, archive.metadata, changed, func(artifact Artifact) (io.ReadCloser, error) {
		return archive.openArtifact(artifact, true)
	}); err != nil {
		return DeltaReport{}, err
	}
	report.BasisSHA256, report.TargetSHA256 = verifiedBasis.BundleSHA256, verifiedTarget.BundleSHA256
	return report, nil
}

// MaterializeDelta writes a candidate full archive to private staging. The
// caller publishes it only after success and full verification.
func MaterializeDelta(w io.Writer, basis, delta io.ReadSeeker, options VerifyOptions, basisSHA256, targetSHA256 string) (DeltaReport, error) {
	if !validSHA256(basisSHA256) || !validSHA256(targetSHA256) {
		return DeltaReport{}, fmt.Errorf("exact basis and target package digests are required")
	}
	verifiedBasis, targetOptions, err := deltaBasis(basis, options)
	if err != nil {
		return DeltaReport{}, err
	}
	if verifiedBasis.BundleSHA256 != basisSHA256 {
		return DeltaReport{}, fmt.Errorf("delta basis differs from the pinned signed archive")
	}
	old, err := openDeltaArchive(basis, options)
	if err != nil {
		return DeltaReport{}, err
	}
	defer old.gzip.Close()
	sidecar, err := openDeltaArchive(delta, targetOptions)
	if err != nil {
		return DeltaReport{}, fmt.Errorf("verify delta target metadata: %w", err)
	}
	defer sidecar.gzip.Close()
	reuse, report, err := deltaMembers(verifiedBasis.Manifest, sidecar.metadata.manifest)
	if err != nil {
		return DeltaReport{}, err
	}
	digest := sha256.New()
	if err := writeSignedBundle(io.MultiWriter(w, digest), sidecar.metadata, sidecar.metadata.manifest.Artifacts,
		func(artifact Artifact) (io.ReadCloser, error) {
			if reuse[artifact.Path] {
				return old.openArtifact(artifact, true)
			}
			return sidecar.openArtifact(artifact, false)
		}); err != nil {
		return DeltaReport{}, err
	}
	if err := sidecar.finish(); err != nil {
		return DeltaReport{}, err
	}
	if hex.EncodeToString(digest.Sum(nil)) != targetSHA256 {
		return DeltaReport{}, fmt.Errorf("materialized archive differs from the pinned target bytes")
	}
	report.BasisSHA256, report.TargetSHA256 = basisSHA256, targetSHA256
	return report, nil
}
