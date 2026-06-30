# FIX-SIGNAL.md — PRD1 UI Redesign Closeout

**修复者：** Codex
**日期：** 2026-07-01
**修复轮次：** PRD1 UI redesign closeout

---

## 背景

用户确认 PRD1 关闭前需要做完整视觉重设计，并明确 Canary/OpenClaw 没有视觉验收能力。

Codex 本轮执行的是 PRD1 控制台 UI 收尾：

- 不替换 Semi UI。
- 不新增产品功能。
- 不修改真实 provider、付费调用或生产数据。
- 将已有 SPE 设计文档工程化为可复用前端 UI 基础层。

---

## Fix #1: 新增 PRD1 UI 基础层

**文件：**

- `frontend/src/ui/layout/PageHeader.tsx`
- `frontend/src/ui/layout/Surface.tsx`
- `frontend/src/ui/layout/MetricTile.tsx`
- `frontend/src/ui/data/MoneyText.tsx`
- `frontend/src/ui/data/LatencyText.tsx`
- `frontend/src/ui/data/StatusTag.tsx`
- `frontend/src/ui/feedback/EmptyState.tsx`
- `frontend/src/ui/index.ts`
- `frontend/src/index.css`

**修复内容：**

- 增加 UniRouter 领域 UI 层，继续以 Semi UI 为基础。
- 增加控制台 design tokens、页面容器、面板、KPI、表格 shell、Explorer 三栏、移动端安全间距。
- 统一金额、延迟、状态和空状态表达。

---

## Fix #2: 重构控制台外壳和主要页面视觉

**文件：**

- `frontend/src/App.tsx`
- `frontend/src/pages/LoginPage.tsx`
- `frontend/src/pages/DashboardPage.tsx`
- `frontend/src/pages/LogsPage.tsx`
- `frontend/src/pages/WalletPage.tsx`
- `frontend/src/pages/ApiDocsPage.tsx`
- `frontend/src/pages/ChannelsPage.tsx`
- `frontend/src/pages/KeysPage.tsx`
- `frontend/src/pages/ChannelKeysPage.tsx`
- `frontend/src/pages/ModelPricesPage.tsx`
- `frontend/src/pages/OrgModelPricesPage.tsx`
- `frontend/src/pages/GrantsPage.tsx`
- `frontend/src/pages/UsersPage.tsx`
- `frontend/src/pages/AuditLogsPage.tsx`
- `frontend/src/pages/SettingsPage.tsx`
- `frontend/src/pages/TestPage.tsx`
- `frontend/src/pages/ReconciliationPage.tsx`

**修复内容：**

- 刷新 sidebar brand、内容 shell、移动端汉堡按钮和 `/docs` 导航入口。
- Dashboard 使用分析视图模板、KPI tiles、稳定图表、健康卡片、表格 surface。
- Logs 使用 Explorer 模板；详情打开后列表使用紧凑列，完整字段在右侧详情。
- Wallet 显示清晰 KPI、预算进度、管理员操作和组织信息。
- 资源管理页统一 PageHeader + Surface + table shell。
- Login、Settings、Test、Reconciliation、API Docs 统一视觉骨架。

---

## Fix #3: Dashboard 时间窗口和图表稳定性

**文件：**

- `frontend/src/pages/DashboardPage.tsx`

**修复内容：**

- 修正 `24h` range 由错误的 `24 * 86400000` 改为 `86400000`。
- 关闭 Recharts 动画，避免 Playwright 截图时趋势图/柱图未绘制完成。

---

## Fix #4: PRD1 UI 设计执行文档和状态同步

**文件：**

- `docs/design/PRD1-UI-REDESIGN-SPEC.md`
- `docs/design/README.md`
- `docs/ui/FRONTEND-UI-ARCHITECTURE-AUDIT.md`
- `docs/issues/OPEN-ISSUES.md`
- `docs/issues/PRD1-UI-FINDINGS-2026-07-01.md`
- `docs/ops/CURRENT-STATUS.md`
- `docs/ops/PRD1-CLOSEOUT-PLAN.md`
- `docs/ops/PRD-ACCEPTANCE-MATRIX.md`
- `README.md`
- `.gitignore`

**修复内容：**

- 新增 PRD1 UI redesign 执行规范和验收清单。
- 将 UI-005 标记为 fixed awaiting Canary。
- 记录 Codex 本地 Playwright 视觉 QA 证据。
- 忽略本地截图产物 `output/playwright/*.png`，避免 PR 带入二进制噪音。

---

## Codex 已执行验证

```bash
go test ./...
cd frontend && npm run build
cd frontend && npm test -- --run
git diff --check
```

结果：

- `go test ./...`: PASS
- `npm run build`: PASS，仅保留既有 Vite large chunk warning
- `npm test -- --run`: PASS，36/36 tests
- `git diff --check`: PASS

Codex 还执行了本地 Playwright 视觉 QA，使用只读 mock API 数据，不调用真实 provider、不写生产数据。

最终保留的本地截图证据：

- `output/playwright/login.png`
- `output/playwright/dashboard-final.png`
- `output/playwright/logs-detail-final-v4.png`
- `output/playwright/wallet-final.png`
- `output/playwright/api-docs.png`
- `output/playwright/channels.png`
- `output/playwright/dashboard-mobile-final.png`
- `output/playwright/logs-mobile-final.png`

说明：

- Canary/OpenClaw 不需要也不能判断截图视觉质量。
- Canary 可以验证截图路径存在，但视觉判断由 Codex/User 完成。
- Playwright 临时会话目录 `.playwright-cli/` 已移动到 `trash/playwright-cli-20260701-ui-redesign/`。

---

## Canary 验收命令

```bash
cd /Users/m4/Desktop/projects/UniRouter
git diff --check
go test ./...
cd frontend && npm run build
cd frontend && npm test -- --run
```

Source-level checks:

```bash
cd /Users/m4/Desktop/projects/UniRouter
rg -n "PageHeader|Surface|MetricTile|MoneyText|LatencyText|StatusTag|EmptyState" frontend/src/ui frontend/src/pages frontend/src/App.tsx
rg -n "86400000|isAnimationActive=\\{false\\}" frontend/src/pages/DashboardPage.tsx
rg -n "PRD1 UI Redesign|frontend/src/ui|output/playwright" docs/design/PRD1-UI-REDESIGN-SPEC.md docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/agent-workflows/FIX-SIGNAL.md
test -f output/playwright/dashboard-final.png
test -f output/playwright/logs-detail-final-v4.png
test -f output/playwright/dashboard-mobile-final.png
```

---

## 期望输出格式

请将验收结果写入 `docs/agent-workflows/VERIFY-RESULT.md`：

```markdown
# VERIFY-RESULT.md — PRD1 UI Redesign Closeout 验收结果

**验收者：** OpenClaw Canary
**日期：** 以文件系统时间戳为准
**对应轮次：** PRD1 UI redesign closeout

## 逐条结果

- Fix #1: PASS/FAIL — `frontend/src/ui/` 是否存在并提供 PageHeader/Surface/MetricTile/MoneyText/LatencyText/StatusTag/EmptyState
- Fix #2: PASS/FAIL — 主要页面是否接入统一 PageHeader/Surface/table shell 或等价 UI 基础层
- Fix #3: PASS/FAIL — Dashboard 24h 是否使用 `86400000`，图表是否关闭动画用于稳定截图
- Fix #4: PASS/FAIL — PRD1 UI redesign 文档、状态、open issues、README 是否同步

## 全局验证

- `git diff --check`: PASS/FAIL
- `go test ./...`: PASS/FAIL
- `npm run build`: PASS/FAIL
- `npm test -- --run`: PASS/FAIL
- 本地截图文件存在性: PASS/FAIL
- 浏览器视觉验收: NOT_APPLICABLE_TO_CANARY

## 新发现的问题

- [如有，在此列出；没有则写“无”]

## 结论

- [全部通过 / 需要 Codex 修复以下问题]
```
