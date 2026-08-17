import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { Toast } from '@douyinfe/semi-ui';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import UsersPage from '../UsersPage';
import { useAuthStore } from '../../store/auth';
import { ApiError } from '../../api/client';

const mocks = vi.hoisted(() => ({
  getUsers: vi.fn(),
  getOrganizations: vi.fn(),
  getProjects: vi.fn(),
  upsertProjectMember: vi.fn(),
  createUser: vi.fn(),
  updateUser: vi.fn(),
  deleteUser: vi.fn(),
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

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((complete) => { resolve = complete; });
  return { promise, resolve };
}

describe('UsersPage enterprise identity presentation', () => {
  beforeEach(() => {
    act(() => { Toast.destroyAll(); });
    vi.clearAllMocks();
    useAuthStore.setState({
      token: 'token',
      isAuthenticated: true,
      profileLoading: false,
      profileError: null,
      user: {
        id: 1, username: 'admin', organization_role: 'platform_admin', role: 'platform_admin',
        org_id: 99, org_name: '技术组织',
        customer_context: { state: 'ready', org_id: 1, org_name: '总部' },
        identity_scope: 'deployment_operator',
      },
    });
    mocks.getOrganizations.mockResolvedValue({ data: [{ id: 1, name: '总部' }, { id: 2, name: '分部' }] });
    mocks.getProjects.mockResolvedValue({ data: [{ id: 31, org_id: 1, name: '核心项目', status: 'active' }], total: 1 });
	mocks.getUsers.mockImplementation((_page: number, _size: number, provider: 'local' | 'enterprise') => Promise.resolve({
		data: [provider === 'enterprise' ? {
        id: 9,
        org_id: 1,
        org_name: '总部',
        username: 'oidc-user',
        email: 'user@example.com',
        auth_provider: 'oidc',
        organization_role: 'member',
        role: 'developer',
        status: 'active',
        project_ids: [31],
        project_names: '核心项目',
			created_at: '2026-07-15T00:00:00Z',
		} : {
			id: 8, org_id: 1, org_name: '总部', username: 'local-user', email: 'local@example.com',
			auth_provider: 'local', organization_role: 'member', role: 'member', status: 'active',
			project_ids: [], project_names: '', created_at: '2026-07-15T00:00:00Z',
		}],
      total: 1,
	}));
    mocks.updateUser.mockResolvedValue({ success: true });
    mocks.upsertProjectMember.mockResolvedValue({ success: true });
  });

  it('keeps OIDC source fields read-only while allowing UniRouter-owned role, status, and offboarding controls', async () => {
    render(<UsersPage />);

	expect(await screen.findByText('local-user')).toBeTruthy();
	expect(mocks.getUsers).toHaveBeenCalledWith(1, 20, 'local');
	fireEvent.click(screen.getByText('企业身份 / OIDC'));
	expect(await screen.findByText('oidc-user')).toBeTruthy();
	expect(mocks.getUsers).toHaveBeenCalledWith(1, 20, 'enterprise');
    expect(mocks.getOrganizations).not.toHaveBeenCalled();
    expect(screen.queryByLabelText('所属组织')).toBeNull();
    expect(screen.getByText('总部')).toBeTruthy();
    expect(screen.getByText('user@example.com')).toBeTruthy();
    expect(screen.getByText('企业 OIDC')).toBeTruthy();
    expect(screen.getByText('启用')).toBeTruthy();

	expect((screen.getByRole('button', { name: '编辑用户 oidc-user' }) as HTMLButtonElement).disabled).toBe(false);
	expect((screen.getByRole('button', { name: '离场用户 oidc-user' }) as HTMLButtonElement).disabled).toBe(false);
	expect(screen.queryByText('新增本地用户')).toBeNull();
	expect(screen.queryByText(/新密码/)).toBeNull();
	expect(screen.getByText(/OIDC \/ 企业目录拥有登录来源与外部身份属性；UniRouter 管理已映射账号的组织角色、启停与离场/)).toBeTruthy();

	fireEvent.click(screen.getByRole('button', { name: '编辑用户 oidc-user' }));
	expect(await screen.findByText('编辑 UniRouter 权限与状态')).toBeTruthy();
	expect(screen.getByText(/登录来源、外部 subject、用户名与邮箱由 OIDC \/ 企业目录提供/)).toBeTruthy();
	expect((screen.getByLabelText('用户名') as HTMLInputElement).disabled).toBe(true);
	expect(screen.queryByLabelText('初始密码')).toBeNull();
	fireEvent.click(screen.getByText('更新'));
	await waitFor(() => expect(mocks.updateUser).toHaveBeenCalledWith(9, { organization_role: 'member', status: 'active' }));
  });

  it('does not present an empty user directory before the initial request completes', async () => {
    const usersRequest = deferred<any>();
    mocks.getUsers.mockReturnValue(usersRequest.promise);

    render(<UsersPage />);

    expect(await screen.findByRole('heading', { name: '正在加载用户列表' })).toBeTruthy();
    expect(screen.queryByRole('heading', { name: '暂无用户' })).toBeNull();

    await act(async () => {
      usersRequest.resolve({ data: [], total: 0 });
      await Promise.resolve();
    });
    expect(await screen.findByRole('heading', { name: '暂无用户' })).toBeTruthy();
  });

  it('creates a local user with an explicit organization', async () => {
    mocks.createUser.mockResolvedValue({ id: 10 });
    render(<UsersPage />);

	await screen.findByText('local-user');
    fireEvent.click(screen.getByText('新增本地用户'));

    expect(await screen.findByText('本地登录名在当前部署中全局唯一，忽略大小写和首尾空白。')).toBeTruthy();

    fireEvent.change(await screen.findByLabelText('用户名'), { target: { value: 'local-user' } });
    fireEvent.change(screen.getByLabelText('初始密码'), { target: { value: 'secret-password-123' } });
    fireEvent.click(screen.getByText('创建'));

    await waitFor(() => expect(mocks.createUser).toHaveBeenCalledWith({
      org_id: 1,
      username: 'local-user',
      password: 'secret-password-123',
      organization_role: 'member',
      status: 'active',
    }));
  });

  it('grants normal model use through project membership instead of an advanced model grant', async () => {
    render(<UsersPage />);

    await screen.findByText('local-user');
    fireEvent.click(screen.getByRole('button', { name: '授予项目访问 local-user' }));

    expect(await screen.findByText(/先加入一个项目，再由项目使用者在“项目工作台”创建自己的项目访问凭证/)).toBeTruthy();
    await waitFor(() => expect(mocks.getProjects).toHaveBeenCalled());
    fireEvent.click(screen.getByRole('combobox', { name: '项目' }));
    fireEvent.click(await screen.findByText('核心项目'));
    fireEvent.click(screen.getByRole('button', { name: '授予项目访问' }));

    await waitFor(() => expect(mocks.upsertProjectMember).toHaveBeenCalledWith(31, 8, 'project_user'));
  });

  it('shows a safe conflict message when a local login ID is unavailable', async () => {
    mocks.createUser.mockRejectedValue(new ApiError('internal detail must not be shown', 409, {
      error: 'Local login ID is unavailable',
      code: 'local_login_id_conflict',
    }));
    render(<UsersPage />);

	await screen.findByText('local-user');
    fireEvent.click(screen.getByText('新增本地用户'));
    fireEvent.change(await screen.findByLabelText('用户名'), { target: { value: 'Taken-User' } });
    fireEvent.change(screen.getByLabelText('初始密码'), { target: { value: 'secret-password-123' } });
    fireEvent.click(screen.getByText('创建'));

    expect(await screen.findByText('本地登录名不可用，请使用其他登录名')).toBeTruthy();
    expect(screen.queryByText('internal detail must not be shown')).toBeNull();
  });

  it('preserves a resource-limit conflict instead of relabeling it as a login-ID conflict', async () => {
    const licenseMessage = 'commercial License limit exceeded for active_users: 10 active, limit 10';
    mocks.createUser.mockRejectedValue(new ApiError(licenseMessage, 409, {
      error: licenseMessage,
      code: 'license_error.resource_limit_exceeded',
    }));
    render(<UsersPage />);

	await screen.findByText('local-user');
    fireEvent.click(screen.getByText('新增本地用户'));
    fireEvent.change(await screen.findByLabelText('用户名'), { target: { value: 'new-user' } });
    fireEvent.change(screen.getByLabelText('初始密码'), { target: { value: 'secret-password-123' } });
    fireEvent.click(screen.getByText('创建'));

    expect(await screen.findByText(licenseMessage)).toBeTruthy();
    expect(screen.queryByText('本地登录名不可用，请使用其他登录名')).toBeNull();
  });

  it('counts the minimum password length in Unicode code points', async () => {
    mocks.createUser.mockResolvedValue({ id: 10 });
    render(<UsersPage />);

	await screen.findByText('local-user');
    fireEvent.click(screen.getByText('新增本地用户'));
    fireEvent.change(await screen.findByLabelText('用户名'), { target: { value: 'unicode-user' } });
    fireEvent.change(screen.getByLabelText('初始密码'), { target: { value: '😀'.repeat(12) } });
    fireEvent.click(screen.getByText('创建'));

    await waitFor(() => expect(mocks.createUser).toHaveBeenCalledWith(expect.objectContaining({
      username: 'unicode-user',
      password: '😀'.repeat(12),
    })));
  });

  it('rejects fewer than 12 Unicode code points even when UTF-16 has 22 code units', async () => {
    render(<UsersPage />);

	await screen.findByText('local-user');
    fireEvent.click(screen.getByText('新增本地用户'));
    fireEvent.change(await screen.findByLabelText('用户名'), { target: { value: 'short-unicode-user' } });
    fireEvent.change(screen.getByLabelText('初始密码'), { target: { value: '😀'.repeat(11) } });
    fireEvent.click(screen.getByText('创建'));

    expect(await screen.findByText('至少 12 个 Unicode 字符（按码点计）')).toBeTruthy();
    expect(mocks.createUser).not.toHaveBeenCalled();
  });

  it('accepts exactly 72 UTF-8 bytes and rejects 73 UTF-8 bytes', async () => {
    mocks.createUser.mockResolvedValue({ id: 10 });
    const { unmount } = render(<UsersPage />);

	await screen.findByText('local-user');
    fireEvent.click(screen.getByText('新增本地用户'));
    fireEvent.change(await screen.findByLabelText('用户名'), { target: { value: 'byte-boundary-user' } });
    fireEvent.change(screen.getByLabelText('初始密码'), { target: { value: '界'.repeat(24) } });
    fireEvent.click(screen.getByText('创建'));

    await waitFor(() => expect(mocks.createUser).toHaveBeenCalledWith(expect.objectContaining({
      username: 'byte-boundary-user',
      password: '界'.repeat(24),
    })));

    unmount();
    vi.clearAllMocks();
    mocks.getOrganizations.mockResolvedValue({ data: [{ id: 1, name: '总部' }] });
    mocks.getProjects.mockResolvedValue({ data: [], total: 0 });
    mocks.getUsers.mockResolvedValue({ data: [], total: 0 });
    render(<UsersPage />);

    await screen.findByText('暂无用户');
    fireEvent.click(screen.getByText('新增本地用户'));
    fireEvent.change(await screen.findByLabelText('用户名'), { target: { value: 'over-byte-boundary-user' } });
    fireEvent.change(screen.getByLabelText('初始密码'), { target: { value: `${'界'.repeat(23)}😀` } });
    fireEvent.click(screen.getByText('创建'));

    expect(await screen.findByText('最多 72 个 UTF-8 字节')).toBeTruthy();
    expect(mocks.createUser).not.toHaveBeenCalled();
  });

  it('passes the selected disabled status when creating a local user', async () => {
    mocks.createUser.mockResolvedValue({ id: 10 });
    render(<UsersPage />);

	await screen.findByText('local-user');
    fireEvent.click(screen.getByText('新增本地用户'));

    fireEvent.change(await screen.findByLabelText('用户名'), { target: { value: 'disabled-user' } });
    fireEvent.change(screen.getByLabelText('初始密码'), { target: { value: 'secret-password-123' } });
    fireEvent.click(screen.getByRole('combobox', { name: '状态' }));
    fireEvent.click(await screen.findByText('禁用'));
    fireEvent.click(screen.getByText('创建'));

    await waitFor(() => expect(mocks.createUser).toHaveBeenCalledWith({
      org_id: 1,
      username: 'disabled-user',
      password: 'secret-password-123',
      organization_role: 'member',
      status: 'disabled',
    }));
  });

  it('prevents editing or deleting the current account', async () => {
    mocks.getUsers.mockResolvedValue({
      data: [{
        id: 1,
        org_id: 1,
        org_name: '总部',
        username: 'admin',
        auth_provider: 'local',
        role: 'platform_admin',
        status: 'active',
        created_at: '2026-07-15T00:00:00Z',
      }],
      total: 1,
    });

    render(<UsersPage />);

    expect(await screen.findByText('当前账号')).toBeTruthy();
    expect((screen.getByRole('button', { name: '编辑用户 admin' }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole('button', { name: '离场用户 admin' }) as HTMLButtonElement).disabled).toBe(true);
  });

  it('keeps platform-admin rows read-only for an organization admin', async () => {
    useAuthStore.setState({
      user: { id: 2, username: 'org-admin', role: 'org_admin', org_id: 1, org_name: '总部' },
    });
    mocks.getUsers.mockResolvedValue({
      data: [{
        id: 1,
        org_id: 1,
        org_name: '总部',
        username: 'platform-admin',
        auth_provider: 'local',
        role: 'platform_admin',
        status: 'active',
        created_at: '2026-07-15T00:00:00Z',
      }],
      total: 1,
    });

    render(<UsersPage />);

    await screen.findByText('platform-admin');
    expect((screen.getByRole('button', { name: '编辑用户 platform-admin' }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole('button', { name: '离场用户 platform-admin' }) as HTMLButtonElement).disabled).toBe(true);
  });

  it('does not present a failed user request as an empty directory', async () => {
    mocks.getUsers.mockRejectedValueOnce(new Error('unavailable'));

    render(<UsersPage />);

    expect(await screen.findByRole('heading', { name: '用户列表加载失败' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '重新加载用户列表' })).toBeTruthy();
  });

  it('blocks customer user operations when a platform session has no canonical customer', () => {
    useAuthStore.setState({
      user: {
        id: 1, username: 'admin', role: 'platform_admin', org_id: 99,
        customer_context: { state: 'ambiguous' }, identity_scope: 'deployment_operator',
      },
    });

    render(<UsersPage />);

    expect(screen.getByRole('heading', { name: '客户组织上下文不唯一' })).toBeTruthy();
    expect(mocks.getUsers).not.toHaveBeenCalled();
    expect(mocks.getOrganizations).not.toHaveBeenCalled();
  });
});
