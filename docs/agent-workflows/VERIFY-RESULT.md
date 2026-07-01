# VERIFY-RESULT.md — PRD1 API Key, Channel Test, Gateway, and Stream Fix 验收结果

**验收者：** OpenClaw Canary
**日期：** 2026-07-01
**对应轮次：** PRD1 API key, channel test, upstream URL, and stream final chunk fix

---

## 逐条结果

- **Fix #1 (API key 完整密钥复制 fallback):** PASS — `KeysPage.tsx:9` `fallbackCopyText`，`:18` `document.execCommand('copy')` fallback，`:133` 优先 `navigator.clipboard?.writeText && window.isSecureContext`
- **Fix #2 (API key 重名校验):** PASS — `key_handlers.go:83` 同 org+name 查重，`:91` 返回 HTTP 409。`TestCreateAPIKey_DuplicateName` PASS
- **Fix #3 (渠道测试 401 不算成功 + 使用所选模型):** PASS — `test_handlers.go:77/79` `/chat/completions`，`:120` 仅 2xx 为 success，`:127` 非 2xx 返回错误。`TestTestChannel_Upstream401IsFailureAndUsesSelectedModel` PASS
- **Fix #4 (Gateway 上游 URL 不再双拼 `/v1`):** PASS — `relay.go:60` `upstreamEndpoint` helper，`:204` 和 `:725` 使用。`TestUpstreamEndpoint_DoesNotDoubleAppendV1` 3 子测试 PASS
- **Fix #5 (Stream final chunk 顺序 + `_uniroute` 包装):** PASS — `relay.go:429` 拦截上游 `data: [DONE]`，`:573` 在 final chunk 中写入 `_uniroute`（含 request_id/cost/provider/channel），`:583` 最后才发 `data: [DONE]`。`relay_mock_test.go:905` `TestRelayStreamRequest_UnirouteExtension` 验证 `_uniroute` 在 `[DONE]` 前且 `[DONE]` 只出现 1 次（`:950-951`）。另有 6 个 stream 测试（NormalFlow/NoUpstreamUsage/SettleError/UpstreamUsageSource/EmptyStream/UnirouteExtension）全部 PASS

## 全局验证

| 检查项 | 结果 |
|--------|------|
| `git diff --check` | PASS |
| targeted admin tests（6/6） | PASS |
| targeted gateway tests（17/17，含 6 stream + 3 upstream + 8 chat） | PASS |
| `go test ./...` | PASS（16/16 包） |
| `npm run build` | PASS（3.82s） |
| `npm test -- --run` | PASS（36/36 测试，5 文件） |

## 新发现的问题

无

## 结论

**5/5 全部 PASS。** PRD1 API key, channel test, upstream URL, and stream final chunk fix 验收通过。
