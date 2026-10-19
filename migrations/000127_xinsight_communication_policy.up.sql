-- Maintenance boundary: stop and retire old writers before applying M127.
BEGIN;
LOCK TABLE runtime_change_releases IN ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE runtime_generations, runtime_change_slots, runtime_ingress_changes IN SHARE ROW EXCLUSIVE MODE NOWAIT;
DO $$ BEGIN
 IF (SELECT MAX(version) FROM schema_migrations) IS DISTINCT FROM '000126_commercial_license_retirement' THEN
  RAISE EXCEPTION 'M127 requires exact M126 predecessor'; END IF;
 IF EXISTS(SELECT 1 FROM runtime_generations WHERE state<>'stopped' OR ingress_state<>'absent')
 OR EXISTS(SELECT 1 FROM runtime_ingress_changes WHERE status IN ('pending','ambiguous')) THEN
  RAISE EXCEPTION 'M127 requires stopped retired writers'; END IF;
END $$;
CREATE TABLE xinsight_communication_policies (
 org_id BIGINT NOT NULL, project_id BIGINT NOT NULL,
 automatic_enabled BOOLEAN NOT NULL DEFAULT FALSE,
 cadence_ms INTEGER DEFAULT 60000 CHECK(cadence_ms BETWEEN 250 AND 3600000),
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 next_auto_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 lease_id TEXT, lease_kind TEXT CHECK(lease_kind IN ('automatic','manual')),
 lease_installation TEXT, lease_revision BIGINT, dispatch_deadline TIMESTAMPTZ, owner_instance TEXT, owner_artifact TEXT,
 peer_status BYTEA CHECK(octet_length(peer_status)<=32768), peer_observed_at TIMESTAMPTZ,
 last_attempt_at TIMESTAMPTZ, last_success_at TIMESTAMPTZ, last_code TEXT NOT NULL DEFAULT '',
 updated_by BIGINT REFERENCES users(id) ON DELETE SET NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 PRIMARY KEY(org_id,project_id),
 FOREIGN KEY(project_id,org_id) REFERENCES projects(id,org_id) ON DELETE CASCADE,
 CHECK((lease_id IS NULL AND lease_kind IS NULL AND lease_installation IS NULL AND lease_revision IS NULL AND dispatch_deadline IS NULL AND owner_instance IS NULL AND owner_artifact IS NULL)
 OR (lease_id IS NOT NULL AND lease_kind IS NOT NULL AND lease_installation IS NOT NULL AND lease_revision IS NOT NULL AND dispatch_deadline IS NOT NULL AND owner_instance IS NOT NULL AND owner_artifact IS NOT NULL))
);
-- NULL retains each existing deployment's configured interval, including overrides.
INSERT INTO xinsight_communication_policies(org_id,project_id,automatic_enabled,cadence_ms)
 SELECT DISTINCT org_id,project_id,TRUE,NULL::INTEGER FROM xinsight_connector_installations WHERE observed_state<>'disconnected';
CREATE TABLE xinsight_communication_runs (
 org_id BIGINT NOT NULL,project_id BIGINT NOT NULL,idempotency_key VARCHAR(128) NOT NULL,
 installation_id VARCHAR(128) NOT NULL,deployment_id VARCHAR(128) NOT NULL,revision BIGINT NOT NULL,
 requested_by BIGINT REFERENCES users(id) ON DELETE SET NULL,lease_id TEXT NOT NULL,
 state TEXT NOT NULL DEFAULT 'running' CHECK(state IN ('running','completed','empty','partial','failed','interrupted')),
 units INTEGER NOT NULL DEFAULT 0 CHECK(units BETWEEN 0 AND 8),requests INTEGER NOT NULL DEFAULT 0 CHECK(requests BETWEEN 0 AND 24),
 started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),finished_at TIMESTAMPTZ,
 PRIMARY KEY(org_id,project_id,idempotency_key),
 FOREIGN KEY(installation_id,org_id,project_id) REFERENCES xinsight_connector_installations(installation_id,org_id,project_id),
 CHECK((state='running')=(finished_at IS NULL))
);
CREATE INDEX xinsight_communication_runs_recent ON xinsight_communication_runs(org_id,project_id,started_at DESC);
CREATE FUNCTION initialize_xinsight_communication_policy() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.observed_state='disconnected' THEN RETURN NEW; END IF;
 INSERT INTO xinsight_communication_policies(org_id,project_id) VALUES(NEW.org_id,NEW.project_id)
 ON CONFLICT(org_id,project_id) DO UPDATE SET automatic_enabled=FALSE,cadence_ms=60000,revision=xinsight_communication_policies.revision+1,peer_status=NULL,peer_observed_at=NULL,last_attempt_at=NULL,last_success_at=NULL,last_code='',updated_by=NULL,updated_at=clock_timestamp()
 WHERE xinsight_communication_policies.lease_id IS NULL;
 IF NOT FOUND THEN RAISE EXCEPTION 'Prior communication must finish before replacing its connection'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER xinsight_communication_policy_initialize AFTER INSERT ON xinsight_connector_installations FOR EACH ROW EXECUTE FUNCTION initialize_xinsight_communication_policy();
CREATE OR REPLACE FUNCTION verify_source_budget_coverage_commit() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE c execution_source_budget_coverage%ROWTYPE; m JSONB; ledger TEXT[]; certified_ledger TEXT[];
BEGIN
    SELECT * INTO c FROM execution_source_budget_coverage WHERE coverage_epoch=NEW.coverage_epoch;
    SELECT array_agg(version::text ORDER BY version) INTO ledger FROM schema_migrations;
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
