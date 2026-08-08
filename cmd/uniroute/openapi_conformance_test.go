package main

import (
	"bufio"
	"os"
	"reflect"
	"strings"
	"testing"

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
