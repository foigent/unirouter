package modelcapabilityqualification

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"mime"
	"strings"
	"unicode/utf8"

	"github.com/uniroute/unirouter/internal/adapters"
)

const (
	DeepSeekSSEMaxResponseBytes = 1 << 20
	DeepSeekSSEMaxLineBytes     = 256 << 10
)

var ErrInvalidDeepSeekSSE = errors.New("invalid DeepSeek Chat SSE probe response")

type deepSeekSSEValidationError struct {
	code  string
	cause error
}

func (e *deepSeekSSEValidationError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", ErrInvalidDeepSeekSSE, e.code, e.cause)
	}
	return fmt.Sprintf("%s: %s", ErrInvalidDeepSeekSSE, e.code)
}

func (e *deepSeekSSEValidationError) Unwrap() error {
	return ErrInvalidDeepSeekSSE
}

func invalidDeepSeekSSE(code string, cause ...error) error {
	validationErr := &deepSeekSSEValidationError{code: code}
	if len(cause) > 0 {
		validationErr.cause = cause[0]
	}
	return validationErr
}

// DeepSeekSSEErrorCode returns a bounded diagnostic category suitable for an
// immutable probe receipt. It never includes provider response content.
func DeepSeekSSEErrorCode(err error) string {
	var validationErr *deepSeekSSEValidationError
	if errors.As(err, &validationErr) {
		return validationErr.code
	}
	return ""
}

type DeepSeekSSEObservation struct {
	ReportedModel     string
	DataEventCount    int
	DeltaCount        int
	ContentDeltaCount int
	FinishReason      string
	PromptTokens      int64
	CompletionTokens  int64
	TotalTokens       int64
	UsageSeen         bool
	DoneSeen          bool
	ResponseBytes     int
}

// ValidateDeepSeekChatSSE validates one immutable probe-contract version.
// Version 1.0.0 preserves DeepSeek's historical separate usage-only chunk;
// version 2.0.0 models the current final choice + usage chunk documented by
// DeepSeek. Callers must never reinterpret an old receipt with a new contract.
func ValidateDeepSeekChatSSE(
	probeContractID, probeContractVersion, contentType string,
	reader io.Reader,
) (DeepSeekSSEObservation, error) {
	return ValidateDeepSeekChatSSEForModel(
		probeContractID, probeContractVersion, "deepseek-v4-flash", contentType, reader,
	)
}

// ValidateDeepSeekChatSSEForModel applies the same immutable DeepSeek Chat SSE
// grammar to one binding-owned upstream model identity. The expected model is
// supplied by the exact capability contract, never by the client request.
func ValidateDeepSeekChatSSEForModel(
	probeContractID, probeContractVersion, expectedModel, contentType string,
	reader io.Reader,
) (DeepSeekSSEObservation, error) {
	expectedModel = strings.TrimSpace(expectedModel)
	if expectedModel == "" || safeReportedModel(expectedModel) != expectedModel {
		return DeepSeekSSEObservation{}, invalidDeepSeekSSE("expected_model")
	}
	profileID, profileVersion := "deepseek.v4-flash.api", "1.4.0"
	if probeContractVersion == DeepSeekChatSSEProbeContractCurrentVersion {
		profileVersion = "1.5.0"
		if expectedModel == "deepseek-v4-pro" {
			profileID = "deepseek.v4-pro.api"
		}
	}
	mechanics, found, err := adapters.ResolveDeepSeekProbeChatStreamMechanics(adapters.DeepSeekChatStreamProbeIdentity{
		OperationID: "openai.chat_completions.v1", CapabilityID: "streaming",
		ExecutionProfileID: profileID, ExecutionProfileVersion: profileVersion,
		ProtocolProfileID: "protocol.deepseek-openai-chat.v4", ProtocolProfileVersion: "1.0.0",
		ProbeContractID: probeContractID, ProbeContractVersion: probeContractVersion,
		ExpectedProvider: "deepseek", ExpectedUpstreamModel: expectedModel, PublicPath: "/v1/chat/completions",
	})
	if err != nil || !found {
		return DeepSeekSSEObservation{}, invalidDeepSeekSSE("probe_contract", err)
	}
	return validateDeepSeekChatSSE(mechanics, expectedModel, contentType, reader)
}

// ValidateDeepSeekChatSSEForBinding is the production entrypoint. It refuses
// any tuple that is not the exact current compiled binding before reading the
// provider response.
func ValidateDeepSeekChatSSEForBinding(binding Binding, contentType string, reader io.Reader) (DeepSeekSSEObservation, error) {
	resolved, err := Resolve(
		binding.OperationID, binding.CapabilityID,
		binding.ExecutionProfileID, binding.ExecutionProfileVersion,
		binding.ProtocolProfileID, binding.ProtocolProfileVersion,
	)
	if err != nil || resolved != binding {
		return DeepSeekSSEObservation{}, invalidDeepSeekSSE("probe_contract", err)
	}
	mechanics, found, err := adapters.ResolveDeepSeekProbeChatStreamMechanics(adapters.DeepSeekChatStreamProbeIdentity{
		OperationID: binding.OperationID, CapabilityID: binding.CapabilityID,
		ExecutionProfileID: binding.ExecutionProfileID, ExecutionProfileVersion: binding.ExecutionProfileVersion,
		ProtocolProfileID: binding.ProtocolProfileID, ProtocolProfileVersion: binding.ProtocolProfileVersion,
		ProbeContractID: binding.ProbeContractID, ProbeContractVersion: binding.ProbeContractVersion,
		ExpectedProvider: binding.ExpectedProvider, ExpectedUpstreamModel: binding.ExpectedUpstreamModel,
		PublicPath: binding.PublicPath,
	})
	if err != nil || !found {
		return DeepSeekSSEObservation{}, invalidDeepSeekSSE("probe_contract", err)
	}
	return validateDeepSeekChatSSE(mechanics, binding.ExpectedUpstreamModel, contentType, reader)
}

func validateDeepSeekChatSSE(
	mechanics adapters.DeepSeekChatStreamMechanics,
	expectedModel, contentType string,
	reader io.Reader,
) (DeepSeekSSEObservation, error) {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "text/event-stream" {
		return DeepSeekSSEObservation{}, invalidDeepSeekSSE("content_type")
	}
	limited := &io.LimitedReader{R: reader, N: DeepSeekSSEMaxResponseBytes + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 0, 64*1024), DeepSeekSSEMaxLineBytes)
	scanner.Split(adapters.SplitSSELines)
	assembler, err := adapters.NewChatSSEEventAssembler(DeepSeekSSEMaxResponseBytes + 1)
	if err != nil {
		return DeepSeekSSEObservation{}, invalidDeepSeekSSE("read", err)
	}
	state, err := adapters.NewDeepSeekChatStreamState(mechanics)
	if err != nil {
		return DeepSeekSSEObservation{}, invalidDeepSeekSSE("probe_contract", err)
	}
	var observation DeepSeekSSEObservation
	observed := func() DeepSeekSSEObservation {
		observation.ResponseBytes = DeepSeekSSEMaxResponseBytes + 1 - int(limited.N)
		return observation
	}
	applyReportedModel := func(reported string) error {
		if model := safeReportedModel(reported); model != "" {
			if observation.ReportedModel != "" && observation.ReportedModel != model {
				return invalidDeepSeekSSE("model_changed")
			}
			observation.ReportedModel = model
		}
		return nil
	}
	applyPrepared := func(prepared adapters.DeepSeekChatStreamPrepared) error {
		if err := applyReportedModel(prepared.ReportedModel); err != nil {
			return err
		}
		observation.DeltaCount += prepared.DeltaCount
		observation.ContentDeltaCount += prepared.ContentDeltaCount
		if prepared.FinishReason != "" {
			if observation.FinishReason != "" && observation.FinishReason != prepared.FinishReason {
				return invalidDeepSeekSSE("finish_reason_changed")
			}
			observation.FinishReason = prepared.FinishReason
		}
		if err := state.Accept(prepared); err != nil {
			return invalidDeepSeekSSEFromMechanics(err)
		}
		prepared, err = adapters.ParseDeepSeekChatStreamUsage(prepared)
		if err != nil {
			return invalidDeepSeekSSEFromMechanics(err)
		}
		if prepared.HasUsage {
			observation.PromptTokens = prepared.PromptTokens
			observation.CompletionTokens = prepared.CompletionTokens
			observation.TotalTokens = prepared.TotalTokens
			observation.UsageSeen = true
		}
		return nil
	}
	process := func(event adapters.ChatSSEEvent, ready bool, assembleErr error) error {
		if assembleErr != nil {
			return invalidDeepSeekSSE("unsupported_field", assembleErr)
		}
		if !ready {
			return nil
		}
		observation.DataEventCount++
		if string(event.Data) == "[DONE]" {
			if err := state.AcceptDone(); err != nil {
				return invalidDeepSeekSSEFromMechanics(err)
			}
			observation.DoneSeen = true
			return nil
		}
		if err := state.BeginDataEvent(); err != nil {
			return invalidDeepSeekSSEFromMechanics(err)
		}
		prepared, prepareErr := adapters.PrepareDeepSeekChatStreamEvent(event.Data, mechanics)
		if prepareErr != nil {
			if err := applyReportedModel(prepared.ReportedModel); err != nil {
				return err
			}
			observation.DeltaCount += prepared.DeltaCount
			observation.ContentDeltaCount += prepared.ContentDeltaCount
			if prepared.FinishReason != "" {
				if observation.FinishReason != "" && observation.FinishReason != prepared.FinishReason {
					return invalidDeepSeekSSE("finish_reason_changed")
				}
				observation.FinishReason = prepared.FinishReason
			}
			return invalidDeepSeekSSEFromMechanics(prepareErr)
		}
		return applyPrepared(prepared)
	}
	for scanner.Scan() {
		event, ready, assembleErr := assembler.AcceptLine(scanner.Text())
		if err := process(event, ready, assembleErr); err != nil {
			return observed(), err
		}
	}
	if err := scanner.Err(); err != nil {
		return observed(), invalidDeepSeekSSE("read", err)
	}
	if limited.N <= 0 {
		return observed(), invalidDeepSeekSSE("response_too_large")
	}
	event, ready, assembleErr := assembler.Finish()
	if err := process(event, ready, assembleErr); err != nil {
		return observed(), err
	}
	if observation.ReportedModel != expectedModel {
		return observed(), invalidDeepSeekSSE("reported_model")
	}
	if err := state.FinishEOF(); err != nil {
		return observed(), invalidDeepSeekSSEFromMechanics(err)
	}
	return observed(), nil
}

func invalidDeepSeekSSEFromMechanics(err error) error {
	issue := adapters.DeepSeekChatStreamIssueForError(err)
	if issue == "" {
		return invalidDeepSeekSSE("json", err)
	}
	return invalidDeepSeekSSE(string(issue), err)
}

func safeReportedModel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 200 || !utf8.ValidString(value) || strings.ContainsAny(value, "\x00\r\n") {
		return ""
	}
	return value
}
