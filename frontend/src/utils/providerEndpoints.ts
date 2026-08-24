export type ProviderAdapterType = 'openai_compatible' | 'anthropic_native' | 'azure_openai';

export type ProviderPreset = {
  value: string;
  label: string;
  provider: string;
  baseURL: string;
  adapterType: ProviderAdapterType;
  note: string;
  executionProfilePrefixes?: string[];
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
  { value: 'deepseek', label: 'DeepSeek 官方', provider: 'deepseek', baseURL: 'https://api.deepseek.com', adapterType: 'openai_compatible', note: 'OpenAI 兼容协议；可从版本化执行 Profile 选择已审查模型，创建后仍需精确 Operation 探测。', executionProfilePrefixes: ['deepseek.'] },
  { value: 'kimi', label: 'Kimi / Moonshot 官方', provider: 'kimi', baseURL: 'https://api.moonshot.cn/v1', adapterType: 'openai_compatible', note: 'OpenAI 兼容协议；可从版本化执行 Profile 选择已审查模型，不按名称猜测能力。', executionProfilePrefixes: ['kimi.'] },
  { value: 'minimax', label: 'MiniMax 官方（中国区）', provider: 'minimax', baseURL: 'https://api.minimaxi.com/v1', adapterType: 'openai_compatible', note: '使用 MiniMax 官方 OpenAI 兼容入口；Token Plan 不生成金额，普通按量 API Key 只有在显式确认后才可生成待审核目录价草稿。', executionProfilePrefixes: ['minimax.'] },
  { value: 'glm', label: '智谱 GLM 官方', provider: 'glm', baseURL: 'https://open.bigmodel.cn/api/paas/v4', adapterType: 'openai_compatible', note: '使用智谱 OpenAI Chat Completions 兼容入口；目录价按中国区人民币来源建草稿。', executionProfilePrefixes: ['glm.'] },
  { value: 'mimo', label: '小米 MiMo 官方', provider: 'mimo', baseURL: 'https://api.xiaomimimo.com/v1', adapterType: 'openai_compatible', note: '使用 MiMo 官方 OpenAI Chat Completions 兼容入口；订阅计划如给出专用地址，以控制台为准。', executionProfilePrefixes: ['mimo.'] },
  { value: 'hy3', label: '腾讯混元 Hy3 官方', provider: 'hy3', baseURL: 'https://tokenhub.tencentmaas.com/v1', adapterType: 'openai_compatible', note: '使用腾讯混元 Token Hub 的 OpenAI 兼容入口；目录价按人民币来源建草稿。', executionProfilePrefixes: ['hy3.'] },
  { value: 'openai', label: 'OpenAI 官方', provider: 'openai', baseURL: 'https://api.openai.com/v1', adapterType: 'openai_compatible', note: 'GPT-5.6 模板使用 Responses operation；service tier、推理强度与长上下文价格保持为待建模维度。', executionProfilePrefixes: ['openai.'] },
  { value: 'gemini', label: 'Google Gemini 官方', provider: 'gemini', baseURL: 'https://generativelanguage.googleapis.com/v1beta/openai', adapterType: 'openai_compatible', note: '使用 Gemini OpenAI compatibility 入口；价格保留 USD 和 Standard/Batch/Priority 维度。', executionProfilePrefixes: ['gemini.'] },
  { value: 'xai', label: 'xAI 官方', provider: 'xai', baseURL: 'https://api.x.ai/v1', adapterType: 'openai_compatible', note: '使用 xAI OpenAI 兼容入口；长上下文分档和工具调用费用不被平价草稿折叠。', executionProfilePrefixes: ['xai.'] },
  { value: 'doubao', label: '火山方舟 Doubao 官方（北京）', provider: 'doubao', baseURL: 'https://ark.cn-beijing.volces.com/api/v3', adapterType: 'openai_compatible', note: '使用火山方舟北京地域 OpenAI Chat Completions 兼容入口；仅列入 Seed Evolving / 2.1 Pro / 2.1 Turbo。', executionProfilePrefixes: ['doubao.'] },
  { value: 'openrouter', label: 'OpenRouter', provider: 'openrouter', baseURL: 'https://openrouter.ai/api/v1', adapterType: 'openai_compatible', note: '同一公共模型可与官方端点并存，供应商模型 ID 必须分别填写。' },
  { value: 'dashscope', label: '阿里云百炼兼容模式', provider: 'dashscope', baseURL: 'https://dashscope.aliyuncs.com/compatible-mode/v1', adapterType: 'openai_compatible', note: '使用百炼 OpenAI 兼容端点；不会把供应商名称等同于模型身份。', executionProfilePrefixes: ['qwen.'] },
  { value: 'anthropic', label: 'Anthropic 官方', provider: 'anthropic', baseURL: 'https://api.anthropic.com', adapterType: 'anthropic_native', note: '使用 Anthropic Messages 原生协议；5 分钟与 1 小时缓存写入价格保持分离。', executionProfilePrefixes: ['anthropic.'] },
  { value: 'azure', label: 'Azure OpenAI / Foundry 部署', provider: 'azure', baseURL: '', adapterType: 'azure_openai', note: 'Base URL、部署名和 API Version 来自你的 Azure 资源；部署名是上游身份。' },
];

export const providerIdentityOptions = providerPresets
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

export function providerPreset(value: unknown): ProviderPreset {
  return providerPresets.find((preset) => preset.value === value) || providerPresets[0];
}
