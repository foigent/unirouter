-- unirouter:explicit-compatible-expand-v1
BEGIN;
LOCK TABLE runtime_change_releases IN ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE runtime_change_slots IN SHARE ROW EXCLUSIVE MODE NOWAIT;
DO $$ BEGIN
    IF NOT pg_try_advisory_xact_lock(86420517831702492) THEN
        RAISE EXCEPTION 'budget snapshot expansion owner is busy' USING ERRCODE='55P03';
    END IF;
    -- Expired recovery actors must also have exited or carry the new contract.
    IF EXISTS (SELECT 1 FROM runtime_generations g LEFT JOIN runtime_support_manifests m USING(artifact_digest)
       WHERE g.state <> 'stopped' AND (m.artifact_digest IS NULL
        OR NOT (convert_from(m.canonical_bytes,'UTF8')::jsonb->'schema_heads' ? '000118_source_budget_snapshots')
        OR NOT (convert_from(m.canonical_bytes,'UTF8')::jsonb->'implementations' @>
          '[{"kind":"source_budgets","id":"unirouter.budget-snapshot.v1"},{"kind":"financial_journal","id":"unirouter.gateway-financial-journal.v2"}]'::jsonb)))
    THEN RAISE EXCEPTION 'all serving and recovery generations must support M118 budget and journal contracts'; END IF;
END $$;
LOCK TABLE projects, execution_source_connections, execution_source_credentials,
    enterprise_finance_budget_versions IN ACCESS EXCLUSIVE MODE NOWAIT;

-- D has its own authorization/cursor generation. A/B streams are untouched.
CREATE TABLE execution_source_budget_scopes (
    source_instance VARCHAR(128) NOT NULL, org_id BIGINT NOT NULL, project_id BIGINT NOT NULL,
    d_access_generation BIGINT NOT NULL DEFAULT 1 CHECK(d_access_generation > 0),
    catalog_revision BIGINT NOT NULL DEFAULT 1 CHECK(catalog_revision > 0),
    PRIMARY KEY(source_instance,org_id,project_id),
    FOREIGN KEY(org_id,project_id) REFERENCES execution_source_connections(org_id,project_id) ON DELETE RESTRICT
);
INSERT INTO execution_source_budget_scopes(source_instance,org_id,project_id)
SELECT source_instance,org_id,project_id FROM execution_source_connections;
CREATE FUNCTION initialize_source_budget_scope() RETURNS TRIGGER LANGUAGE plpgsql AS $$ BEGIN
    INSERT INTO execution_source_budget_scopes(source_instance,org_id,project_id)
    VALUES(NEW.source_instance,NEW.org_id,NEW.project_id);
    RETURN NEW;
END $$;
CREATE TRIGGER execution_source_budget_scope_init AFTER INSERT ON execution_source_connections
FOR EACH ROW EXECUTE FUNCTION initialize_source_budget_scope();

CREATE FUNCTION invalidate_source_budget_grants() RETURNS TRIGGER LANGUAGE plpgsql AS $$ BEGIN
    IF (SELECT array_agg(p ORDER BY p) FROM unnest(NEW.profiles) p WHERE p IN
       ('unirouter.budget-definition.v1','unirouter.budget-period-summary.v1','unirouter.budget-alert.v1')) IS DISTINCT FROM
       (SELECT array_agg(p ORDER BY p) FROM unnest(OLD.profiles) p WHERE p IN
       ('unirouter.budget-definition.v1','unirouter.budget-period-summary.v1','unirouter.budget-alert.v1')) THEN
      UPDATE execution_source_budget_scopes SET d_access_generation=d_access_generation+1
      WHERE source_instance=NEW.source_instance AND org_id=NEW.org_id AND project_id=NEW.project_id;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER execution_source_budget_grants AFTER UPDATE OF profiles ON execution_source_connections
FOR EACH ROW EXECUTE FUNCTION invalidate_source_budget_grants();

CREATE TABLE execution_source_budget_definitions (
    org_id BIGINT NOT NULL, project_id BIGINT NOT NULL, revision BIGINT NOT NULL CHECK(revision > 0),
    budget_cents BIGINT, budget_period VARCHAR(20), changed_at TIMESTAMPTZ NOT NULL,
    change_kind VARCHAR(24) NOT NULL CHECK(change_kind IN ('observed_baseline','created','value_changed')),
    -- Historical identity remains readable after the original empty default
    -- project is removed by first-customer bootstrap. The original projects
    -- trigger binds identity at write time; current reads still require the
    -- live authorized project. Current bindings retain their project FK.
    PRIMARY KEY(org_id,project_id,revision),
    CHECK(org_id > 0 AND project_id > 0)
);
CREATE FUNCTION source_budget_metadata_immutable() RETURNS TRIGGER LANGUAGE plpgsql AS $$ BEGIN
    RAISE EXCEPTION 'source budget history is immutable';
END $$;
CREATE TRIGGER execution_source_budget_definitions_immutable BEFORE UPDATE OR DELETE ON execution_source_budget_definitions
FOR EACH ROW EXECUTE FUNCTION source_budget_metadata_immutable();
INSERT INTO execution_source_budget_definitions(org_id,project_id,revision,budget_cents,budget_period,changed_at,change_kind)
SELECT org_id,id,1,budget_cents,budget_period,clock_timestamp(),'observed_baseline' FROM projects;
CREATE FUNCTION record_source_budget_definition() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE next_revision BIGINT;
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.budget_cents IS NOT DISTINCT FROM OLD.budget_cents
       AND NEW.budget_period IS NOT DISTINCT FROM OLD.budget_period THEN RETURN NEW; END IF;
    -- The original project writer holds the project row lock; all supported
    -- create/update/budget-only writers enter this same atomic revision path.
    SELECT COALESCE(MAX(revision),0)+1 INTO next_revision FROM execution_source_budget_definitions
     WHERE org_id=NEW.org_id AND project_id=NEW.id;
    INSERT INTO execution_source_budget_definitions(org_id,project_id,revision,budget_cents,budget_period,changed_at,change_kind)
    VALUES(NEW.org_id,NEW.id,next_revision,NEW.budget_cents,NEW.budget_period,clock_timestamp(),
      CASE WHEN TG_OP='INSERT' THEN 'created' ELSE 'value_changed' END);
    UPDATE execution_source_budget_scopes SET catalog_revision=catalog_revision+1
    WHERE org_id=NEW.org_id AND project_id=NEW.id;
    RETURN NEW;
END $$;
CREATE TRIGGER execution_source_budget_definition_revision AFTER INSERT OR UPDATE OF budget_cents,budget_period ON projects
FOR EACH ROW EXECUTE FUNCTION record_source_budget_definition();

CREATE TABLE execution_source_budget_bindings (
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE RESTRICT,
    enterprise_project_scope_ref VARCHAR(160) NOT NULL CHECK(length(enterprise_project_scope_ref)>0),
    project_id BIGINT NOT NULL, revision BIGINT NOT NULL CHECK(revision>0),
    status VARCHAR(16) NOT NULL CHECK(status IN ('active','revoked')),
    audit_ref VARCHAR(200) NOT NULL, changed_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(org_id,enterprise_project_scope_ref),
    FOREIGN KEY(project_id,org_id) REFERENCES projects(id,org_id) ON DELETE RESTRICT
);
CREATE INDEX execution_source_budget_bindings_project ON execution_source_budget_bindings(org_id,project_id)
WHERE status='active';
CREATE TABLE execution_source_budget_binding_history (
    org_id BIGINT NOT NULL, enterprise_project_scope_ref VARCHAR(160) NOT NULL, project_id BIGINT NOT NULL,
    revision BIGINT NOT NULL, status VARCHAR(16) NOT NULL, audit_ref VARCHAR(200) NOT NULL, changed_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY(org_id,enterprise_project_scope_ref,revision),
    FOREIGN KEY(org_id,enterprise_project_scope_ref) REFERENCES execution_source_budget_bindings(org_id,enterprise_project_scope_ref) ON DELETE RESTRICT
);
CREATE TRIGGER execution_source_budget_binding_history_immutable BEFORE UPDATE OR DELETE ON execution_source_budget_binding_history
FOR EACH ROW EXECUTE FUNCTION source_budget_metadata_immutable();
CREATE FUNCTION record_source_budget_binding() RETURNS TRIGGER LANGUAGE plpgsql AS $$ BEGIN
    IF TG_OP='UPDATE' AND (NEW.org_id<>OLD.org_id OR NEW.enterprise_project_scope_ref<>OLD.enterprise_project_scope_ref
       OR NEW.revision<>OLD.revision+1) THEN RAISE EXCEPTION 'budget binding identity or revision is invalid'; END IF;
    IF TG_OP='INSERT' AND NEW.revision<>1 THEN RAISE EXCEPTION 'budget binding starts at revision one'; END IF;
    -- Stable project order also covers rebinding from one project to another.
    PERFORM 1 FROM execution_source_budget_scopes WHERE org_id=NEW.org_id
      AND (project_id=NEW.project_id OR (TG_OP='UPDATE' AND project_id=OLD.project_id))
      ORDER BY project_id,source_instance FOR UPDATE;
    UPDATE execution_source_budget_scopes SET d_access_generation=d_access_generation+1,catalog_revision=catalog_revision+1
      WHERE org_id=NEW.org_id AND (project_id=NEW.project_id OR (TG_OP='UPDATE' AND project_id=OLD.project_id));
    INSERT INTO execution_source_budget_binding_history VALUES
      (NEW.org_id,NEW.enterprise_project_scope_ref,NEW.project_id,NEW.revision,NEW.status,NEW.audit_ref,NEW.changed_at);
    RETURN NEW;
END $$;
CREATE TRIGGER execution_source_budget_binding_revision AFTER INSERT OR UPDATE ON execution_source_budget_bindings
FOR EACH ROW EXECUTE FUNCTION record_source_budget_binding();
CREATE FUNCTION invalidate_source_enterprise_budget_catalog() RETURNS TRIGGER LANGUAGE plpgsql AS $$ BEGIN
    -- Include prior project scopes if a later version changes a budget's scope.
    PERFORM 1 FROM execution_source_budget_scopes s WHERE s.org_id=NEW.org_id AND s.project_id IN
      (SELECT b.project_id FROM execution_source_budget_bindings b JOIN enterprise_finance_budget_versions v
       ON v.org_id=b.org_id AND v.scope_kind='project' AND v.scope_ref=b.enterprise_project_scope_ref
       WHERE v.org_id=NEW.org_id AND v.budget_ref=NEW.budget_ref AND b.status='active')
      ORDER BY s.project_id,s.source_instance FOR UPDATE;
    UPDATE execution_source_budget_scopes s SET catalog_revision=catalog_revision+1
    WHERE s.org_id=NEW.org_id AND s.project_id IN
      (SELECT b.project_id FROM execution_source_budget_bindings b JOIN enterprise_finance_budget_versions v
       ON v.org_id=b.org_id AND v.scope_kind='project' AND v.scope_ref=b.enterprise_project_scope_ref
       WHERE v.org_id=NEW.org_id AND v.budget_ref=NEW.budget_ref AND b.status='active');
    RETURN NEW;
END $$;
CREATE TRIGGER execution_source_enterprise_budget_revision AFTER INSERT ON enterprise_finance_budget_versions
FOR EACH ROW EXECUTE FUNCTION invalidate_source_enterprise_budget_catalog();

-- No amount accumulator. The authoritative settled journal remains the writer.
CREATE INDEX usage_finalization_journal_budget_settled ON usage_finalization_journal(org_id,project_id,occurred_at)
INCLUDE(charged_cost_micros) WHERE event_type='settled';
CREATE INDEX internal_outbox_events_budget_financial ON internal_outbox_events(org_id,event_type)
WHERE event_type IN ('billing.usage.prepared.v1','billing.usage.prepared.v2','billing.settled.v1','billing.settled.v2','billing.cancelled.v1','billing.cancelled.v2');
-- Scope mismatch detection also reads the original payload organization; keep
-- that required fail-closed branch from scanning unrelated financial queues.
CREATE INDEX internal_outbox_events_budget_payload_org ON internal_outbox_events((payload->>'org_id'),event_type)
WHERE event_type IN ('billing.usage.prepared.v1','billing.usage.prepared.v2','billing.settled.v1','billing.settled.v2','billing.cancelled.v1','billing.cancelled.v2');

CREATE TABLE execution_source_budget_coverage (
    coverage_epoch BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    certified_from TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(), ended_at TIMESTAMPTZ,
    state VARCHAR(16) NOT NULL CHECK(state IN ('pending','certified')),
    reason VARCHAR(24) NOT NULL CHECK(reason IN ('initial_certification','restore_rebase')),
    writer_contract TEXT NOT NULL CHECK(writer_contract='unirouter.gateway-financial-journal.v2'),
    schema_head TEXT NOT NULL CHECK(schema_head='000118_source_budget_snapshots'),
    migration_sha256 VARCHAR(71), schema_ledger_sha256 VARCHAR(64), support_manifest_sha256 VARCHAR(64), support_manifest_bytes BYTEA,
    predecessor_epoch BIGINT REFERENCES execution_source_budget_coverage(coverage_epoch) ON DELETE RESTRICT,
    restore_boundary TIMESTAMPTZ,
    CHECK(ended_at IS NULL OR ended_at>=certified_from),
    CHECK((reason='restore_rebase')=(restore_boundary IS NOT NULL)),
    CHECK(state='pending' OR (migration_sha256 ~ '^sha256:[a-f0-9]{64}$' AND support_manifest_sha256 ~ '^[a-f0-9]{64}$'
      AND support_manifest_bytes IS NOT NULL AND migration_sha256 IS NOT NULL AND support_manifest_sha256 IS NOT NULL
      AND schema_ledger_sha256 IS NOT NULL AND schema_ledger_sha256 ~ '^[a-f0-9]{64}$'))
);
CREATE UNIQUE INDEX execution_source_budget_coverage_current ON execution_source_budget_coverage((true)) WHERE ended_at IS NULL;
CREATE FUNCTION verify_source_budget_coverage_commit() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE c execution_source_budget_coverage%ROWTYPE; m JSONB;
BEGIN
    SELECT * INTO c FROM execution_source_budget_coverage WHERE coverage_epoch=NEW.coverage_epoch;
    IF c.state<>'certified' OR c.support_manifest_sha256<>encode(sha256(c.support_manifest_bytes),'hex')
       OR c.schema_ledger_sha256 IS DISTINCT FROM (SELECT encode(sha256(convert_to(string_agg(version,E'\n' ORDER BY version),'UTF8')),'hex') FROM schema_migrations) THEN
      RAISE EXCEPTION 'budget coverage requires verified support proof in the migration transaction'; END IF;
    m := convert_from(c.support_manifest_bytes,'UTF8')::jsonb;
    IF COALESCE((m->'schema_heads' ? '000118_source_budget_snapshots'),false)=false OR COALESCE((m->'implementations' @>
       '[{"kind":"source_budgets","id":"unirouter.budget-snapshot.v1"},{"kind":"financial_journal","id":"unirouter.gateway-financial-journal.v2"}]'::jsonb),false)=false THEN
      RAISE EXCEPTION 'budget coverage support proof lacks the writer or snapshot contract'; END IF;
    RETURN NEW;
END $$;
CREATE CONSTRAINT TRIGGER execution_source_budget_coverage_certified AFTER INSERT OR UPDATE ON execution_source_budget_coverage
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION verify_source_budget_coverage_commit();
CREATE FUNCTION preserve_source_budget_coverage_history() RETURNS TRIGGER LANGUAGE plpgsql AS $$ BEGIN
    IF TG_OP='DELETE' OR (OLD.state='certified' AND
       (OLD.ended_at IS NOT NULL OR NEW.ended_at IS NULL OR (to_jsonb(NEW)-'ended_at')<>(to_jsonb(OLD)-'ended_at'))) THEN
      RAISE EXCEPTION 'certified budget coverage history is immutable'; END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER execution_source_budget_coverage_history BEFORE UPDATE OR DELETE ON execution_source_budget_coverage
FOR EACH ROW EXECUTE FUNCTION preserve_source_budget_coverage_history();
INSERT INTO execution_source_budget_coverage(state,reason,writer_contract,schema_head)
VALUES('pending','initial_certification','unirouter.gateway-financial-journal.v2','000118_source_budget_snapshots');

ALTER TABLE execution_source_connections DROP CONSTRAINT execution_source_connections_profiles_check,
 ADD CONSTRAINT execution_source_connections_profiles_check CHECK(cardinality(profiles) BETWEEN 1 AND 11 AND profiles <@ ARRAY[
 'unirouter.execution-context.v1','unirouter.execution-identity.v1','unirouter.cost-basis.v1',
 'unirouter.runtime-observation.v1','unirouter.attempt-observation.v1','unirouter.capability-snapshot.v1','unirouter.policy-observation.v1','unirouter.destination-observation.v1',
 'unirouter.budget-definition.v1','unirouter.budget-period-summary.v1','unirouter.budget-alert.v1']::text[]);
ALTER TABLE execution_source_credentials DROP CONSTRAINT execution_source_credentials_profiles_check,
 ADD CONSTRAINT execution_source_credentials_profiles_check CHECK(cardinality(profiles) BETWEEN 1 AND 11 AND profiles <@ ARRAY[
 'unirouter.execution-context.v1','unirouter.execution-identity.v1','unirouter.cost-basis.v1',
 'unirouter.runtime-observation.v1','unirouter.attempt-observation.v1','unirouter.capability-snapshot.v1','unirouter.policy-observation.v1','unirouter.destination-observation.v1',
 'unirouter.budget-definition.v1','unirouter.budget-period-summary.v1','unirouter.budget-alert.v1']::text[]);
COMMIT;
