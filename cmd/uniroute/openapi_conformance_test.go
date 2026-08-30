package main

import (
	"bufio"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/uniroute/unirouter/internal/modelcapabilityqualification"
	"gopkg.in/yaml.v3"
)

func TestOpenAPI30AvoidsUnsupportedReusableOperationReferences(t *testing.T) {
	specBytes, err := os.ReadFile("../../docs/tech-ref/05-openapi-spec.yaml")
	if err != nil {
		t.Fatalf("read OpenAPI spec: %v", err)
	}
	spec := string(specBytes)

	// OpenAPI 3.0 Components has no pathItems field, and an HTTP method under a
	// Path Item must contain an Operation Object rather than a Reference Object.
	// YAML anchors are resolved before an OpenAPI consumer sees the document and
	// let the source retain reusable operation templates without either violation.
	for _, forbidden := range []string{"#/components/pathItems/", "\n  pathItems:\n"} {
		if strings.Contains(spec, forbidden) {
			t.Fatalf("OpenAPI 3.0 spec contains unsupported reusable operation construct %q", forbidden)
		}
	}
}

func TestOpenAPIFlowDescriptionsWithCommasAreQuoted(t *testing.T) {
	spec, err := os.Open("../../docs/tech-ref/05-openapi-spec.yaml")
	if err != nil {
		t.Fatalf("open OpenAPI spec: %v", err)
	}
	defer spec.Close()

	scanner := bufio.NewScanner(spec)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		braceIndex := strings.Index(line, "{")
		if braceIndex == -1 {
			continue
		}
		descriptionOffset := strings.Index(line[braceIndex+1:], "description:")
		if descriptionOffset == -1 {
			continue
		}
		descriptionIndex := braceIndex + 1 + descriptionOffset
		value := strings.TrimSpace(line[descriptionIndex+len("description:"):])
		if value == "" || value[0] == '\'' || value[0] == '"' {
			continue
		}
		if strings.Contains(value, ",") {
			t.Fatalf("line %d has an unquoted flow-style description containing a comma: %s", lineNumber, line)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan OpenAPI spec: %v", err)
	}
}

func TestOpenAPIModelCatalogRBACPreservesStableArrayShape(t *testing.T) {
	specBytes, err := os.ReadFile("../../docs/tech-ref/05-openapi-spec.yaml")
	if err != nil {
		t.Fatalf("read OpenAPI spec: %v", err)
	}
	var spec struct {
		Paths map[string]struct {
			RBAC map[string][]string `yaml:"x-unirouter-rbac"`
		} `yaml:"paths"`
	}
	if err := yaml.Unmarshal(specBytes, &spec); err != nil {
		t.Fatalf("parse OpenAPI YAML: %v", err)
	}
	got := spec.Paths["/api/v1/model-catalog"].RBAC["get"]
	want := []string{
		"platform_admin", "org_admin", "org_viewer",
		"project_admin", "project_user", "project_viewer",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("model-catalog x-unirouter-rbac.get must remain the canonical string-array contract: got=%v want=%v", got, want)
	}
}

func TestOpenAPIModelCapabilityQualificationIdentityEnumsMatchCompiledBindings(t *testing.T) {
	specBytes, err := os.ReadFile("../../docs/tech-ref/05-openapi-spec.yaml")
	if err != nil {
		t.Fatalf("read OpenAPI spec: %v", err)
	}
	schema := openAPISchemaSection(t, string(specBytes), "ModelCapabilityQualification")

	bindings := []modelcapabilityqualification.Binding{
		modelcapabilityqualification.DeepSeekFlashStreamingBinding(),
		modelcapabilityqualification.DeepSeekFlashLegacyStreamingBinding(),
		modelcapabilityqualification.DeepSeekFlashReasoningBinding(),
		modelcapabilityqualification.DeepSeekFlashLegacyReasoningBinding(),
		modelcapabilityqualification.DeepSeekFlashReasoningEffortBinding(),
		modelcapabilityqualification.DeepSeekFlashLegacyReasoningEffortBinding(),
	}
	bindings = append(bindings, modelcapabilityqualification.DeepSeekFlashCoreBindings()...)
	bindings = append(bindings, modelcapabilityqualification.DeepSeekFlashCurrentBindings()...)
	bindings = append(bindings, modelcapabilityqualification.DeepSeekProCurrentBindings()...)
	bindings = append(bindings, modelcapabilityqualification.KimiK26CurrentBindings()...)

	uniqueSorted := func(values []string) []string {
		seen := make(map[string]struct{}, len(values))
		for _, value := range values {
			seen[value] = struct{}{}
		}
		result := make([]string, 0, len(seen))
		for value := range seen {
			result = append(result, value)
		}
		sort.Strings(result)
		return result
	}
	compiled := map[string][]string{}
	for _, binding := range bindings {
		compiled["operation_id"] = append(compiled["operation_id"], binding.OperationID)
		compiled["capability_id"] = append(compiled["capability_id"], binding.CapabilityID)
		compiled["execution_profile_id"] = append(compiled["execution_profile_id"], binding.ExecutionProfileID)
		compiled["execution_profile_version"] = append(compiled["execution_profile_version"], binding.ExecutionProfileVersion)
		compiled["protocol_profile_id"] = append(compiled["protocol_profile_id"], binding.ProtocolProfileID)
		compiled["protocol_profile_version"] = append(compiled["protocol_profile_version"], binding.ProtocolProfileVersion)
		compiled["probe_contract_id"] = append(compiled["probe_contract_id"], binding.ProbeContractID)
		compiled["probe_contract_version"] = append(compiled["probe_contract_version"], binding.ProbeContractVersion)
	}

	for name, values := range compiled {
		got := uniqueSorted(openAPIFlowEnum(t, schema, name))
		want := uniqueSorted(values)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ModelCapabilityQualification.%s enum drifted from compiled bindings: got=%v want=%v", name, got, want)
		}
	}
}

func TestOpenAPIModelCapabilityContractErrorCodesCoverKimiValidators(t *testing.T) {
	specBytes, err := os.ReadFile("../../docs/tech-ref/05-openapi-spec.yaml")
	if err != nil {
		t.Fatalf("read OpenAPI spec: %v", err)
	}
	schema := openAPISchemaSection(t, string(specBytes), "ModelCapabilityContractErrorCode")
	declared := make(map[string]struct{})
	for _, line := range strings.Split(schema, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- ") {
			declared[strings.TrimSpace(strings.TrimPrefix(line, "- "))] = struct{}{}
		}
	}
	for _, code := range []string{
		"probe_contract", "expected_model", "content_type", "terminal_order", "data_after_terminal",
		"json", "reported_model", "usage_order", "choices", "reasoning_content", "content",
		"finish_reason", "unsupported_field", "read", "response_too_large", "tool_calls",
		"tool_call_identity", "tool_arguments", "structured_output", "usage", "usage_total", "cached_tokens",
		"usage_conflict", "cached_tokens_conflict", "reasoning_tokens", "json_unknown_field",
		"json_type_mismatch", "json_duplicate_field", "json_decode_error",
	} {
		if _, ok := declared[code]; !ok {
			t.Fatalf("ModelCapabilityContractErrorCode omits Kimi validator code %q", code)
		}
	}
	if !strings.Contains(schema, "^json_unknown_field_[A-Za-z0-9_.-]{1,64}$") {
		t.Fatal("ModelCapabilityContractErrorCode omits the bounded Kimi unknown-field diagnostic pattern")
	}
}

func openAPISchemaSection(t *testing.T, spec, name string) string {
	t.Helper()
	lines := strings.Split(spec, "\n")
	start := -1
	for index, line := range lines {
		if line == "    "+name+":" {
			start = index
			break
		}
	}
	if start == -1 {
		t.Fatalf("OpenAPI schema %s is missing", name)
	}
	end := len(lines)
	for index := start + 1; index < len(lines); index++ {
		line := lines[index]
		if strings.HasPrefix(line, "    ") && len(line) > 4 && line[4] != ' ' && strings.HasSuffix(strings.TrimSpace(line), ":") {
			end = index
			break
		}
	}
	return strings.Join(lines[start:end], "\n")
}

func openAPIFlowEnum(t *testing.T, schema, property string) []string {
	t.Helper()
	marker := "\n        " + property + ": {"
	start := strings.Index(schema, marker)
	if start < 0 {
		t.Fatalf("OpenAPI property %s is missing", property)
	}
	line := schema[start+1:]
	if end := strings.IndexByte(line, '\n'); end >= 0 {
		line = line[:end]
	}
	enumStart := strings.Index(line, "enum: [")
	if enumStart < 0 {
		t.Fatalf("OpenAPI property %s has no flow enum", property)
	}
	values := line[enumStart+len("enum: ["):]
	if end := strings.IndexByte(values, ']'); end >= 0 {
		values = values[:end]
	} else {
		t.Fatalf("OpenAPI property %s has an unterminated flow enum", property)
	}
	parts := strings.Split(values, ",")
	for index := range parts {
		parts[index] = strings.TrimSpace(parts[index])
	}
	return parts
}

func TestOpenAPIWalletRepresentsUnprovisionedStateWithoutMutationAuthority(t *testing.T) {
	specBytes, err := os.ReadFile("../../docs/tech-ref/05-openapi-spec.yaml")
	if err != nil {
		t.Fatalf("read OpenAPI spec: %v", err)
	}
	spec := string(specBytes)
	for _, fragment := range []string{
		"summary: Read the organization wallet or its explicit unprovisioned state in canonical micro-CNY",
		"When `exists=false`, all monetary facts are exact zero strings and wallet-specific mutations remain unavailable; this read never creates a wallet.",
		"required: [id, org_id, org_name, exists, balance_micros, granted_balance_micros, frozen_micros, total_spent_micros, threshold_micros, currency, amount_scale, auto_freeze, low_balance_threshold_cents, is_admin, can_top_up, can_adjust, can_export_transactions, can_select_org, created_at, updated_at]",
		"exists: { type: boolean, description: False only when the organization exists but no runtime settlement-wallet row has been provisioned. }",
		"can_top_up: { type: boolean, description: True only when the actor is authorized and the wallet exists. }",
		"can_adjust: { type: boolean, description: True only when the actor is authorized and the wallet exists. }",
		"can_export_transactions: { type: boolean, description: Read-only export authority; an unprovisioned wallet yields an empty transaction history rather than disabling this read. }",
		"created_at: { type: string, format: date-time, nullable: true }",
		"updated_at: { type: string, format: date-time, nullable: true }",
	} {
		if !strings.Contains(spec, fragment) {
			t.Fatalf("OpenAPI wallet unprovisioned-state contract missing %q", fragment)
		}
	}
}
