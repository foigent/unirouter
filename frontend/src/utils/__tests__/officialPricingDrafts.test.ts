import { describe, expect, it } from 'vitest';
import {
  officialPricingDraftFor,
} from '../officialPricingDrafts';

describe('official onboarding pricing drafts', () => {
  it('does not invent a DeepSeek cache-creation price', () => {
    const candidate = officialPricingDraftFor('deepseek.v4-flash.api', 'deepseek-v4-flash', 'cn');
    expect(candidate).toMatchObject({
      state: 'manual_review_required',
      documentedPricingSummary: expect.stringContaining('缓存命中输入 ¥0.02'),
      reason: expect.stringContaining('不能把未缓存输入价冒充'),
    });
  });

  it('keeps the reviewed Kimi platform model identities separate from unselected variants', () => {
    const standard = officialPricingDraftFor('kimi.k2.7-code.api', 'kimi-k2.7-code', 'cn');
    const highspeed = officialPricingDraftFor('kimi.k2.7-code.api', 'kimi-k2.7-code-highspeed', 'cn');
    expect(standard).toMatchObject({ state: 'manual_review_required', documentedPricingSummary: expect.stringContaining('输出 ¥27') });
    expect(highspeed).toBeUndefined();
    expect(officialPricingDraftFor('kimi.k3.api', 'kimi-k3', 'cn')).toMatchObject({
      state: 'manual_review_required', documentedPricingSummary: expect.stringContaining('输出 ¥100'),
    });
    expect(officialPricingDraftFor('kimi.k3.api', 'k3', 'cn')).toBeUndefined();
  });

  it('does not flatten MiniMax M3 tiered pay-as-you-go pricing', () => {
    expect(officialPricingDraftFor('minimax.m3.token-plan', 'MiniMax-M3', 'cn')).toMatchObject({
      state: 'manual_review_required',
      currency: 'CNY',
      sourceRevision: 'retrieved-2026-08-24',
      documentedPricingSummary: expect.stringContaining('Standard ≤512K 输入 ¥2.10 / 输出 ¥8.40 / cacheRead ¥0.42'),
      reason: expect.stringContaining('Token Plan 商业身份'),
    });
    const candidate = officialPricingDraftFor('minimax.m3.token-plan', 'MiniMax-M3', 'cn');
    if (candidate?.state === 'manual_review_required') {
      expect(candidate.documentedPricingSummary).toContain('Priority ≤512K 为 ¥3.15 / ¥12.60 / ¥0.63');
      expect(candidate.documentedPricingSummary).toContain('>512K 为 ¥4.20 / ¥16.80 / ¥0.84');
      expect(candidate.documentedPricingSummary).toContain('>512K 为 ¥6.30 / ¥25.20 / ¥1.26');
      expect(candidate.documentedPricingSummary).toContain('总输入（含缓存命中）');
      expect(candidate.reason).toContain('normalized service_tier');
      expect(candidate.reason).toContain('tier-aware CustomerChargeQuote 也尚未落地');
      expect(candidate.reason).toContain('订阅 Key 与按量 API Key 不可互换');
      expect(candidate.reason).toContain('自动缓存写入无额外费用');
    }
  });

  it('does not flatten MiniMax M2 pricing when the OpenAI usage lacks cache creation authority', () => {
    for (const [profileID, upstreamModelID] of [
      ['minimax.m2.7.token-plan', 'MiniMax-M2.7'],
      ['minimax.m2.5-highspeed.token-plan', 'MiniMax-M2.5-highspeed'],
    ]) {
      expect(officialPricingDraftFor(profileID, upstreamModelID, 'cn')).toMatchObject({
        state: 'manual_review_required',
        reason: expect.stringContaining('cache_creation_tokens'),
      });
    }
    expect(officialPricingDraftFor('minimax.m2.7.token-plan', 'MiniMax-M2.7-highspeed', 'cn')).toBeUndefined();
  });

  it('does not map GLM cache storage into cache creation tokens', () => {
    for (const [profileID, model] of [['glm.5.3.api', 'glm-5.3'], ['glm.5.2.api', 'glm-5.2']]) {
      expect(officialPricingDraftFor(profileID, model, 'cn')).toMatchObject({
        state: 'manual_review_required',
        documentedPricingSummary: expect.stringContaining('缓存命中 ¥2'),
        reason: expect.stringContaining('不能把存储费'),
      });
    }
  });

  it('keeps MiMo domestic RMB prices and models the current free cache write as zero', () => {
    for (const [profileID, upstreamModelID, input, output, cacheRead] of [
      ['mimo.v2.5-pro.api', 'mimo-v2.5-pro', '3', '6', '0.025'],
      ['mimo.v2.5.api', 'mimo-v2.5', '1', '2', '0.02'],
    ]) {
      const candidate = officialPricingDraftFor(profileID, upstreamModelID, 'cn');
      expect(candidate).toMatchObject({
        state: 'available', currency: 'CNY', inputPrice: input, outputPrice: output,
        cacheReadPrice: cacheRead, cacheCreationPrice: '0', requiresPayGoConfirmation: true,
      });
    }
  });

  it('keeps Hy3 token and search prices documented without inventing cache creation billing', () => {
    expect(officialPricingDraftFor('hy3.api', 'hy3', 'cn')).toMatchObject({
      state: 'manual_review_required', currency: 'CNY',
      documentedPricingSummary: expect.stringContaining('输入 ¥1 / 输出 ¥4 / 缓存命中 ¥0.25'),
      reason: expect.stringContaining('不能无损结算'),
    });
  });

  it('preserves Gemini USD evidence without flattening tiers, dates, or token-hour storage', () => {
    for (const [profileID, upstreamModelID] of [
      ['gemini.3.7-flash.api', 'gemini-3.7-flash'],
      ['gemini.3.5-flash.api', 'gemini-3.5-flash'],
      ['gemini.3.5-flash-lite.api', 'gemini-3.5-flash-lite'],
    ]) {
      expect(officialPricingDraftFor(profileID, upstreamModelID, 'global')).toMatchObject({
        state: 'manual_review_required',
        currency: 'USD',
        documentedPricingSummary: expect.stringContaining('cacheRead'),
        reason: expect.stringContaining('Token-hour'),
      });
      const candidate = officialPricingDraftFor(profileID, upstreamModelID, 'global');
      if (candidate?.state === 'manual_review_required') {
        expect(candidate.reason).toContain('provider-reported individual search query');
        expect(candidate.reason).toContain('一次请求可能产生多次搜索查询');
      }
    }
  });

  it('documents but does not flatten context, tier, or cache-TTL price dimensions', () => {
    for (const [profileID, upstreamModelID, region] of [
      ['glm.5v-turbo.api', 'glm-5v-turbo', 'cn'],
      ['qwen.3.7-flash.api', 'qwen3.7-flash', 'cn-beijing'],
      ['openai.gpt-5.6-sol.chat', 'gpt-5.6-sol', 'global'],
      ['openai.gpt-5.6-terra.chat', 'gpt-5.6-terra', 'global'],
      ['openai.gpt-5.6-luna.chat', 'gpt-5.6-luna', 'global'],
      ['xai.grok-4.6.api', 'grok-4.6', 'global'],
      ['anthropic.claude-opus-5.api', 'claude-opus-5', 'global'],
    ]) {
      const candidate = officialPricingDraftFor(profileID, upstreamModelID, region);
      expect(candidate?.state).toBe('manual_review_required');
      if (candidate?.state === 'manual_review_required') {
        expect(candidate.documentedPricingSummary).not.toBe('');
        expect(candidate.reason).not.toBe('');
      }
    }
  });

  it('keeps xAI pricing manual until exact provider cost ticks and tiered rates are modeled', () => {
    expect(officialPricingDraftFor('xai.grok-4.6.api', 'grok-4.6', 'global')).toMatchObject({
      state: 'manual_review_required',
      currency: 'USD',
      documentedPricingSummary: expect.stringContaining('cost_in_usd_ticks'),
      reason: expect.stringContaining('service tier'),
    });
  });

  it('binds Qwen pricing evidence to the Beijing profile region', () => {
    expect(officialPricingDraftFor('qwen.3.8-max.api', 'qwen3.8-max', 'cn-beijing')).toMatchObject({
      state: 'manual_review_required', pricingRegion: 'cn-beijing', currency: 'CNY',
    });
    expect(officialPricingDraftFor('qwen.3.8-max.api', 'qwen3.8-max', 'ap-southeast-1')).toBeUndefined();
  });

  it('has a dated pricing candidate for every frozen model in the 22-model catalog', () => {
    const models = [
      ['glm.5.3.api', 'glm-5.3', 'cn'],
      ['glm.5.2.api', 'glm-5.2', 'cn'],
      ['glm.5v-turbo.api', 'glm-5v-turbo', 'cn'],
      ['qwen.3.8-max.api', 'qwen3.8-max', 'cn-beijing'],
      ['qwen.3.7-plus.api', 'qwen3.7-plus', 'cn-beijing'],
      ['qwen.3.7-flash.api', 'qwen3.7-flash', 'cn-beijing'],
      ['gemini.3.7-flash.api', 'gemini-3.7-flash', 'global'],
      ['gemini.3.5-flash.api', 'gemini-3.5-flash', 'global'],
      ['gemini.3.5-flash-lite.api', 'gemini-3.5-flash-lite', 'global'],
      ['mimo.v2.5-pro.api', 'mimo-v2.5-pro', 'cn'],
      ['mimo.v2.5.api', 'mimo-v2.5', 'cn'],
      ['hy3.api', 'hy3', 'cn'],
      ['openai.gpt-5.6-sol.chat', 'gpt-5.6-sol', 'global'],
      ['openai.gpt-5.6-terra.chat', 'gpt-5.6-terra', 'global'],
      ['openai.gpt-5.6-luna.chat', 'gpt-5.6-luna', 'global'],
      ['xai.grok-4.6.api', 'grok-4.6', 'global'],
      ['anthropic.claude-opus-5.api', 'claude-opus-5', 'global'],
      ['anthropic.claude-sonnet-5.api', 'claude-sonnet-5', 'global'],
      ['anthropic.claude-haiku-4.5.api', 'claude-haiku-4-5-20251001', 'global'],
      ['doubao.seed-evolving.api', 'doubao-seed-evolving', 'cn-beijing'],
      ['doubao.seed-2.1-pro.api', 'doubao-seed-2-1-pro-260628', 'cn-beijing'],
      ['doubao.seed-2.1-turbo.api', 'doubao-seed-2-1-turbo-260628', 'cn-beijing'],
    ];
    expect(models).toHaveLength(22);
    for (const [profileID, upstreamModelID, region] of models) {
      expect(officialPricingDraftFor(profileID, upstreamModelID, region), `${profileID}/${upstreamModelID}/${region}`).toBeDefined();
    }
    expect(officialPricingDraftFor('openai.gpt-5.6-sol.responses', 'gpt-5.6-sol', 'global')).toMatchObject({
      state: 'manual_review_required', currency: 'USD',
      documentedPricingSummary: expect.stringContaining('Responses 内置工具另行计费'),
    });
    expect(officialPricingDraftFor('openai.gpt-5.6-terra.responses', 'gpt-5.6-terra', 'global')).toMatchObject({
      state: 'manual_review_required', currency: 'USD',
      documentedPricingSummary: expect.stringContaining('Responses 内置工具另行计费'),
    });
    expect(officialPricingDraftFor('openai.gpt-5.6-luna.responses', 'gpt-5.6-luna', 'global')).toMatchObject({
      state: 'manual_review_required', currency: 'USD',
      documentedPricingSummary: expect.stringContaining('Responses 内置工具另行计费'),
    });
  });
});
