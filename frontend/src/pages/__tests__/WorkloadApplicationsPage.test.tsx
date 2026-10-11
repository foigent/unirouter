import { setLocale } from '../../i18n';
import { useLayoutEffect } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Modal, Toast } from '@douyinfe/semi-ui';
import { useLocation, useNavigate } from 'react-router-dom';
import { MemoryRouter } from '../../test/MemoryRouter';
import WorkloadApplicationsPage from '../WorkloadApplicationsPage';
import { useAuthStore } from '../../store/auth';
import { pasteValue } from '../../test/user';

const clientMocks = {
  getContract: vi.fn(), getProjects: vi.fn(), getApplications: vi.fn(), getCredentials: vi.fn(),
  getProjectMembers: vi.fn(),
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
    getProjectMembers: (...args: any[]) => clientMocks.getProjectMembers(...args),
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

async function chooseSelectOption(label: string, option: string, user = userEvent.setup()) {
  const combobox = screen.getByRole('combobox', { name: label });
  await user.click(combobox);
  await waitFor(() => expect(combobox).toHaveAttribute('aria-expanded', 'true'));
  const controlledID = combobox.getAttribute('aria-controls');
  if (!controlledID) throw new Error(`missing controlled option list for ${label}`);
  const currentChoice = () => Array.from(document.querySelectorAll<HTMLElement>('[id]'))
    .filter(node => node.id === controlledID)
    .flatMap(node => Array.from(node.querySelectorAll<HTMLElement>('[role="option"]')))
    .find(node => node.textContent === option);
  await waitFor(() => expect(currentChoice()).toBeTruthy());
  const choice = currentChoice();
  if (!choice) throw new Error(`missing option ${option} for ${label}`);
  const popup = choice.closest('[role="dialog"]');
  if (!popup) throw new Error(`missing option popup for ${label}`);
  await waitFor(() => expect(popup).toHaveClass('semi-tooltip-animation-show'));
  fireEvent.animationEnd(popup);
  await user.click(choice);
  await waitFor(() => expect(popup).toHaveClass('semi-tooltip-animation-hide'));
  fireEvent.animationEnd(popup);
  await waitFor(() => expect(combobox).toHaveTextContent(option));
}

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

function ApplicationTransitionProbe({ onApplicationBLayout }: { onApplicationBLayout: (snapshot: { text: string; credentialActions: string[] }) => void }) {
  const location = useLocation();
  useLayoutEffect(() => {
    const search = new URLSearchParams(location.search);
    if (search.get('application_ref') !== applicationSameProjectB.application_ref) return;
    onApplicationBLayout({
      text: document.body.textContent || '',
      credentialActions: Array.from(document.querySelectorAll('button'))
        .map((button) => button.textContent?.trim() || '')
        .filter((label) => ['创建应用 Key', '安全轮换', '撤销'].includes(label)),
    });
  }, [location.search, onApplicationBLayout]);
  return null;
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
  expect(await screen.findByText('cred-project-b runtime')).toBeTruthy();

  aRequests.resolve({ data: [], pagination: { limit: 50, has_more: false }, scope: 'project' });
  aGrants.resolve({ data: [grantFor(application.id, 11, 'grant-project-a')], pagination: { limit: 50, has_more: false }, scope: 'project' });
  await waitFor(() => expect(screen.queryByText('cred-project-a runtime')).toBeNull());
}

describe('ApplicationsV2Page', () => {
  it('preserves application drafts, scope and access data when switching language', async () => {
    const user = userEvent.setup();
    render(<MemoryRouter initialEntries={['/applications?project_id=11&application_ref=app_claims_agent']}><WorkloadApplicationsPage /></MemoryRouter>);
    await screen.findByText('Claims runtime');
    await user.click(screen.getByRole('button', { name: /新建应用/ }));
    await pasteValue(user, screen.getByLabelText('显示名称'), '服务草稿');
    const reads = [clientMocks.getProjects, clientMocks.getApplications, clientMocks.getCredentials, accessMocks.listProjectGrants].map(mock => mock.mock.calls.length);
    act(() => setLocale('en-US'));
    expect(screen.getByLabelText('Display name')).toHaveValue('服务草稿');
    expect(screen.getAllByText('理赔 Agent').length).toBeGreaterThan(0);
    [clientMocks.getProjects, clientMocks.getApplications, clientMocks.getCredentials, accessMocks.listProjectGrants].forEach((mock, i) => expect(mock).toHaveBeenCalledTimes(reads[i]));
    expect(clientMocks.createApplication).not.toHaveBeenCalled();
    expect(accessMocks.createRequest).not.toHaveBeenCalled();
    expect(accessMocks.issueCredential).not.toHaveBeenCalled();
  });

  afterEach(async () => {
    await act(async () => {
      setLocale('zh-CN');
      Modal.destroyAll();
      Toast.destroyAll();
      await Promise.resolve();
    });
  });

  beforeEach(() => {
    vi.clearAllMocks();
    useAuthStore.setState({ token: 'token', isAuthenticated: true, profileLoading: false, user: { id: 1, username: 'admin', organization_role: 'platform_admin', role: 'platform_admin', org_id: 99, org_name: '技术组织', customer_context: { state: 'ready', org_id: 1, org_name: '总部' } } });
    clientMocks.getContract.mockResolvedValue({
      contract_version: 'unirouter.workload-application.v2', readiness_version: 'unirouter.workload-readiness.v1',
      client_profiles: [{ profile_id: 'client.openai-sdk.chat.v1', display_name: 'OpenAI SDK', runtime_state: 'runtime_supported', supported_endpoints: ['/v1/chat/completions'], configuration_note: 'optional' }],
    });
    clientMocks.getProjects.mockResolvedValue({ data: [{ id: 11, org_id: 1, name: '理赔 POC', status: 'active', current_user_membership_role: 'project_admin' }], total: 1 });
    clientMocks.getApplications.mockResolvedValue({ data: [application], total: 1 });
    clientMocks.getProjectMembers.mockResolvedValue({ data: [
      { user_id: 40, username: 'customer-sponsor', status: 'active', membership_role: 'project_user' },
      { user_id: 41, username: 'disabled-member', status: 'disabled', membership_role: 'project_user' },
    ], total: 2 });
    clientMocks.createApplication.mockResolvedValue({
      ...application,
      id: 45,
      application_ref: 'app_generated_0123456789abcdef01234567',
      logical_ref: 'workload_generated_0123456789abcdef01234567',
      display_name: '新服务',
      kind: 'service',
      environment: 'development',
    });
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
    accessMocks.createRequest.mockResolvedValue({
      request: { request_ref: 'req-workload', status: 'approved', revision: 1 },
      grant: { grant_ref: 'grant-workload', status: 'active' },
    });
    accessMocks.rotateCredential.mockResolvedValue({ credential: { secret_available: false, recovery_action: 'rotate again' } });
    accessMocks.revokeCredential.mockResolvedValue({ status: 'revoked' });
  });

  it('renders native v2 identity, optional profiles, access evidence and logical credential versions', async () => {
    render(<MemoryRouter initialEntries={['/applications?project_id=11&application_ref=app_claims_agent']}><WorkloadApplicationsPage /></MemoryRouter>);

    expect(await screen.findByRole('heading', { name: '应用与 Agent' })).toBeTruthy();
    expect((await screen.findAllByText('理赔 Agent')).length).toBeGreaterThan(0);
    expect(screen.getByText('Logical identity: workload_claims_agent')).toBeTruthy();
    expect(screen.getByText('可选，未配置')).toBeTruthy();
    expect(await screen.findByText('Claims runtime')).toBeTruthy();
    expect(screen.getByText('sk-agent（可用）')).toBeTruthy();
    expect(screen.getByRole('button', { name: '安全轮换' })).toBeTruthy();
    expect(screen.queryByText('interactive_client')).toBeNull();
    expect(screen.queryByText('personal_development')).toBeNull();
    expect(accessMocks.listProjectRequests).toHaveBeenCalledWith(11, { workload_application_id: 31, limit: 50 });
    expect(accessMocks.listProjectGrants).toHaveBeenCalledWith(11, { workload_application_id: 31, limit: 50 });
  });

  it('uses only the canonical read-only legacy preview and exposes no migration mutation', async () => {
    render(<MemoryRouter initialEntries={['/applications?project_id=11&tab=legacy']}><WorkloadApplicationsPage /></MemoryRouter>);

    expect(await screen.findByRole('tab', { name: '旧版兼容（只读）' })).toBeTruthy();
    expect(await screen.findByText('app_legacy')).toBeTruthy();
    expect(screen.getByText('interactive_client_requires_review')).toBeTruthy();
    expect(screen.getByText(/不自动迁移、不推断、不提供写操作/)).toBeTruthy();
    expect(accessMocks.previewLegacy).toHaveBeenCalledTimes(1);
    expect(accessMocks.previewLegacy).toHaveBeenCalledWith(1);
    expect(clientMocks.createApplication).not.toHaveBeenCalled();
  });

  it('fails closed without a canonical customer and makes no customer-domain calls', async () => {
    useAuthStore.setState({
      token: 'token', isAuthenticated: true, profileLoading: false,
      user: { id: 1, username: 'admin', organization_role: 'platform_admin', role: 'platform_admin', org_id: 99, customer_context: { state: 'ambiguous' } },
    });

    render(<MemoryRouter initialEntries={['/applications?project_id=11&tab=legacy']}><WorkloadApplicationsPage /></MemoryRouter>);

    expect(await screen.findByText('客户组织上下文不唯一')).toBeTruthy();
    expect(clientMocks.getContract).not.toHaveBeenCalled();
    expect(clientMocks.getProjects).not.toHaveBeenCalled();
    expect(clientMocks.getApplications).not.toHaveBeenCalled();
    expect(accessMocks.previewLegacy).not.toHaveBeenCalled();
  });

  it('does not present missing projects or applications before the initial scope finishes loading', async () => {
    const contractRequest = deferred<any>();
    const projectsRequest = deferred<any>();
    clientMocks.getContract.mockReturnValue(contractRequest.promise);
    clientMocks.getProjects.mockReturnValue(projectsRequest.promise);

    render(<MemoryRouter initialEntries={['/applications']}><WorkloadApplicationsPage /></MemoryRouter>);

    expect(await screen.findByRole('heading', { name: '正在读取项目范围' })).toBeTruthy();
    expect(screen.getByRole('heading', { name: '正在准备应用范围' })).toBeTruthy();
    expect(screen.getByRole('button', { name: /新建应用/ })).toBeDisabled();
    expect(screen.queryByRole('heading', { name: '暂无可用项目' })).toBeNull();
    expect(screen.queryByRole('heading', { name: '当前项目暂无应用' })).toBeNull();

    await act(async () => {
      contractRequest.resolve({ contract_version: 'unirouter.workload-application.v2', client_profiles: [] });
      projectsRequest.resolve({ data: [], total: 0 });
      await Promise.resolve();
    });

    expect(await screen.findByRole('heading', { name: '暂无可用项目' })).toBeTruthy();
    expect(screen.getByRole('heading', { name: '没有可读取的项目应用' })).toBeTruthy();
  });

  it('keeps Grant and credential facts when the independent Request source fails', async () => {
    accessMocks.listProjectRequests.mockRejectedValue(new Error('request store unavailable'));
    render(<MemoryRouter initialEntries={['/applications?project_id=11&application_ref=app_claims_agent']}><WorkloadApplicationsPage /></MemoryRouter>);

    expect(await screen.findByText('访问申请暂时不可用；不会清空授权或 Key 的已知事实。')).toBeTruthy();
    expect(await screen.findByText('当前授权：已生效')).toBeTruthy();
    expect(await screen.findByText('Claims runtime')).toBeTruthy();
  });

  it('does not present an empty access state before all application facts finish loading', async () => {
    const requests = deferred<any>();
    const grants = deferred<any>();
    const credentials = deferred<any>();
    accessMocks.listProjectRequests.mockReturnValue(requests.promise);
    accessMocks.listProjectGrants.mockReturnValue(grants.promise);
    clientMocks.getCredentials.mockReturnValue(credentials.promise);

    render(<MemoryRouter initialEntries={['/applications?project_id=11&application_ref=app_claims_agent']}><WorkloadApplicationsPage /></MemoryRouter>);

    expect(await screen.findByText('正在读取访问状态…')).toBeTruthy();
    expect(screen.getByText('正在读取应用 Key')).toBeTruthy();
    expect(screen.queryByText('访问申请：未申请')).toBeNull();
    expect(screen.queryByText('当前授权：未授权')).toBeNull();
    expect(screen.queryByText('尚未创建应用 Key')).toBeNull();

    await act(async () => {
      requests.resolve({ data: [], pagination: { limit: 50, has_more: false }, scope: 'project' });
      grants.resolve({ data: [], pagination: { limit: 50, has_more: false }, scope: 'project' });
      await Promise.resolve();
    });
    await waitFor(() => expect(clientMocks.getCredentials).toHaveBeenCalled());
    expect(screen.getByText('正在读取应用 Key')).toBeTruthy();

    await act(async () => {
      credentials.resolve({ data: [], has_more: false });
      await Promise.resolve();
    });
    expect(await screen.findByText('访问申请：未申请')).toBeTruthy();
    expect(screen.getByText('当前授权：未授权')).toBeTruthy();
    expect(screen.getByText('尚未创建应用 Key')).toBeTruthy();
  });

  it('creates an application without asking the operator for internal identity refs', async () => {
    const user = userEvent.setup();
    render(<MemoryRouter initialEntries={['/applications?project_id=11']}><WorkloadApplicationsPage /></MemoryRouter>);

    await waitFor(() => expect(screen.getByRole('button', { name: /新建应用/ })).toBeEnabled());
    const createButton = screen.getByRole('button', { name: /新建应用/ });
    await user.click(createButton);
    expect((await screen.findAllByText('新建应用')).length).toBe(2);
    expect(screen.getByText('环境（创建后不可变）')).toBeTruthy();
    expect(screen.queryByLabelText(/Application ref|Logical ref/i)).toBeNull();
    expect(screen.queryByText('personal_development')).toBeNull();
    expect(screen.queryByText('interactive_client')).toBeNull();
    expect(screen.getByText('高级配置（可选）')).toBeTruthy();
    expect(screen.getByRole('combobox', { name: '应用类型' })).toBeTruthy();
    expect(screen.getByRole('combobox', { name: '环境（创建后不可变）' })).toBeTruthy();

    await pasteValue(user, screen.getByLabelText('显示名称'), '新服务');
    await user.click(screen.getByRole('button', { name: '保存' }));
    await waitFor(() => expect(clientMocks.createApplication).toHaveBeenCalledWith(expect.objectContaining({
      display_name: '新服务', kind: 'service', environment: 'development', production_use: false,
    })));
    const payload = clientMocks.createApplication.mock.calls[0][0];
    expect(payload).not.toHaveProperty('application_ref');
    expect(payload).not.toHaveProperty('logical_ref');
  });

  it('requires a platform administrator to select an active customer project Sponsor', async () => {
    const user = userEvent.setup();
    accessMocks.listProjectGrants.mockResolvedValue({ data: [], pagination: { limit: 50, has_more: false }, scope: 'project' });
    render(<MemoryRouter initialEntries={['/applications?project_id=11&application_ref=app_claims_agent']}><WorkloadApplicationsPage /></MemoryRouter>);

    await user.click(await screen.findByRole('button', { name: '申请应用访问' }));
    expect(await screen.findByText(/平台管理员身份不能替代业务主体/)).toBeTruthy();
    expect(clientMocks.getProjectMembers).toHaveBeenCalledWith(11, 1, 100);
    await chooseSelectOption('业务 Sponsor', 'customer-sponsor · project_user', user);
    await user.click(screen.getByRole('button', { name: '提交申请' }));

    await waitFor(() => expect(accessMocks.createRequest).toHaveBeenCalledWith(expect.objectContaining({
      org_id: 1,
      project_id: 11,
      subject: { type: 'workload_application', workload_application_id: 31 },
      sponsor_user_id: 40,
    }), expect.any(String)));
    expect(accessMocks.createRequest.mock.calls[0][0].sponsor_user_id).not.toBe(1);
  });

  it('keeps an in-organization administrator as the direct Sponsor', async () => {
    useAuthStore.setState({
      token: 'token', isAuthenticated: true, profileLoading: false,
      user: { id: 7, username: 'org-admin', organization_role: 'org_admin', role: 'admin', org_id: 1, org_name: '总部', customer_context: { state: 'ready', org_id: 1, org_name: '总部' } },
    });
    accessMocks.listProjectGrants.mockResolvedValue({ data: [], pagination: { limit: 50, has_more: false }, scope: 'project' });
    render(<MemoryRouter initialEntries={['/applications?project_id=11&application_ref=app_claims_agent']}><WorkloadApplicationsPage /></MemoryRouter>);

    fireEvent.click(await screen.findByRole('button', { name: '申请应用访问' }));
    await waitFor(() => expect(accessMocks.createRequest).toHaveBeenCalledWith(expect.objectContaining({ sponsor_user_id: 7 }), expect.any(String)));
    expect(screen.queryByRole('combobox', { name: '业务 Sponsor' })).toBeNull();
  });

  it('closes a frozen Sponsor decision when the selected Application changes and ignores the late member response', async () => {
    const user = userEvent.setup();
    const members = deferred<any>();
    clientMocks.getApplications.mockResolvedValue({ data: [application, applicationSameProjectB], total: 2 });
    clientMocks.getProjectMembers.mockReturnValue(members.promise);
    accessMocks.listProjectGrants.mockResolvedValue({ data: [], pagination: { limit: 50, has_more: false }, scope: 'project' });
    render(<MemoryRouter initialEntries={['/applications?project_id=11&application_ref=app_claims_agent']}><WorkloadApplicationsPage /><ScopeSwitches /></MemoryRouter>);

    await user.click(await screen.findByRole('button', { name: '申请应用访问' }));
    expect(await screen.findByText(/平台管理员身份不能替代业务主体/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'show-app-b-same-project' }));
    expect((await screen.findAllByText('Project B Agent')).length).toBeGreaterThan(0);
    members.resolve({ data: [{ user_id: 40, username: 'customer-sponsor', status: 'active', membership_role: 'project_user' }], total: 1 });

    await waitFor(() => expect(screen.getByRole('combobox', { name: '业务 Sponsor' })).toHaveAttribute('aria-disabled', 'true'));
    expect(screen.getByRole('button', { name: '提交申请' })).toBeDisabled();
    expect(accessMocks.createRequest).not.toHaveBeenCalled();
  });

  it('treats an archived application as read-only terminal history', async () => {
    clientMocks.getApplications.mockResolvedValue({
      data: [{ ...application, status: 'archived', can_manage: true, can_request_access: false }],
      total: 1,
    });
    render(<MemoryRouter initialEntries={['/applications?project_id=11&application_ref=app_claims_agent']}><WorkloadApplicationsPage /></MemoryRouter>);

    expect(await screen.findByText('已归档')).toBeTruthy();
    expect(screen.getByRole('button', { name: '编辑' })).toBeDisabled();
    expect(screen.getByRole('button', { name: '申请应用访问' })).toBeDisabled();
    expect(screen.getByRole('button', { name: '归档' })).toBeDisabled();
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

    expect(await screen.findByText('Project B')).toBeTruthy();
    await waitFor(() => expect(clientMocks.getApplications).toHaveBeenCalledWith({ project_id: 12 }));
    expect((screen.getByText('新建应用').closest('button') as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText(/当前所选项目只有读取或使用权限/)).toBeTruthy();
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

    expect(await screen.findByText('当前授权：已过期')).toBeTruthy();
    expect(screen.queryByRole('button', { name: '创建应用 Key' })).toBeNull();
    const rotate = await screen.findByRole('button', { name: '安全轮换' }) as HTMLButtonElement;
    expect(rotate.disabled).toBe(true);
    expect((screen.getByRole('button', { name: '撤销' }) as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(screen.getByRole('button', { name: '撤销' }));
    await waitFor(() => expect(screen.getAllByText('撤销应用 Key？').length).toBeGreaterThan(0));
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

    expect(await screen.findByText('cred-suspended runtime')).toBeTruthy();
    expect((screen.getByRole('button', { name: '安全轮换' }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole('button', { name: '撤销' }));
    await waitFor(() => expect(screen.getAllByText('撤销应用 Key？').length).toBeGreaterThan(0));
    const confirmButtons = screen.getAllByRole('button', { name: 'confirm' });
    fireEvent.click(confirmButtons[confirmButtons.length - 1]);

    await waitFor(() => expect(accessMocks.revokeCredential).toHaveBeenCalledWith('grant-suspended-family', 191, expect.any(String)));
    expect(accessMocks.revokeCredential).not.toHaveBeenCalledWith('grant-unrelated-active', expect.anything(), expect.anything());
  });

  it('disables destructive credential controls when the exact family identity is unavailable', async () => {
    clientMocks.getCredentials.mockResolvedValue({ data: [{ ...credentialFor('cred-missing-grant', '', 291), grant_ref: '' }], has_more: false });
    render(<MemoryRouter initialEntries={['/applications?project_id=11&application_ref=app_claims_agent']}><WorkloadApplicationsPage /></MemoryRouter>);

    expect(await screen.findByText('授权身份缺失')).toBeTruthy();
    expect((screen.getByRole('button', { name: '安全轮换' }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole('button', { name: '撤销' }) as HTMLButtonElement).disabled).toBe(true);
    expect(accessMocks.rotateCredential).not.toHaveBeenCalled();
    expect(accessMocks.revokeCredential).not.toHaveBeenCalled();
  });

  it('binds credential issue to the currently displayed Application after adverse access-response ordering', async () => {
    const user = userEvent.setup();
    await renderApplicationBAfterAdverseAccessOrdering();

    await user.click(screen.getByRole('button', { name: /创建应用 Key/ }));
    await pasteValue(user, await screen.findByLabelText('Key 名称'), 'project-b-runtime');
    await pasteValue(user, screen.getByLabelText('允许模型（可选）'), ' deepseek-v4-pro，deepseek-v4-pro ');
    await user.click(screen.getByRole('button', { name: '创建' }));

    await waitFor(() => expect(accessMocks.issueCredential).toHaveBeenCalledWith('grant-project-b', expect.objectContaining({
      name: 'project-b-runtime', allowed_models: ['deepseek-v4-pro'],
    }), expect.any(String)));
    expect(accessMocks.issueCredential).not.toHaveBeenCalledWith('grant-project-a', expect.anything(), expect.anything());
  }, 30_000);

  it('refreshes the application summary after issuing a credential', async () => {
    const user = userEvent.setup();
    const withoutCredential = { ...application, credential_count: 0, active_credential_count: 0 };
    clientMocks.getApplications
      .mockResolvedValueOnce({ data: [withoutCredential], total: 1 })
      .mockResolvedValue({ data: [application], total: 1 });
    clientMocks.getCredentials
      .mockResolvedValueOnce({ data: [], has_more: false })
      .mockResolvedValue({ data: [credentialFor('cred-issued', 'grant_claims_agent', 391)], has_more: false });

    render(<MemoryRouter initialEntries={['/applications?project_id=11&application_ref=app_claims_agent']}><WorkloadApplicationsPage /></MemoryRouter>);

    expect(await screen.findByText('0/0')).toBeTruthy();
    await user.click(await screen.findByRole('button', { name: /创建应用 Key/ }));
    await pasteValue(user, await screen.findByLabelText('Key 名称'), 'issued-runtime');
    await user.click(screen.getByRole('button', { name: '创建' }));

    await waitFor(() => expect(accessMocks.issueCredential).toHaveBeenCalled());
    expect(await screen.findByText('1/1')).toBeTruthy();
    expect(clientMocks.getApplications.mock.calls.length).toBeGreaterThanOrEqual(2);
  });

  it('reuses one credential intent for equivalent server-normalized model order after an uncertain response', async () => {
    const user = userEvent.setup();
    accessMocks.issueCredential
      .mockRejectedValueOnce(new Error('transport uncertain'))
      .mockResolvedValueOnce({ secret_available: false, recovery_action: 'rotate again' });
    render(<MemoryRouter initialEntries={['/applications?project_id=11&application_ref=app_claims_agent']}><WorkloadApplicationsPage /></MemoryRouter>);

    await user.click(await screen.findByRole('button', { name: /创建应用 Key/ }));
    await pasteValue(user, await screen.findByLabelText('Key 名称'), 'ordered-runtime');
    const models = screen.getByLabelText('允许模型（可选）');
    await pasteValue(user, models, 'zeta,deepseek-v4-pro');
    await user.click(screen.getByRole('button', { name: '创建' }));
    await waitFor(() => expect(accessMocks.issueCredential).toHaveBeenCalledTimes(1));

    await user.clear(models);
    await pasteValue(user, models, 'deepseek-v4-pro,zeta');
    await user.click(screen.getByRole('button', { name: '创建' }));
    await waitFor(() => expect(accessMocks.issueCredential).toHaveBeenCalledTimes(2));

    expect(accessMocks.issueCredential.mock.calls[0][1].allowed_models).toEqual(['deepseek-v4-pro', 'zeta']);
    expect(accessMocks.issueCredential.mock.calls[1][1].allowed_models).toEqual(['deepseek-v4-pro', 'zeta']);
    expect(accessMocks.issueCredential.mock.calls[0][2]).toBe(accessMocks.issueCredential.mock.calls[1][2]);
  });

  it('binds credential rotation to the currently displayed exact Grant after adverse response ordering', async () => {
    await renderApplicationBAfterAdverseAccessOrdering();

    fireEvent.click(screen.getByRole('button', { name: '安全轮换' }));
    await waitFor(() => expect(accessMocks.rotateCredential).toHaveBeenCalledWith('grant-project-b', 291, expect.any(String)));
    await waitFor(() => expect(clientMocks.getApplications.mock.calls.length).toBeGreaterThanOrEqual(2));
    expect(accessMocks.rotateCredential).not.toHaveBeenCalledWith('grant-project-a', expect.anything(), expect.anything());
  }, 30_000);

  it('binds credential revocation to the currently displayed exact Grant after adverse response ordering', async () => {
    await renderApplicationBAfterAdverseAccessOrdering();

    fireEvent.click(screen.getByRole('button', { name: '撤销' }));
    await waitFor(() => expect(screen.getAllByText('撤销应用 Key？').length).toBeGreaterThan(0));
    const confirmButtons = screen.getAllByRole('button', { name: 'confirm' });
    fireEvent.click(confirmButtons[confirmButtons.length - 1]);

    await waitFor(() => expect(accessMocks.revokeCredential).toHaveBeenCalledWith('grant-project-b', 291, expect.any(String)));
    await waitFor(() => expect(clientMocks.getApplications.mock.calls.length).toBeGreaterThanOrEqual(2));
    expect(accessMocks.revokeCredential).not.toHaveBeenCalledWith('grant-project-a', expect.anything(), expect.anything());
  }, 30_000);

  it('hides committed Application A access state during the same-project transition to Application B', async () => {
    const projectBRequests = deferred<any>();
    const projectBGrants = deferred<any>();
    const projectBCredentials = deferred<any>();
    const transitionSnapshots: Array<{ text: string; credentialActions: string[] }> = [];
    const recordTransition = vi.fn((snapshot: { text: string; credentialActions: string[] }) => transitionSnapshots.push(snapshot));

    clientMocks.getApplications.mockResolvedValue({ data: [application, applicationSameProjectB], total: 2 });
    accessMocks.listProjectRequests.mockImplementation((_projectID: number, filters: { workload_application_id: number }) => filters.workload_application_id === application.id
      ? Promise.resolve({ data: [], pagination: { limit: 50, has_more: false }, scope: 'project' })
      : projectBRequests.promise);
    accessMocks.listProjectGrants.mockImplementation((_projectID: number, filters: { workload_application_id: number }) => filters.workload_application_id === application.id
      ? Promise.resolve({ data: [grantFor(application.id, 11, 'grant-project-a')], pagination: { limit: 50, has_more: false }, scope: 'project' })
      : projectBGrants.promise);
    clientMocks.getCredentials.mockImplementation((applicationRef: string) => applicationRef === application.application_ref
      ? Promise.resolve({ data: [credentialFor('cred-project-a', 'grant-project-a', 191)], has_more: false })
      : projectBCredentials.promise);

    render(<MemoryRouter initialEntries={['/applications?project_id=11&application_ref=app_claims_agent']}><WorkloadApplicationsPage /><ScopeSwitches /><ApplicationTransitionProbe onApplicationBLayout={recordTransition} /></MemoryRouter>);
    expect(await screen.findByText('当前授权：已生效')).toBeTruthy();
    expect(await screen.findByText('cred-project-a runtime')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'show-app-b-same-project' }));
    expect(recordTransition).toHaveBeenCalledTimes(1);
    expect(transitionSnapshots[0].text).not.toContain('grant-project-a (active)');
    expect(transitionSnapshots[0].text).not.toContain('cred-project-a runtime');
    expect(transitionSnapshots[0].credentialActions).toEqual([]);
    expect(accessMocks.issueCredential).not.toHaveBeenCalled();
    expect(accessMocks.rotateCredential).not.toHaveBeenCalled();
    expect(accessMocks.revokeCredential).not.toHaveBeenCalled();

    projectBRequests.resolve({ data: [], pagination: { limit: 50, has_more: false }, scope: 'project' });
    projectBGrants.resolve({ data: [grantFor(applicationSameProjectB.id, 11, 'grant-project-b')], pagination: { limit: 50, has_more: false }, scope: 'project' });
    await waitFor(() => expect(clientMocks.getCredentials).toHaveBeenCalledWith(applicationSameProjectB.application_ref, 1, 1, 100));
    projectBCredentials.resolve({ data: [credentialFor('cred-project-b', 'grant-project-b', 291)], has_more: false });
    expect(await screen.findByText('grant-project-b (active)')).toBeTruthy();
    expect(await screen.findByText('cred-project-b runtime')).toBeTruthy();
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
    await waitFor(() => expect(screen.queryByText('Logical identity: workload_claims_agent')).toBeNull());
    expect(screen.getByText('Logical identity: workload_project_b_agent')).toBeTruthy();
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
    fireEvent.click(await screen.findByRole('button', { name: '编辑' }));
    fireEvent.click(screen.getByRole('button', { name: 'show-app-b' }));
    expect((await screen.findAllByText('Project B Agent')).length).toBeGreaterThan(0);
    const closingSheet = document.querySelector<HTMLElement>('.semi-sidesheet-inner');
    expect(closingSheet).toBeTruthy();
    await waitFor(() => expect(closingSheet).toHaveClass('semi-sidesheet-animation-content_hide_right'));
    fireEvent.animationEnd(closingSheet!);

    await waitFor(() => expect(screen.queryByRole('button', { name: '保存' })).toBeNull());
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
    await screen.findByText('归档应用？');
    fireEvent.click(screen.getByRole('button', { name: 'show-app-b' }));
    expect((await screen.findAllByText('Project B Agent')).length).toBeGreaterThan(0);
    const modalButtons = screen.getAllByRole('button');
    fireEvent.click(modalButtons[modalButtons.length - 1]);

    await waitFor(() => expect(clientMocks.archiveApplication).not.toHaveBeenCalled());
  }, 30_000);
});
