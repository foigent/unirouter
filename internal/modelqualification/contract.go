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
	ExecutorContractVersion = "unirouter.endpoint-profile-nonstream-executor.v1"
	LiveProbeSourceKind     = "admin.endpointprotocol_live_probe.v1"
	ApprovalSourceKind      = "admin.model_operation_production_approval.v1"
	PublicBindingSourceKind = "runtime.public_execution_binding.v1"
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

var compiledBindings = map[string]Binding{
	string(modeloperation.IDResponses): {
		OperationID:             string(modeloperation.IDResponses),
		PublicPath:              modeloperation.PathResponses,
		ProtocolProfileID:       endpointprotocol.ProfileOpenAIResponsesText,
		ProtocolProfileVersion:  "1.0.0",
		ExecutorContractVersion: ExecutorContractVersion,
	},
	string(modeloperation.IDMessages): {
		OperationID:             string(modeloperation.IDMessages),
		PublicPath:              modeloperation.PathMessages,
		ProtocolProfileID:       endpointprotocol.ProfileAnthropicMessagesText,
		ProtocolProfileVersion:  "1.0.0",
		ExecutorContractVersion: ExecutorContractVersion,
	},
}

func IsQualificationRequired(operationID string) bool {
	_, ok := compiledBindings[operationID]
	return ok
}

func CompiledBinding(operationID string) (Binding, error) {
	binding, ok := compiledBindings[operationID]
	if !ok {
		return Binding{}, fmt.Errorf("%w: %s", ErrUnsupportedOperation, operationID)
	}
	return binding, nil
}

func ValidateCompiledBinding(binding Binding) error {
	expected, err := CompiledBinding(binding.OperationID)
	if err != nil {
		return err
	}
	if binding != expected {
		return fmt.Errorf("%w for %s", ErrBindingMismatch, binding.OperationID)
	}
	return nil
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
	return endpointprotocol.PublicContractIdentity{
		ModelOperationID:           binding.OperationID,
		PublicPath:                 binding.PublicPath,
		ExecutorContractVersion:    binding.ExecutorContractVersion,
		ProfileID:                  binding.ProtocolProfileID,
		ProfileVersion:             binding.ProtocolProfileVersion,
		DeliveryContractVersion:    endpointprotocol.PublicDeliveryContractV1,
		ChargeContractVersion:      endpointprotocol.PublicCustomerChargeContractV1,
		IdempotencyContractVersion: endpointprotocol.PublicIdempotencyContractV1,
	}, nil
}

func ValidateCompiledPublicBinding(binding endpointprotocol.PublicContractIdentity) error {
	expected, err := CompiledPublicBinding(binding.ModelOperationID)
	if err != nil {
		return err
	}
	if binding != expected {
		return fmt.Errorf("%w for %s", ErrBindingMismatch, binding.ModelOperationID)
	}
	return nil
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
