import { create } from 'zustand';
import { api, ApiError } from '../api/client';
import type { LicenseAccess } from '../navigation';

export interface UserProfile {
  id: number;
  username: string;
  role?: string;
  organization_role?: string;
  org_id?: number;
  org_name?: string;
  status?: string;
  email?: string;
  auth_provider?: string;
  // undefined means the current session has not loaded /me yet. null means a
  // rolling-upgrade backend returned no capability projection, so the client
  // must not treat that response as positive entitlement evidence.
  license_access?: LicenseAccess | null;
}

export function profileNeedsRefresh(user: UserProfile | null) {
  return !user?.role || user.license_access === undefined;
}

interface AuthState {
  token: string | null;
  user: UserProfile | null;
  profileLoading: boolean;
  profileError: string | null;
  isAuthenticated: boolean;
  login: (username: string, password: string) => Promise<UserProfile>;
  logout: () => Promise<void>;
  clearSession: () => void;
  checkAuth: () => void;
  loadProfile: () => Promise<UserProfile>;
}

export const useAuthStore = create<AuthState>((set) => ({
  token: localStorage.getItem('token'),
  user: null,
  profileLoading: false,
  profileError: null,
  isAuthenticated: !!localStorage.getItem('token'),

  login: async (username, password) => {
    const data = await api.login(username, password);
    if (data.token) {
      const user: UserProfile = {
        id: data.user_id,
        username: data.username,
        role: data.role,
        organization_role: data.organization_role,
        org_id: data.org_id,
        org_name: data.org_name,
        status: data.status,
        email: data.email,
        auth_provider: data.auth_provider,
        license_access: data.license_access ?? undefined,
      };
      localStorage.setItem('token', data.token);
      set({ token: data.token, isAuthenticated: true, user, profileError: null });
      return user;
    } else {
      throw new Error(data.error || 'Login failed');
    }
  },

  logout: async () => {
    try {
      await api.logout();
    } catch (err) {
      // A 401 is authoritative evidence that the bearer is already unusable;
      // other failures retain the local session so the UI cannot claim that a
      // server-side revocation succeeded when it did not.
      if (!(err instanceof ApiError) || err.status !== 401) throw err;
    }
    localStorage.removeItem('token');
    set({ token: null, isAuthenticated: false, user: null, profileLoading: false, profileError: null });
  },

  clearSession: () => {
    localStorage.removeItem('token');
    set({ token: null, isAuthenticated: false, user: null, profileLoading: false, profileError: null });
  },

  checkAuth: () => {
    const token = localStorage.getItem('token');
    set({ isAuthenticated: !!token, token });
  },

  loadProfile: async () => {
    set({ profileLoading: true, profileError: null });
    try {
      const data = await api.getMe();
      const user: UserProfile = {
        id: data.id,
        username: data.username,
        role: data.role,
        organization_role: data.organization_role,
        org_id: data.org_id,
        org_name: data.org_name,
        status: data.status,
        email: data.email,
        auth_provider: data.auth_provider,
        license_access: data.license_access ?? null,
      };
      set({ user, isAuthenticated: true, profileLoading: false, profileError: null });
      return user;
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        localStorage.removeItem('token');
        set({ user: null, isAuthenticated: false, profileLoading: false, profileError: null, token: null });
      } else {
        set({ profileLoading: false, profileError: '认证服务暂时不可用，请稍后重试。' });
      }
      throw err;
    }
  },
}));
