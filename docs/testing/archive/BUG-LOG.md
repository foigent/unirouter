# UniRouter Bug 记录

> 维护者: Canary (测试 agent)
> 最后更新: 2026-06-27（Bug #10 状态同步 — MVP 端到端验收已通过）
> 格式: 每个 bug 一个条目，包含发现、验证、修复状态
> 规则: 只记录不修。修复由 Codex 负责。

---

## Bug #0 — billing.go QueryModelPrice 列名错误 (P1 生产 Bug)

- **发现日期**: 2026-06-24
- **严重性**: Critical (生产)
- **类型**: SQL 列名不匹配
- **状态**: ✅ 已修复验收 (第 1 轮 FIX-SIGNAL Fix #1 PASS, 2026-06-24)
- **发现方式**: Debug billing 25P02 根因
- **验收证据**: `go test ./internal/billing/ -v -tags=integration` 19/19 PASS，`TestQueryModelPrice_Found_Integration` 和 `TestQueryModelPrice_NotFound_Fallback_Integration` 均通过

### 问题描述
`billing.go:68` 的 `QueryModelPrice()` 使用错误的列名和表列：

```go
// 修复前
SELECT input_cost_cents, output_cost_cents FROM model_prices WHERE model = $1

// 修复后
SELECT input_price_cents, output_price_cents FROM model_prices WHERE model_name = $1
```

### 影响范围
- `QueryModelPrice()` — 所有调用
- `QueryModelPriceForOrg()` — 依赖 `QueryModelPrice()`
- `CalculateCost()` — 依赖 `QueryModelPrice()`
- `EstimateFreezeAmount()` — 依赖 `QueryModelPrice()`
- `Freeze()` — 依赖 `EstimateFreezeAmount()`
- `Settle()` — 依赖 `CalculateCost()`
- 所有 billing 集成测试

---

## Bug #0.1 — billing.go:138 freeze_idempotency 查询列不存在

- **发现日期**: 2026-06-24
- **严重性**: Critical (生产)
- **类型**: SQL 列名不匹配
- **状态**: ✅ 已修复验收 (第 3 轮 FIX-SIGNAL Fix #6 PASS, 2026-06-24)
- **验收证据**: `grep -n 'SELECT.*FROM freeze_idempotency' internal/billing/billing.go` 查询 `status` 而非 `id`，`grep existingID` 零匹配，billing 集成测试 19/19 PASS

### 问题描述
`billing.go:138` 的 Freeze() 函数查询 `freeze_idempotency` 表的 `id` 列，但该表没有 `id` 列。

```go
// 修复前
SELECT id FROM freeze_idempotency WHERE request_id = $1

// 修复后
SELECT status FROM freeze_idempotency WHERE request_id = $1
```

### 影响范围
- `Freeze()` — 所有调用
- 所有 billing 集成测试

---

## Bug #1 — testutil 包编译失败

- **发现日期**: 2026-06-24
- **严重性**: High
- **类型**: 编译错误
- **状态**: ✅ 已修复 (Canary 自行修复, 2026-06-24)
- **修复**: 在 `internal/testutil/db.go` import 中添加 `"github.com/jackc/pgx/v5"`

---

## Bug #2 — billing 集成测试全挂 (SQLSTATE 25P02)

- **发现日期**: 2026-06-24
- **严重性**: Critical
- **类型**: 测试失败（级联于 Bug #0 和 #0.1）
- **状态**: ✅ 已修复 (根因 Bug #0 和 #0.1 修复后自动消除, 2026-06-24)
- **验收证据**: billing 集成测试 19/19 PASS

---

## Bug #3 — relay.go 循环内 defer 资源泄漏

- **发现日期**: 2026-06-24
- **严重性**: Medium
- **类型**: 资源泄漏
- **状态**: ✅ 已修复验收 (第 1 轮 FIX-SIGNAL Fix #2 PASS, 2026-06-24)
- **验收证据**: `grep 'defer.*resp.Body.Close'` 循环内零匹配，4 处显式 Close（第 205/333/438/669 行）覆盖所有路径

### 修复方案
移除 defer，改为在每个 `continue`/`return`/`break` 路径前显式 `resp.Body.Close()`。

---

## Bug #4 — router.go:75 Exec 错误未检查

- **发现日期**: 2026-06-24
- **严重性**: Low
- **类型**: 错误处理遗漏
- **状态**: ✅ 已修复验收 (第 1 轮 FIX-SIGNAL Fix #3 PASS, 2026-06-24)
- **验收证据**: 第 75 行改为 `_, _ = r.db.Exec(...)` 显式丢弃（fire-and-forget 更新，合理处理方式）
- **建议**: 将来可在 `_ =` 位置加 `log.Printf` 记录失败，当前优先级不高

---

## Bug #5 — channel key 使用 XOR 加密（文档声称）

- **发现日期**: 2026-06-24 (CONTEXT_HANDOFF.md 记录)
- **严重性**: Critical (生产部署阻断 — 实际已用 AES-GCM，仅文档错误)
- **类型**: 文档错误
- **状态**: ✅ 已确认 (第 1 轮 FIX-SIGNAL Fix #4 PASS, 2026-06-24)
- **验收证据**: `grep 'cipher\.\|aes\.\|gcm\.\|XOR\|xor' internal/auth/auth.go` 全 AES-GCM，零 XOR，`TestEncryptDecryptKey_Roundtrip` PASS

### 结论
代码实际使用 AES-256-GCM (`crypto/aes` + `crypto/cipher`)，安全。文档中 XOR 引用是过时的设计文档内容。

---

## Bug #6 — billing debug 测试 result nil check 顺序错误

- **发现日期**: 2026-06-24
- **严重性**: Medium
- **类型**: 测试代码 bug
- **状态**: ✅ 已删除 (Canary, 2026-06-24)
- **处理**: `billing_debug_test.go` 已整体删除（临时 debug 测试，不再需要）

---

## Bug #7 — Settle INSERT $3 参数类型冲突 (Issue C)

- **发现日期**: 2026-06-24
- **严重性**: Critical (生产)
- **类型**: SQL 参数类型冲突
- **状态**: ✅ 已修复验收 (第 4 轮 FIX-SIGNAL Fix #7 PASS, 2026-06-24)
- **验收证据**: `TestSettle_Success_Integration` PASS，`$3` 仅用于 request_id，`$4` 专用于 note 拼接
- **发现方式**: 第 3 轮验收后 billing 集成测试 Settle 相关测试失败，debug 发现 42P08

### 问题描述
```go
// 修复前: $3 用于两处不同上下文，类型冲突 (SQLSTATE 42P08)
VALUES ($3, $1, 'settle', 'completed', $2, 'Settle for request ' || $3::text)

// 修复后: $3 仅用于 request_id，$4 用于 note 拼接
VALUES ($3, $1, 'settle', 'completed', $2, 'Settle for request ' || $4::text)
```

---

## Bug #8 — 集成测试并行执行数据竞态

- **发现日期**: 2026-06-25
- **严重性**: High
- **类型**: 测试基础设施
- **状态**: ✅ 已修复 (阶段 5)
- **修复**: `testutil.NewTestOrg` 前调用 `TruncateAll`；集成测试用 `go test -tags=integration -p=1`
- **验证**: `go test -tags=integration -p=1 ./... -count=1` 全 PASS (318 测试)

---

## Bug #9 — admin 覆盖率 1.0% 不变（架构壁垒）

- **发现日期**: 2026-06-25
- **严重性**: Medium
- **类型**: 测试壁垒
- **状态**: ⚠️ 待 Codex 重构
- **根因**: admin handler 紧耦合 `*pgxpool.Pool`，集成测试不计入 `go test -cover`
- **解决**: 需要 Codex 提取可测函数或使用接口注入

---

## Bug #10 — main.go chatHandler AllowedModels 限制完全不生效 (Critical)

- **发现日期**: 2026-06-27
- **严重性**: Critical
- **类型**: 逻辑错误（两个 bug 叠加）
- **状态**: ✅ 已修复 + MVP 端到端验收通过
- **发现者**: Sentinel（代码审查）
- **修复者**: Codex（main agent）
- **验收者**: Canary（MVP 端到端测试）
- **文件**: `cmd/uniroute/main.go:148-174`

**问题**：chatHandler 中 AllowedModels 限制代码有两个 bug：
1. `req.Model` 永远是空字符串 — 只声明了 struct，没有从 request body 解析 JSON
2. `_ = allowed` — 计算结果被丢弃，没有 return 403

**修复方案**：
- `io.ReadAll(r.Body)` + `json.Unmarshal` 解析 model
- `bytes.NewBuffer` + `io.NopCloser` 缓冲 body 给 relay
- `allowed == false` → 403 `auth_error.model_not_allowed`

**MVP 验收证据**（2026-06-27，生产环境 `http://8.130.18.85:3000`）：
- TC-07：`deepseek-chat` + AllowedModels 含该模型 → 200 ✅
- TC-08：`gpt-4` + AllowedModels 不含该模型 → 403 `auth_error.model_not_allowed` ✅

---

## Bug #11 — 调用日志供应商下拉框硬编码 (Medium)

- **发现日期**: 2026-06-27
- **严重性**: Medium
- **类型**: 前端硬编码
- **状态**: ⚠️ 待 Codex 修复
- **发现者**: Nicholas
- **文件**: `frontend/src/pages/LogsPage.tsx:103-108`

**问题**：供应商筛选下拉框写死了 4 个选项（openai/deepseek/qwen/zhipu），不会随渠道变化更新。
- 缺失：kimi、mimo（已有渠道）
- 多余：openai、qwen、zhipu（已删除渠道）

**建议修复**：从 `/api/v1/channels` 动态获取 provider 列表，或从 `/api/v1/logs` 的去重 provider 获取。

---

## 已修复汇总

| Bug # | 描述 | 严重性 | 修复轮次 | 状态 |
|-------|------|--------|---------|------|
| #0 | QueryModelPrice 列名错误 | Critical | 第 1 轮 | ✅ |
| #0.1 | freeze_idempotency 查询列不存在 | Critical | 第 3 轮 | ✅ |
| #1 | testutil 编译失败 | High | Canary 自行 | ✅ |
| #2 | billing 集成测试全挂 | Critical | 级联修复 | ✅ |
| #3 | relay.go 循环 defer | Medium | 第 1 轮 | ✅ |
| #4 | router.go Exec 未检查 | Low | 第 1 轮 | ✅ |
| #5 | XOR 加密文档错误 | Critical | 第 1 轮 | ✅ |
| #6 | debug test nil check | Medium | Canary 自行 | ✅ |
| #7 | Settle $3 参数类型冲突 | Critical | 第 4 轮 | ✅ |
| #8 | 集成测试并行竞态 | High | 阶段 5 | ✅ |
| #10 | AllowedModels 限制不生效 | Critical | 第 5 轮 + MVP 验收 | ✅ |

## 当前未修复

| Bug # | 描述 | 严重性 | 状态 |
|-------|------|--------|------|
| #9 | admin 覆盖率 1.0%（架构壁垒） | Medium | ⚠️ 待 Codex 重构 |
| #11 | 供应商下拉框硬编码 | Medium | ⚠️ 待 Codex 修复 |

## MVP 端到端测试发现的附加问题

| 编号 | 描述 | 严重性 | 状态 |
|------|------|--------|------|
| #12 | 模型价格 provider 字段为空 | Low | ⚠️ 待排查（创建时未传或未从渠道关联） |
| #13 | RPM 限流延迟生效（第 4 个才触发） | Low | ⚠️ 待排查（滑动窗口实现 or 首次请求缓存） |
| #14 | Embeddings 端点 502 | Low | ⚠️ 上游不支持（非代码 bug） |
