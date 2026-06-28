# 待办事项 — 监控指标集成

> **日期**: 2026-06-27
> **来源**: Canary 测试发现
> **状态**: ✅ P1+P2 已完成，P3 待执行

## 背景

`internal/middleware/metrics.go` 定义了 6 个 Prometheus 指标，其中 3 个 HTTP 指标已自动注册并出现在 `/metrics` 端点，但 3 个业务指标未被 relay.go 调用，因此不会出现在 `/metrics` 输出中。

## 待办清单

### 1. relay.go 集成 Record* 函数（P1）✅

| 函数 | 用途 | 调用位置 |
|------|------|---------|
| `RecordTokenUsage(tokenType, model, count)` | 记录 token 处理量 | relayStreamRequest / relayNonStreamRequest / HandleEmbeddings 获取 usage 后 |
| `RecordUpstreamLatency(provider, model, duration)` | 记录上游延迟 | handleUpstreamWithFallback / relayStreamRequest / HandleEmbeddings 上游请求完成后 |
| `RecordBillingFreeze(status)` | 记录计费冻结操作 | ChatCompletion + Embeddings Freeze 调用后（success/error） |

### 2. Prometheus + Grafana 部署验证（P2）✅

- Prometheus: http://8.130.18.85:9090, target=up, scrape_interval=5s
- Grafana: http://8.130.18.85:3001, UniRouter Overview 8 面板
- 数据链路: Grafana → Prometheus → Backend `/metrics` 已打通
- 线上验证: 3 个业务指标均有数据（tokens/upstream/billing_freeze）

### 3. k6 压测执行（P2）

- 需要安装 k6 工具
- 需要有效的 API Key（`K6_API_KEY` 环境变量）
- 脚本位置：`infra/loadtest/k6-chat-completions.js`

## 当前状态

| 指标 | 定义 | 注册 | relay 调用 | /metrics 可见 |
|------|------|------|-----------|--------------|
| unirouter_http_requests_total | ✅ | ✅ | ✅ (MetricsMiddleware) | ✅ |
| unirouter_http_request_duration_seconds | ✅ | ✅ | ✅ (MetricsMiddleware) | ✅ |
| unirouter_active_requests | ✅ | ✅ | ✅ (MetricsMiddleware) | ✅ |
| unirouter_tokens_processed_total | ✅ | ✅ | ✅ relay.go 三处调用 | ✅ |
| unirouter_upstream_latency_seconds | ✅ | ✅ | ✅ 三处上游调用 | ✅ |
| unirouter_billing_freeze_total | ✅ | ✅ | ✅ Freeze 后 success/error | ✅ |
