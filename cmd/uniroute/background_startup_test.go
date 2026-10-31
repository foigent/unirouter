package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

// Audit actual production call sites, in addition to testing the startup
// helper. Removing any immediate writer from the common group fails here.
func TestBackgroundStartupProductionWiring(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", nil, 0)
	require.NoError(t, err)
	var groups []*ast.CallExpr
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if s, ok := call.Fun.(*ast.SelectorExpr); ok {
			if receiver, ok := s.X.(*ast.Ident); ok && receiver.Name == "background" && s.Sel.Name == "Go" {
				groups = append(groups, call)
			}
		}
		return true
	})
	writers := map[string]bool{"mediaWorker.Run": false, "sourceFactsSvc.RunRetention": false,
		"outboxSvc.Run": false, "completedFeedSvc.RunRetention": false, "billingSvc.RunSettlementRecoveryLoop": false,
		"startXInsightConnectorRuntime": false, "startCleanupLoop": false}
	ast.Inspect(file, func(node ast.Node) bool {
		var name string
		switch n := node.(type) {
		case *ast.SelectorExpr:
			if r, ok := n.X.(*ast.Ident); ok {
				name = r.Name + "." + n.Sel.Name
			}
		case *ast.Ident:
			name = n.Name
		}
		if _, watched := writers[name]; !watched {
			return true
		}
		for _, group := range groups {
			if node.Pos() >= group.Pos() && node.End() <= group.End() {
				writers[name] = true
				return true
			}
		}
		// The function declaration is not a launch site.
		if n, ok := node.(*ast.Ident); ok && n.Obj != nil && n.Obj.Decl != nil {
			if d, ok := n.Obj.Decl.(*ast.FuncDecl); ok && d.Name == n {
				return true
			}
		}
		t.Errorf("business writer %s is outside production background group", name)
		return true
	})
	for name, wired := range writers {
		require.True(t, wired, "production writer missing: %s", name)
	}
}

func TestBackgroundStartupNativeRenewalKeepsHeldCandidateReadyIntegration(t *testing.T) {
	pool := testutil.SetupMigratedTestDB(t)
	ledger, err := schemaauthority.ExpectedLedger("../../migrations")
	require.NoError(t, err)
	wire := []byte("synthetic whole-fleet withdrawal")
	empty := runtimegeneration.IngressPlan{ChangeID: "held-native-renewal", ExpectedConfigDigest: fmt.Sprintf("%064d", 0),
		DesiredConfigBytes: wire, DesiredConfigDigest: fmt.Sprintf("%x", sha256.Sum256(wire)),
		Before: []runtimegeneration.Identity{}, After: []runtimegeneration.Identity{}}
	_, err = runtimegeneration.PrepareIngress(t.Context(), pool, empty)
	require.NoError(t, err)
	_, err = runtimegeneration.CompleteIngress(t.Context(), pool, empty.ChangeID, empty.DesiredConfigDigest, true, json.RawMessage(`{"synthetic":true}`))
	require.NoError(t, err)
	runtime := testHTTPRuntime(t)
	runtime.artifact = "sha256:" + fmt.Sprintf("%064d", 1)
	startup := runtimegeneration.NewBackgroundStartup()
	defer startup.Stop()
	runtime.background = startup
	workerDone := startup.Go(t.Context(), func(context.Context) {})
	startup.Start(nil)
	stop, done, err := startRuntimeGeneration(t.Context(), pool, runtime, ledger, ledger, startup)
	require.NoError(t, err)
	defer func() { stop(); <-done }()
	require.False(t, startup.Admitted())
	require.False(t, startup.Ready())
	require.Equal(t, "serving", runtime.status().State)
	require.True(t, runtime.canClaimRecovery(), "a lease is live, but not permission to launch a candidate's recovery loop")
	handler := runtime.handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	require.Equal(t, http.StatusOK, w.Code)
	join := empty
	join.ChangeID, join.PredecessorID, join.ExpectedConfigDigest = "held-native-join", empty.ChangeID, empty.DesiredConfigDigest
	join.After = []runtimegeneration.Identity{{Instance: runtime.instance, Artifact: runtime.artifact}}
	_, err = runtimegeneration.PrepareIngress(t.Context(), pool, join)
	require.NoError(t, err)
	require.False(t, startup.Ready())
	_, err = runtimegeneration.CompleteIngress(t.Context(), pool, join.ChangeID, join.DesiredConfigDigest, true, json.RawMessage(`{"synthetic":true}`))
	require.NoError(t, err)
	// The actual existing renewal ticker releases startup; no second poller or
	// test-only admission callback substitutes for the production wiring.
	select {
	case <-startup.Done():
	case <-time.After(runtimegeneration.LeaseRefresh + 5*time.Second):
		t.Fatal("native renewal did not observe completed ingress")
	}
	require.NoError(t, startup.Err())
	<-workerDone
	require.True(t, startup.Ready())
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	require.Equal(t, http.StatusOK, w.Code)
}
