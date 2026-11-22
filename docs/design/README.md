# UniRouter UI 设计导航

| 要查什么 | 入口 |
| --- | --- |
| 现行页面、路由和代码位置 | [前端代码地图](../CODEBASE-MAP.md#frontend-map) |
| Semi Design 共用组件、分页、错误和加载交互 | [组件入口](../../frontend/src/ui/index.ts) · [前端交互规范](../governance/README.md#frontend-interaction-conventions) |
| 设计原则和早期页面模板 | [设计宣言](manifesto.md) · [设计规范 v1](spec-v1.md) |
| 仪表盘和执行决策凭证的设计 | [Dashboard v2](pages/dashboard-v2.md) · [Logs Explorer](pages/logs-explorer-v2.md)（兼容路径） |
| 2026 年 7 月 PRD1 界面改版的范围和验证记录 | [PRD1 执行记录](PRD1-UI-REDESIGN-SPEC.md) · [PRD1 产品合同](../PRD-Phase1.md) |
| 原始咨询和采纳理由 | [设计咨询](../consultations/ui-redesign-gpt/) · [设计反馈](../consultations/ui-redesign-gpt/FEEDBACK.md) |

布局优先展示业务活动，保持信息密度，把低频细节放在展开层，并控制装饰。
组件选型和交互变更依照上表的代码与规范入口。
