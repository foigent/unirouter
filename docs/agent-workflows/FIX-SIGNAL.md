# FIX-SIGNAL.md — PRD2 Batch 26 UI/UX Hardening

**修复者：** main agent
**日期：** 2026-07-04
**修复轮次：** PRD2 Batch 26

---

## 前置条件（本轮预期不变的部分）

| 项目 | 预期状态 | 原因 |
|------|---------|------|
| PRD2 Batch 25 final closeout | 已通过 Canary/Sentinel per User report | 本轮是用户反馈后的 UI/UX follow-up，不改变 Batch 25 终态结论 |
| Billing / reconciliation / metering backend | 不应有代码变更 | P2-F06 风险边界是不能重写账务；本轮只做 UI/UX 和渠道测试前端修复 |
| Production verified evidence | 不新增 | 本轮只有本地前端测试、build 和浏览器 smoke |

> Canary 验收时：以上项目不应作为 Batch 26 FAIL。请重点核验本轮前端代码、测试、构建、浏览器证据和“不改账务后端”的边界。

---

## Fix #1: Sidebar Hierarchy, Scroll, Collapse Indicators, And Icons

**文件：** `frontend/src/index.css:53`, `frontend/src/index.css:74`, `frontend/src/index.css:109`, `frontend/src/index.css:137`, `frontend/src/index.css:149`, `frontend/src/index.css:189`, `frontend/src/navigation.tsx:69`

**原问题：**

- 一级/二级菜单主要靠 hover 阴影区分。
- 二级菜单折叠图标不可见。
- 左侧功能栏缺少可靠纵向滚动，低高度视口下 API 文档可能被遮挡。
- 多组图标重复，例如概览/仪表盘、AI 流量/渠道管理、上游密钥/API 密钥、账单/钱包。

**当前代码：**

- `ur-sider` 和 Semi Sider 内层 `.semi-layout-sider-children` 都设置为 full-height flex column。
- `.ur-sider .semi-navigation` 使用 `flex: 1 1 auto; min-height: 0; overflow-y: auto`。
- 一级组标题使用独立背景、字重和颜色；二级菜单缩进并加左侧层级线。
- `.semi-navigation-item-icon-toggle-right` 恢复为可见。
- 导航图标重新分配：概览用 home、仪表盘用 monitor、AI 流量用 send、渠道管理用 route、上游密钥用 lock、API 密钥用 key、钱包用 credit card。

**验证方法：**

```bash
rg -n "semi-layout-sider-children|overflow-y: auto|semi-navigation-sub-title|semi-navigation-sub \\.semi-navigation-item|semi-navigation-item-icon-toggle-right" frontend/src/index.css
rg -n "IconSendStroked|IconRoute|IconLockStroked|IconKeyStroked|IconCreditCard|IconMonitorStroked" frontend/src/navigation.tsx
```

浏览器 smoke 期望：

- 1280x560 视口下侧栏 `navScrollHeight > navClientHeight`。
- `navOverflowY=auto`。
- 页面文本包含 `API 文档`。
- 二级菜单项和折叠 toggle 可见。

---

## Fix #2: Audit Logs Wide Table Scroll

**文件：** `frontend/src/pages/AuditLogsPage.tsx:68`

**原问题：** 审计日志在 50/100 条和宽字段场景下只能依赖页面底部横向滚动，操作不友好。

**当前代码：**

- 审计日志列设置明确宽度。
- detail 字段保持单行 monospace 展示，避免把表格高度撑乱。
- Semi Table 设置 `scroll={{ x: 1570, y: 620 }}`，使表格主体内部提供横向/纵向滚动。
- `Org` / `Project` 列名改为中文 `组织` / `项目`。

**验证方法：**

```bash
rg -n "scroll=\\{\\{ x: 1570, y: 620 \\}\\}|title: '组织'|title: '项目'|whiteSpace: 'nowrap'" frontend/src/pages/AuditLogsPage.tsx
```

浏览器 smoke 期望：

- 审计日志页存在横向 scroll container `.semi-table-body`。

---

## Fix #3: Channel Test Uses Configured Channel Models

**文件：** `frontend/src/pages/TestPage.tsx:27`, `frontend/src/pages/TestPage.tsx:78`, `frontend/src/pages/TestPage.tsx:102`, `frontend/src/pages/TestPage.tsx:130`, `frontend/src/pages/TestPage.tsx:190`, `frontend/src/pages/__tests__/TestPage.test.tsx:1`

**原问题：** 渠道测试前端默认写死 `gpt-3.5-turbo`，与当前渠道配置不兼容；浏览器点击也暴露出 Semi Select 显示值和 formApi values 不总是一致。

**当前代码：**

- 新增 `parseChannelModels`、`firstChannelModel`、`resolveChannelTestModel`。
- 单渠道测试优先使用用户选择模型，否则 fallback 到该渠道配置的第一个模型。
- 无配置模型时不再硬编码 fallback，提示 `请选择已配置模型的渠道`。
- 批量测试对每个 active channel 使用自身第一个配置模型，无模型时生成可见失败结果 `渠道未配置测试模型`。
- 渠道和模型选择同步到 React state，提交按钮显式读取当前表单/state 并调用 `handleTest`。
- 新增单元测试证明不会发明 `gpt-3.5-turbo` fallback。

**验证方法：**

```bash
rg -n "parseChannelModels|resolveChannelTestModel|selectedModel|渠道未配置测试模型|gpt-3.5-turbo" frontend/src/pages/TestPage.tsx frontend/src/pages/__tests__/TestPage.test.tsx
npm --prefix frontend test -- --run TestPage
```

浏览器 smoke 期望：

- 选择 mock 渠道 `Azure-East (azure)` 后，测试请求 payload 为 `model=gpt-4o-mini`。
- 页面展示 `测试成功` 和 `gpt-4o-mini`。
- 页面不展示 `gpt-3.5-turbo`。

---

## Fix #4: PRD2 UI English Label Cleanup

**文件：** `frontend/src/pages/ModelGovernancePage.tsx`, `frontend/src/pages/UsageAnalyticsPage.tsx`, `frontend/src/pages/DeveloperConsolePage.tsx`, `frontend/src/pages/LogsPage.tsx`, `frontend/src/pages/DashboardPage.tsx`, related tests

**原问题：** 多处 PRD2 UI 仍展示英文术语，例如 `Model Registry`、`Aliases`、`Provider`、`Catalog`。

**当前代码：**

- 模型治理页主要可见标签改为中文：`模型目录`、`企业别名`、`模型集`、`项目分配`、`供应商族`、`目录可见性`、`已批准`、`内部` 等。
- 用量分析、开发者控制台、日志、Dashboard 的 Provider/Alias 文案改为中文语境。
- 对应页面测试断言同步更新。

**验证方法：**

```bash
rg -n "模型目录|企业别名|模型集|项目分配|供应商族|目录可见性|供应商：" frontend/src/pages frontend/src/pages/__tests__
npm --prefix frontend test -- --run ModelGovernancePage UsageAnalyticsPage DeveloperConsolePage LogsPage DashboardPage
```

---

## Fix #5: Local Evidence And Status Handoff

**文件：** `docs/ops/evidence/PRD2-B26-ui-hardening-local-20260704.md`, `docs/ops/CURRENT-STATUS.md`, `docs/agent-workflows/FIX-SIGNAL.md`

**原问题：** 用户反馈的 Batch 26 UI/UX hardening 需要独立记录，不能混入 Batch 25 已完成状态，也不能误称 Canary/Sentinel/Atlas 已通过。

**当前文档：**

- 新增 Batch 26 local evidence 文件，记录测试、build、Playwright smoke 断言和截图路径。
- Current Status 增加 Batch 26 fixed locally awaiting Canary 行。
- 本文件作为 Canary 验收入口。

**验证方法：**

```bash
test -f docs/ops/evidence/PRD2-B26-ui-hardening-local-20260704.md
rg -n "Batch 26|UI/UX hardening|awaiting Canary|prd2-batch26-ux" docs/ops/CURRENT-STATUS.md docs/ops/evidence/PRD2-B26-ui-hardening-local-20260704.md docs/agent-workflows/FIX-SIGNAL.md
```

---

## 全局验证命令

```bash
cd /Users/m4/Desktop/projects/UniRouter
rg -n "semi-layout-sider-children|overflow-y: auto|semi-navigation-sub-title|semi-navigation-sub \\.semi-navigation-item|semi-navigation-item-icon-toggle-right" frontend/src/index.css
rg -n "IconSendStroked|IconRoute|IconLockStroked|IconKeyStroked|IconCreditCard|IconMonitorStroked" frontend/src/navigation.tsx
rg -n "scroll=\\{\\{ x: 1570, y: 620 \\}\\}|title: '组织'|title: '项目'|whiteSpace: 'nowrap'" frontend/src/pages/AuditLogsPage.tsx
rg -n "parseChannelModels|resolveChannelTestModel|selectedModel|渠道未配置测试模型|gpt-3.5-turbo" frontend/src/pages/TestPage.tsx frontend/src/pages/__tests__/TestPage.test.tsx
rg -n "模型目录|企业别名|模型集|项目分配|供应商族|目录可见性|供应商：" frontend/src/pages frontend/src/pages/__tests__
test -f docs/ops/evidence/PRD2-B26-ui-hardening-local-20260704.md
rg -n "Batch 26|UI/UX hardening|awaiting Canary|prd2-batch26-ux" docs/ops/CURRENT-STATUS.md docs/ops/evidence/PRD2-B26-ui-hardening-local-20260704.md docs/agent-workflows/FIX-SIGNAL.md
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

Optional browser smoke for a browser-capable verifier:

- Use local frontend `http://localhost:3000`.
- Mock `/api/v1/me`, `/api/v1/audit-logs`, `/api/v1/channels`, and `/api/v1/channels/{id}/test`.
- Confirm the assertions recorded in `docs/ops/evidence/PRD2-B26-ui-hardening-local-20260704.md`.

## 期望输出格式

请 OpenClaw Canary 覆盖写入 `docs/agent-workflows/VERIFY-RESULT.md`：

```markdown
# VERIFY-RESULT.md — PRD2 Batch 26 验收结果

**验收者：** OpenClaw Canary
**日期：** [实际日期]
**对应轮次：** PRD2 Batch 26 UI/UX Hardening

## 逐条结果

- Fix #1: PASS/FAIL — [sidebar hierarchy, scroll, visible collapse indicators, differentiated icons]
- Fix #2: PASS/FAIL — [audit logs wide table has internal horizontal scroll]
- Fix #3: PASS/FAIL — [channel test uses configured channel models and no hardcoded gpt-3.5-turbo fallback]
- Fix #4: PASS/FAIL — [PRD2 UI English labels localized where changed]
- Fix #5: PASS/FAIL — [local evidence/status/FIX-SIGNAL boundary is consistent]

## 命令结果

| 命令 | 结果 | 备注 |
|------|------|------|
| CSS/nav grep commands | PASS/FAIL | |
| AuditLogs grep command | PASS/FAIL | |
| TestPage grep + tests | PASS/FAIL | |
| UI label grep + targeted tests | PASS/FAIL | |
| Evidence/status grep commands | PASS/FAIL | |
| `npm --prefix frontend test -- --run` | PASS/FAIL | |
| `npm --prefix frontend run build` | PASS/FAIL | large chunk warning is existing/non-blocking unless new errors appear |
| `git diff --check` | PASS/FAIL | |
| Browser smoke | PASS/FAIL/SKIP | If SKIP, state no browser/local frontend was available and inspect the recorded evidence boundary |

## 新发现

- [如无，写“无新发现”]

## 结论

PASS/FAIL。
```
