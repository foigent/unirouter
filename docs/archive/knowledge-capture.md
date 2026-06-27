# UniRouter 知识沉淀：Anti-Patterns & Architectural Lessons

> **来源**: BUG-LOG.md 14 个 bug 的代码级验证 + 多 agent 协作实践
> **验证日期**: 2026-06-27
> **验证方法**: 逐个 bug 对照当前代码确认修复状态和正确模式

---

## 1. HTTP 中间件接口代理遗漏 (Bug #12)

**Anti-Pattern**: 自定义 `ResponseWriter` wrapper 只嵌入了 `http.ResponseWriter`，没有代理 `http.Flusher` 等下游接口。

**后果**: SSE 流式响应中 `w.(http.Flusher)` 断言失败，数据无法推送。

**正确模式**:
```go
// internal/middleware/metrics.go:108-113
func (rw *responseWriter) Flush() {
    if f, ok := rw.ResponseWriter.(http.Flusher); ok {
        f.Flush()
    }
}
```

**教训**: Go 的接口是隐式实现的。当你包装一个接口时，你必须为所有下游消费者可能断言的接口都提供代理方法。建议在 wrapper 上声明所有可能的接口并用 `ok` 检查代理，而不是假设没人会做类型断言。

---

## 2. SQL Schema-Code 漂移 (Bug #0, #0.1, #2)

**Anti-Pattern**: Go 代码引用了数据库中不存在的列，导致 `SQLSTATE` 25P02 错误，并在同一事务内级联传播。

**具体场景**:
- `freeze_idempotency` 表缺少 `org_id` 和 `frozen_amount` 列
- `grants` 表缺少 `client_request_id` 列
- `grants.created_by` 设为 NOT NULL 但代码无用户上下文

**后果**: 所有 billing 集成测试全挂（级联于 Bug #0 和 #0.1），PostgreSQL 事务中一个语句失败后，同一事务中所有后续语句都返回 25P02。

**修复**: `migrations/000006_fix_schema_gaps.up.sql`:
```sql
ALTER TABLE freeze_idempotency ADD COLUMN IF NOT EXISTS org_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE freeze_idempotency ADD COLUMN IF NOT EXISTS frozen_amount INTEGER NOT NULL DEFAULT 0;
ALTER TABLE grants ADD COLUMN IF NOT EXISTS client_request_id VARCHAR(64);
ALTER TABLE grants ALTER COLUMN created_by DROP NOT NULL;
```

**教训**: 
1. 每次 migration 后应运行集成测试验证 schema-code 一致性
2. PostgreSQL 的 `SQLSTATE 25P02` (current transaction is aborted) 是级联错误，根因往往在第一个失败的 SQL 语句
3. `defer tx.Rollback(ctx)` 是惯用模式——如果 `Commit` 成功，`Rollback` 是 no-op

---

## 3. 文档错误 ≠ 代码错误 (Bug #5)

**Anti-Pattern**: 文档声称 channel key 使用 XOR 加密，但代码实际已用 AES-256-GCM。

**验证**: `internal/auth/auth.go` 确认使用 `cipher.NewGCM` + `io.ReadFull(rand.Reader, nonce)`，是标准的 AES-256-GCM 实现。

**后果**: BUG-LOG 记录为 "Critical (生产部署阻断)"，但实际上只是文档误导，代码本身是安全的。

**教训**:
1. 文档和代码不同步是常见的，审查时应以代码为准
2. 加密代码的正确性验证应看实际实现，不要信任文档描述
3. 这种"文档声称安全漏洞但代码实际安全"的情况应区分标注，避免恐慌

---

## 4. Go defer 在循环中的资源泄漏 (Bug #3)

**Anti-Pattern**: 在 `for` 循环内使用 `defer`，导致循环 N 次后才有 N 个 deferred 调用，资源在函数结束前一直被持有。

**正确模式** (当前 relay.go):
```go
// 函数级别 defer —— 资源在函数返回时立即释放
defer r.Body.Close()  // line 87

// 循环内的资源在每次迭代中显式关闭
resp.Body.Close()  // line 216, 447, 463, 701
```

**教训**:
1. `defer` 的作用域是函数，不是代码块——在循环内 defer 会在函数返回时才执行
2. 对于循环中需要立即释放的资源（如 HTTP 响应体），应显式 `Close()` 而不是 `defer Close()`
3. 如果确实需要循环内延迟释放，考虑在循环体内使用匿名函数包裹 defer

---

## 5. HTTP 请求 Body 的一次性消费 (Bug #10)

**Anti-Pattern**: 在中间件中读取 `r.Body` 检查 model 后，body 被消费完毕，下游 handler 读到空 body。

**正确模式** (`cmd/uniroute/main.go:155-179`):
```go
// 读取后缓冲
bodyBytes, err := io.ReadAll(r.Body)
if err != nil { /* handle */ }
r.Body.Close()
r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))  // 重新填充 body

// 用缓冲的 bytes 解析
var req struct { Model string `json:"model"` }
if err := json.Unmarshal(bodyBytes, &req); err == nil && req.Model != "" {
    // 检查 AllowedModels...
}
// 下游 handler 可以再次读取 r.Body
```

**教训**:
1. `http.Request.Body` 是 `io.ReadCloser`，读取后即消费
2. 需要多次读取时，用 `io.ReadAll` + `bytes.NewBuffer` + `io.NopCloser` 重新填充
3. 中间件检查请求内容后必须恢复 body，否则下游收到空请求
4. 同一个 bug 的另一面：`_ = allowed` 丢弃检查结果——确保所有条件分支都有实际效果

---

## 6. 前端硬编码 vs 动态获取 (Bug #11)

**Anti-Pattern**: 供应商下拉框写死了 4 个选项（openai/deepseek/qwen/zhipu），不随渠道变化更新。

**正确模式** (`frontend/src/pages/LogsPage.tsx`):
```tsx
const [providers, setProviders] = useState<string[]>([]);
useEffect(() => {
    api.getChannels(1, 100).then(res => {
        const unique = [...new Set(res.data?.map(ch => ch.provider) || [])];
        setProviders(unique.sort());
    });
}, []);
// 下拉框: {providers.map(p => <option key={p}>{p}</option>)}
```

**教训**:
1. 筛选条件应从数据源动态获取，不要硬编码
2. `Set` 去重 + 排序是提取唯一值的标准模式
3. 如果 API 数据为空，应有 fallback 行为

---

## 7. SSE 流式并发安全 (Bug #8)

**Anti-Pattern**: 多个 goroutine 同时写入 SSE 响应或关闭 `resp.Body`，导致竞态条件。

**正确模式** (`internal/gateway/relay.go`):
```go
// 互斥锁保护 SSE 写入
var writeMu sync.Mutex

// 余额检查通过 channel 信号，不直接关闭 body
balanceExhausted := make(chan struct{}, 1)
go func() {
    // 定期检查余额
    if balance <= 0 {
        select {
        case balanceExhausted <- struct{}{}:
        default:
        }
        return
    }
}()

// 主循环通过 channel 接收信号
select {
case <-balanceExhausted:
    // 优雅终止
default:
}

// 所有写入都加锁
writeMu.Lock()
fmt.Fprintf(w, "data: %s\n\n", jsonBytes)
flusher.Flush()
writeMu.Unlock()
```

**教训**:
1. SSE 流式场景中，主循环、心跳 goroutine、余额检查 goroutine 都可能写入同一连接
2. 用 `sync.Mutex` 保护所有写入操作
3. goroutine 间通信应使用 channel 信号，而非直接操作共享资源（如 `resp.Body.Close()`）
4. `context.WithCancel` + `defer cancel()` 确保所有 goroutine 优雅退出

---

## 8. 测试基础设施：接口解耦 (Bug #9)

**Anti-Pattern**: admin handler 直接依赖 `*pgxpool.Pool`（具体类型），无法在纯单元测试中 mock。

**正确模式** (middleware 包的解决):
```go
// 提取接口
type DBQuerier interface {
    QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row
    Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error)
    Exec(ctx context.Context, sql string, args ...interface{}) (pgtag, error)
}

// 依赖接口而非具体类型
func resolveAPIKey(ctx context.Context, q db.DBQuerier, keyHash string) (*models.ApiKey, string, error)
func GatewayAuth(q db.DBQuerier, jwtSecret string) func(http.Handler) http.Handler
```

**现状**: middleware 覆盖率从 42.7% → 98.2%。admin 包仍紧耦合 `*pgxpool.Pool`，待 Codex 重构。

**教训**:
1. Go 的接口是隐式实现的——定义一个 `DBQuerier` 接口，`*pgxpool.Pool` 自动满足
2. 测试覆盖率的瓶颈往往是架构问题（紧耦合），不是测试数量
3. 接口注入是解决数据库依赖测试的标准模式

---

## 9. Fire-and-Forget 错误处理 (Bug #4)

**场景**: `router.go:75` 中 `_, _ = r.db.Exec(ctx, UPDATE channel_keys SET last_used_at = NOW() WHERE id = $1, ck.ID)`

**判断**: 这是**有意为之**的 fire-and-forget。`last_used_at` 更新失败不影响路由核心功能（round-robin 只是优化，不是关键路径）。

**教训**:
1. 不是所有 `Exec` 错误都需要检查——判断标准是"这个操作失败是否影响核心业务逻辑"
2. 对于非关键操作（如审计日志、统计更新），忽略错误是可接受的
3. 但应在代码注释中说明忽略是有意的，避免审查者误判

---

## 10. Billing 事务模式 (Bug #7)

**正确模式** (`internal/billing/billing.go`):
```go
func (b *Billing) Settle(ctx context.Context, req SettleRequest) (*SettleResult, error) {
    tx, err := b.db.Begin(ctx)
    if err != nil { return nil, fmt.Errorf("begin tx: %w", err) }
    defer tx.Rollback(ctx)  // Commit 成功后 Rollback 是 no-op

    // 1. 查询冻结金额
    // 2. 计算退款
    // 3. 更新钱包余额
    // 4. 记录交易
    // 5. 更新幂等状态

    if err := tx.Commit(ctx); err != nil {
        return nil, fmt.Errorf("commit: %w", err)
    }
    return &SettleResult{...}, nil
}
```

**教训**:
1. `defer tx.Rollback(ctx)` + 最后 `tx.Commit(ctx)` 是 Go 事务的标准模式
2. 事务中每个错误都应 `return`，让 `defer Rollback` 自动回滚
3. SQL 参数顺序必须与 `$1, $2, ...` 占位符严格对应（Bug #7 的根因）

---

## 11. 多 Agent 协作模式

**架构**: Sentinel (代码审查) → Codex (修复) → Canary (测试) → Atlas (部署)

**协调机制**: 通过文件系统协调（无法直接通信）
- `FIX-SIGNAL.md`: Codex → Canary，标记修复完成
- `VERIFY-RESULT.md`: Canary → Codex，验收结果
- `docs/issues/`: Canary 发现的架构性问题

**教训**:
1. 多 agent 项目需要清晰的通信协议（文件命名、格式、路径）
2. 只读 agent (Canary) + 可写 agent (Codex) 的分工避免了冲突
3. 自动化验收流程（FIX-SIGNAL → go test → VERIFY-RESULT）是关键

---

## 12. 断路器状态机

**模式** (`internal/gateway/router.go`):
```
healthy → (fail_count >= threshold) → half_open → (10% 流量测试) → healthy
                                                          ↘ (再次失败) → half_open
```

**实现要点**:
- `half_open` 状态下只允许 10% 流量通过（`rand.Intn(100) >= 10` 跳过）
- `half_open_until` 时间窗口控制恢复期
- `MarkChannelSuccess` 重置为 healthy，`MarkChannelFail` 推进状态机
- Key 级别独立断路：`fail_count >= 3` 自动标记 `status = 'invalid'`

---

## 总结：Top 5 高频 Anti-Patterns

| # | Anti-Pattern | 频率 | 严重性 |
|---|-------------|------|--------|
| 1 | SQL schema-code 漂移 | 高 | Critical |
| 2 | HTTP body 重复消费 | 中 | Critical |
| 3 | 中间件接口代理遗漏 | 中 | Critical |
| 4 | defer 在循环内 | 低 | Medium |
| 5 | 硬编码 vs 动态获取 | 低 | Medium |

