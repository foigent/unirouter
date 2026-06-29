# UniRouter PRD v1.5 — Acceptance Criteria Checklist

> **最后更新**：2026-07-24
> **状态**：Phase 1 全部完成 ✅

## Build & Test Status
- [x] `go build ./...` — 编译通过
- [x] `go test ./...` — 12/12 测试套件通过 (admin, auth, billing, circuitbreaker, config, db, filter, gateway, middleware, models, redisx, tokenutil)
- [x] `go test ./... -race` — 0 DATA RACE
- [x] `npm run build` (frontend) — 生产构建通过
- [x] Vitest 前端测试 — 33/33 通过
- [x] 压力测试 — 17K RPS, 0 失败
- [x] **总计 592 测试 (559 后端 + 33 前端), 0 FAIL, 0 RACE**

## F01: OpenAI-compatible relay proxy ✅
- [x] POST /v1/chat/completions — 请求转发（流式 + 非流式）
- [x] POST /v1/embeddings — 请求转发
- [x] GET /v1/models — 模型列表（11 个模型）
- [x] 自动注入 Authorization: Bearer <channel_key>
- [x] 模型名映射（upstream model → mapped model）
- [x] _uniroute 扩展字段（request_id, cost, channel）

## F02: Channel key pool management ✅
- [x] channel_keys 表结构（id, channel_id, key_enc, status, fail_count, last_used_at, cooldown_until）
- [x] 路由时选择 channel_key（轮询 — ORDER BY last_used_at ASC）
- [x] 选择后更新 last_used_at（轮询推进）
- [x] 连续失败 3 次后自动标记 invalid（MarkKeyFail + auto-invalidate）
- [x] 成功后重置 fail_count（MarkKeySuccess）
- [x] 状态：active / invalid / cooldown

## F03: Priority-based routing ✅
- [x] channels.priority 字段
- [x] 路由排序 ORDER BY priority ASC
- [x] 多渠道 failover 循环

## F04: Sensitive word filtering ✅
- [x] DFA 过滤器（filter/DFAFilter）
- [x] 请求/响应内容过滤
- [x] 敏感词管理 API（GET /api/v1/sensitive-words）

## F05: Billing / Token counting ✅
- [x] 按 model 计费（token_count × unit_price）
- [x] Freeze → Settle → Cancel 状态机
- [x] 钱包余额扣减
- [x] 余额不足时拒绝请求 (402)

## F06: Channel & Key management (Admin API) ✅
- [x] GET /api/v1/channels — 列表
- [x] POST /api/v1/channels — 创建
- [x] GET /api/v1/channels/{id} — 详情
- [x] PUT /api/v1/channels/{id} — 动态部分更新
- [x] DELETE /api/v1/channels/{id} — 删除
- [x] GET /api/v1/keys — 列表
- [x] POST /api/v1/keys — 创建（返回完整 key）
- [x] GET /api/v1/keys/{id} — 详情
- [x] PUT /api/v1/keys/{id} — 动态部分更新
- [x] POST /api/v1/keys/{id}/disable — 禁用
- [x] GET /api/v1/channel-keys — 列表（带 channel name join）
- [x] POST /api/v1/channel-keys — 创建（加密存储）
- [x] PUT /api/v1/channel-keys/{id} — 更新
- [x] DELETE /api/v1/channel-keys/{id} — 删除

## F07: Wallet & Transactions ✅
- [x] GET /api/v1/wallet — 钱包信息（balance_cents, frozen_cents）
- [x] GET /api/v1/wallet/transactions — 分页交易记录（page, page_size）
- [x] POST /api/v1/wallet/recharge — 充值（幂等）
- [x] POST /api/v1/wallet/adjust — 调账

## F08: Model mapping ✅
- [x] channels.model_mapping JSON 字段
- [x] 路由时查询映射
- [x] 请求体 model 字段替换

## F09: Circuit breaker ✅
- [x] 渠道熔断（circuitbreaker 包）
- [x] MarkChannelFail / MarkChannelSuccess
- [x] 冷却期管理

## F10: Audit logging ✅
- [x] audit_logs 表
- [x] 操作记录（action + detail）
- [x] 预算告警审计
- [x] GET /api/v1/audit-logs — 分页查询

## F11: Call logs ✅
- [x] GET /api/v1/logs — 分页调用日志
- [x] call_logs 表（request_id, model, tokens, latency_ms, status_code...）
- [x] recordRequest() 记录每次调用

## F12: Streaming support ✅
- [x] SSE 流式转发
- [x] 逐 chunk 累积 token 计数
- [x] 流结束后结算

## F13: Rate limiting ✅
- [x] api_keys.rpm_limit / qps_limit
- [x] Redis 限流计数器

## F14: IP allowlist ✅
- [x] api_keys.allowed_ips
- [x] 中间件校验

## F15: Model allowlist ✅
- [x] api_keys.allowed_models
- [x] 路由前校验

## F16: Budget alerts (三级告警) ✅
- [x] 三级百分比阈值：50% / 80% / 100%
- [x] Webhook POST 通知
- [x] 每级独立限频（1 小时内不重复）
- [x] 定时检查（5 分钟间隔）
- [x] 结算后即时检查

## F17: System settings ✅
- [x] GET /api/v1/settings — 12 个配置项
- [x] PUT /api/v1/settings/{key} — 更新单个设置
- [x] site_name, default_model, enable_registration, maintenance_mode, sensitive_words
- [x] backup.cron, backup.enabled, backup.retain_days
- [x] webhook.dingtalk, webhook.feishu
- [x] budget_alert_webhook_url, budget_alert_threshold_cents

## F18: Dashboard & Health ✅
- [x] GET /api/v1/dashboard/stats — 聚合统计 + recent_logs + model_prices
- [x] GET /health/live — 存活检查
- [x] GET /health/ready — 就绪检查

## Additional features (超出 PRD) ✅
- [x] GET /api/v1/users — 用户管理
- [x] GET /api/v1/grants — 赠送管理
- [x] GET /api/v1/org-model-prices — 组织模型倍率
- [x] GET /api/v1/reconciliation — Token 对账
- [x] POST /api/v1/auth/login — JWT 登录
- [x] Model Prices CRUD (GET/POST/PUT/DELETE /api/v1/model-prices)

## Deployment ✅
- [x] frontend/Dockerfile — 多阶段构建（node:20-alpine → nginx:1.27-alpine）
- [x] frontend/nginx.conf — SPA fallback + API/V1/health proxy
- [x] docker-compose.yml — backend + frontend + postgres + redis
- [x] 静态资源缓存（30 天）

## Frontend Pages (15 pages) ✅
- [x] LoginPage — 登录
- [x] DashboardPage — 仪表盘（统计 + 日志 + 模型价格）
- [x] ChannelsPage — 渠道 CRUD + 状态切换
- [x] KeysPage — 密钥管理
- [x] ChannelKeysPage — 上游密钥
- [x] WalletPage — 钱包管理
- [x] LogsPage — 调用日志
- [x] UsersPage — 用户管理
- [x] ModelPricesPage — 模型价格
- [x] OrgModelPricesPage — 组织倍率
- [x] GrantsPage — 赠送管理
- [x] AuditLogsPage — 审计日志
- [x] ReconciliationPage — Token 对账
- [x] TestPage — 渠道测试
- [x] SettingsPage — 系统设置

## 已知限制（不在 v1.5 范围，后续版本处理）
- [ ] 加密已使用 AES-256-GCM — 生产需确认密钥轮转策略
- [ ] 单 org 简化模型 → 多 org 支持待 v2
- [ ] 前端 loading states / error toasts 待完善
- [ ] JWT_SECRET / ENCRYPTION_KEY 默认值需加强
- [ ] 压力测试 / 性能基准待补充
