import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import WalletPage from '../WalletPage';

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
});
