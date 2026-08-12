import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { useAuthStore } from '../../store/auth';
import OrganizationMembersPage from '../OrganizationMembersPage';

const apiMocks = vi.hoisted(() => ({
  getEnterpriseDirectorySources: vi.fn(),
  getEnterpriseDirectoryTree: vi.fn(),
  bootstrapFirstCustomer: vi.fn(),
  getMe: vi.fn(),
}));

vi.mock('../../api/client', () => ({
  api: apiMocks,
}));

vi.mock('../UsersPage', () => ({
  default: ({ embedded = false }: { embedded?: boolean }) => <div>people:{String(embedded)}</div>,
}));
vi.mock('../ProjectManagementPage', () => ({
  default: ({ embedded = false }: { embedded?: boolean }) => <div>projects:{String(embedded)}</div>,
}));
vi.mock('../MemberLifecyclePage', () => ({
  default: ({ embedded = false }: { embedded?: boolean }) => <div>lifecycle:{String(embedded)}</div>,
}));

function LocationProbe() {
  const location = useLocation();
  return <div data-testid="location">{location.pathname}{location.search}</div>;
}

function renderPage(entry = '/organization-members') {
  return render(
    <MemoryRouter initialEntries={[entry]}>
      <LocationProbe />
      <OrganizationMembersPage />
    </MemoryRouter>,
  );
}

describe('OrganizationMembersPage', () => {
  beforeEach(() => {
    apiMocks.getEnterpriseDirectorySources.mockReset();
    apiMocks.getEnterpriseDirectoryTree.mockReset();
    apiMocks.bootstrapFirstCustomer.mockReset();
    apiMocks.getMe.mockReset();
    apiMocks.getEnterpriseDirectorySources.mockResolvedValue({ data: [], total: 0 });
    apiMocks.getEnterpriseDirectoryTree.mockResolvedValue({ data: [], total: 0 });
    apiMocks.bootstrapFirstCustomer.mockResolvedValue({
      organization_id: 9,
      organization_name: 'First Customer',
      wallet_balance_micros: 0,
      idempotent: false,
    });
    apiMocks.getMe.mockResolvedValue({
      id: 1,
      username: 'admin',
      role: 'platform_admin',
      organization_role: 'platform_admin',
      org_id: 1,
      org_name: 'Platform Admin',
      customer_context: { state: 'ready', org_id: 9, org_name: 'First Customer' },
      identity_scope: 'deployment_operator',
    });
    useAuthStore.setState({
      user: { id: 1, username: 'org-admin', role: 'org_admin', org_id: 7, license_access: null },
    });
  });

  it('defaults to current local account management without requiring a directory', () => {
    renderPage();

    expect(screen.getByText('people:true')).toBeTruthy();
    expect(screen.getByText('账号与身份')).toBeTruthy();
    expect(screen.getByText('项目与成员')).toBeTruthy();
    expect(screen.getByText('高级诊断与集成准备')).toBeTruthy();
    expect(screen.queryByText('目录接入准备')).toBeNull();
    expect(screen.queryByText('离线生命周期合同')).toBeNull();
    expect(apiMocks.getEnterpriseDirectorySources).not.toHaveBeenCalled();
    expect(screen.queryByText('首客户初始化')).toBeNull();
  });

  it('shows first-customer initialization only while the platform context is not initialized', async () => {
    useAuthStore.setState({
      user: {
        id: 1, username: 'admin', role: 'platform_admin', org_id: 1, license_access: null,
        customer_context: { state: 'not_initialized' }, identity_scope: 'deployment_operator',
      },
    });
    renderPage();

    expect(screen.queryByRole('tab')).toBeNull();
    expect(screen.getAllByRole('heading', { name: '初始化客户组织' })).toHaveLength(2);
    fireEvent.change(screen.getByLabelText('首个客户组织名称'), { target: { value: 'First Customer' } });
    fireEvent.click(screen.getByText('创建组织与零余额钱包'));

    await waitFor(() => expect(apiMocks.bootstrapFirstCustomer).toHaveBeenCalledTimes(1));
    expect(apiMocks.bootstrapFirstCustomer.mock.calls[0][0]).toBe('First Customer');
    expect(apiMocks.bootstrapFirstCustomer.mock.calls[0][1]).toMatch(/^first-customer-/);
    expect(apiMocks.getMe).toHaveBeenCalledTimes(1);
    expect(await screen.findByText('people:true')).toBeTruthy();
  });

  it('removes the initialization entry after the canonical customer is ready', () => {
    useAuthStore.setState({
      user: {
        id: 1, username: 'admin', role: 'platform_admin', org_id: 1, license_access: null,
        customer_context: { state: 'ready', org_id: 9, org_name: 'First Customer' },
        identity_scope: 'deployment_operator',
      },
    });

    renderPage();

    expect(screen.getByText('people:true')).toBeTruthy();
    expect(screen.queryByText('首客户初始化')).toBeNull();
  });

  it('loads directory preparation only after advanced diagnostics are explicitly opened', async () => {
    renderPage();

    expect(apiMocks.getEnterpriseDirectorySources).not.toHaveBeenCalled();
    fireEvent.click(screen.getByText('展开高级诊断'));
    expect(screen.getByText('企业组织目录')).toBeTruthy();
    expect(screen.getByText(/真实目录接入延后到客户 POC/)).toBeTruthy();
    expect(await screen.findByText('尚未配置企业目录来源')).toBeTruthy();
    expect(apiMocks.getEnterpriseDirectorySources).toHaveBeenCalledWith(7);
    expect(screen.getByTestId('location')).toHaveTextContent('/organization-members');
  });

  it('shows orthogonal source health and the local effective tree without exposing credentials', async () => {
    apiMocks.getEnterpriseDirectorySources.mockResolvedValue({
      data: [{
        observation: {
          source_ref: 'hcm-primary', authority: 'hcm', connection: 'connected',
          source_revision: 'rev-17', observed_at: '2026-08-02T08:00:00Z',
        },
        health: {
          connection: 'connected', freshness: 'fresh',
          reconciliation: 'reconciled', coverage: 'complete',
        },
      }],
      total: 1,
    });
    apiMocks.getEnterpriseDirectoryTree.mockResolvedValue({
      data: [{
        unit_ref: 'dept-finance', kind: 'department', name: '财务部',
        source_revision: 'rev-17',
      }],
      total: 1,
    });
    renderPage('/organization-members?section=directory');

    expect(await screen.findByText('hcm-primary')).toBeTruthy();
    expect(screen.getByText('connection: connected')).toBeTruthy();
    expect(screen.getByText('reconciliation: reconciled')).toBeTruthy();
    expect(await screen.findByText('财务部')).toBeTruthy();
    expect(apiMocks.getEnterpriseDirectoryTree).toHaveBeenCalledWith('hcm-primary', undefined, 7);
    expect(screen.queryByText(/credential|password|token/i)).toBeNull();
  });

  it('keeps stable deep links while hiding diagnostics from the daily tabs', async () => {
    renderPage();

    fireEvent.click(screen.getByText('项目与成员'));
    expect(await screen.findByText('projects:true')).toBeTruthy();
    expect(screen.getByTestId('location')).toHaveTextContent('/organization-members?section=projects');

    fireEvent.click(screen.getByText('展开高级诊断'));
    fireEvent.click(screen.getByText('离线生命周期合同'));
    expect(await screen.findByText('lifecycle:true')).toBeTruthy();
    expect(screen.getByTestId('location')).toHaveTextContent('/organization-members?section=lifecycle');
  });

  it('preserves the project-only workspace for project-scoped roles', async () => {
    useAuthStore.setState({
      user: { id: 2, username: 'project-admin', role: 'project_admin', org_id: 7, license_access: null },
    });

    renderPage();

    expect(await screen.findByText('projects:false')).toBeTruthy();
    expect(screen.queryByText('高级诊断与集成准备')).toBeNull();
    expect(screen.queryByText('账号与身份')).toBeNull();
  });
});
