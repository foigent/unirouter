# FIX-SIGNAL.md — 验收指令（给 Canary）

**项目：** UniRouter — 企业级 AI 模型管控平台（Go + React）
**项目路径：** /Users/m4/Desktop/projects/UniRouter
**修复者：** main agent
**日期：** 2026-06-27
**修复轮次：** 第 6 轮

---

## 验收任务

请验证以下 2 条 fix（Bug #10 + Bug #11），以及前 12 条 fix 是否仍然正常。

---

## 前置条件（本轮预期不变的部分）

| 项目 | 预期状态 | 原因 |
|------|---------|------|
| 前 12 条 fix（#1-#12） | 预期仍 PASS | 本轮未修改这些代码 |
| 全量单元测试 | 预期 PASS | 本轮只改了 main.go + main_test.go + LogsPage.tsx |

> Canary 验收时：前置条件中列出的预期失败不算 FAIL，只需在 VERIFY-RESULT.md 的「前置条件核验」中注明。

---

### Fix #13: chatHandler AllowedModels 完全不生效（Bug #10）

**文件：** `cmd/uniroute/main.go:155-179`
**原问题：** req.Model 永远空 + `_ = allowed` 丢弃结果 → 模型限制不生效
**修复方案：** `io.ReadAll` + `json.Unmarshal` 解析 model，body 缓冲给 relay，forbidden → 403
**验证方法：**
1. `sed -n '155,179p' cmd/uniroute/main.go` — 确认 body 缓冲 + model 解析 + 403 return
2. `go test -run TestChatHandler -v ./cmd/uniroute/` — 6 个用例全部 PASS
3. `go build ./... && go vet ./cmd/uniroute/` — 编译 + vet 通过

---

### Fix #14: 调用日志供应商下拉框硬编码（Bug #11）

**文件：** `frontend/src/pages/LogsPage.tsx:36,47-52,119-122`
**原问题：** 供应商下拉框写死 openai/deepseek/qwen/zhipu，不会随渠道变化
**修复方案：**
1. 新增 `providers` state（第 36 行）
2. 组件挂载时从 `/api/v1/channels` 获取渠道列表，提取去重 provider（第 47-52 行）
3. 下拉框改为 `{providers.map((p) => <Select.Option>)}` 动态渲染（第 119-122 行）

**验证方法：**
1. `grep -n 'providers\|Select.Option' frontend/src/pages/LogsPage.tsx` — 确认动态渲染
2. `cd frontend && npm run build` — TypeScript 编译 + Vite 构建通过
3. 浏览器验证：登录后台 → 调用日志页 → 供应商下拉框应显示实际配置的渠道 provider（如 deepseek、kimi、mimo），不应显示未配置的（如 openai、qwen、zhipu）

---

## 验收结论（Canary 2026-06-27 10:01 填写）

- [x] Fix #13 验收结果：**PASS**
- [x] Fix #14 验收结果：**PASS**
- [x] 前 12 条 fix 回归结果：**PASS**
- [x] 全量测试回归结果：**PASS**（14 包，0 FAIL）

详见 VERIFY-RESULT.md。
