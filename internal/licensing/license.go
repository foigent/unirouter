package licensing

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"
)

const (
	CommercialSchemaVersion = "unirouter.license.v1"

	StatusMissing            = "missing"
	StatusActive             = "active"
	StatusExpiring           = "expiring"
	StatusExpired            = "expired"
	StatusInvalid            = "invalid"
	StatusNotYetValid        = "not_yet_valid"
	StatusDeploymentMismatch = "deployment_mismatch"
)

type Payload struct {
	LicenseID    string            `json:"license_id,omitempty"`
	Customer     string            `json:"customer"`
	DeploymentID string            `json:"deployment_id"`
	Tier         string            `json:"tier"`
	IssuedAt     string            `json:"issued_at,omitempty"`
	NotBefore    string            `json:"not_before,omitempty"`
	ExpiresAt    string            `json:"expires_at"`
	Features     []string          `json:"features,omitempty"`
	Limits       map[string]int64  `json:"limits,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type Document struct {
	SchemaVersion string  `json:"schema_version,omitempty"`
	KeyID         string  `json:"key_id,omitempty"`
	Payload       Payload `json:"payload"`
	Signature     string  `json:"signature"`
}

type Status struct {
	Status               string   `json:"status"`
	Valid                bool     `json:"valid"`
	Enforceable          bool     `json:"enforceable"`
	Commercial           bool     `json:"commercial"`
	SignatureValid       bool     `json:"signature_valid"`
	DeploymentMatch      bool     `json:"deployment_match"`
	Warning              bool     `json:"warning"`
	DaysUntilExpiry      int      `json:"days_until_expiry"`
	CheckedAt            string   `json:"checked_at"`
	VerifierConfigured   bool     `json:"verifier_configured"`
	SchemaVersion        string   `json:"schema_version,omitempty"`
	KeyID                string   `json:"key_id,omitempty"`
	ExpectedDeploymentID string   `json:"expected_deployment_id,omitempty"`
	License              *Payload `json:"license,omitempty"`
	Error                string   `json:"error,omitempty"`
}

type Verifier struct {
	publicKey            ed25519.PublicKey
	publicKeys           map[string]ed25519.PublicKey
	expectedDeploymentID string
	warningDays          int
	now                  func() time.Time
}

func NewVerifier(publicKey ed25519.PublicKey, warningDays int) *Verifier {
	if warningDays < 0 {
		warningDays = 0
	}
	return &Verifier{
		publicKey:   append(ed25519.PublicKey(nil), publicKey...),
		warningDays: warningDays,
		now:         time.Now,
	}
}

// NewCommercialVerifier builds the V1 runtime verifier. The key ring is copied
// so callers cannot mutate active trust after construction.
func NewCommercialVerifier(publicKeys map[string]ed25519.PublicKey, legacyPublicKey ed25519.PublicKey, warningDays int, expectedDeploymentID string) *Verifier {
	verifier := NewVerifier(legacyPublicKey, warningDays)
	verifier.publicKeys = make(map[string]ed25519.PublicKey, len(publicKeys))
	for keyID, key := range publicKeys {
		verifier.publicKeys[keyID] = append(ed25519.PublicKey(nil), key...)
	}
	verifier.expectedDeploymentID = strings.TrimSpace(expectedDeploymentID)
	return verifier
}

func NewVerifierFromPublicKey(publicKeyText string, warningDays int) (*Verifier, error) {
	publicKey, err := ParsePublicKey(publicKeyText)
	if err != nil {
		return nil, err
	}
	return NewVerifier(publicKey, warningDays), nil
}

func (v *Verifier) WithNow(now func() time.Time) *Verifier {
	if now != nil {
		v.now = now
	}
	return v
}

func ParsePublicKey(value string) (ed25519.PublicKey, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}

	var lengthErr error
	for _, decoder := range []struct {
		name string
		fn   func(string) ([]byte, error)
	}{
		{"base64", base64.StdEncoding.DecodeString},
		{"raw_base64", base64.RawStdEncoding.DecodeString},
		{"hex", hex.DecodeString},
	} {
		decoded, err := decoder.fn(value)
		if err == nil {
			if len(decoded) == ed25519.PublicKeySize {
				return ed25519.PublicKey(decoded), nil
			}
			lengthErr = fmt.Errorf("LICENSE_PUBLIC_KEY %s decoded to %d bytes, want %d", decoder.name, len(decoded), ed25519.PublicKeySize)
		}
	}
	if lengthErr != nil {
		return nil, lengthErr
	}
	return nil, fmt.Errorf("LICENSE_PUBLIC_KEY must be base64 or hex encoded Ed25519 public key")
}

// ParsePublicKeyRing accepts a JSON object mapping stable key IDs to Ed25519
// public keys. Empty input produces an empty ring.
func ParsePublicKeyRing(value string) (map[string]ed25519.PublicKey, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return map[string]ed25519.PublicKey{}, nil
	}
	var encoded map[string]string
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&encoded); err != nil {
		return nil, fmt.Errorf("LICENSE_PUBLIC_KEYS must be a JSON object: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, fmt.Errorf("LICENSE_PUBLIC_KEYS must contain one JSON object: %w", err)
	}
	if len(encoded) == 0 {
		return nil, fmt.Errorf("LICENSE_PUBLIC_KEYS must contain at least one key")
	}
	if len(encoded) > 32 {
		return nil, fmt.Errorf("LICENSE_PUBLIC_KEYS must contain at most 32 keys")
	}
	ring := make(map[string]ed25519.PublicKey, len(encoded))
	for rawKeyID, rawKey := range encoded {
		keyID := strings.TrimSpace(rawKeyID)
		if !validKeyID(keyID) {
			return nil, fmt.Errorf("LICENSE_PUBLIC_KEYS contains invalid key id %q", rawKeyID)
		}
		parsed, err := ParsePublicKey(rawKey)
		if err != nil {
			return nil, fmt.Errorf("LICENSE_PUBLIC_KEYS[%s]: %w", keyID, err)
		}
		ring[keyID] = parsed
	}
	return ring, nil
}

func (v *Verifier) VerifyBytes(raw []byte) Status {
	checkedAt := v.now().UTC()
	status := Status{
		Status:               StatusMissing,
		CheckedAt:            checkedAt.Format(time.RFC3339),
		VerifierConfigured:   len(v.publicKey) == ed25519.PublicKeySize || len(v.publicKeys) > 0,
		DaysUntilExpiry:      0,
		ExpectedDeploymentID: v.expectedDeploymentID,
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return status
	}
	if !status.VerifierConfigured {
		status.Status = StatusInvalid
		status.Error = "LICENSE_PUBLIC_KEY is not configured"
		return status
	}

	doc, err := DecodeDocument(raw)
	if err != nil {
		status.Status = StatusInvalid
		status.Error = err.Error()
		return status
	}
	status.SchemaVersion = doc.SchemaVersion
	status.KeyID = doc.KeyID
	status.License = &doc.Payload
	if doc.SchemaVersion == CommercialSchemaVersion {
		return v.verifyCommercialDocument(doc, status, checkedAt)
	}
	if len(v.publicKey) != ed25519.PublicKeySize {
		status.Status = StatusInvalid
		status.Error = "legacy LICENSE_PUBLIC_KEY is not configured"
		return status
	}

	canonical, err := canonicalPayload(doc.Payload)
	if err != nil {
		status.Status = StatusInvalid
		status.Error = err.Error()
		return status
	}
	signature, err := decodeSignature(doc.Signature)
	if err != nil {
		status.Status = StatusInvalid
		status.Error = err.Error()
		return status
	}
	if !ed25519.Verify(v.publicKey, canonical, signature) {
		status.Status = StatusInvalid
		status.Error = "license signature verification failed"
		return status
	}

	status.SignatureValid = true
	expiry, err := time.Parse(time.RFC3339, doc.Payload.ExpiresAt)
	if err != nil {
		status.Status = StatusInvalid
		status.Error = "expires_at must be RFC3339"
		return status
	}

	duration := expiry.UTC().Sub(checkedAt)
	status.DaysUntilExpiry = int(duration.Hours() / 24)
	if duration < 0 {
		status.Status = StatusExpired
		status.Valid = false
		return status
	}
	if duration <= time.Duration(v.warningDays)*24*time.Hour {
		status.Status = StatusExpiring
		status.Valid = true
		status.Warning = true
		return status
	}
	status.Status = StatusActive
	status.Valid = true
	return status
}

func (v *Verifier) verifyCommercialDocument(doc *Document, status Status, checkedAt time.Time) Status {
	status.Commercial = true
	if strings.TrimSpace(v.expectedDeploymentID) == "" {
		status.Status = StatusInvalid
		status.Error = "LICENSE_DEPLOYMENT_ID is not configured"
		return status
	}
	key, ok := v.publicKeys[doc.KeyID]
	if !ok || len(key) != ed25519.PublicKeySize {
		status.Status = StatusInvalid
		status.Error = "license signing key is not trusted"
		return status
	}
	canonical, err := canonicalCommercialDocument(*doc)
	if err != nil {
		status.Status = StatusInvalid
		status.Error = err.Error()
		return status
	}
	signature, err := decodeSignature(doc.Signature)
	if err != nil {
		status.Status = StatusInvalid
		status.Error = err.Error()
		return status
	}
	if !ed25519.Verify(key, canonical, signature) {
		status.Status = StatusInvalid
		status.Error = "license signature verification failed"
		return status
	}
	status.SignatureValid = true
	status.DeploymentMatch = doc.Payload.DeploymentID == v.expectedDeploymentID
	if !status.DeploymentMatch {
		status.Status = StatusDeploymentMismatch
		status.Error = "license deployment_id does not match this deployment"
		return status
	}
	notBefore, err := time.Parse(time.RFC3339, doc.Payload.NotBefore)
	if err != nil {
		status.Status = StatusInvalid
		status.Error = "not_before must be RFC3339"
		return status
	}
	if checkedAt.Before(notBefore.UTC()) {
		status.Status = StatusNotYetValid
		status.Error = "license is not valid yet"
		return status
	}
	expiry, err := time.Parse(time.RFC3339, doc.Payload.ExpiresAt)
	if err != nil {
		status.Status = StatusInvalid
		status.Error = "expires_at must be RFC3339"
		return status
	}
	duration := expiry.UTC().Sub(checkedAt)
	status.DaysUntilExpiry = int(duration.Hours() / 24)
	if duration <= 0 {
		status.Status = StatusExpired
		return status
	}
	status.Valid = true
	status.Enforceable = true
	if duration <= time.Duration(v.warningDays)*24*time.Hour {
		status.Status = StatusExpiring
		status.Warning = true
		return status
	}
	status.Status = StatusActive
	return status
}

func DecodeDocument(raw []byte) (*Document, error) {
	var probe struct {
		SchemaVersion string `json:"schema_version"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("license must be valid JSON: %w", err)
	}
	var doc Document
	if probe.SchemaVersion == CommercialSchemaVersion {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&doc); err != nil {
			return nil, fmt.Errorf("commercial license must match the strict schema: %w", err)
		}
		if err := requireJSONEOF(decoder); err != nil {
			return nil, fmt.Errorf("commercial license must contain one JSON object: %w", err)
		}
	} else if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("license must be valid JSON: %w", err)
	}
	if strings.TrimSpace(doc.Payload.Customer) == "" {
		return nil, fmt.Errorf("license payload customer is required")
	}
	if strings.TrimSpace(doc.Payload.DeploymentID) == "" {
		return nil, fmt.Errorf("license payload deployment_id is required")
	}
	if strings.TrimSpace(doc.Payload.Tier) == "" {
		return nil, fmt.Errorf("license payload tier is required")
	}
	if strings.TrimSpace(doc.Payload.ExpiresAt) == "" {
		return nil, fmt.Errorf("license payload expires_at is required")
	}
	if strings.TrimSpace(doc.Signature) == "" {
		return nil, fmt.Errorf("license signature is required")
	}
	if doc.SchemaVersion != "" && doc.SchemaVersion != CommercialSchemaVersion {
		return nil, fmt.Errorf("unsupported license schema_version %q", doc.SchemaVersion)
	}
	if doc.SchemaVersion == CommercialSchemaVersion {
		if !validKeyID(doc.KeyID) {
			return nil, fmt.Errorf("commercial license key_id is invalid")
		}
		if err := validateCommercialPayload(doc.Payload); err != nil {
			return nil, err
		}
	}
	return &doc, nil
}

func NormalizeDocument(raw []byte) ([]byte, error) {
	doc, err := DecodeDocument(raw)
	if err != nil {
		return nil, err
	}
	normalized, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	return normalized, nil
}

func canonicalPayload(payload Payload) ([]byte, error) {
	if strings.TrimSpace(payload.Customer) == "" ||
		strings.TrimSpace(payload.DeploymentID) == "" ||
		strings.TrimSpace(payload.Tier) == "" ||
		strings.TrimSpace(payload.ExpiresAt) == "" {
		return nil, fmt.Errorf("license payload is missing required fields")
	}
	if _, err := time.Parse(time.RFC3339, payload.ExpiresAt); err != nil {
		return nil, fmt.Errorf("expires_at must be RFC3339")
	}
	return json.Marshal(payload)
}

type commercialSignedContent struct {
	SchemaVersion string  `json:"schema_version"`
	KeyID         string  `json:"key_id"`
	Payload       Payload `json:"payload"`
}

func canonicalCommercialDocument(doc Document) ([]byte, error) {
	if doc.SchemaVersion != CommercialSchemaVersion {
		return nil, fmt.Errorf("unsupported commercial license schema")
	}
	if !validKeyID(doc.KeyID) {
		return nil, fmt.Errorf("commercial license key_id is invalid")
	}
	if err := validateCommercialPayload(doc.Payload); err != nil {
		return nil, err
	}
	return json.Marshal(commercialSignedContent{
		SchemaVersion: doc.SchemaVersion,
		KeyID:         doc.KeyID,
		Payload:       doc.Payload,
	})
}

func validateCommercialPayload(payload Payload) error {
	if strings.TrimSpace(payload.LicenseID) == "" || strings.TrimSpace(payload.LicenseID) != payload.LicenseID || len(payload.LicenseID) > 128 || containsControl(payload.LicenseID) {
		return fmt.Errorf("commercial license payload license_id must be normalized and at most 128 bytes")
	}
	if strings.TrimSpace(payload.Customer) == "" || strings.TrimSpace(payload.Customer) != payload.Customer || len(payload.Customer) > 256 || containsControl(payload.Customer) ||
		strings.TrimSpace(payload.DeploymentID) == "" || strings.TrimSpace(payload.DeploymentID) != payload.DeploymentID || len(payload.DeploymentID) > 128 || containsControl(payload.DeploymentID) ||
		strings.TrimSpace(payload.Tier) == "" || strings.TrimSpace(payload.Tier) != payload.Tier || len(payload.Tier) > 64 || containsControl(payload.Tier) {
		return fmt.Errorf("commercial license customer, deployment_id, and tier must be normalized and within supported bounds")
	}
	issuedAt, err := time.Parse(time.RFC3339, payload.IssuedAt)
	if err != nil {
		return fmt.Errorf("issued_at must be RFC3339")
	}
	notBefore, err := time.Parse(time.RFC3339, payload.NotBefore)
	if err != nil {
		return fmt.Errorf("not_before must be RFC3339")
	}
	expiresAt, err := time.Parse(time.RFC3339, payload.ExpiresAt)
	if err != nil {
		return fmt.Errorf("expires_at must be RFC3339")
	}
	if notBefore.Before(issuedAt) {
		return fmt.Errorf("not_before must not precede issued_at")
	}
	if !expiresAt.After(notBefore) {
		return fmt.Errorf("expires_at must be after not_before")
	}
	if len(payload.Features) == 0 {
		return fmt.Errorf("commercial license payload features is required")
	}
	if len(payload.Features) > 32 {
		return fmt.Errorf("commercial license payload has too many features")
	}
	seenFeatures := make(map[string]bool, len(payload.Features))
	for _, feature := range payload.Features {
		normalized := strings.TrimSpace(feature)
		if normalized == "" || normalized != feature || seenFeatures[feature] || !supportedFeatures[feature] {
			return fmt.Errorf("commercial license features must be supported unique normalized values")
		}
		seenFeatures[feature] = true
	}
	for name, limit := range payload.Limits {
		if strings.TrimSpace(name) != name || !supportedLimits[name] || limit <= 0 {
			return fmt.Errorf("commercial license limits must use supported normalized names and positive values")
		}
	}
	if len(payload.Metadata) > 32 {
		return fmt.Errorf("commercial license metadata has too many entries")
	}
	for key, value := range payload.Metadata {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(key) != key || len(key) > 64 || len(value) > 512 || containsControl(key) || containsControl(value) {
			return fmt.Errorf("commercial license metadata keys and values exceed the supported bounds")
		}
	}
	return nil
}

func containsControl(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func SignPayload(payload Payload, privateKey ed25519.PrivateKey) (Document, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return Document{}, fmt.Errorf("private key must be an Ed25519 private key")
	}
	canonical, err := canonicalPayload(payload)
	if err != nil {
		return Document{}, err
	}
	return Document{
		Payload:   payload,
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, canonical)),
	}, nil
}

func SignCommercialPayload(payload Payload, keyID string, privateKey ed25519.PrivateKey) (Document, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return Document{}, fmt.Errorf("private key must be an Ed25519 private key")
	}
	doc := Document{
		SchemaVersion: CommercialSchemaVersion,
		KeyID:         strings.TrimSpace(keyID),
		Payload:       payload,
	}
	canonical, err := canonicalCommercialDocument(doc)
	if err != nil {
		return Document{}, err
	}
	doc.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, canonical))
	return doc, nil
}

func validKeyID(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra interface{}
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}
	return fmt.Errorf("unexpected trailing JSON value")
}

func decodeSignature(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	var lengthErr error
	for _, decoder := range []func(string) ([]byte, error){
		base64.StdEncoding.DecodeString,
		base64.RawStdEncoding.DecodeString,
		hex.DecodeString,
	} {
		decoded, err := decoder(value)
		if err == nil {
			if len(decoded) == ed25519.SignatureSize {
				return decoded, nil
			}
			lengthErr = fmt.Errorf("license signature decoded to %d bytes, want %d", len(decoded), ed25519.SignatureSize)
		}
	}
	if lengthErr != nil {
		return nil, lengthErr
	}
	return nil, fmt.Errorf("license signature must be base64 or hex encoded")
}
