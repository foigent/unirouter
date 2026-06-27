# FIX-SIGNAL.md — 验收指令（给 Canary）

**项目：** UniRouter — 企业级 AI 模型管控平台（Go + React）
**项目路径：** /Users/m4/Desktop/projects/UniRouter
**修复者：** main agent
**日期：** 2026-06-27
**修复轮次：** 第 5 轮

---

## 验收任务

请验证以下 1 条 fix（Bug #10 修复），以及前 12 条 fix 是否仍然正常。

---

## 前置条件（本轮预期不变的部分）

| 项目 | 预期状态 | 原因 |
|------|---------|------|
| 前 12 条 fix（#1-#12） | 预期仍 PASS | 本轮未修改这些代码 |
| 全量单元测试 | 预期 PASS | 本轮只改了 main.go + main_test.go |

> Canary 验收时：前置条件中列出的预期失败不算 FAIL，只需在 VERIFY-RESULT.md 的「前置条件核验」中注明。

---

### Fix #13: chatHandler AllowedModels 完全不生效（Bug #10）

**文件：** `cmd/uniroute/main.go:148-174`
**原问题（Canary 2026-06-27 发现）：**
- `req.Model` 永远是空字符串 — 只声明了 struct，没有 `json.NewDecoder(r.Body).Decode(&req)`
- `_ = allowed` — 计算结果被丢弃，即使 allowed=false 也不会 return 403
- 影响：所有设定了 AllowedModels 限制的 API key，其模型限制完全不生效

**修复方案：**
```go
if apiKey.AllowedModels != "" && apiKey.AllowedModels != "*" {
    // Buffer body so we can peek at model and still pass it to relay
    bodyBytes, err := io.ReadAll(r.Body)
    if err != nil {
        http.Error(w, `{"error":{"type":"server_error","message":"Failed to read request body"}}`, http.StatusBadRequest)
        return
    }
    r.Body.Close()
    r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

    var req struct {
        Model string `json:"model"`
    }
    if err := json.Unmarshal(bodyBytes, &req); err == nil && req.Model != "" {
        allowed := false
        for _, m := range strings.Split(apiKey.AllowedModels, ",") {
            if strings.TrimSpace(m) == req.Model {
                allowed = true
                break
            }
        }
        if !allowed {
            w.Header().Set("Content-Type", "application/json")
            w.WriteHeader(http.StatusForbidden)
            fmt.Fprintf(w, `{"error":{"type":"auth_error.model_not_allowed","message":"Model %s is not allowed for this API key","model":"%s"}}`, req.Model, req.Model)
            return
        }
    }
}
relay.HandleChatCompletion(w, r)
```

**关键设计决策：**
1. **Body 缓冲**：`io.ReadAll` → `bytes.NewBuffer` → `io.NopCloser`，确保 relay 仍能读到完整 body
2. **优雅降级**：如果 body 解析失败（err != nil 或 model 为空），放行请求而不是阻断（兼容非标准请求体）
3. **403 响应**：`auth_error.model_not_allowed` 类型，包含 model 名，前端可据此展示友好提示

**验证方法：**

1. **代码审查**：
   - `sed -n '148,185p' cmd/uniroute/main.go` — 确认 body 缓冲 + model 解析 + 403 return 完整
   - `grep -n '_ = allowed' cmd/uniroute/main.go` — 确认无丢弃结果
   - `grep -n 'io.ReadAll\|json.Unmarshal\|io.NopCloser' cmd/uniroute/main.go` — 确认三个关键调用都在

2. **单元测试**（6 个用例，全部应 PASS）：
   - `go test -run TestChatHandler -v ./cmd/uniroute/`
   - `TestChatHandler_AllowedModels_Bug_NoBodyParsing` — forbidden model → 403 ✅
   - `TestChatHandler_AllowedModels_Bug_AllowedModelAllowed` — allowed model → 200 ✅
   - `TestChatHandler_AllowedModels_Bug_NoAuth` — no key → 401 ✅
   - `TestChatHandler_AllowedModels_Bug_EmptyAllowedModels` — 无限制 → 200 ✅
   - `TestChatHandler_AllowedModels_Bug_Wildcard` — * 通配 → 200 ✅
   - `TestChatHandler_NilAPIKey` — nil key → 401 ✅

3. **全量回归**：
   - `go test ./... -count=1` — 全项目测试通过
   - `go build ./...` — 编译通过
   - `go vet ./cmd/uniroute/` — 无警告

4. **边界场景审查**：
   - 请求体为空（无 body）→ model 为空 → 放行（不阻断）
   - 请求体为 `{}`（无 model 字段）→ 放行
   - 请求体为 `{"model":"gpt-4"}` + AllowedModels=`"gpt-3.5-turbo"` → 403
   - 请求体为 `{"model":"gpt-4"}` + AllowedModels=`"gpt-4,gpt-3.5-turbo"` → 200

---

## 新增 imports

`cmd/uniroute/main.go` 新增了 3 个 import：
- `bytes` — body 缓冲
- `encoding/json` — model 解析
- `io` — ReadAll / NopCloser

---

## 测试文件变更

`cmd/uniroute/main_test.go` — 测试 mux 的 chatHandler 逻辑同步更新：
- 新增 `io`、`strings` import
- 测试 mux 的 AllowedModels 分支从 "留 bug 注释" 改为 "与 main.go 一致的正确逻辑"
- `TestChatHandler_AllowedModels_Bug_NoBodyParsing`：断言从 `t.Log("BUG CONFIRMED")` 改为 `t.Errorf("Expected 403")`
- `TestChatHandler_AllowedModels_Bug_AllowedModelAllowed`：断言从 `t.Log("passed — but only because bypassed")` 改为 `t.Errorf("Expected 200")`

---

## 验收结论（Canary 2026-06-27 04:56 填写）

- [x] Fix #13 验收结果：**PASS**
- [x] 前 12 条 fix 回归结果：**PASS**
- [x] 全量测试回归结果：**PASS**（14 包，0 FAIL）

详见 VERIFY-RESULT.md「Fix #13 验收」section。
