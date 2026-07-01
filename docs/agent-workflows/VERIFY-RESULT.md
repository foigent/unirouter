# VERIFY-RESULT.md — PRD1 Latency / Decimal Pricing Fix 验收结果

**验收者：** OpenClaw Canary
**日期：** 2026-07-01
**对应轮次：** PRD1 dashboard latency persistence, decimal model pricing, and Mimo provider recheck

---

## 逐条结果

- **Fix #1 (调用日志写入真实 latency_ms / ttft_ms):** PASS — `relay.go:69` 定义 `elapsedMilliseconds` helper，`:94` 记录 `requestStart`，`:618` INSERT 包含 `latency_ms, ttft_ms` 列。非流式（`:324`）、流式（`:557`）、无路由（`:186`）、上游失败（`:277`）、embeddings（`:797`）均传入 `elapsedMilliseconds(requestStart)`。`TestRecordRequest_Success` PASS
- **Fix #2 (模型价格支持小数分):** PASS — `migrations/000008_model_price_decimal_cents.up.sql:3-4` 迁移 `input_price_cents/output_price_cents` 为 `NUMERIC(18,6)`。`models.go:90` `OutputPriceCents float64`，`model_handlers.go:156` `OutputPriceCents float64`，`:122` 使用 `::float8` cast。`dashboard_handlers.go:112` 使用 `::float8`。`billing.go:73/80` `QueryModelPrice/QueryModelPriceForOrg` 返回 `float64`。`ModelPricesPage.tsx:7` `yuanPerKToCents` 保留小数，`:147-148` placeholder `0.002`/`0.001`。`ModelPricesPage.test.tsx:6-7` 验证 `yuanPerKToCents('0.002') === 0.2`。`TestCreateModelPrice_DecimalCents` 和 `TestCalculateCost_FractionalCentPrice` 均 PASS
- **Runtime Evidence (Mimo provider key 恢复):** PASS — `OPEN-ISSUES.md:35` `PRD1-PROVIDER-001` 标记为 "Closed"，记录云端 200 响应和临时 key 已禁用；`:37` `PRD1-LOAD-001` 保持 "Open"（未误关）。`CURRENT-STATUS.md:8` 和 `:23` 记录 Mimo 云端验证。`PRD-ACCEPTANCE-MATRIX.md:12` 记录 E2E accepted

## 全局验证

| 检查项 | 结果 |
|--------|------|
| `git diff --check` | PASS |
| gateway targeted tests（9/9：RecordRequest + NonStream + Stream） | PASS |
| admin targeted tests（3/3：DecimalCents + ListModelPrices + DashboardStats） | PASS |
| billing targeted tests（15/15：CalculateCost + QueryModelPrice） | PASS |
| `go test ./...` | PASS（16/16 包） |
| `npm test -- --run` | PASS（37/37 测试，6 文件） |
| `npm run build` | PASS（3.93s） |
| `NUMERIC(18,6)` migration | PASS |
| `yuanPerKToCents('0.002') === 0.2` 测试 | PASS |
| PRD1-PROVIDER-001 关闭 + PRD1-LOAD-001 未误关 | PASS |

## 新发现的问题

无

## 结论

**2/2 Fix + Runtime Evidence 全部 PASS。** PRD1 latency / decimal pricing fix 验收通过。前端测试从 36 增至 37（新增 ModelPricesPage 测试）。
