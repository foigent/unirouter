import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import TestPage, { firstChannelModel, parseChannelModels, resolveChannelTestModel, summarizeBatchChannels } from '../TestPage';

const mocks = vi.hoisted(() => ({
  getChannels: vi.fn(),
  getChannelKeys: vi.fn(),
  request: vi.fn(),
}));

vi.mock('../../api/client', () => ({
  api: {
    getChannels: (...args: any[]) => mocks.getChannels(...args),
    getChannelKeys: (...args: any[]) => mocks.getChannelKeys(...args),
    request: (...args: any[]) => mocks.request(...args),
  },
}));

const CHANNELS = [
  { id: 5, name: 'deepseek', provider: 'deepseek', status: 'active', models: 'DeepSeek-V4-Flash' },
  { id: 6, name: 'kimi', provider: 'kimi', status: 'disabled', models: 'moonshot-v1-8k,moonshot-v1-32k' },
  { id: 7, name: 'mimo', provider: 'mimo', status: 'active', models: 'mimo-v2.5-pro' },
];

beforeEach(() => {
  vi.clearAllMocks();
  mocks.getChannels.mockResolvedValue({ data: CHANNELS, total: CHANNELS.length });
  mocks.getChannelKeys.mockResolvedValue({
    data: [
      { channel_id: 5, status: 'active', allowed_models: 'DeepSeek-V4-Flash' },
      { channel_id: 6, status: 'active', allowed_models: 'Kimi K2.7 Code,kimi-k2.6' },
      { channel_id: 7, status: 'active', allowed_models: 'mimo-v2.5-pro' },
    ],
    total: 3,
  });
});

describe('TestPage model selection helpers', () => {
  it('parses configured channel models and trims blanks', () => {
    expect(parseChannelModels('gpt-4o, claude-3-5-sonnet,  qwen-max  ')).toEqual([
      'gpt-4o',
      'claude-3-5-sonnet',
      'qwen-max',
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
});
