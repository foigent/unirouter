import type { ModelEndpointTarget, ModelEndpointTestResponse } from '../../api/modelEndpoints';
import { ApiError } from '../../api/transport';

const stateCopy: Record<string, { label: string; help: string }> = {
  active: { label: '稳定路由', help: '当前稳定发布可选中至少一个供应商端点。项目或应用能否调用仍由高级调用诊断确认。' },
  canary: { label: '灰度中', help: '稳定路由仍在，部分流量按服务端分桶进入候选路由。' },
  degraded: { label: '路由异常', help: '保留了稳定发布记录，但网关当前没有可用候选。' },
  legacy_active: { label: '兼容路由', help: '网关当前有候选端点，但尚未完全收敛到确定性发布记录。' },
  ready: { label: '可启用', help: '凭证和最新真实检测已就绪，尚未进入路由。' },
  test_failed: { label: '检测失败', help: '最近一次真实供应商请求失败。' },
  needs_test: { label: '待检测', help: '凭证已配置，需要对这个精确端点和模型发起真实检测。' },
  needs_credential: { label: '缺少凭证', help: '没有启用、未冷却且允许该模型的供应商凭证。' },
  unconfigured: { label: '尚无端点', help: '模型目录中存在记录，但还没有供应商端点。' },
  paused: { label: '已暂停', help: '连接被紧急禁用；发布记录仍用于审计和恢复。' },
  stale: { label: '版本不一致', help: '发布固定的连接版本与当前配置不一致。' },
  governance_blocked: { label: '治理阻断', help: '供应商路由与模型目录生命周期冲突；当前模型已退役，网关调用会被拒绝。' },
};

export function stateLabel(state: string) {
  return stateCopy[state]?.label || state;
}

export function stateHelp(state: string) {
  return stateCopy[state]?.help || '状态由配置、凭证、真实检测、发布槽和网关候选共同计算。';
}

export function endpointError(error: unknown, fallback: string) {
  if (error instanceof ApiError) {
    const payload = error.payload as { code?: string; canonical_model?: string; message?: string } | null;
    if (payload?.code === 'model_identity_conflict' && payload.canonical_model) {
      return `检测到同一公共模型已存在：${payload.canonical_model}。请选择这个现有 ID，并把当前供应商配置为它的另一个端点。`;
    }
    if (payload?.code === 'model_retired') return '该公共模型已退役；请先在高级运营中恢复其生命周期，再启用端点。';
    if (payload?.code === 'automatic_release_cleanup_failed') return payload.message || '自动发布记录未能安全终止，请先到发布记录处理。';
    return error.message;
  }
  return error instanceof Error && error.message ? error.message : fallback;
}

export function exactTestSucceeded(target: ModelEndpointTarget, response: ModelEndpointTestResponse) {
  return response.success === true && response.model === target.public_model &&
    String(response.upstream_model || target.upstream_model) === target.upstream_model;
}

export function providerSummary(targets: ModelEndpointTarget[]) {
  if (targets.length === 0) return '尚无供应商端点';
  return targets.map(target => `${target.provider} · ${target.connection_name}`).join('；');
}

export function routeCandidateCopy(target: ModelEndpointTarget) {
  if (target.route_candidate) return '已进入候选';
  switch (target.route_exclusion_reason) {
    case 'channel_inactive': return '尚未发布或当前已暂停';
    case 'no_key':
    case 'no_active_key':
    case 'no_eligible_key': return '没有可用凭证';
    case 'key_model_not_allowed': return '凭证未允许该模型';
    case 'key_cooldown': return '凭证正在冷却';
    case 'circuit_open': return '端点熔断中';
    case 'channel_model_not_found': return '端点未配置该公共模型';
    default: return target.route_exclusion_reason ? '暂不可用（查看高级诊断）' : '未进入候选';
  }
}

export function adapterLabel(adapter: string) {
  switch (adapter) {
    case 'openai_compatible': return 'OpenAI 兼容协议';
    case 'anthropic_native': return 'Anthropic Messages 协议';
    case 'azure_openai': return 'Azure OpenAI 协议';
    default: return adapter || '未声明';
  }
}

export function circuitStateLabel(state: string) {
  switch (state) {
    case 'healthy':
    case 'closed': return '健康';
    case 'half_open': return '恢复探测中';
    case 'open': return '已熔断';
    default: return state || '未知';
  }
}
