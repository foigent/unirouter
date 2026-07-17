BEGIN;

-- Active policies, not disabled history, consume runtime snapshot capacity.
-- The trigger uses transaction-scoped advisory locks so concurrent writers on
-- different replicas cannot each observe spare capacity and over-admit.
CREATE OR REPLACE FUNCTION enforce_guardrail_active_policy_quota()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    org_limit INTEGER := COALESCE(NULLIF(current_setting('unirouter.guardrail_org_active_policy_limit', TRUE), '')::INTEGER, 256);
    platform_limit INTEGER := COALESCE(NULLIF(current_setting('unirouter.guardrail_platform_active_policy_limit', TRUE), '')::INTEGER, 256);
    cache_limit INTEGER := COALESCE(NULLIF(current_setting('unirouter.guardrail_cached_policy_limit', TRUE), '')::INTEGER, 4096);
    tenant_scope_limit INTEGER := COALESCE(NULLIF(current_setting('unirouter.guardrail_cached_tenant_limit', TRUE), '')::INTEGER, 4096);
    active_count BIGINT;
    active_scope_count BIGINT;
BEGIN
    IF NEW.mode = 'disabled' THEN
        RETURN NEW;
    END IF;
    IF org_limit <= 0 OR platform_limit <= 0 OR cache_limit <= 0 OR tenant_scope_limit <= 0 THEN
        RAISE EXCEPTION 'guardrail active-policy quota configuration is invalid'
            USING ERRCODE = '22023';
    END IF;

    -- Lock the aggregate cache budget first, then the target scope. This fixed
    -- ordering avoids deadlocks for concurrent cross-scope updates.
    PERFORM pg_advisory_xact_lock(hashtextextended('unirouter:guardrail-policy-quota:cache', 0));
    PERFORM pg_advisory_xact_lock(hashtextextended(
        'unirouter:guardrail-policy-quota:scope:' || COALESCE(NEW.org_id::TEXT, 'platform'), 0
    ));

    SELECT COUNT(*) INTO active_count
    FROM guardrail_policies
    WHERE mode <> 'disabled' AND id <> COALESCE(NEW.id, 0);
    IF active_count >= cache_limit THEN
        RAISE EXCEPTION 'guardrail active policy cache limit % exceeded', cache_limit
            USING ERRCODE = '23514', CONSTRAINT = 'guardrail_active_policy_cache_quota';
    END IF;

    SELECT COUNT(*) INTO active_count
    FROM guardrail_policies
    WHERE org_id IS NOT DISTINCT FROM NEW.org_id
      AND mode <> 'disabled'
      AND id <> COALESCE(NEW.id, 0);
    IF NEW.org_id IS NULL AND active_count >= platform_limit THEN
        RAISE EXCEPTION 'guardrail platform active policy limit % exceeded', platform_limit
            USING ERRCODE = '23514', CONSTRAINT = 'guardrail_platform_active_policy_quota';
    END IF;
    IF NEW.org_id IS NOT NULL AND active_count >= org_limit THEN
        RAISE EXCEPTION 'guardrail organization active policy limit % exceeded', org_limit
            USING ERRCODE = '23514', CONSTRAINT = 'guardrail_org_active_policy_quota';
    END IF;

    IF NEW.org_id IS NOT NULL AND active_count = 0 THEN
        SELECT COUNT(DISTINCT org_id) INTO active_scope_count
        FROM guardrail_policies
        WHERE org_id IS NOT NULL
          AND mode <> 'disabled'
          AND id <> COALESCE(NEW.id, 0);
        IF active_scope_count >= tenant_scope_limit THEN
            RAISE EXCEPTION 'guardrail active tenant scope cache limit % exceeded', tenant_scope_limit
                USING ERRCODE = '23514', CONSTRAINT = 'guardrail_active_tenant_scope_quota';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER guardrail_active_policy_quota
BEFORE INSERT OR UPDATE OF org_id, mode ON guardrail_policies
FOR EACH ROW EXECUTE FUNCTION enforce_guardrail_active_policy_quota();

-- Advisory failures are metadata-only and aggregated by one bounded key. The
-- unique key guarantees repeated frame drops increment an exact count rather
-- than creating one database row per frame.
CREATE TABLE guardrail_advisory_gap_receipts (
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE RESTRICT,
    request_id VARCHAR(64) NOT NULL,
    policy_ref VARCHAR(160) NOT NULL,
    policy_version BIGINT NOT NULL,
    reason VARCHAR(24) NOT NULL,
    window_started_at TIMESTAMPTZ NOT NULL,
    window_seconds SMALLINT NOT NULL,
    dropped_count BIGINT NOT NULL,
    first_occurred_at TIMESTAMPTZ NOT NULL,
    last_occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (org_id, request_id, policy_ref, policy_version, reason, window_started_at),
    CONSTRAINT guardrail_advisory_gap_request_check CHECK (length(btrim(request_id)) BETWEEN 1 AND 64),
    CONSTRAINT guardrail_advisory_gap_policy_check CHECK (length(btrim(policy_ref)) BETWEEN 1 AND 160 AND policy_version > 0),
    CONSTRAINT guardrail_advisory_gap_reason_check CHECK (reason IN ('overload', 'timeout', 'shutdown')),
    CONSTRAINT guardrail_advisory_gap_window_check CHECK (window_seconds BETWEEN 1 AND 60),
    CONSTRAINT guardrail_advisory_gap_count_check CHECK (dropped_count > 0),
    CONSTRAINT guardrail_advisory_gap_time_check CHECK (
        first_occurred_at <= last_occurred_at
        AND window_started_at <= first_occurred_at
        AND first_occurred_at < window_started_at + make_interval(secs => window_seconds)
    )
);

CREATE INDEX idx_guardrail_advisory_gap_recent
    ON guardrail_advisory_gap_receipts (org_id, last_occurred_at DESC);

COMMENT ON TABLE guardrail_advisory_gap_receipts IS
    'Metadata-only advisory gap evidence retained without automatic deletion by V1 B1.';

-- A stable batch key makes retries idempotent even when a commit result is
-- ambiguous. This remains bounded metadata and is one row per writer batch,
-- never one row per disclosed stream frame.
CREATE TABLE guardrail_advisory_gap_batches (
    batch_id VARCHAR(64) PRIMARY KEY,
    aggregate_count INTEGER NOT NULL,
    dropped_count BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT guardrail_advisory_gap_batch_aggregate_check CHECK (aggregate_count BETWEEN 1 AND 65536),
    CONSTRAINT guardrail_advisory_gap_batch_count_check CHECK (dropped_count > 0)
);

CREATE INDEX idx_guardrail_advisory_gap_batches_created
    ON guardrail_advisory_gap_batches (created_at);

COMMENT ON TABLE guardrail_advisory_gap_batches IS
    'Metadata-only idempotency ledger retained without automatic deletion by V1 B1; one row per writer batch, never per stream frame.';

COMMIT;
