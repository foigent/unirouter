# UniRouter Agent 执行与复核协议

**版本：** 2.1
**生效方式：** User 已确认单一执行负责人流程；当前仓库版本是规则权威

---

## 1. 目标

UniRouter 采用一个执行负责人完成计划、实现、验证、仓库集成和已授权环境操作，避免把一个批次拆成多次人工传话。

当前执行负责人是 **Codex**。Canary、Sentinel、Atlas 不再是批次继续推进的强制关卡；它们或其他人员/agent 可以按需承担独立复核。

本协议不取消以下边界：

1. 自动化测试和 GitHub 强制 CI 不能被角色判断替代。
2. 同一执行负责人完成的实现和测试不能标记为“独立复核”。
3. 生产、高影响数据、凭证和付费外部动作仍需要 User 明确授权。
4. `Local verified`、`CI verified`、`Target verified`、`Production verified`、`External integration verified` 必须分别记录，不能互相推导。

---

## 2. 责任模型

| 角色 | 责任 | 边界 |
|------|------|------|
| **User** | 确认 PRD/范围；批准生产和其他高影响外部动作；决定是否接受剩余风险 | 不需要在 agents 之间搬运常规上下文；不得用一句“都完成了”替代证据 |
| **Codex / 执行负责人** | 计划、开发、测试、文档、分支、提交、PR、CI、合并、已授权非生产部署、运行时验证、证据和状态维护 | 不绕过强制 CI；不把自测写成独立验收；无访问权限或批准时必须明确标记缺口 |
| **独立复核者** | 按执行记录复跑测试、代码审查、集成审查、目标环境检查或安全检查 | 默认不阻塞执行；不得覆盖执行记录；P0/P1 发现会阻断或重新打开批次 |

Canary、Sentinel、Atlas 名称保留为历史角色和可选复核画像：

- Canary 画像：功能、回归、产品语义和边界复核。
- Sentinel 画像：Git diff、PR、分支保护和 CI 复核。
- Atlas 画像：部署、运行时、目标环境、回滚和清理复核。

任何具备能力的复核者都可以执行这些检查，不再要求由特定名字的 agent 完成。

---

## 3. 风险分级

| 等级 | 范围 | 执行要求 |
|------|------|----------|
| **R0 本地整理** | 拼写、链接、索引、无事实状态变化的文档整理 | 聚焦核对和 `git diff --check`；无需单独批次证据 |
| **R1 工具/测试/证据辅助** | 测试、脚本、harness、tech-ref，且不改变生产路径、API/DB/权限/计费语义 | 聚焦测试；进入仓库时必须通过适用 CI |
| **R2 用户可见或事实源变化** | 前端/API 行为、产品语义、当前状态、验收矩阵、流程规则 | 更新执行记录和相关事实源；通过适用测试和 CI |
| **R3 高风险核心路径** | auth、SSO、billing/wallet/cost、routing/provider dispatch、DB migration、secret/RBAC、限流、trace retention/redaction | 独立切片；聚焦回归、全量回归、回滚/停止条件、CI 和明确证据 |
| **R4 目标环境/生产声明** | real upstream、real SSO、license、HA/DR、云端部署、Production verified | 记录环境、版本、命令、实际结果和回滚；生产动作必须单次授权 |

等级不确定时按更高等级处理。PRD alignment gate 每 2 到 3 个实现批次或进入新工作流前执行一次。

---

## 4. 已授权的默认动作

在已确认范围内，执行负责人可以直接执行：

1. 读取和修改 UniRouter 工作区文件。
2. 运行本地测试、构建、静态检查和浏览器验证。
3. 创建 `codex/` 分支、提交、推送和创建/更新 PR。
4. 在 GitHub 强制检查全部通过后合并 PR。
5. 在访问方式和环境边界已经确认后部署到批准的非生产环境。
6. 在共享文件夹写入不含秘密的 UniRouter/X-Insight 请求、合同、证据和回执文件。
7. 在 User 已确认跨项目范围时修改 UniRouter 和 X-Insight 各自仓库，但必须使用独立分支、提交、PR 和发布记录。

以下动作仍需 User 对具体动作明确授权：

1. 每次生产部署、生产回滚或生产流量切换。
2. 目标数据库破坏性操作、不可逆迁移、目标恢复或数据删除。
3. 创建、禁用、轮换或暴露凭证、密钥、令牌、DSN。
4. 扩大已批准的数据导出范围，或启用 raw prompt/response 等敏感数据模式。
5. 超出既定模型、请求次数或费用上限的真实供应商调用。
6. 公开发布、开源切换或外部可见权限变更。

无权限、无凭证或无批准不是“角色等待”，而是具体的外部阻塞条件，必须写入执行记录。

---

## 5. 标准执行流程

```text
User 确认范围和默认权限
  ↓
Codex 读取当前事实源、工作树和远端状态
  ↓
Codex 划分批次和风险等级
  ↓
Codex 实现 + 聚焦测试 + 必要全量测试
  ↓
Codex 更新 EXECUTION-RECORD.md 和不可变 evidence
  ↓
Codex 创建分支/提交/PR
  ↓
GitHub 强制 CI
  ├─ FAIL：Codex 修复并重新验证
  └─ PASS：Codex 合并
       ↓
  需要目标环境？
  ├─ 否：更新状态，继续下一批
  └─ 是：检查访问和批准
       ├─ 非生产且已授权：Codex 部署、验证、记录
       ├─ 生产/高影响：User 单次批准后由 Codex 执行
       └─ 缺访问/批准：记录具体阻塞，不伪造目标结论
```

独立复核可以在本地完成、PR 打开、合并后或部署后异步发生，不作为默认串行关卡。

---

## 6. 记录与证据

### 6.1 当前执行记录

`docs/agent-workflows/EXECUTION-RECORD.md` 是当前批次的可变执行记录，由执行负责人维护，至少包括：

1. 批次、范围、风险等级和状态。
2. 基线分支/commit、当前分支和预期 PR。
3. 修改文件和行为变化。
4. 非目标和边界。
5. 实际执行的命令、结果和未执行原因。
6. CI、合并、部署和外部集成状态。
7. 批准、停止条件、回滚和清理要求。
8. 已知缺口和下一步。
9. 可供复核者直接执行的检查清单。

### 6.2 不可变证据

批次达到重要事实节点时，在 `docs/ops/evidence/` 增加带批次和日期的证据文件。已有证据不覆盖、不回写新历史；后续修复通过新文件引用旧证据。

运行时或外部交换证据还应记录适用字段：

- 环境用途和非生产/生产分类；
- commit、镜像或部署版本；
- 时间、命令、健康检查和实际结果；
- request ID、exchange ID、artifact hash 或 receipt ID；
- 数据最小化、脱敏和保留期；
- 回滚/停止条件和清理状态。

### 6.3 状态词汇

| 状态 | 允许的事实含义 |
|------|----------------|
| `Planned` | 只有已确认范围，无实现证据 |
| `Implemented` | 代码已修改，尚未完成要求测试 |
| `Local verified` | 执行负责人完成记录中的本地验证 |
| `CI verified` | 对应提交通过仓库强制 CI |
| `Target verified` | 指定非生产/批准目标环境有实际证据 |
| `Production verified` | 指定生产或明确生产等价环境有实际证据 |
| `External integration verified` | 外部系统导入/回读/receipt 已完成并可核对 |
| `Blocked` | 缺少明确的权限、批准、环境、凭证或外部结果 |

可以同时保留多个层级，例如 `CI verified; target pending`。不得因为 CI 通过就写 `Target verified`，也不得因为某个 agent 声称完成就写 `Production verified`。

---

## 7. 独立复核流程

复核者首先读取：

1. `docs/agent-workflows/EXECUTION-RECORD.md`；
2. 对应 `docs/ops/evidence/` 文件；
3. PR diff、CI 或目标环境证据。

复核结果写入独立文件：

```text
docs/agent-workflows/reviews/<batch-id>-<reviewer>-<YYYYMMDD>.md
```

每个复核文件必须记录：复核者、独立性、范围、基线 commit、执行命令、实际结果、发现级别和结论。不得覆盖其他复核者文件。

发现处理：

| 级别 | 处理 |
|------|------|
| **P0/P1** | 立即阻断未合并/未部署动作；已发布问题建立新的纠错批次并保留原证据，不得静默重开已完成 PRD 范围 |
| **P2** | 执行负责人评估是否当前批修复；未修复必须进入 `OPEN-ISSUES.md` 或 backlog |
| **P3** | 可记录为非阻断建议，不影响当前事实状态 |

`docs/agent-workflows/VERIFY-RESULT.md` 保留为协议 v1.x 的最近历史验收结果，不再由多个复核者覆盖写入。

---

## 8. GitHub 与合并规则

1. 不直接推送 `main`。
2. 分支默认使用 `codex/<scope>`。
3. 提交前检查工作树，不能回退或覆盖不属于当前批次的改动。
4. PR 必须说明范围、风险、测试、证据、非目标和目标环境状态。
5. UniRouter 当前强制 CI 包括 `Change Routing` 和它判定适用的 `Backend Tests`、`Frontend Build`、`Docker Build`；适用检查通过后才可合并。纯文档 PR 只能由路由显式跳过昂贵任务。
6. 合并后以 `origin/main` 的实际 commit/PR 为准更新状态，不以本地分支 SHA 猜测。
7. 强制推送、删除远端保护分支或绕过检查不在默认授权内。

---

## 9. 部署、凭证与数据安全

1. 凭证只通过 SSH agent、钥匙串、环境变量或批准的秘密管理方式注入，不写入聊天、Git、共享 Markdown 或 evidence。
2. 非生产环境必须有明确用途标签；无法确认环境性质时停止部署。
3. 部署前记录当前版本、健康状态和回滚方式，部署后验证关键健康/API/UI 路径。
4. 数据导出遵循明确 request ID、批准 manifest、脱敏和最小化原则；不得把 Gate A 扩大为广义数据发现。
5. UniRouter 和 X-Insight 是独立项目。共享文件夹只承载沟通、合同、最小化数据交换和回执，不替代各自仓库、CI、数据库或发布治理。

---

## 10. 流程迁移

- 协议 v1.x 的 Canary/Sentinel/Atlas 完成记录继续作为历史证据，不重写。
- `FIX-SIGNAL.md` 保留为旧链接兼容入口，新的当前执行事实写入 `EXECUTION-RECORD.md`。
- `VERIFY-RESULT.md` 保留最近一次 v1.x 独立验收结果。
- 新复核统一写入 `docs/agent-workflows/reviews/`。
- 当前事实以 `CURRENT-STATUS.md`、相应 PRD acceptance matrix、`EXECUTION-RECORD.md` 和不可变 evidence 交叉核对。
