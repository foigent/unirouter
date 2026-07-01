# FIX-SIGNAL.md — PRD2 implementation batch 3 Model Governance backend foundation

**修复者：** main agent
**日期：** 文件系统时间戳为准
**修复轮次：** PRD2 implementation batch 3

---

## 前置条件（本轮预期不变的部分）

| 项目 | 预期状态 | 原因 |
|------|---------|------|
| PRD2 batch 2 | Canary PASS + Sentinel complete | User 已报告 Canary 和 Sentinel 都完成，本轮同步状态并推进 batch 3 |
| Frontend | 预期无功能变化 | 本轮只做 Model Governance 后端基础，不做 Developer Console UI |
| Provider Adapter / Token Metering V2 | 仍未完成 | PRD2 推荐顺序中位于 Model Governance 之后 |
| 真实上游 E2E / 浏览器验收 | 仍未完成 | 本轮提供 schema、API、gateway gate 和单元测试证据 |

> Canary 验收时：上述未完成项不算 FAIL；如果本轮声称完成完整前端模型目录、Provider Adapter、Token Metering V2 或生产/E2E 验收，则应标为 FAIL。

---

## Fix #1: Batch 2 验收状态同步

**文件：** `docs/ops/CURRENT-STATUS.md`

**原问题：** User 报告 batch 2 的 Canary 和 Sentinel 已完成，但状态文档仍停留在 awaiting Canary。

**当前状态：**

- `CURRENT-STATUS.md` 记录 batch 2 为 Canary passed / Sentinel complete。
- `PRD2-ACCEPTANCE-MATRIX.md` 将 batch 2 RBAC 证据更新为 Canary 5/5 PASS + User reported Sentinel completed。
- 未把缺少真实 JWT/E2E 的 RBAC 项误标为 `E2E accepted`。

**验证方法：**

```bash
sed -n '1,32p' docs/ops/CURRENT-STATUS.md
sed -n '34,62p' docs/ops/PRD2-ACCEPTANCE-MATRIX.md
```

---

## Fix #2: Model Governance schema and compatibility seed

**文件：** `migrations/000010_prd2_model_governance.up.sql`

**原问题：** PRD2 P2-F04 要求企业内部模型目录、别名、模型集、项目模型集授权和生命周期，但 PRD1 只有 `model_prices` 与渠道 `models` 字符串。

**当前实现：**

- 新增 `model_registry`：生命周期 `staging/ga/deprecated/retired`、上下文长度、输出限制、能力标签、数据驻留、定价引用、catalog 可见性、迁移建议。
- 新增 `model_aliases`：企业稳定别名到真实模型。
- 新增 `model_sets`、`model_set_items`、`project_model_sets`：项目可用模型集。
- 从现有 `model_prices` 为每个 org seed `ga + approved_catalog=true` 注册项，保留 PRD1 兼容。
- 新增 rollback 文件 `000010_prd2_model_governance.down.sql`。

**验证方法：**

```bash
test -f migrations/000010_prd2_model_governance.up.sql
test -f migrations/000010_prd2_model_governance.down.sql
rg -n "model_registry|model_aliases|project_model_sets|lifecycle" migrations/000010_prd2_model_governance.up.sql
```

---

## Fix #3: Shared model governance access logic

**文件：** `internal/modelgov/modelgov.go`

**原问题：** lifecycle、alias、project model set 规则如果分散在 handler/gateway 中，后续 Adapter 接入会产生规则分叉。

**当前实现：**

- `CheckAccess` 解析 active alias 到真实模型。
- `retired` 模型返回拒绝。
- 项目有模型集绑定时，只允许模型集中的 model 或 alias。
- 项目没有模型集绑定时保持 PRD1 兼容，不突然拦截旧 key。
- `NormalizeCSV`、`NormalizeSlug`、生命周期/status/item type 校验集中在 `modelgov`。

**关键测试：**

- `TestCheckAccess_DeniesRetiredModel`
- `TestCheckAccess_ProjectSetDeniesUnassignedModel`
- `TestCheckAccess_ResolvesAliasAllowedByProjectSet`
- `TestNormalizeCSV`

**验证方法：**

```bash
go test ./internal/modelgov -v
```

---

## Fix #4: Gateway enforces lifecycle, project model sets, and aliases

**文件：** `cmd/uniroute/main.go`, `internal/gateway/relay.go`, `internal/middleware/auth.go`

**原问题：** 即使有模型目录，gateway 如果不检查 lifecycle/project model set，旧 key 仍可调用 retired 或项目外模型。

**当前实现：**

- `/v1/chat/completions` 和 `/v1/embeddings` 在进入 relay 前执行 `modelgov.CheckAccess`。
- retired/project-denied 模型返回 403。
- alias 请求会被改写为真实模型供路由使用。
- key 级 `allowed_models` 同时接受原始 alias 或解析后模型。
- 新增 `CtxModelAlias`，relay 的 `requested_model` 保留原始 alias，`model` 记录解析后模型。

**验证方法：**

```bash
go test ./internal/gateway ./internal/middleware ./cmd/uniroute
rg -n "CheckAccess|CtxModelAlias|requestedModel" cmd/uniroute/main.go internal/gateway/relay.go internal/middleware/auth.go
```

---

## Fix #5: Admin APIs for registry, catalog, aliases, and model sets

**文件：** `internal/admin/model_governance_handlers.go`, `cmd/uniroute/main.go`

**原问题：** PRD2 需要 Admin 管理模型注册表、Developer-facing approved catalog、模型别名和项目模型集绑定。

**当前实现：**

- `GET/POST/PUT /api/v1/model-registry`
- `GET /api/v1/model-catalog`
- `GET/POST/PUT /api/v1/model-aliases`
- `GET/POST /api/v1/model-sets`
- `PUT /api/v1/model-sets/{id}/items`
- `GET/PUT /api/v1/projects/{id}/model-sets`
- catalog 只返回 `approved_catalog=true` 且 lifecycle 为 `ga/deprecated` 的模型。
- project model-set 写入继续走 batch 2 的 project write authorization。

**关键测试：**

- `TestCreateModelRegistry_InvalidLifecycle`
- `TestListApprovedModelCatalog_FiltersApprovedLifecycle`
- `TestReplaceProjectModelSets_DeveloperForbidden`
- `TestCreateModelSet_InvalidItem`
- `TestAdminRoutesRegisteredForFrontendClient`

**验证方法：**

```bash
go test ./internal/admin ./cmd/uniroute -v
```

---

## Fix #6: PRD2 status, matrix, and acceptance discipline

**文件：** `docs/ops/CURRENT-STATUS.md`, `docs/ops/PRD2-ACCEPTANCE-MATRIX.md`

**当前状态：**

- `CURRENT-STATUS.md` 标记 batch 3 为 fixed locally awaiting Canary。
- Model Registry、Approved Catalog、Model Set、Model Alias、Retired lifecycle 等行更新为 `Unit tested` 或 `Implemented not accepted`。
- 未把缺少真实迁移、浏览器 UI、真实 JWT/API E2E 的项目标记为 `E2E accepted`。

**验证方法：**

```bash
sed -n '1,32p' docs/ops/CURRENT-STATUS.md
sed -n '42,62p' docs/ops/PRD2-ACCEPTANCE-MATRIX.md
```

---

## 全局验证命令

```bash
cd /Users/m4/Desktop/projects/UniRouter
go test ./internal/modelgov ./internal/admin ./internal/gateway ./internal/middleware ./cmd/uniroute
go test ./...
git diff --check
git status --short
```

## 期望输出格式

验收结果写入 `docs/agent-workflows/VERIFY-RESULT.md`：

```markdown
# VERIFY-RESULT.md — 验收结果

**验收者：** canary agent
**日期：** 文件系统时间戳为准
**对应轮次：** PRD2 implementation batch 3

---

## 逐条结果

- Fix #1: PASS/FAIL — [原因]
- Fix #2: PASS/FAIL — [原因]
- Fix #3: PASS/FAIL — [原因]
- Fix #4: PASS/FAIL — [原因]
- Fix #5: PASS/FAIL — [原因]
- Fix #6: PASS/FAIL — [原因]

## 前置条件核验

- Frontend 无功能变化: PASS/FAIL
- Provider Adapter / Token Metering V2 未声称完成: PASS/FAIL
- 真实上游 E2E / 浏览器验收未声称完成: PASS/FAIL
- PRD2 acceptance matrix 未过度标注: PASS/FAIL

## 全局验证

- go test ./internal/modelgov ./internal/admin ./internal/gateway ./internal/middleware ./cmd/uniroute: PASS/FAIL
- go test ./...: PASS/FAIL
- git diff --check: PASS/FAIL

## 新发现的问题

- [如有，在此列出；没有则写"无"]

## 结论

- [全部通过 / 需要 Codex 修复以下问题：...]
```
