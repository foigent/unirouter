package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/migrate"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
)

// The host controller holds the installed ingress directory lock and proves
// prior proxy workers have exited. This owner repeats live binary checks under
// the existing migration lock. Neither authorizes arbitrary future schemas.
type compatibleExpandInput struct {
	Environment      string                       `json:"environment"`
	SystemIdentifier string                       `json:"system_identifier"`
	MigrationDigest  string                       `json:"migration_digest"`
	Fleet            map[string]httpRuntimeStatus `json:"fleet"`
	SupportManifests map[string]json.RawMessage   `json:"support_manifests"`
}

func checkBridgeRuntime(expected, actual httpRuntimeStatus, schemaDigest string) error {
	identity := runtimegeneration.Identity{Instance: actual.Instance, Artifact: actual.Artifact}
	if identity.Validate() != nil || expected.Instance != actual.Instance || expected.Artifact != actual.Artifact || actual.State != "serving" ||
		actual.SchemaLedgerDigest != schemaDigest || expected.SchemaLedgerDigest != schemaDigest ||
		actual.CompatibilityEpoch != runtimegeneration.CompatibilityEpoch ||
		!slices.Contains(actual.SupportedSchemaHeads, schemaauthority.LegacyCapabilityHead) ||
		!slices.Contains(actual.SupportedSchemaHeads, schemaauthority.CapabilityExpandHead) ||
		actual.SupportManifestDigest == "" || actual.SupportManifestDigest != expected.SupportManifestDigest {
		return fmt.Errorf("live receiving instance is not the exact observed compatibility bridge")
	}
	return nil
}

func runCompatibleExpandCLI(ctx context.Context, args []string, cfg *config.Config) error {
	if len(args) != 1 || (args[0] != "check" && args[0] != "apply") {
		return fmt.Errorf("usage: compatible-expand <check|apply>, exact host-controller proof on stdin")
	}
	var input compatibleExpandInput
	if err := readBoundedCLIJSON(os.Stdin, 2*runtimegeneration.MaximumManifestBytes+(64<<10), &input); err != nil {
		return err
	}
	if input.Environment != cfg.RuntimeEnvironment || (input.Environment != "development" && input.Environment != "test" && input.Environment != "integration") || input.SystemIdentifier == "" || len(input.Fleet) != 2 || len(input.SupportManifests) != 2 {
		return fmt.Errorf("exact development/test Target and both receiving instances are required")
	}
	dir := configuredMigrationsDir(cfg)
	ledger, err := schemaauthority.ExpectedLedger(dir)
	if err != nil {
		return err
	}
	if _, err = schemaauthority.RuntimeLedgers(ledger); err != nil {
		return err
	}
	wire, err := os.ReadFile(filepath.Join(dir, schemaauthority.CapabilityExpandHead+".up.sql"))
	if err != nil {
		return err
	}
	if input.MigrationDigest != fmt.Sprintf("sha256:%x", sha256.Sum256(wire)) {
		return fmt.Errorf("approved expansion bytes differ from executing Artifact")
	}
	postgres, err := db.NewPostgres(cfg.DB_DSN, cfg.DBMaxConnections, cfg.DBMaxIdleConns)
	if err != nil {
		return err
	}
	defer postgres.Close()
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	manifests := make(map[string]runtimegeneration.Manifest, 2)
	instances := make(map[string]bool, 2)
	for _, host := range []string{"uniroute-api", "uniroute-api-canary"} {
		health, ok := input.Fleet[host]
		if !ok || instances[health.Instance] {
			return fmt.Errorf("two distinct exact bridge instances are required")
		}
		instances[health.Instance] = true
		manifest, err := runtimegeneration.VerifyManifest(input.SupportManifests[host], health.SupportManifestDigest)
		if err != nil {
			return fmt.Errorf("%s support manifest: %w", host, err)
		}
		manifests[host] = manifest
	}
	preflight := func(ctx context.Context, conn *pgxpool.Conn) error {
		var system string
		if err := conn.QueryRow(ctx, `SELECT system_identifier::text FROM pg_control_system()`).Scan(&system); err != nil {
			return err
		}
		if system != input.SystemIdentifier {
			return fmt.Errorf("database system identity changed")
		}
		actual, err := readCompatibleRuntimeSchema(ctx, conn, ledger)
		if err != nil {
			return err
		}
		if err = publication.VerifyRuntimeStaticProjection(ctx, conn, actual); err != nil {
			return err
		}
		encoded, _ := json.Marshal(actual)
		schemaDigest := fmt.Sprintf("%x", sha256.Sum256(encoded))
		for _, host := range []string{"uniroute-api", "uniroute-api-canary"} {
			expected, ok := input.Fleet[host]
			if !ok {
				return fmt.Errorf("exact stable/canary fleet proof is required")
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+host+":8080/health/ready", nil)
			if err != nil {
				return err
			}
			response, err := client.Do(req)
			if err != nil {
				return fmt.Errorf("bridge readiness unavailable for %s", host)
			}
			body, readErr := io.ReadAll(io.LimitReader(response.Body, 64<<10))
			response.Body.Close()
			var health struct {
				Runtime httpRuntimeStatus `json:"runtime"`
			}
			if readErr != nil || response.StatusCode != http.StatusOK || json.Unmarshal(body, &health) != nil {
				return fmt.Errorf("bridge readiness invalid for %s", host)
			}
			if err := checkBridgeRuntime(expected, health.Runtime, schemaDigest); err != nil {
				return fmt.Errorf("%s: %w", host, err)
			}
		}
		return nil
	}
	// check is read-only. Apply repeats preflight under the schema owner before
	// writing; the first check also covers an already-committed recovery read.
	err = migrate.WithSessionLock(ctx, postgres.Pool, func(conn *pgxpool.Conn) error { return preflight(ctx, conn) })
	if err == nil && args[0] == "apply" {
		err = migrate.RunCompatibleExpand(ctx, postgres.Pool, dir, migrate.CompatibleExpandHooks{ExpectedSHA256: input.MigrationDigest, Before: preflight,
			After: func(ctx context.Context, tx pgx.Tx) error {
				for _, host := range []string{"uniroute-api", "uniroute-api-canary"} {
					health := input.Fleet[host]
					if err := runtimegeneration.BootstrapVerifiedBridge(ctx, tx, runtimegeneration.Identity{Instance: health.Instance, Artifact: health.Artifact}, manifests[host], ledger); err != nil {
						return err
					}
				}
				return nil
			}})
	}
	if err != nil {
		return err
	}
	actual, err := verifyRuntimeAuthority(ctx, postgres.Pool, ledger)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"status": args[0], "schema_head": actual[len(actual)-1], "system_identifier": input.SystemIdentifier, "migration_digest": input.MigrationDigest})
}
