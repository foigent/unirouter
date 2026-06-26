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

- **后端**：Go + Gin + pgx + go-redis
- **数据库**：PostgreSQL 15 + Redis 7
- **前端**：React 18 + Semi Design + Vite
- **基础**：OneAPI（Go, MIT）Deep Fork

## 文档

- [贡献指南](CONTRIBUTING.md) — 开发流程、代码规范、提交规范
- [PRD Phase 1](docs/PRD-Phase1.md) — 产品需求文档（source of truth）
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
│   ├── config/           # 配置加载
│   ├── models/           # 数据模型
│   ├── billing/          # 计费模块
│   ├── middleware/        # 中间件（鉴权、限流）
│   ├── admin/            # 管理后台 API
│   └── gateway/          # 网关路由
├── frontend/             # 前端源码（React + Semi Design）
│   └── src/pages/
├── web/admin/            # 前端构建产物（CDN 静态资源）
├── migrations/           # 数据库迁移
├── docs/                 # 文档
│   ├── PRD-Phase1.md     # 产品需求文档
│   ├── ops/              # 运维文档
│   ├── tech-ref/         # 技术参考
│   ├── issues/           # 待处理问题
│   └── consultations/    # 顾问咨询记录
├── .env.example          # 环境变量模板
├── Dockerfile            # 后端容器
├── docker-compose.yml    # Docker 编排
├── go.mod                # Go 模块
└── README.md
```

## 许可证

Copyright © 2024 Foigent. All Rights Reserved.

本软件为闭源商业软件，未经授权不得复制、修改或分发。
