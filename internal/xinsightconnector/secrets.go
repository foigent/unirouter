package xinsightconnector

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/uniroute/unirouter/internal/auth"
)

const connectorControlSecretDomain = "unirouter.xinsight.connector-control.v1"
const connectorControlBindingDomain = "unirouter.xinsight.connector-control.binding.v1"

type connectorSecretKind string

const (
	connectorInvitationSecret connectorSecretKind = "invitation_token"
	connectorControlSecret    connectorSecretKind = "control_token"
)

// connectorSecretCipher reuses the product encryption root while deriving a
// purpose-local key for each Connector authority. The receiver bearer is not a
// supported kind because UniRouter must never receive or persist it.
type connectorSecretCipher struct {
	invitationKey [sha256.Size]byte
	controlKey    [sha256.Size]byte
}

func newConnectorSecretCipher(master []byte) (*connectorSecretCipher, error) {
	if len(master) != sha256.Size {
		return nil, fmt.Errorf("%w: Connector encryption root must be 32 bytes", ErrInvalid)
	}
	return &connectorSecretCipher{
		invitationKey: deriveConnectorSecretKey(master, connectorInvitationSecret),
		controlKey:    deriveConnectorSecretKey(master, connectorControlSecret),
	}, nil
}

func deriveConnectorSecretKey(master []byte, kind connectorSecretKind) [sha256.Size]byte {
	mac := hmac.New(sha256.New, master)
	_, _ = mac.Write([]byte(connectorControlSecretDomain))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(kind))
	var key [sha256.Size]byte
	copy(key[:], mac.Sum(nil))
	return key
}

func (cipher *connectorSecretCipher) seal(
	kind connectorSecretKind,
	installationID, plaintext string,
) (string, error) {
	if cipher == nil || !validExactExternalIdentity(installationID) ||
		plaintext == "" || len(plaintext) > 1024 {
		return "", ErrInvalid
	}
	key, err := cipher.key(kind, installationID)
	if err != nil {
		return "", err
	}
	sealed, err := auth.EncryptKey([]byte(plaintext), key)
	if err != nil {
		return "", fmt.Errorf("%w: seal Connector secret", ErrUnavailable)
	}
	return sealed, nil
}

func (cipher *connectorSecretCipher) open(
	kind connectorSecretKind,
	installationID, ciphertext string,
) (string, error) {
	if cipher == nil || !validExactExternalIdentity(installationID) ||
		ciphertext == "" || len(ciphertext) > 4096 {
		return "", ErrInvalid
	}
	key, err := cipher.key(kind, installationID)
	if err != nil {
		return "", err
	}
	plaintext, err := auth.DecryptKey(ciphertext, key)
	if err != nil || plaintext == "" || len(plaintext) > 1024 {
		return "", fmt.Errorf("%w: open Connector secret", ErrUnavailable)
	}
	return plaintext, nil
}

func (cipher *connectorSecretCipher) key(kind connectorSecretKind, installationID string) ([]byte, error) {
	var purposeKey []byte
	switch kind {
	case connectorInvitationSecret:
		purposeKey = cipher.invitationKey[:]
	case connectorControlSecret:
		purposeKey = cipher.controlKey[:]
	default:
		return nil, errors.Join(ErrInvalid, errors.New("unsupported Connector secret authority"))
	}
	mac := hmac.New(sha256.New, purposeKey)
	_, _ = mac.Write([]byte(connectorControlBindingDomain))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(installationID))
	return mac.Sum(nil), nil
}
