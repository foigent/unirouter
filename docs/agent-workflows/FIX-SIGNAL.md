# FIX-SIGNAL.md — PRD2 Batch 28 Product Consistency And Filter Parity

**修复者：** main agent
**日期：** 2026-07-04
**修复轮次：** PRD2 Batch 28

---

## 前置条件（本轮预期不变的部分）

| 项目 | 预期状态 | 原因 |
|------|---------|------|
| PRD2 Batch 25 final closeout | 已通过 Canary/Sentinel per previous status | 本轮是 post-closeout 产品一致性 polish，不改变 PRD2 feature closeout 结论 |
| PRD2 Batch 27 copy/navigation polish | Canary PASS，User reported Canary/Sentinel/Atlas completed | `VERIFY-RESULT.md` 当前记录 Batch 27 PASS 7/7；本轮不重复验 Batch 27 |
| Billing / settlement / reconciliation semantics | 不应被重写 | 本轮只统一显示标签、筛选入口和 API 人类可读 label |
| `usage_source` stable values | 保持不变 | UI 显示中文，但仍发送 `upstream`、`counter_v2`、`adapter_estimated` 等稳定 API 值 |
| Production verified evidence | 不新增 | 本轮只有本地源码、测试、build、Go test 和 docs evidence |

> Canary 验收时：请重点核验本轮产品命名、筛选项和 enum 显示一致性，以及没有重写账务/计量路径。不要把 Batch 18 真实上游凭证、P2-F10 生产 License、生产 HA/DR 或预算 hard-block 当作 Batch 28 失败项。

---

## Fix #1: Shared Display Label Dictionary

**文件：**

- `frontend/src/utils/displayLabels.ts`

**原问题：** 多个页面各自维护 `usage_source`、通知事件、投递状态、审计来源等选项，导致 UI 暴露 `counter_v2`、`admin_console`、`project_budget.threshold`、`failed` 等内部枚举。

**当前代码：**

- 新增共享 display label dictionary。
- API 值保持原始稳定值，显示值统一为中文企业控制台文案。
- 覆盖范围包括：用量分析分组、请求状态、计量来源、审计来源、通知事件类型、事件级别、投递状态、内部状态。

**验证方法：**

```bash
rg -n "usageAnalyticsGroupOptions|usageSourceLabel|auditSourceLabel|eventTypeLabel|deliveryStateLabel|internalStatusLabel" frontend/src/utils/displayLabels.ts
```

---

## Fix #2: Usage Analytics Filter Surface Matches Backend Contract

**文件：**

- `frontend/src/pages/UsageAnalyticsPage.tsx`
- `frontend/src/api/client.ts`（已具备对应参数，本轮未改）
- `internal/admin/prd2_product_handlers.go`

**原问题：** 后端 `GET /api/v1/usage/analytics` 已支持 `project_id`、`key_id`、`alias`、`channel`、`start_time`、`end_time` 等筛选，但前端只暴露了 group/status/usage_source/model/provider，且部分选项显示英文或内部字段名。

**当前代码：**

- 前端新增项目 ID、访问凭证 ID、模型别名、上游通道、开始时间、结束时间筛选。
- 时间输入转换为 RFC3339 后发送。
- 分组选项统一为：项目、访问凭证、模型、模型别名、供应商、上游通道、请求状态、计量来源。
- `usage_source` 下拉显示中文，但仍发送稳定 API 值。
- 后端只把 Usage Analytics `group_by.label` 改为中文；`group_by.key` 和 SQL 白名单表达式不变。

**验证方法：**

```bash
rg -n "项目 ID|访问凭证 ID|模型别名|上游通道|开始时间|结束时间|toRFC3339|usageAnalyticsGroupOptions" frontend/src/pages/UsageAnalyticsPage.tsx
go test ./internal/admin -run 'TestUsageAnalyticsGroup|TestUsageAnalyticsUpstreamSourceSQLIncludesNull|TestInternalStatusLevel' -count=1
```

---

## Fix #3: Metering Source Labels Are Normalized Across Logs And Reconciliation

**文件：**

- `frontend/src/pages/LogsPage.tsx`
- `frontend/src/pages/ReconciliationPage.tsx`
- `frontend/src/utils/displayLabels.ts`

**原问题：** 请求日志、用量对账直接展示 `api`、`counter_v2`、`adapter_estimated` 等内部来源值，且当前筛选 chip 直接显示 `usage_source: counter_v2`。

**当前代码：**

- `api` / `upstream` 显示为 `上游回传`。
- `counter_v2` / `stream_estimate` 显示为 `流式计数`。
- `adapter_estimated` 显示为 `适配器估算`。
- `estimated` 显示为 `本地估算`。
- `no_route` 显示为 `无可用通道`。
- `upstream_failed` 显示为 `上游失败`。
- 请求日志当前筛选 chip 使用中文 filter key/value。
- 用量对账的 `filtered_query` meta 显示为 `当前筛选口径`。

**验证方法：**

```bash
rg -n "usageSourceLabel|当前筛选口径|计量来源" frontend/src/pages/LogsPage.tsx frontend/src/pages/ReconciliationPage.tsx
npm --prefix frontend test -- --run LogsPage ReconciliationPage
```

---

## Fix #4: Audit, Notification, And Internal Status Enum Labels Are Productized

**文件：**

- `frontend/src/pages/AuditLogsPage.tsx`
- `frontend/src/pages/NotificationCenterPage.tsx`
- `frontend/src/pages/InternalStatusPage.tsx`
- `frontend/src/utils/displayLabels.ts`

**原问题：** 审计日志和通知中心暴露 `admin_console`、`project_budget.threshold`、`critical`、`failed` 等内部枚举，不像企业级产品界面。

**当前代码：**

- 审计来源显示为管理控制台、开发者工作台、网关 API、系统任务。
- 通知事件显示为项目预算阈值、访问凭证停用、模型弃用、模型下线、上游通道故障、License 预警、估算占比过高。
- 事件级别显示为信息、预警、严重。
- 投递状态显示为未配置、待投递、已投递、投递失败。
- 内部状态显示为正常、预警、降级；事件状态 `open` 显示为未关闭。

**验证方法：**

```bash
rg -n "管理控制台|项目预算阈值|投递失败|上游通道故障|严重事件|预警事件" frontend/src/pages/AuditLogsPage.tsx frontend/src/pages/NotificationCenterPage.tsx frontend/src/pages/InternalStatusPage.tsx
npm --prefix frontend test -- --run AuditLogsPage NotificationCenterPage InternalStatusPage
```

---

## Fix #5: Remaining Touched Enterprise UI Terms Are Aligned

**文件：**

- `frontend/src/pages/DeveloperConsolePage.tsx`
- `frontend/src/pages/ApiDocsPage.tsx`
- `frontend/src/pages/ChannelKeysPage.tsx`
- `frontend/src/pages/ChannelsPage.tsx`
- `frontend/src/pages/KeysPage.tsx`
- `frontend/src/pages/DashboardPage.tsx`
- `frontend/src/pages/TestPage.tsx`
- `frontend/src/pages/UsersPage.tsx`

**原问题：** Batch 27 后仍有局部旧称或英文实现术语：`Project ID`、`项目 API Keys`、`Playground`、`Request Lookup`、`Resolved`、`API key`、`Traffic`、`OpenAI-compatible`、`检测 Prompt`。

**当前代码：**

- 开发者工作台改为项目访问凭证、请求调试台、请求定位、路由模型、项目 ID。
- API 文档改为项目访问凭证、请求调试台、兼容 OpenAI 协议。
- 上游凭证页、访问凭证页、渠道页和用户页 eyebrow/description 统一到流量治理/访问控制。
- 概览渠道供应商显示为 `兼容 OpenAI 协议`。
- 连通性检测提示词字段改为 `检测提示词`。

**验证方法：**

```bash
rg -n "项目访问凭证|请求调试台|请求定位|路由模型|兼容 OpenAI 协议|检测提示词|流量治理|访问控制" frontend/src/pages
npm --prefix frontend test -- --run DeveloperConsolePage DashboardPage TestPage
```

---

## Fix #6: Verification And Evidence Are Updated For Batch 28

**文件：**

- `docs/ops/evidence/PRD2-B28-product-consistency-local-20260704.md`
- `docs/ops/CURRENT-STATUS.md`
- `docs/ops/PRD2-COMPLETION-PLAN.md`
- `docs/agent-workflows/FIX-SIGNAL.md`

**当前状态：**

- Batch 27 is treated as Canary PASS per `VERIFY-RESULT.md` and User report that Canary/Sentinel/Atlas completed.
- Batch 28 is fixed locally awaiting Canary.
- Evidence records commands, scope, and explicit accounting compatibility boundary.

**验证方法：**

```bash
rg -n "Batch 28|Product Consistency|PRD2-B28-product-consistency-local|fixed locally awaiting Canary|Batch 27.*PASS" docs/ops/CURRENT-STATUS.md docs/ops/PRD2-COMPLETION-PLAN.md docs/ops/evidence/PRD2-B28-product-consistency-local-20260704.md docs/agent-workflows/FIX-SIGNAL.md docs/agent-workflows/VERIFY-RESULT.md
```

---

## Local Validation Already Run By Codex

```bash
npm --prefix frontend test -- --run
npm --prefix frontend run build
go test ./internal/admin -count=1
go test ./... -count=1
git diff --check
```

Observed result:

- Frontend tests: PASS, 16 files / 88 tests.
- Frontend build: PASS, existing Vite large chunk warning only.
- `go test ./internal/admin -count=1`: PASS.
- `go test ./... -count=1`: PASS.
- `git diff --check`: PASS.

---

## Suggested Canary Result Template

请 OpenClaw Canary 覆盖写入 `docs/agent-workflows/VERIFY-RESULT.md`：

```markdown
# VERIFY-RESULT.md — PRD2 Batch 28 验收结果

**验收者：** OpenClaw Canary
**日期：** 2026-07-04
**对应轮次：** PRD2 Batch 28 Product Consistency And Filter Parity

## 逐条结果

- Fix #1: PASS/FAIL — shared display label dictionary
- Fix #2: PASS/FAIL — Usage Analytics filter parity and backend label-only change
- Fix #3: PASS/FAIL — Logs/Reconciliation metering source labels
- Fix #4: PASS/FAIL — Audit/Notification/Internal Status enum labels
- Fix #5: PASS/FAIL — Remaining touched enterprise UI terms
- Fix #6: PASS/FAIL — Evidence/status handoff

## 命令结果

| 命令 | 结果 | 备注 |
|------|------|------|
| `npm --prefix frontend test -- --run` | PASS/FAIL | |
| `npm --prefix frontend run build` | PASS/FAIL | |
| `go test ./... -count=1` | PASS/FAIL | |
| `git diff --check` | PASS/FAIL | |

## 新发现

- None / list findings

## 结论

PASS/FAIL.
```
