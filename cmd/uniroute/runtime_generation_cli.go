package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/db"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/strictjson"
)

// This CLI is the existing Target operator channel, not an HTTP admin bypass.
// The host ingress controller supplies exact observed Docker/config identities;
// the database owner serializes membership intent with Model Slot activation.
func runRuntimeGenerationCLI(ctx context.Context, args []string, cfg *config.Config) error {
	if len(args) == 1 && args[0] == "support" {
		m := runtimegeneration.CurrentManifest(schemaauthority.SupportedSchemaHeads())
		wire, err := m.CanonicalBytes()
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(append(wire, '\n'))
		return err
	}
	if len(args) != 1 || (args[0] != "inspect" && args[0] != "prepare" && args[0] != "complete" && args[0] != "read" && args[0] != "stop-check" && args[0] != "stop-apply") {
		return fmt.Errorf("usage: runtime-generation <support|inspect|prepare|complete|read|stop-check|stop-apply>; commands accept typed JSON on stdin")
	}
	var plan runtimegeneration.IngressPlan
	var complete struct {
		ChangeID       string          `json:"change_id"`
		ObservedDigest string          `json:"observed_digest"`
		OldWorkersGone bool            `json:"old_workers_gone"`
		Observation    json.RawMessage `json:"observation"`
	}
	var read struct {
		ChangeID string `json:"change_id"`
	}
	var retirement runtimegeneration.RetirementInput
	if args[0] != "inspect" {
		var target any = &plan
		if args[0] == "complete" {
			target = &complete
		}
		if args[0] == "read" {
			target = &read
		}
		if args[0] == "stop-check" || args[0] == "stop-apply" {
			target = &retirement
		}
		if err := readBoundedCLIJSON(os.Stdin, 2<<20, target); err != nil {
			return err
		}
	}
	if (args[0] == "stop-check" || args[0] == "stop-apply") && retirement.ExpectedEnvironment != cfg.RuntimeEnvironment {
		return fmt.Errorf("retirement Target environment differs from runtime configuration")
	}
	// A one-off control command must not outlive its bounded Artifact carrier.
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	postgres, err := db.NewPostgres(cfg.DB_DSN, cfg.DBMaxConnections, cfg.DBMaxIdleConns)
	if err != nil {
		return err
	}
	defer postgres.Close()
	ledger, err := schemaauthority.ExpectedLedger(configuredMigrationsDir(cfg))
	if err != nil {
		return err
	}
	actual, err := readCompatibleRuntimeSchema(ctx, postgres.Pool, ledger)
	if err != nil {
		return err
	}
	head := actual[len(actual)-1]
	if head != schemaauthority.CapabilityExpandHead && head != schemaauthority.StreamCounterExpandHead && head != schemaauthority.SourceFactsExpandHead && head != schemaauthority.ObservationExpandHead && head != schemaauthority.BudgetSnapshotExpandHead && head != schemaauthority.ContentCaptureExpandHead && head != schemaauthority.ContentReclaimExpandHead && head != schemaauthority.PreDispatchExpandHead && head != schemaauthority.ScopedContentExpandHead && head != schemaauthority.XInsightPolicyExpandHead && (head != schemaauthority.XInsightAdoptionExpandHead && head != schemaauthority.XInsightCredentialExpandHead) {
		return fmt.Errorf("runtime generation owner requires the exact expanded schema")
	}
	var result any
	switch args[0] {
	case "inspect":
		var encoded string
		err = postgres.Pool.QueryRow(ctx, `SELECT jsonb_build_object('head',COALESCE((SELECT c.change_id FROM runtime_ingress_changes c WHERE NOT EXISTS(SELECT 1 FROM runtime_ingress_changes n WHERE n.predecessor_id=c.change_id)),''),'head_status',COALESCE((SELECT c.status FROM runtime_ingress_changes c WHERE NOT EXISTS(SELECT 1 FROM runtime_ingress_changes n WHERE n.predecessor_id=c.change_id)),''),'generations',COALESCE((SELECT jsonb_agg(jsonb_build_object('instance',g.instance_id,'artifact_digest',g.artifact_digest,'compatibility_epoch',g.compatibility_epoch,'schema_head',g.schema_head,'revision',g.revision,'state',g.state,'ingress_state',g.ingress_state,'lease_live',g.lease_expires_at>clock_timestamp(),'support_manifest_digest',m.manifest_digest) ORDER BY g.instance_id) FROM runtime_generations g JOIN runtime_support_manifests m USING(artifact_digest)),'[]'::jsonb))::text`).Scan(&encoded)
		result = json.RawMessage(encoded)
	case "prepare":
		var status string
		status, err = runtimegeneration.PrepareIngress(ctx, postgres.Pool, plan)
		result = map[string]string{"change_id": plan.ChangeID, "status": status}
	case "complete":
		var status string
		status, err = runtimegeneration.CompleteIngress(ctx, postgres.Pool, complete.ChangeID, complete.ObservedDigest, complete.OldWorkersGone, complete.Observation)
		result = map[string]string{"change_id": complete.ChangeID, "status": status}
	case "read":
		var p runtimegeneration.IngressPlan
		var status string
		p, status, err = runtimegeneration.ReadIngress(ctx, postgres.Pool, read.ChangeID)
		result = map[string]any{"plan": p, "status": status}
	case "stop-check":
		result, err = runtimegeneration.CheckRetirement(ctx, postgres.Pool, retirement)
	case "stop-apply":
		result, err = runtimegeneration.ApplyRetirement(ctx, postgres.Pool, retirement)
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}

func readBoundedCLIJSON(r io.Reader, maximum int64, target any) error {
	wire, err := io.ReadAll(io.LimitReader(r, maximum+1))
	if err != nil {
		return err
	}
	if len(wire) == 0 || int64(len(wire)) > maximum {
		return fmt.Errorf("CLI document size is invalid")
	}
	if err := strictjson.Validate(wire); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(wire))
	d.DisallowUnknownFields()
	return d.Decode(target)
}
