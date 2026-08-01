# UniRouter

企业级 AI 模型管控平台 — 统一接入 + 按量计费 + 本地部署

下面的源码 `docker compose` 快速开始仅用于开发和内部测试。客户正式交付使用
签名包内的 `deploy/customer` 模板与预构建镜像，不需要也不应取得源码、
builder cache 或签名私钥。

## 快速开始

```bash
# 1. 克隆项目（先将内部仓库地址写入环境变量）
: "${UNIROUTER_REPO_URL:?Set the approved repository URL}"
git clone "$UNIROUTER_REPO_URL" UniRouter && cd UniRouter

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

# 6. 首次部署后必须立刻轮换默认管理员密码。
# 在交互终端中会无回显输入并确认两次；不要把密码放在命令参数或环境变量中。
docker compose exec backend ./uniroute admin set-password \
    --username admin --org-id 1
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
├── PostgreSQL 16
├── Redis 7
├── Prometheus
└── Grafana
```

## 技术栈

- **后端**：Go `net/http` + pgx + go-redis
- **数据库**：PostgreSQL 16 + Redis 7
- **前端**：React 18 + Semi Design + Vite

## 文档

- [客户正式安装](deploy/customer/README.md) — 签名发布包、digest 镜像与 source-free Compose 边界
- [部署与升级手册](docs/ops/deployment-runbook.md) — 开发/内部测试与客户交付的分离流程
- [商业授权](docs/tech-ref/16-license-foundation.md) 与 [签名离线升级](docs/tech-ref/27-signed-offline-upgrade.md) — V1 发布信任合同
- [OpenAPI 合同](docs/tech-ref/05-openapi-spec.yaml) — 当前网关与管理 API 路径
- [贡献指南](CONTRIBUTING.md) 与 [Agent 协议](docs/agent-workflows/AGENT-PROTOCOL.md) — 开发、验证、PR 与授权边界
- [当前执行记录](docs/agent-workflows/EXECUTION-RECORD.md) — 当前 PRD7 本地实施批次、证据与下一依赖；不重开 PRD5/PRD6
- [PRD7 B2 本地证据](docs/ops/evidence/PRD7-B2-P7F02-contextual-inspectors-local-20260731.md) — Audit/Projects 常驻检查器、深链、本地元数据视图与响应式验收
- [当前状态](docs/ops/CURRENT-STATUS.md) 与 [Open Issues](docs/issues/OPEN-ISSUES.md) — 已验证事实、缺口和后续决策
- [文档目录](docs/README.md) 与 [文档清单](docs/DOC-INVENTORY.md) — 技术参考、不可变证据与历史 PRD 索引
- [PRD6 最终完成证据](docs/ops/evidence/PRD6-FINAL-COMPLETION-20260714.md) — `Completed (Accepted Scope)` 的历史边界，不得静默重开

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
│       ├── ui/           # UI 基础层（页面头、面板、指标、状态/金额/延迟显示）
│       └── utils/        # 工具函数
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
│   ├── planning/         # 各阶段规划、研究和候选 backlog
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
