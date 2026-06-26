# gateway relay.go 测试壁垒

> 发现者: Canary (测试 agent)
> 日期: 2026-06-25
> 类型: 测试壁垒 / 架构问题 (非 bug)
> 状态: ✅ 已解决 (Codex 接口注入 + Canary 30+ relay mock 测试)
> gateway 覆盖率: 18.0% → 76.8%

## 问题

`relay.go` 784 行，覆盖率 0%。这是 gateway 包覆盖率只有 18% 的唯一原因（router.go 已接近全覆盖）。

## 根因

Relay 结构体直接持有具体类型，无接口抽象：

```go
type Relay struct {
    db       *pgxpool.Pool    // ← 非接口
    rdb      *redis.Client    // ← 非接口
    router   *Router          // ← 已有 DBQuerier 接口
    billing  *billing.Billing // ← struct，非接口
    filter   *filter.DFAFilter
    encKey   []byte
    alertSvc *billing.AlertService
}
```

## 受影响的函数

| 函数 | 行数 | 依赖 | 可测性 |
|------|------|------|--------|
| HandleChatCompletion | 172 | db, router, billing, filter, rdb | 需全部 mock |
| relayNonStreamRequest | 60 | db, billing | 需 db + billing mock |
| relayStreamRequest | 206 | db, billing, filter | 需全部 mock |
| calculateCost | 14 | billing | 需 billing 接口 |
| recordRequest | 12 | db | 需 db 接口 |
| HandleEmbeddings | 150 | db, router, billing | 需全部 mock |
| HandleModels | 35 | db | 需 db 接口 |
| handleModelsFallback | 28 | db | 需 db 接口 |
| checkBudgetAlert | 4 | alertSvc | 已可测（nil check） |

## 建议方向

### 方案 A: Relay 接口注入（推荐）

```go
type RelayQuerier interface {
    QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
    Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
    Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

type BillingProvider interface {
    QueryModelPriceForOrg(ctx context.Context, orgID int64, model string) (int, int, error)
    CalculateCost(ctx context.Context, model string, promptTokens, completionTokens int) (int, error)
    Freeze(ctx context.Context, orgID int64, requestID string, model string, promptTokens int) (int, error)
    Settle(ctx context.Context, orgID int64, requestID string, model string, promptTokens, completionTokens int) (int, error)
    Cancel(ctx context.Context, requestID string) error
}
```

改动量: 中（Relay 构造函数 + billing 接口提取）
测试收益: 高（relay.go 784 行可全覆盖）
风险: 低

### 方案 B: 提取纯函数

从 relay.go 提取以下为独立函数：
- `calculateCost` → 已有，但依赖 billing
- `buildModelResponse` → 从 HandleModels 提取
- `parseStreamChunk` → 从 relayStreamRequest 提取

改动量: 小
测试收益: 中（只能测提取部分）

### 方案 C: httptest 端到端

用 httptest.NewServer mock 上游 provider，测试 HandleChatCompletion 的完整流程。

改动量: 大（需要 mock 上游 + billing + DB）
测试收益: 最高（端到端覆盖）
风险: 中（维护成本高）

## 优先级

**P1** — relay.go 是 gateway 的核心（请求转发、流式处理、计费），784 行 0% 覆盖率是最大的测试盲区。建议方案 A，与 admin handler 的 DBQuerier 模式一致。

## 当前 gateway 覆盖率

| 文件 | 行数 | 覆盖率 |
|------|------|--------|
| router.go | 229 | ~95% |
| relay.go | 784 | 0% |
| **总计** | **1013** | **18.0%** |
