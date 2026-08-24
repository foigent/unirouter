import { describe, expect, it } from 'vitest';
import type { ModelExecutionProfile } from '../../api/modelExecutionProfiles';
import { providerPreset } from '../providerEndpoints';
import { compareProfileVersions, reviewedModelOptions } from '../modelOnboarding';

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

  it('compares semantic versions numerically', () => {
    expect(compareProfileVersions('1.10.0', '1.9.9')).toBeGreaterThan(0);
    expect(compareProfileVersions('2.0.0', '10.0.0')).toBeLessThan(0);
  });
});
