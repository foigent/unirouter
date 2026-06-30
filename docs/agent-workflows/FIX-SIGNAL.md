# FIX-SIGNAL.md — PRD1 Reference-Inspired UI Redesign 验收请求

**修复者：** Codex
**日期：** 2026-07-01
**修复轮次：** PRD1 reference-inspired visual redesign pass

---

## 背景

用户明确指出 PRD1 UI 仍然“太难看”，并给出两类参考方向：

- Dribbble AdFlux Marketing Dashboard Web Design
- Pinterest light dashboard reference image

本轮目标不是新增业务功能，而是在 PRD1 已有页面和 Semi UI 技术栈内，把控制台视觉语言提升到可关闭 PRD1 的水平：

- 保持现有路由、API contract、PRD1 功能范围不变。
- 不替换 Semi UI，不引入新的前端框架。
- 优先重做全局 shell、登录页、Dashboard 和通用视觉 tokens。
- 由 Codex 执行浏览器截图视觉检查；Canary 只做源码、构建和测试验收。

---

## Fix #1: 全局应用壳层视觉升级

**文件：**

- `frontend/src/App.tsx`
- `frontend/src/index.css`

**修复内容：**

- 将浅色普通侧边栏升级为深色控制台侧栏，增加品牌区、选中态、hover 态和导航图标一致性。
- 增加顶部搜索、生产环境标识、通知按钮和用户 chip。
- 统一全局 tokens：背景、边框、阴影、圆角、状态色、玻璃质感 surface。
- 修复桌面端移动菜单按钮误显示问题：移动菜单默认隐藏，仅移动端展示。

---

## Fix #2: 登录页重做为参考级控制台入口

**文件：**

- `frontend/src/pages/LoginPage.tsx`
- `frontend/src/index.css`

**修复内容：**

- 从单卡片登录页升级为双栏登录 shell。
- 左侧增加 UniRoute 品牌、运行状态 tag、KPI 预览、趋势柱图和渠道状态列表。
- 右侧保留登录表单和原有测试可识别文案 `OpenAI 中转服务管理平台`。
- 按 PRD1 控制台定位处理，而不是营销落地页。

---

## Fix #3: Dashboard 首屏与关键状态重构

**文件：**

- `frontend/src/pages/DashboardPage.tsx`
- `frontend/src/index.css`

**修复内容：**

- 将普通 PageHeader 升级为 `AI Gateway Command Center` 首屏 panel。
- 首屏增加更新时间、健康渠道、钱包余额三个运营 insight。
- 重做 KPI 卡片、AI 活动趋势 surface、空状态、渠道列表和表格视觉层次。
- 保留 24h / 7天 / 30天切换、刷新、趋势指标切换等既有行为。

---

## Fix #4: PRD1 UI 文档同步

**文件：**

- `docs/design/PRD1-UI-REDESIGN-SPEC.md`
- `docs/issues/PRD1-UI-FINDINGS-2026-07-01.md`
- `docs/ops/CURRENT-STATUS.md`
- `docs/agent-workflows/FIX-SIGNAL.md`

**修复内容：**

- 记录本轮 reference-inspired pass 的范围、约束和验收边界。
- 明确 Canary/OpenClaw 不做视觉审美结论，只验证源码、构建、测试和截图证据存在。
- 将新增截图证据登记到 UI findings/status。

---

## Codex 已执行验证

本轮 Codex 已完成本地浏览器截图检查，截图保留在：

```text
output/playwright/ui-redesign-login.png
output/playwright/ui-redesign-dashboard.png
output/playwright/ui-redesign-mobile.png
```

Codex 交付前已重新执行：

```bash
cd /Users/m4/Desktop/projects/UniRouter
git diff --check
go test ./...
cd frontend && npm run build
cd frontend && npm test -- --run
```

结果：

- `git diff --check`: PASS
- `go test ./...`: PASS
- `cd frontend && npm run build`: PASS，保留既有 Vite large chunk warning
- `cd frontend && npm test -- --run`: PASS，36/36 tests passed

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
rg -n "ur-topbar|ur-user-chip|ur-env-pill" frontend/src/App.tsx frontend/src/index.css
rg -n "ur-login-shell|ur-login-showcase|ur-preview-chart" frontend/src/pages/LoginPage.tsx frontend/src/index.css
rg -n "ur-dashboard-hero|ur-hero-insight|AI Gateway Command Center" frontend/src/pages/DashboardPage.tsx frontend/src/index.css
rg -n "mobile-menu-toggle|semi-navigation-item-icon-toggle-right|ur-empty-state::before" frontend/src/index.css
test -f output/playwright/ui-redesign-login.png
test -f output/playwright/ui-redesign-dashboard.png
test -f output/playwright/ui-redesign-mobile.png
```

Do not run destructive cleanup. Do not run real provider paid calls, wallet/channel/API-key mutations, pressure tests, stream disconnects, or fault injection in Canary.

Visual beauty judgment is `NOT_APPLICABLE_TO_CANARY`; Canary should only report whether source/build/test/screenshot evidence exists.

---

## 期望输出格式

请将验收结果写入 `docs/agent-workflows/VERIFY-RESULT.md`：

```markdown
# VERIFY-RESULT.md — PRD1 Reference-Inspired UI Redesign 验收结果

**验收者：** OpenClaw Canary
**日期：** 以文件系统时间戳为准
**对应轮次：** PRD1 reference-inspired visual redesign pass

## 逐条结果

- Fix #1: PASS/FAIL — 全局 shell/topbar/sidebar/tokens 是否存在且源码可验证
- Fix #2: PASS/FAIL — 登录页双栏 showcase 是否实现且保留原测试文案
- Fix #3: PASS/FAIL — Dashboard hero/insight/card/empty state 是否实现
- Fix #4: PASS/FAIL — UI 文档和当前协议交接是否同步

## 全局验证

- `git diff --check`: PASS/FAIL
- `go test ./...`: PASS/FAIL
- `npm run build`: PASS/FAIL
- `npm test -- --run`: PASS/FAIL
- Screenshot evidence files: PASS/FAIL

## 新发现的问题

- [如有，在此列出；没有则写“无”]

## 结论

- [全部通过 / 需要 Codex 修复以下问题]
```
