package admin

import (
	"math"
	"slices"
	"strconv"

	"github.com/uniroute/unirouter/internal/sourcefacts"
)

type contentControlKey struct {
	CredentialID     string                      `json:"credential_id"`
	RegistrationHash string                      `json:"registration_hash"`
	CoverageFrom     sourcefacts.ContentBoundary `json:"coverage_from"`
	AllowedPurposes  []string                    `json:"allowed_purposes"`
	ips              string
}

type contentControlState struct {
	global, local                      string
	version, generation, safetyVersion int64
	active, staged                     *contentControlKey
}

func contentKeyMatches(key *contentControlKey, r contentControlRequest) bool {
	return key != nil && key.CredentialID == r.CredentialID && key.RegistrationHash == r.RegistrationHash
}

// The database adapter calls replay matching BEFORE this CAS transition.
// This function has no side effects and never grants a token by itself.
func nextContentControlState(s contentControlState, action string, r contentControlRequest) (contentControlState, error) {
	conflict := errXInsightFeedConflict
	expected, ok := contentControlNumber(r.ExpectedStateVersion, true)
	if !ok || expected != s.version || s.global == "revoked" || s.global == "not_configured" || s.version == math.MaxInt64 || s.generation == math.MaxInt64 {
		return s, conflict
	}
	next := s
	if s.local == "not_configured" {
		if action != "prepare" {
			return s, conflict
		}
		next.local = "paused"
		next.generation = 1
	}
	switch action {
	case "prepare":
		if s.active != nil || s.staged != nil {
			return s, conflict
		}
		if next.local == "revoked" {
			next.local = "paused"
		}
	case "rotate":
		if s.staged != nil || !contentKeyMatches(s.active, r) || s.local == "revoked" {
			return s, conflict
		}
	case "activate", "cancel":
		if !contentKeyMatches(s.staged, r) || s.local == "revoked" {
			return s, conflict
		}
		if action == "activate" {
			next.generation++
			next.local = "active"
			next.active = s.staged
		}
		next.staged = nil
	case "pause", "resume", "revoke":
		if s.active != nil {
			if !contentKeyMatches(s.active, r) {
				return s, conflict
			}
		} else if r.CredentialID != "" || r.RegistrationHash != "" {
			return s, conflict
		}
		switch action {
		case "pause":
			if s.local == "revoked" {
				return s, conflict
			}
			if s.local != "paused" {
				next.generation++
			}
			next.local = "paused"
		case "resume":
			if s.local == "revoked" || s.active == nil {
				return s, conflict
			}
			next.local = "active"
		case "revoke":
			next.generation++
			next.local = "revoked"
			next.active = nil
			next.staged = nil
		}
	default:
		return s, conflict
	}
	next.version++
	return next, nil
}

func contentReplayApplicable(requestHash, storedHash string, version, safety int64, s contentControlState) bool {
	return requestHash == storedHash && version == s.version && safety == s.safetyVersion && s.global != "revoked"
}

// Rotation inherits the approved boundary, even if current retention changes.
func contentRegistrationBinding(action string, s contentControlState, floor sourcefacts.ContentBoundary, allowedIP string) contentControlKey {
	if action == "rotate" && s.active != nil {
		return contentControlKey{CoverageFrom: s.active.CoverageFrom, AllowedPurposes: slices.Clone(s.active.AllowedPurposes), ips: s.active.ips}
	}
	return contentControlKey{CoverageFrom: floor, AllowedPurposes: []string{"human_review"}, ips: allowedIP}
}

func contentAccessProjection(s contentControlState) *sourcefacts.ContentAccess {
	key := s.active
	if key == nil {
		key = s.staged
	}
	if key == nil {
		return nil
	}
	// A staged-only GET may describe its approved starting boundary, but never
	// advertises it as active authorization. Rotation keeps the active projection.
	enabled := s.active != nil && s.global == "active" && s.local == "active"
	purposes := []string{}
	if enabled {
		purposes = slices.Clone(key.AllowedPurposes)
	}
	boundary := key.CoverageFrom
	return &sourcefacts.ContentAccess{RegistrationHash: key.RegistrationHash, Profile: sourcefacts.ContentProfile, Enabled: enabled, ContentScopeRevision: strconv.FormatInt(s.generation, 10), AllowedPurposes: purposes, CoverageFrom: &boundary}
}
