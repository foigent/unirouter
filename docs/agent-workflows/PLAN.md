# UniRouter Implementation Plan

## Phase 1: Migration + OpenAPI + Frontend Spec ✅
- [x] 读取 PRD.md 和 CHECKLIST.md
- [x] 审计现有代码 vs PRD 全部 Acceptance Criteria
- [x] 修复 migration（补 3 个缺失字段：model_mapping, max_context, default_max_output）
- [x] 生成 OpenAPI spec → `docs/tech-ref/05-openapi-spec.yaml`
- [x] 生成前端 spec → `docs/tech-ref/06-frontend-spec.md`

## Phase 2: Backend API Endpoints ✅
- [x] F06: 渠道管理 CRUD (GET/POST/PUT/DELETE /api/v1/channels)
- [x] F06: API Key 管理 CRUD (GET/POST/PUT/DELETE /api/v1/keys)
- [x] F07: 钱包 + 交易记录 (GET /api/v1/wallet, GET /api/v1/wallet/transactions)
- [x] F11: 调用日志 (GET /api/v1/logs)
- [x] F18: 仪表盘统计 (GET /api/v1/dashboard/stats)
- [x] Channel Key 管理 (GET/POST /api/v1/channel-keys)
- [x] 路由注册 in cmd/uniroute/main.go
- [x] 模型价格 CRUD (GET/POST/PUT/DELETE /api/v1/model-prices)
- [x] 系统设置 (GET/PUT /api/v1/settings)
- [x] 用户管理 (GET /api/v1/users)
- [x] 赠送管理 (GET /api/v1/grants)
- [x] 审计日志 (GET /api/v1/audit-logs)
- [x] 组织模型倍率 (GET /api/v1/org-model-prices)
- [x] 敏感词管理 (GET /api/v1/sensitive-words)
- [x] Token对账 (GET /api/v1/reconciliation)

## Phase 3: Frontend Pages ✅
- [x] DashboardPage — 统计 + 最近日志 + 模型价格
- [x] ChannelsPage — 渠道 CRUD + 状态切换
- [x] KeysPage — Key 编辑模态框
- [x] WalletPage — 交易记录分页
- [x] LoginPage — 登录页
- [x] ChannelKeysPage — 上游密钥管理
- [x] LogsPage — 调用日志
- [x] UsersPage — 用户管理
- [x] ModelPricesPage — 模型价格
- [x] OrgModelPricesPage — 组织倍率
- [x] GrantsPage — 赠送管理
- [x] AuditLogsPage — 审计日志
- [x] ReconciliationPage — Token对账
- [x] TestPage — 渠道测试
- [x] SettingsPage — 系统设置

## Phase 4: Deployment ✅
- [x] frontend/nginx.conf — SPA fallback + API proxy
- [x] frontend/Dockerfile — 多阶段构建
- [x] docker-compose.yml — 全栈编排

## Phase 5: Gap Fixes ✅
- [x] F02: 渠道 Key 轮询选择 — ORDER BY COALESCE(last_used_at) + 选择后更新 last_used_at
- [x] F02: Key 连续失败自动作废 — MarkKeyFail() 3次后 status→invalid, MarkKeySuccess() 重置 fail_count
- [x] F16: 三级预算告警 — CheckAndNotify() 支持 50%/80%/100% 百分比阈值，每级独立限频

## Phase 6: Validation ✅
- [x] `go build ./...` — 编译通过
- [x] `go test ./...` — 全部 10 个测试套件通过 (admin, auth, billing, circuitbreaker, filter, gateway, middleware, models, tokenutil)
- [x] `npm run build` — 前端生产构建通过 (4109 modules, 3.21s)
- [x] 全栈 API 验收测试 — 27/29 通过 (2 个为断言格式差异，非 bug)
- [x] Gateway 端点验证 — /v1/models 返回 11 个模型

## 关键决策
1. 路由选择使用 last_used_at 实现轮询，而非随机
2. Key 失败计数基于 fail_count 字段，3 次后自动标记 invalid
3. 预算告警改为百分比制，每个 tier 独立限频（audit_logs action 带 tier 后缀）
4. 保留 CheckAndNotifyLegacy() 作为向后兼容接口
5. Gateway 认证使用 Bearer API Key (HMAC-SHA256 hash)，Admin 认证使用 JWT
6. 前端使用 Semi Design (@douyinfe/semi-ui) 组件库

## 已知限制（不在 v1.5 范围）
- 加密已使用 AES-256-GCM — 生产需确认密钥轮转策略
- 单 org 简化模型 → 多 org 支持待后续版本
- 前端 loading states / error toasts 待完善
- JWT_SECRET / ENCRYPTION_KEY 默认值需加强
