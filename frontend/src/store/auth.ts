import { create } from 'zustand';
import { api } from '../api/client';

interface AuthState {
  token: string | null;
  user: { id: number; username: string } | null;
  isAuthenticated: boolean;
  login: (username: string, password: string) => Promise<void>;
  logout: () => void;
  checkAuth: () => void;
}

export const useAuthStore = create<AuthState>((set) => ({
  token: localStorage.getItem('token'),
  user: null,
  isAuthenticated: !!localStorage.getItem('token'),

  login: async (username, password) => {
    const data = await api.login(username, password);
    if (data.token) {
      localStorage.setItem('token', data.token);
      set({ token: data.token, isAuthenticated: true, user: { id: data.user_id, username: data.username } });
    } else {
      throw new Error(data.error || 'Login failed');
    }
  },

  logout: () => {
    localStorage.removeItem('token');
    set({ token: null, isAuthenticated: false, user: null });
  },

  checkAuth: () => {
    const token = localStorage.getItem('token');
    set({ isAuthenticated: !!token, token });
  },
}));
