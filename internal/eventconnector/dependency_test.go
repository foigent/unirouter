package eventconnector

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestPhase2BNeutralKernelDependencyBoundary(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("resolve package root: %v", err)
	}
	allowedNonStandard := map[string]struct{}{
		"github.com/uniroute/unirouter/internal/eventconnector": {},
		"golang.org/x/text/unicode/norm":                        {},
	}
	forbiddenFragments := []string{
		"/internal/completedfeed",
		"/internal/db",
		"/internal/middleware",
		"/internal/gateway",
		"/cmd/",
	}

	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if parseErr != nil {
			t.Errorf("parse %s: %v", path, parseErr)
			return nil
		}
		for _, importSpec := range parsed.Imports {
			importPath, unquoteErr := strconv.Unquote(importSpec.Path.Value)
			if unquoteErr != nil {
				t.Errorf("unquote import in %s: %v", path, unquoteErr)
				continue
			}
			for _, fragment := range forbiddenFragments {
				if strings.Contains(importPath, fragment) {
					t.Errorf("neutral package imports forbidden runtime dependency %q in %s", importPath, path)
				}
			}
			if !strings.Contains(importPath, ".") {
				continue
			}
			if _, allowed := allowedNonStandard[importPath]; !allowed {
				t.Errorf("neutral package imports unapproved non-standard dependency %q in %s", importPath, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk neutral package: %v", err)
	}
}
