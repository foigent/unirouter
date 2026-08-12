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
}));

vi.mock('../../api/client', () => ({ api: mocks }));

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
});
