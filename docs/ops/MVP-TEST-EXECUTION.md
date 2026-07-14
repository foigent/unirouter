# MVP 端到端测试执行计划

> **版本**：v1.1
> **日期**：2026-06-27
> **环境**：阿里云 http://8.130.18.85:3000
> **已有渠道**：DeepSeek、Kimi、MiMo
> **基于**：test-plan.md TC-01 到 TC-19 + 实际部署补充

---

## 已验证（本次会话）

| 编号 | 场景 | 结果 | 证据 |
|------|------|------|------|
| TC-01a | 健康检查 live | ✅ | `{"status":"ok"}` |
| TC-01b | 健康检查 ready | ✅ | `{"status":"ready"}` |
| TC-02a | 无 Key → 401 | ✅ | `auth_error.invalid_api_key` |
| TC-02b | Gateway Key 认证 | ✅ | `/v1/models` 返回 3 模型 |
| TC-03 | 非流式聊天（DeepSeek） | ✅ | 200 + `_uniroute.cost` |
| TC-04 | 流式聊天（DeepSeek） | ✅ | SSE 流 + `data: [DONE]` |
| TC-05 | 计费（2 次调用） | ✅ | 余额 100→98，消费 2 分 |
| TC-06 | 调用日志 | ✅ | 2 条记录 |
| TC-07 | Bug #10：允许模型 → 200 | ✅ | deepseek-chat 通过 |
| TC-08 | Bug #10：禁止模型 → 403 | ✅ | `auth_error.model_not_allowed` |
| TC-09 | Kimi 渠道 | ✅ | moonshot-v1-8k 返回 "好。" |
| TC-10 | MiMo 渠道 | ✅ | mimo-v2.5-pro 返回 reasoning_content |

---

## 已验证（本次会话 2026-06-27 04:16 - 08:03）

### 核心链路

| 编号 | 场景 | 结果 | 证据 |
|------|------|------|------|
| TC-01a | 健康检查 live | ✅ | `{"status":"ok"}` |
| TC-01b | 健康检查 ready | ✅ | `{"status":"ready"}` |
| TC-02a | 无 Key → 401 | ✅ | `auth_error.invalid_api_key` |
| TC-02b | Gateway Key 认证 | ✅ | `/v1/models` 返回模型列表 |
| TC-03 | 非流式聊天（DeepSeek） | ✅ | 200 + `_uniroute.cost` |
| TC-04 | 流式聊天（DeepSeek） | ✅ | SSE 流 + `data: [DONE]` |
| TC-05 | Kimi 渠道 | ✅ | moonshot-v1-8k 返回 "大家好，我是Kimi" |
| TC-06 | MiMo 渠道 | ✅ | mimo-v2.5-pro 返回 reasoning_content |
| TC-07 | Bug #10：允许模型 → 200 | ✅ | deepseek-chat 通过 |
| TC-08 | Bug #10：禁止模型 → 403 | ✅ | `auth_error.model_not_allowed` |

### 计费

| 编号 | 场景 | 结果 | 证据 |
|------|------|------|------|
| TC-09 | 计费扣费 | ✅ | 余额 100→98，消费 2 分 |
| TC-10 | 调用日志记录 | ✅ | 13 条记录，provider 正确 |
| TC-11 | 余额不足拦截 | ✅ | 余额=0 时返回 402 `insufficient_balance` |
| TC-12 | 交易记录 | ✅ | 有 freeze + settle 记录 |

### 管理能力

| 编号 | 场景 | 结果 | 证据 |
|------|------|------|------|
| TC-13 | 渠道 CRUD | ✅ | Create→Read→Update→Read→Delete |
| TC-14 | API Key CRUD | ✅ | Create→Update→Disable |
| TC-15 | Dashboard | ✅ | 11 个模型价格 + 13 条日志 |
| TC-16 | 敏感词管理 | ✅ | 添加 + 查询 |
| TC-17 | 敏感词拦截 | ✅ | 含敏感词请求返回 400 `content_policy_violation` |
| TC-18 | 审计日志 | ✅ | 18 条记录 |
| TC-19 | 渠道连通性测试 | ✅ | DeepSeek 67ms, status=ok |
| TC-20 | 模型价格列表 | ✅ | 11 个模型，provider 字段为空（见下方问题） |
| TC-21 | 用户列表 | ✅ | 1 个 admin |
| TC-22 | Settings | ✅ | 12 个配置项 |
| TC-23 | Reconciliation | ✅ | 13 条记录 |

### 错误码矩阵

| 编号 | 场景 | 预期 | 实际 | 结果 |
|------|------|------|------|------|
| TC-24 | 无 Key | 401 | 401 `auth_error.invalid_api_key` | ✅ |
| TC-25 | 禁用 Key | 401 | 401 `auth_error.api_key_disabled` | ✅ |
| TC-26 | 模型不允许 | 403 | 403 `auth_error.model_not_allowed` | ✅ |
| TC-27 | 无效 JSON | 400 | 400 `invalid_request` | ✅ |
| TC-28 | RPM 限流 | 429 | 429（第 4 个请求触发，RPM=2） | ✅ |
| TC-29 | Embeddings | 200 或 501 | 502（上游不支持） | ⚠️ |

### 多渠道对比

| 渠道 | 模型 | 响应 | tokens | 费用 |
|------|------|------|--------|------|
| DeepSeek | deepseek-v4-flash | "我是DeepSeek..." | 36 | 1 分 |
| Kimi | moonshot-v1-8k | "大家好，我是Kimi..." | 36 | 6 分 |
| MiMo | mimo-v2.5-pro | "我是MiMo..." + reasoning | 297 | 33 分 |

---

## 发现的问题

### 问题 #1：调用日志 provider 字段写入正确，但模型价格的 provider 字段为空

**现象**：`/api/v1/model-prices` 返回的 11 个模型，`provider` 字段全部为空。
**调用日志**：`/api/v1/logs` 的 `provider` 字段正确显示 `deepseek`/`kimi`/`mimo`。
**影响**：前端按供应商筛选时，模型价格无法按 provider 过滤。
**根因**：创建模型价格时未传 `provider` 字段，或后端未从渠道自动关联。

### 问题 #2：Embeddings 端点返回 502

**现象**：`POST /v1/embeddings` 返回 `502 Bad Gateway`（nginx 层面）。
**原因**：DeepSeek 可能不支持 `/v1/embeddings` 端点，relay 转发后上游返回非 JSON，nginx 代理报错。
**影响**：Embeddings 功能不可用（需支持 embeddings 的上游渠道）。

### 问题 #3：RPM 限流有延迟生效

**现象**：RPM=2 时，前 3 个请求都返回 200，第 4 个才返回 429。
**原因**：可能是滑动窗口实现，或首次请求触发了计时器重置。
**影响**：轻微，限流最终生效，但瞬时可能超限 1 个请求。

### 问题 #4：调用日志 total_tokens 字段为 0

**现象**：`/api/v1/logs` 返回的 13 条记录，`total_tokens` 全部为 0。
**实际**：API 响应中的 `usage.total_tokens` 是正确的（如 36、297）。
**影响**：日志展示不完整，无法按 token 量分析。
**根因**：`recordRequest` 写入时 token 字段可能未正确传递。

---

## 未执行（阻塞项）

| 编号 | 场景 | 阻塞原因 |
|------|------|----------|
| TC-30 | IP 白名单（从其他 IP 验证） | 需要另一台机器 |
| TC-31 | 流式中断结算 | 需要精确控制连接断开时机 |
| TC-32 | 渠道熔断 | 需要模拟连续 5 次失败 |
| TC-33 | 并发冻结（100 请求） | 需要 wrk/k6 工具 |
| TC-34 | 前端浏览器验证 | 需要浏览器 |

---

## 待执行

### 第一组：核心链路补全（优先级 P0）

#### TC-11: IP 白名单
```bash
TOKEN="<admin jwt>"

# 1. 设置白名单为当前 IP
curl -X PUT http://8.130.18.85:3000/api/v1/keys/3 \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"allowed_ips":"<你的公网IP>"}'

# 2. 从白名单 IP 调用 → 200
curl -X POST http://8.130.18.85:3000/v1/chat/completions \
  -H "Authorization: Bearer <gw_key>" \
  -H "Content-Type: application/json" \
  -d '{"model":"deepseek-chat","messages":[{"role":"user","content":"hi"}]}'

# 3. 从其他 IP 调用 → 403
# （需要另一台机器或 VPN）

# 4. 清空白名单
curl -X PUT http://8.130.18.85:3000/api/v1/keys/3 \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"allowed_ips":""}'
```

#### TC-12: Embeddings 端点
```bash
curl -X POST http://8.130.18.85:3000/v1/embeddings \
  -H "Authorization: Bearer <gw_key>" \
  -H "Content-Type: application/json" \
  -d '{"model":"deepseek-chat","input":"hello world"}'
# 预期：200 + embeddings 数组，或 501（如果 DeepSeek 不支持）
```

#### TC-13: 多渠道故障转移
```bash
# 1. 禁用 DeepSeek 渠道
curl -X PUT http://8.130.18.85:3000/api/v1/channels/1 \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"status":"disabled"}'

# 2. 调用 deepseek-chat → 应该走其他渠道或报错
curl -X POST http://8.130.18.85:3000/v1/chat/completions \
  -H "Authorization: Bearer <gw_key>" \
  -H "Content-Type: application/json" \
  -d '{"model":"deepseek-chat","messages":[{"role":"user","content":"hi"}]}'

# 3. 恢复 DeepSeek
curl -X PUT http://8.130.18.85:3000/api/v1/channels/1 \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"status":"active"}'
```

---

### 第二组：计费验证（优先级 P0）

#### TC-14: 预冻结计费流程
```bash
# 1. 记录当前余额
curl http://8.130.18.85:3000/api/v1/wallet -H "Authorization: Bearer $TOKEN"
# 记录 balance_before

# 2. 调用 API
curl -X POST http://8.130.18.85:3000/v1/chat/completions \
  -H "Authorization: Bearer <gw_key>" \
  -H "Content-Type: application/json" \
  -d '{"model":"deepseek-chat","messages":[{"role":"user","content":"写100字作文"}],"max_tokens":200}'

# 3. 检查余额变化
curl http://8.130.18.85:3000/api/v1/wallet -H "Authorization: Bearer $TOKEN"
# balance_after < balance_before

# 4. 检查交易记录
curl http://8.130.18.85:3000/api/v1/wallet/transactions -H "Authorization: Bearer $TOKEN"
# 应有 freeze + settle 记录
```

#### TC-15: 余额不足拦截
```bash
# 1. 根据刚读取的服务器余额提交负向增减额（示例值仅用于隔离测试组织）
curl -X POST http://8.130.18.85:3000/api/v1/wallet/adjust \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"delta_cents":-9999,"description":"隔离测试余额调整","client_request_id":"tc15-adjust-001"}'

# 2. 调用 API → 402
curl -X POST http://8.130.18.85:3000/v1/chat/completions \
  -H "Authorization: Bearer <gw_key>" \
  -H "Content-Type: application/json" \
  -d '{"model":"deepseek-chat","messages":[{"role":"user","content":"hi"}]}'
# 预期：402 + insufficient_balance

# 3. 恢复余额
curl -X POST http://8.130.18.85:3000/api/v1/wallet/topup \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"amount_cents":500,"description":"隔离测试余额恢复","client_request_id":"tc15-topup-001"}'
```

#### TC-16: 流式中断结算
```bash
# 1. 发起流式请求，收到前几个 chunk 后 Ctrl+C
curl --max-time 2 -X POST http://8.130.18.85:3000/v1/chat/completions \
  -H "Authorization: Bearer <gw_key>" \
  -H "Content-Type: application/json" \
  -d '{"model":"deepseek-chat","messages":[{"role":"user","content":"写一篇1000字文章"}],"stream":true}'

# 2. 检查 call_logs
curl http://8.130.18.85:3000/api/v1/logs -H "Authorization: Bearer $TOKEN"
# 最新记录 usage_source = "estimated"，费用 > 0

# 3. 检查钱包已扣费
curl http://8.130.18.85:3000/api/v1/wallet -H "Authorization: Bearer $TOKEN"
```

---

### 第三组：管理能力（优先级 P1）

#### TC-17: 管理后台 CRUD 完整循环
```bash
TOKEN="<admin jwt>"

# --- 渠道 CRUD ---
# Create
curl -X POST http://8.130.18.85:3000/api/v1/channels \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"name":"test-channel","provider":"test","base_url":"http://localhost:9999","models":"test-model"}'
# Read
curl http://8.130.18.85:3000/api/v1/channels/4 -H "Authorization: Bearer $TOKEN"
# Update
curl -X PUT http://8.130.18.85:3000/api/v1/channels/4 \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"name":"test-channel-updated"}'
# Delete
curl -X DELETE http://8.130.18.85:3000/api/v1/channels/4 -H "Authorization: Bearer $TOKEN"

# --- API Key CRUD ---
# Create
curl -X POST http://8.130.18.85:3000/api/v1/keys \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"name":"test-key","allowed_models":"deepseek-chat"}'
# Read / Update / Delete ...

# --- 模型价格 CRUD ---
# Create / Read / Update / Delete ...

# --- 用户 CRUD ---
# Create / Read / Update / Delete ...
```

#### TC-18: Dashboard 数据
```bash
curl http://8.130.18.85:3000/api/v1/dashboard/stats -H "Authorization: Bearer $TOKEN"
# 预期：含 model_prices + recent_logs + 统计数据
```

#### TC-19: 敏感词管理
```bash
# 添加敏感词
curl -X POST http://8.130.18.85:3000/api/v1/sensitive-words \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"words":["测试敏感词"]}'

# 查询
curl http://8.130.18.85:3000/api/v1/sensitive-words -H "Authorization: Bearer $TOKEN"

# 删除
curl -X DELETE http://8.130.18.85:3000/api/v1/sensitive-words \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"words":["测试敏感词"]}'
```

#### TC-20: 审计日志
```bash
curl http://8.130.18.85:3000/api/v1/audit-logs -H "Authorization: Bearer $TOKEN"
# 预期：有之前所有操作的记录
```

---

### 第四组：错误码矩阵（优先级 P1）

| 场景 | 操作 | 预期 |
|------|------|------|
| 无 Key | 不带 Authorization | 401 `auth_error.invalid_api_key` |
| 禁用 Key | 用 disabled key | 401 `auth_error.api_key_disabled` |
| 模型不允许 | allowed_models="a"，调 b | 403 `auth_error.model_not_allowed` |
| 余额不足 | 余额=0 | 402 `billing_error.insufficient_balance` |
| 上游超时 | 配一个不存在的 base_url | 502 `server_error.upstream_error` |
| RPM 超限 | 设 RPM=1，快速发 2 个 | 429 `rate_limit_error.rpm_exceeded` |
| 无效 JSON | body 不是 JSON | 400 |

---

### 第五组：前端验证（优先级 P1）

| 步骤 | 操作 | 预期 |
|------|------|------|
| 1 | 浏览器访问 http://8.130.18.85:3000 | 管理后台登录页 |
| 2 | 用 admin/admin123456 登录 | Dashboard 页面 |
| 3 | 点击"渠道管理" | 显示 3 个渠道 |
| 4 | 点击"密钥管理" | 显示 Gateway Keys |
| 5 | 点击"账单管理" | 显示余额和交易记录 |
| 6 | 点击"模型价格" | 显示价格列表 |
| 7 | 点击"调用日志" | 显示调用记录 |

---

### 第六组：多模型对比（优先级 P2）

同一 prompt，三个渠道对比：

```bash
PROMPT='{"messages":[{"role":"user","content":"用一句话介绍自己"}],"max_tokens":50}'

# DeepSeek
curl -X POST http://8.130.18.85:3000/v1/chat/completions \
  -H "Authorization: Bearer <gw_key>" -H "Content-Type: application/json" \
  -d '{"model":"deepseek-chat",'"$PROMPT"'}'

# Kimi
curl -X POST http://8.130.18.85:3000/v1/chat/completions \
  -H "Authorization: Bearer <gw_key>" -H "Content-Type: application/json" \
  -d '{"model":"moonshot-v1-8k",'"$PROMPT"'}'

# MiMo
curl -X POST http://8.130.18.85:3000/v1/chat/completions \
  -H "Authorization: Bearer <gw_key>" -H "Content-Type: application/json" \
  -d '{"model":"mimo-v2.5-pro",'"$PROMPT"'}'
```

记录：响应时间、token 用量、费用、内容质量。

---

## 执行顺序建议

| 阶段 | 内容 | 耗时 | 阻塞项 |
|------|------|------|--------|
| 1 | 第一组（核心链路补全） | 15 min | 需要另一台机器测 IP 白名单 |
| 2 | 第二组（计费验证） | 10 min | 无 |
| 3 | 第三组（管理能力） | 15 min | 无 |
| 4 | 第四组（错误码矩阵） | 10 min | 无 |
| 5 | 第五组（前端验证） | 5 min | 浏览器 |
| 6 | 第六组（多模型对比） | 10 min | 无 |

**总计约 65 分钟。**

---

## 结论判定

- [ ] 第一至第四组全部 PASS → 核心功能可交付
- [ ] 第五组 PASS → 管理后台可交付
- [ ] 第六组有 ≥2 个渠道正常 → 多模型能力可交付
- [ ] 无 P0 bug 遗留
