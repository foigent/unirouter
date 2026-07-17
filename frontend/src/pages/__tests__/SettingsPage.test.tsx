import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { Toast } from '@douyinfe/semi-ui';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import SettingsPage from '../SettingsPage';
import { useAuthStore } from '../../store/auth';
import { ApiError } from '../../api/client';

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
    localStorage.setItem('token', 'old-session');
    useAuthStore.setState({ token: 'old-session', isAuthenticated: true });
    mocks.changePassword.mockResolvedValue({ success: true, reauthentication_required: true });

    const { container } = render(<SettingsPage />);
    await screen.findByText('alice');
    const inputs = container.querySelectorAll('input[type="password"]');
    expect(inputs).toHaveLength(3);
    fireEvent.change(inputs[0], { target: { value: 'old-password-123' } });
    fireEvent.change(inputs[1], { target: { value: 'new-password-123' } });
    fireEvent.change(inputs[2], { target: { value: 'new-password-123' } });
    fireEvent.click(screen.getByRole('button', { name: '修改密码' }));

    await waitFor(() => expect(mocks.changePassword).toHaveBeenCalledWith({
      old_password: 'old-password-123',
      new_password: 'new-password-123',
    }));
    await waitFor(() => expect(useAuthStore.getState().isAuthenticated).toBe(false));
    expect(localStorage.getItem('token')).toBeNull();
  });

  it('counts the minimum password length in Unicode code points', async () => {
    render(<SettingsPage />);
    await screen.findByText('alice');
    const inputs = document.querySelectorAll('input[type="password"]');
    fireEvent.change(inputs[0], { target: { value: 'old-password-123' } });
    fireEvent.change(inputs[1], { target: { value: '😀'.repeat(11) } });
    fireEvent.change(inputs[2], { target: { value: '😀'.repeat(11) } });
    fireEvent.click(screen.getByRole('button', { name: '修改密码' }));

    expect(await screen.findByText('至少 12 个 Unicode 字符（按码点计）')).toBeTruthy();
    expect(mocks.changePassword).not.toHaveBeenCalled();
  });

  it('accepts exactly 72 UTF-8 bytes for a new local password', async () => {
    mocks.changePassword.mockResolvedValue({ success: true, reauthentication_required: true });
    render(<SettingsPage />);
    await screen.findByText('alice');
    const inputs = document.querySelectorAll('input[type="password"]');
    fireEvent.change(inputs[0], { target: { value: 'old-password-123' } });
    fireEvent.change(inputs[1], { target: { value: '界'.repeat(24) } });
    fireEvent.change(inputs[2], { target: { value: '界'.repeat(24) } });
    fireEvent.click(screen.getByRole('button', { name: '修改密码' }));

    await waitFor(() => expect(mocks.changePassword).toHaveBeenCalledWith({
      old_password: 'old-password-123',
      new_password: '界'.repeat(24),
    }));
  });

  it('rejects a 73-byte new password before calling the API', async () => {
    render(<SettingsPage />);
    await screen.findByText('alice');
    const inputs = document.querySelectorAll('input[type="password"]');
    const password = `${'界'.repeat(23)}😀`;
    fireEvent.change(inputs[0], { target: { value: 'old-password-123' } });
    fireEvent.change(inputs[1], { target: { value: password } });
    fireEvent.change(inputs[2], { target: { value: password } });
    fireEvent.click(screen.getByRole('button', { name: '修改密码' }));

    expect(await screen.findByText('最多 72 个 UTF-8 字节')).toBeTruthy();
    expect(mocks.changePassword).not.toHaveBeenCalled();
  });

  it('does not mislabel a server password-policy rejection as a wrong current password', async () => {
    mocks.changePassword.mockRejectedValue(new ApiError('password must be at most 72 bytes', 400, {
      error: 'password must be at most 72 bytes',
    }));
    render(<SettingsPage />);
    await screen.findByText('alice');
    const inputs = document.querySelectorAll('input[type="password"]');
    fireEvent.change(inputs[0], { target: { value: 'old-password-123' } });
    fireEvent.change(inputs[1], { target: { value: 'new-password-123' } });
    fireEvent.change(inputs[2], { target: { value: 'new-password-123' } });
    fireEvent.click(screen.getByRole('button', { name: '修改密码' }));

    expect(await screen.findByText('新密码需至少 12 个 Unicode 字符（按码点计），且最多 72 个 UTF-8 字节')).toBeTruthy();
    expect(screen.queryByText(/当前密码不正确/)).toBeNull();
  });

  it('identifies a rejected current password without calling it a policy error', async () => {
    mocks.changePassword.mockRejectedValue(new ApiError('Invalid credentials', 401, {
      error: 'Invalid credentials',
    }));
    render(<SettingsPage />);
    await screen.findByText('alice');
    const inputs = document.querySelectorAll('input[type="password"]');
    fireEvent.change(inputs[0], { target: { value: 'wrong-password' } });
    fireEvent.change(inputs[1], { target: { value: 'new-password-123' } });
    fireEvent.change(inputs[2], { target: { value: 'new-password-123' } });
    fireEvent.click(screen.getByRole('button', { name: '修改密码' }));

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
