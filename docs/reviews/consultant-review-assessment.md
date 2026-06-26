# 顾问审查报告评估

> **评估日期**: 2026-06-20
> **评估方法**: 逐条对照代码验证
> **总体评分**: 9/10 — 优秀，95%准确

---

## 验证结果汇总

### 🔴 Critical (7项) — 全部验证正确

| # | 问题 | 代码位置 | 评估 |
|---|------|---------|------|
| 1 | Freeze 忽略 granted_balance | billing.go:124 | ✅ 正确 — schema 有字段但代码不使用 |
| 2 | Freeze 无事务包裹 | billing.go:124-155 | ✅ 正确 — 3个独立DB操作 |
| 3 | Settle 负退款静默处理 | billing.go:197 | ✅ 正确 — `if refund < 0 { refund = 0 }` |
| 4 | LIKE 通配符未转义 | router.go:38 | ✅ 正确 — 实际风险低 |
| 5 | API Key 格式不符 PRD | auth.go:37 | ✅ 正确 — `ur-sk-{hex}` vs PRD `ur-{org_id}-{random}` |
| 6 | frozen 永久锁定 | billing.go:124-140 | ✅ 正确 — 与#2同根因 |
| 7 | CreateGrant 字段名错误 | handlers.go:1377 | ✅ 正确 — `amount_cents` 应为 `amount`（行号偏差~400行） |

### 🟡 Important (7项) — 全部验证正确

| # | 问题 | 评估 |
|---|------|------|
| 8 | CreateGrant 无幂等性 | ✅ 正确 |
| 9 | Cleanup 删除 freeze_idempotency | ✅ 正确 — handlers.go:1464 |
| 10 | 熔断器状态纯内存 | ✅ 正确 — breaker.go 无持久化 |
| 11 | 限流器 race condition + 比较错误 | ✅ 正确 — UnixNano 分数 vs RFC3339Nano 字符串 |
| 12 | Balance 检查忽略 granted_balance | ✅ 正确 — relay.go:314,413 |
| 13 | Token 估算只估 output | ⚠️ 部分正确 — 有 input 估算但仅在上游未提供 usage 时 |
| 14 | SSE 心跳 race condition | ✅ 正确 — 无 mutex 保护 ResponseWriter |

### 🟢 Minor (6项) — 全部验证正确

| # | 问题 | 评估 |
|---|------|------|
| 15 | 敏感词只检查 request | ✅ 正确 |
| 16 | CORS 过于宽松 | ✅ 正确（Phase 1 可接受） |
| 17 | .env 含密码提交到仓库 | ✅ 正确 |
| 18 | SettingsPage 空实现 | ✅ 正确 |
| 19 | 无 .gitignore | ✅ 正确 |
| 20 | Docker 无 healthcheck | ✅ 正确 |

---

## 顾问报告的不足

1. **行号偏差**: 多处行号不准确（如 #7 报告说 970-973，实际在 1377）
2. **#13 描述不够精确**: 实际有 input token 估算逻辑，但仅在上游未返回 usage 时触发
3. **#11 根因描述偏差**: 实际问题是数值分数 vs 字符串比较，而非窗口清理丢失请求
4. **#16 优先级偏高**: Phase 1 本地部署场景下 CORS `*` 可接受

---

## 修复优先级建议

1. **P0 — 立即修复**: CreateGrant `amount_cents` → `amount`（运行时崩溃）
2. **P0 — 立即修复**: Billing Freeze/Settle/Cancel 事务包裹（数据完整性）
3. **P1 — 本迭代修复**: granted_balance 集成（PRD 核心要求）
4. **P1 — 本迭代修复**: 限流器字符串比较 bug（完全失效）
5. **P1 — 本迭代修复**: API Key 格式对齐 PRD
6. **P2 — 下迭代**: 熔断器状态持久化
7. **P2 — 下迭代**: SSE 写入 mutex
8. **P3 — 后续**: .gitignore、CORS 收紧、SettingsPage 实现

