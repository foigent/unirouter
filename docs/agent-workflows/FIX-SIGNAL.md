# FIX-SIGNAL.md — PRD1 Latency / Decimal Pricing Fix 验收请求

**修复者：** Codex
**日期：** 2026-07-01
**修复轮次：** PRD1 dashboard latency persistence, decimal model pricing, and Mimo provider recheck

---

## 本轮背景

用户在云端 PRD1 验收时报告：

- 仪表盘平均延迟显示为 `0ms`。
- 模型价格配置中把 `mimo-v2.5` 设置为输入 `0.002` 元/千 token、输出 `0.001` 元/千 token 后，列表显示价格为 0。
- `Mimo Token Plan` 上游 key 已更新，需要复核 provider 是否仍是阻断项。

云端核查结论：

- `/api/v1/dashboard/stats` 返回 `avg_latency_ms=0`，最近调用日志 `latency_ms=0`，根因是网关 `recordRequest` 插入 `call_logs` 时没有写入 `latency_ms/ttft_ms`，数据库默认值一直是 0。
- `/api/v1/model-prices` 中 `mimo-v2.5` 已被保存为 `input_price_cents=0/output_price_cents=0`，根因是前端将“元/千 token”乘以 100 后 `Math.round` 到整数分，`0.002` 元会变成 `0.2` 分再被四舍五入为 0；后端和数据库字段也按整数处理。
- 更新后的 channel 8 `Mimo Token Plan` key 已通过云端真实测试：后台渠道测试 `mimo-v2.5` 返回 HTTP 200；临时 UniRouter API key 网关调用 `mimo-v2.5` 返回 HTTP 200，`_uniroute.channel=Mimo Token Plan`，扣费 1 分，临时 key 已禁用。

---

## Fix #1: 调用日志写入真实 latency_ms / ttft_ms

**文件：**

- `internal/gateway/relay.go`
- `internal/gateway/relay_mock_test.go`

**修复内容：**

- `HandleChatCompletion` 和 `HandleEmbeddings` 记录 `requestStart`。
- `recordRequest` 新增 `latencyMs` 和 `ttftMs` 参数。
- `call_logs` insert 显式写入 `latency_ms` 和 `ttft_ms`。
- 非流式、流式、无路由、上游失败、embeddings 成功/无路由路径传入真实耗时。
- 流式路径继续写入已有 TTFT。
- `TestRecordRequest_Success` 断言 insert 参数包含 `latency_ms`。

**预期效果：**

- 新部署后的新调用日志不再全部是 `latency_ms=0`。
- Dashboard `avg_latency_ms` 对新产生的调用会逐步显示真实平均延迟。
- 历史旧日志仍然是 0，这是历史数据事实，不应通过代码回填伪造。

---

## Fix #2: 模型价格支持小数分，保留小额价格

**文件：**

- `migrations/000001_init_schema.up.sql`
- `migrations/000008_model_price_decimal_cents.up.sql`
- `migrations/000008_model_price_decimal_cents.down.sql`
- `internal/models/models.go`
- `internal/admin/model_handlers.go`
- `internal/admin/dashboard_handlers.go`
- `internal/billing/price_provider.go`
- `internal/billing/billing.go`
- `internal/testutil/testdb.go`
- `frontend/src/pages/ModelPricesPage.tsx`
- `frontend/src/pages/__tests__/ModelPricesPage.test.tsx`
- related Go tests under `internal/admin`, `internal/billing`, `internal/gateway`

**修复内容：**

- `model_prices.input_price_cents/output_price_cents` 从 integer 迁移到 `NUMERIC(18,6)`。
- 后端模型价格 list/create/update 使用 `float64`，并拒绝负价格。
- Dashboard model price summary 使用 float scan。
- Billing price provider 和 org multiplier 查询使用小数价格参与计算，最终费用仍按整数分结算。
- 前端模型价格页把“元/千 token”转换成“分/千 token”时保留 6 位小数，不再 `Math.round` 到整数。
- 新增 `yuanPerKToCents('0.002') === 0.2` 前端测试。
- 新增 `TestCreateModelPrice_DecimalCents` 和 `TestCalculateCost_FractionalCentPrice`。

**预期效果：**

- 用户输入 `0.002` 元/千 token 后，API 应保存 `input_price_cents=0.2`。
- 表格用 `MoneyText` 显示为 `¥0.0020`，不再显示为 `¥0.0000`。
- 计费计算不会把小额模型价格退化成 0。

---

## Runtime Evidence: Mimo provider key 已恢复

**云端验证：**

- Channel 8 `Mimo Token Plan` key prefix changed to `****df24`，`fail_count=0`，`cooldown_until=null`。
- `POST /api/v1/channels/8/test` with `model=mimo-v2.5`: PASS, HTTP 200, `latency_ms=1385`, `tokens_used=267`。
- 临时 API key `id=30` 网关调用 `POST /v1/chat/completions` with `model=mimo-v2.5`: PASS, HTTP 200, `_uniroute.channel=Mimo Token Plan`, `_uniroute.cost.charged=1`。
- 临时 API key `id=30` 已禁用，disable API returned HTTP 200。

**结论：**

- `PRD1-PROVIDER-001` 的“上游 key 无效”阻断可关闭。
- `PRD1-LOAD-001` 仍是独立容量/压测问题，不因单次 provider E2E 成功自动关闭。

---

## Codex 已执行验证

```bash
go test ./internal/billing ./internal/admin ./internal/gateway -run 'TestCalculateCost_FractionalCentPrice|TestCalculateCost_Normal|TestQueryModelPrice|TestCreateModelPrice_DecimalCents|TestListModelPrices_Success|TestDashboardStats_Success|TestRecordRequest_Success|TestRelayNonStreamRequest|TestRelayStreamRequest' -v
go test ./...
cd frontend && npm test -- --run
cd frontend && npm run build
```

结果：

- targeted Go tests: PASS
- `go test ./...`: PASS
- `npm test -- --run`: PASS, 37/37 tests
- `npm run build`: PASS，保留既有 Vite large chunk warning

---

## Canary 验收命令

```bash
cd /Users/m4/Desktop/projects/UniRouter
git diff --check
go test ./internal/gateway -run 'TestRecordRequest_Success|TestRelayNonStreamRequest|TestRelayStreamRequest' -v
go test ./internal/admin -run 'TestCreateModelPrice_DecimalCents|TestListModelPrices_Success|TestDashboardStats_Success' -v
go test ./internal/billing -run 'TestCalculateCost_FractionalCentPrice|TestQueryModelPrice|TestCalculateCost' -v
go test ./...
cd frontend && npm test -- --run
cd frontend && npm run build
```

Source-level checks:

```bash
rg -n "latency_ms, ttft_ms|elapsedMilliseconds|requestStart|TestRecordRequest_Success" internal/gateway/relay.go internal/gateway/relay_mock_test.go
rg -n "NUMERIC\\(18,6\\)|model_price_decimal_cents" migrations
rg -n "input_price_cents::float8|OutputPriceCents float64|TestCreateModelPrice_DecimalCents" internal/admin internal/models
rg -n "QueryModelPrice.*float64|FractionalCentPrice|QueryModelPriceForOrg.*float64" internal/billing internal/gateway
rg -n "yuanPerKToCents|0.002|0.001" frontend/src/pages/ModelPricesPage.tsx frontend/src/pages/__tests__/ModelPricesPage.test.tsx
```

Do not run destructive cleanup. Do not run real provider paid calls, wallet/channel/API-key mutations, pressure tests, stream disconnects, or fault injection in Canary.

---

## 期望输出格式

请将验收结果写入 `docs/agent-workflows/VERIFY-RESULT.md`：

```markdown
# VERIFY-RESULT.md — PRD1 Latency / Decimal Pricing Fix 验收结果

**验收者：** OpenClaw Canary
**日期：** 以文件系统时间戳为准
**对应轮次：** PRD1 dashboard latency persistence, decimal model pricing, and Mimo provider recheck

## 逐条结果

- Fix #1: PASS/FAIL — 网关是否写入 call_logs.latency_ms/ttft_ms，且覆盖非流式、流式、失败路径
- Fix #2: PASS/FAIL — 模型价格是否支持小数分，前端 0.002 元是否转换为 0.2 分
- Runtime Evidence: PASS/FAIL — 文档是否记录 Mimo provider key 云端恢复证据，且不误关 PRD1-LOAD-001

## 全局验证

- `git diff --check`: PASS/FAIL
- targeted Go tests: PASS/FAIL
- `go test ./...`: PASS/FAIL
- `npm test -- --run`: PASS/FAIL
- `npm run build`: PASS/FAIL

## 新发现的问题

- [如有，在此列出；没有则写“无”]

## 结论

- [全部通过 / 需要 Codex 修复以下问题]
```
