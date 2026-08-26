import { describe, expect, it } from 'vitest';
import type { ModelExecutionProfile } from '../../api/modelExecutionProfiles';
import { providerPreset } from '../providerEndpoints';
import { compareProfileVersions, reviewedModelOnboardingLimits, reviewedModelOptions } from '../modelOnboarding';

function profile(overrides: Partial<ModelExecutionProfile>): ModelExecutionProfile {
  return {
    id: 'deepseek.v4-flash.api',
    version: '1.0.0',
    label: 'DeepSeek V4 Flash 官方 API',
    current: true,
    tokenizer_profile_id: 'tokenizer.byte-upper.v1',
    tokenizer_profile_version: '1.0.0',
    reasoning_profile: 'reasoning.deepseek.v1',
    cache_usage_profile: 'cache.deepseek.v1',
    protocol_profile: 'protocol.deepseek-openai-chat.v1',
    provider: 'deepseek',
    region: 'cn',
    api_surface: 'openai_chat_completions',
    operations: ['openai.chat_completions.v1'],
    endpoint_classes: ['deepseek_official_openai'],
    upstream_model_ids: ['deepseek-v4-flash'],
    default_output_authority: 'unirouter_policy',
    catalog_evidence_state: 'locally_conformant',
    provider_context_window: 1_000_000,
    provider_max_output: 384_000,
    default_context_window: 1_000_000,
    default_output_limit: 32_768,
    provider_capabilities: ['reasoning'],
    capabilities: ['reasoning'],
    always_required_capabilities: [],
    aliases: ['DeepSeek V4 Flash', 'deepseek-v4-flash'],
    evidence_refs: ['https://example.test/evidence'],
    ...overrides,
  };
}

describe('model onboarding profile composition', () => {
  it('uses only the backend-declared current profile for the selected official provider', () => {
    const options = reviewedModelOptions([
      profile({ version: '1.0.0', current: false }),
      profile({ version: '1.10.0', current: true }),
      profile({ id: 'kimi.k3.api', version: '9.0.0', provider: 'kimi', aliases: ['kimi-k3'] }),
    ], providerPreset('deepseek'));

    expect(options).toHaveLength(1);
    expect(options[0]).toMatchObject({
      key: 'deepseek.v4-flash.api@1.10.0',
      recommendedPublicModelID: 'deepseek-v4-flash',
      recommendedUpstreamModelID: 'deepseek-v4-flash',
      operationIDs: ['openai.chat_completions.v1'],
    });
  });

  it('fails closed when a profile ID has multiple current versions', () => {
    expect(reviewedModelOptions([
      profile({ version: '1.0.0', current: true }),
      profile({ version: '1.1.0', current: true }),
    ], providerPreset('deepseek'))).toEqual([]);
  });

  it('rejects unsafe aliases and profiles without executable defaults', () => {
    expect(reviewedModelOptions([
      profile({ aliases: ['Human name only'], default_output_limit: 0 }),
    ], providerPreset('deepseek'))).toEqual([]);
    expect(reviewedModelOptions([profile({})], providerPreset('custom'))).toEqual([]);
  });

  it('derives a lowercase public ID while preserving a case-sensitive MiniMax upstream ID', () => {
    const options = reviewedModelOptions([profile({
      id: 'minimax.m3.token-plan',
      provider: 'minimax',
      endpoint_classes: ['minimax_cn_openai_v1'],
      aliases: ['MiniMax M3', 'MiniMax-M3'],
      upstream_model_ids: ['MiniMax-M3'],
    })], providerPreset('minimax'));

    expect(options[0]).toMatchObject({
      publicModelIDs: ['minimax-m3'],
      upstreamModelIDs: ['MiniMax-M3'],
      recommendedPublicModelID: 'minimax-m3',
      recommendedUpstreamModelID: 'MiniMax-M3',
    });
  });

  it('compares semantic versions numerically', () => {
    expect(compareProfileVersions('1.10.0', '1.9.9')).toBeGreaterThan(0);
    expect(compareProfileVersions('2.0.0', '10.0.0')).toBeLessThan(0);
  });

  it('maps the current GPT-5.6 reviewed subset to Chat Completions', () => {
    const options = reviewedModelOptions([profile({
      id: 'openai.gpt-5.6-sol.chat',
      provider: 'openai',
      region: 'global',
      label: 'GPT-5.6 Sol 官方 API',
      protocol_profile: 'opaque.profile.name.without.operation.hints',
      aliases: ['GPT-5.6 Sol', 'gpt-5.6-sol'],
      upstream_model_ids: ['gpt-5.6-sol'],
      endpoint_classes: ['openai_official_v1'],
    })], providerPreset('openai'));

    expect(options[0]).toMatchObject({
      recommendedPublicModelID: 'gpt-5.6-sol',
      recommendedUpstreamModelID: 'gpt-5.6-sol',
      operationIDs: ['openai.chat_completions.v1'],
    });
  });

  it('keeps GPT-5.6 Responses onboarding separate from the Chat surface', () => {
    const chat = profile({
      id: 'openai.gpt-5.6-sol.chat', provider: 'openai', region: 'global',
      label: 'GPT-5.6 Sol Chat Completions',
      api_surface: 'openai_chat_completions', operations: ['openai.chat_completions.v1'],
      endpoint_classes: ['openai_official_v1'], aliases: ['gpt-5.6-sol'], upstream_model_ids: ['gpt-5.6-sol'],
    });
    const responses = profile({
      id: 'openai.gpt-5.6-sol.responses', provider: 'openai', region: 'global',
      version: '1.3.0',
      label: 'GPT-5.6 Sol Responses Strict Text Streaming',
      protocol_profile: 'unirouter.openai.responses.text.v2', protocol_profile_version: '1.0.0',
      api_surface: 'openai_responses', operations: ['openai.responses.v1'],
      endpoint_classes: ['openai_official_origin'], aliases: ['gpt-5.6-sol'], upstream_model_ids: ['gpt-5.6-sol'],
      catalog_evidence_state: 'locally_conformant', provider_capabilities: ['reasoning', 'reasoning_effort'],
      capabilities: ['reasoning_effort', 'streaming'], provider_context_window: 1_050_000,
      executable_context_window: 272_000, default_context_window: 272_000,
    });
    const terraChat = profile({
      id: 'openai.gpt-5.6-terra.chat', provider: 'openai', region: 'global',
      label: 'GPT-5.6 Terra Chat Completions',
      api_surface: 'openai_chat_completions', operations: ['openai.chat_completions.v1'],
      endpoint_classes: ['openai_official_v1'], aliases: ['gpt-5.6-terra'], upstream_model_ids: ['gpt-5.6-terra'],
    });
    const terraResponses = profile({
      id: 'openai.gpt-5.6-terra.responses', provider: 'openai', region: 'global',
      version: '1.3.0',
      label: 'GPT-5.6 Terra Responses Strict Text Streaming',
      protocol_profile: 'unirouter.openai.responses.text.v2', protocol_profile_version: '1.0.0',
      api_surface: 'openai_responses', operations: ['openai.responses.v1'],
      endpoint_classes: ['openai_official_origin'], aliases: ['gpt-5.6-terra'], upstream_model_ids: ['gpt-5.6-terra'],
      catalog_evidence_state: 'locally_conformant', provider_capabilities: ['reasoning', 'reasoning_effort'],
      capabilities: ['reasoning_effort', 'streaming'], provider_context_window: 1_050_000,
      executable_context_window: 272_000, default_context_window: 272_000,
    });
    const lunaChat = profile({
      id: 'openai.gpt-5.6-luna.chat', provider: 'openai', region: 'global',
      label: 'GPT-5.6 Luna Chat Completions',
      api_surface: 'openai_chat_completions', operations: ['openai.chat_completions.v1'],
      endpoint_classes: ['openai_official_v1'], aliases: ['gpt-5.6-luna'], upstream_model_ids: ['gpt-5.6-luna'],
    });
	const lunaResponses = profile({
	  id: 'openai.gpt-5.6-luna.responses', provider: 'openai', region: 'global',
	  version: '1.3.0',
	  label: 'GPT-5.6 Luna Responses Strict Text Streaming',
      protocol_profile: 'unirouter.openai.responses.text.v2', protocol_profile_version: '1.0.0',
      api_surface: 'openai_responses', operations: ['openai.responses.v1'],
      endpoint_classes: ['openai_official_origin'], aliases: ['gpt-5.6-luna'], upstream_model_ids: ['gpt-5.6-luna'],
	  catalog_evidence_state: 'locally_conformant', provider_capabilities: ['reasoning', 'reasoning_effort'],
	  capabilities: ['reasoning_effort', 'streaming'], provider_context_window: 1_050_000,
      executable_context_window: 272_000, default_context_window: 272_000,
    });

    const profiles = [chat, responses, terraChat, terraResponses, lunaChat, lunaResponses];
    expect(reviewedModelOptions(profiles, providerPreset('openai'))).toEqual([
      expect.objectContaining({ key: 'openai.gpt-5.6-luna.chat@1.0.0', operationIDs: ['openai.chat_completions.v1'] }),
      expect.objectContaining({ key: 'openai.gpt-5.6-sol.chat@1.0.0', operationIDs: ['openai.chat_completions.v1'] }),
      expect.objectContaining({ key: 'openai.gpt-5.6-terra.chat@1.0.0', operationIDs: ['openai.chat_completions.v1'] }),
    ]);
    expect(reviewedModelOptions(profiles, providerPreset('openai-responses'))).toEqual([
	  expect.objectContaining({ key: 'openai.gpt-5.6-luna.responses@1.3.0', operationIDs: ['openai.responses.v1'] }),
      expect.objectContaining({ key: 'openai.gpt-5.6-sol.responses@1.3.0', operationIDs: ['openai.responses.v1'] }),
      expect.objectContaining({ key: 'openai.gpt-5.6-terra.responses@1.3.0', operationIDs: ['openai.responses.v1'] }),
    ]);
    expect(reviewedModelOnboardingLimits(responses)).toMatchObject({
      providerContextWindow: 1_050_000,
      executableContextWindow: 272_000,
      publicContextLength: 272_000,
      deploymentMaxContext: 272_000,
    });
  });

  it('offers only the current Claude Sonnet 5 exact Messages profile', () => {
    const options = reviewedModelOptions([profile({
      id: 'anthropic.claude-sonnet-5.api', version: '1.4.0', provider: 'anthropic', region: 'global',
      current: true,
      label: 'Claude Sonnet 5 Anthropic Messages Non-Stream Exact Function Tools',
      protocol_profile: 'unirouter.anthropic.messages.text-tools.v3', protocol_profile_version: '1.0.0',
      operation_policy_profile: 'anthropic.messages.sonnet-5-tools.v3',
      api_surface: 'anthropic_messages', operations: ['anthropic.messages.v1'],
      endpoint_classes: ['anthropic_official_origin'], aliases: ['claude-sonnet-5'],
      upstream_model_ids: ['claude-sonnet-5'], provider_context_window: 1_000_000,
      provider_max_output: 128_000, default_output_limit: 32_768,
      catalog_evidence_state: 'locally_conformant',
      provider_capabilities: ['reasoning', 'reasoning_effort', 'streaming', 'tools'],
      capabilities: [
        'reasoning', 'reasoning_effort', 'streaming', 'tools', 'function_tools',
        'tool_choice_auto', 'tool_choice_named', 'tool_choice_required', 'parallel_tool_calls',
      ],
    })], providerPreset('anthropic'));

    expect(options).toEqual([
      expect.objectContaining({
        key: 'anthropic.claude-sonnet-5.api@1.4.0',
        operationIDs: ['anthropic.messages.v1'],
        recommendedUpstreamModelID: 'claude-sonnet-5',
      }),
    ]);
  });

  it('offers the current Claude Opus 5 exact Messages profile', () => {
    const options = reviewedModelOptions([profile({
      id: 'anthropic.claude-opus-5.api', version: '1.2.0', provider: 'anthropic', region: 'global',
      current: true,
      label: 'Claude Opus 5 Anthropic Messages Strict Text Streaming',
      protocol_profile: 'unirouter.anthropic.messages.text.v2', protocol_profile_version: '1.0.0',
      operation_policy_profile: 'anthropic.messages.opus-5-evidence.v1',
      api_surface: 'anthropic_messages', operations: ['anthropic.messages.v1'],
      endpoint_classes: ['anthropic_official_origin'], aliases: ['claude-opus-5'],
      upstream_model_ids: ['claude-opus-5'], provider_context_window: 1_000_000,
      provider_max_output: 128_000, default_output_limit: 32_768,
      catalog_evidence_state: 'locally_conformant',
      provider_capabilities: ['reasoning', 'reasoning_effort', 'streaming'],
      capabilities: ['reasoning', 'reasoning_effort', 'streaming'],
    })], providerPreset('anthropic'));

    expect(options).toEqual([
      expect.objectContaining({
        key: 'anthropic.claude-opus-5.api@1.2.0',
        operationIDs: ['anthropic.messages.v1'],
        recommendedUpstreamModelID: 'claude-opus-5',
      }),
    ]);
  });

  it('offers the current pinned Claude Haiku 4.5 exact Messages profile', () => {
    const options = reviewedModelOptions([profile({
      id: 'anthropic.claude-haiku-4.5.api', version: '1.2.0', provider: 'anthropic', region: 'global',
      current: true,
      label: 'Claude Haiku 4.5 Anthropic Messages Strict Summarized-Thinking Streaming',
      protocol_profile: 'unirouter.anthropic.messages.text.v2', protocol_profile_version: '1.0.0',
      operation_policy_profile: 'anthropic.messages.haiku-4.5-evidence.v1',
      api_surface: 'anthropic_messages', operations: ['anthropic.messages.v1'],
      endpoint_classes: ['anthropic_official_origin'], aliases: ['claude-haiku-4-5-20251001'],
      upstream_model_ids: ['claude-haiku-4-5-20251001'], provider_context_window: 200_000,
      provider_max_output: 64_000, default_output_limit: 16_384,
      catalog_evidence_state: 'locally_conformant',
      provider_capabilities: ['reasoning', 'streaming'], capabilities: ['reasoning', 'reasoning_budget', 'streaming'],
    })], providerPreset('anthropic'));

    expect(options).toEqual([
      expect.objectContaining({
        key: 'anthropic.claude-haiku-4.5.api@1.2.0',
        operationIDs: ['anthropic.messages.v1'],
        recommendedPublicModelID: 'claude-haiku-4-5-20251001',
        recommendedUpstreamModelID: 'claude-haiku-4-5-20251001',
      }),
    ]);
  });

  it('offers only the exact current Doubao Seed 2.1 Pro Ark Chat inline JPEG profile', () => {
    const options = reviewedModelOptions([profile({
      id: 'doubao.seed-2.1-pro.api', version: '1.6.0', provider: 'doubao', region: 'cn-beijing',
      current: true, label: 'Doubao Seed 2.1 Pro Ark Chat Non-Stream Inline JPEG',
      protocol_profile: 'protocol.doubao-seed-2.1-pro-openai-chat.v5',
      reasoning_profile: 'reasoning.doubao-seed-2.1-pro-toggle-tools.v2', cache_usage_profile: 'cache-usage.doubao-ark-chat.v1',
      api_surface: 'openai_chat_completions', operations: ['openai.chat_completions.v1'],
      endpoint_classes: ['doubao_ark_cn_beijing_openai_v3'],
      aliases: ['Doubao Seed 2.1 Pro', 'doubao-seed-2-1-pro-260628'],
      upstream_model_ids: ['doubao-seed-2-1-pro-260628'], provider_context_window: 262_144,
      provider_max_output: 262_144, default_context_window: 262_144, default_output_limit: 4_096,
      default_output_authority: 'provider_documented', catalog_evidence_state: 'locally_conformant',
      provider_capabilities: ['reasoning', 'tools'],
      capabilities: ['reasoning', 'streaming', 'tools', 'function_tools', 'tool_choice_auto', 'tool_choice_named', 'tool_choice_required', 'parallel_tool_calls', 'strict_json_schema', 'prompt_cache', 'cache_read_usage', 'vision'],
    })], providerPreset('doubao'));

    expect(options).toEqual([
      expect.objectContaining({
        key: 'doubao.seed-2.1-pro.api@1.6.0',
        recommendedPublicModelID: 'doubao-seed-2-1-pro-260628',
        recommendedUpstreamModelID: 'doubao-seed-2-1-pro-260628',
        operationIDs: ['openai.chat_completions.v1'],
      }),
    ]);
    expect(reviewedModelOnboardingLimits(options[0].profile)).toMatchObject({
      providerMaxOutput: 262_144,
      publicOutputLimit: 262_144,
      deploymentDefaultMaxOutput: 4_096,
    });
  });

  it('offers only the exact current Doubao Seed 2.1 Turbo Ark Chat inline JPEG profile', () => {
    const options = reviewedModelOptions([profile({
      id: 'doubao.seed-2.1-turbo.api', version: '1.6.0', provider: 'doubao', region: 'cn-beijing',
      current: true, label: 'Doubao Seed 2.1 Turbo Ark Chat Non-Stream Inline JPEG',
      protocol_profile: 'protocol.doubao-seed-2.1-turbo-openai-chat.v5',
      reasoning_profile: 'reasoning.doubao-seed-2.1-turbo-effort-tools.v2', cache_usage_profile: 'cache-usage.doubao-ark-chat.v1',
      api_surface: 'openai_chat_completions', operations: ['openai.chat_completions.v1'],
      endpoint_classes: ['doubao_ark_cn_beijing_openai_v3'],
      aliases: ['Doubao Seed 2.1 Turbo', 'doubao-seed-2-1-turbo-260628'],
      upstream_model_ids: ['doubao-seed-2-1-turbo-260628'], provider_context_window: 262_144,
      provider_max_output: 262_144, default_context_window: 262_144, default_output_limit: 4_096,
      default_output_authority: 'provider_documented', catalog_evidence_state: 'locally_conformant',
      provider_capabilities: ['reasoning', 'tools'],
      capabilities: ['reasoning_effort', 'streaming', 'tools', 'function_tools', 'tool_choice_auto', 'tool_choice_named', 'tool_choice_required', 'parallel_tool_calls', 'strict_json_schema', 'prompt_cache', 'cache_read_usage', 'vision'],
    })], providerPreset('doubao'));

    expect(options).toEqual([
      expect.objectContaining({
        key: 'doubao.seed-2.1-turbo.api@1.6.0',
        recommendedPublicModelID: 'doubao-seed-2-1-turbo-260628',
        recommendedUpstreamModelID: 'doubao-seed-2-1-turbo-260628',
        operationIDs: ['openai.chat_completions.v1'],
      }),
    ]);
    expect(reviewedModelOnboardingLimits(options[0].profile)).toMatchObject({
      providerMaxOutput: 262_144,
      publicOutputLimit: 262_144,
      deploymentDefaultMaxOutput: 4_096,
    });
  });

  it('does not offer the non-current Doubao Seed Evolving floating-identity candidate', () => {
    const options = reviewedModelOptions([profile({
      id: 'doubao.seed-evolving.api', version: '1.1.0', provider: 'doubao', region: 'cn-beijing',
      current: false, label: 'Doubao Seed Evolving Ark Floating Alias Identity',
      protocol_profile: 'protocol.doubao-openai-chat.documented.v1',
      reasoning_profile: 'reasoning.doubao-documented.v1',
      cache_usage_profile: 'cache-usage.doubao-documented.v1',
      api_surface: 'openai_chat_completions', operations: ['openai.chat_completions.v1'],
      endpoint_classes: ['doubao_ark_cn_beijing_openai_v3'],
      aliases: ['Doubao Seed Evolving', 'doubao-seed-evolving'],
      upstream_model_ids: ['doubao-seed-evolving'],
      upstream_model_family: 'doubao-seed-evolving', upstream_version_policy: 'floating',
      provider_context_window: 1_048_576, provider_max_output: 262_144,
      default_context_window: 1_048_576, default_output_limit: 4_096,
      default_output_authority: 'provider_documented', catalog_evidence_state: 'documented',
      provider_capabilities: ['mutable_alias'], capabilities: [],
    })], providerPreset('doubao'));

    expect(options).toEqual([]);
  });

  it('offers only the current Doubao Seed Evolving implicit cache-read profile with floating identity', () => {
    const options = reviewedModelOptions([profile({
      id: 'doubao.seed-evolving.api', version: '1.8.0', provider: 'doubao', region: 'cn-beijing',
      current: true, label: 'Doubao Seed Evolving Ark Chat Implicit Cache Read Usage',
      protocol_profile: 'protocol.doubao-seed-evolving-openai-chat.v6',
      reasoning_profile: 'reasoning.doubao-seed-evolving-toggle-effort.v2',
      cache_usage_profile: 'cache-usage.doubao-ark-chat.v1',
      api_surface: 'openai_chat_completions', operations: ['openai.chat_completions.v1'],
      endpoint_classes: ['doubao_ark_cn_beijing_openai_v3'],
      aliases: ['Doubao Seed Evolving', 'doubao-seed-evolving'],
      upstream_model_ids: ['doubao-seed-evolving'],
      upstream_model_family: 'doubao-seed-evolving', upstream_version_policy: 'floating',
      provider_context_window: 1_048_576, provider_max_output: 262_144,
      default_context_window: 1_048_576, default_output_limit: 4_096,
      default_output_authority: 'provider_documented', catalog_evidence_state: 'locally_conformant',
      provider_capabilities: ['mutable_alias'], capabilities: [
        'streaming', 'reasoning', 'reasoning_effort', 'tools', 'function_tools',
        'tool_choice_auto', 'tool_choice_named', 'tool_choice_required', 'parallel_tool_calls',
        'strict_json_schema', 'vision', 'prompt_cache', 'cache_read_usage',
      ],
      always_required_capabilities: ['prompt_cache', 'cache_read_usage'],
    })], providerPreset('doubao'));

    expect(options).toEqual([
      expect.objectContaining({
        key: 'doubao.seed-evolving.api@1.8.0',
        recommendedPublicModelID: 'doubao-seed-evolving',
        recommendedUpstreamModelID: 'doubao-seed-evolving',
        operationIDs: ['openai.chat_completions.v1'],
      }),
    ]);
    expect(reviewedModelOnboardingLimits(options[0].profile)).toMatchObject({
      providerContextWindow: 1_048_576,
      providerMaxOutput: 262_144,
      publicOutputLimit: 262_144,
      deploymentDefaultMaxOutput: 4_096,
    });
  });

  it('fails closed instead of applying a Beijing Qwen profile to another region', () => {
    const beijing = providerPreset('dashscope');
    expect(reviewedModelOptions([profile({
      id: 'qwen.3.8-max.api',
      provider: 'dashscope',
      region: 'ap-southeast-1',
      aliases: ['qwen3.8-max'],
      upstream_model_ids: ['qwen3.8-max'],
    })], beijing)).toEqual([]);
  });

  it('requires the API surface, unique operation, adapter, and endpoint class to agree', () => {
    expect(reviewedModelOptions([profile({
      operations: ['anthropic.messages.v1'],
    })], providerPreset('deepseek'))).toEqual([]);
    expect(reviewedModelOptions([profile({
      operations: ['openai.chat_completions.v1', 'anthropic.messages.v1'],
    })], providerPreset('deepseek'))).toEqual([]);
    expect(reviewedModelOptions([profile({
      id: 'qwen.3.8-max.api', provider: 'dashscope', region: 'cn-beijing', aliases: ['qwen3.8-max'],
      endpoint_classes: ['dashscope_cn_beijing_shared_payg'],
      upstream_model_ids: ['qwen3.8-max'],
    })], providerPreset('dashscope'))).toEqual([]);
  });

  it('does not promote a display alias into an exact upstream model identity', () => {
    const options = reviewedModelOptions([profile({
      aliases: ['DeepSeek V4 Flash', 'human-display-alias'],
      upstream_model_ids: ['deepseek-v4-flash'],
    })], providerPreset('deepseek'));

    expect(options[0].upstreamModelIDs).toEqual(['deepseek-v4-flash']);
  });

  it('uses documented provider maxima for public limits but keeps the request output default separate', () => {
    expect(reviewedModelOnboardingLimits(profile({}))).toEqual({
      providerContextWindow: 1_000_000,
      providerMaxOutput: 384_000,
      executableContextWindow: 1_000_000,
      executableMaxOutput: 384_000,
      publicContextLength: 1_000_000,
      publicOutputLimit: 384_000,
      deploymentMaxContext: 1_000_000,
      deploymentDefaultMaxOutput: 32_768,
    });
  });

  it('keeps a documented provider context maximum distinct from the executable pricing ceiling', () => {
    expect(reviewedModelOnboardingLimits(profile({
      provider_context_window: 1_050_000,
      executable_context_window: 272_000,
      default_context_window: 272_000,
    }))).toMatchObject({
      providerContextWindow: 1_050_000,
      executableContextWindow: 272_000,
      publicContextLength: 272_000,
      deploymentMaxContext: 272_000,
    });
  });

  it('keeps a no-limit provider fact separate from a finite UniRouter executable output policy', () => {
    expect(reviewedModelOnboardingLimits(profile({
      provider_context_window: 262_144,
      provider_max_output: 0,
      executable_max_output: 131_072,
      default_context_window: 262_144,
      default_output_limit: 32_768,
    }))).toMatchObject({
      providerMaxOutput: 0,
      executableMaxOutput: 131_072,
      publicContextLength: 262_144,
      publicOutputLimit: 131_072,
      deploymentDefaultMaxOutput: 32_768,
    });
  });

  it.each([
    ['Kimi K3', 1_048_576, 1_048_576, 131_072],
    ['MiniMax M3', 1_000_000, 524_288, 131_072],
    ['MiniMax M2.7', 204_800, 204_800, 65_536],
  ])('keeps %s maximum and default output semantics distinct', (_label, context, maximum, defaultOutput) => {
    expect(reviewedModelOnboardingLimits(profile({
      provider_context_window: context,
      provider_max_output: maximum,
      default_context_window: context,
      default_output_limit: defaultOutput,
    }))).toMatchObject({
      publicContextLength: context,
      publicOutputLimit: maximum,
      deploymentMaxContext: context,
      deploymentDefaultMaxOutput: defaultOutput,
    });
  });
});
