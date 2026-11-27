# 开始使用 UniRouter

[Foigent](https://github.com/foigent) · [项目首页](../../README.zh-CN.md) ·
[English](getting-started.md)

本指南以 [UniRouter 1.16.0 Preview 26](https://github.com/foigent/unirouter/releases/tag/v1.16.0-preview.26) 为例。
先按 [Linux amd64 安装与验签指南](install-linux.zh-CN.md)在服务器或 ECS 上安装，使用该预览版
对应的下载、校验值和可信公钥环。[发布状态](release-readiness.md)分别记录各版本的签名下载、
全新安装和原地升级。

## 安装、登录和初始化

1. 按[安装指南](install-linux.zh-CN.md)使用新目录和新的 Compose 项目。保留安装器给出的路径及项目名。
2. 安装器会生成初始管理员密码。如果希望首次登录前设置自己的密码，使用
   [原生密码命令](install-linux.zh-CN.md#自选管理员密码)。密码和浏览器会话仅在本地使用。
3. 按安装指南建立 SSH 隧道，用 `admin` 登录。界面默认英文，可从语言菜单切换到**简体中文**。
   从**系统与集成 → 部署与网络**完成**首次客户初始化**。
   只读预检会列出将创建的对象，确认后才创建客户组织。客户组织与平台管理组织独立。

安装不会导入供应商凭据、执行模型资格探测或发出付费请求。当前版本无需导入商业 License。
开启正文采集或外发前先读[数据处理说明](data-handling.md)。已有安装应保留数据，使用对应版本的升级和恢复指南。

## 准备模型调用权限

1. 管理员先选定客户组织和目标项目。在**模型接入 → 模型与端点**配置供应商连接与凭据、
   模型部署和执行 Profile。完成所需探测，核对证据并激活对应的模型 Release。
   **模型接入 → 调用条件诊断**可以分别检查路由是否就绪和调用方是否有权限。
2. 从**访问与安全 → 人员与权限**把需要的成员加入该项目。仅给调用身份所选 Chat 模型的使用权限，
   配置本次预算和限速。项目只读成员可以查项目请求，无法创建或使用项目调用凭据。
3. 以已授权的项目开发者登录，在**项目工作台**选中该项目，确认已启用的模型别名。
   需要新 Key 时，使用**项目访问凭证 → 创建项目访问凭证**，在支持的情况下把 Key 限定到目标模型。
   若组织要求先取得访问授权，先走正常授权流程。完整 Key 只显示一次，请私下保存。
   验收结束后，停用仅为本次演练创建的 Key。

资格探测和真实调用都可能产生供应商费用。请使用你选定的供应商、凭据和预算。
成本观测模式下，项目预算不能限制供应商的单笔扣费。

## 发送并查验首次请求

已安装签名 Preview 26 的用户可以在客户端新建私有目录，单独下载校验示例。下载地址和 SHA-256 应
与 [Preview 26 发布页的 Make your first request](https://github.com/foigent/unirouter/releases/tag/v1.16.0-preview.26#make-your-first-request)
一致，脚本固定到该版的产品源码。需要 Python 3.10 或更高版本、Bash 和 `sha256sum`。
若已克隆仓库，可直接使用 [`examples/first_request.py`](../../examples/first_request.py)，
将下面命令中的 `first_request.py` 改为仓库内对应路径。

```bash
(
  set -eu
  umask 077
  curl --fail --location --proto '=https' --proto-redir '=https' -o first_request.py \
    https://raw.githubusercontent.com/foigent/unirouter/3e84663f54bd8e57e4637d61bfcdb946a77bc11f/examples/first_request.py
  printf '%s\n' '1bc580acf1038c3a209e7c2398bf1bd5fdcc17468e261c4163529d9b3a00437a  first_request.py' | sha256sum -c -
)
```

下载和校验均通过后再继续。选择支持非流式 Chat 和 `max_tokens` 的已验模型。使用网关的 HTTPS 地址，
或保持安装指南中的 SSH 隧道开启，使用下面的本地地址。

```bash
export UR_BASE_URL='http://127.0.0.1:3000'
export UR_MODEL='your-qualified-model-alias'
read -rsp 'UniRouter application key: ' UR_API_KEY
printf '\n'
export UR_API_KEY
python3 -I -B first_request.py
```

默认只预检，不向模型供应商派发。示例需要同一安装明确回显支持单次上游尝试，并且当前请求通过本地准入。
任一条件不满足都会在发送 Chat 前停止，包括尚不支持该选项的旧版本。所选模型明确支持
`thinking.type=disabled` 且其 Profile 需要关闭思考时，在预检和发送两条命令中都加 `--disable-thinking`。

如要同时验证 XI，先核对[连接和授权范围](#可选接入-x-insight)，再发送。
预检通过且接受可能产生的费用后，只运行一次下面的发送命令。它以同一请求体再次预检，随后最多发送一次
Chat，请求最多 32 个输出 Token，并限制为一次上游尝试。示例没有启用 UniRouter 响应缓存。
只做预检时也请在结束后执行 `unset UR_API_KEY`。

```bash
python3 -I -B first_request.py --send
unset UR_API_KEY
```

### 查原请求和结算

- **项目成员**在**项目工作台**选中同一项目，将完整的 **Chat request ID** 粘贴到**请求定位**，
  点击**查询**。即使输入框提示 `req-…`，也直接粘贴原样返回的 `op_…` ID。
  查询结果显示模型、状态、Token 数和已记录扣费。项目只读成员查找请求无需调用凭据。
- **管理员**在客户组织中打开**监控与用量 → 执行请求**，在**高级筛选**输入精确的
  **操作 ID（Request ID）**，将时间范围设为覆盖本次调用，并确认结果属于目标项目。此管理列表按组织取数。打开执行决策凭证，
  查看路由尝试，并在**成本与终态**里核对计量来源、结算状态及账本记录的客户实扣。
- **结果不明时**，若服务端未返回 Chat ID，将单独打印的 **Chat client request ID** 和时间交给
  获授权的管理员，在同一筛选区用**客户端请求 ID（精确匹配）**查询。**Preflight request ID**
  只对应预检。遇到超时或响应不完整，先查这笔原请求。暂未查到记录时，结果仍未知。

供应商回传的 Token 和网关估算来源不同，观测或计算成本也可能不同于客户实扣。
成本观测模式下客户扣费可为零，供应商仍可能收费。HTTP 成功后，仍需在回执中确认结算终态。

## 可选接入 X-Insight

UniRouter 可以独立运行。需要审阅执行、用量及经济事实时，可搭配
[X-Insight FPV27](https://github.com/foigent/x-insight/releases/tag/v0.2.0-dev.fpv27)。
XI 异步接收获授权事实，UR 模型请求可独立执行。

按 [XI 两端管理员连接与首条事实指南](https://github.com/foigent/x-insight/blob/main/docs/community/getting-started.md#connect-unirouter-and-verify-the-pair)
在两端确认同一 UR 来源、项目和元数据范围。已有连接覆盖该范围时直接沿用。
[UR 1.15.6 与 XI FPV24 的历史验收](release-readiness.md#verified-existing-ur-and-xi-installation)
记录了那组安装的连接恢复和受限成员读取范围。

UR 请求完成后，两端获授权人员可私下使用登记的 UR 来源、组织与项目、**Chat request ID** 和带时区的请求时间联查。
按 [X-Insight 中文入门指南](https://github.com/foigent/x-insight/blob/main/docs/community/getting-started.zh-CN.md)
用相同来源、项目和时段找到同一 ID，再核对结果、
用量和费用。保留原金额及币种。若暂未查到，请管理员检查现有连接、同步状态，以及读者是否有精确的
来源／项目授权；使用原 UR 请求即可定位。元数据和正文权限分别授予。

- [UniRouter API 合同](../tech-ref/05-openapi-spec.yaml)记录组织内发起邀请、预览和同意的接口。
- [连接凭据生命周期](../ops/XINSIGHT-CONTROL-CREDENTIAL-LIFECYCLE.md)用于现有连接的轮换和恢复。
- [数据处理边界](data-handling.md)和 [`.env.example`](../../.env.example)列出 Feed、正文及调用权限的配置边界。
- [架构与已验证范围](architecture.md#integration-entry-and-verified-scope)保留协议、现场观察与待验项。

遇到问题时，先核对对应步骤、错误状态和[版本匹配的部署排查](../ops/deployment-runbook.md#七故障排查)。
在公开 [Issue](https://github.com/foigent/unirouter/issues) 提供产品版本、脱敏错误码和最小复现。
需要标识时使用本地合成或打码的值。公开报告中请勿粘贴生产标识、组织或项目身份、客户正文或凭据。
敏感问题走[安全报告](../../SECURITY.md)。

## 开发与恢复参考

源码构建和补丁请按[贡献指南](../../CONTRIBUTING.md)准备环境：

```sh
git clone https://github.com/foigent/unirouter.git
cd unirouter
```

[原生全新安装说明](../ops/FRESH-INSTALL.md)记录 plan/check/apply/resume。
M125 的历史安装使用专用的 [M126 维护路径](../ops/COMMERCIAL-LICENSE-RETIREMENT.md)，
当前版本全新安装无需执行那套历史步骤。[客户部署模板](../../deploy/customer/README.md)
提供更底层的验签与安装步骤。
