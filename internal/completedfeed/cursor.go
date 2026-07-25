package completedfeed

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

const (
	cursorVersion     = byte(1)
	cursorPayloadSize = 17
	cursorDigestSize  = sha256.Size
)

var ErrInvalidCursor = errors.New("completed-execution feed cursor is invalid")

type CursorCodec struct {
	key            []byte
	sourceInstance string
}

func NewCursorCodec(secret, sourceInstance string) (*CursorCodec, error) {
	if secret == "" {
		return nil, errors.New("completed-execution cursor secret is required")
	}
	if err := validateBoundedText("source_instance", sourceInstance, 1, 128); err != nil {
		return nil, err
	}
	return &CursorCodec{
		key:            []byte(secret),
		sourceInstance: sourceInstance,
	}, nil
}

func (c *CursorCodec) Encode(projectID, sequence int64) (string, error) {
	if c == nil || len(c.key) == 0 || projectID <= 0 || sequence < 0 {
		return "", ErrInvalidCursor
	}
	payload := make([]byte, cursorPayloadSize)
	payload[0] = cursorVersion
	binary.BigEndian.PutUint64(payload[1:9], uint64(projectID))
	binary.BigEndian.PutUint64(payload[9:17], uint64(sequence))
	digest := c.sign(payload)
	token := make([]byte, 0, len(payload)+len(digest))
	token = append(token, payload...)
	token = append(token, digest...)
	encoded := base64.RawURLEncoding.EncodeToString(token)
	if len(encoded) > MaxCursorBytes {
		return "", ErrInvalidCursor
	}
	return encoded, nil
}

func (c *CursorCodec) Decode(encoded string, expectedProjectID int64) (int64, error) {
	if c == nil || len(c.key) == 0 || expectedProjectID <= 0 ||
		encoded == "" || len(encoded) > MaxCursorBytes {
		return 0, ErrInvalidCursor
	}
	token, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(token) != cursorPayloadSize+cursorDigestSize {
		return 0, ErrInvalidCursor
	}
	payload := token[:cursorPayloadSize]
	digest := token[cursorPayloadSize:]
	if payload[0] != cursorVersion || !hmac.Equal(digest, c.sign(payload)) {
		return 0, ErrInvalidCursor
	}
	projectID := binary.BigEndian.Uint64(payload[1:9])
	sequence := binary.BigEndian.Uint64(payload[9:17])
	if projectID != uint64(expectedProjectID) || sequence > math.MaxInt64 {
		return 0, ErrInvalidCursor
	}
	return int64(sequence), nil
}

func (c *CursorCodec) sign(payload []byte) []byte {
	mac := hmac.New(sha256.New, c.key)
	_, _ = mac.Write([]byte("unirouter.completed-execution.cursor.v1\x00"))
	_, _ = mac.Write([]byte(c.sourceInstance))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

func (c *CursorCodec) String() string {
	if c == nil {
		return "completed-execution-cursor<nil>"
	}
	return fmt.Sprintf("completed-execution-cursor<source-bytes=%d>", len(c.sourceInstance))
}
