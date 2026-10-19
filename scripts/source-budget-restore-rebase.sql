-- Run by the original restore owner after SQL restore, with backend and
-- recovery writers and content readers still stopped. D coverage and C cursor
-- epochs change atomically, even for a fresh or paired database/body backup.
BEGIN;
DO $$
DECLARE prior RECORD; boundary TIMESTAMPTZ; ledger TEXT[]; certified_ledger TEXT[];
BEGIN
    IF to_regclass('public.execution_source_budget_coverage') IS NULL THEN
      LOCK TABLE schema_migrations IN SHARE MODE;
      IF EXISTS(SELECT 1 FROM schema_migrations WHERE version >= '000118') THEN
        RAISE EXCEPTION 'restored D schema is incomplete'; END IF;
      IF EXISTS(SELECT 1 FROM unnest(ARRAY[
        'execution_content_streams','execution_source_content_scopes',
        'execution_source_content_bindings','execution_source_content_operations',
        'execution_content_policies','execution_content_manifests',
        'execution_content_objects','execution_content_health']) AS t(name)
        WHERE to_regclass('public.' || name) IS NOT NULL) THEN
        RAISE EXCEPTION 'restored C tables require the exact M119 ledger'; END IF;
      RETURN;
    END IF;
    LOCK TABLE execution_source_budget_coverage IN ACCESS EXCLUSIVE MODE;
    LOCK TABLE schema_migrations IN SHARE MODE;
    SELECT array_agg(version::text ORDER BY version) INTO ledger FROM schema_migrations;
    certified_ledger := ledger;
    IF ledger[cardinality(ledger)]='000127_xinsight_communication_policy' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000126_commercial_license_retirement'
         OR to_regclass('public.xinsight_communication_policies') IS NULL
         OR to_regclass('public.xinsight_communication_runs') IS NULL
         OR NOT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='xinsight_communication_policies'::regclass AND attname='automatic_enabled' AND NOT attisdropped AND atttypid='bool'::regtype AND attnotnull)
         OR NOT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='xinsight_communication_policies'::regclass AND attname='cadence_ms' AND NOT attisdropped AND atttypid='int4'::regtype)
         OR NOT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='xinsight_connector_installations'::regclass AND tgname='xinsight_communication_policy_initialize' AND NOT tgisinternal) THEN
        RAISE EXCEPTION 'M127 restore requires exact communication policy, receipts and M126 predecessor'; END IF;
      ledger:=ledger[1:cardinality(ledger)-1];
      certified_ledger:=ledger;
    END IF;
    IF ledger[cardinality(ledger)]='000126_commercial_license_retirement' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000125_xinsight_control_credential_lifecycle'
         OR EXISTS(SELECT 1 FROM pg_trigger WHERE NOT tgisinternal AND tgname IN
           ('api_keys_commercial_limit','users_commercial_limit','projects_commercial_limit',
            'access_credentials_commercial_limit','provider_connections_commercial_limit')) THEN
        RAISE EXCEPTION 'M126 restore requires exact M125 predecessor and retired commercial triggers'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
      certified_ledger := ledger;
    END IF;
    IF ledger[cardinality(ledger)]='000125_xinsight_control_credential_lifecycle' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000124_xinsight_receiver_adoption'
         OR to_regclass('public.xinsight_connector_credential_operations') IS NULL
         OR NOT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='xinsight_connector_credential_operations'::regclass AND attname='request_enc' AND NOT attisdropped AND atttypid='text'::regtype AND attnotnull)
         OR NOT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='xinsight_connector_credential_operations'::regclass AND attname='replacement_token_enc' AND NOT attisdropped AND atttypid='text'::regtype AND attnotnull)
         OR NOT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='runtime_generations'::regclass AND tgname='runtime_generation_xinsight_control_credential_support' AND NOT tgisinternal) THEN
        RAISE EXCEPTION 'M125 restore requires exact credential journal, runtime guard and M124 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
      certified_ledger := ledger;
    END IF;
    IF ledger[cardinality(ledger)]='000124_xinsight_receiver_adoption' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000123_xinsight_policy_delegation'
         OR to_regclass('public.xinsight_receiver_adoptions') IS NULL
         OR NOT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='xinsight_connector_installations'::regclass AND attname='receiver_mode' AND NOT attisdropped AND attnotnull)
         OR NOT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='xinsight_connector_control_bindings'::regclass AND attname='receiver_adoption_bootstrap_canonical' AND NOT attisdropped AND atttypid='bytea'::regtype)
         OR NOT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='xinsight_connector_control_bindings'::regclass AND attname='receiver_adoption_bootstrap_sha256' AND NOT attisdropped)
         OR NOT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='xinsight_connector_control_bindings'::regclass AND attname='redeemed_origin' AND NOT attisdropped AND atttypid='varchar'::regtype)
         OR NOT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='xinsight_connector_control_bindings'::regclass AND tgname='xinsight_redeemed_origin_identity' AND NOT tgisinternal)
         OR NOT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='xinsight_connector_installations'::regclass AND attname='setup_consent_canonical' AND NOT attisdropped AND atttypid='bytea'::regtype)
         OR NOT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='xinsight_connector_installations'::regclass AND tgname='xinsight_setup_consent_identity' AND NOT tgisinternal) THEN
        RAISE EXCEPTION 'M124 restore requires exact adoption schema and M123 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
      certified_ledger := ledger;
    END IF;
    IF ledger[cardinality(ledger)]='000123_xinsight_policy_delegation' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000122_scoped_content_policy'
         OR ledger[cardinality(ledger)-2] IS DISTINCT FROM '000121_gateway_pre_dispatch_terminal'
         OR ledger[cardinality(ledger)-3] IS DISTINCT FROM '000120_unpublished_content_reclamation'
         OR ledger[cardinality(ledger)-4] IS DISTINCT FROM '000119_execution_content_capture' THEN
        RAISE EXCEPTION 'restored budget coverage proof is invalid: machine policy predecessor'; END IF;
      certified_ledger := ledger[:cardinality(ledger)-5];
    ELSIF ledger[cardinality(ledger)]='000122_scoped_content_policy' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000121_gateway_pre_dispatch_terminal'
         OR ledger[cardinality(ledger)-2] IS DISTINCT FROM '000120_unpublished_content_reclamation'
         OR ledger[cardinality(ledger)-3] IS DISTINCT FROM '000119_execution_content_capture' THEN
        RAISE EXCEPTION 'restored budget coverage proof is invalid: scoped content predecessor'; END IF;
      certified_ledger := ledger[:cardinality(ledger)-4];
    ELSIF ledger[cardinality(ledger)]='000121_gateway_pre_dispatch_terminal' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000120_unpublished_content_reclamation'
         OR ledger[cardinality(ledger)-2] IS DISTINCT FROM '000119_execution_content_capture' THEN
        RAISE EXCEPTION 'restored budget coverage proof is invalid: pre-dispatch predecessor'; END IF;
      certified_ledger := ledger[:cardinality(ledger)-3];
    ELSIF ledger[cardinality(ledger)]='000120_unpublished_content_reclamation' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000119_execution_content_capture' THEN
        RAISE EXCEPTION 'restored budget coverage proof is invalid: content reclamation predecessor'; END IF;
      certified_ledger := ledger[:cardinality(ledger)-2];
    ELSIF ledger[cardinality(ledger)]='000119_execution_content_capture' THEN
      certified_ledger := ledger[:cardinality(ledger)-1];
    END IF;
    SELECT * INTO STRICT prior FROM execution_source_budget_coverage WHERE ended_at IS NULL FOR UPDATE;
    IF prior.state IS DISTINCT FROM 'certified' OR prior.schema_head IS DISTINCT FROM '000118_source_budget_snapshots'
       OR prior.writer_contract IS DISTINCT FROM 'unirouter.gateway-financial-journal.v2'
       OR prior.support_manifest_sha256 IS DISTINCT FROM encode(sha256(prior.support_manifest_bytes),'hex')
       OR certified_ledger[cardinality(certified_ledger)] IS DISTINCT FROM '000118_source_budget_snapshots'
       OR prior.schema_ledger_sha256 IS DISTINCT FROM
          encode(sha256(convert_to(array_to_string(certified_ledger,E'\n'),'UTF8')),'hex') THEN
       RAISE EXCEPTION 'restored budget coverage proof is invalid';
    END IF;
    boundary:=clock_timestamp();
    IF boundary<prior.certified_from THEN RAISE EXCEPTION 'restore clock precedes the retained certification'; END IF;
    UPDATE execution_source_budget_coverage SET ended_at=boundary WHERE coverage_epoch=prior.coverage_epoch;
    INSERT INTO execution_source_budget_coverage(certified_from,state,reason,writer_contract,schema_head,
      migration_sha256,schema_ledger_sha256,support_manifest_sha256,support_manifest_bytes,predecessor_epoch,restore_boundary)
    VALUES(boundary,'certified','restore_rebase',prior.writer_contract,prior.schema_head,
      prior.migration_sha256,prior.schema_ledger_sha256,prior.support_manifest_sha256,prior.support_manifest_bytes,prior.coverage_epoch,boundary);
END $$;
DO $$
DECLARE head TEXT; present INTEGER; prior RECORD; next_epoch BIGINT;
BEGIN
    -- The preceding D block holds coverage -> ledger locks until this same
    -- transaction ends. No reader/writer may be restarted between the blocks.
    SELECT max(version) INTO head FROM schema_migrations;
    SELECT count(*) INTO present FROM unnest(ARRAY[
      'execution_content_streams','execution_source_content_scopes',
      'execution_source_content_bindings','execution_source_content_operations',
      'execution_content_policies','execution_content_manifests',
      'execution_content_objects','execution_content_health']) AS t(name)
      WHERE to_regclass('public.' || name) IS NOT NULL;
    IF head IS DISTINCT FROM '000119_execution_content_capture' AND head IS DISTINCT FROM '000120_unpublished_content_reclamation'
       AND head IS DISTINCT FROM '000121_gateway_pre_dispatch_terminal'
       AND head IS DISTINCT FROM '000122_scoped_content_policy'
       AND head IS DISTINCT FROM '000123_xinsight_policy_delegation'
       AND head IS DISTINCT FROM '000124_xinsight_receiver_adoption'
       AND head IS DISTINCT FROM '000125_xinsight_control_credential_lifecycle'
       AND head IS DISTINCT FROM '000126_commercial_license_retirement' AND head IS DISTINCT FROM '000127_xinsight_communication_policy' THEN
      IF present<>0 THEN RAISE EXCEPTION 'restored C tables require the exact M119 ledger'; END IF;
      RETURN;
    END IF;
    IF present<>8 THEN RAISE EXCEPTION 'restored C schema is incomplete'; END IF;
    IF head IN ('000120_unpublished_content_reclamation','000121_gateway_pre_dispatch_terminal','000122_scoped_content_policy','000123_xinsight_policy_delegation','000124_xinsight_receiver_adoption','000125_xinsight_control_credential_lifecycle','000126_commercial_license_retirement','000127_xinsight_communication_policy') THEN
      IF NOT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='execution_content_objects'::regclass AND attname='reclaimed_at' AND NOT attisdropped AND atttypid='timestamptz'::regtype AND NOT attnotnull) THEN
        RAISE EXCEPTION 'restored content reclamation schema mismatch'; END IF;
    ELSIF EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='execution_content_objects'::regclass AND attname='reclaimed_at' AND NOT attisdropped) THEN
      RAISE EXCEPTION 'pre-M120 schema contains content reclamation state';
    END IF;
    LOCK TABLE execution_content_streams IN ACCESS EXCLUSIVE MODE;
    FOR prior IN SELECT org_id,project_id,recovery_epoch FROM execution_content_streams
      ORDER BY org_id,project_id FOR UPDATE LOOP
      -- A restored sequence or old+1 would repeat after restoring the same
      -- snapshot again. Use fresh entropy, independent of the backup and clock.
      -- Epochs are opaque positive int64 strings, not ordered counters.
      next_epoch := ('x' || substr(md5(gen_random_uuid()::text),1,16))::bit(64)::bigint
        & 9223372036854775807;
      IF next_epoch=0 OR next_epoch=prior.recovery_epoch THEN
        RAISE EXCEPTION 'fresh content recovery epoch collision';
      END IF;
      UPDATE execution_content_streams SET recovery_epoch=next_epoch
        WHERE org_id=prior.org_id AND project_id=prior.project_id;
    END LOOP;
END $$;
COMMIT;
