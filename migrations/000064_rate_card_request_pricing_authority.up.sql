BEGIN;

-- Optional UTC daily schedules are stored with each immutable Rate Card
-- version. An empty array preserves historical flat-price cards. Application
-- validation requires any non-empty schedule to cover 00:00-24:00 exactly
-- once with explicit component prices in every band.
ALTER TABLE enterprise_finance_rate_card_versions
    ADD COLUMN daily_price_bands JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD CONSTRAINT enterprise_finance_rate_card_daily_price_bands_array_check
        CHECK (jsonb_typeof(daily_price_bands) = 'array');

COMMENT ON COLUMN enterprise_finance_rate_card_versions.daily_price_bands IS
    'Optional complete non-overlapping UTC minute schedule; empty means the legacy-compatible flat component set.';

-- A Rate Card source import is evidence, not a network synchronization job.
-- Importers must provide the exact retrieved revision and payload digest; every
-- import remains a draft until the existing maker-checker flow approves and
-- activates it. Existing active cards are never retired by an import failure.
CREATE TABLE enterprise_finance_rate_source_evidence (
    id BIGSERIAL PRIMARY KEY,
    org_id BIGINT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    evidence_ref VARCHAR(160) NOT NULL,
    source_kind VARCHAR(40) NOT NULL,
    source_url TEXT NOT NULL,
    source_revision VARCHAR(255) NOT NULL,
    fetched_at TIMESTAMPTZ(6) NOT NULL,
    payload_sha256 VARCHAR(71) NOT NULL,
    transform_version VARCHAR(160) NOT NULL,
    currency CHAR(3) NOT NULL,
    unit_contract VARCHAR(80) NOT NULL,
    last_success_at TIMESTAMPTZ(6) NOT NULL,
    stale_after TIMESTAMPTZ(6) NOT NULL,
    imported_by VARCHAR(160) NOT NULL,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT enterprise_finance_rate_source_evidence_unique UNIQUE (org_id, evidence_ref),
    CONSTRAINT enterprise_finance_rate_source_identity_check CHECK (
        evidence_ref <> '' AND source_url <> '' AND source_revision <> ''
        AND transform_version <> '' AND imported_by <> ''
    ),
    CONSTRAINT enterprise_finance_rate_source_kind_check CHECK (
        source_kind IN ('official_import', 'legacy_model_prices', 'supplier_contract')
    ),
    CONSTRAINT enterprise_finance_rate_source_sha_check
        CHECK (payload_sha256 ~ '^sha256:[a-f0-9]{64}$'),
    CONSTRAINT enterprise_finance_rate_source_currency_check CHECK (currency = 'CNY'),
    CONSTRAINT enterprise_finance_rate_source_unit_check CHECK (unit_contract = 'tokens'),
    CONSTRAINT enterprise_finance_rate_source_freshness_check CHECK (
        last_success_at >= fetched_at AND stale_after > last_success_at
    )
);

CREATE INDEX enterprise_finance_rate_source_freshness_idx
    ON enterprise_finance_rate_source_evidence (org_id, stale_after, last_success_at DESC);

COMMENT ON TABLE enterprise_finance_rate_source_evidence IS
    'Evidence supplied to controlled Rate Card draft imports; never credentials and never automatic activation authority.';
COMMENT ON COLUMN enterprise_finance_rate_source_evidence.payload_sha256 IS
    'SHA-256 of the exact upstream price payload before deterministic transformation.';
COMMENT ON COLUMN enterprise_finance_rate_source_evidence.stale_after IS
    'Bounded-freshness visibility only. Staleness does not mutate the current active Rate Card.';

-- model_prices is intentionally not copied or activated in SQL: historical
-- rows have no maker/checker identities and provider may be blank. Operators
-- use the authenticated bootstrap-legacy-drafts endpoint, which records exact
-- provenance and creates reviewable provider_list drafts only. The explicit
-- legacy upgrade mode remains authoritative until operators activate complete
-- cards, pass per-model preflight, and select strict rate_card mode; strict mode
-- then fails closed without reading legacy price tables.

COMMIT;
