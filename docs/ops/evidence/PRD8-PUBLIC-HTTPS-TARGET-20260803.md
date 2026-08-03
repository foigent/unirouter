# PRD8 Public HTTPS Target Evidence

> Work item: `UR-PRD8-PUBLIC-HTTPS-POC`
> Evidence date: 2026-08-03 (Asia/Shanghai)
> Product repository baseline commit/tree:
> `26d79ecd01f54cf421cfaa490ee5c677db5bc8b6` /
> `788d492e8344db479a8bcfc60b57cc4602326e8a`
> Deployed code commit/tree:
> `f81d8431aed9fea1f1ff63467f2c4df6cecfd2f5` /
> `29ef34eb96557f3c0f2c16fc59f13fe57ab3fed9`
> Status: `DEVELOPMENT_TARGET_HTTPS_VERIFIED`; second-integration data reset,
> Provider traffic and customer Production remain separate

## Outcome

The existing UniRouter development ECS now has a publicly trusted HTTPS
entrypoint without changing the deployed application images or business data:

- console: `https://8.130.18.85/`;
- OpenAI-compatible Base URL: `https://8.130.18.85/v1`;
- HTTP port 80 retains the ACME challenge path and otherwise returns a `308`
  redirect to the fixed HTTPS IP origin;
- Nginx terminates TLS and preserves the existing 90/10 stable/canary route;
- certificate, private key and renewal state remain host-owned under
  `/opt/unirouter/tls`; private-key material did not enter Git, the UI or the
  UniRouter database; and
- future deployments on this development target must include the persistent
  third Compose layer `/opt/unirouter/tls/docker-compose.https.yml`. Running
  only the source base/canary pair would intentionally restore their HTTP-only
  default and is not an HTTPS-preserving deployment.

This is a development Target result. It is not a customer DNS, customer CA,
Production, HA, load or second-device Agent-traffic claim.

## Hostname Decision And Certificate

`8-130-18-85.sslip.io` resolved correctly and the ECS Nginx returned the ACME
token with HTTP 200. Let’s Encrypt nevertheless received a separate HTTP 403;
an independent external request reproduced `Server: Beaver` and the Alibaba
Cloud `Non-compliance ICP Filing` page. The failure was therefore before the
application response for at least one external validation path, not missing
UniRouter challenge content.

The bounded development solution uses Let’s Encrypt's generally available IP
certificate flow instead of pretending that the unfiled shared hostname is
usable. Official guidance requires the `shortlived` profile for IP certificates
and Certbot 5.4 or later for IP webroot validation:

- <https://letsencrypt.org/2026/03/11/shorter-certs-certbot>
- <https://letsencrypt.org/2026/01/15/6day-and-ip-general-availability.html>

Exact issued certificate identity:

| Fact | Value |
|---|---|
| SAN | critical `IP Address:8.130.18.85` |
| Issuer | `C=US, O=Let's Encrypt, CN=YE2` |
| Validity | `2026-08-02T23:10:18Z` through `2026-08-09T15:10:17Z` |
| SHA-256 fingerprint | `3D:3E:D7:E5:6D:D9:FE:19:23:62:DA:40:56:B3:F5:10:75:D6:50:5D:89:48:F5:93:73:0D:5D:30:28:39:EC:88` |
| Full-chain SHA-256 | `0af6f7db0034750702905df3c840e88232af3651baf5c680951b0ba0f1d8dfb8` |
| Private-key mode | `0600 root:root` |

Certbot is pinned to
`certbot/certbot@sha256:0107d084c225631fc64a8313e19adb07275f7296fde338f7dfa93986c80b2e3e`
(`v5.6.0`, 91,844,480 bytes). No mutable `latest` tag is used by issuance or
renewal.

## Target Configuration Identities

The TLS files are target-owned and intentionally outside the immutable
commit-addressed application release:

| File | SHA-256 |
|---|---|
| `/opt/unirouter/tls/nginx.bootstrap.conf` | `9c3e9de602a910a27b0495e1cd5dba84d9089724ab8b30ff08461b24e7d2bdde` |
| `/opt/unirouter/tls/nginx.https.conf` | `c9c89807e8f2f20f05b6b752b20b2efd840b08e0f51610e612475fc87538e2b3` |
| `/opt/unirouter/tls/docker-compose.bootstrap.yml` | `565b20c3f03e3b18d94d323d1854ceacb9c45ea9218d32be253bf47e0a096b90` |
| `/opt/unirouter/tls/docker-compose.https.yml` | `437f5f5195dd5daacd3ab0c94de5f56a8943c127b0eeade5b3eb3f7db1f982eb` |
| `/usr/local/sbin/unirouter-certbot-renew` | `9cfd8380c69dd70f71113f7afaebdddd4dd47921a5283a01b9850da55ec4374e` |
| `/etc/systemd/system/unirouter-certbot-renew.service` | `41fad0d490972c01ad9b5dc7945136d8cbd0c585f18c0f2de8e2f01b3c28dc00` |
| `/etc/systemd/system/unirouter-certbot-renew.timer` | `74da0484739fa5b4c8cca65eab38538f1b8283aeef9777b4c274be79dd60b30b` |

The rendered final Compose service contains only the existing Nginx ports,
network/IP and four read-only mounts: final Nginx config, ACME webroot, the
certificate `live` directory and its `archive` directory. Both bootstrap and
final configurations passed `nginx -t` in an isolated Nginx container before
cutover. The running Nginx then passed `nginx -t` again.

The security-group change reused the existing exact rule
`sgr-0jldud2715dl4ygo62rc`:

| Field | Before | Final |
|---|---|---|
| port/protocol | `TCP 443/443` | unchanged |
| source | `172.27.13.1/32` | `0.0.0.0/0` |
| policy/priority | `Accept / 10` | `Accept / 10` |
| description | `Public HTTPS entrypoint` | unchanged |

The first modify call omitted `Priority`; the cloud API applied its default
`1`. The operator detected that drift in the immediate readback and issued a
second fully specified modification restoring priority `10`. The final rule
above is the verified state. The public TCP 80 rule remains for HTTP-01 and all
other public TCP remains covered by the existing priority-100 drop rule.

## Protocol, Browser And Isolation Verification

The following checks passed after the final cutover:

| Check | Result |
|---|---|
| public HTTP `/` | `308`, fixed `Location: https://8.130.18.85/` |
| public HTTP ACME preflight path | `200` before removal of the temporary probe file |
| public HTTPS `/` | HTTP/2 `200` |
| public HTTPS `/health/ready` | `200`, `{"status":"ok","db":"ok","redis":"ok"}` |
| public HTTPS `/v1/models` without Authorization | `401`, `auth_error.invalid_api_key` and zero Provider dispatch |
| listener certificate | exact SAN, validity and fingerprint listed above |
| security headers | one authoritative set: CSP from the frontend plus edge `DENY`, `nosniff`, `no-referrer`, restricted permissions and one-day HSTS |
| external blocked ports | `3000`, `3001`, `5432`, `6379`, `8080`, `8081`, `9090` all unreachable |

Chrome independently loaded the public HTTPS login page with the exact title
`UniRouter - 统一 AI 路由网关`. The login form was present, document state was
`complete`, document width equalled viewport width at 2,041 CSS pixels, and the
captured browser console contained zero errors and zero warnings. No login,
credential read, Provider test or business mutation occurred during this
browser check.

## Renewal Evidence

The Certbot renewal configuration records `preferred_profile = shortlived`,
`authenticator = webroot`, the production ACME directory and the exact IP to
webroot mapping. A staging `certbot renew --dry-run` completed successfully.

`unirouter-certbot-renew.timer` is enabled and active, runs twice daily with a
bounded systemd randomized delay, and had its next trigger at
`2026-08-03T16:00:44+08:00` when recorded. A real not-yet-due service run exited
with `Result=success` and status `0`; the certificate SHA and Nginx container
identity/start time/restart count were identical before and after. When the
certificate bytes change, the host script runs `nginx -t` before sending a
reload. It does not mount the Docker socket into the Certbot container.

## Zero-Drift Readback

Only Nginx was recreated. Stable API, canary API, frontend, PostgreSQL and
Redis retained their pre-cutover container IDs/start times and restart count
zero. Their image identities remain:

- frontend `sha256:45a53f4061ded313331555f26696e468c2b7ab2ee4d930c2c3894f66e257c378`;
- stable API `sha256:effbbe22d08beee93a5a0060f90b907edf4ecd9171a3fa5b47061b75ba862c94`;
- canary API `sha256:9db4818ec4e5efeb88d0a2d7be7f4facc989b6b891177586487d164f75745bbf`.

Pre- and post-cutover read-only database results were identical:

| Fact | Before and after |
|---|---:|
| migrations / maximum | `54 / 000055_prd8_deployment_profiles` |
| organizations / users / projects | `1 / 20 / 1` |
| API keys / channels / channel keys | `56 / 3 / 3` |
| channel lifecycle | `active=2 / disabled=1` |
| call logs / transactions | `60 / 286` |
| Feed items / max sequence / retained floor sum / acknowledged sum | `8 / 12 / 4 / 12` |
| workload applications / deployment profiles / Gateway Endpoints | `0 / 0 / 0` |
| Freeze terminal rows | `cancelled=9 / settled=51` |
| Provider attempts | `rejected=6 / settled=13` |
| pending/completed runtime terminal hooks | `0 / 19` |
| internal Outbox rows | `0` |

No organization, user, project, application, key, channel, Provider
credential, model release, authorization, log, transaction, Feed event,
Gateway Endpoint, Source Instance, Cursor or Checkpoint was created or changed.
The retained pre-release PostgreSQL backup remains
`/opt/unirouter/backups/pre-f81d8431-20260802T192126Z.dump`; no database backup
or restore was necessary for this Nginx/security-group-only change.

## Rollback And Remaining Boundaries

Rollback keeps application containers and data in place:

1. render and validate the base + canary +
   `/opt/unirouter/tls/docker-compose.bootstrap.yml` composition;
2. force-recreate only `nginx` with that bootstrap composition;
3. restore security-group rule `sgr-0jldud2715dl4ygo62rc` to exact source
   `172.27.13.1/32`, `TCP 443/443`, `Accept`, priority `10`; and
4. verify public HTTP health and the unchanged container/data readback.

The root filesystem is 85% used with approximately 5.7 GB available after the
91.8 MB pinned Certbot image. No image prune or bulk deletion ran. The existing
Compose `version` warning and known npm advisory boundary remain separate.
`ORG-3` still needs a real customer directory; `FIN-4` still needs a named
ERP/GL. The second-integration data reset, fresh channels/credentials and Agent
traffic remain blocked until the Product Owner explicitly starts that window.
