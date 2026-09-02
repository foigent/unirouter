export type ProviderAdapterType = 'openai_compatible' | 'anthropic_native' | 'azure_openai';

export type ProviderPreset = {
  value: string;
  label: string;
  provider: string;
  baseURL: string;
  adapterType: ProviderAdapterType;
  adapterConfig?: Record<string, unknown>;
  note: string;
  region?: string;
  apiSurface?: 'openai_chat_completions' | 'openai_responses' | 'anthropic_messages';
  endpointClass?: 'provider_https' | 'openai_official_origin' | 'openai_official_v1' | 'gemini_openai_compatibility_v1beta' | 'deepseek_official_openai' | 'kimi_platform_openai_v1' | 'minimax_cn_openai_v1' | 'minimax_official_cn_openai_v1' | 'glm_official_openai_v4' | 'xai_official_openai_v1' | 'anthropic_official_origin' | 'doubao_ark_cn_beijing_openai_v3' | 'dashscope_cn_beijing_workspace_payg' | 'dashscope_cn_beijing_shared_payg';
  editableBaseURL?: boolean;
};

// Presets are shortcuts, not an allowlist. Keep custom first so a provider
// missing from the catalog is still an obvious, supported path.
export const providerPresets: ProviderPreset[] = [
  {
    value: 'custom',
    label: '自定义 OpenAI 兼容供应商',
    provider: 'custom',
    baseURL: '',
    adapterType: 'openai_compatible',
    note: '适用于 MiniMax、MiMo 和其他已确认兼容 Chat Completions 的 MaaS 或聚合平台。',
  },
  {
    value: 'deepseek', label: 'DeepSeek 官方', provider: 'deepseek', region: 'cn',
    apiSurface: 'openai_chat_completions', baseURL: 'https://api.deepseek.com', adapterType: 'openai_compatible',
    endpointClass: 'deepseek_official_openai',
    adapterConfig: { endpoint_path_mode: 'base_relative' },
    note: 'OpenAI 兼容协议；可从版本化执行 Profile 选择已审查模型，创建后仍需精确 Operation 探测。',
  },
  {
    value: 'kimi', label: 'Kimi / Moonshot 官方', provider: 'kimi', region: 'cn',
    apiSurface: 'openai_chat_completions', baseURL: 'https://api.moonshot.cn/v1', adapterType: 'openai_compatible',
    endpointClass: 'kimi_platform_openai_v1',
    adapterConfig: { endpoint_path_mode: 'canonical_v1' },
    note: 'Kimi 开放平台按量 API，面向企业产品集成；不混用 api.kimi.com/coding 的会员订阅权益与模型 ID。Kimi K2.6 与 K2.7 Code 当前开放 exact 非流式 function tools 子集：tool_choice 支持省略/none/auto，省略时按供应商默认 auto；required、named、parallel、显式 null 和工具流式增量仍 fail-closed。Kimi K3 当前开放 exact 非流式 function tools：顶层 tools 可与 messages 中不携带 content 的 system Dynamic Tools 声明并存，支持省略/none/auto/required，省略时按供应商默认 auto；所有可见函数名冻结在同一 Plan 快照，重复名 fail-closed。K3 始终开启思考，因此 named choice 与工具流式增量仍 fail-closed。K2.7 Code 始终开启思考并按 keep=all 保留 reasoning_content；K2.6 可显式关闭思考。普通文本 streaming 不受影响。',
  },
  {
    value: 'minimax', label: 'MiniMax 官方（中国区）', provider: 'minimax', region: 'cn',
    apiSurface: 'openai_chat_completions', baseURL: 'https://api.minimax.cn/v1', adapterType: 'openai_compatible',
    endpointClass: 'minimax_official_cn_openai_v1',
    adapterConfig: { endpoint_path_mode: 'canonical_v1' },
    note: '当前模板只用于 MiniMax PAYG 普通按量 Key 的 OpenAI-compatible Chat 接入。Token Plan Key 与 PAYG Key 不可互换，Token Plan 继续作为独立后续接入路径。',
  },
  {
    value: 'glm', label: '智谱 GLM 官方', provider: 'glm', region: 'cn',
    apiSurface: 'openai_chat_completions', baseURL: 'https://open.bigmodel.cn/api/paas/v4', adapterType: 'openai_compatible',
    endpointClass: 'glm_official_openai_v4',
    adapterConfig: { endpoint_path_mode: 'base_relative' },
    note: '使用智谱 OpenAI Chat Completions 兼容入口；目录价按中国区人民币来源建草稿。',
  },
  {
    value: 'mimo', label: '小米 MiMo 官方', provider: 'mimo', region: 'cn',
    apiSurface: 'openai_chat_completions', baseURL: 'https://api.xiaomimimo.com/v1', adapterType: 'openai_compatible',
    note: '使用 MiMo 官方 OpenAI Chat Completions 兼容入口；订阅计划如给出专用地址，以控制台为准。',
  },
  {
    value: 'hy3', label: '腾讯混元 Hy3 官方', provider: 'hy3', region: 'cn',
    apiSurface: 'openai_chat_completions', baseURL: 'https://tokenhub.tencentmaas.com/v1', adapterType: 'openai_compatible',
    note: '使用腾讯混元 Token Hub 的 OpenAI 兼容入口；目录价按人民币来源建草稿。',
  },
  {
    value: 'openai', label: 'OpenAI 官方（Chat Completions）', provider: 'openai', region: 'global',
    apiSurface: 'openai_chat_completions', baseURL: 'https://api.openai.com/v1', adapterType: 'openai_compatible',
    endpointClass: 'openai_official_v1',
    adapterConfig: { endpoint_path_mode: 'canonical_v1' },
    note: '只选择 surface-specific Chat Profile。GPT-5.6 Sol/Terra/Luna 当前是 locally_conformant core Chat 子集；service tier 与 272K 以上长上下文仍 fail-closed。',
  },
  {
    value: 'openai-responses', label: 'OpenAI 官方（Responses）', provider: 'openai', region: 'global',
    apiSurface: 'openai_responses', baseURL: 'https://api.openai.com', adapterType: 'openai_compatible',
    endpointClass: 'openai_official_origin',
    adapterConfig: { endpoint_path_mode: 'canonical_v1' },
    note: '只选择 surface-specific Responses Profile。GPT-5.6 Sol/Terra/Luna 当前均是 strict core text + exact text SSE，并有 profile-bound reasoning.effort。function tools 与结构化输出（含非流式）仍未开放；内置工具仅 documentation_required，不影响独立 Chat Profile 的 function tools。reasoning summary/多轮加密状态回放、service tier、计费、live verified 与 released 均未据此宣称完成。',
  },
  {
    value: 'gemini', label: 'Google Gemini 官方', provider: 'gemini', region: 'global',
    apiSurface: 'openai_chat_completions', baseURL: 'https://generativelanguage.googleapis.com/v1beta/openai', adapterType: 'openai_compatible',
    endpointClass: 'gemini_openai_compatibility_v1beta',
    adapterConfig: { endpoint_path_mode: 'base_relative' },
    note: '当前提供 Gemini OpenAI-compatible core chat subset；Gemini 3.7 Flash、3.5 Flash 与 3.5 Flash-Lite 均已建模 default/flex/priority 请求级 tier，显式 flex/priority 只允许 cost_observation。原生缓存、搜索、视频/PDF 继续 fail-closed。',
  },
  {
    value: 'xai', label: 'xAI 官方', provider: 'xai', region: 'global',
    apiSurface: 'openai_chat_completions', baseURL: 'https://api.x.ai/v1', adapterType: 'openai_compatible',
    endpointClass: 'xai_official_openai_v1',
    adapterConfig: { endpoint_path_mode: 'canonical_v1' },
    note: '当前模板只开放 xAI OpenAI-compatible core Chat 子集；供应商文档对 Grok 4.6 标注无文本输出上限，UniRouter 另设有界请求策略。长上下文、priority 与工具费用不被平价草稿折叠。',
  },
  {
    value: 'doubao', label: '火山方舟 Doubao 官方（北京）', provider: 'doubao', region: 'cn-beijing',
    apiSurface: 'openai_chat_completions', baseURL: 'https://ark.cn-beijing.volces.com/api/v3', adapterType: 'openai_compatible',
    endpointClass: 'doubao_ark_cn_beijing_openai_v3',
    adapterConfig: { endpoint_path_mode: 'base_relative' },
    note: '使用火山方舟北京地域 OpenAI Chat Completions 兼容入口。Seed 2.1 Pro 当前开放 exact text/reasoning streaming core、非流式 function tools/tool_choice、strict=true 的非流式 json_schema，以及非流式 user image_url 内联 JPEG 安全子集（1–8 张、单张 decoded ≤256 KiB、≤131,072 pixels）；工具调用支持 none/auto/required/named、并行默认开启，并在默认思考开启时要求多轮历史原样保留 encrypted_content。Pro 图像与 stream、tools/tool history、结构化输出、penalties、n 不组合；json_object、非严格 schema、结构化流式输出、工具 strict schema 与工具流式增量仍 fail-closed。Seed 2.1 Turbo 当前开放 exact text/reasoning streaming core、非流式 function tools/tool_choice、strict=true 的非流式 json_schema，以及相同的非流式 user image_url 内联 JPEG 安全子集；支持 minimal/low/medium/high 且默认 high，minimal 关闭思考，工具支持 none/auto/required/named、并行默认开启，并在思考生效的工具历史中要求原样保留 encrypted_content。Turbo 图像与 stream、tools/tool history、结构化输出、penalties、n 不组合；json_object、非严格 schema、结构化流式输出、工具 strict schema 与工具流式增量仍 fail-closed。Seed Evolving 当前开放 floating alias 的 exact text/reasoning streaming core：thinking 支持 enabled/disabled 且默认 enabled；reasoning_effort 支持 none/minimal/low/medium/high/xhigh/max 且默认 high，none/minimal 关闭思考，xhigh/max 按供应商兼容语义生效为 high。Pro、Turbo 与 Evolving 的流式请求会确保 include_usage=true；响应 reasoning_content 进入内容保护，encrypted_content 仅作不透明证据，并要求独立 terminal usage 与 [DONE]。Seed Evolving 另已开放 exact non-stream function tools/tool_choice、strict=true 的非流式 json_schema，以及相同的非流式 user image_url 内联 JPEG 安全子集；图像与 tools、结构化输出、penalties、n 不组合。Pro、Turbo 与 Seed Evolving 的隐式缓存按 Profile 始终生效，非流式最终响应和流式 terminal usage 均必须给出 usage.prompt_tokens_details.cached_tokens（零值同样是权威证据），缺失或冲突会 fail-closed。json_object、非严格 schema、结构化流式输出与未建模 refusal 继续 fail-closed，工具流式增量仍 fail-closed。默认 output 4,096，max_tokens 上限 262,144，max_completion_tokens 上限 65,536。远程图片、其他媒体、供应商 10 MB 图像上限全覆盖、显式缓存创建与存储计费、搜索及其计费、chunk_include_usage 及 service tier 继续 fail-closed。',
  },
  {
    value: 'openrouter', label: 'OpenRouter', provider: 'openrouter',
    baseURL: 'https://openrouter.ai/api/v1', adapterType: 'openai_compatible',
    note: '同一公共模型可与官方端点并存，供应商模型 ID 必须分别填写。',
  },
  {
    value: 'dashscope', label: '阿里云百炼（北京业务空间）', provider: 'dashscope', region: 'cn-beijing',
    apiSurface: 'openai_chat_completions', baseURL: '', adapterType: 'openai_compatible', editableBaseURL: true,
    endpointClass: 'dashscope_cn_beijing_workspace_payg',
    adapterConfig: { endpoint_path_mode: 'canonical_v1' },
    note: '推荐生产入口。请从北京地域业务空间复制完整 API Host（https://{WorkspaceId}.cn-beijing.maas.aliyuncs.com/compatible-mode/v1）；地域与 API Key 必须匹配。',
  },
  {
    value: 'dashscope-shared', label: '阿里云百炼（北京共享兼容域名）', provider: 'dashscope', region: 'cn-beijing',
    apiSurface: 'openai_chat_completions', baseURL: 'https://dashscope.aliyuncs.com/compatible-mode/v1', adapterType: 'openai_compatible',
    endpointClass: 'dashscope_cn_beijing_shared_payg',
    adapterConfig: { endpoint_path_mode: 'canonical_v1' },
    note: '仅用于仍依赖共享兼容域名的存量接入；官方推荐生产迁移到业务空间专属 Host。价格与能力仍锁定北京地域。',
  },
  {
    value: 'anthropic', label: 'Anthropic 官方', provider: 'anthropic', region: 'global',
    apiSurface: 'anthropic_messages', baseURL: 'https://api.anthropic.com', adapterType: 'anthropic_native',
    endpointClass: 'anthropic_official_origin',
    note: '使用 Anthropic 官方 Messages 原生协议。Claude Sonnet 5、Opus 5 与 pinned Haiku 4.5 均提供 exact strict text/reasoning 和文本 SSE；Sonnet 5 还提供非流式用户自定义 function tools、精确 tool_choice 与工具历史/响应合同。Haiku 的 summarized thinking 只允许经输出 guard 恒等放行。服务端工具、工具流、多轮 thinking replay、缓存、视觉、请求 service tier、客户价格和 live/release 仍保持 fail-closed。',
  },
  {
    value: 'azure', label: 'Azure OpenAI / Foundry 部署', provider: 'azure',
    baseURL: '', adapterType: 'azure_openai',
    note: 'Base URL、部署名和 API Version 来自你的 Azure 资源；部署名是上游身份。',
  },
];

export const providerIdentityOptions = providerPresets
  .filter((preset, index, presets) => presets.findIndex(item => item.provider === preset.provider) === index)
  .map((preset) => ({
    label: preset.value === 'custom' ? '自定义供应商（也可直接输入名称）' : preset.label,
    value: preset.provider,
  }));

export function adapterTypeForProvider(provider: unknown): ProviderAdapterType {
  const normalized = String(provider || '').trim().toLowerCase();
  if (normalized === 'anthropic') return 'anthropic_native';
  if (normalized === 'azure') return 'azure_openai';
  return 'openai_compatible';
}

export function providerPresetAdapterConfigMatches(
  preset: ProviderPreset,
  actualConfig: unknown,
) {
  const requiredMode = String(preset.adapterConfig?.endpoint_path_mode || '').trim();
  if (!requiredMode) return true;
  const normalizedConfig = actualConfig === undefined ? {} : actualConfig;
  if (!normalizedConfig || typeof normalizedConfig !== 'object' || Array.isArray(normalizedConfig)) return false;
  const keys = Object.keys(normalizedConfig);
  if (keys.some(key => key !== 'endpoint_path_mode')) return false;
  const actualMode = String(
    (normalizedConfig as Record<string, unknown>).endpoint_path_mode || 'canonical_v1',
  ).trim();
  return actualMode === requiredMode;
}

export function providerPreset(value: unknown): ProviderPreset {
  return providerPresets.find((preset) => preset.value === value) || providerPresets[0];
}
