-- unirouter:explicit-compatible-expand-v1
BEGIN;
LOCK TABLE runtime_change_releases IN ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE runtime_change_slots IN SHARE ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE runtime_generations IN SHARE ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE xinsight_connector_installations,xinsight_connector_control_bindings IN SHARE ROW EXCLUSIVE MODE NOWAIT;
DO $$ BEGIN
 IF (SELECT MAX(version) FROM schema_migrations) IS DISTINCT FROM '000124_xinsight_receiver_adoption' THEN
  RAISE EXCEPTION 'M125 requires exact M124 predecessor'; END IF;
 IF EXISTS(SELECT 1 FROM runtime_generations g LEFT JOIN runtime_support_manifests m USING(artifact_digest)
  WHERE g.state <> 'stopped' AND (m.artifact_digest IS NULL OR NOT COALESCE(
   convert_from(m.canonical_bytes,'UTF8')::jsonb->'schema_heads' ? '000125_xinsight_control_credential_lifecycle',false)
   OR NOT COALESCE(convert_from(m.canonical_bytes,'UTF8')::jsonb->'implementations' @>
    '[{"kind":"xinsight_connector","id":"unirouter.xinsight.connector-control-credential.v1"}]'::jsonb,false))) THEN
  RAISE EXCEPTION 'serving or recovery generation lacks M125 control credential lifecycle support'; END IF;
END $$;
CREATE FUNCTION require_xinsight_control_credential_runtime_support() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.state <> 'stopped' AND NOT EXISTS(SELECT 1 FROM runtime_support_manifests m
  WHERE m.artifact_digest=NEW.artifact_digest
   AND COALESCE(convert_from(m.canonical_bytes,'UTF8')::jsonb->'schema_heads' ? '000125_xinsight_control_credential_lifecycle',false)
   AND COALESCE(convert_from(m.canonical_bytes,'UTF8')::jsonb->'implementations' @>
    '[{"kind":"xinsight_connector","id":"unirouter.xinsight.connector-control-credential.v1"}]'::jsonb,false)) THEN
  RAISE EXCEPTION 'runtime generation lacks M125 control credential lifecycle support'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER runtime_generation_xinsight_control_credential_support BEFORE INSERT OR UPDATE ON runtime_generations
 FOR EACH ROW EXECUTE FUNCTION require_xinsight_control_credential_runtime_support();

-- This is a credential-generation journal, separate from business control
-- operations and their control_fence. Exact secret-bearing request bytes are
-- encrypted in the application before insertion; the database sees digests.
CREATE TABLE xinsight_connector_credential_operations (
 operation_id VARCHAR(26) PRIMARY KEY CHECK(operation_id ~ '^xcr_[A-Za-z0-9_-]{22}$'),
 installation_id VARCHAR(128) NOT NULL,
 org_id BIGINT NOT NULL,
 project_id BIGINT NOT NULL,
 kind VARCHAR(8) NOT NULL CHECK(kind IN ('rotate','recover')),
 previous_token_sha256 VARCHAR(71) NOT NULL CHECK(previous_token_sha256 ~ '^sha256:[a-f0-9]{64}$'),
 replacement_token_enc TEXT NOT NULL CHECK(octet_length(replacement_token_enc) BETWEEN 1 AND 4096),
 replacement_token_sha256 VARCHAR(71) NOT NULL CHECK(replacement_token_sha256 ~ '^sha256:[a-f0-9]{64}$'),
 request_enc TEXT NOT NULL CHECK(octet_length(request_enc) BETWEEN 1 AND 16384),
 request_sha256 VARCHAR(71) NOT NULL CHECK(request_sha256 ~ '^sha256:[a-f0-9]{64}$'),
 state VARCHAR(12) NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','committed','promoted','superseded')),
 receipt_canonical BYTEA,
 receipt_sha256 VARCHAR(71),
 replacement_expires_at TIMESTAMPTZ,
 control_identity_id VARCHAR(26),
 superseded_operation_id VARCHAR(26),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 FOREIGN KEY(installation_id,org_id,project_id)
  REFERENCES xinsight_connector_installations(installation_id,org_id,project_id) ON DELETE RESTRICT,
 FOREIGN KEY(superseded_operation_id) REFERENCES xinsight_connector_credential_operations(operation_id) ON DELETE RESTRICT,
 CHECK ((receipt_canonical IS NULL AND receipt_sha256 IS NULL AND replacement_expires_at IS NULL AND control_identity_id IS NULL)
    OR (receipt_canonical IS NOT NULL AND octet_length(receipt_canonical) BETWEEN 1 AND 8192
      AND receipt_sha256 ~ '^sha256:[a-f0-9]{64}$' AND replacement_expires_at IS NOT NULL
      AND control_identity_id ~ '^xsi_[A-Za-z0-9_-]{22}$')),
 CHECK (state NOT IN ('committed','promoted') OR receipt_canonical IS NOT NULL),
 CHECK (superseded_operation_id IS NULL OR (kind='recover' AND superseded_operation_id<>operation_id))
);
CREATE UNIQUE INDEX xinsight_connector_credential_one_unfinished
 ON xinsight_connector_credential_operations(installation_id)
 WHERE state IN ('pending','committed');
CREATE INDEX xinsight_connector_credential_history
 ON xinsight_connector_credential_operations(installation_id,created_at DESC,operation_id DESC);
CREATE OR REPLACE FUNCTION verify_source_budget_coverage_commit() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE c execution_source_budget_coverage%ROWTYPE; m JSONB; ledger TEXT[]; certified_ledger TEXT[];
BEGIN
    SELECT * INTO c FROM execution_source_budget_coverage WHERE coverage_epoch=NEW.coverage_epoch;
    SELECT array_agg(version::text ORDER BY version) INTO ledger FROM schema_migrations;
    certified_ledger := ledger;
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
