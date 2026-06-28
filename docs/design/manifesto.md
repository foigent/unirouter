# UniRouter Design Manifesto v1.0

> **Version**: 1.0  
> **Status**: 定稿  
> **Last Updated**: 2026-06-28  
> **Audience**: Product / Design / Frontend / Founder  
> **Based on**: GPT 设计咨询 + 团队讨论确认

---

## 1. 产品定位

UniRouter 是基于 OneAPI（Go, MIT）深度 Fork 的 **企业 AI 网关管控平台**。

核心能力：
- **路由**: 多渠道智能路由，自动故障转移
- **计费**: Token 级精确计费，预冻结机制
- **监控**: 实时调用日志，延迟/成功率/成本可视化
- **治理**: 用户管理，权限控制，审计日志

当前阶段定位：**个人开发者 / 小团队的 AI 网关管理工具**

未来演化方向：**企业级 AI 活动管控平台**（多租户、组织治理、合规审计）

> 注：不做聊天机器人、不做 AI 助手、不做 prompt 工具、不做模型市场。

---

## 2. 设计原则

### 原则 1：Show Activity, Not Configuration

仪表盘应该回答"今天 AI 在干什么"，而不是"系统里有几条配置记录"。

| 不好 | 好 |
|------|-----|
| 渠道数量 | 今日请求数 |
| API Key 数量 | 成功率 |
| 配置表 | 趋势图 |

### 原则 2：High Information Density

管理工具不是营销页面。屏幕空间是稀缺资源，应该用来展示信息，不是留白和装饰。

### 原则 3：Progressive Complexity

高频操作零摩擦（一键测试、一键充值），低频操作不干扰（高级配置藏在二级入口）。

### 原则 4：No Decoration

- 不加毛玻璃效果
- 不加装饰性渐变
- 不加抽象图标
- 不加自动播放动画
- 不加"加载成功"确认框

每 100ms 的视觉延迟都会降低运维效率。

### 原则 5：Direct Manipulation

用户应该能直接操作数据，而不是通过表单间接操作。

---

## 3. 当前问题诊断

### 3.1 仪表盘是"配置展示页"

当前 Dashboard 展示的是：
- 模型价格表（配置数据）
- 最近调用表（无分析价值）

应该展示的是：
- 今日请求数 / 成功率 / 平均延迟 / 今日花费（活动信号）
- 请求趋势图（活动趋势）
- 模型用量分布（活动分布）
- 渠道健康状态（系统健康）

### 3.2 导航无层级

14 个菜单项平铺，用户需要扫描全部才能找到目标。应该按功能域分组。

### 3.3 页面模板不统一

有的页面用 Card + Table，有的用 Form + Modal，没有一致的交互模式。用户在不同页面需要重新学习布局。

### 3.4 Logs 是"日志表格"不是"调查工具"

当前 Logs 页面只有简单的筛选和分页。运维人员看日志是为了**调查问题**，不是为了**浏览记录**。

---

## 4. 导航重组

### 当前：14 个平铺菜单

### 目标：5 个功能域分组

```
Overview
└── Dashboard

Traffic
├── Channels
├── Upstream Keys（上游密钥）
├── API Keys（密钥管理）
├── Logs
└── Channel Test（渠道测试）

Billing
├── Wallet
├── Model Pricing（模型价格）
├── Organization Multipliers（组织倍率）
├── Token Reconciliation（Token 对账）
└── Grants（赠送管理）

Access
├── Users
└── Audit Logs

System
└── Settings
```

**为什么这样分：**
- **Overview**: 全局视图，每天打开一次
- **Traffic**: AI 流量相关，运维高频操作
- **Billing**: 成本相关，财务/运营关注
- **Access**: 权限相关，管理员关注
- **System**: 配置，低频操作

---

## 5. 页面模板系统

所有页面使用三种模板之一：

### 模板 A：资源管理
适用于：Channels, Keys, Users, Pricing, Settings

结构：Page Header → Filter Bar → Data Table → Action Drawer/Modal

### 模板 B：探索器
适用于：Logs, Audit Logs, Reconciliation

结构：Search Panel → Filters → Result Table → Detail Panel

### 模板 C：分析视图
适用于：Dashboard

结构：KPI Row → Charts → Activity Feed

---

## 6. 未来演化路径

当产品从小团队扩展到企业级时，按需引入：

| 阶段 | 能力 | 触发条件 |
|------|------|----------|
| 当前 | 基础 CRUD + 监控 | — |
| +1 | Dark Mode | 用户反馈需求 |
| +2 | 多主题支持 | 企业客户定制需求 |
| +3 | 多租户 UI | 组织架构扩展 |
| +4 | 治理视图 | 合规审计需求 |

> 注：每个阶段的 UI 改动应该是增量的，不是推倒重来。当前的模板系统和导航架构为未来留了空间。

---

## 7. 参考

- [GPT 原始设计宣言](../consultations/ui-redesign-gpt/design-manifesto.md)
- [设计规范 v1](spec-v1.md)
- [PRD-Phase1.md](../PRD-Phase1.md)
