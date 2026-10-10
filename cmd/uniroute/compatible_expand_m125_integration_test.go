//go:build integration

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/testutil"
)

type m125ReadyTransport struct {
	pool  *pgxpool.Pool
	fleet map[string]httpRuntimeStatus
}

func (transport m125ReadyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	health, ok := transport.fleet[request.URL.Hostname()]
	if !ok || request.URL.Path != "/health/ready" || request.Method != http.MethodGet {
		return nil, fmt.Errorf("unexpected readiness request")
	}
	actual, err := readRuntimeSchemaLedger(request.Context(), transport.pool)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(actual)
	if err != nil {
		return nil, err
	}
	health.SchemaLedgerDigest = fmt.Sprintf("%x", sha256.Sum256(encoded))
	wire, err := json.Marshal(map[string]any{"runtime": health})
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(wire)), Request: request}, nil
}

func useM125ReadyTransport(t *testing.T, pool *pgxpool.Pool, fleet map[string]httpRuntimeStatus) {
	t.Helper()
	previous := compatibleExpandHTTPClient
	compatibleExpandHTTPClient = func() *http.Client {
		return &http.Client{Transport: m125ReadyTransport{pool: pool, fleet: fleet}}
	}
	t.Cleanup(func() { compatibleExpandHTTPClient = previous })
}

func invokeCompatibleExpandForTest(t *testing.T, cfg *config.Config, action string, input compatibleExpandInput) (json.RawMessage, error) {
	t.Helper()
	wire, err := json.Marshal(input)
	require.NoError(t, err)
	in, err := os.CreateTemp(t.TempDir(), "compatible-expand-input")
	require.NoError(t, err)
	defer in.Close()
	_, err = in.Write(wire)
	require.NoError(t, err)
	_, err = in.Seek(0, 0)
	require.NoError(t, err)
	out, err := os.CreateTemp(t.TempDir(), "compatible-expand-output")
	require.NoError(t, err)
	defer out.Close()
	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = in, out
	defer func() { os.Stdin, os.Stdout = oldIn, oldOut }()
	err = runCompatibleExpandCLI(context.Background(), []string{action}, cfg)
	result, readErr := os.ReadFile(out.Name())
	require.NoError(t, readErr)
	return result, err
}

func m125CLIInput(t *testing.T, pool *pgxpool.Pool) (*config.Config, compatibleExpandInput) {
	t.Helper()
	dir := testutil.MigrationsDir(t)
	artifactLedger, err := testutil.PreRetirementLedger(t)
	require.NoError(t, err)
	actual, err := readCompatibleRuntimeSchema(t.Context(), pool, artifactLedger)
	require.NoError(t, err)
	encoded, err := json.Marshal(actual)
	require.NoError(t, err)
	manifest := runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads())
	manifestWire, err := manifest.CanonicalBytes()
	require.NoError(t, err)
	manifestDigest, err := manifest.Digest()
	require.NoError(t, err)
	artifact := "sha256:" + strings.Repeat("a", 64)
	fleet := make(map[string]httpRuntimeStatus, 2)
	manifests := make(map[string]json.RawMessage, 2)
	for index, host := range []string{"uniroute-api", "uniroute-api-canary"} {
		identity := runtimegeneration.Identity{Instance: "uri_" + strings.Repeat(string(rune('b'+index)), 32), Artifact: artifact}
		_, _, err := runtimegeneration.Renew(t.Context(), pool, identity, manifest, artifactLedger, false)
		require.NoError(t, err)
		fleet[host] = httpRuntimeStatus{Instance: identity.Instance, Artifact: identity.Artifact,
			State: "serving", SchemaLedgerDigest: fmt.Sprintf("%x", sha256.Sum256(encoded)),
			SupportManifestDigest: manifestDigest, CompatibilityEpoch: runtimegeneration.CompatibilityEpoch,
			SupportedSchemaHeads: schemaauthority.SupportedSchemaHeads()}
		manifests[host] = manifestWire
	}
	var system string
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT system_identifier::text FROM pg_control_system()`).Scan(&system))
	up, err := os.ReadFile(filepath.Join(dir, schemaauthority.XInsightCredentialExpandHead+".up.sql"))
	require.NoError(t, err)
	cfg := &config.Config{DB_DSN: pool.Config().ConnString(), DBMaxConnections: 3,
		MigrationsDir: dir, RuntimeEnvironment: "development"}
	input := compatibleExpandInput{TargetVersion: schemaauthority.XInsightCredentialExpandHead,
		Environment: "development", SystemIdentifier: system,
		MigrationDigest: fmt.Sprintf("sha256:%x", sha256.Sum256(up)), Fleet: fleet, SupportManifests: manifests}
	return cfg, input
}

// M126 intentionally no longer serves the old compatible-expand bridge. The
// domain transition still has its historical-artifact tests; this CLI must refuse.
func TestM126CompatibleExpandCLIRejectsM124ToM125Integration(t *testing.T) {
	pool := testutil.SetupMigratedThroughTestDB(t, schemaauthority.XInsightAdoptionExpandHead)
	cfg, input := m125CLIInput(t, pool)
	useM125ReadyTransport(t, pool, input.Fleet)
	for _, action := range []string{"check", "apply"} {
		_, err := invokeCompatibleExpandForTest(t, cfg, action, input)
		require.ErrorContains(t, err, "outside this artifact")
		var head string
		require.NoError(t, pool.QueryRow(t.Context(), `SELECT max(version) FROM schema_migrations`).Scan(&head))
		require.Equal(t, schemaauthority.XInsightAdoptionExpandHead, head, "refusal must not mutate schema")
	}
}

func TestM125CompatibleExpandCLIRejectsNonadjacentPredecessorIntegration(t *testing.T) {
	pool := testutil.SetupMigratedThroughTestDB(t, schemaauthority.XInsightPolicyExpandHead)
	cfg, input := m125CLIInput(t, pool)
	useM125ReadyTransport(t, pool, input.Fleet)
	_, err := invokeCompatibleExpandForTest(t, cfg, "check", input)
	require.ErrorContains(t, err, "outside this artifact")
	var head string
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT max(version) FROM schema_migrations`).Scan(&head))
	require.Equal(t, schemaauthority.XInsightPolicyExpandHead, head)
	input.TargetVersion = "000126_unknown"
	_, err = invokeCompatibleExpandForTest(t, cfg, "check", input)
	require.ErrorContains(t, err, "unknown exact expansion target")
}
