# UniRouter 技术参考

> 面向开发团队的实现参考文档，按阅读顺序编号。

## 文档列表

| # | 文档 | 说明 | 版本 |
|---|------|------|------|
| 01 | [模型能力矩阵](01-provider-capability-matrix.md) | 6 家模型厂商能力对比 | v2.0 |
| 02 | [计费状态机](02-billing-state-machine.md) | 计费流程完整定义 | v2.0 |
| 03 | [错误处理矩阵](03-error-handling-matrix.md) | 错误分类 + 重试策略 | v2.0 |
| 04 | [OneAPI Hook 清单](04-oneapi-hook-manifest.md) | 上游 Hook 分析 | — |
| 05 | [OpenAPI Spec](05-openapi-spec.yaml) | 当前 PRD1 API 路径快照，按 `cmd/uniroute/main.go` 和 `route_parity_test.go` 对齐 | v1.1 |
| 06 | [前端页面规格](06-frontend-spec.md) | 页面布局 + 交互规格 | — |
| 07 | [结构化日志](07-structured-logging.md) | 日志架构 + 使用方式 | v1.0 |
| 08 | [监控指标](08-prometheus-metrics.md) | Prometheus 指标体系 | v1.0 |
| 09 | [准入控制](09-admission-control.md) | 并发限流架构 | v1.0 |

## 关联文档
- [PRD Phase 1](../PRD-Phase1.md) — 产品需求文档
- [运维文档](../ops/) — 部署和测试

## 基础设施配置

| 文件 | 说明 |
|------|------|
| `infra/prometheus/prometheus.yml` | Prometheus 抓取配置 |
| `infra/grafana/provisioning/` | Grafana datasource + dashboard provisioning |
| `infra/grafana/dashboards/unirouter-overview.json` | 预置 Grafana Dashboard（8 面板） |
| `infra/loadtest/k6-chat-completions.js` | k6 压测脚本 |

启动监控栈：`docker compose up -d prometheus grafana`
