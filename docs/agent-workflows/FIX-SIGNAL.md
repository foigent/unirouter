# FIX-SIGNAL.md — PRD1 Dashboard Trend / Grafana Observability Fix 验收请求

**修复者：** Codex
**日期：** 2026-07-01
**修复轮次：** PRD1 Dashboard trend UX, Grafana datasource/PromQL, and Grafana admin hardening

---

## Carry-over Scope

`docs/issues/OPEN-ISSUES.md` 中 `PRD1-DASH-002`（平均延迟一直为 0）和 `PRD1-PRICE-001`（小额模型价格被四舍五入为 0）仍处于 `Fixed awaiting Canary`。本文件继续把这两项纳入 Canary 验收范围，避免前一轮 FIX-SIGNAL 被本轮 Dashboard/Grafana 信号覆盖后产生协议断档。

Carry-over 修复点：

- `internal/gateway/relay.go` 已在 call log 写入真实 `latency_ms/ttft_ms`。
- `model_prices.input_price_cents/output_price_cents` 已迁移为 decimal cents，后端/计费/前端保留小数分价格。

Carry-over targeted tests 本轮重新执行：

```bash
go test ./internal/gateway -run 'TestRecordRequest_Success|TestRelayNonStreamRequest|TestRelayStreamRequest' -v
go test ./internal/admin -run 'TestCreateModelPrice_DecimalCents|TestListModelPrices_Success|TestDashboardStats_Success' -v
go test ./internal/billing -run 'TestCalculateCost_FractionalCentPrice|TestQueryModelPrice|TestCalculateCost' -v
```

结果：全部 PASS。

---

## 本轮背景

用户在 PRD1 云端验收后继续报告：

- `Dashboard -> AI 活动趋势` 有数据的点看起来一直停在最右侧，没有清楚体现滚动时间窗口。
- `费用 / 请求数` 用互斥按钮切换不合理，应该允许同时展示两条曲线。
- 需要检查 Grafana `http://8.130.18.85:3001/` 和 Prometheus `http://8.130.18.85:9090/` 是否存在问题。

Codex 云端核查结论：

- Dashboard stats API 的 24h/7d/30d 趋势桶是按范围变化的；24h 的非零桶集中在最近两个小时，7d/30d 的非零桶集中在实际有调用的日期。后端范围聚合不是这次主要缺陷。
- 前端图表只允许单指标、单 Y 轴展示，导致近期桶视觉上像“永远在右侧”，也无法对照成本和请求量。
- Prometheus 健康：`/-/ready`、`/-/healthy` 均 200；`unirouter` target `backend:8080/metrics` 为 `up`；UniRouter 指标存在。
- Grafana 健康接口可用，但当前 datasource UID 是 Grafana 自动生成值，而 Dashboard JSON 固定引用 `uid: prometheus`，面板可能显示 datasource not found。
- 当前云端 Grafana 仍接受 `admin/admin` 并显示改密码提示，这是独立于 UniRouter seeded admin 的 Grafana 部署安全问题。

---

## Fix #1: Dashboard AI 活动趋势支持双指标叠加

**文件：**

- `frontend/src/pages/DashboardPage.tsx`
- `frontend/src/index.css`
- `frontend/src/pages/__tests__/DashboardPage.test.tsx`

**修复内容：**

- 把 `请求数 / 费用` 从互斥按钮改为可选复选框。
- 默认同时显示 `请求数` 和 `费用`。
- 增加双 Y 轴：左轴为请求数，右轴为费用（元）。
- tooltip 同时展示当前 bucket 的请求数和费用。
- 防止两个指标都被取消，避免空图误导。
- 增加 Dashboard 单测覆盖趋势指标复选框。

**预期效果：**

- 用户可以同时看到请求数曲线和费用曲线。
- 最近 bucket 仍会出现在时间窗口右侧，但用户可以通过完整窗口和双指标对照判断趋势，而不是被单指标切换误导。

---

## Fix #2: Grafana datasource UID 与 Dashboard JSON 对齐

**文件：**

- `infra/grafana/provisioning/datasources/datasource.yml`
- `infra/grafana/dashboards/unirouter-overview.json`
- `docs/ops/PRD1-REMAINING-ACCEPTANCE-PACK.md`
- `docs/ops/PRD-ACCEPTANCE-MATRIX.md`
- `docs/ops/CURRENT-STATUS.md`
- `docs/issues/OPEN-ISSUES.md`

**修复内容：**

- Prometheus datasource provisioning 显式设置 `uid: prometheus`。
- Dashboard JSON 继续引用 `uid: prometheus`，避免 Grafana 自动生成 UID 后面板找不到 datasource。
- Dashboard PromQL 改成标准聚合写法：
  - QPS 使用 `sum by (method, status)`.
  - histogram_quantile 查询使用 `sum by (le)` 或 `sum by (le, provider, model)`。
  - rate 窗口使用 Grafana `$__rate_interval`。
  - Error Rate 使用 `clamp_min` 避免除零。
- PRD1 验收矩阵从“Grafana login reachable”升级为必须确认 datasource 和面板数据。

**预期效果：**

- 新部署的 Grafana Dashboard 面板能够解析 Prometheus datasource。
- Request rate、latency、error rate 等核心面板不再因为 UID 不匹配或错误 histogram 查询而失效。

---

## Fix #3: Grafana admin 密码改为部署环境变量

**文件：**

- `docker-compose.yml`
- `.env.example`
- `README.md`
- `docs/ops/deployment-runbook.md`
- `docs/tech-ref/08-prometheus-metrics.md`
- `docs/issues/OPEN-ISSUES.md`

**修复内容：**

- `docker-compose.yml` 不再硬编码 `GF_SECURITY_ADMIN_PASSWORD: admin`。
- 新增 `GRAFANA_ADMIN_USER` / `GRAFANA_ADMIN_PASSWORD` 环境变量。
- `GRAFANA_ADMIN_PASSWORD` 设为 Compose 必填变量。
- README、部署 runbook 和监控技术参考明确要求设置 Grafana 强密码。

**预期效果：**

- 新部署不会静默使用 `admin/admin`。
- 现有云端 Grafana 仍需要 Atlas 设置 `.env` 并轮换持久化实例中的旧密码。

---

## Codex 已执行验证

```bash
go test ./internal/gateway -run 'TestRecordRequest_Success|TestRelayNonStreamRequest|TestRelayStreamRequest' -v
go test ./internal/admin -run 'TestCreateModelPrice_DecimalCents|TestListModelPrices_Success|TestDashboardStats_Success' -v
go test ./internal/billing -run 'TestCalculateCost_FractionalCentPrice|TestQueryModelPrice|TestCalculateCost' -v
cd frontend && npm test -- --run src/pages/__tests__/DashboardPage.test.tsx
cd frontend && npm test -- --run
cd frontend && npm run build
node -e "JSON.parse(require('fs').readFileSync('infra/grafana/dashboards/unirouter-overview.json','utf8')); console.log('grafana dashboard json ok')"
git diff --check
```

结果：

- Dashboard targeted frontend tests: PASS, 9/9 tests.
- Gateway/admin/billing targeted Go tests for carry-over latency/pricing fixes: PASS.
- Full frontend tests: PASS, 38/38 tests.
- Frontend build: PASS, retained existing Vite large chunk warning.
- Grafana dashboard JSON parse: PASS.
- `git diff --check`: PASS.
- Codex local Playwright visual smoke: PASS, generated `output/playwright/prd1-dashboard-trend-dual-metric.png` with 2 trend checkboxes and rendered chart SVGs using mocked Dashboard data.

Cloud Prometheus query validation:

```bash
sum by (method, status) (rate(unirouter_http_requests_total[5m]))
histogram_quantile(0.99, sum by (le) (rate(unirouter_http_request_duration_seconds_bucket[5m])))
100 * sum(rate(unirouter_http_requests_total{status=~"5.."}[5m])) / clamp_min(sum(rate(unirouter_http_requests_total[5m])), 1e-9)
histogram_quantile(0.95, sum by (le, provider, model) (rate(unirouter_upstream_latency_seconds_bucket[5m])))
sum by (type, model) (rate(unirouter_tokens_processed_total[5m]))
sum by (status) (rate(unirouter_billing_freeze_total[5m]))
sum by (le) (rate(unirouter_http_request_duration_seconds_bucket[5m]))
```

结果：全部通过 Prometheus API，返回 `status=success`。

未执行：

- `docker compose config` 未运行成功，因为当前工作机没有 `docker` 命令。
- Grafana 面板视觉验收未执行；Canary 不具备视觉能力，部署后需要 Atlas 或浏览器能力方确认。

---

## Canary 验收命令

```bash
cd /Users/m4/Desktop/projects/UniRouter
git diff --check
go test ./internal/gateway -run 'TestRecordRequest_Success|TestRelayNonStreamRequest|TestRelayStreamRequest' -v
go test ./internal/admin -run 'TestCreateModelPrice_DecimalCents|TestListModelPrices_Success|TestDashboardStats_Success' -v
go test ./internal/billing -run 'TestCalculateCost_FractionalCentPrice|TestQueryModelPrice|TestCalculateCost' -v
(cd frontend && npm test -- --run src/pages/__tests__/DashboardPage.test.tsx)
(cd frontend && npm test -- --run)
(cd frontend && npm run build)
node -e "JSON.parse(require('fs').readFileSync('infra/grafana/dashboards/unirouter-overview.json','utf8')); console.log('grafana dashboard json ok')"
```

Source-level checks:

```bash
rg -n "visibleTrendMetrics|ur-trend-checklist|cost_yuan|显示.*请求数|显示.*费用" frontend/src/pages/DashboardPage.tsx frontend/src/index.css frontend/src/pages/__tests__/DashboardPage.test.tsx
rg -n "uid: prometheus" infra/grafana/provisioning/datasources/datasource.yml
rg -n "\\$__rate_interval|sum by \\(le\\)|clamp_min|sum by \\(method, status\\)" infra/grafana/dashboards/unirouter-overview.json docs/tech-ref/08-prometheus-metrics.md
rg -n "GRAFANA_ADMIN_PASSWORD|GF_SECURITY_ADMIN_PASSWORD" docker-compose.yml .env.example README.md docs/ops/deployment-runbook.md docs/tech-ref/08-prometheus-metrics.md
rg -n "PRD1-DASH-003|PRD1-OBS-001|PRD1-SEC-003" docs/issues/OPEN-ISSUES.md docs/ops/CURRENT-STATUS.md docs/ops/PRD-ACCEPTANCE-MATRIX.md docs/ops/PRD1-REMAINING-ACCEPTANCE-PACK.md
```

Expected:

- All commands pass.
- Canary should mark visual/browser checks as not applicable.
- If Canary passes, Sentinel can proceed with PR/CI.
- Atlas must set `GRAFANA_ADMIN_PASSWORD` before redeploying Grafana and verify the running Grafana datasource/panels after deployment.
