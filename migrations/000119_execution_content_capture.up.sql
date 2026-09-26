-- unirouter:explicit-compatible-expand-v1
BEGIN;
LOCK TABLE runtime_change_releases IN ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE runtime_change_slots IN SHARE ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE runtime_generations IN SHARE ROW EXCLUSIVE MODE NOWAIT;
DO $$ BEGIN
 IF NOT pg_try_advisory_xact_lock(86420517831702492) THEN
  RAISE EXCEPTION 'content expansion owner is busy' USING ERRCODE='55P03';
 END IF;
 -- A surviving old writer can still run a same-purpose bulk revoke. A future
 -- lease renewal failure cannot make that window safe: reject the migration.
 IF EXISTS(SELECT 1 FROM runtime_generations g LEFT JOIN runtime_support_manifests m USING(artifact_digest)
  WHERE g.state <> 'stopped' AND (m.artifact_digest IS NULL
   OR NOT (convert_from(m.canonical_bytes,'UTF8')::jsonb->'schema_heads' ? '000119_execution_content_capture')
   OR NOT (convert_from(m.canonical_bytes,'UTF8')::jsonb->'implementations' @>
    '[{"kind":"source_content","id":"unirouter.source-content-lifecycle.v1"}]'::jsonb)))
 THEN RAISE EXCEPTION 'all serving and recovery generations must support M119 content lifecycle'; END IF;
END $$;
CREATE FUNCTION require_source_content_runtime_support() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.state <> 'stopped' AND NOT EXISTS(SELECT 1 FROM runtime_support_manifests m
  WHERE m.artifact_digest=NEW.artifact_digest
   AND convert_from(m.canonical_bytes,'UTF8')::jsonb->'schema_heads' ? '000119_execution_content_capture'
   AND convert_from(m.canonical_bytes,'UTF8')::jsonb->'implementations' @>
    '[{"kind":"source_content","id":"unirouter.source-content-lifecycle.v1"}]'::jsonb)
 THEN RAISE EXCEPTION 'runtime requires the content lifecycle implementation'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER runtime_generations_source_content_support BEFORE INSERT OR UPDATE ON runtime_generations
 FOR EACH ROW EXECUTE FUNCTION require_source_content_runtime_support();

LOCK TABLE projects IN SHARE ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE api_keys,execution_source_connections,execution_source_credentials IN ACCESS EXCLUSIVE MODE NOWAIT;
ALTER TABLE api_keys ADD COLUMN source_lifecycle_scope TEXT;
-- This backfill adds a lifecycle label without changing the old key's authority.
-- All writers are excluded by the table lock; the exact trigger state and this
-- temporary suspension are transactional, including failures during backfill.
DO $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_trigger
  WHERE tgrelid='public.api_keys'::regclass
   AND tgname='trg_api_keys_auth_epoch_monotonic' AND NOT tgisinternal
   AND tgenabled='O' AND tgtype=19 AND tgnargs=0 AND tgattr::text='' AND tgqual IS NULL
   AND tgfoid='public.enforce_api_key_auth_epoch()'::regprocedure)
 THEN RAISE EXCEPTION 'content migration requires the original enabled API key auth epoch trigger'; END IF;
END $$;
ALTER TABLE api_keys DISABLE TRIGGER trg_api_keys_auth_epoch_monotonic;
UPDATE api_keys SET source_lifecycle_scope='legacy' WHERE purpose='xinsight.source.read';
ALTER TABLE api_keys ENABLE TRIGGER trg_api_keys_auth_epoch_monotonic;
CREATE FUNCTION default_source_lifecycle_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.purpose='xinsight.source.read' AND NEW.source_lifecycle_scope IS NULL THEN
  NEW.source_lifecycle_scope := 'legacy';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER api_keys_source_lifecycle_default BEFORE INSERT ON api_keys
 FOR EACH ROW EXECUTE FUNCTION default_source_lifecycle_scope();
ALTER TABLE api_keys ADD CONSTRAINT api_keys_source_lifecycle_scope_check CHECK (
 (purpose='xinsight.source.read' AND source_lifecycle_scope IS NOT NULL AND source_lifecycle_scope IN ('legacy','content'))
 OR (purpose<>'xinsight.source.read' AND source_lifecycle_scope IS NULL)),
 ADD CONSTRAINT api_keys_source_lifecycle_identity UNIQUE(id,source_lifecycle_scope);
DROP INDEX api_keys_one_active_xinsight_source;
DROP INDEX api_keys_one_staged_xinsight_source;
CREATE UNIQUE INDEX api_keys_one_active_xinsight_source ON api_keys(project_id,source_lifecycle_scope)
 WHERE purpose='xinsight.source.read' AND status='feed_active';
CREATE UNIQUE INDEX api_keys_one_staged_xinsight_source ON api_keys(project_id,source_lifecycle_scope)
 WHERE purpose='xinsight.source.read' AND status='feed_staged';
ALTER TABLE execution_source_credentials ADD COLUMN source_lifecycle_scope TEXT NOT NULL DEFAULT 'legacy',
 ADD CONSTRAINT execution_source_credentials_scope_identity UNIQUE(api_key_id,org_id,project_id,source_lifecycle_scope),
 ADD CONSTRAINT execution_source_credentials_scope_key FOREIGN KEY(api_key_id,source_lifecycle_scope)
 REFERENCES api_keys(id,source_lifecycle_scope) ON DELETE RESTRICT;
ALTER TABLE execution_source_connections ADD COLUMN content_safety_version BIGINT NOT NULL DEFAULT 1 CHECK(content_safety_version>0);
CREATE FUNCTION advance_content_safety_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status IS DISTINCT FROM OLD.status THEN NEW.content_safety_version := OLD.content_safety_version+1;
 ELSE NEW.content_safety_version := OLD.content_safety_version; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER execution_source_connections_content_safety BEFORE UPDATE ON execution_source_connections
 FOR EACH ROW EXECUTE FUNCTION advance_content_safety_version();
ALTER TABLE execution_source_connections DROP CONSTRAINT execution_source_connections_profiles_check,
 ADD CONSTRAINT execution_source_connections_profiles_check CHECK(cardinality(profiles) BETWEEN 0 AND 11 AND profiles <@ ARRAY[
 'unirouter.execution-context.v1','unirouter.execution-identity.v1','unirouter.cost-basis.v1',
 'unirouter.runtime-observation.v1','unirouter.attempt-observation.v1','unirouter.capability-snapshot.v1','unirouter.policy-observation.v1','unirouter.destination-observation.v1',
 'unirouter.budget-definition.v1','unirouter.budget-period-summary.v1','unirouter.budget-alert.v1']::text[]);
ALTER TABLE execution_source_credentials DROP CONSTRAINT execution_source_credentials_profiles_check,
 ADD CONSTRAINT execution_source_credentials_profiles_check CHECK(
 (source_lifecycle_scope='content' AND profiles=ARRAY['unirouter.interaction-content.v1']::text[]) OR
 (source_lifecycle_scope='legacy' AND cardinality(profiles) BETWEEN 1 AND 11 AND profiles <@ ARRAY[
 'unirouter.execution-context.v1','unirouter.execution-identity.v1','unirouter.cost-basis.v1',
 'unirouter.runtime-observation.v1','unirouter.attempt-observation.v1','unirouter.capability-snapshot.v1','unirouter.policy-observation.v1','unirouter.destination-observation.v1',
 'unirouter.budget-definition.v1','unirouter.budget-period-summary.v1','unirouter.budget-alert.v1']::text[]));

CREATE TABLE execution_content_streams (
 org_id BIGINT NOT NULL, project_id BIGINT NOT NULL, source_instance VARCHAR(128) NOT NULL,
 high_sequence BIGINT NOT NULL DEFAULT 0, retained_after_sequence BIGINT NOT NULL DEFAULT 0,
 retained_after_time TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 recovery_epoch BIGINT NOT NULL DEFAULT 1 CHECK(recovery_epoch>0),
 PRIMARY KEY(org_id,project_id),
 FOREIGN KEY(project_id,org_id) REFERENCES projects(id,org_id) ON DELETE RESTRICT,
 CHECK(retained_after_sequence>=0 AND high_sequence>=retained_after_sequence)
);
-- recovery_epoch has exactly one authority: execution_content_streams.
CREATE TABLE execution_source_content_scopes (
 org_id BIGINT NOT NULL, project_id BIGINT NOT NULL,
 status TEXT NOT NULL DEFAULT 'paused' CHECK(status IN ('active','paused','revoked')),
 state_version BIGINT NOT NULL DEFAULT 0 CHECK(state_version>=0),
 access_generation BIGINT NOT NULL DEFAULT 1 CHECK(access_generation>0),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(org_id,project_id),
 FOREIGN KEY(org_id,project_id) REFERENCES execution_source_connections(org_id,project_id) ON DELETE RESTRICT,
 FOREIGN KEY(org_id,project_id) REFERENCES execution_content_streams(org_id,project_id) ON DELETE RESTRICT
);
CREATE TABLE execution_source_content_bindings (
 api_key_id BIGINT PRIMARY KEY,
 org_id BIGINT NOT NULL, project_id BIGINT NOT NULL,
 source_lifecycle_scope TEXT NOT NULL DEFAULT 'content' CHECK(source_lifecycle_scope='content'),
 coverage_after_sequence BIGINT NOT NULL CHECK(coverage_after_sequence>=0),
 coverage_time TIMESTAMPTZ NOT NULL,
 allowed_purposes TEXT[] NOT NULL CHECK(allowed_purposes=ARRAY['human_review']::text[]),
 FOREIGN KEY(api_key_id,org_id,project_id,source_lifecycle_scope)
  REFERENCES execution_source_credentials(api_key_id,org_id,project_id,source_lifecycle_scope) ON DELETE RESTRICT,
 FOREIGN KEY(org_id,project_id) REFERENCES execution_source_content_scopes(org_id,project_id) ON DELETE RESTRICT
);
CREATE TRIGGER execution_source_content_bindings_immutable BEFORE UPDATE ON execution_source_content_bindings
 FOR EACH ROW EXECUTE FUNCTION execution_source_facts_immutable();
CREATE TABLE execution_source_content_operations (
 org_id BIGINT NOT NULL, project_id BIGINT NOT NULL, operation_id TEXT NOT NULL CHECK(operation_id~'^[0-9a-f]{32}$'),
 request_sha256 TEXT NOT NULL CHECK(request_sha256~'^sha256:[a-f0-9]{64}$'),
 result_version BIGINT NOT NULL CHECK(result_version>0),
 safety_version BIGINT NOT NULL CHECK(safety_version>0),
 connection_status TEXT NOT NULL CHECK(connection_status IN ('active','paused','revoked')),
 response BYTEA NOT NULL CHECK(octet_length(response) BETWEEN 2 AND 32768),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(org_id,project_id,operation_id),
 FOREIGN KEY(org_id,project_id) REFERENCES execution_source_content_scopes(org_id,project_id) ON DELETE RESTRICT
);
CREATE TRIGGER execution_source_content_operations_immutable BEFORE UPDATE ON execution_source_content_operations
 FOR EACH ROW EXECUTE FUNCTION execution_source_facts_immutable();

CREATE TABLE execution_content_policies (
 org_id BIGINT NOT NULL, project_id BIGINT NOT NULL,
 desired_version BIGINT NOT NULL CHECK(desired_version>0), effective_version BIGINT NOT NULL CHECK(effective_version>0 AND effective_version<=desired_version),
 desired_config BYTEA NOT NULL CHECK(octet_length(desired_config) BETWEEN 2 AND 16384),
 effective_config BYTEA NOT NULL CHECK(octet_length(effective_config) BETWEEN 2 AND 16384),
 effective_at TIMESTAMPTZ NOT NULL,
 application_status TEXT NOT NULL CHECK(application_status IN ('applied','pending','failed')),
 application_reason TEXT NOT NULL CHECK(application_reason IN ('none','awaiting_activation','storage_unavailable','invalid_configuration')),
 reserved_bytes BIGINT NOT NULL DEFAULT 0 CHECK(reserved_bytes>=0),
 stored_bytes BIGINT NOT NULL DEFAULT 0 CHECK(stored_bytes>=0),
 PRIMARY KEY(org_id,project_id), FOREIGN KEY(project_id,org_id) REFERENCES projects(id,org_id) ON DELETE RESTRICT,
 CHECK(application_status<>'applied' OR (desired_version=effective_version AND desired_config=effective_config AND application_reason='none'))
);
CREATE TABLE execution_content_manifests (
 org_id BIGINT NOT NULL, project_id BIGINT NOT NULL, sequence BIGINT NOT NULL CHECK(sequence>0),
 source_fact_id TEXT NOT NULL CHECK(source_fact_id~'^uf1_[a-f0-9]{64}$'),
 event_id TEXT NOT NULL CHECK(event_id~'^uf1_[a-f0-9]{64}$'), revision BIGINT NOT NULL CHECK(revision>0),
 request_id VARCHAR(64) NOT NULL, document BYTEA NOT NULL CHECK(octet_length(document) BETWEEN 2 AND 65536),
 source_hash TEXT NOT NULL CHECK(source_hash~'^sha256:[a-f0-9]{64}$'),
 published_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(org_id,project_id,sequence), UNIQUE(org_id,project_id,source_fact_id,revision), UNIQUE(event_id),
 FOREIGN KEY(org_id,project_id) REFERENCES execution_content_streams(org_id,project_id) ON DELETE RESTRICT
);
CREATE INDEX execution_content_manifests_retention ON execution_content_manifests(published_at);
CREATE TRIGGER execution_content_manifests_immutable BEFORE UPDATE ON execution_content_manifests
 FOR EACH ROW EXECUTE FUNCTION execution_source_facts_immutable();
CREATE TABLE execution_content_objects (
 content_ref TEXT PRIMARY KEY CHECK(content_ref~'^uc1_[a-f0-9]{64}$'),
 org_id BIGINT NOT NULL, project_id BIGINT NOT NULL, source_fact_id TEXT NOT NULL,
 revision BIGINT NOT NULL CHECK(revision>0), storage_identity TEXT NOT NULL,
 storage_binding_token TEXT NOT NULL CHECK(storage_binding_token~'^[a-f0-9]{64}$'),
 policy_version BIGINT NOT NULL CHECK(policy_version>0),
 policy_config BYTEA NOT NULL CHECK(octet_length(policy_config) BETWEEN 2 AND 16384),
 policy_effective_at TIMESTAMPTZ NOT NULL,
 -- Allocation snapshot only; the authoritative epoch remains the stream row.
 recovery_epoch BIGINT NOT NULL CHECK(recovery_epoch>0),
 relative_path TEXT NOT NULL CHECK(relative_path~'^[a-f0-9]{64}$'),
 state TEXT NOT NULL CHECK(state IN ('reserved','ready','orphan')),
 reserved_bytes BIGINT NOT NULL CHECK(reserved_bytes>=0), saved_bytes BIGINT NOT NULL CHECK(saved_bytes>=0),
 digest TEXT CHECK(digest~'^sha256:[a-f0-9]{64}$'), expires_at TIMESTAMPTZ NOT NULL,
 lease_owner TEXT NOT NULL, lease_expires_at TIMESTAMPTZ NOT NULL,
 UNIQUE(org_id,project_id,source_fact_id,revision),
 FOREIGN KEY(org_id,project_id) REFERENCES execution_content_streams(org_id,project_id) ON DELETE RESTRICT,
 CHECK(state<>'ready' OR digest IS NOT NULL)
);
CREATE INDEX execution_content_objects_ready_expiry ON execution_content_objects(expires_at,content_ref) WHERE state='ready';
CREATE TABLE execution_content_health (
 org_id BIGINT NOT NULL, project_id BIGINT NOT NULL,
 reason TEXT NOT NULL, detail BYTEA NOT NULL CHECK(octet_length(detail)<=16384),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(org_id,project_id), FOREIGN KEY(org_id,project_id) REFERENCES execution_content_streams(org_id,project_id) ON DELETE RESTRICT
);
-- Preserve the original M118 budget proof across this exact additive expansion.
CREATE OR REPLACE FUNCTION verify_source_budget_coverage_commit() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE c execution_source_budget_coverage%ROWTYPE; m JSONB; ledger TEXT[]; certified_ledger TEXT[];
BEGIN
    SELECT * INTO c FROM execution_source_budget_coverage WHERE coverage_epoch=NEW.coverage_epoch;
    SELECT array_agg(version::text ORDER BY version) INTO ledger FROM schema_migrations;
    certified_ledger := ledger;
    IF ledger[cardinality(ledger)]='000119_execution_content_capture' THEN
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
