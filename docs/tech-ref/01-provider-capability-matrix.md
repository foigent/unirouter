# Provider Capability Matrix（模型能力矩阵）

> **版本**：v2.0
> **日期**：2026-06-18
> **用途**：开发团队配置渠道和预冻结公式的核心参考
> **变更**：基于 v1.0 修正，补充 2026 年新模型，修正 usage 格式差异

---

## 一、国产模型

| 能力 | DeepSeek V4-Pro | DeepSeek V4-Flash | Qwen3.7-Max | Qwen3.7-Plus | GLM-5 | GLM-4 |
|------|----------------|------------------|-------------|--------------|-------|-------|
| **Channel Type** | `deepseek` (36) | `deepseek` (36) | `ali` (17) | `ali` (17) | `zhipu` (16) | `zhipu` (16) |
| **Chat Completions** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| **Embeddings** | ❌ | ❌ | ✅ | ✅ | ✅ | ✅ |
| **Function Calling** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| **Vision** | ❌ | ❌ | ✅ | ✅ | ✅ | ✅ |
| **流式 usage 位置** | 最后一个 chunk | 最后一个 chunk | 最后一个 chunk | 最后一个 chunk | 最后一个 chunk | 最后一个 chunk |
| **usage 字段名** | `prompt_tokens` / `completion_tokens` | 同左 | 同左 | 同左 | 同左 | 同左 |
| **max_context** | 128K | 128K | 128K | 128K | 128K | 128K |
| **default_max_output** | 4096 | 4096 | 8192 | 8192 | 4096 | 4096 |
| **输入价格（¥/百万Token）** | 2.00 | 0.13 | 2.40 | 0.80 | 5.00 | 0.10 |
| **输出价格（¥/百万Token）** | 8.00 | 0.28 | 9.60 | 2.00 | 10.00 | 0.10 |

---

## 二、海外模型（通过合规代理商接入）

| 能力 | GPT-5 | GPT-4o | Claude Opus 4 | Claude Sonnet 4 | Gemini 2.5 Pro | Gemini 2.5 Flash |
|------|-------|--------|---------------|-----------------|----------------|-----------------|
| **Channel Type** | `openai` (1) | `openai` (1) | `anthropic` (14) | `anthropic` (14) | `geminiv2` (50) | `geminiv2` (50) |
| **Chat Completions** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| **Embeddings** | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ |
| **Function Calling** | ✅ | ✅ | ✅ (tool_use) | ✅ (tool_use) | ✅ | ✅ |
| **Vision** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| **流式 usage 位置** | 最后一个 chunk | 最后一个 chunk | `message_start` + `message_delta` | 同左 | 最后一个 chunk | 最后一个 chunk |
| **usage 字段名** | `prompt_tokens` / `completion_tokens` | 同左 | `input_tokens` / `output_tokens` | 同左 | `prompt_tokens` / `completion_tokens` | 同左 |
| **max_context** | 128K | 128K | 200K | 200K | 1M | 1M |
| **default_max_output** | 16384 | 16384 | 32768 | 32768 | 8192 | 8192 |
| **输入价格（$/百万Token）** | 10.00 | 2.50 | 15.00 | 3.00 | 1.25 | 0.075 |
| **输出价格（$/百万Token）** | 30.00 | 10.00 | 75.00 | 15.00 | 5.00 | 0.30 |

---

## 三、Claude usage 格式特殊处理

Claude 的流式 usage 分布在两个 event 中，与其他模型不同：

```
event: message_start
data: {"type":"message_start","message":{"usage":{"input_tokens":1234}}}

... (中间 chunks) ...

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":567}}
```

**实现要求**：
- `StreamChunkHandler` 需要识别 `anthropic` 渠道类型
- 在 `message_start` 中提取 `input_tokens`
- 在 `message_delta` 中提取 `output_tokens`
- 合并后写入 `call_logs`

---

## 四、预冻结价格计算示例

### 示例 1：DeepSeek V4-Pro，中文 prompt

```
请求：2000 字中文 prompt + max_tokens=2000
模型：DeepSeek V4-Pro（输入 ¥2/百万 Token，输出 ¥8/百万 Token）

promptTokens = ceil(2000 / 1.5) = 1334 tokens
inputCost = 1334 / 1,000,000 × 200 = 0.27 分
outputCost = 2000 / 1,000,000 × 800 × 1.5 = 2.40 分
freezeAmount = max(0.27 + 2.40, 10) = 10 分 = ¥0.10
```

### 示例 2：GPT-5，英文 prompt

```
请求：5000 字英文 prompt + max_tokens=4000
模型：GPT-5（输入 $10/百万 Token，输出 $30/百万 Token）
汇率：7.25

promptTokens = ceil(5000 / 4) = 1250 tokens
inputCost = 1250 / 1,000,000 × 1000 × 7.25 = 9.06 分
outputCost = 4000 / 1,000,000 × 3000 × 7.25 × 1.5 = 130.50 分
freezeAmount = max(9.06 + 130.50, 10) = 139.56 分 = ¥1.40
```

---

## 五、价格管理

- `model_prices` 表存储全局基础价格（以分为单位，CNY；支持小数分以覆盖 `¥0.002/千 token` 这类小额价格）
- `org_model_prices` 表存储组织级覆盖（`multiplier` 字段）
- 海外模型价格 = USD × `EXCHANGE_RATE`（环境变量，默认 7.25）
- 价格变更后立即生效（无缓存延迟）

> ⚠️ **注意**：以上价格为 AI 顾问提供的参考值，开发前必须从各厂商官网验证实际 List Price。
