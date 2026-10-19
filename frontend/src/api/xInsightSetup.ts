import { request } from './transport';
import type { ScopedPayloadPolicy, ScopedPayloadPut } from './contentManagement';
import type { FrozenConnectorInvitationV1 } from '../pages/XInsightLiveConnection';

export interface SetupPeer {
  schema_version: 'unirouter.xinsight.setup-peer.v1';
  org_id: string; anchor_project_id: string; source_instance: string;
  xi_origin: string; xi_deployment_id: string; xi_setup_url: string; trust_ready: boolean;
}
export interface SetupCustody {
  mode: 'disabled' | 'full_content'; retention_seconds: number;
  project_quota_bytes: number; max_item_bytes: number;
}
export interface SetupIssue { code: string; requires_choice: boolean }
export type SetupCoverage = 'exact' | 'all_current_future';
export interface SetupPolicyAccess {
  target_kind: 'enterprise_default' | 'project_policy' | 'user_exception';
  coverage: SetupCoverage; read: boolean; write: boolean; user_id?: string;
}
export interface SetupXIConsent {
  schema_version: 'xinsight.connector-setup-consent-view.v1';
  installation_id: string; deployment_id: string; xi_origin: string; ur_origin: string;
  consent_revision: number; consent_sha256: string; expires_at: string;
  custody: SetupCustody; expected_custody_revision: number;
}
export interface SetupPreview {
  schema_version: 'unirouter.xinsight.setup-preview.v1'; preview_sha256: string;
  installation_id: string; invitation: FrozenConnectorInvitationV1; xi_consent: SetupXIConsent;
  available_profiles: string[]; default_profiles: string[]; existing_profiles: string[];
  content_available: boolean; receiver_mode: 'new_install' | 'adopt_existing';
  existing_binding_sha256: string | null; ur_policy: ScopedPayloadPolicy;
  allowed_coverage: SetupCoverage[]; allowed_policy_scopes: ('enterprise' | 'project')[];
  allowed_policy_access: SetupPolicyAccess[]; issues: SetupIssue[];
}
export interface SetupConsentPut {
  schema_version: 'unirouter.xinsight.setup-consent.v1'; request_id: string; project_id: string;
  invitation: FrozenConnectorInvitationV1; expected_preview_sha256: string; expected_xi_consent_sha256: string;
  receiver_mode: SetupPreview['receiver_mode']; existing_binding_sha256: string | null;
  coverage: SetupCoverage; excluded_project_ids: string[]; profiles: string[]; include_content: boolean;
  policy_access: SetupPolicyAccess[]; ur_policy: { scope: 'enterprise' | 'project'; put: ScopedPayloadPut } | null;
}
export interface SetupXIStatus {
  schema_version: 'xinsight.connector-setup-status.v1'; installation_id: string; deployment_id: string;
  consent_revision: number; state: SetupState;
  custody: { desired_revision: number; effective_revision: number; loaded: boolean; settings: SetupCustody | null };
  receiving: { last_success_at: string | null; profiles_received: string[]; content_received_at: string | null; content_read_verified_at: string | null };
  issues: SetupIssue[];
}
export type SetupState = 'waiting_consent' | 'provisioning' | 'needs_action' | 'waiting_data' | 'ready' | 'failed';
export interface SetupStatus {
  schema_version: 'unirouter.xinsight.setup-status.v1'; installation_id: string; consent_revision: number;
  state: SetupState; configuration_ready: boolean;
  purpose_status: { policy_manage: string; source_enroll: string };
  ur_policy_status: { state: 'pending' | 'effective' | 'blocked'; policy_ref: string; policy_version: string; version_vector: string };
  xi_peer_state?: 'unknown' | 'last_observed'; xi_observed_at?: string;
  xi: SetupXIStatus | null; issues: SetupIssue[];
  credential?: CredentialLocalStatus;
}
export interface CredentialLocalStatus {
  state: 'healthy' | 'rotation_due' | 'rotation_pending' | 'committed_unpromoted' | 'expired_recovery_required' | 'revoked' | 'credential_unavailable';
  current_token_sha256: string; current_expires_at: string;
  pending_operation_id?: string; pending_state?: string; pending_replacement_sha256?: string;
}
export interface CredentialRecoveryPreview {
  schema_version: 'unirouter.xinsight.credential-recovery-preview.v1';
  installation_id: string; deployment_id: string; project_id: string;
  final_event_scope: string[]; source_binding_sha256: string;
  xinsight_origin: string; unirouter_origin: string; recovery_id: string;
  expires_at: string; credential: CredentialLocalStatus;
}
export interface CredentialRecoveryScope {
  schema_version: 'unirouter.xinsight.credential-recovery-scope.v1';
  installation_id: string; org_id: number;
}
export interface CredentialRecoveryAccepted {
  schema_version: 'unirouter.xinsight.credential-recovery-accepted.v1';
  installation_id: string; operation_id: string; state: 'pending';
}
export interface SetupProjectMember { user_id: number; username: string; status: string }
export const xInsightSetupApi = {
  members: (project: number, page: number, pageSize: number) => request<{ data: SetupProjectMember[]; total: number }>(`/projects/${project}/members?page=${page}&page_size=${pageSize}`),
  peer: (org: number, project: number) => request<SetupPeer>(`/organizations/${org}/xinsight/setup-peer?project_id=${project}`, {}, 'v2'),
  preview: (org: number, project: number, invitationToken: string) => request<SetupPreview>(
    `/organizations/${org}/xinsight/setup-previews`, { method: 'POST', body: JSON.stringify({ project_id: String(project), invitation_token: invitationToken }) }, 'v2'),
  create: (org: number, put: SetupConsentPut) => request<SetupStatus>(
    `/organizations/${org}/xinsight/setups`, { method: 'POST', body: JSON.stringify(put) }, 'v2'),
  status: (org: number, installation: string) => request<SetupStatus>(
    `/organizations/${org}/xinsight/setups/${encodeURIComponent(installation)}`, {}, 'v2'),
  recoveryScope: (installation: string) => request<CredentialRecoveryScope>(
    `/xinsight/setups/${encodeURIComponent(installation)}/credential-recovery-scope`, {}, 'v2'),
  recoveryPreview: (org: number, installation: string, ticket: string) => request<CredentialRecoveryPreview>(
    `/organizations/${org}/xinsight/setups/${encodeURIComponent(installation)}/credential-recovery/preview`,
    { method: 'POST', body: JSON.stringify({recovery_ticket: ticket}) }, 'v2'),
  recoveryConfirm: (org: number, installation: string, ticket: string) => request<CredentialRecoveryAccepted>(
    `/organizations/${org}/xinsight/setups/${encodeURIComponent(installation)}/credential-recovery`,
    { method: 'POST', body: JSON.stringify({recovery_ticket: ticket}) }, 'v2'),
};
