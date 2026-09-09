package sourcefacts

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"math"
)

type cursorScope struct {
	org, project, generation, epoch int64
	profile                         string
}

func (s *Service) encodeCursor(scope cursorScope, position int64) (string, error) {
	values := []int64{scope.org, scope.project, scope.generation, scope.epoch, position}
	for i, v := range values {
		if v < 0 || (i < 4 && v == 0) {
			return "", errors.New("invalid cursor scope")
		}
	}
	wire := make([]byte, 73)
	wire[0] = 1
	binding := sha256.Sum256([]byte("unirouter.source-cursor.v1\x00" + s.options.SourceInstance + "\x00" + scope.profile))
	copy(wire[1:33], binding[:])
	for i, v := range values {
		binary.BigEndian.PutUint64(wire[33+i*8:], uint64(v))
	}
	mac := hmac.New(sha256.New, []byte(s.options.CursorSecret))
	mac.Write([]byte("unirouter.source-cursor.v1\x00"))
	mac.Write(wire)
	return base64.RawURLEncoding.EncodeToString(append(wire, mac.Sum(nil)...)), nil
}

func (s *Service) decodeCursor(raw string, scope cursorScope) (int64, error) {
	if len(raw) > 512 {
		return 0, errors.New("invalid cursor")
	}
	wire, e := base64.RawURLEncoding.DecodeString(raw)
	if e != nil || len(wire) != 105 || wire[0] != 1 {
		return 0, errors.New("invalid cursor")
	}
	position := binary.BigEndian.Uint64(wire[65:73])
	if position > math.MaxInt64 {
		return 0, errors.New("invalid cursor")
	}
	expected, e := s.encodeCursor(scope, int64(position))
	if e != nil {
		return 0, e
	}
	if !hmac.Equal([]byte(expected), []byte(raw)) {
		return 0, errors.New("cursor binding or signature differs")
	}
	return int64(position), nil
}
