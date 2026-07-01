# FIX-SIGNAL.md — PRD1 Dashboard Percentile / Layout Closeout 验收请求

**修复者：** Codex
**日期：** 2026-07-01
**修复轮次：** PRD1 Dashboard latency percentile metrics and layout polish

---

## 本轮背景

用户在云端部署后继续指出 Dashboard 的 PRD1 收尾问题：

- 平均延迟不足以判断真实 provider 体验，询问是否需要展示 P95/P99。
- Dashboard 整体布局仍显松散，像营销大屏，不像高频运营后台。
- 顶部搜索框没有和页面正文左侧对齐；右侧 `生产环境 / Operator / 平台管理员` 没有贴齐正文右侧。
- `更新时间 / 健康渠道 / 钱包余额` 在 hero 右侧悬浮，和 KPI/图表上下左右关系不稳定。
- `AI 活动趋势` 横向过长。
- `渠道健康状态` 需要更直观的状态图标表达，并且不应过度靠右。

Codex 判断：

- P95/P99 应进入 PRD1 Dashboard。平均值会被快失败、短请求和历史 0 延迟样本稀释，不适合作为主要延迟 SLI。
- PRD1 不需要再做大规模视觉重写，但需要一次结构性收束：统一 1440px 内容网格、降低 hero 大屏感、把状态信息变成 compact status strip、把趋势图和延迟分位并列。

---

## Fix #1: Dashboard API 返回 P95/P99 延迟

**文件：**

- `internal/admin/dashboard_handlers.go:26`
- `internal/admin/dashboard_handlers.go:39`
- `internal/admin/dashboard_handlers.go:89`
- `internal/admin/admin_mock_test.go:2741`
- `internal/admin/admin_mock_test.go:2788`

**原问题：**

Dashboard API 只返回 `avg_latency_ms`。前端无法展示 P95/P99，只能用平均值判断延迟，容易误导。

**当前代码：**

- `DashboardStats` 调用 `dashboardLatencyPercentiles(ctx, orgID, startTime, endTime)`。
- JSON 响应新增：
  - `p95_latency_ms`
  - `p99_latency_ms`
- SQL 使用 PostgreSQL `percentile_cont(0.95/0.99) WITHIN GROUP (ORDER BY latency_ms)`。
- 延迟聚合统一排除 `latency_ms <= 0`，避免历史日志 bug 产生的 0 延迟污染 SLI。
- `delta_vs_yesterday.latency_ms` 现在对比 P95 延迟，而不是平均延迟。

**验证方法：**

```bash
go test ./internal/admin -run 'TestDashboardStats_Success|TestDashboardOverview_TotalUsesTimeRange|TestDashboardLatencyPercentiles_UsesOrgTimeRangeAndPositiveLatency' -v
```

期望：

- `TestDashboardStats_Success` 断言响应包含 `p95_latency_ms=420.5`、`p99_latency_ms=950.25`。
- `TestDashboardLatencyPercentiles_UsesOrgTimeRangeAndPositiveLatency` 断言 SQL 包含 `org_id = $1`、时间窗口、`latency_ms > 0`、`percentile_cont(0.95)`、`percentile_cont(0.99)`。

---

## Fix #2: Dashboard 主 KPI 从平均延迟改为 P95 延迟

**文件：**

- `frontend/src/pages/DashboardPage.tsx:31`
- `frontend/src/pages/DashboardPage.tsx:250`
- `frontend/src/pages/DashboardPage.tsx:383`
- `frontend/src/pages/__tests__/DashboardPage.test.tsx:21`
- `frontend/src/pages/__tests__/DashboardPage.test.tsx:175`

**原问题：**

Dashboard 主卡展示“平均延迟”，不能代表用户实际尾部体验。

**当前代码：**

- `DashboardStats` 类型新增 `p95_latency_ms`、`p99_latency_ms`。
- 主 KPI 标题改为 `P95 延迟`。
- 主值展示 `p95_latency_ms`。
- 副信息展示 `Avg ...ms · P99 ...ms`。
- 如果后端尚未返回 P95/P99，前端回退到 avg，保持向后兼容。

**验证方法：**

```bash
cd frontend
npm test -- --run src/pages/__tests__/DashboardPage.test.tsx
```

期望：

- DashboardPage 测试包含 `P95 延迟`、`780ms`、`P99`、`1290ms`。

---

## Fix #3: Dashboard 顶部和主内容统一对齐

**文件：**

- `frontend/src/pages/DashboardPage.tsx:319`
- `frontend/src/index.css:137`
- `frontend/src/index.css:469`
- `frontend/src/index.css:502`

**原问题：**

Dashboard 使用 `ur-page-wide` 全宽铺开，而 topbar 限制在 1440px；搜索框和右侧用户区无法与正文形成一致边界。原 Dashboard hero 使用左右大块布局，状态卡悬浮在右侧。

**当前代码：**

- Dashboard 根容器改回 `className="ur-page"`，和 topbar 共享 1440px 内容宽度。
- topbar grid 改为 `minmax(280px, 520px) minmax(0, 1fr)`，右侧 actions 能贴齐页面右边界。
- Dashboard hero 收束为普通 header surface：
  - 标题字号从 38px 降到 30px。
  - 移除大面积渐变/装饰圆。
  - `更新时间 / 健康渠道 / 钱包余额` 改为三列 `ur-dashboard-status-strip`。

**验证方法：**

```bash
rg -n "ur-page\\\"|ur-dashboard-status-strip|grid-template-columns: minmax\\(280px, 520px\\)" frontend/src/pages/DashboardPage.tsx frontend/src/index.css
```

期望：

- Dashboard 不再使用 `ur-page-wide`。
- 状态条存在。
- topbar 第二列为 `minmax(0, 1fr)`。

---

## Fix #4: AI 活动趋势不再无节制横向拉伸

**文件：**

- `frontend/src/pages/DashboardPage.tsx:417`
- `frontend/src/pages/DashboardPage.tsx:501`
- `frontend/src/index.css:451`
- `frontend/src/index.css:556`

**原问题：**

趋势图独占整行，宽屏下横向过长，右侧空间没有用于高价值运营信息。

**当前代码：**

- 新增 `ur-dashboard-analytics-grid`：左侧为 AI 活动趋势，右侧为延迟分位。
- 趋势图高度从 300 降为 280，减少“大屏”感。
- 新增 `延迟分位` 面板展示 Avg / P95 / P99，配合进度条表达尾部延迟。

**验证方法：**

```bash
rg -n "ur-dashboard-analytics-grid|延迟分位|LatencyPercentilePanel|height=\\{280\\}" frontend/src/pages/DashboardPage.tsx frontend/src/index.css frontend/src/pages/__tests__/DashboardPage.test.tsx
```

期望：

- DashboardPage 测试断言 `.ur-dashboard-analytics-grid` 和 `.ur-latency-surface` 存在。

---

## Fix #5: 渠道健康状态使用状态图标和成功率进度

**文件：**

- `frontend/src/pages/DashboardPage.tsx:114`
- `frontend/src/pages/DashboardPage.tsx:161`
- `frontend/src/index.css:663`
- `frontend/src/index.css:679`

**原问题：**

渠道健康状态主要依赖文字 tag，不够直观；渠道卡视觉重量不稳定，状态不够一眼可读。

**当前代码：**

- 新增 `channelHealthView()`，统一映射：
  - `健康` + `IconTickCircle`
  - `告警` + `IconAlertTriangle`
  - `异常` + `IconAlertTriangle`
  - `禁用` + `IconMinusCircle`
- 渠道卡加入成功率 Progress。
- `健康渠道` 计数只统计真正 `healthy` 的渠道，不再把 warning 简单算作健康。

**验证方法：**

```bash
rg -n "channelHealthView|IconTickCircle|IconAlertTriangle|IconMinusCircle|ur-channel-state|ur-channel-progress" frontend/src/pages/DashboardPage.tsx frontend/src/index.css
```

期望：

- source check 命中上述状态映射和 CSS 类。

---

## Codex 已执行验证

```bash
go test ./internal/admin -run 'TestDashboard|TestListLogs' -v
cd frontend && npm test -- --run src/pages/__tests__/DashboardPage.test.tsx
cd frontend && npm test -- --run
cd frontend && npm run build
go test ./...
```

结果：

- Admin targeted Go tests: PASS.
- Dashboard targeted frontend tests: PASS, 11/11 tests.
- Full frontend tests: PASS, 43/43 tests.
- Frontend build: PASS, retained existing Vite large chunk warning.
- Full Go tests: PASS.
- `git diff --check`: PASS.
- Codex local Playwright visual smoke: PASS with mocked read-only Dashboard data.
  - `output/playwright/prd1-dashboard-percentile-layout-desktop.png`
  - `output/playwright/prd1-dashboard-percentile-layout-mobile.png`

Playwright smoke 覆盖：

- Desktop 1440px: `.ur-topbar` and `.ur-page` left/right bounds aligned within 2px.
- Desktop 1440px: page includes `P95 延迟` and `P99`.
- Desktop 1440px and mobile 390px: no horizontal overflow.
- Console errors were only existing React/Semi StrictMode `findDOMNode` deprecation warnings, not business/runtime errors.

---

## Canary 验收命令

```bash
cd /Users/m4/Desktop/projects/UniRouter
git diff --check
go test ./internal/admin -run 'TestDashboard|TestListLogs' -v
(cd frontend && npm test -- --run src/pages/__tests__/DashboardPage.test.tsx)
(cd frontend && npm test -- --run)
(cd frontend && npm run build)
```

Source-level checks:

```bash
rg -n "p95_latency_ms|p99_latency_ms|dashboardLatencyPercentiles|percentile_cont|latency_ms > 0" internal/admin/dashboard_handlers.go internal/admin/admin_mock_test.go
rg -n "P95 延迟|Avg .*P99|ur-dashboard-status-strip|ur-dashboard-analytics-grid|LatencyPercentilePanel|ur-latency-surface" frontend/src/pages/DashboardPage.tsx frontend/src/index.css frontend/src/pages/__tests__/DashboardPage.test.tsx
rg -n "channelHealthView|IconTickCircle|IconAlertTriangle|IconMinusCircle|ur-channel-state|ur-channel-progress" frontend/src/pages/DashboardPage.tsx frontend/src/index.css
rg -n "PRD1-DASH-005" docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/ops/PRD-ACCEPTANCE-MATRIX.md docs/ops/PRD1-REMAINING-ACCEPTANCE-PACK.md
```

Expected:

- All commands pass.
- Canary should mark visual/browser checks as not applicable.
- If Canary passes, Sentinel can proceed with PR/CI for this Dashboard closeout round.
- After deployment, user/browser-capable role should confirm the cloud Dashboard visual alignment and P95/P99 rendering.
