import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import XInsightSetup from '../XInsightSetup';
import { xInsightSetupApi, type SetupPreview, type SetupStatus } from '../../api/xInsightSetup';
import { ApiError } from '../../api/transport';
import { xInsightCommunicationApi } from '../../api/xInsightCommunication';

vi.mock('../../api/xInsightCommunication', () => ({ xInsightCommunicationApi: { get: vi.fn().mockResolvedValue({automatic_enabled:false,revision:1,installation_id:'xci_AAAAAAAAAAAAAAAAAAAAAA'}), once: vi.fn().mockResolvedValue({state:'empty'}) } }));
vi.mock('../../api/xInsightSetup', () => ({ xInsightSetupApi: { peer: vi.fn(), preview: vi.fn(), create: vi.fn(), status: vi.fn(), members: vi.fn() } }));
const invitation = { schema_version: 'xinsight.connector-installation-invitation.v1' as const, invitation_id: 'xii_AAAAAAAAAAAAAAAAAAAAAA', installation_id: 'xci_AAAAAAAAAAAAAAAAAAAAAA', deployment_id: 'xid_AAAAAAAAAAAAAAAAAAAAAA', audience: 'unirouter' as const, xinsight_origin: 'https://xi.example', requested_event_scope: ['model.request.completed'] as ['model.request.completed'], expires_at: '2026-10-03T13:00:00Z', install_url: 'https://ur.example/connectors/x-insight/install#invitation=opaque' };
const token = 'xiiv1_' + 'A'.repeat(43);
const invitationFragment = '#' + new URLSearchParams({invitation:token,xinsight_origin:invitation.xinsight_origin}).toString();
const preview: SetupPreview = {
  schema_version: 'unirouter.xinsight.setup-preview.v1', preview_sha256: 'sha256:preview', installation_id: invitation.installation_id, invitation,
  xi_consent: { schema_version: 'xinsight.connector-setup-consent-view.v1', installation_id: invitation.installation_id, deployment_id: invitation.deployment_id, xi_origin: invitation.xinsight_origin, ur_origin: 'https://ur.example', consent_revision: 1, consent_sha256: 'sha256:xi', expires_at: invitation.expires_at, custody: { mode: 'full_content', retention_seconds: 2592000, project_quota_bytes: 5368709120, max_item_bytes: 33554432 }, expected_custody_revision: 0 },
  available_profiles: ['unirouter.execution-context.v1', 'unirouter.execution-identity.v1', 'unirouter.runtime-observation.v2'],
  default_profiles: ['unirouter.execution-context.v1', 'unirouter.execution-identity.v1'], existing_profiles: [], content_available: true,
  receiver_mode: 'new_install', existing_binding_sha256: null,
  ur_policy: { schema_version: 'unirouter.payload-policy.v2', source_instance: 'source', org_id: '2', project_id: '11', enterprise: { policy_ref: 'enterprise', explicit: false }, project: { policy_ref: 'project', explicit: false }, version_vector: 'vector', storage_status: 'ready', deployment_mode: 'full_content', full_content_enabled: true,
    limits: { max_saved_bytes: 33554432, max_read_bytes: 33554432, max_buffer_bytes: 33554432, max_manifest_bytes: 32768, max_manifest_items: 32 }, policy_capabilities: { supported_protocols: ['openai.chat'], observation_points: ['client_request'] } },
  allowed_coverage: ['exact', 'all_current_future'], allowed_policy_scopes: ['project', 'enterprise'],
  allowed_policy_access: [ { target_kind: 'enterprise_default', coverage: 'exact', read: true, write: true }, { target_kind: 'project_policy', coverage: 'exact', read: true, write: true }, { target_kind: 'project_policy', coverage: 'all_current_future', read: true, write: true }, { target_kind: 'user_exception', coverage: 'exact', read: true, write: true } ], issues: [],
};
const status: SetupStatus = { schema_version: 'unirouter.xinsight.setup-status.v1', installation_id: invitation.installation_id, consent_revision: 1, state: 'waiting_data', configuration_ready: true, purpose_status: { policy_manage: 'granted', source_enroll: 'granted' }, ur_policy_status: { state: 'effective', policy_ref: 'p', policy_version: '1', version_vector: 'v' }, xi: null, issues: [] };
const props = { orgId: 2, projectId: 11, projectName: '项目 A', projects: [{ id: 11, org_id: 2, name: '项目 A' }, { id: 12, org_id: 2, name: '项目 B' }], enabled: true, onSaved: vi.fn() };
function prepare() {
 vi.mocked(xInsightCommunicationApi.get).mockResolvedValue({project_id:11,automatic_enabled:false,cadence_ms:60000,revision:1,installation_id:invitation.installation_id,effective:'paused',peer_state:'unknown',last_code:''});
 vi.mocked(xInsightCommunicationApi.once).mockResolvedValue({idempotency_key:'initialize',installation_id:invitation.installation_id,revision:1,state:'empty',units:1,requests:1,started_at:'2026-10-19T00:00:00Z'});
  vi.mocked(xInsightSetupApi.peer).mockResolvedValue({ schema_version: 'unirouter.xinsight.setup-peer.v1', org_id: '2', anchor_project_id: '11', source_instance: 's', xi_origin: 'https://xi.example', xi_deployment_id: invitation.deployment_id, xi_setup_url: 'https://xi.example/connectors/unirouter/setup', trust_ready: true });
  vi.mocked(xInsightSetupApi.preview).mockResolvedValue(preview); vi.mocked(xInsightSetupApi.create).mockResolvedValue(status); vi.mocked(xInsightSetupApi.status).mockResolvedValue(status);
}
afterEach(() => { vi.resetAllMocks(); sessionStorage.clear(); window.history.replaceState(null, '', '/'); });
describe('normal X-Insight setup', () => {
  it('requires the actual pinned address and exposes no technical intake fields', async () => {
    prepare(); render(<XInsightSetup {...props} />); const user = userEvent.setup();
    await user.type(screen.getByLabelText('XI 地址'), 'https://other.example');
    expect(screen.getByRole('button', { name: '在 XI 登录并确认' })).toBeDisabled();
    expect(xInsightSetupApi.preview).not.toHaveBeenCalled();
    expect(screen.queryByRole('textbox', { name: /JSON|IP|registration|invitation/i })).not.toBeInTheDocument();
    await user.clear(screen.getByLabelText('XI 地址')); await user.type(screen.getByLabelText('XI 地址'), 'https://xi.example');
    await waitFor(() => expect(screen.getByRole('button', { name: '在 XI 登录并确认' })).toBeEnabled());
  });
  it('rejects foreign-origin or incomplete fragments before resolving and removes token from the address',async()=>{
    prepare();window.history.replaceState(null,'','/integrations'+invitationFragment);
    render(<XInsightSetup {...props} invitationFragment={invitationFragment.replace('https%3A%2F%2Fxi.example','https%3A%2F%2Fforeign.example')}/>);
    await screen.findByRole('alert');expect(xInsightSetupApi.preview).not.toHaveBeenCalled();expect(window.location.hash).toBe('');expect(sessionStorage.length).toBe(0);
  });
  it('recovers only the actor organization project bound public installation id and still reauthorizes via GET',async()=>{
    prepare();sessionStorage.setItem('xinsight.setup-installation:91:2:11',invitation.installation_id);
    const view=render(<XInsightSetup {...props} actorId={91}/>);await screen.findByText(/等待首次真实数据/);
    expect(xInsightSetupApi.status).toHaveBeenCalledWith(2,invitation.installation_id);view.unmount();vi.mocked(xInsightSetupApi.status).mockClear();
    render(<XInsightSetup {...props} actorId={92}/>);await screen.findByLabelText('XI 地址');expect(xInsightSetupApi.status).not.toHaveBeenCalled();expect(xInsightSetupApi.create).not.toHaveBeenCalled();
  });
  it('drops a stale recovery hint after the authorized status route returns 404',async()=>{
    prepare();sessionStorage.setItem('xinsight.setup-installation:91:2:11',invitation.installation_id);vi.mocked(xInsightSetupApi.status).mockRejectedValue(new ApiError('missing',404,{}));
    render(<XInsightSetup {...props} actorId={91}/>);await waitFor(()=>expect(sessionStorage.getItem('xinsight.setup-installation:91:2:11')).toBeNull());expect(screen.getByLabelText('XI 地址')).toBeInTheDocument();expect(screen.queryByText(/已有连接。/)).not.toBeInTheDocument();
  });
  it('clears the fragment while peer verification is pending and a fresh invitation takes priority over an old recovery hint',async()=>{
    prepare();sessionStorage.setItem('xinsight.setup-installation:91:2:11','xci_BBBBBBBBBBBBBBBBBBBBBB');window.history.replaceState(null,'','/integrations'+invitationFragment);
    let resolvePeer!: (value: Awaited<ReturnType<typeof xInsightSetupApi.peer>>)=>void;
    vi.mocked(xInsightSetupApi.peer).mockReturnValue(new Promise(resolve=>{resolvePeer=resolve;}));
    render(<XInsightSetup {...props} actorId={91} invitationFragment={invitationFragment}/>);
    expect(window.location.hash).toBe('');expect(xInsightSetupApi.status).not.toHaveBeenCalled();
    resolvePeer({schema_version:'unirouter.xinsight.setup-peer.v1',org_id:'2',anchor_project_id:'11',source_instance:'s',xi_origin:'https://xi.example',xi_deployment_id:invitation.deployment_id,xi_setup_url:'https://xi.example/setup',trust_ready:true});
    await screen.findByText('最终确认');expect(xInsightSetupApi.preview).toHaveBeenCalledWith(2,11,token);
    const user=userEvent.setup();await user.click(screen.getByLabelText('我确认上述两端设置、供数范围及实际策略管理权限。'));await user.click(screen.getByRole('button',{name:'确认并连接'}));
    await waitFor(()=>expect(sessionStorage.getItem('xinsight.setup-installation:91:2:11')).toBe(invitation.installation_id));
    expect(Object.values(sessionStorage).join('')).not.toContain(token);
  });
  it('reads back a committed confirmation after its response was lost and the page remounts',async()=>{
    prepare();vi.mocked(xInsightSetupApi.create).mockRejectedValueOnce(new ApiError('response_lost',503,{}));
    const view=render(<XInsightSetup {...props} actorId={91} invitationFragment={invitationFragment}/>);const user=userEvent.setup();await screen.findByText('最终确认');await user.click(screen.getByLabelText('我确认上述两端设置、供数范围及实际策略管理权限。'));await user.click(screen.getByRole('button',{name:'确认并连接'}));
    await screen.findByText(/此请求已保留/);expect(sessionStorage.getItem('xinsight.setup-installation:91:2:11')).toBe(invitation.installation_id);expect(Object.values(sessionStorage).join('')).not.toContain(token);expect(Object.values(sessionStorage).join('')).not.toContain('policy_access');
    view.unmount();render(<XInsightSetup {...props} actorId={91}/>);await screen.findByText(/等待首次真实数据/);expect(xInsightSetupApi.status).toHaveBeenCalledWith(2,invitation.installation_id);expect(xInsightSetupApi.create).toHaveBeenCalledOnce();
  });
  it('confirms future coverage, exact server defaults, both policy scopes and approved body limits together', async () => {
    prepare(); render(<XInsightSetup {...props} invitationFragment={invitationFragment} />); const user = userEvent.setup();
    await screen.findByText('最终确认');
    expect(screen.getByLabelText('供数项目范围')).toHaveValue('all_current_future');
    expect(screen.getByLabelText('网关派发前终止')).not.toBeChecked();
    expect(screen.getByLabelText('成员正文例外')).not.toBeChecked();
    expect(screen.getByLabelText('UR 正文保留（天）')).toHaveValue(30);
    await user.click(screen.getByLabelText('我确认上述两端设置、供数范围及实际策略管理权限。'));
    await user.click(screen.getByRole('button', { name: '确认并连接' }));
    await waitFor(() => expect(xInsightSetupApi.create).toHaveBeenCalledOnce());
    const put = vi.mocked(xInsightSetupApi.create).mock.calls[0][1];
    await waitFor(() => expect(xInsightCommunicationApi.once).toHaveBeenCalledOnce());
    expect(vi.mocked(xInsightCommunicationApi.once).mock.calls[0][2]).toBe(`${put.request_id}:initialize`);

    expect(put.profiles).toEqual(preview.default_profiles); expect(put.coverage).toBe('all_current_future');
    expect(put.policy_access).toEqual([{ target_kind: 'enterprise_default', coverage: 'exact', read: true, write: true }, { target_kind: 'project_policy', coverage: 'all_current_future', read: true, write: true }]);
    expect(put.ur_policy?.scope).toBe('enterprise'); expect(put.ur_policy?.put.expected_vector).toBe('{"enterprise":0}'); expect(put.ur_policy?.put.desired.retention_seconds).toBe(2592000);
    expect(put.ur_policy?.put.desired.quota_bytes).toBe(5368709120); expect(put.ur_policy?.put.desired.max_saved_bytes).toBe(33554432);
    expect(await screen.findByText(/等待首次真实数据/)).toBeInTheDocument();
  });
  it('keeps an empty first control poll recoverably incomplete without enabling automatic communication', async () => {
    prepare();
    vi.mocked(xInsightCommunicationApi.once).mockResolvedValue({idempotency_key:'initialize',installation_id:invitation.installation_id,revision:1,state:'partial',units:2,requests:2,started_at:'2026-10-19T00:00:00Z'});
    vi.mocked(xInsightSetupApi.status).mockResolvedValue({...status,state:'provisioning',configuration_ready:false});
    render(<XInsightSetup {...props} invitationFragment={invitationFragment} />);
    const user=userEvent.setup();await screen.findByText('最终确认');
    await user.click(screen.getByLabelText('我确认上述两端设置、供数范围及实际策略管理权限。'));
    await user.click(screen.getByRole('button',{name:'确认并连接'}));
    await screen.findByText('连接已保存；初始化通讯尚未完成，请在通讯设置中执行一次。');
    await waitFor(()=>expect(xInsightCommunicationApi.once).toHaveBeenCalledOnce());
    expect(vi.mocked(xInsightCommunicationApi.once).mock.calls[0][1].automatic_enabled).toBe(false);
  });
  it('keeps source exclusions distinct and lets pure supply omit enterprise management', async () => {
    prepare(); render(<XInsightSetup {...props} invitationFragment={invitationFragment} />); const user = userEvent.setup(); await screen.findByText('最终确认');
    await user.click(screen.getByLabelText('项目 B')); await user.click(screen.getByLabelText('企业正文默认策略'));
    expect(screen.getByText(/XI 的正文总开关需 UR 组织管理员另行授权/)).toBeInTheDocument();
    expect(screen.getByText(/排除不会缩小企业默认或全项目策略管理权限/)).toBeInTheDocument();
    await user.click(screen.getByLabelText('我确认上述两端设置、供数范围及实际策略管理权限。')); await user.click(screen.getByRole('button', { name: '确认并连接' }));
    await waitFor(() => expect(xInsightSetupApi.create).toHaveBeenCalled());
    const put = vi.mocked(xInsightSetupApi.create).mock.calls[0][1]; expect(put.excluded_project_ids).toEqual(['12']); expect(put.policy_access.map(a => a.target_kind)).toEqual(['project_policy']);
  });
  it('allows a real member choice and never requires typing an identity', async () => {
    prepare(); vi.mocked(xInsightSetupApi.members).mockResolvedValue({ data: [{ user_id: 27, username: '成员甲', status: 'active' }], total: 1 });
    render(<XInsightSetup {...props} invitationFragment={invitationFragment} />); const user = userEvent.setup(); await screen.findByText('最终确认'); await user.click(screen.getByLabelText('成员正文例外'));
    await screen.findByRole('option', { name: '成员甲' }); await user.selectOptions(screen.getByLabelText('成员正文例外成员'), '27');
    await user.click(screen.getByLabelText('我确认上述两端设置、供数范围及实际策略管理权限。')); await user.click(screen.getByRole('button', { name: '确认并连接' }));
    await waitFor(() => expect(xInsightSetupApi.create).toHaveBeenCalled()); expect(vi.mocked(xInsightSetupApi.create).mock.calls[0][1].policy_access.slice(-1)[0]?.user_id).toBe('27');
  });
  it('retains the exact request on an ambiguous response and permits correction after an explicit conflict', async () => {
    prepare(); vi.mocked(xInsightSetupApi.create).mockRejectedValueOnce(new ApiError('unavailable', 503, {})).mockRejectedValueOnce(new ApiError('setup_changed', 409, {}));
    render(<XInsightSetup {...props} invitationFragment={invitationFragment} />); const user = userEvent.setup(); await screen.findByText('最终确认');
    await user.click(screen.getByLabelText('我确认上述两端设置、供数范围及实际策略管理权限。')); await user.click(screen.getByRole('button', { name: '确认并连接' }));
    await screen.findByText('此请求已保留。先读回；重试使用完全相同的确认，不重复扩大授权。'); await user.click(screen.getByRole('button', { name: '重试同一确认' }));
    await waitFor(() => expect(xInsightSetupApi.create).toHaveBeenCalledTimes(2)); expect(vi.mocked(xInsightSetupApi.create).mock.calls[1][1]).toEqual(vi.mocked(xInsightSetupApi.create).mock.calls[0][1]);
    await waitFor(() => expect(screen.getByLabelText('供数项目范围')).toBeEnabled()); expect(screen.getByLabelText('我确认上述两端设置、供数范围及实际策略管理权限。')).not.toBeChecked();
  });
});

describe('setup diagnostics preserve server facts', () => {
  it('keeps an expired invitation rejected by the original server preview without creating a grant', async () => {
    prepare(); vi.mocked(xInsightSetupApi.preview).mockRejectedValue(new ApiError('invitation_expired', 409, {}));
    render(<XInsightSetup {...props} invitationFragment={invitationFragment} />);
    await screen.findByText('邀请预览未确认。请在 XI 重新读取本次确认，勿粘贴凭据或手工登记。');
    expect(screen.queryByRole('button', { name: '确认并连接' })).not.toBeInTheDocument();
    expect(xInsightSetupApi.create).not.toHaveBeenCalled();
  });

  it.each(['needs_action', 'provisioning'] as const)('explains custody loading without promoting %s to ready or repeating consent', async state => {
    prepare();
    vi.mocked(xInsightSetupApi.status).mockResolvedValue({ ...status, state, configuration_ready: false, issues: [{ code: 'custody_loading', requires_choice: false }] });
    render(<XInsightSetup {...props} installationId={invitation.installation_id} />);
    await screen.findByText('本次确认已登记。重新读取只核对状态，不会重复授权。');
    expect(screen.getByText(/XI 正在加载正文保管设置/)).toHaveAttribute('role', 'status');
    expect(screen.getByText(state === 'needs_action' ? '需要处理。配置尚未就绪。' : '正在配置。配置尚未就绪。')).toBeInTheDocument();
    expect(screen.queryByText(/已开始同步|接入条件发生变化/)).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: '确认并连接' })).not.toBeInTheDocument();
    const user = userEvent.setup(); await user.click(screen.getByRole('button', { name: '重新读取接入状态' }));
    await waitFor(() => expect(xInsightSetupApi.status).toHaveBeenCalledTimes(2));
    expect(xInsightSetupApi.create).not.toHaveBeenCalled();
  });
  it.each([
    ['installation_authority_changed', /两端管理员/], ['redeem_origin_changed', /受信对端/],
    ['machine_candidate_changed', /组织管理员/], ['machine_network_changed', /允许地址/],
    ['policy_grant_invalid', /UR 管理员/], ['policy_grant_authority_changed', /UR 管理员/],
    ['policy_grant_conflict', /不会覆盖/], ['source_qualification_changed', /来源身份/],
    ['source_enrollment_conflict', /已有供数授权/], ['machine_authority_revoked', /撤销原因/],
    ['policy_grant_changed', /不会恢复旧授权/], ['source_grant_changed', /不会恢复旧授权/],
    ['installation_inactive', /XI 管理员/], ['invitation_expired', /重新发起邀请/],
    ['custody_unavailable', /XI 管理员/], ['current_custody_pending', /无需重复确认/],
    ['xi_actor_permission_changed', /XI 管理员/], ['custody_revision_changed', /从 XI 重新发起/],
  ])('gives a human next action for %s', async (code, message) => {
    prepare(); vi.mocked(xInsightSetupApi.status).mockResolvedValue({ ...status, state: 'needs_action', configuration_ready: false, issues: [{ code: code as string, requires_choice: true }] });
    render(<XInsightSetup {...props} installationId={invitation.installation_id} />);
    expect(await screen.findByRole('alert')).toHaveTextContent(message);
    expect(screen.getByText('需要处理。配置尚未就绪。')).toBeInTheDocument();
    expect(screen.queryByText(/接入条件发生变化|尚无法解释/)).not.toBeInTheDocument();
    expect(xInsightSetupApi.create).not.toHaveBeenCalled();
  });
  it('keeps preview blockers actionable and requires a new final confirmation', async () => {
    prepare(); vi.mocked(xInsightSetupApi.preview).mockResolvedValue({ ...preview, issues: [{ code: 'policy_grant_conflict', requires_choice: true }] });
    render(<XInsightSetup {...props} invitationFragment={invitationFragment} />);
    expect(await screen.findByRole('alert')).toHaveTextContent('后台不会覆盖它');
    expect(screen.getByLabelText('我确认上述两端设置、供数范围及实际策略管理权限。')).not.toBeChecked();
    expect(screen.getByRole('button', { name: '确认并连接' })).toBeDisabled();
    expect(screen.queryByText('本次确认已登记。重新读取只核对状态，不会重复授权。')).not.toBeInTheDocument();
  });
  it('provides bounded public support identifiers for an unknown code without claiming a changed condition', async () => {
    prepare(); vi.mocked(xInsightSetupApi.status).mockResolvedValue({ ...status, state: 'needs_action', configuration_ready: false, issues: [{ code: 'future_setup_condition', requires_choice: true }] });
    render(<XInsightSetup {...props} installationId={invitation.installation_id} />);
    await screen.findByRole('alert'); expect(screen.getByText('诊断代码：future_setup_condition；接入编号：' + invitation.installation_id)).toBeInTheDocument();
    expect(screen.getByText(/尚无法解释此接入条件/)).toBeInTheDocument();
    expect(screen.queryByText(/接入条件发生变化/)).not.toBeInTheDocument();
  });
  it.each(['xiiv1_' + 'a'.repeat(43), 'xiiv1_' + 'A'.repeat(43), 'secret:' + 'A'.repeat(200), '<script>private</script>', '__proto__'])('hides malformed diagnostic data %s', async code => {
    prepare(); vi.mocked(xInsightSetupApi.status).mockResolvedValue({ ...status, issues: [{ code, requires_choice: true }] });
    render(<XInsightSetup {...props} installationId={invitation.installation_id} />);
    await screen.findByRole('alert'); expect(screen.getByText(/诊断代码：诊断代码不可用/)).toBeInTheDocument();
    expect(screen.queryByText(code, { exact: false })).not.toBeInTheDocument();
  });
});
