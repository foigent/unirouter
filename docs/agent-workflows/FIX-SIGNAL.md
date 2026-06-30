# FIX-SIGNAL.md — PRD1 API Key UX Fix 验收请求

**修复者：** Codex
**日期：** 2026-07-01
**修复轮次：** PRD1 API key creation and re-enable UX

---

## 背景

用户在 PRD1 验收准备中创建 API 密钥 `Codex20260701` 后发现：

- 创建后列表只显示 `key_prefix***`，无法看到完整密钥。
- 禁用 API key 后，前端没有再次启用入口。

代码核查结论：

- 后端 `CreateAPIKey` 只在创建响应中返回一次完整 `key`，数据库只保存 hash，这是正确安全设计。
- 既有前端没有展示创建响应中的完整 `key`，导致用户错过唯一可见时机。
- 后端 `UpdateAPIKey` 已支持更新 `status`，但前端没有“启用”按钮。

---

## Fix #1: 创建 API key 后一次性展示完整密钥

**文件：**

- `frontend/src/pages/KeysPage.tsx`
- `frontend/src/index.css`

**修复内容：**

- `api.createKey()` 返回完整 `key` 时，前端弹出“请保存完整 API 密钥”对话框。
- 对话框明确提示：完整密钥只在创建后显示一次，关闭后无法再次查看或找回。
- 增加复制按钮，调用 `navigator.clipboard.writeText()`。
- 增加 `ur-secret-reveal` 样式，长 key 可换行，不溢出。

---

## Fix #2: 禁用 API key 后允许再次启用

**文件：**

- `frontend/src/pages/KeysPage.tsx`

**修复内容：**

- 当 key 状态为 `disabled` 时，操作列显示启用按钮。
- 启用操作调用既有 `api.updateKey(id, { status: 'active' })`。
- 启用前弹确认框，成功后刷新列表。
- 非 disabled 状态仍显示禁用按钮。

---

## Codex 已执行验证

```bash
cd /Users/m4/Desktop/projects/UniRouter/frontend
npm run build
npm test -- --run
```

结果：

- `npm run build`: PASS，保留既有 Vite large chunk warning
- `npm test -- --run`: PASS，36/36 tests passed

---

## Canary 验收命令

```bash
cd /Users/m4/Desktop/projects/UniRouter
git diff --check
cd frontend && npm run build
cd frontend && npm test -- --run
```

Source-level checks:

```bash
rg -n "setCreatedKey|请保存完整 API 密钥|完整密钥只在创建后显示一次|copyCreatedKey|navigator.clipboard" frontend/src/pages/KeysPage.tsx
rg -n "handleEnable|status: 'active'|IconRefresh" frontend/src/pages/KeysPage.tsx
rg -n "ur-secret-reveal" frontend/src/index.css
```

Do not run destructive cleanup. Do not run real provider paid calls, wallet/channel/API-key mutations, pressure tests, stream disconnects, or fault injection in Canary.

---

## 期望输出格式

请将验收结果写入 `docs/agent-workflows/VERIFY-RESULT.md`：

```markdown
# VERIFY-RESULT.md — PRD1 API Key UX Fix 验收结果

**验收者：** OpenClaw Canary
**日期：** 以文件系统时间戳为准
**对应轮次：** PRD1 API key creation and re-enable UX

## 逐条结果

- Fix #1: PASS/FAIL — 创建 API key 后是否一次性展示完整 key 并提供复制按钮
- Fix #2: PASS/FAIL — disabled API key 是否可通过前端调用 `status: active` 启用

## 全局验证

- `git diff --check`: PASS/FAIL
- `npm run build`: PASS/FAIL
- `npm test -- --run`: PASS/FAIL

## 新发现的问题

- [如有，在此列出；没有则写“无”]

## 结论

- [全部通过 / 需要 Codex 修复以下问题]
```
