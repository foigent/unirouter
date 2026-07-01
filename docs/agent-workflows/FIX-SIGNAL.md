# FIX-SIGNAL.md — PRD2 implementation batch 9 P0 E2E Acceptance Consolidation

**修复者：** main agent
**日期：** 文件系统时间戳为准
**修复轮次：** PRD2 implementation batch 9

---

## 前置条件（本轮预期不变的部分）

| 项目 | 预期状态 | 原因 |
|------|---------|------|
| PRD2 batch 8 | Canary PASS + Sentinel complete | User 已报告 Canary 和 Sentinel 都完成，本轮同步后推进 batch 9 |
| Batch 10 Developer Console 前端 | 未实现 | 本轮只做 P0 后端/网关 E2E acceptance consolidation |
| Batch 11 Notification retry/frontend | 未实现 | 本轮不扩大到 Notification/Event 后续产品闭环 |
| Batch 12 Native Adapter Proof | 未实现 | 本轮不做 `anthropic_native` 或其它 native adapter |
| Batch 13 License Foundation | 未实现 | License policy 仍需后续设计确认 |
| Atlas 部署 / 真实 upstream provider E2E | 未执行 | 本轮只使用本地临时 PostgreSQL 和 mock OpenAI-compatible upstream |
| PRD2 matrix accepted status | 未提前提升到 `E2E accepted` | Batch 9 本地 E2E PASS 仍需 Canary/Sentinel 验证后再推进 accepted 状态 |

> Canary 验收时：上述未完成项不算 FAIL；如果本轮声称已经完成 Batch 10-13、Developer Console frontend、Native Adapter、License、Atlas 部署或真实 upstream E2E，则应标为 FAIL。

---

## Fix #1: Batch 9 scope boundary and status sync

**文件：** `docs/ops/CURRENT-STATUS.md`, `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`

**原问题：** User 询问是否可以一次性闭环 Batch 9-13。按 `AGENT-PROTOCOL.md`，Codex 不应绕过 Canary/Sentinel 连续堆多个批次；应先完成 Batch 9 的本地实现和交接。

**当前实现：**

- `CURRENT-STATUS.md` 标记当前活跃轮次为 batch 9 fixed locally awaiting Canary。
- `PRD2-ACCEPTANCE-MATRIX.md` 只记录 batch 9 本地 E2E 证据，不把相关行提前提升为 `E2E accepted`。
- Batch 10-13 保持后续计划，不混入本轮代码。

**验证方法：**

```bash
sed -n '1,30p' docs/ops/CURRENT-STATUS.md
sed -n '28,62p' docs/ops/PRD2-ACCEPTANCE-MATRIX.md
```

---

## Fix #2: Production gateway handler seam for real E2E tests

**文件：** `cmd/uniroute/main.go:153`, `cmd/uniroute/gateway_handlers.go:20`

**原问题：** `main.go` 内联 gateway chat/embeddings handler，测试只能复制逻辑，无法证明生产 handler 的 model governance、alias rewrite、allowed_models 与 relay 边界。

**当前实现：**

- 新增 `newGatewayChatHandler` 和 `newGatewayEmbeddingsHandler`。
- `main.go` 使用这两个生产 handler 注册 gateway routes。
- `prepareGatewayModelRequest` 统一处理：
  - API key context 缺失时返回 401。
  - 读取并恢复 request body。
  - 调用 `modelgov.CheckAccess`。
  - 对 retired / denied model 返回 403，并保留 `reason`。
  - alias 命中时写入 `middleware.CtxModelAlias` 并把 body model 改写为 target model。
  - 同时允许 API key allowlist 匹配 requested alias 或 resolved model。

**验证方法：**

```bash
go test ./cmd/uniroute -run 'TestChatHandler|TestAdminRoutesRegisteredForFrontendClient' -v
rg -n "newGatewayChatHandler|prepareGatewayModelRequest|applyModelGovernance|apiKeyAllowsModel" cmd/uniroute
```

---

## Fix #3: Isolated migrated PostgreSQL integration-test helper

**文件：** `internal/testutil/testdb.go:58`

**原问题：** 本机长期存在的 `uniroute_test` 可能只包含 PRD1 表，导致 PRD2 integration tests 只能 skip 或输出 missing-table warnings，无法稳定证明迁移后行为。

**当前实现：**

- 新增 `SetupIsolatedTestDB`：使用 `TEST_DATABASE_URL` 的连接参数创建临时 PostgreSQL database。
- 新增 `SetupMigratedTestDB`：在临时 database 从零执行 repo migrations，再 truncate business tables。
- 测试结束后关闭 pool 并 drop 仅由测试创建的临时 database。
- 不修改 `uniroute_test` 的 schema，不依赖现有开发库状态。

**验证方法：**

```bash
go test -tags integration ./cmd/uniroute -run TestPRD2P0DeveloperProjectKeyGatewayE2E_Integration -v
```

---

## Fix #4: 000009 PRD1-to-default-project migration E2E

**文件：** `internal/migrate/prd2_project_migration_integration_test.go:16`

**原问题：** P2-F01 的矩阵 gap 要求 migration-on-existing-DB evidence；此前只有 SQL 静态审查，没有用旧式 PRD1 org/key/log/transaction 数据执行 000009。

**当前实现：**

- 新增 `TestMigration000009BackfillsDefaultProjectForPRD1Data_Integration`。
- 在临时 DB 中先执行 PRD1-era migrations。
- 插入 PRD1 风格的 org、org_admin user、api_key、call_log、transaction。
- 单独执行 `000009_prd2_project_foundation.up.sql`。
- 断言：
  - 为旧 org 创建 `projects.slug='default'`。
  - org_admin 被加入 default project 且角色为 `project_admin`。
  - 旧 api_key 回填 `project_id`。
  - 旧 call_log 回填 `project_id`。
  - 旧 transaction 回填 `project_id`。

**验证方法：**

```bash
go test -count=1 -tags integration ./internal/migrate -run TestMigration000009BackfillsDefaultProjectForPRD1Data_Integration -v
```

---

## Fix #5: Developer project key to gateway mock-upstream E2E

**文件：** `cmd/uniroute/prd2_p0_e2e_integration_test.go:29`

**原问题：** P2-F01/P2-F03/P2-F04/P2-F05/P2-F06/P2-F07 都有跨模块 E2E gaps：developer 创建 project key、调用 gateway、model alias/model set 生效、usage_source 写入、project log/audit 串起来。

**当前实现：**

- 新增 `TestPRD2P0DeveloperProjectKeyGatewayE2E_Integration`。
- 在临时 migrated DB 中配置：
  - org、developer user、project、project membership。
  - wallet 和 model price。
  - model registry、alias `company-chat -> gpt-4o`、model set 和 project assignment。
  - OpenAI-compatible mock upstream channel + encrypted channel key。
- 通过真实 `admin.Handler.CreateAPIKey` 以 developer context 创建 project key。
- 通过真实 `GatewayAuth + newGatewayChatHandler + Relay + Router` 调 `/v1/chat/completions`。
- 断言：
  - mock upstream 收到 resolved model `gpt-4o`。
  - request id 传递到 upstream。
  - upstream key 经过 channel key 解密后进入 Authorization header。
  - gateway response 包含 `_uniroute`。
  - `call_logs` 记录 `project_id/api_key_id/model/requested_model/status_code/usage_source/total_tokens`。
  - `freeze_idempotency` 进入 `settled`。
  - `audit_logs` 记录 project id、developer user id、`source=developer_console`、after-state key name。

**验证方法：**

```bash
go test -count=1 -tags integration ./cmd/uniroute -run TestPRD2P0DeveloperProjectKeyGatewayE2E_Integration -v
```

---

## Fix #6: Model governance gateway denial E2E

**文件：** `cmd/uniroute/prd2_p0_e2e_integration_test.go:128`

**原问题：** Model set assignment and retired-model rejection had unit evidence, but no gateway E2E proof through real project key and handler.

**当前实现：**

- 在 developer gateway E2E 中复用同一 project key。
- 请求 `gpt-4o-mini`，该 model 是 GA 但未进入 project model set，期望 403 且包含 `project_model_not_allowed`。
- 请求 `legacy-model`，该 model lifecycle 为 `retired`，期望 403 且包含 `model_retired`。
- 这两个拒绝发生在 relay/billing 前，防止未授权模型进入上游。

**验证方法：**

```bash
go test -count=1 -tags integration ./cmd/uniroute -run TestPRD2P0DeveloperProjectKeyGatewayE2E_Integration -v
```

---

## Fix #7: Route-level RBAC negative E2E with real JWTs

**文件：** `cmd/uniroute/prd2_p0_e2e_integration_test.go:132`

**原问题：** P2-F02 的 route-level E2E gap 要求真实 JWT 下验证 developer、billing_admin、project_admin 的负向访问，而不只看 helper/unit tests。

**当前实现：**

- 新增 `TestPRD2P0RouteRBACNegativeE2E_Integration`。
- 使用真实 JWT 和 `middleware.AdminAuth + RequireRoles`。
- 断言：
  - developer JWT 访问 platform-only channel route 返回 403。
  - billing_admin JWT 写 provider channel route 返回 403。
  - developer JWT 访问 global audit route 返回 403。
  - project_admin JWT 访问未加入项目的 `/api/v1/logs?project_id=<other>` 返回 403。

**验证方法：**

```bash
go test -count=1 -tags integration ./cmd/uniroute -run TestPRD2P0RouteRBACNegativeE2E_Integration -v
```

---

## Fix #8: Matrix/status updated without overclaim

**文件：** `docs/ops/CURRENT-STATUS.md`, `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`

**原问题：** Batch 9 本地 E2E 证据应进入矩阵，但在 Canary/Sentinel 前不应提前标注为 `E2E accepted`。

**当前实现：**

- `CURRENT-STATUS.md` 记录 batch 9 fixed locally awaiting Canary。
- `PRD2-ACCEPTANCE-MATRIX.md` 更新 P0 行的 local E2E evidence。
- 相关 rows 仍保持 `Unit tested` 或原状态，并把 Canary verification 作为 gap。
- Developer Console frontend、request troubleshooting UI、real upstream、native adapter、license、Atlas deployment 均未声称完成。

**验证方法：**

```bash
sed -n '1,30p' docs/ops/CURRENT-STATUS.md
sed -n '28,62p' docs/ops/PRD2-ACCEPTANCE-MATRIX.md
```

---

## 全局验证命令

Canary 请从仓库根目录执行：

```bash
go test ./cmd/uniroute ./internal/testutil ./internal/migrate
go test ./...
go test -count=1 -tags integration ./internal/migrate -run TestMigration000009BackfillsDefaultProjectForPRD1Data_Integration -v
go test -count=1 -tags integration ./cmd/uniroute -run 'TestPRD2P0' -v
git diff --check
git diff --check -- $(git ls-files --others --exclude-standard)
```

说明：

- Integration tests need PostgreSQL access. They use `TEST_DATABASE_URL` or the default `postgres://uniroute:uniroute@localhost:5432/uniroute_test?sslmode=disable` only as connection parameters.
- Batch 9 integration tests create isolated temporary databases, apply migrations, and drop those temporary databases during cleanup.
- If the PostgreSQL role cannot create databases, the integration tests may skip according to helper behavior; in this local run they PASS.

## 期望的 VERIFY-RESULT.md 输出格式

请覆盖写入 `docs/agent-workflows/VERIFY-RESULT.md`：

```markdown
# VERIFY-RESULT.md — PRD2 Implementation Batch 9 验收结果

**验收者：** OpenClaw Canary
**日期：** 2026-07-02
**对应轮次：** PRD2 implementation batch 9 — P0 E2E Acceptance Consolidation

## 逐条结果

- Fix #1: PASS/FAIL — evidence
- Fix #2: PASS/FAIL — evidence
- Fix #3: PASS/FAIL — evidence
- Fix #4: PASS/FAIL — evidence
- Fix #5: PASS/FAIL — evidence
- Fix #6: PASS/FAIL — evidence
- Fix #7: PASS/FAIL — evidence
- Fix #8: PASS/FAIL — evidence

## 前置条件核验

- Batch 10-13 未声称完成: PASS/FAIL
- Developer Console frontend 未声称完成: PASS/FAIL
- Native Adapter / License / Atlas deployment 未声称完成: PASS/FAIL
- PRD2 matrix 未提前标为 E2E accepted: PASS/FAIL

## 全局验证

| 检查项 | 结果 |
|--------|------|
| `go test ./cmd/uniroute ./internal/testutil ./internal/migrate` | PASS/FAIL |
| `go test ./...` | PASS/FAIL |
| `go test -count=1 -tags integration ./internal/migrate -run TestMigration000009BackfillsDefaultProjectForPRD1Data_Integration -v` | PASS/FAIL/SKIP |
| `go test -count=1 -tags integration ./cmd/uniroute -run 'TestPRD2P0' -v` | PASS/FAIL/SKIP |
| `git diff --check` | PASS/FAIL |
| Matrix/status no overclaim | PASS/FAIL |

## 新发现的问题

无 / 列表

## 结论

PASS/FAIL summary
```
