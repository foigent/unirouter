import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import App from '../App';
import { api } from '../api/client';
import { useAuthStore } from '../store/auth';
import type { LicenseAccess } from '../navigation';

const PARTIAL_LICENSE: LicenseAccess = {
  status: 'active',
  enforceable: true,
  features: {
    gateway: true,
    agent_capabilities: false,
    oidc: false,
    guardrails: false,
    notifications: false,
    model_governance: false,
    data_exchange: false,
  },
};

function LocationProbe() {
  const location = useLocation();
  return <div data-testid="location">{location.pathname}{location.search}</div>;
}

function renderApp(
  role = 'platform_admin',
  options: { initialEntry?: string; licenseAccess?: LicenseAccess | null } = {},
) {
  useAuthStore.setState({
    token: 'token',
    isAuthenticated: true,
    user: {
      id: 1,
      username: 'admin',
      role,
      org_id: 1,
      org_name: '示例组织',
      license_access: options.licenseAccess,
    },
    profileLoading: false,
  });

  return render(
    <MemoryRouter initialEntries={[options.initialEntry || '/']}>
      <Routes>
        <Route path="/" element={<><LocationProbe /><App /></>}>
          <Route index element={<div>Dashboard</div>} />
          <Route path="logs" element={<div>Logs</div>} />
          <Route path="notifications" element={<div>Notifications</div>} />
          <Route path="settings" element={<div>Account Settings</div>} />
          <Route path="license" element={<div>License Page</div>} />
          <Route path="guardrail-policies" element={<div>Restricted Child Page</div>} />
          <Route path="data-exchange" element={<div>Data Exchange Safety Surface</div>} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );
}

describe('App shell', () => {
  beforeEach(() => {
	vi.restoreAllMocks();
	localStorage.clear();
    useAuthStore.setState({
      token: null,
      user: null,
      profileLoading: false,
      isAuthenticated: false,
    });
  });

  it('keeps the session until the server confirms logout revocation', async () => {
	let confirmRevocation!: () => void;
	vi.spyOn(api, 'logout').mockImplementation(() => new Promise<void>((resolve) => {
	  confirmRevocation = resolve;
	}));
	localStorage.setItem('token', 'active-token');
	renderApp();

	const logoutButton = screen.getByRole('button', { name: /退出登录/ });
	fireEvent.click(logoutButton);

	expect(api.logout).toHaveBeenCalledTimes(1);
	expect(useAuthStore.getState().isAuthenticated).toBe(true);
	expect(logoutButton.hasAttribute('disabled')).toBe(true);
	confirmRevocation();
	await waitFor(() => expect(useAuthStore.getState().isAuthenticated).toBe(false));
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

  it('opens account settings from the avatar chip', async () => {
    renderApp();

    fireEvent.click(screen.getByRole('button', { name: '打开我的账号' }));

    await waitFor(() => {
      expect(screen.getByTestId('location')).toHaveTextContent('/settings');
    });
  });

  it('keeps an account-settings entry in the mobile sidebar and closes it after navigation', async () => {
    const { container } = renderApp();

    fireEvent.click(screen.getByRole('button', { name: '打开导航' }));
    expect(container.querySelector('.ur-sider.open')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '打开移动端我的账号' }));

    await waitFor(() => expect(screen.getByTestId('location')).toHaveTextContent('/settings'));
    expect(container.querySelector('.ur-sider.open')).toBeNull();
  });

  it('closes the mobile sidebar after selecting a navigation item', async () => {
    const { container } = renderApp();

    fireEvent.click(screen.getByRole('button', { name: '打开导航' }));
    fireEvent.click(screen.getByText('监控与用量'));
    fireEvent.click(await screen.findByText('请求日志'));

    await waitFor(() => expect(screen.getByTestId('location')).toHaveTextContent('/logs'));
    expect(container.querySelector('.ur-sider.open')).toBeNull();
  });

  it('hides request-log search from roles without request-log access', () => {
    renderApp('org_viewer');

    expect(screen.queryByPlaceholderText('搜索请求日志：请求、模型、供应商、错误或证据标识')).toBeNull();
  });

  it('hides the unscoped notification bell from project roles', () => {
    renderApp('project_admin');

    expect(screen.queryByRole('button', { name: '通知' })).toBeNull();
  });

  it('keeps licensed-by-role navigation visible but locked and disables the notification shortcut', async () => {
    renderApp('platform_admin', { licenseAccess: PARTIAL_LICENSE });

    fireEvent.click(screen.getByText('模型接入'));
    expect(await screen.findByText('模型治理（授权未包含）')).toBeTruthy();
    fireEvent.click(screen.getByText('访问与安全'));
    expect(await screen.findByText('内容安全（授权未包含）')).toBeTruthy();
    expect(screen.getByRole('button', { name: '通知（授权未包含）' }).hasAttribute('disabled')).toBe(true);
  });

  it('replaces a role-allowed direct URL with one unified license explanation before the page mounts', () => {
    renderApp('org_admin', {
      initialEntry: '/guardrail-policies',
      licenseAccess: { ...PARTIAL_LICENSE, status: 'expired', enforceable: false },
    });

    expect(screen.getByText('内容安全暂不可用')).toBeTruthy();
    expect(screen.getAllByText(/当前商业授权已过期/).length).toBeGreaterThan(0);
    expect(screen.queryByText('Restricted Child Page')).toBeNull();
    expect(screen.queryByRole('button', { name: '查看或更新商业授权' })).toBeNull();
  });

  it('gives platform administrators a direct path to repair a restricted license', async () => {
    renderApp('platform_admin', {
      initialEntry: '/guardrail-policies',
      licenseAccess: PARTIAL_LICENSE,
    });

    fireEvent.click(screen.getByRole('button', { name: '查看或更新商业授权' }));
    await waitFor(() => expect(screen.getByTestId('location')).toHaveTextContent('/license'));
  });

  it('keeps the X-Insight safety surface mounted when Data Exchange entitlement is unavailable', () => {
    renderApp('platform_admin', {
      initialEntry: '/data-exchange',
      licenseAccess: { ...PARTIAL_LICENSE, status: 'expired', enforceable: false },
    });

    expect(screen.getByText('Data Exchange Safety Surface')).toBeTruthy();
    expect(screen.queryByText('X-Insight 数据交换控制暂不可用')).toBeNull();
  });
});
