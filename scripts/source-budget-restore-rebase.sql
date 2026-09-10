-- Run by the original restore owner after SQL restore, with backend and
-- recovery writers still stopped. This is deliberately conservative even for
-- a fresh backup. Pre-D backups retain their original restore path.
BEGIN;
DO $$
DECLARE prior RECORD; boundary TIMESTAMPTZ;
BEGIN
    IF to_regclass('public.execution_source_budget_coverage') IS NULL THEN
      IF EXISTS(SELECT 1 FROM schema_migrations WHERE version='000118_source_budget_snapshots') THEN
        RAISE EXCEPTION 'restored D schema is incomplete'; END IF;
      RETURN;
    END IF;
    LOCK TABLE execution_source_budget_coverage IN ACCESS EXCLUSIVE MODE;
    SELECT * INTO STRICT prior FROM execution_source_budget_coverage WHERE ended_at IS NULL FOR UPDATE;
    IF prior.state<>'certified' OR prior.schema_head<>'000118_source_budget_snapshots'
       OR prior.writer_contract<>'unirouter.gateway-financial-journal.v2'
       OR prior.support_manifest_sha256<>encode(sha256(prior.support_manifest_bytes),'hex')
       OR prior.schema_ledger_sha256 IS DISTINCT FROM
          (SELECT encode(sha256(convert_to(string_agg(version,E'\n' ORDER BY version),'UTF8')),'hex') FROM schema_migrations)
       OR (SELECT MAX(version) FROM schema_migrations)<>'000118_source_budget_snapshots' THEN
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
COMMIT;
