import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import WalletPage, { parseYuanToCents } from '../WalletPage';

const mockGetWallet = vi.fn();
const mockTopUp = vi.fn();
const mockAdjustWallet = vi.fn();

vi.mock('../../api/client', () => ({
  api: {
    getWallet: (...args: any[]) => mockGetWallet(...args),
    topUp: (...args: any[]) => mockTopUp(...args),
    adjustWallet: (...args: any[]) => mockAdjustWallet(...args),
  },
}));

const BILLING_WALLET = {
  org_id: 22,
  balance_cents: 100_000,
  granted_cents: 20_000,
  frozen_cents: 5_000,
  threshold_cents: 1_000,
  total_spent_cents: 8_000,
  is_admin: true,
};

describe('WalletPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetWallet.mockResolvedValue(BILLING_WALLET);
  });

  it('shows the financial actions authorized by the backend for a billing administrator', async () => {
    render(<WalletPage />);

    await waitFor(() => expect(mockGetWallet).toHaveBeenCalledTimes(1));
    expect(await screen.findByText('管理员操作')).toBeTruthy();
    expect(screen.getByRole('button', { name: '入账' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '调整额度' })).toBeTruthy();
  });

  it('does not render financial mutation controls without a backend capability', async () => {
    mockGetWallet.mockResolvedValue({ ...BILLING_WALLET, is_admin: false });
    render(<WalletPage />);

    await waitFor(() => expect(mockGetWallet).toHaveBeenCalledTimes(1));
    expect(screen.queryByText('管理员操作')).toBeNull();
    expect(screen.queryByRole('button', { name: '入账' })).toBeNull();
  });

  it('does not subtract frozen funds twice from the spendable wallet buckets', async () => {
    render(<WalletPage />);

    expect(await screen.findByText(/当前可用: ¥1200.00/)).toBeTruthy();
    expect(screen.getByText(/管理资金: ¥1250.00/)).toBeTruthy();
    expect(screen.getByText(/冻结中: ¥50.00/)).toBeTruthy();
  });

  it('submits an atomic delta with a stable idempotency identity and reason', async () => {
    mockAdjustWallet.mockResolvedValue({ success: true, balance_cents: 90_000 });
    render(<WalletPage />);

    fireEvent.click(await screen.findByRole('button', { name: '调整额度' }));
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
});
