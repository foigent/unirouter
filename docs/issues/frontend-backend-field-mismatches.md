# 前后端字段不匹配的历史记录

Canary 于 2026-06-29 扫描时记录了下列问题。当前待办以
[Open Issues](OPEN-ISSUES.md) 为准。

| 历史发现 | 当前源码核对 |
| --- | --- |
| Bug #23。请求日志读取 `total_tokens`，当时的列表响应只返回输入和输出 Token。 | `ListLogs` 现在返回 `total_tokens`，日志页直接读取该字段。[后端列表](../../internal/admin/log_handlers.go) · [前端日志](../../frontend/src/pages/LogsPage.tsx) |
| Bug #25。旧测试页读取 `result.success`，旧响应返回 `status: "ok"`。 | `/test` 现在跳转到模型端点页。[前端路由](../../frontend/src/main.tsx) · [跳转回归](../../frontend/src/routing/__tests__/legacyTestRedirect.test.tsx) |
| Bug #21。旧凭据页曾将 `api_key` 改为后端所需的 `key`。另记录过 `key_prefix` 与 `key_masked` 的显示差异。 | `/channel-keys` 现在跳转到模型端点的凭据标签页。原扫描及修复经过见历史记录。[前端路由](../../frontend/src/main.tsx) · [历史 Bug 记录](../testing/archive/BUG-LOG.md) |
