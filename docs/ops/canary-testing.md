# Canary Testing Guide（灰度测试指南）

> **版本**：v1.0
> **日期**：2026-06-27
> **适用场景**：新版代码上线前的安全验证流程

---

## 一、什么是 Canary Testing

Canary（金丝雀）测试是一种渐进式发布策略：将少量流量导到新版本，观察其行为是否正常，再逐步扩大流量比例。

在 UniRouter 中，canary 测试用于验证：
- 新版代码的 API 兼容性
- 计费逻辑的正确性（Freeze/Settle/Cancel）
- 上游渠道的稳定性
- 流式响应的完整性

---

## 二、架构

```
客户端
  ↓
Nginx (canary 权重路由)
  ├── /v1/*, /api/* → upstream backend_canary
  │     ├── 90% → uniroute-api:8080        (stable)
  │     └── 10% → uniroute-api-canary:8080  (canary)
  ├── /health/* → uniroute-api:8080          (直连 stable)
  └── /* → uniroute-frontend:80              (静态资源)

注：测试环境走 HTTP:80；生产环境启用 TLS 后走 HTTPS:443
```

### 关键设计决策

| 决策 | 原因 |
|------|------|
| canary 跳过 migration | 共享 DB，由 stable 管理 schema |
| 健康检查直连 stable | canary 是临时实例，不应影响监控 |
| 权重在 Nginx 层 | 运维可热调整，不改代码 |
| 前端独立 nginx | 静态资源与 API 路由解耦 |

---

## 三、前置条件

1. `docker-compose.yml` 中的 stable 实例正常运行
2. `curl http://localhost:8080/health/ready` 返回 `{"status":"ok"}`
3. 数据库已备份（可选但推荐）
4. **⚠️ 无 TLS 证书时**：确认 `nginx/nginx.conf` 中 HTTPS redirect 已注释，且 HTTPS server block 已注释（详见 deployment-runbook.md 第九章）

---

## 四、启动 Canary

```bash
cd /opt/uniroute

# 启动 canary（叠加配置）
docker compose -f docker-compose.yml -f docker-compose.canary.yml up -d

# 验证所有容器运行
docker compose -f docker-compose.yml -f docker-compose.canary.yml ps
```

预期输出（6 个容器）：

```
NAME                 STATUS
uniroute-api         Up (healthy)
uniroute-api-canary  Up (healthy)
uniroute-frontend    Up
uniroute-nginx       Up
uniroute-postgres    Up (healthy)
uniroute-redis       Up (healthy)
```

---

## 五、验证清单

### 5.1 基础连通性

```bash
# 通过 Nginx 访问
curl http://localhost/health/ready
# 期望：{"status":"ok","db":"ok","redis":"ok"}

# 前端页面
curl -I http://localhost/
# 期望：HTTP/1.1 200 OK
```

### 5.2 API 兼容性

```bash
# Gateway 端点
curl http://localhost/v1/models \
  -H "Authorization: Bearer sk-your-key"
# 期望：返回可用模型列表

# Admin 端点
curl http://localhost/api/v1/channels \
  -H "Authorization: Bearer <JWT>"
# 期望：返回渠道列表
```

### 5.3 流式响应完整性

```bash
# 测试 SSE 流式请求
curl -N http://localhost/v1/chat/completions \
  -H "Authorization: Bearer sk-your-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "deepseek-chat",
    "messages": [{"role": "user", "content": "说三句话"}],
    "stream": true
  }'
# 期望：收到多个 data: 行，最后以 data: [DONE] 结束
```

### 5.4 计费准确性

```bash
# 记录测试前的余额
curl http://localhost/api/v1/wallets/<org_id> \
  -H "Authorization: Bearer <JWT>" | jq '.balance'

# 发送测试请求
curl http://localhost/v1/chat/completions \
  -H "Authorization: Bearer sk-your-key" \
  -H "Content-Type: application/json" \
  -d '{"model":"deepseek-chat","messages":[{"role":"user","content":"hello"}]}'

# 检查余额变化
curl http://localhost/api/v1/wallets/<org_id> \
  -H "Authorization: Bearer <JWT>" | jq '.balance'

# 检查调用日志
docker compose exec postgres psql -U uniroute -c \
  "SELECT id, model, usage_source, prompt_tokens, completion_tokens, total_cost_cents \
   FROM call_logs ORDER BY created_at DESC LIMIT 5;"
```

### 5.5 错误处理

```bash
# 无效 API Key
curl http://localhost/v1/chat/completions \
  -H "Authorization: Bearer sk-invalid" \
  -H "Content-Type: application/json" \
  -d '{"model":"deepseek-chat","messages":[{"role":"user","content":"test"}]}'
# 期望：401 Unauthorized

# 无效模型
curl http://localhost/v1/chat/completions \
  -H "Authorization: Bearer sk-your-key" \
  -H "Content-Type: application/json" \
  -d '{"model":"nonexistent","messages":[{"role":"user","content":"test"}]}'
# 期望：404 Model not found
```

---

## 六、监控与告警

### 6.1 实时日志

```bash
# Stable 实例日志
docker compose logs -f backend

# Canary 实例日志
docker compose -f docker-compose.yml -f docker-compose.canary.yml logs -f backend-canary
```

### 6.2 错误率对比

```bash
# 过去 5 分钟的错误数
echo "=== Stable ==="
docker compose logs backend --since 5m 2>&1 | grep -ci "error"

echo "=== Canary ==="
docker compose -f docker-compose.yml -f docker-compose.canary.yml \
  logs backend-canary --since 5m 2>&1 | grep -ci "error"
```

### 6.3 关键指标

| 指标 | 告警阈值 | 检查方式 |
|------|----------|----------|
| canary 错误率 > stable | 任何差异 | 日志对比 |
| canary 响应时间 > 2x stable | P99 延迟 | 请求计时 |
| 计费金额不一致 | 任何差异 | call_logs 对比 |
| canary 内存/CPU 异常 | > 80% | `docker stats` |

---

## 七、调整 Canary 权重

编辑 `nginx/nginx.conf`：

```nginx
upstream backend_canary {
    server uniroute-api:8080        weight=9;   # stable
    server uniroute-api-canary:8080 weight=1;   # canary
}
```

常用比例：

| 权重 | canary 流量 | 适用场景 |
|------|-------------|----------|
| 9:1 | 10% | 初始验证 |
| 3:1 | 25% | 初步验证通过 |
| 1:1 | 50% | 发布前验证 |
| 1:9 | 90% | 最终验证 |

热重载 Nginx（无需重启容器）：

```bash
docker compose exec nginx nginx -s reload
```

---

## 八、回滚

如果 canary 出现问题：

```bash
# 方式 1：停止 canary（推荐）
docker compose -f docker-compose.yml -f docker-compose.canary.yml stop backend-canary
docker compose -f docker-compose.yml -f docker-compose.canary.yml rm -f backend-canary

# 方式 2：调整权重为 0（保留容器用于调试）
# 编辑 nginx/nginx.conf，将 canary weight 设为 0
docker compose exec nginx nginx -s reload

# 验证流量回到 stable
curl http://localhost/v1/models -H "Authorization: Bearer sk-your-key"
```

---

## 九、Canary 验证通过后的升级流程

```bash
# 1. 停止 canary
docker compose -f docker-compose.yml -f docker-compose.canary.yml stop backend-canary

# 2. 备份数据库
docker compose exec postgres pg_dump -U uniroute uniroute > pre_upgrade_$(date +%Y%m%d).sql

# 3. 更新 stable 代码
cd /opt/uniroute
git pull origin main

# 4. 重建 stable
docker compose build backend
docker compose up -d backend

# 5. 验证
curl http://localhost:8080/health/ready

# 6. 清理 canary 配置（可选）
docker compose -f docker-compose.yml -f docker-compose.canary.yml down
```

---

## 十、故障排查

### Canary 容器启动失败

```bash
docker compose -f docker-compose.yml -f docker-compose.canary.yml logs backend-canary

# 常见原因：
# - JWT_SECRET/ENCRYPTION_KEY 未设置 → 检查 .env
# - PostgreSQL 未就绪 → 检查 depends_on 条件
# - 端口冲突 → canary 不应暴露端口
```

### Nginx 返回 502

```bash
# 检查 upstream 状态
docker compose exec nginx nginx -T | grep -A5 "upstream"

# 检查 canary 容器网络
docker compose -f docker-compose.yml -f docker-compose.canary.yml exec backend-canary \
  wget -qO- http://localhost:8080/health/ready
```

### 流式响应中断

```bash
# 检查 Nginx SSE 配置
docker compose exec nginx nginx -T | grep "proxy_buffering"
# 期望：proxy_buffering off;

# 检查超时配置
docker compose exec nginx nginx -T | grep "proxy_read_timeout"
# 期望：proxy_read_timeout 300s;
```

---

## 附录：环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `SKIP_MIGRATION` | `false` | canary 必须设为 `true` |
| `CANARY_LOG_LEVEL` | `debug` | canary 日志级别（便于排查） |
| `DB_DSN` | 见 .env.example | canary 复用 stable 的数据库连接 |
| `JWT_SECRET` | 必填 | canary 必须与 stable 相同 |
| `ENCRYPTION_KEY` | 必填 | canary 必须与 stable 相同 |
