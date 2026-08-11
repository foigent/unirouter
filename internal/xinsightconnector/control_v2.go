package xinsightconnector

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/uniroute/unirouter/internal/auth"
)

const (
	ControlOperationV2Schema = "xinsight.connector-control-operation.v2"
	ControlResultV2Schema    = "xinsight.connector-control-result.v2"

	ControlKindConnect           = "connect"
	ControlPhasePreparePaused    = "prepare_paused_source"
	ControlPhaseRegisterReceiver = "register_receiver"
	ControlPhaseResumeSource     = "resume_source"

	ControlResultCompleted = "completed"
	ControlResultFailed    = "failed"

	ControlFixedReceiverRoute = "/api/v1/integrations/xinsight/completed-executions"
)

var (
	controlHexDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
	controlKeyPrefixPattern = regexp.MustCompile(`^ur-xf1-[a-f0-9]{13}$`)
	controlFeedOperationID  = regexp.MustCompile(`^[a-f0-9]{32}$`)
)

type controlV2ValidationError struct {
	code  string
	cause error
}

func (err *controlV2ValidationError) Error() string {
	if err.cause == nil {
		return "X-Insight Connector control v2: " + err.code
	}
	return "X-Insight Connector control v2: " + err.code + ": " + err.cause.Error()
}

func (err *controlV2ValidationError) Unwrap() error { return err.cause }

func controlV2Error(code string, cause error) error {
	return &controlV2ValidationError{code: code, cause: cause}
}

// ControlV2ErrorCode returns the stable fail-closed classification produced by
// the frozen X-owned control-v2 consumer.
func ControlV2ErrorCode(err error) string {
	var validationErr *controlV2ValidationError
	if errors.As(err, &validationErr) {
		return validationErr.code
	}
	return ""
}

type ControlOperationV2 struct {
	SchemaVersion           string                `json:"schema_version"`
	OperationID             string                `json:"operation_id"`
	InstallationID          string                `json:"installation_id"`
	ProjectID               string                `json:"project_id"`
	EventScope              []string              `json:"event_scope"`
	DesiredState            string                `json:"desired_state"`
	Kind                    string                `json:"kind"`
	Phase                   string                `json:"phase"`
	PredecessorOperationID  string                `json:"predecessor_operation_id,omitempty"`
	PredecessorResultSHA256 string                `json:"predecessor_result_sha256,omitempty"`
	Request                 ControlPhaseRequestV2 `json:"request"`
}

type ControlPhaseRequestV2 interface {
	controlPhaseRequestV2()
}

type PreparePausedSourceRequestV2 struct {
	FeedSchema          string `json:"feed_schema"`
	CursorKey           string `json:"cursor_key"`
	RequiredSourceState string `json:"required_source_state"`
}

func (PreparePausedSourceRequestV2) controlPhaseRequestV2() {}

type RegisterReceiverRequestV2 struct {
	SourceInstance            string                   `json:"source_instance"`
	ConsumerEpoch             int64                    `json:"consumer_epoch"`
	CheckpointAuthoritySHA256 string                   `json:"checkpoint_authority_sha256"`
	ReceiverRegistration      ReceiverRegistrationV1   `json:"receiver_registration"`
	NetworkBinding            ReceiverNetworkBindingV2 `json:"network_binding"`
	RequiredSourceState       string                   `json:"required_source_state"`
}

func (RegisterReceiverRequestV2) controlPhaseRequestV2() {}

type ReceiverRegistrationV1 struct {
	SchemaVersion  string `json:"schema_version"`
	TokenFormat    string `json:"token_format"`
	VerifierScheme string `json:"verifier_scheme"`
	KeyPrefix      string `json:"key_prefix"`
	VerifierSHA256 string `json:"verifier_sha256"`
	OperationID    string `json:"operation_id"`
}

type ReceiverNetworkBindingV2 struct {
	Transport     string `json:"transport"`
	Route         string `json:"route"`
	BindingSHA256 string `json:"binding_sha256"`
}

type ResumeSourceRequestV2 struct {
	SourceInstance                 string `json:"source_instance"`
	ReceiverRegistrationSHA256     string `json:"receiver_registration_sha256"`
	NetworkBindingSHA256           string `json:"network_binding_sha256"`
	PausedAuthReceiptSHA256        string `json:"paused_auth_receipt_sha256"`
	DormantCredentialReceiptSHA256 string `json:"dormant_credential_receipt_sha256"`
	RequiredSourceState            string `json:"required_source_state"`
	RequiredReceiverState          string `json:"required_receiver_state"`
	Readiness                      string `json:"readiness"`
}

func (ResumeSourceRequestV2) controlPhaseRequestV2() {}

type ControlResultV2 struct {
	SchemaVersion  string               `json:"schema_version"`
	OperationID    string               `json:"operation_id"`
	InstallationID string               `json:"installation_id"`
	Phase          string               `json:"phase"`
	Status         string               `json:"status"`
	Result         ControlPhaseResultV2 `json:"result,omitempty"`
	ReasonCode     string               `json:"reason_code,omitempty"`
	ObservedAt     string               `json:"observed_at"`
}

type ControlPhaseResultV2 interface {
	controlPhaseResultV2()
}

type PreparePausedSourceResultV2 struct {
	SourceInstance string `json:"source_instance"`
	Cursor         string `json:"cursor"`
	HighWatermark  string `json:"high_watermark"`
	SourceState    string `json:"source_state"`
	Readiness      string `json:"readiness"`
}

func (PreparePausedSourceResultV2) controlPhaseResultV2() {}

type RegisterReceiverResultV2 struct {
	SourceInstance             string `json:"source_instance"`
	ReceiverRegistrationSHA256 string `json:"receiver_registration_sha256"`
	NetworkBindingSHA256       string `json:"network_binding_sha256"`
	SourceState                string `json:"source_state"`
	ReceiverState              string `json:"receiver_state"`
	Readiness                  string `json:"readiness"`
}

func (RegisterReceiverResultV2) controlPhaseResultV2() {}

type ResumeSourceResultV2 struct {
	SourceInstance string `json:"source_instance"`
	SourceState    string `json:"source_state"`
	ReceiverState  string `json:"receiver_state"`
	Readiness      string `json:"readiness"`
}

func (ResumeSourceResultV2) controlPhaseResultV2() {}

type controlOperationV2Wire struct {
	SchemaVersion           string          `json:"schema_version"`
	OperationID             string          `json:"operation_id"`
	InstallationID          string          `json:"installation_id"`
	ProjectID               string          `json:"project_id"`
	EventScope              []string        `json:"event_scope"`
	DesiredState            string          `json:"desired_state"`
	Kind                    string          `json:"kind"`
	Phase                   string          `json:"phase"`
	PredecessorOperationID  string          `json:"predecessor_operation_id,omitempty"`
	PredecessorResultSHA256 string          `json:"predecessor_result_sha256,omitempty"`
	Request                 json.RawMessage `json:"request"`
}

type controlResultV2Wire struct {
	SchemaVersion  string          `json:"schema_version"`
	OperationID    string          `json:"operation_id"`
	InstallationID string          `json:"installation_id"`
	Phase          string          `json:"phase"`
	Status         string          `json:"status"`
	Result         json.RawMessage `json:"result,omitempty"`
	ReasonCode     string          `json:"reason_code,omitempty"`
	ObservedAt     string          `json:"observed_at"`
}

func DecodeControlOperationV2(raw []byte) (ControlOperationV2, error) {
	if err := rejectDuplicateControlV2Fields(raw); err != nil {
		return ControlOperationV2{}, err
	}
	var wire controlOperationV2Wire
	if err := decodeStrictControlV2(raw, &wire); err != nil {
		return ControlOperationV2{}, err
	}
	operation := ControlOperationV2{
		SchemaVersion: wire.SchemaVersion, OperationID: wire.OperationID,
		InstallationID: wire.InstallationID, ProjectID: wire.ProjectID,
		EventScope: wire.EventScope, DesiredState: wire.DesiredState, Kind: wire.Kind,
		Phase: wire.Phase, PredecessorOperationID: wire.PredecessorOperationID,
		PredecessorResultSHA256: wire.PredecessorResultSHA256,
	}
	switch wire.Phase {
	case ControlPhasePreparePaused:
		var request PreparePausedSourceRequestV2
		if err := decodeStrictControlV2(wire.Request, &request); err != nil {
			return ControlOperationV2{}, err
		}
		operation.Request = request
	case ControlPhaseRegisterReceiver:
		var request RegisterReceiverRequestV2
		if err := decodeStrictControlV2(wire.Request, &request); err != nil {
			return ControlOperationV2{}, err
		}
		operation.Request = request
	case ControlPhaseResumeSource:
		var request ResumeSourceRequestV2
		if err := decodeStrictControlV2(wire.Request, &request); err != nil {
			return ControlOperationV2{}, err
		}
		operation.Request = request
	default:
		return ControlOperationV2{}, controlV2Error("phase_invalid", nil)
	}
	if err := validateControlOperationV2(operation); err != nil {
		return ControlOperationV2{}, err
	}
	return operation, nil
}

func DecodeControlResultV2(raw []byte) (ControlResultV2, error) {
	if err := rejectDuplicateControlV2Fields(raw); err != nil {
		return ControlResultV2{}, err
	}
	var wire controlResultV2Wire
	if err := decodeStrictControlV2(raw, &wire); err != nil {
		return ControlResultV2{}, err
	}
	result := ControlResultV2{
		SchemaVersion: wire.SchemaVersion, OperationID: wire.OperationID,
		InstallationID: wire.InstallationID, Phase: wire.Phase, Status: wire.Status,
		ReasonCode: wire.ReasonCode, ObservedAt: wire.ObservedAt,
	}
	if wire.Status == ControlResultCompleted {
		switch wire.Phase {
		case ControlPhasePreparePaused:
			var phaseResult PreparePausedSourceResultV2
			if err := decodeStrictControlV2(wire.Result, &phaseResult); err != nil {
				return ControlResultV2{}, err
			}
			result.Result = phaseResult
		case ControlPhaseRegisterReceiver:
			var phaseResult RegisterReceiverResultV2
			if err := decodeStrictControlV2(wire.Result, &phaseResult); err != nil {
				return ControlResultV2{}, err
			}
			result.Result = phaseResult
		case ControlPhaseResumeSource:
			var phaseResult ResumeSourceResultV2
			if err := decodeStrictControlV2(wire.Result, &phaseResult); err != nil {
				return ControlResultV2{}, err
			}
			result.Result = phaseResult
		default:
			return ControlResultV2{}, controlV2Error("phase_invalid", nil)
		}
	} else if len(wire.Result) != 0 {
		return ControlResultV2{}, controlV2Error("result_invalid", nil)
	}
	if err := validateControlResultV2(result); err != nil {
		return ControlResultV2{}, err
	}
	return result, nil
}

func (operation ControlOperationV2) CanonicalJSON() ([]byte, error) {
	if err := validateControlOperationV2(operation); err != nil {
		return nil, err
	}
	return json.Marshal(operation)
}

func (operation ControlOperationV2) CanonicalSHA256() (string, error) {
	canonical, err := operation.CanonicalJSON()
	if err != nil {
		return "", err
	}
	return hashBytes(canonical), nil
}

func (result ControlResultV2) CanonicalJSON() ([]byte, error) {
	if err := validateControlResultV2(result); err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

func (result ControlResultV2) CanonicalSHA256() (string, error) {
	canonical, err := result.CanonicalJSON()
	if err != nil {
		return "", err
	}
	return hashBytes(canonical), nil
}

func ValidateControlOperationPredecessorV2(operation ControlOperationV2, predecessor *ControlResultV2) error {
	if err := validateControlOperationV2(operation); err != nil {
		return err
	}
	if operation.Phase == ControlPhasePreparePaused {
		if predecessor != nil {
			return controlV2Error("predecessor_phase_conflict", nil)
		}
		return nil
	}
	if predecessor == nil || predecessor.Status != ControlResultCompleted ||
		predecessor.InstallationID != operation.InstallationID ||
		predecessor.OperationID != operation.PredecessorOperationID {
		return controlV2Error("predecessor_phase_conflict", nil)
	}
	expectedPhase := ControlPhasePreparePaused
	if operation.Phase == ControlPhaseResumeSource {
		expectedPhase = ControlPhaseRegisterReceiver
	}
	if predecessor.Phase != expectedPhase {
		return controlV2Error("predecessor_phase_conflict", nil)
	}
	digest, err := predecessor.CanonicalSHA256()
	if err != nil || digest != operation.PredecessorResultSHA256 {
		return controlV2Error("predecessor_result_conflict", err)
	}
	switch request := operation.Request.(type) {
	case RegisterReceiverRequestV2:
		previous, ok := predecessor.Result.(PreparePausedSourceResultV2)
		if !ok || request.SourceInstance != previous.SourceInstance {
			return controlV2Error("predecessor_source_conflict", nil)
		}
	case ResumeSourceRequestV2:
		previous, ok := predecessor.Result.(RegisterReceiverResultV2)
		if !ok || request.SourceInstance != previous.SourceInstance ||
			request.ReceiverRegistrationSHA256 != previous.ReceiverRegistrationSHA256 ||
			request.NetworkBindingSHA256 != previous.NetworkBindingSHA256 {
			return controlV2Error("predecessor_source_conflict", nil)
		}
	}
	return nil
}

func ValidateControlResultForOperationV2(operation ControlOperationV2, result ControlResultV2) error {
	if err := validateControlOperationV2(operation); err != nil {
		return err
	}
	if err := validateControlResultV2(result); err != nil {
		return err
	}
	if result.OperationID != operation.OperationID || result.InstallationID != operation.InstallationID || result.Phase != operation.Phase {
		return controlV2Error("result_binding_conflict", nil)
	}
	if result.Status == ControlResultFailed {
		return nil
	}
	switch request := operation.Request.(type) {
	case RegisterReceiverRequestV2:
		phaseResult, ok := result.Result.(RegisterReceiverResultV2)
		registrationDigest, err := receiverRegistrationDigest(request.ReceiverRegistration)
		if err != nil || !ok || phaseResult.SourceInstance != request.SourceInstance ||
			phaseResult.ReceiverRegistrationSHA256 != registrationDigest ||
			phaseResult.NetworkBindingSHA256 != request.NetworkBinding.BindingSHA256 {
			return controlV2Error("result_binding_conflict", err)
		}
	case ResumeSourceRequestV2:
		phaseResult, ok := result.Result.(ResumeSourceResultV2)
		if !ok || phaseResult.SourceInstance != request.SourceInstance {
			return controlV2Error("result_binding_conflict", nil)
		}
	}
	return nil
}

func validateControlOperationV2(operation ControlOperationV2) error {
	if operation.SchemaVersion != ControlOperationV2Schema ||
		!validControlExternalIdentity(operation.OperationID, "xco_") ||
		!validControlExternalIdentity(operation.InstallationID, "xci_") ||
		!validControlText(operation.ProjectID, 128) ||
		len(operation.EventScope) != 1 || operation.EventScope[0] != EventScopeModelCompleted ||
		operation.DesiredState != DesiredActive || operation.Kind != ControlKindConnect {
		return controlV2Error("operation_invalid", nil)
	}
	switch request := operation.Request.(type) {
	case PreparePausedSourceRequestV2:
		if operation.Phase != ControlPhasePreparePaused || operation.PredecessorOperationID != "" || operation.PredecessorResultSHA256 != "" ||
			request.FeedSchema != "unirouter.completed-execution.v1" || request.CursorKey != "unirouter.completed-execution.v1" || request.RequiredSourceState != DesiredPaused {
			return controlV2Error("phase_payload_invalid", nil)
		}
	case RegisterReceiverRequestV2:
		if !validReceiverNetworkBinding(request.NetworkBinding) {
			return controlV2Error("network_binding_invalid", nil)
		}
		if operation.Phase != ControlPhaseRegisterReceiver || !validControlPredecessor(operation) ||
			!validControlText(request.SourceInstance, 128) || request.ConsumerEpoch <= 0 ||
			!hashPattern.MatchString(request.CheckpointAuthoritySHA256) || request.RequiredSourceState != DesiredPaused ||
			!validReceiverRegistration(request.ReceiverRegistration) {
			return controlV2Error("phase_payload_invalid", nil)
		}
	case ResumeSourceRequestV2:
		if operation.Phase != ControlPhaseResumeSource || !validControlPredecessor(operation) ||
			!validControlText(request.SourceInstance, 128) || !hashPattern.MatchString(request.ReceiverRegistrationSHA256) ||
			!hashPattern.MatchString(request.NetworkBindingSHA256) || !hashPattern.MatchString(request.PausedAuthReceiptSHA256) ||
			!hashPattern.MatchString(request.DormantCredentialReceiptSHA256) || request.RequiredSourceState != DesiredActive ||
			request.RequiredReceiverState != DesiredActive || request.Readiness != "paused_receiver_authenticated" {
			return controlV2Error("phase_payload_invalid", nil)
		}
	default:
		return controlV2Error("phase_invalid", nil)
	}
	return nil
}

func validateControlResultV2(result ControlResultV2) error {
	if result.SchemaVersion != ControlResultV2Schema || !validControlExternalIdentity(result.OperationID, "xco_") ||
		!validControlExternalIdentity(result.InstallationID, "xci_") || !validControlPhase(result.Phase) ||
		!validControlObservedAt(result.ObservedAt) {
		return controlV2Error("result_invalid", nil)
	}
	if result.Status == ControlResultFailed {
		if result.Result != nil || !tokenPattern.MatchString(result.ReasonCode) {
			return controlV2Error("result_invalid", nil)
		}
		return nil
	}
	if result.Status != ControlResultCompleted || result.ReasonCode != "" {
		return controlV2Error("result_invalid", nil)
	}
	switch phaseResult := result.Result.(type) {
	case PreparePausedSourceResultV2:
		if result.Phase != ControlPhasePreparePaused || !validControlText(phaseResult.SourceInstance, 128) ||
			!validControlText(phaseResult.Cursor, 512) || phaseResult.HighWatermark != phaseResult.Cursor ||
			phaseResult.SourceState != DesiredPaused || phaseResult.Readiness != "paused_source_cursor_ready" {
			return controlV2Error("result_invalid", nil)
		}
	case RegisterReceiverResultV2:
		if result.Phase != ControlPhaseRegisterReceiver || !validControlText(phaseResult.SourceInstance, 128) ||
			!hashPattern.MatchString(phaseResult.ReceiverRegistrationSHA256) || !hashPattern.MatchString(phaseResult.NetworkBindingSHA256) ||
			phaseResult.SourceState != DesiredPaused || phaseResult.ReceiverState != "feed_staged" ||
			phaseResult.Readiness != "paused_receiver_registered" {
			return controlV2Error("result_invalid", nil)
		}
	case ResumeSourceResultV2:
		if result.Phase != ControlPhaseResumeSource || !validControlText(phaseResult.SourceInstance, 128) ||
			phaseResult.SourceState != DesiredActive || phaseResult.ReceiverState != DesiredActive ||
			phaseResult.Readiness != "source_resumed_receiver_active" {
			return controlV2Error("result_invalid", nil)
		}
	default:
		return controlV2Error("phase_invalid", nil)
	}
	return nil
}

func validControlPhase(phase string) bool {
	switch phase {
	case ControlPhasePreparePaused, ControlPhaseRegisterReceiver, ControlPhaseResumeSource:
		return true
	default:
		return false
	}
}

func validControlPredecessor(operation ControlOperationV2) bool {
	return validControlExternalIdentity(operation.PredecessorOperationID, "xco_") && hashPattern.MatchString(operation.PredecessorResultSHA256)
}

func validControlExternalIdentity(value, prefix string) bool {
	switch prefix {
	case "xci_":
		return remoteInstallationID.MatchString(value)
	case "xco_":
		return remoteOperationID.MatchString(value)
	default:
		return false
	}
}

func validReceiverRegistration(registration ReceiverRegistrationV1) bool {
	return registration.SchemaVersion == auth.ReceiverFeedRegistrationSchema &&
		registration.TokenFormat == auth.ReceiverFeedTokenFormat &&
		registration.VerifierScheme == auth.ReceiverFeedVerifierScheme &&
		controlKeyPrefixPattern.MatchString(registration.KeyPrefix) &&
		controlHexDigestPattern.MatchString(registration.VerifierSHA256) &&
		controlFeedOperationID.MatchString(registration.OperationID)
}

func validReceiverNetworkBinding(binding ReceiverNetworkBindingV2) bool {
	return binding.Transport == "https" && binding.Route == ControlFixedReceiverRoute && hashPattern.MatchString(binding.BindingSHA256)
}

func receiverRegistrationDigest(registration ReceiverRegistrationV1) (string, error) {
	if !validReceiverRegistration(registration) {
		return "", controlV2Error("phase_payload_invalid", nil)
	}
	canonical, err := json.Marshal(registration)
	if err != nil {
		return "", controlV2Error("canonicalization_failed", err)
	}
	return hashBytes(canonical), nil
}

// ReceiverRegistrationSHA256 returns the canonical identity already used by
// the frozen control-v2 result binding. Product phase executors use this
// wrapper instead of reimplementing JSON canonicalization.
func ReceiverRegistrationSHA256(registration ReceiverRegistrationV1) (string, error) {
	return receiverRegistrationDigest(registration)
}

func validControlText(value string, maximum int) bool {
	if value == "" || len(value) > maximum || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func validControlObservedAt(value string) bool {
	if !strings.HasSuffix(value, "Z") {
		return false
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && parsed.Location() == time.UTC && parsed.Format(time.RFC3339Nano) == value
}

func decodeStrictControlV2(raw []byte, destination any) error {
	if len(raw) == 0 {
		return controlV2Error("json_invalid", io.ErrUnexpectedEOF)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		if strings.Contains(err.Error(), "unknown field") {
			return controlV2Error("unknown_or_secret_field", err)
		}
		return controlV2Error("json_invalid", err)
	}
	if err := requireControlV2EOF(decoder); err != nil {
		return err
	}
	return nil
}

func rejectDuplicateControlV2Fields(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := walkControlV2JSON(decoder); err != nil {
		return err
	}
	return requireControlV2EOF(decoder)
}

func walkControlV2JSON(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return controlV2Error("json_invalid", err)
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return controlV2Error("json_invalid", err)
			}
			key, ok := keyToken.(string)
			if !ok {
				return controlV2Error("json_invalid", errors.New("object key is not a string"))
			}
			if _, exists := seen[key]; exists {
				return controlV2Error("duplicate_field", fmt.Errorf("duplicate field %q", key))
			}
			seen[key] = struct{}{}
			if err := walkControlV2JSON(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return controlV2Error("json_invalid", err)
		}
	case '[':
		for decoder.More() {
			if err := walkControlV2JSON(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return controlV2Error("json_invalid", err)
		}
	default:
		return controlV2Error("json_invalid", fmt.Errorf("unexpected delimiter %q", delimiter))
	}
	return nil
}

func requireControlV2EOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return controlV2Error("json_invalid", err)
	}
	return nil
}
