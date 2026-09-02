import { describe, expect, it } from 'vitest';
import {
  adapterTypeForProvider,
  providerPreset,
  providerPresetAdapterConfigMatches,
  providerPresets,
} from '../providerEndpoints';

describe('provider endpoint presets', () => {
  it('keeps custom providers discoverable without treating presets as an allowlist', () => {
    expect(providerPresets[0].value).toBe('custom');
    expect(providerPreset('minimax')).toMatchObject({
      provider: 'minimax', baseURL: 'https://api.minimaxi.com/v1', endpointClass: 'minimax_cn_openai_v1',
      adapterConfig: { endpoint_path_mode: 'canonical_v1' },
    });
    expect(providerPreset('mimo')).toMatchObject({ provider: 'mimo', baseURL: 'https://api.xiaomimimo.com/v1' });
    expect(providerPreset('glm')).toMatchObject({
      provider: 'glm', baseURL: 'https://open.bigmodel.cn/api/paas/v4',
      endpointClass: 'glm_official_openai_v4', adapterConfig: { endpoint_path_mode: 'base_relative' },
    });
    expect(providerPreset('gemini')).toMatchObject({
      provider: 'gemini', baseURL: 'https://generativelanguage.googleapis.com/v1beta/openai',
      endpointClass: 'gemini_openai_compatibility_v1beta',
    });
    expect(providerPreset('gemini').note).toContain('Gemini 3.7 Flash、3.5 Flash 与 3.5 Flash-Lite');
    expect(providerPreset('gemini').note).toContain('原生缓存、搜索、视频/PDF 继续 fail-closed');
    expect(providerPreset('deepseek')).toMatchObject({
      provider: 'deepseek', baseURL: 'https://api.deepseek.com',
      endpointClass: 'deepseek_official_openai',
      adapterConfig: { endpoint_path_mode: 'base_relative' },
    });
    expect(providerPreset('kimi')).toMatchObject({
      provider: 'kimi', baseURL: 'https://api.moonshot.cn/v1',
      endpointClass: 'kimi_platform_openai_v1',
      adapterConfig: { endpoint_path_mode: 'canonical_v1' },
    });
    expect(providerPreset('kimi').note).toContain('Kimi K3 当前开放 exact 非流式 function tools');
    expect(providerPreset('kimi').note).toContain('system Dynamic Tools 声明并存');
    expect(providerPreset('kimi').note).toContain('省略/none/auto/required');
    expect(providerPreset('kimi').note).toContain('所有可见函数名冻结在同一 Plan 快照');
    expect(providerPreset('kimi').note).toContain('named choice 与工具流式增量仍 fail-closed');
    expect(providerPreset('doubao')).toMatchObject({
      provider: 'doubao', baseURL: 'https://ark.cn-beijing.volces.com/api/v3',
      endpointClass: 'doubao_ark_cn_beijing_openai_v3',
      adapterConfig: { endpoint_path_mode: 'base_relative' },
    });
		expect(providerPreset('doubao').note).toContain('Seed 2.1 Pro 当前开放 exact text/reasoning streaming core、非流式 function tools/tool_choice、strict=true 的非流式 json_schema');
		expect(providerPreset('doubao').note).toContain('Pro 图像与 stream、tools/tool history、结构化输出、penalties、n 不组合');
    expect(providerPreset('doubao').note).toContain('工具调用支持 none/auto/required/named、并行默认开启');
    expect(providerPreset('doubao').note).toContain('工具 strict schema 与工具流式增量仍 fail-closed');
		expect(providerPreset('doubao').note).toContain('json_object、非严格 schema、结构化流式输出、工具 strict schema 与工具流式增量仍 fail-closed');
    expect(providerPreset('doubao').note).toContain('minimal/low/medium/high 且默认 high');
		expect(providerPreset('doubao').note).toContain('Seed 2.1 Turbo 当前开放 exact text/reasoning streaming core、非流式 function tools/tool_choice、strict=true 的非流式 json_schema');
		expect(providerPreset('doubao').note).toContain('非流式 user image_url 内联 JPEG 安全子集');
		expect(providerPreset('doubao').note).toContain('Turbo 图像与 stream、tools/tool history、结构化输出、penalties、n 不组合');
    expect(providerPreset('doubao').note).toContain('Seed Evolving 当前开放 floating alias 的 exact text/reasoning streaming core');
    expect(providerPreset('doubao').note).toContain('none/minimal/low/medium/high/xhigh/max 且默认 high');
    expect(providerPreset('doubao').note).toContain('encrypted_content 仅作不透明证据');
    expect(providerPreset('doubao').note).toContain('exact non-stream function tools/tool_choice');
    expect(providerPreset('doubao').note).toContain('strict=true 的非流式 json_schema');
    expect(providerPreset('doubao').note).toContain('非流式 user image_url 内联 JPEG 安全子集');
    expect(providerPreset('doubao').note).toContain('单张 decoded ≤256 KiB');
    expect(providerPreset('doubao').note).toContain('usage.prompt_tokens_details.cached_tokens');
		expect(providerPreset('doubao').note).toContain('Pro、Turbo 与 Seed Evolving 的隐式缓存按 Profile 始终生效');
    expect(providerPreset('doubao').note).toContain('零值同样是权威证据');
    expect(providerPreset('doubao').note).toContain('远程图片、其他媒体、供应商 10 MB 图像上限全覆盖');
    expect(providerPreset('doubao').note).toContain('未建模 refusal 继续 fail-closed');
    expect(providerPreset('doubao').note).toContain('工具流式增量仍 fail-closed');
    expect(providerPreset('doubao').note).toContain('默认 output 4,096');
    expect(providerPreset('openai')).toMatchObject({
      region: 'global', apiSurface: 'openai_chat_completions', endpointClass: 'openai_official_v1',
    });
    expect(providerPreset('openai').note).toContain('GPT-5.6 Sol/Terra/Luna');
    expect(providerPreset('openai-responses')).toMatchObject({
      provider: 'openai', region: 'global', apiSurface: 'openai_responses',
      baseURL: 'https://api.openai.com', endpointClass: 'openai_official_origin',
      adapterConfig: { endpoint_path_mode: 'canonical_v1' },
    });
    expect(providerPreset('openai-responses').note).toContain('GPT-5.6 Sol/Terra/Luna 当前均是 strict core text + exact text SSE');
    expect(providerPreset('openai-responses').note).toContain('function tools 与结构化输出（含非流式）仍未开放');
    expect(providerPreset('openai-responses').note).toContain('内置工具仅 documentation_required');
    expect(providerPreset('openai-responses').note).toContain('不影响独立 Chat Profile 的 function tools');
    expect(providerPreset('xai')).toMatchObject({
      provider: 'xai', region: 'global', baseURL: 'https://api.x.ai/v1',
      endpointClass: 'xai_official_openai_v1', adapterConfig: { endpoint_path_mode: 'canonical_v1' },
    });
    expect(providerPreset('anthropic')).toMatchObject({
      region: 'global', apiSurface: 'anthropic_messages', baseURL: 'https://api.anthropic.com',
      endpointClass: 'anthropic_official_origin',
    });
    expect(providerPreset('anthropic').note).toContain('Claude Sonnet 5、Opus 5 与 pinned Haiku 4.5 均提供 exact strict text/reasoning 和文本 SSE');
    expect(providerPreset('anthropic').note).toContain('Sonnet 5 还提供非流式用户自定义 function tools');
    expect(providerPreset('anthropic').note).toContain('summarized thinking 只允许经输出 guard 恒等放行');
    expect(providerPreset('anthropic').note).toContain('服务端工具、工具流');
    expect(providerPreset('dashscope')).toMatchObject({
      provider: 'dashscope', region: 'cn-beijing', editableBaseURL: true, baseURL: '',
    });
    expect(providerPreset('dashscope-shared').baseURL).toBe('https://dashscope.aliyuncs.com/compatible-mode/v1');
    expect(providerPreset('not-built-in').value).toBe('custom');
  });

  it('defaults an unknown provider identity to the supported OpenAI-compatible protocol', () => {
    expect(adapterTypeForProvider('custom-aggregator')).toBe('openai_compatible');
    expect(adapterTypeForProvider('anthropic')).toBe('anthropic_native');
    expect(adapterTypeForProvider('azure')).toBe('azure_openai');
  });

  it('matches the effective path mode before treating an existing connection as an official preset', () => {
    expect(providerPresetAdapterConfigMatches(providerPreset('deepseek'), {
      endpoint_path_mode: 'base_relative',
    })).toBe(true);
    expect(providerPresetAdapterConfigMatches(providerPreset('deepseek'), {})).toBe(false);
    expect(providerPresetAdapterConfigMatches(providerPreset('deepseek'), {
      endpoint_path_mode: 'base_relative', unknown: true,
    })).toBe(false);
    expect(providerPresetAdapterConfigMatches(providerPreset('openai'), [])).toBe(false);
    expect(providerPresetAdapterConfigMatches(providerPreset('gemini'), undefined)).toBe(false);
    expect(providerPresetAdapterConfigMatches(providerPreset('kimi'), {})).toBe(true);
    expect(providerPresetAdapterConfigMatches(providerPreset('kimi'), {
      endpoint_path_mode: 'base_relative',
    })).toBe(false);
    expect(providerPresetAdapterConfigMatches(providerPreset('openai'), {})).toBe(true);
    expect(providerPresetAdapterConfigMatches(providerPreset('openai-responses'), {})).toBe(true);
    expect(providerPresetAdapterConfigMatches(providerPreset('minimax'), {})).toBe(true);
    expect(providerPresetAdapterConfigMatches(providerPreset('minimax'), {
      endpoint_path_mode: 'base_relative',
    })).toBe(false);
    expect(providerPresetAdapterConfigMatches(providerPreset('glm'), {
      endpoint_path_mode: 'base_relative',
    })).toBe(true);
    expect(providerPresetAdapterConfigMatches(providerPreset('glm'), {})).toBe(false);
    expect(providerPresetAdapterConfigMatches(providerPreset('xai'), {})).toBe(true);
		expect(providerPresetAdapterConfigMatches(providerPreset('xai'), {
      endpoint_path_mode: 'base_relative',
		})).toBe(false);
    expect(providerPresetAdapterConfigMatches(providerPreset('doubao'), {
      endpoint_path_mode: 'base_relative',
    })).toBe(true);
    expect(providerPresetAdapterConfigMatches(providerPreset('doubao'), {})).toBe(false);
  });
});
