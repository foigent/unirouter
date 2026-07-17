BEGIN;

-- C1.2 binds every live delivery proof to one immutable provider attempt.
-- Historical rows are backfilled only when the provider-attempt ledger or an
-- attempt-bound stream lease supplies one unambiguous identity. Anything else
-- is preserved in quarantine and removed from automatic chargeability.
-- Keep this ALTER first: PostgreSQL retains its AccessExclusive freeze-table
-- lock through commit, so a pre-C1.2 writer cannot cross into the receipt
-- cutover and resumes only after attempt_id is mandatory (where it fails closed).
ALTER TABLE freeze_idempotency
    ADD COLUMN delivery_attempt_id VARCHAR(64);

CREATE TABLE delivery_attempt_quarantine (
    org_id BIGINT NOT NULL,
    request_id VARCHAR(64) NOT NULL,
    reservation_status VARCHAR(32) NOT NULL,
    settlement_pending_reason VARCHAR(40) NOT NULL,
    candidate_attempt_ids TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    receipt_count INTEGER NOT NULL DEFAULT 0,
    quarantine_reason VARCHAR(80) NOT NULL,
    quarantined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (org_id, request_id),
    CONSTRAINT delivery_attempt_quarantine_reservation_fk
        FOREIGN KEY (org_id, request_id)
        REFERENCES freeze_idempotency(org_id, request_id)
        ON DELETE RESTRICT,
    CONSTRAINT delivery_attempt_quarantine_reason_nonempty
        CHECK (length(btrim(quarantine_reason)) > 0),
    CONSTRAINT delivery_attempt_quarantine_receipt_count_nonnegative
        CHECK (receipt_count >= 0)
);

CREATE TABLE request_delivery_receipt_quarantine (
    org_id BIGINT NOT NULL,
    request_id VARCHAR(64) NOT NULL,
    sequence SMALLINT NOT NULL,
    mode VARCHAR(16) NOT NULL,
    outcome VARCHAR(24) NOT NULL,
    delivered_bytes BIGINT NOT NULL,
    delivered_chunks INTEGER NOT NULL,
    http_status INTEGER,
    error_code VARCHAR(80) NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    candidate_attempt_ids TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    quarantine_reason VARCHAR(80) NOT NULL,
    quarantined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (org_id, request_id, sequence),
    CONSTRAINT request_delivery_receipt_quarantine_reservation_fk
        FOREIGN KEY (org_id, request_id)
        REFERENCES freeze_idempotency(org_id, request_id)
        ON DELETE RESTRICT,
    CONSTRAINT request_delivery_receipt_quarantine_reason_nonempty
        CHECK (length(btrim(quarantine_reason)) > 0)
);

ALTER TABLE request_delivery_receipts
    ADD COLUMN attempt_id VARCHAR(64);

CREATE TEMP TABLE delivery_attempt_backfill_candidates
ON COMMIT DROP
AS
WITH receipt_summary AS (
    SELECT org_id, request_id,
           CASE WHEN COUNT(DISTINCT mode) = 1 THEN MIN(mode) ELSE '' END AS mode,
           COUNT(*)::INTEGER AS receipt_count
    FROM request_delivery_receipts
    GROUP BY org_id, request_id
), attempt_summary AS (
    SELECT org_id, request_id,
           COUNT(*) AS attempt_count,
           MIN(attempt_id) AS only_attempt_id
    FROM billing_provider_attempts
    GROUP BY org_id, request_id
), stream_evidence AS (
    SELECT l.org_id, l.request_id, l.attempt_id
    FROM stream_delivery_leases l
    JOIN billing_provider_attempts p
      ON p.org_id = l.org_id
     AND p.request_id = l.request_id
     AND p.attempt_id = l.attempt_id
)
SELECT f.org_id, f.request_id,
       CASE
           WHEN se.attempt_id IS NOT NULL
                AND (rs.mode IS NULL OR rs.mode = 'stream')
             THEN se.attempt_id
           WHEN a.attempt_count = 1
             THEN a.only_attempt_id
           ELSE NULL
       END AS attempt_id
FROM freeze_idempotency f
LEFT JOIN receipt_summary rs
  ON rs.org_id = f.org_id AND rs.request_id = f.request_id
LEFT JOIN attempt_summary a
  ON a.org_id = f.org_id AND a.request_id = f.request_id
LEFT JOIN stream_evidence se
  ON se.org_id = f.org_id AND se.request_id = f.request_id
WHERE rs.receipt_count > 0
   OR (
       f.status = 'settlement_pending'
       AND f.settlement_pending_reason IN ('billable_output', 'delivery_prepared', 'delivery_completed', 'delivery_failed')
       AND a.attempt_count > 0
   );

UPDATE freeze_idempotency f
SET delivery_attempt_id = c.attempt_id
FROM delivery_attempt_backfill_candidates c
WHERE f.org_id = c.org_id
  AND f.request_id = c.request_id
  AND c.attempt_id IS NOT NULL;

-- Backfill and quarantine are a controlled migration of historical evidence.
-- Disable only the append-only guard, transactionally, while the transition
-- trigger continues to protect every newly inserted receipt.
ALTER TABLE request_delivery_receipts
    DISABLE TRIGGER request_delivery_receipt_append_only;

UPDATE request_delivery_receipts r
SET attempt_id = f.delivery_attempt_id
FROM freeze_idempotency f
WHERE r.org_id = f.org_id
  AND r.request_id = f.request_id
  AND f.delivery_attempt_id IS NOT NULL;

INSERT INTO delivery_attempt_quarantine (
    org_id, request_id, reservation_status, settlement_pending_reason,
    candidate_attempt_ids, receipt_count, quarantine_reason
)
SELECT f.org_id, f.request_id, f.status, f.settlement_pending_reason,
       ARRAY(
           SELECT p.attempt_id
           FROM billing_provider_attempts p
           WHERE p.org_id = f.org_id AND p.request_id = f.request_id
           ORDER BY p.attempt_id
       ),
       (SELECT COUNT(*)::INTEGER
        FROM request_delivery_receipts r
        WHERE r.org_id = f.org_id AND r.request_id = f.request_id),
       CASE
           WHEN EXISTS (
               SELECT 1 FROM request_delivery_receipts r
               WHERE r.org_id = f.org_id AND r.request_id = f.request_id
                 AND r.attempt_id IS NULL
           ) THEN 'receipt_attempt_not_uniquely_evidenced'
           ELSE 'pending_attempt_not_uniquely_evidenced'
       END
FROM freeze_idempotency f
WHERE f.delivery_attempt_id IS NULL
  AND (
      EXISTS (
          SELECT 1 FROM request_delivery_receipts r
          WHERE r.org_id = f.org_id AND r.request_id = f.request_id
            AND r.attempt_id IS NULL
      )
      OR (
          f.status = 'settlement_pending'
          AND f.settlement_pending_reason IN ('billable_output', 'delivery_prepared', 'delivery_completed', 'delivery_failed')
          AND EXISTS (
              SELECT 1 FROM billing_provider_attempts p
              WHERE p.org_id = f.org_id AND p.request_id = f.request_id
          )
      )
  );

INSERT INTO request_delivery_receipt_quarantine (
    org_id, request_id, sequence, mode, outcome, delivered_bytes,
    delivered_chunks, http_status, error_code, occurred_at,
    candidate_attempt_ids, quarantine_reason
)
SELECT r.org_id, r.request_id, r.sequence, r.mode, r.outcome,
       r.delivered_bytes, r.delivered_chunks, r.http_status, r.error_code,
       r.occurred_at,
       ARRAY(
           SELECT p.attempt_id
           FROM billing_provider_attempts p
           WHERE p.org_id = r.org_id AND p.request_id = r.request_id
           ORDER BY p.attempt_id
       ),
       'receipt_attempt_not_uniquely_evidenced'
FROM request_delivery_receipts r
WHERE r.attempt_id IS NULL;

-- Pending ambiguous provider-backed rows remain visible for operator review,
-- but are removed from every automatic recovery predicate.
UPDATE freeze_idempotency f
SET settlement_pending_reason = 'ambiguous_attempt',
    provider_cost_pending_at = COALESCE(provider_cost_pending_at, NOW()),
    settlement_claimed_until = NULL,
    settlement_next_attempt_at = NULL,
    updated_at = NOW()
WHERE f.status = 'settlement_pending'
  AND f.delivery_attempt_id IS NULL
  AND f.settlement_pending_reason IN ('billable_output', 'delivery_prepared', 'delivery_completed', 'delivery_failed')
  AND EXISTS (
      SELECT 1 FROM delivery_attempt_quarantine q
      WHERE q.org_id = f.org_id AND q.request_id = f.request_id
  );

-- The append-only evidence is preserved above before ambiguous rows leave the
-- charge-authorizing table. This trigger is re-enabled in the same transaction.
DELETE FROM request_delivery_receipts WHERE attempt_id IS NULL;
ALTER TABLE request_delivery_receipts
    ENABLE TRIGGER request_delivery_receipt_append_only;

ALTER TABLE freeze_idempotency
    ADD CONSTRAINT freeze_idempotency_delivery_attempt_nonempty CHECK (
        delivery_attempt_id IS NULL OR length(btrim(delivery_attempt_id)) > 0
    ),
    ADD CONSTRAINT freeze_idempotency_delivery_attempt_unique
        UNIQUE (org_id, request_id, delivery_attempt_id),
    ADD CONSTRAINT freeze_idempotency_delivery_attempt_provider_fk
        FOREIGN KEY (org_id, request_id, delivery_attempt_id)
        REFERENCES billing_provider_attempts(org_id, request_id, attempt_id)
        ON DELETE RESTRICT;

CREATE INDEX idx_freeze_idempotency_delivery_attempt
    ON freeze_idempotency(org_id, request_id, delivery_attempt_id)
    WHERE delivery_attempt_id IS NOT NULL;

CREATE FUNCTION enforce_freeze_delivery_attempt_immutable()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.delivery_attempt_id IS NOT NULL
       AND NEW.delivery_attempt_id IS DISTINCT FROM OLD.delivery_attempt_id THEN
        RAISE EXCEPTION 'delivery attempt binding is immutable';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER freeze_delivery_attempt_immutable
BEFORE UPDATE OF delivery_attempt_id ON freeze_idempotency
FOR EACH ROW EXECUTE FUNCTION enforce_freeze_delivery_attempt_immutable();

ALTER TABLE request_delivery_receipts
    ALTER COLUMN attempt_id SET NOT NULL,
    ADD CONSTRAINT request_delivery_receipts_attempt_nonempty
        CHECK (length(btrim(attempt_id)) > 0),
    ADD CONSTRAINT request_delivery_receipts_attempt_provider_fk
        FOREIGN KEY (org_id, request_id, attempt_id)
        REFERENCES billing_provider_attempts(org_id, request_id, attempt_id)
        ON DELETE RESTRICT,
    ADD CONSTRAINT request_delivery_receipts_attempt_binding_fk
        FOREIGN KEY (org_id, request_id, attempt_id)
        REFERENCES freeze_idempotency(org_id, request_id, delivery_attempt_id)
        ON DELETE RESTRICT;

CREATE INDEX idx_request_delivery_receipts_attempt_latest
    ON request_delivery_receipts(org_id, request_id, attempt_id, sequence DESC);

CREATE OR REPLACE FUNCTION enforce_request_delivery_receipt_transition()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    previous request_delivery_receipts%ROWTYPE;
BEGIN
    IF length(btrim(NEW.attempt_id)) = 0 THEN
        RAISE EXCEPTION 'delivery receipt attempt identity is required';
    END IF;

    SELECT * INTO previous
    FROM request_delivery_receipts
    WHERE org_id = NEW.org_id AND request_id = NEW.request_id
    ORDER BY sequence DESC
    LIMIT 1;

    IF NOT FOUND THEN
        IF NEW.sequence <> 1 OR NEW.outcome <> 'write_started'
           OR NEW.delivered_bytes <> 0 OR NEW.delivered_chunks <> 0 THEN
            RAISE EXCEPTION 'delivery receipt must begin with zero-byte write_started';
        END IF;
        RETURN NEW;
    END IF;

    IF NEW.attempt_id <> previous.attempt_id THEN
        RAISE EXCEPTION 'delivery receipt attempt identity is immutable';
    END IF;
    IF previous.outcome IN ('completed', 'write_failed', 'interrupted') THEN
        RAISE EXCEPTION 'delivery receipt is already terminal';
    END IF;
    IF NEW.sequence <> previous.sequence + 1 OR NEW.mode <> previous.mode THEN
        RAISE EXCEPTION 'delivery receipt sequence or mode is not monotonic';
    END IF;
    IF NEW.delivered_bytes < previous.delivered_bytes
       OR NEW.delivered_chunks < previous.delivered_chunks THEN
        RAISE EXCEPTION 'delivery receipt counters cannot decrease';
    END IF;
    IF previous.outcome = 'write_started'
       AND NEW.outcome NOT IN ('partial', 'completed', 'write_failed', 'interrupted') THEN
        RAISE EXCEPTION 'invalid delivery transition after write_started';
    END IF;
    IF previous.outcome = 'partial'
       AND NEW.outcome NOT IN ('completed', 'interrupted') THEN
        RAISE EXCEPTION 'invalid delivery transition after partial';
    END IF;
    RETURN NEW;
END;
$$;

COMMIT;
