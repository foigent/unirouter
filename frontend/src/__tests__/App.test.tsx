import { beforeEach, describe, expect, it } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import App from '../App';
import { useAuthStore } from '../store/auth';

function LocationProbe() {
  const location = useLocation();
  return <div data-testid="location">{location.pathname}{location.search}</div>;
}

function renderApp() {
  useAuthStore.setState({
    token: 'token',
    isAuthenticated: true,
    user: { id: 1, username: 'admin', role: 'platform_admin', org_id: 1 },
    profileLoading: false,
  });

  return render(
    <MemoryRouter initialEntries={['/']}>
      <Routes>
        <Route path="/" element={<><LocationProbe /><App /></>}>
          <Route index element={<div>Dashboard</div>} />
          <Route path="logs" element={<div>Logs</div>} />
          <Route path="notifications" element={<div>Notifications</div>} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );
}

describe('App shell', () => {
  beforeEach(() => {
    useAuthStore.setState({
      token: null,
      user: null,
      profileLoading: false,
      isAuthenticated: false,
    });
  });

  it('routes topbar search to existing logs search', async () => {
    renderApp();

    const input = screen.getByPlaceholderText('搜索请求日志：请求、模型、供应商、错误或证据标识');
    fireEvent.change(input, { target: { value: 'req-123' } });
    fireEvent.keyDown(input, { key: 'Enter', code: 'Enter', charCode: 13 });

    await waitFor(() => {
      expect(screen.getByTestId('location')).toHaveTextContent('/logs?search=req-123');
    });
  });

  it('routes empty topbar search to the logs overview', async () => {
    renderApp();

    const input = screen.getByPlaceholderText('搜索请求日志：请求、模型、供应商、错误或证据标识');
    fireEvent.keyDown(input, { key: 'Enter', code: 'Enter', charCode: 13 });

    await waitFor(() => {
      expect(screen.getByTestId('location')).toHaveTextContent('/logs');
    });
  });

  it('does not hard-code production when deployment environment is unset', () => {
    renderApp();

    expect(screen.getByText('环境未配置')).toBeTruthy();
    expect(screen.queryByText('生产环境')).toBeNull();
  });
});
