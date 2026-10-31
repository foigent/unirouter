-- Correct the actual M128 standard-restore failure without changing its predicate.
BEGIN;
LOCK TABLE runtime_generations, runtime_ingress_changes IN SHARE ROW EXCLUSIVE MODE NOWAIT;
DO $$
BEGIN
 IF (SELECT max(version) FROM schema_migrations) IS DISTINCT FROM '000128_upstream_release_observation' THEN
  RAISE EXCEPTION 'M129 requires exact M128 predecessor';
 END IF;
 IF EXISTS(SELECT 1 FROM runtime_generations WHERE state<>'stopped' OR ingress_state<>'absent')
 OR EXISTS(SELECT 1 FROM runtime_ingress_changes WHERE status IN ('pending','ambiguous')) THEN
  RAISE EXCEPTION 'M129 requires stopped retired writers';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace JOIN pg_language l ON l.oid=p.prolang
   WHERE n.nspname='public' AND p.proname='valid_upstream_release_observation'
   AND p.oid='public.valid_upstream_release_observation(jsonb)'::regprocedure
   AND p.provolatile='i' AND NOT p.prosecdef AND NOT p.proisstrict AND NOT p.proleakproof
   AND p.proparallel='u' AND p.prorettype='boolean'::regtype AND l.lanname='plpgsql'
   AND p.prokind='f' AND NOT p.proretset AND p.provariadic=0 AND p.pronargdefaults=0 AND p.proargnames=ARRAY['v']::text[]
   AND encode(sha256(convert_to(p.prosrc,'UTF8')),'hex')='f2ffc71b42a5c0c9e92c0e06f495345ac8d76550284eb82b208727ccc8a70356'
   AND (p.proconfig IS NULL OR p.proconfig=ARRAY['search_path=pg_catalog, public'])
   AND EXISTS(SELECT 1 FROM pg_proc h JOIN pg_language hl ON hl.oid=h.prolang
    WHERE h.oid='public.valid_upstream_release_text(text,integer,boolean)'::regprocedure
    AND encode(sha256(convert_to(h.prosrc,'UTF8')),'hex')='a46a33ebafc84191586d578546880f7bdaabcd46820c65ae0254c3530c7c4d95'
    AND h.provolatile='i' AND NOT h.prosecdef AND NOT h.proisstrict AND NOT h.proleakproof
    AND h.proparallel='u' AND h.prorettype='boolean'::regtype AND hl.lanname='sql' AND h.proconfig IS NULL
    AND h.prokind='f' AND NOT h.proretset AND h.provariadic=0 AND h.pronargdefaults=0 AND h.proargnames=ARRAY['v','n','required']::text[])) THEN
  RAISE EXCEPTION 'M129 refuses an unknown observation validator';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM pg_proc p JOIN pg_language l ON l.oid=p.prolang
   WHERE p.oid='public.verify_source_budget_coverage_commit()'::regprocedure
   AND encode(sha256(convert_to(p.prosrc,'UTF8')),'hex')='f92c935bed2df5936caece500dfe43abfcab6d52bc51c7947430d20b546574f0'
   AND l.lanname='plpgsql' AND p.prorettype='trigger'::regtype AND p.prokind='f' AND p.pronargs=0
   AND p.provolatile='v' AND NOT p.prosecdef AND NOT p.proisstrict AND NOT p.proleakproof
   AND p.proparallel='u' AND NOT p.proretset AND p.provariadic=0 AND p.pronargdefaults=0 AND p.proconfig IS NULL) THEN
  RAISE EXCEPTION 'M129 refuses an unknown financial commit guard';
 END IF;
END $$;
-- unirouter:observation-namespace-correction-begin
ALTER FUNCTION public.valid_upstream_release_observation(jsonb) SET search_path TO pg_catalog, public;
-- unirouter:observation-namespace-correction-end

-- Preserve every financial predicate; recognize only the exact new ledger suffix.
CREATE OR REPLACE FUNCTION public.verify_source_budget_coverage_commit() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE c execution_source_budget_coverage%ROWTYPE; m JSONB; ledger TEXT[]; certified_ledger TEXT[];
BEGIN
    SELECT * INTO c FROM execution_source_budget_coverage WHERE coverage_epoch=NEW.coverage_epoch;
    SELECT array_agg(version::text ORDER BY version) INTO ledger FROM schema_migrations;
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
