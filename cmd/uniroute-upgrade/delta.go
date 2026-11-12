package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/uniroute/unirouter/internal/signing"
	"github.com/uniroute/unirouter/internal/upgradebundle"
)

type deltaFileIdentity struct {
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

func deltaInput(path string, max int64) (*os.File, int64, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > max {
		return nil, 0, fmt.Errorf("delta input must be a bounded regular file")
	}
	input, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	opened, err := input.Stat()
	if err != nil || !os.SameFile(info, opened) || opened.Size() != info.Size() {
		input.Close()
		return nil, 0, fmt.Errorf("delta input changed before opening")
	}
	return input, opened.Size(), nil
}

func deltaFileSHA(file *os.File, size int64) (string, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(file, size+1))
	if err != nil || n != size {
		return "", fmt.Errorf("delta file changed while hashing")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type boundedDeltaOutput struct {
	output    io.Writer
	remaining int64
}

func (w *boundedDeltaOutput) Write(data []byte) (int, error) {
	if int64(len(data)) > w.remaining {
		return 0, fmt.Errorf("delta output exceeds its byte limit")
	}
	n, err := w.output.Write(data)
	w.remaining -= int64(n)
	return n, err
}

func runDelta(action string, args []string, stdout io.Writer) error {
	create := action == "create-delta"
	fs := flag.NewFlagSet(action, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	basisPath := fs.String("basis", "", "retained signed full package")
	keysPath := fs.String("public-keys", "", "trusted public-key ring")
	schema := fs.String("current-schema", "", "schema for verification of the basis as a full package")
	output := fs.String("out", "", "new output path")
	var targetPath, deltaPath, basisSHA, deltaSHA, targetSHA string
	var targetSize int64
	if create {
		fs.StringVar(&targetPath, "target", "", "exact signed full target package")
	} else {
		fs.StringVar(&deltaPath, "delta", "", "pinned transport sidecar")
		fs.StringVar(&basisSHA, "basis-sha256", "", "pinned basis package SHA-256")
		fs.StringVar(&deltaSHA, "delta-sha256", "", "pinned sidecar SHA-256")
		fs.StringVar(&targetSHA, "target-sha256", "", "pinned full target package SHA-256")
		fs.Int64Var(&targetSize, "target-size", 0, "pinned full target byte count")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || *basisPath == "" || *keysPath == "" || *schema == "" || *output == "" ||
		(create && targetPath == "") || (!create && (deltaPath == "" || basisSHA == "" || deltaSHA == "" ||
		targetSHA == "" || targetSize <= 0 || targetSize > upgradebundle.CommunityBundleLimit)) {
		return fmt.Errorf("delta requires basis, trusted keys, current schema, a new output and the target or its pinned sidecar identities")
	}
	keys, keySize, err := deltaInput(*keysPath, 64*1024)
	if err != nil {
		return err
	}
	defer keys.Close()
	keyBytes, err := io.ReadAll(io.LimitReader(keys, keySize+1))
	if err != nil || int64(len(keyBytes)) != keySize {
		return fmt.Errorf("public-key ring changed while reading")
	}
	ring, err := signing.ParsePublicKeyRing(string(keyBytes), "UPGRADE_PUBLIC_KEYS")
	if err != nil {
		return err
	}
	options := upgradebundle.VerifyOptions{
		PublicKeys: ring, Now: time.Now(), TargetOS: "linux", TargetArch: "amd64",
		CurrentSequence: 0, CurrentSchema: *schema, MaxPayloadBytes: upgradebundle.CommunityBundleLimit,
	}
	basis, basisSize, err := deltaInput(*basisPath, upgradebundle.CommunityBundleLimit)
	if err != nil {
		return err
	}
	defer basis.Close()
	otherPath, otherLimit := targetPath, upgradebundle.CommunityBundleLimit
	if !create {
		otherPath, otherLimit = deltaPath, upgradebundle.CommunityDeltaLimit
	}
	other, otherSize, err := deltaInput(otherPath, otherLimit)
	if err != nil {
		return err
	}
	defer other.Close()
	if !create {
		if actual, err := deltaFileSHA(other, otherSize); err != nil || actual != deltaSHA {
			return fmt.Errorf("delta sidecar differs from the pinned SHA-256")
		}
	}
	if _, err := os.Lstat(*output); !os.IsNotExist(err) {
		return fmt.Errorf("delta output path must be unused")
	}
	// The bootstrap holds its private preparation lock. Keep partial local
	// work outside that directory, then publish without replacing any file.
	stagingParent := filepath.Dir(*output)
	if !create {
		stagingParent = filepath.Dir(stagingParent)
	}
	tmp, err := os.CreateTemp(stagingParent, ".ur-delta-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	var report upgradebundle.DeltaReport
	if create {
		targetSize = otherSize
		bounded := &boundedDeltaOutput{tmp, min(targetSize-1, upgradebundle.CommunityDeltaLimit)}
		report, err = upgradebundle.CreateDelta(bounded, basis, other, options)
	} else {
		bounded := &boundedDeltaOutput{tmp, targetSize}
		report, err = upgradebundle.MaterializeDelta(bounded, basis, other, options, basisSHA, targetSHA)
	}
	if err != nil {
		return err
	}
	info, err := tmp.Stat()
	if err != nil {
		return err
	}
	if create {
		if info.Size() <= 0 || info.Size() > upgradebundle.CommunityDeltaLimit || info.Size() >= targetSize {
			return fmt.Errorf("delta sidecar does not fit the transport size limit")
		}
		deltaSHA, err = deltaFileSHA(tmp, info.Size())
		if err != nil {
			return err
		}
		otherSize = info.Size()
		if _, err := basis.Seek(0, io.SeekStart); err != nil {
			return err
		}
		bounded := &boundedDeltaOutput{io.Discard, targetSize}
		if _, err := upgradebundle.MaterializeDelta(bounded, basis, tmp, options, report.BasisSHA256, report.TargetSHA256); err != nil {
			return fmt.Errorf("sidecar round trip does not reproduce the signed target: %w", err)
		}
		if bounded.remaining != 0 {
			return fmt.Errorf("sidecar round trip differs from the exact target size")
		}
	} else {
		if info.Size() != targetSize {
			return fmt.Errorf("materialized package differs from the pinned size")
		}
		if _, err := tmp.Seek(0, io.SeekStart); err != nil {
			return err
		}
		verified, err := upgradebundle.Verify(tmp, options)
		if err != nil || verified.BundleSHA256 != targetSHA {
			return fmt.Errorf("materialized full package verification failed")
		}
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Link(tmp.Name(), *output); err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(struct {
		upgradebundle.DeltaReport
		Basis  deltaFileIdentity `json:"basis"`
		Delta  deltaFileIdentity `json:"delta"`
		Target deltaFileIdentity `json:"target"`
	}{report, deltaFileIdentity{basisSize, report.BasisSHA256}, deltaFileIdentity{otherSize, deltaSHA}, deltaFileIdentity{targetSize, report.TargetSHA256}})
}
