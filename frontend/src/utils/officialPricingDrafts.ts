import type { RateComponent } from '../api/enterpriseFinance';
import { buildRequestRateComponentsForCurrency } from './rateCardPriceBands';

export type OfficialPricingCurrency = 'CNY' | 'USD';

type OfficialPricingSource = {
  sourceURL: string;
  sourceRevision: string;
  fetchedAt: string;
  staleAfter: string;
  payloadSHA256: `sha256:${string}`;
  transformVersion: 'official-onboarding-rate.v1';
};

export type OfficialPricingDraft = OfficialPricingSource & {
  state: 'available';
  provider: string;
  profileID: string;
  upstreamModelID: string;
  currency: OfficialPricingCurrency;
  pricingScope: string;
  effectiveTo?: string;
  inputPrice: string;
  outputPrice: string;
  cacheReadPrice: string;
  cacheCreationPrice: string;
  requiresPayGoConfirmation: boolean;
  reviewNote: string;
};

export type UnsupportedOfficialPricingDraft = {
  state: 'manual_review_required';
  provider: string;
  profileID: string;
  upstreamModelID: string;
  currency: OfficialPricingCurrency;
  sourceURL: string;
  sourceRevision: string;
  documentedPricingSummary: string;
  reason: string;
};

export type OfficialPricingDraftCandidate = OfficialPricingDraft | UnsupportedOfficialPricingDraft;

const fetchedAt = '2026-08-24T08:00:00.000000Z';
const staleAfter = '2026-08-31T08:00:00.000000Z';

function source(sourceURL: string, payloadSHA256: `sha256:${string}`): OfficialPricingSource {
  return {
    sourceURL,
    sourceRevision: 'retrieved-2026-08-24',
    fetchedAt,
    staleAfter,
    payloadSHA256,
    transformVersion: 'official-onboarding-rate.v1',
  };
}

const deepSeekSource = source(
  'https://api-docs.deepseek.com/zh-cn/quick_start/pricing/',
  'sha256:593f092cc8e91ad568f4843a83264e2f7aa2551b5c46d1e35b5e2654e3f06a02',
);
const miniMaxSource = source(
  'https://platform.minimaxi.com/docs/guides/pricing-paygo.md',
  'sha256:2579610dd1658aec9c2290cecb959de925505a4c73ddde893eefcaf7da287907',
);
const glmSource = source('https://bigmodel.cn/pricing', 'sha256:7a61179cab954549cddf140a34ec4eab327e2c9ec6a1b99f56df12ca31076b96');
const qwenSource = source('https://help.aliyun.com/zh/model-studio/qwen3-8-max', 'sha256:90355779df24c60b8e4c1d2f1d857ae999104ed5f6431e949dd7e55324023a29');
const geminiSource = source('https://ai.google.dev/gemini-api/docs/pricing', 'sha256:ba6cb7eb8f7e04c866b6dec1ebf75bc1726514cdb3b133dc508340d30b0d846a');
const mimoSource = source('https://mimo.mi.com/docs/en-US/price/pay-as-you-go', 'sha256:2e9d779a162942b1b8afb359e4242084564b65f1a02bfc914ee91b24aae398fe');
const hy3Source = source('https://cloud.tencent.com/document/product/1759/127342', 'sha256:218f37f9b0b16eacc754611803587a5871ac381ee317dec952fcd69a88c63e06');
const doubaoSource = source('https://www.volcengine.com/product/doubao', 'sha256:97df00960d4973c67198158f515fa86024d67442cbd055b4d470725f4f1320b2');

function flatDraft(
  provider: string,
  profileID: string,
  upstreamModelID: string,
  currency: OfficialPricingCurrency,
  prices: [string, string, string, string],
  pricingSource: OfficialPricingSource,
  reviewNote: string,
  options: { requiresConfirmation?: boolean; pricingScope?: string; effectiveTo?: string } = {},
): OfficialPricingDraft {
  return {
    state: 'available', provider, profileID, upstreamModelID, currency,
    pricingScope: options.pricingScope || '官方标准按量 Token 费率',
    ...pricingSource,
    effectiveTo: options.effectiveTo,
    inputPrice: prices[0], outputPrice: prices[1], cacheReadPrice: prices[2], cacheCreationPrice: prices[3],
    requiresPayGoConfirmation: Boolean(options.requiresConfirmation), reviewNote,
  };
}

function manualDraft(
  provider: string,
  profileID: string,
  upstreamModelID: string,
  currency: OfficialPricingCurrency,
  sourceURL: string,
  documentedPricingSummary: string,
  reason: string,
): UnsupportedOfficialPricingDraft {
  return {
    state: 'manual_review_required', provider, profileID, upstreamModelID, currency, sourceURL,
    sourceRevision: 'retrieved-2026-08-24', documentedPricingSummary, reason,
  };
}

const conservativeCacheNote = '官方未给出可直接映射到 cache_creation_tokens 的独立写入 Token 费率；草稿按未缓存输入价保守填入，激活前必须核对实际 usage 合同。';
const storageExcludedNote = '缓存存储按 MTok·小时另行计费，当前请求 Rate Card 没有存储时长 meter，草稿不包含该费用。';

const candidates: OfficialPricingDraftCandidate[] = [
  flatDraft('deepseek', 'deepseek.v4-flash.api', 'deepseek-v4-flash', 'CNY', ['1', '2', '0.02', '1'], deepSeekSource,
    `官方仅区分缓存命中/未命中输入；${conservativeCacheNote}`),
  flatDraft('deepseek', 'deepseek.v4-pro.api', 'deepseek-v4-pro', 'CNY', ['3', '6', '0.025', '3'], deepSeekSource,
    `官方仅区分缓存命中/未命中输入；${conservativeCacheNote}`),
  ...kimiTemplates(),
  flatDraft('minimax', 'minimax.m2.7.token-plan', 'MiniMax-M2.7', 'CNY', ['2.1', '8.4', '0.42', '2.625'], miniMaxSource,
    '仅适用于 MiniMax 普通按量 API Key；Token Plan/订阅 Key 不得套用该金额。', { requiresConfirmation: true }),
  flatDraft('minimax', 'minimax.m2.7.token-plan', 'MiniMax-M2.7-highspeed', 'CNY', ['4.2', '16.8', '0.42', '2.625'], miniMaxSource,
    '仅适用于 MiniMax 普通按量 API Key；Token Plan/订阅 Key 不得套用该金额。', { requiresConfirmation: true }),
  flatDraft('minimax', 'minimax.m2.5-highspeed.token-plan', 'MiniMax-M2.5-highspeed', 'CNY', ['4.2', '16.8', '0.21', '2.625'], miniMaxSource,
    '仅适用于 MiniMax 普通按量 API Key；Token Plan/订阅 Key 不得套用该金额。', { requiresConfirmation: true }),
  manualDraft('minimax', 'minimax.m3.token-plan', 'MiniMax-M3', 'CNY', miniMaxSource.sourceURL,
    '按输入是否超过 512K 且按 service_tier 分档。',
    '现有 Rate Card 只有 UTC 时段维度，不能无损表达上下文长度与 service_tier 分档，禁止自动生成错误平价草稿。'),

  flatDraft('glm', 'glm.5.3.api', 'glm-5.3', 'CNY', ['8', '28', '2', '8'], glmSource,
    `缓存存储当前限时免费；${conservativeCacheNote}`),
  flatDraft('glm', 'glm.5.2.api', 'glm-5.2', 'CNY', ['8', '28', '2', '8'], glmSource,
    `缓存存储当前限时免费；${conservativeCacheNote}`),
  manualDraft('glm', 'glm.5v-turbo.api', 'glm-5v-turbo', 'CNY', glmSource.sourceURL,
    '输入 <32K：¥5 / 输出 ¥22 / cacheRead ¥1.2；输入 ≥32K：¥7 / 输出 ¥26 / cacheRead ¥1.8，每百万 Token。',
    '价格按输入长度分档，当前 Rate Card 不能按请求上下文长度选择组件。'),

  flatDraft('dashscope', 'qwen.3.8-max.api', 'qwen3.8-max', 'CNY', ['12', '36', '1.5', '15'], qwenSource,
    'cacheRead 采用较高的隐式缓存命中价 ¥1.5；显式缓存命中为 ¥1，cacheCreate 为 ¥15。两类缓存语义仍需在 usage 合同核对后方可批准。', { requiresConfirmation: true }),
  manualDraft('dashscope', 'qwen.3.7-plus.api', 'qwen3.7-plus', 'CNY', 'https://help.aliyun.com/zh/model-studio/qwen3-7-plus',
    '≤256K：输入 ¥2 / 输出 ¥8 / 隐式缓存 ¥0.4 / 显式创建 ¥2.5 / 显式命中 ¥0.2；>256K：¥6 / ¥24 / ¥1.2 / ¥7.5 / ¥0.6，每百万 Token。',
    '价格按输入长度分档，且隐式与显式缓存费率不同，当前 Rate Card 不能无损表达。'),
  manualDraft('dashscope', 'qwen.3.7-flash.api', 'qwen3.7-flash', 'CNY', 'https://help.aliyun.com/zh/model-studio/qwen3-7-flash',
    '≤32K：输入 ¥0.2 / 输出 ¥0.8；32K–256K：¥0.6 / ¥2.4；256K–1M：¥1.2 / ¥4.8；各档另有隐式与显式缓存价格。',
    '存在三个上下文长度档和两套缓存语义，当前 Rate Card 不能无损表达。'),

  flatDraft('gemini', 'gemini.3.7-flash.api', 'gemini-3.7-flash', 'USD', ['0.75', '3.75', '0.075', '0.75'], geminiSource,
    `仅为 Standard tier 促销 Token 费率；${storageExcludedNote}搜索、Batch、Flex、Priority 另计。`,
    { requiresConfirmation: true, pricingScope: 'Gemini Developer API Standard 促销费率', effectiveTo: '2027-01-01T00:00:00.000000Z' }),
  flatDraft('gemini', 'gemini.3.5-flash.api', 'gemini-3.5-flash', 'USD', ['1.5', '9', '0.15', '1.5'], geminiSource,
    `仅为 Standard tier Token 费率；${storageExcludedNote}搜索、Batch、Priority 另计。`,
    { requiresConfirmation: true, pricingScope: 'Gemini Developer API Standard 费率' }),
  flatDraft('gemini', 'gemini.3.5-flash-lite.api', 'gemini-3.5-flash-lite', 'USD', ['0.3', '2.5', '0.03', '0.3'], geminiSource,
    `仅为 Standard tier Token 费率；${storageExcludedNote}搜索、Batch、Priority 另计。`,
    { requiresConfirmation: true, pricingScope: 'Gemini Developer API Standard 费率' }),

  flatDraft('mimo', 'mimo.v2.5-pro.api', 'mimo-v2.5-pro', 'CNY', ['3', '6', '0.025', '3'], mimoSource,
    `缓存写入当前限时免费；${conservativeCacheNote}内置搜索 ¥16/千次不包含在本 Token 草稿。`),
  flatDraft('mimo', 'mimo.v2.5.api', 'mimo-v2.5', 'CNY', ['1', '2', '0.02', '1'], mimoSource,
    `缓存写入当前限时免费；${conservativeCacheNote}内置搜索 ¥16/千次不包含在本 Token 草稿。`),
  flatDraft('hy3', 'hy3.api', 'hy3', 'CNY', ['1', '4', '0.25', '1'], hy3Source, conservativeCacheNote),

  ...openAIManualCandidates(),
  manualDraft('xai', 'xai.grok-4.6.api', 'grok-4.6', 'USD', 'https://docs.x.ai/developers/pricing',
    '<200K 输入：$2 / 输出 $6 / cached input $0.5；≥200K：$4 / $12 / $1，每百万 Token；工具调用 $5/千次。',
    '价格按输入长度分档且工具按次计费，当前 Rate Card 不能无损表达。'),
  ...anthropicManualCandidates(),

  flatDraft('doubao', 'doubao.seed-evolving.api', 'doubao-seed-evolving', 'CNY', ['6', '30', '1.2', '6'], doubaoSource,
    `${storageExcludedNote}该模型 ID 是持续升级 alias，建档时必须记录实际调用日期与响应模型证据。`, { requiresConfirmation: true }),
  flatDraft('doubao', 'doubao.seed-2.1-pro.api', 'doubao-seed-2-1-pro-260628', 'CNY', ['6', '30', '1.2', '6'], doubaoSource,
    storageExcludedNote, { requiresConfirmation: true }),
  flatDraft('doubao', 'doubao.seed-2.1-turbo.api', 'doubao-seed-2-1-turbo-260628', 'CNY', ['3', '15', '0.6', '3'], doubaoSource,
    storageExcludedNote, { requiresConfirmation: true }),
];

function kimiTemplates(): OfficialPricingDraft[] {
  const pricingSource = (path: string, payloadSHA256: `sha256:${string}`) => source(
    `https://platform.kimi.com/docs/pricing/${path}.md`, payloadSHA256,
  );
  const reviewNote = `官方只给出缓存命中/未命中输入；${conservativeCacheNote}内置搜索单价属于另一官方来源，本草稿不拼接复合来源。`;
  return [
    flatDraft('kimi', 'kimi.k2.6.api', 'kimi-k2.6', 'CNY', ['6.5', '27', '1.1', '6.5'],
      pricingSource('chat-k26', 'sha256:ccb36ead7c95e60d8d9cb6a81b4fc4e53b6f23bd83497e182e8be9e7b0ad66dd'), reviewNote),
    flatDraft('kimi', 'kimi.k2.6.api', 'kimi-k2.6-preview', 'CNY', ['6.5', '27', '1.1', '6.5'],
      pricingSource('chat-k26', 'sha256:ccb36ead7c95e60d8d9cb6a81b4fc4e53b6f23bd83497e182e8be9e7b0ad66dd'), reviewNote),
    flatDraft('kimi', 'kimi.k2.7-code.api', 'kimi-k2.7-code', 'CNY', ['6.5', '27', '1.3', '6.5'],
      pricingSource('chat-k27-code', 'sha256:e619a7b5710667aa3348611f421f43714585ef3a04b787b809a96f1aa9c73b29'), reviewNote),
    flatDraft('kimi', 'kimi.k2.7-code.api', 'kimi-k2.7-code-highspeed', 'CNY', ['13', '54', '2.6', '13'],
      pricingSource('chat-k27-code', 'sha256:e619a7b5710667aa3348611f421f43714585ef3a04b787b809a96f1aa9c73b29'), reviewNote),
    ...['kimi-k3', 'k3', 'k3-256k'].map(upstreamModelID => flatDraft(
      'kimi', 'kimi.k3.api', upstreamModelID, 'CNY', ['20', '100', '2', '20'],
      pricingSource('chat-k3', 'sha256:e0942aeaf39a8f14697044421a0246036926269a866b63616b3ca4beea014ad9'), reviewNote,
    )),
  ];
}

function openAIManualCandidates(): UnsupportedOfficialPricingDraft[] {
  const sourceURL = 'https://developers.openai.com/api/docs/models';
  const reason = 'Standard、Flex、Priority/Fast、Ultrafast 与长上下文分档会改变价格，当前 Rate Card 缺少 service_tier 和上下文长度选择维度。';
  return [
    manualDraft('openai', 'openai.gpt-5.6-sol.api', 'gpt-5.6-sol', 'USD', sourceURL,
      'Standard：输入 $4 / cached input $0.40 / 输出 $20，每百万 Token；cache write 为未缓存输入的 1.25 倍，>272K 另有长上下文倍率。', reason),
    manualDraft('openai', 'openai.gpt-5.6-terra.api', 'gpt-5.6-terra', 'USD', sourceURL,
      'Standard：输入 $2 / cached input $0.20 / 输出 $12，每百万 Token；另有 service tier 与长上下文价格。', reason),
    manualDraft('openai', 'openai.gpt-5.6-luna.api', 'gpt-5.6-luna', 'USD', sourceURL,
      'Standard：输入 $0.20 / cached input $0.02 / 输出 $1.20，每百万 Token；另有 service tier 价格。', reason),
  ];
}

function anthropicManualCandidates(): UnsupportedOfficialPricingDraft[] {
  const sourceURL = 'https://platform.claude.com/docs/en/about-claude/pricing';
  const reason = '5 分钟与 1 小时 cache write 是不同费率，当前单一 cache_creation_tokens meter 不能同时无损表达。';
  return [
    manualDraft('anthropic', 'anthropic.claude-opus-5.api', 'claude-opus-5', 'USD', sourceURL,
      '输入 $5 / 输出 $25 / cacheRead $0.50 / 5m write $6.25 / 1h write $10，每百万 Token。', reason),
    manualDraft('anthropic', 'anthropic.claude-sonnet-5.api', 'claude-sonnet-5', 'USD', sourceURL,
      '输入 $2 / 输出 $10 / cacheRead $0.20 / 5m write $2.50 / 1h write $4，每百万 Token。', reason),
    manualDraft('anthropic', 'anthropic.claude-haiku-4.5.api', 'claude-haiku-4-5-20251001', 'USD', sourceURL,
      '输入 $1 / 输出 $5 / cacheRead $0.10 / 5m write $1.25 / 1h write $2，每百万 Token。', reason),
  ];
}

export function officialPricingDraftFor(profileID: string, upstreamModelID: string): OfficialPricingDraftCandidate | undefined {
  return candidates.find(candidate => candidate.profileID === profileID && candidate.upstreamModelID === upstreamModelID);
}

export function officialPricingDraftComponents(template: OfficialPricingDraft): RateComponent[] {
  return buildRequestRateComponentsForCurrency({
    input_price_yuan: template.inputPrice,
    output_price_yuan: template.outputPrice,
    cache_read_price_yuan: template.cacheReadPrice,
    cache_creation_price_yuan: template.cacheCreationPrice,
  }, template.currency) || [];
}

export function officialPricingDraftRefs(template: OfficialPricingDraft, modelRef: string) {
  const identity = `${template.provider}-${modelRef}-${template.sourceRevision}-${template.transformVersion}`
    .toLowerCase()
    .replace(/[^a-z0-9._-]+/gu, '-')
    .replace(/^-+|-+$/gu, '')
    .slice(0, 110);
  const digest = template.payloadSHA256.slice('sha256:'.length, 'sha256:'.length + 12);
  return {
    rateCardRef: `onboarding-rate-${identity}-${digest}`,
    evidenceRef: `onboarding-source-${identity}-${digest}`,
  };
}
