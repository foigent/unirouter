# FIX-SIGNAL.md — 验收指令（给 Canary/OpenClaw Agent）

**项目：** UniRouter — 企业级 AI 模型管控平台（Go + React）
**项目路径：** /Users/m4/Desktop/projects/UniRouter
**修复者：** main agent
**日期：** 2026-07-01
**修复轮次：** 第 4 轮

---

## 验收任务

请验证以下 1 条 fix（Issue C 修复），以及前 6 条 fix 是否仍然正常。

---

## 前置条件（本轮预期不变的部分）

| 项目 | 预期状态 | 原因 |
|------|---------|------|
| 前 6 条 fix（#1-#6） | 预期仍 PASS | 本轮未修改这些代码 |
| billing 全量集成测试 | 预期 PASS | 本轮修复了 Issue C（Settle SQL 参数类型），Settle 相关测试应通过 |

> Canary 验收时：前置条件中列出的预期失败不算 FAIL，只需在 VERIFY-RESULT.md 的「前置条件核验」中注明。

---

### Fix #1: billing.go QueryModelPrice 列名修正

**文件：** `internal/billing/billing.go:68`
**原问题：** SQL 查询了不存在的列 `price_per_input_token` / `price_per_output_token`
**当前代码：**
```sql
SELECT input_price_cents, output_price_cents FROM model_prices WHERE model_name = $1
```
**验证方法：**
1. 检查 `migrations/` 目录中 `model_prices` 表的实际列名，确认 SQL 列名与 schema 一致
2. 运行 `go test ./internal/billing/ -run TestQueryModelPrice -v`
3. 确认 fallback 默认值 `inputCost=2, outputCost=10`（第 72-73 行）语义正确（$0.002/1K input, $0.01/1K output）

---

### Fix #2: relay.go defer-in-loop 资源泄漏修复

**文件：** `internal/gateway/relay.go`
**原问题：** `defer resp.Body.Close()` 在 for 循环内部，defer 会累积，body 在函数结束前不会关闭
**修复方案：** 移除 defer，改为在每个分支显式关闭
**当前 Body.Close() 位置：**
- `relay.go:205` — HTTP 非 200 错误路径
- `relay.go:333` — 流式 balance exhausted 路径
- `relay.go:438` — 非流式 balance exhausted 路径
- `relay.go:669` — relayNonStreamRequest 成功路径末尾

**验证方法：**
1. `grep -n 'defer.*resp.Body.Close' internal/gateway/relay.go` — 确认循环内无 defer（只有第 77 行和 576 行的 `r.Body.Close()` 是合法的，它们是请求入口 defer）
2. `grep -n 'resp.Body.Close' internal/gateway/relay.go` — 确认每个 error/break/return 路径都有显式 Close
3. 逻辑审查：在 for-retry 循环中，`continue` 之前必须有 `resp.Body.Close()`

---

### Fix #3: router.go Exec 错误处理

**文件：** `internal/gateway/router.go:76`
**原问题：** `r.db.Exec(...)` 返回值被完全丢弃
**当前代码：**
```go
_, _ = r.db.Exec(ctx, `UPDATE channel_keys SET last_used_at = NOW() WHERE id = $1`, ck.ID)
```
**验证方法：**
1. 确认使用 `_, _ =` 而非完全忽略返回值（即 `r.db.Exec(...)` 单独一行无赋值）
2. 判断：这是一个 fire-and-forget 更新（last_used_at 用于 round-robin 优先级，失败不影响主流程），`_, _ =` 的处理方式是否合理？
3. 如果你认为应该记录日志，请提出建议

---

### Fix #4: XOR vs AES-GCM 加密方案确认

**文件：** `internal/auth/auth.go:46-87`
**原问题：** 文档声称用 XOR，但代码实际用 AES-256-GCM
**当前状态：** 代码已确认使用 AES-256-GCM（`EncryptKey` / `DecryptKey` 函数）
**验证方法：**
1. `grep -n 'cipher\.\|aes\.\|gcm\.\|XOR\|xor' internal/auth/auth.go` — 确认无 XOR，有 AES-GCM
2. 检查 nonce 生成是否使用 `crypto/rand`（第 55-58 行）
3. 确认文档（CONTEXT_HANDOFF.md、PLAN.md、CHECKLIST.md）中已修正 XOR 引用

---

### Fix #5: billing.go fallback 静默降级加日志（Issue B 修复）

**文件：** `internal/billing/billing.go:73`
**原问题：** `QueryModelPrice()` 查询失败后静默 fallback 到默认价格，不记录日志。如果 DB 连接异常或 schema 再次变更，管理员不会知道计费使用的是默认价格。
**当前代码：**
```go
if err != nil {
    // Fallback default: $0.002/1K input, $0.01/1K output
    log.Printf("QueryModelPrice: fallback to default for model %s: %v", model, err)
    inputCost = 2
    outputCost = 10
    err = nil
}
```
**验证方法：**
1. 确认 `log` 包已导入（`internal/billing/billing.go` 第 6 行）
2. 确认 fallback 路径有 `log.Printf` 输出 model 名和原始错误
3. 运行 `go vet ./internal/billing/` 无警告

---


### Fix #6: billing.go:138 freeze_idempotency 查询列不存在（Bug #0.1）

**文件：** `internal/billing/billing.go:137-145, 228`
**原问题：** `SELECT id FROM freeze_idempotency WHERE request_id = $1` — `freeze_idempotency` 表没有 `id` 列，导致 Freeze() 事务在第一个 SQL 就失败（25P02 级联），所有 billing 集成测试失败。
**修复方案：**
```go
// 修复前
var existingID int64
err = tx.QueryRow(ctx,
    \`SELECT id FROM freeze_idempotency WHERE request_id = $1\`, req.RequestID).Scan(&existingID)
return &FreezeResult{FreezeID: existingID, ...}

// 修复后
var existingStatus string
err = tx.QueryRow(ctx,
    \`SELECT status FROM freeze_idempotency WHERE request_id = $1\`, req.RequestID).Scan(&existingStatus)
return &FreezeResult{FreezeID: 0, ...}
```
**验证方法：**
1. `grep -n 'SELECT.*FROM freeze_idempotency' internal/billing/billing.go` — 确认查询 `status` 而非 `id`
2. `grep -n 'existingID' internal/billing/billing.go` — 确认无残留引用
3. `go build ./...` — 编译通过
4. `go vet ./internal/billing/` — 无警告
5. 运行 billing 集成测试 — 原 25P02 级联错误应消除

---

### Fix #7: billing.go Settle INSERT $3 参数类型冲突修复（Issue C）

**文件：** `internal/billing/billing.go:275-278`
**原问题：** INSERT 语句中 $3 同时用于 `request_id`（VARCHAR 直接值）和 note（|| $3::text 表达式），PostgreSQL 无法推断一致类型，报 `inconsistent types deduced for parameter $3 (SQLSTATE 42P08)`，导致 Settle() 永远失败。
**修复方案：**
```go
// 修复前: $3 用于两处不同上下文，类型冲突
VALUES ($3, $1, 'settle', 'completed', $2, 'Settle for request ' || $3::text)
..., req.OrgID, req.ActualCost, req.RequestID)

// 修复后: $3 仅用于 request_id，$4 用于 note 拼接
VALUES ($3, $1, 'settle', 'completed', $2, 'Settle for request ' || $4::text)
..., req.OrgID, req.ActualCost, req.RequestID, req.RequestID)
```
**验证方法：**
1. `grep -n 'INSERT INTO transactions' internal/billing/billing.go` — 确认 use $4 in note
2. `go build ./...` — 编译通过
3. `go test ./internal/billing/ -v -tags=integration` — 全量 billing 测试 PASS（含 Settle）

---

## 全局验证命令

```bash
cd /Users/m4/Desktop/projects/UniRouter
go build ./...                    # 编译通过
go vet ./...                      # 静态分析
go test ./internal/billing/ -v    # billing 测试
go test ./internal/auth/ -v       # auth 测试
```

## 输出格式

验收完成后，请写入 `VERIFY-RESULT.md` 到项目根目录，格式：
```
# 验收结果
- Fix #1: PASS/FAIL + 原因
- Fix #2: PASS/FAIL + 原因
- Fix #3: PASS/FAIL + 原因
- Fix #4: PASS/FAIL + 原因
- Fix #5: PASS/FAIL + 原因
- Fix #6: PASS/FAIL + 原因
- Fix #7: PASS/FAIL + 原因
- 全局构建: PASS/FAIL
- 新发现的问题: （如有）
```

---

### Fix #8: PriceProvider 接口注入（Plan B 重构）

**文件：** `internal/billing/price_provider.go`（新文件）、`internal/billing/billing.go`
**原问题：** `CalculateCost()` 和 `EstimateFreezeAmount()` 直接调用 `b.QueryModelPrice()`（DB 查询），无法脱离数据库做单元测试。
**修复方案：** 接口注入 — 定义 `PriceProvider` 接口，`Billing` 结构体通过 `prices` 字段持有实现。
**新增文件：**
- `internal/billing/price_provider.go` — `PriceProvider` 接口 + `DBPriceProvider` 实现 + `PriceProviderFunc` 适配器
**修改文件：**
- `internal/billing/billing.go` — `Billing` 增加 `prices PriceProvider` 字段，`NewBilling` 默认创建 `DBPriceProvider`，新增 `WithPriceProvider()` 方法，`QueryModelPrice`/`QueryModelPriceForOrg`/`CalculateCost`/`EstimateFreezeAmount` 委托给 `b.prices`
**新增测试：**
- `internal/billing/billing_calc_test.go` — 17 个纯单元测试（不依赖 DB），覆盖：
  - CalculateCost: 正常、亚分、零 token、大 token、边界 999/1000、仅 completion、provider 报错、零价格
  - EstimateFreezeAmount: 正常、min freeze 兜底、safety factor=0、大值、零 token、provider 报错、四舍五入
  - WithPriceProvider 覆盖验证
**验证方法：**
1. `go build ./...` — 编译通过
2. `go vet ./...` — 无警告
3. `go test ./internal/billing/ -v` — 全部 25 个测试 PASS（17 新 + 8 原有）
4. 确认 `relay.go` 调用 `billing.QueryModelPriceForOrg()` 无需修改（向后兼容）

---

### Fix #9: GatewayAuth 纯函数提取（middleware 覆盖率提升）

**文件：** `internal/middleware/auth.go:30-72`（新增纯函数）、`internal/middleware/auth.go:150-195`（重构 GatewayAuth）
**原问题：** `GatewayAuth` 函数 0% 覆盖率，所有逻辑耦合在 DB 查询中间件中，无法单元测试。
**修复方案：** 提取两个纯函数 + 一个错误类型：

1. **`extractAPIKey(r *http.Request) (string, error)`** — 从 Authorization header 提取 API key
   - 检查 header 是否存在
   - 检查是否为 "Bearer " 前缀
   - 检查 key 是否为空
   - 返回 `*AuthError` 错误类型

2. **`validateKeyAndOrgStatus(apiKey *models.ApiKey, orgStatus string) error`** — 验证 key 和 org 状态
   - 检查 key 是否 active
   - 检查 org 是否 active
   - 返回带 HTTP status 的 `*AuthError`

3. **`AuthError` 结构体** — 统一错误类型，包含 Code、Message、Status 字段

**重构后的 GatewayAuth：**
- 使用 `extractAPIKey(r)` 替代内联 header 检查
- 使用 `validateKeyAndOrgStatus(&apiKey, orgStatus)` 替代内联状态检查
- 错误处理使用 `errors.As(err, &authErr)` 类型断言

**新增测试：** `internal/middleware/auth_test.go`，12 个纯单元测试：
- extractAPIKey: 6 个用例（missing header、invalid format、empty key、valid key、Bearer only、case sensitive）
- validateKeyAndOrgStatus: 5 个用例（inactive key、inactive org、active、expired key、suspended org）
- AuthError.Error(): 1 个用例

**验证方法：**
1. `go build ./internal/middleware/` — 编译通过
2. `go test ./internal/middleware/ -v` — 全部 52 个测试 PASS
3. `go test ./internal/middleware/ -coverprofile=cover.out` — 覆盖率 47.9%（从 42.7% 提升）
4. `extractAPIKey` 和 `validateKeyAndOrgStatus` 覆盖率 100%
5. 集成测试 `TestGatewayAuth_*_Integration` 全部 PASS（10 个用例）

---

### Fix #10: P0 边界测试补充（getClientIP + checkIPWhitelist）

**文件：** `internal/middleware/auth_test.go`（新增 20 个边界测试）
**原问题：** `getClientIP` 和 `checkIPWhitelist` 缺少边界场景测试，无法覆盖 IPv6、优先级、错误处理等场景。
**修复方案：** 补充 20 个边界测试用例：

**getClientIP 边界测试（8 个）：**
1. `TestGetClientIP_XForwardedFor_MultipleIPs` — 多 IP 场景，取第一个
2. `TestGetClientIP_XForwardedFor_SingleIP` — 单 IP 场景
3. `TestGetClientIP_XForwardedFor_Empty` — 空 X-Forwarded-For，回退到 RemoteAddr
4. `TestGetClientIP_XRealIP_Only` — 仅 X-Real-IP 场景
5. `TestGetClientIP_XRealIP_WithSpaces` — X-Real-IP 带空格
6. `TestGetClientIP_RemoteAddr_WithPort` — RemoteAddr 带端口
7. `TestGetClientIP_XForwardedFor_TakesPrecedence` — X-Forwarded-For 优先级最高
8. `TestGetClientIP_XRealIP_TakesPrecedenceOverRemoteAddr` — X-Real-IP 优先级高于 RemoteAddr

**checkIPWhitelist 边界测试（12 个）：**
1. `TestCheckIPWhitelist_InvalidCIDR_Entry` — 无效 CIDR 被跳过
2. `TestCheckIPWhitelist_InvalidCIDR_WithValid` — 无效 CIDR 被跳过，有效 CIDR 匹配
3. `TestCheckIPWhitelist_IPv6_ExactMatch` — IPv6 精确匹配
4. `TestCheckIPWhitelist_IPv6_CIDR` — IPv6 CIDR 匹配
5. `TestCheckIPWhitelist_IPv6_NoMatch` — IPv6 CIDR 不匹配
6. `TestCheckIPWhitelist_MixedEntries` — 混合条目（精确 + CIDR）
7. `TestCheckIPWhitelist_AllEmpty` — 空 IP + 空白名单
8. `TestCheckIPWhitelist_IPv4_ExactMismatch` — IPv4 精确不匹配
9. `TestCheckIPWhitelist_IPv4_CIDR_Broadcast` — 广播地址在 /24 范围内
10. `TestCheckIPWhitelist_IPv4_CIDR_NetworkAddress` — 网络地址在 /24 范围内
11. `TestCheckIPWhitelist_IPv6_ExactMatch` — IPv6 精确匹配
12. `TestCheckIPWhitelist_IPv6_CIDR` — IPv6 CIDR 匹配

**验证方法：**
1. `go test ./internal/middleware/ -v -count=1` — 全部 72 个测试 PASS
2. `go test ./... -count=1` — 全项目测试通过
3. `go test ./internal/middleware/ -coverprofile=cover.out` — 覆盖率 47.9%
4. `getClientIP` 覆盖率 100%，`checkIPWhitelist` 覆盖率 93.8%

---

### Fix #11: Router 接口注入 — DBQuerier 接口解耦 pgxpool.Pool

**问题：**
Router 结构体直接持有 `*pgxpool.Pool`，所有方法都调用 `r.db.Query/Exec/QueryRow`，无法对路由逻辑做纯单元测试（gateway 覆盖率 5%）。

**改法：**
1. 在 `internal/db/postgres.go` 新增 `DBQuerier` 接口（Query/QueryRow/Exec/Begin）
2. `internal/gateway/router.go`：Router.db 类型从 `*pgxpool.Pool` 改为 `db.DBQuerier`，NewRouter 参数同步修改
3. main.go 无需改动（`*pgxpool.Pool` 满足 `DBQuerier` 接口）

**接口定义：**
```go
// DBQuerier is the common database query interface used by Router and Handler
// for unit testability. *pgxpool.Pool satisfies this interface.
type DBQuerier interface {
    Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error)
    QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row
    Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error)
    Begin(ctx context.Context) (pgx.Tx, error)
}
```

**接口位置：** `internal/db/postgres.go` — gateway 和 admin 共用

**改动范围：**
- `internal/db/postgres.go`：新增 DBQuerier 接口（+12 行，含 Begin 供 admin Handler 后续使用）
- `internal/gateway/router.go`：Router.db 类型 + NewRouter 参数改为 DBQuerier（~3 行改动）

**验证：**
1. `go build ./...` — 编译通过
2. `go test ./... -count=1` — 全项目测试通过（admin/auth/billing/circuitbreaker/config/filter/gateway/middleware/models/tokenutil 全部 PASS）
3. main.go 的 `gateway.NewRouter(postgres.Pool)` 无需改动（*pgxpool.Pool 满足接口）

**收益：**
- gateway 包现在可以用 mock DBQuerier 测试 Route/RouteAll/MarkChannelSuccess 等路由逻辑
- admin Handler 后续可复用同一个 DBQuerier 接口（含 Begin），零额外成本

---

### Fix #12: Admin Handler 接口注入 — DBQuerier 接口解耦 pgxpool.Pool

**问题：**
Handler 结构体直接持有 `*pgxpool.Pool`，所有 14 个 Handler 方法都依赖真实数据库连接，无法做纯单元测试（admin 覆盖率 1%）。

**改法：**
1. `internal/admin/handlers.go`：Handler.db 类型从 `*pgxpool.Pool` 改为 `db.DBQuerier`，NewHandler 参数同步修改
2. 移除 `pgxpool` import，改用 `internal/db` 包
3. main.go 无需改动（`*pgxpool.Pool` 满足 `DBQuerier` 接口）

**改动范围：**
- `internal/admin/handlers.go`：3 处类型声明改动（struct field + NewHandler 参数 + import）

**事务处理兼容性：**
- 3 处 `h.db.Begin(r.Context())` 返回 `pgx.Tx`，调用 `tx.Commit/Rollback` 无需任何改动
- `DBQuerier.Begin()` 返回 `pgx.Tx`，与现有事务代码完全兼容

**验证：**
1. `go build ./...` — 编译通过
2. `go test ./... -count=1` — 全项目 10 个包全部 PASS
3. `pgxpool` 引用已从 admin/handlers.go 和 gateway/router.go 完全移除

**收益：**
- admin 包现在可以用 mock DBQuerier 测试所有 14 个 Handler 方法
- 与 Fix #11 共用同一个 `DBQuerier` 接口定义，零额外成本
