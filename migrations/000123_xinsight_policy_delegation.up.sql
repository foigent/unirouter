-- unirouter:explicit-compatible-expand-v1
BEGIN;
LOCK TABLE runtime_change_releases IN ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE runtime_change_slots IN SHARE ROW EXCLUSIVE MODE NOWAIT;
LOCK TABLE runtime_generations IN SHARE ROW EXCLUSIVE MODE NOWAIT;
DO $$ BEGIN
 IF (SELECT MAX(version) FROM schema_migrations) IS DISTINCT FROM '000122_scoped_content_policy' THEN
  RAISE EXCEPTION 'M123 requires exact M122 predecessor'; END IF;
 IF EXISTS(SELECT 1 FROM runtime_generations g LEFT JOIN runtime_support_manifests m USING(artifact_digest)
  WHERE g.state <> 'stopped' AND (m.artifact_digest IS NULL OR NOT COALESCE(
   convert_from(m.canonical_bytes,'UTF8')::jsonb->'schema_heads' ? '000123_xinsight_policy_delegation',false)
   OR NOT COALESCE(convert_from(m.canonical_bytes,'UTF8')::jsonb->'implementations' @>
    '[{"kind":"xinsight_policy","id":"unirouter.xinsight.policy-write.v1"},{"kind":"xinsight_source_enrollment","id":"unirouter.xinsight.source-enrollment.v1"}]'::jsonb,false))) THEN
  RAISE EXCEPTION 'serving or recovery generation lacks M123 machine authority support'; END IF;
END $$;
CREATE FUNCTION require_xinsight_policy_runtime_support() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.state <> 'stopped' AND NOT EXISTS(SELECT 1 FROM runtime_support_manifests m
  WHERE m.artifact_digest=NEW.artifact_digest
   AND COALESCE(convert_from(m.canonical_bytes,'UTF8')::jsonb->'schema_heads' ? '000123_xinsight_policy_delegation',false)
   AND COALESCE(convert_from(m.canonical_bytes,'UTF8')::jsonb->'implementations' @>
    '[{"kind":"xinsight_policy","id":"unirouter.xinsight.policy-write.v1"},{"kind":"xinsight_source_enrollment","id":"unirouter.xinsight.source-enrollment.v1"}]'::jsonb,false)) THEN
  RAISE EXCEPTION 'runtime requires X-Insight policy and source enrollment support'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER runtime_generations_xinsight_policy_support BEFORE INSERT OR UPDATE ON runtime_generations
 FOR EACH ROW EXECUTE FUNCTION require_xinsight_policy_runtime_support();
LOCK TABLE api_keys,audit_logs IN ACCESS EXCLUSIVE MODE NOWAIT;
ALTER TABLE api_keys DROP CONSTRAINT api_keys_completed_feed_purpose_check,
 ADD CONSTRAINT api_keys_completed_feed_purpose_check CHECK (purpose IN
  ('','xinsight.feed.read','xinsight.source.read','xinsight.policy.manage','xinsight.source.enroll'));
ALTER TABLE api_keys DROP CONSTRAINT api_keys_completed_feed_status_check,
 ADD CONSTRAINT api_keys_completed_feed_status_check CHECK (
  (purpose='' AND status NOT IN ('feed_active','feed_staged','feed_revoked')) OR
  (purpose IN ('xinsight.feed.read','xinsight.source.read','xinsight.policy.manage','xinsight.source.enroll')
   AND status IN ('feed_active','feed_staged','feed_revoked','disabled')));
ALTER TABLE api_keys DROP CONSTRAINT api_keys_verifier_scheme_purpose_check,
 ADD CONSTRAINT api_keys_verifier_scheme_purpose_check CHECK (
  (purpose='' AND verifier_scheme='legacy_hmac_sha256_v1' AND registration_operation_id='') OR
  (purpose='xinsight.feed.read' AND ((verifier_scheme='legacy_hmac_sha256_v1' AND registration_operation_id='')
   OR (verifier_scheme='receiver_sha256_v1' AND registration_operation_id ~ '^[0-9a-f]{32}$'))) OR
  (purpose IN ('xinsight.source.read','xinsight.policy.manage','xinsight.source.enroll')
   AND verifier_scheme='receiver_sha256_v1' AND registration_operation_id ~ '^[0-9a-f]{32}$'));

CREATE TABLE xinsight_machine_credentials (
 api_key_id BIGINT PRIMARY KEY REFERENCES api_keys(id) ON DELETE RESTRICT,
 installation_id VARCHAR(128) NOT NULL,
 org_id BIGINT NOT NULL,
 anchor_project_id BIGINT NOT NULL,
 x_deployment_id VARCHAR(128) NOT NULL,
 source_instance VARCHAR(128) NOT NULL,
 purpose VARCHAR(48) NOT NULL CHECK(purpose IN ('xinsight.policy.manage','xinsight.source.enroll')),
 registration_sha256 VARCHAR(71) NOT NULL CHECK(registration_sha256 ~ '^sha256:[a-f0-9]{64}$'),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 FOREIGN KEY(installation_id,org_id,anchor_project_id)
  REFERENCES xinsight_connector_installations(installation_id,org_id,project_id) ON DELETE RESTRICT,
 FOREIGN KEY(anchor_project_id,org_id) REFERENCES projects(id,org_id) ON DELETE RESTRICT,
 UNIQUE(installation_id,purpose,registration_sha256)
);
CREATE UNIQUE INDEX xinsight_machine_one_active_purpose
 ON api_keys(project_id,purpose) WHERE purpose IN ('xinsight.policy.manage','xinsight.source.enroll') AND status='feed_active';
CREATE UNIQUE INDEX xinsight_machine_registration_operation ON api_keys(org_id,project_id,purpose,registration_operation_id)
 WHERE purpose IN ('xinsight.policy.manage','xinsight.source.enroll');

CREATE TABLE xinsight_policy_delegations (
 id BIGSERIAL PRIMARY KEY,
 installation_id VARCHAR(128) NOT NULL,
 org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE RESTRICT,
 anchor_project_id BIGINT NOT NULL,
 target_kind VARCHAR(32) NOT NULL CHECK(target_kind IN ('enterprise_default','project_policy','user_exception')),
 coverage VARCHAR(24) NOT NULL CHECK(coverage IN ('exact','all_current_future')),
 exact_project_id BIGINT,
 exact_user_id BIGINT,
 can_read BOOLEAN NOT NULL DEFAULT FALSE,
 can_write BOOLEAN NOT NULL DEFAULT FALSE,
 grantor_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 state VARCHAR(12) NOT NULL DEFAULT 'active' CHECK(state IN ('active','revoked')),
 expires_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 FOREIGN KEY(installation_id,org_id,anchor_project_id)
  REFERENCES xinsight_connector_installations(installation_id,org_id,project_id) ON DELETE RESTRICT,
 FOREIGN KEY(exact_project_id,org_id) REFERENCES projects(id,org_id) ON DELETE RESTRICT,
 FOREIGN KEY(exact_user_id,org_id) REFERENCES users(id,org_id) ON DELETE RESTRICT,
 CHECK(can_read OR can_write),
 CHECK((target_kind='enterprise_default' AND coverage='exact' AND exact_project_id IS NULL AND exact_user_id IS NULL)
  OR (target_kind='project_policy' AND exact_user_id IS NULL AND
      ((coverage='exact' AND exact_project_id IS NOT NULL) OR (coverage='all_current_future' AND exact_project_id IS NULL)))
  OR (target_kind='user_exception' AND
      ((coverage='exact' AND exact_project_id IS NOT NULL AND exact_user_id IS NOT NULL)
       OR (coverage='all_current_future' AND exact_project_id IS NULL AND exact_user_id IS NULL))))
);
CREATE INDEX xinsight_policy_delegations_installation ON xinsight_policy_delegations(installation_id,state);

CREATE TABLE xinsight_policy_operations (
 installation_id VARCHAR(128) NOT NULL,
 operation_ref VARCHAR(64) NOT NULL,
 request_sha256 VARCHAR(71) NOT NULL CHECK(request_sha256 ~ '^sha256:[a-f0-9]{64}$'),
 delegation_id BIGINT NOT NULL REFERENCES xinsight_policy_delegations(id) ON DELETE RESTRICT,
 org_id BIGINT NOT NULL,
 project_id BIGINT,
 user_id BIGINT,
 policy_ref VARCHAR(128) NOT NULL,
 state VARCHAR(12) NOT NULL CHECK(state IN ('pending','committed')),
 result JSONB,
 audit_log_id BIGINT REFERENCES audit_logs(id) ON DELETE RESTRICT,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 PRIMARY KEY(installation_id,operation_ref),
 FOREIGN KEY(project_id,org_id) REFERENCES projects(id,org_id) ON DELETE RESTRICT,
 FOREIGN KEY(user_id,org_id) REFERENCES users(id,org_id) ON DELETE RESTRICT
 ,CHECK((state='pending' AND result IS NULL AND audit_log_id IS NULL) OR
        (state='committed' AND result IS NOT NULL AND audit_log_id IS NOT NULL))
);

CREATE TABLE xinsight_source_enrollments (
 id BIGSERIAL PRIMARY KEY,
 installation_id VARCHAR(128) NOT NULL,
 org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE RESTRICT,
 anchor_project_id BIGINT NOT NULL,
 coverage VARCHAR(24) NOT NULL CHECK(coverage IN ('exact','all_current_future')),
 exact_project_id BIGINT,
 profiles TEXT[] NOT NULL DEFAULT '{}',
 include_content BOOLEAN NOT NULL DEFAULT FALSE,
 grantor_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 state VARCHAR(12) NOT NULL DEFAULT 'active' CHECK(state IN ('active','revoked')),
 expires_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 FOREIGN KEY(installation_id,org_id,anchor_project_id)
  REFERENCES xinsight_connector_installations(installation_id,org_id,project_id) ON DELETE RESTRICT,
 FOREIGN KEY(exact_project_id,org_id) REFERENCES projects(id,org_id) ON DELETE RESTRICT,
 CHECK((coverage='exact' AND exact_project_id IS NOT NULL) OR
       (coverage='all_current_future' AND exact_project_id IS NULL)),
 CHECK(cardinality(profiles)>0 OR include_content)
);
CREATE TABLE xinsight_source_enrollment_bindings (
 enrollment_id BIGINT NOT NULL REFERENCES xinsight_source_enrollments(id) ON DELETE RESTRICT,
 org_id BIGINT NOT NULL,
 project_id BIGINT NOT NULL,
 kind VARCHAR(8) NOT NULL CHECK(kind IN ('facts','content')),
 api_key_id BIGINT NOT NULL UNIQUE REFERENCES api_keys(id) ON DELETE RESTRICT,
 source_instance VARCHAR(128) NOT NULL,
 operation_id VARCHAR(32) NOT NULL CHECK(operation_id ~ '^[0-9a-f]{32}$'),
 bind_operation_id VARCHAR(32) CHECK(bind_operation_id ~ '^[0-9a-f]{32}$'),
 bind_request_sha256 VARCHAR(71) CHECK(bind_request_sha256 ~ '^sha256:[a-f0-9]{64}$'),
 state VARCHAR(20) NOT NULL CHECK(state IN ('pending','active','failed','revoked')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 PRIMARY KEY(enrollment_id,org_id,project_id,kind),
 FOREIGN KEY(project_id,org_id) REFERENCES projects(id,org_id) ON DELETE RESTRICT,
 CHECK((bind_operation_id IS NULL)=(bind_request_sha256 IS NULL))
);
CREATE UNIQUE INDEX xinsight_source_bind_operation
 ON xinsight_source_enrollment_bindings(enrollment_id,bind_operation_id) WHERE bind_operation_id IS NOT NULL;
CREATE TABLE xinsight_source_enrollment_exclusions (
 enrollment_id BIGINT NOT NULL REFERENCES xinsight_source_enrollments(id) ON DELETE RESTRICT,
 org_id BIGINT NOT NULL,
 project_id BIGINT NOT NULL,
 excluded BOOLEAN NOT NULL,
 revision BIGINT NOT NULL DEFAULT 1 CHECK(revision>0),
 updated_by BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 PRIMARY KEY(enrollment_id,project_id),
 FOREIGN KEY(project_id,org_id) REFERENCES projects(id,org_id) ON DELETE RESTRICT
);

-- Auto-enrolled read credentials carry a visible, immutable discriminator so
-- pre-M123 manual credentials never depend on the new authorization table.
-- A bound credential cannot shed the discriminator on an ordinary API-key edit.
CREATE FUNCTION protect_xinsight_enrollment_key_label() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM xinsight_source_enrollment_bindings b WHERE b.api_key_id=OLD.id)
    AND NEW.policy_labels IS DISTINCT FROM 'xinsight_auto_enrollment' THEN
  RAISE EXCEPTION 'auto-enrolled source credential label is immutable';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER api_keys_xinsight_enrollment_label BEFORE UPDATE OF policy_labels ON api_keys
 FOR EACH ROW EXECUTE FUNCTION protect_xinsight_enrollment_key_label();

ALTER TABLE audit_logs ALTER COLUMN user_id DROP NOT NULL,
 ADD COLUMN actor_kind VARCHAR(8) NOT NULL DEFAULT 'human'
  CHECK(actor_kind IN ('human','service','system')),
 ADD COLUMN actor_ref VARCHAR(96),
 ADD COLUMN delegation_id BIGINT REFERENCES xinsight_policy_delegations(id) ON DELETE RESTRICT;
UPDATE audit_logs SET actor_kind=CASE WHEN user_id=0 THEN 'system' ELSE 'human' END,
 actor_ref=CASE WHEN user_id=0 THEN 'system' ELSE 'user:'||user_id::text END;
CREATE FUNCTION set_audit_actor_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.actor_kind='human' AND NEW.user_id=0 THEN
  NEW.actor_kind:='system';
 END IF;
 IF NEW.actor_kind='human' AND NEW.actor_ref IS NULL THEN
  NEW.actor_ref:='user:'||NEW.user_id::text;
 ELSIF NEW.actor_kind='system' AND NEW.actor_ref IS NULL THEN
  NEW.actor_ref:='system';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER audit_actor_identity_default BEFORE INSERT OR UPDATE OF user_id,actor_kind,actor_ref,source ON audit_logs
 FOR EACH ROW EXECUTE FUNCTION set_audit_actor_identity();
ALTER TABLE audit_logs ALTER COLUMN actor_ref SET NOT NULL;
ALTER TABLE audit_logs ADD CONSTRAINT audit_actor_identity CHECK(
 (actor_kind='service' AND user_id IS NULL AND actor_ref IS NOT NULL AND actor_ref ~ '^api_key:[1-9][0-9]*$') OR
 (actor_kind='human' AND user_id IS NOT NULL AND user_id > 0 AND actor_ref=('user:'||user_id::text)) OR
 (actor_kind='system' AND (user_id=0 OR user_id IS NULL) AND actor_ref='system'));
-- The M118 certificate remains bound to its original ledger. Accept only the
-- exact additive suffix through M123 when a restore rebase is committed.
CREATE OR REPLACE FUNCTION verify_source_budget_coverage_commit() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE c execution_source_budget_coverage%ROWTYPE; m JSONB; ledger TEXT[]; certified_ledger TEXT[];
BEGIN
    SELECT * INTO c FROM execution_source_budget_coverage WHERE coverage_epoch=NEW.coverage_epoch;
    SELECT array_agg(version::text ORDER BY version) INTO ledger FROM schema_migrations;
    certified_ledger := ledger;
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
