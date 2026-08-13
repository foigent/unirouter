import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import TestPage, { channelPublishesModel, firstChannelModel, parseChannelModels, resolveChannelTestModel, summarizeBatchChannels } from '../TestPage';

const mocks = vi.hoisted(() => ({
  getChannels: vi.fn(),
  getChannelKeys: vi.fn(),
  request: vi.fn(),
	createRuntimeChange: vi.fn(),
	validateRuntimeChange: vi.fn(),
	activateInitialRuntimeChange: vi.fn(),
}));

vi.mock('../../api/client', () => ({
  api: {
    getChannels: (...args: any[]) => mocks.getChannels(...args),
    getChannelKeys: (...args: any[]) => mocks.getChannelKeys(...args),
    request: (...args: any[]) => mocks.request(...args),
	createRuntimeChange: (...args: any[]) => mocks.createRuntimeChange(...args),
	validateRuntimeChange: (...args: any[]) => mocks.validateRuntimeChange(...args),
	activateInitialRuntimeChange: (...args: any[]) => mocks.activateInitialRuntimeChange(...args),
  },
}));

const CHANNELS = [
	{ id: 5, name: 'deepseek', provider: 'deepseek', status: 'active', models: 'DeepSeek-V4-Flash', routing_epoch: 3 },
	{ id: 6, name: 'kimi', provider: 'kimi', status: 'disabled', models: 'moonshot-v1-8k,moonshot-v1-32k', routing_epoch: 4 },
	{ id: 7, name: 'mimo', provider: 'mimo', status: 'active', models: 'mimo-v2.5-pro', routing_epoch: 5 },
];

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((next) => { resolve = next; });
  return { promise, resolve };
}

beforeEach(() => {
  vi.clearAllMocks();
	window.history.replaceState(null, '', '/test');
  mocks.getChannels.mockResolvedValue({ data: CHANNELS, total: CHANNELS.length });
  mocks.getChannelKeys.mockResolvedValue({
    data: [
      { channel_id: 5, status: 'active', allowed_models: 'DeepSeek-V4-Flash' },
      { channel_id: 6, status: 'active', allowed_models: 'Kimi K2.7 Code,kimi-k2.6' },
      { channel_id: 7, status: 'active', allowed_models: 'mimo-v2.5-pro' },
    ],
    total: 3,
  });
	mocks.createRuntimeChange.mockResolvedValue({ release_ref: 'rcr_initial_model', version: 1 });
	mocks.validateRuntimeChange.mockResolvedValue({ release_ref: 'rcr_initial_model', version: 2 });
	mocks.activateInitialRuntimeChange.mockResolvedValue({ release_ref: 'rcr_initial_model', version: 3, stage: 'active' });
});

describe('TestPage model selection helpers', () => {
  it('parses all supported separators, trims blanks and removes duplicates', () => {
    expect(parseChannelModels('gpt-4o， claude-3-5-sonnet; qwen-max；gpt-4o\n deepseek-chat ')).toEqual([
      'gpt-4o',
      'claude-3-5-sonnet',
      'qwen-max',
      'deepseek-chat',
    ]);
  });

  it('uses the selected model before falling back to the first configured channel model', () => {
    const channel = { models: 'gpt-4o,claude-3-5-sonnet' };

    expect(firstChannelModel(channel)).toBe('gpt-4o');
    expect(resolveChannelTestModel(channel, 'claude-3-5-sonnet')).toBe('claude-3-5-sonnet');
    expect(resolveChannelTestModel(channel, '')).toBe('gpt-4o');
  });

  it('does not invent a hardcoded fallback model when the channel has no configured models', () => {
    expect(resolveChannelTestModel({ models: '' }, '')).toBe('');
  });

  it('uses model-level publication evidence instead of treating every model on one channel as enabled', () => {
    const channel = {
      id: 8, name: 'kimi', provider: 'kimi', status: 'staged', effective_status: 'active',
      models: 'kimi-k2.6,kimi-k3', published_models: 'kimi-k2.6',
    };
    expect(channelPublishesModel(channel, 'kimi-k2.6')).toBe(true);
    expect(channelPublishesModel(channel, 'kimi-k3')).toBe(false);
  });

  it('counts only active channels in the batch scope', () => {
    const scope = summarizeBatchChannels(CHANNELS);
    expect(scope.included.map(channel => channel.name)).toEqual(['deepseek', 'mimo']);
    expect(scope.excluded.map(channel => channel.name)).toEqual(['kimi']);
  });

  it('shows the batch scope and exact excluded channel before any provider request', async () => {
    render(<TestPage />);

    expect(await screen.findByRole('button', { name: '批量检测 2 个生效通道' })).toBeTruthy();
    expect(screen.getByText(/只检测 2 个状态为“生效”的通道；排除 1 个（kimi（通道禁用））/)).toBeTruthy();
    expect(mocks.request).not.toHaveBeenCalled();
  });

  it('keeps selected-channel and batch loading indicators independent', async () => {
    const request = deferred<any>();
    mocks.request.mockReturnValueOnce(request.promise);
    render(<TestPage />);

    fireEvent.click(await screen.findByRole('combobox', { name: '选择通道' }));
    fireEvent.click(await screen.findByText('deepseek (deepseek) · 已启用'));
    const selectedTestButton = screen.getByRole('button', { name: '检测选中通道' });
    await waitFor(() => expect(selectedTestButton).not.toBeDisabled());
    fireEvent.click(selectedTestButton);

    await waitFor(() => expect(mocks.request).toHaveBeenCalledTimes(1));
    expect(screen.getByRole('button', { name: '检测选中通道' })).toHaveClass('semi-button-loading');
    expect(screen.getByRole('button', { name: '批量检测 2 个生效通道' })).not.toHaveClass('semi-button-loading');

    request.resolve({
      success: true,
      channel_id: 5,
      latency_ms: 12,
      status_code: 200,
      model: 'DeepSeek-V4-Flash',
    });
    await waitFor(() => expect(screen.getByRole('button', { name: '检测选中通道' })).not.toHaveClass('semi-button-loading'));
  });

	it('deep-links to a staged connection and turns a successful probe into the first stable route', async () => {
		const staged = { id: 8, name: 'kimi-new', provider: 'kimi', status: 'staged', models: 'kimi-k2.6,kimi-k3', routing_epoch: 11 };
		window.history.replaceState(null, '', '/test?channel_id=8&model=kimi-k2.6');
		mocks.getChannels.mockResolvedValueOnce({ data: [staged], total: 1 });
		mocks.getChannelKeys.mockResolvedValueOnce({
			data: [{ channel_id: 8, status: 'active', allowed_models: 'kimi-k2.6,kimi-k3' }],
			total: 1,
		});
		mocks.request.mockResolvedValueOnce({
			success: true, channel_id: 8, latency_ms: 18, status_code: 200,
			model: 'kimi-k2.6', tokens_used: 3,
		});

		render(<TestPage />);
		const channelSelector = await screen.findByRole('combobox', { name: '选择通道' });
		await waitFor(() => expect(channelSelector).toHaveTextContent('kimi-new'));
		expect(screen.getByRole('combobox', { name: '检测模型' })).toHaveTextContent('kimi-k2.6');
		const selectedTestButton = screen.getByRole('button', { name: '检测选中通道' });
		await waitFor(() => expect(selectedTestButton).not.toBeDisabled());
		fireEvent.click(selectedTestButton);

		expect(await screen.findByRole('button', { name: '首次启用' })).toBeTruthy();
		fireEvent.click(screen.getByRole('button', { name: '首次启用' }));
		fireEvent.click(await screen.findByRole('button', { name: 'confirm' }));

		await waitFor(() => expect(mocks.createRuntimeChange).toHaveBeenCalledWith({
			model_name: 'kimi-k2.6',
			reason: '首次启用已通过真实上游检测的模型连接',
			overrides: [{ channel_id: 8, expected_routing_epoch: 11, admission: 'allow' }],
		}));
		expect(mocks.validateRuntimeChange).toHaveBeenCalledWith('rcr_initial_model', 1);
		expect(mocks.activateInitialRuntimeChange).toHaveBeenCalledWith('rcr_initial_model', 2);
		expect(await screen.findByText('已启用')).toBeTruthy();
	});

  it('disables batch execution and labels the empty active scope explicitly', async () => {
    mocks.getChannels.mockResolvedValueOnce({
      data: [CHANNELS[1]],
      total: 1,
    });

    render(<TestPage />);

    expect(await screen.findByRole('button', { name: '无生效通道可批量检测' })).toBeDisabled();
    expect(screen.getByText(/只检测 0 个状态为“生效”的通道/)).toBeTruthy();
    expect(mocks.request).not.toHaveBeenCalled();
  });

  it('fails closed instead of presenting a transient channel-list failure as zero channels', async () => {
    mocks.getChannels
      .mockRejectedValueOnce(new Error('temporary unavailable'))
      .mockResolvedValue({ data: CHANNELS, total: CHANNELS.length });

    render(<TestPage />);

    expect(await screen.findByText('读取失败')).toBeTruthy();
    expect(screen.getByText(/当前数量未知，真实检测操作已阻止/)).toBeTruthy();
    expect(screen.getByRole('button', { name: '批量检测（通道未就绪）' })).toBeDisabled();
    expect(screen.queryByText(/批量检测口径：共 0 个通道/)).toBeNull();
    expect(mocks.request).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: '重试通道列表' }));

    expect(await screen.findByRole('button', { name: '批量检测 2 个生效通道' })).toBeTruthy();
    expect(mocks.getChannels).toHaveBeenCalledTimes(2);
    expect(mocks.request).not.toHaveBeenCalled();
  });
});
