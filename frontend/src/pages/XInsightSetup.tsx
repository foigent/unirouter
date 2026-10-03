import { useEffect, useState } from 'react';
import { xInsightSetupApi, type SetupConsentPut, type SetupCoverage, type SetupPeer, type SetupPolicyAccess, type SetupPreview, type SetupStatus, type SetupProjectMember } from '../api/xInsightSetup';

import { loadAllPages } from '../utils/loadAllPages';
import { ApiError } from '../api/transport';
import type { ScopedPayloadPut } from '../api/contentManagement';

const profileNames: Record<string, string> = {
  'unirouter.execution-context.v1': '执行上下文', 'unirouter.execution-identity.v1': '执行身份',
  'unirouter.capability-snapshot.v1': '能力快照', 'unirouter.cost-basis.v1': '成本依据',
  'unirouter.attempt-observation.v1': '请求尝试', 'unirouter.destination-observation.v1': '目标选择',
  'unirouter.policy-observation.v1': '策略判断', 'unirouter.runtime-observation.v1': '运行状态', 'unirouter.runtime-observation.v2': '网关派发前终止',
};
const issueNames: Record<string, string> = {
  machine_registration_requires_org_admin: '此接入需要组织管理员确认，请由组织管理员在这里继续。',
  existing_receiver_incompatible: '已有连接的身份或受信地址不兼容，请先核对原连接。',
  existing_receiver_conflict: '已有连接存在冲突，请先在维护详情中核对。',
  existing_enrollment_requires_review: '此范围已有供数授权，请先核对原授权，避免重复接入。',
 existing_source_requires_review: '已有来源的身份、范围或受信地址不完整匹配，请核对原来源后重新预览。',
 existing_source_changed: '确认后原来源已变化，后台保留原凭据并停止，请重新核对。',
  enterprise_joint_management_not_granted: '未授予企业正文联管写权限。仍可接收所选数据；XI 的正文总开关需 UR 组织管理员另行授权。',
  human_authority_changed: '确认者的权限已变化，后台已停止，请由有权限的管理员核对。',
  ur_policy_changed: 'UR 策略或其生效条件已变化，后台未覆盖新策略，请重新核对。',
  machine_activation_conflict: '现有机器授权冲突，后台未替换其他有效凭据。',
  xi_status_unavailable: 'XI 当前状态暂不可读，稍后刷新核对；不会重新提交授权。',
};
const stateNames: Record<string, string> = { waiting_consent: '等待确认', provisioning: '正在配置', needs_action: '需要处理', waiting_data: '等待首次真实数据', ready: '已开始同步', failed: '接入失败' };
const kindNames: Record<SetupPolicyAccess['target_kind'], string> = { enterprise_default: '企业正文默认策略', project_policy: '项目正文策略', user_exception: '成员正文例外' };

interface Props {
  actorId?: number; actorOrgId?: number; orgId: number; projectId: number; projectName: string;
  projects: { id: number; org_id?: number; name: string }[];
  invitationFragment?: string;
  installationId?: string;
  enabled: boolean;
  onSaved: () => void;
}
export default function XInsightSetup({ actorId, actorOrgId, orgId, projectId, projectName, projects, invitationFragment = '', installationId, enabled, onSaved }: Props) {
  const fragment = new URLSearchParams(invitationFragment.replace(/^#/, ''));
  const invitationToken = fragment.get('invitation') || '';
  const fragmentOrigin = fragment.get('xinsight_origin') || '';
  const fragmentValid = [...fragment.keys()].length === 2 && fragment.getAll('invitation').length === 1 && fragment.getAll('xinsight_origin').length === 1 && /^xiiv1_[A-Za-z0-9_-]{43}$/.test(invitationToken);
  const recoveryKey = actorId ? `xinsight.setup-installation:${actorId}:${orgId}:${projectId}` : undefined;
  const [recoveryId,setRecoveryId] = useState(() => {const value=recoveryKey && !invitationFragment ? sessionStorage.getItem(recoveryKey) : null;return value && /^xci_[A-Za-z0-9_-]{22}$/.test(value) ? value : undefined;});
  function receiveStatus(value: SetupStatus) {setStatus(value);if(recoveryKey){sessionStorage.setItem(recoveryKey,value.installation_id);setRecoveryId(value.installation_id);}}
  const [members,setMembers]=useState<SetupProjectMember[]>([]);
 const [oldConnection,setOldConnection]=useState(false);
 const [peer, setPeer] = useState<SetupPeer>();
  const [address, setAddress] = useState('');
  const [preview, setPreview] = useState<SetupPreview>();
 const invitation = preview?.invitation;
  const [profiles, setProfiles] = useState<string[]>([]);
  const [coverage, setCoverage] = useState<SetupCoverage>('all_current_future');
  const [excluded, setExcluded] = useState<string[]>([]);
  const [includeContent, setIncludeContent] = useState(false);
  const [access, setAccess] = useState<SetupPolicyAccess[]>([]);
  const [policyScope, setPolicyScope] = useState<'reuse' | 'enterprise' | 'project'>('reuse');
  const [retentionDays, setRetentionDays] = useState(30);
  const [quotaMiB, setQuotaMiB] = useState(5120);
  const [maximumMiB, setMaximumMiB] = useState(32);
  const [confirmed, setConfirmed] = useState(false);
  const [status, setStatus] = useState<SetupStatus>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [requestId,setRequestId] = useState(() => crypto.randomUUID());
  const [submitted, setSubmitted] = useState<SetupConsentPut>();

  useEffect(() => {if(invitationFragment)window.history.replaceState(null, '', `${window.location.pathname}${window.location.search}`);}, [invitationFragment]);
  useEffect(() => { let active = true; xInsightSetupApi.peer(orgId, projectId).then(v => { if (active) setPeer(v); }).catch(() => { if (active) setError('无法核对受信 XI 地址，请稍后重读。'); }); return () => { active = false; }; }, [orgId, projectId]);
  useEffect(() => {
    if (!invitationFragment || !peer) return;
    if (!fragmentValid || !peer.trust_ready || fragmentOrigin !== peer.xi_origin) {setError('邀请地址与受信 XI 不一致或链接不完整，请从 XI 正常接入页面重新发起。');return;}
    let active = true;
    xInsightSetupApi.preview(orgId, projectId, invitationToken).then(v => {
      if (!active) return;
      setPreview(v); setMaximumMiB(Math.min(32,v.ur_policy.limits.max_saved_bytes / 1048576)); setProfiles(v.default_profiles);setCoverage(v.allowed_coverage.includes('all_current_future')?'all_current_future':'exact'); setIncludeContent(v.content_available && v.xi_consent.custody.mode === 'full_content');
      setPolicyScope(v.content_available&&v.xi_consent.custody.mode==='full_content'&&v.ur_policy.enterprise.effective?.config.mode!=='full_content'&&v.allowed_policy_scopes.includes('enterprise')?'enterprise':'reuse');
      setAccess(v.allowed_policy_access.filter(a => a.target_kind === 'enterprise_default' || (a.target_kind === 'project_policy' && a.coverage === (v.allowed_coverage.includes('all_current_future')?'all_current_future':'exact'))));
    }).catch(() => { if (active) setError('邀请预览未确认。请在 XI 重新读取本次确认，勿粘贴凭据或手工登记。'); });
    return () => { active = false; };
  }, [orgId, projectId, invitationFragment, invitationToken, fragmentOrigin, fragmentValid, peer]);
  const wantsMember=access.some(a=>a.target_kind==='user_exception'&&a.coverage==='exact');
 useEffect(()=>{if(!wantsMember)return;let active=true;loadAllPages<SetupProjectMember>((page,size)=>xInsightSetupApi.members(projectId,page,size)).then(v=>{if(active)setMembers(v.filter(m=>m.status==='active'));}).catch(()=>{if(active)setError('当前项目成员目录不可读，请重读后选择，不能手填身份。');});return ()=>{active=false;};},[projectId,wantsMember]);
 const currentInstallation = status?.installation_id || installationId || recoveryId;
  useEffect(() => {
    if (!currentInstallation || oldConnection) return;
    let active = true;
    const refresh = () => xInsightSetupApi.status(orgId, currentInstallation).then(v => { if (active) receiveStatus(v); }).catch(e => { if (active) {if(e instanceof ApiError&&e.status===404){if(installationId)setOldConnection(true);else{if(recoveryKey)sessionStorage.removeItem(recoveryKey);setRecoveryId(undefined);}} else setError('接入状态暂不可读。刷新只核对状态，不会重复授权。');} });
    void refresh(); const timer = window.setInterval(() => { void refresh(); }, 5000);
    return () => { active = false; window.clearInterval(timer); };
  }, [orgId, currentInstallation,oldConnection,installationId,recoveryKey]);

  function changeAccess(kind: SetupPolicyAccess['target_kind'], patch: Partial<SetupPolicyAccess>, selected = true) {
    setConfirmed(false);
    setAccess(current => {
      const prior = current.find(a => a.target_kind === kind);
      const next = current.filter(a => a.target_kind !== kind);
      if (selected) next.push({ target_kind: kind, coverage: kind === 'enterprise_default' ? 'exact' : 'exact', read: true, write: true, ...prior, ...patch });
      return next.sort((a, b) => a.target_kind.localeCompare(b.target_kind));
    });
  }
  function policyPut(): SetupConsentPut['ur_policy'] {
    if (!preview || policyScope === 'reuse') return null;
    const layer = policyScope === 'enterprise' ? preview.ur_policy.enterprise : preview.ur_policy.project;
    if (!layer) throw new Error('当前策略层不可读，请刷新。');
    const capabilities = preview.ur_policy.policy_capabilities;
    if (!capabilities) throw new Error('UR 当前正文能力未确认，请刷新。');
    const put: ScopedPayloadPut = {
      schema_version: 'unirouter.payload-policy.v2', expected_policy_ref: layer.policy_ref,
      expected_version: layer.desired?.policy_version || '0', expected_vector: policyScope === 'enterprise' ? `{"enterprise":${layer.effective?.policy_version || '0'}}` : preview.ur_policy.version_vector,
      execution_storage_mode: 'temporary_allowed', project_quota_bytes: Math.round(quotaMiB * 1048576),
      desired: { mode: 'full_content', supported_protocols: capabilities.supported_protocols,
        observation_points: capabilities.observation_points, allowed_purposes: ['human_review'],
        retention_seconds: Math.round(retentionDays * 86400), quota_bytes: Math.round(quotaMiB * 1048576),
        max_saved_bytes: Math.round(maximumMiB * 1048576), on_limit: 'record_capture_gap' },
    };
    return { scope: policyScope, put };
  }
  async function save() {
    if (!preview || !invitation || !confirmed) return;
    setBusy(true); setError('');
    try {
      const put = submitted || { schema_version: 'unirouter.xinsight.setup-consent.v1' as const, request_id: requestId, project_id: String(projectId), invitation,
        expected_preview_sha256: preview.preview_sha256, expected_xi_consent_sha256: preview.xi_consent.consent_sha256,
        receiver_mode: preview.receiver_mode, existing_binding_sha256: preview.existing_binding_sha256,
        coverage, excluded_project_ids: excluded, profiles: [...profiles].sort(), include_content: includeContent,
        policy_access: access, ur_policy: policyPut() };
      setSubmitted(put);
      // A nonsecret locator survives a lost response. Only the authorized GET
      // may establish whether this confirmation actually committed.
      if (recoveryKey) sessionStorage.setItem(recoveryKey, put.invitation.installation_id);
      const next = await xInsightSetupApi.create(orgId, put); receiveStatus(next);
      window.history.replaceState(null, '', `${window.location.pathname}${window.location.search}`); onSaved();
    } catch (e) { if(e instanceof ApiError&&[400,403,409].includes(e.status)){if(recoveryKey)sessionStorage.removeItem(recoveryKey);setSubmitted(undefined);setConfirmed(false);setRequestId(crypto.randomUUID());}setError(e instanceof Error ? e.message : '确认结果未明，请重读状态后再用同一请求重试。'); }
    finally { setBusy(false); }
  }
  const enterpriseWrite = access.some(a => a.target_kind === 'enterprise_default' && a.write);
  const bodyInherits = preview?.ur_policy.enterprise.effective?.config.mode === 'full_content';

  return <section aria-label="连接 X-Insight" style={{ border: '1px solid var(--semi-color-border)', borderRadius: 8, padding: 16 }}>
    <h3>连接 X-Insight</h3>
    <p>核对 XI 地址，在 XI 登录并确认保管设置，然后在这里确认供数范围和管理权限。确认保存后，关闭页面也会由后台继续。</p>
    {error && <p role="alert">{error}</p>}
    {!status && !preview && !installationId && <>
      <label>XI 地址 <input aria-label="XI 地址" value={address} disabled={!enabled || busy} onChange={e => setAddress(e.target.value)} placeholder="https://xi.example.com" /></label>
      <button disabled={!enabled || !peer?.trust_ready || address !== peer.xi_origin} onClick={() => { if (peer && address === peer.xi_origin) {if(actorId)sessionStorage.setItem(`xinsight.setup-project:${actorId}:${actorOrgId ?? orgId}`,String(projectId));window.location.assign(peer.xi_setup_url);} }}>在 XI 登录并确认</button>
      {peer && !peer.trust_ready && <p role="status">尚未配置受信 XI 对端。请运维使用正式安装工具准备并回读；本页不登记任意地址。</p>}
      {peer?.trust_ready && address && address !== peer.xi_origin && <p role="status">输入地址与当前受信 XI 不一致，请核对准确地址。</p>}
    </>}
    {oldConnection && <p>已有连接。当前状态与授权可在维护详情中核对；本页不会改写历史同意。</p>}
 {preview && !status && <fieldset disabled={busy || !!submitted}>
      <legend>最终确认</legend><button type="button" onClick={()=>{if(invitation)void xInsightSetupApi.preview(orgId,projectId,invitationToken).then(v=>{setPreview(v);setConfirmed(false);}).catch(()=>setError('预览仍未确认，请核对邀请和当前范围。'));}}>重新预览当前范围</button>
      <p>XI 已由其登录者确认保管：{preview.xi_consent.custody.mode === 'full_content' ? '允许正文副本' : '关闭正文副本'}，保存 {preview.xi_consent.custody.retention_seconds / 86400} 天；确认到期后需在 XI 重新发起。</p>
      <p>{preview.receiver_mode === 'adopt_existing' ? '接管现有连接，保留原凭据和同步进度。' : '创建本次新连接。'}</p>
      <label>供数项目范围 <select aria-label="供数项目范围" value={coverage} onChange={e => { setCoverage(e.target.value as SetupCoverage); setExcluded([]); setConfirmed(false); }}>
        {preview.allowed_coverage.map(v => <option key={v} value={v}>{v === 'exact' ? `当前项目：${projectName}` : '本组织当前及未来全部项目'}</option>)}
      </select></label>
      {coverage === 'all_current_future' && <div><p>排除这些现有项目的供数。排除不会缩小企业默认或全项目策略管理权限。</p>{projects.filter(p => p.org_id === orgId && p.id !== projectId).map(p => <label key={p.id} style={{ display: 'block' }}><input type="checkbox" checked={excluded.includes(String(p.id))} onChange={e => { setExcluded(v => e.target.checked ? [...v, String(p.id)].sort() : v.filter(id => id !== String(p.id))); setConfirmed(false); }} />{p.name}</label>)}</div>}
      <p>选择数据类别。未来项目继承这些选择；新增数据类别不会自动加入。</p>
 {preview.existing_profiles.length>0 && <p>已读取原来源选择。本次接管保留匹配的原凭据、绑定和同步进度；如需改变这些类别，请先按原来源流程处理后重新预览。</p>}
      {preview.available_profiles.map(p => <label key={p} style={{ display: 'block' }}><input type="checkbox" checked={profiles.includes(p)} onChange={e => { setProfiles(v => e.target.checked ? [...v, p].sort() : v.filter(x => x !== p)); setConfirmed(false); }} />{profileNames[p] || '其他可用数据类别'}</label>)}
      <label style={{ display: 'block' }}><input type="checkbox" checked={includeContent} disabled={!preview.content_available || preview.xi_consent.custody.mode !== 'full_content'} onChange={e => { setIncludeContent(e.target.checked); setConfirmed(false); }} />同步所授权范围内可用的正文</label>
      <label style={{ display: 'block' }}>UR 初始正文策略 <select aria-label="UR 初始正文策略" value={policyScope} onChange={e => { setPolicyScope(e.target.value as typeof policyScope); setConfirmed(false); }}>
        <option value="reuse">复用当前已生效策略</option>{preview.allowed_policy_scopes.map(s => <option key={s} value={s}>{s === 'enterprise' ? '明确修改企业默认正文策略' : '明确修改当前项目正文策略'}</option>)}
      </select></label>
      {includeContent && coverage === 'all_current_future' && !bodyInherits && policyScope !== 'enterprise' && <p role="alert">未来项目正文需要有效的企业默认策略，仅修改当前项目不能覆盖未来项目。</p>}
      {policyScope !== 'reuse' && <>
        <p>{policyScope === 'enterprise' ? '企业默认可能影响本组织未同步的项目；原项目与成员例外保留。' : '只修改当前项目；成员例外保留。'}此初始策略修改与下方后续管理委托分别确认。</p>
        <label>UR 正文保留（天） <input type="number" min={1 / 86400} max={365} step="any" value={retentionDays} onChange={e => { setRetentionDays(Number(e.target.value)); setConfirmed(false); }} /></label>
        <label>项目限额（MiB） <input type="number" min={1 / 1048576} value={quotaMiB} onChange={e => { setQuotaMiB(Number(e.target.value)); setConfirmed(false); }} /></label>
        <label>单条上限（MiB） <input type="number" min={1 / 1048576} value={maximumMiB} onChange={e => { setMaximumMiB(Number(e.target.value)); setConfirmed(false); }} /></label>
      </>}
      <h4>允许 XI 后续管理的策略</h4>
      <p>管理权限独立于供数。企业默认可能影响未同步项目；全部项目权限包括供数中排除的项目。未勾选的权限不会授予。</p>
      {(['enterprise_default', 'project_policy', 'user_exception'] as const).map(kind => {
        const options = preview.allowed_policy_access.filter(a => a.target_kind === kind);
        if (!options.length) return null;
        const selected = access.find(a => a.target_kind === kind);
        return <div key={kind}><label><input type="checkbox" checked={!!selected} onChange={e => changeAccess(kind, {}, e.target.checked)} />{kindNames[kind]}</label>
          {selected && <>
            <label>作用范围 <select aria-label={`${kindNames[kind]}作用范围`} value={selected.coverage} onChange={e => changeAccess(kind, { coverage: e.target.value as SetupCoverage })}>
              {options.map(a => <option key={a.coverage} value={a.coverage}>{kind === 'enterprise_default' ? '整个组织的默认策略' : a.coverage === 'exact' ? '当前项目' : kind === 'user_exception' ? '整个组织当前及未来成员' : '整个组织当前及未来项目'}</option>)}
            </select></label>
            <label><input type="checkbox" checked={selected.read} onChange={e => changeAccess(kind, { read: e.target.checked })} />读取</label>
            <label><input type="checkbox" checked={selected.write} onChange={e => changeAccess(kind, { write: e.target.checked })} />修改</label>
            {kind === 'user_exception' && selected.coverage === 'exact' && <label>当前项目成员 <select aria-label="成员正文例外成员" value={selected.user_id || ''} onChange={e=>changeAccess(kind,{user_id:e.target.value})}><option value="">请选择成员</option>{members.map(m=><option key={m.user_id} value={String(m.user_id)}>{m.username}</option>)}</select></label>}
          </>}
        </div>;
      })}
      {!enterpriseWrite && <p role="status">{issueNames.enterprise_joint_management_not_granted}</p>}
      {preview.issues.map(i => <p key={i.code} role={i.requires_choice ? 'alert' : 'status'}>{issueNames[i.code] || '当前范围需要核对，请使用维护详情处理后重新预览。'}</p>)}
      <label style={{ display: 'block', marginTop: 12 }}><input type="checkbox" checked={confirmed} onChange={e => setConfirmed(e.target.checked)} />我确认上述两端设置、供数范围及实际策略管理权限。</label>
      <button onClick={() => { void save(); }} disabled={!enabled || !confirmed || !profiles.length || preview.issues.some(i => i.requires_choice) || access.some(a => !a.read && !a.write || a.target_kind === 'user_exception' && a.coverage === 'exact' && !a.user_id)}>确认并连接</button>
    </fieldset>}
    {submitted && !status && <div><p>此请求已保留。先读回；重试使用完全相同的确认，不重复扩大授权。</p><button onClick={()=>{void xInsightSetupApi.status(orgId,submitted.invitation.installation_id).then(receiveStatus).catch(()=>setError('此请求状态尚未确认，可使用同一确认重试。'));}}>读取确认结果</button><button disabled={busy} onClick={()=>{void save();}}>重试同一确认</button></div>}
    {status && <>
      <p role="status">{stateNames[status.state]}。配置{status.configuration_ready ? '已就绪' : '尚未就绪'}。</p>
      {status.xi && <><p>已收到：{status.xi.receiving.profiles_received.map(p => profileNames[p] || '其他类别').join('、') || '尚无实际数据'}。尚未出现的事件类别不会被当作错误。</p><p>正文副本：{status.xi.receiving.content_received_at ? '已有实际副本' : '尚未收到'}；受保护读取：{status.xi.receiving.content_read_verified_at ? '已有服务端授权读取验证' : '尚无实际读取验证'}。</p></>}
      {status.issues.map(i => <p key={i.code} role={i.requires_choice ? 'alert' : 'status'}>{issueNames[i.code] || '接入条件发生变化，请核对维护详情。'}</p>)}
      <button onClick={() => { if (currentInstallation) void xInsightSetupApi.status(orgId, currentInstallation).then(receiveStatus).catch(() => setError('状态读取失败，请稍后重试。')); }}>重新读取接入状态</button>
    </>}
  </section>;
}
