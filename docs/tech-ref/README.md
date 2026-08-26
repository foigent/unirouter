# UniRouter 技术参考

> 面向开发团队的实现参考文档，按阅读顺序编号。

## 文档列表

| # | 文档 | 说明 | 版本 |
|---|------|------|------|
| 01 | [模型能力矩阵](01-provider-capability-matrix.md) | 2026-06-18 历史参考；不是当前运行时或价格权威 | v2.0 |
| 02 | [计费状态机](02-billing-state-machine.md) | 计费流程完整定义 | v2.0 |
| 03 | [错误处理矩阵](03-error-handling-matrix.md) | 错误分类 + 重试策略 | v2.0 |
| 04 | [OneAPI Hook 清单](04-oneapi-hook-manifest.md) | 未实现的历史架构参考，不是当前扩展合同 | — |
| 05 | [OpenAPI Spec](05-openapi-spec.yaml) | 当前 UniRouter API、RBAC、调用优化 opt-in/结果头与非敏感 License 能力合同，按路由、handler 和 route parity 对齐 | v1.4 |
| 06 | [前端页面规格](06-frontend-spec.md) | PRD1 历史设计快照；当前 UI 以代码、OpenAPI 和测试为准 | — |
| 07 | [结构化日志](07-structured-logging.md) | 日志架构 + 使用方式 | v1.0 |
| 08 | [监控指标](08-prometheus-metrics.md) | Prometheus 指标体系 | v1.0 |
| 09 | [准入控制](09-admission-control.md) | 并发限流架构 | v1.0 |
| 10 | [Provider Adapter Framework](10-provider-adapter-framework.md) | PRD2 上游适配器框架 | v0.1 |
| 11 | [Project And RBAC Model](11-project-rbac-model.md) | 当前项目、成员、预算与模型集合权限合同 | v1.0 |
| 12 | [Token Metering V2](12-token-metering-v2.md) | PRD2 token metering source and reconciliation contract | v0.1 |
| 13 | [Audit Schema Upgrade](13-audit-schema-upgrade.md) | PRD2 audit source, filter, and before/after contract | v0.1 |
| 14 | [Notification And Event Foundation](14-notification-event-foundation.md) | 当前事件生命周期、财务通知边界、Webhook 路由与预算阈值合同 | v1.0 |
| 15 | [HA Readiness](15-ha-readiness.md) | PRD2 multi-instance, shared-state, smoke, and rollback contract | v0.1 |
| 16 | [Commercial License Enforcement](16-license-foundation.md) | V1 strict signed License, embedded release trust, double-click macOS License Studio, deployment/feature/resource enforcement and PRD2 compatibility boundary | v1.1 |
| 17 | [Cache Research](17-cache-research.md) | PRD2 exact/semantic cache research boundary; no runtime cache by default | v0.1 |
| 18 | [OIDC First SSO](18-oidc-sso.md) | PRD2 OIDC login, claim mapping, provisioning, and audit contract | v0.1 |
| 19 | [Prompt And Response Storage Policy](19-prompt-response-storage-policy.md) | PRD2 P2-F11 policy: plaintext disabled by default; future opt-in controls | v0.1 |
| 20 | [PRD3 Product Glossary](20-prd3-product-glossary.md) | PRD3 canonical terminology for provider/channel, usage analytics, notification status, roles, and PRD4 boundaries | v0.1 |
| 21 | [PRD4 Quota Vocabulary](21-prd4-quota-vocabulary.md) | PRD4 RPM/RPS/TPM/tokens/s terms, key quota visibility, and `qps_limit` compatibility boundary | v0.1 |
| 22 | [PRD4 PlungeIn Port Contract](22-prd4-plungein-port-contract.md) | PRD4 Observe/Act/Enrich/Decide/Mutate port boundaries and runtime-plugin deferral contract | v0.1 |
| 23 | [PRD4 Real Upstream Evidence](23-prd4-real-upstream-evidence.md) | PRD4 real-provider evidence harness, credential guardrails, and target-environment acceptance boundary | v0.1 |
| 24 | [PRD4 Real SSO Evidence](24-prd4-real-sso-evidence.md) | PRD4 Logto/OIDC browser-login evidence harness, credential guardrails, and target-environment acceptance boundary | v0.1 |
| 25 | [PRD5 X-Insight Sanitized Snapshot](25-prd5-xinsight-sanitized-snapshot.md) | PRD5 Gate A snapshot tool, handoff note, safety behavior, source refs, capture-gap candidates, controlled handoff prerequisites, and read-only access gate | v0.4 |
| 26 | [PRD5 Evidence Field Contract](26-prd5-evidence-field-contract.md) | PRD5 trace/span/source/capture/decision/actor/payload field dictionary and nullable migration plan | v0.1 |
| 27 | [Signed Offline Upgrade Bundle](27-signed-offline-upgrade.md) | V1 deterministic signed bundle, embedded independent trust root, tamper/rollback/schema preflight and installed-release readback | v1.1 |
| 28 | [Completed-Execution Feed V1](28-completed-execution-feed-v1.md) | UniRouter-owned final metadata contract, private pull cursor, feed-only authorization, retention and rollback boundary | v1.0 |
| 29 | [Foigent Event Connector 协议 Pin 与 Reference Runtime](29-foigent-event-connector-protocol-pin.md) | 协议 v1.1 authority、产品 pin、Reference runtime 与 LiteLLM conformance-only 所有权边界 | v1.1 |
| 30 | [Provider Conformance Pack V1](30-provider-conformance-pack-v1.md) | 三个离线 Provider Pack、Azure OpenAI 原生适配、确定性 runner、Admin API 与兼容性声明边界 | v1.0 |
| 31 | [Contextual Inspectors And Local Views](31-contextual-inspectors-and-local-views.md) | Audit/Projects 常驻检查器、受控深链、本地元数据视图、竞态与失效关闭合同 | v1.0 |
| 32 | [OTel/OpenInference Metadata Export](32-otel-openinference-metadata-export.md) | 默认关闭、Outbox 后置、metadata-only OTLP/HTTP 导出、隐私/基数/重试与 Source 兼容边界 | v1.0 |
| 33 | [Secret Reference And Rotation](33-secret-reference-rotation.md) | 后端中立的版本化密钥引用、生命周期和 fail-closed 消费边界 | v1.0 |
| 34 | [Exact Response Cache V2](34-exact-response-cache.md) | 精确身份、加密 Redis、Shadow/Canary、零 Provider/零收费交付与默认关闭运行时 | v2.0 |
| 35 | [Endpoint Protocol Conformance](35-endpoint-protocol-conformance.md) | 模型端点协议一致性 Runner 与兼容边界 | v1.0 |
| 35b | [Provider COGS Offline](35-provider-cogs-offline.md) | 离线供应商成本事实、对账与客户计价隔离边界 | v1.0 |
| 36 | [Governance Simulation V1](36-governance-simulation-v1.md) | 治理策略只读模拟、证据和失效关闭合同 | v1.0 |
| 37 | [Member Lifecycle V1](37-member-lifecycle-v1.md) | 企业人员/账号生命周期预览与执行边界 | v1.0 |
| 38 | [Agent Trust Firewall](38-agent-trust-firewall.md) | Agent 执行信任控制、风险和授权边界 | v1.0 |
| 39 | [Provenance Passport And Second Connector](39-provenance-passport-and-second-connector.md) | exact Source provenance、Capture Gap、LiteLLM 跨语言 conformance-only 证明与运行时隔离 | v1.0 |
| 40 | [Invocation Optimization And Compression V1](40-invocation-optimization-v1.md) | 调用优化生命周期、内容无关分桶、Compression Shadow/Canary 与元数据证据 | v1.0 |
| 42 | [Model Execution Profile And Capability Status](42-model-execution-profile-capability-status.md) | 版本化模型执行档案、M82 Release 密封、供应商能力与 UniRouter 已验证能力的事实边界 | v1.1 |
| 43 | [Versioned Pricing Profiles And Client Projection](43-versioned-pricing-and-client-profile-sync.md) | 绝对单价、来源证据、Freeze 版本、非 Token 计费与 OpenClaw Profile 投影 | v1.0 |
| 44 | [Model Adaptation Required Baseline V1](44-model-adaptation-required-baseline-v1.md) | 30 个产品身份的不可变需求范围、surface/version policy 与里程碑验收分类；不作为运行时状态机 | v1.0 |

## 关联文档
- [PRD Phase 1](../PRD-Phase1.md) — 历史需求参考
- [PRD Phase 2](../PRD-Phase2.md) — 已冻结需求参考
- [运维文档](../ops/) — 部署和测试

## 基础设施配置

| 文件 | 说明 |
|------|------|
| `infra/prometheus/prometheus.yml` | Prometheus 抓取配置 |
| `infra/grafana/provisioning/` | Grafana datasource + dashboard provisioning |
| `infra/grafana/dashboards/unirouter-overview.json` | 预置 Grafana Dashboard（8 面板） |
| `infra/loadtest/k6-chat-completions.js` | k6 压测脚本 |

启动监控栈：`docker compose up -d prometheus grafana`
