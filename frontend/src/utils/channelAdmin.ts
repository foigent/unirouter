import { ApiError } from '../api/client';
import { parseChannelModels } from './channelReadiness';

export interface Channel {
  id: number;
  name: string;
  provider: string;
  base_url: string;
  models: string;
  adapter_type?: string;
  adapter_config?: Record<string, unknown> | string;
  priority: number;
  routing_weight: number;
  routing_epoch: number;
  degradation_threshold: number;
  status: string;
  effective_status?: string;
  published_models?: string;
  circuit_state: string;
  fail_count: number;
  created_at: string;
}

export interface RoutingPreviewCandidate {
  channel_id: number;
  channel_name: string;
  provider: string;
  priority: number;
  weight: number;
  order?: number;
  circuit_state: string;
  eligible_key_count: number;
  reason: string;
  requires_probe: boolean;
}

export interface RoutingPreview {
  policy_version: string;
  snapshot_version: string;
  snapshot_source: string;
  degraded: boolean;
  model: string;
  candidates: RoutingPreviewCandidate[];
  exclusions: RoutingPreviewCandidate[];
}

export interface ProviderPackManifest {
  pack_id: string;
  pack_version: string;
  provider: string;
  adapter_type: string;
  conformance_level: 'fixture_verified';
  network_verified: boolean;
  endpoints: string[];
  modalities: string[];
  auth_profiles: Array<{ type: string; wire_header: string }>;
}

export interface ProviderPack {
  manifest: ProviderPackManifest;
  manifest_sha256: string;
  schema_sha256: string;
}

export type ProviderPackMatch = {
  pack: ProviderPack;
  scope: 'provider_fixture' | 'adapter_protocol_fixture';
};

export function findProviderPackMatch(packs: ProviderPack[], channel?: Channel): ProviderPackMatch | undefined {
  if (!channel) return undefined;
  const adapterType = channel.adapter_type || 'openai_compatible';
  const exact = packs.find(pack =>
    pack.manifest.provider.toLowerCase() === channel.provider.toLowerCase() &&
    pack.manifest.adapter_type === adapterType);
  if (exact) return { pack: exact, scope: 'provider_fixture' };

  const referenceProvider = ({
    openai_compatible: 'openai',
    anthropic_native: 'anthropic',
    azure_openai: 'azure',
  } as Record<string, string>)[adapterType];
  const protocolFixture = referenceProvider
    ? packs.find(pack => pack.manifest.provider === referenceProvider && pack.manifest.adapter_type === adapterType)
    : undefined;
  return protocolFixture ? { pack: protocolFixture, scope: 'adapter_protocol_fixture' } : undefined;
}

export const channelStatusOptions = [
  { label: '待发布（staged）', value: 'staged' },
  { label: '紧急禁用（disabled）', value: 'disabled' },
];

export function channelUpdateErrorMessage(error: unknown) {
  if (!(error instanceof ApiError)) return '更新失败，请稍后重试';
  const backendMessage = error.message.toLowerCase();
  if (error.status === 400 && backendMessage.includes('status must be')) {
    return '通道状态仅支持 active、staged 或 disabled';
  }
  if (error.status === 400 && backendMessage.includes('adapter_type')) {
    return 'Adapter Type 不受支持';
  }
  if (error.status === 400 && backendMessage.includes('adapter_config')) {
    return 'Adapter Config 与所选协议不兼容';
  }
  if (error.status === 400) return '通道配置无效，请检查字段后重试';
  if (error.status === 403) return '当前账号无权更新此通道';
  if (error.status === 404) return '通道不存在或已被删除';
  if (error.status >= 500) return '服务暂时无法更新通道，请稍后重试';
  return `更新失败（HTTP ${error.status}）`;
}

export function channelConflictCode(error: ApiError) {
  const payload = error.payload && typeof error.payload === 'object'
    ? error.payload as { code?: unknown }
    : {};
  return typeof payload.code === 'string' ? payload.code : '';
}

export function channelConflictMessage(error: ApiError) {
  const backendMessage = error.message.toLowerCase();
  const payload = error.payload && typeof error.payload === 'object'
    ? error.payload as { published_models?: unknown }
    : {};
  const code = channelConflictCode(error);
  if (code === 'channel_bound_to_active_release') {
    const models = Array.isArray(payload.published_models)
      ? payload.published_models.filter((model): model is string => typeof model === 'string').join('、')
      : '';
    return `该连接仍关联已生效的历史发布记录${models ? `（${models}）` : ''}。请点击“恢复连接”，先结束这些历史发布并把连接转为待发布；刷新页面不会改变运行状态。`;
  }
  if (code === 'stale_routing_epoch') {
    return '该连接确实已在你打开页面后发生变化，请刷新后重试';
  }
  if (backendMessage.includes('active channel runtime fields')) {
    return '生效通道的运行参数不能直接修改；请先改为 staged，再通过“模型治理 → 运行时发布”上线';
  }
  if (backendMessage.includes('direct channel activation')) {
    return '不能直接启用通道；请通过“模型治理 → 运行时发布”完成验证、灰度与提升';
  }
  return '连接状态在更新时发生变化，请刷新后重试';
}

export const routingReasonLabel = (reason: string) => ({
  equal_priority_static_weight: '同优先级内按权重排序',
  priority_failover: '按优先级故障转移',
  channel_inactive: '通道未启用',
  circuit_recovery_pending: '熔断恢复等待中',
  probe_sample_not_selected: '本次未抽中恢复探测',
  circuit_unavailable: '熔断状态不可用',
  no_key: '没有凭证',
  no_active_key: '没有启用的凭证',
  key_cooldown: '凭证冷却中',
  key_model_not_allowed: '凭证不允许此模型',
  candidate_limit: '超过单次候选上限',
}[reason] || reason);

export const routingSourceLabel = (source: string) => ({
  live: '刚从数据库刷新',
  cache: '本机 1 秒内快照',
  last_known_good: '数据库异常时的短时备用快照',
}[source] || source);

export function selectedChannelIDFromParams() {
  const value = Number(new URLSearchParams(window.location.search).get('channel_id'));
  return Number.isSafeInteger(value) && value > 0 ? value : undefined;
}

export function recoveryModelFromParams() {
  const params = new URLSearchParams(window.location.search);
  if (params.get('from') !== 'connections' || params.get('intent') !== 'recover') return '';
  return String(params.get('model') || '').trim();
}

export function clearConnectionRecoveryParams() {
  const params = new URLSearchParams(window.location.search);
  params.delete('from');
  params.delete('model');
  params.delete('intent');
  params.delete('tab');
  const query = params.toString();
  window.history.replaceState(null, '', `${window.location.pathname}${query ? `?${query}` : ''}`);
}

export function syncSelectedChannelID(id?: number) {
  const params = new URLSearchParams(window.location.search);
  if (id) params.set('channel_id', String(id));
  else params.delete('channel_id');
  const query = params.toString();
  window.history.replaceState(null, '', `${window.location.pathname}${query ? `?${query}` : ''}`);
}

export function effectiveChannelStatus(channel: Channel) {
  return channel.effective_status || channel.status;
}

export function channelHasRetainedRelease(channel: Channel) {
  return effectiveChannelStatus(channel) !== 'active' && parseChannelModels(channel.published_models).length > 0;
}

export function navigateWithinApp(path: string) {
  window.history.pushState(null, '', path);
  window.dispatchEvent(new PopStateEvent('popstate'));
}

export function connectionAdvancedPath(path: string, channelID: number, model?: string) {
  const params = new URLSearchParams({ from: 'connections', channel_id: String(channelID) });
  if (model) params.set('model', model);
  return `${path}?${params.toString()}`;
}

export function adapterConfigFacts(channel: Channel) {
  let config: Record<string, unknown> = {};
  if (channel.adapter_config && typeof channel.adapter_config === 'object') {
    config = channel.adapter_config;
  } else if (typeof channel.adapter_config === 'string') {
    try {
      const parsed = JSON.parse(channel.adapter_config);
      if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) config = parsed;
    } catch {
      return [{ key: 'config', label: '配置状态', value: '无法解析' }];
    }
  }
  const allowed = channel.adapter_type === 'azure_openai'
    ? [['deployment', 'Deployment'], ['api_version', 'API version'], ['auth_mode', 'Auth mode']]
    : channel.adapter_type === 'anthropic_native'
      ? [['anthropic_version', 'Anthropic version'], ['anthropic_beta', 'Anthropic beta']]
      : [];
  return allowed.flatMap(([key, label]) => {
    const value = config[key];
    return typeof value === 'string' && value.trim()
      ? [{ key, label, value }]
      : [];
  });
}
