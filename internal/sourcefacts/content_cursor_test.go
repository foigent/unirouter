package sourcefacts

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"testing"
)

func TestContentCursorBindingsAndRecovery(t *testing.T) {
	secret := []byte("synthetic-test-only-not-a-credential")
	scope := ContentCursorScope{SourceInstance: "synthetic-ur", OrgID: 1, ProjectID: 2, Profile: ContentProfile, AccessGeneration: 3, RecoveryEpoch: 4}
	token, err := EncodeContentCursor(secret, scope, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(token) > 512 || strings.ContainsAny(token, "=.") {
		t.Fatal("cursor outside C0 wire")
	}
	if after, err := DecodeContentCursor(secret, token, scope, 10); err != nil || after != 10 {
		t.Fatalf("floor inclusive position: %d %v", after, err)
	}
	if _, err := DecodeContentCursor(secret, token, scope, 11); !errors.Is(err, ErrContentCursorExpired) {
		t.Fatalf("retention: %v", err)
	}
	for name, tc := range map[string]struct {
		change func(*ContentCursorScope)
		want   error
	}{
		"source":     {func(s *ContentCursorScope) { s.SourceInstance = "other" }, ErrContentInvalidCursor},
		"org":        {func(s *ContentCursorScope) { s.OrgID++ }, ErrContentInvalidCursor},
		"project":    {func(s *ContentCursorScope) { s.ProjectID++ }, ErrContentInvalidCursor},
		"profile":    {func(s *ContentCursorScope) { s.Profile = ContextProfile }, ErrContentInvalidCursor},
		"generation": {func(s *ContentCursorScope) { s.AccessGeneration++ }, ErrContentScopeChanged},
		"epoch":      {func(s *ContentCursorScope) { s.RecoveryEpoch++ }, ErrContentEpochChanged},
	} {
		t.Run(name, func(t *testing.T) {
			s := scope
			tc.change(&s)
			if _, err := DecodeContentCursor(secret, token, s, 0); !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
		})
	}
	if _, err := DecodeContentCursor([]byte("wrong-synthetic-secret"), token, scope, 0); !errors.Is(err, ErrContentInvalidCursor) {
		t.Fatal(err)
	}
	for _, raw := range []string{"", token + "=", token + "\n", strings.Repeat("a", 513)} {
		if _, err := DecodeContentCursor(secret, raw, scope, 0); err == nil {
			t.Fatal("accepted malformed token")
		}
	}
	bootstrap, err := EncodeContentCursor(secret, scope, 0)
	if err != nil {
		t.Fatal(err)
	}
	if after, err := DecodeContentCursor(secret, bootstrap, scope, 0); err != nil || after != 0 {
		t.Fatalf("bootstrap: %d %v", after, err)
	}
	if _, err := EncodeContentCursor(nil, scope, 0); err == nil {
		t.Fatal("unsigned cursor")
	}
}
func TestContentCursorRejectsLegacyAndSignedMalformed(t *testing.T) {
	secret := []byte("synthetic-test-only-not-a-credential")
	scope := ContentCursorScope{SourceInstance: "synthetic-ur", OrgID: 1, ProjectID: 2, Profile: ContentProfile, AccessGeneration: 3, RecoveryEpoch: 4}
	token, _ := EncodeContentCursor(secret, scope, 0)
	original, _ := base64.RawURLEncoding.DecodeString(token)
	for _, version := range []byte{1, 2, 4} {
		wire := append([]byte{}, original...)
		wire[0] = version
		mac := hmac.New(sha256.New, secret)
		mac.Write([]byte(contentCursorDomain))
		mac.Write(wire[:73])
		copy(wire[73:], mac.Sum(nil))
		if _, err := DecodeContentCursor(secret, base64.RawURLEncoding.EncodeToString(wire), scope, 0); !errors.Is(err, ErrContentInvalidCursor) {
			t.Fatal("accepted non-C version")
		}
	}
	for _, offset := range []int{33, 41, 49, 57, 65} {
		wire := append([]byte{}, original...)
		binary.BigEndian.PutUint64(wire[offset:], uint64(math.MaxInt64)+1)
		mac := hmac.New(sha256.New, secret)
		mac.Write([]byte(contentCursorDomain))
		mac.Write(wire[:73])
		copy(wire[73:], mac.Sum(nil))
		if _, err := DecodeContentCursor(secret, base64.RawURLEncoding.EncodeToString(wire), scope, 0); !errors.Is(err, ErrContentInvalidCursor) {
			t.Fatal("accepted integer overflow")
		}
	}
	// Even an otherwise well-formed C wire cannot be signed in A/B/D domains.
	for _, domain := range []string{"unirouter.source-cursor.v1\x00", "unirouter.source-observation-cursor.v1\x00", "unirouter.source-budget-cursor.v1\x00"} {
		wire := append([]byte{}, original...)
		mac := hmac.New(sha256.New, secret)
		mac.Write([]byte(domain))
		mac.Write(wire[:73])
		copy(wire[73:], mac.Sum(nil))
		if _, err := DecodeContentCursor(secret, base64.RawURLEncoding.EncodeToString(wire), scope, 0); !errors.Is(err, ErrContentInvalidCursor) {
			t.Fatal("accepted foreign domain")
		}
	}
}
