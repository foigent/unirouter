package modelcapabilitypublication

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// ReadArtifactMigrationFiles hashes every exact .up.sql file in ledger order.
func ReadArtifactMigrationFiles(dir string, ledger []string) ([]MigrationFileIdentity, error) {
	files := make([]MigrationFileIdentity, 0, len(ledger))
	for _, version := range ledger {
		if err := validateMigrationID("migration artifact version", version); err != nil {
			return nil, err
		}
		digest, err := fileSHA256(filepath.Join(dir, version+".up.sql"))
		if err != nil {
			return nil, err
		}
		files = append(files, MigrationFileIdentity{Version: version, UpSHA256: digest})
	}
	return files, nil
}

func ReadPublicationAuxiliaryDigests(dir, publicationID string) (down, verify string, err error) {
	if err := validateMigrationID("publication_id", publicationID); err != nil {
		return "", "", err
	}
	down, err = fileSHA256(filepath.Join(dir, publicationID+".down.sql"))
	if err != nil {
		return "", "", err
	}
	verify, err = fileSHA256(filepath.Join(dir, publicationID+".verify.sql"))
	if err != nil {
		return "", "", err
	}
	return down, verify, nil
}

func VerifyPlanArtifactFiles(dir string, plan Plan) error {
	files, err := ReadArtifactMigrationFiles(dir, plan.ArtifactMigrationLedger)
	if err != nil {
		return err
	}
	if !slices.Equal(files, plan.ArtifactMigrationFiles) {
		return fmt.Errorf("%w: artifact migration bytes do not match Plan", ErrPlanConflict)
	}
	down, verify, err := ReadPublicationAuxiliaryDigests(dir, plan.PublicationID)
	if err != nil {
		return err
	}
	if down != plan.PublicationDownSHA256 || verify != plan.PublicationVerifySHA256 {
		return fmt.Errorf("%w: publication auxiliary artifact bytes do not match Plan", ErrPlanConflict)
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	wire, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read artifact file %s: %w", path, err)
	}
	hash := sha256.Sum256(wire)
	return "sha256:" + hex.EncodeToString(hash[:]), nil
}
