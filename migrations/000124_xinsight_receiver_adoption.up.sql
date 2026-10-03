-- unirouter:explicit-compatible-expand-v1
BEGIN;
LOCK TABLE runtime_change_releases IN ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE runtime_change_slots IN SHARE ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE runtime_generations IN SHARE ROW EXCLUSIVE MODE NOWAIT;
DO $$ BEGIN
 IF (SELECT MAX(version) FROM schema_migrations) IS DISTINCT FROM '000123_xinsight_policy_delegation' THEN
  RAISE EXCEPTION 'M124 requires exact M123 predecessor'; END IF;
 IF EXISTS(SELECT 1 FROM runtime_generations g LEFT JOIN runtime_support_manifests m USING(artifact_digest)
  WHERE g.state <> 'stopped' AND (m.artifact_digest IS NULL OR NOT COALESCE(
   convert_from(m.canonical_bytes,'UTF8')::jsonb->'schema_heads' ? '000124_xinsight_receiver_adoption',false)
   OR NOT COALESCE(convert_from(m.canonical_bytes,'UTF8')::jsonb->'implementations' @>
    '[{"kind":"xinsight_connector","id":"unirouter.xinsight.connector-control.v3"}]'::jsonb,false))) THEN
  RAISE EXCEPTION 'serving or recovery generation lacks M124 receiver adoption support'; END IF;
END $$;
CREATE FUNCTION require_xinsight_adoption_runtime_support() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.state <> 'stopped' AND NOT EXISTS(SELECT 1 FROM runtime_support_manifests m
  WHERE m.artifact_digest=NEW.artifact_digest
   AND COALESCE(convert_from(m.canonical_bytes,'UTF8')::jsonb->'schema_heads' ? '000124_xinsight_receiver_adoption',false)
   AND COALESCE(convert_from(m.canonical_bytes,'UTF8')::jsonb->'implementations' @>
    '[{"kind":"xinsight_connector","id":"unirouter.xinsight.connector-control.v3"}]'::jsonb,false)) THEN
  RAISE EXCEPTION 'runtime generation lacks M124 receiver adoption support'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER runtime_generation_xinsight_adoption_support BEFORE INSERT OR UPDATE ON runtime_generations
 FOR EACH ROW EXECUTE FUNCTION require_xinsight_adoption_runtime_support();
ALTER TABLE xinsight_connector_installations ADD COLUMN receiver_mode VARCHAR(16) NOT NULL DEFAULT 'new_install'
 CHECK(receiver_mode IN ('new_install','adopt_existing'));
ALTER TABLE xinsight_connector_control_bindings
 ADD COLUMN redeemed_origin VARCHAR(512),
 ADD COLUMN receiver_adoption_bootstrap_canonical BYTEA,
 ADD COLUMN receiver_adoption_bootstrap_sha256 VARCHAR(71),
 ADD CONSTRAINT xinsight_adoption_bootstrap_pair CHECK(
  (receiver_adoption_bootstrap_canonical IS NULL AND receiver_adoption_bootstrap_sha256 IS NULL) OR
  (receiver_adoption_bootstrap_canonical IS NOT NULL AND octet_length(receiver_adoption_bootstrap_canonical) BETWEEN 1 AND 4096
   AND receiver_adoption_bootstrap_sha256 IS NOT NULL AND receiver_adoption_bootstrap_sha256 ~ '^sha256:[a-f0-9]{64}$'));
ALTER TABLE xinsight_connector_control_bindings ADD CONSTRAINT xinsight_redeemed_origin_completed
 CHECK (redeemed_origin IS NULL OR (redeemed_origin=trim(redeemed_origin)
  AND redeemed_origin LIKE 'https://%' AND redeemed_origin !~ '[[:cntrl:]]'
  AND invitation_token_enc IS NULL AND control_token_enc IS NOT NULL
  AND redeem_request_sha256 IS NOT NULL AND redeem_result_sha256 IS NOT NULL));
CREATE FUNCTION protect_xinsight_redeemed_origin() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.redeemed_origin IS DISTINCT FROM OLD.redeemed_origin
  AND (OLD.redeemed_origin IS NOT NULL OR OLD.redeem_result_sha256 IS NOT NULL) THEN
  RAISE EXCEPTION 'redeemed origin is immutable and cannot upgrade historical authority'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER xinsight_redeemed_origin_identity BEFORE UPDATE ON xinsight_connector_control_bindings
 FOR EACH ROW EXECUTE FUNCTION protect_xinsight_redeemed_origin();
CREATE FUNCTION protect_xinsight_receiver_mode() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.receiver_mode IS DISTINCT FROM OLD.receiver_mode THEN
  RAISE EXCEPTION 'receiver mode is immutable'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER xinsight_receiver_mode_identity BEFORE UPDATE ON xinsight_connector_installations
 FOR EACH ROW EXECUTE FUNCTION protect_xinsight_receiver_mode();
CREATE FUNCTION protect_xinsight_adoption_bootstrap() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE mode text;
BEGIN
 SELECT receiver_mode INTO mode FROM xinsight_connector_installations WHERE installation_id=NEW.installation_id;
 IF (mode='adopt_existing') IS DISTINCT FROM (NEW.receiver_adoption_bootstrap_canonical IS NOT NULL) THEN
  RAISE EXCEPTION 'receiver mode and bootstrap must agree'; END IF;
 IF TG_OP='UPDATE' AND (NEW.receiver_adoption_bootstrap_canonical,NEW.receiver_adoption_bootstrap_sha256)
  IS DISTINCT FROM (OLD.receiver_adoption_bootstrap_canonical,OLD.receiver_adoption_bootstrap_sha256) THEN
  RAISE EXCEPTION 'receiver bootstrap is immutable'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER xinsight_adoption_bootstrap_identity BEFORE INSERT OR UPDATE ON xinsight_connector_control_bindings
 FOR EACH ROW EXECUTE FUNCTION protect_xinsight_adoption_bootstrap();
CREATE TABLE xinsight_receiver_adoptions (
 installation_id VARCHAR(128) PRIMARY KEY,
 org_id BIGINT NOT NULL,
 project_id BIGINT NOT NULL,
 feed_key_id BIGINT NOT NULL UNIQUE REFERENCES api_keys(id),
 bind_operation_id VARCHAR(128) NOT NULL UNIQUE,
 candidate_sha256 VARCHAR(71) NOT NULL CHECK(candidate_sha256 ~ '^sha256:[a-f0-9]{64}$'),
 adoption_binding_sha256 VARCHAR(71) NOT NULL CHECK(adoption_binding_sha256 ~ '^sha256:[a-f0-9]{64}$'),
 status VARCHAR(16) NOT NULL CHECK(status IN ('bound','confirmed')),
 bound_at TIMESTAMPTZ NOT NULL,
 confirm_operation_id VARCHAR(128),
 confirm_result_sha256 VARCHAR(71),
 xi_adoption_receipt_sha256 VARCHAR(71),
 feed_read_receipt_sha256 VARCHAR(71),
 confirmed_at TIMESTAMPTZ,
 FOREIGN KEY(installation_id,org_id,project_id) REFERENCES xinsight_connector_installations(installation_id,org_id,project_id),
 FOREIGN KEY(installation_id,bind_operation_id) REFERENCES xinsight_connector_phase_operations(installation_id,operation_id),
 FOREIGN KEY(installation_id,confirm_operation_id) REFERENCES xinsight_connector_phase_operations(installation_id,operation_id),
 CHECK ((status='bound' AND confirm_operation_id IS NULL AND confirm_result_sha256 IS NULL
  AND xi_adoption_receipt_sha256 IS NULL AND feed_read_receipt_sha256 IS NULL AND confirmed_at IS NULL)
  OR (status='confirmed' AND confirm_operation_id IS NOT NULL
   AND confirm_result_sha256 IS NOT NULL AND confirm_result_sha256 ~ '^sha256:[a-f0-9]{64}$'
   AND xi_adoption_receipt_sha256 IS NOT NULL AND xi_adoption_receipt_sha256 ~ '^sha256:[a-f0-9]{64}$'
   AND feed_read_receipt_sha256 IS NOT NULL AND feed_read_receipt_sha256 ~ '^sha256:[a-f0-9]{64}$'
   AND confirmed_at IS NOT NULL AND confirmed_at>=bound_at))
);
CREATE FUNCTION protect_xinsight_adoption_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.installation_id,NEW.org_id,NEW.project_id,NEW.feed_key_id,NEW.bind_operation_id,
     NEW.candidate_sha256,NEW.adoption_binding_sha256,NEW.bound_at)
 IS DISTINCT FROM
    (OLD.installation_id,OLD.org_id,OLD.project_id,OLD.feed_key_id,OLD.bind_operation_id,
     OLD.candidate_sha256,OLD.adoption_binding_sha256,OLD.bound_at)
 OR (OLD.status='confirmed' AND NEW IS DISTINCT FROM OLD) THEN
  RAISE EXCEPTION 'receiver adoption identity and confirmed receipt are immutable'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER xinsight_adoption_identity BEFORE UPDATE ON xinsight_receiver_adoptions
 FOR EACH ROW EXECUTE FUNCTION protect_xinsight_adoption_identity();
CREATE OR REPLACE FUNCTION verify_source_budget_coverage_commit() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE c execution_source_budget_coverage%ROWTYPE; m JSONB; ledger TEXT[]; certified_ledger TEXT[];
BEGIN
    SELECT * INTO c FROM execution_source_budget_coverage WHERE coverage_epoch=NEW.coverage_epoch;
    SELECT array_agg(version::text ORDER BY version) INTO ledger FROM schema_migrations;
    certified_ledger := ledger;
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
-- Bounded normal-administrator consent, on the original installation lane.
ALTER TABLE xinsight_connector_installations
 ADD COLUMN setup_request_id UUID UNIQUE,
 ADD COLUMN setup_consent_canonical BYTEA,
 ADD COLUMN setup_consent_sha256 VARCHAR(71),
 ADD COLUMN setup_state VARCHAR(16),
 ADD COLUMN setup_progress JSONB,
 ADD CONSTRAINT xinsight_setup_consent_pair CHECK (
  (setup_request_id IS NULL AND setup_consent_canonical IS NULL AND setup_consent_sha256 IS NULL AND setup_state IS NULL AND setup_progress IS NULL) OR
  (setup_request_id IS NOT NULL AND setup_consent_canonical IS NOT NULL
   AND octet_length(setup_consent_canonical) BETWEEN 1 AND 32768
   AND setup_consent_sha256 IS NOT NULL AND setup_consent_sha256='sha256:'||encode(sha256(setup_consent_canonical),'hex')
   AND setup_state IS NOT NULL AND setup_state IN ('waiting_consent','provisioning','needs_action','waiting_data','ready','failed')
   AND setup_progress IS NOT NULL AND jsonb_typeof(setup_progress)='object'
   AND octet_length(setup_progress::text)<=8192));
CREATE FUNCTION protect_xinsight_setup_consent() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.setup_request_id IS NOT NULL AND (
  NEW.setup_request_id IS DISTINCT FROM OLD.setup_request_id OR
  NEW.setup_consent_canonical IS DISTINCT FROM OLD.setup_consent_canonical OR
  NEW.setup_consent_sha256 IS DISTINCT FROM OLD.setup_consent_sha256 OR
  NEW.consented_by IS DISTINCT FROM OLD.consented_by) THEN
  RAISE EXCEPTION 'setup consent identity is immutable'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER xinsight_setup_consent_identity BEFORE UPDATE ON xinsight_connector_installations
 FOR EACH ROW EXECUTE FUNCTION protect_xinsight_setup_consent();
COMMIT;
