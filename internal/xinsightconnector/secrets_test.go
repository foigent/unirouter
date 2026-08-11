package xinsightconnector

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConnectorSecretsUseDistinctPurposeDomains(t *testing.T) {
	cipher, err := newConnectorSecretCipher([]byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	const secret = "same-fixture-secret"
	const installationID = "xci_AbCdEfGhIjKlMnOpQrStUv"

	invitationCiphertext, err := cipher.seal(connectorInvitationSecret, installationID, secret)
	require.NoError(t, err)
	controlCiphertext, err := cipher.seal(connectorControlSecret, installationID, secret)
	require.NoError(t, err)
	require.NotEqual(t, invitationCiphertext, controlCiphertext)
	require.NotContains(t, invitationCiphertext, secret)
	require.NotContains(t, controlCiphertext, secret)

	invitation, err := cipher.open(connectorInvitationSecret, installationID, invitationCiphertext)
	require.NoError(t, err)
	require.Equal(t, secret, invitation)
	control, err := cipher.open(connectorControlSecret, installationID, controlCiphertext)
	require.NoError(t, err)
	require.Equal(t, secret, control)

	_, err = cipher.open(connectorControlSecret, installationID, invitationCiphertext)
	require.Error(t, err, "an invitation ciphertext must not authorize control polling")
	_, err = cipher.open(connectorInvitationSecret, installationID, controlCiphertext)
	require.Error(t, err, "a control ciphertext must not authorize invitation redemption")
	_, err = cipher.open(connectorControlSecret, "xci_ZZZZZZZZZZZZZZZZZZZZZZ", controlCiphertext)
	require.Error(t, err, "a ciphertext must not be movable to another installation")
}

func TestConnectorSecretCipherFailsClosed(t *testing.T) {
	_, err := newConnectorSecretCipher([]byte("short"))
	require.ErrorIs(t, err, ErrInvalid)
	cipher, err := newConnectorSecretCipher([]byte(strings.Repeat("k", 32)))
	require.NoError(t, err)

	const installationID = "xci_AbCdEfGhIjKlMnOpQrStUv"
	_, err = cipher.seal("receiver_bearer", installationID, "must-never-enter-unirouter")
	require.ErrorIs(t, err, ErrInvalid)
	_, err = cipher.seal(connectorControlSecret, installationID, "")
	require.ErrorIs(t, err, ErrInvalid)
	_, err = cipher.open(connectorControlSecret, installationID, "not-ciphertext")
	require.Error(t, err)
}
