package sourcefacts

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"math"
)

const contentCursorDomain = "unirouter.source-content-cursor.v1\x00"
const contentCursorVersion byte = 3

var (
	ErrContentInvalidCursor = errors.New("invalid_cursor")
	ErrContentScopeChanged  = errors.New("content_scope_changed")
	ErrContentEpochChanged  = errors.New("source_epoch_changed")
	ErrContentCursorExpired = errors.New("cursor_expired")
)

// ContentCursorScope is an authorization snapshot. RecoveryEpoch comes from the
// content stream's single authoritative row, not a second scope-local epoch.
type ContentCursorScope struct {
	SourceInstance   string
	OrgID            int64
	ProjectID        int64
	Profile          string
	AccessGeneration int64
	RecoveryEpoch    int64
}

func (s ContentCursorScope) validate() error {
	if !contentText(s.SourceInstance, 128) || s.Profile != ContentProfile || s.OrgID <= 0 || s.ProjectID <= 0 || s.AccessGeneration <= 0 || s.RecoveryEpoch <= 0 {
		return ErrContentInvalidCursor
	}
	return nil
}
func contentCursorBinding(s ContentCursorScope) [32]byte {
	h := sha256.New()
	h.Write([]byte(contentCursorDomain))
	for _, part := range []string{s.SourceInstance, s.Profile} {
		var n [4]byte
		binary.BigEndian.PutUint32(n[:], uint32(len(part)))
		h.Write(n[:])
		h.Write([]byte(part))
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}
func EncodeContentCursor(secret []byte, scope ContentCursorScope, after int64) (string, error) {
	if len(secret) == 0 || scope.validate() != nil || after < 0 {
		return "", ErrContentInvalidCursor
	}
	wire := make([]byte, 73)
	wire[0] = contentCursorVersion
	binding := contentCursorBinding(scope)
	copy(wire[1:33], binding[:])
	for i, v := range []int64{scope.OrgID, scope.ProjectID, scope.AccessGeneration, scope.RecoveryEpoch, after} {
		binary.BigEndian.PutUint64(wire[33+i*8:], uint64(v))
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(contentCursorDomain))
	mac.Write(wire)
	return base64.RawURLEncoding.EncodeToString(append(wire, mac.Sum(nil)...)), nil
}

// DecodeContentCursor authenticates first, then distinguishes generation changes,
// restore changes, and a cleaned retention prefix. The position is exclusive.
func DecodeContentCursor(secret []byte, raw string, scope ContentCursorScope, retentionFloor int64) (int64, error) {
	if len(secret) == 0 || scope.validate() != nil || retentionFloor < 0 || len(raw) > 512 {
		return 0, ErrContentInvalidCursor
	}
	wire, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	if err != nil || len(wire) != 105 || wire[0] != contentCursorVersion || base64.RawURLEncoding.EncodeToString(wire) != raw {
		return 0, ErrContentInvalidCursor
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(contentCursorDomain))
	mac.Write(wire[:73])
	if !hmac.Equal(wire[73:], mac.Sum(nil)) {
		return 0, ErrContentInvalidCursor
	}
	binding := contentCursorBinding(scope)
	if !hmac.Equal(wire[1:33], binding[:]) {
		return 0, ErrContentInvalidCursor
	}
	values := [5]int64{}
	for i := range values {
		v := binary.BigEndian.Uint64(wire[33+i*8:])
		if v > math.MaxInt64 || (i < 4 && v == 0) {
			return 0, ErrContentInvalidCursor
		}
		values[i] = int64(v)
	}
	if values[0] != scope.OrgID || values[1] != scope.ProjectID {
		return 0, ErrContentInvalidCursor
	}
	if values[2] != scope.AccessGeneration {
		return 0, ErrContentScopeChanged
	}
	if values[3] != scope.RecoveryEpoch {
		return 0, ErrContentEpochChanged
	}
	if values[4] < retentionFloor {
		return 0, ErrContentCursorExpired
	}
	return values[4], nil
}
