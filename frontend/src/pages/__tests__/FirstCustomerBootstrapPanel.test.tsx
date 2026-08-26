import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import FirstCustomerBootstrapPanel from '../FirstCustomerBootstrapPanel';
import { useAuthStore } from '../../store/auth';

const mocks = vi.hoisted(() => ({ bootstrap: vi.fn(), loadProfile: vi.fn() }));
vi.mock('../../api/client', async importOriginal => ({
  ...await importOriginal<typeof import('../../api/client')>(),
  api: { bootstrapFirstCustomer: (...args: unknown[]) => mocks.bootstrap(...args) },
}));

const originalLoadProfile = useAuthStore.getState().loadProfile;
const preview = {
  contract_version: 'unirouter.first-customer-bootstrap-preview.v1', dry_run: true, ready: true,
  checkpoint: 'runtime_reset', organization_name: 'Test Customer', platform_org_id: 1,
  would_remove: { project_id: 85, zero_wallet_org_id: 1 },
};

async function inspect() {
  render(<FirstCustomerBootstrapPanel />);
  fireEvent.change(screen.getByLabelText('测试或客户组织名称'), { target: { value: 'Test Customer' } });
  fireEvent.click(screen.getByRole('button', { name: '只读预检' }));
  await screen.findByText(/自动空项目 #85/);
}

describe('FirstCustomerBootstrapPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    act(() => useAuthStore.setState({
      user: { id: 1, username: 'admin', organization_role: 'platform_admin', org_id: 1,
        customer_context: { state: 'ready', org_id: 1 }, identity_scope: 'deployment_operator_and_customer' },
      token: 'fixture-session-one',
      loadProfile: mocks.loadProfile,
    }));
    mocks.bootstrap.mockImplementation((_name, _id, options) => Promise.resolve(options?.dryRun
      ? preview : { organization_id: 2, organization_name: 'Test Customer', wallet_balance_micros: 0 }));
    mocks.loadProfile.mockResolvedValue({});
  });
  afterEach(() => act(() => useAuthStore.setState({ user: null, token: null, loadProfile: originalLoadProfile })));

  it('does not automatically inspect or create anything', () => {
    render(<FirstCustomerBootstrapPanel />);
    expect(mocks.bootstrap).not.toHaveBeenCalled();
    expect(screen.getByRole('button', { name: '只读预检' })).toBeDisabled();
    expect(screen.queryByRole('button', { name: '确认初始化客户' })).toBeNull();
  });

  it('requires a valid server preview and explicit confirmation before creation', async () => {
    await inspect();
    expect(mocks.bootstrap).toHaveBeenCalledTimes(1);
    expect(mocks.bootstrap).toHaveBeenCalledWith('Test Customer', expect.any(String), { dryRun: true });
    expect(screen.getByRole('button', { name: '确认初始化客户' })).toBeDisabled();
    await userEvent.click(screen.getByRole('checkbox', { name: '确认上述精确对象及初始化范围' }));
    await userEvent.click(screen.getByRole('button', { name: '确认初始化客户' }));
    await waitFor(() => expect(mocks.bootstrap).toHaveBeenCalledTimes(2));
    expect(mocks.bootstrap.mock.calls[1]).toEqual(['Test Customer', mocks.bootstrap.mock.calls[0][1]]);
    expect(await screen.findByText(/客户初始化已提交/)).toBeTruthy();
    expect(mocks.loadProfile).toHaveBeenCalledTimes(1);
  });

  it('retains the same intent and name after an uncertain response', async () => {
    await inspect();
    mocks.bootstrap.mockRejectedValueOnce(new Error('connection lost'));
    await userEvent.click(screen.getByRole('checkbox'));
    await userEvent.click(screen.getByRole('button', { name: '确认初始化客户' }));
    await screen.findByText('connection lost');
    expect(screen.getByLabelText('测试或客户组织名称')).toBeDisabled();
    await userEvent.click(screen.getByRole('button', { name: '重试同一初始化请求' }));
    await waitFor(() => expect(mocks.bootstrap).toHaveBeenCalledTimes(3));
    expect(mocks.bootstrap.mock.calls[2]).toEqual(mocks.bootstrap.mock.calls[1]);
  });

  it('invalidates a preview when the proposed name changes', async () => {
    await inspect();
    fireEvent.change(screen.getByLabelText('测试或客户组织名称'), { target: { value: 'Different Customer' } });
    expect(screen.queryByRole('button', { name: '确认初始化客户' })).toBeNull();
    expect(mocks.bootstrap).toHaveBeenCalledTimes(1);
  });

  it('fails closed for an older or malformed preview response', async () => {
    mocks.bootstrap.mockResolvedValue({ ready: true });
    render(<FirstCustomerBootstrapPanel />);
    fireEvent.change(screen.getByLabelText('测试或客户组织名称'), { target: { value: 'Test Customer' } });
    fireEvent.click(screen.getByRole('button', { name: '只读预检' }));
    expect(await screen.findByText(/未取得有效的只读预检结果/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: '确认初始化客户' })).toBeNull();
  });

  it('does not offer initialization to customer admins or an already separated operator', () => {
    act(() => useAuthStore.setState({ user: { id: 2, username: 'owner', organization_role: 'org_admin', org_id: 2 } }));
    const { container } = render(<FirstCustomerBootstrapPanel />);
    expect(container).toBeEmptyDOMElement();
    act(() => useAuthStore.setState({ user: { id: 1, username: 'admin', organization_role: 'platform_admin', org_id: 1,
      customer_context: { state: 'ready', org_id: 2 }, identity_scope: 'deployment_operator' } }));
    expect(container).toBeEmptyDOMElement();
    expect(mocks.bootstrap).not.toHaveBeenCalled();
  });

  it('discards the confirmed intent when the current actor changes', async () => {
    await inspect();
    await userEvent.click(screen.getByRole('checkbox'));
    act(() => useAuthStore.setState({ user: { id: 3, username: 'other-admin', organization_role: 'platform_admin', org_id: 9,
      customer_context: { state: 'ready', org_id: 9 }, identity_scope: 'deployment_operator_and_customer' } }));
    expect(screen.queryByRole('button', { name: '确认初始化客户' })).toBeNull();
    expect(screen.getByLabelText('测试或客户组织名称')).toHaveValue('');
    expect(mocks.bootstrap).toHaveBeenCalledTimes(1);
  });

  it('ignores a late preview after the same actor changes sessions', async () => {
    let resolvePreview: (value: unknown) => void = () => undefined;
    mocks.bootstrap.mockReturnValue(new Promise(resolve => { resolvePreview = resolve; }));
    render(<FirstCustomerBootstrapPanel />);
    fireEvent.change(screen.getByLabelText('测试或客户组织名称'), { target: { value: 'Test Customer' } });
    fireEvent.click(screen.getByRole('button', { name: '只读预检' }));
    act(() => useAuthStore.setState({ token: 'fixture-session-two' }));
    await act(async () => { resolvePreview(preview); });
    expect(screen.queryByRole('button', { name: '确认初始化客户' })).toBeNull();
    expect(screen.getByLabelText('测试或客户组织名称')).toHaveValue('');
    expect(mocks.bootstrap).toHaveBeenCalledTimes(1);
  });
});
