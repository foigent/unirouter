# UniRouter 全面测试报告 — 最终版（完整）

**日期：** 2026-07-08
**环境：** 阿里云 2c4g `http://8.130.18.85`（PRD4 Batch 1N，PR#98）
**测试方法：** 基于实际 API 响应、浏览器截图、压测数据、代码审计，不猜测，不幻觉
**测试范围：** 环境健康、安全、API 功能、前端 UI、压力测试、数据一致性、依赖漏洞

---

## 一、环境健康检查 ✅

| 服务 | 端口 | 状态 | 备注 |
|------|------|------|------|
| Backend | :8080 | ✅ 200 | `{"status":"ok","db":"ok","redis":"ok"}` |
| Frontend | :3000 | ✅ 200 | HTML 正常加载 |
| Prometheus | :9090 | ✅ 200 | healthy |
| Grafana | :3001 | ⚠️ 外部不可达 | 容器内 URL 配置错误 |
| Metrics | :8080/metrics | ✅ 200 | Prometheus 文本格式 |

**Docker（Atlas 确认）：** 8 容器运行中，`uniroute-api` 无 healthy 标记。

---

## 二、安全测试

### 发现的安全问题

| # | 严重性 | 问题 | 证据 |
|---|--------|------|------|
| 1 | 🔴 HIGH | 存储型 XSS — Key 名称接受 `<script>` | key id=33 原样存储；React 当前转义 |
| 2 | 🟡 MEDIUM | SQL 注入 payload 被原样存储 | 参数化查询正确阻止执行 |
| 3 | 🟡 MEDIUM | 无安全响应头 | 缺 X-Content-Type-Options, X-Frame-Options, CSP |
| 4 | 🟡 MEDIUM | Go 标准库21个漏洞 | govulncheck 发现，需升级 Go 到1.25.11+ |
| 5 | 🟢 LOW | 404 catch-all 暴露版本 | 未知路径返回版本信息 |
| 6 | 🟢 LOW | Grafana URL 错误 | localhost vs grafana:3000 |
| 7 | 🟢 LOW | 版本/commit 未注入 | Dockerfile 无 ldflags |
| 8 | 🟢 LOW | 环境未配置 | RUNTIME_ENVIRONMENT 未设置 |

### Go 依赖漏洞详情（govulncheck）

| 漏洞 | 包 | 修复版本 |
|------|-----|----------|
| GO-2026-5039 | net/textproto | go1.25.11 |
| GO-2026-5037 | crypto/x509 | go1.25.11 |
| GO-2026-4971 | net | go1.25.10 |
| GO-2026-4947 | crypto/x509 | go1.25.9 |
| GO-2026-4946 | crypto/x509 | go1.25.9 |
| GO-2026-4918 | net/http | go1.25.10 |
| GO-2026-4870 | crypto/tls | go1.25.9 |
| GO-2026-4602 | os | go1.25.8 |
| GO-2026-4601 | net/url | go1.25.8 |
| GO-2026-4341 | net/url | go1.25.6 |
| + 11 more | 各种标准库 | go1.25.6+ |

**npm 依赖漏洞：** 0（`npm audit` clean）

### 安全测试通过项

| 测试项 | 结果 |
|--------|------|
| 默认密码拦截 | ✅ 401 |
| 暴力破解防护 | ✅ 8次后429 |
| SQL 注入防护 | ✅ 参数化查询 |
| 无token访问 | ✅ 401 |
| 伪造token | ✅ 401 |
| 路径遍历 | ✅ |
| CORS 收敛 | ✅ |
| API Key 不泄露 | ✅ |
| React XSS 转义 | ✅ |
| Trace metadata 安全 | ✅ |
| RBAC 隔离 | ✅ |

---

## 三、API 功能回归（完整）

### 所有已测试端点

| 端点 | 方法 | 状态 | 备注 |
|------|------|------|------|
| /api/v1/auth/login | POST | ✅ | JWT HS256 |
| /api/v1/users | GET | ✅ | 3 用户 |
| /api/v1/users | POST | ✅ | 创建成功 |
| /api/v1/users/{id} | DELETE | ✅ | 删除成功 |
| /api/v1/projects | GET | ✅ | 1 项目 |
| /api/v1/keys | GET | ✅ | 33 key + owner/type |
| /api/v1/keys | POST | ✅ | 创建成功 |
| /api/v1/keys/{id} | PUT | ✅ | 更新成功 |
| /api/v1/keys/{id} | DELETE | ⚠️ 405 | 设计如此 |
| /api/v1/channel-keys | GET | ✅ | 4 上游凭证 |
| /api/v1/model-catalog | GET | ✅ | 11 模型 |
| /api/v1/runtime/status | GET | ✅ | CPU/disk/network |
| /api/v1/dashboard/stats | GET | ✅ | 4 渠道健康 |
| /api/v1/usage/analytics | GET | ✅ | 汇总+分组 |
| /api/v1/usage/analytics?scope=my | ✅ | 过滤正确 |
| /api/v1/usage/analytics?group_by=alias | ✅ | 5 行模型分组 |
| /api/v1/logs | GET | ✅ | 149 条 |
| /api/v1/logs/{id}/trace | GET | ✅ | 6 trace events |
| /api/v1/audit-logs | GET | ✅ | 87 条 |
| /v1/chat/completions | POST | ✅ | 非流式+流式 |
| /health/ready | GET | ✅ | db+redis ok |
| /health/live | GET | ✅ | |
| /metrics | GET | ✅ | Prometheus 格式 |
| /api/v1/orgs | GET | ⚠️ | 返回 catch-all |
| /api/v1/models | GET | ⚠️ | 正确端点是 model-catalog |

---

## 四、Gateway Proxy 测试

| 场景 | 结果 | 证据 |
|------|------|------|
| 无 Key | ✅ 401 | |
| 无效 Key | ✅ 401 | |
| 已禁用 Key | ✅ 401 | |
| 非流式 (mimo-v2.5) | ✅ 200 | 262 tokens, 1 cent |
| 流式 (mimo-v2.5) | ✅ SSE | 正确事件流 |
| Call log 记录 | ✅ | request_id/model/status/tokens/cost |
| Trace events | ✅ | 6 events 全生命周期 |
| Billing 结算 | ✅ | settled, charged=1 cent |
| 压测 5并发 10请求 | ✅ 0失败 | 6.4 QPS, 780ms 均延迟 |
| 压测 10并发 30请求 | ⚠️ 18失败 | RPS 限速生效 (10 RPS/key) |

---

## 五、前端 UI 回归（完整）

### 所有已验证页面（21页面 + 详细内容验证）

| 页面 | URL | 状态 | 备注 |
|------|-----|------|------|
| 登录 | /login | ✅ | |
| Dashboard | / | ✅ | KPI/趋势/渠道/最近请求 |
| 访问凭证 | /keys | ✅ | 33 key 分页 |
| 用户管理 | /users | ✅ | 3 用户 |
| 上游通道 | /channels | ✅ | 4 渠道（deepseek/kimi/mimo/Mimo Token Plan） |
| 上游凭证 | /channel-keys | ✅ | |
| 请求日志 | /logs | ✅ | 182条，筛选器+分页 |
| 用量分析 | /usage-analytics | ✅ | 分组/筛选/CSV导出 |
| 通知中心 | /notifications | ✅ | |
| 连通性检测 | /connectivity | ✅ | |
| 模型治理 | /model-governance | ✅ | |
| 资金账户 | /wallet | ✅ | 余额¥8.82，入账/调整额度按钮 |
| 模型定价 | /model-pricing | ✅ | |
| 价格策略 | /price-strategy | ✅ | |
| 用量对账 | /usage-reconciliation | ✅ | |
| 额度调整 | /grant | ✅ | |
| License | /license | ✅ | |
| 内部状态 | /status | ✅ | 0严重/0预警事件 |
| 系统设置 | /settings | ✅ | |
| API 文档 | /docs | ✅ | |
| 审计日志 | /audit-logs | ✅ | 87条 |
| **总计** | **21 页面** | **全部200** | |

---

## 六、RBAC 测试

| 场景 | 结果 |
|------|------|
| admin 访问所有端点 | ✅ 200 |
| viewer 访问 /users | ✅ 403 |
| viewer 访问 /channel-keys | ✅ 403 |
| viewer 访问 /runtime/status | ✅ 403 |
| viewer 访问 /keys | ✅ 200 |
| viewer 访问 /logs | ✅ 403 |
| viewer 访问 /usage/analytics | ✅ 403 |

---

## 七、压力测试（完整）

> 环境：阿里云 2c4g，单机 8 容器

### HTTP 端点压测

| 场景 | 并发 | 请求 | QPS | 延迟 | 失败 |
|------|------|------|-----|------|------|
| Health | 50 | 500 | 296 | 169ms | 0 |
| Health | 100 | 500 | 341 | 293ms | 0 |
| Dashboard | 20 | 200 | 141 | 142ms | 0 |
| Dashboard | 50 | 300 | 161 | 310ms | 0 |
| Dashboard | 80 | 400 | — | — | 3 (timeout) |
| Keys List | 20 | 200 | 35 | 575ms | 0 |
| Logs | 20 | 200 | 146 | 137ms | 0 |
| Login (rate limit) | 10 | 100 | 56 | 177ms | 90 (429) |

### Gateway Proxy 压测

| 场景 | 并发 | 请求 | QPS | 延迟 | 失败 | 原因 |
|------|------|------|-----|------|------|------|
| mimo-v2.5 | 5 | 10 | 6.4 | 780ms | 0 | |
| mimo-v2.5 | 10 | 30 | 14.3 | 700ms | 18 | RPS 限速 |

### 压测结论

- 稳定范围：50 并发以下（HTTP 端点）
- 极限点：80 并发 socket timeout（2c4g）
- Gateway proxy：受 RPS 限速保护，5 并发稳定
- 恢复能力：压测后自动恢复

---

## 八、数据一致性

| 检查项 | 结果 | 证据 |
|--------|------|------|
| Analytics 总数 = Logs 总数 | ✅ | 149 = 149 |
| Call log 含完整字段 | ✅ | request_id/model/status/tokens/cost |
| Trace metadata 无敏感字段 | ✅ | 无 api_key/auth/prompt |
| Trace events 覆盖全生命周期 | ✅ | 6 events |
| Dashboard 渠道与 API 一致 | ✅ | 4 渠道 |
| scope=my 过滤正确 | ✅ | key 无 owner 返回0 |
| Gateway 响应含 _uniroute 元数据 | ✅ | channel/provider/cost/request_id |

---

## 九、系统强健性评分

| 维度 | 评分 | 说明 |
|------|------|------|
| 认证安全 | ⭐⭐⭐⭐ | JWT + rate limiting + 密码拦截，缺安全头 |
| 注入防护 | ⭐⭐⭐⭐ | 参数化查询正确，未过滤输入字符 |
| 依赖安全 | ⭐⭐⭐ | Go 标准库21漏洞，npm 0漏洞 |
| API 功能 | ⭐⭐⭐⭐ | 核心功能正常，2端点未注册 |
| 前端 UI | ⭐⭐⭐⭐⭐ | 21页面全部正常 |
| 网关转发 | ⭐⭐⭐⭐⭐ | 非流式+流式+计费+日志+trace 全链路 |
| RBAC | ⭐⭐⭐⭐ | 角色隔离正确 |
| 性能 (2c4g) | ⭐⭐⭐ | 50并发稳定，80并发退化 |
| 可观测性 | ⭐⭐⭐ | Prometheus 正常，Grafana/版本/环境需修复 |
| 数据一致性 | ⭐⭐⭐⭐ | analytics/logs/trace 数据一致 |

**总体评价：** UniRouter 核心功能强健，网关转发全链路正常，RBAC 隔离正确，21个前端页面全部可用。主要风险点是存储型 XSS、Go 标准库漏洞、缺少安全响应头。

---

## 十、Bug 列表（最终）

| # | 严重性 | 问题 | 修复建议 |
|---|--------|------|----------|
| 1 | 🔴 HIGH | 存储型 XSS | 后端 sanitize name 字段 |
| 2 | 🟡 MEDIUM | SQL payload 存储 | 后端过滤特殊字符 |
| 3 | 🟡 MEDIUM | 无安全响应头 | Gin middleware 添加 |
| 4 | 🟡 MEDIUM | Go 标准库21漏洞 | 升级 Go 到1.25.11+ |
| 5 | 🟢 LOW | 404 暴露版本 | 返回标准404格式 |
| 6 | 🟢 LOW | Grafana URL 错误 | .env 改为 grafana:3000 |
| 7 | 🟢 LOW | 版本未注入 | Dockerfile 加 ldflags |
| 8 | 🟢 LOW | 环境未配置 | .env 设置 RUNTIME_ENVIRONMENT |

---

## 十一、测试覆盖统计

| 维度 | 计划项 | 已完成 | 覆盖率 |
|------|--------|--------|--------|
| 环境健康 | 7 | 7 | 100% |
| 安全测试 | 23 | 23 | 100% |
| API 功能 | 40 | 35 | 88% |
| 前端 UI | 27 | 21 | 78% |
| Gateway Proxy | 15 | 10 | 67% |
| 压力测试 | 6 | 8 | 133% |
| 数据一致性 | 5 | 7 | 140% |
| RBAC | 7 | 7 | 100% |
| 依赖漏洞 | 2 | 2 | 100% |
| **总计** | **132** | **120** | **91%** |

---

## 十二、Atlas 待办

1. .env: `GRAFANA_URL=http://grafana:3000`
2. .env: `RUNTIME_ENVIRONMENT=prod`
3. Dockerfile: ldflags 注入 version/commit
4. 确认 `uniroute-api` 缺 healthy 标记的原因
5. 删除测试数据库 `uniroute_test`
6. 考虑升级 Go 到1.25.11+（修复21个标准库漏洞）

---

## 十三、测试数据清理

| 项目 | 状态 |
|------|------|
| 测试用户 | ✅ 已删除 |
| 测试 Key (32,33) | ✅ 已禁用/删除 |
| 压测 Key (34) | ✅ 已禁用 |
| 测试数据库 | 待 Atlas 删除 |

---

## 十四、补充测试结果（第二轮）

### 新发现的 Bug

| # | 严重性 | 问题 | 证据 |
|---|--------|------|------|
| 9 | 🟡 MEDIUM | Key 名称1000字符返回500而非400 | `POST /api/v1/keys` name=1000个A → 500 "Failed to create key"，应返回400 |
| 10 | 🟡 MEDIUM | 负数 project_id 创建 Key 返回201 | `POST /api/v1/keys` project_id=-1 → 201 Created，应拒绝负数 |

### 跨 project 越权测试

| 场景 | 结果 | 证据 |
|------|------|------|
| viewer 访问 /keys?project_id=1 | ✅ 403 | |
| viewer 访问 /logs?project_id=1 | ✅ 403 | |
| viewer 访问 /usage/analytics?project_id=1 | ✅ 403 | |
| viewer 访问 /users | ✅ 403 | |
| viewer 访问 /channel-keys | ✅ 403 | |
| viewer 访问 /runtime/status | ✅ 403 | |
| viewer 访问 /dashboard/stats | ✅ 200 | Dashboard 公开可见 |

### 前端表单验证（API 层）

| 场景 | 结果 | 证据 |
|------|------|------|
| 空 username 登录 | ✅ 400 | "username and password are required" |
| 空 key name | ✅ 400 | "name is required" |
| 1000字符 key name | ⚠️ 500 | 应返回400，不是500 |
| 不存在的 project_id | ✅ 400 | "Invalid project_id" |
| 负数 project_id | ⚠️ 201 | 应拒绝负数 |

### Gateway Proxy 多渠道测试

| 渠道 | 模型 | 结果 | 证据 |
|------|------|------|------|
| Mimo Token Plan | mimo-v2.5 | ✅ 200 | 262 tokens |
| deepseek | deepseek-chat | ✅ 200 | deepseek-v4-flash, 11 tokens |
| kimi | moonshot-v1-8k | ✅ 200 | 14 tokens |
| 不存在模型 | nonexistent | ✅ 503 | "No available channel for model" |

### 用户删除级联测试

| 步骤 | 结果 |
|------|------|
| 创建用户 + personal key | ✅ |
| 添加用户到项目 | ⚠️ 项目成员管理 API 未注册（405） |
| 因无法添加项目成员，无法创建 personal key | 跳过级联验证 |

### 并发会话测试

| 场景 | 结果 |
|------|------|
| 5个并发请求（dashboard/keys/logs/usage/users） | ✅ 全部200 |

### API 端点补充验证

| 端点 | 状态 | 备注 |
|------|------|------|
| GET /api/v1/notifications | ✅ | |
| GET /api/v1/notification-events | ✅ | total=0 |
| GET /api/v1/connectivity | ✅ | service/version/status |
| GET /api/v1/license | ✅ | status/valid/signature_valid/warning/days_until_expiry |
| GET /api/v1/settings | ✅ | data 字段 |

### 最终 Bug 列表（10个）

| # | 严重性 | 问题 |
|---|--------|------|
| 1 | 🔴 HIGH | 存储型 XSS — Key 名称接受 HTML 标签 |
| 2 | 🟡 MEDIUM | SQL 注入 payload 被原样存储 |
| 3 | 🟡 MEDIUM | 无安全响应头 |
| 4 | 🟡 MEDIUM | Go 标准库21个漏洞 |
| 5 | 🟡 MEDIUM | Key 名称1000字符返回500而非400 |
| 6 | 🟡 MEDIUM | 负数 project_id 创建 Key 返回201 |
| 7 | 🟢 LOW | 404 暴露版本信息 |
| 8 | 🟢 LOW | Grafana URL 配置错误 |
| 9 | 🟢 LOW | 版本/commit 未注入 |
| 10 | 🟢 LOW | 环境未配置 |
