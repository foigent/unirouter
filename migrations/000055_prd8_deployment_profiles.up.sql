BEGIN;

CREATE TABLE deployment_profiles (
    id BIGSERIAL PRIMARY KEY,
    contract_version VARCHAR(64) NOT NULL DEFAULT 'unirouter.deployment-profile.v1',
    profile_ref VARCHAR(64) NOT NULL UNIQUE,
    display_name VARCHAR(160) NOT NULL,
    kind VARCHAR(32) NOT NULL,
    environment VARCHAR(32) NOT NULL,
    ingress_mode VARCHAR(40) NOT NULL,
    dns_mode VARCHAR(32) NOT NULL,
    outbound_mode VARCHAR(24) NOT NULL,
    notes TEXT NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    desired_revision BIGINT NOT NULL DEFAULT 1,
    created_by BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    updated_by BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT deployment_profiles_contract_check CHECK (contract_version = 'unirouter.deployment-profile.v1'),
    CONSTRAINT deployment_profiles_identity_check CHECK (
        profile_ref ~ '^dep_[a-z0-9][a-z0-9._-]{2,59}$'
        AND length(btrim(display_name)) BETWEEN 1 AND 160
        AND environment ~ '^[a-z][a-z0-9_-]{0,31}$'
        AND length(notes) <= 2000
        AND desired_revision > 0
    ),
    CONSTRAINT deployment_profiles_kind_check CHECK (kind IN ('ecs_development', 'onprem_compose', 'onprem_kubernetes')),
    CONSTRAINT deployment_profiles_ingress_check CHECK (ingress_mode IN ('external_load_balancer', 'bundled_reverse_proxy', 'cluster_ingress')),
    CONSTRAINT deployment_profiles_dns_check CHECK (dns_mode IN ('public_dns', 'private_dns', 'ip_address', 'customer_managed')),
    CONSTRAINT deployment_profiles_outbound_check CHECK (outbound_mode IN ('direct', 'proxy', 'restricted', 'air_gapped')),
    CONSTRAINT deployment_profiles_status_check CHECK (status IN ('active', 'disabled'))
);

CREATE TABLE gateway_endpoints (
    id BIGSERIAL PRIMARY KEY,
    deployment_profile_id BIGINT NOT NULL REFERENCES deployment_profiles(id) ON DELETE RESTRICT,
    endpoint_ref VARCHAR(64) NOT NULL UNIQUE,
    display_name VARCHAR(160) NOT NULL,
    base_url VARCHAR(512) NOT NULL,
    visibility VARCHAR(16) NOT NULL,
    tls_mode VARCHAR(32) NOT NULL,
    certificate_ref VARCHAR(240) NOT NULL DEFAULT '',
    endpoint_profile VARCHAR(120) NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    desired_revision BIGINT NOT NULL DEFAULT 1,
    observed_status VARCHAR(24) NOT NULL DEFAULT 'unverified',
    observed_base_url VARCHAR(512) NOT NULL DEFAULT '',
    observed_evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    observed_at TIMESTAMPTZ(6),
    created_by BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    updated_by BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT NOW(),
    CONSTRAINT gateway_endpoints_identity_check CHECK (
        endpoint_ref ~ '^gwe_[a-z0-9][a-z0-9._-]{2,59}$'
        AND length(btrim(display_name)) BETWEEN 1 AND 160
        AND length(base_url) BETWEEN 8 AND 512
        AND length(certificate_ref) <= 240
        AND desired_revision > 0
    ),
    CONSTRAINT gateway_endpoints_visibility_check CHECK (visibility IN ('public', 'internal')),
    CONSTRAINT gateway_endpoints_tls_check CHECK (tls_mode IN ('external_termination', 'mounted_certificate', 'private_ca', 'disabled')),
    CONSTRAINT gateway_endpoints_transport_check CHECK (
        (visibility <> 'public' OR base_url LIKE 'https://%')
        AND (
            (tls_mode = 'disabled' AND base_url LIKE 'http://%')
            OR (tls_mode <> 'disabled' AND base_url LIKE 'https://%')
        )
        AND position('-----BEGIN' IN certificate_ref) = 0
    ),
    CONSTRAINT gateway_endpoints_profile_check CHECK (endpoint_profile IN ('endpoint.openai-chat.v1', 'endpoint.openai-responses.contract.v1')),
    CONSTRAINT gateway_endpoints_status_check CHECK (status IN ('active', 'disabled')),
    CONSTRAINT gateway_endpoints_observed_check CHECK (observed_status IN ('unverified', 'ready', 'degraded', 'unreachable'))
);

CREATE INDEX deployment_profiles_list_idx ON deployment_profiles (status, updated_at DESC, id DESC);
CREATE INDEX gateway_endpoints_profile_idx ON gateway_endpoints (deployment_profile_id, status, updated_at DESC, id DESC);
CREATE UNIQUE INDEX gateway_endpoints_active_base_url_idx ON gateway_endpoints (base_url, endpoint_profile) WHERE status = 'active';

COMMIT;
