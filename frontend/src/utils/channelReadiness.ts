import { parseModelList } from './modelList';

export interface ChannelReadinessChannel {
  id: number;
  name: string;
  status: string;
  models?: string;
}

export interface ChannelReadinessCredential {
  channel_id: number;
  status: string;
  allowed_models?: string;
  cooldown_until?: string | null;
}

export type CredentialCombinationState =
  | 'credential_disabled'
  | 'cooling_down'
  | 'model_mismatch'
  | 'route_candidate'
  | 'awaiting_release'
  | 'channel_disabled'
  | 'unknown_channel';

export function parseChannelModels(value?: string) {
  return parseModelList(value);
}

export function channelStatusLabel(status?: string) {
  if (status === 'active') return '生效';
  if (status === 'staged') return '待发布';
  if (status === 'disabled') return '禁用';
  return status || '未知';
}

export function isCredentialCoolingDown(
  credential: Pick<ChannelReadinessCredential, 'cooldown_until'>,
  nowMs = Date.now(),
) {
  if (!credential.cooldown_until) return false;
  const until = Date.parse(credential.cooldown_until);
  return Number.isFinite(until) && until > nowMs;
}

export function credentialAllowsModelScope(allowedModels: string | undefined, model: string) {
  const allowed = parseChannelModels(allowedModels);
  if (allowed.length === 0) return true;
  return allowed.includes(model.trim());
}

export function credentialModelCoverage(
  channelModelsValue?: string,
  allowedModelsValue?: string,
) {
  const channelModels = parseChannelModels(channelModelsValue);
  const allowedModels = parseChannelModels(allowedModelsValue);
  if (channelModels.length === 0) {
    return { kind: 'channel_unconfigured' as const, matching: [] as string[], channelModels, allowedModels };
  }
  if (allowedModels.length === 0) {
    return { kind: 'all' as const, matching: channelModels, channelModels, allowedModels };
  }
  const allowedSet = new Set(allowedModels);
  const matching = channelModels.filter(model => allowedSet.has(model));
  return {
    kind: matching.length > 0 ? 'partial' as const : 'none' as const,
    matching,
    channelModels,
    allowedModels,
  };
}

export function evaluateCredentialCombination(
  credential: ChannelReadinessCredential,
  channel?: ChannelReadinessChannel,
  nowMs = Date.now(),
): CredentialCombinationState {
  if (credential.status !== 'active') return 'credential_disabled';
  if (isCredentialCoolingDown(credential, nowMs)) return 'cooling_down';
  if (!channel) return 'unknown_channel';
  if (credentialModelCoverage(channel.models, credential.allowed_models).kind === 'none') return 'model_mismatch';
  if (channel.status === 'active') return 'route_candidate';
  if (channel.status === 'staged') return 'awaiting_release';
  if (channel.status === 'disabled') return 'channel_disabled';
  return 'unknown_channel';
}

export function eligibleCredentialsForModel(
  channel: ChannelReadinessChannel,
  credentials: ChannelReadinessCredential[],
  model: string,
  nowMs = Date.now(),
) {
  return credentials.filter(credential => (
    credential.channel_id === channel.id
    && credential.status === 'active'
    && !isCredentialCoolingDown(credential, nowMs)
    && credentialAllowsModelScope(credential.allowed_models, model)
  ));
}
