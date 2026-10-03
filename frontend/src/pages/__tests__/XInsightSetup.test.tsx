import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import XInsightSetup from '../XInsightSetup';
import { xInsightSetupApi, type SetupPreview, type SetupStatus } from '../../api/xInsightSetup';
import { ApiError } from '../../api/transport';

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
    expect(put.profiles).toEqual(preview.default_profiles); expect(put.coverage).toBe('all_current_future');
    expect(put.policy_access).toEqual([{ target_kind: 'enterprise_default', coverage: 'exact', read: true, write: true }, { target_kind: 'project_policy', coverage: 'all_current_future', read: true, write: true }]);
    expect(put.ur_policy?.scope).toBe('enterprise'); expect(put.ur_policy?.put.expected_vector).toBe('{"enterprise":0}'); expect(put.ur_policy?.put.desired.retention_seconds).toBe(2592000);
    expect(put.ur_policy?.put.desired.quota_bytes).toBe(5368709120); expect(put.ur_policy?.put.desired.max_saved_bytes).toBe(33554432);
    expect(await screen.findByText(/等待首次真实数据/)).toBeInTheDocument();
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
