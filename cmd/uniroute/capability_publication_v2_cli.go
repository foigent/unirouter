package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/db"
	publication "github.com/uniroute/unirouter/internal/modelcapabilitypublication"
	qualification "github.com/uniroute/unirouter/internal/modelcapabilityqualification"
	"github.com/uniroute/unirouter/internal/modelprofile"
	"github.com/uniroute/unirouter/internal/schemaauthority"
)

// The authenticated deployment operator is the approval authority, as for
// existing exact Artifact publication. scripts/publish-capability-bundle.py
// verifies official CI/source bytes on the operator host before streaming this
// envelope over SSH. There is no HTTP upload or second runtime writer.
type operatorPublicationInputV2 struct {
	PublicationID        string                         `json:"publication_id"`
	Kind                 string                         `json:"kind"`
	Compensates          string                         `json:"compensates_publication_id"`
	BundleBytes          []byte                         `json:"bundle_bytes"`
	ApprovedBundleDigest string                         `json:"approved_bundle_digest"`
	Carrier              *publication.CarrierIdentityV2 `json:"carrier"`
	PlanBytes            []byte                         `json:"plan_bytes"`
	PlanDigest           string                         `json:"plan_digest"`
}

func validateDataBinding(b publication.DefinitionBindingV2, d modelprofile.Definition) error {
	_, err := qualification.BindDefinition(qualification.Binding{OperationID: b.OperationID, CapabilityID: b.CapabilityID, ExecutionProfileID: b.ExecutionProfileID, ExecutionProfileVersion: b.ExecutionProfileVersion, ProtocolProfileID: b.ProtocolProfileID, ProtocolProfileVersion: b.ProtocolProfileVersion, ProbeContractID: b.ProbeContractID, ProbeContractVersion: b.ProbeContractVersion, ExpectedProvider: b.ExpectedProvider, ExpectedUpstreamModel: b.ExpectedUpstreamModel, PublicPath: b.PublicPath}, d)
	return err
}

func runCapabilityPublicationV2CLI(ctx context.Context, action string, args []string, cfg *config.Config) error {
	if len(args) != 0 {
		return fmt.Errorf("%s accepts only its bounded document on stdin", action)
	}
	if action == "check-bundle" {
		wire, err := io.ReadAll(io.LimitReader(os.Stdin, publication.MaximumBundleBytes+1))
		if err != nil {
			return err
		}
		if err := qualification.ValidateBundleSource(wire); err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]string{"status": "bundle_source_validated"})
	}
	var input operatorPublicationInputV2
	if err := readBoundedCLIJSON(os.Stdin, 48<<20, &input); err != nil {
		return err
	}
	if action == "plan-v2" && (len(input.PlanBytes) != 0 || input.PlanDigest != "") {
		return fmt.Errorf("planning cannot carry an existing Plan")
	}
	artifact, err := runtimePublicationArtifact(cfg)
	if err != nil {
		return err
	}
	ledger, err := schemaauthority.ExpectedLedger(configuredMigrationsDir(cfg))
	if err != nil {
		return err
	}
	authority := publication.AuthorityV2{Environment: cfg.RuntimeEnvironment, ArtifactMigrationLedger: ledger, Publisher: publication.PublisherIdentityV2{Artifact: artifact, Carrier: input.Carrier}, ApprovedBundleDigest: input.ApprovedBundleDigest, ValidateBinding: validateDataBinding}
	request := publication.PlanRequestV2{PublicationID: input.PublicationID, Kind: input.Kind, CompensatesPublicationID: input.Compensates, BundleBytes: input.BundleBytes}
	var approved publication.PlanV2
	if action != "plan-v2" {
		approved, err = publication.DecodePlanV2(input.PlanBytes, input.PlanDigest)
		if err != nil {
			return err
		}
	}
	postgres, err := db.NewPostgres(cfg.DB_DSN, cfg.DBMaxConnections, cfg.DBMaxIdleConns)
	if err != nil {
		return err
	}
	defer postgres.Close()
	switch action {
	case "plan-v2":
		tx, err := postgres.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		p, err := publication.BuildPlanV2(ctx, tx, authority, request)
		if err != nil {
			return err
		}
		wire, err := p.CanonicalBytes()
		if err != nil {
			return err
		}
		digest, err := p.Digest()
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"plan_bytes": wire, "plan_digest": digest})
	case "apply-v2":
		result, err := publication.ApplyV2(ctx, postgres.Pool, authority, request, approved)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(result)
	case "recover-v2":
		result, err := publication.RecoverV2(ctx, postgres.Pool, authority, approved)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(result)
	default:
		return fmt.Errorf("unknown capability publication action")
	}
}
