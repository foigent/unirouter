import { describe, expect, it } from 'vitest';
import {
  officialPricingDraftComponents,
  officialPricingDraftFor,
  officialPricingDraftRefs,
} from '../officialPricingDrafts';

describe('official onboarding pricing drafts', () => {
  it('builds a review-only DeepSeek V4 Flash four-meter draft from exact source evidence', () => {
    const candidate = officialPricingDraftFor('deepseek.v4-flash.api', 'deepseek-v4-flash');
    expect(candidate?.state).toBe('available');
    if (!candidate || candidate.state !== 'available') return;

    expect(officialPricingDraftComponents(candidate).map(component => [component.meter, component.amount.value_minor])).toEqual([
      ['input_tokens', '1000000'],
      ['output_tokens', '2000000'],
      ['cache_read_tokens', '20000'],
      ['cache_creation_tokens', '1000000'],
    ]);
    expect(officialPricingDraftRefs(candidate, 'deepseek-v4-flash')).toEqual({
      rateCardRef: 'onboarding-rate-deepseek-deepseek-v4-flash-retrieved-2026-08-24-official-onboarding-rate.v1-593f092cc8e9',
      evidenceRef: 'onboarding-source-deepseek-deepseek-v4-flash-retrieved-2026-08-24-official-onboarding-rate.v1-593f092cc8e9',
    });
  });

  it('keeps standard and highspeed model prices separate', () => {
    const standard = officialPricingDraftFor('kimi.k2.7-code.api', 'kimi-k2.7-code');
    const highspeed = officialPricingDraftFor('kimi.k2.7-code.api', 'kimi-k2.7-code-highspeed');
    expect(standard?.state === 'available' && standard.outputPrice).toBe('27');
    expect(highspeed?.state === 'available' && highspeed.outputPrice).toBe('54');
  });

  it('does not flatten MiniMax M3 tiered pay-as-you-go pricing', () => {
    expect(officialPricingDraftFor('minimax.m3.token-plan', 'MiniMax-M3')).toMatchObject({
      state: 'manual_review_required',
      reason: expect.stringContaining('不能无损表达'),
    });
  });

  it('preserves USD without converting Gemini official prices to CNY', () => {
    const candidate = officialPricingDraftFor('gemini.3.7-flash.api', 'gemini-3.7-flash');
    expect(candidate).toMatchObject({
      state: 'available',
      currency: 'USD',
      inputPrice: '0.75',
      outputPrice: '3.75',
      effectiveTo: '2027-01-01T00:00:00.000000Z',
    });
    if (!candidate || candidate.state !== 'available') return;
    expect(officialPricingDraftComponents(candidate).every(component => component.amount.currency === 'USD')).toBe(true);
  });

  it('documents but does not flatten context, tier, or cache-TTL price dimensions', () => {
    for (const [profileID, upstreamModelID] of [
      ['glm.5v-turbo.api', 'glm-5v-turbo'],
      ['qwen.3.7-flash.api', 'qwen3.7-flash'],
      ['openai.gpt-5.6-sol.api', 'gpt-5.6-sol'],
      ['xai.grok-4.6.api', 'grok-4.6'],
      ['anthropic.claude-opus-5.api', 'claude-opus-5'],
    ]) {
      const candidate = officialPricingDraftFor(profileID, upstreamModelID);
      expect(candidate?.state).toBe('manual_review_required');
      if (candidate?.state === 'manual_review_required') {
        expect(candidate.documentedPricingSummary).not.toBe('');
        expect(candidate.reason).not.toBe('');
      }
    }
  });

  it('has a dated pricing candidate for every frozen model in the 22-model catalog', () => {
    const models = [
      ['glm.5.3.api', 'glm-5.3'],
      ['glm.5.2.api', 'glm-5.2'],
      ['glm.5v-turbo.api', 'glm-5v-turbo'],
      ['qwen.3.8-max.api', 'qwen3.8-max'],
      ['qwen.3.7-plus.api', 'qwen3.7-plus'],
      ['qwen.3.7-flash.api', 'qwen3.7-flash'],
      ['gemini.3.7-flash.api', 'gemini-3.7-flash'],
      ['gemini.3.5-flash.api', 'gemini-3.5-flash'],
      ['gemini.3.5-flash-lite.api', 'gemini-3.5-flash-lite'],
      ['mimo.v2.5-pro.api', 'mimo-v2.5-pro'],
      ['mimo.v2.5.api', 'mimo-v2.5'],
      ['hy3.api', 'hy3'],
      ['openai.gpt-5.6-sol.api', 'gpt-5.6-sol'],
      ['openai.gpt-5.6-terra.api', 'gpt-5.6-terra'],
      ['openai.gpt-5.6-luna.api', 'gpt-5.6-luna'],
      ['xai.grok-4.6.api', 'grok-4.6'],
      ['anthropic.claude-opus-5.api', 'claude-opus-5'],
      ['anthropic.claude-sonnet-5.api', 'claude-sonnet-5'],
      ['anthropic.claude-haiku-4.5.api', 'claude-haiku-4-5-20251001'],
      ['doubao.seed-evolving.api', 'doubao-seed-evolving'],
      ['doubao.seed-2.1-pro.api', 'doubao-seed-2-1-pro-260628'],
      ['doubao.seed-2.1-turbo.api', 'doubao-seed-2-1-turbo-260628'],
    ];
    expect(models).toHaveLength(22);
    for (const [profileID, upstreamModelID] of models) {
      expect(officialPricingDraftFor(profileID, upstreamModelID), `${profileID}/${upstreamModelID}`).toBeDefined();
    }
  });
});
