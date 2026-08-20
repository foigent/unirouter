BEGIN;

-- A coordinator dispatch receipt needs two small durable fences: one owner for
-- commit-ambiguity-safe authorization replay, and one terminal health outcome
-- so retrying a callback cannot apply breaker/credential mutations twice.
ALTER TABLE billing_provider_attempts
    ADD COLUMN dispatch_owner_hash VARCHAR(71),
    ADD COLUMN health_outcome VARCHAR(32),
    ADD COLUMN health_applied_at TIMESTAMPTZ(6);

ALTER TABLE billing_provider_attempts
    DROP CONSTRAINT billing_provider_attempts_state_check,
    DROP CONSTRAINT billing_provider_attempts_certainty_check,
    DROP CONSTRAINT billing_provider_attempts_state_certainty_check;

ALTER TABLE billing_provider_attempts
    ADD CONSTRAINT billing_provider_attempts_state_check CHECK (state IN (
        'dispatch_started', 'rejected', 'response_accepted', 'outcome_unknown',
        'response_unusable', 'funding_pending', 'delivery_pending',
        'delivery_failed', 'output_withheld', 'settled', 'reconciled',
        'released_undispatched'
    )),
    ADD CONSTRAINT billing_provider_attempts_certainty_check CHECK (
        outcome_certainty IN (
            'pending', 'known_rejected', 'unknown', 'known_usage',
            'not_dispatched'
        )
    ),
    ADD CONSTRAINT billing_provider_attempts_state_certainty_check CHECK (
        (state IN ('dispatch_started', 'response_accepted') AND outcome_certainty = 'pending')
        OR (state = 'rejected' AND outcome_certainty = 'known_rejected')
        OR (state IN ('outcome_unknown', 'response_unusable') AND outcome_certainty = 'unknown')
        OR (state IN ('funding_pending', 'delivery_pending', 'delivery_failed', 'output_withheld', 'settled')
            AND outcome_certainty = 'known_usage')
        OR (state = 'reconciled' AND outcome_certainty IN ('unknown', 'known_usage'))
        OR (state = 'released_undispatched' AND outcome_certainty = 'not_dispatched')
    ),
    ADD CONSTRAINT billing_provider_attempts_dispatch_owner_check CHECK (
        dispatch_owner_hash IS NULL
        OR (
            dispatch_owner_hash ~ '^sha256:[a-f0-9]{64}$'
            AND route_evidence_hash IS NOT NULL
        )
    ),
    ADD CONSTRAINT billing_provider_attempts_health_fence_check CHECK (
        (health_outcome IS NULL AND health_applied_at IS NULL)
        OR (
            dispatch_owner_hash IS NOT NULL
            AND route_evidence_hash IS NOT NULL
            AND health_outcome IN (
                'success','provider_failure','auth_failure','rate_limited',
                'released_undispatched'
            )
            AND health_applied_at IS NOT NULL
        )
    ),
    ADD CONSTRAINT billing_provider_attempts_released_undispatched_check CHECK (
        (
            state = 'released_undispatched'
            AND outcome_certainty = 'not_dispatched'
            AND health_outcome = 'released_undispatched'
            AND health_applied_at IS NOT NULL
            AND financial_resolved_at IS NOT NULL
            AND NOT retry_authorized
            AND http_status IS NULL
            AND failure_kind IS NULL
            AND upstream_request_id IS NULL
            AND response_received_at IS NULL
        )
        OR (
            state <> 'released_undispatched'
            AND outcome_certainty <> 'not_dispatched'
            AND health_outcome IS DISTINCT FROM 'released_undispatched'
        )
    );

CREATE FUNCTION preserve_provider_attempt_dispatch_fence()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.dispatch_owner_hash IS DISTINCT FROM OLD.dispatch_owner_hash THEN
        RAISE EXCEPTION 'provider attempt dispatch owner is immutable'
            USING ERRCODE = '55000';
    END IF;
    IF OLD.state = 'released_undispatched' AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'released-undispatched provider attempt is terminal'
            USING ERRCODE = '55000';
    END IF;
    IF OLD.health_outcome IS NULL AND NEW.health_outcome IS NOT NULL THEN
        IF NOT (
            (NEW.health_outcome = 'released_undispatched'
                AND OLD.state = 'dispatch_started'
                AND OLD.failure_kind IS NULL
                AND NEW.state = 'released_undispatched')
            OR (NEW.health_outcome = 'success'
                AND OLD.state = 'response_accepted'
                AND OLD.failure_kind IS NULL
                AND NEW.state = OLD.state)
            OR (NEW.health_outcome = 'provider_failure'
                AND (
                    (OLD.state = 'response_accepted' AND OLD.failure_kind IS NULL)
                    OR (OLD.state IN ('rejected','outcome_unknown')
                        AND OLD.failure_kind IS NOT DISTINCT FROM 'channel_failure')
                )
                AND NEW.state = OLD.state)
            OR (NEW.health_outcome = 'auth_failure'
                AND OLD.state = 'rejected'
                AND OLD.failure_kind IS NOT DISTINCT FROM 'auth'
                AND NEW.state = OLD.state)
            OR (NEW.health_outcome = 'rate_limited'
                AND OLD.state = 'rejected'
                AND OLD.failure_kind IS NOT DISTINCT FROM 'rate_limit'
                AND NEW.state = OLD.state)
        ) THEN
            RAISE EXCEPTION 'provider attempt health outcome is out of sequence'
                USING ERRCODE = '55000';
        END IF;
    END IF;
    IF OLD.health_outcome IS NOT NULL
       AND ROW(NEW.health_outcome,NEW.health_applied_at)
           IS DISTINCT FROM ROW(OLD.health_outcome,OLD.health_applied_at) THEN
        RAISE EXCEPTION 'provider attempt health outcome is terminal'
            USING ERRCODE = '55000';
    END IF;
    IF OLD.health_outcome IS NULL
       AND NEW.health_outcome IS NULL
       AND NEW.health_applied_at IS DISTINCT FROM OLD.health_applied_at THEN
        RAISE EXCEPTION 'provider attempt health timestamp requires an outcome'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER billing_provider_attempts_dispatch_fence_immutable
BEFORE UPDATE ON billing_provider_attempts
FOR EACH ROW EXECUTE FUNCTION preserve_provider_attempt_dispatch_fence();

COMMENT ON COLUMN billing_provider_attempts.dispatch_owner_hash IS
    'SHA-256 of an ephemeral, process-only dispatch owner capability. Exact-owner replay is safe; a new owner cannot redispatch an existing attempt.';
COMMENT ON COLUMN billing_provider_attempts.health_outcome IS
    'One terminal route-health outcome applied atomically with the breaker/credential projection; NULL means not yet applied.';
COMMENT ON COLUMN billing_provider_attempts.health_applied_at IS
    'Database wall-clock time at which the one terminal health outcome was claimed.';

COMMENT ON CONSTRAINT billing_provider_attempts_released_undispatched_check
    ON billing_provider_attempts IS
    'An owned provider attempt released before network dispatch is financially resolved and cannot masquerade as a provider response.';

COMMIT;
