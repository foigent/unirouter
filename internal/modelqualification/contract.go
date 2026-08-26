// Package modelqualification owns the compiled half of the exact execution
// qualification contract for public model operations. Mutable production
// qualification is database-owned by migration 000075; this package never
// infers approval from a local protocol fixture or descriptor.
package modelqualification

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/modeloperation"
)

const (
	ExecutorContractVersion         = "unirouter.endpoint-profile-nonstream-executor.v1"
	ResponsesDualExecutorContractV1 = "unirouter.endpoint-profile-responses-dual-executor.v1"
	MessagesDualExecutorContractV1  = endpointprotocol.PublicMessagesDualExecutorContractV1
	MessagesToolsExecutorContractV1 = endpointprotocol.PublicMessagesToolsExecutorContractV1
	LiveProbeSourceKind             = "admin.endpointprotocol_live_probe.v1"
	ApprovalSourceKind              = "admin.model_operation_production_approval.v1"
	PublicBindingSourceKind         = "runtime.public_execution_binding.v1"
)

var (
	ErrUnsupportedOperation = errors.New("modelqualification: unsupported operation")
	ErrBindingMismatch      = errors.New("modelqualification: exact support binding mismatch")
	ErrHTTPSOriginInvalid   = errors.New("modelqualification: HTTPS origin is invalid")
	asciiDNSLabelPattern    = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?$`)
	numericHostPattern      = regexp.MustCompile(`^[0-9.]+$`)
)

// Binding is immutable compiled support. It must exactly match the current row
// in model_operation_execution_support_bindings before a SQL qualification can
// be consumed by runtime code.
type Binding struct {
	OperationID             string
	PublicPath              string
	ProtocolProfileID       string
	ProtocolProfileVersion  string
	ExecutorContractVersion string
}

var compiledBindings = map[string][]Binding{
	string(modeloperation.IDResponses): {
		{
			OperationID:             string(modeloperation.IDResponses),
			PublicPath:              modeloperation.PathResponses,
			ProtocolProfileID:       endpointprotocol.ProfileOpenAIResponsesText,
			ProtocolProfileVersion:  "1.0.0",
			ExecutorContractVersion: ExecutorContractVersion,
		},
		{
			OperationID:             string(modeloperation.IDResponses),
			PublicPath:              modeloperation.PathResponses,
			ProtocolProfileID:       endpointprotocol.ProfileOpenAIResponsesTextV2,
			ProtocolProfileVersion:  "1.0.0",
			ExecutorContractVersion: ResponsesDualExecutorContractV1,
		},
	},
	string(modeloperation.IDMessages): {
		{
			OperationID:             string(modeloperation.IDMessages),
			PublicPath:              modeloperation.PathMessages,
			ProtocolProfileID:       endpointprotocol.ProfileAnthropicMessagesText,
			ProtocolProfileVersion:  "1.0.0",
			ExecutorContractVersion: ExecutorContractVersion,
		},
		{
			OperationID:             string(modeloperation.IDMessages),
			PublicPath:              modeloperation.PathMessages,
			ProtocolProfileID:       endpointprotocol.ProfileAnthropicMessagesTextV2,
			ProtocolProfileVersion:  "1.0.0",
			ExecutorContractVersion: MessagesDualExecutorContractV1,
		},
		{
			OperationID:             string(modeloperation.IDMessages),
			PublicPath:              modeloperation.PathMessages,
			ProtocolProfileID:       endpointprotocol.ProfileAnthropicMessagesTextV3,
			ProtocolProfileVersion:  "1.0.0",
			ExecutorContractVersion: MessagesToolsExecutorContractV1,
		},
	},
}

func IsQualificationRequired(operationID string) bool {
	return len(compiledBindings[operationID]) != 0
}

// CompiledBinding preserves the historical operation-only convenience for
// callers that have not selected an exact Profile yet. Runtime qualification
// and release authoring must use CompiledBindingFor or ValidateCompiledBinding
// so adding an append-only protocol version cannot reinterpret this result.
func CompiledBinding(operationID string) (Binding, error) {
	bindings := compiledBindings[operationID]
	if len(bindings) == 0 {
		return Binding{}, fmt.Errorf("%w: %s", ErrUnsupportedOperation, operationID)
	}
	return bindings[0], nil
}

func CompiledBindingFor(operationID, protocolProfileID, protocolProfileVersion string) (Binding, error) {
	bindings := compiledBindings[operationID]
	if len(bindings) == 0 {
		return Binding{}, fmt.Errorf("%w: %s", ErrUnsupportedOperation, operationID)
	}
	var matched Binding
	found := false
	for _, binding := range bindings {
		if binding.ProtocolProfileID != protocolProfileID || binding.ProtocolProfileVersion != protocolProfileVersion {
			continue
		}
		if found {
			return Binding{}, fmt.Errorf("%w for %s", ErrBindingMismatch, operationID)
		}
		matched = binding
		found = true
	}
	if !found {
		return Binding{}, fmt.Errorf("%w for %s", ErrBindingMismatch, operationID)
	}
	return matched, nil
}

func ValidateCompiledBinding(binding Binding) error {
	bindings := compiledBindings[binding.OperationID]
	if len(bindings) == 0 {
		return fmt.Errorf("%w: %s", ErrUnsupportedOperation, binding.OperationID)
	}
	for _, expected := range bindings {
		if binding == expected {
			return nil
		}
	}
	return fmt.Errorf("%w for %s", ErrBindingMismatch, binding.OperationID)
}

// CompiledPublicBinding is the exact customer-visible execution contract.
// It is intentionally wider than Binding: a locally conforming provider
// profile or an M75 production approval does not imply delivery, charging, or
// idempotency semantics.
func CompiledPublicBinding(operationID string) (endpointprotocol.PublicContractIdentity, error) {
	binding, err := CompiledBinding(operationID)
	if err != nil {
		return endpointprotocol.PublicContractIdentity{}, err
	}
	return CompiledPublicBindingFor(binding)
}

// CompiledPublicBindingFor resolves the complete customer-visible contract
// from one exact compiled provider/executor binding. It is the runtime and
// release-authoring authority; operation-only selection is intentionally not.
func CompiledPublicBindingFor(binding Binding) (endpointprotocol.PublicContractIdentity, error) {
	if err := ValidateCompiledBinding(binding); err != nil {
		return endpointprotocol.PublicContractIdentity{}, err
	}
	delivery := endpointprotocol.PublicDeliveryContractV1
	if binding.ExecutorContractVersion == ResponsesDualExecutorContractV1 {
		delivery = endpointprotocol.PublicResponsesDeliveryContractV2
	} else if binding.ExecutorContractVersion == MessagesDualExecutorContractV1 {
		delivery = endpointprotocol.PublicAnthropicMessagesDeliveryContractV2
	} else if binding.ExecutorContractVersion == MessagesToolsExecutorContractV1 {
		delivery = endpointprotocol.PublicAnthropicMessagesToolsDeliveryV1
	}
	return endpointprotocol.PublicContractIdentity{
		ModelOperationID:           binding.OperationID,
		PublicPath:                 binding.PublicPath,
		ExecutorContractVersion:    binding.ExecutorContractVersion,
		ProfileID:                  binding.ProtocolProfileID,
		ProfileVersion:             binding.ProtocolProfileVersion,
		DeliveryContractVersion:    delivery,
		ChargeContractVersion:      endpointprotocol.PublicCustomerChargeContractV1,
		IdempotencyContractVersion: endpointprotocol.PublicIdempotencyContractV1,
	}, nil
}

func ValidateCompiledPublicBinding(binding endpointprotocol.PublicContractIdentity) error {
	bindings := compiledBindings[binding.ModelOperationID]
	if len(bindings) == 0 {
		return fmt.Errorf("%w: %s", ErrUnsupportedOperation, binding.ModelOperationID)
	}
	for _, compiled := range bindings {
		expected, err := CompiledPublicBindingFor(compiled)
		if err == nil && binding == expected {
			return nil
		}
	}
	return fmt.Errorf("%w for %s", ErrBindingMismatch, binding.ModelOperationID)
}

// NormalizeHTTPSOrigin accepts an exact HTTPS origin (an optional trailing
// slash is harmless) and returns its stable, path-free identity. Provider base
// URLs with a path are deliberately rejected for Responses/Messages because
// endpointprotocol owns the complete public-to-provider path mapping.
func NormalizeHTTPSOrigin(raw string) (string, error) {
	// Match model_operation_https_origin exactly: only an ordinary ASCII space
	// is insignificant around an origin. Controls and Unicode whitespace are
	// rejected so a Go caller and a raw SQL writer cannot create different
	// qualification identities from the same bytes.
	trimmed := strings.Trim(raw, " ")
	parsed, err := url.Parse(trimmed)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Host == "" || parsed.Hostname() == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawFragment != "" ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawPath != "" || strings.Contains(parsed.Host, "%") {
		return "", ErrHTTPSOriginInvalid
	}
	hostname := parsed.Hostname()
	isIPAddress := false
	if ip := net.ParseIP(hostname); ip != nil {
		isIPAddress = true
		// Embedded dotted-quad spellings have multiple cross-runtime text
		// renderings. Keep the shared origin grammar to canonical hexadecimal
		// IPv6 and ordinary unbracketed IPv4 only.
		if strings.HasPrefix(parsed.Host, "[") && strings.Contains(parsed.Host, ".") {
			return "", ErrHTTPSOriginInvalid
		}
		if ip.To4() != nil {
			// Bracketed IPv4 and IPv4-mapped IPv6 are deliberately outside the
			// one canonical origin grammar shared with PostgreSQL.
			if numericHostPattern.MatchString(hostname) && ip.String() != hostname {
				return "", ErrHTTPSOriginInvalid
			}
			hostname = ip.String()
		} else if !strings.HasPrefix(parsed.Host, "[") {
			return "", ErrHTTPSOriginInvalid
		} else {
			hostname = ip.String()
		}
	} else {
		if len(hostname) > 253 || numericHostPattern.MatchString(hostname) {
			return "", ErrHTTPSOriginInvalid
		}
		for _, label := range strings.Split(hostname, ".") {
			if len(label) == 0 || len(label) > 63 || !asciiDNSLabelPattern.MatchString(label) {
				return "", ErrHTTPSOriginInvalid
			}
		}
	}
	port := parsed.Port()
	if port != "" {
		value, parseErr := strconv.Atoi(port)
		if parseErr != nil || value < 1 || value > 65535 {
			return "", ErrHTTPSOriginInvalid
		}
	}
	authority := hostname
	if strings.Contains(hostname, ":") {
		authority = "[" + hostname + "]"
	}
	if port != "" {
		authority += ":" + port
	}
	if strings.HasSuffix(parsed.Host, ":") || (!isIPAddress && !strings.EqualFold(parsed.Host, authority)) {
		return "", ErrHTTPSOriginInvalid
	}
	return "https://" + strings.ToLower(authority), nil
}
