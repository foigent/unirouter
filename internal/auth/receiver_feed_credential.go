package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

const (
	LegacyAPIVerifierScheme            = "legacy_hmac_sha256_v1"
	ReceiverFeedVerifierScheme         = "receiver_sha256_v1"
	ReceiverFeedRegistrationSchema     = "unirouter.xinsight.feed-credential-registration.v1"
	ReceiverFeedTokenFormat            = "ur-xf1-hex256.v1"
	ReceiverFeedTokenPrefix            = "ur-xf1-"
	ReceiverFeedVerifierDomain         = "unirouter.xinsight.feed-credential.v1"
	ReceiverFeedCredentialPrefixLength = 20
	XInsightPolicyTokenPrefix          = "ur-xpm1-"
	XInsightPolicyVerifierDomain       = "unirouter.xinsight.policy-manage.v1"
	XInsightEnrollmentTokenPrefix      = "ur-xse1-"
	XInsightEnrollmentVerifierDomain   = "unirouter.xinsight.source-enroll.v1"
)

var errInvalidReceiverFeedCredential = errors.New("invalid receiver-generated feed credential")

// ReceiverFeedVerifier derives the public, non-secret verifier for the
// receiver-generated feed credential contract.
func ReceiverFeedVerifier(rawToken string) ([]byte, error) {
	if !validReceiverFeedRawToken(rawToken) {
		return nil, errInvalidReceiverFeedCredential
	}
	input := make([]byte, 0, len(ReceiverFeedVerifierDomain)+1+len(rawToken))
	input = append(input, ReceiverFeedVerifierDomain...)
	input = append(input, 0)
	input = append(input, rawToken...)
	sum := sha256.Sum256(input)
	return append([]byte(nil), sum[:]...), nil
}

func ReceiverFeedVerifierHex(rawToken string) (string, error) {
	verifier, err := ReceiverFeedVerifier(rawToken)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(verifier), nil
}

func ReceiverFeedKeyPrefix(rawToken string) (string, error) {
	if !validReceiverFeedRawToken(rawToken) {
		return "", errInvalidReceiverFeedCredential
	}
	return rawToken[:ReceiverFeedCredentialPrefixLength], nil
}

// DecodeReceiverFeedVerifier accepts only the exact lowercase 32-byte digest
// representation frozen by the registration contract.
func DecodeReceiverFeedVerifier(raw string) ([]byte, error) {
	if len(raw) != sha256.Size*2 || strings.ToLower(raw) != raw {
		return nil, errInvalidReceiverFeedCredential
	}
	decoded, err := hex.DecodeString(raw)
	if err != nil || len(decoded) != sha256.Size {
		return nil, errInvalidReceiverFeedCredential
	}
	return decoded, nil
}

// HashReceiverFeedVerifier stores a server-secret HMAC over the decoded public
// verifier. The raw bearer token and the public verifier are never persisted.
func HashReceiverFeedVerifier(verifier []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(verifier)
	return hex.EncodeToString(mac.Sum(nil))
}

func HashReceiverFeedCredential(rawToken, secret string) (string, string, error) {
	verifier, err := ReceiverFeedVerifier(rawToken)
	if err != nil {
		return "", "", err
	}
	prefix, err := ReceiverFeedKeyPrefix(rawToken)
	if err != nil {
		return "", "", err
	}
	return HashReceiverFeedVerifier(verifier, secret), prefix, nil
}

// The same receiver-verifier primitive is domain-separated by purpose. A
// source-read bearer cannot authenticate a policy or enrollment route.
func HashXInsightMachineCredential(rawToken, secret, purpose string) (string, string, error) {
	var prefix, domain string
	switch purpose {
	case "xinsight.policy.manage":
		prefix, domain = XInsightPolicyTokenPrefix, XInsightPolicyVerifierDomain
	case "xinsight.source.enroll":
		prefix, domain = XInsightEnrollmentTokenPrefix, XInsightEnrollmentVerifierDomain
	default:
		return "", "", errInvalidReceiverFeedCredential
	}
	if len(rawToken) != len(prefix)+64 || !strings.HasPrefix(rawToken, prefix) {
		return "", "", errInvalidReceiverFeedCredential
	}
	for _, c := range rawToken[len(prefix):] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return "", "", errInvalidReceiverFeedCredential
		}
	}
	sum := sha256.Sum256([]byte(domain + "\x00" + rawToken))
	return HashReceiverFeedVerifier(sum[:], secret), rawToken[:ReceiverFeedCredentialPrefixLength], nil
}

func validReceiverFeedRawToken(raw string) bool {
	if len(raw) != len(ReceiverFeedTokenPrefix)+64 ||
		!strings.HasPrefix(raw, ReceiverFeedTokenPrefix) {
		return false
	}
	for _, character := range raw[len(ReceiverFeedTokenPrefix):] {
		if (character < '0' || character > '9') &&
			(character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
