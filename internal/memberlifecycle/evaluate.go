package memberlifecycle

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/eventconnector"
)

var (
	portableIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,159}$`)
	digestPattern     = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	unsignedPattern   = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
	utcMicrosPattern  = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}Z$`)
)

func Evaluate(body []byte) (Report, error) {
	event, err := decodeEvent(body)
	if err != nil {
		return Report{}, err
	}
	return evaluate(body, event)
}

func decodeEvent(body []byte) (Event, error) {
	if len(body) == 0 || len(body) > MaxInputBytes {
		return Event{}, contractError(ErrInvalid, "input_size_invalid")
	}
	if strictErr := eventconnector.ValidateStrictJSON(body, eventconnector.StrictJSONOptions{}); strictErr != nil {
		if strictErr.Code == "duplicate_json_key" {
			return Event{}, contractError(ErrInvalid, "duplicate_json_key")
		}
		return Event{}, contractError(ErrInvalid, "input_json_invalid")
	}
	var event Event
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&event); err != nil {
		return Event{}, contractError(ErrInvalid, "input_shape_invalid")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Event{}, contractError(ErrInvalid, "trailing_json")
	}
	if !hasExactJSONShape(body, event) {
		return Event{}, contractError(ErrInvalid, "input_shape_invalid")
	}
	if err := validateEvent(event); err != nil {
		return Event{}, err
	}
	return event, nil
}

func validateEvent(event Event) error {
	if event.ContractVersion != ContractVersion {
		return contractError(ErrInvalid, "contract_version_unsupported")
	}
	if !portableIDPattern.MatchString(event.EventID) {
		return contractError(ErrInvalid, "event_id_invalid")
	}
	if !digestPattern.MatchString(event.SubjectRef) || !digestPattern.MatchString(event.SourceRef) {
		return contractError(ErrInvalid, "portable_identity_invalid")
	}
	if !validOperation(event.Operation) {
		return contractError(ErrInvalid, "operation_invalid")
	}
	if _, err := parseUTCMicros(event.OccurredAt); err != nil {
		return contractError(ErrInvalid, "occurred_at_invalid")
	}
	parent, err := parseUnsignedInt64(event.ParentRevision)
	if err != nil {
		return contractError(ErrInvalid, "parent_revision_invalid")
	}
	revision, err := parsePositiveInt64(event.Revision)
	if err != nil || revision != parent+1 {
		return contractError(ErrInvalid, "revision_invalid")
	}
	if event.After.Revision != event.Revision {
		return contractError(ErrInvalid, "after_revision_mismatch")
	}
	if event.Operation == OperationCreate {
		if err := validateSnapshot(event.After); err != nil {
			return err
		}
		if event.Before != nil || parent != 0 || event.After.Status != StatusActive {
			return contractError(ErrInvalid, "create_transition_invalid")
		}
	} else {
		if event.Before == nil || event.Before.Revision != event.ParentRevision {
			return contractError(ErrInvalid, "before_snapshot_required")
		}
		if err := validateSnapshot(*event.Before); err != nil {
			return err
		}
		if event.Before.Status == StatusDeprovisioned {
			return contractError(ErrConflict, "subject_deprovisioned")
		}
		if err := validateSnapshot(event.After); err != nil {
			return err
		}
	}
	switch event.Operation {
	case OperationUpdate:
		if event.After.Status != StatusActive {
			return contractError(ErrInvalid, "update_status_invalid")
		}
	case OperationSuspend:
		if event.Before.Status != StatusActive || event.After.Status != StatusSuspended ||
			!sameAssignments(*event.Before, event.After) {
			return contractError(ErrInvalid, "suspend_transition_invalid")
		}
	case OperationDeprovision:
		if event.After.Status != StatusDeprovisioned || len(event.After.Memberships) != 0 ||
			len(event.After.Capabilities) != 0 {
			return contractError(ErrInvalid, "deprovision_transition_invalid")
		}
	}
	return nil
}

func validateSnapshot(snapshot StateSnapshot) error {
	if snapshot.Status != StatusActive && snapshot.Status != StatusSuspended && snapshot.Status != StatusDeprovisioned {
		return contractError(ErrInvalid, "state_status_invalid")
	}
	if !validUserRole(snapshot.Role) {
		return contractError(ErrInvalid, "role_invalid")
	}
	if _, err := parsePositiveInt64(snapshot.Revision); err != nil {
		return contractError(ErrInvalid, "snapshot_revision_invalid")
	}
	if len(snapshot.Memberships) > 1000 || len(snapshot.Capabilities) > 2000 {
		return contractError(ErrInvalid, "assignment_count_invalid")
	}
	if snapshot.Status != StatusDeprovisioned && len(snapshot.Memberships) == 0 {
		return contractError(ErrInvalid, "active_project_membership_required")
	}
	lastProject := ""
	projectRoles := make(map[string]string, len(snapshot.Memberships))
	for _, membership := range snapshot.Memberships {
		if !digestPattern.MatchString(membership.ProjectRef) || !validMembershipRole(membership.Role) ||
			(lastProject != "" && membership.ProjectRef <= lastProject) {
			return contractError(ErrInvalid, "membership_order_invalid")
		}
		if membership.Role != roleMembership(snapshot.Role) {
			return contractError(ErrInvalid, "membership_role_mismatch")
		}
		projectRoles[membership.ProjectRef] = membership.Role
		lastProject = membership.ProjectRef
	}
	lastCapability := ""
	for _, capability := range snapshot.Capabilities {
		if !digestPattern.MatchString(capability.CapabilityRef) ||
			(capability.ProjectRef != "" && !digestPattern.MatchString(capability.ProjectRef)) ||
			!validCapabilityKind(capability.Kind) ||
			(capability.Status != "active" && capability.Status != "disabled") ||
			(lastCapability != "" && capability.CapabilityRef <= lastCapability) {
			return contractError(ErrInvalid, "capability_order_invalid")
		}
		if (capability.Kind == "admin_session" && capability.ProjectRef != "") ||
			(capability.Kind != "admin_session" && capability.ProjectRef == "") {
			return contractError(ErrInvalid, "capability_scope_invalid")
		}
		if capability.ProjectRef != "" {
			if _, exists := projectRoles[capability.ProjectRef]; !exists {
				return contractError(ErrInvalid, "capability_membership_missing")
			}
		}
		lastCapability = capability.CapabilityRef
	}
	return nil
}

func evaluate(body []byte, event Event) (Report, error) {
	semanticHash, err := canonicalHash(event)
	if err != nil {
		return Report{}, contractError(ErrInvalid, "canonicalization_failed")
	}
	memberships := make([]Membership, 0)
	capabilities := make([]Capability, 0)
	if event.After.Status == StatusActive {
		memberships = append(memberships, event.After.Memberships...)
		for _, capability := range event.After.Capabilities {
			if capability.Status == "active" {
				capabilities = append(capabilities, capability)
			}
		}
	}
	revocations := computeRevocations(event)
	securityPrincipalChanged := event.Before != nil &&
		(event.Before.Role != event.After.Role || event.Before.Status != event.After.Status)
	controls := EnforcementControls{
		DenyAdminSessions:               event.After.Status != StatusActive,
		RevokeLiveAdminSessions:         securityPrincipalChanged || event.Operation == OperationDeprovision,
		DisablePersonalKeysBeforeDelete: event.Operation == OperationDeprovision,
		AgentCapabilityRecheckSeconds:   "15",
	}
	return Report{
		ReportVersion:           ReportVersion,
		ContractVersion:         ContractVersion,
		EventID:                 event.EventID,
		SubjectRef:              event.SubjectRef,
		Operation:               event.Operation,
		Revision:                event.Revision,
		InputPayloadHash:        hash(body),
		InputSemanticHash:       semanticHash,
		StateStatus:             event.After.Status,
		EffectiveMemberships:    memberships,
		EffectiveCapabilities:   capabilities,
		Revocations:             revocations,
		Controls:                controls,
		LocalProjection:         localProjection(event),
		ReplayStatus:            ReplayAccepted,
		VendorAdapterAuthorized: false,
		RuntimeMutation:         RuntimeMutationNone,
	}, nil
}

func computeRevocations(event Event) []Revocation {
	if event.Before == nil {
		return []Revocation{}
	}
	afterMemberships := make(map[string]struct{}, len(event.After.Memberships))
	afterCapabilities := make(map[string]struct{}, len(event.After.Capabilities))
	if event.After.Status == StatusActive {
		for _, membership := range event.After.Memberships {
			afterMemberships[membership.ProjectRef] = struct{}{}
		}
		for _, capability := range event.After.Capabilities {
			if capability.Status == "active" {
				afterCapabilities[capability.CapabilityRef] = struct{}{}
			}
		}
	}
	result := make([]Revocation, 0)
	for _, membership := range event.Before.Memberships {
		if _, retained := afterMemberships[membership.ProjectRef]; !retained {
			result = append(result, Revocation{
				Kind: "membership", Ref: membership.ProjectRef, Reason: revocationReason(event.Operation),
			})
		}
	}
	for _, capability := range event.Before.Capabilities {
		if capability.Status != "active" {
			continue
		}
		if _, retained := afterCapabilities[capability.CapabilityRef]; !retained {
			result = append(result, Revocation{
				Kind: "capability", Ref: capability.CapabilityRef, Reason: revocationReason(event.Operation),
			})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Kind == result[j].Kind {
			return result[i].Ref < result[j].Ref
		}
		return result[i].Kind < result[j].Kind
	})
	return result
}

func revocationReason(operation string) string {
	switch operation {
	case OperationSuspend:
		return "subject_suspended"
	case OperationDeprovision:
		return "subject_deprovisioned"
	default:
		return "assignment_removed"
	}
}

func localProjection(event Event) LocalProjection {
	switch event.Operation {
	case OperationSuspend:
		return LocalProjection{
			UserStatus: "disabled", MembershipStorage: "retained_but_ineffective",
			PersonalKeyHandling: "retained_but_authority_gated",
			SessionHandling:     "revoked_before_completion",
		}
	case OperationDeprovision:
		return LocalProjection{
			UserStatus: "deleted", MembershipStorage: "cascade_deleted",
			PersonalKeyHandling: "disabled_before_owner_delete",
			SessionHandling:     "cascade_deleted",
		}
	default:
		return LocalProjection{
			UserStatus: "active", MembershipStorage: "replace_atomically",
			PersonalKeyHandling: "authority_gated",
			SessionHandling:     "revoke_on_role_or_status_change",
		}
	}
}

func sameAssignments(left, right StateSnapshot) bool {
	left.Revision = ""
	right.Revision = ""
	left.Status = ""
	right.Status = ""
	leftBody, _ := json.Marshal(left)
	rightBody, _ := json.Marshal(right)
	return bytes.Equal(leftBody, rightBody)
}

func validOperation(value string) bool {
	return value == OperationCreate || value == OperationUpdate ||
		value == OperationSuspend || value == OperationDeprovision
}

func validUserRole(value string) bool {
	return value == "project_admin" || value == "developer" || value == "viewer"
}

func roleMembership(value string) string {
	switch value {
	case "project_admin":
		return "project_admin"
	case "developer":
		return "project_developer"
	default:
		return "project_viewer"
	}
}

func validMembershipRole(value string) bool {
	return value == "project_admin" || value == "project_developer" || value == "project_viewer"
}

func validCapabilityKind(value string) bool {
	return value == "admin_session" || value == "personal_api_key" || value == "agent_capability_root"
}

func parsePositiveInt64(value string) (int64, error) {
	parsed, err := parseUnsignedInt64(value)
	if err != nil || parsed < 1 {
		return 0, errors.New("positive int64 required")
	}
	return parsed, nil
}

func parseUnsignedInt64(value string) (int64, error) {
	if !unsignedPattern.MatchString(value) {
		return 0, errors.New("invalid decimal")
	}
	return strconv.ParseInt(value, 10, 64)
}

func parseUTCMicros(value string) (time.Time, error) {
	if !utcMicrosPattern.MatchString(value) {
		return time.Time{}, errors.New("six-digit UTC required")
	}
	return time.Parse("2006-01-02T15:04:05.000000Z", value)
}

func canonicalHash(value any) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return hash(body), nil
}

func hash(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func hasExactJSONShape(body []byte, value any) bool {
	encoded, err := json.Marshal(value)
	if err != nil {
		return false
	}
	var provided, expected any
	if json.Unmarshal(body, &provided) != nil || json.Unmarshal(encoded, &expected) != nil {
		return false
	}
	return sameJSONShape(provided, expected)
}

func sameJSONShape(provided, expected any) bool {
	switch expectedValue := expected.(type) {
	case map[string]any:
		providedValue, ok := provided.(map[string]any)
		if !ok || len(providedValue) != len(expectedValue) {
			return false
		}
		for key, child := range expectedValue {
			if providedChild, exists := providedValue[key]; !exists || !sameJSONShape(providedChild, child) {
				return false
			}
		}
		return true
	case []any:
		providedValue, ok := provided.([]any)
		if !ok || len(providedValue) != len(expectedValue) {
			return false
		}
		for index := range expectedValue {
			if !sameJSONShape(providedValue[index], expectedValue[index]) {
				return false
			}
		}
		return true
	default:
		switch provided.(type) {
		case map[string]any, []any:
			return false
		default:
			return true
		}
	}
}

func snapshotEqual(left, right StateSnapshot) bool {
	leftBody, _ := json.Marshal(left)
	rightBody, _ := json.Marshal(right)
	return bytes.Equal(leftBody, rightBody)
}

func cloneReport(report Report) Report {
	body, _ := json.Marshal(report)
	var cloned Report
	_ = json.Unmarshal(body, &cloned)
	return cloned
}

func cloneEvent(event Event) Event {
	body, _ := json.Marshal(event)
	var cloned Event
	_ = json.Unmarshal(body, &cloned)
	return cloned
}

func errorCode(err error) string {
	var contractErr *ContractError
	if errors.As(err, &contractErr) {
		return contractErr.Code
	}
	if err != nil {
		return "unexpected_error"
	}
	return ""
}

func sortedUnique(values []string) []string {
	sort.Strings(values)
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || value != result[len(result)-1] {
			result = append(result, value)
		}
	}
	return result
}

func redactedRef(label string) string {
	return hash([]byte(strings.TrimSpace(label)))
}
