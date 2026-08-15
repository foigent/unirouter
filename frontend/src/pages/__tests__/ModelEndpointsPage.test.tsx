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
    contract_version: 'unirouter.model-endpoints.v1',
    generated_at: '2026-08-16T01:00:00Z',
    summary: { total: 1, callable: 0, attention: 1, canary: 0 },
    models: [{
      public_model: 'deepseek-v4-flash',
      display_name: 'DeepSeek V4 Flash',
      lifecycle: 'ga',
      catalog_approved: true,
      state: 'needs_test',
      reason_code: 'provider_probe_required',
      callable: false,
      canary_basis_points: 0,
      release_history_count: 7,
      targets,
      ...modelOverrides,
    }],
  };
}

function renderPage() {
  return render(<MemoryRouter initialEntries={['/channels']}><ModelEndpointsPage /></MemoryRouter>);
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
    fireEvent.click(await screen.findByRole('button', { name: '逐项检测待检测端点（2）' }));
    fireEvent.click(await screen.findByText('确认逐项检测'));

    await waitFor(() => expect(mocks.testChannelModel).toHaveBeenCalledTimes(2));
    expect(mocks.testChannelModel).toHaveBeenNthCalledWith(1, 13, 'deepseek-v4-flash');
    expect(mocks.testChannelModel).toHaveBeenNthCalledWith(2, 14, 'deepseek-v4-flash');
  });

  it('creates separate public and upstream identities with a provider mapping', async () => {
    renderPage();
    await screen.findByText('DeepSeek V4 Flash');
    fireEvent.click(screen.getByRole('button', { name: /新增供应商端点/ }));
    await screen.findByText('公共模型 ID 是应用调用 UniRouter 时使用的名字；上游模型 ID 是这个供应商实际接受的模型或部署名。相同公共模型可以绑定多个供应商端点。');
    fireEvent.change(await screen.findByLabelText('连接名称（可选）'), { target: { value: 'OpenRouter DeepSeek' } });
    fireEvent.change(await screen.findByLabelText('公共模型 ID'), { target: { value: 'company-fast' } });
    fireEvent.change(await screen.findByLabelText('上游模型 / 部署 ID'), { target: { value: 'deepseek/deepseek-v4-flash' } });
    fireEvent.change(await screen.findByLabelText('供应商 API Key'), { target: { value: 'sk-test-only' } });
    fireEvent.click(screen.getByRole('button', { name: '保存端点与凭证' }));

    await waitFor(() => expect(mocks.createChannel).toHaveBeenCalledWith(expect.objectContaining({
      models: 'company-fast',
      model_mapping: { 'company-fast': 'deepseek/deepseek-v4-flash' },
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
});
