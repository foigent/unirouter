import { fireEvent, render, screen, waitFor } from '@testing-library/react';
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
    expect((await screen.findAllByText('rcr_company_chat_openai_v2')).length).toBeGreaterThanOrEqual(2);
    expect(screen.getByText(`sha256:${'a'.repeat(64)}`)).toBeTruthy();
    expect(screen.getByText('1 个精确 Pin')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: '验证候选' }));
    await waitFor(() => expect(mocks.validateRuntimeChange).toHaveBeenCalledWith('rcr_company_chat_openai_v2', 1));
  });
});
