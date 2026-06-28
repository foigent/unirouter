# FIX-SIGNAL.md — Bug #14-#17 + 回归修复

**修复者：** main agent (Codex)
**日期：** [文件系统时间戳为准，不要硬编码日期]
**修复轮次：** 第 9 轮（第 8 轮回归修复）

---

## 前置条件（本轮预期不变的部分）

| 项目 | 预期状态 | 原因 |
|------|---------|------|
| 前 13 条 fix（#1-#13） | 预期仍 PASS | 本轮未修改相关代码 |
| 后端 Go 编译 | 预期 PASS | 本轮只改了 mock 测试和 handlers |
| 前端 TypeScript 编译 | 预期 PASS | 本轮修改了前端页面 |
| Go 测试 | 预期 PASS | 本轮修复了回归测试 |

> Canary 验收时：前置条件中列出的预期失败不算 FAIL，只需在结论中注明。

---

## Fix #14: DashboardStats SQL 语法错误

**文件：** `internal/admin/handlers.go:1083`
**原问题：** `COUNT(*) FILTER` 语法在 SQLite 中不支持（仅 PostgreSQL 支持）。
**当前代码：**
```go
// 当前已改为兼容语法：
var successCount, totalCount int
h.db.QueryRow(ctx,
    \`SELECT COUNT(*) FROM call_logs WHERE created_at >= $1 AND created_at <= $2 AND status_code >= 200 AND status_code < 300\`,
    startTime, endTime,
).Scan(&successCount)
```
**验证方法：**
1. `grep -n 'FILTER' internal/admin/handlers.go` — 确认无 `FILTER` 语法
2. `go build ./...` — 编译通过

---

## Fix #15: LogsPage 字段名错误

**文件：** `frontend/src/pages/LogsPage.tsx:269`
**原问题：** `rowKey="request_id"` 但后端返回 `id`，导致 React key 为 undefined。
**当前代码：**
```tsx
<Table
    columns={columns}
    dataSource={logs}
    loading={loading}
    rowKey="id"
    size="small"
```
**验证方法：**
1. `grep -n 'rowKey=' frontend/src/pages/LogsPage.tsx` — 确认字段名为 `"id"`
2. `cd frontend && npm run build` — 编译通过

---

## Fix #16: ChannelKeysPage/OrgModelPricesPage 缺少分页

**文件：**
- `internal/admin/handlers.go:1314` — `ListChannelKeys` 添加 `page`/`page_size` 参数和 `COUNT(*)` 查询
- `internal/admin/handlers.go:1741` — `ListOrgModelPrices` 添加 `page`/`page_size` 参数和 `COUNT(*)` 查询
- `frontend/src/api/client.ts` — 添加分页参数
- `frontend/src/pages/ChannelKeysPage.tsx` — 添加分页状态和 Semi UI 分页控件
- `frontend/src/pages/OrgModelPricesPage.tsx` — 添加分页状态和 Semi UI 分页控件

**原问题：** 后端全量查询，前端 `pagination={false}`。
**当前代码：**
```go
// ListChannelKeys (handlers.go:1314)
func (h *Handler) ListChannelKeys(w http.ResponseWriter, r *http.Request) {
    page, _ := strconv.Atoi(r.URL.Query().Get("page"))
    if page < 1 { page = 1 }
    pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
    if pageSize < 1 || pageSize > 100 { pageSize = 20 }
    offset := (page - 1) * pageSize

    var total int
    err := h.db.QueryRow(r.Context(), \`SELECT COUNT(*) FROM channel_keys\`).Scan(&total)
    // ... LIMIT $1 OFFSET $2
}
```
**验证方法：**
1. `grep -n 'page_size' internal/admin/handlers.go` — 确认分页参数存在
2. `grep -n 'pagination' frontend/src/pages/ChannelKeysPage.tsx` — 确认前端分页控件存在

---

## Fix #16 回归修复: TestListChannelKeys_Success mock 缺失

**文件：** `internal/admin/admin_mock_test.go`
**原问题：** 新增的 `QueryRow` COUNT 查询在 mock DB 中没有被 mock，导致 `TestListChannelKeys_Success` 失败。
**当前代码：**
```go
// admin_mock_test.go 中已有 queryRowFn mock
type mockDB struct {
    // ...
    queryRowFn func(ctx context.Context, sql string, args ...any) pgx.Row
}
```
**验证方法：**
1. `grep -n 'queryRowFn' internal/admin/admin_mock_test.go` — 确认 mock 存在
2. `go test ./internal/admin/ -run TestListChannelKeys -v` — 两个测试均 PASS

---

## Fix #17: DashboardPage 缺少 API 调用

**文件：** `frontend/src/pages/DashboardPage.tsx`
**原问题：** DashboardPage 硬编码空数据，未调用 `/api/admin/dashboard/stats` API。
**当前代码：**
```tsx
// DashboardPage.tsx 现在调用真实 API
const fetchData = async () => {
    const data = await getDashboardStats();
    setStats(data);
};
```
**验证方法：**
1. `grep -n 'getDashboardStats\|/api/admin/dashboard' frontend/src/pages/DashboardPage.tsx` — 确认 API 调用存在
2. `cd frontend && npm run build` — 编译通过

---

## Fix #14 补充修复：DashboardPage model_prices 字段名

**文件：** `frontend/src/pages/DashboardPage.tsx`
**原问题：** DashboardPage 的 `priceColumns` 使用 `dataIndex: 'model'`，但后端 `DashboardStats` API 返回的 `model_prices` 数组中字段名为 `model_name`，导致仪表盘模型价格表的"模型名称"列显示为空。
**修复内容：**
1. 类型接口 `model: string` → `model_name: string`（:15）
2. priceColumns `dataIndex: 'model'` → `dataIndex: 'model_name'`（:202）
3. Table `rowKey="model"` → `rowKey="model_name"`（:339）

**验证方法：**
1. `grep -n 'dataIndex.*model_name\|rowKey.*model_name' frontend/src/pages/DashboardPage.tsx` — 确认字段名一致
2. `cd frontend && npx vite build` — 编译通过
3. `go build ./...` — 后端编译通过
4. `go test ./...` — 全部 PASS

**状态：** ✅ 已修复验收（Codex 2026-06-28 修复并验收）

---

## 全局验证命令

```bash
cd /Users/m4/Desktop/projects/UniRouter
go build ./...
go vet ./...
go test ./... -count=1
cd frontend && npm run build
```

## 期望输出格式

验收结果写入 VERIFY-RESULT.md，格式见 AGENT-PROTOCOL.md 中的 VERIFY-RESULT.md 模板。
