package licensing

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
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

func commercialPayload() Payload {
	return Payload{
		LicenseID:    "lic-acme-prod-cn-1",
		Customer:     "Acme Corp",
		DeploymentID: "prod-cn-1",
		Tier:         "enterprise",
		IssuedAt:     "2026-07-01T00:00:00Z",
		NotBefore:    "2026-07-01T00:00:00Z",
		ExpiresAt:    "2026-12-31T00:00:00Z",
		Features:     []string{FeatureGateway, FeatureAgentCapabilities},
		Limits:       map[string]int64{"active_projects": 20},
	}
}

func TestCommercialVerifier_EnforcesKeyDeploymentAndTime(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	verifier := NewCommercialVerifier(map[string]ed25519.PublicKey{"license-2026-a": publicKey}, nil, 30, "prod-cn-1").WithNow(func() time.Time {
		return time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	})
	doc, err := SignCommercialPayload(commercialPayload(), "license-2026-a", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}

	status := verifier.VerifyBytes(raw)
	if status.Status != StatusActive || !status.Valid || !status.Enforceable || !status.Commercial || !status.DeploymentMatch {
		t.Fatalf("expected enforceable commercial status, got %#v", status)
	}

	wrongDeployment := NewCommercialVerifier(map[string]ed25519.PublicKey{"license-2026-a": publicKey}, nil, 30, "prod-cn-2").WithNow(verifier.now)
	status = wrongDeployment.VerifyBytes(raw)
	if status.Status != StatusDeploymentMismatch || status.Valid || status.Enforceable || !status.SignatureValid {
		t.Fatalf("expected signed deployment mismatch, got %#v", status)
	}

	notYet := NewCommercialVerifier(map[string]ed25519.PublicKey{"license-2026-a": publicKey}, nil, 30, "prod-cn-1").WithNow(func() time.Time {
		return time.Date(2026, 6, 30, 23, 59, 0, 0, time.UTC)
	})
	status = notYet.VerifyBytes(raw)
	if status.Status != StatusNotYetValid || status.Valid || status.Enforceable || !status.SignatureValid {
		t.Fatalf("expected not-yet-valid denial, got %#v", status)
	}
}

func TestCommercialVerifier_ExpiringExpiredAndUntrustedKey(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := SignCommercialPayload(commercialPayload(), "license-2026-a", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}

	expiring := NewCommercialVerifier(map[string]ed25519.PublicKey{"license-2026-a": publicKey}, nil, 30, "prod-cn-1").WithNow(func() time.Time {
		return time.Date(2026, 12, 15, 0, 0, 0, 0, time.UTC)
	}).VerifyBytes(raw)
	if expiring.Status != StatusExpiring || !expiring.Valid || !expiring.Enforceable || !expiring.Warning {
		t.Fatalf("expected enforceable expiring commercial status, got %#v", expiring)
	}

	expired := NewCommercialVerifier(map[string]ed25519.PublicKey{"license-2026-a": publicKey}, nil, 30, "prod-cn-1").WithNow(func() time.Time {
		return time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	}).VerifyBytes(raw)
	if expired.Status != StatusExpired || expired.Valid || expired.Enforceable || !expired.SignatureValid {
		t.Fatalf("expected signature-valid expired commercial status, got %#v", expired)
	}

	otherPublicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	untrusted := NewCommercialVerifier(map[string]ed25519.PublicKey{"license-2026-b": otherPublicKey}, nil, 30, "prod-cn-1").VerifyBytes(raw)
	if untrusted.Status != StatusInvalid || untrusted.Valid || untrusted.Enforceable || untrusted.SignatureValid || !strings.Contains(untrusted.Error, "not trusted") {
		t.Fatalf("expected untrusted key rejection, got %#v", untrusted)
	}
}

func TestCommercialVerifier_RejectsTamperUnknownFieldAndLegacyEnforcement(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	verifier := NewCommercialVerifier(map[string]ed25519.PublicKey{"license-2026-a": publicKey}, publicKey, 30, "prod-cn-1").WithNow(func() time.Time {
		return time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	})
	doc, err := SignCommercialPayload(commercialPayload(), "license-2026-a", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	doc.Payload.Tier = "tampered"
	raw, _ := json.Marshal(doc)
	status := verifier.VerifyBytes(raw)
	if status.Status != StatusInvalid || status.SignatureValid || status.Enforceable {
		t.Fatalf("expected tamper rejection, got %#v", status)
	}

	validDoc, err := SignCommercialPayload(commercialPayload(), "license-2026-a", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(validDoc)
	var object map[string]interface{}
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	object["unsigned_extra"] = true
	raw, _ = json.Marshal(object)
	status = verifier.VerifyBytes(raw)
	if status.Status != StatusInvalid || status.Enforceable {
		t.Fatalf("expected strict unknown-field rejection, got %#v", status)
	}

	legacy := signedLicenseJSON(t, testLicensePayload("2026-12-31T00:00:00Z"), privateKey)
	status = verifier.VerifyBytes(legacy)
	if !status.Valid || status.Enforceable || status.Commercial {
		t.Fatalf("legacy evidence may verify but must not be commercially enforceable: %#v", status)
	}
}

func TestCommercialVerifierLegacyDocumentWithoutLegacyKeyFailsClosed(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	verifier := NewCommercialVerifier(map[string]ed25519.PublicKey{"license-2026-a": publicKey}, nil, 30, "prod-cn-1")
	legacy := signedLicenseJSON(t, testLicensePayload("2026-12-31T00:00:00Z"), privateKey)
	status := verifier.VerifyBytes(legacy)
	if status.Status != StatusInvalid || status.Valid || status.Enforceable || status.Error != "legacy LICENSE_PUBLIC_KEY is not configured" {
		t.Fatalf("legacy document without a legacy key must fail closed: %#v", status)
	}
}

func TestParsePublicKeyRing(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	raw := `{"license-2026-a":"` + base64.StdEncoding.EncodeToString(publicKey) + `"}`
	ring, err := ParsePublicKeyRing(raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(ring["license-2026-a"]) != string(publicKey) {
		t.Fatal("parsed key ring mismatch")
	}
	if _, err := ParsePublicKeyRing(`{"bad key":"abc"}`); err == nil {
		t.Fatal("expected invalid key id rejection")
	}
}

func TestCommercialLicenseRejectsUnknownEntitlements(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload := commercialPayload()
	payload.Features = []string{"typo_feature"}
	if _, err := SignCommercialPayload(payload, "license-key-a", privateKey); err == nil {
		t.Fatal("expected unknown feature rejection")
	}
	payload = commercialPayload()
	payload.Limits = map[string]int64{"typo_limit": 1}
	if _, err := SignCommercialPayload(payload, "license-key-a", privateKey); err == nil {
		t.Fatal("expected unknown limit rejection")
	}
}

func TestCommercialLicenseRejectsUnboundedIdentityAndMetadata(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload := commercialPayload()
	payload.LicenseID = strings.Repeat("x", 129)
	if _, err := SignCommercialPayload(payload, "license-key-a", privateKey); err == nil {
		t.Fatal("expected oversized license_id rejection")
	}
	payload = commercialPayload()
	payload.Metadata = map[string]string{"support": strings.Repeat("x", 513)}
	if _, err := SignCommercialPayload(payload, "license-key-a", privateKey); err == nil {
		t.Fatal("expected oversized metadata rejection")
	}
	payload = commercialPayload()
	payload.Customer = "Acme\nInjected"
	if _, err := SignCommercialPayload(payload, "license-key-a", privateKey); err == nil {
		t.Fatal("expected control-character identity rejection")
	}
}
