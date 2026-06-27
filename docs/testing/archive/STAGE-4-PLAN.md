# UniRouter 阶段 4 计划 — middleware GatewayAuth 集成测试

> 维护者: Canary (测试 agent)
> 创建: 2026-06-25 01:25 GMT+8
> 状态: ✅ 完成 (2026-06-25 01:30 GMT+8)

## 基线

- 阶段前: middleware 40.0%, 36 测试
- 阶段后: middleware 42.7%, 41 单元 + 9 集成 = 50 测试

## 步骤

### 步骤 1: GatewayAuth 集成测试 ✅
- [x] TestGatewayAuth_ValidKey: 完整认证流程通过
- [x] TestGatewayAuth_MissingHeader: 无 Authorization → 401
- [x] TestGatewayAuth_WrongPrefix: Basic xxx → 401
- [x] TestGatewayAuth_InvalidKey: 不存在的 key → 401
- [x] TestGatewayAuth_DisabledKey: status=disabled → 401
- [x] TestGatewayAuth_DisabledOrg: org status=disabled → 403
- [x] TestGatewayAuth_IPBlocked: IP 不匹配 → 403
- [x] TestGatewayAuth_IPAllowed: IP 匹配白名单 → 200
- [x] TestGatewayAuth_RequestIDFromHeader: X-Request-ID 从 header 传递
- [x] TestGatewayAuth_ContextInjection: ctx 值全部注入
- [x] TestAdminAuth_ContextInjection_Integration: AdminAuth ctx 注入验证
- 验证: 11/11 PASS ✅

### 步骤 2: 纯函数补测 ✅
- [x] TestGenerateRequestID_Uniqueness: 不空
- [x] TestUniqueMember_Format: 16 字符 hex
- [x] TestUniqueMember_Uniqueness: 两次不等
- [x] TestGetClientIP_XForwardedFor_SpacesOnly: 空格 XFF fallback
- [x] TestGetClientIP_RemoteAddr_NoPort: RemoteAddr 无端口
- 验证: 5/5 PASS ✅

### 步骤 3: 基线验证 + 文档更新 ✅
- [x] `go test ./... -cover` 0 FAIL
- [x] middleware 覆盖率 40.0% → 42.7% (+2.7%)
- [x] 更新 TEST-STATUS.md / COVERAGE.md
- [x] 文档一致性检查
- [x] 架构壁垒记录到 `issues/middleware-testability.md`

## 统计

| 指标 | 阶段前 | 阶段后 | 变化 |
|------|--------|--------|------|
| middleware 单元测试 | 36 | 41 | +5 |
| middleware 集成测试 | 0 | 9 | +9 |
| middleware 覆盖率 | 40.0% | 42.7% | +2.7% |
| 全量单元 | 221 | 226 | +5 |
| 全量集成 | 65 | 74 | +9 |
| 总测试 | 286 | 300 | +14 |

## 覆盖率未达目标的根因

middleware 未达到 50% 目标的根因：
- **GatewayAuth**: ✅ 接口注入已完成 — `resolveAPIKey` 已提取，接收 `db.DBQuerier`，可写 mock 单测
- **RateLimit (75行) 0%**: 依赖 Redis

这些不能通过纯单元测试覆盖。详见 `issues/middleware-testability.md`。

## 关键决策

| 时间 | 决策 | 依据 |
|------|------|------|
| 2026-06-25 01:25 | GatewayAuth 集成测试 | 唯一能测试认证逻辑的方式 |
| 2026-06-25 01:28 | 接受 42.7%，不改代码 | AGENTS.md 规则：不改生产代码 |
| 2026-06-25 01:29 | 记录架构壁垒 | 给 Codex 提供重构参考 |
