# Customer Runtime Template

This directory is an input to the signed customer release, not a source-build
deployment. The shipped copy must be declared by path, size and SHA-256 in the
signed upgrade manifest together with prebuilt application and dependency
image artifacts (or explicitly approved preloaded dependency digests),
monitoring configuration, release notes and SBOM/provenance files.
Every runtime image variable is mandatory and must use the exact digest identity
approved in the release note; this template has no mutable image-tag fallback.

The Compose file contains no `build:` entry, source mount, builder cache or
host migration mount. The backend image owns its exact migrations, immutable
build version/commit and vendor License/upgrade public trust roots. Runtime
environment variables cannot replace those embedded roots. A stable deployment
identity remains mandatory configuration.

Before first start:

1. verify the complete signed release with the separately bootstrapped
   `uniroute-upgrade` verifier and vendor-published public-key ring;
2. copy `.env.example` to `.env`, replace every placeholder, keep the file out
   of tickets/evidence, and retain the exact signed image references;
3. include the signed `config/` tree beside this Compose file;
4. load or pull only the digest-pinned image identities declared by the signed
   release (including PostgreSQL, Redis, Prometheus and Grafana);
5. run `docker compose config` and confirm no `build:` or source volume exists;
6. start the stack, rotate the seeded administrator through
   `docker compose exec backend ./uniroute admin set-password ...`, then import
   the customer-bound V1 License through the local platform-admin recovery
   surface.

The default HTTP bind is `127.0.0.1:3000`; expose it through an approved TLS
reverse proxy or explicitly change `UNIROUTER_LISTEN_ADDRESS`. PostgreSQL,
Redis, Prometheus and Grafana have no host port in this template.
