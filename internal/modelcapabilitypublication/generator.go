package modelcapabilitypublication

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uniroute/unirouter/internal/migrate"
)

const publicationMigrationMarker = migrate.CapabilityContractPublicationMarker

type GeneratedMigration struct {
	UpSQL     []byte
	DownSQL   []byte
	VerifySQL []byte
}

type migrationArtifactFile struct {
	suffix string
	wire   []byte
}

type migrationArtifactWriter struct {
	link func(string, string) error
}

// WriteGeneratedMigrationFiles materializes one compiled declaration as an
// all-or-nothing, fail-if-exists repository artifact set. It never writes a
// database and never overwrites an existing user file.
func WriteGeneratedMigrationFiles(dir string, predecessor Baseline, declaration Declaration) error {
	return writeGeneratedMigrationFiles(dir, predecessor, declaration, migrationArtifactWriter{link: os.Link})
}

func writeGeneratedMigrationFiles(dir string, predecessor Baseline, declaration Declaration, writer migrationArtifactWriter) error {
	generated, err := GenerateMigration(predecessor, declaration)
	if err != nil {
		return err
	}
	if writer.link == nil {
		return fmt.Errorf("%w: migration artifact linker is required", ErrInvalidDeclaration)
	}
	files := []migrationArtifactFile{
		{suffix: ".up.sql", wire: generated.UpSQL},
		{suffix: ".down.sql", wire: generated.DownSQL},
		{suffix: ".verify.sql", wire: generated.VerifySQL},
	}
	for _, file := range files {
		path := filepath.Join(dir, declaration.PublicationID+file.suffix)
		if _, statErr := os.Lstat(path); statErr == nil {
			return fmt.Errorf("%w: generated publication artifact already exists: %s", ErrInvalidDeclaration, path)
		} else if !os.IsNotExist(statErr) {
			return fmt.Errorf("inspect generated publication artifact %s: %w", path, statErr)
		}
	}

	type stagedArtifact struct{ staged, final string }
	staged := make([]stagedArtifact, 0, len(files))
	linked := make([]string, 0, len(files))
	cleanup := func() {
		for _, path := range linked {
			_ = os.Remove(path)
		}
		for _, artifact := range staged {
			_ = os.Remove(artifact.staged)
		}
	}
	for _, file := range files {
		temporary, createErr := os.CreateTemp(dir, "."+declaration.PublicationID+file.suffix+".*.staged")
		if createErr != nil {
			cleanup()
			return fmt.Errorf("stage generated publication artifact: %w", createErr)
		}
		artifact := stagedArtifact{staged: temporary.Name(), final: filepath.Join(dir, declaration.PublicationID+file.suffix)}
		staged = append(staged, artifact)
		if chmodErr := temporary.Chmod(0o644); chmodErr != nil {
			_ = temporary.Close()
			cleanup()
			return fmt.Errorf("set generated publication artifact mode: %w", chmodErr)
		}
		if _, writeErr := temporary.Write(file.wire); writeErr != nil {
			_ = temporary.Close()
			cleanup()
			return fmt.Errorf("write generated publication artifact: %w", writeErr)
		}
		if syncErr := temporary.Sync(); syncErr != nil {
			_ = temporary.Close()
			cleanup()
			return fmt.Errorf("sync generated publication artifact: %w", syncErr)
		}
		if closeErr := temporary.Close(); closeErr != nil {
			cleanup()
			return fmt.Errorf("close generated publication artifact: %w", closeErr)
		}
	}
	for _, artifact := range staged {
		if linkErr := writer.link(artifact.staged, artifact.final); linkErr != nil {
			cleanup()
			return fmt.Errorf("publish generated publication artifact %s: %w", artifact.final, linkErr)
		}
		linked = append(linked, artifact.final)
	}
	for _, artifact := range staged {
		if removeErr := os.Remove(artifact.staged); removeErr != nil {
			cleanup()
			return fmt.Errorf("remove staged publication artifact %s: %w", artifact.staged, removeErr)
		}
	}
	directory, err := os.Open(dir)
	if err != nil {
		cleanup()
		return fmt.Errorf("open publication artifact directory: %w", err)
	}
	if err := directory.Sync(); err != nil {
		_ = directory.Close()
		cleanup()
		return fmt.Errorf("sync publication artifact directory: %w", err)
	}
	if err := directory.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close publication artifact directory: %w", err)
	}
	return nil
}

// GenerateMigration emits deterministic migration artifacts for one
// declaration. Environment-specific exact rows remain in the approved Plan
// and are applied by the explicit publication executor, never by re-running a
// declaration filter inside generated SQL.
func GenerateMigration(predecessor Baseline, declaration Declaration) (GeneratedMigration, error) {
	declaration = NormalizeDeclaration(declaration)
	if err := ValidateDeclaration(declaration); err != nil {
		return GeneratedMigration{}, err
	}
	if predecessor.ThroughMigration != declaration.ExpectedStaticPredecessor {
		return GeneratedMigration{}, fmt.Errorf("%w: generator predecessor mismatch", ErrInvalidDeclaration)
	}
	final, err := Fold(predecessor, []Declaration{declaration})
	if err != nil {
		return GeneratedMigration{}, err
	}
	digest, err := DeclarationDigest(declaration)
	if err != nil {
		return GeneratedMigration{}, err
	}
	return GeneratedMigration{
		UpSQL:     generateUp(predecessor, final, declaration, digest),
		DownSQL:   generateDown(predecessor, declaration, digest),
		VerifySQL: generateVerify(final, declaration, digest),
	}, nil
}

func IsPublicationMigration(sql []byte) bool {
	return bytes.Contains(sql, []byte(publicationMigrationMarker))
}

// VerifyGeneratedMigrationFiles rejects any drift between a compiled
// declaration and its reviewable repository projection artifacts.
func VerifyGeneratedMigrationFiles(dir string, predecessor Baseline, declaration Declaration) error {
	generated, err := GenerateMigration(predecessor, declaration)
	if err != nil {
		return err
	}
	for suffix, want := range map[string][]byte{
		".up.sql": generated.UpSQL, ".down.sql": generated.DownSQL, ".verify.sql": generated.VerifySQL,
	} {
		path := filepath.Join(dir, declaration.PublicationID+suffix)
		got, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read generated publication artifact %s: %w", path, err)
		}
		if !bytes.Equal(got, want) {
			return fmt.Errorf("%w: generated publication artifact drift: %s", ErrInvalidDeclaration, path)
		}
	}
	return nil
}

func generateUp(predecessor, final Baseline, declaration Declaration, digest string) []byte {
	var b strings.Builder
	writeHeader(&b, declaration, digest, "up")
	b.WriteString("BEGIN;\n\nSELECT pg_advisory_xact_lock(86420517831702491);\n\n")
	b.WriteString(`LOCK TABLE model_capability_probe_contract_bindings,
    model_capability_current_probe_contracts,
    model_deployment_capabilities,
    model_deployment_operations,
    model_deployments,
    provider_connections,
    model_registry,
    schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

`)
	writeLedgerPredecessorCheck(&b, declaration.ExpectedSchemaPredecessor)
	writeStaticVerification(&b, predecessor, "publication predecessor")
	b.WriteString(`DROP TRIGGER model_capability_probe_contract_bindings_immutable
    ON model_capability_probe_contract_bindings;
DROP TRIGGER model_capability_current_probe_contracts_immutable
    ON model_capability_current_probe_contracts;

`)
	for _, binding := range declaration.BindingAppends {
		fmt.Fprintf(&b, `INSERT INTO model_capability_probe_contract_bindings (
    operation_id,capability_id,execution_profile_id,execution_profile_version,
    protocol_profile_id,protocol_profile_version,probe_contract_id,
    probe_contract_version,expected_provider,expected_upstream_model,
    public_path,introduced_migration
) VALUES (%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s);
`, sqlLiteral(binding.OperationID), sqlLiteral(binding.CapabilityID),
			sqlLiteral(binding.ExecutionProfileID), sqlLiteral(binding.ExecutionProfileVersion),
			sqlLiteral(binding.ProtocolProfileID), sqlLiteral(binding.ProtocolProfileVersion),
			sqlLiteral(binding.ProbeContractID), sqlLiteral(binding.ProbeContractVersion),
			sqlLiteral(binding.ExpectedProvider), sqlLiteral(binding.ExpectedUpstreamModel),
			sqlLiteral(binding.PublicPath), sqlLiteral(binding.IntroducedMigration))
	}
	if len(declaration.BindingAppends) != 0 {
		b.WriteByte('\n')
	}
	for _, transition := range declaration.SelectorTransitions {
		writeSelectorTransition(&b, transition)
	}
	writeStaticVerification(&b, final, "publication final")
	b.WriteString(`CREATE TRIGGER model_capability_probe_contract_bindings_immutable
BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON model_capability_probe_contract_bindings
FOR EACH STATEMENT EXECUTE FUNCTION reject_model_capability_probe_contract_binding_mutation();

CREATE TRIGGER model_capability_current_probe_contracts_immutable
BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON model_capability_current_probe_contracts
FOR EACH STATEMENT EXECUTE FUNCTION reject_model_capability_current_probe_contract_mutation();

COMMIT;
`)
	return []byte(b.String())
}

func generateVerify(final Baseline, declaration Declaration, digest string) []byte {
	var b strings.Builder
	writeHeader(&b, declaration, digest, "verify")
	writeStaticVerification(&b, final, "post-commit publication")
	return []byte(strings.TrimRight(b.String(), "\n") + "\n")
}

func generateDown(predecessor Baseline, declaration Declaration, digest string) []byte {
	var b strings.Builder
	writeHeader(&b, declaration, digest, "down")
	b.WriteString("BEGIN;\n\nSELECT pg_advisory_xact_lock(86420517831702491);\n\n")
	b.WriteString(`LOCK TABLE model_capability_probe_contract_bindings,
    model_capability_current_probe_contracts,
    model_deployment_capabilities,
    model_capability_probes,
    model_capability_routing_approvals,
    freeze_idempotency,
    billing_provider_attempts,
    schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

`)
	fmt.Fprintf(&b, `DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM schema_migrations WHERE version > %s) THEN
        RAISE EXCEPTION 'later migration prevents guarded capability publication downgrade';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM schema_migrations WHERE version=%s) THEN
        RAISE EXCEPTION 'capability publication ledger is missing';
    END IF;
END;
$$;

`, sqlLiteral(declaration.PublicationID), sqlLiteral(declaration.PublicationID))
	writeDownEvidenceChecks(&b, declaration)
	b.WriteString(`DROP TRIGGER model_capability_probe_contract_bindings_immutable
    ON model_capability_probe_contract_bindings;
DROP TRIGGER model_capability_current_probe_contracts_immutable
    ON model_capability_current_probe_contracts;

`)
	for index := len(declaration.SelectorTransitions) - 1; index >= 0; index-- {
		writeSelectorRollback(&b, declaration.SelectorTransitions[index])
	}
	for index := len(declaration.BindingAppends) - 1; index >= 0; index-- {
		binding := declaration.BindingAppends[index]
		fmt.Fprintf(&b, `DELETE FROM model_capability_probe_contract_bindings
 WHERE operation_id=%s AND capability_id=%s
   AND execution_profile_id=%s AND execution_profile_version=%s
   AND protocol_profile_id=%s AND protocol_profile_version=%s
   AND probe_contract_id=%s AND probe_contract_version=%s
   AND expected_provider=%s AND expected_upstream_model=%s
   AND public_path=%s AND introduced_migration=%s;
`, sqlLiteral(binding.OperationID), sqlLiteral(binding.CapabilityID),
			sqlLiteral(binding.ExecutionProfileID), sqlLiteral(binding.ExecutionProfileVersion),
			sqlLiteral(binding.ProtocolProfileID), sqlLiteral(binding.ProtocolProfileVersion),
			sqlLiteral(binding.ProbeContractID), sqlLiteral(binding.ProbeContractVersion),
			sqlLiteral(binding.ExpectedProvider), sqlLiteral(binding.ExpectedUpstreamModel),
			sqlLiteral(binding.PublicPath), sqlLiteral(binding.IntroducedMigration))
	}
	writeStaticVerification(&b, predecessor, "guarded downgrade final")
	fmt.Fprintf(&b, `DO $$
DECLARE affected BIGINT;
BEGIN
    DELETE FROM schema_migrations WHERE version=%s;
    GET DIAGNOSTICS affected = ROW_COUNT;
    IF affected <> 1 THEN
        RAISE EXCEPTION 'capability publication ledger delete mismatch';
    END IF;
END;
$$;

`, sqlLiteral(declaration.PublicationID))
	b.WriteString(`CREATE TRIGGER model_capability_probe_contract_bindings_immutable
BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON model_capability_probe_contract_bindings
FOR EACH STATEMENT EXECUTE FUNCTION reject_model_capability_probe_contract_binding_mutation();

CREATE TRIGGER model_capability_current_probe_contracts_immutable
BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON model_capability_current_probe_contracts
FOR EACH STATEMENT EXECUTE FUNCTION reject_model_capability_current_probe_contract_mutation();

COMMIT;
`)
	return []byte(b.String())
}

func writeDownEvidenceChecks(b *strings.Builder, declaration Declaration) {
	b.WriteString(`DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM freeze_idempotency WHERE execution_snapshot_hash IS NOT NULL)
       OR EXISTS (SELECT 1 FROM billing_provider_attempts WHERE execution_snapshot_hash IS NOT NULL) THEN
        RAISE EXCEPTION 'retained execution Freeze evidence prevents guarded publication downgrade';
    END IF;
END;
$$;

`)
	for _, binding := range declaration.BindingAppends {
		contractRef := "capability_contract:" + binding.ProbeContractID + "@" + binding.ProbeContractVersion
		fmt.Fprintf(b, `DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM model_capability_probes
                WHERE operation_id=%s AND capability_id=%s
                  AND execution_profile_id=%s AND execution_profile_version=%s
                  AND protocol_profile_id=%s AND protocol_profile_version=%s
                  AND probe_contract_id=%s AND probe_contract_version=%s)
       OR EXISTS (SELECT 1 FROM model_capability_routing_approvals
                WHERE operation_id=%s AND capability_id=%s
                  AND execution_profile_id=%s AND execution_profile_version=%s
                  AND protocol_profile_id=%s AND protocol_profile_version=%s
                  AND probe_contract_id=%s AND probe_contract_version=%s)
       OR EXISTS (SELECT 1 FROM billing_provider_attempts
                WHERE route_capability_evidence @> jsonb_build_array(jsonb_build_object(
                    'capability_id',%s::text,'evidence_ref',%s::text))) THEN
        RAISE EXCEPTION 'retained capability evidence prevents guarded publication downgrade';
    END IF;
END;
$$;

`, sqlLiteral(binding.OperationID), sqlLiteral(binding.CapabilityID),
			sqlLiteral(binding.ExecutionProfileID), sqlLiteral(binding.ExecutionProfileVersion),
			sqlLiteral(binding.ProtocolProfileID), sqlLiteral(binding.ProtocolProfileVersion),
			sqlLiteral(binding.ProbeContractID), sqlLiteral(binding.ProbeContractVersion),
			sqlLiteral(binding.OperationID), sqlLiteral(binding.CapabilityID),
			sqlLiteral(binding.ExecutionProfileID), sqlLiteral(binding.ExecutionProfileVersion),
			sqlLiteral(binding.ProtocolProfileID), sqlLiteral(binding.ProtocolProfileVersion),
			sqlLiteral(binding.ProbeContractID), sqlLiteral(binding.ProbeContractVersion),
			sqlLiteral(binding.CapabilityID), sqlLiteral(contractRef))
	}
	for _, effect := range declaration.DeclaredProjectionEffects {
		fmt.Fprintf(b, `DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM model_deployment_capabilities capability
        JOIN model_deployments deployment ON deployment.id=capability.deployment_id
        JOIN provider_connections connection ON connection.id=deployment.connection_id
        JOIN model_registry model ON model.id=deployment.public_model_id
        JOIN model_deployment_operations operation
          ON operation.deployment_id=deployment.id
         AND operation.operation_id=capability.operation_id
       WHERE capability.operation_id=%s AND capability.capability_id=%s
         AND capability.evidence_ref=%s
         AND model.execution_profile_id=%s AND model.execution_profile_version=%s
         AND operation.protocol_profile_id=%s AND operation.protocol_profile_version=%s
         AND connection.provider=%s AND deployment.upstream_model=%s
    ) THEN
        RAISE EXCEPTION 'exact projection recovery must precede guarded publication downgrade';
    END IF;
END;
$$;

`, sqlLiteral(effect.OperationID), sqlLiteral(effect.CapabilityID), sqlLiteral(effect.FinalContractRef),
			sqlLiteral(effect.ExecutionProfileID), sqlLiteral(effect.ExecutionProfileVersion),
			sqlLiteral(effect.ProtocolProfileID), sqlLiteral(effect.ProtocolProfileVersion),
			sqlLiteral(effect.ExpectedProvider), sqlLiteral(effect.ExpectedUpstreamModel))
	}
}

func writeHeader(b *strings.Builder, declaration Declaration, digest, kind string) {
	fmt.Fprintf(b, "%s\n-- publication: %s\n-- declaration-sha256: %s\n-- artifact: %s\n\n",
		publicationMigrationMarker, declaration.PublicationID, digest, kind)
}

func writeLedgerPredecessorCheck(b *strings.Builder, predecessor string) {
	fmt.Fprintf(b, `DO $$
BEGIN
    IF (SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1) IS DISTINCT FROM %s THEN
        RAISE EXCEPTION 'capability publication schema predecessor mismatch';
    END IF;
END;
$$;

`, sqlLiteral(predecessor))
}

func writeStaticVerification(b *strings.Builder, projection Baseline, label string) {
	fmt.Fprintf(b, `DO $$
BEGIN
    IF (SELECT COUNT(*) FROM model_capability_probe_contract_bindings) <> %d THEN
        RAISE EXCEPTION '%s binding count mismatch';
    END IF;
    IF (SELECT COUNT(*) FROM model_capability_current_probe_contracts) <> %d THEN
        RAISE EXCEPTION '%s selector count mismatch';
    END IF;
`, len(projection.Bindings), label, len(projection.Selectors), label)
	for _, binding := range projection.Bindings {
		fmt.Fprintf(b, `    IF NOT EXISTS (SELECT 1 FROM model_capability_probe_contract_bindings WHERE operation_id=%s AND capability_id=%s AND execution_profile_id=%s AND execution_profile_version=%s AND protocol_profile_id=%s AND protocol_profile_version=%s AND probe_contract_id=%s AND probe_contract_version=%s AND expected_provider=%s AND expected_upstream_model=%s AND public_path=%s AND introduced_migration=%s) THEN
        RAISE EXCEPTION '%s binding content mismatch';
    END IF;
`, sqlLiteral(binding.OperationID), sqlLiteral(binding.CapabilityID),
			sqlLiteral(binding.ExecutionProfileID), sqlLiteral(binding.ExecutionProfileVersion),
			sqlLiteral(binding.ProtocolProfileID), sqlLiteral(binding.ProtocolProfileVersion),
			sqlLiteral(binding.ProbeContractID), sqlLiteral(binding.ProbeContractVersion),
			sqlLiteral(binding.ExpectedProvider), sqlLiteral(binding.ExpectedUpstreamModel),
			sqlLiteral(binding.PublicPath), sqlLiteral(binding.IntroducedMigration), label)
	}
	for _, selector := range projection.Selectors {
		fmt.Fprintf(b, `    IF NOT EXISTS (SELECT 1 FROM model_capability_current_probe_contracts WHERE operation_id=%s AND capability_id=%s AND execution_profile_id=%s AND execution_profile_version=%s AND protocol_profile_id=%s AND protocol_profile_version=%s AND probe_contract_id=%s AND probe_contract_version=%s) THEN
        RAISE EXCEPTION '%s selector content mismatch';
    END IF;
`, sqlLiteral(selector.OperationID), sqlLiteral(selector.CapabilityID),
			sqlLiteral(selector.ExecutionProfileID), sqlLiteral(selector.ExecutionProfileVersion),
			sqlLiteral(selector.ProtocolProfileID), sqlLiteral(selector.ProtocolProfileVersion),
			sqlLiteral(selector.ProbeContractID), sqlLiteral(selector.ProbeContractVersion), label)
	}
	b.WriteString("END;\n$$;\n\n")
}

func writeSelectorTransition(b *strings.Builder, transition SelectorTransition) {
	if transition.ExpectedPredecessor == nil {
		fmt.Fprintf(b, `INSERT INTO model_capability_current_probe_contracts (
    operation_id,capability_id,execution_profile_id,execution_profile_version,
    protocol_profile_id,protocol_profile_version,probe_contract_id,probe_contract_version
) VALUES (%s,%s,%s,%s,%s,%s,%s,%s);
`, sqlLiteral(transition.OperationID), sqlLiteral(transition.CapabilityID),
			sqlLiteral(transition.ExecutionProfileID), sqlLiteral(transition.ExecutionProfileVersion),
			sqlLiteral(transition.ProtocolProfileID), sqlLiteral(transition.ProtocolProfileVersion),
			sqlLiteral(transition.Final.ProbeContractID), sqlLiteral(transition.Final.ProbeContractVersion))
		return
	}
	fmt.Fprintf(b, `DO $$
DECLARE affected BIGINT;
BEGIN
    UPDATE model_capability_current_probe_contracts
       SET probe_contract_id=%s,probe_contract_version=%s,updated_at=NOW()
     WHERE operation_id=%s AND capability_id=%s
       AND execution_profile_id=%s AND execution_profile_version=%s
       AND protocol_profile_id=%s AND protocol_profile_version=%s
       AND probe_contract_id=%s AND probe_contract_version=%s;
    GET DIAGNOSTICS affected = ROW_COUNT;
    IF affected <> 1 THEN
        RAISE EXCEPTION 'capability publication selector compare-and-swap mismatch';
    END IF;
END;
$$;
`, sqlLiteral(transition.Final.ProbeContractID), sqlLiteral(transition.Final.ProbeContractVersion),
		sqlLiteral(transition.OperationID), sqlLiteral(transition.CapabilityID),
		sqlLiteral(transition.ExecutionProfileID), sqlLiteral(transition.ExecutionProfileVersion),
		sqlLiteral(transition.ProtocolProfileID), sqlLiteral(transition.ProtocolProfileVersion),
		sqlLiteral(transition.ExpectedPredecessor.ProbeContractID), sqlLiteral(transition.ExpectedPredecessor.ProbeContractVersion))
}

func writeSelectorRollback(b *strings.Builder, transition SelectorTransition) {
	if transition.ExpectedPredecessor == nil {
		fmt.Fprintf(b, `DELETE FROM model_capability_current_probe_contracts
 WHERE operation_id=%s AND capability_id=%s
   AND execution_profile_id=%s AND execution_profile_version=%s
   AND protocol_profile_id=%s AND protocol_profile_version=%s
   AND probe_contract_id=%s AND probe_contract_version=%s;
`, sqlLiteral(transition.OperationID), sqlLiteral(transition.CapabilityID),
			sqlLiteral(transition.ExecutionProfileID), sqlLiteral(transition.ExecutionProfileVersion),
			sqlLiteral(transition.ProtocolProfileID), sqlLiteral(transition.ProtocolProfileVersion),
			sqlLiteral(transition.Final.ProbeContractID), sqlLiteral(transition.Final.ProbeContractVersion))
		return
	}
	fmt.Fprintf(b, `UPDATE model_capability_current_probe_contracts
   SET probe_contract_id=%s,probe_contract_version=%s,updated_at=NOW()
 WHERE operation_id=%s AND capability_id=%s
   AND execution_profile_id=%s AND execution_profile_version=%s
   AND protocol_profile_id=%s AND protocol_profile_version=%s
   AND probe_contract_id=%s AND probe_contract_version=%s;
`, sqlLiteral(transition.ExpectedPredecessor.ProbeContractID), sqlLiteral(transition.ExpectedPredecessor.ProbeContractVersion),
		sqlLiteral(transition.OperationID), sqlLiteral(transition.CapabilityID),
		sqlLiteral(transition.ExecutionProfileID), sqlLiteral(transition.ExecutionProfileVersion),
		sqlLiteral(transition.ProtocolProfileID), sqlLiteral(transition.ProtocolProfileVersion),
		sqlLiteral(transition.Final.ProbeContractID), sqlLiteral(transition.Final.ProbeContractVersion))
}

func sqlLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
