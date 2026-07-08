# UniRouter 技术参考

> 面向开发团队的实现参考文档，按阅读顺序编号。

## 文档列表

| # | 文档 | 说明 | 版本 |
|---|------|------|------|
| 01 | [模型能力矩阵](01-provider-capability-matrix.md) | 6 家模型厂商能力对比 | v2.0 |
| 02 | [计费状态机](02-billing-state-machine.md) | 计费流程完整定义 | v2.0 |
| 03 | [错误处理矩阵](03-error-handling-matrix.md) | 错误分类 + 重试策略 | v2.0 |
| 04 | [OneAPI Hook 清单](04-oneapi-hook-manifest.md) | 上游 Hook 分析 | — |
| 05 | [OpenAPI Spec](05-openapi-spec.yaml) | 当前 UniRouter API 路径合同，按 `cmd/uniroute/main.go`、前端 `/docs` 和 `route_parity_test.go` 对齐 | v1.2 |
| 06 | [前端页面规格](06-frontend-spec.md) | 页面布局 + 交互规格 | — |
| 07 | [结构化日志](07-structured-logging.md) | 日志架构 + 使用方式 | v1.0 |
| 08 | [监控指标](08-prometheus-metrics.md) | Prometheus 指标体系 | v1.0 |
| 09 | [准入控制](09-admission-control.md) | 并发限流架构 | v1.0 |
| 10 | [Provider Adapter Framework](10-provider-adapter-framework.md) | PRD2 上游适配器框架 | v0.1 |
| 11 | [Project And RBAC Model](11-project-rbac-model.md) | PRD2 Project/RBAC 数据与权限模型 | v0.1 |
| 12 | [Token Metering V2](12-token-metering-v2.md) | PRD2 token metering source and reconciliation contract | v0.1 |
| 13 | [Audit Schema Upgrade](13-audit-schema-upgrade.md) | PRD2 audit source, filter, and before/after contract | v0.1 |
| 14 | [Notification And Event Foundation](14-notification-event-foundation.md) | PRD2 notification events, webhook routing, and project budget threshold contract | v0.1 |
| 15 | [HA Readiness](15-ha-readiness.md) | PRD2 multi-instance, shared-state, smoke, and rollback contract | v0.1 |
| 16 | [License Foundation](16-license-foundation.md) | PRD2 offline license document, signature verification, status, and audit contract | v0.1 |
| 17 | [Cache Research](17-cache-research.md) | PRD2 exact/semantic cache research boundary; no runtime cache by default | v0.1 |
| 18 | [OIDC First SSO](18-oidc-sso.md) | PRD2 OIDC login, claim mapping, provisioning, and audit contract | v0.1 |
| 19 | [Prompt And Response Storage Policy](19-prompt-response-storage-policy.md) | PRD2 P2-F11 policy: plaintext disabled by default; future opt-in controls | v0.1 |
| 20 | [PRD3 Product Glossary](20-prd3-product-glossary.md) | PRD3 canonical terminology for provider/channel, usage analytics, notification status, roles, and PRD4 boundaries | v0.1 |
| 21 | [PRD4 Quota Vocabulary](21-prd4-quota-vocabulary.md) | PRD4 RPM/RPS/TPM/tokens/s terms, key quota visibility, and `qps_limit` compatibility boundary | v0.1 |
| 22 | [PRD4 PlungeIn Port Contract](22-prd4-plungein-port-contract.md) | PRD4 Observe/Act/Enrich/Decide/Mutate port boundaries and runtime-plugin deferral contract | v0.1 |
| 23 | [PRD4 Real Upstream Evidence](23-prd4-real-upstream-evidence.md) | PRD4 real-provider evidence harness, credential guardrails, and target-environment acceptance boundary | v0.1 |
| 24 | [PRD4 Real SSO Evidence](24-prd4-real-sso-evidence.md) | PRD4 Logto/OIDC browser-login evidence harness, credential guardrails, and target-environment acceptance boundary | v0.1 |
| 25 | [PRD5 X-Insight Sanitized Snapshot](25-prd5-xinsight-sanitized-snapshot.md) | PRD5 Gate A snapshot tool, handoff note, safety behavior, source refs, and read-only access gate | v0.2 |
| 26 | [PRD5 Evidence Field Contract](26-prd5-evidence-field-contract.md) | PRD5 trace/span/source/capture/decision/actor/payload field dictionary and nullable migration plan | v0.1 |

## 关联文档
- [PRD Phase 1](../PRD-Phase1.md) — 产品需求文档
- [PRD Phase 2](../PRD-Phase2.md) — PRD2 产品需求草案
- [运维文档](../ops/) — 部署和测试

## 基础设施配置

| 文件 | 说明 |
|------|------|
| `infra/prometheus/prometheus.yml` | Prometheus 抓取配置 |
| `infra/grafana/provisioning/` | Grafana datasource + dashboard provisioning |
| `infra/grafana/dashboards/unirouter-overview.json` | 预置 Grafana Dashboard（8 面板） |
| `infra/loadtest/k6-chat-completions.js` | k6 压测脚本 |

启动监控栈：`docker compose up -d prometheus grafana`
