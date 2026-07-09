# UniRouter 文档目录

## 当前入口
- [PROJECT-HANDOFF.md](PROJECT-HANDOFF.md) — Agent 接手项目的第一阅读入口
- [ops/CURRENT-STATUS.md](ops/CURRENT-STATUS.md) — 当前项目真实状态（进度、测试、部署、缺口）
- [PRD-Phase5.md](PRD-Phase5.md) — PRD5 当前正式范围：X-Insight evidence readiness、结构字段、snapshot、payload 边界、轻量 trace UI、竞品追赶和 Data Exchange Control 边界
- [ops/PRD5-ACCEPTANCE-MATRIX.md](ops/PRD5-ACCEPTANCE-MATRIX.md) — PRD5 验收矩阵；Batch 1A/1B/1C through Batch 1S Canary/Sentinel complete，Batch 1T fixed awaiting Canary
- [ops/PRD5-GATE-A-REQUEST-ID-APPROVAL.md](ops/PRD5-GATE-A-REQUEST-ID-APPROVAL.md) — PRD5 Gate A request ID 非技术审批说明和 X-Insight snapshot 数据最小化边界
- [ops/PRD5-GATE-A-CONTROLLED-RUN-AUTHORIZATION.md](ops/PRD5-GATE-A-CONTROLLED-RUN-AUTHORIZATION.md) — 新建 Gate A 场景的环境、操作人、数据类别和保留期授权边界
- [planning/PRD5-IMPLEMENTATION-PLAN.md](planning/PRD5-IMPLEMENTATION-PLAN.md) — PRD5 分批实施计划和 UniRouter/X-Insight 并行边界
- [planning/PRD5-COMPETITOR-COMPARISON.md](planning/PRD5-COMPETITOR-COMPARISON.md) — UniRouter 与 LiteLLM/New API/one-api/Langfuse/Helicone 对比和 PRD5 追赶项
- [PRD-Phase4.md](PRD-Phase4.md) — PRD4 已按当前确认范围完成；真实上游/真实 SSO target E2E 等延后到 PRD5
- [ops/PRD4-ACCEPTANCE-MATRIX.md](ops/PRD4-ACCEPTANCE-MATRIX.md) — PRD4 验收矩阵和证据边界
- [planning/PRD5-BACKLOG.md](planning/PRD5-BACKLOG.md) — PRD5 未实现/延后能力清单
- [planning/PRD5-XINSIGHT-GATE-A-COORDINATION.md](planning/PRD5-XINSIGHT-GATE-A-COORDINATION.md) — PRD5 / X-Insight Gate A 协调：snapshot 先、Batch 1O controlled handoff prep、只读 DB/view 后、结构字段先于 exporter/outbox
- [planning/PRD5-XINSIGHT-DATA-EXCHANGE-CONTROL.md](planning/PRD5-XINSIGHT-DATA-EXCHANGE-CONTROL.md) — PRD5 X-Insight Data Exchange Control UI/control-plane 边界：UniRouter 管 outbound approval/export ledger/receipts，X-Insight 管 downstream audit/investigation
- [planning/PRD4-BACKLOG.md](planning/PRD4-BACKLOG.md) — PRD4 候选能力来源记录；正式范围以 `PRD-Phase4.md` 为准
- [ops/PRD3-ACCEPTANCE-MATRIX.md](ops/PRD3-ACCEPTANCE-MATRIX.md) — 已完成 PRD3 清理/优化验收矩阵
- [PRD-Phase3.md](PRD-Phase3.md) — 已完成 PRD3 一致性、语义、契约和架构清理范围
- [planning/PRD3-CONSISTENCY-AUDIT.md](planning/PRD3-CONSISTENCY-AUDIT.md) — PRD3 范围审计和 PRD1→PRD2 遗留问题梳理
- [issues/OPEN-ISSUES.md](issues/OPEN-ISSUES.md) — 当前未关闭问题列表
- [agent-workflows/FIX-SIGNAL.md](agent-workflows/FIX-SIGNAL.md) — 当前 Codex 验证信号：PRD5 Batch 1T Gate A Exchange Envelope And Controlled Run Authorization fixed awaiting Canary
- [agent-workflows/VERIFY-RESULT.md](agent-workflows/VERIFY-RESULT.md) — 最新 Canary 验收结果；PRD5 Batch 1S PASS 6/6
- [CODEBASE-MAP.md](CODEBASE-MAP.md) — 代码结构、路由、数据、测试、部署地图
- [DOC-INVENTORY.md](DOC-INVENTORY.md) — 文档清单与当前/冻结/历史/候选边界
- [DOCS-GOVERNANCE.md](DOCS-GOVERNANCE.md) — 文档治理规则

## 核心文档
- [PRD-Phase5.md](PRD-Phase5.md) — PRD5 当前正式范围草案
- [PRD-Phase4.md](PRD-Phase4.md) — PRD4 正式范围草案
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
- [ops/PRD4-ACCEPTANCE-MATRIX.md](ops/PRD4-ACCEPTANCE-MATRIX.md) — PRD4 当前验收矩阵
- [ops/PRD5-ACCEPTANCE-MATRIX.md](ops/PRD5-ACCEPTANCE-MATRIX.md) — PRD5 当前验收矩阵
- [ops/PRD5-GATE-A-REQUEST-ID-APPROVAL.md](ops/PRD5-GATE-A-REQUEST-ID-APPROVAL.md) — PRD5 Gate A request ID 审批 runbook
- [ops/PRD2-COMPLETION-PLAN.md](ops/PRD2-COMPLETION-PLAN.md) — PRD2 收尾决策和历史计划
- [ops/PRD-ACCEPTANCE-MATRIX.md](ops/PRD-ACCEPTANCE-MATRIX.md) — PRD1 历史验收矩阵
- [ops/PRD1-CLOSEOUT-PLAN.md](ops/PRD1-CLOSEOUT-PLAN.md) — PRD1 历史收尾计划
- [ops/PRD1-STRICT-CLOSEOUT-RUNBOOK.md](ops/PRD1-STRICT-CLOSEOUT-RUNBOOK.md) — PRD1 历史严格关闭 Runbook
- [ops/PRD1-REMAINING-ACCEPTANCE-PACK.md](ops/PRD1-REMAINING-ACCEPTANCE-PACK.md) — PRD1 历史剩余验收包

## Agent 协作
- [agent-workflows/AGENT-PROTOCOL.md](agent-workflows/AGENT-PROTOCOL.md) — Agent 协作协议（Codex/Canary 角色定义）
- [agent-workflows/FIX-SIGNAL.md](agent-workflows/FIX-SIGNAL.md) — Codex 修复信号（验收指令）
- [agent-workflows/VERIFY-RESULT.md](agent-workflows/VERIFY-RESULT.md) — Canary 验收结果

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
