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
    status = 409;
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
});
