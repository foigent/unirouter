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
	return s.encodeVersionedCursor(1, scope, position)
}

func (s *Service) encodeObservationCursor(scope cursorScope, position int64) (string, error) {
	return s.encodeVersionedCursor(2, scope, position)
}

func (s *Service) encodeVersionedCursor(version byte, scope cursorScope, position int64) (string, error) {
	domain := "unirouter.source-cursor.v1\x00"
	if version == 2 {
		domain = "unirouter.source-observation-cursor.v1\x00"
	} else if version != 1 {
		return "", errors.New("invalid cursor version")
	}
	values := []int64{scope.org, scope.project, scope.generation, scope.epoch, position}
	for i, v := range values {
		if v < 0 || (i < 4 && v == 0) {
			return "", errors.New("invalid cursor scope")
		}
	}
	wire := make([]byte, 73)
	wire[0] = version
	binding := sha256.Sum256([]byte(domain + s.options.SourceInstance + "\x00" + scope.profile))
	copy(wire[1:33], binding[:])
	for i, v := range values {
		binary.BigEndian.PutUint64(wire[33+i*8:], uint64(v))
	}
	mac := hmac.New(sha256.New, []byte(s.options.CursorSecret))
	mac.Write([]byte(domain))
	mac.Write(wire)
	return base64.RawURLEncoding.EncodeToString(append(wire, mac.Sum(nil)...)), nil
}

func (s *Service) decodeCursor(raw string, scope cursorScope) (int64, error) {
	return s.decodeVersionedCursor(1, raw, scope)
}

func (s *Service) decodeObservationCursor(raw string, scope cursorScope) (int64, error) {
	return s.decodeVersionedCursor(2, raw, scope)
}

func (s *Service) decodeVersionedCursor(version byte, raw string, scope cursorScope) (int64, error) {
	if len(raw) > 512 {
		return 0, errors.New("invalid cursor")
	}
	wire, e := base64.RawURLEncoding.DecodeString(raw)
	if e != nil || len(wire) != 105 || wire[0] != version {
		return 0, errors.New("invalid cursor")
	}
	position := binary.BigEndian.Uint64(wire[65:73])
	if position > math.MaxInt64 {
		return 0, errors.New("invalid cursor")
	}
	expected, e := s.encodeVersionedCursor(version, scope, int64(position))
	if e != nil {
		return 0, e
	}
	if !hmac.Equal([]byte(expected), []byte(raw)) {
		return 0, errors.New("cursor binding or signature differs")
	}
	return int64(position), nil
}
