# 测试发现 — billing 包纯计算逻辑测试壁垒

> 发现者: Canary (测试 agent)
> 日期: 2026-06-24
> 状态: ✅ 已解决（Codex Fix #8 PriceProvider 接口注入 + Canary 17 纯单元测试）
> 来源: 阶段 1 集成测试扩展 + 阶段 1b 纯单元测试

## 发现

`internal/billing/billing.go` 中的 `CalculateCost` 和 `EstimateFreezeAmount` 方法存在架构壁垒：核心计算逻辑直接嵌在 `*Billing` 方法中，紧耦合 `b.db.QueryRow()`，无法在不连 PostgreSQL 的情况下单元测试。

## 证据

```go
// CalculateCost 直接调用 b.QueryModelPrice(ctx, model)，无法 mock
func (b *Billing) CalculateCost(ctx context.Context, model string, promptTokens, completionTokens int) (int, error) {
    inputPrice, outputPrice, err := b.QueryModelPrice(ctx, model)
    // ... 计算逻辑直接内嵌
}

// EstimateFreezeAmount 同理
func (b *Billing) EstimateFreezeAmount(ctx context.Context, model string, estimatedTokens int, safetyFactor float64, minFreezeAmount int) (int, error) {
    _, outputPrice, err := b.QueryModelPrice(ctx, model)
    // ... 计算逻辑直接内嵌
}
```

## 影响

- billing 单元覆盖率永久 0.5%（只有结构体字段测试）
- 边界值测试（溢出、负值、大数值）只能通过集成测试覆盖，每个边界需 DB 建行
- 无法对计算逻辑做变异测试
- 测试反馈慢（需 DB round-trip）

## 现有覆盖

集成测试（35 个，`//go:build integration`）已覆盖 Happy Path + 主要边界，但纯计算逻辑的边界值（0 token、MaxInt token、溢出、负值）无法高效测试。

## 建议方向

提取计算逻辑为纯函数（不依赖 DB 的结构体或方法），使计算方法可独立单元测试。具体实现方案由 Codex 评估。

## 优先级

Medium — 不阻塞功能，但长期阻碍 billing 包测试质量提升。
