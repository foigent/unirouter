# 本地测试准备与交付复用

2026-09-11 的优化仅涉及内部测试和交接。已验收产品为 `59f7712d09653034756b846b15b8d69a16782791`，tree `ab09c48939539a10dbb039814489d764c0b53ca5`，版本 1.4.0。下文历史路径保留原始归属，不代表现在可用。

## 当前入口与复用判定（2026-09-15）

持久隔离工作树为 `/Users/m4/Desktop/test/ur-regression-20260914/source`，分支
`codex/fresh-regression-20260914`。已交付诊断候选为
`fe771d5c177f255523f290141acfc2809bb9cc31`，tree
`cffaa770e08fcf9494d62af7a2c5c2230a294846`，1.4.1；其父基线是
`a84ac2726e079796ade0af573b575b7c11bbdfbc`。当前已进入获批U1本地实现，存在未提交
产品和测试diff；不能把HEAD、旧归档和未提交工作树视为同一个已验收对象。

- 当前目标、授权和下一动作从相邻的 [DIAGNOSIS.md](/Users/m4/Desktop/test/ur-regression-20260914/DIAGNOSIS.md) 顶部恢复；其中 U1 实施计划与诊断交付分别记录，不用旧的“U1 未放行”快照推断当前状态。
- 诊断交付的精确归档、bundle 前驱和验证范围见 [diagnostic-delivery.json](/Users/m4/Desktop/test/ur-regression-20260914/diagnostic-delivery.json)。该交付是诊断及内部工具变更，原 F 两项根因仍未关闭。
- [工具优化验证](/Users/m4/Desktop/test/ur-regression-20260914/optimization-evidence/verification.json) 记录 14 项离线测试、选择器等价性及配置预检；诊断交付另记录 3 包、31 项测试/子测试通过。它们不证明 U1、完整 consolidated、性能或目标环境通过。
- U1当前轻量证据：[检查点摘要](/private/tmp/u1-validation-checkpoint-a1q850kp/summary.json)，
  五包74个顶层测试通过，兼容性纯测试另列；完整JSON日志同目录。初次缓存访问被沙箱拒绝，
  未运行测试；获准重跑日志独立保留。此处是WIP检查点，不是产品或PG验收。
  [旧55项摘要](/private/tmp/u1-local-check.xKqSRh/summary.json)保留原版本，不能与当前通过数相加。
- 上述检查点的持久入口为[delivery-index.json](/Users/m4/Desktop/test/ur-regression-20260914/u1-pg-checkpoint-c3fb9083/delivery-index.json)：原归档、manifest、summary与日志逐字节复制，原临时证据保留；索引映射所有持久路径。
- 后续C数据合同复用[原C0资产](../../contracts/source-content/v1/README.md)；
  `TestContentContractOriginalC0Vectors`直接验证25个正例和19个反例，避免另写相似夹具。
  Go canonical helper须先调用既有整数规范化；`json.Number`不能直接传入。
  此后续增量不属于上面已固定的PG检查点，证据单独保留在桌面test的`u1-read-preparation-20260915`。
- U1真实PG检查复用同一 `scripts/test-backend-integration.sh`：consolidated选择器新增
  `TestContentControl.*Integration`、`TestContentPolicy.*Integration`、`TestContentCaptureMigration.*`，
  source-media保留原两包并增加`internal/contentcapture`；runner离线测试断言三个选择器及精确包列表。
  不另写初始化器，不改变原Redis组顺序、超时或断言。新增测试仅编译通过不能写为PG验收。

下面的工具版本和依赖状态来自 9 月 14 日检查，未因本页更新日期改变而重新验证。
实际执行前只核对会影响该次运行的路径和配置；原生运行仍须原操作方核对目标、资源及授权。

| 对象 | 已核对的入口与状态 |
| --- | --- |
| 原生证据裁定 | [manager-decision.md](</Users/m4/Desktop/projects/About Me/library/reports/企业AI工作事实平台-目标拆解与验收-2026-09-05/supporting/xi-iteration-20260909/f-bounded/native-review-a84-f4/manager-decision.md>)：接受 a84 同镜像首装、M113 中断续装、新库恢复的明确范围，未接受完整产品 |
| 原 consolidated FAIL | [恢复的原始摘要](</Users/m4/Desktop/projects/About Me/library/reports/企业AI工作事实平台-目标拆解与验收-2026-09-05/supporting/xi-iteration-20260909/f-bounded/native-review-a84-f4/recovered-evidence/regression-consolidated.summary.json>)，SHA-256 `1b7ca4b6ffacf55f3686976e13542cdad283fd7885eef8777ff525e2a627a31c`：17 包通过、2 包失败；760 测试/子测试通过、3 失败、4 跳过。原始失败保留，两项根因及原合并范围仍待关闭 |
| M4 Go | `/Users/m4/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.25.12.darwin-arm64/bin/go`，实测 1.25.12；默认 Go 1.24.3 不能替代 |
| M4 PostgreSQL 工具 | `/opt/homebrew/opt/postgresql@16/bin`，实测 psql 16.14；默认 PostgreSQL 18 不能冒充 PG16。这里只核工具，不证明服务或数据库就绪 |
| M4 Python | `/Users/m4/.cache/codex-runtimes/codex-primary-runtime/dependencies/python/bin/python3`，实测 3.12.14；可运行标准库摘要。Homebrew Python 为 3.14.5 |
| HTTP JSON Schema 验证依赖 | 旧 `/private/tmp/ur-workfacts-test.RDgTyF/schema-validator` 已不存在；上述两个 Python 均未找到 jsonschema。HTTP 校验入口的这项依赖待恢复，不能引用旧通过状态；本次未安装依赖 |

下方 2026-09-11 的 tmp 工作树已失效；其他历史材料在使用前逐项核对。复用 a84 的原生
证据时检查受影响差异和环境，不把四个后续组单独通过覆盖 consolidated FAIL。
本轮仅优化内部工具及交接，不宣称性能、成功 M118 reset、真实 XI 或父平台验收完成。

## 2026-09-11 交付与依赖（历史）

后续 1.4.1 安装器源码工作见 [FRESH-INSTALL.md](../ops/FRESH-INSTALL.md)。其
`scripts/test-fresh-install.sh` 为新增覆盖，复用本页工具链预检和日志摘要；原五组
回归保持不变。下表的 59 产品验收与 6676 工具验证不能作为该安装器的执行证明。

以下为此 M4 的精确本地路径，不作为其他主机的默认配置。

| 对象 | 当前入口 |
| --- | --- |
| 独立最终验收 | [BD-independent-acceptance-59f7712d.json](</Users/m4/Desktop/projects/About Me/library/reports/企业AI工作事实平台-目标拆解与验收-2026-09-05/supporting/ur-iteration-20260909/BD-independent-acceptance-59f7712d.json>)，SHA-256 `4ac04dc697d00f27c99f6f36591a077655a5e4a709e65e23c8b8b7ecc3c287a4` |
| 原始完整索引 | `/private/tmp/ur-asus-a11-20260910/bd-delivery-evidence-59f7712d.json`；`BD-DELIVERY.md` 是提交验收时的检查点，其中 pending 已被最终验收取代 |
| 最终源码与修复证据 | `/private/tmp/ur-bd-workfacts-20260910/compatible-recovery-delivery-59f7712d.json` |
| 原始失败分类 | `/private/tmp/ur-asus-a11-20260910/bd-asus-batch-evidence-through-10.json` |
| M4 Python | `/Users/m4/.cache/codex-runtimes/codex-primary-runtime/dependencies/python/bin/python3`，已验收执行使用 3.12.14 |
| M4 JSON Schema 依赖 | `PYTHONPATH=/private/tmp/ur-workfacts-test.RDgTyF/schema-validator`，jsonschema 4.25.1 |
| 既有 ASUS 执行入口 | `/private/tmp/ur-asus-a11-20260910/run-n1-batch-bd-index38.py`；原 SSH/N1 资源与恢复检查继续适用，接口编号、容器和数据库前态须现场核对，不能直接重放旧批次 |

UR A/B/D/X Local 已通过独立整体验收。性能与附加观测缺口、M118 无受支持成功 reset、未发布/远端 CI/exact-main/真实 XI/父平台验收仍保留。B/BD 约 4.99/5.56 ms 增量来自两次运行各自 p95 的均值，不是合并样本 p95 或容量验收。

## 共享测试入口

本地与 CI 的 consolidated backend 步骤统一使用 `scripts/test-backend-integration.sh`。原包列表、正则和 integration/race/count/timeout 参数保留；顺序为 consolidated、source-media、agentcap、limitpolicy、workloadadmission，三个 Redis 组不并行。原 CI 其余全量 unit/race、admin、sourceevidence 等步骤继续保留。

按批次设置专用合成环境的 `TEST_DATABASE_URL`、`REDIS_ADDR`、`REDIS_DB` 和 `REQUIRE_TEST_DATABASE=1`。脚本先离线核对实际工具链与 go.mod，再检查 CGO/编译器；不能把自动选出的 Go 版本当成强制 `GOTOOLCHAIN=local` 后的版本。脚本不安装工具、启动服务或创建凭据。

```sh
# 从当前候选根目录运行；只核对配置和 race 编译工具，不连接数据库。
bash scripts/test-backend-integration.sh --check --log-dir /Users/m4/Desktop/test/ur-regression-20260914/next-run
# 确认专用环境、前态与授权后执行；日志目录必须尚不存在。
bash scripts/test-backend-integration.sh --log-dir /Users/m4/Desktop/test/ur-regression-20260914/next-run
```

日志模式完整保留 `go test -json`，只返回退出码、包数、测试/子测试数、失败行号与路径。首次失败停止，未到达的组仍是未执行；不自动重试。空选择、全部跳过、不完整执行不会被摘要判为通过。摘要保留观察到的 commit/tree、tracked diff 摘要、未跟踪路径、runner 摘要；存在差异/覆盖文件时不能只引用 HEAD 声称 exact-commit 通过，也不自动授予证据复用资格。

`--check --log-dir` 会先用所选 Python 执行摘要脚本 `--help`，验证实际语法和标准库，
不创建日志目录、不运行测试。fresh 入口复用同一检查。摘要要求显式 `--runner` 并核对
它与现有测试组的对应关系，记录实际入口路径、SHA-256 和摘要工具 SHA-256；不再把
fresh 归为 backend。历史摘要不回写；原 a84 首装摘要中的 backend runner hash 是已知
归属问题，需连同当时实际命令及源码裁定读取，不能用新字段追认旧证据。

## 昂贵执行前的具体准备

下表的 `P` 为 `/private/tmp/ur-asus-a11-20260910`。复用对应已通过材料，审查新候选差异；历史 ID 和前态不能用全局替换机械改写。

| 已发生问题 | 复用与预检 |
| --- | --- |
| APK 索引缺失 | 复用 `bd-linux-tests-resume-8e4af934.sh` 的准备段：在获批 Alpine 环境 `apk update` → `apk add --simulate gcc musl-dev` → 安装 → Go/CGO/编译器检查，再执行 race。新入口会在缺编译器时提前停止。该历史脚本后续初始化曾失败，不能整段作为成功入口重放。 |
| 初始化器版本不匹配 | 复用 `bd-install-versioned-8e4af934.sh` 的已通过安装段和 `$P/bd-fixtures-8e4af934/bd-init-db.go`。M116 初始化器从精确 A `25f5c1fdfb6b5376496c34e9b9cd295efbaf17cd` 编译并配同源 migrations；M118 用当前源码和同源 migrations。先核对 artifact ceiling、完整 ledger 和空数据库，不能用新初始化器配旧目录。该脚本后续 ready 检查曾失败，安装证据与就绪证据分别引用。 |
| 生命周期未完成就检查 ready | 依序复用同一 fixture 目录中的 `bd-route-fixture.go`、`bd_canonical_fixture_test.go`。先检查预期数据库/M116/无活动 generation/无既有部分写入，再由 `TestA11CanonicalStartupFixture` 完成原 authoring/lifecycle，最后检查 canonical ready；仅安装 schema 不等于就绪。 |
| 停止宽限改成 40 秒 | 原 Compose `35m` 对应实际 `StopTimeout=2100`。复用 `bd-compatible-repaired-current-batch-59f7712d.py` 的设置；readiness 轮询时限不是停止宽限。现有 rollout 回归检查 40 秒仍被拒绝。 |
| JSON 字节序列错误 | 含 `json.RawMessage` 的 manifest/摘要输入复用产品生成的规范原始字节，不经 pretty-print、解码重编码或 SQL text 再拼装。先通过原校验器，再归因产品；批次 07 格式错误与 08 真实反例分别保留。 |
| 再次漏掉 Docker 日志 | 原 `scripts/test-artifact-owner-docker.py` 入口已直接包含 A 验证过的 `local / max-size=10m / max-file=3`，不再临时复制后补丁。原产品配置核验器未放宽。 |

HTTP 消费者入口为 `contracts/source-facts/v2/check_http_pages.py`。先验证上面的 Python/依赖路径，再用入口的 `--help` 核对实际参数；路径失效先定位已有依赖，不自动另装环境。

## 证据与交接

继续使用现有 `EXECUTION-RECORD.md` 与交付索引。每条必要证据标明：**入口/覆盖行为、实际版本和环境、原始路径、当前运行/明确复用/待补、复用理由或受影响差异**。候选变化时更新，送审前核对所有必需入口；独立审查不代替执行者补找入口和计数。

| 既有证据组 | 版本、覆盖与复用边界 |
| --- | --- |
| 全量/合同/前端 | `bd-local-code-evidence-d60b833d.json`；后续局部修复之外的行为按原审查结论复用，不把 d60 升格为最终候选 |
| 空预算切片修复、三个独立 M118 Redis 选择器 | `bd-close-fix-8e4af934.json`；覆盖 8e 修复和 M118，59 的兼容 CLI 差异不影响该证据 |
| 最终兼容修复、镜像、保留数据库升级 | `compatible-recovery-delivery-59f7712d.json` 和最终 ASUS 索引；绑定 59，早期失败不能代替 |
| 本轮内部工具优化 | 单独保留 selector 等价性和离线回归，不冒充新产品 Docker/性能验收 |

前三项本地索引位于 `/private/tmp/ur-bd-workfacts-20260910`，最终 ASUS 索引路径见上表。包数取 package 终态，测试/子测试另计；不要相加多次运行或覆盖初始失败。交接从本入口提供精确候选、证据、命令、依赖路径及限制，历史 pending 保留原语义。

先用 `rg --files`/定义搜索定位再读片段；嵌入载荷解码落盘，大 JSON 按字段读取，完整日志保留磁盘。先保存退出码再摘要，避免 `tail` 掩盖失败。异步任务沿用现有通知/游标等待；无变化不反复读取交接或重复说明。必要测试和独立验收均保留。经验文件无需变化：本轮不形成新的模型/Provider 适配经验。
