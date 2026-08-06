package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestProductionUserRoleSQLDoesNotDependOnLegacyWriterCompatibility(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	allowedRoleOnlyUpdate := filepath.Join("internal", "admin", "project_membership_service.go")
	for _, subtree := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(repoRoot, subtree), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			relativePath, err := filepath.Rel(repoRoot, path)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(node ast.Node) bool {
				literal, ok := node.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					return true
				}
				value, err := strconv.Unquote(literal.Value)
				if err != nil {
					return true
				}
				normalized := strings.ToLower(strings.Join(strings.Fields(value), " "))
				if strings.Contains(normalized, "insert into users") {
					columnsEnd := strings.Index(normalized, ")")
					if columnsEnd > 0 {
						columns := normalized[:columnsEnd]
						if strings.Contains(columns, " role") && !strings.Contains(columns, "organization_role") {
							t.Errorf("%s contains a current users.role INSERT without organization_role", relativePath)
						}
					}
				}
				if strings.Contains(normalized, "update users set role =") && relativePath != allowedRoleOnlyUpdate {
					t.Errorf("%s contains an unreviewed current users.role-only UPDATE", relativePath)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("scan production Go under %s: %v", subtree, err)
		}
	}

	userHandlers, err := os.ReadFile(filepath.Join(repoRoot, "internal", "admin", "user_handlers.go"))
	if err != nil {
		t.Fatalf("read current user writer: %v", err)
	}
	source := string(userHandlers)
	for _, fragment := range []string{
		`INSERT INTO users (org_id, username, password_hash, organization_role, role, status)`,
		`sets = append(sets, fmt.Sprintf("organization_role = $%d", len(args)+1))`,
		`sets = append(sets, fmt.Sprintf("role = $%d", len(args)+1))`,
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("current user writer lost explicit dual-axis contract %q", fragment)
		}
	}
}
