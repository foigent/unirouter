import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { Route, Routes, useLocation } from 'react-router-dom';
import App from '../App';
import { api } from '../api/client';
import { useAuthStore } from '../store/auth';
import type { LicenseAccess } from '../navigation';
import { MemoryRouter } from '../test/MemoryRouter';

const PARTIAL_LICENSE: LicenseAccess = {
  status: 'active',
  enforceable: true,
  features: {
    gateway: true,
    agent_capabilities: false,
    oidc: false,
    guardrails: true,
    notifications: true,
    model_governance: true,
    data_exchange: false,
  },
};

function LocationProbe() {
  const location = useLocation();
  return <div data-testid="location">{location.pathname}{location.search}</div>;
}

function renderApp(
  role = 'platform_admin',
  options: { initialEntry?: string; licenseAccess?: LicenseAccess | null; hasLiveProjectMembership?: boolean } = {},
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
	  navigation_capabilities: options.hasLiveProjectMembership === undefined ? undefined : {
		has_live_project_membership: options.hasLiveProjectMembership,
		has_project_admin_membership: false,
		can_discover_access_review: role === 'platform_admin' || role === 'org_admin',
		can_discover_workloads: role === 'platform_admin' || role === 'org_admin',
	  },
    },
    profileLoading: false,
  });

  return render(
    <MemoryRouter initialEntries={[options.initialEntry || '/']}>
      <Routes>
        <Route path="/login" element={<div>Login</div>} />
        <Route path="/" element={<><LocationProbe /><App /></>}>
          <Route index element={<div>Dashboard</div>} />
          <Route path="logs" element={<div>Logs</div>} />
          <Route path="notifications" element={<div>Notifications</div>} />
          <Route path="settings" element={<div>Account Settings</div>} />
          <Route path="license" element={<div>License Page</div>} />
          <Route path="channels" element={<div>Model Endpoint Safety Surface</div>} />
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

    const input = screen.getByRole('textbox', { name: '全局搜索执行请求' });
    expect(input).toHaveAttribute('placeholder', '搜索请求日志：请求、模型、供应商、错误或证据标识');
    fireEvent.change(input, { target: { value: 'req-123' } });
    fireEvent.keyDown(input, { key: 'Enter', code: 'Enter', charCode: 13 });

    await waitFor(() => {
      expect(screen.getByTestId('location')).toHaveTextContent('/logs?search=req-123');
    });
  });

  it('keeps global utilities in a lightweight non-navigation toolbar', () => {
    renderApp();

    const toolbar = screen.getByRole('toolbar', { name: '全局工具栏' });
    expect(toolbar.classList.contains('ur-topbar')).toBe(true);
    expect(toolbar.querySelector('nav')).toBeNull();
    expect(toolbar).toContainElement(screen.getByPlaceholderText('搜索请求日志：请求、模型、供应商、错误或证据标识'));
    expect(toolbar).toContainElement(screen.getByRole('button', { name: '打开我的账号' }));
  });

	it('hides end-user entries from a pure management identity', () => {
		renderApp('platform_admin', { hasLiveProjectMembership: false });

		expect(screen.queryByText('访问申请与授权')).toBeNull();
		expect(screen.queryByText('项目工作台')).toBeNull();
		expect(screen.getByText('客户运营总览')).toBeTruthy();
	});

	it('keeps end-user entries hidden when a management identity has project membership data', () => {
		renderApp('platform_admin', { hasLiveProjectMembership: true });

		expect(screen.queryByText('访问申请与授权')).toBeNull();
		expect(screen.queryByText('项目工作台')).toBeNull();
		expect(screen.getByText('客户运营总览')).toBeTruthy();
	});

	it('keeps access requests but hides the project workbench for a member awaiting first membership', () => {
		renderApp('member', { hasLiveProjectMembership: false });

		expect(screen.getByText('访问申请与授权')).toBeTruthy();
		expect(screen.queryByText('项目工作台')).toBeNull();
	});

	it('places the project workbench before access history for a live member', () => {
		renderApp('member', { hasLiveProjectMembership: true });

		const workbench = screen.getByText('项目工作台');
		const access = screen.getByText('访问申请与授权');
		expect(workbench.compareDocumentPosition(access) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
		expect(screen.queryByText('API 与兼容性')).toBeNull();
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
    expect(screen.getByLabelText('UniRouter 版本 V1.2.17')).toHaveTextContent('V1.2.17');
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
    fireEvent.click(await screen.findByText('执行请求'));

    await waitFor(() => expect(screen.getByTestId('location')).toHaveTextContent('/logs'));
    expect(container.querySelector('.ur-sider.open')).toBeNull();
  });

  it('keeps every secondary navigation section expanded without another disclosure click', () => {
    renderApp();

    expect(screen.getByText('模型与端点')).toBeTruthy();
    expect(screen.getByText('人员与权限')).toBeTruthy();
    expect(screen.getByText('执行请求')).toBeTruthy();
    expect(screen.getByText('预算与成本中心')).toBeTruthy();
    expect(screen.getByText('部署与网络')).toBeTruthy();
    expect(screen.getByText('集成管理')).toBeTruthy();
    expect(screen.getByText('API 与兼容性')).toBeTruthy();
  });

  it('hides request-log search from roles without request-log access', () => {
    renderApp('org_viewer');

    expect(screen.queryByPlaceholderText('搜索请求日志：请求、模型、供应商、错误或证据标识')).toBeNull();
  });

  it('hides the unscoped notification bell from project roles', () => {
    renderApp('project_admin');

    expect(screen.queryByRole('button', { name: '通知' })).toBeNull();
  });

  it('keeps core management workspaces available with an enforceable commercial License', async () => {
    renderApp('platform_admin', { licenseAccess: PARTIAL_LICENSE });

    expect(screen.queryByText('模型治理')).toBeNull();
    expect(await screen.findByText('模型与端点')).toBeTruthy();
    expect(await screen.findByText('内容安全')).toBeTruthy();
    expect(screen.getByRole('button', { name: '通知' }).hasAttribute('disabled')).toBe(false);
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

  it('keeps the model endpoint safety surface mounted when the gateway License is invalid', () => {
    renderApp('platform_admin', {
      initialEntry: '/channels?tab=credentials',
      licenseAccess: { ...PARTIAL_LICENSE, status: 'invalid', enforceable: false },
    });

    expect(screen.getByText('Model Endpoint Safety Surface')).toBeTruthy();
    expect(screen.queryByText('模型网关暂不可用')).toBeNull();
    expect(screen.getByText('模型与端点')).toBeTruthy();
    expect(screen.queryByText('模型与端点（未授权）')).toBeNull();
  });

  it('gives platform administrators a direct path to repair a restricted license', async () => {
    renderApp('platform_admin', {
      initialEntry: '/guardrail-policies',
      licenseAccess: { ...PARTIAL_LICENSE, status: 'invalid', enforceable: false },
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
    expect(screen.queryByText('X-Insight 数据交换暂不可用')).toBeNull();
  });
});
