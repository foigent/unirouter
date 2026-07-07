import { describe, expect, it, vi, beforeEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import KeysPage from '../KeysPage';

const mockGetKeys = vi.fn();
const mockGetModelNames = vi.fn();
const mockCreateKey = vi.fn();
const mockUpdateKey = vi.fn();
const mockDisableKey = vi.fn();

vi.mock('../../api/client', () => ({
  api: {
    getKeys: (...args: any[]) => mockGetKeys(...args),
    getModelNames: (...args: any[]) => mockGetModelNames(...args),
    createKey: (...args: any[]) => mockCreateKey(...args),
    updateKey: (...args: any[]) => mockUpdateKey(...args),
    disableKey: (...args: any[]) => mockDisableKey(...args),
  },
}));

describe('KeysPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetKeys.mockResolvedValue({
      data: [{
        id: 1,
        name: 'service-key',
        key_prefix: 'sk-test',
        allowed_ips: '',
        allowed_models: 'company-chat',
        status: 'active',
        rpm_limit: 60,
        rps_limit: 10,
        qps_limit: 10,
        created_at: '2026-07-07T10:00:00Z',
      }],
      total: 1,
    });
    mockGetModelNames.mockResolvedValue({ data: ['company-chat'] });
    mockCreateKey.mockResolvedValue({ key: 'sk-created', key_prefix: 'sk-created' });
    mockUpdateKey.mockResolvedValue({ message: 'updated' });
    mockDisableKey.mockResolvedValue({ message: 'disabled' });
  });

  it('renders key quota vocabulary as RPM/RPS/TPM', async () => {
    render(<KeysPage />);

    expect(await screen.findByText('service-key')).toBeTruthy();
    expect(screen.getByText('RPM 60 / RPS 10 / TPM 未配置')).toBeTruthy();
    expect(screen.getByText(/RPM\/RPS\/TPM/)).toBeTruthy();
  });

  it('creates keys with rps_limit rather than visible QPS wording', async () => {
    render(<KeysPage />);

    await screen.findByText('service-key');
    fireEvent.click(screen.getByText('新增访问凭证'));
    fireEvent.change(await screen.findByPlaceholderText('service-prod'), { target: { value: 'new-key' } });
    expect(screen.getByText('RPS 限速')).toBeTruthy();
    expect(screen.queryByText('QPS 限速')).toBeNull();
    fireEvent.click(screen.getByText('创建'));

    await waitFor(() => {
      expect(mockCreateKey).toHaveBeenCalledWith(expect.objectContaining({
        name: 'new-key',
        rps_limit: 10,
      }));
    });
  });
});
