BEGIN;
LOCK TABLE runtime_generations, runtime_ingress_changes IN SHARE ROW EXCLUSIVE MODE NOWAIT;
DO $$
BEGIN
    IF (SELECT max(version) FROM schema_migrations) IS DISTINCT FROM '000129_upstream_observation_restore_namespace'
       OR EXISTS (SELECT 1 FROM runtime_generations WHERE state <> 'stopped' OR ingress_state <> 'absent')
       OR EXISTS (SELECT 1 FROM runtime_ingress_changes WHERE status <> 'applied')
       OR EXISTS (
           SELECT 1 FROM runtime_ingress_changes c
           WHERE NOT EXISTS (SELECT 1 FROM runtime_ingress_changes n WHERE n.predecessor_id=c.change_id)
           AND c.membership_after <> '[]'::jsonb
       ) THEN
        RAISE EXCEPTION 'M130 requires exact M129 with stopped writers and completed empty ingress';
    END IF;
END $$;

CREATE OR REPLACE FUNCTION guard_runtime_ingress_receipt() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE original runtime_ingress_changes%ROWTYPE;
BEGIN
    IF TG_OP='INSERT' THEN
        IF NEW.status='cancelled' THEN
            RAISE EXCEPTION 'cannot insert a cancelled ingress intent' USING ERRCODE='55000';
        END IF;
        SELECT * INTO original FROM runtime_ingress_changes WHERE change_id=NEW.predecessor_id;
        IF original.status='cancelled' THEN
            IF NEW.change_id <> original.change_id || '-withdrawn'
               OR NEW.status <> 'applied' OR NEW.completed_at IS NULL
               OR NEW.membership_before <> original.membership_after OR NEW.membership_after <> '[]'::jsonb
               OR NEW.expected_config_digest <> original.desired_config_digest
               OR NEW.desired_config_digest <> original.desired_config_digest
               OR NEW.desired_config_bytes <> original.desired_config_bytes
               OR NEW.observation->>'cancelled_admission' IS DISTINCT FROM original.change_id
               OR COALESCE(NEW.observation->>'host_proof_sha256','') !~ '^[a-f0-9]{64}$' THEN
                RAISE EXCEPTION 'cancelled admission requires its exact applied empty successor' USING ERRCODE='55000';
            END IF;
        ELSIF NEW.observation ? 'cancelled_admission' THEN
            RAISE EXCEPTION 'withdrawal receipt does not belong to a cancelled admission' USING ERRCODE='55000';
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP<>'UPDATE' THEN
        RAISE EXCEPTION 'runtime ingress history cannot be deleted' USING ERRCODE='55000';
    END IF;
    IF OLD.status IN ('applied','cancelled') OR NEW.change_id IS DISTINCT FROM OLD.change_id
       OR NEW.predecessor_id IS DISTINCT FROM OLD.predecessor_id
       OR NEW.expected_config_digest IS DISTINCT FROM OLD.expected_config_digest
       OR NEW.desired_config_digest IS DISTINCT FROM OLD.desired_config_digest
       OR NEW.desired_config_bytes IS DISTINCT FROM OLD.desired_config_bytes
       OR NEW.membership_before IS DISTINCT FROM OLD.membership_before
       OR NEW.membership_after IS DISTINCT FROM OLD.membership_after
       OR NEW.created_at IS DISTINCT FROM OLD.created_at
       OR NEW.status NOT IN ('ambiguous','applied','cancelled')
       OR (NEW.status IN ('applied','cancelled')) IS DISTINCT FROM (NEW.completed_at IS NOT NULL) THEN
        RAISE EXCEPTION 'runtime ingress intent is immutable and completion is monotonic' USING ERRCODE='55000';
    END IF;
    IF NEW.status='cancelled' AND (
       OLD.change_id !~ '^ucu_[a-f0-9]{32}-admit(-[a-f0-9]{32})?$'
       OR NEW.observation IS DISTINCT FROM OLD.observation
       OR OLD.membership_before <> '[]'::jsonb OR jsonb_array_length(OLD.membership_after) NOT BETWEEN 1 AND 2
       OR NOT EXISTS (SELECT 1 FROM runtime_ingress_changes p WHERE p.change_id=OLD.predecessor_id
                      AND p.status='applied' AND p.membership_after='[]'::jsonb
                      AND p.desired_config_digest=OLD.expected_config_digest)
       OR EXISTS (SELECT 1 FROM runtime_ingress_changes n WHERE n.predecessor_id=OLD.change_id)) THEN
        RAISE EXCEPTION 'only the exact unresolved upgrade admission may be cancelled' USING ERRCODE='55000';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER runtime_ingress_successor_guard BEFORE INSERT ON runtime_ingress_changes
    FOR EACH ROW EXECUTE FUNCTION guard_runtime_ingress_receipt();

CREATE FUNCTION require_runtime_admission_withdrawal() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    -- The insert guard verifies the unique successor's full shape. This deferred
    -- check prevents committing the cancellation without that same receipt.
    IF NOT EXISTS (SELECT 1 FROM runtime_ingress_changes WHERE predecessor_id=NEW.change_id
                   AND change_id=NEW.change_id || '-withdrawn' AND status='applied')
       OR EXISTS (SELECT 1 FROM runtime_generations WHERE ingress_state <> 'absent') THEN
        RAISE EXCEPTION 'cancelled admission and physical withdrawal must commit together' USING ERRCODE='55000';
    END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER runtime_admission_requires_withdrawal AFTER UPDATE ON runtime_ingress_changes
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.status='cancelled')
    EXECUTE FUNCTION require_runtime_admission_withdrawal();

-- M130 changes ingress only. Keep the historical financial certificate on its original ledger.
CREATE OR REPLACE FUNCTION public.verify_source_budget_coverage_commit() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE c execution_source_budget_coverage%ROWTYPE; m JSONB; ledger TEXT[]; certified_ledger TEXT[];
BEGIN
    SELECT * INTO c FROM execution_source_budget_coverage WHERE coverage_epoch=NEW.coverage_epoch;
    SELECT array_agg(version::text ORDER BY version) INTO ledger FROM schema_migrations;
    IF ledger[cardinality(ledger)]='000130_upgrade_admission_recovery' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000129_upstream_observation_restore_namespace' THEN
        RAISE EXCEPTION 'budget coverage requires exact M130 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
    END IF;
    IF ledger[cardinality(ledger)]='000129_upstream_observation_restore_namespace' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000128_upstream_release_observation' THEN
        RAISE EXCEPTION 'budget coverage requires exact M129 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
    END IF;
    IF ledger[cardinality(ledger)]='000128_upstream_release_observation' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000127_xinsight_communication_policy' THEN
        RAISE EXCEPTION 'budget coverage requires exact M128 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
    END IF;
    certified_ledger := ledger;
    IF ledger[cardinality(ledger)]='000127_xinsight_communication_policy' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000126_commercial_license_retirement' THEN
        RAISE EXCEPTION 'budget coverage requires exact M127 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
      certified_ledger := ledger;
    END IF;
    IF ledger[cardinality(ledger)]='000126_commercial_license_retirement' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000125_xinsight_control_credential_lifecycle' THEN
        RAISE EXCEPTION 'budget coverage requires exact M126 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
      certified_ledger := ledger;
    END IF;
    IF ledger[cardinality(ledger)]='000125_xinsight_control_credential_lifecycle' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000124_xinsight_receiver_adoption' THEN
        RAISE EXCEPTION 'budget coverage requires exact M125 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
      certified_ledger := ledger;
    END IF;
    IF ledger[cardinality(ledger)]='000124_xinsight_receiver_adoption' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000123_xinsight_policy_delegation' THEN
        RAISE EXCEPTION 'budget coverage requires exact M124 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
      certified_ledger := ledger;
    END IF;
    IF ledger[cardinality(ledger)]='000123_xinsight_policy_delegation' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000122_scoped_content_policy'
         OR ledger[cardinality(ledger)-2] IS DISTINCT FROM '000121_gateway_pre_dispatch_terminal'
         OR ledger[cardinality(ledger)-3] IS DISTINCT FROM '000120_unpublished_content_reclamation'
         OR ledger[cardinality(ledger)-4] IS DISTINCT FROM '000119_execution_content_capture' THEN
        RAISE EXCEPTION 'budget coverage requires exact M123 predecessor'; END IF;
      certified_ledger := ledger[:cardinality(ledger)-5];
    ELSIF ledger[cardinality(ledger)]='000122_scoped_content_policy' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000121_gateway_pre_dispatch_terminal'
         OR ledger[cardinality(ledger)-2] IS DISTINCT FROM '000120_unpublished_content_reclamation'
         OR ledger[cardinality(ledger)-3] IS DISTINCT FROM '000119_execution_content_capture' THEN
        RAISE EXCEPTION 'budget coverage requires exact M122 predecessor'; END IF;
      certified_ledger := ledger[:cardinality(ledger)-4];
    ELSIF ledger[cardinality(ledger)]='000121_gateway_pre_dispatch_terminal' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000120_unpublished_content_reclamation'
         OR ledger[cardinality(ledger)-2] IS DISTINCT FROM '000119_execution_content_capture' THEN
        RAISE EXCEPTION 'budget coverage requires exact M121 predecessor'; END IF;
      certified_ledger := ledger[:cardinality(ledger)-3];
    ELSIF ledger[cardinality(ledger)]='000120_unpublished_content_reclamation' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000119_execution_content_capture' THEN
        RAISE EXCEPTION 'budget coverage requires exact M120 predecessor'; END IF;
      certified_ledger := ledger[:cardinality(ledger)-2];
    ELSIF ledger[cardinality(ledger)]='000119_execution_content_capture' THEN
      certified_ledger := ledger[:cardinality(ledger)-1];
    END IF;
    IF c.state IS DISTINCT FROM 'certified' OR c.support_manifest_sha256 IS DISTINCT FROM encode(sha256(c.support_manifest_bytes),'hex')
       OR certified_ledger[cardinality(certified_ledger)] IS DISTINCT FROM '000118_source_budget_snapshots'
       OR c.schema_ledger_sha256 IS DISTINCT FROM encode(sha256(convert_to(array_to_string(certified_ledger,E'\n'),'UTF8')),'hex') THEN
      RAISE EXCEPTION 'budget coverage requires verified support proof in the migration transaction'; END IF;
    m := convert_from(c.support_manifest_bytes,'UTF8')::jsonb;
    IF COALESCE((m->'schema_heads' ? '000118_source_budget_snapshots'),false)=false OR COALESCE((m->'implementations' @>
       '[{"kind":"source_budgets","id":"unirouter.budget-snapshot.v1"},{"kind":"financial_journal","id":"unirouter.gateway-financial-journal.v2"}]'::jsonb),false)=false THEN
      RAISE EXCEPTION 'budget coverage support proof lacks the writer or snapshot contract'; END IF;
    RETURN NEW;
END $$;
COMMIT;
