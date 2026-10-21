# UniRouter

**企业 AI 的模型执行与可信事实底座。**

[Foigent](https://github.com/foigent) · [English](README.md) · [设计理念](docs/community/why-unirouter.zh-CN.md) · [开始使用](docs/community/getting-started.md) ·
[架构与 X-Insight](docs/community/architecture.md) · [参与贡献](CONTRIBUTING.md) ·
[路线图](ROADMAP.md) · [许可证](LICENSE)

UniRouter（UR）为应用和 Agent 提供统一模型入口及管理控制台，将模型访问策略、
路由、预算、用量、计费和执行回执放在同一条可检查的链路中。你可以追溯请求
如何执行，以及费用依据如何形成。

> **源码已公开，Linux 安装器为预览版：** 项目源码采用 Apache-2.0。
> [1.14.0 Linux amd64 安装说明](docs/community/install-linux.zh-CN.md)提供固定签名包与下载身份。
> 签名、公开下载和真实安装的分别验证结果见[发布准备状态](docs/community/release-readiness.md)。
> 历史合成测试不能代替当前公开安装或现有服务器升级验收。

项目以英文作为对外文档与项目更新的第一语言，简体中文作为第二语言。
中英文提问和贡献均欢迎；文档存在差异时，以英文版本为准，并欢迎提交翻译修正。
详见[语言约定](CONTRIBUTING.md#language)。

## 可以用它做什么

- 统一接入兼容的模型端点，管理 Provider 连接、凭据、执行 Profile 和经过资格验证的 Release。
- 通过组织、项目、角色、模型策略、预算、速率限制与撤销机制控制模型使用。
- 检查路由、失败、用量来源、计费状态，以及单次模型执行的回执。
- 区分供应商返回的用量、估算、客户收费和供应商成本证据。
- 将经过授权的执行事实异步交给证据分析系统，包括 X-Insight。

具体端点和参数能力取决于 Provider、执行 Profile、资格证据和活动 Release。
兼容地址不代表所有模型支持相同操作，详见 [API 合同](docs/tech-ref/05-openapi-spec.yaml)。

## 从哪里开始

| 你的目标 | 阅读入口 |
| --- | --- |
| 理解为什么做 UR 及设计取舍 | [设计理念](docs/community/why-unirouter.zh-CN.md) |
| 深入了解网关实现差异 | [源码比较说明（英文）](docs/community/gateway-design-comparison.md) |
| 理解产品与职责边界 | [架构与 X-Insight](docs/community/architecture.md) |
| 安装并发出第一次请求 | [开始使用](docs/community/getting-started.md) |
| 了解 Linux／ECS 简易安装预览版 | [Linux 安装说明](docs/community/install-linux.zh-CN.md) |
| 将执行事实与 X-Insight 组合使用 | [接入入口与当前限制（英文）](docs/community/getting-started.md#optional-x-insight-integration) |
| 了解数据保存和外发 | [数据处理说明](docs/community/data-handling.md) |
| 构建、测试或贡献 | [贡献指南](CONTRIBUTING.md)、[贡献地图](docs/community/contribution-map.md) |
| 深入源码和历史验证 | [文档索引](docs/README.zh-CN.md) |

首次使用围绕“配置模型 → 设置访问策略与预算 → 请求 → 核对用量与回执”展开。
真实请求与模型资格 Probe 使用你选定的供应商及凭据，可能产生供应商费用。
源码开发与签名发布包安装分别使用对应流程。

## 与 X-Insight 的关系

UniRouter 负责模型执行、路由、策略、用量与经济事实，包括自身客户账务，可以独立用于模型接入与
可追溯的执行。X-Insight 是独立的证据与调查产品，关联不同来源的事实，支持分析和
审阅。需要把 UniRouter 执行与其他获授权来源一起调查时，可以组合使用两者；
X-Insight 异步接收事实，不进入模型调用的同步关键路径。

[Foigent 首页](https://github.com/foigent) 是产品选择入口。X-Insight 源码正在准备公开，
此页只链接可公开访问的资料。已有合同、验证过的组合及公开上手路径的剩余工作见
[接入入口（英文）](docs/community/getting-started.md#optional-x-insight-integration)。
共享协议与 Integration Control 为产品及验证提供基础组件，不是需要用户另行选择或
自行拼装的其他产品。

“可信”指已记录的执行与费用依据可检查、可追溯。模型回答是否正确、业务任务
是否完成，仍需来自相应来源的证据。

## 参与

欢迎文档、可复现问题、测试、模型适配和易用性改进。先看
[贡献地图](docs/community/contribution-map.md)，较大的设计变更先通过
[Issue](https://github.com/foigent/unirouter/issues) 讨论，再按
[贡献指南](CONTRIBUTING.md) 提交。贡献采用 Apache-2.0 与 DCO 来源声明。

问题交流、想法与集成展示可前往 [Discussions](https://github.com/foigent/unirouter/discussions)。
[首次贡献任务](docs/community/starter-issues.md)列出了具体入口与验收要求。
敏感问题请按[安全报告说明](SECURITY.md)处理，社区协作遵循[行为准则](CODE_OF_CONDUCT.md)。

## 开源许可与产品范围

UniRouter 是采用 [Apache-2.0](LICENSE) 的完整开源产品。已接受的方向是让
UniRouter 与 X-Insight 两个完整产品都采用 Apache-2.0 开源，不划分社区版和商业版。
Foigent 目前没有商业产品规划。各项目的源码公开与制品发布状态分别记录。

UniRouter 已接受的源码已退役运行时商业激活要求，并通过临时环境中的全新安装和
维护升级测试。产品鉴权、租户隔离、预算、财务与审计完整性、发布校验及数据保护
继续保留。现有部署仍需执行各自受支持的升级；正式签名下载包尚未发布。
具体证据见[发布准备状态](docs/community/release-readiness.md)。

第三方组件保留原许可证及声明，Foigent 署名继续保留。贡献许可和第三方可以进行
商业使用的条件见[开源许可与产品范围（英文）](docs/community/commercial-boundaries.md)。
