# UniRoute

企业级 AI 模型管控平台 — 统一接入 + 按量计费 + 本地部署

## 快速开始

```bash
# 1. 克隆项目
git clone <repo-url> && cd uniroute

# 2. 配置环境变量
cp .env.example .env
# 编辑 .env，修改 JWT_SECRET、API_KEY_HASH_SECRET、ENCRYPTION_KEY、GRAFANA_ADMIN_PASSWORD；如需导入离线 License，同时配置 LICENSE_PUBLIC_KEY
openssl rand -hex 32  # 生成密钥

# 3. 启动服务
docker compose up -d

# 4. 验证
curl http://localhost:8080/health/ready
# {"status":"ok","db":"ok","redis":"ok"}

# 5. 数据库迁移会在后端启动时自动执行；也可以手动补跑
docker compose exec backend ./uniroute migrate up

# 6. 首次部署后必须立刻轮换默认管理员密码
docker compose exec backend ./uniroute admin set-password \
    --username admin --password <strong-password> --org-id 1
```

## 架构

```
Docker Compose（6 个容器）：
├── backend（Go 二进制）
│   ├── /v1/*       → OpenAI 兼容网关
│   ├── /api/v1/*   → 管理后台 API
│   └── /health/*   → 健康检查
├── frontend（Nginx + React 静态资源）
├── PostgreSQL 15
├── Redis 7
├── Prometheus
└── Grafana
```

## 技术栈

- **后端**：Go `net/http` + pgx + go-redis
- **数据库**：PostgreSQL 15 + Redis 7
- **前端**：React 18 + Semi Design + Vite
- **基础**：OneAPI（Go, MIT）Deep Fork

## 文档

- [贡献指南](CONTRIBUTING.md) — 开发流程、代码规范、提交规范
- [Agent Handoff](docs/PROJECT-HANDOFF.md) — 其他 agent 接手项目的第一入口
- [当前状态](docs/ops/CURRENT-STATUS.md) — 当前真实进度、测试和缺口
- [PRD6 启动入口](docs/planning/PRD6-START-HERE.md) — 当前决策状态、基线和阅读顺序
- [PRD6 产品范围](docs/PRD-Phase6.md) — PRD6 范围提案、安全边界和所有权
- [PRD6 验收矩阵](docs/ops/PRD6-ACCEPTANCE-MATRIX.md) — 已接受、待确认、外部和目标证据边界
- [PRD6 竞品与四象限](docs/planning/PRD6-COMPETITOR-ALIGNMENT-20260711.md) — 固定版本竞品核验和优先级
- [PRD6 P6-F16 本地证据](docs/ops/evidence/PRD6-B0B-P6F16-tenant-isolation-local-20260711.md) — key/user/project/project-policy 租户边界修复与回归
- [PRD6 管理面租户审计](docs/ops/evidence/PRD6-B0B-admin-tenant-surface-audit-20260711.md) — 财务、通知、价格、全局设置和模型治理的后续 confirmed findings
- [PRD6 P6-F33 本地证据](docs/ops/evidence/PRD6-B0B-P6F33-notification-tenant-isolation-local-20260711.md) — 通知/Webhook 租户隔离修复与红绿回归
- [PRD6 P6-F37 本地证据](docs/ops/evidence/PRD6-B0B-P6F37-audit-log-tenant-isolation-local-20260711.md) — 审计日志租户隔离修复与双租户回归
- [PRD6 P6-F38 本地证据](docs/ops/evidence/PRD6-B0B-P6F38-frontend-test-ci-local-20260711.md) — 现有前端测试纳入 required CI
- [PRD6 P6-F32/P6-F39 本地证据](docs/ops/evidence/PRD6-B0B-P6F32-P6F39-finance-and-usage-tenant-isolation-local-20260711.md) — 财务、请求日志和用量派生视图的租户隔离
- [PRD6 产品/架构/UX 审计](docs/planning/PRD6-PRODUCT-ARCHITECTURE-UX-AUDIT-20260711.md) — 审计日志、用量核验、链路 ID、UI、扩展性与升级建议
- [PRD7 候选 Backlog](docs/planning/PRD7-CANDIDATE-BACKLOG.md) — 未来首项方向已选；全部实施后置到 PRD6 闭环后
- [PRD5 产品范围](docs/PRD-Phase5.md) — Accepted Scope Complete 历史事实
- [PRD5 验收矩阵](docs/ops/PRD5-ACCEPTANCE-MATRIX.md) — PRD5 关闭证据和明确决策
- [PRD3 验收矩阵](docs/ops/PRD3-ACCEPTANCE-MATRIX.md) — 已完成 PRD3 清理/优化验收状态
- [PRD3 产品范围](docs/PRD-Phase3.md) — 已完成 PRD3 一致性、语义、契约和架构清理范围
- [PRD3 一致性审计](docs/planning/PRD3-CONSISTENCY-AUDIT.md) — PRD1→PRD2 遗留问题梳理
- [PRD4 候选 Backlog](docs/planning/PRD4-BACKLOG.md) — PRD4 候选能力来源记录，正式范围以 PRD4 产品范围为准
- [Open Issues](docs/issues/OPEN-ISSUES.md) — 当前未关闭问题
- [Agent 协议](docs/agent-workflows/AGENT-PROTOCOL.md) — 单一执行负责人、授权、证据和可选独立复核流程
- [文档目录](docs/README.md) — 当前、冻结、历史和候选文档入口
- [文档治理](docs/DOCS-GOVERNANCE.md) — 文档状态和更新规则
- [PRD2 产品需求](docs/PRD-Phase2.md) — PRD2 已冻结需求参考
- [PRD1 产品需求](docs/PRD-Phase1.md) — PRD1 历史需求参考
- [技术参考](docs/tech-ref/) — 开发团队技术参考
  - [01 模型能力矩阵](docs/tech-ref/01-provider-capability-matrix.md)
  - [02 计费状态机](docs/tech-ref/02-billing-state-machine.md)
  - [03 错误处理矩阵](docs/tech-ref/03-error-handling-matrix.md)
  - [04 OneAPI Hook 清单](docs/tech-ref/04-oneapi-hook-manifest.md)
  - [05 OpenAPI Spec](docs/tech-ref/05-openapi-spec.yaml)
  - [06 前端页面规格](docs/tech-ref/06-frontend-spec.md)
- [运维文档](docs/ops/) — 部署和测试
  - [部署手册](docs/ops/deployment-runbook.md)
  - [测试计划](docs/ops/test-plan.md)

## 项目结构

```
UniRouter/
├── cmd/uniroute/         # 入口
│   └── main.go
├── internal/             # 内部包
│   ├── admin/            # 管理后台 API handlers
│   ├── auth/             # 认证逻辑
│   ├── billing/          # 计费模块（计价、告警、对账）
│   ├── circuitbreaker/   # 熔断器
│   ├── config/           # 配置加载
│   ├── db/               # 数据库连接
│   ├── errdocs/          # 错误码文档注册表
│   ├── filter/           # 请求过滤
│   ├── gateway/          # 网关路由 & 中继
│   ├── licensing/        # 离线 License 校验
│   ├── logger/           # 日志辅助
│   ├── metering/         # token usage 来源与口径
│   ├── middleware/        # HTTP 中间件（鉴权、限流、CORS）
│   ├── migrate/          # 数据库迁移执行器
│   ├── modelgov/         # 模型治理辅助
│   ├── models/           # 数据模型
│   ├── oidc/             # OIDC SSO
│   ├── redisx/           # Redis 封装
│   ├── testutil/         # 测试工具
│   └── tokenutil/        # Token 计数工具
├── frontend/             # 前端源码（React + Semi Design）
│   └── src/
│       ├── api/          # API 请求
│       ├── components/   # 通用组件
│       ├── pages/        # 页面（Dashboard、Channels、Users 等）
│       ├── store/        # 状态管理
│       ├── test/         # 前端测试
│       ├── ui/           # PRD1 UI 基础层（页面头、面板、指标、状态/金额/延迟显示）
│       └── utils/        # 工具函数
├── web/
│   ├── admin/            # 管理后台构建产物（CDN 静态资源）
│   └── console/          # 控制台构建产物
├── migrations/           # SQL 迁移脚本
├── scripts/              # 验收/运维辅助脚本
├── docs/                 # 文档
│   ├── PROJECT-HANDOFF.md # Agent 接手入口
│   ├── CODEBASE-MAP.md   # 代码结构地图
│   ├── DOC-INVENTORY.md  # 文档清单和边界
│   ├── PRD-Phase1.md     # 产品需求文档
│   ├── PRD-Phase2.md     # PRD2 需求文档（冻结）
│   ├── PRD-Phase3.md     # PRD3 清理/优化范围（已完成）
│   ├── PRD-Phase4.md     # PRD4 范围（已完成）
│   ├── PRD-Phase5.md     # PRD5 范围（Accepted Scope Complete）
│   ├── PRD-Phase6.md     # PRD6 当前范围提案
│   ├── planning/         # PRD2-PRD6 规划和 backlog
│   ├── ops/              # 部署 & 测试
│   ├── tech-ref/         # 技术参考（模型矩阵、计费状态机等）
│   ├── agent-workflows/  # Agent 工作流
│   ├── consultations/    # 顾问咨询记录
│   ├── reviews/          # 代码评审
│   ├── testing/          # 测试文档
│   ├── issues/           # 问题跟踪
│   └── archive/          # 归档
├── trash/                # 本地无用调试产物隔离区；仅 README 入库，内容不参与运行/发布
├── .env.example          # 环境变量模板
├── Dockerfile            # 后端容器
├── docker-compose.yml    # Docker 编排
├── go.mod                # Go 模块
└── README.md
```

## 许可证

Copyright © 2024 Foigent. All Rights Reserved.

本软件为闭源商业软件，未经授权不得复制、修改或分发。
