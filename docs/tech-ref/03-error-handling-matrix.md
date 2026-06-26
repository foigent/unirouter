# Error Handling Matrix（错误处理矩阵）

> **版本**：v2.0
> **日期**：2026-06-18
> **用途**：开发团队实现网关异常处理逻辑的核心参考
> **变更**：基于 v1.0 修正，统一错误码命名规范，补充 SSE 流式错误处理

---

## 一、上游错误处理规则

| 上游状态码 | 上游错误类型 | Gateway 处理 | 重试 | 熔断 | 返回客户端 |
|-----------|------------|-------------|------|------|-----------|
| **200** | 正常 | 解析 usage → Settle | — | — | 200 + 响应体 |
| **400** | 请求格式错误 | 直接返回 | ❌ | ❌ | 400 + upstream_error |
| **401** | 认证失败 | 标记该 Key invalid，切下一个 Key | ❌ | ✅ Key 级 | 502（所有 Key 失败） |
| **403** | 权限不足 | 直接返回 | ❌ | ✅ 渠道级 | 403 + upstream_error |
| **408** | 请求超时 | 切 fallback 渠道 | ✅ 1 次 | ✅ 计数 | 502（全部超时） |
| **429** | 限流 | 该 Key 冷却 60s，切下一个 Key | ❌ | ✅ Key 级 | 429（全部限流） |
| **500** | 服务器错误 | 切 fallback 渠道 | ✅ 1 次 | ✅ 计数 | 502 |
| **502** | 网关错误 | 切 fallback 渠道 | ✅ 1 次 | ✅ 计数 | 502 |
| **503** | 服务不可用 | 切 fallback 渠道 | ✅ 1 次 | ✅ 计数 | 502 |
| **连接超时** | 无响应 | 切 fallback 渠道 | ✅ 1 次 | ✅ 计数 | 502 |

---

## 二、平台错误码规范

### 2.1 错误响应格式（OpenAI 兼容）

```json
{
    "error": {
        "message": "人类可读的错误描述",
        "type": "error_type",
        "code": "error_code",
        "param": null
    }
}
```

### 2.2 错误码分类

采用 `{category}.{specific}` 两段式命名，避免与上游错误码冲突。

**鉴权错误（4xx）**：

| HTTP | type | code | message | 触发场景 |
|------|------|------|---------|---------|
| 400 | `invalid_request_error` | `validation_error.sensitive_word_detected` | 请求包含敏感内容 | 敏感词拦截 |
| 400 | `invalid_request_error` | `validation_error.context_length_exceeded` | 请求超过上下文限制 | prompt 过长 |
| 400 | `invalid_request_error` | `validation_error.invalid_request` | 请求格式错误 | 参数缺失/格式错误 |
| 401 | `authentication_error` | `auth_error.invalid_api_key` | API Key 无效 | Key 不存在或已禁用 |
| 403 | `permission_error` | `auth_error.ip_denied` | IP 不在白名单 | IP 白名单拦截 |
| 403 | `permission_error` | `auth_error.model_not_allowed` | 模型不在授权列表 | allowed_models 拦截 |
| 429 | `rate_limit_error` | `rate_limit_error.rpm_exceeded` | 请求过于频繁（RPM） | Key 级限流 |

**计费错误（4xx）**：

| HTTP | type | code | message | 触发场景 |
|------|------|------|---------|---------|
| 402 | `billing_error` | `billing_error.insufficient_balance` | 余额不足，请充值后重试 | 预冻结失败 |
| 409 | `billing_error` | `billing_error.idempotent_conflict` | 请求正在处理中 | 幂等性冲突 |

**服务器错误（5xx）**：

| HTTP | type | code | message | 触发场景 |
|------|------|------|---------|---------|
| 500 | `server_error` | `server_error.internal_error` | 内部服务器错误 | 未预期异常 |
| 502 | `server_error` | `server_error.upstream_error` | 上游服务不可用 | 所有渠道失败 |
| 503 | `server_error` | `server_error.service_unavailable` | 服务暂时不可用 | 健康检查失败 |

---

## 三、SSE 流式错误处理

| 错误时机 | 处理方式 |
|---------|---------|
| **转发前**（Freeze 后，首个 chunk 前） | Cancel 冻结金额 + 返回标准错误 JSON |
| **转发中**（已发送部分 chunk） | 发送 `data: {"error":{...}}\n\n` + 关闭 SSE |
| **客户端断开** | 关闭上游连接 + 按已收到 chunk 结算 |
| **上游断开（无 usage）** | 本地 token 估算 + 结算 + 发送 [DONE] |
| **余额穿底** | 强制断开上游 + 补发 [DONE] + 按已收到 chunk 结算 |

### 流式错误 JSON 格式

```
data: {"error":{"message":"上游服务不可用","type":"server_error","code":"server_error.upstream_error"}}

data: [DONE]
```

---

## 四、Failover 逻辑

```
请求 → 渠道 A（priority=1）
  ├── Key 1 失败（401）→ 标记 Key 1 invalid → 尝试 Key 2
  ├── Key 2 失败（429）→ Key 2 冷却 60s → 尝试 Key 3
  ├── Key 3 失败（500）→ 渠道 A 失败计数 +1
  │
  ▼ （所有 Key 失败）
  渠道 A 失败计数 ≥ 5? → 熔断
  │
  ▼
  渠道 B（priority=2，同模型）
  ├── Key 1 成功 → 返回
  └── Key 1 失败 → 继续...
  │
  ▼ （所有渠道失败）
  返回 502 + server_error.upstream_error
```

**关键规则**：
- 只重试 1 次（`MAX_RETRY_COUNT=1`）
- 只做同模型重试，不做跨模型 Fallback
- 400 错误不重试（客户端问题）
- 401/429 不触发渠道级熔断（Key 级处理）
- 408/500/502/503 触发渠道级熔断计数
