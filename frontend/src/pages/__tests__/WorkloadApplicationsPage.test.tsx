import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { Modal, Toast } from '@douyinfe/semi-ui';
import { MemoryRouter, useNavigate } from 'react-router-dom';
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

const applicationB = {
  ...application,
  id: 32,
  project_id: 12,
  project_name: 'Project B',
  application_ref: 'app_project_b_agent',
  logical_ref: 'workload_project_b_agent',
  display_name: 'Project B Agent',
};

const applicationSameProjectB = {
  ...applicationB,
  project_id: 11,
  project_name: '理赔 POC',
};

const grantFor = (applicationID: number, projectID: number, grantRef: string, status: 'active' | 'suspended' | 'revoked' = 'active') => ({
  id: applicationID + 40, grant_ref: grantRef, access_request_id: applicationID + 30, org_id: 1, project_id: projectID,
  subject: { type: 'workload_application', workload_application_id: applicationID },
  scope: { project_role: 'project_user', scope_mode: 'project_default', models: [], endpoints: [] },
  environment: 'poc', production_use: false, sponsor_user_id: 1, status, revision: 1,
  valid_from: '2026-08-09T00:00:00Z', policy_reason_codes: ['workload_project_default_auto'], decision_reason_codes: [],
  created_at: '2026-08-09T00:00:00Z', updated_at: '2026-08-09T00:00:00Z',
});

const credentialFor = (credentialRef: string, grantRef: string, keyID: number, effectiveStatus = 'active') => ({
  id: keyID - 10, credential_ref: credentialRef, grant_ref: grantRef, name: `${credentialRef} runtime`,
  status: 'active', effective_status: effectiveStatus, allowed_ips: '', allowed_models: [],
  allowed_endpoints: ['/v1/chat/completions'], rpm_limit: 60, rps_limit: 10,
  created_at: '2026-08-09T00:00:00Z', version_count: 1,
  versions: [{ id: keyID, key_prefix: `sk-${credentialRef}`, status: 'active', effective_status: effectiveStatus, created_at: '2026-08-09T00:00:00Z' }],
});

function ScopeSwitches() {
  const navigate = useNavigate();
  return <>
    <button onClick={() => navigate('/applications?project_id=11&application_ref=app_claims_agent')}>show-app-a</button>
    <button onClick={() => navigate('/applications?project_id=11&application_ref=app_project_b_agent')}>show-app-b-same-project</button>
    <button onClick={() => navigate('/applications?project_id=12&application_ref=app_project_b_agent')}>show-app-b</button>
  </>;
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((complete) => { resolve = complete; });
  return { promise, resolve };
}

async function renderApplicationBAfterAdverseAccessOrdering() {
  const aRequests = deferred<any>();
  const aGrants = deferred<any>();
  clientMocks.getApplications.mockResolvedValue({ data: [application, applicationSameProjectB], total: 2 });
  accessMocks.listProjectRequests.mockImplementation((_projectID: number, filters: { workload_application_id: number }) => filters.workload_application_id === application.id
    ? aRequests.promise
    : Promise.resolve({ data: [], pagination: { limit: 50, has_more: false }, scope: 'project' }));
  accessMocks.listProjectGrants.mockImplementation((_projectID: number, filters: { workload_application_id: number }) => filters.workload_application_id === application.id
    ? aGrants.promise
    : Promise.resolve({ data: [grantFor(applicationSameProjectB.id, 11, 'grant-project-b')], pagination: { limit: 50, has_more: false }, scope: 'project' }));
  clientMocks.getCredentials.mockImplementation((applicationRef: string) => Promise.resolve({
    data: applicationRef === application.application_ref
      ? [credentialFor('cred-project-a', 'grant-project-a', 191)]
      : [credentialFor('cred-project-b', 'grant-project-b', 291)],
    has_more: false,
  }));

  render(<MemoryRouter initialEntries={['/applications?project_id=11&application_ref=app_claims_agent']}><WorkloadApplicationsPage /><ScopeSwitches /></MemoryRouter>);
  await waitFor(() => expect(accessMocks.listProjectGrants).toHaveBeenCalledWith(11, expect.objectContaining({ workload_application_id: application.id })));
  fireEvent.click(screen.getByRole('button', { name: 'show-app-b-same-project' }));
  expect(await screen.findByText('cred-project-b runtime · cred-project-b')).toBeTruthy();

  aRequests.resolve({ data: [], pagination: { limit: 50, has_more: false }, scope: 'project' });
  aGrants.resolve({ data: [grantFor(application.id, 11, 'grant-project-a')], pagination: { limit: 50, has_more: false }, scope: 'project' });
  await waitFor(() => expect(screen.queryByText('cred-project-a runtime · cred-project-a')).toBeNull());
}

describe('ApplicationsV2Page', () => {
  afterEach(() => { Modal.destroyAll(); Toast.destroyAll(); });

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
      id: 81, credential_ref: 'cred_claims_agent', grant_ref: 'grant_claims_agent', name: 'Claims runtime', status: 'active', effective_status: 'active',
      allowed_ips: '', allowed_models: [], allowed_endpoints: ['/v1/chat/completions'], rpm_limit: 60, rps_limit: 10,
      created_at: '2026-08-09T00:00:00Z', version_count: 1,
      versions: [{ id: 91, key_prefix: 'sk-agent', status: 'active', effective_status: 'active', created_at: '2026-08-09T00:00:00Z' }],
    }], has_more: false });
    accessMocks.listProjectRequests.mockResolvedValue({ data: [], pagination: { limit: 50, has_more: false }, scope: 'project' });
    accessMocks.listProjectGrants.mockResolvedValue({ data: [{
      id: 71, grant_ref: 'grant_claims_agent', access_request_id: 61, org_id: 1, project_id: 11,
      subject: { type: 'workload_application', workload_application_id: 31 }, scope: { project_role: 'project_user', scope_mode: 'project_default', models: [], endpoints: [] },
      environment: 'poc', production_use: false, sponsor_user_id: 1, status: 'active', revision: 1,
      valid_from: '2026-08-09T00:00:00Z', policy_reason_codes: ['workload_project_default_auto'], decision_reason_codes: [], created_at: '2026-08-09T00:00:00Z', updated_at: '2026-08-09T00:00:00Z',
    }], pagination: { limit: 50, has_more: false }, scope: 'project' });
    accessMocks.previewLegacy.mockResolvedValue({ mutation: false, data: [{ object_type: 'workload_application', object_id: 44, object_ref: 'app_legacy', classification: 'review_required', reason_code: 'interactive_client_requires_review', migration_status: 'review_required', automatic_migration: false, client_profiles: [] }], total: 1 });
    accessMocks.issueCredential.mockResolvedValue({ secret_available: false, recovery_action: 'rotate again' });
    accessMocks.rotateCredential.mockResolvedValue({ credential: { secret_available: false, recovery_action: 'rotate again' } });
    accessMocks.revokeCredential.mockResolvedValue({ status: 'revoked' });
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
      id: 81, credential_ref: 'cred_claims_agent', grant_ref: 'grant_claims_agent', name: 'Claims runtime', status: 'active', effective_status: 'grant_expired',
      allowed_ips: '', allowed_models: [], allowed_endpoints: ['/v1/chat/completions'], rpm_limit: 60, rps_limit: 10,
      created_at: '2026-08-09T00:00:00Z', version_count: 1,
      versions: [{ id: 91, key_prefix: 'sk-agent', status: 'active', effective_status: 'grant_expired', created_at: '2026-08-09T00:00:00Z' }],
    }], has_more: false });

    render(<MemoryRouter initialEntries={['/applications?project_id=11&application_ref=app_claims_agent']}><WorkloadApplicationsPage /></MemoryRouter>);

    expect(await screen.findByText('Grant：grant_claims_agent (expired)')).toBeTruthy();
    expect(screen.queryByRole('button', { name: '签发 Workload 凭据' })).toBeNull();
    const rotate = await screen.findByRole('button', { name: '原子轮换' }) as HTMLButtonElement;
    expect(rotate.disabled).toBe(true);
    expect((screen.getByRole('button', { name: '撤销' }) as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(screen.getByRole('button', { name: '撤销' }));
    await waitFor(() => expect(screen.getAllByText('撤销整个逻辑凭据？').length).toBeGreaterThan(0));
    const confirmButtons = screen.getAllByRole('button', { name: 'confirm' });
    fireEvent.click(confirmButtons[confirmButtons.length - 1]);
    await waitFor(() => expect(accessMocks.revokeCredential).toHaveBeenCalledWith('grant_claims_agent', 91, expect.any(String)));
  });

  it('uses each credential family exact Grant for suspended containment in a multiple-Grant response', async () => {
    accessMocks.listProjectGrants.mockResolvedValue({
      data: [grantFor(31, 11, 'grant-suspended-family', 'suspended'), grantFor(99, 11, 'grant-unrelated-active')],
      pagination: { limit: 50, has_more: false }, scope: 'project',
    });
    clientMocks.getCredentials.mockResolvedValue({ data: [credentialFor('cred-suspended', 'grant-suspended-family', 191, 'grant_suspended')], has_more: false });
    accessMocks.revokeCredential.mockResolvedValue({ status: 'revoked' });

    render(<MemoryRouter initialEntries={['/applications?project_id=11&application_ref=app_claims_agent']}><WorkloadApplicationsPage /></MemoryRouter>);

    expect(await screen.findByText('cred-suspended runtime · cred-suspended')).toBeTruthy();
    expect((screen.getByRole('button', { name: '原子轮换' }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole('button', { name: '撤销' }));
    await waitFor(() => expect(screen.getAllByText('撤销整个逻辑凭据？').length).toBeGreaterThan(0));
    const confirmButtons = screen.getAllByRole('button', { name: 'confirm' });
    fireEvent.click(confirmButtons[confirmButtons.length - 1]);

    await waitFor(() => expect(accessMocks.revokeCredential).toHaveBeenCalledWith('grant-suspended-family', 191, expect.any(String)));
    expect(accessMocks.revokeCredential).not.toHaveBeenCalledWith('grant-unrelated-active', expect.anything(), expect.anything());
  });

  it('disables destructive credential controls when the exact family identity is unavailable', async () => {
    clientMocks.getCredentials.mockResolvedValue({ data: [{ ...credentialFor('cred-missing-grant', '', 291), grant_ref: '' }], has_more: false });
    render(<MemoryRouter initialEntries={['/applications?project_id=11&application_ref=app_claims_agent']}><WorkloadApplicationsPage /></MemoryRouter>);

    expect(await screen.findByText('exact Grant identity unavailable')).toBeTruthy();
    expect((screen.getByRole('button', { name: '原子轮换' }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole('button', { name: '撤销' }) as HTMLButtonElement).disabled).toBe(true);
    expect(accessMocks.rotateCredential).not.toHaveBeenCalled();
    expect(accessMocks.revokeCredential).not.toHaveBeenCalled();
  });

  it('binds credential issue to the currently displayed Application after adverse access-response ordering', async () => {
    await renderApplicationBAfterAdverseAccessOrdering();

    fireEvent.click(screen.getByRole('button', { name: /签发 Workload 凭据/ }));
    fireEvent.change(await screen.findByLabelText('凭据名称'), { target: { value: 'project-b-runtime' } });
    fireEvent.click(screen.getByRole('button', { name: '签发' }));

    await waitFor(() => expect(accessMocks.issueCredential).toHaveBeenCalledWith('grant-project-b', expect.objectContaining({ name: 'project-b-runtime' }), expect.any(String)));
    expect(accessMocks.issueCredential).not.toHaveBeenCalledWith('grant-project-a', expect.anything(), expect.anything());
  }, 30_000);

  it('binds credential rotation to the currently displayed exact Grant after adverse response ordering', async () => {
    await renderApplicationBAfterAdverseAccessOrdering();

    fireEvent.click(screen.getByRole('button', { name: '原子轮换' }));
    await waitFor(() => expect(accessMocks.rotateCredential).toHaveBeenCalledWith('grant-project-b', 291, expect.any(String)));
    expect(accessMocks.rotateCredential).not.toHaveBeenCalledWith('grant-project-a', expect.anything(), expect.anything());
  }, 30_000);

  it('binds credential revocation to the currently displayed exact Grant after adverse response ordering', async () => {
    await renderApplicationBAfterAdverseAccessOrdering();

    fireEvent.click(screen.getByRole('button', { name: '撤销' }));
    await waitFor(() => expect(screen.getAllByText('撤销整个逻辑凭据？').length).toBeGreaterThan(0));
    const confirmButtons = screen.getAllByRole('button', { name: 'confirm' });
    fireEvent.click(confirmButtons[confirmButtons.length - 1]);

    await waitFor(() => expect(accessMocks.revokeCredential).toHaveBeenCalledWith('grant-project-b', 291, expect.any(String)));
    expect(accessMocks.revokeCredential).not.toHaveBeenCalledWith('grant-project-a', expect.anything(), expect.anything());
  }, 30_000);

  it('ignores a late prior-project Application list before update, archive, or access sinks become reachable', async () => {
    const projectA = deferred<{ data: typeof application[]; total: number }>();
    clientMocks.getProjects.mockResolvedValue({
      data: [
        { id: 11, org_id: 1, name: 'Project A', status: 'active', current_user_membership_role: 'project_admin' },
        { id: 12, org_id: 1, name: 'Project B', status: 'active', current_user_membership_role: 'project_admin' },
      ], total: 2,
    });
    clientMocks.getApplications.mockImplementation(({ project_id }: { project_id: number }) => project_id === 11
      ? projectA.promise
      : Promise.resolve({ data: [applicationB], total: 1 }));
    accessMocks.listProjectRequests.mockResolvedValue({ data: [], pagination: { limit: 50, has_more: false }, scope: 'project' });
    accessMocks.listProjectGrants.mockResolvedValue({ data: [], pagination: { limit: 50, has_more: false }, scope: 'project' });
    clientMocks.getCredentials.mockResolvedValue({ data: [], has_more: false });

    render(<MemoryRouter initialEntries={['/applications?project_id=11&application_ref=app_claims_agent']}><WorkloadApplicationsPage /><ScopeSwitches /></MemoryRouter>);
    await waitFor(() => expect(clientMocks.getApplications).toHaveBeenCalledWith({ project_id: 11 }));
    fireEvent.click(screen.getByRole('button', { name: 'show-app-b' }));
    expect((await screen.findAllByText('Project B Agent')).length).toBeGreaterThan(0);

    projectA.resolve({ data: [application], total: 1 });
    await waitFor(() => expect(screen.queryByText('workload_claims_agent · unirouter.workload-identity.v2')).toBeNull());
    expect(screen.getByText('workload_project_b_agent · unirouter.workload-identity.v2')).toBeTruthy();
    expect(clientMocks.updateApplication).not.toHaveBeenCalled();
    expect(clientMocks.archiveApplication).not.toHaveBeenCalled();
    expect(accessMocks.createRequest).not.toHaveBeenCalled();
  }, 30_000);

  it('invalidates an open edit action when the project/Application scope changes', async () => {
    clientMocks.getProjects.mockResolvedValue({ data: [
      { id: 11, org_id: 1, name: 'Project A', status: 'active', current_user_membership_role: 'project_admin' },
      { id: 12, org_id: 1, name: 'Project B', status: 'active', current_user_membership_role: 'project_admin' },
    ], total: 2 });
    clientMocks.getApplications.mockImplementation(({ project_id }: { project_id: number }) => Promise.resolve({ data: [project_id === 11 ? application : applicationB], total: 1 }));

    render(<MemoryRouter initialEntries={['/applications?project_id=11&application_ref=app_claims_agent']}><WorkloadApplicationsPage /><ScopeSwitches /></MemoryRouter>);
    fireEvent.click(await screen.findByRole('button', { name: '编辑元数据' }));
    fireEvent.click(screen.getByRole('button', { name: 'show-app-b' }));
    expect((await screen.findAllByText('Project B Agent')).length).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole('button', { name: '保存' }));

    await waitFor(() => expect(clientMocks.updateApplication).not.toHaveBeenCalled());
  }, 30_000);

  it('invalidates an archive confirmation when the project/Application scope changes', async () => {
    clientMocks.getProjects.mockResolvedValue({ data: [
      { id: 11, org_id: 1, name: 'Project A', status: 'active', current_user_membership_role: 'project_admin' },
      { id: 12, org_id: 1, name: 'Project B', status: 'active', current_user_membership_role: 'project_admin' },
    ], total: 2 });
    clientMocks.getApplications.mockImplementation(({ project_id }: { project_id: number }) => Promise.resolve({ data: [project_id === 11 ? application : applicationB], total: 1 }));

    render(<MemoryRouter initialEntries={['/applications?project_id=11&application_ref=app_claims_agent']}><WorkloadApplicationsPage /><ScopeSwitches /></MemoryRouter>);
    fireEvent.click(await screen.findByRole('button', { name: '归档' }));
    await screen.findByText('归档 Workload Application？');
    fireEvent.click(screen.getByRole('button', { name: 'show-app-b' }));
    expect((await screen.findAllByText('Project B Agent')).length).toBeGreaterThan(0);
    const modalButtons = screen.getAllByRole('button');
    fireEvent.click(modalButtons[modalButtons.length - 1]);

    await waitFor(() => expect(clientMocks.archiveApplication).not.toHaveBeenCalled());
  }, 30_000);
});
