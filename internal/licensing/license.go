package licensing

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	StatusMissing  = "missing"
	StatusActive   = "active"
	StatusExpiring = "expiring"
	StatusExpired  = "expired"
	StatusInvalid  = "invalid"
)

type Payload struct {
	Customer     string            `json:"customer"`
	DeploymentID string            `json:"deployment_id"`
	Tier         string            `json:"tier"`
	IssuedAt     string            `json:"issued_at,omitempty"`
	ExpiresAt    string            `json:"expires_at"`
	Features     []string          `json:"features,omitempty"`
	Limits       map[string]int64  `json:"limits,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type Document struct {
	Payload   Payload `json:"payload"`
	Signature string  `json:"signature"`
}

type Status struct {
	Status             string   `json:"status"`
	Valid              bool     `json:"valid"`
	SignatureValid     bool     `json:"signature_valid"`
	Warning            bool     `json:"warning"`
	DaysUntilExpiry    int      `json:"days_until_expiry"`
	CheckedAt          string   `json:"checked_at"`
	VerifierConfigured bool     `json:"verifier_configured"`
	License            *Payload `json:"license,omitempty"`
	Error              string   `json:"error,omitempty"`
}

type Verifier struct {
	publicKey   ed25519.PublicKey
	warningDays int
	now         func() time.Time
}

func NewVerifier(publicKey ed25519.PublicKey, warningDays int) *Verifier {
	if warningDays < 0 {
		warningDays = 0
	}
	return &Verifier{
		publicKey:   publicKey,
		warningDays: warningDays,
		now:         time.Now,
	}
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

func (v *Verifier) VerifyBytes(raw []byte) Status {
	checkedAt := v.now().UTC()
	status := Status{
		Status:             StatusMissing,
		CheckedAt:          checkedAt.Format(time.RFC3339),
		VerifierConfigured: len(v.publicKey) == ed25519.PublicKeySize,
		DaysUntilExpiry:    0,
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
	status.License = &doc.Payload

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

func DecodeDocument(raw []byte) (*Document, error) {
	var doc Document
	if err := json.Unmarshal(raw, &doc); err != nil {
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

func SignPayload(payload Payload, privateKey ed25519.PrivateKey) (Document, error) {
	canonical, err := canonicalPayload(payload)
	if err != nil {
		return Document{}, err
	}
	return Document{
		Payload:   payload,
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, canonical)),
	}, nil
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
