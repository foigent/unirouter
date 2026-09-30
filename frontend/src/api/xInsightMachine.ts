import { request } from './transport';

export type MachinePurpose = 'xinsight.policy.manage' | 'xinsight.source.enroll';
export interface MachineCredentialRegistration {
  installation_id: string;
  purpose: MachinePurpose;
  key_prefix: string;
  verifier_sha256: string;
  operation_id: string;
  allowed_ip: string;
}
export interface MachineCredential {
  key_id: string;
  key_prefix: string;
  status: string;
  purpose: MachinePurpose;
  operation_id: string;
}
export interface PolicyDelegation {
  delegation_id: string;
  target_kind: 'enterprise_default' | 'project_policy' | 'user_exception';
  coverage: 'exact' | 'all_current_future';
  project_id: string;
  user_id: string;
  read: boolean;
  write: boolean;
  grantor_user_id: string;
  revision: string;
  state: 'active' | 'revoked';
}
export interface SourceEnrollment {
  enrollment_id: string;
  coverage: 'exact' | 'all_current_future';
  project_id: string;
  profiles: string[];
  include_content: boolean;
  grantor_user_id: string;
  revision: string;
  state: 'active' | 'revoked';
  expires_at?: string | null;
}
export interface BindExistingCandidate {
  schema_version: string;
  installation_id: string;
  x_deployment_id: string;
  source_instance: string;
  org_id: string;
  project_id: string;
  enrollment_id: string;
  enrollment_revision: string;
  kind: 'facts' | 'content';
  connection_status: string;
  scope_generation: string;
  content_safety_version: string;
  connection_profiles: string[];
  content_status?: string;
  content_state_version?: string;
  content_access_generation?: string;
  state_sha256: string;
  keys: Array<{
    credential_id: string; status: string; key_prefix: string; allowed_ip: string;
    registration_sha256: string; profiles: string[]; policy_labels: string;
    coverage_after_sequence?: string; coverage_time?: string; allowed_purposes?: string[];
  }>;
}
export const xInsightMachineApi = {
  listCredentials: (orgId: number, installationId: string) =>
    request<{ data: MachineCredential[] }>(`/organizations/${orgId}/xinsight-policy-credentials?installation_id=${encodeURIComponent(installationId)}`, {}, 'v2'),
  registerCredential: (orgId: number, body: MachineCredentialRegistration) =>
    request<{ key_id: string; status: string }>(`/organizations/${orgId}/xinsight-policy-credentials`,
      { method: 'POST', body: JSON.stringify(body) }, 'v2'),
  changeCredential: (orgId: number, keyId: string, action: 'activate' | 'revoke') =>
    request<{ key_id: string; status: string }>(`/organizations/${orgId}/xinsight-policy-credentials/${keyId}/${action}`,
      { method: 'POST', body: '{}' }, 'v2'),
  listPolicyDelegations: (orgId: number, installationId: string, projectId?: number) =>
    request<{ data: PolicyDelegation[] }>(`/organizations/${orgId}/xinsight-policy-delegations?installation_id=${encodeURIComponent(installationId)}${projectId ? `&project_id=${projectId}` : ''}`, {}, 'v2'),
  createPolicyDelegation: (orgId: number, body: {
    installation_id: string; target_kind: PolicyDelegation['target_kind']; coverage: PolicyDelegation['coverage'];
    project_id?: string; user_id?: string; read: boolean; write: boolean;
  }) => request<{ delegation_id: string; revision: string }>(`/organizations/${orgId}/xinsight-policy-delegations`,
    { method: 'POST', body: JSON.stringify(body) }, 'v2'),
  revokePolicyDelegation: (orgId: number, grant: PolicyDelegation) =>
    request<{ delegation_id: string; revision: string }>(`/organizations/${orgId}/xinsight-policy-delegations/${grant.delegation_id}/revoke`,
      { method: 'POST', body: JSON.stringify({ expected_revision: grant.revision }) }, 'v2'),
  listSourceEnrollments: (orgId: number, installationId: string, projectId?: number) =>
    request<{ data: SourceEnrollment[] }>(`/organizations/${orgId}/xinsight-source-enrollments?installation_id=${encodeURIComponent(installationId)}${projectId ? `&project_id=${projectId}` : ''}`, {}, 'v2'),
  createSourceEnrollment: (orgId: number, body: {
    installation_id: string; coverage: SourceEnrollment['coverage']; project_id?: string;
    profiles: string[]; include_content: boolean;
  }) => request<{ enrollment_id: string; revision: string }>(`/organizations/${orgId}/xinsight-source-enrollments`,
    { method: 'POST', body: JSON.stringify(body) }, 'v2'),
  revokeSourceEnrollment: (orgId: number, grant: SourceEnrollment) =>
    request<{ enrollment_id: string; revision: string }>(`/organizations/${orgId}/xinsight-source-enrollments/${grant.enrollment_id}/revoke`,
      { method: 'POST', body: JSON.stringify({ expected_revision: grant.revision }) }, 'v2'),
  listBindExistingCandidates: (orgId: number, enrollmentId: string, projectId: number) =>
    request<{ data: BindExistingCandidate[] }>(
      `/organizations/${orgId}/xinsight-source-enrollments/${enrollmentId}/projects/${projectId}/bind-existing`, {}, 'v2'),
  bindExisting: (orgId: number, candidate: BindExistingCandidate, credentialId: string, operationId: string) =>
    request<{ status: string; credential_id: string }>(
      `/organizations/${orgId}/xinsight-source-enrollments/${candidate.enrollment_id}/projects/${candidate.project_id}/bind-existing`,
      { method: 'POST', body: JSON.stringify({ schema_version: 'unirouter.xinsight.bind-existing.v1', kind: candidate.kind,
        credential_id: credentialId, expected_state_sha256: candidate.state_sha256, operation_id: operationId }) }, 'v2'),
};
