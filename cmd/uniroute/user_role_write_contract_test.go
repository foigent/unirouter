package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var (
	insertUsersSQLPattern = regexp.MustCompile(`(?is)\binsert\s+into\s+(?:public\.)?users\s*\(([^)]*)\)`)
	updateUsersSQLPattern = regexp.MustCompile(`(?is)\bupdate\s+(?:public\.)?users\b`)
	roleAssignmentPattern = regexp.MustCompile(`(?is)(?:^|[^[:alnum:]_])role\s*=`)
)

func TestProductionUserRoleSQLDoesNotDependOnLegacyWriterCompatibility(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	for _, subtree := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(repoRoot, subtree), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			fileSet := token.NewFileSet()
			file, err := parser.ParseFile(fileSet, path, nil, 0)
			if err != nil {
				return err
			}
			relativePath, err := filepath.Rel(repoRoot, path)
			if err != nil {
				return err
			}
			for _, declaration := range file.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				if !ok || function.Body == nil {
					continue
				}
				literals := stringLiterals(function.Body)
				combined := strings.Join(literals, "\n")
				for _, match := range insertUsersSQLPattern.FindAllStringSubmatch(combined, -1) {
					columns := sqlIdentifierSet(match[1])
					if _, writesRole := columns["role"]; writesRole {
						if _, writesOrganizationRole := columns["organization_role"]; !writesOrganizationRole {
							t.Errorf("%s:%s contains a users.role INSERT without organization_role", relativePath, function.Name.Name)
						}
					}
				}
				if !updateUsersSQLPattern.MatchString(combined) || !roleAssignmentPattern.MatchString(combined) {
					continue
				}
				switch {
				case relativePath == filepath.Join("internal", "admin", "user_handlers.go") && function.Name.Name == "UpdateUser":
					if !strings.Contains(combined, "organization_role") ||
						!callsFunction(function, "resolveOrganizationRolePointers") ||
						!callsFunction(function, "projectCompatibilityRoleTx") {
						t.Errorf("%s:%s lost validated dual-axis update construction", relativePath, function.Name.Name)
					}
				case relativePath == filepath.Join("internal", "admin", "project_membership_service.go") && function.Name.Name == "syncUserCompatibilityRoleTx":
					normalized := normalizeSQL(combined)
					const reviewedStatement = "update users set role = $2, auth_epoch = auth_epoch + 1, updated_at = now() where id = $1"
					if !strings.Contains(normalized, reviewedStatement) ||
						!callsFunction(function, "projectCompatibilityRoleTx") ||
						!containsIdentifier(function, "RoleMember") {
						t.Errorf("%s:%s no longer matches the reviewed conservative compatibility-shadow writer", relativePath, function.Name.Name)
					}
				default:
					t.Errorf("%s:%s contains an unreviewed users.role UPDATE", relativePath, function.Name.Name)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan production Go under %s: %v", subtree, err)
		}
	}

}

func TestUserRoleWriteContractPatternsAreFormattingIndependent(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		sql        string
		unsafeRole bool
	}{
		{name: "multiline omitted organization role", sql: `INSERT
INTO public.users (org_id,
  "role", status) VALUES ($1,$2,$3)`, unsafeRole: true},
		{name: "quoted dual axis", sql: `insert into users ("org_id", "organization_role", "role") values ($1,$2,$3)`},
		{name: "multiline role update", sql: "UPDATE public.users\n SET\n role = $2 WHERE id = $1", unsafeRole: true},
		{name: "organization role only is not compatibility shadow", sql: `UPDATE users SET organization_role = $2 WHERE id = $1`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			unsafeRole := false
			if match := insertUsersSQLPattern.FindStringSubmatch(testCase.sql); len(match) == 2 {
				columns := sqlIdentifierSet(match[1])
				_, writesRole := columns["role"]
				_, writesOrganizationRole := columns["organization_role"]
				unsafeRole = writesRole && !writesOrganizationRole
			}
			if updateUsersSQLPattern.MatchString(testCase.sql) && roleAssignmentPattern.MatchString(testCase.sql) {
				unsafeRole = true
			}
			if unsafeRole != testCase.unsafeRole {
				t.Fatalf("unsafeRole=%t, want %t for %q", unsafeRole, testCase.unsafeRole, testCase.sql)
			}
		})
	}
}

func stringLiterals(node ast.Node) []string {
	values := make([]string, 0)
	ast.Inspect(node, func(candidate ast.Node) bool {
		literal, ok := candidate.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(literal.Value)
		if err == nil {
			values = append(values, strings.ToLower(value))
		}
		return true
	})
	return values
}

func sqlIdentifierSet(raw string) map[string]struct{} {
	identifiers := make(map[string]struct{})
	for _, candidate := range strings.Split(raw, ",") {
		identifier := strings.Trim(strings.TrimSpace(strings.ToLower(candidate)), `"`)
		if identifier != "" {
			identifiers[identifier] = struct{}{}
		}
	}
	return identifiers
}

func callsFunction(function *ast.FuncDecl, name string) bool {
	found := false
	ast.Inspect(function.Body, func(candidate ast.Node) bool {
		call, ok := candidate.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch target := call.Fun.(type) {
		case *ast.Ident:
			found = found || target.Name == name
		case *ast.SelectorExpr:
			found = found || target.Sel.Name == name
		}
		return !found
	})
	return found
}

func containsIdentifier(function *ast.FuncDecl, name string) bool {
	found := false
	ast.Inspect(function.Body, func(candidate ast.Node) bool {
		identifier, ok := candidate.(*ast.Ident)
		if ok && identifier.Name == name {
			found = true
		}
		return !found
	})
	return found
}

func normalizeSQL(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}
