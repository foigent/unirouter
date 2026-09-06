# Deployment Runbook（部署运维手册）

> **版本**：v1.25
> **日期**：2026-08-30
> **用途**：运维人员部署和维护 UniRouter 的操作手册
> **变更记录**：v1.25 收敛 development/test 的同 ceiling 与升 ceiling 部署为同一显式 Compose project/file-chain 权威，并增加 168h 默认、120h 磁盘压力模式的受保护 BuildKit 清理边界。
> **变更记录**：v1.24 增加 `000084` 官方价格来源 CNY/USD 审核草稿、CNY 结算 fail-closed、非滚动升级与 guarded down 边界。
> **变更记录**：v1.23 增加 `000083` 版本化非 Token 计费证据、非滚动升级与 guarded down 边界。
> **变更记录**：v1.22 增加 `000082` canonical Release 执行 Profile/上下文上限密封、非滚动升级与 guarded down 边界。
> **变更记录**：v1.21 增加 `000081` 版本化模型执行 Profile、历史模型不推断、guarded down 与非滚动升级边界。
> **变更记录**：v1.20 增加 release worktree 的 N/N-1/N-2 只读报告、非规范/dirty/attached fail-closed 分类和显式 apply 边界，并补充精确版本构建的 BuildKit Go 缓存约束；v1.19 增加 ECS 不可变标签构建复用、构建前磁盘 fail-closed、可注入 Go module proxy、全服务 Docker 日志轮转、N-1/N-2 schema 兼容门禁和需显式授权的按需镜像/BuildKit 清理；v1.18 增加 development/test Target 单次构建复用、两代已验证回滚保留和只读镜像生命周期报告；v1.17 增加默认关闭的 Exact Response Cache 三段式密钥注入、客户 preflight all-or-none 校验、冷失效轮换边界和迁移 `000056` 运维约束；v1.16 增加 `000037`→`000042` 统一停流量/单次 migrate/部分提交后 roll-forward 流程，补充 `000042` provider-scoped 本地/OIDC 身份合同和 guarded down，并移除管理 CLI 的 argv 密码输入，改为双次无回显交互或受控单行 `--password-stdin`；v1.15 增加迁移原子 ledger 语义，以及 `000039` Guardrail 租户隔离、`000040` 投递尝试绑定、`000041` 流式 generation fencing 的非滚动切换与 quarantine/roll-forward 边界；v1.14 增加 `000038` 管理会话撤销的非滚动切换与 guarded-down 边界；v1.13 增加正式模式 32-byte 运行时密钥门禁，并补齐客户首次安装对同一验签包的容器 staging、`record-applied` 与读回；v1.12 修正客户 preflight 调用、可执行占位符、micro-CNY 查询和 Canary Compose/Nginx 命令；v1.11 明确分离源码开发与签名客户交付路径，增加客户 Compose digest preflight 和通知 Webhook HMAC 配置；v1.10 增加真实上游启用前的模型身份、映射、价格权限和受控调用检查；v1.9 增加可双击 License Studio，并把正式 License/升级验签根从客户运行时环境变量迁到发布二进制；v1.8 新增 V1 商业 License 强制、公钥环/部署绑定和签名离线升级包流程；v1.7 新增 PRD3 SEC-005 运行时暴露策略：CORS 精确 origin、登录限流、metrics 开关/CIDR；v1.6 新增 PRD2 OIDC-first SSO 配置和 mock-provider smoke；v1.5 新增 HTTP-only canary overlay、8081 canary direct smoke、PRD2 HA/backup rehearsal 命令；v1.4 新增生产 `.env` 保护、`API_KEY_HASH_SECRET`、rsync 安全同步规则和 PR #44 部署事故复盘；v1.3 新增 Prometheus + Grafana 监控栈、k6 压测脚本；v1.2 新增 Nginx 反向代理 + canary 测试部署章节；v1.1 修正日志描述（标准库 log，非 zap）、容器名更新（backend/frontend）

---

Same-schema runtime withdrawal and the explicit first-install boundary are
documented in [RUNTIME-CONTINUITY.md](RUNTIME-CONTINUITY.md). Schema-changing
legacy Publication still uses the maintenance procedure below.

## 一、环境要求

| 组件 | 版本 | 说明 |
|------|------|------|
| Docker | 24+ | 容器运行时 |
| Docker Compose | 2.20+，且支持 `config --environment` | 编排与正式密钥 preflight |
| Bash | 3.2+ | 执行客户 Compose 的本地 preflight |
| 磁盘 | 50GB+ | PostgreSQL 数据 + 备份 |
| 内存 | 4GB+ | Go 二进制 + PG + Redis |
| CPU | 2 核+ | 建议 4 核 |

部署路径只有两条，不能混用：

- 根目录 `docker-compose.yml` 与 `docker-compose.canary.yml` 会从源码构建，
  固定使用 `development` release-trust mode，仅用于开发、内部测试和已授权的
  非生产 ECS 验证；
- 客户正式安装/升级只使用已经验签的离线发布包、`deploy/customer`、预构建
  digest 镜像和嵌入式信任根。先验证签名 manifest，再运行
  `bash deploy/customer/preflight.sh`。preflight 检查正式密钥下限、Compose 形状与
  digest 语法，不能代替签名验证。正式 release 二进制也会拒绝短于 32 bytes 的
  `JWT_SECRET` 或 `API_KEY_HASH_SECRET`；development 构建保留非空值兼容。

---

## 二、源码开发与内部测试首次部署

### 2.1 准备配置文件

```bash
cd /opt/uniroute
cp .env.example .env

# 编辑 .env，必须修改以下项：
# - DB_DSN: PostgreSQL 连接串
# - JWT_SECRET: 32 字节随机字符串
# - API_KEY_HASH_SECRET: API Key HMAC 稳定密钥
# - ENCRYPTION_KEY: 32 字节随机字符串
# - RESPONSE_CACHE_*: 默认全部留空；只有获批启用 Exact Cache 时才同时注入两把独立 32-byte 密钥和一个版本标签
# - UNIROUTE_NOTIFICATION_WEBHOOK_SIGNING_SECRET: 启用签名通知时至少 32 字节；stable/canary 必须相同
# - LICENSE_DEPLOYMENT_ID: 仅在本地测试商业 License 时填写绑定标识
# - GRAFANA_ADMIN_PASSWORD: Grafana 管理员强密码
# - ADMIN_CORS_ALLOWED_ORIGINS: 仅当浏览器会跨域直连 backend 时配置精确 origin
# - METRICS_ALLOWED_CIDRS: 生产建议限制到 Prometheus 所在网络
# - MIGRATIONS_DIR: 迁移文件目录（默认 /app/migrations）

# 生成随机密钥
openssl rand -hex 32  # 用于 JWT_SECRET
openssl rand -hex 32  # 用于 API_KEY_HASH_SECRET
openssl rand -base64 24  # 恰好 32 个 ASCII bytes，用于 ENCRYPTION_KEY
openssl rand -base64 24  # 恰好 32 个 ASCII bytes，用于 RESPONSE_CACHE_INDEX_KEY
openssl rand -base64 24  # 恰好 32 个 ASCII bytes，用于 RESPONSE_CACHE_ENCRYPTION_KEY
openssl rand -hex 32  # 用于 UNIROUTE_NOTIFICATION_WEBHOOK_SIGNING_SECRET
openssl rand -base64 24  # 用于 GRAFANA_ADMIN_PASSWORD
```

密钥职责：

| 变量 | 作用 | 丢失/变更影响 |
|------|------|---------------|
| `JWT_SECRET` | Admin JWT 签名 | 管理员需要重新登录；不应影响 API Key |
| `API_KEY_HASH_SECRET` | API Key HMAC 哈希 | 变更会导致既有 API Key 全部失效 |
| `ENCRYPTION_KEY` | 上游渠道密钥 AES-GCM 加密 | 变更且无法恢复会导致已保存 channel key 无法解密 |
| `RESPONSE_CACHE_INDEX_KEY` | Exact Cache 不透明索引 HMAC；默认空 | 非空时必须与加密密钥和版本同时配置；轮换会使旧索引不可达并冷启动缓存 |
| `RESPONSE_CACHE_ENCRYPTION_KEY` / `RESPONSE_CACHE_KEY_VERSION` | Exact Cache 值 AES-GCM 与不可变版本标签；默认空 | 当前进程只加载一个版本；轮换后旧条目 fail-closed miss 并按 TTL 老化，不得复用版本标签 |
| `UNIROUTE_NOTIFICATION_WEBHOOK_SIGNING_SECRET` | 出站通知 Webhook HMAC | 非空必须至少 32 字节；stable/canary 不一致会造成接收端间歇验签失败 |
| 正式镜像内嵌 License 公钥环 | V1 商业 License Ed25519 验签根 | 发布构建缺失时镜像构建失败；客户运行时不能替换 |
| `LICENSE_DEPLOYMENT_ID` | V1 License 绑定的稳定部署标识 | 缺失或与签名 payload 不一致时商业 License 不可强制执行 |
| 正式镜像内嵌升级公钥环 | 独立的升级包 Ed25519 验签根 | 发布构建缺失时镜像构建失败；客户运行时不能替换 |
| `LICENSE_PUBLIC_KEY` / `LICENSE_PUBLIC_KEYS` / `UPGRADE_PUBLIC_KEYS` | 仅源码开发兼容 | 正式 release 二进制发现任一运行时覆盖即拒绝启动 |

两个公钥环均为严格 JSON 对象，例如
`{"license-2026-a":"base64-public-key"}`，由受控发布构建分别编码到
`UNIROUTER_LICENSE_TRUST_BUNDLE_B64` 和
`UNIROUTER_UPGRADE_TRUST_BUNDLE_B64`。`Dockerfile` 默认按 `release` 构建，任一
公开验签环缺失就失败；根目录源码 Compose 显式使用 `development`，只能用于
开发。公钥不是 secret，但信任根变更是高影响发布操作；stable/canary 必须使用
同一签名镜像和部署标识。两类私钥必须分开保管，且不得进入 `.env`、镜像、仓库、
数据库、日志或客户包。

运行时暴露策略：

| 变量 | 默认 | 建议 |
|------|------|------|
| `SHUTDOWN_TIMEOUT_SECONDS` | `120` | graceful shutdown 等待时间；默认与 backend `WriteTimeout` 对齐，避免 30 秒内强制截断较长流式响应 |
| `RESPONSE_CACHE_INDEX_KEY` / `RESPONSE_CACHE_ENCRYPTION_KEY` / `RESPONSE_CACHE_KEY_VERSION` | 全空 | 全空表示运行时不可用且策略默认关闭；任一非空则必须三者完整、两把密钥各 32 bytes。配置就绪也不会自动创建/推进策略或启用流量 |
| `ADMIN_CORS_ALLOWED_ORIGINS` | 空 | 为空表示只支持同源/服务端代理访问；需要 Vite 或跨域控制台直连 backend 时填精确 origin，如 `http://localhost:3000,http://localhost:5173`；不支持 `*` |
| `LOGIN_RATE_LIMIT_ENABLED` | `true` | 保持开启；分别按可信代理解析后的客户端 IP 与规范化 username 计数；没有可信代理时安全退回 backend `RemoteAddr` |
| `LOGIN_RATE_LIMIT_WINDOW_SECONDS` | `60` | 登录尝试固定窗口秒数 |
| `LOGIN_RATE_LIMIT_IP_REQUESTS` | `30` | 单个解析后客户端 IP 每窗口最大登录尝试数 |
| `LOGIN_RATE_LIMIT_USER_REQUESTS` | `10` | 同一规范化 username 在所有客户端 IP 之间共享的每窗口最大登录尝试数 |
| `TRUSTED_PROXY_CIDRS` | 空（customer Compose 显式设置） | 只填实际受控代理 hop；为空时忽略 `X-Forwarded-For`/`X-Real-IP`。customer Compose 必须包含固定 frontend `/32`，同宿主 TLS 代理默认另含固定 Docker gateway `/32` |
| `METRICS_ENABLED` | `true` | 兼容 bundled Prometheus；不需要应用内指标时设为 `false` |
| `METRICS_ALLOWED_CIDRS` | 空 | 为空表示应用层不限制；生产建议填 Prometheus/内网 CIDR，公网隔离仍应依赖安全组、防火墙或反向代理 |
| `TRACE_EVENTS_ENABLED` | `true` | 记录 metadata-only 请求路径事件；高流量生产环境可设为 `false` 暂停写入 |
| `TRACE_EVENT_SAMPLE_RATE` | `1` | 按 `request_id` 确定性采样，范围 `0..1`；例如 `0.1` 约采样 10% 请求并保留同一请求的完整事件段 |
| `TRACE_EVENT_RETENTION_DAYS` | `LOG_RETENTION_DAYS` | `request_trace_events` 自动清理窗口；设为 `0` 时跳过 trace event 清理但不关闭写入，关闭写入仍使用 `TRACE_EVENTS_ENABLED=false` |
| `PAYLOAD_CAPTURE_MODE` | `metadata_only` | PRD5 payload evidence 状态；当前仅支持 `metadata_only` 和 `disabled`，不会启用 raw prompt/response 存储 |
| `UNIROUTE_OTEL_EXPORT_ENABLED` | `false` | 开启 metadata-only OTLP/HTTP trace export；必须同时保持 `TRACE_EVENTS_ENABLED=true`。默认关闭，collector 故障和队列丢弃不会改变网关请求、Outbox 或计费结果 |
| `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` | 空 | 开启导出时必填；可填 collector origin 或精确 `/v1/traces` URL，仅允许 http/https，拒绝 userinfo、query 和 fragment。B3 不支持自定义鉴权 header，需鉴权的 collector 等待 B4 secret-reference 接线或置于已受控的本地代理后 |
| `UNIROUTE_OTEL_EXPORT_QUEUE_SIZE` | `256` | 单进程固定内存队列，范围 `1..65536`；满时 fail-open 丢弃 export snapshot，不阻塞请求 |
| `UNIROUTE_OTEL_EXPORT_BATCH_SIZE` | `32` | 单次 OTLP 请求 span 上限，范围 `1..128` 且不得大于 queue |
| `UNIROUTE_OTEL_EXPORT_FLUSH_INTERVAL_MS` | `500` | 非满批次刷新窗口，范围 `10..60000` 毫秒 |
| `UNIROUTE_OTEL_EXPORT_TIMEOUT_MS` | `2000` | 每次 collector 请求的硬超时，范围 `50..60000` 毫秒 |
| `UNIROUTE_OTEL_EXPORT_MAX_RETRIES` | `2` | 仅网络失败及 HTTP 429/502/503/504 的附加重试次数，范围 `0..5`；HTTP 200 partial success 已确认、不重试 |
| `CHAT_REQUEST_MAX_BYTES` | `8388608` | Chat 同步 JSON 请求默认 8 MiB；应用按实际读取字节执行，不信任 `Content-Length` |
| `EMBEDDINGS_REQUEST_MAX_BYTES` | `16777216` | Embeddings 批量同步请求默认 16 MiB；大媒体不应通过此端点传输 |
| `NONSTREAM_RESPONSE_MAX_BYTES` | `16777216` | 最终非流式客户响应默认 16 MiB（其中为 UniRouter 元数据预留 64 KiB）；超限返回 502、不 failover、不向客户收费，并保留可能的上游成本待处理状态 |
| `PAYLOAD_NODE_INFLIGHT_BYTES` | `268435456` | 单个 UniRouter 副本实际保留 payload 的总字节预算；需按堆内存与并发实测调整 |
| `PAYLOAD_ORG_INFLIGHT_BYTES` | `67108864` | 单组织在单副本的实际保留字节预算；必须小于等于 node budget |
| `GUARDRAIL_ORG_ACTIVE_POLICY_LIMIT` | `256` | 单组织 active guardrail policy 原子上限；`disabled` 历史不计入，不会被自动删除 |
| `GUARDRAIL_PLATFORM_ACTIVE_POLICY_LIMIT` | `256` | 平台级 active guardrail policy 独立上限；平台 mandatory 快照失效可全局 fail closed |
| `GUARDRAIL_CACHED_POLICY_LIMIT` | `4096` | 单副本 immutable active-policy cache 总上限，同时由数据库触发器防止跨副本超配 |
| `GUARDRAIL_CACHED_TENANT_LIMIT` | `4096` | 单副本可缓存的 active tenant policy scope 数；必须小于等于总 cache 上限 |
| `EVIDENCE_CONTEXT_TRUST_SECRET` | 空 | PRD5 受信 evidence context 共享密钥；为空时忽略所有 inbound trace/session/actor/delegation headers。仅在受控内网/上游业务系统可持有密钥时启用，且不要写入 repo 或共享文档 |
| `UNIROUTE_NOTIFICATION_WEBHOOK_SIGNING_SECRET` | 空（客户 Compose 必填） | 出站通知 Webhook HMAC 密钥；非空至少 32 字节，同一部署的 stable/canary 必须完全相同。接收端验证规则见 `docs/ops/notification-webhooks.md` |
| `LOG_RETENTION_DAYS` | `30` | call logs、settle/cancel transactions 和相关清理的保留窗口；audit logs 保留该值的 2 倍 |
| `RUNTIME_ENVIRONMENT` | `unspecified`（docker-compose 默认 `local`） | Runtime Status Center 展示的环境标签，如 `dev`、`staging`、`prod` |
| `VITE_DEPLOY_ENV` | 空 | 前端镜像构建时写入的环境标签；支持 `development`、`staging`、`canary`、`production` 等值，并应与 `RUNTIME_ENVIRONMENT` 保持一致。修改后必须重建 frontend 镜像，仅重启容器不会生效 |
| `MODEL_ROUTE_AUTHORITY` | `canonical` | Chat/Embeddings 的进程级路由权威。二进制与普通 Compose 默认 `canonical` 且不会自动回退；`legacy`/`verify` 只允许显式兼容阶段。仅当全部当前 assignment 仍为 v1 时，准备 `verify` 才显式运行 `uniroute model-route-authority apply --customer-org-id <id>`（逐项准备当前 v1 stable/canary，但不 promote/rollback），再运行只读 `check`；任一 v2 成为当前 stable/canary 后（包括仍在 verify 的滚动窗口）apply 会在投影写入前拒绝。准备 `canonical` 时只运行 `check`，`apply` 会在访问数据库前被拒绝，防止 legacy 反向覆盖 canonical pause/revoke/config。reconcile 与 v2 lifecycle 以固定 releases→slots 表锁顺序串行。切换前必须完成全 fleet drain；backend 会在 Redis、后台任务和 HTTP bind 前再次从 PostgreSQL fail-closed 校验，不能只改环境变量。旧 channel/key/v1 runtime-change/model-endpoint mutation 已在所有模式永久返回 410，不再由模式开关恢复；组织管理员仍可在缺少/过期 Gateway License 时调用 canonical connection pause 与 terminal credential revoke 两个止损动作。修改后必须重启 backend |
| `MODEL_ROUTE_COMPATIBILITY_ENABLED` | `false` | `canonical` 必须为 `false`；`legacy`/`verify` 必须显式为 `true`。正式 customer fresh install 的样例仅在隔离流量的一次性 bootstrap/authoring 阶段使用 `legacy + true`，完成 customer root、首个 sealed release/slot 和只读 authority check 后改为 `canonical + false` 并重启。它不是旧写面开关，也不能绕过 startup authority |
| `RUNTIME_DEPLOYMENT_MODE` | `single-node` | Runtime Status Center 展示的部署形态，如 `docker-compose`、`kubernetes` |
| `RUNTIME_DEPLOYMENT_ROLE` | `stable` | 当前实例角色，如 `stable`、`canary` |
| `RUNTIME_VERSION` | 空 | 仅 source/dev 构建的 fallback；正式镜像始终以不可变 Docker build `-ldflags` 注入值为准 |
| `RUNTIME_COMMIT` | 空 | 仅 source/dev 构建的 fallback；签名发布必须注入完整 40/64 位小写 Git commit，环境变量不能覆盖 |
| `PROMETHEUS_URL` | 空（docker-compose 默认 `http://prometheus:9090`） | Runtime Status Center 的 Prometheus base URL；用于 `/-/ready` 和 `up{job="unirouter"}` 探测，同时作为深链入口；不嵌入完整 Prometheus UI |
| `GRAFANA_URL` | 空（docker-compose 默认 `http://grafana:3000`） | Runtime Status Center 的 Grafana base URL；用于 `/api/health` 探测，同时作为深链入口；不嵌入完整 Grafana UI |

登录限流和 API Key IP allowlist 使用同一个可信代理解析器。只有 backend
直接 peer 命中 `TRUSTED_PROXY_CIDRS` 时才读取转发头，并从
`X-Forwarded-For` 右向左剥离受信 hop，取第一个非受信地址；否则完全忽略转发头并
使用 `RemoteAddr`。frontend Nginx 会 append 自己看到的网络 peer，因此直连调用者
伪造的左侧 XFF 不能抢占真实 peer。外层 TLS 代理必须使用 overwrite 或 append
语义，并把其实际 hop CIDR 精确加入配置；禁止信任 `0.0.0.0/0` 或 `::/0`。

IP 桶继续限制单一来源的总尝试量；username 桶不再按来源 IP 分片，以阻止多来源绕过
同一账户预算。该账户级短窗口也意味着知道 username 的调用者可以暂时耗尽其预算，
运维应观察登录 `429` 并按实际风险调整窗口和阈值，不应改回 `IP+username`。
限流启用时，Redis client 缺失、计数操作失败或请求上下文取消都会在密码验证前返回
`503` 和 `Retry-After: 1`；服务不会在共享限流状态不确定时降级放行。此时先恢复
Redis 并确认 `/health/ready`，不要通过关闭登录限流来绕过故障。

PRD2 Batch 22 smoke 只验证历史 payload-only foundation，不能作为 V1 商业
激活证据，也不能放行新模型流量。发行方在 macOS 上应双击 `UniRouter License
Studio.app` 生成两套独立密钥和严格 `unirouter.license.v1` 文档；正常签发无需
终端。CLI 只保留给 CI/受控自动化。任何非生产目标激活仍须在执行前获得明示授权。
完整流程见 `docs/tech-ref/16-license-foundation.md`。

激活顺序：

1. 通过独立受信渠道取得签名发布清单，核对已内嵌两个公钥环的 backend 镜像
   digest，并为 stable/canary 设置同一个批准的部署标识；
2. 启动后确认 `/health/ready` 与本地平台管理员登录可用，并从每个
   stable/canary 启动日志读回 `Release trust mode: release`、非零
   `license_keys` / `upgrade_keys`；出现 `development` 或零验签根必须停止
   切换并回滚，不能把 source Compose 健康当作正式发布成功；
3. 在未导入 License 时用无付费上游副作用的请求证明 `license_error.missing`；
4. 在“商业授权”页导入与部署标识绑定、当前有效的 V1 文档；
5. 读回 `commercial=true`、`enforceable=true`、签名/部署匹配、key ID、到期日、
   feature 和 limit；
6. 仅用 mock/无付费路径证明授权后 admission，再进行另外批准的真实用户测试。

License 导入会同时锁定并核对 active API keys/users/projects 以及 enabled canonical
provider connections（License 字段名仍为 `active_channels`）；低于
当前使用量的续期返回 `409`，不会写入一半状态。严禁直接修改
`settings.license.offline` 或 `commercial_license_entitlements` 绕过该事务。

OIDC SSO 默认关闭。启用前在身份提供商侧创建 Web/OIDC client，并将 callback 配置为：

```text
https://console.example.com/api/v1/auth/oidc/callback
```

然后设置：

```bash
OIDC_ENABLED=true
OIDC_ISSUER_URL=https://idp.example.com
OIDC_CLIENT_ID=uniroute
OIDC_CLIENT_SECRET=REPLACE_WITH_OIDC_CLIENT_SECRET
OIDC_REDIRECT_URL=https://console.example.com/api/v1/auth/oidc/callback
OIDC_PROVIDER_NAME='Company SSO'
OIDC_SCOPES=openid,email,profile
OIDC_AUTO_PROVISION=false
```

如果允许首登自动创建用户，必须同时设置 `OIDC_AUTO_PROVISION=true`，并将
`OIDC_DEFAULT_ORG_ID` 设置为已存在的活动组织 ID。不要复制带尖括号的占位符到
shell 或 `.env`。角色可由 `OIDC_ROLE_CLAIM` 映射，缺失时使用
`OIDC_DEFAULT_ROLE`。组织角色只设置 canonical `organization_role`，不会自动获得
任何项目；`project_admin`、`project_user`、`project_viewer`（以及兼容 alias
`developer`/`project_developer`/`viewer`）会创建 `organization_role=member`，并仅在
`OIDC_DEFAULT_PROJECT_SLUG` 指向的默认项目建立 canonical membership。配置语义见
`docs/tech-ref/18-oidc-sso.md`。

迁移 `000058_enterprise_project_role_separation` 支持一个版本窗口的滚动升级：旧
binary 省略 `users.organization_role` 的 INSERT 由不可落盘 sentinel 识别，旧
`users.role` UPDATE 与旧 `project_members.role` alias 由兼容 trigger 归一化。行级
trigger 无法区分“旧 binary 只更新 role”和“客户端把 organization_role 重复写成
旧值、同时写入不一致 role”这两种 UPDATE；因此该精确形状在兼容窗口内按 legacy
语义归一化。当前 binary 的组织角色写路径必须始终验证并同时写入双轴。确认所有旧
writer 退役后，应通过后续独立迁移移除 sentinel default 与两个兼容 trigger；不得把
它们作为长期授权机制。

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

共享或长期测试环境的 `.env` 必须视为状态文件，不属于代码发布目录的可删除内容。建议在部署机保留只读备份：

```bash
cp .env .env.production
chmod 600 .env .env.production
```

### 2.2 启动服务

下面的普通启动只适用于已完成 canonical authoring/inventory、且 migration ledger 与
当前 binary ceiling 精确一致的数据库。全新空库不能用默认
`canonical + compatibility=false` 直接启动；必须先按根
[`README` 的隔离 bootstrap 流程](../../README.md#快速开始) 完成 author/check、停止
bootstrap backend、恢复 canonical 默认并重启。bootstrap 期间不得接入外部代理、
负载均衡或客户 Gateway 流量。

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

#### 2.3.1 真实上游启用前检查

真实模型不能只凭管理页显示名称判断可用。每次只启用一个明确模型，并完成：

1. 从厂商官方文档和当前凭证的只读模型列表核对精确 supplier model ID；不得把
   历史别名或营销名称当作长期 API 合同，也不得在日志中输出凭证；
2. 创建 canonical provider connection/deployment，设置四个正的执行限额并绑定
   credential；public model 与 upstream model 必须保留各自的精确身份；
3. 对 endpoint 对应的 exact `model_operation_id` 执行受支持 probe，并保存 revision、
   credential binding 和成功证据；不得通过 legacy channel/key 或 Router preview
   猜测当前路由；
4. 经 M78 release workspace 原子 author sealed release，执行 server-described
   `activate_initial`；用 Readiness v2 和 canonical Model Endpoints v3 读回同一个
   `deployment_id + model_operation_id` 当前身份；
5. 单独核对客户价格的来源、币种、生效日期和批准的加价规则。模型映射变更不
   授权修改价格；客户售价与供应商 COGS 不一致时必须先取得商业定价决定；
6. 只有取得具体模型、次数和费用上限授权后才发真实请求。使用最小合成输入，
   读回 canonical deployment/operation 的 route/attempt/call-log/trace/钱包守恒，
   随后禁用临时凭证并确认无 pending attempt、reservation 或 outbox。

Probe、Readiness 和管理投影只证明其各自的控制面事实；至少一个同范围真实 HTTP
200 请求才证明对应上游可调通。单次成功仍不能替代容量、HA、供应商账单或
Production 证据。

### 2.4 初始化管理员

```bash
# 首次部署会随迁移创建 admin 用户，但公开默认密码不可用于任何非本地流量。
# 交互终端中会无回显输入并确认两次。
docker compose exec backend ./uniroute admin set-password \
    --username admin \
    --org-id 1

# 如需额外管理员，可显式创建；同样使用无回显的交互输入，禁止使用 admin123456。
: "${UNIROUTER_NEW_ADMIN_USERNAME:?Set the new administrator username}"
docker compose exec backend ./uniroute admin create \
    --username "$UNIROUTER_NEW_ADMIN_USERNAME" \
    --role admin \
    --org-id 1
```

非交互自动化必须从受控、只读的 secret 文件向容器标准输入传递恰好一行密码，
并使用 `-T` 禁止 Docker 分配 TTY。文件内容允许一个末尾 LF 或 CRLF，不允许第二行
或任何额外字节；不要改用环境变量密码或命令参数：

```bash
: "${UNIROUTER_ADMIN_PASSWORD_FILE:?Set the approved administrator password file}"
test -r "$UNIROUTER_ADMIN_PASSWORD_FILE"
docker compose exec -T backend ./uniroute admin set-password \
    --username admin \
    --org-id 1 \
    --password-stdin < "$UNIROUTER_ADMIN_PASSWORD_FILE"

: "${UNIROUTER_NEW_ADMIN_USERNAME:?Set the new administrator username}"
: "${UNIROUTER_NEW_ADMIN_PASSWORD_FILE:?Set the approved new-administrator password file}"
test -r "$UNIROUTER_NEW_ADMIN_PASSWORD_FILE"
docker compose exec -T backend ./uniroute admin create \
    --username "$UNIROUTER_NEW_ADMIN_USERNAME" \
    --role admin \
    --org-id 1 \
    --password-stdin < "$UNIROUTER_NEW_ADMIN_PASSWORD_FILE"
```

当前二进制会在数据库访问前拒绝旧的 value-bearing `--password`，也会拒绝位置参数
形式的密码。没有 `--password-stdin` 时，非 TTY 标准输入会明确失败，不会从环境变量
回退读取秘密。

`admin create --role` 只接受组织角色 `platform_admin`、`org_admin`、
`billing_admin`、`org_viewer`、`member`，并保留 `admin` 作为
`platform_admin` 的兼容别名。项目角色必须通过项目成员关系授予；
`project_admin`、`project_user`、`project_viewer` 及旧别名会在数据库访问前被拒绝。

### 2.5 发布同步安全规则

历史 source-based 内部目标同步时，禁止让代码同步删除运行态密钥文件。
客户正式交付不使用本节 `rsync` 路径。PR #44 部署时曾因
`rsync --delete` 删除服务器 `.env`，导致只能重新生成 `JWT_SECRET`；
由于历史 API Key 的 `key_hash` 使用旧 JWT secret 计算，31 个
API Key 全部失效。`ENCRYPTION_KEY` 从历史记录恢复后，上游渠道密钥未受影响。

内部测试目标也不得使用 `rsync --delete` 或其他批量删除参数。先设置明确的目标
主机，再只同步新增/变更文件；如需清理旧文件，逐个确认明确路径后单独处理：

```bash
: "${DEPLOY_USER:?Set the approved deployment user}"
: "${DEPLOY_HOST:?Set the approved deployment host}"

rsync -az \
  --exclude='.env' \
  --exclude='.env.production' \
  --exclude='backups/' \
  --exclude='data/' \
  ./ "$DEPLOY_USER@$DEPLOY_HOST:/opt/uniroute/"
```

同步后必须在目标主机检查，且不得把密钥值打印到终端或 CI 日志：

```bash
ssh "$DEPLOY_USER@$DEPLOY_HOST" 'cd /opt/uniroute &&
  test -s .env &&
  test -s .env.production &&
  grep -Eq "^JWT_SECRET=.+" .env &&
  grep -Eq "^API_KEY_HASH_SECRET=.+" .env &&
  grep -Eq "^ENCRYPTION_KEY=.+" .env &&
  grep -Eq "^UNIROUTE_NOTIFICATION_WEBHOOK_SIGNING_SECRET=.+" .env &&
  docker compose config >/dev/null'
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

迁移 SQL 仍保留首个独立 `BEGIN;` 和末尾独立 `COMMIT;`，以兼容人工直接执行；由
当前二进制的 `migrate.Run` 执行时，只移除这两个最外层语句，并为每个 migration
自行开启一个事务。该事务同时提交 SQL 正文和对应 `schema_migrations` 行，因此不会
再出现“schema 已提交但 ledger 未登记”的正常失败窗口。一个 session advisory lock
覆盖完整迁移列表和各 migration 之间的间隙，多个副本不能并发发现并重复应用同一
版本。人工直接执行单个 SQL 文件时仍由该文件自己的事务负责，且不会替你写入
`schema_migrations`；不得把人工 SQL 和二进制迁移混用为自动流程。需要预演时，必须
把备份恢复到明确的隔离数据库，使用待发布二进制运行 `migrate up` 并读回 schema 与
ledger。若发现两者不一致，立即停止启动/自动迁移，逐个核对对象、约束与版本，再通过
受审计的修复步骤恢复准确 ledger；不得猜测成功或直接重跑。

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

### 3.8 V1 `000037` → `000042` 统一非滚动升级窗口

当前目标环境仍在 `000037` 时，`uniroute migrate up` 不会让运维在
`000038`、`000039`、`000040`、`000041` 或 `000042` 之间暂停选择，而会按
`schema_migrations` ledger 顺序尝试全部 pending migration。以下各节分别解释每个
migration 的领域约束，但不是五次可独立启动业务的部署窗口。

从 `000037` 升级到本版本必须采用一个统一停机窗口：

1. 固定已经通过 required CI 的 release commit 和本次实际运行的全部镜像 digest。
   客户正式包必须固定六个 digest 镜像；已批准的 development 目标也至少记录
   stable/canary/frontend 及未变基础服务的实际 digest。确认待发布 backend 只会运行
   `000038` 到 `000042` 这一条连续 migration 链，并先在与目标数据量接近的副本上评估
   锁等待和迁移耗时；
2. 在任何写入停止前完成下述各节要求的全部只读预检，包括 Guardrail 四类 quota、
   active stream lease、delivery-attempt/quarantine 影响范围以及本地登录 ID 的 blank、
   untrimmed 和 collision 计数。任一拒绝条件非零都先停止发布并由有权人员处置，不让
   migration 猜测删除、改名或补绑定；
3. 阻断新的管理登录、控制面和 gateway 流量，drain 并停止全部旧 stable/canary
   backend，以及 admin、gateway、stream/terminal/settlement recovery、outbox、Agent
   capability、workload admission 和所有其他数据库/Redis writer；随后备份 PostgreSQL，
   记录 preflight 计数和镜像 digest；
4. 只用待发布二进制执行一次受支持的 `migrate up`。不得在版本间手工执行 SQL、补写
   ledger，或启动任何业务副本；
5. 每个 migration 与自己的 ledger 行在独立事务中提交。如果后续 migration 拒绝，
   已成功的较早版本不会自动回滚。此时继续保持全部流量和 writer 停止，保留 ledger
   与证据，修正准确拒绝条件后重跑同一个 `migrate up`；它会跳过已登记版本并继续
   roll-forward。不得为了回到“整齐版本”而删除 ledger、quarantine、lease、会话或
   账户；
6. 只有 `schema_migrations` 连续读回 `000038` 至 `000042`，且各节列出的 function、
   index、constraint、quarantine、fence、会话与配置指纹验证全部通过后，才能启动同一
   release commit 的新副本；先完成无流量 smoke，再逐步恢复入口。

若目标已经包含其中部分 migration，也使用同一原则：把实际 ledger 与待发布 manifest
的差集作为一个不可拆分的非滚动窗口，不在 pending 版本之间启动。以下分节中的
“应用 migration”均受本统一流程约束。

### 3.9 V1 `000038` 管理会话撤销非滚动启用

`000038_v1_admin_session_revocation` 把管理员 JWT 从纯客户端持有的无状态令牌改为
服务端可撤销会话，并用用户级 `auth_epoch` 让改密和受控密码重置立即失效全部旧
会话。新令牌带随机 `sid`；数据库只保存其 SHA-256，不保存可直接使用的 bearer 值。
旧二进制既不检查会话行，也不检查 `auth_epoch`，因此旧、新 backend 不能混跑：
否则已经注销或改密失效的令牌仍可能被旧副本接受。

安全切换顺序是：

1. 固定待发布 image/commit，确认所有 stable、canary 和管理 API 副本使用同一
   `JWT_SECRET`，并备份 PostgreSQL；
2. 阻断新的管理端登录和写请求，drain 后停止全部旧 backend；不能保留旧 canary
   承接少量流量；
3. 应用 `000038`，只启动认识 `sid`/`auth_epoch` 的新版本。迁移前签发的无状态 JWT
   会 fail closed，管理员需要重新登录；
4. 在恢复管理流量前验证本地和 OIDC 登录均创建 32-byte `sid_hash`，JWT 原文和
   `sid` 未进入数据库/日志；验证同一令牌 logout 重试两次均返回成功、注销后的受保护
   API 返回 `401`，以及改密后该用户所有旧会话均返回 `401`、重新登录可用；
5. 再恢复管理流量并观察登录失败、会话落库失败、`401` 和审计写入。业务 API Key
   鉴权不使用此管理员会话表，不应被混为一类。

down 只允许在严格逆序、且新版本从未签发会话或产生密码撤销纪元的预激活窗口执行。
存在任何后续 migration、任意 `admin_sessions` 行或任意非零 `auth_epoch` 时，down 会
明确拒绝并保留证据。更严格的运维边界是：一旦新 backend 已经承接管理流量，即使
数据库 guard 尚未观察到会话写入，也把恢复视为 roll-forward；不得删除会话、清零
`auth_epoch` 或恢复旧 JWT 接受逻辑来迁就旧镜像。仓库测试只证明代码、迁移 guard
和本地数据库行为，不代表任何目标环境已经执行该切换。

### 3.10 V1 `000039` Guardrail 租户隔离与公平调度非滚动启用

`000039_v1_guardrail_tenant_isolation` 为平台策略和每个租户策略建立独立运行时
快照边界，并在数据库中原子限制活跃平台策略、每组织活跃策略、总缓存策略和活跃租户
scope。它还持久化只含组织、请求、策略、原因、时间窗和丢弃计数的 advisory gap
聚合证据。旧版本的全局快照或无租户公平性的 scheduler 不能与新版本混跑，否则一个
异常租户仍可影响其他租户，且 gap 证据语义不一致。

当前 scheduler 固定上限为全局并发 `64`、每组织并发 `4`、全局队列 `4096`、
每组织队列 `64`、单任务 `256 KiB`、全局排队正文 `32 MiB`、每组织排队正文
`1 MiB`；这些是当前二进制合同，不是本节建议运维人员临时调大的容量旋钮。

安全切换顺序是：

1. 固定待发布 image/commit，确认所有副本使用完全相同的四项 quota；默认分别为每组织
   活跃策略 `256`、平台活跃策略 `256`、总缓存策略 `4096`、活跃租户 scope `4096`；
2. 备份 PostgreSQL，停止入口流量并 drain/停止所有旧 gateway/control-plane；保留
   disabled 历史，不通过删除策略来满足配额；
3. 按平台、组织、总缓存和活跃租户 scope 读回当前计数。任何已超目标配额的 scope
   必须先由产品/运维明确处置，不能靠迁移猜测删除；
4. 应用 `000039`，只启动具备 scoped snapshot、租户公平队列和 gap writer 的新版本；
5. 验证一个无效/超限租户只对自身 fail closed，平台 scope 无效仍全局 fail closed；
   验证跨租户轮转、全局/组织并发与队列上限，以及 gap 聚合幂等读回；
6. 恢复流量后监控 quota 拒绝、advisory overload/timeout/shutdown 和未持久化 shutdown
   计数。gap receipt/batch 不保存模型正文，但 V1 不自动清理，保留周期由部署方制定。

存在 gap receipt/batch 历史时 down 会拒绝。不得删除策略历史或 gap 证据来迁就旧版本；
激活后按 roll-forward 处理。仓库测试不代表目标环境已经执行该切换。

### 3.11 V1 `000040` / `000041` 投递尝试绑定与流式 generation fencing

`000040_v1_delivery_attempt_binding` 把 reservation、投递回执、结算和恢复绑定到一个
不可变 provider attempt。历史数据只有在数据库证据能唯一确定 attempt 时才回填；
歧义 reservation/receipt 被完整保留到 `delivery_attempt_quarantine` 和
`request_delivery_receipt_quarantine`，相关 pending 原因改为 `ambiguous_attempt`，不会
被自动恢复或猜测收费。`000041_v1_stream_lease_fencing` 再以
`{org, request, attempt, owner, generation}` 隔离旧 stream worker，并要求 checkpoint、
terminal receipt 与 settlement recovery 在副作用提交前证明当前 authority。

安全切换顺序是：

1. 固定待发布 image/commit并备份 PostgreSQL；停止新入口，drain/停止所有旧 gateway、
   stream recovery、terminal recovery、settlement/outbox 和其他相关写入者；
2. 确认不存在仍为 `active` 的 `stream_delivery_leases`。`000041` 即使租约时间已过也会
   拒绝 active 行；应先用受支持的恢复/终态流程排空，禁止手工改状态或删除 lease；
3. 使用待发布二进制执行一次受支持的 `migrate up`，让所有 pending migration 按
   ledger 顺序执行。不要暂停在 `000040` 后改用人工 SQL 或手工补 ledger；
4. 全部 pending migration 完成后、启动流量前，读回两个 quarantine 表的行数、
   request/attempt 候选与 `settlement_pending_reason='ambiguous_attempt'` 数量；把它们
   进入人工账务复核，不得为了启动而删除或补猜 attempt。此时只启动认识 attempt
   binding、owner/generation、wall-clock expiry 和 durable settlement claim 的新版本；
5. 恢复流量前验证合法 non-stream/stream 各一次，确认 attempt 一致；制造旧 owner 或
   旧 generation 冲突，必须在 checkpoint、receipt 和 settlement 前失败；再验证当前
   recovery owner 只形成一次终态与一次准确结算；
6. 持续监控 quarantine、`ambiguous_attempt`、lease conflict、checkpoint conflict 和
   settlement recovery。000040/000041 激活后的金融/投递证据不得为 down 而删除；
   恢复策略是修复后 roll-forward。

每个 migration 与自己的 ledger 行各自在一个独立事务中提交。如果 `000040` 已成功、
而 `000041` 因 active lease 拒绝，`000040` 会保持已提交；此时继续停流量，按受支持
流程排空 lease 后重跑 `migrate up`，让它跳过已登记的 `000040` 并 roll-forward
`000041`。不得手工删除 ledger/quarantine，也不得先 down `000040`。任何人工 down
都必须严格按版本逆序且满足各 down guard；产生新金融/投递证据后默认只允许
roll-forward。这两个 migration 会持有较强表锁，执行窗口必须按目标数据量评估；
本地真实 PostgreSQL 验证不能替代目标规模的锁等待和迁移耗时预演。

注意 `000041.up` 只要求切换时不存在 `active` lease，但 `000041.down` 会在存在任意
`stream_delivery_leases` 行（包括 inactive 历史）或任何 fenced 副作用证据时拒绝。
因此一旦新版本产生租约历史，实际恢复路径就是保留证据并 roll-forward。

### 3.12 V1 Agent capability 与 workload admission 非滚动切换

C1.1 把 Agent refresh grant 和 superseded marker 移到 workflow Redis hash slot，
并在写入 child/topology 的同一 Lua transition 内检查父 token；新版本只读兼容旧 key，
不会继续写旧 layout。D1 则把新 workload admission 改为 Redis 不确定时 fail closed，
并把共享配置指纹升级为 `admission-v2|redis=fail-closed`。旧写入者无法参与这两个新
原子合同，因此不能依赖混合版本长期兼容。

切换时先停止新 Agent/workload 入口，drain 并停止全部旧 gateway/capability issuer，
再只启动新版本。不要批量删除 Redis key：既有 Agent legacy grant/marker 由 token TTL
自然过期，新 reader 在有界窗口只读兼容；既有 workload Redis reservation 仍可由原
请求释放，新版本不会再创建本地 fallback。恢复流量前验证：父 token refresh 与 child
注册竞态必须拒绝 child 且不增加拓扑计数；健康 Redis 下多副本容量仍一致；Redis
不可用时新请求返回 `state_unavailable`/重试语义且没有 provider dispatch；所有副本
报告同一 admission v2 配置，否则配置冲突应 fail closed。仓库证据不代表目标环境已
完成该 drain/readback。

### 3.13 V1 `000042` 全局本地登录 ID 非滚动切换

本地密码登录页只有“用户名 + 密码”，没有组织选择器。因此 `000042` 把本地账户
登录 ID 固定为跨组织全局唯一、忽略外层空白且大小写不敏感；OIDC 身份继续由
issuer/subject 识别，不进入这个本地 namespace。同组织 OIDC 与 local 允许使用相同
显示名；外部身份自身仍通过 provider-scoped partial index 保持同组织显示名唯一，
但显示名不是 OIDC 的认证键。CLI 保存时只去除 username 外层空白并保留显示大小写，
数据库函数 `public.normalize_local_login_id(text)` 是本地登录比较和唯一性的最终权威。

这是非滚动切换。迁移前停止新的登录/用户管理写请求，drain 并停止所有旧 admin API、
本地登录和管理 CLI writer，再备份 PostgreSQL。只读预检仅输出计数，不列出 username、
组织或密码：

```sql
WITH local_identities AS (
    SELECT username,
           lower(btrim(
               username,
               U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000'
           )) AS login_id
      FROM users
     WHERE COALESCE(NULLIF(auth_provider, ''), 'local') = 'local'
)
SELECT COUNT(*) FILTER (WHERE login_id = '') AS blank_count,
       COUNT(*) FILTER (WHERE lower(username) IS DISTINCT FROM login_id) AS untrimmed_count
  FROM local_identities;

WITH local_identities AS (
    SELECT lower(btrim(
               username,
               U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000'
           )) AS login_id
      FROM users
     WHERE COALESCE(NULLIF(auth_provider, ''), 'local') = 'local'
)
SELECT COUNT(*) AS collision_groups
  FROM (
        SELECT login_id
          FROM local_identities
         GROUP BY login_id
        HAVING COUNT(*) > 1
       ) AS conflicts;
```

任一计数非零都必须停止并由有权人员逐个解决；不得自动改名、选择“赢家”或删除账户。
`000042` 会取得 `users` 的 `ACCESS EXCLUSIVE` 锁并再次执行同样的 preflight，任一
`blank_count`、`untrimmed_count` 或 `collision_groups` 非零都会让 migration 与 ledger
整事务回滚。清理完成后重跑 `migrate up`，只启动新版本。

恢复管理流量前验证：

```sql
SELECT to_regprocedure('public.normalize_local_login_id(text)');
SELECT indexname, indexdef
  FROM pg_indexes
 WHERE indexname IN ('users_local_login_id_unique', 'users_external_org_username_unique')
 ORDER BY indexname;
SELECT conname, convalidated
  FROM pg_constraint
 WHERE conname IN ('users_local_login_id_nonblank', 'users_local_login_username_canonical');
SELECT version FROM schema_migrations WHERE version = '000042_v1_local_login_identity';
```

确认 function、两个 validated CHECK、local/external 两个 partial unique index、旧
`users_org_username_unique` constraint 已移除且 ledger 存在后，再验证同一 local ID
的大小写/外层空白登录、跨组织重复 local 创建的通用冲突响应、同组织同显示名的
local 与 OIDC 身份可并存且密码登录只选择 local、并发首次 OIDC 登录收敛到同一身份，
以及 OIDC 不受本地密码重置影响。管理 CLI 的以下两个秘密输入面是唯一受支持方式：
交互 TTY 双次无回显确认，或 `docker compose exec -T ... --password-stdin` 从受控单行
文件读取。旧
value-bearing `--password`、位置参数密码、非 TTY 无 flag、TTY 强制 stdin flag 和
多行/超长输入都必须在数据库访问前失败；不存在密码环境变量回退。

down 只允许在未对外激活且严格逆序的明确回退窗口执行；存在任何 version 大于
`000042_v1_local_login_identity` 的 migration ledger 时会拒绝。因为旧 constraint
无法表达 provider scope，down 还会先统计 `(org_id, username)` 重叠；只要新合同下
已经存在同组织 cross-provider 同显示名，整个 down 会原子拒绝，不改任何 index、
constraint、function 或 ledger。只有不存在这类重叠时，成功 down 才恢复旧
`users_org_username_unique`，删除两个 partial unique index、两个 CHECK、规范化函数和
本 migration ledger，从而重新引入本地身份歧义风险。一旦新版本承接登录或用户管理
流量，优先保留身份合同并 roll-forward，不为迁就旧二进制删除账户或约束。

### 3.14 `000078` 原子 sealed-candidate authoring 非滚动切换

升级前先 drain 并停止所有 M77 deployment authoring writer，固定仍可回退启动的
M77 image digest，再只读检查 canonical v2 draft：

```sql
SELECT COUNT(*) AS unsealed_v2_drafts
  FROM runtime_change_releases
 WHERE candidate_version='unirouter.runtime-change-candidate.v2'
   AND (route_items_sealed_at IS NULL OR stage='draft');
```

计数必须为零才可启动 M78 migration。`000078` 会在取得迁移表锁后、任何 schema
mutation 前重复检查；发现一行就整事务拒绝，因此数据库和 ledger 仍保持 M77。此时
不要启动 M78 runtime，也不要用 SQL 补 receipt、改 stage、删 event 或禁用 trigger。
使用刚才固定的 M77 artifact，通过其正式控制面调用 `SealDeploymentDraft`，逐条完成
draft 的 current revision/evidence 复核和 validate event；全部成功后再次运行只读检查，
再用 M78 artifact 执行一次 `migrate up`。升级后的这些 historical sealed v2 release
保持 `authoring_receipt_id IS NULL`；只有 M78 新 authoring 才必须绑定 durable receipt。

若任一 M77 draft 已无法通过正式 Seal，M78 不会替它制造 provenance。只有目标本来就
在已批准的全量数据重置范围内，才能继续使用同一个 M77 artifact，按
`docs/runbooks/SECOND-INTEGRATION-POC.md` 的 exact reset preview、备份、独立恢复证明、
typed confirmation 和 apply 全流程清空后再升级/re-author；该 reset 会删除产品数据，
不是单条 release 的修复工具。生产或其他未获全量 reset 授权的目标必须停在 M77，
从备份恢复或另行审批处置，禁止手工 DELETE/UPDATE 绕过。当前 M78 binary 的 legacy
`PrepareDeploymentDraft` / `SealDeploymentDraft` 仅 fail closed，不能替代旧 artifact。

### 3.15 M78 release workspace、legacy cutover 与止损回退

M78 只支持新的原子 authoring 面。先以 `platform_admin`、`org_admin` 或
`org_viewer` 读取：

```http
GET /api/v1/model-deployment-releases
Authorization: Bearer <admin-jwt>
```

该读取不要求 Gateway License，并在同一个只读 Repeatable Read snapshot 中返回每个
public model 的 server-owned `authoring.baseline`、slot、stable release、可选择的
deployment/operation 以及已有 actionable v2 release。`authoring.available=false` 时只按
`reason_code` 处理；不得在客户端猜测 slot/release revision、parent、hash、profile、
binding 或 release ref。`assigned_public_model_id` 未赋值表示仍是 raw v1 slot，不等于
找不到逻辑 public model。存在 identity ambiguity、canary transition、retired model 或
没有 eligible operation 时禁止 author。

`platform_admin`/`org_admin` 且 Gateway License 有效时，使用一个新的、非秘密的
`Idempotency-Key`，把 collection 返回的 baseline 原样回送，并只选择允许的
deployment/operation：

```http
POST /api/v1/model-deployment-releases
Authorization: Bearer <admin-jwt>
Idempotency-Key: <unique-authoring-key>
Content-Type: application/json

{
  "public_model_id": 123,
  "baseline": {
    "kind": "stable",
    "expected_slot_version": 7,
    "stable_release_ref": "rcr_example-stable",
    "expected_stable_release_version": 4
  },
  "selections": [
    {"deployment_id": 456, "operation_id": "openai.chat_completions.v1"},
    {"deployment_id": 456, "operation_id": "openai.embeddings.v1"}
  ],
  "reason": "prepare the reviewed canonical release"
}
```

这里的数字和 ref 只是 payload 形状示例，实际值必须来自刚读取的同一 server
snapshot。`initial` baseline 的其余 revision/ref 字段必须为零或省略；`stable` 和
`legacy_cutover` 必须完整回送三个 optimistic baseline 字段。请求体必须是单个、最多
16384 bytes 的 strict JSON object；duplicate/unknown member 和 trailing JSON 会在
receipt access 前拒绝。

首次成功返回 `201 Created`、`Location: /api/v1/model-deployment-releases/{releaseRef}`
和 immutable authoring receipt。相同 authoring scope、actor、key 与 normalized payload 的
后续 exact replay 返回 `200 OK`、相同 `Location`、
`X-UniRouter-Idempotency-Replayed: true` 和
`X-UniRouter-Original-Request-ID`。另一个 payload 复用同一 key 返回 409。Authoring
只在一个事务内创建并 seal 到 `validated`；它不会 activate、启动 canary 或修改 slot。

随后读取 `Location`，以返回的 `actions[]` 作为唯一 UI/运维动作事实：

```http
GET /api/v1/model-deployment-releases/{releaseRef}
Authorization: Bearer <admin-jwt>
```

普通 canonical lineage 返回五个动作描述符：`activate_initial`、`start_canary`、
`promote`、`cancel_canary`、`rollback_stable`。精确 sealed v1 stable parent lineage 只返回
`cutover_legacy`、`rollback_legacy`，不同时展示普通 canary/rollback 按钮。每个写操作
都使用新的 action-scoped `Idempotency-Key`，只提交 descriptor 的 `required_fields`。
409 后重新 GET collection/detail 并由人复核；不得自动重试陈旧 baseline。

legacy cutover 请求形状为：

```json
{
  "expected_release_version": 2,
  "expected_slot_version": 7,
  "reason": "switch the reviewed legacy stable route to its canonical equivalent"
}
```

`POST .../{releaseRef}/cutover-legacy` 是 Gateway License 扩张操作。事务必须证明：slot
仍指向该 release 的精确 sealed v1 parent；v2 manifest 的 operation set 精确为 Chat
Completions 与 Embeddings；legacy 与 canonical 当前全部可执行候选、provider/upstream、
adapter、routing、breaker、credential eligibility、probe/revision 等计划事实相同，且
secret-free plan hash 完全一致。成功后 v1 parent、v2 target、raw slot、events、audit、
qualification snapshot 和 lifecycle receipt 一起提交；该动作不修改
`MODEL_ROUTE_AUTHORITY`，fleet drain 与进程模式切换仍按独立发布边界执行。

`POST .../{releaseRef}/rollback-legacy` 使用相同三个必填字段，是
`platform_admin`/`org_admin` 可在 Gateway License 缺失或过期时执行的止损收缩。它故意
不检查已经损坏的 v2 target health、qualification 或 parity；但必须重新证明 exact
target/slot lineage、保留的 sealed v1 parent 以及该 v1 parent 当前完整 Chat+Embeddings
计划仍可执行。成功后 slot 的 assigned public model 从 canonical ID 原子恢复为 raw v1
`NULL`，v2 target 回到 validated，v1 parent 恢复 active，receipt 保留原始 plan/evidence
链。不要用 SQL 手改 slot/release stage 来模拟 rollback。

M78 downgrade 不是常规回滚路径。执行任何 down 前至少只读核对：

```sql
SELECT COUNT(*) AS legacy_lifecycle_receipts
  FROM model_deployment_lifecycle_receipts
 WHERE action IN ('cutover_legacy','rollback_legacy');
SELECT COUNT(*) AS authoring_receipts
  FROM model_deployment_release_authoring_receipts;
SELECT COUNT(*) AS receipt_linked_releases
  FROM runtime_change_releases
 WHERE authoring_receipt_id IS NOT NULL;
SELECT COUNT(*) AS public_execution_bindings
  FROM model_operation_public_execution_bindings;
SELECT version
  FROM schema_migrations
 WHERE version > '000078_atomic_model_release_authoring'
 ORDER BY version;
```

任一 legacy lifecycle receipt、任一 M78 authoring receipt/link、任一 public execution
binding 或任一更高 migration ledger 都会让 `000078` down 在 schema mutation 前原子
拒绝；不得删除 immutable evidence 来迁就旧 binary，应保留 M78 schema 并 roll
forward。已获完整 second-integration reset 授权时，现有 exact reset v3 合同已把
`model_deployment_release_authoring_receipts`、lifecycle receipt 三表、release/items/
bindings/events/audit 纳入 reset candidate 和 trigger-safe 删除顺序；仍必须遵循
`docs/runbooks/SECOND-INTEGRATION-POC.md` 的 preview、backup、独立恢复证明、typed
confirmation、apply 与 bootstrap readback，不能把全量 reset 当作单条 release 清理。

### 3.16 `000079` canonical connection License authority 与 guarded down

M79 只切换商业 License v1 的 `active_channels` 计数权威：字段名和签名 payload
保持不变，运行时改为统计 `provider_connections.admin_state='enabled'`，不再统计
legacy `channels.status='active'`。升级前必须 drain 所有仍能写 legacy/verify model
state 的旧进程；migration 本身不会安装 legacy DML seal，也不能证明 fleet 已退出。
这是非滚动 schema 切换。M79 binary 无论执行 migration 还是设置
`SKIP_MIGRATION=true`，都会在 customer baseline 与 HTTP bind 前要求完整、有序
database migration ledger 与 M79 binary artifact ledger 完全一致；missing、extra、
future、duplicate、reordered 或空 ledger 全部拒绝。
因此不能让 M78 stable 与 M79 canary 共享 M78 数据库并声称 ready。顺序固定为：

1. 阻断入口，drain 并停止所有 M78 stable、canary 和其他 backend 副本；
2. 只允许一个批准的 M79 migration owner 在 `SKIP_MIGRATION=false` 下升级数据库；
3. 只读确认完整 ledger 与 M79 artifact 完全一致，再启动 M79 stable；
4. 仅在同一数据库已有 exact M79 ledger 后，才可用 `SKIP_MIGRATION=true` 启动 M79
   canary/secondary，并分别读取 readiness。

在维护窗口内先只读对比：

```sql
SELECT COUNT(*) AS enabled_canonical_connections
  FROM provider_connections
 WHERE admin_state='enabled';
SELECT active_channels AS signed_limit
  FROM commercial_license_entitlements
 WHERE singleton=TRUE;
```

`signed_limit` 非空且小于 enabled canonical count 时，M79 会在任何 trigger
切换前以 `commercial_license_active_channels` 原子拒绝。成功 migration 的锁序是：

1. shared model-execution migration advisory lock；
2. `provider_connections`、`channels`、`schema_migrations` 的
   `SHARE ROW EXCLUSIVE` relation locks；
3. `unirouter.commercial_limit.active_channels` resource advisory lock；
4. `commercial_license_entitlements` 的 `SHARE ROW EXCLUSIVE` relation lock；
5. preflight 后删除 legacy channel trigger、替换 resource function，并创建
   canonical provider-connection trigger。

relation/resource lock 被刻意拆开：connection activation 先取得 resource-table
RowExclusive lock 再取 resource advisory lock，License renewal 则先取 advisory lock
再写 entitlement；上述 migration 顺序同时等待两者而不制造反向 lock cycle。禁止
为了“解除卡住”而手工 disable trigger、删除 entitlement 或跳过 migration ledger。

M79 down 不是普通模式回退。它使用同一锁序，并在 trigger mutation 前同时拒绝：

- 任一高于 `000079_canonical_connection_license_authority` 的 migration；
- 任一 enabled canonical connection 没有一条 exact active legacy projection；
- 任一 projection 的 name/provider/base URL/adapter/config/config revision 与 legacy
  routing epoch 不一致；
- 恢复后的 active legacy channel count 超过签名 `active_channels`。

可用下列只读检查提前暴露前两类数据阻塞；最终权威仍是 down transaction 内的
重复校验：

```sql
SELECT version
  FROM schema_migrations
 WHERE version > '000079_canonical_connection_license_authority'
 ORDER BY version;

SELECT COUNT(*) AS enabled_without_exact_active_legacy_projection
  FROM provider_connections connection
  LEFT JOIN channels legacy ON legacy.id=connection.legacy_channel_id
 WHERE connection.admin_state='enabled'
   AND (
     connection.legacy_channel_id IS NULL OR legacy.id IS NULL OR
     legacy.status<>'active' OR
     connection.name IS DISTINCT FROM legacy.name OR
     connection.provider IS DISTINCT FROM legacy.provider OR
     connection.base_url IS DISTINCT FROM legacy.base_url OR
     connection.adapter_type IS DISTINCT FROM
       COALESCE(NULLIF(BTRIM(legacy.adapter_type),''),'openai_compatible') OR
     connection.adapter_config IS DISTINCT FROM
       COALESCE(legacy.adapter_config,'{}'::jsonb) OR
     connection.config_revision IS DISTINCT FROM legacy.routing_epoch
   );
```

通过 guarded down 只会恢复 M59 `channels_commercial_limit` function/trigger 并移除
M79 ledger；canonical connections、deployments、credential bindings、release 和
execution evidence 均保留。若旧 binary 无法理解这些 retained schema/data，停止并
roll forward，不得删除 canonical 证据来制造可启动假象。

### 3.17 `000080` observation provider-cost reconciliation 与 guarded down

M80 只修复 `cost_observation` 请求已形成 provider-cost hold 后无法通过正式接口终结
的死路。`collect` 在 observation 模式仍然拒绝；平台管理员只能在现有
`refund`/`subsidy` 权威下释放 hold。成功操作会：

- 保留 provider reported usage、冻结价格与 calculated cost；
- 将 reservation 终结为 `cancelled`，provider attempt 终结为 `reconciled`；
- 写一条 append-only `billing_observation_receipts` 零收费收据；
- 在 `provider_cost_reconciliations` 写 requested/completed 两阶段证据；
- 不创建 `transactions`，不修改 wallet balance/frozen/spent。

M80 将 completed reconciliation 的终态权威冻结为 XOR：预付费模式引用
`terminal_transaction_id`，observation 模式引用
`terminal_observation_receipt_id`。deferred database guard 会复核 requested
action/actor/reason/evidence、reservation、attempt 和 terminal receipt 精确一致；禁止
手工补零金额 transaction、直接 UPDATE evidence 或绕过 pending-key admission。

M79→M80 仍按 schema-ceiling 非滚动顺序执行：停止旧 fleet，由唯一 migration owner
升级，确认数据库完整有序 ledger 与 M80 artifact ledger 完全一致，再启动 M80
stable/canary。不能只看 `MAX(version)`。M80 down 在 schema mutation 前拒绝任一 later
migration 或任一 observation reconciliation terminal evidence；安全 down 只恢复 M79
terminal check，不删除既有 prepaid reconciliation。可先只读检查：

```sql
SELECT version FROM schema_migrations ORDER BY version;
SELECT COUNT(*) AS observation_reconciliation_terminals
  FROM provider_cost_reconciliations
 WHERE terminal_observation_receipt_id IS NOT NULL;
```

第二项非零时必须 roll forward，不得删除 append-only receipt/reconciliation 来迁就旧
binary。

### 3.18 `000081` versioned model execution profiles 与 guarded down

M81 为 `model_registry` 增加精确的 `execution_profile_id` 与
`execution_profile_version`。升级只给历史记录写入
`unverified.chat@1.0.0`，不会按 `model_name` 猜测 MiniMax、Kimi、DeepSeek、
Tokenizer、上下文上限或供应商能力。只有操作员在控制台或 API 选择仓库审核过的
Profile，模型才获得对应的 Tokenizer、reasoning、cache-usage、protocol 身份与供应商
最大值；客户 `context_length`/`output_limit` 仍是可修改的生效策略，但不能超过该
Profile。

M80→M81 是 schema-ceiling 变化，必须停止旧 fleet，由唯一 migration owner 升级并
确认完整 ledger 精确等于 M81 artifact，再启动 M81 stable/canary。不能让 M80 stable
和 M81 canary 共用数据库。升级后先检查历史行仍为 unverified，再由人工逐模型选择
已核验 Profile：

```sql
SELECT version FROM schema_migrations ORDER BY version;
SELECT id, model_name, context_length, output_limit,
       execution_profile_id, execution_profile_version
  FROM model_registry
 ORDER BY id;
```

M81 down 在任何 schema mutation 前拒绝 later migration，也拒绝任一非
`unverified.chat@1.0.0` 的持久 Profile 选择。若需要回滚 binary，优先 roll forward；
不得删除或伪造模型 Profile 证据来迁就旧 binary。只有确认所有模型仍是 unverified、
且没有 later migration 时，guarded down 才会移除两列并恢复 M80 schema。

Profile 本身也按版本演进，不能原地改写已选择的旧身份。
`kimi.k3.api@1.2.0` 使用 `reasoning.kimi-k3-effort.v1`、
`cache-usage.kimi-platform.v1` 和 `protocol.kimi-openai-chat.k3.v2`：缺省
`reasoning_effort` 时保留字段缺失，让 Kimi 使用供应商默认 `max`；显式值只允许
`low/high/max`。只有该 cache-usage 合同声明的顶层 `usage.cached_tokens` 才会成为
缓存读取计费证据，通用 OpenAI-compatible 路由不能凭同名未知字段获得折扣。K3 的
Partial Mode、system-message 动态工具、内置联网搜索和内联多模态只在 v2 typed
contract 中可执行；供应商 URL/file media 与大型载荷仍不属于该合同。

当前 `kimi.k2.6.api@1.5.0` 使用新的
`protocol.kimi-openai-chat.v3`，允许 K2.6 在 thinking 开启或由供应商默认时使用
`$web_search`；历史 v2 合同仍保持“必须显式关闭 thinking”的不可变语义。
`kimi.k3.api@1.5.0` 继续使用 K3 v2 合同并开放同一计量能力。两者都只允许一次串行、
非流式搜索，以便响应解析器产生权威实际调用量；K2.7 Code 当前 Profile 不声明搜索。
切换已持久化模型时必须选择这些新 Profile 版本，不能覆写旧 Profile 或协议合同。

### 3.19 `000082` sealed model execution contracts 与 guarded down

M82 不新增第二套发布生命周期。它在现有 canonical v2
`runtime_change_releases` 上密封 `execution_profile_id/version` 以及生效的
`public_context_limit`/`public_output_limit`。升级时，既有 v2 Release 以迁移边界
当时的 `model_registry` 值完成一次性 backfill；此后新 Release 必须与锁内模型行
精确一致，四项均不可修改。Canonical Planner 只读当前 stable/canary Release 的
密封值并把它们纳入 snapshot hash。修改模型目录只影响下一次 Author，只有新
Release 经过 canary/promotion 后才改变线上行为；运行时安全检查仍可缩小上限，不能
扩大。价格版本仍由 Billing Freeze 独立固定，不进入 Release Profile。

M81→M82 是 schema-ceiling 变化，必须停止 M81 fleet，由唯一 migration owner
升级并确认完整 ledger 精确等于 M82 artifact，再启动 M82 stable/canary。不得让
M81 stable 与 M82 canary 共用数据库。升级后只读检查：

```sql
SELECT version FROM schema_migrations ORDER BY version;
SELECT release_ref,stage,execution_profile_id,execution_profile_version,
       public_context_limit,public_output_limit
  FROM runtime_change_releases
 WHERE candidate_version='unirouter.runtime-change-candidate.v2'
 ORDER BY id;
```

所有 v2 行必须有合法 Profile 和正数上限，且 output 不大于 context。要更换 Profile
或上限，必须在发布工作台 Author 新 Release；禁止直接 UPDATE release pins。

M82 down 在 schema mutation 前拒绝 later migration；若任一 durable v2 Release 的
密封值与当前 `model_registry` 不完全相同，也会拒绝，因为 M81 会重新回到可变目录
lookup。出现该拒绝时应 roll forward，不得删除历史 Release、receipt/event 或改写
pins 来迁就旧 binary。只有所有 durable pins 在 M81 语义下仍完全等价时，guarded
down 才能删除四列并恢复 M81 schema。

### 3.20 `000083` versioned unit-meter billing 与 guarded down

M83 不改变 M82 canonical Release 或 traffic authority。它为现有 Freeze、费用观察、
usage finalization journal 和 call log 增加四类证据：冻结的绝对 unit rate、预留
meter quantity、供应商终态实际 meter quantity 以及 non-token micro-CNY cost。首个
受支持 meter 是 Kimi `$web_search` 对应的 `builtin_web_search_calls`；只有 exact
execution profile 和响应条件共同成立时才产生一次实际调用证据。

M82→M83 是 schema-ceiling 变化。必须先完成备份/恢复清单检查，停止全部 M82
backend，由唯一 migration owner 升级并确认 ledger 精确等于 M83 artifact，再启动
M83 stable/canary。不得让 M82 stable 与 M83 canary 共用数据库。升级后只读检查：

```sql
SELECT version FROM schema_migrations ORDER BY version;
SELECT column_name
  FROM information_schema.columns
 WHERE table_name='freeze_idempotency'
   AND column_name IN ('effective_unit_rates','reserved_meter_usage','meter_usage','non_token_cost_micros')
 ORDER BY column_name;
```

四列必须全部存在。部署前为将使用 `$web_search` 的精确 `model_ref` 发布经 maker/checker
批准的绝对单价 Rate Card；缺少 `builtin_web_search_calls` 组件时请求应在 dispatch
前 fail closed，不得退回 token-only 或比例价格。目标 smoke 应核对 Freeze、journal、
observation/receipt 与 call log 的 meter、rate component hash 和 cost 一致。

M83 down 在任何 schema mutation 前拒绝 later migration；任一相关表已有非空 unit
rate/meter evidence、非零 non-token cost，或来源表已经记录
`versioned_rate_card_components.v1` 证据时也拒绝。出现拒绝应 roll forward，不得
删除或改写价格来源/客户费用证据来迁就 M82 binary。只有确认四个计费表均未形成
durable M83 证据且没有新版 unit-contract source evidence 时，guarded down 才能恢复
M82 schema。

### 3.21 `000084` official rate-source currency drafts 与 guarded down

M84 只把 `enterprise_finance_rate_source_evidence.currency` 从 `CNY` 放宽为单一
`CNY|USD`，用于保留 Gemini、OpenAI、xAI、Anthropic 的官方原币审核草稿。Rate Card
审批、激活、request quote、钱包、Freeze/settlement 和 Outbox 仍要求 micro-CNY；USD
草稿必须在 approval 阶段 fail closed，不得做隐式汇率转换。

M83→M84 是 schema-ceiling 变化，必须停止全部 M83 backend，由唯一 migration owner
升级并确认 ledger 精确等于 M84 artifact，再启动 M84 stable/canary。不得让 M83 与 M84
进程共用数据库。升级后只读检查：

```sql
SELECT version FROM schema_migrations ORDER BY version;
SELECT pg_get_constraintdef(oid)
  FROM pg_constraint
 WHERE conname='enterprise_finance_rate_source_currency_check';
```

约束必须只允许 `CNY` 与 `USD`。smoke 应导入一个 USD `provider_list` draft，确认来源
证据保留 USD、草稿不自动批准/激活，并确认 approval 返回 CNY request-pricing 未就绪。
M84 down 在 schema mutation 前拒绝 later migration 或任何非 CNY 来源证据；存在 USD
草稿时必须 roll forward，不得删除来源证据来迁就 M83 binary。

---

## 四、备份与恢复

### 4.1 手动备份

```bash
umask 077
install -d -m 700 backups
chmod 700 backups

# 备份 PostgreSQL
postgres_backup_file="$(mktemp "$PWD/backups/postgres.XXXXXX")"
chmod 600 "$postgres_backup_file"
docker compose exec -T postgres pg_dump -U uniroute uniroute > "$postgres_backup_file"
test -s "$postgres_backup_file"
printf 'PostgreSQL backup retained at %s\n' "$postgres_backup_file"

# 备份 Redis RDB；等待后台快照完成后再复制
docker compose exec redis redis-cli BGSAVE
redis_bgsave_state=1
for _ in $(seq 1 60); do
  redis_bgsave_state="$(docker compose exec -T redis redis-cli --raw INFO persistence \
    | tr -d '\r' | awk -F: '/^rdb_bgsave_in_progress:/ {print $2}')"
  [ "$redis_bgsave_state" = 0 ] && break
  sleep 1
done
test "$redis_bgsave_state" = 0
docker compose exec -T redis redis-cli --raw INFO persistence \
  | tr -d '\r' | grep -q '^rdb_last_bgsave_status:ok$'
docker compose cp redis:/data/dump.rdb ./backups/redis_$(date +%Y%m%d_%H%M%S).rdb
```

### 4.2 恢复

```bash
# 恢复 PostgreSQL
cat backup.sql | docker compose exec -T postgres psql -U uniroute uniroute

# 恢复 Redis；必须先停止实例，避免关停保存覆盖待恢复的 RDB
docker compose stop redis
docker compose cp ./backups/dump.rdb redis:/data/dump.rdb
docker compose start redis
redis_ready=0
for _ in $(seq 1 30); do
  if docker compose exec -T redis redis-cli PING | grep -q PONG; then
    redis_ready=1
    break
  fi
  sleep 1
done
test "$redis_ready" = 1
```

### 4.3 PRD2 备份/恢复演练

PRD2 的恢复演练必须在空的非生产数据库执行。脚本不会清空目标库；如果目标库
已有对象，恢复会失败，由运维人员重新准备安全目标库后再跑。两个 DSN 都不得
内嵌密码；需要密码认证时，必须由批准的秘密注入机制分别设置
`UNIROUTE_BACKUP_SOURCE_PASSWORD` 和
`UNIROUTE_RESTORE_REHEARSAL_PASSWORD`，不要把密码写入命令、DSN、文档或
evidence。脚本也会拒绝在 shell xtrace 开启时运行。

```bash
: "${UNIROUTE_BACKUP_SOURCE_DSN:?Set the approved source DSN}"
: "${UNIROUTE_RESTORE_REHEARSAL_DSN:?Set the empty non-production restore DSN}"

UNIROUTE_BACKUP_SOURCE_DSN="$UNIROUTE_BACKUP_SOURCE_DSN" \
UNIROUTE_RESTORE_REHEARSAL_DSN="$UNIROUTE_RESTORE_REHEARSAL_DSN" \
UNIROUTE_BACKUP_RESTORE_CONFIRM=non-production-restore-target \
scripts/prd2-backup-restore-rehearsal.sh
```

期望输出：

```text
PASS backup/restore rehearsal schema_versions=N core_tables=6 dump_file=/tmp/uniroute-prd2-backup-rehearsal.XXXXXX/database.XXXXXX
```

脚本拒绝 `UNIROUTE_BACKUP_DUMP_FILE` 任意路径覆盖，并在
`${TMPDIR:-/tmp}` 下建立随机的 `0700` 私有工作目录和 `0600` dump 文件。
成功后的 dump 不会自动删除，便于按既定保留策略校验和加密归档；失败后的私有
工作区也会保留供故障调查。运维人员必须把输出路径作为敏感信息处理，并按批准的
保留周期处置。不要把该脚本指向生产恢复目标。

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
: "${UNIROUTER_ADMIN_JWT:?Set an active administrator JWT}"
curl http://localhost:8080/api/v1/dashboard/stats \
    -H "Authorization: Bearer $UNIROUTER_ADMIN_JWT"
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

# 指定已批准的测试地址和专用 API Key
: "${UNIROUTE_LOADTEST_BASE_URL:?Set the approved load-test base URL}"
: "${UNIROUTE_LOADTEST_API_KEY:?Set a dedicated load-test API key}"
BASE_URL="$UNIROUTE_LOADTEST_BASE_URL" API_KEY="$UNIROUTE_LOADTEST_API_KEY" \
  k6 run infra/loadtest/k6-chat-completions.js
```

脚本路径：`infra/loadtest/k6-chat-completions.js`。当前脚本固定请求
`gpt-3.5-turbo`；仅当该模型已在测试项目中批准并绑定时运行，不能把默认占位
模型的失败计入平台容量结论。

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
# - 正式镜像的 release trust 异常 → 检查启动日志和内嵌公钥环；不得添加运行时 LICENSE_PUBLIC_KEY 覆盖
```

### 7.2 请求返回 502

```bash
# 检查上游渠道状态
: "${UNIROUTER_ADMIN_JWT:?Set an active administrator JWT}"
curl http://localhost:8080/api/v1/channels \
    -H "Authorization: Bearer $UNIROUTER_ADMIN_JWT"

# 检查熔断器状态
docker compose logs backend | grep "circuit"
```

### 7.3 余额异常

```bash
: "${UNIROUTER_ORG_ID:?Set the organization ID to inspect}"

# 检查钱包状态
docker compose exec postgres psql -U uniroute \
    -v org_id="$UNIROUTER_ORG_ID" -c \
    "SELECT * FROM wallets WHERE org_id = :'org_id'::bigint;"

# 检查未进入已结算/已取消终态的冻结记录
docker compose exec postgres psql -U uniroute -c \
    "SELECT * FROM freeze_idempotency WHERE status NOT IN ('settled', 'cancelled') ORDER BY created_at DESC LIMIT 20;"

# 检查交易记录
docker compose exec postgres psql -U uniroute \
    -v org_id="$UNIROUTER_ORG_ID" -c \
    "SELECT * FROM transactions WHERE org_id = :'org_id'::bigint ORDER BY created_at DESC LIMIT 20;"
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

V1 客户升级必须使用签名离线包。第一次安装需要通过受认证的 bootstrap 渠道
取得独立 `uniroute-upgrade` 验证器、公钥环及验证器 SHA-256；之后由已安装的
`./uniroute upgrade verify` 作为本地信任延续。不要从待验证包内部取出并运行
验证器来验证它自己。

```bash
: "${UNIROUTER_RELEASE_OPERATOR:?Set the approved operator or deployment identity}"

# 1. 读取真实安装状态，不改状态
docker compose -f deploy/customer/docker-compose.yml --env-file deploy/customer/.env \
  exec backend ./uniroute upgrade status

# 2. 将只读升级包提供给当前 backend 后，完整验证签名、hash/size、平台、
#    sequence 和 schema。失败时立即停止，不提取、不执行。
docker compose -f deploy/customer/docker-compose.yml --env-file deploy/customer/.env \
  cp /approved-read-only/unirouter-release.tar.gz backend:/tmp/unirouter-release.tar.gz
docker compose -f deploy/customer/docker-compose.yml --env-file deploy/customer/.env \
  exec backend ./uniroute upgrade verify \
  --package /tmp/unirouter-release.tar.gz

# 3. 备份数据库并验证备份目录可读
docker compose -f deploy/customer/docker-compose.yml --env-file deploy/customer/.env \
  exec postgres pg_dump -Fc -U uniroute uniroute > pre_upgrade_backup.dump
pg_restore -l pre_upgrade_backup.dump >/dev/null

# 4. 进入批准的维护窗口，drain 旧请求；严格按该包的 release note 应用已验证
#    的 OCI 镜像/迁移制品。不得将“tar 可打开”当作签名验证。
#    具体 docker load / compose switch 命令由每个包的签名版本/平台清单给出。

# 5. 验证新 runtime 版本/commit、目标 schema 和健康状态
docker compose -f deploy/customer/docker-compose.yml --env-file deploy/customer/.env \
  exec backend wget -qO- http://localhost:8080/health/ready
docker compose -f deploy/customer/docker-compose.yml --env-file deploy/customer/.env \
  exec backend ./uniroute migrate status
docker compose -f deploy/customer/docker-compose.yml --env-file deploy/customer/.env \
  exec backend ./uniroute upgrade status

# 6. 只从已健康的新 binary 重新验证同一包并登记安装事实
docker compose -f deploy/customer/docker-compose.yml --env-file deploy/customer/.env \
  cp /approved-read-only/unirouter-release.tar.gz backend:/tmp/unirouter-release.tar.gz
docker compose -f deploy/customer/docker-compose.yml --env-file deploy/customer/.env \
  exec backend ./uniroute upgrade record-applied \
  --package /tmp/unirouter-release.tar.gz \
  --recorded-by "$UNIROUTER_RELEASE_OPERATOR"

# 7. 登记成功后只删除这个明确的容器内临时包；保留受控原件和证据摘要
docker compose -f deploy/customer/docker-compose.yml --env-file deploy/customer/.env \
  exec backend rm -f /tmp/unirouter-release.tar.gz
```

`record-applied` 要求运行中的 version/commit 与签名 manifest 一致，当前 schema
已等于签名 target，并在 serializable 事务和 release-state 锁下写入 package
SHA-256、key ID、sequence、manifest 和操作者。重复包、旧 sequence 或不兼容
schema 会拒绝。`000037_v1_commercial_release_control` 一旦已有签名发布历史便
拒绝 down migration；此时恢复策略是评审后的 roll-forward，不是静默回滚旧
writer。

### 8.1 回退到不识别 feed purpose 的旧 binary

当前正式流程没有自动回滚控制器，镜像切换由维护窗口中的操作者执行。若目标
binary 早于 `xinsight.feed.read` 语义，必须按以下顺序操作：

```bash
: "${UNIROUTER_ADMIN_JWT:?Set an active administrator JWT}"
: "${UNIROUTER_FEED_KEY_ID:?Set the exact xinsight.feed.read key ID}"

# 1. 先停止 X-Insight poller，确认它不再发起 feed 请求。

# 2. 仍使用当前 purpose-aware binary 通过专用 X-Insight 控制面撤销精确 feed key。
: "${UNIROUTER_FEED_PROJECT_ID:?Set the exact feed project ID}"
curl -fsS -X POST \
  "http://localhost:8080/api/v1/data-exchange/xinsight/connections/${UNIROUTER_FEED_PROJECT_ID}/credentials/${UNIROUTER_FEED_KEY_ID}/revoke" \
  -H "Authorization: Bearer ${UNIROUTER_ADMIN_JWT}"

# 3. 在任何旧 binary 启动前，从当前 backend 执行兼容性预检。
docker compose -f deploy/customer/docker-compose.yml --env-file deploy/customer/.env \
  exec backend ./uniroute upgrade preflight-legacy-rollback

# 期望：
# rollback_preflight=pass
# active_purpose_keys=0
```

预检失败、数据库不可读或输出不为 PASS 时不得启动旧 binary。启用中的 feed
key 在数据库内使用 `feed_active`，而旧版网关只放行 `active`；`000043` 约束
同时禁止把 purpose key 改成普通 `active`。`feed_revoked` 是不可逆终态，通用
Keys API 不允许重新启用。因此即使操作顺序被遗漏，旧 binary
也不能把 feed-only 凭据扩权成模型调用凭据。明确禁用 feed key 后支持 binary
回退；重新启用只能在 purpose-aware binary 恢复后进行。

schema down 更严格：只要 purpose key 仍存在或保留 feed 状态存在，`000043`
就拒绝回退。常规故障恢复仍应 roll-forward；删除凭据/保留数据不是普通 binary
回退的一部分。

该流程没有网页“上传即执行”按钮，也没有静默自动更新。未来若要做 Update
Center，必须先补 maintenance window、多节点编排、受控 staging、N-1 夹具、
失败暂停和审计责任边界；不能复用 License 导入接口执行二进制。

以下命令只适用于源码开发/内部测试构建，不属于客户签名包升级流程。如果开发
构建网络无法访问默认的 `proxy.golang.org`，可显式选择经审批且可达的 Go
module proxy；默认值仍保持官方代理：

```bash
GOPROXY=https://goproxy.cn,direct docker compose build backend
```

该变量只影响构建阶段的模块下载，不应设置 `GONOSUMDB=*`、`GOSUMDB=off` 或跳过现有 `go.sum` 校验。

---


---

## 九、Canary 测试部署

本章使用根目录源码 Compose，固定为 `development` trust mode，只适用于开发、
staging 或已授权的非生产测试目标。不得把本章命令当作客户正式安装/升级路径。
stable 与 canary 必须读取同一个
`UNIROUTE_NOTIFICATION_WEBHOOK_SIGNING_SECRET`；Compose/CI 会检查两者配置一致。
本章的流量 canary 只允许两个 binary 的 exact schema ceiling 相同且共享数据库已经
位于该 ceiling 时使用。任何改变 ceiling 的发布（包括当前 M82→M83）必须先按
对应 migration 小节（当前为 3.20）完成非滚动 schema 切换，不能保留旧 stable 再
启动新 canary。

### 9.1 前置条件：Nginx 配置

默认 canary overlay 挂载 `nginx/nginx.http-canary.conf`，只监听 HTTP 80，适合本地和 staging smoke，不需要 TLS 证书。

```bash
docker compose -f docker-compose.yml -f docker-compose.canary.yml config
```

共享 Target 启用 TLS 时，不要编辑 commit-addressed release，也不要把证书或私钥
复制进仓库。使用一个受控宿主拥有的第三 Compose overlay，按容器目标路径替换
`/etc/nginx/conf.d/default.conf`，只读挂载 ACME webroot、证书 `live` 目录及其
`archive` 目录：

```bash
# Target-owned TLS overlay example
# - /opt/unirouter/tls/nginx.https.conf:/etc/nginx/conf.d/default.conf:ro
# - /opt/unirouter/tls/acme-webroot:/var/www/certbot:ro
# - /opt/unirouter/tls/letsencrypt/live/<identity>:/etc/letsencrypt/live/<identity>:ro
# - /opt/unirouter/tls/letsencrypt/archive/<identity>:/etc/letsencrypt/archive/<identity>:ro
```

证书 `live` 文件是指向 `archive` 的相对符号链接，所以两者必须一起按目录挂载；
不要只绑定一个当前证书文件，否则续期切换符号链接后运行中容器可能继续读取旧
inode。HTTP 80 必须保留 `/.well-known/acme-challenge/`，其他请求才重定向到
HTTPS。每次 release 切换都必须继续带上同一个 Target-owned overlay，否则默认
canary 配置会按设计恢复为 HTTP-only。

先渲染三层 Compose，再用一次性 Nginx 容器和真实挂载执行 `nginx -t`；验证通过后
仅重建 Nginx。续期必须由宿主调度、固定 Certbot image digest，并只在证书字节发生
变化且 `nginx -t` 通过后 reload。开发 ECS 的精确实现、回滚和 SHA 见
`docs/ops/evidence/PRD8-PUBLIC-HTTPS-TARGET-20260803.md`；客户正式部署仍遵循 10.3，
使用客户批准的 DNS/CA/TLS 代理。

启动后可检查 Nginx 配置语法：

```bash
docker compose -f docker-compose.yml -f docker-compose.canary.yml \
  -f /path/to/target-owned-tls-overlay.yml exec nginx nginx -t
```

### 9.2 架构说明

Canary 测试通过外层 Nginx 将部分流量路由到 candidate backend，同时由 stable
backend 处理大部分请求；两者必须声明相同的 exact schema ceiling，且该 ceiling
必须与共享数据库 migration ledger 精确相等。

```
客户端 → Nginx:80 (HTTP)
            ├── 90% → uniroute-api:8080        (stable)
            └── 10% → uniroute-api-canary:8080  (canary，共享 PG/Redis)
```

关键约束：
- canary 实例 **共享** stable 的 PostgreSQL 和 Redis
- canary 实例可设置 `SKIP_MIGRATION=true`，但这只跳过 migration 执行，不跳过
  exact runtime-schema gate；共享数据库必须已经由唯一 migration owner 升到两个
  binary 共同声明的 ceiling
- schema ceiling 不同的 stable/canary 禁止共存；当前 M82→M83 使用 3.20 的非滚动流程
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
: "${UNIROUTER_ADMIN_JWT:?Set an active administrator JWT}"

# 先证明两个直连实例各自报告正确角色，再对 Nginx 做无计费的只读采样。
curl -fsS http://localhost:8080/api/v1/runtime/status \
  -H "Authorization: Bearer $UNIROUTER_ADMIN_JWT" \
  | jq -r '.runtime_environment.deployment_role'
curl -fsS http://localhost:8081/api/v1/runtime/status \
  -H "Authorization: Bearer $UNIROUTER_ADMIN_JWT" \
  | jq -r '.runtime_environment.deployment_role'
for _ in $(seq 1 50); do
  curl -fsS http://localhost/api/v1/runtime/status \
    -H "Authorization: Bearer $UNIROUTER_ADMIN_JWT" \
    | jq -r '.runtime_environment.deployment_role'
done | sort | uniq -c
```

前两次应分别输出 `stable` 和 `canary`。代理采样应与当前权重大致一致，但有限
样本不是精确比例证明；若没有观察到 canary，先检查实际挂载配置和 Nginx 日志，
不要用付费模型调用反复试探。

### 9.5 调整 canary 权重

编辑当前实际挂载的 Nginx 配置，修改 `upstream backend_canary` 的
`weight` 值。默认 HTTP overlay 使用 `nginx/nginx.http-canary.conf`；
只有按 9.1 切换 TLS mount 后才编辑 `nginx/nginx.conf`：

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
docker compose -f docker-compose.yml -f docker-compose.canary.yml \
  exec nginx nginx -s reload
```

### 9.6 监控 canary

```bash
# 查看 canary 日志（独立容器）
docker compose -f docker-compose.yml -f docker-compose.canary.yml logs -f backend-canary

# 对比 stable 和 canary 的错误率
echo "=== Stable errors ==="
docker compose logs backend --since 5m | grep -i "error\|panic\|fatal" | wc -l

echo "=== Canary errors ==="
docker compose -f docker-compose.yml -f docker-compose.canary.yml \
  logs backend-canary --since 5m | grep -i "error\|panic\|fatal" | wc -l

# 检查共享流量的近期客户扣费摘要（micro-CNY）
docker compose exec postgres psql -U uniroute -c \
    "SELECT api_key_id, SUM(cost_micros) AS total_cost_micros, COUNT(*) AS requests \
     FROM call_logs WHERE created_at > NOW() - INTERVAL '1 hour' \
     GROUP BY api_key_id ORDER BY requests DESC LIMIT 10;"
```

该聚合查询不能单独证明 canary 计费正确，因为 stable/canary 共享数据库。
精确验收必须用已批准请求的 request ID 核对 route/attempt/call-log/钱包守恒；
真实上游请求仍需独立授权。

### 9.7 回滚 canary

如果 canary 出现问题，立即停止 canary 容器：

```bash
# 停止 canary（Nginx 会自动回退到 stable）
docker compose -f docker-compose.yml -f docker-compose.canary.yml stop backend-canary
docker compose -f docker-compose.yml -f docker-compose.canary.yml rm -f backend-canary

# 验证流量全部回到 stable
: "${UNIROUTER_ADMIN_JWT:?Set an active administrator JWT}"
for _ in $(seq 1 20); do
  curl -fsS http://localhost/api/v1/runtime/status \
    -H "Authorization: Bearer $UNIROUTER_ADMIN_JWT" \
    | jq -r '.runtime_environment.deployment_role'
done | sort | uniq -c
```

### 9.8 同一 schema ceiling 下升级 stable

只有 stable/canary binary 声明相同 schema ceiling、且数据库已经精确位于该 ceiling
时，canary 验证通过后才可按本节升级 stable。若 candidate 提高 ceiling（包括
M82→M83），不要执行本节命令；按 3.20 停止整个旧 fleet、由唯一 migration owner
升级，再启动新 stable/canary。

本节不得使用缺省 `docker compose` 命令。先完整执行 9.10.1 的
project/config-label、env-file、exact image、存储和备份 preflight，并冻结同一个
`target_compose` 数组。当前与候选 binary 的 schema ceiling、候选
`./uniroute migrate status` 和数据库 ledger 必须三者精确一致；本路径不运行
`migrate up`。镜像只能由已验证的 exact release 构建/复用流程产生，不能在活动
release 目录中临时执行 ad-hoc `docker compose build`。

然后使用冻结的 `target_compose` 先 `--force-recreate` canary，完成直连 readiness
和只读身份验证后，再 `--force-recreate` stable。stable/canary 均通过后，只有在
frontend artifact 或 Compose provenance 变化时，才按 `frontend` → `nginx` 顺序
重建；最终读回四个容器完全相同的 project/config-files 标签和 exact image ID。
Provider 调用、Release/alias/canonical 变化不属于部署 smoke，不得由本节自动触发。

### 9.9 PRD2 HA readiness smoke

PRD2 HA readiness 的最小验收目标是：两个 UniRouter 应用实例连接同一 PostgreSQL 和 Redis，且各自的 gateway/admin health check 均可服务。完整边界见 `docs/tech-ref/15-ha-readiness.md`。

关键约束：

- 唯一 stable/migration-owner 实例负责迁移。
- canary/secondary 实例设置 `SKIP_MIGRATION=true` 时仍会执行 exact
  runtime-schema gate；只有共享数据库已精确等于其 binary ceiling 才能 ready。
- schema ceiling 变化必须非滚动切换，不能用旧 stable 加新 canary 做 HA smoke。
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
: "${UNIROUTE_API_KEY:?Set the approved bounded-smoke API key}"
: "${UNIROUTE_MODEL:?Set the approved bounded-smoke model}"

UNIROUTE_STABLE_URL=http://localhost:8080 \
UNIROUTE_CANARY_URL=http://localhost:8081 \
UNIROUTE_PROXY_URL=http://localhost \
UNIROUTE_RUN_GATEWAY_SMOKE=1 \
UNIROUTE_API_KEY="$UNIROUTE_API_KEY" \
UNIROUTE_MODEL="$UNIROUTE_MODEL" \
scripts/prd2-ha-smoke.sh
```

如需数据库证据，另行通过秘密注入设置 `UNIROUTE_HA_DB_DSN`；不要把 DSN 写进
文档、命令历史或 evidence。

可选共享 rate-limit smoke 需要一把低限额测试 API Key。该检查会交替打 stable/canary，直到观察到 HTTP 429：

```bash
: "${UNIROUTE_LOW_LIMIT_API_KEY:?Set the dedicated low-limit API key}"
: "${UNIROUTE_MODEL:?Set the approved bounded-smoke model}"

UNIROUTE_RUN_GATEWAY_SMOKE=1 \
UNIROUTE_RUN_RATE_LIMIT_SMOKE=1 \
UNIROUTE_RATE_LIMIT_REQUESTS=4 \
UNIROUTE_API_KEY="$UNIROUTE_LOW_LIMIT_API_KEY" \
UNIROUTE_MODEL="$UNIROUTE_MODEL" \
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

### 9.10 development/test Target 镜像生命周期

同一 release 的每个发生变化的组件只构建一次。stable 与 canary 必须引用同一个
backend image ID；不能因为副本角色不同重复构建。构建入口要求 clean Git checkout、
`HEAD == RUNTIME_COMMIT`、磁盘门禁通过，并给镜像写入完整 commit/version/component
标签。目标标签已存在时只能验证并复用，不能覆盖成另一份内容：

历史上由 rsync 长期覆盖的 `/opt/unirouter` working tree 不是构建事实源；即使文件
看起来最新，它的 Git HEAD 与工作树也可能不一致。以后必须从已 fetch 的精确 commit
创建独立 clean detached worktree，在该 worktree 内运行下面的 helper。运行 Compose
的目录可以继续保存目标配置，但不得兼任可变源码构建目录。

```bash
: "${RUNTIME_VERSION:?Set the immutable development version}"
: "${RUNTIME_COMMIT:?Set the complete Git commit}"

# 仅在目标网络无法访问官方代理且已批准替代代理时设置。
export GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}"
scripts/internal-target-build-images.sh

# 从脚本最后两行复制实际 SELECTED 引用；不要猜测或改用 latest。
export UNIROUTER_BACKEND_IMAGE="unirouter-release/backend:<selected-tag>"
export UNIROUTER_FRONTEND_IMAGE="unirouter-release/frontend:<selected-tag>"

docker compose \
  -f docker-compose.yml \
  -f docker-compose.canary.yml \
  -f deploy/internal-target-images.override.yml \
  up -d --no-build
```

#### 9.10.1 development/test schema-ceiling 部署的唯一受支持顺序

仅前端修复可在 exact clean worktree 使用
`scripts/internal-target-build-images.sh --frontend-only`。此模式保留源码、锁、
存储和不可变标签校验，只输出前端的 `SELECTED` 身份；不会检查、构建或重新
标记 backend，也不证明前后端兼容。部署前核对当前 backend 的精确镜像、既有
API 合同和完整 Compose 配置，保留其运行身份。只有前端服务的镜像发生变化时，
使用已冻结的 Compose chain `up -d --no-build --no-deps --force-recreate frontend`；
随后回读前端镜像、页面版本以及 backend 容器、schema、Release/Slot 均未改变。
组件可以有不同的精确提交标识，不得把保留的 backend 冒充当前 frontend 提交。

development/test 的现存 Compose project 是 `unirouter`，不是根据当前
worktree 目录名推导的默认值。每次部署必须显式传入
`--project-name unirouter`，并显式列出完整 Compose file chain。发布前先从
stable、canary、frontend 和 Nginx 容器的
`com.docker.compose.project.config_files` 标签读取当前链；四者必须
一致。新发布只替换 release-scoped 源文件与当次 runtime-identity
overlay；TLS 与已批准的 site-local overlays 必须逐项保留。当次 evidence
目录必须保存不含 secret 的 project、env-file 路径和有序 config-file
manifest。不得依赖 Compose 目录推导、缺省 overlay 或历史命令记忆。

当次审核后应把唯一 Compose 命令定义为 shell 数组，后续步骤不得
临时增删 `-f`：

```bash
target_project=unirouter
target_env_file=/opt/unirouter/.env
target_compose=(docker compose --project-name "${target_project}" \
  --env-file "${target_env_file}" \
  -f "${release_dir}/docker-compose.yml" \
  -f "${release_dir}/docker-compose.canary.yml" \
  -f "${release_dir}/deploy/internal-target-images.override.yml" \
  -f /opt/unirouter/tls/docker-compose.https.yml \
  "${approved_site_overlay_args[@]}" \
  -f "${current_runtime_identity_overlay}")

test "${target_project}" = unirouter
test -f "${target_env_file}"
test "$(stat -c %a "${target_env_file}")" = 600
"${target_compose[@]}" config >/dev/null
"${release_dir}/scripts/docker-storage-preflight.sh" --path /var/lib/docker
```

`approved_site_overlay_args` 必须是当次 manifest 中已复核的有序 `-f path`
参数，不允许空白猜测。preflight 还必须读回候选 backend 与
backend-canary 均为 `SKIP_MIGRATION=true`，两者引用同一 backend
image，候选 image label 的完整 commit/version 与 exact worktree 一致。
任一不符即停止。

对于 migration ceiling 变化，stable/canary 本身不是 migration owner。必须
先排空并停止两个旧 backend，再用候选 backend image 启动不绑定
HTTP 端口的 CLI one-off 容器执行受支持的 `./uniroute migrate up`。
读回 exact ledger 后才可以重建 stable/canary：

```bash
"${target_compose[@]}" stop backend backend-canary
"${target_compose[@]}" run --rm --no-deps \
  -e SKIP_MIGRATION=false backend ./uniroute migrate up
"${target_compose[@]}" run --rm --no-deps \
  -e SKIP_MIGRATION=true backend ./uniroute migrate status
"${target_compose[@]}" up -d --no-build --no-deps --force-recreate \
  backend backend-canary
```

两个 backend readiness、exact image ID 和 schema gate 通过后，再按
`frontend` → `nginx` 顺序执行 `--force-recreate`。即使镜像或 Nginx
基础 image 可复用，容器的 Compose config-file/provenance 标签也必须指向
当次 exact release：

```bash
"${target_compose[@]}" up -d --no-build --no-deps --force-recreate frontend
"${target_compose[@]}" up -d --no-build --no-deps --force-recreate nginx
"${target_compose[@]}" ps
```

任一阶段失败都应保持旧流量已排空状态，不得让旧二进制与新
schema 共享写入。迁移已提交后的恢复路径是修正候选并 roll-forward，
不是在已有新 evidence 时盲目回退 schema。

所有长期 Compose 服务默认使用 Docker `json-file` 日志轮转：单文件 20 MiB、最多
3 个文件。可通过 `DOCKER_LOG_MAX_SIZE` 和 `DOCKER_LOG_MAX_FILES` 在部署环境调整，
但不能留空或移除 `logging` 合同。日志参数只在容器创建时生效；修改后必须按
canary → stable → frontend/基础服务顺序受控重建，并用 `docker inspect` 核对
`HostConfig.LogConfig.Config`。不要把应用数据库中的请求日志保留期
`LOG_RETENTION_DAYS` 与 Docker stdout/stderr 轮转混为一项。

如果新 commit 的 `frontend/` 没有变化，可显式设置
`UNIROUTER_FRONTEND_REUSE_FROM=unirouter-release/frontend:<verified-old-tag>`。
脚本会读取旧镜像的完整 revision 标签并执行 Git diff；只有目录确实不变才直接返回
旧引用，不创建新镜像或新标签。backend stable/canary 由 Compose overlay 强制复用；
不得再创建 `backend-canary:<tag>`。Docker/BuildKit 仍会自动复用各组件相同的 layer，
`docker system df` 的 virtual size 不能当作物理磁盘占用相加。

同理，`docker builder du` 的 `Shared`、`Reclaimable` 和记录数量描述的是 build graph
的逻辑引用；带 `*` 的共享记录会重复出现在多个 build result 中。清理判断必须先从
`docker info` 确认 storage driver：使用 containerd snapshotter 时，实际 layer 位于
`/var/lib/containerd`，不能只看 Docker root。随后同时核对相关目录的 `du` 和根盘
`df`。被近期构建继续引用的旧 parent record 是有效复用缓存，不应为了降低记录数
而无条件 prune。

80% / 8 GiB 是 40 GiB development/test 系统盘的当前硬门槛，不是告警建议。
任一门槛失败都应先运行受控 maintenance 或扩盘，不能通过继续构建来“完成后再清”。

每次 stable 验证通过后，运维必须维护一个部署机本地 keep file。它列出最近两个
**已经验证可回滚的完整部署状态**所引用的精确 backend/frontend 镜像，以及不由
常驻容器引用、但由定时任务或下一次构建依赖的镜像。N-1/N-2 必须满足：commit
线性前驱关系已确认、候选 migrations 最大版本不低于当前数据库版本、候选
`migrate status` 对当前数据库无 pending、frontend source 在复用镜像时确实无
变化。不能按“最新标签”猜测，也不能把失败构建当作回滚点。示例：

```text
# /opt/unirouter/image-retention.keep
unirouter-release/backend:<previous-release>
unirouter-release/frontend:<previous-release>
unirouter-release/backend:<previous-previous-release>
unirouter-release/frontend:<previous-previous-release>
certbot/certbot:<renewal-version>
golang:<current-builder-version>
node:<current-builder-version>
alpine:<current-runtime-base-version>
```

当前运行容器会自动保护。已退出容器会单独标为
`protected-stopped-container`，用于暴露尚未收口的一次性 migration/smoke
容器；完成证据留存后应按精确容器名人工移除，避免永久锁住旧镜像。

先运行 dry-run：

```bash
scripts/docker-image-retention-maintenance.sh \
  --keep-file /opt/unirouter/image-retention.keep \
  --min-image-age-days 14 \
  --cache-until 168h \
  --report-out /opt/unirouter/image-retention-report.tsv
```

报告使用完整 image ID 并冻结当时的创建时间和 tags。只有未被任何容器引用、属于
UniRouter、超过 14 天且不在 keep 集合中的对象才标为 `candidate`。其中的
`VIRTUAL_SIZE_MIB` 包含共享 layer，不能相加冒充精确物理回收量。

复核 dry-run、记录精确候选集合并获得该次删除授权后才可执行：

```bash
scripts/docker-image-retention-maintenance.sh \
  --apply \
  --keep-file /opt/unirouter/image-retention.keep \
  --min-image-age-days 14 \
  --cache-until 168h \
  --report-out /opt/unirouter/image-retention-report.tsv
```

apply 会在每个删除前重新核对完整 image ID、创建时间、全部 tags、容器引用和 keep
集合；任何漂移立即停止。它只删除已确认的 UniRouter candidate，并调用 Docker
原生 builder prune 清理超过 `--cache-until` 的未使用 BuildKit cache。例行维护保持
`168h`；只有 80%/8 GiB preflight 已失败、168h 模式无法恢复门槛且精确 dry-run
仍保护 N/N-1/N-2、基础 image、备份和 evidence 时，才允许用 `120h` 作为磁盘压力
模式重新 dry-run 并单独 apply。120h 后仍不能通过时，应检查重复 build、containerd
layer 和旧镜像或扩盘，不能继续机械缩短为 72h/24h。它不删除容器、volume、
数据库、备份、release 目录或非 UniRouter 镜像。

这些工具只按需安装和运行，不启用 systemd timer、cron 或其他自动删除：

```bash
install -d -m 0755 /usr/local/libexec/unirouter
install -m 0755 scripts/docker-image-retention-report.sh \
  scripts/docker-image-retention-maintenance.sh \
  scripts/docker-storage-preflight.sh \
  scripts/internal-target-build-images.sh \
  scripts/release-retention-common.sh \
  scripts/release-worktree-retention-report.sh \
  scripts/release-worktree-retention-maintenance.sh \
  /usr/local/libexec/unirouter/
```

禁止直接使用无范围的 `docker image prune -a`：它无法知道哪两个版本已经完成
回滚验证。禁止 `--volumes`。正式客户安装仍应使用签名、digest-pinned 制品；本节
的 source build 和 local tag 只适用于内部 development/test Target。

commit-addressed build worktree 也必须显式保留 N/N-1/N-2，不能因镜像已经治理就让
`/opt/unirouter/releases` 无限增长。keep file 只接受完整 40 位小写 Git commit；它
应与已经验证的镜像回滚集合同时更新：

```text
# /opt/unirouter/release-retention.keep
<current-verified-commit>
<previous-verified-commit>
<previous-previous-verified-commit>
```

先生成只读报告：

```bash
scripts/release-worktree-retention-maintenance.sh \
  --release-root /opt/unirouter/releases \
  --keep-file /opt/unirouter/release-retention.keep \
  --min-age-days 14 \
  --report-out /opt/unirouter/release-retention-report.tsv
```

只有目录名等于完整 HEAD、处于 detached/clean 状态、没有运行容器 revision 或
bind mount 引用、未出现在 keep file、且超过保留期的直接子 checkout 才会标为
`candidate`。当前 linked worktree 与历史遗留的自包含 standalone repository 都会
明确标出拓扑；后者只在唯一 worktree、没有 Git alternates 时才可进入候选。脏目录、
branch worktree、符号链接、非 Git 目录、目录名与 HEAD 不一致等情况全部标为
`review-required`，不会进入删除集合。

复核并对本次精确候选集合取得删除授权后，才增加 `--apply`：

```bash
scripts/release-worktree-retention-maintenance.sh --apply \
  --release-root /opt/unirouter/releases \
  --keep-file /opt/unirouter/release-retention.keep \
  --min-age-days 14 \
  --report-out /opt/unirouter/release-retention-report.tsv
```

apply 会先对冻结候选全集核对路径、HEAD、mtime、clean/detached 状态、Git 拓扑、
keep file、运行容器 revision 与 bind mount；只有全部通过才进入删除阶段，并在每项
删除前再次复核。linked worktree 通过 `git worktree remove` 删除；历史 standalone
repository 仅在再次证明自包含后执行限定到该精确目录的树删除。工具不会删除镜像、
容器、volume、bundle、备份、数据库或其他 review-required 目录，也不会安装 timer
或 cron。

---

## 十、客户正式安装与 HTTPS

客户路径从已经通过独立信任渠道取得并验签的发布包开始，不从 Git checkout
开始，也不运行根目录 source Compose 或 `scripts/fix-compose-for-deploy.py`。
以下命令假定独立 `verify` 已 PASS，并已按该包的签名 release note 把经校验的
`payload/` 解包到一个新的 `/opt/unirouter-release` staging 目录；原始 archive
仍原样保存在 `UNIROUTER_SIGNED_PACKAGE` 指向的受控只读路径。未验签前不得解包。

### 10.1 配置与可执行 preflight

```bash
cd /opt/unirouter-release
cp payload/deploy/.env.example payload/deploy/.env
chmod 600 payload/deploy/.env

# 按签名 manifest 填入六个完整 image@sha256:64位小写十六进制摘要，并替换所有 secret。
# JWT_SECRET、API_KEY_HASH_SECRET 与通知 Webhook HMAC 必须分别至少 32 bytes。
# 同时设置稳定的 LICENSE_DEPLOYMENT_ID。
bash ./payload/deploy/preflight.sh payload/deploy/.env
```

preflight 必须拒绝短密钥或示例占位符，找到恰好六个 digest 镜像且没有
`build:`。它不会判断 digest 是否由 UniRouter 批准；该授权事实只来自已经验证的
签名 manifest 和逐文件 hash/size。正式 fresh-install 样例此时明确是一次性的
`MODEL_ROUTE_AUTHORITY=legacy` 与
`MODEL_ROUTE_COMPATIBILITY_ENABLED=true`；这会启动完整 legacy runtime，不是安全的
bootstrap-only listener。外层 TLS proxy、负载均衡和任何客户 Gateway 入口必须继续
断开，只允许经批准的 loopback/bootstrap 管理、License 导入、authoring 和 probe。

### 10.2 启动、首装登记与读回

```bash
: "${UNIROUTER_SIGNED_PACKAGE:?Set the absolute path to the exact verified release package}"
: "${UNIROUTER_RELEASE_OPERATOR:?Set the approved operator or deployment identity}"
test -r "${UNIROUTER_SIGNED_PACKAGE}"

docker compose --env-file payload/deploy/.env \
  -f payload/deploy/docker-compose.yml up -d
docker compose --env-file payload/deploy/.env \
  -f payload/deploy/docker-compose.yml ps
docker compose --env-file payload/deploy/.env \
  -f payload/deploy/docker-compose.yml exec backend \
  wget -qO- http://localhost:8080/health/ready

# 首次安装同样必须登记已应用的签名发布。客户 Compose 不挂载宿主包目录，
# 因此显式把步骤 10.1 中独立验签的同一包 staging 到 backend。
docker compose --env-file payload/deploy/.env \
  -f payload/deploy/docker-compose.yml cp \
  "${UNIROUTER_SIGNED_PACKAGE}" backend:/tmp/unirouter-initial-release.tar.gz
docker compose --env-file payload/deploy/.env \
  -f payload/deploy/docker-compose.yml exec backend \
  ./uniroute upgrade record-applied \
  --package /tmp/unirouter-initial-release.tar.gz \
  --recorded-by "${UNIROUTER_RELEASE_OPERATOR}"
docker compose --env-file payload/deploy/.env \
  -f payload/deploy/docker-compose.yml exec backend \
  ./uniroute upgrade status
docker compose --env-file payload/deploy/.env \
  -f payload/deploy/docker-compose.yml exec backend \
  rm -f /tmp/unirouter-initial-release.tar.gz

# 轮换 seed admin 密码；密码文件合同见 deploy/customer/README.md，禁止把值放入参数。
: "${UNIROUTER_ADMIN_PASSWORD_FILE:?Set the approved administrator password file}"
test -r "${UNIROUTER_ADMIN_PASSWORD_FILE}"
docker compose --env-file payload/deploy/.env \
  -f payload/deploy/docker-compose.yml exec -T backend \
  ./uniroute admin set-password --username admin --org-id 1 --password-stdin \
  < "${UNIROUTER_ADMIN_PASSWORD_FILE}"

# 使用轮换后的 local admin 成功登录一次，再调用 POST /api/v1/bootstrap/first-customer。
# 在客户 Gateway 流量仍被阻断时，通过受支持的 authenticated platform-admin
# 控制面依次创建唯一 root customer、导入 customer-bound License，并完成：
# canonical connection/deployment（四个执行限额均为正数）→ credential binding →
# exact operation probe → M78 atomic sealed release authoring → activate_initial。

# 用 one-off canonical 环境读取同一 DB authority；不要运行 legacy reconcile apply。
: "${UNIROUTER_CUSTOMER_ORG_ID:?Set the created root customer organization ID}"
docker compose --env-file payload/deploy/.env \
  -f payload/deploy/docker-compose.yml exec \
  -e MODEL_ROUTE_AUTHORITY=canonical \
  -e MODEL_ROUTE_COMPATIBILITY_ENABLED=false \
  -e ASYNC_MEDIA_ENABLED=false \
  backend ./uniroute model-route-authority check \
  --customer-org-id "${UNIROUTER_CUSTOMER_ORG_ID}"

# 必须读到 ready=true 并保留 secret-free report，随后停止 bootstrap backend。
docker compose --env-file payload/deploy/.env \
  -f payload/deploy/docker-compose.yml stop backend
```

`first-customer` 有三个互斥 checkpoint。一个是已获授权的 second-integration reset
产生的严格 post-reset 空壳：保留 active local platform admin 与组织壳，其余 reset-owned
产品表为空。另一个是当前完整迁移链的精确 fresh seed：seed admin 已轮换密码、仅有一个
有效登录 session，且恰有一个逻辑 login 审计；该证据可以仍是 durable Outbox pending
event，也可以已经物化为 `audit_logs`，关联的同一事件只计一次。正式
`RUNTIME_ENVIRONMENT=production`（`staging` 同样 fail closed）的 fresh 路径还要求
步骤 10.2 先写入唯一、非零、与当前 target schema 精确匹配的
`release_state`/`release_history`；未登记、重复或不匹配均返回 `409`。

fresh checkpoint 的全部表、字段和证据先在同一个 `SERIALIZABLE` 事务中匹配。只有匹配
完成后，服务才删除精确的 partner 零余额 seed wallet、`Default Project` 和可选的 seed
admin `project_admin` membership，再创建唯一 active customer 及其零余额钱包。任何额外
数据、字段漂移、并发变化或删除行数不符都会返回 `409`，并回滚清理、客户创建和审计；
不会顺带创建客户用户、客户项目、Provider/Feed 凭据、application key、policy 或流量。
随后导入 customer-bound License 时，首装唯一的 reviewed customer project 必须使用
`slug=default`；最终 canonical 重启会复用该项目，不得再自动占用第二个 active-project
License seat。

第三个 checkpoint 接受受支持 reset 后已由正常启动生成的运行时壳，而不是 fresh
seed 的宽松兜底：必须是唯一 active local platform admin（reset 已推进 auth epoch）、
唯一 active partner 根组织、精确自动项目形状、全零 CNY wallet、reset 设置、完整制品
迁移账本和空业务表。项目成员、预算、业务引用、非登录审计或登录审计/outbox 的项目
引用均拒绝。可以保留同一管理员的多个合法登录记录及历史会话；提交时仍锁定并重新
验证当前会话。License/Release 等平台状态、admin/verifier、平台组织及登录证据不修改。

在平台运维的“部署与网络”入口输入客户名称，先执行只读预检。对应 API 请求体为
`{"organization_name":"<approved customer name>","dry_run":true}`；使用现有认证和
client-request-id 合同，不输出凭据。预检列出 `would_remove.project_id` 和
`would_remove.zero_wallet_org_id`（strict-reset 无空壳对象），不写入任何数据或 receipt。
操作者应确认这些精确对象的归位权限后再提交；预检结果不绕过提交时重新执行的完整
SERIALIZABLE 检查。runtime-reset 只原子移除该自动空项目和全零钱包，并创建独立客户及
其零余额钱包；客户默认项目按原有启动流程生成。失败不得通过删除审计、改组织类型、
手工 SQL、reset 或伪造 probe 来凑前态。此步骤不授权任何供应商流量或 canonical 切换。

确认 backend 已停止后，手工把 `payload/deploy/.env` 的
`MODEL_ROUTE_AUTHORITY` 改为 `canonical`、
`MODEL_ROUTE_COMPATIBILITY_ENABLED` 改为 `false`；保持
`ASYNC_MEDIA_ENABLED=false`。保存并复核精确 diff 后，再从新命令块执行 preflight
和 recreate，避免整段复制时跳过人工切换：

```bash
bash ./payload/deploy/preflight.sh payload/deploy/.env
docker compose --env-file payload/deploy/.env \
  -f payload/deploy/docker-compose.yml up -d --no-deps --force-recreate backend
docker compose --env-file payload/deploy/.env \
  -f payload/deploy/docker-compose.yml exec backend \
  wget -qO- http://localhost:8080/health/ready
```

启动日志必须读回 `Release trust mode: release`、非零 License/upgrade key 数量、
签名版本/commit 和批准的部署标识。出现 `development`、mutable tag、缺失签名
密钥或镜像不符都应停止切换。`upgrade status` 还必须读回与同一签名包一致的
非零 sequence、package SHA-256、runtime version/commit 和 target schema；在此
之前不得接入客户流量。只有 canonical restart 的启动数据库 gate、readiness 和
release readback 全部通过，且确认没有其他 legacy writer process，才可连接客户
Gateway 入口；失败时保持隔离，不能恢复 sample 的 legacy 值继续对外服务。受控宿主
原包继续保留，容器只删除上述明确临时文件。

### 10.3 HTTPS 边界

客户 Compose 默认只把前端绑定到 `127.0.0.1:3000`，PostgreSQL、Redis、
Prometheus 和 Grafana 不暴露宿主端口。使用客户已有或经批准的 TLS 反向代理把
`https://console.example.com` 转发到 `127.0.0.1:3000`，并在安全组/防火墙只开放所需
入口。不要为获得 TLS 而改用根目录 canary/source Compose。

客户模板把 frontend 固定在 `UNIROUTER_FRONTEND_PROXY_IP`，preflight 要求该地址
位于专用 IPv4 `/24`、不是 network/broadcast/gateway，且其 `/32` 必须出现在
`TRUSTED_PROXY_CIDRS`。默认 gateway `/32` 只覆盖同宿主受控 TLS 代理；代理位于
其他主机或容器时，应以 frontend 实际观察到的 hop IP/CIDR 替换或追加，并保持
`UNIROUTER_LISTEN_ADDRESS=127.0.0.1`。这样浏览器 IP 可用于登录限流与 API Key
allowlist，同时外部请求不能直接伪造 XFF 获得信任。

---

## 十一、安全加固

| 措施 | 说明 |
|------|------|
| HTTPS | Nginx 反向代理 + Let's Encrypt SSL |
| 防火墙 | 只开放 80/443 端口 |
| 数据库 | 不暴露外部端口，只通过 Docker 网络访问 |
| CORS | `ADMIN_CORS_ALLOWED_ORIGINS` 只允许精确 origin；禁止 `*` |
| 登录限流 | `LOGIN_RATE_LIMIT_*` 默认开启；独立 IP 桶与跨 IP 共享的 username 桶同时生效；Redis 状态不可用时在密码验证前 fail-closed 为 `503`，并返回 `Retry-After: 1` |
| Metrics | `/metrics` 默认兼容 Prometheus；生产用 `METRICS_ALLOWED_CIDRS`、防火墙或反向代理限制，或设置 `METRICS_ENABLED=false` |
| Redis | 设置密码，不暴露外部端口 |
| 密钥 | 正式模式要求 `JWT_SECRET` 与 `API_KEY_HASH_SECRET` 均至少 32 bytes；`JWT_SECRET` 可按需轮换；`API_KEY_HASH_SECRET`、`ENCRYPTION_KEY` 和通知 Webhook HMAC 轮换前必须确认 API Key、上游通道密钥及接收端双密钥窗口的影响；正式验签根由签发/发布方管理 |
| Grafana | 设置 `GRAFANA_ADMIN_PASSWORD`，禁止继续使用 `admin/admin` |
| 备份 | 备份文件加密存储 |
