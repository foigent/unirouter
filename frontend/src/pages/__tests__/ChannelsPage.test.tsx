import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import ChannelsPage from '../ChannelsPage';

const mocks = vi.hoisted(() => ({
  getChannels: vi.fn(),
  createChannel: vi.fn(),
  updateChannel: vi.fn(),
  deleteChannel: vi.fn(),
  getProviderPacks: vi.fn(),
  previewRouting: vi.fn(),
  getChannelKeys: vi.fn(),
  request: vi.fn(),
  createRuntimeChange: vi.fn(),
  validateRuntimeChange: vi.fn(),
  activateInitialRuntimeChange: vi.fn(),
  getRuntimeChangeSlot: vi.fn(),
  getRuntimeChanges: vi.fn(),
  rollbackRuntimeChange: vi.fn(),
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
    window.history.replaceState(null, '', '/channels');
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
    mocks.getProviderPacks.mockResolvedValue({
      data: [{
        manifest: {
          pack_id: 'unirouter.openai.core',
          pack_version: '1.0.0',
          provider: 'openai',
          adapter_type: 'openai_compatible',
          conformance_level: 'fixture_verified',
          network_verified: false,
          endpoints: ['/v1/chat/completions', '/v1/embeddings'],
          modalities: ['text', 'embeddings'],
          auth_profiles: [{ type: 'bearer', wire_header: 'Authorization' }],
        },
        manifest_sha256: `sha256:${'a'.repeat(64)}`,
        schema_sha256: `sha256:${'b'.repeat(64)}`,
      }],
      total: 1,
      claim_boundary: {
        conformance_level: 'fixture_verified',
        network_verified: false,
        live_calls: false,
        credentials_used: false,
      },
    });
    mocks.updateChannel.mockResolvedValue({ message: 'Channel updated' });
	mocks.createChannel.mockResolvedValue({ id: 99, credential_configured: true });
    mocks.getChannelKeys.mockResolvedValue({
      data: [{ channel_id: 7, status: 'active', allowed_models: 'gpt-4o' }],
      total: 1,
    });
    mocks.request.mockResolvedValue({ success: true, channel_id: 7, model: 'gpt-4o', upstream_model: 'gpt-4o', status_code: 200, latency_ms: 9 });
    mocks.createRuntimeChange.mockResolvedValue({ release_ref: 'rcr_test', version: 1 });
    mocks.validateRuntimeChange.mockResolvedValue({ release_ref: 'rcr_test', version: 2 });
    mocks.activateInitialRuntimeChange.mockResolvedValue({ release_ref: 'rcr_test', version: 3 });
    mocks.getRuntimeChangeSlot.mockResolvedValue({ model_name: 'gpt-4o', runtime_mutation: 'none' });
    mocks.getRuntimeChanges.mockResolvedValue({ data: [], total: 0 });
    mocks.rollbackRuntimeChange.mockResolvedValue({});
  });

  it('explains weight semantics and renders safe candidate/exclusion preview', async () => {
    render(<ChannelsPage />);

    expect((await screen.findAllByText('primary-cn')).length).toBeGreaterThan(0);
    expect(mocks.getChannels).toHaveBeenCalledWith(1, 20);
    expect(screen.getByText('同级权重')).toBeTruthy();
	expect(screen.getByText(/填写供应商地址、模型和 Key/)).toBeTruthy();
	expect(screen.getByText('三步完成模型上线')).toBeTruthy();
	expect(screen.getByText(/检测通过后启用该模型/)).toBeTruthy();
	 expect(screen.getByRole('button', { name: '检测当前连接全部模型（1）' })).toBeTruthy();
    expect(screen.getByText('高级：预览当前路由')).toBeTruthy();

    fireEvent.click(screen.getByText('高级：预览当前路由'));

	const previewModelPicker = screen.getByRole('group', { name: '选择精确模型 ID' });
	fireEvent.click(within(previewModelPicker).getByRole('combobox'));
	fireEvent.click(await screen.findByText('gpt-4o · primary-cn'));
    fireEvent.click(screen.getByRole('button', { name: '预览路由' }));

    await waitFor(() => expect(mocks.previewRouting).toHaveBeenCalledWith('gpt-4o'));
    expect(await screen.findByText(/模型 gpt-4o · 策略 routing-policy.v1/)).toBeTruthy();
    expect(screen.getByText(/刚从数据库刷新/)).toBeTruthy();
    expect(screen.getByText('同优先级内按权重排序')).toBeTruthy();
    expect(screen.getByText('凭证冷却中')).toBeTruthy();
    expect(screen.getByRole('button', { name: '编辑通道 primary-cn' })).toBeTruthy();

	expect(screen.queryByText('claude-3-5-sonnet')).toBeNull();
  });

  it('auto-selects the first channel and keeps fixture-only compatibility in the advanced evidence section', async () => {
    render(<ChannelsPage />);

    fireEvent.click(screen.getByText('高级：查看协议与连接证据'));
    expect(await screen.findByRole('complementary', { name: 'Provider 兼容性检查器' })).toBeTruthy();
    expect((await screen.findAllByText('协议夹具（同名）')).length).toBeGreaterThan(0);
    expect(screen.getByText('not verified')).toBeTruthy();
    expect(screen.getByText('unirouter.openai.core@1.0.0')).toBeTruthy();
    expect(screen.getByText(/不代表真实账号、网络、额度、模型权限或账单/)).toBeTruthy();
	expect(screen.getByText(/真实检测就在本区域下方/)).toBeTruthy();
	expect(screen.queryByRole('button', { name: '查看并检测模型' })).toBeNull();
    expect(new URLSearchParams(window.location.search).get('channel_id')).toBe('7');
  });

  it('does not present a failed channel request as an empty list', async () => {
    mocks.getChannels.mockRejectedValueOnce(new Error('unavailable'));

    render(<ChannelsPage />);

    expect(await screen.findByRole('heading', { name: '上游通道加载失败' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '重新加载上游通道' })).toBeTruthy();
  });

  it('permits a disabled channel without retained releases to move to staged', async () => {
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
    expect((await screen.findAllByText('协议夹具（参考）')).length).toBeGreaterThan(0);
    fireEvent.click(await screen.findByRole('button', { name: '编辑通道 kimi-cn' }));

    expect(await screen.findByRole('combobox', { name: '供应商身份' })).toHaveTextContent('Kimi');
    expect(screen.getByRole('combobox', { name: '状态' })).toHaveTextContent('禁用');
    fireEvent.change(screen.getByLabelText('支持模型'), { target: { value: 'kimi-k2.7-code； kimi-k2.6;kimi-k2.7-code' } });
    fireEvent.click(screen.getByRole('combobox', { name: '状态' }));
    expect(screen.queryByText('启用')).toBeNull();
    fireEvent.click(await screen.findByText('待发布（staged）'));
    fireEvent.click(screen.getByRole('button', { name: '更新' }));

    await waitFor(() => expect(mocks.updateChannel).toHaveBeenCalledWith(8, expect.objectContaining({
      provider: 'kimi',
      models: 'kimi-k2.7-code,kimi-k2.6',
      status: 'staged',
      expected_routing_epoch: 4,
    })));
  });

  it('offers DeepSeek, Kimi and custom as supplier identities without treating protocol fixtures as live verification', async () => {
    render(<ChannelsPage />);
	await screen.findByRole('heading', { name: '模型连接', level: 3 });
	fireEvent.click(screen.getByRole('button', { name: /新增模型连接/ }));
    fireEvent.click(await screen.findByRole('combobox', { name: '供应商身份' }));

    expect(await screen.findByText('DeepSeek（OpenAI-compatible）')).toBeTruthy();
    expect(screen.getByText('Kimi / Moonshot（OpenAI-compatible）')).toBeTruthy();
    expect(screen.getByText('自定义 OpenAI-compatible')).toBeTruthy();
    fireEvent.click(screen.getByText('DeepSeek（OpenAI-compatible）'));
    expect(screen.getByRole('combobox', { name: '请求协议（Adapter Type）' })).toHaveTextContent('OpenAI Compatible');
    expect(screen.getByText(/不会因为选择 OpenAI-compatible 协议而把 DeepSeek、Kimi 或自定义服务改名为 OpenAI/)).toBeTruthy();
    expect(screen.getByText(/离线协议夹具不等于真实供应商连通验证/)).toBeTruthy();
  });

	it('saves a new connection and keeps detection on the same page', async () => {
		mocks.getChannels.mockResolvedValueOnce({
			data: [{
				id: 7, name: 'primary-cn', provider: 'openai', base_url: 'https://provider.invalid', models: 'gpt-4o',
				adapter_type: 'openai_compatible', adapter_config: {}, priority: 10, routing_weight: 200, routing_epoch: 9,
				degradation_threshold: 4, status: 'active', circuit_state: 'healthy', fail_count: 0, created_at: '2026-07-12T00:00:00Z',
			}], total: 1,
		}).mockResolvedValueOnce({
			data: [{
				id: 99, name: 'kimi-new', provider: 'kimi', base_url: 'https://provider.invalid/v1', models: 'kimi-k2.6,kimi-k3',
				adapter_type: 'openai_compatible', adapter_config: {}, priority: 100, routing_weight: 100, routing_epoch: 1,
				degradation_threshold: 10, status: 'staged', circuit_state: 'healthy', fail_count: 0, created_at: '2026-08-13T00:00:00Z',
			}], total: 1,
		});
		render(<ChannelsPage />);
		fireEvent.click(await screen.findByRole('button', { name: /新增模型连接/ }));
		fireEvent.change(screen.getByLabelText('通道名称'), { target: { value: 'kimi-new' } });
		fireEvent.click(screen.getByRole('combobox', { name: '供应商身份' }));
		fireEvent.click(await screen.findByText('Kimi / Moonshot（OpenAI-compatible）'));
		fireEvent.change(screen.getByLabelText('服务地址（Base URL）'), { target: { value: 'https://provider.invalid/v1' } });
		fireEvent.change(screen.getByLabelText('支持模型'), { target: { value: 'kimi-k2.6；kimi-k3' } });
		fireEvent.change(screen.getByLabelText('供应商 API Key'), { target: { value: 'provider-secret' } });
		fireEvent.click(screen.getByRole('button', { name: '保存并进入检测' }));

		await waitFor(() => expect(mocks.createChannel).toHaveBeenCalledWith(expect.objectContaining({
			name: 'kimi-new', provider: 'kimi', models: 'kimi-k2.6,kimi-k3', api_key: 'provider-secret',
		})));
		expect(window.location.pathname).toBe('/channels');
		expect(new URLSearchParams(window.location.search).get('channel_id')).toBe('99');
		expect(await screen.findByText('kimi-new · 模型上线')).toBeTruthy();
		expect(screen.getByRole('button', { name: '检测当前连接全部模型（2）' })).toBeTruthy();
	});

  it('keeps the epoch conflict message and maps other backend failures to safe useful text', async () => {
    const { ApiError } = await import('../../api/client');
    mocks.getChannels.mockResolvedValue({
      data: [{
        id: 7, name: 'primary-cn', provider: 'openai', base_url: 'https://provider.invalid', models: 'gpt-4o',
        adapter_type: 'openai_compatible', adapter_config: {}, priority: 10, routing_weight: 200, routing_epoch: 9,
        degradation_threshold: 4, status: 'disabled', circuit_state: 'healthy', fail_count: 0, created_at: '2026-07-12T00:00:00Z',
      }],
      total: 1,
    });
    mocks.updateChannel.mockRejectedValueOnce(new ApiError('status must be active or disabled', 400, null));

    render(<ChannelsPage />);
    fireEvent.click(await screen.findByRole('button', { name: '编辑通道 primary-cn' }));
    fireEvent.click(screen.getByRole('button', { name: '更新' }));
    expect(await screen.findByText('通道状态仅支持 active、staged 或 disabled')).toBeTruthy();

    mocks.updateChannel.mockRejectedValueOnce(new ApiError('Channel routing epoch changed since it was loaded', 409, {
      code: 'stale_routing_epoch',
      expected_routing_epoch: 9,
      current_routing_epoch: 10,
    }));
    fireEvent.click(screen.getByRole('button', { name: '更新' }));
    expect(await screen.findByText('该连接确实已在你打开页面后发生变化，请刷新后重试')).toBeTruthy();

    mocks.updateChannel.mockRejectedValueOnce(new ApiError('Channel is referenced by an active runtime release', 409, {
      code: 'channel_bound_to_active_release',
      published_models: ['kimi-k3'],
    }));
    fireEvent.click(screen.getByRole('button', { name: '更新' }));
    expect(await screen.findByText(/仍关联已生效的历史发布记录（kimi-k3）/)).toBeTruthy();
    expect(screen.getByText(/刷新页面不会改变运行状态/)).toBeTruthy();
    expect(screen.queryByText(/占位/)).toBeNull();
  });

  it('emergency-disables a runtime-published staged connection with a status-only update', async () => {
    mocks.getChannels.mockResolvedValueOnce({
      data: [{
        id: 14, name: 'Kimi', provider: 'kimi', base_url: 'https://api.moonshot.cn/v1', models: 'kimi-k3',
        adapter_type: 'openai_compatible', adapter_config: {}, priority: 100, routing_weight: 100, routing_epoch: 6,
        degradation_threshold: 10, status: 'staged', effective_status: 'active', published_models: 'kimi-k3',
        circuit_state: 'healthy', fail_count: 0, created_at: '2026-08-14T00:00:00Z',
      }],
      total: 1,
    });

    render(<ChannelsPage />);
    fireEvent.click(await screen.findByRole('button', { name: '编辑通道 Kimi' }));

    expect(await screen.findByText(/当前由运行时发布生效/)).toBeTruthy();
    expect(screen.getByLabelText('服务地址（Base URL）')).toBeDisabled();
    expect(screen.getByRole('combobox', { name: '状态' })).toHaveTextContent('紧急禁用');
    fireEvent.click(screen.getByRole('button', { name: '确认紧急禁用' }));

    await waitFor(() => expect(mocks.updateChannel).toHaveBeenCalledWith(14, {
      status: 'disabled',
      expected_routing_epoch: 6,
    }));
  });

  it('opens the exact recovery release for a disabled connection with retained evidence', async () => {
    mocks.getChannels.mockResolvedValue({
      data: [{
        id: 14, name: 'Kimi', provider: 'kimi', base_url: 'https://api.moonshot.cn/v1', models: 'kimi-k3',
        adapter_type: 'openai_compatible', adapter_config: {}, priority: 100, routing_weight: 100, routing_epoch: 7,
        degradation_threshold: 10, status: 'disabled', effective_status: 'disabled', published_models: 'kimi-k3',
        circuit_state: 'healthy', fail_count: 0, created_at: '2026-08-14T00:00:00Z',
      }],
      total: 1,
    });
    mocks.getRuntimeChanges.mockResolvedValue({
      data: [{
        id: 31,
        release_ref: 'rcr_kimi_active',
        model_name: 'kimi-k3',
        candidate_hash: `sha256:${'a'.repeat(64)}`,
        baseline_hash: `sha256:${'b'.repeat(64)}`,
        candidate: { contract_version: 'unirouter.runtime-change-candidate.v1', model_name: 'kimi-k3', overrides: [{ channel_id: 14, expected_routing_epoch: 6, admission: 'allow' }] },
        provider_pack_pins: [],
        simulation_report: { report_version: 'v1', overall_risk: 'compatible' },
        overall_risk: 'compatible',
        stage: 'active',
        version: 3,
        canary_basis_points: 0,
        reason: 'Kimi 首次启用',
        created_at: '2026-08-13T00:00:00Z',
        updated_at: '2026-08-13T00:00:00Z',
      }],
      total: 1,
    });

    render(<ChannelsPage />);
    expect(await screen.findByRole('button', { name: '恢复连接 Kimi' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: '编辑通道 Kimi' })).toBeNull();
    expect(screen.queryByRole('button', { name: '删除通道 Kimi' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: '恢复连接 Kimi' }));

    const params = new URLSearchParams(window.location.search);
    expect(window.location.pathname).toBe('/channels');
    expect(params.get('channel_id')).toBe('14');
    expect(params.get('model')).toBe('kimi-k3');
    expect(params.get('intent')).toBe('recover');
	expect(await screen.findByRole('heading', { name: 'Kimi · 安全恢复' })).toBeTruthy();
	expect(screen.getByText(/不会自动恢复流量/)).toBeTruthy();
	expect(screen.getByRole('button', { name: '释放全部旧发布并转为待发布' })).toBeTruthy();
  });
});
