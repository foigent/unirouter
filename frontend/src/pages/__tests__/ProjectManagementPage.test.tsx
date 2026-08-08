import { fireEvent, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
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

describe('ProjectManagementPage', () => {
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
        license_access: { status: 'valid', enforceable: true, features: { gateway: true, agent_capabilities: true, oidc: true, guardrails: true, notifications: true, model_governance: false, data_exchange: true } },
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

    fireEvent.click(screen.getByText('成员与角色'));
    expect(await screen.findByText('developer-a')).toBeTruthy();
    expect(mocks.getProjectMembers).toHaveBeenCalledWith(31, 1, 20, '');

    fireEvent.click(screen.getByText('模型范围'));
    expect(await screen.findByText('当前授权未包含模型治理')).toBeTruthy();
    expect(mocks.getModelSets).not.toHaveBeenCalled();
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

  it('does not let a project administrator create projects', async () => {
    useAuthStore.setState({
      user: {
        id: 7,
        username: 'project-admin',
        organization_role: 'member',
        role: 'project_admin',
        org_id: 1,
        org_name: '总部',
        license_access: { status: 'valid', enforceable: true, features: { gateway: true, agent_capabilities: true, oidc: true, guardrails: true, notifications: true, model_governance: false, data_exchange: true } },
      },
    });
    render(<ProjectManagementPage />);
    await screen.findByLabelText('项目详情：核心项目');
    expect(screen.queryByText('新建项目')).toBeNull();
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
    window.history.replaceState(null, '', '/projects?project_id=32&project_tab=members');
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

    expect(await screen.findByLabelText('项目详情：辅助项目')).toBeTruthy();
    expect(await screen.findByText('developer-a')).toBeTruthy();
    expect(mocks.getProjectMembers).toHaveBeenCalledWith(32, 1, 20, '');
    expect(new URLSearchParams(window.location.search).get('project_tab')).toBe('members');
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
