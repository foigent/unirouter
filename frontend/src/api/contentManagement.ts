import { request } from './transport';

export type PayloadMode = 'disabled' | 'metadata_only' | 'full_content';

export interface PayloadConfig {
  mode: PayloadMode;
  supported_protocols: string[];
  observation_points: string[];
  retention_seconds?: number;
  quota_bytes?: number;
  max_saved_bytes?: number;
  allowed_purposes: string[];
  on_limit?: 'record_capture_gap';
}

export interface PayloadPolicy {
  schema_version: 'unirouter.payload-policy.v1';
  source_instance: string;
  org_id: string;
  project_id: string;
  policy_ref: string;
  desired: { policy_version: string; config: PayloadConfig };
  effective: { policy_version: string; config: PayloadConfig };
  effective_at: string;
  application_status: 'applied' | 'pending' | 'failed';
  application_reason: string;
  storage_status: 'ready' | 'not_configured' | 'unavailable' | 'capacity_exhausted';
  limits: { max_saved_bytes: number; max_read_bytes: number; max_buffer_bytes: number; max_manifest_bytes: number; max_manifest_items: number };
}

export type PayloadScope = 'enterprise' | 'project' | 'user';
export type ExecutionStorageMode = 'temporary_allowed' | 'strict_no_persist';
export interface ScopedPolicyLayer {
  policy_ref: string;
  explicit: boolean;
  desired?: { policy_version: string; config: PayloadConfig };
  effective?: { policy_version: string; config: PayloadConfig };
  desired_execution_storage_mode?: ExecutionStorageMode;
  effective_execution_storage_mode?: ExecutionStorageMode;
  application_status?: string;
  application_reason?: string;
  project_quota_bytes?: number;
  desired_default_project_quota_bytes?: number;
  effective_default_project_quota_bytes?: number;
}
export interface ScopedPayloadPolicy {
  schema_version: 'unirouter.payload-policy.v2';
  source_instance: string;
  org_id: string;
  project_id?: string;
  user_id?: string;
  enterprise: ScopedPolicyLayer;
  project?: ScopedPolicyLayer;
  user?: ScopedPolicyLayer;
  effective?: {
    scope: PayloadScope | 'deployment';
    policy_ref: string;
    effective: { policy_version: string; config: PayloadConfig };
    execution_storage_mode: ExecutionStorageMode;
    project_quota_bytes: number;
    version_vector: string;
  };
  version_vector: string;
  storage_status: string;
  deployment_mode: PayloadMode;
  full_content_enabled: boolean;
  limits: PayloadPolicy['limits'];
  policy_capabilities?: { supported_protocols: string[]; observation_points: string[] };
}
export interface ScopedPayloadPut {
  schema_version: 'unirouter.payload-policy.v2';
  expected_policy_ref: string;
  expected_version: string;
  expected_vector: string;
  desired: PayloadConfig;
  execution_storage_mode: ExecutionStorageMode;
  project_quota_bytes?: number;
}

export interface ContentRegistration {
  schema_version: 'unirouter.xinsight.source-credential-registration.v1';
  token_format: 'ur-xf1-hex256.v1';
  verifier_scheme: 'receiver_sha256_v1';
  key_prefix: string;
  verifier_sha256: string;
  operation_id: string;
  source_instance: string;
  org_id: string;
  project_id: string;
  profiles: ['unirouter.interaction-content.v1'];
}

export interface ContentConnection {
  schema_version: 'unirouter.source-content-control.v1';
  source_instance: string;
  org_id: string;
  project_id: string;
  connection_status: 'not_configured' | 'paused' | 'active' | 'revoked';
  content_status: 'not_configured' | 'paused' | 'active' | 'revoked';
  state_version: string;
  content_scope_revision: string;
  active?: { credential_id: string; registration_hash: string };
  staged?: { credential_id: string; registration_hash: string };
  content_access?: { enabled: boolean; profile: string; content_scope_revision: string };
}

export type ContentAction = 'prepare' | 'rotate' | 'activate' | 'pause' | 'resume' | 'cancel' | 'revoke';
export interface ContentControlRequest {
  operation_id: string;
  expected_state_version: string;
  credential_id?: string;
  registration_hash?: string;
  registration?: ContentRegistration;
  allowed_ip?: string;
}
export interface ContentControlResult {
  operation_id: string;
  action: ContentAction;
  state_version: string;
  content_scope_revision: string;
  credential_id?: string;
  registration_hash?: string;
}

export const contentManagementApi = {
  getPolicy: (projectId: number) => request<PayloadPolicy>(`/projects/${projectId}/payload-policy`),
  putPolicy: (projectId: number, expectedPolicyVersion: string, desired: PayloadConfig) => request<PayloadPolicy>(
    `/projects/${projectId}/payload-policy`, {
      method: 'PUT',
      body: JSON.stringify({ schema_version: 'unirouter.payload-policy.update.v1', expected_policy_version: expectedPolicyVersion, desired }),
    },
  ),
  getScopedPolicy: (projectId: number, userId?: number) => request<ScopedPayloadPolicy>(
    userId ? `/projects/${projectId}/users/${userId}/payload-policy` : `/projects/${projectId}/payload-policy`, {}, 'v2'),
  getEnterprisePolicy: (orgId: number) => request<ScopedPayloadPolicy>(`/organizations/${orgId}/payload-policy`, {}, 'v2'),
  putScopedPolicy: (scope: PayloadScope, orgId: number, projectId: number, userId: number | undefined, body: ScopedPayloadPut) => {
    const path = scope === 'enterprise' ? `/organizations/${orgId}/payload-policy`
      : scope === 'user' ? `/projects/${projectId}/users/${userId}/payload-policy`
        : `/projects/${projectId}/payload-policy`;
    return request<ScopedPayloadPolicy>(path, { method: 'PUT', body: JSON.stringify(body) }, 'v2');
  },
  getConnection: (projectId: number) => request<ContentConnection>(`/data-exchange/xinsight/source-connections/${projectId}/content`),
  control: (projectId: number, action: ContentAction, body: ContentControlRequest) => request<ContentControlResult>(
    `/data-exchange/xinsight/source-connections/${projectId}/content/${action}`,
    { method: 'POST', body: JSON.stringify(body) },
  ),
};

const registrationFields = [
  'schema_version', 'token_format', 'verifier_scheme', 'key_prefix', 'verifier_sha256',
  'operation_id', 'source_instance', 'org_id', 'project_id', 'profiles',
];

export function parseContentRegistration(raw: string, current: ContentConnection): ContentRegistration {
  let parsed: unknown;
  try { parsed = JSON.parse(raw); } catch { throw new Error('请输入接收端生成的正文注册 JSON'); }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('正文注册必须是 JSON object');
  const value = parsed as Record<string, unknown>;
  if (Object.keys(value).length !== registrationFields.length || Object.keys(value).some(key => !registrationFields.includes(key)) ||
    value.schema_version !== 'unirouter.xinsight.source-credential-registration.v1' ||
    value.token_format !== 'ur-xf1-hex256.v1' || value.verifier_scheme !== 'receiver_sha256_v1' ||
    typeof value.key_prefix !== 'string' || !/^ur-xf1-[0-9a-f]{13}$/.test(value.key_prefix) ||
    typeof value.verifier_sha256 !== 'string' || !/^[0-9a-f]{64}$/.test(value.verifier_sha256) ||
    typeof value.operation_id !== 'string' || !/^[0-9a-f]{32}$/.test(value.operation_id) ||
    value.source_instance !== current.source_instance || value.org_id !== current.org_id ||
    value.project_id !== current.project_id || !Array.isArray(value.profiles) ||
    value.profiles.length !== 1 || value.profiles[0] !== 'unirouter.interaction-content.v1') {
    throw new Error('正文注册与当前来源或项目不匹配，或包含不受支持字段；不得粘贴明文 Bearer');
  }
  return value as unknown as ContentRegistration;
}
