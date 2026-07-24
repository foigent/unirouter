import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import ChannelsPage from '../ChannelsPage';

const mocks = vi.hoisted(() => ({
  getChannels: vi.fn(),
  createChannel: vi.fn(),
  updateChannel: vi.fn(),
  deleteChannel: vi.fn(),
  previewRouting: vi.fn(),
}));

vi.mock('../../api/client', () => ({
  api: mocks,
  ApiError: class ApiError extends Error {
    status: number;
    payload: unknown;

    constructor(message: string, status: number, payload: unknown = null) {
      super(message);
      this.status = status;
      this.payload = payload;
    }
  },
}));

describe('ChannelsPage routing-policy.v1 controls', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getChannels.mockResolvedValue({
      data: [{
        id: 7,
        name: 'primary-cn',
        provider: 'openai',
        base_url: 'https://provider.invalid',
        models: 'gpt-4o',
        adapter_type: 'openai_compatible',
        adapter_config: {},
        priority: 10,
        routing_weight: 200,
        routing_epoch: 9,
        degradation_threshold: 4,
        status: 'active',
        circuit_state: 'healthy',
        fail_count: 0,
        created_at: '2026-07-12T00:00:00Z',
      }],
      total: 1,
    });
    mocks.previewRouting.mockResolvedValue({
      policy_version: 'routing-policy.v1',
      snapshot_version: 'rs1_test',
      snapshot_source: 'live',
      degraded: false,
      model: 'gpt-4o',
      candidates: [{
        channel_id: 7,
        channel_name: 'primary-cn',
        provider: 'openai',
        priority: 10,
        weight: 200,
        order: 1,
        circuit_state: 'healthy',
        eligible_key_count: 1,
        reason: 'equal_priority_static_weight',
        requires_probe: false,
      }],
      exclusions: [{
        channel_id: 8,
        channel_name: 'cooling-channel',
        provider: 'azure',
        priority: 10,
        weight: 100,
        circuit_state: 'healthy',
        eligible_key_count: 0,
        reason: 'key_cooldown',
        requires_probe: false,
      }],
    });
    mocks.updateChannel.mockResolvedValue({ message: 'Channel updated' });
  });

  it('explains weight semantics and renders safe candidate/exclusion preview', async () => {
    render(<ChannelsPage />);

    expect(await screen.findByText('primary-cn')).toBeTruthy();
    expect(mocks.getChannels).toHaveBeenCalledWith(1, 20);
    expect(screen.getByText('同级权重')).toBeTruthy();
    expect(screen.getByText(/熔断阈值决定连续失败多少次/)).toBeTruthy();
    expect(screen.getByText(/真实请求仍以服务器请求 ID 进行稳定选择/)).toBeTruthy();

    fireEvent.change(screen.getByPlaceholderText('模型名称，例如 gpt-4o'), { target: { value: 'gpt-4o' } });
    fireEvent.click(screen.getByRole('button', { name: '预览路由' }));

    await waitFor(() => expect(mocks.previewRouting).toHaveBeenCalledWith('gpt-4o'));
    expect(await screen.findByText(/模型 gpt-4o · 策略 routing-policy.v1/)).toBeTruthy();
    expect(screen.getByText(/刚从数据库刷新/)).toBeTruthy();
    expect(screen.getByText('同优先级内按权重排序')).toBeTruthy();
    expect(screen.getByText('凭证冷却中')).toBeTruthy();
    expect(screen.getByRole('button', { name: '编辑通道 primary-cn' })).toBeTruthy();

    fireEvent.change(screen.getByPlaceholderText('模型名称，例如 gpt-4o'), { target: { value: 'claude-3-5-sonnet' } });
    expect(screen.queryByText(/策略 routing-policy.v1/)).toBeNull();
  });

  it('does not present a failed channel request as an empty list', async () => {
    mocks.getChannels.mockRejectedValueOnce(new Error('unavailable'));

    render(<ChannelsPage />);

    expect(await screen.findByRole('heading', { name: '上游通道加载失败' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '重新加载上游通道' })).toBeTruthy();
  });

  it('restores Kimi and disabled status when editing and sends the selected status', async () => {
    mocks.getChannels.mockResolvedValueOnce({
      data: [{
        id: 8,
        name: 'kimi-cn',
        provider: 'kimi',
        base_url: 'https://provider.invalid',
        models: 'kimi-k2',
        adapter_type: 'openai_compatible',
        adapter_config: {},
        priority: 20,
        routing_weight: 100,
        routing_epoch: 4,
        degradation_threshold: 5,
        status: 'disabled',
        circuit_state: 'healthy',
        fail_count: 0,
        created_at: '2026-07-16T00:00:00Z',
      }],
      total: 1,
    });

    render(<ChannelsPage />);
    fireEvent.click(await screen.findByRole('button', { name: '编辑通道 kimi-cn' }));

    expect(await screen.findByRole('combobox', { name: '供应商' })).toHaveTextContent('Kimi');
    expect(screen.getByRole('combobox', { name: '状态' })).toHaveTextContent('禁用');
    fireEvent.click(screen.getByRole('combobox', { name: '状态' }));
    fireEvent.click(await screen.findByText('启用'));
    fireEvent.click(screen.getByRole('button', { name: '更新' }));

    await waitFor(() => expect(mocks.updateChannel).toHaveBeenCalledWith(8, expect.objectContaining({
      provider: 'kimi',
      status: 'active',
      expected_routing_epoch: 4,
    })));
  });

  it('keeps the epoch conflict message and maps other backend failures to safe useful text', async () => {
    const { ApiError } = await import('../../api/client');
    mocks.updateChannel.mockRejectedValueOnce(new ApiError('status must be active or disabled', 400, null));

    render(<ChannelsPage />);
    fireEvent.click(await screen.findByRole('button', { name: '编辑通道 primary-cn' }));
    fireEvent.click(screen.getByRole('button', { name: '更新' }));
    expect(await screen.findByText('通道状态仅支持 active 或 disabled')).toBeTruthy();

    mocks.updateChannel.mockRejectedValueOnce(new ApiError('stale', 409, null));
    fireEvent.click(screen.getByRole('button', { name: '更新' }));
    expect(await screen.findByText('该通道已被其他管理员修改，请关闭编辑页、刷新后重试')).toBeTruthy();
  });
});
