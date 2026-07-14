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
        key_prefix: '****c20c',
        allowed_models: 'mimo-v2.5-pro，mimo-v2.5',
        status: 'active',
        created_at: '2026-07-01T00:00:00Z',
      }],
      total: 1,
    });
    mockGetChannels.mockResolvedValue({ data: [{ id: 3, name: 'mimo' }] });
    mockUpdateChannelKey.mockResolvedValue({ success: true });
  });

  it('updates model scope without forcing an upstream secret rotation', async () => {
    render(<ChannelKeysPage />);

    await screen.findByText('mimo-v2.5-pro，mimo-v2.5');
    fireEvent.click(screen.getByRole('button', { name: '编辑上游凭证 mimo' }));

    const allowedModels = await screen.findByLabelText('允许模型（逗号分隔，留空=全部）');
    fireEvent.change(allowedModels, { target: { value: 'mimo-v2.5-pro，mimo-v2.5,mimo-v2.5-pro' } });
    fireEvent.click(screen.getByText('更新'));

    await waitFor(() => expect(mockUpdateChannelKey).toHaveBeenCalledWith(7, {
      channel_id: 3,
      allowed_models: 'mimo-v2.5-pro,mimo-v2.5',
      status: 'active',
    }));
  });
});
