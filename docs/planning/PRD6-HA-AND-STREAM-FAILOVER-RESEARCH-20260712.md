# PRD6 High Availability And Stream Failover Research

> Date: 2026-07-12
> Owner: Codex
> Status: Architecture reference; D6-12 accepted, no deployment authorization
> Evidence class: repository facts plus official competitor documentation; no target or production verification

## 1. Outcome

UniRouter already has application-level primitives for running multiple replicas
against shared PostgreSQL and Redis state. The repository deployment profile is
still a single-host stable/canary topology, not a machine-level high-availability
cluster.

Two gateway machines improve availability for new connections after a process or
host failure. They do not transfer an established TCP/SSE connection or its
in-process parser, token counter and upstream cursor to another process. A
complete availability design must therefore separate:

1. request-level availability: healthy replicas, load balancing and highly
   available dependencies; from
2. in-flight stream semantics: explicit interruption, financial ambiguity,
   client retry and any separately designed checkpoint/resume protocol.

This distinction is the accepted D6-12 boundary. P6-F20 owns durable recovery
after final usage is persisted. P6-F11 owns mid-stream interruption, bounded
checkpoints and client recovery semantics. Managed HA/IaC and customer SLA
operation remain future PRD7/target-evidence work.

## 2. Method And Labels

The repository was inspected at base
`bd1692ef7d4f546fde2dbd0d5a35fe2bc92cb148`. The relevant deployment files
matched `origin/main` in this pass. Competitor sources below are official product
documentation or official repositories accessed on 2026-07-12.

- **Confirmed finding:** directly supported by current source, configuration,
  evidence or an official competitor source.
- **Inference:** a conclusion from documented protocol behavior; not a vendor
  product promise.
- **Recommendation:** a UniRouter product or architecture choice, not current
  implementation evidence.

## 3. Competitor Patterns

| Product | Confirmed official design | Stream/failure boundary |
|---------|---------------------------|-------------------------|
| LiteLLM | Regional load balancers and proxy pods share one PostgreSQL source of truth; regional Redis coordinates rate limits, router state and cache. The topology supports active-active or active-passive operation, and migrations run once rather than on every replica. | Official reliability material covers provider retries/fallbacks. It does not promise migration of an established client stream after the proxy process or host dies. |
| New API | The official cluster guide uses at least two application nodes, shared MySQL, shared Redis, identical session/encryption secrets and Nginx/HAProxy/cloud load balancing. It recommends HA MySQL and Redis Sentinel/Cluster. | The documented load balancer changes the node selected for a request. It does not define a mid-body SSE takeover or exactly-once resume protocol. |
| Portkey | The EKS guide recommends at least two worker nodes across availability zones, ALB/NLB health checks and Redis/Valkey in the VPC. Gateway policies add retries, fallbacks, circuit breakers and provider load balancing. | Portkey explicitly says a failed provider stream can fail over and **restart** the stream. Restart is not a claim of exact continuation from the last delivered token and does not cover failure of the Portkey gateway host itself. |
| Kong AI Gateway | Gateway nodes run as a cluster behind an external load balancer and share configuration/database state. Active and passive health checks skip unhealthy upstream targets. | The standard cluster and health-check design sends new work to healthy nodes. It does not transfer an existing TCP/SSE connection between gateway processes. |

Official sources:

- LiteLLM:
  [multi-region deployment](https://docs.litellm.ai/docs/proxy/multi_region),
  [Docker/Helm/Kubernetes deployment](https://docs.litellm.ai/docs/proxy/deploy),
  [official Helm values](https://github.com/BerriAI/litellm/blob/main/helm/litellm/values.yaml).
- New API:
  [cluster deployment](https://docs.newapi.pro/zh/docs/installation/deployment-methods/cluster-deployment),
  [environment variables](https://docs.newapi.pro/zh/docs/installation/config-maintenance/environment-variables),
  [official Compose reference](https://github.com/QuantumNous/new-api/blob/main/docker-compose.yml).
- Portkey:
  [EKS high-availability deployment](https://portkey.ai/docs/self-hosting/hybrid-deployments/aws/eks),
  [AI Gateway capabilities](https://portkey.ai/docs/product/ai-gateway),
  [provider-stream restart statement](https://portkey.ai/docs/guides/use-cases/enterprise-ready-unified-api).
- Kong:
  [traditional cluster mode](https://developer.konghq.com/gateway/traditional-mode/),
  [health checks and circuit breakers](https://developer.konghq.com/gateway/traffic-control/health-checks-circuit-breakers/).
- Protocol and dependency references:
  [NGINX proxy retry boundary](https://nginx.org/en/docs/http/ngx_http_proxy_module.html#proxy_next_upstream),
  [Redis Sentinel high availability](https://redis.io/docs/latest/operate/oss_and_stack/management/sentinel/).

## 4. Industry Boundary: Three Different Failovers

### 4.1 Provider failover

The gateway remains alive but an upstream model/key/provider fails. UniRouter can
retry or select another route while it still owns the client connection and has
not crossed an unsafe response boundary. P6-F03, P6-F04 and P6-F08 cover the
current request-level routing behavior.

### 4.2 Gateway replica failover

The gateway process or host dies. A load balancer can remove the unhealthy
replica and send later connections to another replica. It cannot move the
existing kernel socket, upstream response cursor or in-process stream state.

NGINX documents that moving a request to the next upstream is possible only
before anything has been sent to the client. It also avoids retrying a
non-idempotent POST after the request was sent unless that behavior is explicitly
enabled. Therefore blind automatic replay after partial LLM output risks duplicate
generation, tool effects or billing.

### 4.3 Resumable application protocol

Continuing one logical stream after a connection dies is a separate product
protocol. It requires at least:

- a stable operation identity and per-event sequence;
- durable server checkpoints or a replay source;
- client acknowledgement or `Last-Event-ID`-like recovery semantics;
- an explicit duplicate-output and billing policy; and
- upstream provider support or a defined restart-from-prompt fallback.

Even server checkpoints alone cannot prove exactly which final bytes reached the
client at the instant of a network or process failure. An exact-delivery claim
requires client acknowledgement and remains dependent on provider semantics.

## 5. Current UniRouter Facts

### 5.1 Multi-replica-ready application primitives

- `docs/tech-ref/15-ha-readiness.md:8-30` permits multiple application instances
  against shared PostgreSQL/Redis and assigns canonical financial, routing,
  evidence, rate and workload state to those stores.
- `internal/billing/settlement_recovery.go:45-48,244-318` uses leased PostgreSQL
  claims and is safe to run in every replica.
- P6-F07 and P6-F21 use shared Redis-time policy/workload leases rather than
  multiplying capacity by replica count.
- P6-F08 stores routing/circuit/key health and half-open probe ownership in
  PostgreSQL.
- P6-F17 stores operation/idempotency identity in PostgreSQL, but deliberately
  does not persist model responses for replay; a completed duplicate currently
  returns a conflict receipt.
- P6-F20 adds a PostgreSQL leased outbox and durable runtime terminal hooks. PR
  #177 and its post-merge `main` run are CI verified / closed; this remains no
  target or production claim.

### 5.2 Current deployment is not machine-level HA

- `docker-compose.yml:4-32` defines one PostgreSQL and one Redis service;
  `docker-compose.yml:34-130` defines one backend and one frontend with local
  named volumes.
- `docker-compose.canary.yml:7-24,37-119` adds one Nginx and a second canary
  backend to the same Compose topology.
- `nginx/nginx.http-canary.conf:5-62` implements stable/canary weighting, while
  health and metrics are fixed to stable.
- `docs/ops/evidence/PRD5-B1Z-target-deployment-and-infra-security-20260710.md:7,70-80`
  records one shared development/test ECS and runtime readback
  `multi-instance state=not_configured`.
- `docs/issues/OPEN-ISSUES.md:81` keeps production HA/DR, PostgreSQL HA and
  application reconnect/failover evidence open.
- `internal/redisx/redis.go:16-39` currently constructs a single-address Redis
  client rather than a Sentinel/Cluster client.
- `/health/ready` requires both PostgreSQL and Redis, so one shared dependency
  failure removes every current replica from readiness.

### 5.3 Current stream crash behavior

- `docs/tech-ref/15-ha-readiness.md:14-20` assigns in-flight streaming state to
  the current application process.
- `internal/gateway/relay.go` keeps the upstream body, SSE scanner, stream state,
  token counter and downstream writer in that process.
- P6-F18 persists `settlement_pending` before the first billable content leaves
  the gateway and persists immutable final usage at normal stream finalization.
- If the process dies after the marker but before final usage, recovery does not
  guess a charge or refund. The reservation remains conservatively held because
  exact delivered usage is unknown.
- The P6-F20 outbox recovers work only after final usage or another terminal
  financial fact is durable. It does not change the mid-stream network/database
  boundary.

## 6. Four Quadrants

### Known Knowns

- Current Compose and target evidence are single-host, not machine-level HA.
- Stateless application replicas plus shared authoritative stores are compatible
  with the current code direction.
- Load balancing protects new connections, not an established SSE connection.
- PostgreSQL, Redis and the edge must each avoid a single point of failure.

### Known Unknowns

- The private-deployment RTO/RPO and customer SLA have not been selected.
- Customer environments may prefer managed services, self-hosted quorum or no
  automatic data-layer failover.
- Exact replica count, connection-pool budget and regional topology require load
  evidence rather than a guessed threshold.
- Product demand for resumable generation versus explicit restart is unverified.

### Unknown Knowns

- Existing leased settlement, routing probe, distributed policy and weighted
  workload primitives already reduce the code work needed for an HA monolith.
- Each replica adds PostgreSQL/Redis connections and worker capacity; scaling
  replicas without connection budgets can move the bottleneck to shared stores.
- The current 30-minute application stream limit, five-minute Nginx proxy timeout
  and two-minute shutdown window are different contracts and must be reconciled
  before an uninterrupted-upgrade claim.

### Unknown Unknowns

- Correlated host, network and control-plane failures may violate independence
  assumptions.
- A two-node self-hosted data layer can split brain when each side cannot tell a
  dead peer from a network partition.
- Provider-side retries may duplicate generation or side effects when an
  operation lacks upstream idempotency/resume support.
- Long video/multimodal jobs will create recovery and storage demands beyond an
  HTTP streaming gateway.

## 7. Recommended Availability Architecture

### Tier 0 — Current evidence

One Compose host with stable/canary backends and shared local PostgreSQL/Redis.
This is useful for release validation, not a host-failure SLA.

### Tier 1 — Recommended private-deployment HA monolith

1. Run at least two active-active UniRouter instances in independent failure
   domains behind a managed L4/L7 load balancer.
2. Remove failed instances through readiness, drain new traffic before upgrades,
   and never retry a partially emitted SSE request blindly.
3. Use managed multi-AZ PostgreSQL with one stable failover endpoint, backups and
   point-in-time recovery.
4. Use managed Redis/Valkey Multi-AZ or a supported Sentinel topology. Redis
   recommends at least three independent Sentinel instances for robust quorum.
5. Make schema migration a single explicit job rather than an application-replica
   race.
6. Budget PostgreSQL and Redis connections across all replicas and collect
   metrics from every instance.
7. Keep the monolith: high availability does not require a premature microservice
   split.

If only two physical/virtual machines are available, application active-active is
possible, but reliable self-hosted data-layer election needs an independent third
witness/quorum participant or a managed service. Two application VMs on one
physical host do not provide physical-host availability.

### Tier 2 — Future SaaS/regional-cell profile

Use multiple availability zones, managed state, independent regional cells,
explicit blast-radius controls and measured failover objectives. Add a resumable
stream protocol only if product evidence justifies the storage, client SDK and
provider constraints. This remains PRD7 architecture, not an accepted PRD6
runtime implementation.

## 8. D6-12 Accepted Boundary

The User accepted on 2026-07-12:

1. P6-F20 closes when final prepared/settled/cancelled usage and terminal work
   survive process replacement through the bounded durable outbox and required
   PR/CI gates.
2. P6-F20 does not claim exact usage when a gateway dies mid-stream before final
   usage persistence.
3. P6-F11 owns the bounded mid-stream checkpoint, explicit interruption,
   client-retry and any future resume semantics.
4. PRD6 does not promise transparent stream migration after gateway/host death.
   It must avoid guessed usage, unsafe replay and double charge while retaining a
   clear auditable pending/interrupted state.
5. Managed HA/IaC, customer SLA operation and target HA/DR proof remain deferred
   to PRD7/target evidence as previously decided.

## 9. Future Acceptance Evidence

No deployment should be called machine-level HA until an authorized environment
passes at least:

1. kill one application process and one host; prove new-request recovery time,
   capacity and no duplicate financial terminal;
2. fail over PostgreSQL; prove reconnect, one financial truth and migration
   ownership;
3. fail over Redis; prove bounded denial/recovery without replica-multiplied
   admission;
4. interrupt a live SSE stream; prove explicit client terminal behavior and the
   accepted conservative financial state;
5. roll one version while draining long streams; prove the published timeout and
   compatibility contract; and
6. restore from backup/PITR in an isolated environment and read back application
   behavior, not only database objects.

These are future target-evidence gates. This research authorizes no ECS, network,
database, credential, provider-request or production change.
