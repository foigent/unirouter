# UniRouter 文档目录

## 当前入口
- [PROJECT-HANDOFF.md](PROJECT-HANDOFF.md) — Agent 接手项目的第一阅读入口
- [ops/CURRENT-STATUS.md](ops/CURRENT-STATUS.md) — 当前项目真实状态（进度、测试、部署、缺口）
- [planning/PRD6-START-HERE.md](planning/PRD6-START-HERE.md) — PRD6 Accepted Scope Complete、决策边界和证据阅读顺序
- [ops/evidence/PRD6-B0-baseline-and-competitor-audit-20260711.md](ops/evidence/PRD6-B0-baseline-and-competitor-audit-20260711.md) — Git/CI/测试/X-Insight/代码事实基线
- [ops/evidence/PRD6-B0B-scalability-antipattern-audit-20260711.md](ops/evidence/PRD6-B0B-scalability-antipattern-audit-20260711.md) — 100x、Agent 扇出、鉴权、计费、路由、流式和多模态反模式审计
- [ops/evidence/PRD6-B0B-P6F16-tenant-isolation-local-20260711.md](ops/evidence/PRD6-B0B-P6F16-tenant-isolation-local-20260711.md) — P6-F16 key/user/project/project-policy 租户边界本地证据
- [ops/evidence/PRD6-B0B-admin-tenant-surface-audit-20260711.md](ops/evidence/PRD6-B0B-admin-tenant-surface-audit-20260711.md) — P6-F16 之外财务、通知、价格、全局设置和模型治理租户审计
- [ops/evidence/PRD6-B0B-P6F33-notification-tenant-isolation-local-20260711.md](ops/evidence/PRD6-B0B-P6F33-notification-tenant-isolation-local-20260711.md) — P6-F33 通知/Webhook 租户隔离红绿回归与本地证据
- [ops/evidence/PRD6-B0B-P6F37-audit-log-tenant-isolation-local-20260711.md](ops/evidence/PRD6-B0B-P6F37-audit-log-tenant-isolation-local-20260711.md) — P6-F37 审计日志租户隔离红绿回归与本地证据
- [ops/evidence/PRD6-B0B-P6F38-frontend-test-ci-local-20260711.md](ops/evidence/PRD6-B0B-P6F38-frontend-test-ci-local-20260711.md) — P6-F38 前端测试进入 required CI 的本地证据
- [ops/evidence/PRD6-B0B-P6F32-P6F39-finance-and-usage-tenant-isolation-local-20260711.md](ops/evidence/PRD6-B0B-P6F32-P6F39-finance-and-usage-tenant-isolation-local-20260711.md) — P6-F32/P6-F39 财务与用量租户隔离本地证据
- [ops/evidence/PRD6-B0B-P6F17-operation-idempotency-identity-local-20260711.md](ops/evidence/PRD6-B0B-P6F17-operation-idempotency-identity-local-20260711.md) — P6-F17 服务端操作、客户端幂等和链路身份 CI 证据
- [ops/evidence/PRD6-B0B-P6F18A-billing-reservation-invariants-local-20260711.md](ops/evidence/PRD6-B0B-P6F18A-billing-reservation-invariants-local-20260711.md) — P6-F18A 钱包/预留/价格快照/单终态 CI 证据
- [ops/evidence/PRD6-B0B-P6F18B-stream-settlement-recovery-local-20260711.md](ops/evidence/PRD6-B0B-P6F18B-stream-settlement-recovery-local-20260711.md) — P6-F18B 流式输出标记、用量持久化和重启恢复 CI 证据
- [ops/evidence/PRD6-ENG-P6TESTDB-admin-integration-isolation-local-20260711.md](ops/evidence/PRD6-ENG-P6TESTDB-admin-integration-isolation-local-20260711.md) — P6-TEST-DB-001 管理端集成测试隔离、当前迁移和 required CI 证据
- [ops/evidence/PRD6-B0C-P6F03-retry-classification-local-20260711.md](ops/evidence/PRD6-B0C-P6F03-retry-classification-local-20260711.md) — P6-F03 安全重试分类、故障转移、计费和审计轨迹本地证据
- [ops/evidence/PRD6-B0C-P6F04-embeddings-failover-local-20260711.md](ops/evidence/PRD6-B0C-P6F04-embeddings-failover-local-20260711.md) — P6-F04 Embeddings 多路由、故障转移、计费与最终尝试证据
- [ops/evidence/PRD6-B0C-P6F05-routing-config-truth-local-20260711.md](ops/evidence/PRD6-B0C-P6F05-routing-config-truth-local-20260711.md) — P6-F05 有效路由配置、项目预算告警边界、运行时展示和 PostgreSQL 状态/租户证据
- [ops/evidence/PRD6-B0C-P6F06-multiplier-freeze-local-20260711.md](ops/evidence/PRD6-B0C-P6F06-multiplier-freeze-local-20260711.md) — P6-F06 组织倍率冻结、精确并发结算、零未收金额和取消退款证据
- [ops/evidence/PRD6-B0C-P6STREAM002A-stream-transport-local-20260712.md](ops/evidence/PRD6-B0C-P6STREAM002A-stream-transport-local-20260712.md) — P6-STREAM-002A 流式帧、超时、写入串行化、部分结算和未完成边界
- [ops/evidence/PRD6-B1-P6F19A-agent-capability-kernel-local-20260712.md](ops/evidence/PRD6-B1-P6F19A-agent-capability-kernel-local-20260712.md) — P6-F19A 显式 Agent 登记、5 分钟能力、刷新/撤销和 100 子 Agent 原子上限
- [ops/evidence/PRD6-B1-P6F19B-agent-ancestor-budget-local-20260712.md](ops/evidence/PRD6-B1-P6F19B-agent-ancestor-budget-local-20260712.md) — P6-F19B 祖先成本/Token/速率/并发共享执行与 P6-F19 收口证据
- [ops/evidence/PRD6-B1-P6F07-distributed-limit-policy-local-20260712.md](ops/evidence/PRD6-B1-P6F07-distributed-limit-policy-local-20260712.md) — P6-F07 跨副本 RPM/RPS/TPM/并发策略、故障语义、控制 UI、迁移与 CI 收口证据
- [ops/evidence/PRD6-B1-P6F21-weighted-workload-admission-local-20260712.md](ops/evidence/PRD6-B1-P6F21-weighted-workload-admission-local-20260712.md) — P6-F21 加权跨副本租约、公平队列、故障/混合配置及 PR #170/#180/#181 required CI 真值链
- [ops/evidence/PRD6-B2-P6F08-routing-policy-v1-local-20260712.md](ops/evidence/PRD6-B2-P6F08-routing-policy-v1-local-20260712.md) — P6-F08 确定性同级权重、短时快照、跨副本探针、epoch CAS、预览/证据、安全回滚及 PR #172/main CI 收口证据
- [ops/evidence/PRD6-B3-P6F09-prompt-cache-billing-local-20260712.md](ops/evidence/PRD6-B3-P6F09-prompt-cache-billing-local-20260712.md) — P6-F09 上游 Prompt Cache 用量归一、冻结价格、结算恢复、日志/对账/UI、迁移回滚、PR #174/#175 与最终 main CI 收口证据
- [ops/evidence/PRD6-B3-P6F20-durable-outbox-local-20260712.md](ops/evidence/PRD6-B3-P6F20-durable-outbox-local-20260712.md) — P6-F20 最终用量/终态持久化、内部 outbox、重放及 PR #177/main CI 收口证据
- [ops/evidence/PRD6-B4-P6F10-guardrail-kernel-local-20260713.md](ops/evidence/PRD6-B4-P6F10-guardrail-kernel-local-20260713.md) — P6-F10 Guardrail、内容上限、成本 hold、迁移及 PR #188 CI 收口证据
- [ops/evidence/PRD6-B5-P6F11-stream-policy-termination-local-20260713.md](ops/evidence/PRD6-B5-P6F11-stream-policy-termination-local-20260713.md) — P6-F11 流式微缓冲、终止、预算扩容、checkpoint/recovery 及 PR #189 CI 收口证据
- [ops/evidence/PRD6-ACCEPTED-SCOPE-CLOSEOUT-20260713.md](ops/evidence/PRD6-ACCEPTED-SCOPE-CLOSEOUT-20260713.md) — PRD6 27/27 accepted rows 收口、证据等级和后续边界
- [planning/PRD6-HA-AND-STREAM-FAILOVER-RESEARCH-20260712.md](planning/PRD6-HA-AND-STREAM-FAILOVER-RESEARCH-20260712.md) — LiteLLM/New API/Portkey/Kong 高可用模式、UniRouter 集群事实和 D6-12 流式边界
- [planning/PRD6-PRODUCT-ARCHITECTURE-UX-AUDIT-20260711.md](planning/PRD6-PRODUCT-ARCHITECTURE-UX-AUDIT-20260711.md) — UI、对账、链路身份、架构与 2B 升级审计
- [planning/PRD7-CANDIDATE-BACKLOG.md](planning/PRD7-CANDIDATE-BACKLOG.md) — 未来首项方向已选；PRD6 已闭环，但实现仍按用户决定后置
- [planning/PRD7-CACHE-SERVICE-CANDIDATE.md](planning/PRD7-CACHE-SERVICE-CANDIDATE.md) — 未来独立 Cache Service、容量拆分触发线和 SaaS cell 复用边界
- [planning/PRD6-COMPETITOR-ALIGNMENT-20260711.md](planning/PRD6-COMPETITOR-ALIGNMENT-20260711.md) — LiteLLM/New API/One API 固定版本核验、四象限和优先级矩阵
- [planning/PRD6-ADAPTIVE-TRUST-INNOVATION-20260711.md](planning/PRD6-ADAPTIVE-TRUST-INNOVATION-20260711.md) — CARTA、影子评测、反串谋、四象限、三平面边界和 Killer Features
- [planning/prd6-adaptive-trust-hardening/hardening.md](planning/prd6-adaptive-trust-hardening/hardening.md) — adaptive-trust 三种结构方案、安全权衡和 signed local enforcement 推荐
- [PRD-Phase6.md](PRD-Phase6.md) — PRD6 Accepted Scope Complete、proposed advanced/long-tail、所有权和安全边界
- [ops/PRD6-ACCEPTANCE-MATRIX.md](ops/PRD6-ACCEPTANCE-MATRIX.md) — PRD6 accepted/proposed/external/target-evidence 验收矩阵
- [planning/PRD6-IMPLEMENTATION-PLAN.md](planning/PRD6-IMPLEMENTATION-PLAN.md) — PRD6 可执行 batches、风险和 gates
- [planning/PRD6-BACKLOG.md](planning/PRD6-BACKLOG.md) — PRD6 分类 backlog 和决策登记
- [ops/evidence/PRD5-XINSIGHT-runtime-contract-reconciliation-20260711.md](ops/evidence/PRD5-XINSIGHT-runtime-contract-reconciliation-20260711.md) — X-Insight 已答复 runtime contract、v1 sidecar 边界、UniRouter 合同锁和 PRD5 完成分类
- [planning/PRD5-CURRENT-GAP-AUDIT-20260710.md](planning/PRD5-CURRENT-GAP-AUDIT-20260710.md) — PRD5 收口时完成度、所有权和目标证据边界
- [ops/evidence/PRD5-P5F12-architecture-audit-20260710.md](ops/evidence/PRD5-P5F12-architecture-audit-20260710.md) — PRD5 repository-wide 架构/功能/UI/数据/部署/安全审计、P5-F12 accept/defer/reject 筛选、P0-P3 batches 和 INFRA-SEC-001 证据设计
- [ops/evidence/PRD5-B1Y-unirouter-owned-completion-local-20260710.md](ops/evidence/PRD5-B1Y-unirouter-owned-completion-local-20260710.md) — PRD5 已接受 UniRouter 自有 trace search、Data Exchange Control、route/failover、redacted topology 和本地验证
- [ops/evidence/PRD5-B1Z-target-deployment-and-infra-security-20260710.md](ops/evidence/PRD5-B1Z-target-deployment-and-infra-security-20260710.md) — PRD5 目标迁移/部署/浏览器、canary 配置对齐和 INFRA-SEC-001 切换证据
- [planning/PRD5-DEFERRED-CONTROL-DESIGNS-20260710.md](planning/PRD5-DEFERRED-CONTROL-DESIGNS-20260710.md) — budget hard-block、service account 和 incident/SLO 实施级设计；运行时仍 defer
- [PRD-Phase5.md](PRD-Phase5.md) — PRD5 已接受 UniRouter 自有范围完成；X-Insight 未实现、目标证据和明确 deferred/rejected 边界分开记录
- [ops/PRD5-ACCEPTANCE-MATRIX.md](ops/PRD5-ACCEPTANCE-MATRIX.md) — PRD5 验收矩阵；Batch 1Z 已记录目标迁移/部署/浏览器/安全证据，P5-F12 screening 保持 Local verified，route/failover 保持 CI verified
- [ops/PRD5-GATE-A-REQUEST-ID-APPROVAL.md](ops/PRD5-GATE-A-REQUEST-ID-APPROVAL.md) — PRD5 Gate A request ID 非技术审批说明和 X-Insight snapshot 数据最小化边界
- [ops/PRD5-GATE-A-CONTROLLED-RUN-AUTHORIZATION.md](ops/PRD5-GATE-A-CONTROLLED-RUN-AUTHORIZATION.md) — 新建 Gate A 场景的环境、操作人、数据类别和保留期授权边界
- [planning/PRD5-IMPLEMENTATION-PLAN.md](planning/PRD5-IMPLEMENTATION-PLAN.md) — PRD5 分批实施计划和 UniRouter/X-Insight 并行边界
- [planning/PRD5-COMPETITOR-COMPARISON.md](planning/PRD5-COMPETITOR-COMPARISON.md) — UniRouter 与 LiteLLM/New API/one-api/Langfuse/Helicone 对比和 PRD5 追赶项
- [PRD-Phase4.md](PRD-Phase4.md) — PRD4 已按当前确认范围完成；真实上游/真实 SSO target E2E 等延后到 PRD5
- [ops/PRD4-ACCEPTANCE-MATRIX.md](ops/PRD4-ACCEPTANCE-MATRIX.md) — PRD4 验收矩阵和证据边界
- [planning/PRD5-BACKLOG.md](planning/PRD5-BACKLOG.md) — PRD5 未实现/延后能力清单
- [planning/PRD5-XINSIGHT-GATE-A-COORDINATION.md](planning/PRD5-XINSIGHT-GATE-A-COORDINATION.md) — PRD5 / X-Insight Gate A 协调：Batch 1V 诊断快照与 Batch 1W 目标复测、receipt 和后续集成边界
- [planning/PRD5-XINSIGHT-DATA-EXCHANGE-CONTROL.md](planning/PRD5-XINSIGHT-DATA-EXCHANGE-CONTROL.md) — PRD5 X-Insight Data Exchange Control UI/control-plane 边界：UniRouter 管 outbound approval/export ledger/receipts，X-Insight 管 downstream audit/investigation
- [planning/PRD4-BACKLOG.md](planning/PRD4-BACKLOG.md) — PRD4 候选能力来源记录；正式范围以 `PRD-Phase4.md` 为准
- [ops/PRD3-ACCEPTANCE-MATRIX.md](ops/PRD3-ACCEPTANCE-MATRIX.md) — 已完成 PRD3 清理/优化验收矩阵
- [PRD-Phase3.md](PRD-Phase3.md) — 已完成 PRD3 一致性、语义、契约和架构清理范围
- [planning/PRD3-CONSISTENCY-AUDIT.md](planning/PRD3-CONSISTENCY-AUDIT.md) — PRD3 范围审计和 PRD1→PRD2 遗留问题梳理
- [issues/OPEN-ISSUES.md](issues/OPEN-ISSUES.md) — 当前未关闭问题列表
- [agent-workflows/EXECUTION-RECORD.md](agent-workflows/EXECUTION-RECORD.md) — 当前执行记录：PRD6 Accepted Scope Closeout 与纯文档 PR 门禁
- [agent-workflows/reviews/README.md](agent-workflows/reviews/README.md) — 可选独立复核记录格式和问题级别规则
- [ops/evidence/AGENT-PROTOCOL-v2-single-owner-20260710.md](ops/evidence/AGENT-PROTOCOL-v2-single-owner-20260710.md) — 单一执行负责人、Git/CI、高影响授权和复核规则的确认记录
- [ops/evidence/PRD5-B1W-pre-relay-model-permission-parity-local-20260710.md](ops/evidence/PRD5-B1W-pre-relay-model-permission-parity-local-20260710.md) — Batch 1W 根因、代码、回归和目标证据边界
- [ops/evidence/PRD5-B1W-target-rerun-20260710.md](ops/evidence/PRD5-B1W-target-rerun-20260710.md) — Batch 1W 目标部署、单次拒绝复测、临时密钥禁用、第二轮 X-Insight 持久化回读和基础设施风险
- [ops/evidence/PRD5-P5F12-architecture-audit-20260710.md](ops/evidence/PRD5-P5F12-architecture-audit-20260710.md) — Batch 1X 本地审计、P5-F12 screening、Logs/CI 本地修复证据和 INFRA-SEC-001 方案
- [ops/evidence/PRD5-B1Y-unirouter-owned-completion-local-20260710.md](ops/evidence/PRD5-B1Y-unirouter-owned-completion-local-20260710.md) — Batch 1Y UniRouter 自有功能本地完成证据
- [ops/evidence/PRD5-B1Z-target-deployment-and-infra-security-20260710.md](ops/evidence/PRD5-B1Z-target-deployment-and-infra-security-20260710.md) — Batch 1Z 目标部署、迁移、浏览器、canary parity、安全组切换和清理证据
- [ops/evidence/PRD5-XINSIGHT-runtime-contract-reconciliation-20260711.md](ops/evidence/PRD5-XINSIGHT-runtime-contract-reconciliation-20260711.md) — X-Insight runtime facts、synthetic-only target、未实现能力和 UniRouter v1 contract lock
- [ops/evidence/PRD5-GATE-A-XINSIGHT-external-integration-20260710.md](ops/evidence/PRD5-GATE-A-XINSIGHT-external-integration-20260710.md) — Gate A hash/receipt、持久化幂等、readback 和剩余边界对账
- [agent-workflows/FIX-SIGNAL.md](agent-workflows/FIX-SIGNAL.md) — 协议 v1.x 兼容入口
- [agent-workflows/VERIFY-RESULT.md](agent-workflows/VERIFY-RESULT.md) — 最近一次协议 v1.x 历史验收结果
- [CODEBASE-MAP.md](CODEBASE-MAP.md) — 代码结构、路由、数据、测试、部署地图
- [DOC-INVENTORY.md](DOC-INVENTORY.md) — 文档清单与当前/冻结/历史/候选边界
- [DOCS-GOVERNANCE.md](DOCS-GOVERNANCE.md) — 文档治理规则

## 核心文档
- [PRD-Phase6.md](PRD-Phase6.md) — PRD6 Accepted Scope Complete；proposed/deferred rows 仍需单独决定
- [PRD-Phase5.md](PRD-Phase5.md) — PRD5 Accepted Scope Complete 历史事实
- [PRD-Phase4.md](PRD-Phase4.md) — PRD4 已完成范围历史
- [PRD-Phase3.md](PRD-Phase3.md) — 已完成 PRD3 清理/优化范围
- [PRD-Phase2.md](PRD-Phase2.md) — PRD2 已冻结需求参考
- [PRD-Phase1.md](PRD-Phase1.md) — PRD1 历史需求参考
- [PRD-Phase1.pdf](PRD-Phase1.pdf) — PRD1 PDF 导出，可能落后于 Markdown

## 设计文档
- [design/README.md](design/README.md) — UI 设计文档索引（人类友好版本）
- [design/manifesto.md](design/manifesto.md) — 设计宣言（定稿）
- [design/spec-v1.md](design/spec-v1.md) — 设计规范 v1（定稿）
- [design/pages/](design/pages/) — 页面级设计文档
- [ui/FRONTEND-UI-ARCHITECTURE-AUDIT.md](ui/FRONTEND-UI-ARCHITECTURE-AUDIT.md) — 当前前端 UI/架构审计与 PRD1/PRD2 建议

## 技术参考
- [tech-ref/](tech-ref/) — 开发团队技术参考索引
- [tech-ref/20-prd3-product-glossary.md](tech-ref/20-prd3-product-glossary.md) — PRD3 产品术语口径：供应商/上游通道、用量分析、通知状态、角色和 PRD4 边界
- [tech-ref/21-prd4-quota-vocabulary.md](tech-ref/21-prd4-quota-vocabulary.md) — PRD4 RPM/RPS/TPM/tokens/s 口径和 `qps_limit` 兼容边界
- [tech-ref/22-prd4-plungein-port-contract.md](tech-ref/22-prd4-plungein-port-contract.md) — PRD4 PlungeIn Observe/Act/Enrich/Decide/Mutate 端口边界和 runtime plugin 延后契约
- [tech-ref/23-prd4-real-upstream-evidence.md](tech-ref/23-prd4-real-upstream-evidence.md) — PRD4 真实上游证据 harness、凭证门控和 PRD5 目标环境验收边界
- [tech-ref/24-prd4-real-sso-evidence.md](tech-ref/24-prd4-real-sso-evidence.md) — PRD4 真实 SSO/Logto/OIDC 证据 harness、凭证门控和 PRD5 目标环境验收边界
- [tech-ref/25-prd5-xinsight-sanitized-snapshot.md](tech-ref/25-prd5-xinsight-sanitized-snapshot.md) — PRD5 X-Insight Gate A sanitized snapshot 工具、handoff note、输出合同和只读访问门槛
- [tech-ref/26-prd5-evidence-field-contract.md](tech-ref/26-prd5-evidence-field-contract.md) — PRD5 trace/span/source/capture/decision/actor/payload evidence 字段合同

## 工程治理
- [governance/README.md](governance/README.md) — PRD3 最低工程治理：ADR、API 兼容、migration/rollback、release、incident、deprecation
- [governance/ADR-TEMPLATE.md](governance/ADR-TEMPLATE.md) — Architecture Decision Record 模板
- [governance/adr/ADR-0001-prd4-plungein-ports.md](governance/adr/ADR-0001-prd4-plungein-ports.md) — PRD4 PlungeIn ports 架构决策
- [governance/adr/ADR-0002-prd4-api-key-owner-usage.md](governance/adr/ADR-0002-prd4-api-key-owner-usage.md) — PRD4 API key owner 用量归因决策草案
- [governance/adr/ADR-0003-prd4-status-center-observability.md](governance/adr/ADR-0003-prd4-status-center-observability.md) — PRD4 Status Center / Runtime Observability 决策草案

## 运维文档
- [ops/deployment-runbook.md](ops/deployment-runbook.md) — 部署手册（含 Canary + HTTPS 章节）
- [ops/canary-testing.md](ops/canary-testing.md) — 灰度测试操作指南
- [ops/test-plan.md](ops/test-plan.md) — 测试计划
- [ops/test-report.md](ops/test-report.md) — 测试报告
- [ops/evidence/COMPREHENSIVE-TEST-REPORT-20260705.md](ops/evidence/COMPREHENSIVE-TEST-REPORT-20260705.md) — 2026-07-05 Canary 综合测试/安全审计证据；当前问题状态以 `issues/OPEN-ISSUES.md` 为准
- [ops/PRD2-ACCEPTANCE-MATRIX.md](ops/PRD2-ACCEPTANCE-MATRIX.md) — PRD2 已冻结验收状态
- [ops/PRD4-ACCEPTANCE-MATRIX.md](ops/PRD4-ACCEPTANCE-MATRIX.md) — PRD4 已完成验收矩阵
- [ops/PRD5-ACCEPTANCE-MATRIX.md](ops/PRD5-ACCEPTANCE-MATRIX.md) — PRD5 收口验收矩阵
- [ops/PRD5-GATE-A-REQUEST-ID-APPROVAL.md](ops/PRD5-GATE-A-REQUEST-ID-APPROVAL.md) — PRD5 Gate A request ID 审批 runbook
- [ops/PRD2-COMPLETION-PLAN.md](ops/PRD2-COMPLETION-PLAN.md) — PRD2 收尾决策和历史计划
- [ops/PRD-ACCEPTANCE-MATRIX.md](ops/PRD-ACCEPTANCE-MATRIX.md) — PRD1 历史验收矩阵
- [ops/PRD1-CLOSEOUT-PLAN.md](ops/PRD1-CLOSEOUT-PLAN.md) — PRD1 历史收尾计划
- [ops/PRD1-STRICT-CLOSEOUT-RUNBOOK.md](ops/PRD1-STRICT-CLOSEOUT-RUNBOOK.md) — PRD1 历史严格关闭 Runbook
- [ops/PRD1-REMAINING-ACCEPTANCE-PACK.md](ops/PRD1-REMAINING-ACCEPTANCE-PACK.md) — PRD1 历史剩余验收包

## Agent 协作
- [agent-workflows/AGENT-PROTOCOL.md](agent-workflows/AGENT-PROTOCOL.md) — 单一执行负责人、授权、证据和可选独立复核协议
- [agent-workflows/EXECUTION-RECORD.md](agent-workflows/EXECUTION-RECORD.md) — 当前批次执行、测试、CI、部署和缺口记录
- [agent-workflows/reviews/README.md](agent-workflows/reviews/README.md) — 独立复核入口
- [agent-workflows/FIX-SIGNAL.md](agent-workflows/FIX-SIGNAL.md) — 协议 v1.x 兼容入口
- [agent-workflows/VERIFY-RESULT.md](agent-workflows/VERIFY-RESULT.md) — 协议 v1.x 历史结果

## 测试文档
- [testing/archive/](testing/archive/) — 历史测试阶段文档；当前状态以 `ops/CURRENT-STATUS.md` 为准
- [testing/archive/TEST-STATUS.md](testing/archive/TEST-STATUS.md) — 历史测试状态快照
- [testing/archive/BUG-LOG.md](testing/archive/BUG-LOG.md) — 历史 Bug 记录；当前未关闭问题以 `issues/OPEN-ISSUES.md` 为准

## 项目管理
- [issues/OPEN-ISSUES.md](issues/OPEN-ISSUES.md) — 当前待处理问题
- [issues/](issues/) — 问题补充说明和历史 issue 文件
- [consultations/](consultations/) — 顾问咨询记录

## 归档（本地保留，不推送 GitHub）
- `archive/issues/` — 已解决的 issue
- `archive/todolist/` — 待办事项（非 Bug，如功能集成、配置完善）
- `reviews/` — PRD 评审记录
