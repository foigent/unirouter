import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { Toast } from '@douyinfe/semi-ui';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import ModelAuthorizationWorkspace from '../ModelAuthorizationWorkspace';
import { useAuthStore } from '../../store/auth';

const mocks = vi.hoisted(() => ({
  getModelAuthorizationContract: vi.fn(),
  getModelAuthorizations: vi.fn(),
  getUsers: vi.fn(),
  getProjects: vi.fn(),
  getKeys: vi.fn(),
  getModelEndpoints: vi.fn(),
  createModelAuthorization: vi.fn(),
  activateModelAuthorization: vi.fn(),
  revokeModelAuthorization: vi.fn(),
}));

vi.mock('../../api/client', () => ({ api: mocks }));
vi.mock('../../api/modelEndpoints', () => ({
  modelEndpointApi: { getModelEndpoints: mocks.getModelEndpoints },
}));

describe('ModelAuthorizationWorkspace', () => {
  beforeEach(() => {
    act(() => { Toast.destroyAll(); });
    vi.clearAllMocks();
    useAuthStore.setState({
      token: 'token', isAuthenticated: true, profileLoading: false, profileError: null,
      user: {
        id: 1, username: 'admin', role: 'platform_admin', organization_role: 'platform_admin', org_id: 99,
        customer_context: { state: 'ready', org_id: 7, org_name: '客户组织' }, identity_scope: 'deployment_operator',
      },
    });
    mocks.getModelAuthorizationContract.mockResolvedValue({
      contract_version: 'unirouter.model-call-authorization.v1', environment: 'production',
      subjects: ['user', 'project', 'api_key'], effects: ['allow', 'deny'],
      endpoints: ['/v1/chat/completions', '/v1/models'], precedence: 'deny-first', authority: 'narrow_only', tool_or_mcp_authority: 'none',
    });
    mocks.getModelAuthorizations.mockResolvedValue({ data: [], total: 0 });
    mocks.getUsers.mockImplementation((_page: number, _size: number, provider: string) => Promise.resolve({
      data: provider === 'local' ? [{ id: 37, org_id: 7, username: 'test2', auth_provider: 'local', status: 'active' }] : [], total: provider === 'local' ? 1 : 0,
    }));
    mocks.getProjects.mockResolvedValue({ data: [{ id: 71, org_id: 7, name: 'Default Project', status: 'active' }], total: 1 });
    mocks.getKeys.mockResolvedValue({ data: [], total: 0 });
    mocks.getModelEndpoints.mockResolvedValue({
      contract_version: 'unirouter.model-endpoints.v1', generated_at: '2026-08-19T00:00:00Z',
      summary: { total: 2, route_available: 2, attention: 0, canary: 0 },
      models: [
        { public_model: 'deepseek-v4-flash', display_name: 'DeepSeek V4 Flash', targets: [] },
        { public_model: 'kimi-k3', display_name: 'Kimi K3', targets: [] },
      ],
    });
    mocks.createModelAuthorization.mockResolvedValue({ authorization_ref: 'mca_generated_by_server', status: 'draft' });
  });

  it('uses people and model pickers and leaves the internal authorization reference to the server', async () => {
    render(<ModelAuthorizationWorkspace />);

    expect(await screen.findByText('高级模型限制策略')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '新增限制策略' }));
    expect(await screen.findByText(/这不是开通权限/)).toBeTruthy();
    expect(screen.queryByText('策略标识')).toBeNull();

    fireEvent.click(screen.getByRole('combobox', { name: '选择人员' }));
    fireEvent.click(await screen.findByText('test2 · 本地账号'));
    fireEvent.click(screen.getByRole('combobox', { name: '只允许调用这些模型' }));
    fireEvent.click(await screen.findByText('deepseek-v4-flash'));
    fireEvent.click(screen.getByRole('button', { name: '创建草稿' }));

    await waitFor(() => expect(mocks.createModelAuthorization).toHaveBeenCalledWith({
      org_id: 7,
      subject_type: 'user',
      subject_ref: '37',
      environment: 'production',
      effect: 'allow',
      allowed_models: ['deepseek-v4-flash'],
      allowed_endpoints: ['/v1/chat/completions'],
      reason: '管理员配置的模型调用范围',
    }));
    expect(Object.prototype.hasOwnProperty.call(mocks.createModelAuthorization.mock.calls[0][0], 'authorization_ref')).toBe(false);
  });
});
