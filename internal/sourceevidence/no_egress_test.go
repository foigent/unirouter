package sourceevidence

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSourceEvidenceProducerHasNoEgressClientDependency(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	packageDir := filepath.Dir(file)
	repoRoot := filepath.Clean(filepath.Join(packageDir, "..", ".."))
	paths := []string{
		filepath.Join(packageDir, "contract.go"),
		filepath.Join(packageDir, "generator.go"),
		filepath.Join(packageDir, "neutral.go"),
		filepath.Join(packageDir, "package.go"),
		filepath.Join(packageDir, "receipts.go"),
		filepath.Join(repoRoot, "cmd", "uniroute-source-evidence", "main.go"),
	}
	forbidden := []string{
		"net",
		"net/http",
		"github.com/redis/go-redis",
		"github.com/aws/aws-sdk-go-v2",
		"internal/gateway",
		"internal/provider",
	}
	files := token.NewFileSet()
	for _, path := range paths {
		parsed, err := parser.ParseFile(files, path, nil, parser.ImportsOnly)
		require.NoError(t, err)
		for _, declaration := range parsed.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, specification := range general.Specs {
				importSpec, ok := specification.(*ast.ImportSpec)
				if !ok {
					continue
				}
				name, err := strconv.Unquote(importSpec.Path.Value)
				require.NoError(t, err)
				for _, prefix := range forbidden {
					require.Falsef(t, name == prefix || strings.HasPrefix(name, prefix+"/"), "%s imports egress-capable dependency %s", path, name)
				}
			}
		}
	}
}
