# UniRouter Design Specification v1

> **Version**: 1.0  
> **Status**: 定稿  
> **Last Updated**: 2026-06-28  
> **Scope**: 现有功能 UI 改版，不新增功能  
> **Component Library**: Semi Design（继续使用）

---

## 1. 设计目标

将 UniRouter 从 CRUD 管理面板升级为 **AI 活动管控平台**。

优先级：
1. **P1 可见性** — 用户能快速理解 AI 活动状态
2. **P2 控制** — 用户能高效执行操作
3. **P3 一致性** — 统一的页面模板和交互模式

每个页面必须回答：
- What is happening?
- Is it healthy?
- What needs attention?
- What action should I take?

---

## 2. 导航架构

### 侧边栏

| 属性 | 值 |
|------|-----|
| 宽度 | 240px（展开）/ 72px（折叠） |
| 头部高度 | 56px |
| 分组数 | 5 |

### 菜单分组

| 分组 | 菜单项 | 说明 |
|------|--------|------|
| **Overview** | Dashboard | 全局视图 |
| **Traffic** | Channels, Upstream Keys, API Keys, Logs, Channel Test | AI 流量相关 |
| **Billing** | Wallet, Model Pricing, Org Multipliers, Token Reconciliation, Grants | 成本相关 |
| **Access** | Users, Audit Logs | 权限相关 |
| **System** | Settings | 配置 |

### 折叠状态

- 折叠时只显示图标 + Tooltip
- 分组标题在折叠状态下隐藏
- 当前页面对应的菜单项高亮

---

## 3. 页面模板

### 模板 A：资源管理

**适用页面**: Channels, Upstream Keys, API Keys, Users, Model Pricing, Org Multipliers, Grants, Settings

**结构**:
```
┌─────────────────────────────────────────────┐
│ Page Header (标题 + 操作按钮)                │
├─────────────────────────────────────────────┤
│ Filter Bar (筛选器 + 搜索)                   │
├─────────────────────────────────────────────┤
│ Data Table (Semi Table)                      │
│ - 分页                                       │
│ - 行操作（编辑/删除）                         │
│ - 状态列用 Tag 着色                          │
├─────────────────────────────────────────────┤
│ Action Drawer/Modal (新增/编辑表单)           │
└─────────────────────────────────────────────┘
```

**交互规范**:
- 新增/编辑用 Drawer（侧边抽屉），不用 Modal（居中弹窗）
- 删除用 Modal.confirm 二次确认
- 表单验证在提交时触发
- 成功后 Toast 提示 + 刷新列表

### 模板 B：探索器

**适用页面**: Logs, Audit Logs, Token Reconciliation

**结构**:
```
┌──────────┬────────────────────┬──────────────┐
│ Left     │ Center             │ Right        │
│ 280px    │ Flexible           │ 420px        │
│          │                    │              │
│ Filters  │ Result Table       │ Detail Panel │
│          │ (分页)             │ (选中行)     │
│          │                    │              │
└──────────┴────────────────────┴──────────────┘
```

**交互规范**:
- 筛选器变更即时刷新结果（debounce 300ms）
- 点击行展开右侧详情面板
- 详情面板可关闭，关闭后表格占满宽度
- 支持导出当前筛选结果

### 模板 C：分析视图

**适用页面**: Dashboard

**结构**:
```
┌─────────────────────────────────────────────┐
│ Header + Time Range Selector                 │
├─────────────────────────────────────────────┤
│ KPI Row (4 个统计卡片)                       │
├─────────────────────────────────────────────┤
│ Charts (趋势图 + 分布图)                     │
├─────────────────────────────────────────────┤
│ Activity Feed (最近请求 + 最近事件)           │
└─────────────────────────────────────────────┘
```

---

## 4. Dashboard 设计

### 4.1 目标

- **主目标**: 10 秒内理解企业 AI 活动状态
- **次目标**: 30 秒内识别异常

### 4.2 布局

```
┌─────────────────────────────────────────────┐
│ Dashboard                    [时间范围] [刷新] │
├─────────┬─────────┬─────────┬───────────────┤
│ 总请求数 │ 成功率   │ 平均延迟 │ 今日花费      │
│ 2,493K  │ 99.82%  │ 127ms  │ ¥1,248       │
│ +12.5%  │ +0.12%  │ -8%    │ +15%         │
├─────────┴─────────┴─────────┴───────────────┤
│ AI 活动趋势 (Area Chart)                     │
│ - 主指标: 请求数                              │
│ - 可切换: 成本 / 延迟                         │
├──────────────────────┬──────────────────────┤
│ 模型活动分布          │ 渠道健康状态          │
│ (Horizontal Bar)      │ (Card Grid)          │
│ Top 5 模型按请求数    │ 每个渠道: 名称+状态   │
├──────────────────────┴──────────────────────┤
│ 最近请求 (Table, 最近 10 条)                  │
│ ID | 模型 | 状态码 | 延迟 | Token | 费用 | 时间│
└─────────────────────────────────────────────┘
```

### 4.3 KPI 卡片

| 卡片 | 数据 | 颜色 | Delta |
|------|------|------|-------|
| 总请求数 | `total_requests` | 蓝 #0077FA | vs 昨日 |
| 成功率 | `success_rate * 100`% | 绿 #05944F | vs 昨日 |
| 平均延迟 | `avg_latency_ms` ms | 橙 #F5A623 | vs 昨日（负值=改善） |
| 今日花费 | `today_cost_cents / 100` ¥ | 紫 #7B61FF | vs 昨日 |

**Delta 规则**:
- 正值绿色（改善）：成功率提升、延迟下降
- 负值红色（恶化）：成功率下降、延迟上升、花费超预期
- 与昨日对比（不是与上周）

### 4.4 趋势图

- 使用 `daily_trend` 数据（需确认后端 API 已返回）
- 主指标：请求数（Area Chart）
- 可切换：成本、延迟
- 时间范围：24h / 7d / 30d

### 4.5 模型活动分布

- Horizontal Bar Chart
- Top 5 模型按请求数排序
- 显示：模型名 + 请求数 + 占比

### 4.6 渠道健康状态

- Card Grid 布局
- 每个渠道一张卡片：名称 + 状态 Tag + 失败数
- 状态颜色：active=绿, disabled=红, cooldown=橙

### 4.7 最近请求

- Table，最近 10 条
- 列：Request ID, 模型, 状态码, 延迟, Token, 费用, 时间
- 状态码着色：2xx 绿, 4xx 橙, 5xx 红
- 点击行跳转到 Logs 页面（带 request_id 筛选）

### 4.8 移除的内容

| 移除 | 原因 |
|------|------|
| 模型价格表 | 配置数据，不是活动信号。移到 Model Pricing 页面 |
| 预算使用提示 | 整合到"今日花费"KPI 卡片的 Budget Status |

---

## 5. Logs Explorer 设计

### 5.1 目标

将 Logs 从"日志表格"升级为"调查工作区"。

### 5.2 用户场景

1. 请求失败 → 找原因
2. 成本上升 → 找来源
3. 延迟上升 → 找瓶颈
4. 服务商不稳定 → 找影响
5. 审计使用 → 找责任人

### 5.3 布局（三面板）

```
┌──────────┬────────────────────┬──────────────┐
│ Filters  │ Results            │ Details      │
│ 280px    │ Flexible           │ 420px        │
│          │                    │              │
│ 时间范围  │ Table              │ 请求详情     │
│ 模型     │ - Request ID       │ - Headers    │
│ 渠道     │ - 模型             │ - Body       │
│ 状态码   │ - 渠道             │ - Response   │
│ 延迟范围 │ - 状态码           │ - Timing     │
│          │ - 延迟             │ - Cost       │
│          │ - Token            │              │
│          │ - 费用             │              │
│          │ - 时间             │              │
└──────────┴────────────────────┴──────────────┘
```

### 5.4 筛选器

| 筛选器 | 类型 | 选项 |
|--------|------|------|
| 时间范围 | 快捷选择 | 最近 15min / 1h / 24h / 7d / 自定义 |
| 模型 | Multi Select | 从 API 动态获取 |
| 渠道 | Multi Select | 从 API 动态获取 provider |
| 状态码 | Select | 全部 / 2xx / 4xx / 5xx / 429 / 502 / 503 |
| 延迟范围 | Range Slider | 0-100ms / 100-500ms / 500-1000ms / 1000ms+ |

**交互**:
- 筛选器变更即时刷新结果（debounce 300ms）
- URL 参数同步（可分享筛选条件）
- 筛选器可折叠（移动端友好）

### 5.5 结果表格

- 列：Request ID, 模型, 渠道, 状态码, 延迟, Token, 费用, 时间
- Request ID 显示为 monospace + 复制按钮
- 状态码 Tag 着色
- 分页：每页 50 条，支持翻页

### 5.6 详情面板

点击行展开右侧详情面板：

| 区域 | 内容 |
|------|------|
| 基本信息 | Request ID, 模型, 渠道, 用户, 时间 |
| 性能 | 延迟, Token 用量, 费用 |
| 错误 | Error Code, Error Message（如有） |
| 请求 | Headers, Body（如有） |
| 响应 | Status Code, Body（如有） |

**交互**:
- 详情面板可关闭
- 关闭后表格占满宽度
- 支持复制单个字段

### 5.7 全局搜索

- 顶部搜索栏，全宽
- 支持：Request ID, 模型, 渠道, 用户, 错误码
- 快捷键：⌘K / Ctrl+K

### 5.8 暂缓功能

| 功能 | 状态 | 说明 |
|------|------|------|
| Saved Views | 暂缓 | 当前筛选器够用，后续迭代 |
| Explorer Modes | 暂缓 | 单一视图模式，后续按需拆分 |
| Trace 关联 | 暂缓 | 需要后端支持 Trace ID |
| 导出 | 暂缓 | 后续迭代 |

---

## 6. 通用组件规范

### 6.1 状态着色

| 状态 | 颜色 | 使用场景 |
|------|------|----------|
| Success / Active | 绿 #05944F | 成功请求、活跃渠道、启用状态 |
| Warning / Cooldown | 橙 #F5A623 | 警告、冷却中、延迟偏高 |
| Error / Disabled | 红 #DD2C00 | 错误请求、禁用状态、熔断打开 |
| Info | 蓝 #0077FA | 信息提示、一般状态 |
| Neutral | 灰 #6B7280 | 未知、无数据 |

### 6.2 Tag 使用

- 渠道状态：`active` / `disabled` / `cooldown` → Tag 着色
- 熔断状态：`closed` / `open` / `half_open` → Tag 着色
- 审计动作：`create` / `update` / `delete` / `login` / `topup` → Tag 着色
- 请求状态码：2xx 绿 / 4xx 橙 / 5xx 红

### 6.3 表格规范

- 使用 Semi Table 组件
- 默认分页：每页 20 条（可选 50/100）
- 行操作：编辑 / 删除 按钮在右侧
- 空状态：显示"暂无数据"提示
- 加载状态：Skeleton Loading

### 6.4 表单规范

- 使用 Semi Form 组件
- 新增/编辑用 Drawer（侧边抽屉）
- 删除用 Modal.confirm 二次确认
- 表单验证在提交时触发
- 成功后 Toast 提示 + 刷新列表

### 6.5 时间显示

- 使用 `formatDateTime()` 工具函数
- 格式：`YYYY-MM-DD HH:mm:ss`
- 相对时间：最近 24h 内显示"X 分钟前"，否则显示绝对时间

---

## 7. 间距和尺寸

| 元素 | 值 |
|------|-----|
| 页面内边距 | 24px（桌面）/ 16px（笔记本） |
| 卡片间距 | 16px |
| 表格行高 | 48px |
| 按钮高度 | 32px |
| 输入框高度 | 32px |
| 图标尺寸 | 16px（行内）/ 20px（导航）/ 24px（页面标题） |

---

## 8. 实施计划

### Phase 1：导航重组
- 侧边栏分组
- 菜单项重新排列
- 折叠状态支持

### Phase 2：Dashboard 重设计
- KPI 卡片
- 趋势图（需确认 daily_trend API）
- 模型活动分布
- 渠道健康状态

### Phase 3：Logs Explorer 升级
- 三面板布局
- 筛选器增强
- 详情面板
- 全局搜索

### Phase 4：页面模板统一
- 资源管理页面统一为 Template A
- 探索器页面统一为 Template B

> 注：每个 Phase 独立可交付，不阻塞其他 Phase。

---

## 9. 参考

- [GPT 原始设计规范](../consultations/ui-redesign-gpt/design-spec-v1.md)
- [Design Manifesto](manifesto.md)
- [Dashboard Wireframe](pages/dashboard-v2.md)
- [Logs Explorer Spec](pages/logs-explorer-v2.md)

---

## 10. Body 存储决策

### 10.1 问题

请求/响应 Body 是否应该存储在数据库中？

### 10.2 决策

**默认不存储 Body，仅存储元数据。**

### 10.3 原因

| 因素 | 分析 |
|------|------|
| **数据量** | Body 可能很大（10KB-100KB+），日志量大时存储成本高 |
| **隐私** | Body 可能包含敏感信息（PII、商业机密） |
| **性能** | 大字段影响查询性能，增加数据库备份时间 |
| **需求** | 大部分场景只需要元数据（模型、Token、延迟、状态码） |

### 10.4 已存储字段

| 字段 | 说明 |
|------|------|
| `request_id` | 请求唯一标识 |
| `model` | 请求的模型 |
| `channel_id` | 渠道 ID |
| `channel_name` | 渠道名称 |
| `user_id` | 用户 ID |
| `status_code` | HTTP 状态码 |
| `latency_ms` | 请求延迟（毫秒） |
| `prompt_tokens` | 输入 Token 数 |
| `completion_tokens` | 输出 Token 数 |
| `total_tokens` | 总 Token 数 |
| `cost` | 费用 |
| `error_code` | 错误码（如有） |
| `error_message` | 错误信息（如有） |
| `ip` | 客户端 IP |
| `user_agent` | User Agent |
| `created_at` | 创建时间 |

### 10.5 可选扩展

如果未来需要存储 Body，建议：

1. **独立存储**：使用对象存储（S3/OSS），数据库只存引用
2. **采样存储**：仅存储错误请求或高延迟请求的 Body
3. **TTL 策略**：Body 自动过期（如 7 天）

### 10.6 当前实现

- 日志表 `logs` 仅存储元数据
- 详情面板展示已存储的元数据字段
- Body 字段在详情面板中标注"未存储"
