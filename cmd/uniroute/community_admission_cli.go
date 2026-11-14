package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uniroute/unirouter/internal/config"
	"github.com/uniroute/unirouter/internal/installstate"
	"github.com/uniroute/unirouter/internal/runtimegeneration"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/upgradebundle"
	"github.com/uniroute/unirouter/internal/upstreamrelease"
)

type communityAdmissionRecoveryInput struct {
	InstallationID         string                              `json:"installation_id"`
	InstallationPlanDigest string                              `json:"installation_plan_digest"`
	DatabaseSystemID       string                              `json:"database_system_id"`
	Database               string                              `json:"database"`
	DatabaseOID            int64                               `json:"database_oid"`
	ReleaseSequence        int64                               `json:"release_sequence"`
	PackageSHA256          string                              `json:"package_sha256"`
	TrustedRingSHA256      string                              `json:"trusted_ring_sha256"`
	Admission              runtimegeneration.AdmissionRecovery `json:"admission"`
}

func runCommunityAdmissionRecovery(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, in communityAdmissionRecoveryInput, apply bool) (runtimegeneration.AdmissionWithdrawal, error) {
	var zero runtimegeneration.AdmissionWithdrawal
	const packagePath = "/upgrade/candidate.tar.gz"
	info, err := os.Lstat(packagePath)
	if err != nil || !info.Mode().IsRegular() {
		return zero, fmt.Errorf("admission recovery requires the retained signed installed package")
	}
	ring, err := communityTrustRingSHA256(cfg.UpgradePublicKeys)
	if err != nil || ring != in.TrustedRingSHA256 {
		return zero, fmt.Errorf("admission recovery installed trust ring changed")
	}
	options := pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead}
	if apply {
		options = pgx.TxOptions{IsoLevel: pgx.ReadCommitted}
	}
	tx, err := pool.BeginTx(ctx, options)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(context.Background())
	if apply {
		if err := lockCommunityMaintenance(ctx, tx); err != nil {
			return zero, err
		}
	}
	s, err := installstate.RequireOrdinary(ctx, tx)
	if err != nil {
		return zero, err
	}
	artifact, err := schemaauthority.ExpectedLedger(cfg.MigrationsDir)
	if err != nil {
		return zero, err
	}
	if len(artifact) < 2 || artifact[len(artifact)-1] != schemaauthority.UpgradeAdmissionRecoveryHead ||
		!slices.Equal(s.Ledger, artifact) || s.Record == nil || s.Record.State != "completed" ||
		s.Record.Plan.InstallationID != in.InstallationID || s.Record.Plan.Digest != in.InstallationPlanDigest ||
		s.Record.Plan.Target.Environment != cfg.RuntimeEnvironment {
		return zero, fmt.Errorf("admission recovery requires the exact completed M130 installation")
	}
	if err := upstreamrelease.CheckNamespace(ctx, tx); err != nil {
		return zero, err
	}
	var system, database string
	var oid int64
	if err := tx.QueryRow(ctx, `SELECT (SELECT system_identifier::text FROM pg_control_system()),
	current_database(),oid::bigint FROM pg_database WHERE datname=current_database()`).Scan(&system, &database, &oid); err != nil {
		return zero, err
	}
	if system != in.DatabaseSystemID || database != in.Database || oid != in.DatabaseOID {
		return zero, fmt.Errorf("admission recovery database changed")
	}
	for _, r := range in.Admission.Departed {
		if r.ExpectedDatabaseSystemID != system || r.ExpectedEnvironment != cfg.RuntimeEnvironment ||
			r.ExpectedSchemaHead != schemaauthority.UpgradeAdmissionRecoveryHead || r.ArtifactDigest != cfg.RuntimeImageDigest {
			return zero, fmt.Errorf("admission recovery writer differs from the installed artifact")
		}
	}
	state, err := readInstalledReleaseState(ctx, tx, apply)
	if err != nil {
		return zero, err
	}
	if state.ReleaseSequence != in.ReleaseSequence || state.PackageSHA256 != in.PackageSHA256 ||
		state.Version != cfg.RuntimeVersion || state.Commit != cfg.RuntimeCommit ||
		state.TargetSchema != schemaauthority.UpgradeAdmissionRecoveryHead {
		return zero, fmt.Errorf("admission recovery installed release changed")
	}
	var raw, singleton []byte
	var recorded upgradebundle.Manifest
	if err := tx.QueryRow(ctx, `SELECT h.manifest,s.manifest FROM release_history h JOIN release_state s
	ON s.singleton AND s.release_sequence=h.release_sequence AND s.package_sha256=h.package_sha256
	WHERE h.release_sequence=$1 AND h.package_sha256=$2 AND h.version=s.version AND h.commit=s.commit
	AND h.target_schema=s.target_schema AND h.trusted_key_id=s.trusted_key_id`,
		in.ReleaseSequence, in.PackageSHA256).Scan(&raw, &singleton); err != nil {
		return zero, err
	}
	if err := json.Unmarshal(raw, &recorded); err != nil {
		return zero, err
	}
	var current upgradebundle.Manifest
	if json.Unmarshal(singleton, &current) != nil || !reflect.DeepEqual(recorded, current) ||
		recorded.ReleaseSequence != state.ReleaseSequence || recorded.KeyID != state.TrustedKeyID ||
		recorded.Version != state.Version || recorded.Commit != state.Commit || recorded.TargetSchema != state.TargetSchema {
		return zero, fmt.Errorf("admission recovery manifest history changed")
	}
	verified, err := verifyInstalledPackage(packagePath, cfg.UpgradePublicKeys,
		recorded.MinCurrentSequence, recorded.MinCurrentSchema, state.UpdatedAt, 0)
	if err != nil {
		return zero, err
	}
	if verified.BundleSHA256 != state.PackageSHA256 || !reflect.DeepEqual(verified.Manifest, recorded) {
		return zero, fmt.Errorf("retained package does not match the exact recorded signed release")
	}
	result, err := runtimegeneration.RecoverAdmissionTx(ctx, tx, in.Admission, apply)
	if err != nil {
		return zero, err
	}
	if err := tx.Commit(ctx); err != nil {
		return zero, fmt.Errorf("admission withdrawal commit unconfirmed: %w", err)
	}
	return result, nil
}
