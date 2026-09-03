import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { Modal } from '@douyinfe/semi-ui';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { CanonicalModelApplication, CanonicalOperation } from '../../../api/modelDeployments';
import ModelCapabilityQualificationPanel, { isQualifiedCapabilityID } from '../ModelCapabilityQualificationPanel';

const mocks = vi.hoisted(() => ({
  getQualification: vi.fn(),
  probeCapability: vi.fn(),
  approveCapability: vi.fn(),
}));

vi.mock('../../../api/modelDeployments', async importOriginal => {
  const actual = await importOriginal<typeof import('../../../api/modelDeployments')>();
  return {
    ...actual,
    modelDeploymentApi: {
      ...actual.modelDeploymentApi,
      getCapabilityQualification: mocks.getQualification,
      probeCapability: mocks.probeCapability,
      approveCapability: mocks.approveCapability,
    },
  };
});

const selected = {
  deployment: { id: 41, config_revision: 7 },
  connection: { id: 21, config_revision: 5 },
  public_model: {
    execution_profile_id: 'deepseek.v4-flash.api',
    execution_profile_version: '1.4.0',
  },
  credential: {
    bindings: [{ id: 91, status: 'active', state_revision: 3, fail_count: 0, eligible: true }],
  },
} as unknown as CanonicalModelApplication;

const operation = {
  operation_id: 'openai.chat_completions.v1',
  revision: 9,
  protocol_profile_id: 'protocol.deepseek-openai-chat.v4',
  protocol_profile_version: '1.0.0',
} as CanonicalOperation;

const selectedB = {
  ...selected,
  deployment: { ...selected.deployment, id: 42, config_revision: 8 },
  connection: { ...selected.connection, id: 22, config_revision: 6 },
  credential: {
    bindings: [{ id: 92, status: 'active', state_revision: 1, fail_count: 0, eligible: true }],
  },
} as unknown as CanonicalModelApplication;

const baseQualification = {
  contract_version: 'unirouter.model-capability-qualification.v1',
  deployment_id: 41,
  operation_id: 'openai.chat_completions.v1',
  capability_id: 'streaming',
  support_state: 'enabled',
  support_revision: 1,
  execution_profile_id: 'deepseek.v4-flash.api',
  execution_profile_version: '1.4.0',
  protocol_profile_id: 'protocol.deepseek-openai-chat.v4',
  protocol_profile_version: '1.0.0',
  probe_contract_id: 'unirouter.capability-probe.deepseek-chat-sse.v1',
  probe_contract_version: '2.0.0',
  probe_available: true,
  probe_availability_reason_code: 'typed_probe_available',
  state: 'not_probed',
  qualified: false,
  qualification_state: 'approval_missing',
};

describe('ModelCapabilityQualificationPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getQualification.mockResolvedValue(baseQualification);
  });

  afterEach(async () => {
    await act(async () => {
      Modal.destroyAll();
      await Promise.resolve();
    });
  });

  it.each([
    ['cache_read_usage', '缓存读取计量', 0],
    ['prompt_cache', 'Prompt Cache 能力', 128],
  ] as const)('renders the published %s workflow without inventing a provider identity', async (capabilityID, shortName, cachedTokens) => {
    let confirmationConfig: any;
    vi.spyOn(Modal, 'confirm').mockImplementation((config: any) => {
      confirmationConfig = config;
      return { destroy: vi.fn(), update: vi.fn() } as any;
    });
    mocks.getQualification.mockResolvedValue({
      ...baseQualification,
      capability_id: capabilityID,
      state: 'probed_awaiting_approval',
      latest_probe: {
        capability_probe_id: 505, success: true, provider_success: true, current: true,
        status_code: 200, latency_ms: 120, tested_at: '2026-09-04T01:00:00Z',
        cache_read_counter_observed: true, cached_tokens: cachedTokens,
      },
    });
    mocks.probeCapability.mockResolvedValue({ success: true, current: true });

    expect(isQualifiedCapabilityID(capabilityID)).toBe(true);
    expect(isQualifiedCapabilityID('unpublished_cache_mode')).toBe(false);
    render(<ModelCapabilityQualificationPanel
      selected={selected} operation={operation} canManage canApprove gatewayLicensed
      gatewayRestriction="" capabilityID={capabilityID}
    />);

    expect(await screen.findByText(`cache read usage: ${cachedTokens} tokens`)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: new RegExp(`运行${shortName}探测`) }));
    expect(confirmationConfig.title).toBe(`运行${shortName}探测？`);
    await act(async () => { await confirmationConfig.onOk(); });
    expect(mocks.probeCapability).toHaveBeenCalledWith(
      41, 'openai.chat_completions.v1', capabilityID, { credential_binding_id: 91 },
    );
    expect(mocks.approveCapability).not.toHaveBeenCalled();
  });

  it.each([false, undefined])('does not turn an unobserved cache counter (%s) into zero', async observed => {
    mocks.getQualification.mockResolvedValue({
      ...baseQualification, capability_id: 'cache_read_usage', state: 'probe_failed',
      qualification_state: 'probe_failed',
      latest_probe: {
        capability_probe_id: 506, success: false, provider_success: true, current: true,
        status_code: 200, latency_ms: 120, tested_at: '2026-09-04T01:00:00Z',
        cache_read_counter_observed: observed, cached_tokens: 0,
      },
    });
    render(<ModelCapabilityQualificationPanel
      selected={selected} operation={operation} canManage canApprove gatewayLicensed
      gatewayRestriction="" capabilityID="cache_read_usage"
    />);
    expect(await screen.findByText('cache read usage: not_reported')).toBeInTheDocument();
    expect(screen.queryByText('cache read usage: 0 tokens')).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '批准在当前环境进入路由池' })).not.toBeInTheDocument();
  });

  it('keeps a server-declared prompt-cache miss unapproved even with an explicit zero counter', async () => {
    mocks.getQualification.mockResolvedValue({
      ...baseQualification, capability_id: 'prompt_cache', state: 'probe_failed',
      qualification_state: 'probe_failed', probe_available: false,
      latest_probe: {
        capability_probe_id: 507, success: false, provider_success: true, current: true,
        status_code: 200, latency_ms: 120, tested_at: '2026-09-04T01:00:00Z',
        cache_read_counter_observed: true, cached_tokens: 0, contract_error_code: 'cache_miss',
      },
    });
    render(<ModelCapabilityQualificationPanel
      selected={selected} operation={operation} canManage canApprove gatewayLicensed
      gatewayRestriction="" capabilityID="prompt_cache"
    />);
    expect(await screen.findByText('cache read usage: 0 tokens')).toBeInTheDocument();
    expect(screen.getByText('合同校验：cache_miss')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /运行Prompt Cache/u })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '批准在当前环境进入路由池' })).not.toBeInTheDocument();
  });

  it('keeps probe and approval separate while presenting the real-call and cost boundary', async () => {
    let confirmationConfig: any;
    vi.spyOn(Modal, 'confirm').mockImplementation((config: any) => {
      confirmationConfig = config;
      return { destroy: vi.fn(), update: vi.fn() } as any;
    });
    mocks.probeCapability.mockResolvedValue({
      capability_probe_id: 501,
      deployment_id: 41,
      operation_id: 'openai.chat_completions.v1',
      capability_id: 'streaming',
      success: true,
      provider_success: true,
      current: true,
      stale: false,
      reason_code: 'ok',
      provider_reason_code: 'ok',
      status_code: 200,
      latency_ms: 120,
      finish_reason: 'stop',
      prompt_tokens: 16,
      completion_tokens: 8,
      total_tokens: 24,
    });
    mocks.approveCapability.mockResolvedValue({
      approval_id: 601,
      capability_probe_id: 501,
      deployment_id: 41,
      operation_id: 'openai.chat_completions.v1',
      capability_id: 'streaming',
      qualified: true,
      qualification_state: 'qualified',
      evidence_ref: 'capability_approval:601',
      approved_at: '2026-08-27T01:00:00Z',
      idempotency_replay: false,
    });

    render(<ModelCapabilityQualificationPanel
      selected={selected}
      operation={operation}
      canManage
      canApprove
      gatewayLicensed
      gatewayRestriction=""
    />);

    const probeButton = await screen.findByRole('button', { name: /运行流式能力探测（预计 1 次调用）/u });
    fireEvent.click(probeButton);
    expect(confirmationConfig.title).toBe('运行流式能力探测？');
    expect(confirmationConfig.okText).toBe('运行流式能力探测（预计 1 次调用）');
    const content = render(confirmationConfig.content);
    expect(content.getByText('预计发起 1 次真实供应商调用，并计入已批准的测试调用上限。')).toBeInTheDocument();
    expect(content.getByText('供应商成本当前不可得；这不表示费用为 ¥0。')).toBeInTheDocument();
    expect(content.getByText('Connection #21@5 · Deployment #41@7')).toBeInTheDocument();
    expect(content.getByText('openai.chat_completions.v1@9 · deepseek.v4-flash.api@1.4.0 · protocol.deepseek-openai-chat.v4@1.0.0')).toBeInTheDocument();
    await act(async () => { await confirmationConfig.onOk(); });
    await waitFor(() => expect(mocks.probeCapability).toHaveBeenCalledWith(
      41, 'openai.chat_completions.v1', 'streaming', { credential_binding_id: 91 },
    ));
    expect(mocks.approveCapability).not.toHaveBeenCalled();
  });

  it('shows the bounded contract failure category without exposing provider response content', async () => {
    mocks.getQualification.mockResolvedValue({
      ...baseQualification,
      state: 'probe_failed',
      qualification_state: 'probe_failed',
      latest_probe: {
        capability_probe_id: 502,
        success: false,
        provider_success: true,
        current: true,
        status_code: 200,
        latency_ms: 984,
        error_summary: 'provider_stream_contract_invalid',
        contract_error_code: 'json_unknown_field',
        diagnostic_member: 'provider_extension',
        tested_at: '2026-08-27T01:00:00Z',
      },
    });

    render(<ModelCapabilityQualificationPanel
      selected={selected}
      operation={operation}
      canManage
      canApprove
      gatewayLicensed
      gatewayRestriction=""
    />);

    expect(await screen.findByText('合同校验：json_unknown_field（成员：provider_extension）')).toBeInTheDocument();
    expect(screen.getByText('provider_stream_contract_invalid')).toBeInTheDocument();
  });

  it('explains probe-contract staleness as revalidation rather than provider regression', async () => {
    mocks.getQualification.mockResolvedValue({
      ...baseQualification,
      capability_id: 'tools',
      probe_contract_id: 'unirouter.capability-probe.deepseek-chat-function-tools.v1',
      probe_contract_version: '2.0.0',
      state: 'probe_stale',
      qualification_state: 'probe_contract_stale',
      latest_probe: {
        capability_probe_id: 500,
        success: true,
        provider_success: true,
        current: false,
        status_code: 200,
        latency_ms: 200,
        tested_at: '2026-08-27T01:00:00Z',
      },
    });

    render(<ModelCapabilityQualificationPanel
      selected={selected}
      operation={operation}
      canManage
      canApprove
      gatewayLicensed
      gatewayRestriction=""
      capabilityID="tools"
    />);

    expect(await screen.findByText(/探测合同已升级/u)).toBeInTheDocument();
    expect(screen.queryByText(/供应商.*不支持/u)).not.toBeInTheDocument();
  });

  it('uses the same two-stage workflow for typed reasoning without exposing reasoning text', async () => {
    let confirmationConfig: any;
    vi.spyOn(Modal, 'confirm').mockImplementation((config: any) => {
      confirmationConfig = config;
      return { destroy: vi.fn(), update: vi.fn() } as any;
    });
    mocks.getQualification.mockResolvedValue({
      ...baseQualification,
      capability_id: 'reasoning',
      probe_contract_id: 'unirouter.capability-probe.deepseek-chat-reasoning.v1',
      probe_contract_version: '1.0.0',
      state: 'probed_awaiting_approval',
      latest_probe: {
        capability_probe_id: 503,
        success: true,
        provider_success: true,
        current: true,
        status_code: 200,
        latency_ms: 640,
        finish_reason: 'stop',
        prompt_tokens: 12,
        completion_tokens: 7,
        total_tokens: 19,
        reasoning_observed: true,
        tested_at: '2026-08-27T01:00:00Z',
      },
    });
    mocks.probeCapability.mockResolvedValue({
      capability_probe_id: 504,
      deployment_id: 41,
      operation_id: 'openai.chat_completions.v1',
      capability_id: 'reasoning',
      success: true,
      provider_success: true,
      current: true,
      stale: false,
      reason_code: 'ok',
      provider_reason_code: 'ok',
      status_code: 200,
      latency_ms: 610,
      reasoning_observed: true,
    });

    render(<ModelCapabilityQualificationPanel
      selected={selected}
      operation={operation}
      canManage
      canApprove
      gatewayLicensed
      gatewayRestriction=""
      capabilityID="reasoning"
    />);

    expect(await screen.findByText('reasoning evidence: observed')).toBeInTheDocument();
    expect(screen.queryByText(/private reasoning/iu)).not.toBeInTheDocument();
    const probeButton = screen.getByRole('button', { name: /运行思考模式能力探测（预计 1 次调用）/u });
    fireEvent.click(probeButton);
    expect(confirmationConfig.title).toBe('运行思考模式能力探测？');
    const content = render(confirmationConfig.content);
    expect(content.getByText(/不会保存私有思维文本/u)).toBeInTheDocument();
    await act(async () => { await confirmationConfig.onOk(); });
    await waitFor(() => expect(mocks.probeCapability).toHaveBeenCalledWith(
      41, 'openai.chat_completions.v1', 'reasoning', { credential_binding_id: 91 },
    ));
  });

  it('ignores a late qualification response after the exact target changes', async () => {
    let resolveA: (value: typeof baseQualification) => void = () => undefined;
    const delayedA = new Promise<typeof baseQualification>(resolve => { resolveA = resolve; });
    mocks.getQualification.mockImplementation((deploymentID: number) => {
      if (deploymentID === 41) return delayedA;
      return Promise.resolve({
        ...baseQualification,
        deployment_id: 42,
        state: 'approved_current',
        qualified: true,
        qualification_state: 'qualified',
        preferred_approval_id: 702,
      });
    });

    const view = render(<ModelCapabilityQualificationPanel
      selected={selected}
      operation={operation}
      canManage
      canApprove
      gatewayLicensed
      gatewayRestriction=""
    />);
    view.rerender(<ModelCapabilityQualificationPanel
      selected={selectedB}
      operation={operation}
      canManage
      canApprove
      gatewayLicensed
      gatewayRestriction=""
    />);

    expect(await screen.findByText(/approval_id=702/u)).toBeInTheDocument();
    await act(async () => { resolveA(baseQualification); });
    expect(screen.getByText(/approval_id=702/u)).toBeInTheDocument();
  });

  it('destroys an open approval confirmation and refuses its stale action after identity change', async () => {
    const destroy = vi.fn();
    let approvalConfig: any;
    vi.spyOn(Modal, 'confirm').mockImplementation((config: any) => {
      approvalConfig = config;
      return { destroy, update: vi.fn() } as any;
    });
    mocks.getQualification.mockResolvedValue({
      ...baseQualification,
      state: 'probed_awaiting_approval',
      qualification_state: 'approval_missing',
      latest_probe: {
        capability_probe_id: 501,
        success: true,
        provider_success: true,
        current: true,
        stale: false,
        reason_code: 'ok',
        provider_reason_code: 'ok',
        status_code: 200,
        latency_ms: 120,
        finish_reason: 'stop',
        prompt_tokens: 16,
        completion_tokens: 8,
        total_tokens: 24,
        tested_at: '2026-08-27T01:00:00Z',
      },
    });

    const view = render(<ModelCapabilityQualificationPanel
      selected={selected}
      operation={operation}
      canManage
      canApprove
      gatewayLicensed
      gatewayRestriction=""
    />);
    fireEvent.click(await screen.findByRole('button', { name: '批准在当前环境进入路由池' }));
    expect(approvalConfig.title).toBe('批准在当前环境进入路由池？');
    const content = render(approvalConfig.content);
    expect(content.getByText('Connection #21@5 · Deployment #41@7')).toBeInTheDocument();
    expect(content.getByText('Probe #501')).toBeInTheDocument();

    view.rerender(<ModelCapabilityQualificationPanel
      selected={selectedB}
      operation={operation}
      canManage
      canApprove
      gatewayLicensed
      gatewayRestriction=""
    />);
    await waitFor(() => expect(destroy).toHaveBeenCalled());
    await act(async () => { await approvalConfig.onOk(); });
    expect(mocks.approveCapability).not.toHaveBeenCalled();
  });
});
