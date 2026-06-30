# TEST-STATUS.md — 测试状态总览

**最后更新：** 2026-06-30（第 15 轮 — handlers.go 拆分 + billing 单元测试 + DashboardStats 重构）
**轮次：** 第 15 轮

---

## 后端 Go 测试

| 包 | 测试数 | 覆盖率 | 状态 |
|----|--------|--------|------|
| cmd/uniroute | 3 | 0.0% | ✅ PASS |
| internal/admin | 141 | 79.7% | ✅ PASS |
| internal/auth | 16 | 86.0% | ✅ PASS |
| internal/billing | 35 | 10.4% | ✅ PASS |
| internal/circuitbreaker | 20 | 93.0% | ✅ PASS |
| internal/config | 8 | 95.8% | ✅ PASS |
| internal/db | 40 | 96.2% | ✅ PASS |
| internal/filter | 52 | 100.0% | ✅ PASS |
| internal/gateway | 46 | 76.6% | ✅ PASS |
| internal/logger | 4 | 74.3% | ✅ PASS |
| internal/middleware | 84 | 91.8% | ✅ PASS |
| internal/migrate | 0 | - | ⚠️ no test files |
| internal/models | 60 | - | ✅ PASS |
| internal/redisx | 48 | 95.7% | ✅ PASS |
| internal/testutil | 0 | - | ⚠️ no test files |
| internal/tokenutil | 72 | 95.7% | ✅ PASS |
| **合计** | **568** | - | **✅ 全部 PASS** |

## 前端测试

| 文件 | 测试数 | 状态 |
|------|--------|------|
| `src/utils/__tests__/formatDate.test.ts` | 6 | ✅ PASS |
| `src/api/__tests__/client.test.ts` | 7 | ✅ PASS |
| `src/pages/__tests__/LoginPage.test.tsx` | 5 | ✅ PASS |
| `src/pages/__tests__/DashboardPage.test.tsx` | 8 | ✅ PASS |
| `src/pages/__tests__/LogsPage.test.tsx` | 7 | ✅ PASS |
| **合计** | **33** | **✅ 全部 PASS** |

---

## 最近一轮变更 (第 15 轮)

**handlers.go 拆分 (P1):**
- `internal/admin/handlers.go` (2577行) → 15 个文件:
  - `handlers.go` (struct + NewHandler + parsePagination + common helpers + GetMe)
  - `auth_handlers.go` — Login
  - `channel_handlers.go` — Channels CRUD + GetChannel
  - `key_handlers.go` — API Keys CRUD + GetAPIKey
  - `wallet_handlers.go` — Wallet + ExportWalletTransactions
  - `log_handlers.go` — ListLogs + GetLogDetail
  - `model_handlers.go` — ListReconciliation + ListModelPrices + Model Prices CRUD + Org Model Prices
  - `settings_handlers.go` — ListSettings + UpdateSetting
  - `dashboard_handlers.go` — DashboardStats
  - `channel_key_handlers.go` — Channel Keys CRUD + GetModelNames + maskKey
  - `user_handlers.go` — Users CRUD
  - `grant_handlers.go` — Grants
  - `audit_handlers.go` — Audit + CleanupOldData
  - `sensitive_handlers.go` — Sensitive Words CRUD
  - `test_handlers.go` — TestChannel

**DashboardStats 内部拆分 (P2):**
- 230 行单方法 → 30 行编排 + 8 个私有方法:
  - `dashboardOverview` — 总请求数、成本、延迟、成功率
  - `dashboardRecentLogs` — 最近 10 条日志
  - `dashboardModelPrices` — 模型价格
  - `dashboardDailyTrend` — 7 天趋势
  - `dashboardWalletInfo` — 钱包信息
  - `dashboardModelDistribution` — 模型分布 Top 5
  - `dashboardChannelHealth` — 渠道健康（含状态判定逻辑）
  - `dashboardYesterdayDelta` — 昨日对比

**billing 单元测试 (P3):**
- 新增 `billing_price_test.go` — 9 个测试:
  - TestQueryModelPrice_Success / Error / ZeroPrices
  - TestCalculateCost_SingleToken / HighPriceModel
  - TestEstimateFreezeAmount_LargeSafetyFactor / ExactMinFreeze
  - TestWithPriceProvider_ReturnsSelf / NilPriceProvider

**Round 14 FIX-SIGNAL 验收:**
- 7 PASS / 1 FAIL（GetModelNames 数据源与 FIX-SIGNAL 不一致）
- 详见 `docs/agent-workflows/VERIFY-RESULT.md`

---

## 测试质量指标

| 指标 | 值 |
|------|-----|
| 后端测试总数 | 568 |
| 前端测试总数 | 33 |
| 总计 | 601 |
| 0 FAIL | ✅ |
| 0 DATA RACE | ✅ |
| go build | ✅ |
| go vet | ✅ |
