# VERIFY-RESULT.md — PRD2 Batch 17 验收结果

**验收者：** OpenClaw Canary
**日期：** 2026-07-02
**对应轮次：** PRD2 Batch 17 Model Governance And Usage/Metering

## 逐条结果

- Fix #1: **PASS** — `ListReconciliation` 实现 full-query summary 聚合（`estimated_tokens`/`upstream_tokens`/`variance_ratio`/`estimated_ratio` 等），scope 标记为 `filtered_query`。超阈值时通过 notification event service 发射 `token_metering.estimated_ratio.high`（含 dedupe key）。admin handler 通过 `cmd/uniroute/main.go` 接入 shared event service。
- Fix #2: **PASS** — API client `getReconciliation()` 和 `getLogs()` 支持 `usage_source` 过滤。Reconciliation page 使用 API client（非 raw request），渲染 source filter + source-confidence/variance 列 + full-query metric cards。Logs page 新增 `usage_source` filter 和列。前端 73 tests 通过。
- Fix #3: **PASS** — `DeveloperConsolePage.tsx` 对 `deprecated` lifecycle 模型渲染 `migration_recommendation`。fixture 验证 `Migrate to gpt-4.1-mini` 渲染。既有测试无回归。
- Fix #4: **PASS (code-verified, smoke-SKIP)** — `prd2-batch17-modelgov-usage-smoke.sh`、seed Go 脚本、Playwright smoke 脚本存在且结构完整。seed 创建隔离 Batch17 org/project/GA/deprecated/unapproved/retired 模型/reconciliation logs。smoke 覆盖 Model Registry CRUD、project model-set assignment、approved catalog 过滤、deprecated migration 可见、retired 403、reconciliation full-query aggregate、notification event 持久化。Codex 本地 `status=PASS`、`estimated_ratio=0.8`、`estimated_ratio_scope=filtered_query`。当前环境无运行中 backend，无法独立执行。
- Fix #5: **PASS** — `CURRENT-STATUS.md` 标记 Batch 17 "fixed locally; awaiting Canary"，Batch 16 已更新为 Canary/Sentinel/Atlas completed。`PRD2-ACCEPTANCE-MATRIX.md` 相关行更新（Model Registry、Approved Catalog、Deprecated migration、Retired 403、Token Metering、Estimated-vs-upstream variance、Notification）。`PRD2-COMPLETION-PLAN.md` 记录 Batch 17 完成状态。P2-F13/F14/F15 仍 Planned，OIDC/budget hard-block/production HA 仍 Open。

## 命令结果

| 命令 | 结果 | 备注 |
|------|------|------|
| `go test ./...` | ✅ PASS | 20 packages, 0 FAIL |
| `npm --prefix frontend test -- --run` | ✅ PASS | 11 files, 73 tests |
| `npm --prefix frontend run build` | ✅ PASS | large chunk warning（既有） |
| `node --check scripts/prd2_batch17_modelgov_usage_smoke.mjs` | ✅ PASS | exit 0 |
| `scripts/prd2-batch17-modelgov-usage-smoke.sh` | **SKIP** | 当前环境无运行中 backend。Codex 本地 PASS 已记录 |
| `git diff --check` | ✅ PASS | exit 0 |

## 新发现

- 无新发现。代码和文档验证通过。

## 结论

**PASS (5/5, 1 项 smoke SKIP)**。Batch 17 可交给 Sentinel 进行 commit/push/PR 操作。下一轮应进入 Batch 18（OpenRouter Claude and Direct Anthropic Native Separation）。
