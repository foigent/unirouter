import { beforeEach, describe, expect, it, vi } from 'vitest';
import { api, ApiError } from '../../api/client';
import { useAuthStore } from '../auth';

describe('auth profile loading', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    localStorage.clear();
    useAuthStore.setState({
      token: null,
      user: null,
      profileLoading: false,
      profileError: null,
      isAuthenticated: false,
    });
  });

  it('keeps the existing session when profile state is temporarily unavailable', async () => {
    localStorage.setItem('token', 'existing-token');
    useAuthStore.setState({ token: 'existing-token', isAuthenticated: true });
    vi.spyOn(api, 'getMe').mockRejectedValue(new ApiError('temporarily unavailable', 503, null));

    await expect(useAuthStore.getState().loadProfile()).rejects.toMatchObject({ status: 503 });

    expect(localStorage.getItem('token')).toBe('existing-token');
    expect(useAuthStore.getState()).toMatchObject({
      token: 'existing-token',
      isAuthenticated: true,
      user: null,
      profileLoading: false,
      profileError: '认证服务暂时不可用，请稍后重试。',
    });
  });

  it('clears the session when the profile request is unauthorized', async () => {
    localStorage.setItem('token', 'revoked-token');
    useAuthStore.setState({ token: 'revoked-token', isAuthenticated: true });
    vi.spyOn(api, 'getMe').mockRejectedValue(new ApiError('unauthorized', 401, null));

    await expect(useAuthStore.getState().loadProfile()).rejects.toMatchObject({ status: 401 });

    expect(localStorage.getItem('token')).toBeNull();
    expect(useAuthStore.getState()).toMatchObject({
      token: null,
      isAuthenticated: false,
      user: null,
      profileLoading: false,
      profileError: null,
    });
  });
});
