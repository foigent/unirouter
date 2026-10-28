# 在 Linux 服务器上安装

**已有 M127 服务器需要升级？** 使用[保留数据的升级手册](../ops/COMMUNITY-UPGRADE.md)，不要把新安装命令用于现有目录。

明确无到期限制的安装包使用签名格式 `unirouter.upgrade-manifest.v2` 和 `expires_at: "never"`（UR 1.14.0 起支持）。独立校验程序和后端都必须支持 v2，后端的 `upgrade record-applied` 也不例外。旧 v1 包仍遵守原到期时间；更换包不能恢复绑定原包哈希的安装计划。检查、安装、恢复仍验证原包的签名、哈希、创建时间、平台、数据库版本和发布序号。无到期限制不等于允许回滚、跳过信任检查或延长所有者的签名授权。这一策略本身不代表公开下载可用或已完成部署。

[English](install-linux.md) · [配置模型与第一次请求（英文）](getting-started.md)

**Linux amd64 预览版：v1.15.0-preview.17，源码
`4eb32b5514d47aa91610fc66ef561368d6a18900`，M128，发布序号17。**
实际签名和全部26项材料已核验。签名声明前态序号0至16、最低schema为M127；现有M127实例使用升级手册。
发布附件和两个UR镜像已公开。[正式Linux验收](https://github.com/foigent/unirouter/actions/runs/40739178872)已通过完整公开包下载与验签、六镜像匿名拉取、M128安装、登录、首次客户初始化及中断恢复。
[源码PR482](https://github.com/foigent/unirouter/pull/482)另已通过候选Linux安装和M127→M128维护升级测试，使用合成数据和临时信任。
这些结果不代表某台现有服务器已升级或已完成真实模型／UR→XI验收；升级前仍须运行原机检查。见[发布状态](release-readiness.md)。
本入口用于新实例。保留旧尝试，使用未占用的路径与项目；恢复不能更换绑定的包摘要。
历史版本与失败候选保留，序号14不可安装或复用。只克隆仓库不会获得签名附件。

安装器会准备 PostgreSQL、Redis、UR 和监控服务，生成必要的本地密码与配置，
初始化数据库，并启动私有管理页面。使用者无需安装 Go、Node.js 或 npm 来编译 UR。
安装后在页面中配置组织和模型即可继续接入流程。X-Insight 是可选产品，不是安装前提。

## 安装前准备

本预览版在线入口支持 Linux amd64；原有直接使用已验证发布目录的源码路径
仍保留 amd64／arm64 支持，公开 arm64 制品还需要单独实机验收。请使用新的安装目录和 Docker Compose 项目。
服务器需要 Python 3.10 以上、Bash、`curl`、`sha256sum`、本机以 root 权限运行的 Docker Engine 和
Compose v2；Compose 须支持 `up --wait` 与 `config --environment`。
Docker 的准备方法见[官方 Linux 安装说明](https://docs.docker.com/compose/install/linux/)。

还需要 sudo 权限、空闲的本机端口（默认 3000）、能够访问发布包指定镜像仓库的网络，
以及足够保存发布包、六个镜像和数据库的磁盘。安装器的磁盘检查只覆盖安装文件，
不估算镜像体积或后续业务数据增长。

发布方还需提供准备好的发布目录、通过独立可信渠道确认的 `uniroute-upgrade`
校验程序和公钥文件。第一版不自动安装 Docker、不修改防火墙，会拒绝占用已有项目，
并检查 Docker 子网冲突。管理页面只监听 `127.0.0.1`，数据库不开放宿主机端口。

## 下载并安装本预览版

先通过已认证的官方仓库获取本说明，阅读
[预览版发布页面](https://github.com/foigent/unirouter/releases/tag/v1.15.0-preview.17)，再运行下载的代码。
脚本固定到产品源码提交，JSON 固定校验程序、公钥和签名包。与下载放在一起的摘要不能
单独建立信任；本入口不更换已有公钥，也不需要把签名私钥放到服务器。

| 下载文件 | 字节 | SHA-256 |
| --- | ---: | --- |
| 源码 4eb32b55 中的 `bootstrap.py` | 25,076 | `1697f604f91bd3cb2f0dc4f6663dcc45ddd6608206432d3b6dc65fb10f3b00a6` |
| `community-bootstrap.json` | 1,120 | `44b289bfc006ca785964174f19e937a5eedd53a7cb9c5539ad4459dbe155f454` |
| `uniroute-upgrade-linux-amd64` | 4,117,964 | `d767e64f2db31f97c886c53e14fe30b948794933aef28137db982442a2a0b02f` |
| `public-keys.json` | 83 | `96c91635460ddfb2a14aa1ea97d8445951730e9eb04a33cbdf4320141e61d63d` |
| `unirouter-1.15.0-linux-amd64.tar.gz` | 130,045,928 | `87f9fbd62b95a8293613f4fe726a2eca0ce723f31d4349a94b3e04a0844e23bf` |

先用 `sudo -i` 进入 root shell，再执行下方代码块。
下载目录、准备目录和运行目录必须尚未使用；保留它们用于恢复。下载或摘要校验失败时，
必须在安装器执行前停止。

```sh
set -eu
umask 077
mkdir -m 700 /opt/unirouter-downloads-1.15.0
cd /opt/unirouter-downloads-1.15.0
curl --fail --location --proto '=https' --proto-redir '=https' \
  --output bootstrap.py \
  https://raw.githubusercontent.com/foigent/unirouter/4eb32b5514d47aa91610fc66ef561368d6a18900/deploy/community/bootstrap.py
curl --fail --location --proto '=https' --proto-redir '=https' \
  --output community-bootstrap.json \
  https://github.com/foigent/unirouter/releases/download/v1.15.0-preview.17/community-bootstrap.json
printf '%s  %s\n' \
  1697f604f91bd3cb2f0dc4f6663dcc45ddd6608206432d3b6dc65fb10f3b00a6 bootstrap.py \
  44b289bfc006ca785964174f19e937a5eedd53a7cb9c5539ad4459dbe155f454 community-bootstrap.json \
  | sha256sum --check --status
python3 -I -B bootstrap.py install \
  --bootstrap community-bootstrap.json \
  --bootstrap-sha256 44b289bfc006ca785964174f19e937a5eedd53a7cb9c5539ad4459dbe155f454 \
  --directory /opt/unirouter-release
```

入口会验证并准备 `/opt/unirouter-release`，然后默认安装到 `/opt/unirouter`，
Compose 项目为 `unirouter`，管理端口为本机3000。如希望先检查，可在相同参数下依次运行
`prepare`、`check`、`install`。准备阶段不创建安装密码或启动服务；准备后的 `check`
不修改资源、不重新下载。全程使用同一个 root 身份和 root 控制的路径。

中断后不要重新运行下载代码块。保留原 JSON、摘要、发布目录、数据库卷与运行目录，
在相同 root shell 和下载目录中，把最后一条命令的 `install` 改成 `resume`，
并保留任何自定义路径、项目名和端口。恢复沿用原数据库计划，不重新下载或生成密码。
详见[安装中断怎么办](#安装中断怎么办)。

发布附件还包含
[`unirouter-retained-images-source-notices.tar.gz`](https://github.com/foigent/unirouter/releases/download/v1.15.0-preview.17/unirouter-retained-images-source-notices.tar.gz)
及 `SHA256SUMS`。该未改动的历史说明包保留1.12／1.13镜像与共享材料的对应关系；
1.14.0材料保留在[Preview 13](https://github.com/foigent/unirouter/releases/tag/v1.14.0-preview.13)。
本次签名包包含1.15.0的源码处置说明、组件声明和所需源码材料。第三方许可仍分别适用。

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

## 自选管理员密码

安装器会生成初始随机密码，目前不会在安装时询问你希望使用的密码。
安装成功后，可以在服务器上通过产品已有的原生命令自选密码。默认安装对应：

```sh
sudo docker compose --project-name unirouter \
  --project-directory /opt/unirouter/runtime \
  --env-file /opt/unirouter/runtime/.env \
  -f /opt/unirouter/runtime/docker-compose.yml \
  -f /opt/unirouter/runtime/installation.json \
  -f /opt/unirouter/runtime/runtime-image.json \
  exec backend ./uniroute admin set-password --username admin --org-id 1
```

如自定义了项目名或安装目录，请替换对应参数。在交互终端执行后输入两次新密码，
输入过程不回显；不要将密码写入命令参数或共享日志。成功修改会使原管理员会话失效。
原 `admin-password` 文件仍是生成的旧值，之后应使用你刚选择的密码，并妥善保存在密码管理器中。

## 打开管理页面

安装成功后会显示访问地址和密码文件的位置。在你自己的电脑上建立 SSH 隧道，并保持连接：

```sh
ssh -N -L 3000:127.0.0.1:3000 your-user@your-server
```

将用户名和服务器地址替换为你的实际信息，然后在电脑浏览器中打开
`http://127.0.0.1:3000`，使用 `admin` 和你刚自选的密码登录。
如保留了初始随机密码，可在服务器上私下执行 `sudo cat /opt/unirouter/admin-password` 读取。
不要把密码或密码文件粘贴到 Issue 或共享日志中。
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
