import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import RuntimeChangeControlWorkspace from '../RuntimeChangeControlWorkspace';

const mocks = vi.hoisted(() => ({
  getRuntimeChanges: vi.fn(),
  getChannels: vi.fn(),
  createRuntimeChange: vi.fn(),
  validateRuntimeChange: vi.fn(),
  shadowRuntimeChange: vi.fn(),
  canaryRuntimeChange: vi.fn(),
  promoteRuntimeChange: vi.fn(),
  rollbackRuntimeChange: vi.fn(),
	rejectRuntimeChange: vi.fn(),
	updateChannel: vi.fn(),
}));

vi.mock('../../api/channels', () => ({
  channelApi: {
    list: mocks.getChannels,
    update: mocks.updateChannel,
  },
}));
vi.mock('../../api/routing', () => ({
  routingApi: {
    listReleases: mocks.getRuntimeChanges,
    createRelease: mocks.createRuntimeChange,
    validateRelease: mocks.validateRuntimeChange,
    markReleaseShadow: mocks.shadowRuntimeChange,
    startReleaseCanary: mocks.canaryRuntimeChange,
    promoteRelease: mocks.promoteRuntimeChange,
    rollbackRelease: mocks.rollbackRuntimeChange,
    rejectRelease: mocks.rejectRuntimeChange,
  },
}));

const RELEASE = {
  id: 21,
  release_ref: 'rcr_company_chat_openai_v2',
  model_name: 'company-chat',
  candidate_hash: `sha256:${'a'.repeat(64)}`,
  baseline_hash: `sha256:${'b'.repeat(64)}`,
  candidate: {
    contract_version: 'unirouter.runtime-change.v1',
    model_name: 'company-chat',
    overrides: [{ channel_id: 7, expected_routing_epoch: 9, admission: 'allow', priority: 10, weight: 200 }],
  },
  provider_pack_pins: [{ pack_id: 'unirouter.openai.core', pack_version: '1.0.0', manifest_sha256: `sha256:${'c'.repeat(64)}` }],
  simulation_report: {
    report_version: 'unirouter.runtime-change-simulation.v1',
    overall_risk: 'compatible',
    summary: { unchanged: 2, compatible: 1, review_required: 0, breaking: 0 },
    route_changes: [],
  },
  overall_risk: 'compatible',
  stage: 'draft',
  version: 1,
  canary_basis_points: 0,
  reason: '增加已验证的企业 Chat Provider 容量',
  created_at: '2026-08-01T08:00:00Z',
  updated_at: '2026-08-01T08:00:00Z',
};

describe('RuntimeChangeControlWorkspace', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    window.history.replaceState(null, '', '/model-governance?tab=releases');
    mocks.getRuntimeChanges.mockResolvedValue({ data: [RELEASE], total: 1 });
    mocks.getChannels.mockResolvedValue({
      data: [{
        id: 7,
        name: 'primary-cn',
        provider: 'openai',
        models: 'company-chat',
        status: 'staged',
        priority: 10,
        routing_weight: 200,
        routing_epoch: 9,
      }],
      total: 1,
    });
    mocks.validateRuntimeChange.mockResolvedValue({ ...RELEASE, stage: 'validated', version: 2 });
    mocks.rollbackRuntimeChange.mockResolvedValue({ ...RELEASE, stage: 'rolled_back', version: 5 });
	mocks.updateChannel.mockResolvedValue({ success: true });
  });

  it('keeps release evidence visible and advances a draft with its exact revision', async () => {
    render(<RuntimeChangeControlWorkspace />);

    expect(await screen.findByRole('complementary', { name: '运行时发布检查器' })).toBeTruthy();
    expect((await screen.findAllByText('company-chat')).length).toBeGreaterThanOrEqual(2);
    expect(screen.getByText('高级标识与证据哈希').closest('details')?.open).toBe(false);
    expect(screen.getByText('rcr_company_chat_openai_v2')).toBeTruthy();
    expect(within(screen.getByRole('grid')).queryByText('rcr_company_chat_openai_v2')).toBeNull();
    expect(screen.getByText(`sha256:${'a'.repeat(64)}`)).toBeTruthy();
    expect(screen.getByText('1 个固定身份')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: '验证候选' }));
    await waitFor(() => expect(mocks.validateRuntimeChange).toHaveBeenCalledWith('rcr_company_chat_openai_v2', 1));
  });

  it('derives the target model from an eligible channel and lets the server generate the release ref', async () => {
    mocks.getChannels.mockResolvedValue({
      data: [
        {
          id: 7, name: 'primary-cn', provider: 'deepseek', models: 'deepseek-v4-flash；deepseek-v4-pro',
          status: 'staged', priority: 10, routing_weight: 200, routing_epoch: 9,
        },
        {
          id: 8, name: 'disabled-cn', provider: 'kimi', models: 'kimi-k2',
          status: 'disabled', priority: 20, routing_weight: 100, routing_epoch: 4,
        },
      ],
      total: 2,
    });
    mocks.createRuntimeChange.mockResolvedValue({ ...RELEASE, release_ref: 'rcr_deepseek_v4_flash_auto', model_name: 'deepseek-v4-flash' });

    render(<RuntimeChangeControlWorkspace />);
    await screen.findByText('确定性发布队列');
    fireEvent.click(screen.getByRole('button', { name: /新建发布/ }));

    expect(screen.queryByLabelText('发布标识')).toBeNull();
    expect(screen.getByText(/发布标识由服务器安全生成/)).toBeTruthy();
    expect(screen.getByText(/已排除 1 个禁用或无模型通道/)).toBeTruthy();

    fireEvent.click(screen.getByRole('combobox', { name: '通道快照' }));
    fireEvent.click(await screen.findByText(/primary-cn · deepseek · staged/));
    fireEvent.click(screen.getByRole('combobox', { name: '目标模型' }));
    fireEvent.click(await screen.findByText('deepseek-v4-flash'));
    fireEvent.change(screen.getByRole('textbox', { name: '业务理由' }), { target: { value: '验证新通道' } });
    fireEvent.click(screen.getByRole('button', { name: '生成草稿与模拟证据' }));

    await waitFor(() => expect(mocks.createRuntimeChange).toHaveBeenCalledWith(expect.objectContaining({
      model_name: 'deepseek-v4-flash',
      reason: '验证新通道',
      overrides: [expect.objectContaining({ channel_id: 7, expected_routing_epoch: 9 })],
    })));
    expect(mocks.createRuntimeChange.mock.calls[0][0]).not.toHaveProperty('release_ref');
  });

  it('labels shadow as a zero-traffic waiting state', async () => {
    mocks.getRuntimeChanges.mockResolvedValue({ data: [{ ...RELEASE, stage: 'shadow', version: 3 }], total: 1 });
    render(<RuntimeChangeControlWorkspace />);

    expect((await screen.findAllByText('待灰度（零流量）')).length).toBeGreaterThan(0);
    expect(screen.getByText(/不会发送真实请求/)).toBeTruthy();
    expect(screen.getByRole('button', { name: '启动 5% 灰度' })).toBeTruthy();
  });

  it('separates active release records from assignments that can currently apply', async () => {
    const activeRelease = (id: number, model: string, channelID: number, expectedEpoch: number, risk = 'unchanged') => ({
      ...RELEASE,
      id,
      release_ref: `rcr_${model}`,
      model_name: model,
      stage: 'active',
      version: 3,
      overall_risk: risk,
      candidate: {
        ...RELEASE.candidate,
        model_name: model,
        overrides: [{ channel_id: channelID, expected_routing_epoch: expectedEpoch, admission: 'allow' }],
      },
    });
    mocks.getChannels.mockResolvedValue({
      data: [
        { id: 13, name: 'Deepseek', provider: 'deepseek', models: 'deepseek-v4-flash,deepseek-v4-pro', status: 'staged', effective_status: 'active', priority: 1, routing_weight: 1, routing_epoch: 3 },
        { id: 14, name: 'Kimi', provider: 'kimi', models: 'kimi-k3,kimi-k2.7-code,kimi-k2.6', status: 'disabled', effective_status: 'disabled', priority: 1, routing_weight: 1, routing_epoch: 3 },
      ],
      total: 2,
    });
    mocks.getRuntimeChanges.mockResolvedValue({
      data: [
        activeRelease(1, 'kimi-k3', 14, 2, 'review_required'),
        activeRelease(2, 'deepseek-v4-flash', 13, 3, 'review_required'),
        activeRelease(3, 'deepseek-v4-pro', 13, 3, 'review_required'),
        activeRelease(4, 'kimi-k2.6', 14, 3),
        activeRelease(5, 'kimi-k2.7-code', 14, 3),
      ],
      total: 5,
      page: 1,
      page_size: 20,
      summary: { active_records: 5, current_stable: 2, retained_active: 3, canary: 0, pending: 0, history: 0 },
    });

    render(<RuntimeChangeControlWorkspace />);

    expect(await screen.findByText('当前稳定发布')).toBeTruthy();
    expect(screen.getByText('当前稳定发布').nextElementSibling?.textContent).toBe('2');
    expect(screen.getByText('保留的旧生效记录').nextElementSibling?.textContent).toBe('3');
    expect(screen.getByText('灰度中').nextElementSibling?.textContent).toBe('0');
    expect(screen.getByText('待推进').nextElementSibling?.textContent).toBe('0');
    expect(screen.getByText('历史记录').nextElementSibling?.textContent).toBe('0');
    expect((await screen.findAllByText('启用时有变化（已确认）')).length).toBeGreaterThan(0);
    expect(screen.queryByText('修订 3')).toBeNull();
    expect(screen.queryByText('r3')).toBeNull();
  });

  it('recovers a disabled connection by releasing every retained model record before restaging', async () => {
    window.history.replaceState(null, '', '/channels?from=connections&channel_id=14&model=kimi-k3&intent=recover');
	mocks.getChannels.mockResolvedValue({
	  data: [{ id: 14, name: 'Kimi', provider: 'kimi', models: 'kimi-k3,kimi-k2.6', status: 'disabled', priority: 1, routing_weight: 1, routing_epoch: 7 }],
	  total: 1,
	});
    mocks.getRuntimeChanges.mockResolvedValue({
      data: [
        RELEASE,
		{ ...RELEASE, id: 22, release_ref: 'rcr_kimi_active', model_name: 'kimi-k3', stage: 'active', version: 4, reason: 'Kimi 首次启用', candidate: { ...RELEASE.candidate, model_name: 'kimi-k3', overrides: [{ channel_id: 14, expected_routing_epoch: 6, admission: 'allow' }] } },
		{ ...RELEASE, id: 23, release_ref: 'rcr_kimi_26_active', model_name: 'kimi-k2.6', stage: 'active', version: 3, reason: 'Kimi 2.6 启用', candidate: { ...RELEASE.candidate, model_name: 'kimi-k2.6', overrides: [{ channel_id: 14, expected_routing_epoch: 7, admission: 'allow' }] } },
      ],
	  total: 3,
    });
    const onBack = vi.fn();

    render(<RuntimeChangeControlWorkspace recoveryOnly onBack={onBack} />);

	expect(await screen.findByRole('heading', { name: 'Kimi · 安全恢复' })).toBeTruthy();
	expect(screen.getByText(/释放 2 条旧发布记录/)).toBeTruthy();
	expect(screen.getByText('kimi-k3、kimi-k2.6')).toBeTruthy();
    expect(screen.queryByRole('button', { name: /新建发布/ })).toBeNull();
	fireEvent.click(await screen.findByRole('button', { name: '释放全部旧发布并转为待发布' }));
	fireEvent.click(await screen.findByRole('button', { name: '确认释放并转为待发布' }));
	await waitFor(() => expect(mocks.rollbackRuntimeChange).toHaveBeenNthCalledWith(1, 'rcr_kimi_active', 4));
	expect(mocks.rollbackRuntimeChange).toHaveBeenNthCalledWith(2, 'rcr_kimi_26_active', 3);
	expect(mocks.updateChannel).toHaveBeenCalledWith(14, { status: 'staged', expected_routing_epoch: 7 });
	await waitFor(() => expect(onBack).toHaveBeenCalledTimes(1));
  });

  it('refuses quick recovery when a retained release also carries another connection', async () => {
    window.history.replaceState(null, '', '/channels?from=connections&channel_id=14&model=kimi-k3&intent=recover');
    mocks.getChannels.mockResolvedValue({
      data: [
        { id: 14, name: 'Kimi', provider: 'kimi', models: 'kimi-k3', status: 'disabled', priority: 1, routing_weight: 1, routing_epoch: 7 },
        { id: 15, name: 'Kimi 备用', provider: 'kimi', models: 'kimi-k3', status: 'staged', effective_status: 'active', priority: 2, routing_weight: 1, routing_epoch: 4 },
      ],
      total: 2,
    });
    mocks.getRuntimeChanges.mockResolvedValue({
      data: [{
        ...RELEASE,
        release_ref: 'rcr_kimi_shared',
        model_name: 'kimi-k3',
        stage: 'active',
        version: 4,
        candidate: {
          ...RELEASE.candidate,
          model_name: 'kimi-k3',
          overrides: [
            { channel_id: 14, expected_routing_epoch: 7, admission: 'allow' },
            { channel_id: 15, expected_routing_epoch: 4, admission: 'allow' },
          ],
        },
      }],
      total: 1,
    });

    render(<RuntimeChangeControlWorkspace recoveryOnly />);

    expect(await screen.findByText(/快速恢复已停用/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: '释放全部旧发布并转为待发布' })).toBeNull();
    expect(mocks.rollbackRuntimeChange).not.toHaveBeenCalled();
  });
});
