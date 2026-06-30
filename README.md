# UniRoute

企业级 AI 模型管控平台 — 统一接入 + 按量计费 + 本地部署

## 快速开始

```bash
# 1. 克隆项目
git clone <repo-url> && cd uniroute

# 2. 配置环境变量
cp .env.example .env
# 编辑 .env，修改 JWT_SECRET 和 ENCRYPTION_KEY
openssl rand -hex 32  # 生成密钥

# 3. 启动服务
docker compose up -d

# 4. 验证
curl http://localhost:8080/health/ready
# {"status":"ok","db":"ok","redis":"ok"}

# 5. 执行数据库迁移
docker compose exec uniroute ./uniroute migrate

# 6. 创建管理员
docker compose exec uniroute ./uniroute admin create \
    --username admin --password <your-password> --role platform_admin
```

## 架构

```
Docker Compose（3 个容器）：
├── uniroute（单个 Go 二进制）
│   ├── /v1/*       → OpenAI 兼容网关
│   ├── /api/v1/*   → 管理后台 API
│   └── /health/*   → 健康检查
├── PostgreSQL 15
└── Redis 7（AOF always）
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
- [PRD1 收尾计划](docs/ops/PRD1-CLOSEOUT-PLAN.md) — PRD1 到 PRD2 的退出标准和收尾项
- [PRD 验收矩阵](docs/ops/PRD-ACCEPTANCE-MATRIX.md) — Phase 1 验收进度
- [PRD1 UI Redesign](docs/design/PRD1-UI-REDESIGN-SPEC.md) — PRD1 控制台视觉重设计和前端 UI 基础层
- [Open Issues](docs/issues/OPEN-ISSUES.md) — 当前未关闭问题
- [Agent 协议](docs/agent-workflows/AGENT-PROTOCOL.md) — Codex/OpenClaw 协作和验收流程
- [PRD Phase 1](docs/PRD-Phase1.md) — 产品需求文档（需求和验收标准的 source of truth）
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
│   ├── filter/           # 请求过滤
│   ├── gateway/          # 网关路由 & 中继
│   ├── middleware/        # HTTP 中间件（鉴权、限流、CORS）
│   ├── migrate/          # 数据库迁移执行器
│   ├── models/           # 数据模型
│   ├── ratelimit/        # 限流实现
│   ├── redisx/           # Redis 封装
│   ├── testutil/         # 测试工具
│   └── tokenutil/        # Token 计数工具
├── frontend/             # 前端源码（React + Semi Design）
│   └── src/
│       ├── api/          # API 请求
│       ├── components/   # 通用组件
│       ├── hooks/        # 自定义 hooks
│       ├── pages/        # 页面（Dashboard、Channels、Users 等）
│       ├── store/        # 状态管理
│       ├── test/         # 前端测试
│       ├── ui/           # PRD1 UI 基础层（页面头、面板、指标、状态/金额/延迟显示）
│       └── utils/        # 工具函数
├── web/
│   ├── admin/            # 管理后台构建产物（CDN 静态资源）
│   └── console/          # 控制台构建产物
├── migrations/           # SQL 迁移脚本
├── docs/                 # 文档
│   ├── PROJECT-HANDOFF.md # Agent 接手入口
│   ├── CODEBASE-MAP.md   # 代码结构地图
│   ├── DOC-INVENTORY.md  # 文档清单和边界
│   ├── PRD-Phase1.md     # 产品需求文档
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
