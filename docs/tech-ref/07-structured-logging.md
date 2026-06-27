# Structured Logging（结构化日志）

> **版本**：v1.0
> **日期**：2026-06-24
> **用途**：开发团队理解日志架构和使用方式的核心参考
> **变更**：初始版本，覆盖 G2 迭代实现

---

## 一、设计目标

| 目标 | 说明 |
|------|------|
| 零外部依赖 | 基于标准库 `log` 包，不引入 zap/zerolog 等第三方库 |
| 结构化输出 | 每行日志包含固定字段，便于 grep/awk/ELK 解析 |
| 请求级追踪 | 通过 `request_id` 串联同一请求的全部日志 |
| 渐进式上下文 | `With()` 方法支持在请求中途追加字段（如 channel_id） |

---

## 二、日志格式

### 2.1 输出格式

```
[seq] LEVEL key1=value1 key2=value2 ... message
```

**字段说明**：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `seq` | uint64 | ✅ | 全局自增序列号，用于排序 |
| `LEVEL` | string | ✅ | `DEBUG` / `INFO` / `WARN` / `ERROR` |
| `request_id` | string | ❌ | 请求追踪 ID |
| `org_id` | uint | ❌ | 组织 ID |
| `channel_id` | uint | ❌ | 渠道 ID |
| `user_id` | uint | ❌ | 用户 ID |
| `module` | string | ❌ | 模块名（如 `relay`、`billing`） |

### 2.2 示例输出

```
[1] INFO request_id=abc-123 org_id=1 module=relay request received model=gpt-4 method=POST
[2] INFO request_id=abc-123 org_id=1 channel_id=5 module=relay relay succeeded duration=1.2s
[3] ERROR request_id=def-456 org_id=2 module=relay relay failed upstream=500 attempts=3
```

---

## 三、API 使用

### 3.1 创建 Logger

```go
import "github.com/uniroute/unirouter/internal/logger"

// 带基础字段创建
log := logger.New(logger.Fields{
    RequestID: requestID,
    OrgID:     orgID,
    Module:    "relay",
})

// 无字段创建（用于全局日志）
log := logger.New(logger.Fields{})
```

### 3.2 日志方法

```go
log.Debug("starting provider selection model=%s", model)
log.Info("request received method=%s path=%s", r.Method, r.URL.Path)
log.Warn("upstream slow duration=%v", duration)
log.Error("relay failed err=%v attempts=%d", err, attempts)
```

### 3.3 追加字段（With）

```go
// 在请求中途获取更多上下文后，创建子 logger
childLog := log.With(logger.Fields{
    ChannelID: channel.ID,
    UserID:    userID,
})

childLog.Info("channel selected provider=%s", channel.Provider)
```

**继承规则**：
- 子 logger 继承父 logger 的所有字段
- 子 logger 的非零字段覆盖父 logger 的同名字段
- 父 logger 不受影响

---

## 四、在 Relay 中的集成

### 4.1 集成点

`internal/gateway/relay.go` 中的 `Relay()` 方法在以下节点记录日志：

| 节点 | 级别 | 字段 | 说明 |
|------|------|------|------|
| 请求入口 | INFO | model, method | 收到转发请求 |
| 渠道选择 | INFO | channel_id, provider | 选中的上游渠道 |
| 上游请求 | DEBUG | url, timeout | 发送到上游的请求详情 |
| 上游响应 | INFO | status, duration, tokens | 上游返回结果 |
| 重试 | WARN | attempt, reason | 触发重试 |
| 最终失败 | ERROR | attempts, last_err | 所有重试耗尽 |
| 结算完成 | INFO | cost, freeze_id | 计费结算结果 |

### 4.2 代码示例

```go
func (r *Relay) Relay(ctx context.Context, ...) {
    log := logger.New(logger.Fields{
        RequestID: requestID,
        OrgID:     orgID,
        Module:    "relay",
    })

    log.Info("request received model=%s method=%s", model, method)

    channel, err := r.router.Select(ctx, model)
    if err != nil {
        log.Error("channel selection failed err=%v", err)
        return nil, err
    }

    log = log.With(logger.Fields{ChannelID: channel.ID})
    log.Info("channel selected provider=%s", channel.Provider)
    // ...
}
```

---

## 五、配置与扩展

### 5.1 日志级别

当前实现中所有级别都输出。如需动态级别控制，可扩展 `Logger` 添加级别过滤：

```go
// 未来扩展方向
type Logger struct {
    fields Fields
    level  string // 最低输出级别
}
```

### 5.2 输出目标

默认输出到 `stderr`（标准库 `log` 的默认行为）。可通过 `SetOutput` 切换：

```go
f, _ := os.Create("/var/log/unirouter.log")
logger.SetOutput(f)
```

### 5.3 与 ELK/Loki 集成

当前格式为 `key=value`，可通过 Logstash/Grok 或 Loki 的 `pattern` 解析器提取字段。如需 JSON 格式，可在生产环境扩展 `basePrefix()` 方法输出 JSON 行。

---

## 六、测试覆盖

测试文件：`internal/logger/logger_test.go`

| 测试用例 | 覆盖内容 |
|---------|---------|
| `TestLogger_Info` | 基本日志输出格式 |
| `TestLogger_With` | 字段继承和覆盖 |
| `TestLogger_Fields` | 各字段的输出验证 |
