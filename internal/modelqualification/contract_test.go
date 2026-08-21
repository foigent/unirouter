package modelqualification

import (
	"errors"
	"testing"

	"github.com/uniroute/unirouter/internal/endpointprotocol"
)

func TestCompiledBindingsAreExact(t *testing.T) {
	tests := []struct {
		operation string
		path      string
		profile   string
	}{
		{"openai.responses.v1", "/v1/responses", "unirouter.openai.responses.text.v1"},
		{"anthropic.messages.v1", "/v1/messages", "unirouter.anthropic.messages.text.v1"},
	}
	for _, test := range tests {
		binding, err := CompiledBinding(test.operation)
		if err != nil {
			t.Fatalf("CompiledBinding(%q): %v", test.operation, err)
		}
		if binding.PublicPath != test.path || binding.ProtocolProfileID != test.profile ||
			binding.ProtocolProfileVersion != "1.0.0" || binding.ExecutorContractVersion != ExecutorContractVersion {
			t.Fatalf("binding=%+v", binding)
		}
		if err := ValidateCompiledBinding(binding); err != nil {
			t.Fatalf("ValidateCompiledBinding(%q): %v", test.operation, err)
		}
		publicBinding, err := CompiledPublicBinding(test.operation)
		if err != nil {
			t.Fatalf("CompiledPublicBinding(%q): %v", test.operation, err)
		}
		if publicBinding.ModelOperationID != binding.OperationID ||
			publicBinding.PublicPath != binding.PublicPath ||
			publicBinding.ProfileID != binding.ProtocolProfileID ||
			publicBinding.ProfileVersion != binding.ProtocolProfileVersion ||
			publicBinding.ExecutorContractVersion != binding.ExecutorContractVersion ||
			publicBinding.DeliveryContractVersion != endpointprotocol.PublicDeliveryContractV1 ||
			publicBinding.ChargeContractVersion != endpointprotocol.PublicCustomerChargeContractV1 ||
			publicBinding.IdempotencyContractVersion != endpointprotocol.PublicIdempotencyContractV1 {
			t.Fatalf("public binding=%+v", publicBinding)
		}
		if err := ValidateCompiledPublicBinding(publicBinding); err != nil {
			t.Fatalf("ValidateCompiledPublicBinding(%q): %v", test.operation, err)
		}
		publicBinding.ChargeContractVersion = "other"
		if err := ValidateCompiledPublicBinding(publicBinding); !errors.Is(err, ErrBindingMismatch) {
			t.Fatalf("mismatched public binding err=%v", err)
		}
	}
	if _, err := CompiledBinding("openai.chat.completions.v1"); !errors.Is(err, ErrUnsupportedOperation) {
		t.Fatalf("unsupported err=%v", err)
	}
}

func TestNormalizeHTTPSOrigin(t *testing.T) {
	for input, want := range map[string]string{
		"https://API.Example.com":             "https://api.example.com",
		" https://api.example.com:8443/ ":     "https://api.example.com:8443",
		"https://[2001:db8::1]:443":           "https://[2001:db8::1]:443",
		"https://[2001:0DB8:0:0:0:0:0:1]:443": "https://[2001:db8::1]:443",
	} {
		got, err := NormalizeHTTPSOrigin(input)
		if err != nil || got != want {
			t.Fatalf("NormalizeHTTPSOrigin(%q)=(%q,%v), want %q", input, got, err, want)
		}
	}
	for _, input := range []string{
		"http://api.example.com", "https://api.example.com/v1", "https://user@api.example.com",
		"https://api.example.com?x=1", "https://api.example.com#x", "https://api.example.com:70000",
		"https://api .example.com", "https://bad_label.example.com", "https://999.999.999.999",
		"https://[not-ip]", "https://api.example.com:", "https://[2001:db8::1]:", "\thttps://api.example.com\t",
		"https://[127.0.0.1]", "https://[::ffff:192.0.2.1]", "https://[::192.0.2.1]",
		"\rhttps://api.example.com\n", "\u00a0https://api.example.com\u00a0", "api.example.com",
	} {
		if _, err := NormalizeHTTPSOrigin(input); !errors.Is(err, ErrHTTPSOriginInvalid) {
			t.Fatalf("NormalizeHTTPSOrigin(%q) err=%v", input, err)
		}
	}
}
