# AGENTS.md — UniRoute 开发规范

## 项目概览

UniRoute 是企业级 AI 模型管控平台（MaaS），基于 OneAPI（Go, MIT）Deep Fork。
模块化单体架构，单个 Go 二进制，Docker Compose 部署。

## 技术栈

- Go 1.23+
- net/http（Go 标准库 HTTP）
- pgx（PostgreSQL 驱动）
- go-redis/v9（Redis 客户端）
- log（标准库日志，未来可升级 zap）
- React 18 + Semi Design + Vite（前端）

## 开发约定

### 代码风格

- Go 标准 `gofmt` 格式
- 命名：camelCase（Go），snake_case（SQL/环境变量）
- 错误处理：所有错误必须显式处理，不允许 `_ = err`
- 日志：使用 log，建议包含 `request_id` / `org_id` 字段
- 注释：导出函数必须有 godoc 注释

### 目录结构

```
cmd/uniroute/         # 入口，只做初始化和启动
internal/
  config/             # 配置加载（从环境变量读取）
  models/             # 数据模型（struct + SQL）
  billing/            # 计费模块（Freeze/Settle/Cancel）
  middleware/          # 中间件（auth, ratelimit, cors）
  admin/              # 管理后台 API handler
  gateway/            # 网关路由 + relay
migrations/           # SQL 迁移文件
frontend/              # 前端资源（React + Vite）
```

### 数据库

- 迁移文件命名：`{6位数字}_{描述}.{up|down}.sql`
- 所有 DDL 在事务中执行（`BEGIN; ... COMMIT;`）
- 余额字段使用 `CHECK (>= 0)` 约束
- 不使用 ORM，直接写 SQL（pgx）
- 金额单位：分（integer），不是元（float）

### API 设计

- Gateway 端点：100% 兼容 OpenAI API 格式
- Admin 端点：RESTful JSON，JWT 认证
- 错误码：`{category}.{specific}` 两段式
- 幂等性：充值/调账/赠送使用 `client_request_id`
- 分页：`page` + `page_size` 参数

### 安全

- API Key：HMAC-SHA256 哈希存储，不存明文
- 上游 Key：AES-256-GCM 加密存储
- JWT：HS256，24 小时过期
- 密钥来源：环境变量（JWT_SECRET, ENCRYPTION_KEY）

### 计费

- PostgreSQL 是余额唯一真相源
- Redis 做热数据缓存（AOF always）
- 预冻结机制：请求前冻结，完成后结算
- 扣费优先级：先 granted_balance → 再 balance
- 流式请求：每 100 token 检查余额

### 测试

- 单元测试：`_test.go` 文件，同目录
- 集成测试：使用 testcontainers（PG + Redis）
- 基准测试：`go test -bench`

### Git

- 提交信息：`type(scope): description`
  - type: feat / fix / refactor / docs / test / chore
- 分支：main（稳定）+ feature/*（开发）
- PR 必须通过 CI 才能合并

## 关键参考文档

开发前必读：
- `docs/PRD-Phase1.md` — 产品需求（source of truth）
- `docs/tech-ref/02-billing-state-machine.md` — 计费逻辑
- `docs/tech-ref/03-error-handling-matrix.md` — 错误处理
- `docs/tech-ref/04-oneapi-hook-manifest.md` — OneAPI 注入点
- `docs/tech-ref/05-openapi-spec.yaml` — API 定义
- `migrations/000001_init_schema.up.sql` — 数据库 Schema
- `.env.example` — 所有配置项

## 环境变量

所有配置通过环境变量注入，见 `.env.example`。
代码中不允许硬编码任何密钥、地址或端口。
