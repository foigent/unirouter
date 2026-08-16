import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { Modal } from '@douyinfe/semi-ui';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import ModelEndpointsPage from '../ModelEndpointsPage';

const mocks = vi.hoisted(() => ({
  getModelEndpoints: vi.fn(),
  testChannelModel: vi.fn(),
  activateModelEndpoint: vi.fn(),
  startModelEndpointCanary: vi.fn(),
  recoverModelEndpoint: vi.fn(),
  updateChannel: vi.fn(),
  createChannel: vi.fn(),
  promoteRuntimeChange: vi.fn(),
  rollbackRuntimeChange: vi.fn(),
}));

vi.mock('../../api/client', () => ({
  api: mocks,
  ApiError: class ApiError extends Error {
    status: number;
    payload: unknown;
    constructor(message: string, status = 500, payload: unknown = null) {
      super(message);
      this.status = status;
      this.payload = payload;
    }
  },
}));

vi.mock('../ChannelKeysPage', () => ({ default: () => <div>凭证内嵌视图</div> }));
vi.mock('../ModelGovernancePage', () => ({ default: () => <div>目录内嵌视图</div> }));
vi.mock('../ModelReadinessPage', () => ({ default: () => <div>诊断内嵌视图</div> }));
vi.mock('../RuntimeChangeControlWorkspace', () => ({ default: () => <div>发布证据内嵌视图</div> }));

function target(overrides: Record<string, unknown>) {
  return {
    target_ref: 'target:13:deepseek-v4-flash',
    channel_id: 13,
    connection_name: 'DeepSeek official',
    provider: 'deepseek',
    base_url: 'https://api.deepseek.com',
    adapter_type: 'openai_compatible',
    public_model: 'deepseek-v4-flash',
    upstream_model: 'deepseek-v4-flash',
    config_status: 'staged',
    routing_epoch: 4,
    priority: 100,
    weight: 100,
    circuit_state: 'healthy',
    eligible_credential_count: 1,
    stable_assignment: false,
    canary_assignment: false,
    route_candidate: false,
    state: 'needs_test',
    reason_code: 'no_current_probe',
    ...overrides,
  };
}

function projection(targets = [target({})], modelOverrides: Record<string, unknown> = {}) {
  return {
    contract_version: 'unirouter.model-endpoints.v2',
    generated_at: '2026-08-16T01:00:00Z',
    summary: { total: 1, route_available: 0, attention: 1, canary: 0 },
    models: [{
      public_model: 'deepseek-v4-flash',
      display_name: 'DeepSeek V4 Flash',
      lifecycle: 'ga',
      catalog_approved: true,
      state: 'needs_test',
      reason_code: 'provider_probe_required',
      canary_basis_points: 0,
      release_history_count: 7,
      targets,
      ...modelOverrides,
    }],
  };
}

function renderPage(entry = '/channels') {
  return render(<MemoryRouter initialEntries={[entry]}><ModelEndpointsPage /></MemoryRouter>);
}

describe('ModelEndpointsPage canonical model and provider target flow', () => {
  afterEach(() => { Modal.destroyAll(); });

  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getModelEndpoints.mockResolvedValue(projection());
    mocks.testChannelModel.mockResolvedValue({
      success: true,
      model: 'deepseek-v4-flash',
      upstream_model: 'deepseek-v4-flash',
      status_code: 200,
      latency_ms: 18,
    });
    mocks.activateModelEndpoint.mockResolvedValue({ stage: 'active' });
    mocks.startModelEndpointCanary.mockResolvedValue({ stage: 'canary' });
    mocks.recoverModelEndpoint.mockResolvedValue({ stage: 'active' });
    mocks.updateChannel.mockResolvedValue({ message: 'Channel updated' });
    mocks.createChannel.mockResolvedValue({ id: 21, credential_configured: true });
  });

  it('groups several provider targets under one public model and keeps immutable history in advanced audit copy', async () => {
    mocks.getModelEndpoints.mockResolvedValue(projection([
      target({}),
      target({
        target_ref: 'target:14:deepseek-v4-flash',
        channel_id: 14,
        connection_name: 'OpenRouter production',
        provider: 'openrouter',
        base_url: 'https://openrouter.ai/api/v1',
        upstream_model: 'deepseek/deepseek-v4-flash',
        priority: 200,
      }),
    ]));

    renderPage();

    expect(await screen.findByText('DeepSeek V4 Flash')).toBeTruthy();
    expect(screen.getByText(/deepseek · DeepSeek official/)).toBeTruthy();
    expect(screen.getByText(/openrouter · OpenRouter production/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '管理端点' }));
    expect(await screen.findByText('DeepSeek official · deepseek')).toBeTruthy();
    expect(screen.getByText('OpenRouter production · openrouter')).toBeTruthy();
    expect(screen.queryByText(/rcr_/)).toBeNull();
    fireEvent.click(screen.getByText('高级审计信息'));
    expect(screen.getByText(/历史记录 7 条。旧发布是不可变审计记录，不是重复的当前模型/)).toBeTruthy();
  });

  it('tests the exact selected DeepSeek target and never substitutes a Kimi target', async () => {
    renderPage();
    fireEvent.click(await screen.findByRole('button', { name: '管理端点' }));
    const endpointSurface = screen.getByText('DeepSeek official · deepseek').closest('section');
    expect(endpointSurface).toBeTruthy();
    fireEvent.click(within(endpointSurface!).getByRole('button', { name: '真实检测' }));
    fireEvent.click(await screen.findByText('发起真实检测'));

    await waitFor(() => expect(mocks.testChannelModel).toHaveBeenCalledWith(13, 'deepseek-v4-flash'));
    expect(mocks.testChannelModel).not.toHaveBeenCalledWith(expect.anything(), 'kimi-k3');
  });

  it('batch tests every pending provider target instead of only the first row', async () => {
    mocks.getModelEndpoints.mockResolvedValue(projection([
      target({}),
      target({
        target_ref: 'target:14:deepseek-v4-flash',
        channel_id: 14,
        connection_name: 'OpenRouter production',
        provider: 'openrouter',
        upstream_model: 'deepseek/deepseek-v4-flash',
      }),
    ]));
    mocks.testChannelModel
      .mockResolvedValueOnce({ success: true, model: 'deepseek-v4-flash', upstream_model: 'deepseek-v4-flash' })
      .mockResolvedValueOnce({ success: true, model: 'deepseek-v4-flash', upstream_model: 'deepseek/deepseek-v4-flash' });

    renderPage();
    fireEvent.click(await screen.findByRole('button', { name: '补齐端点检测证据（2）' }));
    fireEvent.click(await screen.findByText('确认逐项检测'));

    await waitFor(() => expect(mocks.testChannelModel).toHaveBeenCalledTimes(2));
    expect(mocks.testChannelModel).toHaveBeenNthCalledWith(1, 13, 'deepseek-v4-flash');
    expect(mocks.testChannelModel).toHaveBeenNthCalledWith(2, 14, 'deepseek-v4-flash');
  });

  it('separates route availability from missing endpoint evidence', async () => {
    mocks.getModelEndpoints.mockResolvedValue(projection([
      target({ state: 'active', route_candidate: true, stable_assignment: true }),
    ], { state: 'active', route_available: true }));

    renderPage();

    expect(await screen.findByText('路由可用')).toBeTruthy();
    expect(screen.getByText('补齐端点检测证据（1）')).toBeTruthy();
    expect(screen.getByText('需要处理')).toBeTruthy();
  });

  it('clears the selected model URL state before changing top-level tabs', async () => {
    renderPage();
    fireEvent.click(await screen.findByRole('button', { name: '管理端点' }));
    const sheetTitle = await screen.findByText('DeepSeek V4 Flash · 供应商端点');
    const sheet = sheetTitle.closest<HTMLElement>('[role="dialog"]');
    expect(sheet).toBeTruthy();

    fireEvent.click(within(sheet!).getByRole('button', { name: 'close' }));
    await waitFor(() => expect(screen.queryByText('DeepSeek V4 Flash · 供应商端点')).toBeNull());
    fireEvent.click(screen.getByRole('tab', { name: '凭证' }));

    await waitFor(() => expect(screen.queryByText('DeepSeek V4 Flash · 供应商端点')).toBeNull());
    expect(await screen.findByText('凭证内嵌视图')).toBeTruthy();
  });

  it('creates a provider-specific upstream mapping for the selected public model', async () => {
    renderPage();
    fireEvent.click(await screen.findByRole('button', { name: '管理端点' }));
    await screen.findByText('DeepSeek V4 Flash · 供应商端点');
    fireEvent.click(await screen.findByText('为此公共模型增加供应商端点'));
    await screen.findByText('公共模型 ID 是应用调用 UniRouter 时使用的名字；上游模型 ID 是这个供应商实际接受的模型或部署名。相同公共模型可以绑定多个供应商端点。');
    fireEvent.change(await screen.findByLabelText('连接名称（可选）'), { target: { value: 'OpenRouter DeepSeek' } });
    const publicModelField = await screen.findByLabelText('公共模型 ID');
    await waitFor(() => expect(publicModelField).toHaveTextContent('DeepSeek V4 Flash'));
    expect(publicModelField).toHaveAttribute('aria-disabled', 'true');
    fireEvent.change(await screen.findByLabelText('上游模型 / 部署 ID'), { target: { value: 'deepseek/deepseek-v4-flash' } });
    fireEvent.change(await screen.findByLabelText('供应商 API Key'), { target: { value: 'sk-test-only' } });
    fireEvent.click(screen.getByRole('button', { name: '保存端点与凭证' }));

    await waitFor(() => expect(mocks.createChannel).toHaveBeenCalledWith(expect.objectContaining({
      models: 'deepseek-v4-flash',
      model_mapping: { 'deepseek-v4-flash': 'deepseek/deepseek-v4-flash' },
      api_key: 'sk-test-only',
    })));
  });

  it('uses the server workflow for first activation after a current successful probe', async () => {
    const readyTarget = target({
      state: 'ready',
      reason_code: 'probe_passed_not_routed',
      last_probe: { success: true, current: true, status_code: 200, latency_ms: 12, tested_at: '2026-08-16T01:00:00Z' },
    });
    mocks.getModelEndpoints.mockResolvedValue(projection([readyTarget], { state: 'ready', reason_code: 'tested_target_ready_to_enable' }));

    renderPage();
    fireEvent.click(await screen.findByRole('button', { name: '管理端点' }));
    fireEvent.click(await screen.findByRole('button', { name: '首次启用' }));
    fireEvent.click(await screen.findByText('确认首次启用'));

    await waitFor(() => expect(mocks.activateModelEndpoint).toHaveBeenCalledWith({
      channel_id: 13,
      model: 'deepseek-v4-flash',
      expected_routing_epoch: 4,
    }));
  });

  it('replaces a retained stable release when it has no gateway candidate', async () => {
    const readyTarget = target({
      state: 'ready',
      reason_code: 'probe_passed_not_routed',
      last_probe: { success: true, current: true, status_code: 200, latency_ms: 12, tested_at: '2026-08-16T01:00:00Z' },
    });
    mocks.getModelEndpoints.mockResolvedValue(projection([readyTarget], {
      state: 'degraded',
      reason_code: 'stable_route_unavailable',
      route_available: false,
      stable_release_ref: 'rcr_old_stable',
      stable_release_version: 3,
    }));

    renderPage();
    fireEvent.click(await screen.findByRole('button', { name: '管理端点' }));
    fireEvent.click(await screen.findByRole('button', { name: '替换失效路由' }));
    expect(await screen.findByText(/旧发布保留为“已回滚”历史/)).toBeTruthy();
    const dialogs = screen.getAllByRole('dialog');
    fireEvent.click(within(dialogs[dialogs.length - 1]).getByRole('button', { name: 'confirm' }));

    await waitFor(() => expect(mocks.recoverModelEndpoint).toHaveBeenCalledWith({
      channel_id: 13,
      model: 'deepseek-v4-flash',
      expected_routing_epoch: 4,
    }));
  });
});
