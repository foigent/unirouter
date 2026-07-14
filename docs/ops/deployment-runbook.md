# Deployment Runbook（部署运维手册）

> **版本**：v1.7
> **日期**：2026-07-06
> **用途**：运维人员部署和维护 UniRoute 的操作手册
> **变更记录**：v1.7 新增 PRD3 SEC-005 运行时暴露策略：CORS 精确 origin、登录限流、metrics 开关/CIDR；v1.6 新增 PRD2 OIDC-first SSO 配置和 mock-provider smoke；v1.5 新增 HTTP-only canary overlay、8081 canary direct smoke、PRD2 HA/backup rehearsal 命令；v1.4 新增生产 `.env` 保护、`API_KEY_HASH_SECRET`、rsync 安全同步规则和 PR #44 部署事故复盘；v1.3 新增 Prometheus + Grafana 监控栈、k6 压测脚本；v1.2 新增 Nginx 反向代理 + canary 测试部署章节；v1.1 修正日志描述（标准库 log，非 zap）、容器名更新（backend/frontend）

---

## 一、环境要求

| 组件 | 版本 | 说明 |
|------|------|------|
| Docker | 24+ | 容器运行时 |
| Docker Compose | 2.20+ | 编排工具 |
| 磁盘 | 50GB+ | PostgreSQL 数据 + 备份 |
| 内存 | 4GB+ | Go 二进制 + PG + Redis |
| CPU | 2 核+ | 建议 4 核 |

---

## 二、首次部署

### 2.1 准备配置文件

```bash
cd /opt/uniroute
cp .env.example .env

# 编辑 .env，必须修改以下项：
# - DB_DSN: PostgreSQL 连接串
# - JWT_SECRET: 32 字节随机字符串
# - API_KEY_HASH_SECRET: API Key HMAC 稳定密钥
# - ENCRYPTION_KEY: 32 字节随机字符串
# - LICENSE_PUBLIC_KEY: 离线 License Ed25519 公钥（导入 License 前必填）
# - GRAFANA_ADMIN_PASSWORD: Grafana 管理员强密码
# - ADMIN_CORS_ALLOWED_ORIGINS: 仅当浏览器会跨域直连 backend 时配置精确 origin
# - METRICS_ALLOWED_CIDRS: 生产建议限制到 Prometheus 所在网络
# - MIGRATIONS_DIR: 迁移文件目录（默认 /app/migrations）

# 生成随机密钥
openssl rand -hex 32  # 用于 JWT_SECRET
openssl rand -hex 32  # 用于 API_KEY_HASH_SECRET
openssl rand -hex 32  # 用于 ENCRYPTION_KEY
openssl rand -base64 24  # 用于 GRAFANA_ADMIN_PASSWORD
```

密钥职责：

| 变量 | 作用 | 丢失/变更影响 |
|------|------|---------------|
| `JWT_SECRET` | Admin JWT 签名 | 管理员需要重新登录；不应影响 API Key |
| `API_KEY_HASH_SECRET` | API Key HMAC 哈希 | 变更会导致既有 API Key 全部失效 |
| `ENCRYPTION_KEY` | 上游渠道密钥 AES-GCM 加密 | 变更且无法恢复会导致已保存 channel key 无法解密 |
| `LICENSE_PUBLIC_KEY` | 离线 License Ed25519 签名校验公钥 | 缺失时可启动服务，但无法导入或验证 License |

License 公钥由 License 签发方提供，支持 base64 或 hex 编码的 32 字节 Ed25519 public key。该值不是 secret，但 stable/canary 应保持一致，避免同一份 license 在不同实例上校验结果不同。

运行时暴露策略：

| 变量 | 默认 | 建议 |
|------|------|------|
| `SHUTDOWN_TIMEOUT_SECONDS` | `120` | graceful shutdown 等待时间；默认与 backend `WriteTimeout` 对齐，避免 30 秒内强制截断较长流式响应 |
| `ADMIN_CORS_ALLOWED_ORIGINS` | 空 | 为空表示只支持同源/服务端代理访问；需要 Vite 或跨域控制台直连 backend 时填精确 origin，如 `http://localhost:3000,http://localhost:5173`；不支持 `*` |
| `LOGIN_RATE_LIMIT_ENABLED` | `true` | 保持开启；按 backend `RemoteAddr` 与 `RemoteAddr+username` 计数 |
| `LOGIN_RATE_LIMIT_WINDOW_SECONDS` | `60` | 登录尝试固定窗口秒数 |
| `LOGIN_RATE_LIMIT_IP_REQUESTS` | `30` | 单个 `RemoteAddr` 每窗口最大登录尝试数；经过反向代理时应结合代理或网络层限流 |
| `LOGIN_RATE_LIMIT_USER_REQUESTS` | `10` | 单个 `RemoteAddr+username` 每窗口最大登录尝试数 |
| `METRICS_ENABLED` | `true` | 兼容 bundled Prometheus；不需要应用内指标时设为 `false` |
| `METRICS_ALLOWED_CIDRS` | 空 | 为空表示应用层不限制；生产建议填 Prometheus/内网 CIDR，公网隔离仍应依赖安全组、防火墙或反向代理 |
| `TRACE_EVENTS_ENABLED` | `true` | 记录 metadata-only 请求路径事件；高流量生产环境可设为 `false` 暂停写入 |
| `TRACE_EVENT_SAMPLE_RATE` | `1` | 按 `request_id` 确定性采样，范围 `0..1`；例如 `0.1` 约采样 10% 请求并保留同一请求的完整事件段 |
| `TRACE_EVENT_RETENTION_DAYS` | `LOG_RETENTION_DAYS` | `request_trace_events` 自动清理窗口；设为 `0` 时跳过 trace event 清理但不关闭写入，关闭写入仍使用 `TRACE_EVENTS_ENABLED=false` |
| `PAYLOAD_CAPTURE_MODE` | `metadata_only` | PRD5 payload evidence 状态；当前仅支持 `metadata_only` 和 `disabled`，不会启用 raw prompt/response 存储 |
| `CHAT_REQUEST_MAX_BYTES` | `8388608` | Chat 同步 JSON 请求默认 8 MiB；应用按实际读取字节执行，不信任 `Content-Length` |
| `EMBEDDINGS_REQUEST_MAX_BYTES` | `16777216` | Embeddings 批量同步请求默认 16 MiB；大媒体不应通过此端点传输 |
| `NONSTREAM_RESPONSE_MAX_BYTES` | `16777216` | 最终非流式客户响应默认 16 MiB（其中为 UniRouter 元数据预留 64 KiB）；超限返回 502、不 failover、不向客户收费，并保留可能的上游成本待处理状态 |
| `PAYLOAD_NODE_INFLIGHT_BYTES` | `268435456` | 单个 UniRouter 副本实际保留 payload 的总字节预算；需按堆内存与并发实测调整 |
| `PAYLOAD_ORG_INFLIGHT_BYTES` | `67108864` | 单组织在单副本的实际保留字节预算；必须小于等于 node budget |
| `EVIDENCE_CONTEXT_TRUST_SECRET` | 空 | PRD5 受信 evidence context 共享密钥；为空时忽略所有 inbound trace/session/actor/delegation headers。仅在受控内网/上游业务系统可持有密钥时启用，且不要写入 repo 或共享文档 |
| `LOG_RETENTION_DAYS` | `30` | call logs、settle/cancel transactions 和相关清理的保留窗口；audit logs 保留该值的 2 倍 |
| `RUNTIME_ENVIRONMENT` | `unspecified`（docker-compose 默认 `local`） | Runtime Status Center 展示的环境标签，如 `dev`、`staging`、`prod` |
| `VITE_DEPLOY_ENV` | 空 | 前端镜像构建时写入的环境标签；支持 `development`、`staging`、`canary`、`production` 等值，并应与 `RUNTIME_ENVIRONMENT` 保持一致。修改后必须重建 frontend 镜像，仅重启容器不会生效 |
| `RUNTIME_DEPLOYMENT_MODE` | `single-node` | Runtime Status Center 展示的部署形态，如 `docker-compose`、`kubernetes` |
| `RUNTIME_DEPLOYMENT_ROLE` | `stable` | 当前实例角色，如 `stable`、`canary` |
| `RUNTIME_VERSION` | 空 | 可选版本号；为空时使用 Docker build `-ldflags` 注入值 |
| `RUNTIME_COMMIT` | 空 | 可选 git commit；为空时使用 Docker build `-ldflags` 注入值 |
| `PROMETHEUS_URL` | 空（docker-compose 默认 `http://prometheus:9090`） | Runtime Status Center 的 Prometheus base URL；用于 `/-/ready` 和 `up{job="unirouter"}` 探测，同时作为深链入口；不嵌入完整 Prometheus UI |
| `GRAFANA_URL` | 空（docker-compose 默认 `http://grafana:3000`） | Runtime Status Center 的 Grafana base URL；用于 `/api/health` 探测，同时作为深链入口；不嵌入完整 Grafana UI |

登录限流只信任 backend 看到的 `RemoteAddr`，不会直接信任客户端可伪造的 `X-Forwarded-For`。如果后续需要按真实客户端 IP 穿透多层代理，需要先定义可信代理链策略，不在 PRD3 临时扩展。

非生产环境可用 Batch 22 smoke 生成测试 License 公钥和离线 License，并验证 `/license` 浏览器导入、状态、审计与 `license.warning` 事件：

```bash
# 生成 smoke 测试公钥；将输出值写入非生产 backend 的 LICENSE_PUBLIC_KEY 后重启 backend
scripts/prd2-batch22-license-smoke.sh --print-public-key

UNIROUTE_SMOKE_BACKEND_URL=http://localhost:8080 \
UNIROUTE_SMOKE_FRONTEND_URL=http://localhost:3000 \
UNIROUTE_SMOKE_DB_DSN='postgres://uniroute:password@localhost:5432/uniroute?sslmode=disable' \
UNIROUTE_BATCH22_ALLOW_LICENSE_RESET=non-production-license-reset \
UNIROUTE_SMOKE_SKIP_BROWSER_INSTALL=true \
scripts/prd2-batch22-license-smoke.sh
```

`UNIROUTE_BATCH22_ALLOW_LICENSE_RESET=non-production-license-reset` 会清空或覆盖全局 `settings.license.offline`，只能在本地、预发或用户明确批准的目标环境使用。
smoke 默认结束后会清理测试 License；如需人工检查最终导入状态，可临时设置 `UNIROUTE_BATCH22_KEEP_LICENSE=true`。

OIDC SSO 默认关闭。启用前在身份提供商侧创建 Web/OIDC client，并将 callback 配置为：

```text
https://<console-host>/api/v1/auth/oidc/callback
```

然后设置：

```bash
OIDC_ENABLED=true
OIDC_ISSUER_URL=https://idp.example.com
OIDC_CLIENT_ID=uniroute
OIDC_CLIENT_SECRET=...
OIDC_REDIRECT_URL=https://<console-host>/api/v1/auth/oidc/callback
OIDC_PROVIDER_NAME='Company SSO'
OIDC_SCOPES=openid,email,profile
OIDC_AUTO_PROVISION=false
```

如果允许首登自动创建用户，必须同时设置 `OIDC_AUTO_PROVISION=true` 和 `OIDC_DEFAULT_ORG_ID=<active org id>`。角色可由 `OIDC_ROLE_CLAIM` 映射，缺失时使用 `OIDC_DEFAULT_ROLE`；项目角色会被加入 `OIDC_DEFAULT_PROJECT_SLUG` 指向的默认项目。配置语义见 `docs/tech-ref/18-oidc-sso.md`。

非生产环境可用 Batch 24 mock-provider smoke 验证 OIDC 协议、用户映射和登录页入口：

```bash
scripts/prd2-batch24-oidc-smoke.sh
```

真实企业 IdP 验收仍需要目标环境 issuer/client 配置和浏览器登录证据。

现有部署升级到支持 `API_KEY_HASH_SECRET` 的版本时：

1. 先将 `API_KEY_HASH_SECRET` 设置为当前正在使用的 `JWT_SECRET` 值。
2. 部署新版本并验证既有 API Key 仍可调用。
3. 之后如需轮换管理员 JWT，只轮换 `JWT_SECRET`，保持 `API_KEY_HASH_SECRET` 不变。
4. 只有在接受“所有 API Key 需要重新生成”的前提下，才轮换 `API_KEY_HASH_SECRET`。

生产 `.env` 必须视为状态文件，不属于代码发布目录的可删除内容。建议在部署机保留只读备份：

```bash
cp .env .env.production
chmod 600 .env .env.production
```

### 2.2 启动服务

```bash
docker compose up -d
```

服务包含 6 个容器：
- `backend` — Go API 服务（端口 8080）
- `frontend` — Nginx 静态文件 + 反向代理（端口 3000）
- `postgres` — PostgreSQL 数据库（内部端口 5432）
- `redis` — Redis 缓存（内部端口 6379）
- `prometheus` — 指标采集与存储（端口 9090）
- `grafana` — 监控面板可视化（端口 3001）

### 2.3 验证部署

```bash
# 检查容器状态
docker compose ps

# 检查健康状态
curl http://localhost:8080/health/ready
# 期望：{"status":"ok","db":"ok","redis":"ok"}

# 检查前端
curl -I http://localhost:3000
# 期望：HTTP/1.1 200 OK

# 检查日志
docker compose logs -f backend
```

### 2.4 初始化管理员

```bash
# 首次部署会随迁移创建 admin 用户，但公开默认密码不可用于生产。
# 部署后必须立即轮换密码，否则 PRD1 不能关闭。
docker compose exec backend ./uniroute admin set-password \
    --username admin \
    --password <strong-password> \
    --org-id 1

# 如需额外管理员，可显式创建，禁止使用 admin123456。
docker compose exec backend ./uniroute admin create \
    --username <new-admin> \
    --password <strong-password> \
    --role admin \
    --org-id 1
```

### 2.5 发布同步安全规则

生产发布时禁止让代码同步删除运行态密钥文件。PR #44 部署时曾因 `rsync --delete` 删除服务器 `.env`，导致只能重新生成 `JWT_SECRET`；由于历史 API Key 的 `key_hash` 使用旧 JWT secret 计算，31 个 API Key 全部失效。`ENCRYPTION_KEY` 从历史记录恢复后，上游渠道密钥未受影响。

如必须使用 `rsync --delete`，至少排除生产密钥和运行态数据：

```bash
rsync -az --delete \
  --exclude='.env' \
  --exclude='.env.production' \
  --exclude='backups/' \
  --exclude='data/' \
  ./ <deploy-user>@<host>:/opt/uniroute/
```

同步后必须检查：

```bash
test -s .env
test -s .env.production
grep -E '^(JWT_SECRET|API_KEY_HASH_SECRET|ENCRYPTION_KEY|LICENSE_PUBLIC_KEY)=' .env
docker compose config >/tmp/uniroute-compose-check.yml
```

若 `.env` 丢失：

1. 先从 `.env.production` 恢复，不要生成新密钥。
2. 如果 `ENCRYPTION_KEY` 无法恢复，已保存上游渠道密钥需要重新录入。
3. 如果 `API_KEY_HASH_SECRET` 无法恢复，既有 API Key 无法验证，必须重新生成。
4. 如果仅 `JWT_SECRET` 无法恢复，管理员需要重新登录；在 `API_KEY_HASH_SECRET` 已独立保存的版本中，API Key 不应受影响。

---

## 三、数据库迁移

### 3.1 自动迁移

服务启动时自动执行迁移（`internal/migrate/migrate.go`）。日志输出：

```
Connected to PostgreSQL
Migrations complete
```

### 3.2 手动迁移

```bash
# 查看当前迁移版本
docker compose exec backend ./uniroute migrate status
```

### 3.3 手动补跑迁移

```bash
# 正常情况下服务启动会自动迁移；需要手动补跑时使用：
docker compose exec backend ./uniroute migrate up
```

当前二进制不提供自动 down 回滚命令。回滚必须由运维人员按具体 migration `.down.sql` 和备份恢复方案执行。

迁移文件各自包含 `BEGIN` / `COMMIT`，不要把多个 `.up.sql` 再包在一个外层
transaction 中当作“dry-run”。PostgreSQL 的内层 `COMMIT` 会结束外层 transaction，
最后的 `ROLLBACK` 不能撤销已经提交的迁移。需要预演时，必须恢复备份到一个明确的
隔离数据库，在隔离库运行当前发布二进制的 `migrate up` 并完成 schema/ledger
读回；不要把生产或共享目标数据库当作 dry-run 目标。如果手工执行 SQL 后发现
schema 对象已存在但 `schema_migrations` 尚未登记，立即停止自动迁移，逐个核对
迁移文件、对象和约束，再通过受审计的修复步骤登记准确版本；不得直接重跑或假定
外层回滚已经生效。

### 3.4 PRD6 `000020` Webhook 安全确认

`000020_prd6_notification_webhook_scope_verification` 不删除任何 Webhook，
但会把升级前已经存在的路由标记为“已隔离”。未重新确认的路由不会接收
新通知，也不会执行历史失败投递的重试；确认路由后，升级前的失败投递也
不会自动重放。

该迁移的数据库 trigger 会拒绝旧版本进程继续新增或修改 Webhook，但旧版本
的读取 handler 不具备新的租户条件。因此这个控制面不能做普通的混合版本
滚动切换。安全顺序是：

1. 进入维护窗口，先阻断旧版本的通知管理 API 和通知 emit/retry；必要时
   临时封锁 Webhook egress；
2. drain/停止全部旧 backend，并至少等待旧 Webhook HTTP 超时窗口，确认没有
   已读入内存、尚未发出的旧投递；
3. 备份数据库，应用 `000020` 并启动新版本；
4. 读回隔离计数，确认没有 pre-P6-F33 实例继续承接通知管理或投递，再恢复
   通知中心访问和 egress；
5. 各组织管理员逐条核对并确认路由。

不要在旧、新 backend 同时承接通知读取请求时开放通知中心。此仓库证据只
验证迁移和代码，不代表上述目标切换已经执行。

升级后由各组织的授权管理员或平台管理员在“通知中心 → Webhook”逐条核对
组织、项目和目标 URL，再点击“确认范围”。不要用批量 SQL 将
`scope_verified` 全部改为 `TRUE`，也不要把审计日志是否存在当作路由安全的
充分证明。新建路由在组织/项目权限校验成功后会自动标记为已确认。
任何名称、URL、事件类型或启用状态编辑都会重新隔离该路由，要求再次确认。

若必须执行 `000020` down，down SQL 会先停用全部 Webhook，并保留
`enabled = FALSE` 的数据库 lockdown；额外的只读 trigger 会拒绝全部 Webhook
新增、修改和删除，同时删除对应 migration ledger 记录，以便随后 roll-forward
重新应用 `000020`。lockdown 期间必须持续关闭旧版本的
`notification-webhooks` POST/PUT/verify 路由，直到 roll-forward 完成。回滚到旧
二进制不会恢复通知投递或配置写入；这是安全停机状态，不是功能完整的旧版本恢复。

### 3.5 PRD6 `000033` Guardrail 非滚动启用

`000033_prd6_guardrail_kernel` 新增版本化策略表，以及旧版本不认识的
`output_withheld` / `guardrail_withheld` / `response_too_large` 财务状态。迁移本身
不会创建或启用任何 PII、语义或外部检测策略，但一旦开始写入这些状态，就不能让
旧网关或旧管理端继续处理同一数据库。

安全启用顺序是：

1. 进入维护窗口，备份数据库并 drain/停止全部旧 gateway/control-plane 副本；
2. 在 `guardrail_policies` 仍为空时应用 `000033`，启动且只启动新版本；
3. 验证 `/health/ready`、策略列表、Chat/Embeddings 普通兼容请求和财务恢复 worker；
4. 先创建 `shadow` 策略并读回具体版本，不要在迁移同时直接批量启用强制规则；
5. 观察无原文证据、`indeterminate`/`unavailable` 比例、延迟和上游成本待处理队列，
   再逐条评审是否进入 `enforce`。

若策略、`output_withheld` 或对应财务证据已经存在，down 会明确拒绝破坏性回滚，
必须修复并 roll-forward。仓库测试只验证此顺序和拒绝条件；它不代表任何目标环境
已经执行升级，也不授权配置真实外部检测器、凭证或付费上游请求。

### 3.6 PRD6 `000034` 流式安全与崩溃恢复非滚动启用

`000034_prd6_stream_policy_termination` 增加 `stream_output` 策略阶段、不可变价格的
流式预算扩容收据、可续租的进程所有权，以及只保存字节数、分片数、Token 计数和
策略版本的追加式 checkpoint。它不保存模型正文、Prompt 或上游凭证，也不会自动
创建或启用任何流式策略。

安全启用顺序是：

1. 先完成 `000033` 的非滚动升级，drain 并停止全部旧 gateway/control-plane 副本；
2. 应用 `000034`，只启动认识扩容序号、lease 和 checkpoint 的新版本；
3. 验证普通流式请求、显式 `max_tokens`、无流式策略兼容路径，以及过期 lease 的
   单所有者恢复；确认 200 ms / 4 KiB 标准化模型数据 JSON 微缓冲按先到边界关闭、
   单帧超限和强制策略无法检查的输出形状在交付前中断，且无 checkpoint 的中断
   不会形成客户用量；
4. 先用 `shadow` / `audit` 策略观察，并向租户明确标注其判断可能晚于内容交付；
5. 只有固定注册表显示 `synchronous=true` 且 lookbehind 在平台上限内的检测器，
   才可逐条评审为流式 `enforce`。命中后不得把同一已接受流静默切换到其他渠道；
6. 验证数据库拒绝扩容收据更新、乱序序号和价格漂移，并确认恢复会复用已有完成或
   中断终态收据，而不会创建冲突的第二终态；
7. 监控预算扩容拒绝、lease 丢失、checkpoint 失败、部分客户结算与 provider-cost
   pending/reconciliation；不得把本地 checkpoint 当作客户已实际读取正文的证明。

一旦存在流式策略、预算扩容收据、lease 或 checkpoint，down 会拒绝破坏性回滚。
必须保留证据并 roll-forward，不能删除记录来迁就旧二进制。此仓库步骤不授权部署、
真实/付费上游调用、外部检测器启用或凭证变更。

### 3.7 V1 `000035` / `000036` 微人民币账本非滚动启用

`000035_v1_pricing_unit_contract` 只固定公开的 CNY/百万 tokens 价格合同，不会
自动修改既有价格。`000036_v1_micro_precision_ledger` 将客户账本、冻结、结算、
恢复和用量证据从整数分切换为显式 `BIGINT` 微人民币，并同步启用 outbox/runtime
v2 与 `ur:agentcap:v2`。旧二进制不认识新列和新单位，因此不能滚动混跑。

该切换必须先取得 R3 User 批准，再按下列顺序执行：

1. 固定待发布 image/commit，确认 migration 顺序包含 `000035` 后紧接 `000036`；
2. 停止入口流量，drain 并停止全部 stable/canary gateway、control-plane、outbox、
   terminal recovery 和其他旧写入者；
3. 备份 PostgreSQL，记录受影响表的行数、金额合计与校验值；逐条复核目标价格的
   来源、币种和生效日期，不允许自动换算或批量改价；
4. 确认数据库不存在 `frozen` / `settlement_pending` reservation、未解决 provider
   attempt 或未完成 terminal hook。`000036` 会再次 fail closed 检查这些条件以及
   价格/金额溢出；
5. 使用只读 `SCAN`/审计证明不存在仍活跃的 `ur:agentcap:v1:*:workflow` 权限，
   再切换 v2。旧 v1 key 按 TTL 自然过期，不做 Redis 批量删除；
6. 确认 X-Insight 已接受带 `cost_micros`、`currency=CNY` 和
   `amount_scale=1000000` 的 snapshot v2，或暂停下一次跨系统交换；
7. 应用 migration，一次只启动确认过 commit 的新版本，读回
   `ledger.version="micro-cny.v1"`、列类型、金额合计和约束；
8. 在恢复流量前验证零用量零收费、2-micro 最小精度、现金/赠送额度结算、outbox
   v2、Redis v2、钱包/API/UI 六位精度和恢复 worker；
9. 分阶段恢复流量并持续核对余额、冻结、终态交易、provider pending 与 outbox
   backlog，不把 readiness 绿灯单独当作财务验收。

启用后，基础价格、cache ratio 与组织倍率必须通过受支持的 Admin API 修改；该路径
会按 model 串行化并在同一事务中验证所有有效组合的精确 micro-CNY `int64` 边界。
不得用手工 SQL 或旧 seed 脚本绕过价格权限、审计和组合溢出校验。

`000036` 的 down 只允许在完全无损时执行：任何非整分金额、不可逆价格、v2-only
outbox/runtime 证据或数据库 `micro_evidence_written` 标记都会拒绝。写入第一条新
微人民币证据后，默认恢复方式是保留证据并 roll-forward，不得删除记录或四舍五入
回旧单位。完整本地证据和 target checklist 见
`docs/ops/evidence/V1-POST-PRD6-MICRO-PRECISION-LEDGER-LOCAL-20260714.md`。

---

## 四、备份与恢复

### 4.1 手动备份

```bash
# 备份 PostgreSQL
docker compose exec postgres pg_dump -U uniroute uniroute > backup_$(date +%Y%m%d_%H%M%S).sql

# 备份 Redis（AOF 文件）
docker compose exec redis redis-cli BGSAVE
docker compose cp redis:/data/dump.rdb ./backups/redis_$(date +%Y%m%d_%H%M%S).rdb
```

### 4.2 恢复

```bash
# 恢复 PostgreSQL
cat backup.sql | docker compose exec -T postgres psql -U uniroute uniroute

# 恢复 Redis
docker compose cp ./backups/dump.rdb redis:/data/dump.rdb
docker compose restart redis
```

### 4.3 PRD2 备份/恢复演练

PRD2 的恢复演练必须在空的非生产数据库执行。脚本不会清空目标库；如果目标库已有对象，恢复会失败，由运维人员重新准备安全目标库后再跑。

```bash
UNIROUTE_BACKUP_SOURCE_DSN='postgres://uniroute:...@postgres:5432/uniroute?sslmode=disable' \
UNIROUTE_RESTORE_REHEARSAL_DSN='postgres://uniroute:...@localhost:5432/uniroute_restore_rehearsal?sslmode=disable' \
UNIROUTE_BACKUP_RESTORE_CONFIRM=non-production-restore-target \
scripts/prd2-backup-restore-rehearsal.sh
```

期望输出：

```text
PASS backup/restore rehearsal schema_versions=<n> core_tables=6 dump_file=/tmp/...
```

备份文件默认保留在 `/tmp`，便于事后校验和归档。不要把该脚本指向生产恢复目标。

2026-07-03 Atlas 在 Alibaba Cloud ECS dev/test 环境完成了一次 Docker PostgreSQL 备份恢复演练，证据见 `docs/ops/evidence/PRD2-B21-backup-recovery-drill-20260703.md`。该演练验证了 custom-format dump、`pg_restore -l`、隔离 drill DB 恢复、迁移/表/核心行数/FK/索引/查询校验、drill DB 清理和 WAL 归档。

该演练未覆盖：备份加密、NAS 自动同步、cron 自动备份、飞书失败告警、PITR 恢复、从远程备份恢复、以及应用连接 drill DB 后的端到端恢复验证。

---

## 五、日志管理

### 5.1 日志格式

当前使用 Go 标准库 `log`，输出纯文本到 stdout，格式：

```
2026/06/19 15:30:00 [req_a1b2c3d4] Billing freeze error: insufficient balance
```

### 5.2 查看日志

```bash
# 实时日志
docker compose logs -f backend

# 按时间筛选
docker compose logs --since "2026-06-19T00:00:00" backend

# 按 request_id 搜索
docker compose logs backend | grep "req_a1b2c3d4"
```

### 5.3 后续规划

以下功能尚未实现，列入后续版本：

- 结构化 JSON 日志（zap/slog）
- 日志级别控制
- 请求延迟、org_id 等结构化字段

---

## 六、监控

### 6.1 健康检查

```bash
# 后端健康检查
curl http://localhost:8080/health/ready
# 期望：{"status":"ok","db":"ok","redis":"ok"}

# 通过管理 API 查看系统状态
curl http://localhost:8080/api/v1/dashboard/stats \
    -H "Authorization: Bearer <JWT>"
```

### 6.2 Prometheus 指标

后端暴露 `/metrics` 端点，自动采集 HTTP 请求、延迟、上游延迟、Token 用量、计费冻结等指标。

```bash
# 查看原始指标
curl http://localhost:8080/metrics

# Prometheus 已配置自动抓取 backend:8080/metrics（5s 间隔）
# Prometheus UI: http://localhost:9090
```

运行时暴露策略：

```bash
# 默认开启，兼容 bundled Prometheus。
METRICS_ENABLED=true

# 可选：只允许指定 CIDR 抓取。为空表示应用层不限制，
# 仍应依赖安全组/防火墙/反向代理避免公网直连。
METRICS_ALLOWED_CIDRS=10.0.0.0/8,172.16.0.0/12

# 不需要应用内指标时关闭，/metrics 返回 404。
METRICS_ENABLED=false

# Runtime Status Center 展示元数据、外部监控健康和入口。
RUNTIME_ENVIRONMENT=staging
# 前端标签是构建时配置；docker compose build frontend 后才会生效。
VITE_DEPLOY_ENV=staging
RUNTIME_DEPLOYMENT_MODE=docker-compose
RUNTIME_DEPLOYMENT_ROLE=stable
PROMETHEUS_URL=http://prometheus:9090
GRAFANA_URL=http://grafana:3000
```

在 bundled docker-compose 内，`GRAFANA_URL` 应使用容器服务地址
`http://grafana:3000`，这样 backend 才能探测 `/api/health`。如果需要从浏览器
打开外部 Grafana，请在部署层使用反向代理或显式覆盖为 backend 可达的外部 URL。

完整指标清单见 `docs/tech-ref/08-prometheus-metrics.md`。

### 6.3 Grafana Dashboard

Grafana 预置了 UniRouter Overview 面板，包含 8 个面板：

| 面板 | 说明 |
|------|------|
| RPS | 每秒请求数，按 method/status 分组 |
| P99/P95/P50 延迟 | 请求处理耗时分位数 |
| 活跃请求数 | 当前并发处理中的请求数（三色阈值） |
| 错误率 | 5xx 占比百分比 |
| 上游延迟 | 按 provider+model 分组的 P95 |
| Token 吞吐 | 按 type+model 分组的 Token 处理速率 |
| 计费冻结 | 按 status 分组的冻结操作频率 |
| 延迟热力图 | 请求耗时分布热力图 |

```bash
# Grafana 登录
# 地址：http://localhost:3001
# 用户名：$GRAFANA_ADMIN_USER（默认 admin）
# 密码：$GRAFANA_ADMIN_PASSWORD
# Dashboard 路径：Dashboards → UniRouter Overview
```

Dashboard JSON 位于 `infra/grafana/dashboards/unirouter-overview.json`，可通过 Grafana UI 导入导出。Prometheus datasource provisioning 固定使用 UID `prometheus`，Dashboard 面板也引用该 UID；如果已有 Grafana 持久化卷来自旧配置，部署后应确认 datasource UID 是否已同步。

### 6.4 性能压测

项目内置 k6 压测脚本，包含梯度爬坡和突发尖峰两个场景：

```bash
# 安装 k6
brew install k6

# 运行压测
k6 run infra/loadtest/k6-chat-completions.js

# 自定义目标地址和 API Key
BASE_URL=http://your-server:8080 API_KEY=your-key k6 run infra/loadtest/k6-chat-completions.js
```

脚本路径：`infra/loadtest/k6-chat-completions.js`

### 6.5 告警建议

生产环境建议配置以下 Prometheus 告警规则：

| 告警 | 条件 | 严重程度 |
|------|------|---------|
| 错误率过高 | 5xx rate > 5% 持续 5 分钟 | critical |
| P99 延迟过高 | P99 > 3s 持续 5 分钟 | warning |
| 活跃请求数过高 | > 100 持续 2 分钟 | warning |
| 上游全部失败 | 所有渠道熔断 | critical |

---

## 七、故障排查

### 7.1 服务无法启动

```bash
# 检查容器日志
docker compose logs backend

# 常见原因：
# - DB_DSN 连接失败 → 检查 PostgreSQL 容器状态
# - REDIS_ADDR 连接失败 → 检查 Redis 容器状态
# - JWT_SECRET/API_KEY_HASH_SECRET/ENCRYPTION_KEY 未设置 → 检查 .env 文件
# - License 导入失败且提示 public key → 检查 LICENSE_PUBLIC_KEY
```

### 7.2 请求返回 502

```bash
# 检查上游渠道状态
curl http://localhost:8080/api/v1/channels \
    -H "Authorization: Bearer <JWT>"

# 检查熔断器状态
docker compose logs backend | grep "circuit"
```

### 7.3 余额异常

```bash
# 检查钱包状态
docker compose exec postgres psql -U uniroute -c \
    "SELECT * FROM wallets WHERE org_id = <org_id>;"

# 检查冻结记录
docker compose exec postgres psql -U uniroute -c \
    "SELECT * FROM freeze_idempotency WHERE status = 'Processing';"

# 检查交易记录
docker compose exec postgres psql -U uniroute -c \
    "SELECT * FROM transactions WHERE org_id = <org_id> ORDER BY created_at DESC LIMIT 20;"
```

### 7.4 前端无法访问

```bash
# 检查 frontend 容器
docker compose logs frontend

# 常见原因：
# - backend 容器未启动 → 检查 docker compose ps
# - nginx 配置错误 → docker compose exec frontend nginx -t
# - SPA 路由 404 → 检查 try_files 配置
```

---

## 八、升级流程

```bash
# 1. 备份数据库
docker compose exec postgres pg_dump -U uniroute uniroute > pre_upgrade_backup.sql

# 2. 拉取新镜像
docker compose pull

# 3. 重启服务（迁移自动执行）
docker compose up -d

# 4. 验证
curl http://localhost:8080/health/ready
```

如果目标构建网络无法访问默认的 `proxy.golang.org`，可在构建时显式选择经审批且可达的 Go module proxy；默认值仍保持官方代理：

```bash
GOPROXY=https://goproxy.cn,direct docker compose build backend
```

该变量只影响构建阶段的模块下载，不应设置 `GONOSUMDB=*`、`GOSUMDB=off` 或跳过现有 `go.sum` 校验。

---


---

## 九、Canary 测试部署

### 9.1 前置条件：Nginx 配置

默认 canary overlay 挂载 `nginx/nginx.http-canary.conf`，只监听 HTTP 80，适合本地和 staging smoke，不需要 TLS 证书。

```bash
docker compose -f docker-compose.yml -f docker-compose.canary.yml config
```

生产启用 TLS 时，把 `docker-compose.canary.yml` 的 Nginx mount 改为 `./nginx/nginx.conf:/etc/nginx/conf.d/default.conf:ro`，并挂载证书：

```bash
# TLS 生产配置示例
# - ./nginx/nginx.conf:/etc/nginx/conf.d/default.conf:ro
# - /etc/letsencrypt/live/your-domain/fullchain.pem:/etc/nginx/ssl/fullchain.pem:ro
# - /etc/letsencrypt/live/your-domain/privkey.pem:/etc/nginx/ssl/privkey.pem:ro
```

启动后可检查 Nginx 配置语法：

```bash
docker compose -f docker-compose.yml -f docker-compose.canary.yml exec nginx nginx -t
```

### 9.2 架构说明

Canary 测试通过外层 Nginx 将部分流量路由到新版（canary）后端实例，同时保持旧版（stable）实例处理大部分请求。

```
客户端 → Nginx:80 (HTTP)
            ├── 90% → uniroute-api:8080        (stable)
            └── 10% → uniroute-api-canary:8080  (canary，共享 PG/Redis)
```

关键约束：
- canary 实例 **共享** stable 的 PostgreSQL 和 Redis
- canary 实例 **跳过** 数据库迁移（`SKIP_MIGRATION=true`），由 stable 实例负责迁移
- 健康检查和 Prometheus 指标 **只走 stable**

### 9.3 启动 Canary

```bash
cd /opt/uniroute

# 确保 stable 实例正常运行
docker compose up -d
curl http://localhost:8080/health/ready  # 必须返回 ok

# 启动 canary（叠加配置）
docker compose -f docker-compose.yml -f docker-compose.canary.yml up -d

# 验证 canary 容器；backend-canary 也暴露 http://localhost:8081 用于直连 smoke
docker compose -f docker-compose.yml -f docker-compose.canary.yml ps
# 应看到 8 个服务：postgres, redis, backend, backend-canary, frontend, nginx, prometheus, grafana
```

### 9.4 验证流量分发

```bash
# 连续请求 20 次，观察响应差异
for i in $(seq 1 20); do
  curl -s http://localhost/v1/chat/completions     -H "Authorization: Bearer sk-test"     -H "Content-Type: application/json"     -d '{"model":"deepseek-chat","messages":[{"role":"user","content":"ping"}]}'     | jq -r '.model // "error"'
done
```

### 9.5 调整 canary 权重

编辑 `nginx/nginx.conf`，修改 `upstream backend_canary` 的 `weight` 值：

```nginx
upstream backend_canary {
    server uniroute-api:8080        weight=9;   # stable
    server uniroute-api-canary:8080 weight=1;   # canary
}
```

常用权重比例：
- `9:1` — 10% canary（保守）
- `3:1` — 25% canary（中等）
- `1:1` — 50% canary（激进，用于发布前验证）

修改后重载 Nginx：

```bash
docker compose exec nginx nginx -s reload
```

### 9.6 监控 canary

```bash
# 查看 canary 日志（独立容器）
docker compose -f docker-compose.yml -f docker-compose.canary.yml logs -f backend-canary

# 对比 stable 和 canary 的错误率
echo "=== Stable errors ==="
docker compose logs backend --since 5m | grep -i "error\|panic\|fatal" | wc -l

echo "=== Canary errors ==="
docker compose logs backend-canary --since 5m | grep -i "error\|panic\|fatal" | wc -l

# 检查 canary 的计费准确性（对比 call_logs）
docker compose exec postgres psql -U uniroute -c \
    "SELECT api_key_id, SUM(total_cost_cents) as total, COUNT(*) as requests \
     FROM call_logs WHERE created_at > NOW() - INTERVAL '1 hour' \
     GROUP BY api_key_id ORDER BY requests DESC LIMIT 10;"
```

### 9.6 回滚 canary

如果 canary 出现问题，立即停止 canary 容器：

```bash
# 停止 canary（Nginx 会自动回退到 stable）
docker compose -f docker-compose.yml -f docker-compose.canary.yml stop backend-canary
docker compose -f docker-compose.yml -f docker-compose.canary.yml rm -f backend-canary

# 验证流量全部回到 stable
curl http://localhost/v1/models -H "Authorization: Bearer sk-test"
```

### 9.7 升级 stable

canary 验证通过后，将 stable 升级到 canary 版本：

```bash
# 1. 停止 canary
docker compose -f docker-compose.yml -f docker-compose.canary.yml stop backend-canary

# 2. 备份数据库
docker compose exec postgres pg_dump -U uniroute uniroute > pre_upgrade_$(date +%Y%m%d).sql

# 3. 重建 stable（新代码）
export RUNTIME_VERSION="$(date +%Y%m%d.%H%M)"
export RUNTIME_COMMIT="$(git rev-parse --short HEAD)"
docker compose build backend
docker compose up -d backend

# 4. 验证
curl http://localhost:8080/health/ready

# 5. 移除外层 Nginx（可选，如果不需要持续 canary）
docker compose -f docker-compose.yml -f docker-compose.canary.yml down nginx
```

### 9.8 PRD2 HA readiness smoke

PRD2 HA readiness 的最小验收目标是：两个 UniRouter 应用实例连接同一 PostgreSQL 和 Redis，且各自的 gateway/admin health check 均可服务。完整边界见 `docs/tech-ref/15-ha-readiness.md`。

关键约束：

- stable 实例负责迁移。
- canary/secondary 实例必须设置 `SKIP_MIGRATION=true`。
- PostgreSQL 和 Redis 是共享状态源。
- in-process admission controller 是单实例容量保护，不是全局分布式 semaphore。

健康验证：

```bash
UNIROUTE_STABLE_URL=http://localhost:8080 \
UNIROUTE_CANARY_URL=http://localhost:8081 \
scripts/prd2-ha-smoke.sh
```

通过 Nginx 或负载均衡器验证：

```bash
UNIROUTE_STABLE_URL=http://localhost:8080 \
UNIROUTE_CANARY_URL=http://localhost:8081 \
UNIROUTE_PROXY_URL=http://localhost \
scripts/prd2-ha-smoke.sh
```

可选真实 gateway smoke（会真实发起模型请求，必须显式开启）。如果提供 `UNIROUTE_HA_DB_DSN`，脚本会额外检查同一 `request_id` 没有产生重复 freeze idempotency/transaction 行：

```bash
UNIROUTE_STABLE_URL=http://localhost:8080 \
UNIROUTE_CANARY_URL=http://localhost:8081 \
UNIROUTE_PROXY_URL=http://localhost \
UNIROUTE_RUN_GATEWAY_SMOKE=1 \
UNIROUTE_API_KEY=sk-... \
UNIROUTE_MODEL=gpt-4o \
UNIROUTE_HA_DB_DSN='postgres://uniroute:...@localhost:5432/uniroute?sslmode=disable' \
scripts/prd2-ha-smoke.sh
```

可选共享 rate-limit smoke 需要一把低限额测试 API Key。该检查会交替打 stable/canary，直到观察到 HTTP 429：

```bash
UNIROUTE_RUN_GATEWAY_SMOKE=1 \
UNIROUTE_RUN_RATE_LIMIT_SMOKE=1 \
UNIROUTE_RATE_LIMIT_REQUESTS=4 \
UNIROUTE_API_KEY=sk-low-limit \
scripts/prd2-ha-smoke.sh
```

回滚验证：

```bash
# 1. 将 canary weight 调整为 0 或停止 backend-canary
docker compose -f docker-compose.yml -f docker-compose.canary.yml stop backend-canary

# 2. 验证 stable 仍然可服务
curl -fsS http://localhost:8080/health/ready
# 期望：{"status":"ok","db":"ok","redis":"ok"}
```

---

## 十、生产环境部署（HTTPS）

### 10.1 架构

```
客户端 → Nginx:443 (TLS) → backend:8080 + frontend:80
```

### 10.2 申请 SSL 证书

```bash
# 安装 certbot
apt install certbot

# 申请证书（需先停止 Nginx 或使用 DNS 验证）
certbot certonly --standalone -d your-domain.com

# 证书路径
# /etc/letsencrypt/live/your-domain.com/fullchain.pem
# /etc/letsencrypt/live/your-domain.com/privkey.pem
```

### 10.3 配置 Nginx

编辑 `nginx/nginx.conf`：
1. 将 `server_name _;` 改为你的域名
2. 取消注释 TLS 证书路径
3. 取消注释 HTTP→HTTPS 重定向

### 10.4 启动

```bash
docker compose -f docker-compose.yml -f docker-compose.canary.yml up -d
```

---

## 十一、安全加固

| 措施 | 说明 |
|------|------|
| HTTPS | Nginx 反向代理 + Let's Encrypt SSL |
| 防火墙 | 只开放 80/443 端口 |
| 数据库 | 不暴露外部端口，只通过 Docker 网络访问 |
| CORS | `ADMIN_CORS_ALLOWED_ORIGINS` 只允许精确 origin；禁止 `*` |
| 登录限流 | `LOGIN_RATE_LIMIT_*` 默认开启；Redis 错误时 fail-open，避免运维锁死 |
| Metrics | `/metrics` 默认兼容 Prometheus；生产用 `METRICS_ALLOWED_CIDRS`、防火墙或反向代理限制，或设置 `METRICS_ENABLED=false` |
| Redis | 设置密码，不暴露外部端口 |
| 密钥 | `JWT_SECRET` 可按需轮换；`API_KEY_HASH_SECRET` 和 `ENCRYPTION_KEY` 轮换前必须确认会话、API Key、上游渠道密钥的影响；`LICENSE_PUBLIC_KEY` 由签发方管理 |
| Grafana | 设置 `GRAFANA_ADMIN_PASSWORD`，禁止继续使用 `admin/admin` |
| 备份 | 备份文件加密存储 |
