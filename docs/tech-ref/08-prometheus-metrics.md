# Prometheus Metrics（监控指标）

> **版本**：v1.2
> **日期**：2026-06-27
> **用途**：开发团队理解监控指标体系的核心参考
> **变更**：
> - v1.2：补充 3.2 节——业务指标在 relay.go 中的完整调用位置表（15 处调用点，含行号、函数、标签值、触发条件）
> - v1.1：新增 Grafana Dashboard JSON、Prometheus 抓取配置、k6 压测脚本说明；初始版本覆盖 G3 迭代实现

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

以下表格列出 `internal/gateway/relay.go` 中所有 15 处 `middleware.Record*` 调用的精确位置、触发条件和标签值。所有调用均已在代码中落地并通过编译验证（`go build ./...` + `go test` PASS）。

#### 3.2.1 `RecordBillingFreeze` — 6 处调用

`BillingFreeze` 在每次请求的计费预冻结阶段调用，记录冻结是否成功。

**函数：`HandleChatCompletion`（`/v1/chat/completions`）** — 4 处

| 行号 | 标签值 | 触发条件 |
|------|--------|---------|
| 150 | `"error"` | Freeze 成功返回但业务失败，错误码 `insufficient_balance`（余额不足） |
| 153 | `"error"` | Freeze 成功返回但业务失败，错误码 `already_frozen`（幂等冲突） |
| 156 | `"error"` | Freeze 成功返回但业务失败，其他未知错误码 |
| 161 | `"success"` | Freeze 成功且业务成功，请求继续进入路由选择阶段 |

**函数：`HandleEmbeddings`（`/v1/embeddings`）** — 2 处

| 行号 | 标签值 | 触发条件 |
|------|--------|---------|
| 666 | `"error"` | Freeze 失败（`err != nil` 或 `!freezeResult.Success`，含 `insufficient_balance` 和其他错误） |
| 674 | `"success"` | Freeze 成功，请求继续进入路由选择阶段 |

> **注意**：ChatCompletion 路径对 `!freezeResult.Success` 做了三分支 switch（`insufficient_balance` / `already_frozen` / default），每个分支独立记录 `"error"`。Embeddings 路径合并为统一的 `"error"` 分支。`err != nil`（网络/DB 层错误）在两个路径中都不调用 `RecordBillingFreeze`，而是直接返回 500，因为此时 freeze 未产生有效结果。

#### 3.2.2 `RecordTokenUsage` — 5 处调用

`RecordTokenUsage` 在请求完成后、Settle 结算成功后调用，记录实际消耗的 token 数。

**函数：`relayNonStreamRequest`（非流式 ChatCompletion）** — 2 处

| 行号 | type 标签 | model 标签 | 值来源 |
|------|-----------|-----------|--------|
| 287 | `"prompt"` | `model`（请求中的模型名） | `usage.Usage.PromptTokens` — 从上游响应 JSON 的 `usage.prompt_tokens` 解析 |
| 288 | `"completion"` | `model` | `usage.Usage.CompletionTokens` — 从上游响应 JSON 的 `usage.completion_tokens` 解析 |

调用时机：Settle 结算完成后（无论 Settle 是否报错都记录，因为 usage 数据来自上游，Settle 错误是计费侧问题）。

**函数：`relayStreamRequest`（流式 ChatCompletion）** — 2 处

| 行号 | type 标签 | model 标签 | 值来源 |
|------|-----------|-----------|--------|
| 503 | `"prompt"` | `model` | `tokenCounter.InputTokens()` — 若上游发送了 `stream_options.include_usage=true` 的最终 chunk 则取上游值，否则由 `tokenutil.EstimateRequestTokens` 从 messages 估算 |
| 504 | `"completion"` | `model` | `tokenCounter.OutputTokens()` — 若上游发送了 usage 则取上游值，否则从流式 delta content 估算 |

调用时机：流读取结束后、Settle 结算完成后。`usageSource` 字段（`"api"` vs `"stream_estimate"`）决定了 token 数据的可信度。

**函数：`HandleEmbeddings`（`/v1/embeddings`）** — 1 处

| 行号 | type 标签 | model 标签 | 值来源 |
|------|-----------|-----------|--------|
| 739 | `"prompt"` | `req.Model`（请求中的模型名） | `usage.Usage.PromptTokens` — 从上游响应 JSON 的 `usage.prompt_tokens` 解析 |

Embeddings 请求只有 input token，无 completion token，因此只记录 `"prompt"` 类型。

#### 3.2.3 `RecordUpstreamLatency` — 4 处调用

`RecordUpstreamLatency` 记录从发送上游请求到收到响应的耗时。使用 `time.Duration` 直接传入，函数内部调用 `.Seconds()` 转换后传给 Prometheus Histogram。

**函数：`HandleChatCompletion`（非流式上游请求）** — 1 处

| 行号 | provider 标签 | model 标签 | 计时范围 |
|------|--------------|-----------|---------|
| 208 | `route.Channel.Provider`（渠道供应商标识） | `req.Model` | `client.Do(upstreamReq)` 前后 — 从发送请求到收到完整响应头+体 |

此调用位于路由重试循环内部，每次重试（不同渠道）都会独立记录一次上游延迟。即使请求失败（err != nil 或非 200），也会记录延迟，因为延迟数据本身就是有价值的——慢的失败也是慢。

**函数：`relayStreamRequest`（流式上游请求）** — 1 处

| 行号 | provider 标签 | model 标签 | 计时范围 |
|------|--------------|-----------|---------|
| 508 | `route.Channel.Provider` | `model` | `streamStart` 到流读取结束（`time.Since(streamStart)`） |

此调用记录的是整个流式请求的端到端耗时（从开始流读取到所有 chunk 处理完毕），而非单个 chunk 的延迟。`streamStart` 在流读取循环开始前设置（约第 400 行）。

**函数：`HandleEmbeddings`（Embeddings 上游请求）** — 1 处

| 行号 | provider 标签 | model 标签 | 计时范围 |
|------|--------------|-----------|---------|
| 712 | `route.Channel.Provider` | `req.Model` | `client.Do(upstreamReq)` 前后 — 从发送请求到收到完整响应 |

与 ChatCompletion 非流式路径结构相同，但 Embeddings 路径无重试机制，只调用一次。

> **标签值一致性说明**：`provider` 标签来自 `route.Channel.Provider`（数据库 channels 表的 provider 字段），`model` 标签来自请求体中的原始模型名（`req.Model`），而非渠道映射后的模型名（`route.MappedModel`）。这确保同一模型在不同供应商上的延迟可以对比。

---

## 四、端点配置

### 4.1 暴露端点

```
GET /metrics
```

返回 Prometheus 文本格式的指标数据。PRD3 Batch 13 起，该端点的暴露策略由运行时配置控制：

| 配置 | 默认 | 说明 |
|------|------|------|
| `METRICS_ENABLED` | `true` | 为兼容现有 Prometheus 栈默认开启；设置为 `false` 时 `/metrics` 返回 404 |
| `METRICS_ALLOWED_CIDRS` | 空 | 为空时不做应用层 CIDR 限制；设置后仅允许 `RemoteAddr` 落在这些 CIDR 内的请求访问 `/metrics` |

生产环境不应把 backend `:8080/metrics` 直接暴露到公网。推荐做法是通过安全组、防火墙或反向代理限制到 Prometheus 所在网络；如果部署形态允许，也可以设置 `METRICS_ALLOWED_CIDRS` 或关闭 `METRICS_ENABLED`。

### 4.2 中间件链顺序

```
CORS → Metrics → Admission → Router
```

**注意事项**：
- Metrics 中间件在 Admission 之前，确保被拒绝的请求也被记录
- `/metrics` 端点本身不经过业务中间件链

### 4.3 安全考虑

生产环境中 `/metrics` 端点建议：
- 仅内网访问（通过防火墙、反向代理或 `METRICS_ALLOWED_CIDRS` 限制）
- 不需要应用内指标时设置 `METRICS_ENABLED=false`
- 如果需要 Basic Auth 或更复杂的代理认证，应作为部署策略或 PRD4 运维能力设计，不在 PRD3 中临时扩展

---

## 五、Grafana 面板建议

### 5.1 请求速率面板

```promql
sum by (method, status) (rate(unirouter_http_requests_total[$__rate_interval]))
```

### 5.2 延迟分布面板

```promql
histogram_quantile(0.95, sum by (le) (rate(unirouter_http_request_duration_seconds_bucket[$__rate_interval])))
```

### 5.3 错误率面板

```promql
100 * sum(rate(unirouter_http_requests_total{status=~"5.."}[$__rate_interval])) / clamp_min(sum(rate(unirouter_http_requests_total[$__rate_interval])), 1e-9)
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

## 六、基础设施文件

项目提供完整的 Prometheus + Grafana 监控栈配置：

| 文件 | 用途 |
|------|------|
| `infra/prometheus/prometheus.yml` | Prometheus 抓取配置，5s 间隔采集 backend:8080/metrics |
| `infra/grafana/provisioning/datasources/datasource.yml` | 自动注册 Prometheus 数据源 |
| `infra/grafana/provisioning/dashboards/dashboards.yml` | Dashboard 文件夹 provisioning |
| `infra/grafana/dashboards/unirouter-overview.json` | 预置 8 面板 Grafana Dashboard |
| `infra/loadtest/k6-chat-completions.js` | k6 压测脚本（梯度爬坡 + 突发尖峰） |

```bash
# 启动监控栈（需要 Docker）
docker compose up -d prometheus grafana

# Grafana: http://localhost:3001  $GRAFANA_ADMIN_USER / $GRAFANA_ADMIN_PASSWORD
# Prometheus: http://localhost:9090
```

### 6.1 Dashboard 导入导出

Dashboard JSON 可通过 Grafana UI 的 Import 功能导入，也可通过 API 导出：

```bash
# 导出 Dashboard
curl -s http://admin:admin@localhost:3001/api/dashboards/uid/unirouter-overview | jq '.dashboard' > dashboard.json

# 导入 Dashboard
curl -X POST http://admin:admin@localhost:3001/api/dashboards/db \
  -H "Content-Type: application/json" \
  -d "{\"dashboard\": $(cat infra/grafana/dashboards/unirouter-overview.json), \"overwrite\": true}"
```

### 6.2 k6 压测

```bash
# 安装 k6
brew install k6

# 运行压测
k6 run infra/loadtest/k6-chat-completions.js

# 自定义目标地址和 API Key
BASE_URL=http://your-server:8080 API_KEY=your-key k6 run infra/loadtest/k6-chat-completions.js
```

脚本包含两个 scenario：
- **sustained_load**：0→10 VU 梯度爬坡，维持 2 分钟
- **spike_test**：正常 5 req/s → 突增 50 req/s → 恢复，测试熔断和指标在压力下的表现

---

## 七、扩展方向

| 方向 | 说明 |
|------|------|
| 按 org 聚合 | 在 counter 标签中添加 `org_id` |
| 计费金额指标 | 新增 `unirouter_billing_amount_total` counter |
| 渠道健康指标 | 新增 `unirouter_channel_health` gauge |
| 队列深度 | 新增 `unirouter_queue_depth` gauge |
