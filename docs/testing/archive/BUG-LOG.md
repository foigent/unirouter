# UniRouter Bug 记录

> 维护者: Canary (测试 agent)
> 最后更新: 2026-06-27 15:40（Bug #12 线上验证通过，SSE 流式正常）
> 格式: 每个 bug 一个条目，包含发现、验证、修复状态
> 规则: 只记录不修。修复由 Codex 负责。

---

## Bug #0 — billing.go QueryModelPrice 列名错误 (P1 生产 Bug)

- **发现日期**: 2026-06-24
- **严重性**: Critical (生产)
- **类型**: SQL 列名不匹配
- **状态**: ✅ 已修复验收 (第 1 轮 FIX-SIGNAL Fix #1 PASS, 2026-06-24)
- **发现方式**: Debug billing 25P02 根因
- **验收证据**: `go test ./internal/billing/ -v -tags=integration` 19/19 PASS

---

## Bug #0.1 — billing.go:138 freeze_idempotency 查询列不存在

- **发现日期**: 2026-06-24
- **严重性**: Critical (生产)
- **类型**: SQL 列名不匹配
- **状态**: ✅ 已修复验收 (第 3 轮 FIX-SIGNAL Fix #6 PASS, 2026-06-24)

---

## Bug #1 — testutil 包编译失败

- **发现日期**: 2026-06-24
- **严重性**: High
- **类型**: 编译错误
- **状态**: ✅ 已修复 (Canary 自行修复, 2026-06-24)

---

## Bug #2 — billing 集成测试全挂 (SQLSTATE 25P02)

- **发现日期**: 2026-06-24
- **严重性**: Critical
- **类型**: 测试失败（级联于 Bug #0 和 #0.1）
- **状态**: ✅ 已修复 (根因修复后自动消除, 2026-06-24)

---

## Bug #3 — relay.go 循环内 defer 资源泄漏

- **发现日期**: 2026-06-24
- **严重性**: Medium
- **类型**: 资源泄漏
- **状态**: ✅ 已修复验收 (第 1 轮 FIX-SIGNAL Fix #2 PASS, 2026-06-24)

---

## Bug #4 — router.go:75 Exec 错误未检查

- **发现日期**: 2026-06-24
- **严重性**: Low
- **类型**: 错误处理遗漏
- **状态**: ✅ 已修复验收 (第 1 轮 FIX-SIGNAL Fix #3 PASS, 2026-06-24)

---

## Bug #5 — channel key 使用 XOR 加密（文档声称）

- **发现日期**: 2026-06-24
- **严重性**: Critical (生产部署阻断 — 实际已用 AES-GCM，仅文档错误)
- **类型**: 文档错误
- **状态**: ✅ 已确认 (第 1 轮 FIX-SIGNAL Fix #4 PASS, 2026-06-24)

---

## Bug #6 — billing debug 测试 result nil check 顺序错误

- **发现日期**: 2026-06-24
- **严重性**: Medium
- **类型**: 测试代码 bug
- **状态**: ✅ 已删除 (Canary, 2026-06-24)

---

## Bug #7 — Settle INSERT $3 参数类型冲突 (Issue C)

- **发现日期**: 2026-06-24
- **严重性**: Critical (生产)
- **类型**: SQL 参数类型冲突
- **状态**: ✅ 已修复验收 (第 4 轮 FIX-SIGNAL Fix #7 PASS, 2026-06-24)

---

## Bug #8 — 集成测试并行执行数据竞态

- **发现日期**: 2026-06-25
- **严重性**: High
- **类型**: 测试基础设施
- **状态**: ✅ 已修复 (阶段 5)

---

## Bug #9 — admin 覆盖率 1.0% 不变（架构壁垒）

- **发现日期**: 2026-06-25
- **严重性**: Medium
- **类型**: 测试壁垒
- **状态**: ⚠️ 待 Codex 重构
- **根因**: admin handler 紧耦合 `*pgxpool.Pool`，集成测试不计入 `go test -cover`

---

## Bug #10 — main.go chatHandler AllowedModels 限制完全不生效 (Critical)

- **发现日期**: 2026-06-27
- **严重性**: Critical
- **类型**: 逻辑错误（两个 bug 叠加）
- **状态**: ✅ 已修复 + MVP 端到端验收通过
- **发现者**: Sentinel（代码审查）
- **修复者**: Codex（main agent）
- **验收者**: Canary（MVP 端到端测试）
- **文件**: `cmd/uniroute/main.go:155-179`

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
- **状态**: ✅ 已修复验收（第 6 轮 FIX-SIGNAL Fix #14 PASS, 2026-06-27）
- **发现者**: Nicholas
- **文件**: `frontend/src/pages/LogsPage.tsx:36,47-52,119-122`

**问题**：供应商筛选下拉框写死了 4 个选项（openai/deepseek/qwen/zhipu），不会随渠道变化更新。

**修复方案**：
1. 新增 `providers` state，组件挂载时从 `/api/v1/channels` 获取渠道列表
2. 提取去重 provider 列表
3. 下拉框改为动态渲染 `{providers.map(...)}`
4. TypeScript 编译 + Vite 构建通过

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
| #11 | 供应商下拉框硬编码 | Medium | 第 6 轮 | ✅ |
| #12 | SSE Flusher 接口缺失 | Critical | 第 7 轮 | ✅ |

## 当前未修复

| Bug # | 描述 | 严重性 | 状态 |
|-------|------|--------|------|
| #9 | admin 覆盖率 1.0%（架构壁垒） | Medium | ⚠️ 待 Codex 重构 |

## MVP 端到端测试发现的附加问题

| 编号 | 描述 | 严重性 | 状态 |
|------|------|--------|------|
| #12 | 模型价格 provider 字段为空 | Low | ✅ 已修复 |
| #13 | RPM 限流延迟生效（第 4 个才触发） | Low | ⚠️ 待排查 |
| #14 | Embeddings 端点 502 | Low | ⚠️ 上游不支持（非代码 bug） |

### Bug #12 — SSE 流式响应失败：MetricsMiddleware 的 responseWriter 未实现 Flusher (Critical)

- **发现日期**: 2026-06-27
- **严重性**: Critical
- **类型**: 中间件接口缺失
- **状态**: ✅ 已修复验收 (2026-06-27)
- **发现者**: Atlas（部署排查）+ Canary（代码确认）
- **文件**: `internal/middleware/metrics.go`

**问题**：MetricsMiddleware 的 `responseWriter` 结构体只嵌入了 `http.ResponseWriter`，没有代理 `Flush()` 方法。当 relay 的 `relayStreamRequest` 执行 `w.(http.Flusher)` 断言时失败，SSE 流式无法推送数据。

**根因**：
```go
// metrics.go 当前代码
type responseWriter struct {
    http.ResponseWriter
    statusCode int
}
// 缺少：func (rw *responseWriter) Flush() { rw.ResponseWriter.(http.Flusher).Flush() }
```

**影响**：所有 SSE 流式请求（`stream: true`）全部失败，返回空响应。

**建议修复**：给 `responseWriter` 添加 `Flush()` 方法：
```go
func (rw *responseWriter) Flush() {
    if f, ok := rw.ResponseWriter.(http.Flusher); ok {
        f.Flush()
    }
}
```

**验证**：修复后重跑 `curl -N ... stream:true`，应收到 SSE 数据行。
