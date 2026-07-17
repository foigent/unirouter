import { beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import WalletPage, { parseYuanToCents, WalletTransactionsPanel } from '../WalletPage';
import { useAuthStore } from '../../store/auth';

const mockGetWallet = vi.fn();
const mockTopUp = vi.fn();
const mockAdjustWallet = vi.fn();
const mockGetProjects = vi.fn();
const mockGetOrganizations = vi.fn();
const mockGetWalletTransactions = vi.fn();
const mockExportWalletTransactions = vi.fn();

vi.mock('../../api/client', () => ({
  api: {
    getWallet: (...args: any[]) => mockGetWallet(...args),
    topUp: (...args: any[]) => mockTopUp(...args),
    adjustWallet: (...args: any[]) => mockAdjustWallet(...args),
    getProjects: (...args: any[]) => mockGetProjects(...args),
    getOrganizations: (...args: any[]) => mockGetOrganizations(...args),
    getWalletTransactions: (...args: any[]) => mockGetWalletTransactions(...args),
    exportWalletTransactions: (...args: any[]) => mockExportWalletTransactions(...args),
  },
}));

const BILLING_WALLET = {
  org_id: 22,
  balance_micros: '1000000000',
  granted_balance_micros: '200000000',
  frozen_micros: '50000000',
  threshold_micros: '10000000',
  total_spent_micros: '80000000',
  currency: 'CNY',
  amount_scale: 1_000_000,
  is_admin: true,
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
  });

  it('shows the financial actions authorized by the backend for a billing administrator', async () => {
    render(<WalletPage />);

    await waitFor(() => expect(mockGetWallet).toHaveBeenCalledTimes(1));
    expect(await screen.findByText('现金操作')).toBeTruthy();
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

  it('does not duplicate the billing-only budget panel for a platform administrator', async () => {
    useAuthStore.setState({
      user: { id: 1, username: 'platform', role: 'platform_admin', org_id: 1, license_access: null },
    });
    render(<WalletPage />);

    await waitFor(() => expect(mockGetWallet).toHaveBeenCalledTimes(1));
    expect(screen.queryByText('项目预算')).toBeNull();
  });

  it('does not subtract frozen funds twice from the spendable wallet buckets', async () => {
    render(<WalletPage />);

    expect(await screen.findByText(/当前可用: ¥1200.00/)).toBeTruthy();
    expect(screen.getByText(/管理资金: ¥1250.00/)).toBeTruthy();
    expect(screen.getByText(/冻结中: ¥50.00/)).toBeTruthy();
  });

  it('clears the previous organization balance when the next scope fails to load', async () => {
    useAuthStore.setState({
      user: { id: 1, username: 'platform', role: 'platform_admin', org_id: 1, license_access: null },
    });
    mockGetWallet
      .mockResolvedValueOnce({ ...BILLING_WALLET, org_id: 1, org_name: '北区组织', can_select_org: true })
      .mockRejectedValueOnce(new Error('temporarily unavailable'));

    render(<WalletPage />);

    expect(await screen.findByText(/当前可用: ¥1200.00/)).toBeTruthy();
    fireEvent.click(screen.getByRole('combobox', { name: '查看组织' }));
    fireEvent.click(await screen.findByText('南区组织（ID 2）'));

    expect(await screen.findByText('无法加载资金账户信息')).toBeTruthy();
    expect(screen.getAllByText(/temporarily unavailable/).length).toBeGreaterThan(0);
    expect(screen.queryByText(/当前可用: ¥1200.00/)).toBeNull();
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
