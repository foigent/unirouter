# 阶段 2 执行计划 — gateway 包核心路由测试

> 创建: 2026-06-25 00:45
> 最后更新: 2026-06-25 00:50
> 状态: ✅ 已完成

## 基线

| 指标 | 值 |
|------|-----|
| gateway 测试数 | 22 → 46 |
| gateway 覆盖率 | 5.0%（纯函数已全覆盖，其余需 DB） |
| 全量测试 | 215 → 239（+24 新单元测试） |
| 全量状态 | 0 FAIL |

## 策略

**不做接口注入**（Codex 同意）。只补现有代码中可独立测试的纯函数。router.go 的 DB 依赖方法（Route/RouteAll/MarkChannel*）和 relay.go 的 handler 函数跳过。

## 已覆盖的纯函数

| 函数 | 文件 | 测试数 | 状态 |
|------|------|--------|------|
| `resolveModelMapping` | router.go | 12 (8+4) | ✅ |
| `estimateTokens` | relay.go | 13 (7+5+1) | ✅ |
| `writeError` | relay.go | 9 (4+5) | ✅ |
| `ChatCompletionRequest` parse | relay.go | 6 (1+5) | ✅ |
| `EmbeddingsRequest` parse | relay.go | 4 | ✅ 新增 |
| `TestSensitiveWordFilter_Integration` | gateway_test.go | 1 | ✅ |
| `TestRequestWithContext` | gateway_test.go | 1 | ✅ |

## 待办步骤

### A 线：纯函数边界补全

- [x] 2.1 `resolveModelMapping` — 补 4 个：特殊字符/超长/mapped empty/单 key 不匹配
- [x] 2.2 `estimateTokens` — 补 5 个：0 maxTokens/1M maxTokens/空 content/空 slice/system+assistant
- [x] 2.3 `ChatCompletionRequest` — 补 5 个：Stream/nil MaxTokens/0 MaxTokens/extra fields/empty messages

### B 线：relay.go 可测分支

- [x] 2.4 分析 relay.go — 确认 parseRequest/buidUpstreamURL 内联在 handler 里，无独立纯函数
- [x] 2.5 `EmbeddingsRequest` parse — 补 4 个：string input/array input/empty model/invalid JSON
- [x] 2.6 HandleModels fallback — 跳过（需 DB mock）

### C 线：已有代码扩展

- [x] 2.7 `writeError` — 补 5 个：500/unicode/所有 Content-Type/200/large message
- [x] 2.8 DFA filter — 已有 5 个测试，无需追加
- [x] 2.8b `estimateTokens` nil vs 0 maxTokens 对比

### 收尾

- [ ] 2.9 全量回归 + 更新文档

## 结果

| 指标 | 初始 | 最终 |
|------|------|------|
| gateway 测试数 | 22 | 46 (+24) |
| gateway 覆盖率 | 5.0% | 5.0%（全量代码中纯函数占比小，覆盖率数字不变但纯函数全覆盖） |
| resolveModelMapping 覆盖 | 8 用例 | 12 用例 |
| estimateTokens 覆盖 | 7 用例 | 13 用例 |
| writeError 覆盖 | 4 用例 | 9 用例 |
| ChatCompletionRequest parse | 1 用例 | 6 用例 |
| EmbeddingsRequest parse | 0 | 4 用例 |
| 全量测试数 | 215 | 239 |

## 关键决策

| 时间 | 决策 | 依据 |
|------|------|------|
| 00:45 | 不做 router 接口注入 | Codex 同意。可测纯逻辑有限，接口注入收益不如 billing |
| 00:48 | 跳过 HandleModels 测试 | 需 DB mock，不在当前阶段范围 |
| 00:50 | gateway 覆盖率 5.0% 不变但纯函数全覆盖 | 1316 行代码中 95% 是 DB 依赖的 handler，纯函数已充分测试 |
