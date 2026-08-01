package mediajob

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDecodeSubmitRequestStrictImageV1(t *testing.T) {
	body := []byte(`{"model":"image-prod","prompt":"draw a calm control room","size":"1024x1024","quality":"high","output_format":"png"}`)
	request, err := DecodeSubmitRequest(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if request.Model != "image-prod" || request.N != 1 || request.Stream {
		t.Fatalf("unexpected normalized request: %+v", request)
	}

	cases := []struct {
		name string
		body string
		code string
	}{
		{"duplicate", `{"model":"m","model":"n","prompt":"p"}`, "duplicate_json_key"},
		{"unknown", `{"model":"m","prompt":"p","callback":"https://example.test"}`, "request_shape_invalid"},
		{"null", `{"model":"m","prompt":null}`, "request_json_invalid"},
		{"multiple", `{"model":"m","prompt":"p","n":2}`, "multiple_images_unsupported"},
		{"stream", `{"model":"m","prompt":"p","stream":true}`, "streaming_unsupported"},
		{"format", `{"model":"m","prompt":"p","output_format":"svg"}`, "output_format_invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeSubmitRequest([]byte(tc.body))
			var contractErr *Error
			if !errors.As(err, &contractErr) || contractErr.Code != tc.code {
				t.Fatalf("error = %v, want %s", err, tc.code)
			}
		})
	}
}

func TestIdentityBindsTenantKeyAndPayload(t *testing.T) {
	secret := []byte(strings.Repeat("s", 32))
	scope := Scope{OrgID: 4, ProjectID: 9}
	first, err := IdempotencyHash(secret, scope, 12, "client-op-1")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	repeated, _ := IdempotencyHash(secret, scope, 12, "client-op-1")
	otherTenant, _ := IdempotencyHash(secret, Scope{OrgID: 5, ProjectID: 9}, 12, "client-op-1")
	otherKey, _ := IdempotencyHash(secret, scope, 13, "client-op-1")
	if first != repeated || first == otherTenant || first == otherKey {
		t.Fatalf("identity binding failed: %s %s %s %s", first, repeated, otherTenant, otherKey)
	}

	request, err := NormalizeSubmitRequest(SubmitRequest{Model: "image-prod", Prompt: "hello"})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	digest, body, err := RequestSHA256(request)
	if err != nil || !strings.HasPrefix(digest, "sha256:") || !json.Valid(body) {
		t.Fatalf("request digest = %s body=%s err=%v", digest, body, err)
	}
}

func TestJobAndCursorIdentityValidation(t *testing.T) {
	valid := "mediajob_0123456789abcdef0123456789abcdef"
	if !ValidJobID(valid) ||
		ValidJobID("mediajob_0123456789abcdef0123456789abcdeg") ||
		ValidJobID("job_0123456789abcdef0123456789abcdef") {
		t.Fatal("job identity validation drifted")
	}
	cursor := encodeListCursor(
		time.Date(2026, 7, 31, 1, 2, 3, 456000000, time.UTC),
		valid,
	)
	createdAt, jobID, err := DecodeListCursor(cursor)
	if err != nil || jobID != valid || createdAt.Nanosecond() != 456000000 {
		t.Fatalf("cursor = %s %s err=%v", createdAt, jobID, err)
	}
}
