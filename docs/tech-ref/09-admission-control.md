# Admission Control（准入控制/并发限流）

> **版本**：v1.0
> **日期**：2026-06-24
> **用途**：开发团队理解并发限流架构的核心参考
> **变更**：初始版本，覆盖 G4 迭代实现

---

## 一、设计目标

| 目标 | 说明 |
|------|------|
| 双层防护 | 全局并发上限 + 单组织并发上限 |
| 非阻塞 | 请求立即返回 429，不排队等待 |
| 无外部依赖 | 基于 Go channel 实现信号量，不依赖 Redis |
| 公平性 | 全局限流不偏向任何组织 |

---

## 二、架构

### 2.1 双层信号量模型

```
                    ┌─────────────────────────┐
                    │   Global Semaphore (500)  │
                    │   ████████░░░░░░░░░░░░░░  │
                    └────────────┬────────────┘
                                 │
              ┌──────────────────┼──────────────────┐
              ▼                  ▼                  ▼
     ┌────────────────┐ ┌────────────────┐ ┌────────────────┐
     │ Org 1 Sem (50) │ │ Org 2 Sem (50) │ │ Org 3 Sem (50) │
     │ ██░░░░░░░░░░░░ │ │ ████████████░░ │ │ ░░░░░░░░░░░░░░ │
     └────────────────┘ └────────────────┘ └────────────────┘
```

### 2.2 请求流程

```
请求到达
    │
    ▼
全局信号量获取（非阻塞）
    │
    ├── 失败 → 返回 429 Too Many Requests
    │
    ▼
组织信号量获取（非阻塞）
    │
    ├── 失败 → 释放全局信号量 → 返回 429
    │
    ▼
执行请求处理
    │
    ▼
释放组织信号量 + 全局信号量
```

**关键约束**：
- 全局获取必须在组织获取之前
- 组织获取失败时必须释放已获取的全局信号量
- 释放顺序与获取顺序相反

---

## 三、默认配置

| 场景 | 全局上限 | 单组织上限 | 说明 |
|------|---------|-----------|------|
| 默认（开发/测试） | 100 | 10 | `NewDefaultAdmissionController()` |
| 生产环境 | 500 | 50 | `NewProductionAdmissionController()` |
| 自定义 | 用户指定 | 用户指定 | `NewAdmissionController(global, perOrg)` |

---

## 四、API 使用

### 4.1 创建控制器

```go
import "github.com/uniroute/unirouter/internal/middleware"

// 默认配置
ac := middleware.NewDefaultAdmissionController()

// 生产配置
ac := middleware.NewProductionAdmissionController()

// 自定义配置
ac := middleware.NewAdmissionController(1000, 100)
```

### 4.2 作为 HTTP 中间件使用

```go
// 提取 orgID 的函数（生产环境中从 JWT 解析）
orgIDExtractor := func(r *http.Request) int {
    claims, _ := middleware.GetClaimsFromContext(r.Context())
    if claims != nil {
        return claims.OrgID
    }
    return 0
}

// 创建中间件
admissionMiddleware := middleware.AdmissionMiddleware(ac, orgIDExtractor)

// 注册到路由链
handler := admissionMiddleware(nextHandler)
```

### 4.3 直接调用（非 HTTP 场景）

```go
if !ac.TryAcquire(orgID) {
    return errors.New("too many concurrent requests")
}
defer ac.Release(orgID)

// 执行业务逻辑
```

### 4.4 查看当前状态

```go
globalAvail, orgAvail := ac.Stats()
// globalAvail: 全局可用槽位数
// orgAvail: map[int]int, 每个组织的可用槽位数
```

---

## 五、错误响应

当请求被准入控制拒绝时，返回 HTTP 429：

```json
{
    "error": {
        "type": "rate_limit.exceeded",
        "message": "Too many concurrent requests"
    }
}
```

**与其他限流的区别**：

| 限流类型 | 层级 | 实现 | 粒度 |
|---------|------|------|------|
| RPM 限流 | 请求频率 | Redis + 滑动窗口 | Key 级 |
| 准入控制 | 并发数 | Go channel 信号量 | 全局 + 组织级 |

---

## 六、中间件链中的位置

```
CORS → Metrics → Admission → Router
                                    │
                    ┌───────────────┼───────────────┐
                    ▼               ▼               ▼
              GatewayAuth     RateLimit(RPM)     Handler
```

**设计考量**：
- Admission 在 Metrics 之后：确保被拒绝的请求也被记录
- Admission 在 GatewayAuth 之前：先做粗粒度限流，减少鉴权开销
- 与 RPM 限流互不干扰：一个控制并发数，一个控制频率

---

## 七、并发安全

### 7.1 实现机制

```go
type AdmissionController struct {
    mu        sync.Mutex           // 保护 orgLimits map
    orgLimits map[int]*semaphore   // 每个组织的信号量
    orgMax    int                  // 单组织上限
    globalMax int                  // 全局上限
    globalSem *semaphore           // 全局信号量
}

type semaphore struct {
    ch chan struct{}  // 带缓冲的 channel 作为信号量
}
```

### 7.2 安全保证

| 操作 | 线程安全 | 机制 |
|------|---------|------|
| `TryAcquire` | ✅ | channel 操作天然并发安全；map 访问受 `mu` 保护 |
| `Release` | ✅ | channel 操作天然并发安全；map 访问受 `mu` 保护 |
| `Stats` | ✅ | 受 `mu` 保护 |
| org map 扩展 | ✅ | 惰性创建，受 `mu` 保护 |

---

## 八、测试覆盖

测试文件：`internal/middleware/admission_test.go`

| 测试用例 | 覆盖内容 |
|---------|---------|
| `TestAdmissionController_BasicAcquireRelease` | 基本获取/释放流程 |
| `TestAdmissionController_PerOrgLimit` | 单组织并发上限 |
| `TestAdmissionController_GlobalLimit` | 全局并发上限 |
| `TestAdmissionController_ConcurrentAccess` | 100 并发 goroutine 安全性 |
| `TestAdmissionController_Stats` | 状态查询正确性 |

---

## 九、扩展方向

| 方向 | 说明 |
|------|------|
| 动态调整 | 支持运行时修改上限，无需重启 |
| Redis 分布式 | 多实例部署时使用 Redis 实现跨实例限流 |
| 优先级队列 | VIP 组织可突破普通限流 |
| 指标集成 | 将拒绝次数接入 Prometheus 计数器 |
