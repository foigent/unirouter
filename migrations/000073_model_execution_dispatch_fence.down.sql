-- Join the same lifecycle lock as migrate.Run before inspecting the migration
-- ledger. A future migration need not touch billing_provider_attempts, so the
-- table lock alone cannot stabilize schema_migrations.
BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE billing_provider_attempts IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM schema_migrations
         WHERE version > '000073_model_execution_dispatch_fence'
    ) THEN
        RAISE EXCEPTION 'Model execution dispatch fence rollback refused: later migrations must be reversed first';
    END IF;
    IF EXISTS (
        SELECT 1 FROM billing_provider_attempts
         WHERE dispatch_owner_hash IS NOT NULL
            OR health_outcome IS NOT NULL
            OR health_applied_at IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'Model execution dispatch fence rollback refused: dispatch owner or terminal health evidence exists';
    END IF;
END;
$$;

DROP TRIGGER IF EXISTS billing_provider_attempts_dispatch_fence_immutable
    ON billing_provider_attempts;
DROP FUNCTION IF EXISTS preserve_provider_attempt_dispatch_fence();

ALTER TABLE billing_provider_attempts
    DROP CONSTRAINT billing_provider_attempts_released_undispatched_check,
    DROP CONSTRAINT billing_provider_attempts_health_fence_check,
    DROP CONSTRAINT billing_provider_attempts_dispatch_owner_check,
    DROP CONSTRAINT billing_provider_attempts_state_certainty_check,
    DROP CONSTRAINT billing_provider_attempts_certainty_check,
    DROP CONSTRAINT billing_provider_attempts_state_check,
    DROP COLUMN health_applied_at,
    DROP COLUMN health_outcome,
    DROP COLUMN dispatch_owner_hash,
    ADD CONSTRAINT billing_provider_attempts_state_check CHECK (state IN (
        'dispatch_started', 'rejected', 'response_accepted', 'outcome_unknown',
        'response_unusable', 'funding_pending', 'delivery_pending',
        'delivery_failed', 'output_withheld', 'settled', 'reconciled'
    )),
    ADD CONSTRAINT billing_provider_attempts_certainty_check CHECK (
        outcome_certainty IN ('pending', 'known_rejected', 'unknown', 'known_usage')
    ),
    ADD CONSTRAINT billing_provider_attempts_state_certainty_check CHECK (
        (state IN ('dispatch_started', 'response_accepted') AND outcome_certainty = 'pending')
        OR (state = 'rejected' AND outcome_certainty = 'known_rejected')
        OR (state IN ('outcome_unknown', 'response_unusable') AND outcome_certainty = 'unknown')
        OR (state IN ('funding_pending', 'delivery_pending', 'delivery_failed', 'output_withheld', 'settled')
            AND outcome_certainty = 'known_usage')
        OR (state = 'reconciled' AND outcome_certainty IN ('unknown', 'known_usage'))
    );

DELETE FROM schema_migrations
WHERE version='000073_model_execution_dispatch_fence';

COMMIT;
