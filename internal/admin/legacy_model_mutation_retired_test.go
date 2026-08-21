package admin

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"testing"
)

type panicLegacyMutationBody struct{}

func (panicLegacyMutationBody) Read([]byte) (int, error) {
	panic("retired legacy mutation handler read the request body")
}

type retiredLegacyMutationErrorResponse struct {
	Error struct {
		Type    string `json:"type"`
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func TestLegacyModelMutationHandlersFailClosedBeforeRequestOrDependencies(t *testing.T) {
	var handler *Handler
	tests := map[string]func(http.ResponseWriter, *http.Request){
		"CreateChannel":                handler.CreateChannel,
		"UpdateChannel":                handler.UpdateChannel,
		"DeleteChannel":                handler.DeleteChannel,
		"TestChannel":                  handler.TestChannel,
		"CreateChannelKey":             handler.CreateChannelKey,
		"UpdateChannelKey":             handler.UpdateChannelKey,
		"DeleteChannelKey":             handler.DeleteChannelKey,
		"RotateChannelKey":             handler.RotateChannelKey,
		"ActivateModelEndpoint":        handler.ActivateModelEndpoint,
		"StartModelEndpointCanary":     handler.StartModelEndpointCanary,
		"RecoverModelEndpoint":         handler.RecoverModelEndpoint,
		"CreateRuntimeChange":          handler.CreateRuntimeChange,
		"ValidateRuntimeChange":        handler.ValidateRuntimeChange,
		"AdvanceRuntimeChangeShadow":   handler.AdvanceRuntimeChangeShadow,
		"ActivateInitialRuntimeChange": handler.ActivateInitialRuntimeChange,
		"StartRuntimeChangeCanary":     handler.StartRuntimeChangeCanary,
		"PromoteRuntimeChange":         handler.PromoteRuntimeChange,
		"RollbackRuntimeChange":        handler.RollbackRuntimeChange,
		"RejectRuntimeChange":          handler.RejectRuntimeChange,
	}

	for name, invoke := range tests {
		t.Run(name, func(t *testing.T) {
			request := &http.Request{Method: http.MethodPost, Body: io.NopCloser(panicLegacyMutationBody{})}
			response := httptest.NewRecorder()

			invoke(response, request)

			if response.Code != http.StatusGone {
				t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusGone, response.Body.String())
			}
			if got := response.Header().Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type = %q", got)
			}
			if got := response.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control = %q", got)
			}

			var body retiredLegacyMutationErrorResponse
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body.Error.Type != "gone_error" || body.Error.Code != legacyModelMutationRetiredCode || body.Error.Message != "legacy model mutation ingress is retired; use the canonical model deployment APIs" {
				t.Fatalf("response = %#v", body.Error)
			}
		})
	}
}

func TestLegacyModelMutationHandlersKeepRetirementFenceFirst(t *testing.T) {
	methodsByFile := map[string][]string{
		"channel_handlers.go":              {"CreateChannel", "UpdateChannel", "DeleteChannel"},
		"channel_key_handlers.go":          {"CreateChannelKey", "UpdateChannelKey", "DeleteChannelKey"},
		"channel_key_rotation_handlers.go": {"RotateChannelKey"},
		"test_handlers.go":                 {"TestChannel"},
		"model_endpoint_handlers.go":       {"ActivateModelEndpoint", "StartModelEndpointCanary", "RecoverModelEndpoint"},
		"runtime_change_handlers.go":       {"CreateRuntimeChange", "ValidateRuntimeChange", "AdvanceRuntimeChangeShadow", "ActivateInitialRuntimeChange", "StartRuntimeChangeCanary", "PromoteRuntimeChange", "RollbackRuntimeChange", "RejectRuntimeChange"},
	}
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}

	for file, methods := range methodsByFile {
		t.Run(file, func(t *testing.T) {
			parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(filepath.Dir(currentFile), file), nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", file, err)
			}
			found := make(map[string]bool, len(methods))
			for _, declaration := range parsed.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				if !ok || function.Recv == nil || !containsLegacyMutationMethod(methods, function.Name.Name) {
					continue
				}
				found[function.Name.Name] = true
				if function.Body == nil || len(function.Body.List) != 1 || !isLegacyMutationRetirementCall(function.Body.List[0]) {
					t.Fatalf("%s body must contain only writeLegacyModelMutationRetired", function.Name.Name)
				}
			}
			for _, method := range methods {
				if !found[method] {
					t.Fatalf("retired handler %s not found in %s", method, file)
				}
			}
		})
	}
}

func containsLegacyMutationMethod(methods []string, name string) bool {
	for _, method := range methods {
		if method == name {
			return true
		}
	}
	return false
}

func isLegacyMutationRetirementCall(statement ast.Stmt) bool {
	expression, ok := statement.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := expression.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	identifier, ok := call.Fun.(*ast.Ident)
	return ok && identifier.Name == "writeLegacyModelMutationRetired"
}
