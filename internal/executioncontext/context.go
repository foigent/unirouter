// Package executioncontext carries allowlisted contemporaneous evidence. It
// contains no credential material and is not an authorization decision maker.
package executioncontext

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const Version = "unirouter.execution-context-snapshot.v1"

// Identity is a separate permission group. Known account ownership does not
// imply that a particular human performed the invocation.
type Identity struct {
	OwnerUserID            string   `json:"owner_user_id,omitempty"`
	GrantID                string   `json:"grant_id,omitempty"`
	GrantRevision          string   `json:"grant_revision,omitempty"`
	GrantSubjectType       string   `json:"grant_subject_type,omitempty"`
	GrantSubjectUserID     string   `json:"grant_subject_user_id,omitempty"`
	GrantSubjectWorkloadID string   `json:"grant_subject_workload_id,omitempty"`
	SponsorUserID          string   `json:"sponsor_user_id,omitempty"`
	WorkflowID             string   `json:"workflow_id,omitempty"`
	RootAgentID            string   `json:"root_agent_id,omitempty"`
	AgentID                string   `json:"agent_id,omitempty"`
	ParentAgentID          string   `json:"parent_agent_id,omitempty"`
	Lineage                []string `json:"lineage,omitempty"`
	ActualHuman            string   `json:"actual_human"`
	AuthorityVersion       string   `json:"authority_version,omitempty"`
	AuthorityRead          string   `json:"authority_read"`
}

type Core struct {
	RequestID             string `json:"request_id"`
	OrgID                 string `json:"org_id"`
	ProjectID             string `json:"project_id"`
	APIKeyID              string `json:"api_key_id"`
	KeyType               string `json:"key_type"`
	CredentialID          string `json:"credential_id,omitempty"`
	CredentialRef         string `json:"credential_ref,omitempty"`
	WorkloadApplicationID string `json:"workload_application_id,omitempty"`
	AuthKind              string `json:"auth_kind"`
	AuthEpoch             string `json:"auth_epoch"`
	Operation             string `json:"operation"`
	Protocol              string `json:"protocol"`
	CapturedAt            string `json:"captured_at"`
}

type Snapshot struct {
	SchemaVersion string   `json:"schema_version"`
	Core          Core     `json:"core"`
	Identity      Identity `json:"identity"`
}

type contextKey struct{}

func WithSnapshot(ctx context.Context, value Snapshot) context.Context {
	value.Identity.Lineage = append([]string(nil), value.Identity.Lineage...)
	return context.WithValue(ctx, contextKey{}, value)
}

func FromContext(ctx context.Context) (Snapshot, bool) {
	value, ok := ctx.Value(contextKey{}).(Snapshot)
	value.Identity.Lineage = append([]string(nil), value.Identity.Lineage...)
	return value, ok
}

func ID(value int64) string {
	if value <= 0 {
		return ""
	}
	return strconv.FormatInt(value, 10)
}

func Time(value time.Time) string { return value.UTC().Format("2006-01-02T15:04:05.000000Z") }

func bounded(value string, max int, required bool) bool {
	if (required && value == "") || len(value) > max || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	return strings.IndexFunc(value, unicode.IsControl) < 0
}

func decimalID(value string, required bool) bool {
	if value == "" {
		return !required
	}
	n, e := strconv.ParseInt(value, 10, 64)
	return e == nil && n > 0 && ID(n) == value
}

func (c Core) Validate() error {
	for _, value := range []string{c.OrgID, c.ProjectID, c.APIKeyID} {
		if !decimalID(value, true) {
			return errors.New("invalid execution scope")
		}
	}
	for _, value := range []string{c.CredentialID, c.WorkloadApplicationID} {
		if !decimalID(value, false) {
			return errors.New("invalid execution reference")
		}
	}
	for _, value := range []string{c.RequestID, c.AuthKind, c.Operation, c.Protocol} {
		if !bounded(value, 64, true) {
			return errors.New("invalid execution field")
		}
	}
	if c.KeyType != "personal" && c.KeyType != "service" && c.KeyType != "shared" || !bounded(c.CredentialRef, 128, false) {
		return errors.New("invalid credential context")
	}
	epoch, e := strconv.ParseInt(c.AuthEpoch, 10, 64)
	if e != nil || epoch < 0 || strconv.FormatInt(epoch, 10) != c.AuthEpoch {
		return errors.New("invalid authentication epoch")
	}
	instant, e := time.Parse("2006-01-02T15:04:05.000000Z", c.CapturedAt)
	if e != nil || Time(instant) != c.CapturedAt {
		return errors.New("invalid capture time")
	}
	return nil
}

func (i Identity) Validate() error {
	if i.ActualHuman != "unknown" || len(i.Lineage) > 32 || !bounded(i.AuthorityRead, 64, true) {
		return errors.New("invalid actual actor evidence")
	}
	for _, value := range []string{i.OwnerUserID, i.GrantID, i.GrantRevision, i.GrantSubjectUserID, i.GrantSubjectWorkloadID, i.SponsorUserID} {
		if !decimalID(value, false) {
			return errors.New("invalid identity reference")
		}
	}
	for _, value := range append([]string{i.GrantSubjectType, i.WorkflowID, i.RootAgentID, i.AgentID, i.ParentAgentID, i.AuthorityVersion}, i.Lineage...) {
		if !bounded(value, 128, false) {
			return errors.New("invalid identity field")
		}
	}
	return nil
}

func (s Snapshot) Validate() error {
	if s.SchemaVersion != Version {
		return errors.New("invalid execution context snapshot")
	}
	if e := s.Core.Validate(); e != nil {
		return e
	}
	if e := s.Identity.Validate(); e != nil {
		return e
	}
	encoded, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if len(encoded) > 32<<10 {
		return errors.New("execution context snapshot exceeds bound")
	}
	return nil
}
