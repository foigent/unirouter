# Issue: 前后端字段名不匹配汇总

**发现者**: Canary
**日期**: 2026-06-29
**状态**: 待修复

## 概述

全功能扫描发现多处前端和后端字段名不匹配，导致功能异常。

## Bug #23: LogsPage total_tokens

- 前端 `LogsPage.tsx:22` 期望 `total_tokens`
- 后端 `ListLogs` handler 只返回 `prompt_tokens` + `completion_tokens`
- 修复方向：后端添加 `total_tokens` 字段（`prompt_tokens + completion_tokens`）

## Bug #25: TestPage success vs status

- 前端 `TestPage.tsx:72` 检查 `result.success`
- 后端 `TestChannel` 返回 `status: "ok"`
- 修复方向：统一为 `success: true` 或前端改为检查 `result.status === "ok"`

## Bug #21 (已修复): ChannelKeysPage api_key vs key

- 前端发送 `api_key`，后端期望 `key`
- 已修复：前端改为 `key: values.api_key`

## 额外发现: channel_keys key_prefix vs key_masked

- 前端 `ChannelKeysPage.tsx` dataIndex 为 `key_prefix`
- 后端返回 `key_masked`
- 需确认是否为 bug
