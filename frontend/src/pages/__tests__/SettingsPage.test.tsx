import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Toast } from '@douyinfe/semi-ui';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import SettingsPage from '../SettingsPage';
import { useAuthStore } from '../../store/auth';
import { ApiError } from '../../api/client';
import { pasteValue } from '../../test/user';

const mocks = vi.hoisted(() => ({
  getMe: vi.fn(),
  changePassword: vi.fn(),
}));

vi.mock('../../api/client', () => ({
  api: mocks,
  ApiError: class ApiError extends Error {
    status: number;
    payload: unknown;

    constructor(message: string, status: number, payload: unknown) {
      super(message);
      this.status = status;
      this.payload = payload;
    }
  },
}));

describe('SettingsPage account scope', () => {
  beforeEach(() => {
    act(() => { Toast.destroyAll(); });
    vi.clearAllMocks();
    localStorage.clear();
    useAuthStore.setState({ token: null, user: null, isAuthenticated: false });
    mocks.getMe.mockResolvedValue({
      id: 7,
      username: 'alice',
      role: 'org_admin',
      org_id: 42,
      org_name: '东区组织',
      email: 'alice@example.com',
      auth_provider: 'local',
      created_at: '2026-07-15T00:00:00Z',
    });
  });

  it('presents personal account information instead of system settings', async () => {
    render(<SettingsPage />);

    expect(await screen.findByText('我的账号')).toBeTruthy();
    expect(screen.getByText('alice')).toBeTruthy();
    expect(screen.getByText('组织管理员')).toBeTruthy();
    expect(screen.getByText('东区组织')).toBeTruthy();
    expect(screen.getByText('alice@example.com')).toBeTruthy();
    expect(screen.getByText('本地账号')).toBeTruthy();
    expect(screen.getAllByText('修改密码')).toHaveLength(2);
    expect(screen.queryByText('系统设置')).toBeNull();
    expect(screen.queryByText('系统信息')).toBeNull();
    expect(screen.queryByText('v1.0.0')).toBeNull();
  });

  it('shows the authoritative platform role and deployment scope instead of compatibility shadows', async () => {
    mocks.getMe.mockResolvedValue({
      id: 1,
      username: 'admin',
      role: 'admin',
      organization_role: 'platform_admin',
      org_id: 1,
      org_name: 'Platform Admin',
      customer_context: { state: 'ready', org_id: 1, org_name: 'Platform Admin' },
      identity_scope: 'deployment_operator_and_customer',
      auth_provider: 'local',
    });

    render(<SettingsPage />);

    expect(await screen.findByText('平台管理员')).toBeTruthy();
    expect(screen.getByText('当前部署（运维与客户管理）')).toBeTruthy();
    expect(screen.queryByText('系统管理员（兼容）')).toBeNull();
    expect(screen.queryByText('Platform Admin')).toBeNull();
  });

  it('shows identity-provider ownership and hides local password controls for OIDC accounts', async () => {
    mocks.getMe.mockResolvedValue({
      id: 8,
      username: 'bob',
      role: 'developer',
      org_id: 42,
      org_name: '东区组织',
      email: 'bob@example.com',
      auth_provider: 'oidc',
      created_at: '2026-07-15T00:00:00Z',
    });

    render(<SettingsPage />);

    expect((await screen.findAllByText('企业身份认证（OIDC）')).length).toBeGreaterThan(0);
    expect(screen.getByText(/密码与多因素认证由企业身份提供方统一管理/)).toBeTruthy();
    expect(screen.queryByLabelText('当前密码')).toBeNull();
    expect(screen.queryByRole('button', { name: '修改密码' })).toBeNull();
  });

  it('clears the now-revoked local session after a successful password change', async () => {
    const user = userEvent.setup();
    localStorage.setItem('token', 'old-session');
    useAuthStore.setState({ token: 'old-session', isAuthenticated: true });
    mocks.changePassword.mockResolvedValue({ success: true, reauthentication_required: true });

    const { container } = render(<SettingsPage />);
    await screen.findByText('alice');
    const inputs = container.querySelectorAll('input[type="password"]');
    expect(inputs).toHaveLength(3);
    await pasteValue(user, inputs[0], 'old-password-123');
    await pasteValue(user, inputs[1], 'new-password-123');
    await pasteValue(user, inputs[2], 'new-password-123');
    await user.click(screen.getByRole('button', { name: '修改密码' }));

    await waitFor(() => expect(mocks.changePassword).toHaveBeenCalledWith({
      old_password: 'old-password-123',
      new_password: 'new-password-123',
    }));
    await waitFor(() => expect(useAuthStore.getState().isAuthenticated).toBe(false));
    expect(localStorage.getItem('token')).toBeNull();
  });

  it('counts the minimum password length in Unicode code points', async () => {
    const user = userEvent.setup();
    render(<SettingsPage />);
    await screen.findByText('alice');
    const inputs = document.querySelectorAll('input[type="password"]');
    await pasteValue(user, inputs[0], 'old-password-123');
    await pasteValue(user, inputs[1], '😀'.repeat(11));
    await pasteValue(user, inputs[2], '😀'.repeat(11));
    await user.click(screen.getByRole('button', { name: '修改密码' }));

    expect(await screen.findByText('至少 12 个 Unicode 字符（按码点计）')).toBeTruthy();
    expect(mocks.changePassword).not.toHaveBeenCalled();
  });

  it('accepts exactly 72 UTF-8 bytes for a new local password', async () => {
    const user = userEvent.setup();
    mocks.changePassword.mockResolvedValue({ success: true, reauthentication_required: true });
    render(<SettingsPage />);
    await screen.findByText('alice');
    const inputs = document.querySelectorAll('input[type="password"]');
    await pasteValue(user, inputs[0], 'old-password-123');
    await pasteValue(user, inputs[1], '界'.repeat(24));
    await pasteValue(user, inputs[2], '界'.repeat(24));
    await user.click(screen.getByRole('button', { name: '修改密码' }));

    await waitFor(() => expect(mocks.changePassword).toHaveBeenCalledWith({
      old_password: 'old-password-123',
      new_password: '界'.repeat(24),
    }));
  });

  it('rejects a 73-byte new password before calling the API', async () => {
    const user = userEvent.setup();
    render(<SettingsPage />);
    await screen.findByText('alice');
    const inputs = document.querySelectorAll('input[type="password"]');
    const password = `${'界'.repeat(23)}😀`;
    await pasteValue(user, inputs[0], 'old-password-123');
    await pasteValue(user, inputs[1], password);
    await pasteValue(user, inputs[2], password);
    await user.click(screen.getByRole('button', { name: '修改密码' }));

    expect(await screen.findByText('最多 72 个 UTF-8 字节')).toBeTruthy();
    expect(mocks.changePassword).not.toHaveBeenCalled();
  });

  it('does not mislabel a server password-policy rejection as a wrong current password', async () => {
    const user = userEvent.setup();
    mocks.changePassword.mockRejectedValue(new ApiError('password must be at most 72 bytes', 400, {
      error: 'password must be at most 72 bytes',
    }));
    render(<SettingsPage />);
    await screen.findByText('alice');
    const inputs = document.querySelectorAll('input[type="password"]');
    await pasteValue(user, inputs[0], 'old-password-123');
    await pasteValue(user, inputs[1], 'new-password-123');
    await pasteValue(user, inputs[2], 'new-password-123');
    await user.click(screen.getByRole('button', { name: '修改密码' }));

    expect(await screen.findByText('新密码需至少 12 个 Unicode 字符（按码点计），且最多 72 个 UTF-8 字节')).toBeTruthy();
    expect(screen.queryByText(/当前密码不正确/)).toBeNull();
  });

  it('identifies a rejected current password without calling it a policy error', async () => {
    const user = userEvent.setup();
    mocks.changePassword.mockRejectedValue(new ApiError('Invalid credentials', 401, {
      error: 'Invalid credentials',
    }));
    render(<SettingsPage />);
    await screen.findByText('alice');
    const inputs = document.querySelectorAll('input[type="password"]');
    await pasteValue(user, inputs[0], 'wrong-password');
    await pasteValue(user, inputs[1], 'new-password-123');
    await pasteValue(user, inputs[2], 'new-password-123');
    await user.click(screen.getByRole('button', { name: '修改密码' }));

    expect(await screen.findByText('当前密码不正确或登录状态已失效，请重新登录')).toBeTruthy();
    expect(screen.queryByText('新密码需至少 12 个 Unicode 字符（按码点计），且最多 72 个 UTF-8 字节')).toBeNull();
  });

  it('does not infer a local account when the profile request fails', async () => {
    mocks.getMe.mockRejectedValueOnce(new Error('network unavailable'));

    render(<SettingsPage />);

    expect(await screen.findByText('账号信息加载失败')).toBeTruthy();
    expect(screen.queryByLabelText('当前密码')).toBeNull();
    expect(screen.queryByText('本地账号')).toBeNull();

    mocks.getMe.mockResolvedValueOnce({
      id: 8,
      username: 'bob',
      role: 'developer',
      org_id: 42,
      org_name: '东区组织',
      auth_provider: 'oidc',
    });
    fireEvent.click(screen.getByRole('button', { name: '重新加载账号信息' }));

    await waitFor(() => expect(mocks.getMe).toHaveBeenCalledTimes(2));
    expect((await screen.findAllByText('企业身份认证（OIDC）')).length).toBeGreaterThan(0);
  });
});
