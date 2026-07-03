# VERIFY-RESULT.md — PRD2 Batch 26 验收结果

**验收者：** OpenClaw Canary
**日期：** 2026-07-04
**对应轮次：** PRD2 Batch 26 UI/UX Hardening

## 逐条结果

- Fix #1: PASS — sidebar hierarchy/scroll/collapse indicators/icons 全部就位。`overflow-y: auto` 在 `.semi-navigation` 上(line 114)，`semi-navigation-sub-title` 独立样式(line 131/137/145)，二级菜单缩进+层级线(line 149/158/169)，`semi-navigation-item-icon-toggle-right` 恢复可见(line 189)，7 个导航图标全部重新分配(IconMonitorStroked/IconSendStroked/IconRoute/IconLockStroked/IconKeyStroked/IconCreditCard)。
- Fix #2: PASS — 审计日志表格设置 `scroll={{ x: 1570, y: 620 }}`(line 118)，detail 字段 `whiteSpace: 'nowrap'`(line 84)，列名改为中文 `组织`/`项目`(line 70/71)。
- Fix #3: PASS — 新增 `parseChannelModels`/`firstChannelModel`/`resolveChannelTestModel` 函数，渠道测试优先使用用户选择模型再 fallback 到渠道配置第一个模型，无配置时提示 `渠道未配置测试模型`。`gpt-3.5-turbo` 硬编码已完全移除(grep 确认 0 匹配)。单元测试 3/3 通过。
- Fix #4: PASS — PRD2 UI 英文标签已中文化：`模型目录`/`企业别名`/`模型集`/`项目分配`/`供应商族`/`目录可见性`/`已批准`/`内部`/`供应商：` 等。对应测试断言已同步更新。
- Fix #5: PASS — `docs/ops/evidence/PRD2-B26-ui-hardening-local-20260704.md` 存在，`CURRENT-STATUS.md` 包含 Batch 26 awaiting Canary 行，FIX-SIGNAL.md 边界声明一致（不改账务后端、不误称已完成）。

## 命令结果

| 命令 | 结果 | 备注 |
|------|------|------|
| CSS/nav grep commands | PASS | sidebar 层级、滚动、折叠图标、导航图标全部匹配 |
| AuditLogs grep command | PASS | scroll/列名/nowrap 全部匹配 |
| TestPage grep + tests | PASS | 3 函数就位，gpt-3.5-turbo 硬编码已移除，3/3 测试通过 |
| UI label grep + targeted tests | PASS | 中文标签大量匹配，ModelGovernancePage 5/5 + DashboardPage 11/11 测试通过 |
| Evidence/status grep commands | PASS | evidence 文件存在，CURRENT-STATUS 包含 Batch 26 行 |
| `npm --prefix frontend test -- --run` | PASS | 16 文件 / 88 测试 / 0 失败 / 10.43s |
| `npm --prefix frontend run build` | PASS | 仅现有 large chunk warning，无新错误 |
| `git diff --check` | PASS | 无 whitespace 问题 |
| Browser smoke | SKIP | 无本地前端运行中，以 Codex 提交的 Playwright 截图为边界证据 |

## 新发现

无新发现。

## 结论

PASS。PRD2 Batch 26 UI/UX Hardening 5/5 Fix 全部验收通过，前端测试 88/88 通过，构建成功，无新发现问题。待 Sentinel commit。
