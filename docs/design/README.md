# UniRouter UI 设计文档

> **Status**: 定稿（基于 GPT 设计咨询 + 团队讨论确认）
> **Last Updated**: 2026-06-29
> **Scope**: 现有功能 UI 改版，不新增功能

## 快速导航

| 文档 | 用途 | 状态 |
|------|------|------|
| [manifesto.md](manifesto.md) | 产品愿景 + 设计原则 | ✅ 定稿 |
| [spec-v1.md](spec-v1.md) | 可实施的设计规范 | ✅ 定稿 |
| [PRD1-UI-REDESIGN-SPEC.md](PRD1-UI-REDESIGN-SPEC.md) | PRD1 UI 重设计执行规范与验收清单 | ⏳ 本地已实现，待验收 |
| [pages/dashboard-v2.md](pages/dashboard-v2.md) | Dashboard 线框图 | ✅ 已实现 |
| [pages/logs-explorer-v2.md](pages/logs-explorer-v2.md) | Logs Explorer 规范 | ✅ 已实现 |

## 设计决策摘要

### 范围界定
- **改什么**: 现有 14 个页面的布局、导航、信息层级
- **不改什么**: 不新增功能，不改后端 API，不改数据模型
- **组件库**: 继续使用 Semi Design（已有投入，支持暗色模式）

### 核心改动
1. **导航重组**: 14 个平铺菜单 → 5 个分组（Overview / Traffic / Billing / Access / System）
2. **Dashboard 重设计**: 从"配置展示"转向"活动监控"
3. **Logs Explorer 升级**: 从"日志表格"转向"调查工作区"
4. **页面模板统一**: 三种模板覆盖所有页面（资源管理 / 探索器 / 分析）
5. **工程化落地**: `frontend/src/ui/` 提供 PageHeader、Surface、MetricTile、MoneyText、LatencyText、StatusTag、EmptyState，避免设计只停留在文档层

### 暂缓事项
- **Dark Mode**: Semi Design 原生支持，但当前优先级不高，后续迭代
- **Theme Engine**: 不做多主题系统，保持单一品牌主题
- **Motion System**: 不做复杂动画，保持基本过渡效果

## 设计原则（30 秒版）

1. **Show activity, not configuration** — 仪表盘展示业务活动，不是数据库记录
2. **High information density** — 管理工具不是营销页面，信息密度优先
3. **Progressive complexity** — 高频操作零摩擦，低频操作不干扰
4. **No decoration** — 不加毛玻璃、不加装饰性动画、不加抽象图标

## 目标用户

| 阶段 | 用户 | 特征 |
|------|------|------|
| 当前 | 个人开发者 / 小团队 | 技术能力强，关注成本和调试效率 |
| 未来 | 企业 AI 平台团队 | 关注治理、合规、多部门协作 |

设计以当前用户为主，架构为未来用户留空间。

## 参考资料

- [GPT 设计咨询原始文档](../consultations/ui-redesign-gpt/) — 4 份 PDF + Markdown 转换版
- [GPT 设计反馈](../consultations/ui-redesign-gpt/FEEDBACK.md) — 对 GPT 方案的评价和采纳情况
- [PRD-Phase1.md](../PRD-Phase1.md) — 产品需求文档（source of truth）
