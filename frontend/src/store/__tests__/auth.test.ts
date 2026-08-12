import { beforeEach, describe, expect, it, vi } from 'vitest';
import { api, ApiError } from '../../api/client';
import { profileNeedsRefresh, useAuthStore } from '../auth';

const ACTIVE_LICENSE_ACCESS = {
  status: 'active',
  enforceable: true,
  features: {
    gateway: true,
    agent_capabilities: true,
    oidc: true,
    guardrails: true,
    notifications: true,
    model_governance: true,
    data_exchange: true,
  },
} as const;

describe('auth store', () => {
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

  it('marks login-only identity as needing /me and stores the capability projection on refresh', async () => {
    const loginUser = {
      id: 42,
      username: 'alice',
      role: 'org_admin',
      org_id: 22,
      org_name: 'Acme',
      status: 'active',
      email: 'alice@example.com',
      auth_provider: 'oidc',
    };
    const refreshedUser = {
      ...loginUser,
      organization_role: 'org_admin',
      license_access: ACTIVE_LICENSE_ACCESS,
      navigation_capabilities: {
        has_live_project_membership: true,
        has_project_admin_membership: false,
        can_discover_access_review: true,
        can_discover_workloads: true,
      },
      customer_context: { state: 'ready', org_id: 22, org_name: 'Acme' },
      identity_scope: 'customer_identity',
    };
    vi.spyOn(api, 'login').mockResolvedValue({
      token: 'fresh-token',
      user_id: loginUser.id,
      username: loginUser.username,
      role: loginUser.role,
      org_id: loginUser.org_id,
      org_name: loginUser.org_name,
      status: loginUser.status,
      email: loginUser.email,
      auth_provider: loginUser.auth_provider,
    });
    vi.spyOn(api, 'getMe').mockResolvedValue(refreshedUser);

    await expect(useAuthStore.getState().login('alice', 'secret')).resolves.toMatchObject(loginUser);
    expect(localStorage.getItem('token')).toBe('fresh-token');
    expect(useAuthStore.getState()).toMatchObject({
      token: 'fresh-token',
      isAuthenticated: true,
      user: loginUser,
      profileError: null,
    });
    expect(useAuthStore.getState().user?.license_access).toBeUndefined();
    expect(profileNeedsRefresh(useAuthStore.getState().user)).toBe(true);

    await expect(useAuthStore.getState().loadProfile()).resolves.toEqual(refreshedUser);
    expect(useAuthStore.getState().user).toEqual(refreshedUser);
    expect(profileNeedsRefresh(useAuthStore.getState().user)).toBe(false);
  });

  it('uses null as a one-shot rolling-upgrade sentinel when /me has no capability field', async () => {
    vi.spyOn(api, 'getMe').mockResolvedValue({
      id: 7,
      username: 'legacy-backend-user',
      role: 'org_admin',
      org_id: 1,
    });

    await useAuthStore.getState().loadProfile();

    expect(api.getMe).toHaveBeenCalledTimes(1);
    expect(useAuthStore.getState().user?.license_access).toBeNull();
    expect(profileNeedsRefresh(useAuthStore.getState().user)).toBe(false);
  });

  it('does not create a local session when login is denied', async () => {
    vi.spyOn(api, 'login').mockRejectedValue(new ApiError('Account is disabled', 403, null));

    await expect(useAuthStore.getState().login('alice', 'secret')).rejects.toMatchObject({ status: 403 });

    expect(localStorage.getItem('token')).toBeNull();
    expect(useAuthStore.getState()).toMatchObject({
      token: null,
      isAuthenticated: false,
      user: null,
    });
  });

  it('waits for server revocation before clearing the local session', async () => {
    localStorage.setItem('token', 'active-token');
    useAuthStore.setState({ token: 'active-token', isAuthenticated: true });
    let confirmRevocation!: () => void;
    vi.spyOn(api, 'logout').mockImplementation(() => new Promise<void>((resolve) => {
      confirmRevocation = resolve;
    }));

    const pending = useAuthStore.getState().logout();
    expect(localStorage.getItem('token')).toBe('active-token');
    expect(useAuthStore.getState().isAuthenticated).toBe(true);

    confirmRevocation();
    await pending;
    expect(localStorage.getItem('token')).toBeNull();
    expect(useAuthStore.getState()).toMatchObject({ token: null, isAuthenticated: false, user: null });
  });

  it('keeps the local session when the server cannot confirm revocation', async () => {
    localStorage.setItem('token', 'active-token');
    useAuthStore.setState({ token: 'active-token', isAuthenticated: true });
    vi.spyOn(api, 'logout').mockRejectedValue(new ApiError('temporarily unavailable', 503, null));

    await expect(useAuthStore.getState().logout()).rejects.toMatchObject({ status: 503 });

    expect(localStorage.getItem('token')).toBe('active-token');
    expect(useAuthStore.getState()).toMatchObject({ token: 'active-token', isAuthenticated: true });
  });

  it('clears the local session when the server says the bearer is already invalid', async () => {
    localStorage.setItem('token', 'already-revoked-token');
    useAuthStore.setState({ token: 'already-revoked-token', isAuthenticated: true });
    vi.spyOn(api, 'logout').mockRejectedValue(new ApiError('already invalid', 401, null));

    await expect(useAuthStore.getState().logout()).resolves.toBeUndefined();

    expect(localStorage.getItem('token')).toBeNull();
    expect(useAuthStore.getState()).toMatchObject({ token: null, isAuthenticated: false });
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
