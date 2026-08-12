import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import ChannelKeysPage from '../ChannelKeysPage';

const mockGetChannelKeys = vi.fn();
const mockGetChannels = vi.fn();
const mockUpdateChannelKey = vi.fn();

vi.mock('../../api/client', () => ({
  api: {
    getChannelKeys: (...args: any[]) => mockGetChannelKeys(...args),
    getChannels: (...args: any[]) => mockGetChannels(...args),
    updateChannelKey: (...args: any[]) => mockUpdateChannelKey(...args),
    createChannelKey: vi.fn(),
    deleteChannelKey: vi.fn(),
  },
}));

describe('ChannelKeysPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetChannelKeys.mockResolvedValue({
      data: [{
        id: 7,
        channel_id: 3,
        channel_name: 'mimo',
        channel_status: 'staged',
        key_prefix: '****c20c',
        allowed_models: 'mimo-v2.5-pro，mimo-v2.5',
        status: 'active',
        created_at: '2026-07-01T00:00:00Z',
      }],
      total: 1,
    });
    mockGetChannels.mockResolvedValue({ data: [{ id: 3, name: 'mimo', status: 'staged', models: 'mimo-v2.5-pro,mimo-v2.5' }] });
    mockUpdateChannelKey.mockResolvedValue({ success: true });
  });

  it('updates model scope without forcing an upstream secret rotation', async () => {
    render(<ChannelKeysPage />);

    await screen.findByText('mimo-v2.5-pro，mimo-v2.5');
    fireEvent.click(screen.getByRole('button', { name: '编辑供应商凭证 mimo' }));

    expect(await screen.findByLabelText('供应商 API Key')).toHaveAttribute('type', 'password');
    const allowedModels = await screen.findByLabelText('允许模型（留空=全部）');
    fireEvent.change(allowedModels, { target: { value: 'mimo-v2.5-pro；mimo-v2.5;mimo-v2.5-pro' } });
    fireEvent.click(screen.getByText('更新'));

    await waitFor(() => expect(mockUpdateChannelKey).toHaveBeenCalledWith(7, {
      channel_id: 3,
      allowed_models: 'mimo-v2.5-pro,mimo-v2.5',
      status: 'active',
    }));
  });

  it('shows an explicit retry state when supplier credentials cannot be loaded', async () => {
    mockGetChannelKeys.mockRejectedValueOnce(new Error('unavailable'));

    render(<ChannelKeysPage />);

    expect(await screen.findByRole('heading', { name: '供应商凭证加载失败' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '重新加载供应商凭证' })).toBeTruthy();
  });

  it('explains an enabled credential on a disabled channel with no model overlap', async () => {
    mockGetChannelKeys.mockResolvedValueOnce({
      data: [{
        id: 6,
        channel_id: 6,
        channel_name: 'kimi',
        channel_status: 'disabled',
        key_prefix: '****89ec',
        allowed_models: 'Kimi K2.7 Code,kimi-k2.6',
        status: 'active',
        fail_count: 0,
        cooldown_until: '2026-07-01T00:52:45Z',
        created_at: '2026-06-27T05:05:15Z',
      }],
      total: 1,
    });
    mockGetChannels.mockResolvedValueOnce({
      data: [{
        id: 6,
        name: 'kimi',
        status: 'disabled',
        models: 'moonshot-v1-8k,moonshot-v1-32k,moonshot-v1-128k',
      }],
    });

    render(<ChannelKeysPage />);

    expect(await screen.findByText('凭证启用')).toBeTruthy();
    expect(screen.getByText('通道禁用')).toBeTruthy();
    expect(screen.getByText('模型范围无交集')).toBeTruthy();
    expect(screen.getByText('阻断：模型无交集')).toBeTruthy();
    expect(screen.getByText(/1 条启用凭证绑定待发布\/禁用通道，1 条凭证与通道模型范围无交集/)).toBeTruthy();
    expect(screen.queryByText(/冷却至/)).toBeNull();
  });
});
