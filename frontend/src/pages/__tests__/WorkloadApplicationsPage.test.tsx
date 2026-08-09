import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import WorkloadApplicationsPage from '../WorkloadApplicationsPage';
import { useAuthStore } from '../../store/auth';

const clientMocks = {
  getContract: vi.fn(), getProjects: vi.fn(), getApplications: vi.fn(), getCredentials: vi.fn(),
  createApplication: vi.fn(), updateApplication: vi.fn(), archiveApplication: vi.fn(),
};

const accessMocks = {
  listProjectRequests: vi.fn(), listProjectGrants: vi.fn(), createRequest: vi.fn(),
  issueCredential: vi.fn(), rotateCredential: vi.fn(), revokeCredential: vi.fn(), previewLegacy: vi.fn(),
};

vi.mock('../../api/client', () => ({
  ApiError: class ApiError extends Error { status: number; constructor(status: number, message: string) { super(message); this.status = status; } },
  api: {
    getWorkloadApplicationContract: (...args: any[]) => clientMocks.getContract(...args),
    getProjects: (...args: any[]) => clientMocks.getProjects(...args),
    getWorkloadApplications: (...args: any[]) => clientMocks.getApplications(...args),
    getWorkloadApplicationCredentials: (...args: any[]) => clientMocks.getCredentials(...args),
    createWorkloadApplication: (...args: any[]) => clientMocks.createApplication(...args),
    updateWorkloadApplication: (...args: any[]) => clientMocks.updateApplication(...args),
    archiveWorkloadApplication: (...args: any[]) => clientMocks.archiveApplication(...args),
  },
}));

vi.mock('../../api/access', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../api/access')>();
  return { ...actual, accessApi: {
    listProjectRequests: (...args: any[]) => accessMocks.listProjectRequests(...args),
    listProjectGrants: (...args: any[]) => accessMocks.listProjectGrants(...args),
    createRequest: (...args: any[]) => accessMocks.createRequest(...args),
    issueCredential: (...args: any[]) => accessMocks.issueCredential(...args),
    rotateCredential: (...args: any[]) => accessMocks.rotateCredential(...args),
    revokeCredential: (...args: any[]) => accessMocks.revokeCredential(...args),
    previewLegacy: (...args: any[]) => accessMocks.previewLegacy(...args),
  } };
});

const application = {
  id: 31, org_id: 1, project_id: 11, project_name: '理赔 POC', project_status: 'active',
  application_ref: 'app_claims_agent', contract_version: 'unirouter.workload-application.v2',
  identity_contract_version: 'unirouter.workload-identity.v2', logical_ref: 'workload_claims_agent',
  display_name: '理赔 Agent', kind: 'agent', environment: 'poc', production_use: false,
  identity_mode: 'service_credential', client_profiles: [], owner_team: 'AI 平台', description: '理赔助手',
  status: 'active', revision: 2, credential_count: 1, active_credential_count: 1,
  identity_classification: 'v2', can_manage: true, can_request_access: true, can_read_credentials: true,
};

describe('ApplicationsV2Page', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useAuthStore.setState({ token: 'token', isAuthenticated: true, profileLoading: false, user: { id: 1, username: 'admin', organization_role: 'platform_admin', role: 'platform_admin', org_id: 1, org_name: '总部' } });
    clientMocks.getContract.mockResolvedValue({
      contract_version: 'unirouter.workload-application.v2', readiness_version: 'unirouter.workload-readiness.v1',
      client_profiles: [{ profile_id: 'client.openai-sdk.chat.v1', display_name: 'OpenAI SDK', runtime_state: 'runtime_supported', supported_endpoints: ['/v1/chat/completions'], configuration_note: 'optional' }],
    });
    clientMocks.getProjects.mockResolvedValue({ data: [{ id: 11, org_id: 1, name: '理赔 POC', status: 'active', current_user_membership_role: 'project_admin' }], total: 1 });
    clientMocks.getApplications.mockResolvedValue({ data: [application], total: 1 });
    clientMocks.getCredentials.mockResolvedValue({ data: [{
      id: 81, credential_ref: 'cred_claims_agent', name: 'Claims runtime', status: 'active', effective_status: 'active',
      allowed_ips: '', allowed_models: [], allowed_endpoints: ['/v1/chat/completions'], rpm_limit: 60, rps_limit: 10,
      created_at: '2026-08-09T00:00:00Z', version_count: 1,
      versions: [{ id: 91, key_prefix: 'sk-agent', status: 'active', effective_status: 'active', created_at: '2026-08-09T00:00:00Z' }],
    }] });
    accessMocks.listProjectRequests.mockResolvedValue({ data: [], pagination: { limit: 50, has_more: false }, scope: 'project' });
    accessMocks.listProjectGrants.mockResolvedValue({ data: [{
      id: 71, grant_ref: 'grant_claims_agent', access_request_id: 61, org_id: 1, project_id: 11,
      subject: { type: 'workload_application', workload_application_id: 31 }, scope: { project_role: 'project_user', scope_mode: 'project_default', models: [], endpoints: [] },
      environment: 'poc', production_use: false, sponsor_user_id: 1, status: 'active', revision: 1,
      valid_from: '2026-08-09T00:00:00Z', policy_reason_codes: ['workload_project_default_auto'], decision_reason_codes: [], created_at: '2026-08-09T00:00:00Z', updated_at: '2026-08-09T00:00:00Z',
    }], pagination: { limit: 50, has_more: false }, scope: 'project' });
    accessMocks.previewLegacy.mockResolvedValue({ mutation: false, data: [{ object_type: 'workload_application', object_id: 44, object_ref: 'app_legacy', classification: 'review_required', reason_code: 'interactive_client_requires_review', migration_status: 'review_required', automatic_migration: false, client_profiles: [] }], total: 1 });
  });

  it('renders native v2 identity, optional profiles, access evidence and logical credential versions', async () => {
    render(<MemoryRouter initialEntries={['/applications?project_id=11&application_ref=app_claims_agent']}><WorkloadApplicationsPage /></MemoryRouter>);

    expect(await screen.findByRole('heading', { name: '应用与 Agent' })).toBeTruthy();
    expect((await screen.findAllByText('理赔 Agent')).length).toBeGreaterThan(0);
    expect(screen.getByText('workload_claims_agent · unirouter.workload-identity.v2')).toBeTruthy();
    expect(screen.getByText('可选，未配置')).toBeTruthy();
    expect(await screen.findByText('Claims runtime · cred_claims_agent')).toBeTruthy();
    expect(screen.getByText('sk-agent (active)')).toBeTruthy();
    expect(screen.getByRole('button', { name: '原子轮换' })).toBeTruthy();
    expect(screen.queryByText('interactive_client')).toBeNull();
    expect(screen.queryByText('personal_development')).toBeNull();
    expect(accessMocks.listProjectRequests).toHaveBeenCalledWith(11, { workload_application_id: 31, limit: 50 });
    expect(accessMocks.listProjectGrants).toHaveBeenCalledWith(11, { workload_application_id: 31, limit: 50 });
  });

  it('uses only the canonical read-only legacy preview and exposes no migration mutation', async () => {
    render(<MemoryRouter initialEntries={['/applications?project_id=11&tab=legacy']}><WorkloadApplicationsPage /></MemoryRouter>);

    expect(await screen.findByRole('tab', { name: 'Legacy 兼容区（只读）' })).toBeTruthy();
    expect(await screen.findByText('app_legacy')).toBeTruthy();
    expect(screen.getByText('interactive_client_requires_review')).toBeTruthy();
    expect(screen.getByText(/不自动迁移、不推断、不提供写操作/)).toBeTruthy();
    expect(accessMocks.previewLegacy).toHaveBeenCalledTimes(1);
    expect(clientMocks.createApplication).not.toHaveBeenCalled();
  });

  it('keeps Grant and credential facts when the independent Request source fails', async () => {
    accessMocks.listProjectRequests.mockRejectedValue(new Error('request store unavailable'));
    render(<MemoryRouter initialEntries={['/applications?project_id=11&application_ref=app_claims_agent']}><WorkloadApplicationsPage /></MemoryRouter>);

    expect(await screen.findByText('Request 暂时不可用；不会清空 Grant 或凭据事实。')).toBeTruthy();
    expect(await screen.findByText('Grant：grant_claims_agent (active)')).toBeTruthy();
    expect(await screen.findByText('Claims runtime · cred_claims_agent')).toBeTruthy();
  });

  it('opens a native form with only v2 workload kinds and immutable identity copy', async () => {
    render(<MemoryRouter initialEntries={['/applications?project_id=11']}><WorkloadApplicationsPage /></MemoryRouter>);

    const createLabel = await screen.findByText('新建 v2 Application');
    fireEvent.click(createLabel.closest('button') as HTMLButtonElement);
    expect((await screen.findAllByText('新建 v2 Application')).length).toBe(2);
    expect(screen.getByText('Environment（创建后不可变）')).toBeTruthy();
    expect(screen.queryByText('personal_development')).toBeNull();
    expect(screen.queryByText('interactive_client')).toBeNull();
    await waitFor(() => expect(screen.getByText('Client profiles（可选、多选）')).toBeTruthy());
  });

  it('keeps an actor-scoped project_user project discoverable without granting workload writes', async () => {
    useAuthStore.setState({
      token: 'token', isAuthenticated: true, profileLoading: false,
      user: { id: 1, username: 'hybrid', organization_role: 'member', role: 'project_admin', org_id: 1, org_name: '总部' },
    });
    clientMocks.getProjects.mockResolvedValue({
      data: [
        { id: 11, org_id: 1, name: 'Project A', status: 'active', current_user_membership_role: 'project_admin' },
        { id: 12, org_id: 1, name: 'Project B', status: 'active', current_user_membership_role: 'project_user' },
      ],
      total: 2,
    });
    clientMocks.getApplications.mockResolvedValue({ data: [], total: 0 });

    render(<MemoryRouter initialEntries={['/applications?project_id=12']}><WorkloadApplicationsPage /></MemoryRouter>);

    expect(await screen.findByText('Project B (#12)')).toBeTruthy();
    await waitFor(() => expect(clientMocks.getApplications).toHaveBeenCalledWith({ project_id: 12 }));
    expect((screen.getByText('新建 v2 Application').closest('button') as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText(/当前所选项目只有读取\/使用权限/)).toBeTruthy();
  });

  it('renders expired Grant state and prevents issue or rotation while preserving containment', async () => {
    accessMocks.listProjectGrants.mockResolvedValue({
      data: [{
        id: 71, grant_ref: 'grant_claims_agent', access_request_id: 61, org_id: 1, project_id: 11,
        subject: { type: 'workload_application', workload_application_id: 31 }, scope: { project_role: 'project_user', scope_mode: 'project_default', models: [], endpoints: [] },
        environment: 'poc', production_use: false, sponsor_user_id: 1, status: 'active', revision: 1,
        valid_from: '2026-08-09T00:00:00Z', valid_until: '2026-08-09T00:00:01Z', policy_reason_codes: ['workload_project_default_auto'], decision_reason_codes: [], created_at: '2026-08-09T00:00:00Z', updated_at: '2026-08-09T00:00:00Z',
      }],
      pagination: { limit: 50, has_more: false }, scope: 'project',
    });
    clientMocks.getCredentials.mockResolvedValue({ data: [{
      id: 81, credential_ref: 'cred_claims_agent', name: 'Claims runtime', status: 'active', effective_status: 'grant_expired',
      allowed_ips: '', allowed_models: [], allowed_endpoints: ['/v1/chat/completions'], rpm_limit: 60, rps_limit: 10,
      created_at: '2026-08-09T00:00:00Z', version_count: 1,
      versions: [{ id: 91, key_prefix: 'sk-agent', status: 'active', effective_status: 'grant_expired', created_at: '2026-08-09T00:00:00Z' }],
    }] });

    render(<MemoryRouter initialEntries={['/applications?project_id=11&application_ref=app_claims_agent']}><WorkloadApplicationsPage /></MemoryRouter>);

    expect(await screen.findByText('Grant：grant_claims_agent (expired)')).toBeTruthy();
    expect(screen.queryByRole('button', { name: '签发 Workload 凭据' })).toBeNull();
    const rotate = await screen.findByRole('button', { name: '原子轮换' }) as HTMLButtonElement;
    expect(rotate.disabled).toBe(true);
    expect((screen.getByRole('button', { name: '撤销' }) as HTMLButtonElement).disabled).toBe(false);
  });
});
