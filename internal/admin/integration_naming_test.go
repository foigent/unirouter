package admin

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAdminIntegrationFilesUseIntegrationSuffix(t *testing.T) {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve admin test directory")
	}
	files, err := filepath.Glob(filepath.Join(filepath.Dir(currentFile), "*_integration_test.go"))
	if err != nil {
		t.Fatalf("list admin integration tests: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no admin integration test files found")
	}

	for _, file := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", filepath.Base(file), err)
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv != nil || !strings.HasPrefix(function.Name.Name, "Test") {
				continue
			}
			if !strings.HasSuffix(function.Name.Name, "Integration") {
				t.Errorf("%s: %s must end with Integration so the integration-only CI selector cannot omit it", filepath.Base(file), function.Name.Name)
			}
		}
	}
}
