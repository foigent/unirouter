import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
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
    useAuthStore.setState({
      token: 'token',
      isAuthenticated: true,
      profileLoading: false,
      profileError: null,
      user: {
        id: 2,
        username: 'org-admin',
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
    fireEvent.click(await screen.findByRole('button', { name: '核心项目' }));
    const detail = await screen.findByLabelText('项目详情：核心项目');
    await waitFor(() => expect(document.activeElement).toBe(detail));
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

  it('does not let a project administrator create projects', async () => {
    useAuthStore.setState({
      user: {
        id: 7,
        username: 'project-admin',
        role: 'project_admin',
        org_id: 1,
        org_name: '总部',
        license_access: { status: 'valid', enforceable: true, features: { gateway: true, agent_capabilities: true, oidc: true, guardrails: true, notifications: true, model_governance: false, data_exchange: true } },
      },
    });
    render(<ProjectManagementPage />);
    await screen.findByRole('button', { name: '核心项目' });
    expect(screen.queryByText('新建项目')).toBeNull();
  });
});
