package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReceiverFeedCredentialDerivation(t *testing.T) {
	rawToken := ReceiverFeedTokenPrefix + strings.Repeat("ab", 32)
	input := append([]byte(ReceiverFeedVerifierDomain), 0)
	input = append(input, rawToken...)
	expectedVerifier := sha256.Sum256(input)

	verifier, err := ReceiverFeedVerifier(rawToken)
	require.NoError(t, err)
	require.Equal(t, expectedVerifier[:], verifier)
	require.Equal(t, hex.EncodeToString(expectedVerifier[:]), mustReceiverVerifierHex(t, rawToken))

	expectedMAC := hmac.New(sha256.New, []byte("server-secret"))
	_, _ = expectedMAC.Write(expectedVerifier[:])
	hash, prefix, err := HashReceiverFeedCredential(rawToken, "server-secret")
	require.NoError(t, err)
	require.Equal(t, hex.EncodeToString(expectedMAC.Sum(nil)), hash)
	require.Equal(t, rawToken[:ReceiverFeedCredentialPrefixLength], prefix)
}

func TestReceiverFeedCredentialBilateralZeroVector(t *testing.T) {
	rawToken := ReceiverFeedTokenPrefix + strings.Repeat("0", 64)
	require.Equal(t, "ur-xf1-0000000000000", mustReceiverPrefix(t, rawToken))
	require.Equal(
		t,
		"d14985e8cedc38985529b846e34f152448401b2b2e5e28db0f64bd8727c1ff07",
		mustReceiverVerifierHex(t, rawToken),
	)
}

func TestReceiverFeedCredentialRejectsNonCanonicalInputs(t *testing.T) {
	valid := ReceiverFeedTokenPrefix + strings.Repeat("ab", 32)
	for _, invalid := range []string{
		"",
		valid + "0",
		valid[:len(valid)-1],
		strings.ToUpper(valid),
		" " + valid,
		valid + "\n",
		"ur-xf2-" + strings.Repeat("ab", 32),
	} {
		_, err := ReceiverFeedVerifier(invalid)
		require.Error(t, err, invalid)
	}

	validVerifier := mustReceiverVerifierHex(t, valid)
	for _, invalid := range []string{
		"",
		validVerifier[:63],
		validVerifier + "0",
		strings.ToUpper(validVerifier),
		" " + validVerifier,
		strings.Repeat("z", 64),
	} {
		_, err := DecodeReceiverFeedVerifier(invalid)
		require.Error(t, err, invalid)
	}
}

func mustReceiverVerifierHex(t *testing.T, rawToken string) string {
	t.Helper()
	verifier, err := ReceiverFeedVerifierHex(rawToken)
	require.NoError(t, err)
	return verifier
}

func mustReceiverPrefix(t *testing.T, rawToken string) string {
	t.Helper()
	prefix, err := ReceiverFeedKeyPrefix(rawToken)
	require.NoError(t, err)
	return prefix
}
