# 在 Linux 服务器上安装

明确无到期限制的安装包使用签名格式 `unirouter.upgrade-manifest.v2` 和 `expires_at: "never"`（UR 1.14.0 起支持）。独立校验程序和后端都必须支持 v2，后端的 `upgrade record-applied` 也不例外。旧 v1 包仍遵守原到期时间；更换包不能恢复绑定原包哈希的安装计划。检查、安装、恢复仍验证原包的签名、哈希、创建时间、平台、数据库版本和发布序号。无到期限制不等于允许回滚、跳过信任检查或延长所有者的签名授权。本次源码修改不代表新版包已发布或已部署。

[English](install-linux.md) · [配置模型与第一次请求（英文）](getting-started.md)

**预览版：安装器尚未正式发布，目前没有公开下载包。** 真实 Linux／Docker 测试结果
及其适用范围见[安装验收记录（英文）](../ops/COMMUNITY-INSTALLER.md#accepted-native-bootstrap-result--2026-10-11)。
以下命令适用于已经准备并验证的发布目录。只克隆仓库还不会获得
签名安装包、可信校验程序或已发布的镜像。

安装器会准备 PostgreSQL、Redis、UR 和监控服务，生成必要的本地密码与配置，
初始化数据库，并启动私有管理页面。使用者无需安装 Go、Node.js 或 npm 来编译 UR。
安装后在页面中配置组织和模型即可继续接入流程。X-Insight 是可选产品，不是安装前提。

## 安装前准备

在线下载入口的第一版候选支持 Linux amd64；原有直接使用已验证发布目录的源码路径
仍保留 amd64／arm64 支持，公开 arm64 制品还需要单独实机验收。请使用新的安装目录和 Docker Compose 项目。
服务器需要 Python 3.10 以上、Bash、本机以 root 权限运行的 Docker Engine 和
Compose v2；Compose 须支持 `up --wait` 与 `config --environment`。
Docker 的准备方法见[官方 Linux 安装说明](https://docs.docker.com/compose/install/linux/)。

还需要 sudo 权限、空闲的本机端口（默认 3000）、能够访问发布包指定镜像仓库的网络，
以及足够保存发布包、六个镜像和数据库的磁盘。安装器的磁盘检查只覆盖安装文件，
不估算镜像体积或后续业务数据增长。

发布方还需提供准备好的发布目录、通过独立可信渠道确认的 `uniroute-upgrade`
校验程序和公钥文件。第一版不自动安装 Docker、不修改防火墙，会拒绝占用已有项目，
并检查 Docker 子网冲突。管理页面只监听 `127.0.0.1`，数据库不开放宿主机端口。

## 下载并准备正式发布包

1.11.0 源码候选新增了[下载准备入口（英文）](../../deploy/community/README.md)。
正式发布时，需要先从已审核、固定提交的官方说明获取入口脚本、发布 JSON 和独立核对的摘要。
脚本随后下载并校验指定校验程序、公钥及安装包，再准备文件、调用包内安装器。
目前还没有公开的正式下载地址和摘要。

使用 `install` 可以连同下载准备一起安装，也可以先 `prepare`、再 `check`、再 `install`。
全程使用同一个 root 身份和 root 控制的准备目录。保留原目录、发布 JSON 和摘要，
中断后用 `resume`；检查及恢复不会重新下载。单独 `prepare` 不启动服务、不创建安装密码。
[入口说明](../../deploy/community/README.md#actions)解释了发布文件目录与运行数据目录的区别。

## 直接使用已准备发布目录时的检查与安装

将下列路径替换为对应发布提供的真实路径。发布目录中须有 `release.tar.gz` 和已准备的
`payload/` 文件。校验程序须支持本版新增的 `manifest_sha256` 结果；旧版会在写入前被拒绝。

```sh
cd /path/to/verified-release
sudo python3 payload/deploy/install.py check \
  --verifier /path/to/trusted/uniroute-upgrade \
  --public-keys /path/to/trusted/public-keys.json

sudo python3 payload/deploy/install.py install \
  --verifier /path/to/trusted/uniroute-upgrade \
  --public-keys /path/to/trusted/public-keys.json
```

`check` 只检查，不创建配置、密码、容器或数据库。`install` 默认创建私有目录
`/opt/unirouter`，拉取签名清单指定的六个镜像，启动数据库，初始化 UR，登记准确版本，
设置随机管理员密码，然后启动管理页面。进度输出和命令参数不会显示密码。

可用 `--directory`、`--name`、`--port` 指定其他目录、项目名和本机端口，恢复时须保留
这些参数。这是新安装命令，不用于升级现有实例。
同一个项目一次只运行一个安装程序，即使使用不同目录也不要并行安装。

## 打开管理页面

安装成功后会显示访问地址和密码文件的位置。在你自己的电脑上建立 SSH 隧道，并保持连接：

```sh
ssh -N -L 3000:127.0.0.1:3000 your-user@your-server
```

将用户名和服务器地址替换为你的实际信息，然后在电脑浏览器中打开
`http://127.0.0.1:3000`。在服务器上私下执行 `sudo cat /opt/unirouter/admin-password`
读取随机密码，使用 `admin` 登录；不要把密码粘贴到 Issue 或共享日志中。
按页面完成首次客户设置，再按[模型配置指南](getting-started.md#prepare-model-access)继续。

安装器不会自动登录、导入模型密钥、执行资格探测或发起付费模型请求。管理页面可用，
还不代表模型已经可以向应用提供服务。完成配置和资格验证前保留本机访问限制；公开 HTTPS
与最终路由切换按[部署说明](../ops/deployment-runbook.md)执行。

## 安装中断怎么办

保留安装目录、数据库卷和原发布包，将原命令中的 `install` 改为 `resume`，使用相同路径、
项目名、端口及 Docker 实例。恢复流程保留密码，沿用数据库中的原始安装计划，并先核对
版本登记结果，避免重复登记。

如果管理员密码操作的结果不确定，程序会停下，交给维护者核对。直接重试可能重复写入审计，
破坏已有首次设置要求。不要通过删审计、重新生成密码或重置数据库来绕过错误。
如果最初文件准备失败、还未生成 `install-state.json`，也需要先检查，不能当作可直接恢复的
数据库安装。

求助时提供产品版本、中断步骤和脱敏错误即可。不要上传 `.env`、`admin-password`、
数据库备份或未经脱敏的内部命令输出。第一版不自动卸载或清理数据。

维护者可查看[安装器合同与验收状态（英文）](../ops/COMMUNITY-INSTALLER.md)。
