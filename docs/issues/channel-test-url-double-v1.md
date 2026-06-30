# Issue: 渠道测试 URL 构造 — base_url 含 /v1 时双拼

**发现者**: Canary
**日期**: 2026-06-29
**状态**: 待修复
**对应 Bug**: #24

## 问题

`TestChannel` handler（handlers.go:2374）拼接测试 URL：
```go
testURL := strings.TrimRight(baseURL, "/") + "/v1/models"
```

当 `base_url` 已含 `/v1`（如 `https://token-plan-cn.xiaomimimo.com/v1`），结果为 `/v1/v1/models` → 404。

## 影响

所有 base_url 以 `/v1` 结尾的渠道，渠道测试都会失败。

## 修复方向

检查 base_url 是否已以 `/v1` 结尾，如果是则不再拼接 `/v1`：
```go
if strings.HasSuffix(baseURL, "/v1") {
    testURL = baseURL + "/models"
} else {
    testURL = strings.TrimRight(baseURL, "/") + "/v1/models"
}
```
