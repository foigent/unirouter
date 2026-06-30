# VERIFY-RESULT.md — 第 14 轮 FIX-SIGNAL 验收结果

**验收者：** Canary
**日期：** 2026-06-30（文件系统时间戳为准）
**对应轮次：** 第 14 轮（对应 FIX-SIGNAL.md — Bug #23~#28 + 前端动态模型列表）

---

## 逐条结果

- **Fix #23 (ListLogs SELECT 列数不匹配):** PASS — `handlers.go:824` SELECT 扩展为 17 列（id, request_id, model, provider, channel, prompt_tokens, completion_tokens, total_tokens, cost_cents, status_code, latency_ms, ttft_ms, upstream_status_code, upstream_error, usage_source, is_stream, created_at），Scan dest 对齐
- **Fix #24 (TestChannel URL 双重 /v1/):** PASS — `handlers.go:2400-2405` TrimRight + HasSuffix 双重清理，避免 `/v1/v1/` 重复
- **Fix #25 (TestChannel 成功响应缺少 success 字段):** PASS — `handlers.go:2421` 错误路径返回 `"success": false`，`handlers.go:2436` 成功路径返回 `"success": true`
- **Fix #26 (ModelPricesPage 模型名称手动输入):** PASS — `ModelPricesPage.tsx:133` 已改为 `Form.Select` + filter 搜索，数据源从 `/model-names` API 动态获取
- **Fix #27 (OrgModelPricesPage 模型名称手动输入):** PASS — `OrgModelPricesPage.tsx:153` 同 Fix #26，改为 `Form.Select` 动态下拉
- **Fix #28 (Token 对账模型筛选硬编码):** PASS — `ReconciliationPage.tsx:43` 从 `/model-names` API 动态加载，替换硬编码列表
- **GetModelNames 新端点:** FAIL — FIX-SIGNAL 声明查询 `SELECT DISTINCT model_name FROM model_prices ORDER BY model_name`，实际代码查询 `SELECT DISTINCT model FROM call_logs WHERE model != '' ORDER BY model`。表不同（`model_prices` vs `call_logs`），列名不同（`model_name` vs `model`）。数据源选择可能有业务考量，但与 FIX-SIGNAL 描述不一致
- **key_masked → key_prefix:** PASS — `handlers.go:1377` ListChannelKeys 返回字段已从 `key_masked` 改为 `key_prefix`

## 前置条件核验

- 后端 Go 测试：568 PASS ✅（FIX-SIGNAL 预期 559+，实际 568，因 Canary 新增 9 个 billing 单元测试）
- `go build ./...`: PASS ✅
- `go vet ./...`: PASS ✅
- `go test ./... -race`: 0 DATA RACE ✅
- 前端编译：未验证（node_modules 未安装，环境问题非代码问题）

## 全局构建

- `go build ./...`: PASS
- `go vet ./...`: PASS
- `go test ./... -count=1`: PASS（16/16 包 OK）
- `go test ./... -race`: PASS（0 DATA RACE）

## 新发现的问题

1. **GetModelNames 数据源与 FIX-SIGNAL 不一致**（见上方 FAIL 详情）— 业务上 `call_logs` 返回已使用的模型，`model_prices` 返回已配置的模型。创建新模型价格时，`model_prices` 更合理（新模型无 call log 则不出现在下拉中）。建议 Codex 确认数据源选择意图

## 结论

**7 PASS / 1 FAIL。** GetModelNames 数据源与 FIX-SIGNAL 描述不一致，需要 Codex 确认意图或修正。其余修复全部验证通过。
