import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import InvocationOptimizationWorkspace from '../InvocationOptimizationWorkspace';

const getPolicies = vi.fn();
const getContract = vi.fn();
const getReadiness = vi.fn();
const getProjects = vi.fn();
const transitionPolicy = vi.fn();

vi.mock('../../api/client', () => ({
  api: {
    getInvocationOptimizationPolicies: (...args: any[]) => getPolicies(...args),
    getInvocationOptimizationContract: (...args: any[]) => getContract(...args),
    getInvocationOptimizationPolicyReadiness: (...args: any[]) => getReadiness(...args),
    getProjects: (...args: any[]) => getProjects(...args),
    transitionInvocationOptimizationPolicy: (...args: any[]) => transitionPolicy(...args),
    createInvocationOptimizationPolicy: vi.fn(),
    updateInvocationOptimizationPolicy: vi.fn(),
  },
}));

const policy = {
  id: 1,
  contract_version: 'unirouter.invocation-optimization-policy.v1',
  org_id: 7,
  project_id: 11,
  policy_ref: 'opt_company_chat',
  display_name: 'Company Chat 优化',
  model_selector: 'company-chat',
  region: 'cn-north-1',
  residency: 'customer_policy',
  response_cache_target: 'canary',
  response_cache_canary_basis_points: 100,
  response_cache_ttl_seconds: 300,
  response_cache_persistence_allowed: true,
  response_cache_require_request_opt_in: true,
  compression_target: 'canary',
  compression_canary_basis_points: 200,
  compression_require_request_opt_in: true,
  compression_minimum_input_tokens: 128,
  compression_minimum_saved_tokens: 8,
  compression_maximum_reduction_basis_points: 2500,
  minimum_shadow_samples: 100,
  status: 'active',
  stage: 'shadow',
  revision: 3,
  updated_at: '2026-08-04T00:00:00Z',
};

const blockedReadiness = {
  contract_version: 'unirouter.invocation-optimization-readiness.v1',
  purpose: 'canary_admission',
  overall: 'blocked',
  canary_admission_ready: false,
  policy_ref: 'opt_company_chat',
  policy_revision: 3,
  policy_stage: 'shadow',
  policy_status: 'active',
  drivers: [
    {
      driver: 'response_cache', target: 'canary', state: 'blocked', runtime_required: true, runtime_ready: false,
      minimum_shadow_samples: 100, evidence: { samples: 0, eligible: 0, failures: 0 },
      blockers: [{ code: 'response_cache_runtime_unavailable', driver: 'response_cache', description: 'runtime unavailable' }],
    },
    {
      driver: 'compression', target: 'canary', state: 'blocked', runtime_required: false, runtime_ready: true,
      minimum_shadow_samples: 100, evidence: { samples: 12, eligible: 2, failures: 1 },
      blockers: [{ code: 'shadow_failures_present', driver: 'compression', description: 'failures present' }],
    },
  ],
  blockers: [
    { code: 'response_cache_runtime_unavailable', driver: 'response_cache', description: 'runtime unavailable' },
    { code: 'shadow_failures_present', driver: 'compression', description: 'failures present' },
  ],
  runtime_mutation: 'none',
  grants_activation_authority: false,
  grants_traffic_authority: false,
};

describe('InvocationOptimizationWorkspace', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getPolicies.mockResolvedValue({ data: [policy], total: 1 });
    getContract.mockResolvedValue({
      contract_version: 'unirouter.invocation-optimization-policy.v1',
      response_cache_runtime_ready: false,
      maximum_canary_basis_points: 1000,
      request_opt_in_always_required: true,
      raw_content_in_receipts: false,
      x_insight_in_request_path: false,
    });
    getReadiness.mockResolvedValue(blockedReadiness);
    getProjects.mockResolvedValue({ data: [{ id: 11, name: '核心项目' }], total: 1 });
    transitionPolicy.mockResolvedValue({ ...policy, stage: 'rolled_back', revision: 4 });
  });

  it('keeps the inspector visible and fails closed when cache keys are unavailable', async () => {
    render(<InvocationOptimizationWorkspace orgId={7} />);

    expect(await screen.findByText(/Exact Cache 密钥运行时未配置/)).toBeTruthy();
    expect(getPolicies).toHaveBeenCalledWith(7);
    expect(getReadiness).toHaveBeenCalledWith('opt_company_chat', 7);
    expect(getProjects).toHaveBeenCalledWith(1, 100, { status: 'active', org_id: 7 });

    const inspector = screen.getByLabelText('调用优化策略检查器');
    expect(within(inspector).getByText('证据门禁与执行边界')).toBeTruthy();
    expect(within(inspector).getByText(/两个驱动均强制显式 opt-in/)).toBeTruthy();
    expect(within(inspector).getByText(/策略证据不保存 Prompt \/ Response 明文/)).toBeTruthy();
    expect(within(inspector).getByText('Canary 技术准备度')).toBeTruthy();
    expect(within(inspector).getByText(/不授予启用或真实流量权限/)).toBeTruthy();
    expect(await within(inspector).findByText(/Exact Cache 密钥与索引运行时尚未配置/)).toBeTruthy();

    const canary = within(inspector).getByRole('button', { name: '启动 Canary' });
    expect(canary).toBeDisabled();
    expect(screen.queryByRole('button', { name: /全量/ })).toBeNull();
  });

  it('enables the canary action only when the exact readiness revision is technically ready', async () => {
    getReadiness.mockResolvedValueOnce({
      ...blockedReadiness,
      overall: 'ready',
      canary_admission_ready: true,
      drivers: blockedReadiness.drivers.map(driver => ({ ...driver, state: 'ready', runtime_ready: true, blockers: [], evidence: { samples: 100, eligible: 20, failures: 0 } })),
      blockers: [],
    });

    render(<InvocationOptimizationWorkspace orgId={7} />);
    const inspector = await screen.findByLabelText('调用优化策略检查器');
    await waitFor(() => expect(within(inspector).getAllByText('技术门禁满足')).toHaveLength(3));
    expect(within(inspector).getByRole('button', { name: '启动 Canary' })).not.toBeDisabled();
  });

  it('fails closed when the readiness evidence cannot be read', async () => {
    getReadiness.mockRejectedValueOnce(new Error('readiness unavailable'));

    render(<InvocationOptimizationWorkspace orgId={7} />);
    const inspector = await screen.findByLabelText('调用优化策略检查器');
    expect(await within(inspector).findByText(/readiness unavailable.*fail-closed/)).toBeTruthy();
    expect(within(inspector).getByRole('button', { name: '启动 Canary' })).toBeDisabled();
  });

  it('rejects readiness for a stale policy revision', async () => {
    getReadiness.mockResolvedValueOnce({
      ...blockedReadiness,
      overall: 'ready',
      canary_admission_ready: true,
      policy_revision: 2,
      blockers: [],
    });

    render(<InvocationOptimizationWorkspace orgId={7} />);
    const inspector = await screen.findByLabelText('调用优化策略检查器');
    expect(await within(inspector).findByText(/准备度合同或策略修订与当前选择不一致/)).toBeTruthy();
    expect(within(inspector).getByRole('button', { name: '启动 Canary' })).toBeDisabled();
  });

  it('requires and submits an auditable rollback reason', async () => {
    render(<InvocationOptimizationWorkspace orgId={7} />);
    const inspector = await screen.findByLabelText('调用优化策略检查器');
    fireEvent.click(within(inspector).getByRole('button', { name: '立即回滚' }));

    const reason = await screen.findByPlaceholderText('填写可审计理由（必填）');
    fireEvent.change(reason, { target: { value: 'Shadow 证据需重新评估' } });
    const buttons = screen.getAllByRole('button', { name: '立即回滚' });
    fireEvent.click(buttons[buttons.length - 1]);

    await waitFor(() => expect(transitionPolicy).toHaveBeenCalledWith(
      'opt_company_chat', 'rollback', 3, 'Shadow 证据需重新评估',
    ));
  });

  it('requires a separate cache-persistence consent instead of enabling it with the target', async () => {
    render(<InvocationOptimizationWorkspace orgId={7} />);
    await screen.findByLabelText('调用优化策略检查器');

    fireEvent.click(screen.getByRole('button', { name: /新建策略/ }));

    expect(await screen.findByText(/允许在该范围内以独立密钥加密持久化/)).toBeTruthy();
    expect(screen.getByRole('checkbox')).not.toBeChecked();
  });

  it('keeps a no-driver draft editable instead of advancing it into a dead-end lifecycle', async () => {
    getPolicies.mockResolvedValueOnce({
      data: [{
        ...policy,
        policy_ref: 'opt_disabled_drivers',
        response_cache_target: 'disabled',
        response_cache_canary_basis_points: 0,
        compression_target: 'disabled',
        compression_canary_basis_points: 0,
        stage: 'draft',
        revision: 1,
      }],
      total: 1,
    });

    render(<InvocationOptimizationWorkspace orgId={7} />);
    const inspector = await screen.findByLabelText('调用优化策略检查器');

    expect(within(inspector).getByRole('button', { name: /编辑草稿/ })).toBeTruthy();
    expect(within(inspector).queryByRole('button', { name: '验证策略' })).toBeNull();
    expect(within(inspector).getByText(/至少选择一个准备 Shadow \/ Canary 的驱动/)).toBeTruthy();
  });
});
