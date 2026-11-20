BEGIN;
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
       )
       OR EXISTS (
           SELECT 1 FROM runtime_ingress_changes c
           WHERE NOT EXISTS (SELECT 1 FROM runtime_ingress_changes n WHERE n.predecessor_id=c.change_id)
             AND c.membership_after <> '[]'::jsonb
       ) THEN
        RAISE EXCEPTION 'M131 requires stopped writers and completed empty ingress';
    END IF;
END $$;

DO $$
BEGIN
    IF (SELECT max(version) FROM schema_migrations) IS DISTINCT FROM '000130_upgrade_admission_recovery' THEN
        RAISE EXCEPTION 'customer pricing requires exact M130 predecessor';
    END IF;
END $$;

CREATE TABLE customer_price_table_versions (
    org_id BIGINT NOT NULL REFERENCES orgs(id),
    table_ref VARCHAR(160) NOT NULL CHECK (table_ref <> ''),
    version BIGINT NOT NULL CHECK (version > 0),
    effective_from TIMESTAMPTZ(6) NOT NULL,
    effective_to TIMESTAMPTZ(6),
    entries JSONB NOT NULL CHECK (jsonb_typeof(entries) = 'array' AND jsonb_array_length(entries) > 0),
    created_by VARCHAR(160) NOT NULL CHECK (created_by <> ''),
    approved_by VARCHAR(160),
    approved_at TIMESTAMPTZ(6),
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    PRIMARY KEY (org_id, table_ref, version),
    CHECK (effective_to IS NULL OR effective_to > effective_from),
    CHECK ((approved_by IS NULL AND approved_at IS NULL) OR
           (approved_by IS NOT NULL AND approved_by <> '' AND approved_by <> created_by AND approved_at IS NOT NULL))
);

CREATE TABLE customer_price_binding_versions (
    org_id BIGINT NOT NULL,
    scope_project_id BIGINT NOT NULL CHECK (scope_project_id >= 0),
    revision BIGINT NOT NULL CHECK (revision > 0),
    table_ref VARCHAR(160) NOT NULL,
    table_version BIGINT NOT NULL,
    bound_by VARCHAR(160) NOT NULL CHECK (bound_by <> ''),
    bound_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    PRIMARY KEY (org_id, scope_project_id, revision),
    FOREIGN KEY (org_id, table_ref, table_version) REFERENCES customer_price_table_versions
);

CREATE TABLE customer_charge_quote_receipts (
    org_id BIGINT NOT NULL,
    request_id VARCHAR(64) NOT NULL,
    pricing_version VARCHAR(80) NOT NULL CHECK (pricing_version ~ '^ccq1_[a-f0-9]{64}$'),
    maximum_authorized_micros BIGINT NOT NULL CHECK (maximum_authorized_micros >= 0),
    receipt JSONB NOT NULL CHECK (jsonb_typeof(receipt) = 'object'),
    PRIMARY KEY (org_id, request_id),
    FOREIGN KEY (org_id, request_id) REFERENCES freeze_idempotency (org_id, request_id)
);

CREATE FUNCTION protect_customer_pricing_evidence() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME='customer_price_table_versions' AND TG_OP='UPDATE'
       AND OLD.approved_at IS NULL AND NEW.approved_at IS NOT NULL
       AND (to_jsonb(OLD) - 'approved_by' - 'approved_at') =
           (to_jsonb(NEW) - 'approved_by' - 'approved_at') THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'customer pricing evidence is append-only';
END;
$$;

CREATE TRIGGER customer_price_table_evidence_immutable
    BEFORE UPDATE OR DELETE ON customer_price_table_versions
    FOR EACH ROW EXECUTE FUNCTION protect_customer_pricing_evidence();
CREATE TRIGGER customer_price_binding_evidence_immutable
    BEFORE UPDATE OR DELETE ON customer_price_binding_versions
    FOR EACH ROW EXECUTE FUNCTION protect_customer_pricing_evidence();
CREATE TRIGGER customer_charge_quote_evidence_immutable
    BEFORE UPDATE OR DELETE ON customer_charge_quote_receipts
    FOR EACH ROW EXECUTE FUNCTION protect_customer_pricing_evidence();

ALTER TABLE freeze_idempotency DROP CONSTRAINT freeze_idempotency_pending_reason_valid;
ALTER TABLE freeze_idempotency ADD CONSTRAINT freeze_idempotency_pending_reason_valid CHECK (
    settlement_pending_reason IN (
        '', 'billable_output', 'insufficient_topup', 'ambiguous_attempt',
        'provider_response_unusable', 'provider_attempt_unresolved',
        'delivery_prepared', 'delivery_completed', 'delivery_failed',
        'guardrail_withheld', 'response_too_large', 'customer_authorization_exceeded',
        'response_cache_delivery_prepared', 'response_cache_delivery_completed',
        'response_cache_delivery_failed'
    )
);

CREATE OR REPLACE FUNCTION public.verify_source_budget_coverage_commit() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE c execution_source_budget_coverage%ROWTYPE; m JSONB; ledger TEXT[]; certified_ledger TEXT[];
BEGIN
    SELECT * INTO c FROM execution_source_budget_coverage WHERE coverage_epoch=NEW.coverage_epoch;
    SELECT array_agg(version::text ORDER BY version) INTO ledger FROM schema_migrations;
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

COMMIT;
