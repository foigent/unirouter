import { request } from './transport';

export const sourceProfiles = [
  'unirouter.attempt-observation.v1',
  'unirouter.budget-alert.v1',
  'unirouter.budget-definition.v1',
  'unirouter.budget-period-summary.v1',
  'unirouter.capability-snapshot.v1',
  'unirouter.cost-basis.v1',
  'unirouter.destination-observation.v1',
  'unirouter.execution-context.v1',
  'unirouter.execution-identity.v1',
  'unirouter.policy-observation.v1',
  'unirouter.runtime-observation.v1',
] as const;
export type SourceProfile = typeof sourceProfiles[number];
export interface SourceRegistration {
  schema_version: 'unirouter.xinsight.source-credential-registration.v1';
  token_format: 'ur-xf1-hex256.v1';
  verifier_scheme: 'receiver_sha256_v1';
  key_prefix: string;
  verifier_sha256: string;
  operation_id: string;
  source_instance: string;
  org_id: string;
  project_id: string;
  profiles: SourceProfile[];
}
export interface SourceConnection {
  capture_enabled?: boolean;
  available_profiles?: SourceProfile[];
  status: 'not_configured' | 'paused' | 'active' | 'revoked';
  source_instance: string;
  scope_generation?: string;
  profiles?: SourceProfile[];
  credentials?: { id: string; key_prefix: string; status: 'feed_active' | 'feed_staged'; operation_id: string; profiles: SourceProfile[] }[];
}
export type SourceAction = 'prepare' | 'rotate' | 'activate' | 'cancel' | 'pause' | 'resume' | 'revoke' | 'profiles';
export interface SourceControlResult {
  status?: SourceConnection['status'];
  scope_generation?: string;
  credential_id?: string;
  credential_status?: string;
  replayed?: boolean;
}
export const sourceConnectionsApi = {
  get: (projectId: number) => request<SourceConnection>(`/data-exchange/xinsight/source-connections/${projectId}`),
  control: (projectId: number, action: SourceAction, body: {
    allowed_ip?: string; registration?: SourceRegistration; profiles?: SourceProfile[]; credential_id?: string;
  } = {}) => request<SourceControlResult>(`/data-exchange/xinsight/source-connections/${projectId}/${action}`, {
    method: 'POST', body: JSON.stringify(body),
  }),
};

export function parseSourceRegistration(raw: string, instance: string, projectId: number, availableProfiles: readonly SourceProfile[] = sourceProfiles): SourceRegistration {
  const value: unknown = JSON.parse(raw);
  if (!value || Array.isArray(value) || typeof value !== 'object') throw new Error('请输入来源凭据注册 JSON');
  const r = value as Record<string, unknown>;
  const fields = ['schema_version', 'token_format', 'verifier_scheme', 'key_prefix', 'verifier_sha256', 'operation_id', 'source_instance', 'org_id', 'project_id', 'profiles'];
  if (Object.keys(r).length !== fields.length || Object.keys(r).some(key => !fields.includes(key)) ||
    r.schema_version !== 'unirouter.xinsight.source-credential-registration.v1' || r.token_format !== 'ur-xf1-hex256.v1' ||
    r.verifier_scheme !== 'receiver_sha256_v1' || typeof r.key_prefix !== 'string' || !/^ur-xf1-[0-9a-f]{13}$/.test(r.key_prefix) ||
    typeof r.verifier_sha256 !== 'string' || !/^[0-9a-f]{64}$/.test(r.verifier_sha256) ||
    typeof r.operation_id !== 'string' || !/^[0-9a-f]{32}$/.test(r.operation_id) ||
    r.source_instance !== instance || r.project_id !== String(projectId) || typeof r.org_id !== 'string' || !/^[1-9][0-9]{0,18}$/.test(r.org_id) ||
    !Array.isArray(r.profiles) || !r.profiles.length || r.profiles.length > sourceProfiles.length ||
    r.profiles.some((p: unknown, i: number) => typeof p !== 'string' || !(sourceProfiles as readonly string[]).includes(p) || !(availableProfiles as readonly string[]).includes(p) || i > 0 && String(r.profiles && (r.profiles as string[])[i - 1]) >= p)) {
    throw new Error('注册信息与当前来源、项目或权限不匹配；请使用接收端生成的专用注册包');
  }
  return r as unknown as SourceRegistration;
}

export interface SourceBudgetBinding {
  scope_kind: 'project'; scope_ref: string; project_id: string;
  revision: string; status: 'active' | 'revoked'; audit_ref: string; changed_at: string;
}
export const sourceBudgetBindingsApi = {
  list: (orgId: number, projectId: number, after = '') => request<{ data: SourceBudgetBinding[]; has_more: boolean; next_after: string }>(
    `/enterprise-finance/source-budget-bindings?org_id=${orgId}&project_id=${projectId}&after=${encodeURIComponent(after)}`),
  set: (orgId: number, projectId: number, scopeRef: string, expectedRevision: string, status: 'active' | 'revoked') => request<SourceBudgetBinding>(
    '/enterprise-finance/source-budget-bindings', { method: 'POST', body: JSON.stringify({ org_id: orgId, project_id: projectId, scope_kind: 'project', scope_ref: scopeRef, expected_revision: expectedRevision, status }) }),
};
