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
	"github.com/uniroute/unirouter/internal/metering"
	"github.com/uniroute/unirouter/internal/migrate"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
)

// The host controller holds the installed ingress directory lock and proves
// prior proxy workers have exited. This owner repeats live binary checks under
// the existing migration lock. Neither authorizes arbitrary future schemas.
type compatibleExpandInput struct {
	TargetVersion    string                       `json:"target_version"`
	Environment      string                       `json:"environment"`
	SystemIdentifier string                       `json:"system_identifier"`
	MigrationDigest  string                       `json:"migration_digest"`
	Fleet            map[string]httpRuntimeStatus `json:"fleet"`
	SupportManifests map[string]json.RawMessage   `json:"support_manifests"`
}

var compatibleExpandHTTPClient = func() *http.Client {
	return &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
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
	if input.TargetVersion == "" {
		input.TargetVersion = schemaauthority.CapabilityExpandHead
	}
	heads := schemaauthority.SupportedSchemaHeads()
	targetIndex := slices.Index(heads, input.TargetVersion)
	if targetIndex < 1 {
		return fmt.Errorf("unknown exact expansion target")
	}
	predecessor := heads[targetIndex-1]
	dir := configuredMigrationsDir(cfg)
	ledger, err := schemaauthority.ExpectedLedger(dir)
	if err != nil {
		return err
	}
	if _, err = schemaauthority.RuntimeLedgers(ledger); err != nil {
		return err
	}
	wire, err := os.ReadFile(filepath.Join(dir, input.TargetVersion+".up.sql"))
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
	client := compatibleExpandHTTPClient()
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
		if input.TargetVersion == schemaauthority.StreamCounterExpandHead || input.TargetVersion == schemaauthority.SourceFactsExpandHead || input.TargetVersion == schemaauthority.ObservationExpandHead || input.TargetVersion == schemaauthority.BudgetSnapshotExpandHead || (input.TargetVersion == schemaauthority.ContentCaptureExpandHead || input.TargetVersion == schemaauthority.ContentReclaimExpandHead || input.TargetVersion == schemaauthority.PreDispatchExpandHead || (input.TargetVersion == schemaauthority.ScopedContentExpandHead || (input.TargetVersion == schemaauthority.XInsightPolicyExpandHead || (input.TargetVersion == schemaauthority.XInsightAdoptionExpandHead || input.TargetVersion == schemaauthority.XInsightCredentialExpandHead)))) {
			if err := manifest.Require(input.TargetVersion, []modelprofile.ImplementationReference{{Kind: "stream_counter", ID: metering.GatewayCounterEstimateVersionV2}, {Kind: "stream_counter", ID: metering.GatewayCounterEstimateVersionV3}}); err != nil {
				return err
			}
			if !slices.Contains(health.SupportedSchemaHeads, input.TargetVersion) {
				return fmt.Errorf("runtime does not support requested expansion")
			}
		}
		if input.TargetVersion == schemaauthority.ObservationExpandHead || input.TargetVersion == schemaauthority.BudgetSnapshotExpandHead || (input.TargetVersion == schemaauthority.ContentCaptureExpandHead || input.TargetVersion == schemaauthority.ContentReclaimExpandHead || input.TargetVersion == schemaauthority.PreDispatchExpandHead || (input.TargetVersion == schemaauthority.ScopedContentExpandHead || (input.TargetVersion == schemaauthority.XInsightPolicyExpandHead || (input.TargetVersion == schemaauthority.XInsightAdoptionExpandHead || input.TargetVersion == schemaauthority.XInsightCredentialExpandHead)))) {
			if err := manifest.Require(input.TargetVersion, []modelprofile.ImplementationReference{{Kind: "source_observations", ID: "unirouter.source-observation.v1"}, {Kind: "source_dispatch", ID: "unirouter.source-dispatch-outbox.v1"}}); err != nil {
				return err
			}
		}
		if input.TargetVersion == schemaauthority.BudgetSnapshotExpandHead || (input.TargetVersion == schemaauthority.ContentCaptureExpandHead || input.TargetVersion == schemaauthority.ContentReclaimExpandHead || input.TargetVersion == schemaauthority.PreDispatchExpandHead || (input.TargetVersion == schemaauthority.ScopedContentExpandHead || (input.TargetVersion == schemaauthority.XInsightPolicyExpandHead || (input.TargetVersion == schemaauthority.XInsightAdoptionExpandHead || input.TargetVersion == schemaauthority.XInsightCredentialExpandHead)))) {
			if err := manifest.Require(input.TargetVersion, runtimegeneration.BudgetCoverageRequirements()); err != nil {
				return err
			}
		}
		if input.TargetVersion == schemaauthority.ContentCaptureExpandHead || input.TargetVersion == schemaauthority.ContentReclaimExpandHead || input.TargetVersion == schemaauthority.PreDispatchExpandHead || (input.TargetVersion == schemaauthority.ScopedContentExpandHead || (input.TargetVersion == schemaauthority.XInsightPolicyExpandHead || (input.TargetVersion == schemaauthority.XInsightAdoptionExpandHead || input.TargetVersion == schemaauthority.XInsightCredentialExpandHead))) {
			if err := manifest.Require(input.TargetVersion, runtimegeneration.ContentLifecycleRequirements()); err != nil {
				return err
			}
		}
		if input.TargetVersion == schemaauthority.PreDispatchExpandHead || (input.TargetVersion == schemaauthority.ScopedContentExpandHead || (input.TargetVersion == schemaauthority.XInsightPolicyExpandHead || (input.TargetVersion == schemaauthority.XInsightAdoptionExpandHead || input.TargetVersion == schemaauthority.XInsightCredentialExpandHead))) {
			if err := manifest.Require(input.TargetVersion, []modelprofile.ImplementationReference{{Kind: "source_observations", ID: "unirouter.source-observation.v2"}}); err != nil {
				return err
			}
		}
		if input.TargetVersion == schemaauthority.ScopedContentExpandHead || (input.TargetVersion == schemaauthority.XInsightPolicyExpandHead || (input.TargetVersion == schemaauthority.XInsightAdoptionExpandHead || input.TargetVersion == schemaauthority.XInsightCredentialExpandHead)) {
			if err := manifest.Require(input.TargetVersion, runtimegeneration.ScopedContentRequirements()); err != nil {
				return err
			}
		}
		if input.TargetVersion == schemaauthority.XInsightPolicyExpandHead || (input.TargetVersion == schemaauthority.XInsightAdoptionExpandHead || input.TargetVersion == schemaauthority.XInsightCredentialExpandHead) {
			if err := manifest.Require(input.TargetVersion, runtimegeneration.XInsightPolicyRequirements()); err != nil {
				return err
			}
		}
		if input.TargetVersion == schemaauthority.XInsightAdoptionExpandHead || input.TargetVersion == schemaauthority.XInsightCredentialExpandHead {
			if err := manifest.Require(input.TargetVersion, runtimegeneration.XInsightAdoptionRequirements()); err != nil {
				return err
			}
		}
		if input.TargetVersion == schemaauthority.XInsightCredentialExpandHead {
			if err := manifest.Require(input.TargetVersion, runtimegeneration.XInsightCredentialRequirements()); err != nil {
				return err
			}
		}
		manifests[host] = manifest
		if input.TargetVersion == schemaauthority.SourceFactsExpandHead || input.TargetVersion == schemaauthority.ObservationExpandHead || input.TargetVersion == schemaauthority.BudgetSnapshotExpandHead || (input.TargetVersion == schemaauthority.ContentCaptureExpandHead || input.TargetVersion == schemaauthority.ContentReclaimExpandHead || input.TargetVersion == schemaauthority.PreDispatchExpandHead || (input.TargetVersion == schemaauthority.ScopedContentExpandHead || (input.TargetVersion == schemaauthority.XInsightPolicyExpandHead || (input.TargetVersion == schemaauthority.XInsightAdoptionExpandHead || input.TargetVersion == schemaauthority.XInsightCredentialExpandHead)))) {
			if err := manifest.Require(input.TargetVersion, []modelprofile.ImplementationReference{{Kind: "source_facts", ID: "unirouter.source-fact.v1"}}); err != nil {
				return err
			}
		}
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
		if actual[len(actual)-1] != predecessor && actual[len(actual)-1] != input.TargetVersion {
			return fmt.Errorf("expansion predecessor differs")
		}
		if input.TargetVersion == schemaauthority.StreamCounterExpandHead || input.TargetVersion == schemaauthority.SourceFactsExpandHead || input.TargetVersion == schemaauthority.ObservationExpandHead || input.TargetVersion == schemaauthority.BudgetSnapshotExpandHead || (input.TargetVersion == schemaauthority.ContentCaptureExpandHead || input.TargetVersion == schemaauthority.ContentReclaimExpandHead || input.TargetVersion == schemaauthority.PreDispatchExpandHead || (input.TargetVersion == schemaauthority.ScopedContentExpandHead || (input.TargetVersion == schemaauthority.XInsightPolicyExpandHead || (input.TargetVersion == schemaauthority.XInsightAdoptionExpandHead || input.TargetVersion == schemaauthority.XInsightCredentialExpandHead)))) {
			if err := verifyCompatibleRecoveryFleet(ctx, conn, input.TargetVersion, instances); err != nil {
				return err
			}
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
		err = migrate.RunCompatibleExpand(ctx, postgres.Pool, dir, migrate.CompatibleExpandHooks{Version: input.TargetVersion, ExpectedSHA256: input.MigrationDigest, Before: preflight,
			After: func(ctx context.Context, tx pgx.Tx) error {
				if input.TargetVersion == schemaauthority.BudgetSnapshotExpandHead {
					return runtimegeneration.CertifyBudgetCoverageTx(ctx, tx, manifests["uniroute-api"], input.MigrationDigest)
				}
				if input.TargetVersion == schemaauthority.StreamCounterExpandHead || input.TargetVersion == schemaauthority.SourceFactsExpandHead || input.TargetVersion == schemaauthority.ObservationExpandHead || input.TargetVersion == schemaauthority.BudgetSnapshotExpandHead || (input.TargetVersion == schemaauthority.ContentCaptureExpandHead || input.TargetVersion == schemaauthority.ContentReclaimExpandHead || input.TargetVersion == schemaauthority.PreDispatchExpandHead || (input.TargetVersion == schemaauthority.ScopedContentExpandHead || (input.TargetVersion == schemaauthority.XInsightPolicyExpandHead || (input.TargetVersion == schemaauthority.XInsightAdoptionExpandHead || input.TargetVersion == schemaauthority.XInsightCredentialExpandHead)))) {
					return nil
				} // Existing actors renew against the new head; never re-bootstrap M114.
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

// Dormant identities remain recovery identities, not retired rows. M117/M118/M119
// may retain them only when their own immutable manifest supports the complete
// target contract. The host controller still proves the actual process fleet.
func verifyCompatibleRecoveryFleet(ctx context.Context, conn *pgxpool.Conn, target string, instances map[string]bool) error {
	retainedRecovery := target == schemaauthority.ObservationExpandHead || target == schemaauthority.BudgetSnapshotExpandHead || (target == schemaauthority.ContentCaptureExpandHead || target == schemaauthority.ContentReclaimExpandHead || target == schemaauthority.PreDispatchExpandHead || target == schemaauthority.ScopedContentExpandHead || (target == schemaauthority.XInsightPolicyExpandHead || (target == schemaauthority.XInsightAdoptionExpandHead || target == schemaauthority.XInsightCredentialExpandHead)))
	inventory := `SELECT g.instance_id,(g.ingress_state<>'absent' OR g.lease_expires_at>clock_timestamp()),COALESCE(m.manifest_digest,''),m.canonical_bytes
FROM runtime_generations g LEFT JOIN runtime_support_manifests m USING(artifact_digest) WHERE g.state<>'stopped'`
	if !retainedRecovery {
		inventory += ` AND (g.ingress_state<>'absent' OR g.lease_expires_at>clock_timestamp())`
	}
	rows, err := conn.Query(ctx, inventory)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var instance, digest string
		var active bool
		var wire []byte
		if err := rows.Scan(&instance, &active, &digest, &wire); err != nil {
			return err
		}
		if instances[instance] {
			continue
		}
		if active || !retainedRecovery {
			return fmt.Errorf("unaccounted serving or recovery generation")
		}
		if err := verifyDormantExpansionManifest(wire, digest, target); err != nil {
			return fmt.Errorf("unsupported dormant recovery generation: %w", err)
		}
	}
	return rows.Err()
}

func verifyDormantExpansionManifest(wire []byte, digest, target string) error {
	manifest, err := runtimegeneration.VerifyManifest(wire, digest)
	if err != nil {
		return err
	}
	refs := []modelprofile.ImplementationReference{
		{Kind: "stream_counter", ID: metering.GatewayCounterEstimateVersionV2},
		{Kind: "stream_counter", ID: metering.GatewayCounterEstimateVersionV3},
		{Kind: "source_facts", ID: "unirouter.source-fact.v1"},
		{Kind: "source_observations", ID: "unirouter.source-observation.v1"},
		{Kind: "source_dispatch", ID: "unirouter.source-dispatch-outbox.v1"},
	}
	if target == schemaauthority.BudgetSnapshotExpandHead || (target == schemaauthority.ContentCaptureExpandHead || target == schemaauthority.ContentReclaimExpandHead || target == schemaauthority.PreDispatchExpandHead || target == schemaauthority.ScopedContentExpandHead || (target == schemaauthority.XInsightPolicyExpandHead || (target == schemaauthority.XInsightAdoptionExpandHead || target == schemaauthority.XInsightCredentialExpandHead))) {
		refs = append(refs, runtimegeneration.BudgetCoverageRequirements()...)
	}
	if target == schemaauthority.ContentCaptureExpandHead || target == schemaauthority.ContentReclaimExpandHead || target == schemaauthority.PreDispatchExpandHead || target == schemaauthority.ScopedContentExpandHead || (target == schemaauthority.XInsightPolicyExpandHead || (target == schemaauthority.XInsightAdoptionExpandHead || target == schemaauthority.XInsightCredentialExpandHead)) {
		refs = append(refs, runtimegeneration.ContentLifecycleRequirements()...)
	}
	if target == schemaauthority.PreDispatchExpandHead || target == schemaauthority.ScopedContentExpandHead || (target == schemaauthority.XInsightPolicyExpandHead || (target == schemaauthority.XInsightAdoptionExpandHead || target == schemaauthority.XInsightCredentialExpandHead)) {
		refs = append(refs, modelprofile.ImplementationReference{Kind: "source_observations", ID: "unirouter.source-observation.v2"})
	}
	if target == schemaauthority.ScopedContentExpandHead || (target == schemaauthority.XInsightPolicyExpandHead || (target == schemaauthority.XInsightAdoptionExpandHead || target == schemaauthority.XInsightCredentialExpandHead)) {
		refs = append(refs, runtimegeneration.ScopedContentRequirements()...)
	}
	if target == schemaauthority.XInsightPolicyExpandHead || (target == schemaauthority.XInsightAdoptionExpandHead || target == schemaauthority.XInsightCredentialExpandHead) {
		refs = append(refs, runtimegeneration.XInsightPolicyRequirements()...)
	}
	if target == schemaauthority.XInsightAdoptionExpandHead || target == schemaauthority.XInsightCredentialExpandHead {
		refs = append(refs, runtimegeneration.XInsightAdoptionRequirements()...)
	}
	if target == schemaauthority.XInsightCredentialExpandHead {
		refs = append(refs, runtimegeneration.XInsightCredentialRequirements()...)
	}
	return manifest.Require(target, refs)
}
