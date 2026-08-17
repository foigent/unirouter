export type ProviderAdapterType = 'openai_compatible' | 'anthropic_native' | 'azure_openai';

export type ProviderPreset = {
  value: string;
  label: string;
  provider: string;
  baseURL: string;
  adapterType: ProviderAdapterType;
  note: string;
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
  { value: 'deepseek', label: 'DeepSeek 官方', provider: 'deepseek', baseURL: 'https://api.deepseek.com', adapterType: 'openai_compatible', note: 'OpenAI 兼容协议；上游模型 ID 以 DeepSeek 控制台实际授权为准。' },
  { value: 'kimi', label: 'Kimi / Moonshot 官方', provider: 'kimi', baseURL: 'https://api.moonshot.cn/v1', adapterType: 'openai_compatible', note: 'OpenAI 兼容协议；不会按模型名称猜测参数能力。' },
  { value: 'minimax', label: 'MiniMax 官方（中国区）', provider: 'minimax', baseURL: 'https://api.minimaxi.com/v1', adapterType: 'openai_compatible', note: '使用 MiniMax 官方 OpenAI 兼容入口；模型 ID 以当前控制台为准。' },
  { value: 'mimo', label: '小米 MiMo 官方', provider: 'mimo', baseURL: 'https://api.xiaomimimo.com/v1', adapterType: 'openai_compatible', note: '使用 MiMo 官方 OpenAI Chat Completions 兼容入口；订阅计划如给出专用地址，以控制台为准。' },
  { value: 'openai', label: 'OpenAI 官方', provider: 'openai', baseURL: 'https://api.openai.com/v1', adapterType: 'openai_compatible', note: '当前连接使用 Chat Completions 兼容适配。' },
  { value: 'openrouter', label: 'OpenRouter', provider: 'openrouter', baseURL: 'https://openrouter.ai/api/v1', adapterType: 'openai_compatible', note: '同一公共模型可与官方端点并存，供应商模型 ID 必须分别填写。' },
  { value: 'dashscope', label: '阿里云百炼兼容模式', provider: 'dashscope', baseURL: 'https://dashscope.aliyuncs.com/compatible-mode/v1', adapterType: 'openai_compatible', note: '使用百炼 OpenAI 兼容端点；不会把供应商名称等同于模型身份。' },
  { value: 'anthropic', label: 'Anthropic 官方', provider: 'anthropic', baseURL: 'https://api.anthropic.com', adapterType: 'anthropic_native', note: '使用 Anthropic Messages 原生协议。' },
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
