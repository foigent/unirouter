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
  pricingRegion: string;
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
  pricingRegion: string;
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
  pricingRegion: string,
  prices: [string, string, string, string],
  pricingSource: OfficialPricingSource,
  reviewNote: string,
  options: { requiresConfirmation?: boolean; pricingScope?: string; effectiveTo?: string } = {},
): OfficialPricingDraft {
  return {
    state: 'available', provider, profileID, upstreamModelID, currency, pricingRegion,
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
  pricingRegion: string,
  sourceURL: string,
  documentedPricingSummary: string,
  reason: string,
): UnsupportedOfficialPricingDraft {
  return {
    state: 'manual_review_required', provider, profileID, upstreamModelID, currency, pricingRegion, sourceURL,
    sourceRevision: 'retrieved-2026-08-24', documentedPricingSummary, reason,
  };
}

const storageExcludedNote = '缓存存储按 MTok·小时另行计费，当前请求 Rate Card 没有存储时长 meter，草稿不包含该费用。';

const candidates: OfficialPricingDraftCandidate[] = [
  manualDraft('deepseek', 'deepseek.v4-flash.api', 'deepseek-v4-flash', 'CNY', 'cn', deepSeekSource.sourceURL,
    '未缓存输入 ¥1 / 缓存命中输入 ¥0.02 / 输出 ¥2，每百万 Token。',
    '官方未发布 cache_creation_tokens 独立写入费率；当前 Rate Card 强制要求该 meter，不能把未缓存输入价冒充缓存创建价。'),
  manualDraft('deepseek', 'deepseek.v4-pro.api', 'deepseek-v4-pro', 'CNY', 'cn', deepSeekSource.sourceURL,
    '未缓存输入 ¥3 / 缓存命中输入 ¥0.025 / 输出 ¥6，每百万 Token。',
    '官方未发布 cache_creation_tokens 独立写入费率；当前 Rate Card 强制要求该 meter，不能把未缓存输入价冒充缓存创建价。'),
  ...kimiTemplates(),
  manualDraft('minimax', 'minimax.m2.7.token-plan', 'MiniMax-M2.7', 'CNY', 'cn', miniMaxSource.sourceURL,
    '普通按量 Standard：输入 ¥2.10 / 输出 ¥8.40 / cacheRead ¥0.42 / cacheWrite ¥2.625，每百万 Token。',
    'Token Plan 与普通按量 Key 的结算身份不同；官方 OpenAI Chat usage 未提供可映射到 cache_creation_tokens 的创建计数，当前四 meter Rate Card 不能无损结算。'),
  manualDraft('minimax', 'minimax.m2.5-highspeed.token-plan', 'MiniMax-M2.5-highspeed', 'CNY', 'cn', miniMaxSource.sourceURL,
    '普通按量 Standard：输入 ¥4.20 / 输出 ¥16.80 / cacheRead ¥0.21 / cacheWrite ¥2.625，每百万 Token。',
    '该模型当前列于官方历史模型；Token Plan 与普通按量 Key 的结算身份不同，且 OpenAI Chat usage 缺少 cache_creation_tokens 创建计数，禁止自动生成平价草稿。'),
  manualDraft('minimax', 'minimax.m3.token-plan', 'MiniMax-M3', 'CNY', 'cn', miniMaxSource.sourceURL,
    '当前普通按量永久五折价（CNY/百万 Token）：Standard ≤512K 输入 ¥2.10 / 输出 ¥8.40 / cacheRead ¥0.42，>512K 为 ¥4.20 / ¥16.80 / ¥0.84；Priority ≤512K 为 ¥3.15 / ¥12.60 / ¥0.63，>512K 为 ¥6.30 / ¥25.20 / ¥1.26。长上下文档按总输入（含缓存命中）判定。',
    '当前 Rate Card selector 尚不能同时表达 normalized service_tier 与 ≤/>512K 输入分档，tier-aware CustomerChargeQuote 也尚未落地；该 Profile 还是 Token Plan 商业身份，而订阅 Key 与按量 API Key 不可互换。OpenAI Chat usage 只有 cacheRead、没有 cache_creation_tokens，且自动缓存写入无额外费用，不能据此生成一个覆盖各 tier/band 的平价草稿。'),

  manualDraft('glm', 'glm.5.3.api', 'glm-5.3', 'CNY', 'cn', glmSource.sourceURL,
    '输入 ¥8 / 输出 ¥28 / 缓存命中 ¥2，每百万 Token；缓存存储当前限时免费，单位为百万 Token/小时。',
    '当前 Rate Card 强制要求 cache_creation_tokens 价格，但官方只发布缓存存储 Token-hour 与缓存命中价，不能把存储费或未缓存输入价冒充缓存创建 Token 费。'),
  manualDraft('glm', 'glm.5.2.api', 'glm-5.2', 'CNY', 'cn', glmSource.sourceURL,
    '输入 ¥8 / 输出 ¥28 / 缓存命中 ¥2，每百万 Token；缓存存储当前限时免费，单位为百万 Token/小时。',
    '当前 Rate Card 强制要求 cache_creation_tokens 价格，但官方只发布缓存存储 Token-hour 与缓存命中价，不能把存储费或未缓存输入价冒充缓存创建 Token 费。'),
  manualDraft('glm', 'glm.5v-turbo.api', 'glm-5v-turbo', 'CNY', 'cn', glmSource.sourceURL,
    '输入 <32K：¥5 / 输出 ¥22 / cacheRead ¥1.2；输入 ≥32K：¥7 / 输出 ¥26 / cacheRead ¥1.8，每百万 Token。',
    '价格按输入长度分档，当前 Rate Card 不能按请求上下文长度选择组件。'),

  manualDraft('dashscope', 'qwen.3.8-max.api', 'qwen3.8-max', 'CNY', 'cn-beijing', qwenSource.sourceURL,
    '北京地域：输入 ¥12 / 输出 ¥36 / 隐式缓存命中 ¥1.5 / 显式缓存创建 ¥15 / 显式缓存命中 ¥1，每百万 Token。',
    '该候选仅适用于 cn-beijing；官方 usage 使用同一 cached_tokens 字段承载隐式或显式命中，现有 Rate Card 没有缓存模式维度，不能用单一 cacheRead 价格无损结算。'),
  manualDraft('dashscope', 'qwen.3.7-plus.api', 'qwen3.7-plus', 'CNY', 'cn-beijing', 'https://help.aliyun.com/zh/model-studio/qwen3-7-plus',
    '≤256K：输入 ¥2 / 输出 ¥8 / 隐式缓存 ¥0.4 / 显式创建 ¥2.5 / 显式命中 ¥0.2；>256K：¥6 / ¥24 / ¥1.2 / ¥7.5 / ¥0.6，每百万 Token。',
    '价格按输入长度分档，且隐式与显式缓存费率不同，当前 Rate Card 不能无损表达。'),
  manualDraft('dashscope', 'qwen.3.7-flash.api', 'qwen3.7-flash', 'CNY', 'cn-beijing', 'https://help.aliyun.com/zh/model-studio/qwen3-7-flash',
    '≤32K：输入 ¥0.2 / 输出 ¥0.8；32K–256K：¥0.6 / ¥2.4；256K–1M：¥1.2 / ¥4.8；各档另有隐式与显式缓存价格。',
    '存在三个上下文长度档和两套缓存语义，当前 Rate Card 不能无损表达。'),

  manualDraft('gemini', 'gemini.3.7-flash.api', 'gemini-3.7-flash', 'USD', 'global', geminiSource.sourceURL,
    'Standard 至 2026-12-31：输入 $0.75 / 输出 $3.75 / cacheRead $0.075；2027-01-01 起翻倍。另有 Batch、Flex、Priority；缓存存储按百万 Token/小时、搜索按次计费。',
    '当前 Rate Card 缺少 service_tier、生效期、Token-hour 存储和 provider-reported individual search query 计量维度，并强制要求 cache_creation_tokens；一次请求可能产生多次搜索查询，禁止把未缓存输入价冒充缓存创建价。'),
  manualDraft('gemini', 'gemini.3.5-flash.api', 'gemini-3.5-flash', 'USD', 'global', geminiSource.sourceURL,
    'Standard：输入 $1.50 / 输出 $9 / cacheRead $0.15；Batch：$0.75 / $4.50 / $0.075；Flex cacheRead $0.08；Priority：$2.70 / $16.20 / $0.27；存储 $1/百万 Token/小时。',
    '当前 Rate Card 缺少 service_tier、Token-hour 存储和 provider-reported individual search query 计量维度，并强制要求 cache_creation_tokens；一次请求可能产生多次搜索查询，不能用单一平价草稿无损结算。'),
  manualDraft('gemini', 'gemini.3.5-flash-lite.api', 'gemini-3.5-flash-lite', 'USD', 'global', geminiSource.sourceURL,
    'Standard：输入 $0.30 / 输出 $2.50 / cacheRead $0.03；Batch/Flex：$0.15 / $1.25 / $0.02；Priority：$0.54 / $4.50 / $0.05；存储 $1/百万 Token/小时。',
    '当前 Rate Card 缺少 service_tier、Token-hour 存储和 provider-reported individual search query 计量维度，并强制要求 cache_creation_tokens；一次请求可能产生多次搜索查询，不能用单一平价草稿无损结算。'),

  flatDraft('mimo', 'mimo.v2.5-pro.api', 'mimo-v2.5-pro', 'CNY', 'cn', ['3', '6', '0.025', '0'], mimoSource,
    '国内普通按量 API Key：缓存写入官方当前限时免费，因此 cache_creation_tokens 草稿为 ¥0；激活前需重新确认活动仍有效。内置搜索 ¥16/千次不包含在本 Token 草稿。',
    { requiresConfirmation: true }),
  flatDraft('mimo', 'mimo.v2.5.api', 'mimo-v2.5', 'CNY', 'cn', ['1', '2', '0.02', '0'], mimoSource,
    '国内普通按量 API Key：缓存写入官方当前限时免费，因此 cache_creation_tokens 草稿为 ¥0；激活前需重新确认活动仍有效。内置搜索 ¥16/千次不包含在本 Token 草稿。',
    { requiresConfirmation: true }),
  manualDraft('hy3', 'hy3.api', 'hy3', 'CNY', 'cn', hy3Source.sourceURL,
    '输入 ¥1 / 输出 ¥4 / 缓存命中 ¥0.25，每百万 Token；联网搜索轻量版与标准版另行按调用计费。',
    '官方未发布可映射到 cache_creation_tokens 的独立写入费率，且搜索价格是独立按次维度；当前 Token 草稿不能无损结算。'),

  ...openAIManualCandidates(),
  manualDraft('xai', 'xai.grok-4.6.api', 'grok-4.6', 'USD', 'global', 'https://docs.x.ai/developers/pricing',
    '<200K 输入：$2 / 输出 $6 / cached input $0.5；≥200K：$4 / $12 / $1，每百万 Token；另有 priority 与工具费用。供应商 usage 还返回精确 cost_in_usd_ticks。',
    '价格按输入长度、service tier 和工具调用分档，当前 Rate Card 不能无损表达；provider-reported cost_in_usd_ticks 尚未成为结算证据合同，禁止生成错误平价草稿。'),
  ...anthropicManualCandidates(),

  flatDraft('doubao', 'doubao.seed-evolving.api', 'doubao-seed-evolving', 'CNY', 'cn-beijing', ['6', '30', '1.2', '6'], doubaoSource,
    `${storageExcludedNote}该模型 ID 是持续升级 alias，建档时必须记录实际调用日期与响应模型证据。`, { requiresConfirmation: true }),
  flatDraft('doubao', 'doubao.seed-2.1-pro.api', 'doubao-seed-2-1-pro-260628', 'CNY', 'cn-beijing', ['6', '30', '1.2', '6'], doubaoSource,
    storageExcludedNote, { requiresConfirmation: true }),
  flatDraft('doubao', 'doubao.seed-2.1-turbo.api', 'doubao-seed-2-1-turbo-260628', 'CNY', 'cn-beijing', ['3', '15', '0.6', '3'], doubaoSource,
    storageExcludedNote, { requiresConfirmation: true }),
];

function kimiTemplates(): OfficialPricingDraftCandidate[] {
  const pricingURL = (path: string) => `https://platform.kimi.com/docs/pricing/${path}.md`;
  const missingCreationRate = '官方未发布 cache_creation_tokens 独立写入费率；当前 Rate Card 强制要求该 meter，不能用未缓存输入价代填。';
  return [
    ...[['kimi.k2.6.api', 'kimi-k2.6']].map(([profileID, upstreamModelID]) => manualDraft(
      'kimi', profileID, upstreamModelID, 'CNY', 'cn', pricingURL('chat-k26'),
      '未缓存输入 ¥6.5 / 缓存命中 ¥1.1 / 输出 ¥27，每百万 Token；内置搜索另行按次计费。', missingCreationRate,
    )),
    ...[['kimi-k2.7-code', '未缓存输入 ¥6.5 / 缓存命中 ¥1.3 / 输出 ¥27']].map(([upstreamModelID, summary]) => manualDraft(
      'kimi', 'kimi.k2.7-code.api', upstreamModelID, 'CNY', 'cn', pricingURL('chat-k27-code'),
      `${summary}，每百万 Token；内置搜索另行按次计费。`, missingCreationRate,
    )),
    ...['kimi-k3'].map(upstreamModelID => manualDraft(
      'kimi', 'kimi.k3.api', upstreamModelID, 'CNY', 'cn', pricingURL('chat-k3'),
      '未缓存输入 ¥20 / 缓存命中 ¥2 / 输出 ¥100，每百万 Token；内置搜索另行按次计费。', missingCreationRate,
    )),
  ];
}

function openAIManualCandidates(): UnsupportedOfficialPricingDraft[] {
  const solReason = 'Standard、Flex、Priority/Fast、Ultrafast 与长上下文分档会改变价格，当前 Rate Card 缺少 service_tier 和上下文长度选择维度。';
  const sharedReason = 'Standard、Flex、Priority/Fast 与长上下文分档会改变价格，当前 Rate Card 缺少 service_tier 和上下文长度选择维度。';
  return [
    manualDraft('openai', 'openai.gpt-5.6-sol.api', 'gpt-5.6-sol', 'USD', 'global', 'https://developers.openai.com/api/docs/models/gpt-5.6-sol',
      'Standard：输入 $4 / cached input $0.40 / 输出 $20，每百万 Token；cache write 为未缓存输入的 1.25 倍；>272K 时整个请求输入按 2 倍、输出按 1.5 倍计价。', solReason),
    manualDraft('openai', 'openai.gpt-5.6-sol.chat', 'gpt-5.6-sol', 'USD', 'global', 'https://developers.openai.com/api/docs/models/gpt-5.6-sol',
      'Standard：输入 $4 / cached input $0.40 / 输出 $20，每百万 Token；cache write 为未缓存输入的 1.25 倍；>272K 时整个请求输入按 2 倍、输出按 1.5 倍计价。', solReason),
    manualDraft('openai', 'openai.gpt-5.6-sol.responses', 'gpt-5.6-sol', 'USD', 'global', 'https://developers.openai.com/api/docs/models/gpt-5.6-sol',
      'Standard：输入 $4 / cached input $0.40 / 输出 $20，每百万 Token；cache write 为未缓存输入的 1.25 倍；>272K 时整个请求输入按 2 倍、输出按 1.5 倍计价；Responses 内置工具另行计费。', solReason),
    manualDraft('openai', 'openai.gpt-5.6-terra.api', 'gpt-5.6-terra', 'USD', 'global', 'https://developers.openai.com/api/docs/models/gpt-5.6-terra',
      'Standard：输入 $2 / cached input $0.20 / 输出 $12，每百万 Token；cache write 为未缓存输入的 1.25 倍；>272K 时整个请求输入按 2 倍、输出按 1.5 倍计价。', sharedReason),
    manualDraft('openai', 'openai.gpt-5.6-terra.chat', 'gpt-5.6-terra', 'USD', 'global', 'https://developers.openai.com/api/docs/models/gpt-5.6-terra',
      'Standard：输入 $2 / cached input $0.20 / 输出 $12，每百万 Token；cache write 为未缓存输入的 1.25 倍；>272K 时整个请求输入按 2 倍、输出按 1.5 倍计价。', sharedReason),
    manualDraft('openai', 'openai.gpt-5.6-terra.responses', 'gpt-5.6-terra', 'USD', 'global', 'https://developers.openai.com/api/docs/models/gpt-5.6-terra',
      'Standard：输入 $2 / cached input $0.20 / 输出 $12，每百万 Token；cache write 为未缓存输入的 1.25 倍；>272K 时整个请求输入按 2 倍、输出按 1.5 倍计价；Responses 内置工具另行计费。', sharedReason),
    manualDraft('openai', 'openai.gpt-5.6-luna.api', 'gpt-5.6-luna', 'USD', 'global', 'https://developers.openai.com/api/docs/models/gpt-5.6-luna',
      'Standard：输入 $0.20 / cached input $0.02 / 输出 $1.20，每百万 Token；cache write 为未缓存输入的 1.25 倍；>272K 时整个请求输入按 2 倍、输出按 1.5 倍计价。', sharedReason),
    manualDraft('openai', 'openai.gpt-5.6-luna.chat', 'gpt-5.6-luna', 'USD', 'global', 'https://developers.openai.com/api/docs/models/gpt-5.6-luna',
      'Standard：输入 $0.20 / cached input $0.02 / 输出 $1.20，每百万 Token；cache write 为未缓存输入的 1.25 倍；>272K 时整个请求输入按 2 倍、输出按 1.5 倍计价。', sharedReason),
    manualDraft('openai', 'openai.gpt-5.6-luna.responses', 'gpt-5.6-luna', 'USD', 'global', 'https://developers.openai.com/api/docs/models/gpt-5.6-luna',
      'Standard：输入 $0.20 / cached input $0.02 / 输出 $1.20，每百万 Token；cache write 为未缓存输入的 1.25 倍；>272K 时整个请求输入按 2 倍、输出按 1.5 倍计价；Responses 内置工具另行计费。', sharedReason),
  ];
}

function anthropicManualCandidates(): UnsupportedOfficialPricingDraft[] {
  const sourceURL = 'https://platform.claude.com/docs/en/about-claude/pricing';
  const reason = '5 分钟与 1 小时 cache write 是不同费率，当前单一 cache_creation_tokens meter 不能同时无损表达。';
  return [
    manualDraft('anthropic', 'anthropic.claude-opus-5.api', 'claude-opus-5', 'USD', 'global', sourceURL,
      '输入 $5 / 输出 $25 / cacheRead $0.50 / 5m write $6.25 / 1h write $10，每百万 Token。', reason),
    manualDraft('anthropic', 'anthropic.claude-sonnet-5.api', 'claude-sonnet-5', 'USD', 'global', sourceURL,
      '输入 $2 / 输出 $10 / cacheRead $0.20 / 5m write $2.50 / 1h write $4，每百万 Token。', reason),
    manualDraft('anthropic', 'anthropic.claude-haiku-4.5.api', 'claude-haiku-4-5-20251001', 'USD', 'global', sourceURL,
      '输入 $1 / 输出 $5 / cacheRead $0.10 / 5m write $1.25 / 1h write $2，每百万 Token。', reason),
  ];
}

export function officialPricingDraftFor(profileID: string, upstreamModelID: string, region: string): OfficialPricingDraftCandidate | undefined {
  return candidates.find(candidate => candidate.profileID === profileID && candidate.upstreamModelID === upstreamModelID
    && candidate.pricingRegion === region);
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
  const identity = `${template.provider}-${template.pricingRegion}-${modelRef}-${template.sourceRevision}-${template.transformVersion}`
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
