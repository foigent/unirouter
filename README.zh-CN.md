# UniRouter

**企业 AI 的模型执行与可信事实底座。**

[English](README.md) · [设计理念](docs/community/why-unirouter.zh-CN.md) · [开始使用](docs/community/getting-started.md) ·
[架构与 X-Insight](docs/community/architecture.md) · [参与贡献](CONTRIBUTING.md) ·
[路线图](ROADMAP.md) · [许可证](LICENSE)

UniRouter 为应用和 Agent 提供统一模型入口及管理控制台，将模型访问策略、
路由、预算、用量、计费和执行回执放在同一条可检查的链路中。你可以追溯请求
如何执行，以及费用依据如何形成。

> **源码已公开，安装器为预览版：** 项目源码采用 Apache-2.0。无商业 License 的
> Linux amd64 全新安装和维护升级已在 GitHub 临时环境中通过；正式签名下载包仍待发布。
> 验证证据、贡献者 CI 状态及剩余分发工作见[发布准备状态](docs/community/release-readiness.md)。

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
| 了解数据保存和外发 | [数据处理说明](docs/community/data-handling.md) |
| 构建、测试或贡献 | [贡献指南](CONTRIBUTING.md)、[贡献地图](docs/community/contribution-map.md) |
| 深入源码和历史验证 | [文档索引](docs/README.zh-CN.md) |

首次使用围绕“配置模型 → 设置访问策略与预算 → 请求 → 核对用量与回执”展开。
真实请求与模型资格 Probe 使用你选定的供应商及凭据，可能产生供应商费用。
源码开发与签名发布包安装分别使用对应流程。

## 与 X-Insight 的关系

UR 负责模型执行决定和自身客户账务。X-Insight 是独立的证据与调查产品，
负责关联不同来源的事实、支持比较和调查。两者通过经授权的异步接口协作；
X-Insight 不进入模型调用的同步关键路径。UR 可以独立使用，X-Insight 的
具体集成可用性按其发布状态单独说明。

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

## 开源与商业服务

UR 项目代码使用 [Apache-2.0](LICENSE)；第三方组件保留原许可证及声明。
版权署名沿用仓库已有的 Foigent 署名。

已接受的产品方向是移除核心产品的运行时商业激活要求，其实现和验证见
[发布准备状态](docs/community/release-readiness.md)。仅修改源码许可证，
不会改变已安装旧版本的运行时行为。

企业支持、私有化交付、托管服务及未来独立商业模块可以分别提供，详见
[商业边界](docs/community/commercial-boundaries.md)。源码发布本身不承诺
某项收费服务已经开放，也不附带 SLA。
