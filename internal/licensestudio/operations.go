package licensestudio

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/licensing"
	"github.com/uniroute/unirouter/internal/signing"
)

const (
	KeyPurposeLicense = "license"
	KeyPurposeUpgrade = "upgrade"

	maxPrivateKeyFileBytes = 4096
	maxPublicRingFileBytes = 64 * 1024
	maxLicenseFileBytes    = 256 * 1024
)

type KeyGenerationResult struct {
	Purpose           string `json:"purpose"`
	KeyID             string `json:"key_id"`
	PrivateFile       string `json:"private_file"`
	PublicFile        string `json:"public_file"`
	PublicFingerprint string `json:"public_fingerprint"`
	BuildArgument     string `json:"build_argument"`
	BuildValue        string `json:"build_value"`
}

type IssueResult struct {
	LicenseFile   string            `json:"license_file"`
	LicenseSHA256 string            `json:"license_sha256"`
	KeyID         string            `json:"key_id"`
	Payload       licensing.Payload `json:"payload"`
}

func GenerateKeyFiles(directory, purpose, keyID string) (KeyGenerationResult, error) {
	directory = strings.TrimSpace(directory)
	purpose = strings.TrimSpace(purpose)
	keyID = strings.TrimSpace(keyID)
	if purpose != KeyPurposeLicense && purpose != KeyPurposeUpgrade {
		return KeyGenerationResult{}, fmt.Errorf("key purpose must be license or upgrade")
	}
	if !signing.ValidKeyID(keyID) {
		return KeyGenerationResult{}, fmt.Errorf("key_id must contain only letters, digits, dot, dash or underscore and be at most 64 characters")
	}
	info, err := os.Stat(directory)
	if err != nil {
		return KeyGenerationResult{}, fmt.Errorf("inspect selected folder: %w", err)
	}
	if !info.IsDir() {
		return KeyGenerationResult{}, fmt.Errorf("selected path is not a folder")
	}

	privateName := keyID + ".private"
	publicName := keyID + ".public-keys.json"
	privatePath := filepath.Join(directory, privateName)
	publicPath := filepath.Join(directory, publicName)
	if samePath(privatePath, publicPath) {
		return KeyGenerationResult{}, fmt.Errorf("private and public output paths must differ")
	}
	if _, err := os.Lstat(privatePath); err == nil {
		return KeyGenerationResult{}, fmt.Errorf("refusing to overwrite existing file %s", privateName)
	} else if !os.IsNotExist(err) {
		return KeyGenerationResult{}, fmt.Errorf("inspect private-key destination: %w", err)
	}
	if _, err := os.Lstat(publicPath); err == nil {
		return KeyGenerationResult{}, fmt.Errorf("refusing to overwrite existing file %s", publicName)
	} else if !os.IsNotExist(err) {
		return KeyGenerationResult{}, fmt.Errorf("inspect public-key destination: %w", err)
	}

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return KeyGenerationResult{}, fmt.Errorf("generate Ed25519 key: %w", err)
	}
	defer zeroBytes(privateKey)
	encodedPrivate, err := signing.EncodePrivateKey(privateKey)
	if err != nil {
		return KeyGenerationResult{}, err
	}
	encodedPublic, err := signing.EncodePublicKey(publicKey)
	if err != nil {
		return KeyGenerationResult{}, err
	}
	publicRing, err := json.Marshal(map[string]string{keyID: encodedPublic})
	if err != nil {
		return KeyGenerationResult{}, fmt.Errorf("encode public-key ring: %w", err)
	}
	if err := writeNewFile(privatePath, []byte(encodedPrivate+"\n"), 0o600); err != nil {
		return KeyGenerationResult{}, fmt.Errorf("write private key: %w", err)
	}
	if err := writeNewFile(publicPath, append(publicRing, '\n'), 0o644); err != nil {
		if removeErr := os.Remove(privatePath); removeErr != nil && !os.IsNotExist(removeErr) {
			return KeyGenerationResult{}, fmt.Errorf("write public key: %v; remove newly created private key: %w", err, removeErr)
		}
		return KeyGenerationResult{}, fmt.Errorf("write public key: %w", err)
	}
	if runtime.GOOS != "windows" {
		privateInfo, statErr := os.Stat(privatePath)
		if statErr != nil {
			return KeyGenerationResult{}, fmt.Errorf("verify private-key permissions: %w", statErr)
		}
		if privateInfo.Mode().Perm() != 0o600 {
			return KeyGenerationResult{}, fmt.Errorf("selected storage does not preserve private-key mode 0600")
		}
	}
	digest := sha256.Sum256(publicKey)
	argument := "UNIROUTER_LICENSE_TRUST_BUNDLE_B64"
	if purpose == KeyPurposeUpgrade {
		argument = "UNIROUTER_UPGRADE_TRUST_BUNDLE_B64"
	}
	return KeyGenerationResult{
		Purpose:           purpose,
		KeyID:             keyID,
		PrivateFile:       privateName,
		PublicFile:        publicName,
		PublicFingerprint: hex.EncodeToString(digest[:]),
		BuildArgument:     argument,
		BuildValue:        base64.RawURLEncoding.EncodeToString(publicRing),
	}, nil
}

func IssueLicense(privateKeyPath, outputPath, keyID string, payload licensing.Payload) (IssueResult, error) {
	keyID = strings.TrimSpace(keyID)
	if !signing.ValidKeyID(keyID) {
		return IssueResult{}, fmt.Errorf("key_id is invalid")
	}
	privateRaw, err := readLimitedFile(privateKeyPath, maxPrivateKeyFileBytes)
	if err != nil {
		return IssueResult{}, fmt.Errorf("read private key: %w", err)
	}
	defer zeroBytes(privateRaw)
	privateKey, err := signing.ParsePrivateKeyBytes(privateRaw)
	if err != nil {
		return IssueResult{}, fmt.Errorf("parse private key: %w", err)
	}
	defer zeroBytes(privateKey)
	doc, err := licensing.SignCommercialPayload(payload, keyID, privateKey)
	if err != nil {
		return IssueResult{}, fmt.Errorf("sign License: %w", err)
	}
	document, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return IssueResult{}, fmt.Errorf("encode License: %w", err)
	}
	document = append(document, '\n')
	publicKey := append(ed25519.PublicKey(nil), privateKey.Public().(ed25519.PublicKey)...)
	verificationTime, err := time.Parse(time.RFC3339, payload.NotBefore)
	if err != nil {
		return IssueResult{}, fmt.Errorf("self-verify License: invalid not_before: %w", err)
	}
	verifier := licensing.NewCommercialVerifier(map[string]ed25519.PublicKey{keyID: publicKey}, nil, 30, payload.DeploymentID)
	verifier.WithNow(func() time.Time { return verificationTime })
	status := verifier.VerifyBytes(document)
	if !status.Valid || !status.Enforceable || !status.SignatureValid {
		return IssueResult{}, fmt.Errorf("self-verify License failed: status=%s error=%s", status.Status, status.Error)
	}
	if err := writeNewFile(outputPath, document, 0o600); err != nil {
		return IssueResult{}, fmt.Errorf("write signed License: %w", err)
	}
	digest := sha256.Sum256(document)
	return IssueResult{
		LicenseFile:   filepath.Base(outputPath),
		LicenseSHA256: hex.EncodeToString(digest[:]),
		KeyID:         keyID,
		Payload:       payload,
	}, nil
}

func VerifyLicense(publicKeyRingPath, licensePath, deploymentID string, at time.Time) licensing.Status {
	publicRaw, err := readLimitedFile(publicKeyRingPath, maxPublicRingFileBytes)
	if err != nil {
		return licensing.Status{Status: licensing.StatusInvalid, Error: fmt.Sprintf("read public-key ring: %v", err)}
	}
	ring, err := licensing.ParsePublicKeyRing(string(publicRaw))
	if err != nil {
		return licensing.Status{Status: licensing.StatusInvalid, Error: err.Error()}
	}
	licenseRaw, err := readLimitedFile(licensePath, maxLicenseFileBytes)
	if err != nil {
		return licensing.Status{Status: licensing.StatusInvalid, Error: fmt.Sprintf("read License: %v", err)}
	}
	verifier := licensing.NewCommercialVerifier(ring, nil, 30, strings.TrimSpace(deploymentID))
	if !at.IsZero() {
		verifier.WithNow(func() time.Time { return at })
	}
	return verifier.VerifyBytes(licenseRaw)
}

func writeNewFile(path string, contents []byte, mode os.FileMode) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("output path is required")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	removePartial := func(cause error) error {
		_ = file.Close()
		if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
			return fmt.Errorf("%v; remove partial output: %w", cause, removeErr)
		}
		return cause
	}
	if _, err := file.Write(contents); err != nil {
		return removePartial(err)
	}
	if err := file.Sync(); err != nil {
		return removePartial(err)
	}
	if err := file.Close(); err != nil {
		if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
			return fmt.Errorf("close output: %v; remove partial output: %w", err, removeErr)
		}
		return err
	}
	return nil
}

func readLimitedFile(path string, maxBytes int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > maxBytes {
		return nil, fmt.Errorf("file exceeds %d bytes", maxBytes)
	}
	return raw, nil
}

func zeroBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && leftAbs == rightAbs
}
