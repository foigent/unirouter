BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE runtime_generations, runtime_ingress_changes IN SHARE ROW EXCLUSIVE MODE NOWAIT;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM runtime_generations WHERE state <> 'stopped' OR ingress_state <> 'absent')
       OR EXISTS (SELECT 1 FROM runtime_ingress_changes WHERE status NOT IN ('applied', 'cancelled'))
       OR EXISTS (
           SELECT 1 FROM runtime_ingress_changes c
           WHERE c.status='cancelled' AND NOT EXISTS (
               SELECT 1 FROM runtime_ingress_changes n
               WHERE n.predecessor_id=c.change_id AND n.change_id=c.change_id || '-withdrawn'
                 AND n.status='applied' AND n.membership_after='[]'::jsonb
           )
       ) OR EXISTS (
           SELECT 1 FROM runtime_ingress_changes c
           WHERE NOT EXISTS (SELECT 1 FROM runtime_ingress_changes n WHERE n.predecessor_id=c.change_id)
             AND c.membership_after <> '[]'::jsonb
       ) THEN
        RAISE EXCEPTION 'M134 requires stopped writers and completed empty ingress';
    END IF;
    IF (SELECT max(version) FROM schema_migrations) IS DISTINCT FROM '000134_native_inline_jpeg_public_bindings' THEN
        RAISE EXCEPTION 'M134 rollback requires exact M134 head';
    END IF;
END $$;

LOCK TABLE model_profile_definitions, model_capability_probe_contract_bindings,
    model_operation_execution_support_bindings,
    model_operation_public_execution_contract_extensions,
    model_operation_public_execution_bindings,
    runtime_route_revision_items, schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM model_profile_definitions
        WHERE convert_from(canonical_bytes,'UTF8')::jsonb #>> '{profile,protocol_profile}' IN ('unirouter.openai.responses.inline-jpeg.v1', 'unirouter.anthropic.messages.inline-jpeg.v1'))
       OR EXISTS (SELECT 1 FROM model_operation_public_execution_bindings WHERE protocol_profile_id IN ('unirouter.openai.responses.inline-jpeg.v1', 'unirouter.anthropic.messages.inline-jpeg.v1'))
       OR EXISTS (SELECT 1 FROM runtime_route_revision_items WHERE protocol_profile_id IN ('unirouter.openai.responses.inline-jpeg.v1', 'unirouter.anthropic.messages.inline-jpeg.v1')) THEN
        RAISE EXCEPTION 'M134 rollback would remove retained native vision execution evidence';
    END IF;
END $$;

DROP TRIGGER model_operation_public_execution_contract_extensions_immutable ON model_operation_public_execution_contract_extensions;

DELETE FROM model_operation_public_execution_contract_extensions
WHERE introduced_migration='000134_native_inline_jpeg_public_bindings' AND protocol_profile_id IN ('unirouter.openai.responses.inline-jpeg.v1', 'unirouter.anthropic.messages.inline-jpeg.v1');

ALTER TABLE model_operation_public_execution_contract_extensions
    DROP CONSTRAINT model_operation_public_execution_contract_extensions_identity_check,
    ADD CONSTRAINT model_operation_public_execution_contract_extensions_identity_check CHECK (
        charge_contract_version='unirouter.provider-final-token-charge.v1'
        AND idempotency_contract_version='unirouter.public-at-most-once.v1'
        AND (
            (
                introduced_migration='000077_public_model_execution_authority'
                AND delivery_contract_version='unirouter.public-nonstream-delivery.v1'
                AND executor_contract_version='unirouter.endpoint-profile-nonstream-executor.v1'
                AND (
                    (operation_id='openai.responses.v1'
                     AND protocol_profile_id='unirouter.openai.responses.text.v1'
                     AND protocol_profile_version='1.0.0')
                    OR
                    (operation_id='anthropic.messages.v1'
                     AND protocol_profile_id='unirouter.anthropic.messages.text.v1'
                     AND protocol_profile_version='1.0.0')
                )
            )
            OR
            (
                introduced_migration='000086_exact_public_execution_bindings'
                AND protocol_profile_version='1.0.0'
                AND (
                    (
                        operation_id='openai.responses.v1'
                        AND protocol_profile_id='unirouter.openai.responses.text.v2'
                        AND executor_contract_version='unirouter.endpoint-profile-responses-dual-executor.v1'
                        AND delivery_contract_version='unirouter.public-responses-delivery.v2'
                    )
                    OR
                    (
                        operation_id='anthropic.messages.v1'
                        AND protocol_profile_id='unirouter.anthropic.messages.text.v2'
                        AND executor_contract_version='unirouter.endpoint-profile-messages-dual-executor.v1'
                        AND delivery_contract_version='unirouter.public-anthropic-messages-delivery.v2'
                    )
                )
            )
            OR
            (
                introduced_migration='000087_anthropic_messages_tools_public_binding'
                AND operation_id='anthropic.messages.v1'
                AND protocol_profile_id='unirouter.anthropic.messages.text-tools.v3'
                AND protocol_profile_version='1.0.0'
                AND executor_contract_version='unirouter.endpoint-profile-messages-tools-executor.v1'
                AND delivery_contract_version='unirouter.public-anthropic-messages-tools-delivery.v1'
            )
            OR
            (
                introduced_migration='000132_openai_responses_client_tools'
                AND operation_id='openai.responses.v1'
                AND protocol_profile_id='unirouter.openai.responses.text-tools.v3'
                AND protocol_profile_version='1.0.0'
                AND executor_contract_version='unirouter.endpoint-profile-responses-tools-executor.v1'
                AND delivery_contract_version='unirouter.public-responses-tools-delivery.v1'
            )
        )
    );

CREATE TRIGGER model_operation_public_execution_contract_extensions_immutable
BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE
ON model_operation_public_execution_contract_extensions
FOR EACH STATEMENT EXECUTE FUNCTION reject_model_operation_public_execution_contract_mutation();

DROP TRIGGER model_operation_execution_support_bindings_immutable ON model_operation_execution_support_bindings;

DELETE FROM model_operation_execution_support_bindings
WHERE introduced_migration='000134_native_inline_jpeg_public_bindings' AND protocol_profile_id IN ('unirouter.openai.responses.inline-jpeg.v1', 'unirouter.anthropic.messages.inline-jpeg.v1');

CREATE TRIGGER model_operation_execution_support_bindings_immutable
BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE
ON model_operation_execution_support_bindings
FOR EACH STATEMENT EXECUTE FUNCTION reject_model_operation_execution_support_mutation();

CREATE OR REPLACE FUNCTION public.verify_source_budget_coverage_commit() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE c execution_source_budget_coverage%ROWTYPE; m JSONB; ledger TEXT[]; certified_ledger TEXT[];
BEGIN
    SELECT * INTO c FROM execution_source_budget_coverage WHERE coverage_epoch=NEW.coverage_epoch;
    SELECT array_agg(version::text ORDER BY version) INTO ledger FROM schema_migrations;
    IF ledger[cardinality(ledger)]='000133_media_charge_source_facts' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000132_openai_responses_client_tools' THEN
        RAISE EXCEPTION 'budget coverage requires exact M133 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
    END IF;
    IF ledger[cardinality(ledger)]='000132_openai_responses_client_tools' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000131_customer_standard_pricing' THEN
        RAISE EXCEPTION 'budget coverage requires exact M132 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
    END IF;
    IF ledger[cardinality(ledger)]='000131_customer_standard_pricing' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000130_upgrade_admission_recovery' THEN
        RAISE EXCEPTION 'budget coverage requires exact M131 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
    END IF;
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

DELETE FROM schema_migrations WHERE version='000134_native_inline_jpeg_public_bindings';

COMMIT;
