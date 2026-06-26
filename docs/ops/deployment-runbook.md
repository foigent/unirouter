# Deployment Runbook（部署运维手册）

> **版本**：v1.1
> **日期**：2026-06-19
> **用途**：运维人员部署和维护 UniRoute 的操作手册
> **变更记录**：v1.1 修正日志描述（标准库 log，非 zap）、容器名更新（backend/frontend）、Prometheus 标记为规划中

---

## 一、环境要求

| 组件 | 版本 | 说明 |
|------|------|------|
| Docker | 24+ | 容器运行时 |
| Docker Compose | 2.20+ | 编排工具 |
| 磁盘 | 50GB+ | PostgreSQL 数据 + 备份 |
| 内存 | 4GB+ | Go 二进制 + PG + Redis |
| CPU | 2 核+ | 建议 4 核 |

---

## 二、首次部署

### 2.1 准备配置文件

```bash
cd /opt/uniroute
cp .env.example .env

# 编辑 .env，必须修改以下项：
# - DB_DSN: PostgreSQL 连接串
# - JWT_SECRET: 32 字节随机字符串
# - ENCRYPTION_KEY: 32 字节随机字符串
# - MIGRATIONS_DIR: 迁移文件目录（默认 /app/migrations）

# 生成随机密钥
openssl rand -hex 32  # 用于 JWT_SECRET
openssl rand -hex 32  # 用于 ENCRYPTION_KEY
```

### 2.2 启动服务

```bash
docker compose up -d
```

服务包含 4 个容器：
- `backend` — Go API 服务（端口 8080）
- `frontend` — Nginx 静态文件 + 反向代理（端口 3000）
- `postgres` — PostgreSQL 数据库（内部端口 5432）
- `redis` — Redis 缓存（内部端口 6379）

### 2.3 验证部署

```bash
# 检查容器状态
docker compose ps

# 检查健康状态
curl http://localhost:8080/health/ready
# 期望：{"status":"ok","db":"ok","redis":"ok"}

# 检查前端
curl -I http://localhost:3000
# 期望：HTTP/1.1 200 OK

# 检查日志
docker compose logs -f backend
```

### 2.4 初始化管理员

```bash
# 创建平台超管（首次部署）
docker compose exec backend ./uniroute admin create \
    --username admin \
    --password <your-password> \
    --role platform_admin
```

---

## 三、数据库迁移

### 3.1 自动迁移

服务启动时自动执行迁移（`internal/migrate/migrate.go`）。日志输出：

```
Connected to PostgreSQL
Migrations complete
```

### 3.2 手动迁移

```bash
# 查看当前迁移版本
docker compose exec backend ./uniroute migrate status
```

### 3.3 回滚迁移

```bash
# 回滚最近一次迁移
docker compose exec backend ./uniroute migrate down
```

---

## 四、备份与恢复

### 4.1 手动备份

```bash
# 备份 PostgreSQL
docker compose exec postgres pg_dump -U uniroute uniroute > backup_$(date +%Y%m%d_%H%M%S).sql

# 备份 Redis（AOF 文件）
docker compose exec redis redis-cli BGSAVE
docker compose cp redis:/data/dump.rdb ./backups/redis_$(date +%Y%m%d_%H%M%S).rdb
```

### 4.2 恢复

```bash
# 恢复 PostgreSQL
cat backup.sql | docker compose exec -T postgres psql -U uniroute uniroute

# 恢复 Redis
docker compose cp ./backups/dump.rdb redis:/data/dump.rdb
docker compose restart redis
```

---

## 五、日志管理

### 5.1 日志格式

当前使用 Go 标准库 `log`，输出纯文本到 stdout，格式：

```
2026/06/19 15:30:00 [req_a1b2c3d4] Billing freeze error: insufficient balance
```

### 5.2 查看日志

```bash
# 实时日志
docker compose logs -f backend

# 按时间筛选
docker compose logs --since "2026-06-19T00:00:00" backend

# 按 request_id 搜索
docker compose logs backend | grep "req_a1b2c3d4"
```

### 5.3 后续规划

以下功能尚未实现，列入后续版本：

- 结构化 JSON 日志（zap/slog）
- 日志级别控制
- 请求延迟、org_id 等结构化字段

---

## 六、监控

### 6.1 当前可用

```bash
# 健康检查
curl http://localhost:8080/health/ready
# 期望：{"status":"ok","db":"ok","redis":"ok"}

# 通过管理 API 查看系统状态
curl http://localhost:8080/api/v1/dashboard/stats \
    -H "Authorization: Bearer <JWT>"
# 返回：请求统计、模型价格、最近日志
```

### 6.2 后续规划：Prometheus Metrics

以下功能尚未实现，列入后续版本：

- `/metrics` 端点暴露 Prometheus 指标
- 关键指标：请求数、延迟、冻结/结算次数、错误率、熔断器状态
- Grafana Dashboard 模板

---

## 七、故障排查

### 7.1 服务无法启动

```bash
# 检查容器日志
docker compose logs backend

# 常见原因：
# - DB_DSN 连接失败 → 检查 PostgreSQL 容器状态
# - REDIS_ADDR 连接失败 → 检查 Redis 容器状态
# - JWT_SECRET 未设置 → 检查 .env 文件
```

### 7.2 请求返回 502

```bash
# 检查上游渠道状态
curl http://localhost:8080/api/v1/channels \
    -H "Authorization: Bearer <JWT>"

# 检查熔断器状态
docker compose logs backend | grep "circuit"
```

### 7.3 余额异常

```bash
# 检查钱包状态
docker compose exec postgres psql -U uniroute -c \
    "SELECT * FROM wallets WHERE org_id = <org_id>;"

# 检查冻结记录
docker compose exec postgres psql -U uniroute -c \
    "SELECT * FROM freeze_idempotency WHERE status = 'Processing';"

# 检查交易记录
docker compose exec postgres psql -U uniroute -c \
    "SELECT * FROM transactions WHERE org_id = <org_id> ORDER BY created_at DESC LIMIT 20;"
```

### 7.4 前端无法访问

```bash
# 检查 frontend 容器
docker compose logs frontend

# 常见原因：
# - backend 容器未启动 → 检查 docker compose ps
# - nginx 配置错误 → docker compose exec frontend nginx -t
# - SPA 路由 404 → 检查 try_files 配置
```

---

## 八、升级流程

```bash
# 1. 备份数据库
docker compose exec postgres pg_dump -U uniroute uniroute > pre_upgrade_backup.sql

# 2. 拉取新镜像
docker compose pull

# 3. 重启服务（迁移自动执行）
docker compose up -d

# 4. 验证
curl http://localhost:8080/health/ready
```

---

## 九、安全加固

| 措施 | 说明 |
|------|------|
| HTTPS | Nginx 反向代理 + Let's Encrypt SSL |
| 防火墙 | 只开放 80/443 端口 |
| 数据库 | 不暴露外部端口，只通过 Docker 网络访问 |
| Redis | 设置密码，不暴露外部端口 |
| 密钥 | JWT_SECRET 和 ENCRYPTION_KEY 定期轮换 |
| 备份 | 备份文件加密存储 |
