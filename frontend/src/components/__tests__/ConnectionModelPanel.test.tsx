import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import ConnectionModelPanel, { exactModelTestResult, type ModelConnectionChannel } from '../ConnectionModelPanel';

const mocks = vi.hoisted(() => ({
  request: vi.fn(),
  createRuntimeChange: vi.fn(),
  validateRuntimeChange: vi.fn(),
  activateInitialRuntimeChange: vi.fn(),
  getRuntimeChangeSlot: vi.fn(),
}));

vi.mock('../../api/client', () => ({ api: mocks }));

const channel: ModelConnectionChannel = {
  id: 12,
  name: 'DeepSeek',
  provider: 'deepseek',
  models: 'deepseek-v4-flash,deepseek-v4-pro',
  status: 'staged',
  routing_epoch: 4,
};

const credentials = [{ channel_id: 12, status: 'active', allowed_models: 'deepseek-v4-flash,deepseek-v4-pro' }];

function renderPanel(modelsPublishedElsewhere = new Set<string>(), selectedChannel = channel) {
  const props = {
    channel: selectedChannel,
    credentials,
    credentialLoadState: 'ready' as const,
    modelsPublishedElsewhere,
    onModelEnabled: vi.fn(),
    onManageCredentials: vi.fn(),
    onOpenRouteManagement: vi.fn(),
    onDiagnose: vi.fn(),
  };
  render(<ConnectionModelPanel {...props} />);
  return props;
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.request.mockImplementation((_path: string, init: { body: string }) => {
    const model = JSON.parse(init.body).model;
    return Promise.resolve({ success: true, channel_id: 12, model, upstream_model: model, status_code: 200, latency_ms: 12, tokens_used: 3 });
  });
  mocks.createRuntimeChange.mockResolvedValue({ release_ref: 'rcr_deepseek', version: 1 });
  mocks.validateRuntimeChange.mockResolvedValue({ release_ref: 'rcr_deepseek', version: 2 });
  mocks.activateInitialRuntimeChange.mockResolvedValue({ release_ref: 'rcr_deepseek', version: 3 });
  mocks.getRuntimeChangeSlot.mockResolvedValue({ model_name: 'deepseek-v4-pro', runtime_mutation: 'none' });
});

describe('ConnectionModelPanel', () => {
  it('detects every model on the selected connection and never switches to another provider', async () => {
    renderPanel();
    fireEvent.click(screen.getByRole('button', { name: '检测当前连接全部模型（2）' }));
    fireEvent.click(await screen.findByText('确认检测全部模型'));

    await waitFor(() => expect(mocks.request).toHaveBeenCalledTimes(2));
    expect(mocks.request.mock.calls.map((call) => ({
      path: call[0],
      model: JSON.parse(call[1].body).model,
    }))).toEqual([
      { path: '/channels/12/test', model: 'deepseek-v4-flash' },
      { path: '/channels/12/test', model: 'deepseek-v4-pro' },
    ]);
    expect(mocks.request.mock.calls.some(call => call[0].includes('kimi'))).toBe(false);
  });

  it('fails closed when the response model differs from the exact requested model', async () => {
    expect(exactModelTestResult('deepseek-v4-pro', {
      success: true,
      model: 'deepseek-v4-flash',
      upstream_model: 'deepseek-v4-flash',
    }).success).toBe(false);

    mocks.request.mockResolvedValueOnce({ success: true, model: 'deepseek-v4-flash', status_code: 200, latency_ms: 8 });
    renderPanel();
    const row = screen.getByText('deepseek-v4-pro').closest('tr')!;
    fireEvent.click(within(row).getByRole('button', { name: '检测' }));

    expect((await screen.findAllByText(/请求 deepseek-v4-pro，返回 deepseek-v4-flash/)).length).toBeGreaterThan(0);
    expect(within(row).queryByRole('button', { name: '启用此模型' })).toBeNull();
  });

  it('binds activation to the successful deepseek-v4-pro result', async () => {
    renderPanel();
    const row = screen.getByText('deepseek-v4-pro').closest('tr')!;
    fireEvent.click(within(row).getByRole('button', { name: '检测' }));
    expect(await within(row).findByRole('button', { name: '启用此模型' })).toBeTruthy();

    fireEvent.click(within(row).getByRole('button', { name: '启用此模型' }));
    expect(await screen.findByText('启用 deepseek-v4-pro？')).toBeTruthy();
    fireEvent.click(screen.getByText('确认启用此模型'));

    await waitFor(() => expect(mocks.createRuntimeChange).toHaveBeenCalledWith({
      model_name: 'deepseek-v4-pro',
      reason: '启用已通过真实上游检测的模型连接',
      overrides: [{ channel_id: 12, expected_routing_epoch: 4, admission: 'allow' }],
    }));
  });

  it('offers route replacement instead of direct activation when another connection already serves the model', async () => {
    const props = renderPanel(new Set(['deepseek-v4-pro']));
    const row = screen.getByText('deepseek-v4-pro').closest('tr')!;
    fireEvent.click(within(row).getByRole('button', { name: '检测' }));
    fireEvent.click(await within(row).findByRole('button', { name: '替换现有连接' }));
    expect(props.onOpenRouteManagement).toHaveBeenCalledWith('deepseek-v4-pro', 'replace');
    expect(mocks.createRuntimeChange).not.toHaveBeenCalled();
  });

  it('preflights the authoritative slot and never shows initial activation for an existing route', async () => {
    mocks.getRuntimeChangeSlot.mockResolvedValueOnce({
      model_name: 'deepseek-v4-pro',
      stable_release_ref: 'rcr_existing',
      runtime_mutation: 'bounded_route_assignment',
    });
    renderPanel();
    const row = screen.getByText('deepseek-v4-pro').closest('tr')!;
    fireEvent.click(within(row).getByRole('button', { name: '检测' }));
    fireEvent.click(await within(row).findByRole('button', { name: '启用此模型' }));

    expect(await within(row).findByRole('button', { name: '替换现有连接' })).toBeTruthy();
    expect(screen.queryByRole('dialog', { name: '启用 deepseek-v4-pro？' })).toBeNull();
    expect(mocks.createRuntimeChange).not.toHaveBeenCalled();
  });

  it('does not call a disabled retained release enabled and provides the recovery path', async () => {
    const props = renderPanel(new Set(), {
      ...channel,
      status: 'disabled',
      effective_status: 'disabled',
      published_models: 'deepseek-v4-pro',
    });

    const row = screen.getByText('deepseek-v4-pro').closest('tr')!;
    expect(within(row).getByText('当前不可调用（发布记录保留）')).toBeTruthy();
    expect(screen.getByText(/保留的发布记录只用于审计，不代表模型仍可调用/)).toBeTruthy();
    fireEvent.click(within(row).getByRole('button', { name: '查看恢复步骤' }));
    expect(props.onOpenRouteManagement).toHaveBeenCalledWith('deepseek-v4-pro', 'recover');
  });
});
