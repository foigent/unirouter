# UniRouter 阶段 5 计划 — 集成测试基础设施修复 + admin handler 测试补充

> 维护者: Canary (测试 agent)
> 创建: 2026-06-25 02:55 GMT+8
> 完成: 2026-06-25 03:10 GMT+8
> 状态: ✅ 完成

## 基线 (2026-06-25 02:54)

| 包 | 单元测试 | 集成测试 | 覆盖率 | 集成测试状态 |
|---|---------|---------|--------|-------------|
| admin | 9 PASS | 30 (25 PASS, 5 FAIL) | 1.0% | ⚠️ 间歇性失败 |
| billing | 25 PASS | 25 (20 PASS, 5 FAIL) | 9.8% | ⚠️ 间歇性失败 |
| middleware | 41 PASS | 11 (8 PASS, 3 FAIL) | 42.7% | ⚠️ 间歇性失败 |
| **整体** | **226 PASS** | **66 (53 PASS, 13 FAIL)** | **~23%** | **⚠️** |

## 阶段目标

1. ✅ 修复集成测试基础设施 — 66 个现有集成测试全部 PASS
2. ✅ 补充 admin handler 集成测试 — 覆盖高风险 handler
3. ✅ admin 集成测试 30 → 47 (+17)

## 步骤

### Phase A: 集成测试基础设施修复 ✅

#### A1. 确认失败根因 ✅
- [x] 逐个跑失败的测试 — 全部单独 PASS，确认为环境残留问题
- [x] 根因: 不同包的测试是独立进程，共享同一个 `uniroute_test` 数据库，TruncateAll 互相干扰

#### A2. 修复 testutil ✅
- [x] `NewTestOrg` 添加显式 `TruncateAll` 调用
- [x] 验证: `go test -tags=integration ./internal/admin/ -count=1` 全 PASS

#### A3. 确认串行执行 ✅
- [x] `go test -tags=integration -p=1 ./... -count=1` — 0 FAIL
- [x] 结论: 集成测试必须用 `-p=1` 串行执行

### Phase B: admin handler 集成测试补充 ✅

#### B1. User management (+11) ✅
- [x] TestCreateUser_Success_Integration
- [x] TestCreateUser_DuplicateUsername_Integration
- [x] TestCreateUser_MissingFields_Integration
- [x] TestCreateUser_DefaultRole_Integration
- [x] TestDeleteUser_Success_Integration
- [x] TestDeleteUser_NotFound_Integration
- [x] TestDeleteUser_InvalidID_Integration
- [x] TestUpdateUser_RoleChange_Integration
- [x] TestUpdateUser_StatusChange_Integration
- [x] TestUpdateUser_NotFound_Integration
- [x] TestListUsers_Success_Integration

#### B2. API Key management (+6) ✅
- [x] TestUpdateAPIKey_Success_Integration
- [x] TestUpdateAPIKey_NotFound_Integration
- [x] TestUpdateAPIKey_NoFields_Integration
- [x] TestDisableAPIKey_Success_Integration
- [x] TestDisableAPIKey_NotFound_Integration
- [x] TestListAPIKeys_Success_Integration

## 统计

| 指标 | 阶段前 | 阶段后 | 变化 |
|------|--------|--------|------|
| admin 集成测试 | 30 | 47 | +17 |
| admin 总测试 | 39 | 56 | +17 |
| 全量集成测试 | 66 | 83 | +17 |
| 全量总测试 | 300 | 319 | +19 |
| 集成测试 PASS 率 | 80% | 100% | +20% |

## 关键决策

| 时间 | 决策 | 依据 |
|------|------|------|
| 02:55 | 集成测试修复优先于新测试 | 基线不可靠时补充测试无意义 |
| 03:00 | NewTestOrg 加 TruncateAll | 防止前一个测试残留数据导致 FK violation |
| 03:05 | 集成测试用 -p=1 串行 | 不同包共享数据库，无法并行 |
| 03:08 | ListAPIKeys 测试用 handler 创建 key | 直接 INSERT 的 key_hash 与 handler 生成的不一致 |

## 发现的问题

### Bug #8 — 集成测试并行执行数据竞态 (已修复)
- 根因: 不同包的测试是独立进程，共享同一个数据库
- 修复: `NewTestOrg` 前 TruncateAll + `-p=1` 串行

### Bug #9 — admin 覆盖率 1.0% 不变 (架构壁垒)
- 根因: admin handler 紧耦合 `*pgxpool.Pool`，集成测试不计入 cover
- 详见: `issues/admin-coverage-barrier.md`

## 下一步

1. **阶段 6**: GatewayAuth 拆分 + 单元测试 — ✅ Codex 已提取 `resolveAPIKey`，接口注入完成
2. **阶段 7**: router 接口注入 + 单元测试（需 Codex 注入接口）
