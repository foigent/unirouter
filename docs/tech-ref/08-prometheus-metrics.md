# Prometheus Metrics（监控指标）

> **版本**：v1.0
> **日期**：2026-06-24
> **用途**：开发团队理解监控指标体系的核心参考
> **变更**：初始版本，覆盖 G3 迭代实现

---

## 一、设计目标

| 目标 | 说明 |
|------|------|
| 标准兼容 | 遵循 Prometheus 指标命名规范 |
| 零配置 | 指标自动注册，无需手动配置 |
| 最小侵入 | 中间件方式采集，业务代码无感知 |
| Grafana 友好 | 指标标签设计支持多维度聚合 |

---

## 二、指标清单

### 2.1 HTTP 请求指标

| 指标名 | 类型 | 标签 | 说明 |
|--------|------|------|------|
| `unirouter_http_requests_total` | Counter | `method`, `path`, `status` | HTTP 请求总数 |
| `unirouter_http_request_duration_seconds` | Histogram | `method`, `path` | 请求处理耗时 |

**耗时分桶**：`0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5` 秒

### 2.2 业务指标

| 指标名 | 类型 | 标签 | 说明 |
|--------|------|------|------|
| `unirouter_tokens_processed_total` | Counter | `type`, `model` | 处理的 Token 总数 |
| `unirouter_upstream_latency_seconds` | Histogram | `provider`, `model` | 上游供应商延迟 |
| `unirouter_active_requests` | Gauge | — | 当前活跃请求数 |
| `unirouter_billing_freeze_total` | Counter | `status` | 计费冻结操作次数 |

**上游延迟分桶**：`0.01, 0.05, 0.1, 0.5, 1, 2, 5, 10` 秒

### 2.3 标签值说明

| 标签 | 取值范围 | 示例 |
|------|---------|------|
| `method` | HTTP 方法 | `GET`, `POST` |
| `path` | 请求路径 | `/v1/chat/completions` |
| `status` | HTTP 状态码 | `200`, `500` |
| `type` | Token 类型 | `prompt`, `completion` |
| `model` | 模型名称 | `gpt-4`, `deepseek-v4` |
| `provider` | 供应商标识 | `openai`, `deepseek` |
| `status` (billing) | 操作结果 | `success`, `error`, `admission_rejected` |

---

## 三、采集方式

### 3.1 中间件自动采集

`MetricsMiddleware` 在每个 HTTP 请求的入口和出口自动采集：

```go
func MetricsMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()
        activeRequests.Inc()
        defer activeRequests.Dec()

        rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
        next.ServeHTTP(rw, r)

        duration := time.Since(start).Seconds()
        status := strconv.Itoa(rw.statusCode)

        httpRequestsTotal.WithLabelValues(r.Method, r.URL.Path, status).Inc()
        httpRequestDuration.WithLabelValues(r.Method, r.URL.Path).Observe(duration)
    })
}
```

### 3.2 业务代码手动采集

在 relay 和 billing 模块中，通过辅助函数记录业务指标：

```go
// 记录 Token 使用量
middleware.RecordTokenUsage("prompt", "gpt-4", 150)
middleware.RecordTokenUsage("completion", "gpt-4", 500)

// 记录上游延迟
middleware.RecordUpstreamLatency("openai", "gpt-4", 1.2*time.Second)

// 记录计费冻结
middleware.RecordBillingFreeze("success")
middleware.RecordBillingFreeze("error")
```

---

## 四、端点配置

### 4.1 暴露端点

```
GET /metrics
```

返回 Prometheus 文本格式的指标数据，无需认证。

### 4.2 中间件链顺序

```
CORS → Metrics → Admission → Router
```

**注意事项**：
- Metrics 中间件在 Admission 之前，确保被拒绝的请求也被记录
- `/metrics` 端点本身不经过业务中间件链

### 4.3 安全考虑

生产环境中 `/metrics` 端点建议：
- 仅内网访问（通过防火墙或反向代理限制）
- 或添加 Basic Auth 认证

---

## 五、Grafana 面板建议

### 5.1 请求速率面板

```promql
rate(unirouter_http_requests_total{path="/v1/chat/completions"}[5m])
```

### 5.2 延迟分布面板

```promql
histogram_quantile(0.95, rate(unirouter_http_request_duration_seconds_bucket[5m]))
```

### 5.3 错误率面板

```promql
rate(unirouter_http_requests_total{status=~"5.."}[5m]) / rate(unirouter_http_requests_total[5m])
```

### 5.4 活跃请求面板

```promql
unirouter_active_requests
```

### 5.5 上游延迟面板

```promql
histogram_quantile(0.95, rate(unirouter_upstream_latency_seconds_bucket[5m]))
```

---

## 六、扩展方向

| 方向 | 说明 |
|------|------|
| 按 org 聚合 | 在 counter 标签中添加 `org_id` |
| 计费金额指标 | 新增 `unirouter_billing_amount_total` counter |
| 渠道健康指标 | 新增 `unirouter_channel_health` gauge |
| 队列深度 | 新增 `unirouter_queue_depth` gauge |
