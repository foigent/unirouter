# middleware GatewayAuth 测试壁垒

> 发现者: Canary (测试 agent)
> 日期: 2026-06-25
> 状态: ✅ 已解决 (Codex 接口注入 + Canary 8 GatewayAuth mock 测试)
> middleware 覆盖率: 78.8% → 98.2%

## 问题

middleware 包覆盖率停留在 42.7%，无法达到 50% 目标，根因是：

1. **GatewayAuth (108行) 0% 覆盖** — 核心认证中间件，全部逻辑内联在闭包中，紧耦合 pgxpool.Pool DB 查询
2. **RateLimit (75行) 0% 覆盖** — Redis 依赖，无法在纯单元测试中覆盖

## 已做的覆盖

| 纯函数 | 测试数 | 覆盖率 |
|--------|--------|--------|
| checkIPWhitelist | 10 | 93.8% |
| getClientIP | 9 | 100% |
| hashKey | 6 | 100% |
| generateRequestID | 2 | 100% |
| uniqueMember | 2 | 100% |
| CORS | 3 | 100% |
| context getters | 6 | 100% |
| AdminAuth | 5 | 100% |
| **GatewayAuth (集成)** | **9** | 集成不计入 cover |

## 建议（Codex 已完成）

### GatewayAuth 透明化

✅ 已完成。GatewayAuth 闭包中的查询逻辑提取为独立函数 `resolveAPIKey`，接收 `db.DBQuerier` 接口。

```go
// internal/middleware/auth.go:151
func resolveAPIKey(ctx context.Context, q db.DBQuerier, keyHash string) (*models.ApiKey, string, error)

// internal/middleware/auth.go:174
func GatewayAuth(q db.DBQuerier, jwtSecret string) func(http.Handler) http.Handler
```

GatewayAuth 现在是编排这些函数的薄层。`resolveAPIKey` 可单独单元测试。

### RateLimit 透明化

将 RateLimit 中的 QPS/RPM 计数逻辑提取为纯函数，接收 `redis.UniversalClient` 接口（非具体类型），单元测试可以用 miniredis mock。

## 结论

这是可测试性架构问题，不是代码 bug。GatewayAuth 和 RateLimit 的功能正确性已通过集成测试覆盖。
