import { create } from 'zustand';
import { api } from '../api/client';

export interface UserProfile {
  id: number;
  username: string;
  role?: string;
  org_id?: number;
  org_name?: string;
  status?: string;
}

interface AuthState {
  token: string | null;
  user: UserProfile | null;
  profileLoading: boolean;
  isAuthenticated: boolean;
  login: (username: string, password: string) => Promise<UserProfile>;
  logout: () => void;
  checkAuth: () => void;
  loadProfile: () => Promise<UserProfile>;
}

export const useAuthStore = create<AuthState>((set) => ({
  token: localStorage.getItem('token'),
  user: null,
  profileLoading: false,
  isAuthenticated: !!localStorage.getItem('token'),

  login: async (username, password) => {
    const data = await api.login(username, password);
    if (data.token) {
      const user = {
        id: data.user_id,
        username: data.username,
        role: data.role,
        org_id: data.org_id,
      };
      localStorage.setItem('token', data.token);
      set({ token: data.token, isAuthenticated: true, user });
      return user;
    } else {
      throw new Error(data.error || 'Login failed');
    }
  },

  logout: () => {
    localStorage.removeItem('token');
    set({ token: null, isAuthenticated: false, user: null, profileLoading: false });
  },

  checkAuth: () => {
    const token = localStorage.getItem('token');
    set({ isAuthenticated: !!token, token });
  },

  loadProfile: async () => {
    set({ profileLoading: true });
    try {
      const data = await api.getMe();
      const user = {
        id: data.id,
        username: data.username,
        role: data.role,
        org_id: data.org_id,
        org_name: data.org_name,
        status: data.status,
      };
      set({ user, isAuthenticated: true, profileLoading: false });
      return user;
    } catch (err) {
      localStorage.removeItem('token');
      set({ user: null, isAuthenticated: false, profileLoading: false, token: null });
      throw err;
    }
  },
}));
