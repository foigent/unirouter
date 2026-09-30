-- unirouter:explicit-compatible-expand-v1
-- P2a is additive. Old project policy rows remain explicit; old media jobs retain no request deadline.
BEGIN;
LOCK TABLE runtime_change_releases IN ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE runtime_change_slots IN SHARE ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE runtime_generations IN SHARE ROW EXCLUSIVE MODE NOWAIT;
DO $$ BEGIN
 IF (SELECT MAX(version) FROM schema_migrations) IS DISTINCT FROM '000121_gateway_pre_dispatch_terminal' THEN
  RAISE EXCEPTION 'M122 requires exact M121 predecessor'; END IF;
 IF EXISTS(SELECT 1 FROM runtime_generations g LEFT JOIN runtime_support_manifests m USING(artifact_digest)
  WHERE g.state <> 'stopped' AND (m.artifact_digest IS NULL OR NOT COALESCE(
   convert_from(m.canonical_bytes,'UTF8')::jsonb->'schema_heads' ? '000122_scoped_content_policy',false)
   OR NOT COALESCE(convert_from(m.canonical_bytes,'UTF8')::jsonb->'implementations' @>
    '[{"kind":"source_content","id":"unirouter.content-policy.v2"},{"kind":"media_request","id":"unirouter.media-request-expiry.v1"}]'::jsonb,false))) THEN
  RAISE EXCEPTION 'serving or recovery generation lacks M122 scoped policy and media expiry support'; END IF;
END $$;
CREATE FUNCTION require_scoped_content_runtime_support() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.state <> 'stopped' AND NOT EXISTS(SELECT 1 FROM runtime_support_manifests m
  WHERE m.artifact_digest=NEW.artifact_digest
   AND COALESCE(convert_from(m.canonical_bytes,'UTF8')::jsonb->'schema_heads' ? '000122_scoped_content_policy',false)
   AND COALESCE(convert_from(m.canonical_bytes,'UTF8')::jsonb->'implementations' @>
    '[{"kind":"source_content","id":"unirouter.content-policy.v2"},{"kind":"media_request","id":"unirouter.media-request-expiry.v1"}]'::jsonb,false)) THEN
  RAISE EXCEPTION 'runtime requires scoped policy and media expiry implementations'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER runtime_generations_scoped_content_support BEFORE INSERT OR UPDATE ON runtime_generations
 FOR EACH ROW EXECUTE FUNCTION require_scoped_content_runtime_support();

CREATE TABLE execution_content_enterprise_policies (
 org_id BIGINT PRIMARY KEY REFERENCES orgs(id) ON DELETE RESTRICT,
 desired_version BIGINT NOT NULL CHECK(desired_version>0),
 effective_version BIGINT NOT NULL CHECK(effective_version>0 AND effective_version<=desired_version),
 desired_config BYTEA NOT NULL CHECK(octet_length(desired_config) BETWEEN 2 AND 16384),
 effective_config BYTEA NOT NULL CHECK(octet_length(effective_config) BETWEEN 2 AND 16384),
 desired_execution_storage_mode TEXT NOT NULL CHECK(desired_execution_storage_mode IN ('temporary_allowed','strict_no_persist')),
 effective_execution_storage_mode TEXT NOT NULL CHECK(effective_execution_storage_mode IN ('temporary_allowed','strict_no_persist')),
 desired_default_project_quota_bytes BIGINT NOT NULL CHECK(desired_default_project_quota_bytes>0),
 effective_default_project_quota_bytes BIGINT NOT NULL CHECK(effective_default_project_quota_bytes>0),
 effective_at TIMESTAMPTZ NOT NULL,
 application_status TEXT NOT NULL CHECK(application_status IN ('applied','pending','failed')),
 application_reason TEXT NOT NULL CHECK(application_reason IN ('none','awaiting_activation','storage_unavailable','invalid_configuration')),
 CHECK(application_status<>'applied' OR (desired_version=effective_version AND desired_config=effective_config
  AND desired_execution_storage_mode=effective_execution_storage_mode
  AND desired_default_project_quota_bytes=effective_default_project_quota_bytes AND application_reason='none'))
);
CREATE TABLE execution_content_user_policies (
 org_id BIGINT NOT NULL, project_id BIGINT NOT NULL, user_id BIGINT NOT NULL,
 desired_version BIGINT NOT NULL CHECK(desired_version>0),
 effective_version BIGINT NOT NULL CHECK(effective_version>0 AND effective_version<=desired_version),
 desired_config BYTEA NOT NULL CHECK(octet_length(desired_config) BETWEEN 2 AND 16384),
 effective_config BYTEA NOT NULL CHECK(octet_length(effective_config) BETWEEN 2 AND 16384),
 desired_execution_storage_mode TEXT NOT NULL CHECK(desired_execution_storage_mode IN ('temporary_allowed','strict_no_persist')),
 effective_execution_storage_mode TEXT NOT NULL CHECK(effective_execution_storage_mode IN ('temporary_allowed','strict_no_persist')),
 effective_at TIMESTAMPTZ NOT NULL,
 application_status TEXT NOT NULL CHECK(application_status IN ('applied','pending','failed')),
 application_reason TEXT NOT NULL CHECK(application_reason IN ('none','awaiting_activation','storage_unavailable','invalid_configuration')),
 PRIMARY KEY(org_id,project_id,user_id),
 FOREIGN KEY(project_id,org_id) REFERENCES projects(id,org_id) ON DELETE RESTRICT,
 FOREIGN KEY(user_id,org_id) REFERENCES users(id,org_id) ON DELETE RESTRICT,
 CHECK(application_status<>'applied' OR (desired_version=effective_version AND desired_config=effective_config
  AND desired_execution_storage_mode=effective_execution_storage_mode AND application_reason='none'))
);
ALTER TABLE execution_content_policies
 ADD COLUMN explicit_override BOOLEAN NOT NULL DEFAULT TRUE,
 ADD COLUMN desired_execution_storage_mode TEXT NOT NULL DEFAULT 'temporary_allowed'
  CHECK(desired_execution_storage_mode IN ('temporary_allowed','strict_no_persist')),
 ADD COLUMN effective_execution_storage_mode TEXT NOT NULL DEFAULT 'temporary_allowed'
  CHECK(effective_execution_storage_mode IN ('temporary_allowed','strict_no_persist')),
 ADD COLUMN project_quota_bytes BIGINT CHECK(project_quota_bytes>0);
ALTER TABLE execution_content_objects
 ADD COLUMN policy_ref TEXT CHECK(policy_ref IS NULL OR (length(policy_ref) BETWEEN 1 AND 128 AND policy_ref !~ '[[:cntrl:]]')),
 ADD COLUMN policy_scope TEXT CHECK(policy_scope IS NULL OR policy_scope IN ('enterprise','project','user')),
 ADD COLUMN policy_vector JSONB,
 ADD CONSTRAINT execution_content_objects_scoped_policy_pair CHECK((policy_ref IS NULL AND policy_scope IS NULL AND policy_vector IS NULL)
  OR (policy_ref IS NOT NULL AND policy_scope IS NOT NULL AND policy_vector IS NOT NULL));
ALTER TABLE async_media_jobs
 ADD COLUMN request_expires_at TIMESTAMPTZ,
 ADD COLUMN request_cleared_at TIMESTAMPTZ,
 ADD COLUMN execution_storage_mode TEXT CHECK(execution_storage_mode IS NULL OR execution_storage_mode='temporary_allowed'),
 ADD COLUMN content_policy_ref TEXT CHECK(content_policy_ref IS NULL OR (length(content_policy_ref) BETWEEN 1 AND 128 AND content_policy_ref !~ '[[:cntrl:]]')),
 ADD COLUMN content_policy_version BIGINT CHECK(content_policy_version IS NULL OR content_policy_version>0),
 ADD COLUMN content_policy_vector JSONB,
 ADD CONSTRAINT async_media_request_deadline_consistency CHECK(
  (request_expires_at IS NULL AND request_cleared_at IS NULL AND execution_storage_mode IS NULL
   AND content_policy_ref IS NULL AND content_policy_version IS NULL AND content_policy_vector IS NULL)
  OR (request_expires_at IS NOT NULL AND request_expires_at>created_at AND execution_storage_mode='temporary_allowed'
   AND content_policy_ref IS NOT NULL AND content_policy_version IS NOT NULL AND content_policy_vector IS NOT NULL
   AND ((request_ciphertext IS NULL)=(request_cleared_at IS NOT NULL))));
CREATE INDEX async_media_jobs_request_expiry ON async_media_jobs(request_expires_at,job_id)
 WHERE request_expires_at IS NOT NULL AND request_ciphertext IS NOT NULL;
CREATE FUNCTION set_async_media_request_cleared_at() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.request_expires_at IS NOT NULL AND OLD.request_ciphertext IS NOT NULL AND NEW.request_ciphertext IS NULL THEN
  NEW.request_cleared_at := clock_timestamp();
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER async_media_request_cleared_at BEFORE UPDATE ON async_media_jobs
 FOR EACH ROW EXECUTE FUNCTION set_async_media_request_cleared_at();
CREATE OR REPLACE FUNCTION verify_source_budget_coverage_commit() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE c execution_source_budget_coverage%ROWTYPE; m JSONB; ledger TEXT[]; certified_ledger TEXT[];
BEGIN
    SELECT * INTO c FROM execution_source_budget_coverage WHERE coverage_epoch=NEW.coverage_epoch;
    SELECT array_agg(version::text ORDER BY version) INTO ledger FROM schema_migrations;
    certified_ledger := ledger;
    IF ledger[cardinality(ledger)]='000122_scoped_content_policy' THEN
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
