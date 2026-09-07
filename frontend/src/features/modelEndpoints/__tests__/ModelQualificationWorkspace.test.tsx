import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../../../api/transport';
import { useAuthStore } from '../../../store/auth';
import type { CanonicalModelApplication, CanonicalOperation } from '../../../api/modelDeployments';
import ModelQualificationWorkspace from '../ModelQualificationWorkspace';
import ModelClientSetup from '../ModelClientSetup';
const mocks = vi.hoisted(() => ({ view: vi.fn(), plan: vi.fn(), probe: vi.fn(), read: vi.fn(), approve: vi.fn() }));
vi.mock('../../../api/modelDeployments', async original => ({ ...(await original<typeof import('../../../api/modelDeployments')>()), modelDeploymentApi: {
  getCapabilityQualification: mocks.view, getCapabilityProbePlan: mocks.plan, probeCapabilityGroup: mocks.probe,
  getCapabilityProbeGroup: mocks.read, approveCapability: mocks.approve,
} }));
const ids = ['function_tools', 'streaming'];
const selected = { deployment: { id: 41, config_revision: 1 }, connection: { id: 21, config_revision: 1 }, credential: { bindings: [{ id: 91, state_revision: 1, eligible: true, status: 'active' }] } } as unknown as CanonicalModelApplication;
const operation = { operation_id: 'openai.chat_completions.v1', revision: 1, capability_probe_contracts: ids.map(capability_id => ({ capability_id })) } as CanonicalOperation;
const plan = { deployment_id: 41, operation_id: operation.operation_id, credential_binding_id: 91, groups: [{ plan_digest: 'a'.repeat(64), request_digest: 'b'.repeat(64), capability_ids: ids, planned_provider_requests: 1, probe_contract: 'fixture' }], separate_capabilities: [] };
const observation = { version: 'unirouter.shared-capability-observation.v1', source_ref: 'op_' + '1'.repeat(32), plan_digest: plan.groups[0].plan_digest, request_digest: 'b'.repeat(64), capability_ids: ids, anchor_capability_id: ids[0], dispatch_state: 'response_received', outcome_ambiguous: false };
const result = { observation, results: ids.map(capability_id => ({ capability_id, success: true })) };
function view(_: number, __: string, id: string) { return Promise.resolve({ capability_id: id, qualified: false, state: 'not_probed' }); }
const props = { selected, operation, canManage: true, canApprove: true, gatewayLicensed: true };
async function click(element: HTMLElement) {
  await act(async () => { fireEvent.click(element); });
}
async function confirm() {
  await screen.findByText(/本轮合并验证预计 1 次/);
  await click(screen.getByLabelText('确认发起本轮 1 次付费验证'));
  await click(screen.getByRole('button', { name: '验证这些能力' }));
}
describe('concentrated qualification', () => {
  beforeEach(() => {
    vi.clearAllMocks(); useAuthStore.setState({ isAuthenticated: true, user: { id: 1, username: 'test', org_id: 2, role: 'platform_admin', organization_role: 'platform_admin' } });
    mocks.view.mockImplementation(view); mocks.plan.mockResolvedValue(plan); mocks.probe.mockResolvedValue(result);
    mocks.read.mockResolvedValue({ observation, facts: [], current_qualifications: [] }); mocks.approve.mockResolvedValue({ qualified: true });
  });
  it('loads without paid calls and dispatches once for multiple facts only after consent', async () => {
    render(<ModelQualificationWorkspace {...props}/>);
    await screen.findByText(/本轮合并验证预计 1 次/);
    expect(mocks.probe).not.toHaveBeenCalled(); expect(screen.getByRole('button', { name: '验证这些能力' })).toBeDisabled();
    await confirm(); await screen.findByText(/已取得逐项验证事实/);
    expect(mocks.probe).toHaveBeenCalledTimes(1); expect(mocks.probe).toHaveBeenCalledWith(41, operation.operation_id, { credential_binding_id: 91, plan_digest: plan.groups[0].plan_digest, capability_ids: ids });
    expect(mocks.approve).not.toHaveBeenCalled();
    await click(screen.getByRole('button', { name: '刷新逐项状态' })); await waitFor(() => expect(mocks.plan).toHaveBeenCalledTimes(2));
    expect(mocks.probe).toHaveBeenCalledTimes(1);
  });
  it('fences double clicks and ignores a late result after exact target change', async () => {
    let finish!: (v: unknown) => void; mocks.probe.mockReturnValue(new Promise(r => { finish = r; }));
    const ui = render(<ModelQualificationWorkspace {...props}/>); await confirm();
    await click(screen.getByRole('button', { name: '验证这些能力' })); expect(mocks.probe).toHaveBeenCalledTimes(1);
    await act(async () => { ui.rerender(<ModelQualificationWorkspace {...props} operation={{ ...operation, revision: 2 }}/>); });
    await act(async () => { finish(result); });
    expect(screen.queryByText(/已取得逐项验证事实/)).not.toBeInTheDocument(); expect(mocks.approve).not.toHaveBeenCalled();
  });
  it('stops on an unconfirmed result and recovers by exact source_ref without another dispatch', async () => {
    mocks.probe.mockRejectedValue(new ApiError('unconfirmed', 503, { error: { source_ref: observation.source_ref } }));
    render(<ModelQualificationWorkspace {...props}/>); await confirm();
    await click(await screen.findByRole('button', { name: '回读本次观察' }));
    await screen.findByText(/已回读完整观察/);
    expect(mocks.read).toHaveBeenCalledWith(41, operation.operation_id, observation.source_ref); expect(mocks.probe).toHaveBeenCalledTimes(1);
  });
  it('does not use another observation or refresh to turn an unknown call into success', async () => {
    mocks.probe.mockRejectedValue(new ApiError('unconfirmed', 503, { error: { source_ref: observation.source_ref } }));
    mocks.read.mockResolvedValue({ observation: { ...observation, source_ref: 'op_' + '2'.repeat(32) } });
    render(<ModelQualificationWorkspace {...props}/>); await confirm();
    await click(await screen.findByRole('button', { name: '回读本次观察' })); await screen.findByText(/观察身份不匹配/);
    await click(screen.getByRole('button', { name: '刷新逐项状态' }));
    await waitFor(() => expect(mocks.plan).toHaveBeenCalledTimes(2));
    expect(screen.getByRole('button', { name: '验证这些能力' })).toBeDisabled(); expect(mocks.probe).toHaveBeenCalledTimes(1);
  });
  it('keeps a lost response with no physical ref unconfirmed and never invokes readback by plan digest', async () => {
    mocks.probe.mockRejectedValue(new TypeError('network failure'));
    render(<ModelQualificationWorkspace {...props}/>); await confirm(); await screen.findByText(/结果未确认，已停止后续调用/);
    expect(screen.queryByRole('button', { name: '回读本次观察' })).not.toBeInTheDocument();
    expect(mocks.read).not.toHaveBeenCalled(); expect(mocks.probe).toHaveBeenCalledTimes(1);
  });
  it('allows read-only refresh after a pre-dispatch rejection, not an automatic retry', async () => {
    mocks.probe.mockRejectedValue(new ApiError('stale plan', 409, { error: 'model_capability_qualification_conflict' }));
    render(<ModelQualificationWorkspace {...props}/>); await confirm(); await screen.findByText(/发送前检查未通过/);
    await click(screen.getByRole('button', { name: '刷新逐项状态' })); await waitFor(() => expect(mocks.plan).toHaveBeenCalledTimes(2));
    expect(mocks.probe).toHaveBeenCalledTimes(1); expect(screen.getByRole('button', { name: '验证这些能力' })).toBeDisabled();
  });
  it('approves only explicit successful current selections through independent commands', async () => {
    mocks.view.mockImplementation((_: number, __: string, id: string) => Promise.resolve({ capability_id: id, qualified: false, latest_probe: { current: true, success: true, capability_probe_id: ids.indexOf(id) + 101 } }));
    render(<ModelQualificationWorkspace {...props}/>); await screen.findAllByText('验证通过 · 待批准');
    expect(mocks.approve).not.toHaveBeenCalled();
    // First checkbox belongs to table's select-all; consent remains separate.
    await click(screen.getAllByRole('checkbox')[0]);
    await click(await screen.findByRole('button', { name: '批准已选 2 项' }));
    await waitFor(() => expect(mocks.approve).toHaveBeenCalledTimes(2));
    expect(mocks.approve.mock.calls.map(c => [c[2], c[3].capability_probe_id])).toEqual([['function_tools', 101], ['streaming', 102]]);
    expect(mocks.probe).not.toHaveBeenCalled();
  });
  it('does not write after actor changes or when license is unavailable', async () => {
    const ui = render(<ModelQualificationWorkspace {...props} gatewayLicensed={false}/>);
    await screen.findByText(/本轮合并验证预计/); expect(screen.getByLabelText('确认发起本轮 1 次付费验证')).toBeDisabled();
    await act(async () => { useAuthStore.setState({ isAuthenticated: false, user: null }); });
    await act(async () => { ui.rerender(<ModelQualificationWorkspace {...props} canManage={false} canApprove={false} gatewayLicensed={false}/>); });
    expect(mocks.probe).not.toHaveBeenCalled(); expect(mocks.approve).not.toHaveBeenCalled();
  });
});
describe('client setup', () => {
  it('uses only the gateway origin, stable name and placeholder; sync remains dry-run', () => {
    render(<ModelClientSetup model="qwen3.7-flash" active={false} chat/>);
    expect(screen.getByText(/尚未启用稳定发布/)).toBeInTheDocument();
    expect(screen.getByText(/<UNIROUTER_APPLICATION_KEY>/)).toBeInTheDocument();
    expect(screen.getByText(/^uniroute-openclaw-sync/).textContent).not.toContain('--write');
    expect(screen.queryByText(/supportsTools/)).not.toBeInTheDocument();
    fireEvent.change(screen.getByLabelText('客户端网关 Base URL'), { target: { value: 'https://user:secret@invalid.example/v1' } });
    expect(screen.queryByText(/^uniroute-openclaw-sync/)).not.toBeInTheDocument();
  });
});
