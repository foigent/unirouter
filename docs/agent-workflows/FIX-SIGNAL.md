# FIX-SIGNAL.md — PRD2 implementation batch 7 Notification/Event backend foundation

**修复者：** main agent
**日期：** 文件系统时间戳为准
**修复轮次：** PRD2 implementation batch 7

---

## 前置条件（本轮预期不变的部分）

| 项目 | 预期状态 | 原因 |
|------|---------|------|
| PRD2 batch 6 | Canary PASS + Sentinel complete | User 已报告 Canary 和 Sentinel 都完成，本轮同步状态并推进 batch 7 |
| Frontend notification center | 仍未完成 | 本轮只做 Notification/Event 后端基础，不做前端通知中心 |
| Email / IM delivery | 仍未完成 | PRD2 允许分阶段，batch 7 只做 webhook route |
| Background retry worker | 仍未完成 | 本轮记录 failed delivery 和 `next_retry_at`，不启动自动重试 worker |
| Production webhook E2E | 仍未完成 | 本轮提供 schema、service、API 和单元测试证据 |
| Key/model/provider/license/estimated-usage emitters | 仅保留事件类型扩展空间 | 本轮只实际接入 project budget threshold 事件 |

> Canary 验收时：上述未完成项不算 FAIL；如果本轮声称完成前端通知中心、邮件/IM、自动 retry worker、生产 webhook E2E 或所有事件 emitters 全覆盖，则应标为 FAIL。

---

## Fix #1: Batch 6 验收状态同步

**文件：** `docs/ops/CURRENT-STATUS.md`, `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`

**原问题：** User 报告 batch 6 的 Canary 和 Sentinel 已完成，但状态文档仍停留在 awaiting Canary。

**当前实现：**

- `CURRENT-STATUS.md` 记录 batch 6 为 Canary passed / Sentinel complete。
- `PRD2-ACCEPTANCE-MATRIX.md` 将 P2-F07 Audit Schema Upgrade 证据更新为 Canary 6/6 PASS + User reported Sentinel completed。
- 当前活跃轮次更新为 batch 7 fixed locally awaiting Canary。

**验证方法：**

```bash
sed -n '1,34p' docs/ops/CURRENT-STATUS.md
sed -n '56,72p' docs/ops/PRD2-ACCEPTANCE-MATRIX.md
```

---

## Fix #2: Notification/Event schema and tech-ref

**文件：** `migrations/000014_prd2_notification_event_foundation.up.sql:6`, `migrations/000014_prd2_notification_event_foundation.down.sql`, `internal/models/models.go`, `docs/tech-ref/14-notification-event-foundation.md`, `docs/tech-ref/README.md`, `docs/DOC-INVENTORY.md`

**原问题：** PRD2 P2-F08 要求事件持久化、webhook routing 和 delivery failure visibility，但当前只有 PRD1 全局低余额 webhook alert，缺少 PRD2 事件层。

**当前实现：**

- 新增 `notification_events`。
- 新增 `notification_webhooks`。
- 新增 `notification_deliveries`。
- 新增 delivery state：`not_configured` / `pending` / `delivered` / `failed`。
- 新增 partial unique dedupe index，避免同一项目同一周期预算阈值重复事件。
- `models.go` 增加 `NotificationEvent`、`NotificationWebhook`、`NotificationDelivery`。
- 新增 tech-ref 14 并写入技术参考索引和文档清单。

**验证方法：**

```bash
test -f docs/tech-ref/14-notification-event-foundation.md
test -f migrations/000014_prd2_notification_event_foundation.up.sql
test -f migrations/000014_prd2_notification_event_foundation.down.sql
rg -n "notification_events|notification_webhooks|notification_deliveries|delivery_state" migrations/000014_prd2_notification_event_foundation.up.sql internal/models/models.go docs/tech-ref/14-notification-event-foundation.md
```

---

## Fix #3: EventService persists events and records webhook delivery state

**文件：** `internal/billing/notifications.go:52`, `internal/billing/notifications_test.go`

**原问题：** 现有 `AlertService` 只做 PRD1 全局钱包告警，成功时写 audit，不支持独立事件表、webhook route 匹配、delivery failure visibility。

**当前实现：**

- `EventService.Emit` 持久化事件。
- 根据 `org_id`、可选 `project_id` 和 `event_types` 匹配 enabled webhook routes。
- webhook 成功时写 `notification_deliveries.status='delivered'`。
- webhook 失败时写 `status='failed'`、`response_status/error` 和 `next_retry_at`。
- 无 webhook 时事件 `delivery_state='not_configured'`。

**关键测试：**

- `TestEventService_EmitDeliversWebhook`
- `TestEventService_EmitRecordsFailedDelivery`

**验证方法：**

```bash
go test ./internal/billing -run 'TestEventService_Emit' -v
```

---

## Fix #4: Project budget threshold emits deduplicated event

**文件：** `internal/billing/notifications.go:138`, `internal/gateway/relay.go:62`, `internal/gateway/relay.go:919`

**原问题：** PRD2 要求项目预算过阈值创建事件并按配置 webhook 发送；现有 relay 只触发全局 wallet alert，且检查发生在 `call_logs` 写入前，无法用当前调用成本做项目预算聚合。

**当前实现：**

- `EventService.CheckProjectBudget` 读取 active project 的 `budget_cents` / `budget_period`。
- 按 `daily` 或 `monthly` 周期聚合 `call_logs.cost_cents`。
- 过阈值后创建 `project_budget.threshold` 事件。
- dedupe key 按 project + period + threshold 生成。
- relay 新增 `SetEventService`。
- relay 在 `recordRequest` 后调用 post-settle budget check，确保当前请求被计入聚合。

**关键测试：**

- `TestEventService_CheckProjectBudgetEmitsThresholdEvent`
- gateway 包现有 relay 回归测试继续通过。

**验证方法：**

```bash
go test ./internal/billing -run TestEventService_CheckProjectBudgetEmitsThresholdEvent -v
go test ./internal/gateway -run 'Test(CheckBudgetAlert|SetAlertService|Handle)' -v
```

---

## Fix #5: Admin-visible notification APIs and webhook configuration

**文件：** `internal/admin/notification_handlers.go:17`, `internal/admin/notification_handlers.go:100`, `internal/admin/notification_handlers.go:254`, `internal/admin/notification_handlers.go:308`, `cmd/uniroute/main.go:467`

**原问题：** PRD2 要求至少 admin-visible notification records 和 webhook routing；当前没有 notification/event admin API。

**当前实现：**

- 新增 `GET /api/v1/notification-events`。
- 新增 `GET /api/v1/notification-deliveries`。
- 新增 `GET /api/v1/notification-webhooks`。
- 新增 `POST /api/v1/notification-webhooks`。
- 新增 `PUT /api/v1/notification-webhooks/{id}`。
- Project-scoped admin 查询必须提供可访问 `project_id`。
- Webhook create/update 写审计 before/after where applicable。
- Route parity guard 增加新 API 路由。

**关键测试：**

- `TestListNotificationEvents_Success`
- `TestListNotificationDeliveries_ShowsFailureState`
- `TestCreateNotificationWebhook_WritesAudit`
- `TestAdminRoutesRegisteredForFrontendClient`

**验证方法：**

```bash
go test ./internal/admin -run 'Test(ListNotificationEvents|ListNotificationDeliveries|CreateNotificationWebhook)' -v
go test ./cmd/uniroute -run TestAdminRoutesRegisteredForFrontendClient -v
```

---

## Fix #6: Project budget configuration audit before/after

**文件：** `internal/admin/project_handlers.go:174`, `internal/admin/project_handlers.go:243`, `internal/admin/project_handlers.go:334`, `internal/admin/project_handlers.go:387`

**原问题：** Project budget fields existed, but project create/update audit did not preserve budget before/after state required by P2-F08 acceptance.

**当前实现：**

- Project create audit writes after-state with `budget_cents` and `budget_period`.
- Project update reads `projectAuditSnapshot` before update.
- Project update audit writes changed fields plus `before` and `after`.

**验证方法：**

```bash
go test ./internal/admin -run TestUpdateProject_Success -v
rg -n "projectAuditSnapshot|budget_cents|budget_period" internal/admin/project_handlers.go
```

---

## Fix #7: PRD2 matrix/status updated without overclaim

**文件：** `docs/ops/CURRENT-STATUS.md`, `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`

**原问题：** P2-F08 was still Planned and batch 6 status was stale.

**当前实现：**

- P2-F08 Notification/Event foundation is marked `Unit tested`, not E2E accepted.
- Project budget alerts are marked `Unit tested`, not E2E accepted.
- Remaining gaps explicitly include Canary verification, frontend notification center, retry worker, production webhook E2E, and remaining emitters.

**验证方法：**

```bash
sed -n '1,38p' docs/ops/CURRENT-STATUS.md
sed -n '64,76p' docs/ops/PRD2-ACCEPTANCE-MATRIX.md
```

---

## 全局验证命令

Canary 请从仓库根目录执行：

```bash
go test ./internal/billing ./internal/admin ./internal/gateway ./cmd/uniroute
go test ./...
git diff --check
git diff --check -- $(git ls-files --others --exclude-standard)
```

## 期望的 VERIFY-RESULT.md 输出格式

请覆盖写入 `docs/agent-workflows/VERIFY-RESULT.md`：

```markdown
# VERIFY-RESULT.md — PRD2 Implementation Batch 7 验收结果

**验收者：** OpenClaw Canary
**日期：** 2026-07-01
**对应轮次：** PRD2 implementation batch 7 — Notification/Event backend foundation

## 逐条结果

- Fix #1: PASS/FAIL — evidence
- Fix #2: PASS/FAIL — evidence
- Fix #3: PASS/FAIL — evidence
- Fix #4: PASS/FAIL — evidence
- Fix #5: PASS/FAIL — evidence
- Fix #6: PASS/FAIL — evidence
- Fix #7: PASS/FAIL — evidence

## 前置条件核验

- Frontend notification center 未声称完成: PASS/FAIL
- Email / IM delivery 未声称完成: PASS/FAIL
- Background retry worker 未声称完成: PASS/FAIL
- Production webhook E2E 未声称完成: PASS/FAIL
- Remaining emitters 未声称全覆盖: PASS/FAIL

## 全局验证

| 检查项 | 结果 |
|--------|------|
| `go test ./internal/billing ./internal/admin ./internal/gateway ./cmd/uniroute` | PASS/FAIL |
| `go test ./...` | PASS/FAIL |
| `git diff --check` | PASS/FAIL |
| schema + tech-ref | PASS/FAIL |
| EventService webhook delivery tests | PASS/FAIL |
| Project budget threshold test | PASS/FAIL |
| Admin notification API tests | PASS/FAIL |
| Matrix 未过度标注 | PASS/FAIL |

## 新发现的问题

无 / 列表

## 结论

PASS/FAIL summary
```
