# VERIFY-RESULT.md — PRD1 Dashboard Trend / Grafana Observability Fix 验收结果

**验收者：** OpenClaw Canary
**日期：** 2026-07-01
**对应轮次：** PRD1 Dashboard trend UX, Grafana datasource/PromQL, and Grafana admin hardening

---

## Carry-over 验证

- **latency_ms/ttft_ms 持久化**: PASS — `TestRecordRequest_Success` + `TestRelayNonStreamRequest` + `TestRelayStreamRequest` 9/9 PASS
- **decimal model pricing**: PASS — `TestCreateModelPrice_DecimalCents` + `TestListModelPrices_Success` + `TestDashboardStats_Success` 3/3 PASS；`TestCalculateCost_FractionalCentPrice` + `TestQueryModelPrice` + `TestCalculateCost` 15/15 PASS

---

## 逐条结果

- **Fix #1 (Dashboard AI 活动趋势支持双指标叠加):** PASS — `DashboardPage.tsx:33` 定义 `TrendMetric = 'requests' | 'cost_yuan'`，`:147` `visibleTrendMetrics` state 默认 `['requests', 'cost_yuan']`，`:341` 使用 `ur-trend-checklist` 复选框（非互斥按钮），`:380/404` 双 Y 轴（`yAxisId='cost_yuan'`），`:134` tooltip 同时展示请求数和费用。`index.css:424` 定义 `.ur-trend-checklist`。`DashboardPage.test.tsx:140` 测试 checkbox 元素存在。Dashboard 测试 9/9 PASS
- **Fix #2 (Grafana datasource UID 对齐 + PromQL 修正):** PASS — `datasource.yml:5` 显式 `uid: prometheus`。`unirouter-overview.json` 全部使用 `$__rate_interval`（`:16/35/40/45/87/112/131/150/169`），`:87` 使用 `clamp_min` 避免除零，`:16/35` 使用 `sum by (method, status)` 和 `sum by (le)` 标准聚合。`08-prometheus-metrics.md` 同步更新 PromQL。Dashboard JSON parse PASS
- **Fix #3 (Grafana admin 密码改为部署环境变量):** PASS — `docker-compose.yml:94` 使用 `${GRAFANA_ADMIN_PASSWORD:?Set GRAFANA_ADMIN_PASSWORD in .env before starting Grafana}`（必填），`.env.example:34` `GRAFANA_ADMIN_PASSWORD=CHANGE_ME_GRAFANA_ADMIN_PASSWORD`，`README.md:13` 提示修改，`deployment-runbook.md:34/40/230/529` 记录密码设置要求，`08-prometheus-metrics.md:250` 使用 `$GRAFANA_ADMIN_USER / $GRAFANA_ADMIN_PASSWORD`。`OPEN-ISSUES.md:45` `PRD1-SEC-003` 记录为 Fixed awaiting Canary

## 全局验证

| 检查项 | 结果 |
|--------|------|
| `git diff --check` | PASS |
| carry-over gateway tests（9/9） | PASS |
| carry-over admin tests（3/3） | PASS |
| carry-over billing tests（15/15） | PASS |
| Dashboard targeted frontend tests（9/9） | PASS |
| `go test ./...` | PASS（16/16 包） |
| `npm test -- --run` | PASS（38/38 测试，6 文件） |
| `npm run build` | PASS（4.37s） |
| Grafana dashboard JSON parse | PASS |
| `uid: prometheus` datasource | PASS |
| `$__rate_interval` + `clamp_min` PromQL | PASS |
| `GRAFANA_ADMIN_PASSWORD` 必填变量 | PASS |
| 浏览器/Grafana 视觉验收 | NOT_APPLICABLE_TO_CANARY |

## 新发现的问题

无

## 部署注意事项

- Atlas 重部署 Grafana 前必须在 `.env` 设置 `GRAFANA_ADMIN_PASSWORD` 强密码
- 云端 Grafana 旧实例仍使用 `admin/admin`，需手动轮换密码
- Dashboard 双指标趋势图的视觉效果需用户/浏览器确认

## 结论

**3/3 Fix + carry-over 全部 PASS。** PRD1 Dashboard trend / Grafana observability fix 验收通过。前端测试从 37 增至 38（Dashboard 从 8 增至 9）。
