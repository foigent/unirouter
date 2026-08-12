import { beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import WalletPage, { parseYuanToCents, WalletTransactionsPanel } from '../WalletPage';
import { useAuthStore } from '../../store/auth';
import { yuanToMicrosString } from '../EnterpriseFinanceWorkspaces';

const mockGetWallet = vi.fn();
const mockTopUp = vi.fn();
const mockAdjustWallet = vi.fn();
const mockGetProjects = vi.fn();
const mockGetOrganizations = vi.fn();
const mockGetWalletTransactions = vi.fn();
const mockExportWalletTransactions = vi.fn();
const mockGetEnterpriseFiscalPeriods = vi.fn();
const mockGetEnterpriseCostCenters = vi.fn();
const mockGetEnterpriseBudgets = vi.fn();
const mockGetEnterpriseBudgetPositions = vi.fn();
const mockApproveEnterpriseBudget = vi.fn();

vi.mock('../../api/client', () => ({
  api: {
    getWallet: (...args: any[]) => mockGetWallet(...args),
    topUp: (...args: any[]) => mockTopUp(...args),
    adjustWallet: (...args: any[]) => mockAdjustWallet(...args),
    getProjects: (...args: any[]) => mockGetProjects(...args),
    getOrganizations: (...args: any[]) => mockGetOrganizations(...args),
    getWalletTransactions: (...args: any[]) => mockGetWalletTransactions(...args),
    exportWalletTransactions: (...args: any[]) => mockExportWalletTransactions(...args),
    getEnterpriseFiscalPeriods: (...args: any[]) => mockGetEnterpriseFiscalPeriods(...args),
    getEnterpriseCostCenters: (...args: any[]) => mockGetEnterpriseCostCenters(...args),
    getEnterpriseBudgets: (...args: any[]) => mockGetEnterpriseBudgets(...args),
    getEnterpriseBudgetPositions: (...args: any[]) => mockGetEnterpriseBudgetPositions(...args),
    approveEnterpriseBudget: (...args: any[]) => mockApproveEnterpriseBudget(...args),
  },
}));

const BILLING_WALLET = {
  exists: true,
  org_id: 22,
  org_name: '财务组织',
  balance_micros: '1000000000',
  granted_balance_micros: '200000000',
  frozen_micros: '50000000',
  threshold_micros: '10000000',
  total_spent_micros: '80000000',
  currency: 'CNY',
  amount_scale: 1_000_000,
  is_admin: true,
  can_top_up: true,
  can_adjust: true,
  can_export_transactions: true,
};

describe('WalletPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    window.history.replaceState(null, '', '/wallet');
    useAuthStore.setState({
      user: { id: 7, username: 'billing', role: 'billing_admin', org_id: 22, license_access: null },
    });
    mockGetWallet.mockResolvedValue(BILLING_WALLET);
    mockGetProjects.mockResolvedValue({ data: [], total: 0 });
    mockGetOrganizations.mockResolvedValue({
      data: [{ id: 1, name: '北区组织' }, { id: 2, name: '南区组织' }],
      total: 2,
    });
    mockGetWalletTransactions.mockResolvedValue({ data: [], total: 0 });
    mockGetEnterpriseFiscalPeriods.mockResolvedValue({ data: [] });
    mockGetEnterpriseCostCenters.mockResolvedValue({ data: [] });
    mockGetEnterpriseBudgets.mockResolvedValue({ data: [] });
    mockGetEnterpriseBudgetPositions.mockResolvedValue({ data: [] });
  });

  it('shows the financial actions authorized by the backend for a billing administrator', async () => {
    render(<WalletPage />);

    await waitFor(() => expect(mockGetWallet).toHaveBeenCalledTimes(1));
    expect(await screen.findByText('兼容结算操作')).toBeTruthy();
    expect(screen.getByRole('button', { name: '现金入账' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '现金调整' })).toBeTruthy();
  });

  it('does not render financial mutation controls without a backend capability', async () => {
    mockGetWallet.mockResolvedValue({ ...BILLING_WALLET, is_admin: false });
    render(<WalletPage />);

    await waitFor(() => expect(mockGetWallet).toHaveBeenCalledTimes(1));
    expect(screen.queryByText('现金操作')).toBeNull();
    expect(screen.queryByRole('button', { name: '现金入账' })).toBeNull();
  });

  it('keeps project budgets in the billing workflow without exposing project membership management', async () => {
    render(<WalletPage />);

    fireEvent.click(await screen.findByText('项目预算'));
    await waitFor(() => expect(mockGetProjects).toHaveBeenCalledTimes(1));
    expect(screen.getByText('暂无项目预算')).toBeTruthy();
    expect(screen.queryByText('成员与角色')).toBeNull();
  });

  it('shows the enterprise budget control layer without relabeling the settlement wallet', async () => {
    render(<WalletPage />);

    fireEvent.click(await screen.findByText('企业预算控制'));
    expect((await screen.findAllByText('财务期间')).length).toBeGreaterThan(0);
    expect(screen.getByText('请求路径影响')).toBeTruthy();
    expect(screen.getByText('尚未建立财务期间')).toBeTruthy();
    expect(mockGetEnterpriseBudgets).toHaveBeenCalledWith(22);
  });

  it('renders a missing wallet as an explicit zero state without wallet mutations and keeps enterprise finance readable', async () => {
    mockGetWallet.mockResolvedValue({
      ...BILLING_WALLET,
      exists: false,
      balance_micros: '0',
      granted_balance_micros: '0',
      frozen_micros: '0',
      threshold_micros: '0',
      total_spent_micros: '0',
      can_top_up: false,
      can_adjust: false,
      can_export_transactions: true,
    });

    render(<WalletPage />);

    expect(await screen.findByText(/当前组织尚未建立运行时结算钱包/)).toBeTruthy();
    expect(screen.queryByText('无法加载预算与成本中心数据')).toBeNull();
    expect(screen.queryByRole('button', { name: '现金入账' })).toBeNull();
    expect(screen.queryByRole('button', { name: '现金调整' })).toBeNull();
    expect(mockTopUp).not.toHaveBeenCalled();
    expect(mockAdjustWallet).not.toHaveBeenCalled();

    fireEvent.click(screen.getByText('结算流水'));
    await waitFor(() => expect(mockGetWalletTransactions).toHaveBeenCalledTimes(1));
    expect(await screen.findByText('导出 CSV')).toBeTruthy();

    fireEvent.click(screen.getByText('企业预算控制'));
    expect((await screen.findAllByText('财务期间')).length).toBeGreaterThan(0);
    expect(mockGetEnterpriseBudgets).toHaveBeenCalledWith(22);
  });

  it('does not duplicate the billing-only budget panel for a platform administrator', async () => {
    useAuthStore.setState({
      user: {
        id: 1, username: 'platform', role: 'platform_admin', org_id: 1, license_access: null,
        customer_context: { state: 'ready', org_id: 22, org_name: '财务组织' },
      },
    });
    render(<WalletPage />);

    await waitFor(() => expect(mockGetWallet).toHaveBeenCalledTimes(1));
    expect(screen.queryByText('项目预算')).toBeNull();
  });

  it('keeps platform enterprise finance readable for an unprovisioned organization without exposing wallet mutations', async () => {
    useAuthStore.setState({
      user: {
        id: 1, username: 'platform', role: 'platform_admin', org_id: 1, license_access: null,
        customer_context: { state: 'ready', org_id: 22, org_name: '财务组织' },
      },
    });
    mockGetWallet.mockResolvedValue({
      ...BILLING_WALLET,
      exists: false,
      balance_micros: '0',
      granted_balance_micros: '0',
      frozen_micros: '0',
      threshold_micros: '0',
      total_spent_micros: '0',
      can_top_up: false,
      can_adjust: false,
      can_export_transactions: true,
      can_select_org: true,
    });

    render(<WalletPage />);

    expect(await screen.findByText(/当前组织尚未建立运行时结算钱包/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: '现金入账' })).toBeNull();
    expect(screen.queryByText('项目预算')).toBeNull();
    fireEvent.click(screen.getByText('企业预算控制'));
    expect((await screen.findAllByText('财务期间')).length).toBeGreaterThan(0);
    expect(mockGetEnterpriseBudgets).toHaveBeenCalledWith(22);
  });

  it('does not subtract frozen funds twice from the spendable wallet buckets', async () => {
    render(<WalletPage />);

    expect(await screen.findByText(/当前可用: ¥1200.00/)).toBeTruthy();
    expect(screen.getByText(/管理资金: ¥1250.00/)).toBeTruthy();
    expect(screen.getByText(/冻结中: ¥50.00/)).toBeTruthy();
  });

  it('binds a platform wallet to the canonical customer without an organization selector', async () => {
    useAuthStore.setState({
      user: {
        id: 1, username: 'platform', role: 'platform_admin', org_id: 99, license_access: null,
        customer_context: { state: 'ready', org_id: 22, org_name: '财务组织' },
      },
    });
    mockGetWallet.mockResolvedValueOnce({ ...BILLING_WALLET, can_select_org: true });

    render(<WalletPage />);

    expect(await screen.findByText('客户范围：财务组织')).toBeTruthy();
    await waitFor(() => expect(mockGetWallet).toHaveBeenCalledWith(22));
    expect(mockGetWallet).not.toHaveBeenCalledWith(99);
    expect(screen.queryByRole('combobox', { name: '查看组织' })).toBeNull();
    expect(mockGetOrganizations).not.toHaveBeenCalled();
  });

  it('fails closed before wallet reads when the canonical customer is ambiguous', async () => {
    useAuthStore.setState({
      user: {
        id: 1, username: 'platform', role: 'platform_admin', org_id: 99, license_access: null,
        customer_context: { state: 'ambiguous' },
      },
    });

    render(<WalletPage />);

    expect(await screen.findByRole('heading', { name: '客户组织上下文不唯一' })).toBeTruthy();
    expect(mockGetWallet).not.toHaveBeenCalled();
    expect(mockGetOrganizations).not.toHaveBeenCalled();
  });

  it('submits an atomic delta with a stable idempotency identity and reason', async () => {
    mockAdjustWallet.mockResolvedValue({ success: true, balance_micros: '900000000' });
    render(<WalletPage />);

    fireEvent.click(await screen.findByRole('button', { name: '现金调整' }));
    fireEvent.change(screen.getByPlaceholderText('请输入金额，例如 100.00'), { target: { value: '-100.00' } });
    fireEvent.change(screen.getByPlaceholderText('请输入业务原因或工单号'), { target: { value: '  TICKET-001  ' } });
    fireEvent.click(screen.getByRole('button', { name: 'confirm' }));

    await waitFor(() => expect(mockAdjustWallet).toHaveBeenCalledTimes(1));
    expect(mockAdjustWallet).toHaveBeenCalledWith(expect.objectContaining({
      delta_cents: -10_000,
      description: 'TICKET-001',
      client_request_id: expect.stringMatching(/^wallet-adjust-/),
    }));
    expect(mockAdjustWallet.mock.calls[0][0]).not.toHaveProperty('target_balance');
  });

  it('parses yuan amounts without floating-point or trailing-character ambiguity', () => {
    expect(parseYuanToCents('100.01')).toBe(10_001);
    expect(parseYuanToCents('-0.05')).toBe(-5);
    expect(parseYuanToCents('1abc')).toBeNull();
    expect(parseYuanToCents('1.001')).toBeNull();
    expect(yuanToMicrosString('9007199254740993.000001')).toBe('9007199254740993000001');
    expect(yuanToMicrosString('1.1234567')).toBeNull();
    expect(yuanToMicrosString('01.00')).toBeNull();
  });

  it('clears prior transaction rows while a new organization scope is loading', async () => {
    let resolveSecond: (value: unknown) => void = () => undefined;
    mockGetWalletTransactions
      .mockResolvedValueOnce({
        data: [{ id: 1, type: 'topup', amount_micros: '1000000', description: 'North ticket', created_at: '2026-07-16T00:00:00Z' }],
        total: 1,
      })
      .mockImplementationOnce(() => new Promise(resolve => { resolveSecond = resolve; }));

    const { rerender } = render(<WalletTransactionsPanel orgId={1} canExport={false} />);
    expect(await screen.findByText('North ticket')).toBeTruthy();

    rerender(<WalletTransactionsPanel orgId={2} canExport={false} />);
    await waitFor(() => expect(mockGetWalletTransactions).toHaveBeenLastCalledWith(1, 20, 2));
    expect(screen.queryByText('North ticket')).toBeNull();

    await act(async () => {
      resolveSecond({ data: [], total: 0 });
    });
  });
});
