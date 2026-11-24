import { setLocale } from '../../i18n';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { StrictMode } from 'react';
import ProjectManagementPage from '../ProjectManagementPage';
import { useAuthStore } from '../../store/auth';

const mocks = vi.hoisted(() => ({
  getProjects: vi.fn(),
  getOrganizations: vi.fn(),
  createProject: vi.fn(),
  updateProject: vi.fn(),
  getProjectMembers: vi.fn(),
  upsertProjectMember: vi.fn(),
  deleteProjectMember: vi.fn(),
  getModelSets: vi.fn(),
  getProjectModelSets: vi.fn(),
  replaceProjectModelSets: vi.fn(),
}));

vi.mock('../../api/client', async () => {
  const actual = await vi.importActual<any>('../../api/client');
  return { ...actual, api: mocks };
});

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((complete) => { resolve = complete; });
  return { promise, resolve };
}

function projectRecord(id: number, name: string, orgId = 1) {
  return { id, org_id: orgId, name, slug: `project-${id}`, status: 'active', budget_cents: 0, budget_period: 'none' };
}

function memberResponse(username: string, total = 120) {
  return {
    data: [{ user_id: 9, username, auth_provider: 'local', membership_role: 'project_user', status: 'active' }],
    eligible_users: [],
    total,
  };
}

describe('ProjectManagementPage', () => {
  it('keeps the project, customer names and unsaved edits when the locale changes', async () => {
    render(<ProjectManagementPage />);
    const input = await screen.findByRole('textbox', { name: '项目名称' });
    await waitFor(() => expect(input).toHaveValue('核心项目'));
    await act(async () => { fireEvent.change(input, { target: { value: '核心项目 · draft' } }); });
    expect(input).toHaveValue('核心项目 · draft');
    const reads = mocks.getProjects.mock.calls.length;
    const memberReads = mocks.getProjectMembers.mock.calls.length;
    await act(async () => setLocale('en-US'));
    expect(screen.getByRole('textbox', { name: 'Project name' })).toHaveValue('核心项目 · draft');
    expect(screen.getByLabelText('Project inspector')).toBeTruthy();
    expect(new URLSearchParams(window.location.search).get('project_id')).toBe('31');
    expect(mocks.getProjects).toHaveBeenCalledTimes(reads);
    expect(mocks.getProjectMembers).toHaveBeenCalledTimes(memberReads);
    expect(mocks.updateProject).not.toHaveBeenCalled();
    expect(mocks.upsertProjectMember).not.toHaveBeenCalled();
    await act(async () => setLocale('zh-CN'));
    expect(screen.getByRole('textbox', { name: '项目名称' })).toHaveValue('核心项目 · draft');
  });

  afterEach(async () => { await act(async () => setLocale('zh-CN')); });
  beforeEach(() => {
    vi.clearAllMocks();
    window.localStorage.clear();
    window.history.replaceState(null, '', '/projects');
    useAuthStore.setState({
      token: 'token',
      isAuthenticated: true,
      profileLoading: false,
      profileError: null,
      user: {
        id: 2,
        username: 'org-admin',
        organization_role: 'org_admin',
        role: 'org_admin',
        org_id: 1,
        org_name: '总部',
        license_access: { status: 'valid', enforceable: true, features: { gateway: true, agent_capabilities: true, oidc: true, guardrails: true, notifications: true, model_governance: true, data_exchange: true } },
      },
    });
    mocks.getProjects.mockResolvedValue({
      data: [{
        id: 31,
        org_id: 1,
        org_name: '总部',
        name: '核心项目',
        slug: 'core',
        description: '核心服务',
        status: 'active',
        budget_cents: 123400,
        budget_period: 'monthly',
        key_count: 2,
      }],
      total: 1,
    });
    mocks.getProjectMembers.mockResolvedValue({
      data: [{ user_id: 9, username: 'developer-a', auth_provider: 'local', user_role: 'developer', membership_role: 'project_developer', status: 'active' }],
      total: 1,
      eligible_users: [],
    });
    mocks.getModelSets.mockResolvedValue({ data: [], total: 0 });
    mocks.getProjectModelSets.mockResolvedValue({ data: [], model_set_ids: [] });
  });

  it('keeps project details, members and licensed model scope in one second-level workspace', async () => {
    render(<ProjectManagementPage />);

    expect(await screen.findByText('项目与成员')).toBeTruthy();
    expect(screen.queryByText(/第三级菜单/)).toBeNull();
    expect(mocks.getProjects).toHaveBeenCalledWith(1, 20, { org_id: undefined });
    const detail = await screen.findByLabelText('项目详情：核心项目');
    expect(detail).toBeTruthy();
    expect(screen.getByLabelText('项目工作区检查器')).toBeTruthy();
    expect(screen.queryByRole('button', { name: '查看与管理' })).toBeNull();
    expect(new URLSearchParams(window.location.search).get('project_id')).toBe('31');
    expect(await screen.findByText('项目概况')).toBeTruthy();
    expect(screen.getByText('成员与角色')).toBeTruthy();
    expect(screen.getByText('模型范围')).toBeTruthy();
    expect(screen.getByRole('textbox', { name: '项目名称' })).toBeTruthy();
    expect(screen.getByRole('combobox', { name: '项目状态' })).toBeTruthy();
    expect(screen.getByRole('combobox', { name: '预算周期' })).toBeTruthy();
    expect(screen.queryByLabelText('项目工作区名称')).toBeNull();

    fireEvent.click(screen.getByText('成员与角色'));
    expect(await screen.findByText('developer-a')).toBeTruthy();
    expect(mocks.getProjectMembers).toHaveBeenCalledWith(31, 1, 20, '');
    expect(screen.getByRole('textbox', { name: '搜索项目成员候选人' })).toBeTruthy();
    expect(screen.getByRole('combobox', { name: '账号' })).toBeTruthy();

    fireEvent.click(screen.getByText('模型范围'));
    expect(await screen.findByText('选择该项目允许使用的模型集合。模型集合本身在“模型治理”中维护。')).toBeTruthy();
    expect(screen.getByRole('combobox', { name: '允许的模型集' })).toBeTruthy();
    expect(mocks.getModelSets).toHaveBeenCalled();
  });

  it('labels the new-project fields without exposing internal identifiers as required input', async () => {
    render(<ProjectManagementPage />);
    fireEvent.click(await screen.findByRole('button', { name: '新建项目' }));

    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText('新建项目')).toBeTruthy();
    expect(within(dialog).getByRole('textbox', { name: '项目名称' })).toBeTruthy();
    expect(within(dialog).getByRole('textbox', { name: '项目标识（可留空自动生成）' })).toBeTruthy();
    expect(within(dialog).getByRole('textbox', { name: '项目说明' })).toBeTruthy();
    expect(within(dialog).getByRole('spinbutton', { name: '预算金额（元）' })).toBeTruthy();
    expect(within(dialog).getByRole('combobox', { name: '预算周期' })).toBeTruthy();
  });

  it('keeps the persistent table and inspector stable under application StrictMode', async () => {
    render(
      <StrictMode>
        <ProjectManagementPage />
      </StrictMode>,
    );

    expect(await screen.findByLabelText('项目详情：核心项目')).toBeTruthy();
    expect(screen.queryByRole('heading', { name: '页面出错了' })).toBeNull();
  });

  it('restores the member deep link for a project administrator without granting project creation', async () => {
    useAuthStore.setState({
      user: {
        id: 7,
        username: 'project-admin',
        organization_role: 'member',
        role: 'project_admin',
        org_id: 1,
        org_name: '总部',
        license_access: { status: 'valid', enforceable: true, features: { gateway: true, agent_capabilities: true, oidc: true, guardrails: true, notifications: true, model_governance: true, data_exchange: true } },
      },
    });
    window.history.replaceState(null, '', '/projects?project_id=31&project_tab=members');
    mocks.getProjects.mockResolvedValueOnce({
      data: [{ ...projectRecord(31, '核心项目'), current_user_membership_role: 'project_admin' }],
      total: 1,
    });
    render(<ProjectManagementPage />);
    await screen.findByLabelText('项目详情：核心项目');
    expect(screen.queryByText('新建项目')).toBeNull();
    expect(await screen.findByText('developer-a')).toBeInTheDocument();
    expect(mocks.getProjectMembers).toHaveBeenCalledWith(31, 1, 20, '');
  });

  it('binds a platform operator to the canonical customer and removes organization selection', async () => {
    useAuthStore.setState({
      user: {
        id: 1,
        username: 'admin',
        organization_role: 'platform_admin',
        role: 'platform_admin',
        org_id: 99,
        org_name: '技术组织',
        customer_context: { state: 'ready', org_id: 7, org_name: '客户企业' },
        identity_scope: 'deployment_operator',
        license_access: null,
      },
    });
    mocks.getProjects.mockResolvedValueOnce({ data: [], total: 0 });

    render(<ProjectManagementPage />);

    expect(await screen.findByText('当前范围暂无项目')).toBeTruthy();
    expect(mocks.getProjects).toHaveBeenCalledWith(1, 20, { org_id: 7 });
    expect(mocks.getOrganizations).not.toHaveBeenCalled();
    expect(screen.queryByLabelText('目标组织')).toBeNull();
    expect(screen.getByText(/当前客户：客户企业/)).toBeTruthy();
  });

  it('does not misrepresent metadata-only project fields as zero or unlimited', async () => {
    useAuthStore.setState({
      user: {
        id: 8,
        username: 'organization-viewer',
        organization_role: 'org_viewer',
        role: 'org_viewer',
        org_id: 1,
        org_name: '总部',
        license_access: { status: 'valid', enforceable: true, features: { gateway: true, agent_capabilities: true, oidc: true, guardrails: true, notifications: true, model_governance: true, data_exchange: true } },
      },
    });
    mocks.getProjects.mockResolvedValueOnce({
      data: [{
        id: 31,
        org_id: 1,
        org_name: '总部',
        name: '只读项目',
        slug: 'read-only',
        description: '普通项目元数据',
        status: 'active',
      }],
      total: 1,
    });

    render(<ProjectManagementPage />);

    expect(await screen.findByLabelText('项目详情：只读项目')).toBeTruthy();
    expect((await screen.findAllByText('受限字段')).length).toBeGreaterThanOrEqual(2);
    expect(screen.queryByText('不限制')).toBeNull();
    expect(screen.getByText(/缺失值不会按 0 或“不限制”解释/)).toBeTruthy();
    expect(screen.queryByText('成员与角色')).toBeNull();
    expect(mocks.getProjectMembers).not.toHaveBeenCalled();
  });

  it('restores the selected project and member tab from a deep link', async () => {
    window.history.replaceState(null, '', '/projects?org_id=99&project_id=32&project_tab=members&project_page=3&project_page_size=50');
    mocks.getProjects.mockResolvedValueOnce({
      data: [
        {
          id: 31,
          org_id: 1,
          org_name: '总部',
          name: '核心项目',
          slug: 'core',
          status: 'active',
          budget_cents: 0,
          budget_period: 'none',
        },
        {
          id: 32,
          org_id: 1,
          org_name: '总部',
          name: '辅助项目',
          slug: 'secondary',
          status: 'active',
          budget_cents: 0,
          budget_period: 'none',
        },
      ],
      total: 112,
    });

    render(<ProjectManagementPage />);

    expect(await screen.findByLabelText('项目详情：辅助项目')).toBeTruthy();
    expect(await screen.findByText('developer-a')).toBeTruthy();
    expect(mocks.getProjects.mock.calls).toEqual([[3, 50, { org_id: undefined }]]);
    expect(mocks.getProjectMembers).toHaveBeenCalledWith(32, 1, 20, '');
    const params = new URLSearchParams(window.location.search);
    expect(params.get('project_tab')).toBe('members');
    expect(params.get('project_page')).toBe('3');
    expect(params.get('org_id')).toBeNull();
  });

  it('restores a saved view with one read at the saved page and size', async () => {
    window.localStorage.setItem('unirouter.projects.saved-views.v1', JSON.stringify([{
      name: '第 3 页模型',
      state: { project_id: 32, project_tab: 'models', project_page: 3, project_page_size: 50 },
    }]));
    mocks.getProjects.mockImplementation((page: number) => Promise.resolve({
      data: page === 3 ? [projectRecord(32, '已保存项目')] : [projectRecord(31, '初始项目')],
      total: 120,
    }));

    render(<ProjectManagementPage />);
    await screen.findByLabelText('项目详情：初始项目');
    fireEvent.click(screen.getByText('已保存工作区（此浏览器）'));
    fireEvent.click(await screen.findByText('第 3 页模型', { selector: '.semi-select-option-text' }));

    expect(await screen.findByLabelText('项目详情：已保存项目')).toBeInTheDocument();
    await waitFor(() => expect(mocks.getProjectModelSets).toHaveBeenCalledWith(32));
    expect(mocks.getProjects.mock.calls).toEqual([[1, 20, { org_id: undefined }], [3, 50, { org_id: undefined }]]);
    expect(new URLSearchParams(window.location.search).get('project_tab')).toBe('models');
  });

  it('hides the previous customer project and prevents saving its view while the new scope loads', async () => {
    const actor = {
      id: 2, username: 'platform', organization_role: 'platform_admin', role: 'platform_admin', org_id: 99,
      customer_context: { state: 'ready' as const, org_id: 7 },
    };
    act(() => useAuthStore.setState({ user: actor }));
    const viewsKey = 'unirouter.projects.saved-views.v1';
    const savedViews = [{
      name: '已有视图',
      state: { project_id: 31, project_tab: 'overview', project_page: 1, project_page_size: 20 },
    }];
    window.localStorage.setItem(viewsKey, JSON.stringify(savedViews));
    const oldResult = deferred<{ data: ReturnType<typeof projectRecord>[]; total: number }>();
    const newResult = deferred<{ data: ReturnType<typeof projectRecord>[]; total: number }>();
    mocks.getProjects
      .mockResolvedValueOnce({ data: [projectRecord(31, '旧客户项目', 7)], total: 1 })
      .mockReturnValueOnce(oldResult.promise)
      .mockReturnValueOnce(newResult.promise);

    render(<ProjectManagementPage />);
    await screen.findByLabelText('项目详情：旧客户项目');
    fireEvent.click(screen.getByRole('button', { name: /刷新$/ }));
    await waitFor(() => expect(mocks.getProjects).toHaveBeenCalledTimes(2));
    act(() => useAuthStore.setState({ user: { ...actor, customer_context: { state: 'ready', org_id: 8 } } }));

    await waitFor(() => expect(mocks.getProjects).toHaveBeenLastCalledWith(1, 20, { org_id: 8 }));
    expect(screen.queryByText('旧客户项目')).not.toBeInTheDocument();
    const viewName = screen.getByLabelText('项目工作区名称');
    const saveView = screen.getByRole('button', { name: '保存 / 覆盖' });
    fireEvent.change(viewName, { target: { value: '新客户视图' } });
    expect(saveView).toBeDisabled();
    fireEvent.keyDown(viewName, { key: 'Enter' });
    expect(JSON.parse(window.localStorage.getItem(viewsKey) || '[]')).toEqual(savedViews);

    await act(async () => newResult.resolve({ data: [projectRecord(32, '新客户项目', 8)], total: 1 }));
    expect(await screen.findByLabelText('项目详情：新客户项目')).toBeInTheDocument();
    await act(async () => oldResult.resolve({ data: [projectRecord(31, '迟到的客户项目', 7)], total: 1 }));
    expect(screen.queryByText('迟到的客户项目')).not.toBeInTheDocument();
    expect(new URLSearchParams(window.location.search).get('project_id')).toBe('32');
    fireEvent.click(screen.getByRole('button', { name: '保存 / 覆盖' }));
    expect(JSON.parse(window.localStorage.getItem(viewsKey) || '[]')).toEqual([
      ...savedViews,
      {
        name: '新客户视图',
        state: { project_id: 32, project_tab: 'overview', project_page: 1, project_page_size: 20 },
      },
    ]);
  });

  it('keeps the submitted candidate search through paging, size changes and retry', async () => {
    const user = userEvent.setup();
    let failed = false;
    mocks.getProjectMembers.mockImplementation((_id: number, page: number, size: number, query: string) => {
      if (size === 50 && !failed) {
        failed = true;
        return Promise.reject(new Error('unavailable'));
      }
      return Promise.resolve(memberResponse(`member-${page}-${size}-${query || 'all'}`));
    });

    render(<ProjectManagementPage />);
    const detail = within(await screen.findByLabelText('项目详情：核心项目'));
    fireEvent.click(detail.getByText('成员与角色'));
    await detail.findByText('member-1-20-all');
    fireEvent.click(detail.getByLabelText('Page 4'));
    await detail.findByText('member-4-20-all');

    const search = detail.getByRole('textbox', { name: '搜索项目成员候选人' });
    fireEvent.change(search, { target: { value: '  candidate  ' } });
    expect(mocks.getProjectMembers).toHaveBeenCalledTimes(2);
    fireEvent.click(detail.getByRole('button', { name: '搜索' }));
    await detail.findByText('member-1-20-candidate');
    fireEvent.click(detail.getByLabelText('Page 4'));
    const currentRow = await detail.findByText('member-4-20-candidate');

    fireEvent.change(search, { target: { value: 'draft only' } });
    const table = currentRow.closest('.ur-table-shell') as HTMLElement;
    const sizeControl = table.querySelector('.semi-page-switch [role="combobox"]') as HTMLElement;
    await user.click(sizeControl);
    const sizeOption = await screen.findByRole('option', { name: /50/ });
    const popup = sizeOption.closest('[role="dialog"]');
    if (!popup) throw new Error('Missing page size popup');
    // jsdom does not send the CSS events that commit a controlled Semi Select.
    await waitFor(() => expect(popup).toHaveClass('semi-tooltip-animation-show'));
    fireEvent.animationEnd(popup);
    await user.click(sizeOption);
    await waitFor(() => expect(popup).toHaveClass('semi-tooltip-animation-hide'));
    await act(async () => {
      fireEvent.animationEnd(popup);
    });
    expect(await detail.findByText('项目成员加载失败')).toBeInTheDocument();
    expect(detail.queryByText('member-4-20-candidate')).not.toBeInTheDocument();
    fireEvent.click(detail.getByRole('button', { name: '重试' }));
    expect(await detail.findByText('member-1-50-candidate')).toBeInTheDocument();
    expect(mocks.getProjectMembers.mock.calls).toEqual([
      [31, 1, 20, ''], [31, 4, 20, ''],
      [31, 1, 20, 'candidate'], [31, 4, 20, 'candidate'],
      [31, 1, 50, 'candidate'], [31, 1, 50, 'candidate'],
    ]);
  });

  it('ignores a late member response after another project is selected', async () => {
    const previousRead = deferred<ReturnType<typeof memberResponse>>();
    mocks.getProjects.mockResolvedValue({
      data: [projectRecord(31, '初始项目'), projectRecord(32, '新选项目')],
      total: 2,
    });
    mocks.getProjectMembers.mockImplementation((id: number) => (
      id === 31 ? previousRead.promise : Promise.resolve(memberResponse('新项目成员'))
    ));

    render(<ProjectManagementPage />);
    const previous = within(await screen.findByLabelText('项目详情：初始项目'));
    fireEvent.click(previous.getByText('成员与角色'));
    await waitFor(() => expect(mocks.getProjectMembers).toHaveBeenLastCalledWith(31, 1, 20, ''));
    fireEvent.click(screen.getByText('新选项目'));

    const current = within(await screen.findByLabelText('项目详情：新选项目'));
    fireEvent.click(current.getByText('成员与角色'));
    await current.findByText('新项目成员');
    await act(async () => previousRead.resolve(memberResponse('迟到的旧项目成员')));
    expect(current.getByText('新项目成员')).toBeInTheDocument();
    expect(screen.queryByText('迟到的旧项目成员')).not.toBeInTheDocument();
    expect(mocks.getProjectMembers.mock.calls).toEqual([[31, 1, 20, ''], [32, 1, 20, '']]);
  });

  it('enables model saves only after the selected project and its organization reads both settle', async () => {
    const actor = {
      ...useAuthStore.getState().user!,
      role: 'platform_admin',
      organization_role: 'platform_admin',
      org_id: 99,
      customer_context: { state: 'ready' as const, org_id: 7 },
    };
    act(() => useAuthStore.setState({ user: actor }));
    window.history.replaceState(null, '', '/projects?project_id=31&project_tab=models');
    const previousAssignments = deferred<{ model_set_ids: number[] }>();
    const lastCatalogPage = deferred<{ data: { id: number; name: string; slug: string; status: string }[]; total: number }>();
    mocks.getProjects.mockImplementation((_page: number, _size: number, scope: { org_id: number }) => Promise.resolve({
      data: scope.org_id === 7
        ? [projectRecord(31, '旧客户项目', 7)]
        : [projectRecord(32, '新客户项目', 8)],
      total: 1,
    }));
    mocks.getModelSets.mockImplementation((page: number, _size: number, scope: { org_id: number }) => {
      if (scope.org_id === 7) {
        return Promise.resolve({ data: [{ id: 71, name: '旧模型集', slug: 'old-set', status: 'active' }], total: 1 });
      }
      if (page === 2) return lastCatalogPage.promise;
      return Promise.resolve({ data: [{ id: 81, name: '新模型集', slug: 'new-set', status: 'active' }], total: 101 });
    });
    mocks.getProjectModelSets.mockImplementation((id: number) => (
      id === 31 ? previousAssignments.promise : Promise.resolve({ data: [{ model_set_id: 81 }] })
    ));
    mocks.replaceProjectModelSets.mockResolvedValue({ success: true });

    render(<ProjectManagementPage />);
    const oldDetail = within(await screen.findByLabelText('项目详情：旧客户项目'));
    const oldSave = oldDetail.getByRole('button', { name: '保存模型范围' });
    expect(oldSave).toBeDisabled();
    fireEvent.click(oldSave);
    expect(mocks.replaceProjectModelSets).not.toHaveBeenCalled();
    expect(mocks.getProjectModelSets).toHaveBeenCalledWith(31);

    act(() => useAuthStore.setState({ user: { ...actor, customer_context: { state: 'ready', org_id: 8 } } }));
    const newDetail = within(await screen.findByLabelText('项目详情：新客户项目'));
    await waitFor(() => expect(mocks.getModelSets).toHaveBeenCalledWith(2, 100, { org_id: 8 }));
    expect(newDetail.getByRole('button', { name: '保存模型范围' })).toBeDisabled();
    await act(async () => lastCatalogPage.resolve({
      data: [{ id: 82, name: '下页模型集', slug: 'next-catalog-page', status: 'active' }], total: 101,
    }));
    await act(async () => previousAssignments.resolve({ model_set_ids: [71] }));

    const save = newDetail.getByRole('button', { name: '保存模型范围' });
    await waitFor(() => expect(save).toBeEnabled());
    expect(newDetail.getByRole('combobox', { name: '允许的模型集' })).toHaveTextContent('新模型集');
    expect(screen.queryByText(/旧模型集/)).not.toBeInTheDocument();
    await act(async () => { fireEvent.click(save); });
    expect(mocks.replaceProjectModelSets.mock.calls).toEqual([[32, [81]]]);
    expect(mocks.getModelSets.mock.calls).toEqual([
      [1, 100, { org_id: 7 }], [1, 100, { org_id: 8 }], [2, 100, { org_id: 8 }],
    ]);
    expect(mocks.getProjectModelSets.mock.calls).toEqual([[31], [32]]);
  });

  it('retries the organization catalog and selected project assignments together', async () => {
    window.history.replaceState(null, '', '/projects?project_id=31&project_tab=models');
    mocks.getModelSets.mockRejectedValueOnce(new Error('unavailable'));

    render(<ProjectManagementPage />);
    const detail = within(await screen.findByLabelText('项目详情：核心项目'));
    expect(await detail.findByText('模型范围加载失败，请稍后重试。')).toBeInTheDocument();
    expect(detail.queryByRole('button', { name: '保存模型范围' })).not.toBeInTheDocument();
    fireEvent.click(detail.getByRole('button', { name: '重试' }));

    await waitFor(() => expect(detail.getByRole('button', { name: '保存模型范围' })).toBeEnabled());
    expect(mocks.getModelSets.mock.calls).toEqual([[1, 100, { org_id: 1 }], [1, 100, { org_id: 1 }]]);
    expect(mocks.getProjectModelSets.mock.calls).toEqual([[31], [31]]);
  });

  it('keeps model reads gated by the model-governance license', async () => {
    window.history.replaceState(null, '', '/projects?project_id=31&project_tab=models');
    act(() => useAuthStore.setState({ user: { ...useAuthStore.getState().user!, license_access: null } }));

    render(<ProjectManagementPage />);
    const detail = within(await screen.findByLabelText('项目详情：核心项目'));
    expect(detail.getByRole('heading', { name: '模型治理暂不可用' })).toBeInTheDocument();
    expect(mocks.getModelSets).not.toHaveBeenCalled();
    expect(mocks.getProjectModelSets).not.toHaveBeenCalled();
  });

  it('saves project workspace metadata in the browser and supports keyboard selection', async () => {
    mocks.getProjects.mockResolvedValueOnce({
      data: [
        {
          id: 31,
          org_id: 1,
          org_name: '总部',
          name: '核心项目',
          slug: 'core',
          status: 'active',
          budget_cents: 0,
          budget_period: 'none',
        },
        {
          id: 32,
          org_id: 1,
          org_name: '总部',
          name: '辅助项目',
          slug: 'secondary',
          status: 'active',
          budget_cents: 0,
          budget_period: 'none',
        },
      ],
      total: 2,
    });

    render(<ProjectManagementPage />);
    const secondaryRow = (await screen.findByText('辅助项目')).closest('tr');
    expect(secondaryRow).toBeTruthy();
    fireEvent.keyDown(secondaryRow as HTMLElement, { key: 'Enter' });
    expect(await screen.findByLabelText('项目详情：辅助项目')).toBeTruthy();

    fireEvent.click(screen.getByText('成员与角色'));
    await screen.findByText('developer-a');
    fireEvent.change(screen.getByLabelText('项目工作区名称'), { target: { value: '辅助项目成员' } });
    fireEvent.click(screen.getByRole('button', { name: '保存 / 覆盖' }));

    expect(JSON.parse(window.localStorage.getItem('unirouter.projects.saved-views.v1') || '[]')).toEqual([
      {
        name: '辅助项目成员',
        state: {
          project_id: 32,
          project_tab: 'members',
          project_page: 1,
          project_page_size: 20,
        },
      },
    ]);
  });
});
