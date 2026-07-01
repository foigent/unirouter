# FIX-SIGNAL.md — PRD1 Logs / Dashboard UI Follow-up 验收请求

**修复者：** Codex
**日期：** 2026-07-01
**修复轮次：** PRD1 call-log readability/copy UX and Dashboard model/channel balance

---

## Carry-over Scope

`docs/issues/OPEN-ISSUES.md` 中 `PRD1-DASH-003`（Dashboard AI 活动趋势仅支持单指标且可读性差）仍处于 `Fixed awaiting Canary`。本文件继续把该项纳入 Canary 验收范围，避免前一轮 FIX-SIGNAL 被本轮日志/Dashboard UI 信号覆盖后产生协议断档。

Carry-over 修复点：

- `frontend/src/pages/DashboardPage.tsx` 已把 `请求数 / 费用` 从互斥切换改为复选框，可同时展示两条曲线。
- Dashboard 趋势图已使用双 Y 轴：左侧请求数，右侧费用（元）。
- 防止两个指标都被取消，避免空图误导。

Carry-over targeted tests 本轮重新执行并通过：

```bash
(cd frontend && npm test -- --run src/pages/__tests__/DashboardPage.test.tsx)
```

---

## 本轮背景

用户在 PRD1 云端验收后继续报告调用日志和 Dashboard 视觉/交互问题：

- `调用日志 -> 日志记录` 时间显示为“24分钟前”，而不是审计友好的正常时间。
- Request ID 复制按钮点击后没有可感知效果。
- 10:57 真实 provider 测试延迟 `1290ms` 是否异常。
- 点击行后的请求详情文字大小、位置和展示方式局促，和整体页面不匹配。
- `调用日志 -> 筛选器` 中 `渠道 / Provider` 应该在 `模型` 上方。
- `仪表盘 -> 模型活动分布` 过长，压缩了 `渠道健康状态`。
- 渠道健康状态中 `deepseek` 下方再显示 `deepseek`、`mimo` 下方再显示 `mimo`、`Mimo Token Plan` 下方显示 `openai`，语义不直观。

Codex 评估结论：

- 日志页使用相对时间不是后端 bug，但对调用审计不合适；PRD1 关闭前应改为绝对时间，避免“24分钟前”这类显示随刷新变化。
- `1290ms` 对一次真实公网 LLM provider 端到端调用不构成单点 bug；应结合 P95/P99、provider/model/channel 分布判断。当前已确认云端日志能记录真实延迟，这个样本可作为延迟持久化证据。
- Request ID 复制无反馈/失败无提示是前端 UX bug。
- 日志详情页和 Dashboard 健康/分布卡片属于 PRD1 UI 质量缺陷，应在关闭前修。

---

## Fix #1: 调用日志时间改为绝对时间

**文件：**

- `frontend/src/pages/LogsPage.tsx`
- `frontend/src/pages/__tests__/LogsPage.test.tsx`

**修复内容：**

- 移除日志列表中的相对时间展示。
- `时间` 列统一使用 `YYYY-MM-DD HH:mm:ss` 绝对时间。
- 增加单测确保不再显示 `分钟前`，并显示实际日期。

**预期效果：**

- 调用日志可用于审计、排查和截图交接。
- 同一条记录不会因为页面刷新从“24分钟前”变成“25分钟前”而降低可追溯性。

---

## Fix #2: Request ID 复制增加 fallback 与反馈

**文件：**

- `frontend/src/pages/LogsPage.tsx`
- `frontend/src/pages/__tests__/LogsPage.test.tsx`

**修复内容：**

- `navigator.clipboard.writeText` 成功时显示 Toast。
- 如果浏览器安全上下文或权限导致 Clipboard API 失败，回退到临时 textarea + `document.execCommand('copy')`。
- fallback 也失败时显示错误 Toast，避免“点击无效果”。
- 复制按钮增加 `aria-label="复制 Request ID"`，便于测试和可访问性。

**预期效果：**

- 在 HTTPS/localhost/部分 HTTP 管理环境中尽可能复制成功。
- 无法复制时用户能明确知道失败，而不是误以为按钮坏了。

---

## Fix #3: 日志详情与筛选器布局优化

**文件：**

- `frontend/src/pages/LogsPage.tsx`
- `frontend/src/index.css`
- `frontend/src/pages/__tests__/LogsPage.test.tsx`

**修复内容：**

- 筛选器顺序调整为：状态、`渠道 / Provider`、模型、时间范围、Search。
- 行详情提示改为更克制的 `点击行查看详情`。
- 请求详情面板改为 header + hero + detail grid，突出 Request ID、状态、模型、渠道、provider/protocol、token、费用、错误信息。
- 增加详情面板 responsive 样式，避免内容堆在窄卡片中。

**预期效果：**

- Provider/渠道先于模型，符合用户先定位上游通道、再定位模型的排查顺序。
- 请求详情的视觉层级和当前 PRD1 redesign shell 更一致。

---

## Fix #4: Dashboard 模型分布与渠道健康状态平衡

**文件：**

- `frontend/src/pages/DashboardPage.tsx`
- `frontend/src/index.css`
- `frontend/src/pages/__tests__/DashboardPage.test.tsx`

**修复内容：**

- `模型活动分布` 从最多 10 条压缩为 Top 6，避免长尾模型挤压渠道健康卡片。
- 左右卡片布局从 `1.2fr / 0.8fr` 调整为更均衡的 `1fr / 1fr`。
- 图表高度固定为 260，减少数据量变化造成的版面跳动。
- 渠道健康状态不再重复展示与渠道名相同的 provider。
- 当渠道名不是 OpenAI 但 provider 为 `openai` 时，显示 `Provider: OpenAI-compatible`，表达这是协议/适配器类型，不是渠道显示名。

**预期效果：**

- `模型活动分布` 不再主导整行布局。
- `deepseek / deepseek`、`mimo / mimo` 这类重复文本消失。
- `Mimo Token Plan / openai` 改为更准确的 `Provider: OpenAI-compatible`。

---

## Codex 已执行验证

```bash
(cd frontend && npm test -- --run src/pages/__tests__/LogsPage.test.tsx src/pages/__tests__/DashboardPage.test.tsx)
(cd frontend && npm test -- --run)
(cd frontend && npm run build)
git diff --check
```

结果：

- Logs + Dashboard targeted frontend tests: PASS, 20/20 tests.
- Full frontend tests: PASS, 42/42 tests.
- Frontend build: PASS, retained existing Vite large chunk warning.
- `git diff --check`: PASS.
- Codex local Playwright visual smoke: PASS with mocked read-only data.
  - `output/playwright/prd1-logs-followup-inspector.png`
  - `output/playwright/prd1-dashboard-model-channel-balance.png`

Playwright smoke 覆盖：

- 调用日志显示绝对时间 `2026-07-01 10:57:06`，页面不再出现 `分钟前`。
- `渠道 / Provider` 筛选器位于 `模型` 上方。
- 点击日志行后请求详情面板可见，详情信息分组展示。
- Dashboard 渠道健康状态不再出现 `Provider: deepseek`。
- `Mimo Token Plan` 的 provider 语义显示为 `Provider: OpenAI-compatible`。

说明：

- 本轮没有执行真实 provider 付费调用或压测；用户问题属于前端显示/交互修复。
- `1290ms` 单点延迟样本不判定为 bug；它证明真实延迟已入库。是否异常应看同 provider/model 的 P95/P99 和时间窗口分布。

---

## Canary 验收命令

```bash
cd /Users/m4/Desktop/projects/UniRouter
git diff --check
(cd frontend && npm test -- --run src/pages/__tests__/LogsPage.test.tsx src/pages/__tests__/DashboardPage.test.tsx)
(cd frontend && npm test -- --run)
(cd frontend && npm run build)
```

Source-level checks:

```bash
rg -n "formatDateTime|copyToClipboard|fallbackCopyText|复制 Request ID|ur-detail-hero|ur-detail-grid" frontend/src/pages/LogsPage.tsx frontend/src/index.css frontend/src/pages/__tests__/LogsPage.test.tsx
rg -n "渠道 / Provider|模型|filter-label" frontend/src/pages/LogsPage.tsx frontend/src/pages/__tests__/LogsPage.test.tsx
rg -n "providerDisplayName|OpenAI-compatible|slice\\(0, 6\\)|height=\\{260\\}|ur-split-grid" frontend/src/pages/DashboardPage.tsx frontend/src/index.css frontend/src/pages/__tests__/DashboardPage.test.tsx
rg -n "PRD1-LOG-001|PRD1-LOG-002|PRD1-LOG-003|PRD1-DASH-004" docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/ops/PRD-ACCEPTANCE-MATRIX.md docs/ops/PRD1-REMAINING-ACCEPTANCE-PACK.md
```

Expected:

- All commands pass.
- Canary should mark visual/browser checks as not applicable.
- If Canary passes, Sentinel can proceed with PR/CI for this UI follow-up.
- After deployment, user/browser-capable role should visually confirm the cloud Logs and Dashboard pages because Canary has no visual judgment capability.
