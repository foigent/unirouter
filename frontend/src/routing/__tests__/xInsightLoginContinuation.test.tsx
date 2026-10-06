import { StrictMode, useEffect, useState } from 'react';
import { BrowserRouter, Navigate, Route, Routes, useLocation } from 'react-router-dom';
import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import userEvent from '@testing-library/user-event';
import ProtectedRoute from '../ProtectedRoute';
import LoginPage from '../../pages/LoginPage';
import { useAuthStore, type UserProfile } from '../../store/auth';
import { request } from '../../api/transport';
import { ROUTER_FUTURE_FLAGS } from '../routerFuture';
import { captureXInsightLoginContinuation, clearXInsightLoginContinuation, finishXInsightLoginContinuation, hasXInsightLoginContinuation, isXInsightInvitationLocation, isXInsightRecoveryLocation, preservesXInsightLoginOn401 } from '../xInsightLoginContinuation';

// Tooltip visibility is covered by LoginPage/PasswordField tests. Keep this
// auth/routing suite independent of Semi's known tooltip compatibility debt.
vi.mock('@douyinfe/semi-ui', async original => ({
  ...await original<typeof import('@douyinfe/semi-ui')>(),
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

const token = `xiiv1_${'A'.repeat(43)}`;
const fragment = '#' + new URLSearchParams({ invitation: token, xinsight_origin: 'https://xi.example' });
const install = '/connectors/x-insight/install';
const freshProfile: UserProfile = {
  id: 92, username: 'new-admin', role: 'org_admin', organization_role: 'org_admin', org_id: 3,
  customer_context: { state: 'ready', org_id: 3, org_name: 'Current organization' },
  identity_scope: 'customer_identity',
  navigation_capabilities: { has_live_project_membership: true, has_project_admin_membership: true, can_discover_access_review: true, can_discover_workloads: true },
  license_access: { status: 'active', enforceable: true, features: {
    gateway: true, agent_capabilities: true, oidc: true, guardrails: true,
    notifications: true, model_governance: true, data_exchange: true,
  } },
};
const fetchMock = vi.fn();
let profileStatus = 200;
let passwordStatus = 200;
let profile = freshProfile;
let observations: string[] = [];
function reply(value: unknown, status = 200) { return new Response(JSON.stringify(value), { status }); }
function Wizard() {
  const [initial] = useState(() => window.location.hash);
  const user = useAuthStore(s => s.user);
  useEffect(() => { observations.push(initial); finishXInsightLoginContinuation(initial); }, [initial]);
  return <p>向导当前账号 {user?.id}：{initial ? '已收到邀请' : '没有邀请'}</p>;
}
function InstallRedirect() { const l = useLocation(); return <Navigate to={`/integrations?tab=xinsight${l.hash}`} replace />; }
function app(path: string, strict = true) {
  window.history.replaceState(null, '', path);
  const gate = (child: React.ReactNode) => <ProtectedRoute>{child}</ProtectedRoute>;
  const routes = <BrowserRouter future={ROUTER_FUTURE_FLAGS}><Routes>
    <Route path="/login" element={<LoginPage />} />
    <Route path={install} element={gate(<InstallRedirect />)} />
    <Route path="/connectors/x-insight/recover" element={gate(<Wizard />)} />
    <Route path="/integrations" element={gate(<Wizard />)} />
    <Route path="/" element={<p>默认入口</p>} />
    <Route path="/access" element={<p>成员入口</p>} />
    <Route path="/developer" element={<p>成员入口</p>} />
  </Routes></BrowserRouter>;
  return render(strict ? <StrictMode>{routes}</StrictMode> : routes);
}
async function submit() {
  const user = userEvent.setup();
  const username = screen.getByPlaceholderText('请输入用户名');
  if (!(username as HTMLInputElement).value) await user.type(username, 'new-admin');
  const password = screen.getByPlaceholderText('请输入密码');
  if (!(password as HTMLInputElement).value) await user.type(password, 'password');
  await user.click(screen.getByRole('button', { name: '登录' }));
}

beforeEach(() => {
  clearXInsightLoginContinuation(); localStorage.clear(); sessionStorage.clear(); observations = [];
  profileStatus = passwordStatus = 200; profile = freshProfile;
  useAuthStore.setState({ token: null, user: null, isAuthenticated: false, profileError: null, profileLoading: false });
  fetchMock.mockReset(); fetchMock.mockImplementation(async (path: string) => {
    if (path === '/api/v1/auth/oidc/config') return reply({ enabled: false });
    if (path === '/api/v1/auth/login') return reply(passwordStatus === 200 ? { token: 'synthetic-bearer', user_id: 92, username: 'new-admin', role: 'org_admin' } : { error: '密码不正确' }, passwordStatus);
    if (path === '/api/v1/me') return reply(profileStatus === 200 ? profile : { error: 'profile unavailable' }, profileStatus);
    throw new Error(`unexpected API ${path}`);
  });
  vi.stubGlobal('fetch', fetchMock);
});
afterEach(() => { clearXInsightLoginContinuation(); vi.unstubAllGlobals(); window.history.replaceState(null, '', '/'); });
describe('fixed password invitation continuation', () => {
  it('keeps a recovery ticket in memory through login and rejects changed recovery destinations', async () => {
    const recoveryPath = '/connectors/x-insight/recover';
    const ticket = `xirt_${'A'.repeat(43)}`;
    const recoveryFragment = '#' + new URLSearchParams({ xinsight_origin: 'https://xi.example', installation_id: `xci_${'A'.repeat(22)}`, recovery_id: `xrr_${'B'.repeat(22)}`, recovery_ticket: ticket });
    expect(isXInsightRecoveryLocation(recoveryPath, '', recoveryFragment)).toBe(true);
    expect(isXInsightRecoveryLocation(recoveryPath, '', recoveryFragment + '&recovery_ticket=extra')).toBe(false);
    expect(isXInsightRecoveryLocation(recoveryPath, '', recoveryFragment.replace('https%3A%2F%2Fxi.example', 'https%3A%2F%2Fevil.example%2Fpath'))).toBe(false);
    app(recoveryPath + recoveryFragment);
    await screen.findByPlaceholderText('请输入用户名');
    expect(window.location.hash).toBe('');
    expect(hasXInsightLoginContinuation()).toBe(true);
    await submit();
    await screen.findByText('向导当前账号 92：已收到邀请');
    expect(window.location.pathname).toBe(recoveryPath);
    expect(observations.every(value => value === recoveryFragment)).toBe(true);
    expect(Object.values(localStorage).join('') + Object.values(sessionStorage).join('')).not.toContain(ticket);
    expect(fetchMock.mock.calls.every(([path]) => !path.includes(ticket))).toBe(true);
  });
  it('preserves a signed-out invitation through real login and fresh /me, without storing it', async () => {
    app(install + fragment); await screen.findByPlaceholderText('请输入用户名');
    expect(window.location.hash).toBe(''); expect(hasXInsightLoginContinuation()).toBe(true);
    expect(fetchMock.mock.calls.some(([p]) => p.includes('setup'))).toBe(false);
    await submit(); await screen.findByText('向导当前账号 92：已收到邀请');
    expect(observations.length).toBeGreaterThan(0); expect(observations.every(v => v === fragment)).toBe(true);
    expect(hasXInsightLoginContinuation()).toBe(false);
    expect(Object.values(localStorage).join('') + Object.values(sessionStorage).join('')).not.toContain(token);
    expect(JSON.stringify(window.history.state)).not.toContain(token);
    expect(window.location.search).not.toContain(token);
    expect(fetchMock.mock.calls.every(([p]) => !p.includes(token))).toBe(true);
    expect(fetchMock.mock.calls.filter(([p]) => p === '/api/v1/auth/login')).toHaveLength(1);
  });
  it('survives expired-bearer startup /me 401 and rechecks a different identity', async () => {
    localStorage.setItem('token', 'expired-synthetic'); useAuthStore.setState({ token: 'expired-synthetic', isAuthenticated: true });
    profileStatus = 401; app(install + fragment); await screen.findByPlaceholderText('请输入用户名');
    expect(localStorage.getItem('token')).toBeNull(); expect(hasXInsightLoginContinuation()).toBe(true);
    profileStatus = 200; await submit(); await screen.findByText('向导当前账号 92：已收到邀请');
    expect(useAuthStore.getState().user?.org_id).toBe(3);
  });
  it('keeps the invitation after a real password 401, then succeeds on retry', async () => {
    passwordStatus = 401; app(install + fragment); await screen.findByPlaceholderText('请输入用户名'); await submit();
    await screen.findByText('密码不正确'); expect(hasXInsightLoginContinuation()).toBe(true);
    expect(window.location.pathname).toBe('/login'); expect(observations).toEqual([]);
    passwordStatus = 200; await submit(); await screen.findByText('向导当前账号 92：已收到邀请');
  });
  it('retries /me after password success plus 503 without logging in again', async () => {
    profileStatus = 503; app(install + fragment); await screen.findByPlaceholderText('请输入用户名'); await submit();
    const retry = await screen.findByRole('button', { name: '重试核对登录状态' });
    expect(observations).toEqual([]); expect(hasXInsightLoginContinuation()).toBe(true);
    profileStatus = 200; fireEvent.click(retry); await screen.findByText('向导当前账号 92：已收到邀请');
    expect(fetchMock.mock.calls.filter(([p]) => p === '/api/v1/auth/login')).toHaveLength(1);
  });
  it('returns to password login when the post-login /me rejects the new bearer', async () => {
    profileStatus = 401; app(install + fragment); await screen.findByPlaceholderText('请输入用户名'); await submit();
    await screen.findByText('登录状态已失效，请重新登录；本次邀请仍保留在当前页面。');
    expect(screen.getByRole('button', { name: '登录' })).toBeEnabled();
    expect(hasXInsightLoginContinuation()).toBe(true); expect(observations).toEqual([]);
    profileStatus = 200; await submit(); await screen.findByText('向导当前账号 92：已收到邀请');
  });
  it('restores before the first wizard mount for a valid stored session after startup retry', async () => {
    localStorage.setItem('token', 'synthetic'); useAuthStore.setState({ token: 'synthetic', isAuthenticated: true });
    profileStatus = 503; app('/integrations?tab=xinsight' + fragment); await screen.findByText('暂时无法验证登录状态');
    expect(observations).toEqual([]); expect(window.location.hash).toBe('');
    profileStatus = 200; fireEvent.click(screen.getByRole('button', { name: '重试' }));
    await screen.findByText('向导当前账号 92：已收到邀请'); expect(observations.every(v => v === fragment)).toBe(true);
    expect(fetchMock.mock.calls.some(([p]) => p === '/api/v1/auth/login')).toBe(false);
  });
  it('leaves an already fresh valid session invitation on the original path', async () => {
    useAuthStore.setState({ user: freshProfile, token: 'synthetic', isAuthenticated: true });
    app(install + fragment); await screen.findByText('向导当前账号 92：已收到邀请');
    expect(fetchMock.mock.calls.some(([p]) => p === '/api/v1/me')).toBe(false);
  });
  it.each(['member', 'unlicensed'])('does not resume for a fresh %s identity', async kind => {
    profile = kind === 'member' ? { ...freshProfile, role: 'member', organization_role: 'member', navigation_capabilities: { has_live_project_membership: true, has_project_admin_membership: false, can_discover_access_review: false, can_discover_workloads: false } } :
      { ...freshProfile, license_access: { ...freshProfile.license_access!, enforceable: false } };
    app(install + fragment); await screen.findByPlaceholderText('请输入用户名'); await submit();
    await screen.findByText(kind === 'member' ? '成员入口' : '默认入口');
    expect(window.location.pathname).toBe(kind === 'member' ? '/developer' : '/'); expect(observations).toEqual([]); expect(hasXInsightLoginContinuation()).toBe(false);
  });
  it('keeps ordinary login on its existing default path', async () => {
    app('/login'); await screen.findByPlaceholderText('请输入用户名'); await submit(); await screen.findByText('默认入口');
    expect(observations).toEqual([]);
  });
  it('keeps domain and ordinary-login 401 hard navigation, and limits exceptions by method/version', async () => {
    window.history.replaceState(null, '', install + fragment); captureXInsightLoginContinuation();
    const location = { pathname: '/wallet', search: '', href: '' }; vi.stubGlobal('window', { location });
    fetchMock.mockImplementation(async () => reply({}, 401)); await expect(request('/wallet')).rejects.toMatchObject({ status: 401 });
    expect(location.href).toBe('/login'); location.pathname = '/login'; location.href = '';
    expect(preservesXInsightLoginOn401('/auth/login', 'GET', 'v1')).toBe(false);
    expect(preservesXInsightLoginOn401('/me', 'GET', 'v2')).toBe(false);
    clearXInsightLoginContinuation(); await expect(request('/auth/login', { method: 'POST' })).rejects.toMatchObject({ status: 401 });
    expect(location.href).toBe('/login');
  });
  it.each([
    ['https://evil.example', '', fragment], ['//evil.example', '', fragment], ['/wallet', '', fragment],
    [install, '?return_url=https://evil.example', fragment], ['/integrations', '?tab=xinsight&return_url=//evil.example', fragment],
    [install, '', fragment + '&invitation=extra'], [install, '', '#invitation=javascript:evil&xinsight_origin=https://xi.example'],
    [install, '', '#' + new URLSearchParams({ invitation: token, xinsight_origin: 'https://user:pass@xi.example' })],
    [install, '', '#' + new URLSearchParams({ invitation: token, xinsight_origin: 'https://xi.example/path' })],
  ])('rejects unrelated or malformed continuation %s %s', (path, search, hash) => {
    expect(isXInsightInvitationLocation(path, search, hash)).toBe(false);
  });
});
