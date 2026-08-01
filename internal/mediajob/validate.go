package mediajob

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode"

	"github.com/uniroute/unirouter/internal/eventconnector"
)

const (
	MaxRequestBytes = 64 * 1024
	MaxPromptBytes  = 32_000
)

func DecodeSubmitRequest(body []byte) (NormalizedRequest, error) {
	if len(body) == 0 || len(body) > MaxRequestBytes {
		return NormalizedRequest{}, domainError(ErrInvalid, "request_size_invalid")
	}
	if strictErr := eventconnector.ValidateStrictJSON(
		body,
		eventconnector.StrictJSONOptions{RejectNull: true},
	); strictErr != nil {
		if strictErr.Code == "duplicate_json_key" {
			return NormalizedRequest{}, domainError(ErrInvalid, "duplicate_json_key")
		}
		return NormalizedRequest{}, domainError(ErrInvalid, "request_json_invalid")
	}
	var request SubmitRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return NormalizedRequest{}, domainError(ErrInvalid, "request_shape_invalid")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return NormalizedRequest{}, domainError(ErrInvalid, "request_trailing_json")
	}
	return NormalizeSubmitRequest(request)
}

func NormalizeSubmitRequest(request SubmitRequest) (NormalizedRequest, error) {
	request.Model = strings.TrimSpace(request.Model)
	if request.Model == "" || len(request.Model) > 200 || containsControl(request.Model) {
		return NormalizedRequest{}, domainError(ErrInvalid, "model_invalid")
	}
	if strings.TrimSpace(request.Prompt) == "" ||
		len(request.Prompt) > MaxPromptBytes ||
		containsDisallowedControl(request.Prompt) {
		return NormalizedRequest{}, domainError(ErrInvalid, "prompt_invalid")
	}
	if request.N != nil && *request.N != 1 {
		return NormalizedRequest{}, domainError(ErrInvalid, "multiple_images_unsupported")
	}
	if request.Stream != nil && *request.Stream {
		return NormalizedRequest{}, domainError(ErrInvalid, "streaming_unsupported")
	}
	if !oneOfOrEmpty(request.Size, "auto", "1024x1024", "1536x1024", "1024x1536") {
		return NormalizedRequest{}, domainError(ErrInvalid, "size_invalid")
	}
	if !oneOfOrEmpty(request.Quality, "auto", "low", "medium", "high") {
		return NormalizedRequest{}, domainError(ErrInvalid, "quality_invalid")
	}
	if !oneOfOrEmpty(request.Background, "auto", "transparent", "opaque") {
		return NormalizedRequest{}, domainError(ErrInvalid, "background_invalid")
	}
	if !oneOfOrEmpty(request.OutputFormat, "png", "jpeg", "webp") {
		return NormalizedRequest{}, domainError(ErrInvalid, "output_format_invalid")
	}
	return NormalizedRequest{
		Model: request.Model, Prompt: request.Prompt, N: 1,
		Size: request.Size, Quality: request.Quality,
		Background: request.Background, OutputFormat: request.OutputFormat,
		Stream: false,
	}, nil
}

func ValidateIdempotencyKey(value string) error {
	if value == "" || value != strings.TrimSpace(value) || len(value) > 255 {
		return domainError(ErrInvalid, "idempotency_key_invalid")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return domainError(ErrInvalid, "idempotency_key_invalid")
		}
	}
	return nil
}

func RequestSHA256(request NormalizedRequest) (string, []byte, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return "", nil, domainError(ErrInvalid, "request_encoding_failed")
	}
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:]), body, nil
}

func IdempotencyHash(secret []byte, scope Scope, apiKeyID int64, value string) (string, error) {
	if len(secret) < 32 || !scope.Valid() || apiKeyID <= 0 {
		return "", domainError(ErrUnavailable, "idempotency_authority_unavailable")
	}
	if err := ValidateIdempotencyKey(value); err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte("unirouter.async-media.idempotency.v1\x00"))
	for _, part := range [][]byte{
		int64Bytes(scope.OrgID),
		int64Bytes(scope.ProjectID),
		int64Bytes(apiKeyID),
		[]byte(value),
	} {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(part)))
		_, _ = mac.Write(length[:])
		_, _ = mac.Write(part)
	}
	return "hmac-sha256:" + hex.EncodeToString(mac.Sum(nil)), nil
}

func NewJobID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", domainError(ErrUnavailable, "job_identity_unavailable")
	}
	return "mediajob_" + hex.EncodeToString(value[:]), nil
}

func ValidJobID(value string) bool {
	if len(value) != len("mediajob_")+32 ||
		!strings.HasPrefix(value, "mediajob_") {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "mediajob_"))
	return err == nil
}

func int64Bytes(value int64) []byte {
	var body [8]byte
	binary.BigEndian.PutUint64(body[:], uint64(value))
	return body[:]
}

func oneOfOrEmpty(value string, options ...string) bool {
	if value == "" {
		return true
	}
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
}

func containsControl(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func containsDisallowedControl(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return true
		}
	}
	return false
}
