# Logs Explorer v3

> **Version**: 3.0（文件路径保留用于现有链接兼容）
> **Status**: ✅ V0 已实现并完成本地测试/构建验证
> **Last Updated**: 2026-07-31
> **Priority**: P0（高优先级）

---

## 1. 设计目标

将 Logs 从“点开详情的日志表格”升级为“执行决策凭证工作区”。管理员进入页面
即可看到最新一条执行的权限、路由、策略、财务终态、Provenance 和 Capture Gap；
不需要第二次点击才能开始判断。

### 用户场景

| 场景 | 问题 | 调查目标 |
|------|------|----------|
| 1 | 请求失败 | 找原因 |
| 2 | 成本上升 | 找来源 |
| 3 | 延迟上升 | 找瓶颈 |
| 4 | 服务商不稳定 | 找影响 |
| 5 | 审计使用 | 找责任主体与委托关系 |
| 6 | 费用已记录但是否真正结算 | 只依据 Billing terminal evidence 判断 |

---

## 2. 布局（三面板）

```
┌──────────┬────────────────────┬───────────────────┐
│ Filters  │ Results            │ Decision Receipt  │
│ 240px    │ Flexible           │ 360–420px         │
│          │                    │ 永久可见            │
│ 时间范围  │ Compact Table      │ - Decision         │
│ 模型     │ - Request ID       │ - Attempts         │
│ 供应商   │ - 模型             │ - Policy           │
│ 状态码   │ - 供应商           │ - Cost / Finality  │
│ 延迟范围 │ - 状态码           │ - Provenance/Gaps  │
└──────────┴────────────────────┴───────────────────┘
```

---

## 3. Header

| 属性 | 值 |
|------|-----|
| 高度 | 56px |
| 左侧 | 标题“执行请求”+ Operations 语义 |
| 右侧 | 组织范围（适用角色）+ 记录总数 + 刷新 |

---

## 4. 全局搜索

- 位置: 左侧筛选面板
- Placeholder: “请求、模型、供应商或证据标识”
- 支持: Request ID、客户端请求 ID、模型、供应商和结构化证据标识
- 快捷键: ⌘K / Ctrl+K

---

## 5. 左面板: Investigation Filters

### 筛选器列表

| 筛选器 | 类型 | 选项 |
|--------|------|------|
| 时间范围 | 快捷选择 | 最近 15min / 1h / 24h / 7d / 自定义 |
| 供应商 | Input | 精确供应商名称 |
| 模型 | Select | 从 API 动态获取 |
| 状态码 | Select | 全部 / 2xx / 4xx / 5xx / 429 / 502 / 503 |
| 计量来源 | Select | API / 估算 / 调和等已实现来源 |
| 延迟范围 | Input pair | 最小/最大毫秒 |
| 证据标识 | Exact inputs | Trace、Workflow、Agent、Attempt、Upstream、Evidence Ref、Source Hash |

### 交互

- 筛选器变更即时刷新结果（debounce 300ms）
- URL 参数同步（包含当前 `log_id`，可分享选中的执行）
- 移动端按筛选器、结果、凭证顺序堆叠；V0 尚未加入筛选器折叠

---

## 6. 中面板: Results

### 表格列

| 列 | 格式 | 说明 |
|----|------|------|
| Request ID | monospace + 复制按钮 | 可点击切换常驻凭证 |
| 模型 | 文本 | 模型名称 |
| 供应商 | Tag | 供应商名称 |
| 状态码 | Tag 着色 | 2xx 绿 / 4xx 橙 / 5xx 红 |
| 其他完整字段 | Receipt | 延迟、Token、费用、时间和证据在常驻凭证中查看 |

### 分页

- 每页 50 条
- 支持翻页

---

## 7. 右面板: Execution Decision Receipt

### 触发方式

- 服务端按 `created_at DESC` 返回结果；页面默认选择当前页第一条记录。
- 点击表格行、`ArrowUp`/`ArrowDown` 或 URL `log_id` 切换凭证。
- 面板永久存在，不提供关闭按钮，不要求第二次点击展开。

### 内容分区

| 区域 | 内容 |
|------|------|
| Decision | 执行结论、权限主体、委托和结构化决策链 |
| Attempts | 路由尝试、故障切换、上游结果、延迟和错误 |
| Policy & Guardrail | 只展示已捕获的策略/Guardrail 决策和 Payload 边界 |
| Cost & Finality | Billing terminal evidence、当前脱敏合同缺口、微元费用和 Prompt Cache |
| Provenance & Gaps | Trace/Schema/Source、Evidence refs/hash、Capture Gap 和事件路径 |

### 交互

- 面板不可关闭；空列表仍保留可解释的空态
- 1280px 以上三列并排；中等宽度筛选器置顶、列表与凭证并排；移动端顺序堆叠
- 支持复制单个字段
- 财务、权限、策略和 Capture Gap 均采用 fail-closed 文案，不从 HTTP/日志字段反推

---

## 8. 暂缓功能

| 功能 | 状态 | 说明 |
|------|------|------|
| Saved Views | 暂缓 | 当前筛选器够用，后续迭代 |
| Explorer Modes | 暂缓 | 单一视图模式，后续按需拆分 |
| 跨产品 Trace 拼接 | 暂缓 | 本页只展示 UniRouter 当前已捕获 Trace；不依赖 X-Insight |
| 导出 | 暂缓 | 后续迭代 |
| Cost 筛选 Low/Medium/High | 不做 | 用户关心具体金额，不是模糊等级 |

---

## 9. 数据依赖

| 数据 | API | 当前状态 |
|------|-----|----------|
| 日志列表 | `GET /api/v1/logs` | ✅ 已实现 |
| 执行 Trace | `GET /api/v1/logs/{id}/trace` | ✅ 已实现 |
| 模型列表（筛选器） | `GET /api/v1/model-names` | ✅ 已实现 |

> **注意**: V0 复用现有 API/DTO，不新增后端语义。只有明确的 Billing terminal
> event 能证明财务终态；HTTP 200 和日志费用都不能证明 `settled`。当前脱敏 Trace
> 不暴露 `usage_authority`、`usage_finality` 或 `usage_purpose`，页面明确显示该缺口，
> 不通过 UI 推断补值。
