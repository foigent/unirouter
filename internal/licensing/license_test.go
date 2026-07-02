package licensing

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"
)

func testLicensePayload(expiresAt string) Payload {
	return Payload{
		Customer:     "Acme Corp",
		DeploymentID: "prod-cn-1",
		Tier:         "enterprise",
		IssuedAt:     "2026-07-01T00:00:00Z",
		ExpiresAt:    expiresAt,
		Features:     []string{"projects", "native_adapters"},
		Limits:       map[string]int64{"projects": 20},
	}
}

func signedLicenseJSON(t *testing.T, payload Payload, privateKey ed25519.PrivateKey) []byte {
	t.Helper()
	doc, err := SignPayload(payload, privateKey)
	if err != nil {
		t.Fatalf("sign payload: %v", err)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal license: %v", err)
	}
	return raw
}

func TestVerifier_ActiveLicense(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	verifier := NewVerifier(publicKey, 30).WithNow(func() time.Time {
		return time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	})

	status := verifier.VerifyBytes(signedLicenseJSON(t, testLicensePayload("2026-12-31T00:00:00Z"), privateKey))

	if status.Status != StatusActive || !status.Valid || !status.SignatureValid {
		t.Fatalf("expected active valid license, got %#v", status)
	}
	if status.License == nil || status.License.Customer != "Acme Corp" {
		t.Fatalf("expected license metadata, got %#v", status.License)
	}
}

func TestVerifier_ExpiringLicense(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	verifier := NewVerifier(publicKey, 30).WithNow(func() time.Time {
		return time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	})

	status := verifier.VerifyBytes(signedLicenseJSON(t, testLicensePayload("2026-07-10T00:00:00Z"), privateKey))

	if status.Status != StatusExpiring || !status.Valid || !status.Warning {
		t.Fatalf("expected expiring warning license, got %#v", status)
	}
}

func TestVerifier_ExpiredLicense(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	verifier := NewVerifier(publicKey, 30).WithNow(func() time.Time {
		return time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	})

	status := verifier.VerifyBytes(signedLicenseJSON(t, testLicensePayload("2026-07-01T00:00:00Z"), privateKey))

	if status.Status != StatusExpired || status.Valid || !status.SignatureValid {
		t.Fatalf("expected expired signature-valid license, got %#v", status)
	}
}

func TestVerifier_InvalidSignature(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate public key: %v", err)
	}
	_, otherPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate other key: %v", err)
	}
	verifier := NewVerifier(publicKey, 30).WithNow(func() time.Time {
		return time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	})

	status := verifier.VerifyBytes(signedLicenseJSON(t, testLicensePayload("2026-12-31T00:00:00Z"), otherPrivateKey))

	if status.Status != StatusInvalid || status.SignatureValid || status.Error == "" {
		t.Fatalf("expected invalid signature status, got %#v", status)
	}
}

func TestVerifier_PublicKeyParsing(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate public key: %v", err)
	}

	parsed, err := ParsePublicKey(base64.StdEncoding.EncodeToString(publicKey))
	if err != nil {
		t.Fatalf("parse public key: %v", err)
	}
	if string(parsed) != string(publicKey) {
		t.Fatal("parsed public key mismatch")
	}

	parsedHex, err := ParsePublicKey(hex.EncodeToString(publicKey))
	if err != nil {
		t.Fatalf("parse hex public key: %v", err)
	}
	if string(parsedHex) != string(publicKey) {
		t.Fatal("parsed hex public key mismatch")
	}
}

func TestVerifier_MissingPublicKeyRejectsConfiguredLicense(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	status := NewVerifier(nil, 30).VerifyBytes(signedLicenseJSON(t, testLicensePayload("2026-12-31T00:00:00Z"), privateKey))

	if status.Status != StatusInvalid || status.Error != "LICENSE_PUBLIC_KEY is not configured" {
		t.Fatalf("expected missing public key error, got %#v", status)
	}
}
