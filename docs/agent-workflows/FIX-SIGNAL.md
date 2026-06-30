# FIX-SIGNAL.md — Bug #32~#36

**修复者：** codex agent
**修复轮次：** 第 15 轮

---

## 前置条件

| 项目 | 预期状态 | 原因 |
|------|---------|------|
| 后端 Go 编译 | `go build ./...` PASS | 修改了 dashboard handler + auth handler |
| 前端编译 | `npx vite build` PASS | 修改了 TestPage / WalletPage / KeysPage |
| BUG-LOG.md | 5 条均标记 ✅ 已修复 | Bug #32~#36 |

---

## Fix #32: TestPage 切换渠道不清空已选模型 (Medium 前端)

**文件：** `frontend/src/pages/TestPage.tsx`

**原问题：** 选择渠道 A → 选模型 → 切换到渠道 B，模型字段仍保留渠道 A 的值，导致测试时模型与渠道不匹配。

**修复内容：**
1. 新增 `const formRef = Form.useFormRef()` 获取表单引用
2. `<Form>` 增加 `getFormApi` 回调绑定 formRef
3. 渠道 `onChange` 中添加 `formRef.current?.setValue('model', undefined)` 重置模型字段

**验证：**
```bash
# 代码审查：确认 formRef 绑定和 onChange 重置逻辑
grep -n 'formRef\|Form.useFormRef\|setValue.*model' frontend/src/pages/TestPage.tsx
```

---

## Fix #33: WalletPage 缺少充值/调账按钮 (Medium 前端)

**文件：** `frontend/src/pages/WalletPage.tsx`

**原问题：** 页面只展示余额信息，无"充值"和"调整额度"操作按钮。后端已有 `POST /api/v1/wallet/recharge` 和 `POST /api/v1/wallet/adjust`。

**修复内容：**
1. 新增 `Button, Modal, Input` 导入
2. 新增 `modalVisible, modalType, modalAmount, submitting` 四个 state
3. 新增 `handleWalletOp` 函数，根据 modalType 调用 `/wallet/recharge` 或 `/wallet/adjust`
4. 仅 `is_admin` 用户可见的"管理员操作"卡片，含"充值"和"调整额度"按钮
5. Modal 弹窗输入金额，提交后自动刷新余额

**验证：**
```bash
grep -n 'handleWalletOp\|modalVisible\|recharge\|adjust' frontend/src/pages/WalletPage.tsx
```

---

## Fix #34: Dashboard AI 活动趋势不随时间范围变化 (High 后端)

**文件：** `internal/admin/dashboard_handlers.go`

**原问题：** `dashboardDailyTrend` 方法 SQL 硬编码 `WHERE created_at >= NOW() - INTERVAL '7 days'`，前端切换 24h/7d/30d 时间范围无效果，趋势图始终显示相同数据。

**修复内容：**
1. 方法签名改为 `func (h *AdminHandler) dashboardDailyTrend(ctx context.Context, startTime, endTime string) ([]DashboardDailyTrend, error)`
2. SQL 改为 `WHERE created_at >= $1 AND created_at <= $2`，参数化传入 startTime/endTime
3. 调用处（`getDashboard` 方法）传入对应的 startTime/endTime 参数

**验证：**
```bash
grep -n 'dashboardDailyTrend\|startTime\|endTime' internal/admin/dashboard_handlers.go
```

---

## Fix #35: KeysPage 允许模型手动输入 (Low 前端 UX)

**文件：** `frontend/src/pages/KeysPage.tsx`

**原问题：** 允许模型字段为 `Form.Input`，需手动输入模型名，容易拼错。

**修复内容：**
1. 新增 `availableModels` state，从 `GET /dashboard/models` 动态获取系统已有模型列表
2. `allowed_models` 字段从 `Form.Input` 改为 `Form.Select` 多选模式
3. 编辑时将逗号分隔字符串拆分为数组，提交时合并回字符串

**验证：**
```bash
grep -n 'availableModels\|Form.Select\|allowed_models' frontend/src/pages/KeysPage.tsx
```

---

## Fix #36: 审计日志无登录记录 (Medium 后端)

**文件：** `internal/admin/auth.go`

**原问题：** Login handler 成功后不调用 `recordAudit`，审计日志中无登录记录。

**修复内容：**
1. Login 成功后异步调用 `go h.recordAudit(ctx, "login", "user", fmt.Sprintf("%d", userID), ...)`
2. 添加 `fmt` import

**验证：**
```bash
grep -n 'recordAudit\|login' internal/admin/auth.go | head -10
```

---

## 全局验证命令

```bash
cd /Users/m4/Desktop/projects/UniRouter

# 1. Go 编译
go build ./...
# 期望：无输出

# 2. Go 测试
go test ./... -count=1
# 期望：所有 package OK

# 3. 前端编译
cd frontend && npx vite build
# 期望：✓ built in Xs

# 4. 关键代码审查
grep -rn 'dashboardDailyTrend.*startTime\|recordAudit.*login\|formRef.*model\|handleWalletOp\|availableModels' internal/ frontend/src/
# 期望：每项至少一条命中
```

---

## 期望 Canary 验收输出格式

请在 `VERIFY-RESULT.md` 中按以下格式写入：

```
# VERIFY-RESULT.md — Bug #32~#36 验收

**验收日期：** YYYY-MM-DD
**验收者：** Canary agent

## 逐条验收

| Bug # | 严重性 | 验收结果 | 备注 |
|-------|--------|---------|------|
| #32   | Medium | ✅/❌   |      |
| #33   | Medium | ✅/❌   |      |
| #34   | High   | ✅/❌   |      |
| #35   | Low    | ✅/❌   |      |
| #36   | Medium | ✅/❌   |      |

## 全局验证

- [ ] `go build ./...` 通过
- [ ] `go test ./...` 通过
- [ ] `npx vite build` 通过

## 新发现的问题

（如有）
```

---

**请 Canary 验收：** `docs/agent-workflows/FIX-SIGNAL.md`
