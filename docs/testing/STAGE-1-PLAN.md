# 阶段 1 执行计划 — billing 包测试补全

> 创建: 2026-06-24 22:55
> 最后更新: 2026-06-24 23:45（Codex Fix #8 已实现 PriceProvider 接口注入）
> 状态: ✅ 已完成

## 基线

| 项目 | 状态 |
|------|------|
| billing 单元测试 | 已从 7 增加到 25（17 calc + 8 字段） |
| billing 集成测试 | 35 PASS |
| Fix #8 阻塞 | ✅ 已解除（PriceProvider 接口注入） |

## 核心问题（已解决）

原问题：`CalculateCost`、`EstimateFreezeAmount` 紧耦合 `b.db.QueryRow()`，无法脱离 DB 测试。

**解决方案**：Codex 实现了 PriceProvider 接口注入（Fix #8）：
- `PriceProvider` 接口 + `DBPriceProvider`（生产）+ `PriceProviderFunc`（测试适配器）
- `WithPriceProvider()` 方法支持测试注入
- 17 个纯单元测试覆盖 CalculateCost/EstimateFreezeAmount 所有边界

## 待办步骤

### A 线：集成测试扩展（立即执行）

- [x] 1.1 QueryModelPriceForOrg — 有 multiplier / 无 multiplier / multiplier=1.0 / fallback+multiplier（4 个新增）
- [x] 1.2 CalculateCost — 零 token（min 1 cent 边界）/ 刚好 1 cent / 大数值 / 纯 completion token（4 个新增）
- [x] 1.3 EstimateFreezeAmount — safetyFactor 边界（0.5/2.0/0.0）/ minFreezeAmount > estimated（3 个新增）
- [x] 1.4 Freeze — granted_balance 优先消费 / 部分 grant + 部分 balance（2 个新增）
- [x] 1.5 Settle — exact cost no refund / cost exceeds frozen（2 个新增）+ Cancel non-existent idempotent（1 个新增）
- [x] 1.6 跑全量集成测试确认新增测试 PASS — 35/35 PASS（19 原有 + 16 新增）

### B 线：纯函数提取（Codex 实现）

- [x] 1.7 写 `issues/billing-pure-function-barrier.md` — 测试发现报告（纯函数测试壁垒）
- [x] 1.8 Codex 实现 Fix #8：PriceProvider 接口注入 + 17 个纯单元测试
- [x] 1.9 Canary 验收：8/8 fix PASS，52/52 测试全通过

### 收尾

- [x] 1.10 更新 TEST-STATUS.md / COVERAGE.md / BUG-LOG.md / MEMORY.md
- [x] 1.11 CHECKLIST.md 交叉验证
- [x] 1.12 VERIFY-RESULT.md 覆盖写入（第 5 轮验收）

---

## 结果

| 指标 | 初始 | 最终 |
|------|------|------|
| billing 单元测试 | 7 | 25（+17 纯单元） |
| billing 集成测试 | 19 | 35 |
| billing 总计 | 26 | 52 |
| 测试壁垒 | 存在（无一脱离 DB） | ✅ 已解除 |

## 后续

- 阶段 2（可立即启动）：gateway 包核心路由测试 (5% → 40%)
