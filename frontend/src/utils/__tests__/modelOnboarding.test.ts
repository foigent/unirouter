import { describe, expect, it } from 'vitest';
import type { ModelExecutionProfile } from '../../api/modelExecutionProfiles';
import { providerPreset } from '../providerEndpoints';
import { compareProfileVersions, reviewedModelOnboardingLimits, reviewedModelOptions } from '../modelOnboarding';

function profile(overrides: Partial<ModelExecutionProfile>): ModelExecutionProfile {
  return {
    id: 'deepseek.v4-flash.api',
    version: '1.0.0',
    label: 'DeepSeek V4 Flash 官方 API',
    tokenizer_profile_id: 'tokenizer.byte-upper.v1',
    tokenizer_profile_version: '1.0.0',
    reasoning_profile: 'reasoning.deepseek.v1',
    cache_usage_profile: 'cache.deepseek.v1',
    protocol_profile: 'protocol.deepseek-openai-chat.v1',
    provider_context_window: 1_000_000,
    provider_max_output: 384_000,
    default_context_window: 1_000_000,
    default_output_limit: 32_768,
    provider_capabilities: ['reasoning'],
    capabilities: ['reasoning'],
    aliases: ['DeepSeek V4 Flash', 'deepseek-v4-flash'],
    evidence_refs: ['https://example.test/evidence'],
    ...overrides,
  };
}

describe('model onboarding profile composition', () => {
  it('keeps only the latest exact profile version for the selected official provider', () => {
    const options = reviewedModelOptions([
      profile({ version: '1.0.0' }),
      profile({ version: '1.10.0' }),
      profile({ id: 'kimi.k3.api', version: '9.0.0', aliases: ['kimi-k3'] }),
    ], providerPreset('deepseek'));

    expect(options).toHaveLength(1);
    expect(options[0]).toMatchObject({
      key: 'deepseek.v4-flash.api@1.10.0',
      recommendedPublicModelID: 'deepseek-v4-flash',
      recommendedUpstreamModelID: 'deepseek-v4-flash',
      operationIDs: ['openai.chat_completions.v1'],
    });
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
      aliases: ['MiniMax M3', 'MiniMax-M3'],
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

  it('maps GPT-5.6 reviewed profiles to the canonical Responses operation', () => {
    const options = reviewedModelOptions([profile({
      id: 'openai.gpt-5.6-sol.api',
      label: 'GPT-5.6 Sol 官方 API',
      protocol_profile: 'protocol.openai-responses.documented.v1',
      aliases: ['GPT-5.6 Sol', 'gpt-5.6-sol'],
    })], providerPreset('openai'));

    expect(options[0]).toMatchObject({
      recommendedPublicModelID: 'gpt-5.6-sol',
      recommendedUpstreamModelID: 'gpt-5.6-sol',
      operationIDs: ['openai.responses.v1'],
    });
  });

  it('uses documented provider maxima for public limits but keeps the request output default separate', () => {
    expect(reviewedModelOnboardingLimits(profile({}))).toEqual({
      providerContextWindow: 1_000_000,
      providerMaxOutput: 384_000,
      publicContextLength: 1_000_000,
      publicOutputLimit: 384_000,
      deploymentMaxContext: 1_000_000,
      deploymentDefaultMaxOutput: 32_768,
    });
  });

  it('falls back to the reviewed request default when the provider does not document an output maximum', () => {
    expect(reviewedModelOnboardingLimits(profile({
      provider_context_window: 262_144,
      provider_max_output: 0,
      default_context_window: 262_144,
      default_output_limit: 32_768,
    }))).toMatchObject({
      providerMaxOutput: 0,
      publicContextLength: 262_144,
      publicOutputLimit: 32_768,
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
