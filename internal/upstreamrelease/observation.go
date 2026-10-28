// Package upstreamrelease records a scoped supplier declaration. It does not
// discover releases, pin remote weights, qualify models or control traffic.
package upstreamrelease

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/uniroute/unirouter/internal/strictjson"
	"io"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const Schema = "unirouter.upstream-release-observation.v1"

var (
	ErrInvalid      = errors.New("invalid upstream release observation")
	ErrConflict     = errors.New("upstream release observation changed or evidence scope differs")
	identityPattern = regexp.MustCompile(`^uor_[0-9a-f]{32}$`)
	digestPattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type Scope struct {
	CustomerOrgID int64  `json:"customer_org_id"`
	DeploymentID  int64  `json:"deployment_id"`
	ConnectionID  int64  `json:"connection_id"`
	Provider      string `json:"provider"`
	BaseURL       string `json:"base_url"`
	APIModel      string `json:"api_model"`
}

type Claim struct {
	ReleaseLabel string `json:"release_label"`
	AnnouncedOn  string `json:"announced_on,omitempty"`
}

type Provenance struct {
	SourceKind     string `json:"source_kind"`
	SourceURL      string `json:"source_url"`
	SourceRevision string `json:"source_revision,omitempty"`
	SourceSHA256   string `json:"source_sha256,omitempty"`
}

type Observation struct {
	SchemaVersion string     `json:"schema_version"`
	Identity      string     `json:"identity"`
	Scope         Scope      `json:"scope"`
	Claim         Claim      `json:"claim"`
	Provenance    Provenance `json:"provenance"`
	CheckedAt     time.Time  `json:"checked_at"`
	RecordedBy    int64      `json:"recorded_by"`
	Declaration   string     `json:"declaration"`
	RemoteWeights string     `json:"remote_weights"`
}

type State struct {
	Observation *Observation `json:"observation"`
	Revision    int64        `json:"revision"`
}

type Input struct {
	ExpectedConnectionRevision int64  `json:"expected_connection_revision"`
	ExpectedDeploymentRevision int64  `json:"expected_deployment_revision"`
	ExpectedRevision           int64  `json:"expected_revision"`
	ExpectedIdentity           string `json:"expected_identity"`
	ReleaseLabel               string `json:"release_label"`
	AnnouncedOn                string `json:"announced_on,omitempty"`
	SourceURL                  string `json:"source_url"`
	SourceRevision             string `json:"source_revision,omitempty"`
	SourceSHA256               string `json:"source_sha256,omitempty"`
}

type Applicability struct {
	State            string `json:"state"`
	Reason           string `json:"reason_code"`
	CurrentIdentity  string `json:"current_identity"`
	EvidenceIdentity string `json:"evidence_identity"`
}

func Identity(value *Observation) string {
	if value == nil {
		return ""
	}
	return value.Identity
}

func ValidIdentity(value string) bool { return value == "" || identityPattern.MatchString(value) }

func bounded(value string, limit int, required bool) bool {
	return (!required || value != "") && len(value) <= limit && utf8.ValidString(value) &&
		value == strings.TrimSpace(value) && !strings.ContainsAny(value, "\x00\r\n")
}

func validateClaim(claim Claim, provenance Provenance) error {
	if !bounded(claim.ReleaseLabel, 160, true) || !bounded(provenance.SourceRevision, 255, false) ||
		!bounded(provenance.SourceURL, 2048, true) || provenance.SourceKind != "official_announcement" ||
		(provenance.SourceSHA256 != "" && !digestPattern.MatchString(provenance.SourceSHA256)) {
		return ErrInvalid
	}
	if claim.AnnouncedOn != "" {
		d, err := time.Parse("2006-01-02", claim.AnnouncedOn)
		if err != nil || d.Format("2006-01-02") != claim.AnnouncedOn {
			return ErrInvalid
		}
	}
	u, err := url.Parse(provenance.SourceURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return ErrInvalid
	}
	return nil
}

func (value *Observation) Validate(scope Scope) error {
	if value == nil {
		return nil
	}
	if value.SchemaVersion != Schema || !identityPattern.MatchString(value.Identity) || value.Scope != scope ||
		scope.CustomerOrgID <= 0 || scope.DeploymentID <= 0 || scope.ConnectionID <= 0 ||
		!bounded(scope.Provider, 100, true) || !bounded(scope.BaseURL, 2048, true) || !bounded(scope.APIModel, 200, true) ||
		value.RecordedBy <= 0 || value.CheckedAt.IsZero() || value.Declaration != "declared" || value.RemoteWeights != "unpinned" {
		return ErrInvalid
	}
	return validateClaim(value.Claim, value.Provenance)
}

func Next(scope Scope, current State, input Input, actorID int64, now time.Time) (State, error) {
	if input.ExpectedRevision != current.Revision || input.ExpectedIdentity != Identity(current.Observation) {
		return State{}, ErrConflict
	}
	if current.Revision < 0 || current.Revision == int64(^uint64(0)>>1) || actorID <= 0 || now.IsZero() ||
		!ValidIdentity(input.ExpectedIdentity) || (current.Observation == nil) != (current.Revision == 0) {
		return State{}, ErrInvalid
	}
	claim := Claim{ReleaseLabel: input.ReleaseLabel, AnnouncedOn: input.AnnouncedOn}
	provenance := Provenance{SourceKind: "official_announcement", SourceURL: input.SourceURL,
		SourceRevision: input.SourceRevision, SourceSHA256: input.SourceSHA256}
	if err := validateClaim(claim, provenance); err != nil {
		return State{}, err
	}
	identity := ""
	if current.Observation != nil && current.Observation.Scope == scope && current.Observation.Claim == claim {
		identity = current.Observation.Identity
	} else {
		var entropy [16]byte
		if _, err := rand.Read(entropy[:]); err != nil {
			return State{}, err
		}
		identity = "uor_" + hex.EncodeToString(entropy[:])
	}
	observation := &Observation{SchemaVersion: Schema, Identity: identity, Scope: scope, Claim: claim,
		Provenance: provenance, CheckedAt: now.UTC(), RecordedBy: actorID, Declaration: "declared", RemoteWeights: "unpinned"}
	if err := observation.Validate(scope); err != nil {
		return State{}, err
	}
	return State{Observation: observation, Revision: current.Revision + 1}, nil
}

func Decode(raw []byte) (*Observation, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	if strictjson.Validate(raw) != nil {
		return nil, ErrInvalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || !exactObservationKeys(raw, "schema_version identity scope claim provenance checked_at recorded_by declaration remote_weights") ||
		!exactObservationKeys(fields["scope"], "customer_org_id deployment_id connection_id provider base_url api_model") ||
		!exactObservationKeys(fields["claim"], "release_label announced_on") ||
		!exactObservationKeys(fields["provenance"], "source_kind source_url source_revision source_sha256") {
		return nil, ErrInvalid
	}
	var value Observation
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return nil, ErrInvalid
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, ErrInvalid
	}
	if err := value.Validate(value.Scope); err != nil {
		return nil, err
	}
	return &value, nil
}

// Matches compares the declaration epoch and scoped claim, never recheck metadata.
func Matches(a, b *Observation) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Identity == b.Identity && a.Scope == b.Scope && a.Claim == b.Claim
}

func Assess(current, evidence *Observation) Applicability {
	result := Applicability{CurrentIdentity: Identity(current), EvidenceIdentity: Identity(evidence)}
	switch {
	case current == nil:
		result.State, result.Reason = "unknown", "upstream_release_unrecorded"
	case evidence == nil:
		result.State, result.Reason = "stale", "probe_upstream_scope_unknown"
	case Matches(current, evidence):
		result.State, result.Reason = "current", "probe_matches_recorded_declaration"
	default:
		result.State, result.Reason = "stale", "upstream_release_observation_changed"
	}
	return result
}

func Require(current, evidence *Observation, expected string) error {
	if !ValidIdentity(expected) || expected != Identity(current) || !Matches(current, evidence) {
		return ErrConflict
	}
	return nil
}

// AssessInScope includes the live endpoint scope without relabeling an old
// declaration after a connection, origin or alias change.
func AssessInScope(scope Scope, current, evidence *Observation) Applicability {
	result := Assess(current, evidence)
	if current != nil && current.Validate(scope) != nil {
		result.State, result.Reason = "stale", "upstream_endpoint_scope_changed"
	}
	return result
}

// encoding/json accepts case-insensitive field aliases. Stored evidence uses
// exact member names so two differently cased keys cannot choose a value.
func exactObservationKeys(raw []byte, allowed string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return false
	}
	keys := strings.Fields(allowed)
	for key := range fields {
		if !slices.Contains(keys, key) {
			return false
		}
	}
	return true
}
