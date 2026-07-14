import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import UsersPage from '../UsersPage';

const mocks = vi.hoisted(() => ({
  getUsers: vi.fn(),
  createUser: vi.fn(),
  updateUser: vi.fn(),
  deleteUser: vi.fn(),
}));

vi.mock('../../api/client', () => ({ api: mocks }));

describe('UsersPage enterprise identity presentation', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getUsers.mockResolvedValue({
      data: [{
        id: 9,
        org_id: 1,
        username: 'oidc-user',
        email: 'user@example.com',
        auth_provider: 'oidc',
        role: 'developer',
        status: 'active',
        created_at: '2026-07-15T00:00:00Z',
      }],
      total: 1,
    });
    mocks.updateUser.mockResolvedValue({ success: true });
  });

  it('shows OIDC identity fields and does not offer a fake password update', async () => {
    render(<UsersPage />);

    expect(await screen.findByText('oidc-user')).toBeTruthy();
    expect(mocks.getUsers).toHaveBeenCalledWith(1, 20);
    expect(screen.getByText('user@example.com')).toBeTruthy();
    expect(screen.getByText('企业 OIDC')).toBeTruthy();
    expect(screen.getByText('启用')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: '编辑用户 oidc-user' }));
    expect(await screen.findByText('编辑用户权限')).toBeTruthy();
    expect(screen.queryByText(/新密码/)).toBeNull();
    expect(screen.queryByText('初始密码')).toBeNull();

    fireEvent.click(screen.getByText('更新'));
    await waitFor(() => expect(mocks.updateUser).toHaveBeenCalledWith(9, expect.objectContaining({
      role: 'developer',
      status: 'active',
    })));
  });
});
