# FIX-SIGNAL.md — Bug #21 渠道密钥创建字段名不匹配

**修复者：** codex agent
**日期：** [见文件系统时间戳]
**修复轮次：** 第 13 轮

---

## 前置条件（本轮预期不变的部分）

| 项目 | 预期状态 | 原因 |
|------|---------|------|
| 后端 Go 测试 | 559 PASS | 本轮未修改后端代码 |
| 前端测试 (33 tests) | 仍 PASS | ChannelKeysPage 无测试文件，不影响现有测试 |
| 前端编译 | PASS | 仅改 payload 字段名 |

> Canary 验收时：前置条件中列出的预期失败不算 FAIL，只需在结论中注明。

---

## Fix #21: 渠道密钥创建失败 — 前端 api_key ≠ 后端 key (High)

**文件：** `frontend/src/pages/ChannelKeysPage.tsx:60,61`

**原问题：** 前端 create/update 调用发送 `{ channel_id, api_key }`，后端 `CreateChannelKey` handler 期望 `{ channel_id, key }`（`json:"key"`）。`req.Key` 为空 → 返回 400 `"channel_id and key are required"`。

**修复内容：** 两处 payload 字段名 `api_key` → `key`：
- `api.updateChannelKey(editing.id, { channel_id, key: values.api_key })`
- `api.createChannelKey({ channel_id, key: values.api_key })`

**验证方法：**
```bash
cd /Users/m4/Desktop/projects/UniRouter/frontend
npx vitest run
# 期望：33 PASS, 0 FAIL

npx tsc --noEmit 2>&1 | head -10
# 期望：无新增错误
```

---

**请 Canary 验收：** `docs/agent-workflows/FIX-SIGNAL.md`
