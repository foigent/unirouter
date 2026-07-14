# UniRoute

企业级 AI 模型管控平台 — 统一接入 + 按量计费 + 本地部署

下面的源码 `docker compose` 快速开始仅用于开发和内部测试。客户正式交付使用
签名包内的 `deploy/customer` 模板与预构建镜像，不需要也不应取得源码、
builder cache 或签名私钥。

## 快速开始

```bash
# 1. 克隆项目
git clone <repo-url> && cd uniroute

# 2. 配置环境变量
cp .env.example .env
# 编辑 .env，修改 JWT_SECRET、API_KEY_HASH_SECRET、ENCRYPTION_KEY、GRAFANA_ADMIN_PASSWORD。
# 源码 Compose 仅用于开发；可选 LICENSE_PUBLIC_KEYS/UPGRADE_PUBLIC_KEYS 也是开发验签输入。
# 正式商业镜像把两类公钥信任根嵌入二进制，运行时环境变量不能替换。
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

服务可以在缺少商业 License 时启动，以保留本地登录、健康检查和 License
导入恢复面；但 Chat、Embeddings、Models 与 Agent capability 新请求会返回
`license_error.*`，不会进入 API Key、计费或上游。客户交付应使用不含源码和
builder cache 的最终镜像/制品，并在“商业授权”页导入由发行方离线签发、与
`LICENSE_DEPLOYMENT_ID` 精确绑定的 `unirouter.license.v1` 文档。

发行方日常生成和签发不需要终端：macOS 使用可双击的
`UniRouter License Studio.app`。它通过本机文件选择器生成 License/升级包两套
独立 Ed25519 密钥、填写客户授权、签发并复核；私钥不进入浏览器、客户部署或
UniRouter 数据库。构建与使用说明见
[商业授权技术参考](docs/tech-ref/16-license-foundation.md)。

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
- [PRD6 收口入口](docs/planning/PRD6-START-HERE.md) — Completed (Accepted Scope)、决策边界和阅读顺序
- [PRD6 产品范围](docs/PRD-Phase6.md) — 已完成 accepted scope、proposed/deferred、安全边界和所有权
- [PRD6 验收矩阵](docs/ops/PRD6-ACCEPTANCE-MATRIX.md) — 27/27 accepted rows、外部和目标证据边界
- [PRD6 收口证据](docs/ops/evidence/PRD6-ACCEPTED-SCOPE-CLOSEOUT-20260713.md) — repository/CI 证据等级、非声明与后续队列
- [PRD6 最终完成证据](docs/ops/evidence/PRD6-FINAL-COMPLETION-20260714.md) — 新鲜 Git/CI/矩阵/目标健康审计、正式完成状态与后续边界
- [PRD6 竞品与四象限](docs/planning/PRD6-COMPETITOR-ALIGNMENT-20260711.md) — 固定版本竞品核验和优先级
- [PRD6 P6-F16 本地证据](docs/ops/evidence/PRD6-B0B-P6F16-tenant-isolation-local-20260711.md) — key/user/project/project-policy 租户边界修复与回归
- [PRD6 管理面租户审计](docs/ops/evidence/PRD6-B0B-admin-tenant-surface-audit-20260711.md) — 财务、通知、价格、全局设置和模型治理的后续 confirmed findings
- [PRD6 P6-F33 本地证据](docs/ops/evidence/PRD6-B0B-P6F33-notification-tenant-isolation-local-20260711.md) — 通知/Webhook 租户隔离修复与红绿回归
- [PRD6 P6-F37 本地证据](docs/ops/evidence/PRD6-B0B-P6F37-audit-log-tenant-isolation-local-20260711.md) — 审计日志租户隔离修复与双租户回归
- [PRD6 P6-F38 本地证据](docs/ops/evidence/PRD6-B0B-P6F38-frontend-test-ci-local-20260711.md) — 现有前端测试纳入 required CI
- [PRD6 P6-F32/P6-F39 本地证据](docs/ops/evidence/PRD6-B0B-P6F32-P6F39-finance-and-usage-tenant-isolation-local-20260711.md) — 财务、请求日志和用量派生视图的租户隔离
- [PRD6 流式传输本地证据](docs/ops/evidence/PRD6-B0C-P6STREAM002A-stream-transport-local-20260712.md) — 长流帧、超时、写入串行化、异常结算及剩余边界
- [PRD6 产品/架构/UX 审计](docs/planning/PRD6-PRODUCT-ARCHITECTURE-UX-AUDIT-20260711.md) — 审计日志、用量核验、链路 ID、UI、扩展性与升级建议
- [V1 商业授权与签名升级合同](docs/planning/V1-COMMERCIAL-LICENSE-AND-SIGNED-UPGRADE-CONTRACT-20260714.md) — 交付前强制门槛、非声明与目标激活边界
- [PRD6 高可用与流式故障研究](docs/planning/PRD6-HA-AND-STREAM-FAILOVER-RESEARCH-20260712.md) — 竞品集群模式、UniRouter 当前事实和已接受 D6-12 边界
- [PRD7 候选 Backlog](docs/planning/PRD7-CANDIDATE-BACKLOG.md) — Post-PRD6 最低签名包基础与仍未接受的 Update Center/自动化/N-1 产品化边界
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
  - [16 商业授权强制](docs/tech-ref/16-license-foundation.md)
  - [27 签名离线升级包](docs/tech-ref/27-signed-offline-upgrade.md)
- [运维文档](docs/ops/) — 部署和测试
  - [部署手册](docs/ops/deployment-runbook.md)
  - [测试计划](docs/ops/test-plan.md)

## 项目结构

```
UniRouter/
├── cmd/uniroute/         # 入口
│   └── main.go
├── cmd/uniroute-license-studio/ # 发行方 macOS 图形化 License 工具
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
│   ├── licensing/        # 商业 License 签名、部署/时间/功能/限额强制
│   ├── licensestudio/    # 本机 GUI、macOS 文件选择和安全文件操作
│   ├── releasetrust/     # 正式镜像编译时嵌入、运行时不可替换的公钥信任根
│   ├── signing/          # Ed25519 密钥和公钥环严格解析
│   ├── upgradebundle/    # 确定性签名离线升级包与流式验证
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
├── deploy/customer/      # 不含 build/source mount 的客户最终制品 Compose 模板
├── docs/                 # 文档
│   ├── PROJECT-HANDOFF.md # Agent 接手入口
│   ├── CODEBASE-MAP.md   # 代码结构地图
│   ├── DOC-INVENTORY.md  # 文档清单和边界
│   ├── PRD-Phase1.md     # 产品需求文档
│   ├── PRD-Phase2.md     # PRD2 需求文档（冻结）
│   ├── PRD-Phase3.md     # PRD3 清理/优化范围（已完成）
│   ├── PRD-Phase4.md     # PRD4 范围（已完成）
│   ├── PRD-Phase5.md     # PRD5 范围（Accepted Scope Complete）
│   ├── PRD-Phase6.md     # PRD6 范围（Completed / Accepted Scope）
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

商业交付不依赖“混淆即可防破解”的错误假设：离线 License 在请求入口强制，升级
制品独立签名并防篡改/回滚；源码、签名私钥和构建缓存不进入客户交付包。拥有主机
root 或源码的一方最终仍可修改本地二进制，因此技术控制必须与合同、授权审计、
签名更新和受控支持渠道共同使用。
