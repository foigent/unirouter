import { api } from './client';

export type AccessSubject = {
  type: 'user' | 'workload_application';
  user_id?: number;
  workload_application_id?: number;
};

export type AccessEnvelope = {
  project_role: 'project_admin' | 'project_user' | 'project_viewer';
  scope_mode: 'project_default' | 'explicit';
  models: string[];
  endpoints: string[];
  rpm_limit?: number;
  rps_limit?: number;
  concurrency_limit?: number;
};

export type RequestActions = {
  can_approve: boolean;
  can_reject: boolean;
  can_cancel: boolean;
  reason_code: string;
};

export type AccessRequest = {
  id: number;
  request_ref: string;
  org_id: number;
  project_id: number;
  subject: AccessSubject;
  request_kind: string;
  requested_scope: AccessEnvelope;
  environment: string;
  production_use: boolean;
  requested_valid_until?: string;
  submission_mode: string;
  sponsor_user_id?: number;
  status: 'approved' | 'needs_review' | 'rejected' | 'cancelled';
  decision_mode: string;
  policy_reason_codes: string[];
  decision_reason_codes: string[];
  submitted_by: number;
  submitted_at: string;
  decided_at?: string;
  cancelled_at?: string;
  revision: number;
  actions?: RequestActions;
};

export type AccessGrant = {
  id: number;
  grant_ref: string;
  access_request_id: number;
  org_id: number;
  project_id: number;
  subject: AccessSubject;
  scope: AccessEnvelope;
  environment: string;
  production_use: boolean;
  sponsor_user_id?: number;
  status: 'active' | 'suspended' | 'revoked';
  revision: number;
  valid_from: string;
  valid_until?: string;
  policy_reason_codes: string[];
  decision_reason_codes: string[];
  created_at: string;
  updated_at: string;
};

export function isGrantCurrentlyActive(grant: AccessGrant, now = Date.now()) {
  if (grant.status !== 'active') return false;
  if (!grant.valid_until) return true;
  const validUntil = Date.parse(grant.valid_until);
  return Number.isFinite(validUntil) && validUntil > now;
}

export function effectiveGrantStatus(grant: AccessGrant, now = Date.now()) {
  return grant.status === 'active' && !isGrantCurrentlyActive(grant, now) ? 'expired' : grant.status;
}

export type PageInfo = {
  limit: number;
  has_more: boolean;
  next_after_id?: number;
};

export type AccessPage<T> = {
  data: T[];
  pagination: PageInfo;
  scope?: 'my' | 'project';
};

export type CredentialVersion = {
  id: number;
  key_prefix: string;
  status: string;
  effective_status: string;
  created_at: string;
  rotated_from_key_id?: number;
};

export type AccessCredentialFamily = {
  id: number;
  credential_ref: string;
  name: string;
  status: string;
  effective_status: string;
  allowed_ips: string;
  allowed_models: string[];
  allowed_endpoints: string[];
  rpm_limit: number;
  rps_limit: number;
  concurrency_limit?: number;
  created_at: string;
  version_count: number;
  versions: CredentialVersion[];
};

export type IssuedCredential = {
  id: number;
  credential_ref: string;
  key?: string;
  key_prefix: string;
  name: string;
  grant_ref: string;
  status: string;
  allowed_ips: string;
  allowed_models: string[];
  allowed_endpoints: string[];
  rpm_limit: number;
  rps_limit: number;
  concurrency_limit?: number;
  classification: string;
  idempotent: boolean;
  secret_available: boolean;
  recovery_action?: string;
};

export type ProvisionResult = {
  request: AccessRequest;
  grant?: AccessGrant;
  idempotent: boolean;
};

export type LegacyCredentialSummary = {
  key_id: number;
  key_prefix: string;
  key_type: string;
  purpose?: string;
  project_id?: number;
  owner_user_id?: number;
};

export type LegacyAccessClassification = {
  object_type: 'credential' | 'workload_application';
  object_id: number;
  object_ref: string;
  classification: string;
  reason_code: string;
  migration_status: string;
  automatic_migration: boolean;
  project_id?: number;
  owner_user_id?: number;
  workload_application_id?: number;
  application_ref?: string;
  kind?: string;
  identity_mode?: string;
  environment?: string;
  status?: string;
  client_profiles?: string[];
  linked_credential_count?: number;
  linked_credentials?: LegacyCredentialSummary[];
  key_id?: number;
  key_prefix?: string;
};

export type ReadinessAxis = {
  state: 'ready' | 'blocked' | 'warning' | 'not_applicable' | 'unavailable';
  reason_code: string;
  safe_evidence: Record<string, unknown>;
  fix_route?: string;
  fix_owner?: string;
};

export type ModelReadiness = {
  contract_version: string;
  scope: {
    org_id: number;
    project_id: number;
    requested_model: string;
    resolved_model: string;
    endpoint: string;
    caller: 'self' | 'workload_application';
    application_ref?: string;
  };
  summary: {
    registered: { value?: boolean; state: ReadinessAxis['state']; reason_code: string };
    catalog_visible: { value?: boolean; state: ReadinessAxis['state']; reason_code: string };
    routable: { value?: boolean; state: ReadinessAxis['state']; reason_code: string };
    current_project_caller_callable: { value?: boolean; state: ReadinessAxis['state']; reason_code: string };
  };
  platform_model: Record<'registration' | 'lifecycle' | 'catalog' | 'price_authority' | 'channel' | 'provider_credential' | 'base_route' | 'runtime_slot', ReadinessAxis>;
  project_caller: Record<'project' | 'alias' | 'model_set' | 'wallet' | 'project_budget' | 'application' | 'grant' | 'credential' | 'model_authorization' | 'limit_policy', ReadinessAxis>;
  runtime_mutation: 'none';
  external_requests: 'none';
  as_of: string;
};

export type WorkloadApplication = {
  id: number;
  org_id: number;
  project_id: number;
  application_ref: string;
  contract_version: string;
  identity_contract_version: string;
  logical_ref?: string;
  display_name: string;
  kind: 'service' | 'agent' | 'workflow' | 'integration_adapter' | string;
  environment: string;
  production_use: boolean;
  identity_mode: string;
  client_profile?: string;
  client_profiles: string[];
  owner_team: string;
  description: string;
  status: string;
  revision: number;
  project_name: string;
  project_status: string;
  credential_count: number;
  active_credential_count: number;
  identity_classification: string;
  compatibility_evidence?: string;
  can_manage: boolean;
  can_request_access: boolean;
  can_read_credentials: boolean;
};

export type WorkloadContract = {
  contract_version: string;
  readiness_version: string;
  client_profiles: Array<{
    profile_id: string;
    display_name: string;
    runtime_state: string;
    supported_endpoints: string[];
    configuration_note: string;
  }>;
};

const clientRequestHeaders = (clientRequestID: string) => ({
  'X-UniRouter-Client-Request-ID': clientRequestID,
});

export const accessApi = {
  listMyRequests: (limit = 50, afterID?: number) =>
    api.request(`/access-requests?scope=my&limit=${limit}${afterID ? `&after_id=${afterID}` : ''}`) as Promise<AccessPage<AccessRequest>>,
  listProjectRequests: (projectID: number, filters?: { status?: string; workload_application_id?: number; limit?: number; after_id?: number }) => {
    const search = new URLSearchParams({ scope: 'project', project_id: String(projectID), limit: String(filters?.limit || 50) });
    if (filters?.status) search.set('status', filters.status);
    if (filters?.workload_application_id) search.set('workload_application_id', String(filters.workload_application_id));
    if (filters?.after_id) search.set('after_id', String(filters.after_id));
    return api.request(`/access-requests?${search.toString()}`) as Promise<AccessPage<AccessRequest>>;
  },
  getRequest: (requestRef: string) =>
    api.request(`/access-requests/${encodeURIComponent(requestRef)}`) as Promise<{ request: AccessRequest; grant?: AccessGrant }>,
  createRequest: (input: Record<string, unknown>, clientRequestID: string) =>
    api.request('/access-requests', { method: 'POST', headers: clientRequestHeaders(clientRequestID), body: JSON.stringify(input) }) as Promise<ProvisionResult>,
  decideRequest: (requestRef: string, input: { expected_revision: number; decision: 'approve' | 'reject'; reason_codes?: string[] }, clientRequestID: string) =>
    api.request(`/access-requests/${encodeURIComponent(requestRef)}/decision`, { method: 'POST', headers: clientRequestHeaders(clientRequestID), body: JSON.stringify({ ...input, reason_codes: input.reason_codes || [] }) }) as Promise<ProvisionResult>,
  cancelRequest: (requestRef: string, expectedRevision: number, clientRequestID: string) =>
    api.request(`/access-requests/${encodeURIComponent(requestRef)}/cancel`, { method: 'POST', headers: clientRequestHeaders(clientRequestID), body: JSON.stringify({ expected_revision: expectedRevision }) }) as Promise<{ request: AccessRequest; idempotent: boolean }>,
  listMyGrants: (limit = 50, afterID?: number) =>
    api.request(`/access-grants?scope=my&limit=${limit}${afterID ? `&after_id=${afterID}` : ''}`) as Promise<AccessPage<AccessGrant>>,
  listProjectGrants: (projectID: number, filters?: { workload_application_id?: number; limit?: number; after_id?: number }) => {
    const search = new URLSearchParams({ scope: 'project', project_id: String(projectID), limit: String(filters?.limit || 50) });
    if (filters?.workload_application_id) search.set('workload_application_id', String(filters.workload_application_id));
    if (filters?.after_id) search.set('after_id', String(filters.after_id));
    return api.request(`/access-grants?${search.toString()}`) as Promise<AccessPage<AccessGrant>>;
  },
  listCredentials: (grantRef: string, limit = 50, afterID?: number) =>
    api.request(`/access-grants/${encodeURIComponent(grantRef)}/credentials?limit=${limit}${afterID ? `&after_id=${afterID}` : ''}`) as Promise<AccessPage<AccessCredentialFamily>>,
  issueCredential: (grantRef: string, input: Record<string, unknown>, clientRequestID: string) =>
    api.request(`/access-grants/${encodeURIComponent(grantRef)}/credentials`, { method: 'POST', headers: clientRequestHeaders(clientRequestID), body: JSON.stringify(input) }) as Promise<IssuedCredential>,
  rotateCredential: (grantRef: string, keyID: number, clientRequestID: string) =>
    api.request(`/access-grants/${encodeURIComponent(grantRef)}/credentials/${keyID}/rotate`, { method: 'POST', headers: clientRequestHeaders(clientRequestID), body: '{}' }) as Promise<{ credential: IssuedCredential; predecessor: CredentialVersion; idempotent: boolean }>,
  revokeCredential: (grantRef: string, keyID: number, clientRequestID: string) =>
    api.request(`/access-grants/${encodeURIComponent(grantRef)}/credentials/${keyID}/revoke`, { method: 'POST', headers: clientRequestHeaders(clientRequestID), body: '{}' }) as Promise<{ status: string }>,
  previewLegacy: (orgID?: number) =>
    api.request(`/access-provisioning/legacy-preview${orgID ? `?org_id=${orgID}` : ''}`) as Promise<{ mutation: false; data: LegacyAccessClassification[]; total: number }>,
  modelReadiness: (input: { project_id: number; model: string; endpoint: string; caller: 'self' | 'workload_application'; application_ref?: string }) => {
    const search = new URLSearchParams({
      project_id: String(input.project_id), model: input.model, endpoint: input.endpoint, caller: input.caller,
    });
    if (input.application_ref) search.set('application_ref', input.application_ref);
    return api.request(`/model-readiness?${search.toString()}`) as Promise<ModelReadiness>;
  },
};
