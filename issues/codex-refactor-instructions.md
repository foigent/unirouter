# Codex 重构指令 — 测试壁垒解除

> 来源: Canary (测试 agent)
> 日期: 2026-06-25
> 前置条件: 458 测试, 0 FAIL, admin 82.7%, gateway 18.0%

## 需要 Codex 做的事

### 优先级 P1: gateway relay.go 接口注入 ✅ DONE

**问题**: `relay.go` 784 行，覆盖率 0%。gateway 包总覆盖率只有 18.0%，全部空白来自 relay.go。

**根因**: Relay 结构体直接持有具体类型：

```go
type Relay struct {
    db       db.DBQuerier     // ← 已改为接口
    rdb      *redis.Client    // ← 具体类型，无法 mock
    router   *Router
    billing  BillingProvider  // ← 已改为接口
    filter   *filter.DFAFilter
    encKey   []byte
}
```

**要求**:

1. ✅ `Relay.db` 已改为 `db.DBQuerier`
2. ✅ `Relay.billing` 已改为 `BillingProvider` 接口，包含：
   - `QueryModelPriceForOrg(ctx, orgID, model) (int, int, error)`
   - `CalculateCost(ctx, model, promptTokens, completionTokens) (int, error)`
   - `Freeze(ctx, billing.FreezeRequest) (*billing.FreezeResult, error)`
   - `Settle(ctx, billing.SettleRequest) (*billing.SettleResult, error)`
   - `Cancel(ctx, requestID string, orgID int64) error`
3. ✅ `NewRelay` 接收 `db.DBQuerier` + `BillingProvider`
4. ✅ 调用方传入 `*pgxpool.Pool` 和 `*billing.Billing`，行为不变

**参考模式**: 已有成功案例：
- `db.DBQuerier` 接口 — gateway/router.go 和 admin/handlers.go 都用这个接口
- `billing.PriceProvider` 接口 — billing.go 已用这个接口做价格查询
- gateway `router_mock_test.go` — 展示了如何用 mockDB 测试 Router

**预期效果**: relay.go 从 0% → 50%+（纯逻辑路径可覆盖），gateway 整体 18% → 40%+

**验证**: Canary 会写 mock 测试验证每个 relay 方法的正常/异常/边界路径。

---

### 优先级 P2: middleware GatewayAuth 提取 ✅ DONE

**问题**: middleware 覆盖率 78.8%，GatewayAuth (55 行) 是唯一 0% 的函数。

**根因**: GatewayAuth 闭包内直接调用 `pgxpool.Pool` 查询 API key 和 org 状态。

**要求**:

✅ 已将 GatewayAuth 闭包中的查询逻辑提取为独立函数：

```go
// 提取后: 查询逻辑独立可测
func resolveAPIKey(ctx context.Context, q db.DBQuerier, keyHash string) (*models.ApiKey, string, error)

func GatewayAuth(q db.DBQuerier, jwtSecret string) func(http.Handler) http.Handler
```

**预期效果**: middleware 78.8% → 85%+

---

## 不需要 Codex 做的事

- admin handler 测试 — Canary 已完成 82.7%
- billing 测试 — PriceProvider 接口已注入，Canary 已写 17 个纯单元测试
- router.go 测试 — Canary 已覆盖 95%+

## 注意事项

1. **不要改测试代码** — Canary 写的测试文件 (`admin_mock_test.go`, `router_mock_test.go`) 不要动
2. **接口定义后通知 Canary** — Canary 会立即写 mock 测试
3. **保持现有测试通过** — 重构后 `go test ./... -cover` 必须 0 FAIL
4. **参考 `issues/gateway-relay-testability.md`** — 有详细的函数依赖分析
