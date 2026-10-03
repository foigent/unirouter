import { useCallback, useEffect, useRef, useState } from 'react';
import { Banner, Button, Descriptions, Input, Popconfirm, Select, Toast, Typography } from '@douyinfe/semi-ui';
import { api } from '../api/client';
import { sourceConnectionsApi, type SourceConnection } from '../api/sourceConnections';
import {
  xInsightMachineApi, type BindExistingCandidate, type MachineCredential, type DeliveredMachineCandidate, type MachinePurpose,
  type PolicyDelegation, type SourceEnrollment,
} from '../api/xInsightMachine';

const { Text, Title } = Typography;
const profileNames: Record<string, string> = {
  'unirouter.execution-context.v1': '调用事实',
  'unirouter.execution-identity.v1': '账号归属',
  'unirouter.cost-basis.v1': '费用依据',
  'unirouter.runtime-observation.v1': '运行观测',
  'unirouter.attempt-observation.v1': '尝试观测',
  'unirouter.capability-snapshot.v1': '能力快照',
  'unirouter.policy-observation.v1': '策略观测',
  'unirouter.destination-observation.v1': '目标观测',
  'unirouter.budget-definition.v1': '预算定义',
  'unirouter.budget-period-summary.v1': '预算周期汇总',
  'unirouter.budget-alert.v1': '预算告警',
};
const policyKindNames: Record<PolicyDelegation['target_kind'], string> = {
  enterprise_default: '企业默认策略',
  project_policy: '项目策略',
  user_exception: '成员策略例外',
};
const coverageName = (coverage: PolicyDelegation['coverage'] | SourceEnrollment['coverage']) =>
  coverage === 'all_current_future' ? '当前及未来项目' : '指定项目';
const authorizationStateName = (state: PolicyDelegation['state'] | SourceEnrollment['state']) =>
  state === 'active' ? '已授权' : '已撤销';
const credentialPurposeName = (purpose: MachinePurpose) =>
  purpose === 'xinsight.policy.manage' ? '策略管理' : '数据接入';
const credentialStateName = (status: string) => {
  if (status === 'feed_staged') return '待激活';
  if (status === 'feed_active') return '已激活';
  if (status === 'feed_revoked') return '已撤销';
  return '状态待核查';
};

export default function XInsightMachineDelegationManagement({
  orgId, projectId, installationId, deploymentId, sourceInstance, orgAdmin, deliveredCandidates = [], onRefreshCandidates,
}: {
  orgId: number; projectId: number; installationId: string; deploymentId: string;
  sourceInstance: string; orgAdmin: boolean; deliveredCandidates?: DeliveredMachineCandidate[]; onRefreshCandidates?: () => Promise<void>;
}) {
  const [grants, setGrants] = useState<PolicyDelegation[]>([]);
  const [enrollments, setEnrollments] = useState<SourceEnrollment[]>([]);
  const [credentials, setCredentials] = useState<MachineCredential[]>([]);
  const [bindCandidates, setBindCandidates] = useState<Record<string, BindExistingCandidate[]>>({});
  const [kind, setKind] = useState<PolicyDelegation['target_kind']>('project_policy');
  const [coverage, setCoverage] = useState<PolicyDelegation['coverage']>(orgAdmin ? 'all_current_future' : 'exact');
  const [action, setAction] = useState<'read' | 'write' | 'read_write'>('read_write');
  const [userId, setUserId] = useState('');
  const [memberQuery, setMemberQuery] = useState('');
  const [members, setMembers] = useState<Array<{ value: string; label: string }>>([]);
  const [enrollmentCoverage, setEnrollmentCoverage] = useState<SourceEnrollment['coverage']>(orgAdmin ? 'all_current_future' : 'exact');
  const [facts, setFacts] = useState<string[]>([]);
  const [sourceConnection, setSourceConnection] = useState<SourceConnection>();
  const [sourceError, setSourceError] = useState('');
  const factsEdited = useRef(false);
  const [content, setContent] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const refresh = useCallback(async () => {
    const projectFilter = orgAdmin ? undefined : projectId;
    const [policyResult, sourceResult, credentialResult] = await Promise.all([
      xInsightMachineApi.listPolicyDelegations(orgId, installationId, projectFilter),
      xInsightMachineApi.listSourceEnrollments(orgId, installationId, projectFilter),
      orgAdmin ? xInsightMachineApi.listCredentials(orgId, installationId) : Promise.resolve({ data: [] }),
    ]);
    setGrants(policyResult.data);
    setEnrollments(sourceResult.data);
    setCredentials(credentialResult.data);
    setError('');
  }, [orgId, projectId, installationId, orgAdmin]);

  useEffect(() => { void refresh().catch(e => setError(e instanceof Error ? e.message : '授权状态读取失败')); }, [refresh]);

  useEffect(() => {
    let cancelled = false;
    factsEdited.current = false;
    setSourceConnection(undefined);
    setSourceError('');
    setFacts([]);
    void sourceConnectionsApi.get(projectId).then(connection => {
      if (cancelled) return;
      if (connection.source_instance !== sourceInstance) throw new Error('供数来源与当前安装不匹配');
      setSourceConnection(connection);
      if (!factsEdited.current) {
        // Keep an existing exact selection visible, including disabled items.
        // Never silently narrow it or add newly published profiles on refresh.
        setFacts(connection.profiles?.length ? [...connection.profiles]
          : [...(connection.default_profiles ?? [])]);
      }
    }).catch(e => {
      if (!cancelled) setSourceError(e instanceof Error ? e.message : '供数字段读取失败');
    });
    return () => { cancelled = true; };
  }, [orgId, projectId, installationId, sourceInstance]);

  const availableProfiles: readonly string[] = sourceConnection?.available_profiles ?? [];
  const profileOptions = [...new Set([...availableProfiles, ...facts])].sort();

  const readMembers = async () => {
    try {
      const result = await api.getProjectMembers(projectId, 1, 100, memberQuery) as {
        data?: Array<{ user_id: number; username: string; status: string }>;
      };
      setMembers((result.data || []).filter(item => item.status === 'active')
        .map(item => ({ value: String(item.user_id), label: item.username })));
      setError('');
    } catch (e) { setError(e instanceof Error ? e.message : '项目成员读取失败'); }
  };

  const run = async (operation: () => Promise<unknown>, success: string) => {
    setBusy(true);
    setError('');
    try {
      await operation();
      await refresh();
      Toast.success(success);
    } catch (e) {
      setError(e instanceof Error ? e.message : '授权操作失败');
    } finally { setBusy(false); }
  };

  const createPolicy = () => run(() => {
    if (kind === 'user_exception' && !/^[1-9][0-9]*$/.test(userId) && coverage === 'exact') {
      throw new Error('请选择当前项目的有效成员');
    }
    return xInsightMachineApi.createPolicyDelegation(orgId, {
      installation_id: installationId, target_kind: kind, coverage: kind === 'enterprise_default' ? 'exact' : coverage,
      ...(kind !== 'enterprise_default' && coverage === 'exact' ? { project_id: String(projectId) } : {}),
      ...(kind === 'user_exception' && coverage === 'exact' ? { user_id: userId } : {}),
      read: action !== 'write', write: action !== 'read',
    });
  }, '策略访问已授权；仍须 X-Insight 实际接线和读取');

  const createEnrollment = () => run(() => {
    if (!sourceConnection || sourceConnection.capture_enabled === false) throw new Error('供数字段尚未读取或采集未启用');
    if (facts.some(profile => !availableProfiles.includes(profile))) throw new Error('所选字段采集未启用；请核对原字段范围后明确选择');
    if (facts.length === 0 && !content) throw new Error('请选择至少一种事实或完整正文');
    return xInsightMachineApi.createSourceEnrollment(orgId, {
      installation_id: installationId, coverage: enrollmentCoverage,
      ...(enrollmentCoverage === 'exact' ? { project_id: String(projectId) } : {}),
      profiles: [...facts].sort(), include_content: content,
    });
  }, '数据读取已授权；仍须 X-Insight 实际接线和读取');

  const registerCandidate = (candidate: DeliveredMachineCandidate) => run(async () => {
    if (candidate.delivery !== 'public_candidate_delivered' || candidate.activation !== 'not_asserted' ||
      candidate.registration.installation_id !== installationId || candidate.registration.purpose !== candidate.purpose) {
      throw new Error('公开候选与当前安装或用途不匹配；请刷新核对');
    }
    await xInsightMachineApi.registerCredential(orgId, candidate.registration);
  }, '公开候选已核对并登记；激活与授权须分别确认');
  const refreshAll = async () => {
    setBusy(true);
    try { await onRefreshCandidates?.(); await refresh(); }
    catch (e) { setError(e instanceof Error ? e.message : '候选或授权状态读取失败'); }
    finally { setBusy(false); }
  };

  const inspectExisting = async (grant: SourceEnrollment) => {
    setBusy(true);
    try {
      const result = await xInsightMachineApi.listBindExistingCandidates(orgId, grant.enrollment_id, projectId);
      setBindCandidates(previous => ({ ...previous, [grant.enrollment_id]: result.data }));
      setError(result.data.length ? '' : '当前项目没有可纳入的已启用人工连接');
    } catch (e) { setError(e instanceof Error ? e.message : '旧连接检查失败'); }
    finally { setBusy(false); }
  };

  const bindExisting = (grant: SourceEnrollment, candidate: BindExistingCandidate, credentialId: string) =>
    run(async () => {
      await xInsightMachineApi.bindExisting(orgId, candidate, credentialId, crypto.randomUUID().replace(/-/g, ''));
      setBindCandidates(previous => ({ ...previous, [grant.enrollment_id]: [] }));
    }, '旧连接已纳入授权；仍须由 X-Insight 用现有凭据完成实际读取探测');

  return <section aria-label="X-Insight 策略与数据接入授权" style={{ display: 'grid', gap: 14, borderTop: '1px solid var(--semi-color-border)', paddingTop: 18 }}>
    <Title heading={5} style={{ margin: 0 }}>X-Insight 策略与数据接入授权</Title>
    <Text>在这里决定 X-Insight 可以管理哪些策略、读取哪些数据。组织管理员默认选择当前及未来项目，并保留已有字段选择；新连接根据服务端当前资格预选事实与观测和完整正文，实际范围以所选字段为准；创建授权后，仍需 X-Insight 完成凭据配对与实际读取，不能仅凭授权认定接入成功。项目管理员只能授权当前项目。</Text>
    <details>
      <summary>查看安装与来源标识</summary>
      <Descriptions row data={[
        { key: '安装 ID', value: installationId },
        { key: 'X-Insight 部署 ID', value: deploymentId },
        { key: '来源实例', value: sourceInstance },
        { key: '当前项目 ID', value: String(projectId) },
      ]} />
    </details>
    {(error || sourceError) && <Banner type="warning" description={error || sourceError} closeIcon={null} />}
    <div className="ur-toolbar" style={{ flexWrap: 'wrap' }}>
      <Text strong>策略访问</Text>
      <Select aria-label="策略目标" value={kind} optionList={[
        ...(orgAdmin ? [{ value: 'enterprise_default', label: '企业默认策略' }] : []),
        { value: 'project_policy', label: '项目策略' }, { value: 'user_exception', label: '成员策略例外' },
      ]} onChange={value => { setKind(value as PolicyDelegation['target_kind']); setCoverage(value === 'enterprise_default' || value === 'user_exception' || !orgAdmin ? 'exact' : 'all_current_future'); }} style={{ width: 140 }} />
      {kind !== 'enterprise_default' && <Select aria-label="策略范围" value={coverage} optionList={[
        { value: 'exact', label: '当前项目' },
        ...(orgAdmin ? [{ value: 'all_current_future', label: '当前及未来项目' }] : []),
      ]} onChange={value => setCoverage(value as PolicyDelegation['coverage'])} style={{ width: 190 }} />}
      <Select aria-label="策略权限" value={action} optionList={[
        { value: 'read', label: '只读' }, { value: 'write', label: '只写' }, { value: 'read_write', label: '读写' },
      ]} onChange={value => setAction(value as typeof action)} style={{ width: 110 }} />
      {kind === 'user_exception' && coverage === 'exact' && <>
        <Input aria-label="搜索委托用户" value={memberQuery} onChange={setMemberQuery} placeholder="用户名" style={{ width: 130 }} />
        <Button onClick={() => void readMembers()} disabled={busy}>搜索成员</Button>
        <Select aria-label="选择委托用户" value={userId} optionList={members} onChange={value => setUserId(String(value))} filter style={{ width: 180 }} />
        {userId && <details><summary>查看所选成员标识</summary><Text>成员 ID：{userId}</Text></details>}
      </>}
      <Popconfirm title={coverage === 'all_current_future' && kind !== 'enterprise_default' ? '确认允许 X-Insight 访问当前及未来项目的所选策略？' : '确认授予 X-Insight 所选策略访问权限？'}
        onConfirm={() => void createPolicy()}>
        <Button type="primary" disabled={busy}>授权策略访问</Button>
      </Popconfirm>
    </div>
    {grants.length === 0 && <Text type="tertiary">尚未授权策略访问。</Text>}
    {grants.map(grant => <div key={grant.delegation_id} style={{ display: 'grid', gap: 6 }}>
      <div className="ur-toolbar" style={{ flexWrap: 'wrap' }}>
        <Text>{policyKindNames[grant.target_kind]} · {grant.target_kind === 'enterprise_default' ? '企业范围' : coverageName(grant.coverage)} · {grant.read && grant.write ? '可查看与修改' : grant.read ? '仅可查看' : '仅可修改'} · {authorizationStateName(grant.state)}</Text>
        {grant.state === 'active' && <Popconfirm title="撤销后 X-Insight 不能再使用这项策略授权；已提交的审计保留。" onConfirm={() => void run(() => xInsightMachineApi.revokePolicyDelegation(orgId, grant), '策略授权已撤销')}>
        <Button size="small" disabled={busy}>撤销</Button>
      </Popconfirm>}
      </div>
      <details>
        <summary>查看策略授权详情</summary>
        <Descriptions row data={[
          { key: '授权 ID', value: grant.delegation_id },
          { key: '目标类型', value: grant.target_kind },
          { key: '覆盖范围', value: grant.coverage },
          { key: '项目 ID', value: grant.project_id || '不适用' },
          { key: '成员 ID', value: grant.user_id || '不适用' },
          { key: '授权者 ID', value: grant.grantor_user_id },
          { key: '修订版本', value: grant.revision },
          { key: '原始状态', value: grant.state },
        ]} />
      </details>
    </div>)}
    <div className="ur-toolbar" style={{ flexWrap: 'wrap' }}>
      <Text strong>调用数据读取</Text>
      <Select aria-label="供数范围" value={enrollmentCoverage} optionList={[
        { value: 'exact', label: '当前项目' },
        ...(orgAdmin ? [{ value: 'all_current_future', label: '当前及未来项目' }] : []),
      ]} onChange={value => setEnrollmentCoverage(value as SourceEnrollment['coverage'])} style={{ width: 190 }} />
      <Select aria-label="选择调用事实" multiple value={facts} disabled={busy || !sourceConnection}
        optionList={profileOptions.map(value => ({ value, label: profileNames[value] || value, disabled: !availableProfiles.includes(value) }))}
        onChange={value => { factsEdited.current = true; setFacts(value as string[]); }} style={{ minWidth: 280 }} />
      <Select aria-label="完整正文读取" value={content ? 'yes' : 'no'} optionList={[
        { value: 'no', label: '不授权完整正文' }, { value: 'yes', label: '授权完整正文供人工核查' },
      ]} onChange={value => setContent(value === 'yes')} style={{ width: 160 }} />
      <Popconfirm title={enrollmentCoverage === 'all_current_future' ? '确认允许 X-Insight 为当前及未来项目分别注册所选数据读取凭据？已有人工连接不会自动迁移。' : '确认授权 X-Insight 读取当前项目所选数据？'}
        onConfirm={() => void createEnrollment()}>
        <Button type="primary" disabled={busy || !sourceConnection}>授权数据读取</Button>
      </Popconfirm>
    </div>
    {enrollments.length === 0 && <Text type="tertiary">尚未授权数据读取。</Text>}
    {enrollments.map(grant => <div key={grant.enrollment_id} style={{ display: 'grid', gap: 6 }}>
      <div className="ur-toolbar" style={{ flexWrap: 'wrap' }}>
        <Text>{coverageName(grant.coverage)} · {grant.profiles.map(profile => profileNames[profile] || '其他调用事实').join('、') || '未授权调用事实'} · {grant.include_content ? '完整正文供人工核查' : '不含完整正文'} · {authorizationStateName(grant.state)}</Text>
        {grant.state === 'active' && <Button size="small" disabled={busy} onClick={() => void inspectExisting(grant)}>检查已有人工连接</Button>}
        {grant.state === 'active' && <Popconfirm title="撤销后，由此授权生成的事实与正文凭据不能继续读取。" onConfirm={() => void run(() => xInsightMachineApi.revokeSourceEnrollment(orgId, grant), '数据读取授权已撤销')}>
          <Button size="small" disabled={busy}>撤销</Button>
        </Popconfirm>}
      </div>
      <details>
        <summary>查看数据授权详情</summary>
        <Descriptions row data={[
          { key: '授权 ID', value: grant.enrollment_id },
          { key: '覆盖范围', value: grant.coverage },
          { key: '项目 ID', value: grant.project_id || '全部项目' },
          { key: '事实 profile', value: grant.profiles.join(', ') || '无' },
          { key: '正文用途', value: grant.include_content ? 'human_review' : '无' },
          { key: '授权者 ID', value: grant.grantor_user_id },
          { key: '修订版本', value: grant.revision },
          { key: '原始状态', value: grant.state },
          { key: '到期时间', value: grant.expires_at || '未设置' },
        ]} />
      </details>
      {(bindCandidates[grant.enrollment_id] || []).flatMap(candidate => candidate.keys
        .filter(key => key.status === 'feed_active' && !key.policy_labels)
        .map(key => <div key={`${candidate.kind}-${key.credential_id}`} style={{ display: 'grid', gap: 6 }}>
          <div className="ur-toolbar" style={{ flexWrap: 'wrap' }}>
            <Text>已有{candidate.kind === 'facts' ? '调用事实' : '完整正文'}连接 · 可纳入当前授权</Text>
            <Popconfirm title="确认把这份已有凭据纳入当前授权？原凭据和读取游标不变；撤销授权后后续读取会失效。X-Insight 仍须实际读取验收。"
            onConfirm={() => void bindExisting(grant, candidate, key.credential_id)}>
            <Button size="small" disabled={busy}>纳入当前授权</Button>
          </Popconfirm>
          </div>
          <details>
            <summary>查看已有连接详情</summary>
            <Descriptions row data={[
              { key: '凭据 ID', value: key.credential_id },
              { key: '凭据前缀', value: key.key_prefix },
              { key: '允许 IP', value: key.allowed_ip },
              { key: '事实 profile', value: key.profiles.join(', ') || '无' },
              { key: '连接代次', value: candidate.scope_generation },
              { key: '正文状态版本', value: candidate.content_state_version || '不适用' },
              { key: '正文覆盖起点', value: key.coverage_time || '不适用' },
            ]} />
          </details>
        </div>))}
    </div>)}
    {orgAdmin && <>
      <Text strong>接入凭据</Text>
      <Text>策略管理和数据接入需要分别配对凭据。授权创建后，还需完成凭据登记与激活，再由 X-Insight 实际读取；待激活凭据不能使用。</Text>
      <Button onClick={() => void refreshAll()} disabled={busy}>刷新候选、授权与凭据</Button>
      {deliveredCandidates.length === 0 && <Text type="tertiary">等待 X-Insight 安全准备并交付两种用途的公开候选；当前没有可登记的候选。</Text>}
      {deliveredCandidates.map(candidate => {
        const otherCredential = credentials.some(key => key.purpose === candidate.purpose &&
          ['feed_active', 'feed_staged'].includes(key.status) && key.operation_id !== candidate.registration.operation_id);
        return <div key={candidate.control_operation_id} style={{ display: 'grid', gap: 6 }}>
          <Text strong>{credentialPurposeName(candidate.purpose)} · 公开候选已交付，激活尚未确认</Text>
          <Text>允许出口：{candidate.registration.allowed_ip} · 凭据前缀：{candidate.registration.key_prefix}</Text>
          {otherCredential && <Text type="warning">同用途已有其他活动或待激活凭据；本次配对不会自动轮换，请先核对现有凭据。</Text>}
          <details><summary>查看公开候选身份</summary><Descriptions row data={[
            { key: '安装', value: candidate.registration.installation_id },
            { key: '用途', value: candidate.purpose },
            { key: '登记摘要', value: candidate.registration_sha256 },
            { key: '交付操作', value: candidate.control_operation_id },
          ]} /></details>
          <Popconfirm title={`确认将已交付的${credentialPurposeName(candidate.purpose)}公开候选登记到当前安装？凭据保管在 X-Insight；此步不激活、不授予策略或数据权限。`}
            onConfirm={() => void registerCandidate(candidate)}>
            <Button disabled={busy || otherCredential}>登记{credentialPurposeName(candidate.purpose)}候选</Button>
          </Popconfirm>
        </div>;
      })}
      {credentials.length === 0 && <Text type="tertiary">尚无已登记的接入凭据。</Text>}
      {credentials.map(key => <div key={key.key_id} style={{ display: 'grid', gap: 6 }}>
        <div className="ur-toolbar" style={{ flexWrap: 'wrap' }}>
          <Text>{credentialPurposeName(key.purpose)} · {credentialStateName(key.status)}</Text>
          {key.status === 'feed_staged' && <Popconfirm title="确认 X-Insight 已安全保存这份新凭据？激活会立即撤销同用途的旧凭据。" onConfirm={() => void run(() => xInsightMachineApi.changeCredential(orgId, key.key_id, 'activate'), '接入凭据已激活')}>
          <Button size="small" disabled={busy || credentials.some(other => other.purpose === key.purpose && other.key_id !== key.key_id && other.status === 'feed_active')}>激活</Button>
        </Popconfirm>}
          {key.status !== 'feed_revoked' && <Popconfirm title="撤销后这份凭据立即失效。" onConfirm={() => void run(() => xInsightMachineApi.changeCredential(orgId, key.key_id, 'revoke'), '接入凭据已撤销')}>
          <Button size="small" type="danger" disabled={busy}>撤销</Button>
        </Popconfirm>}
        </div>
        <details>
          <summary>查看凭据详情</summary>
          <Descriptions row data={[
            { key: '凭据 ID', value: key.key_id },
            { key: '用途', value: key.purpose },
            { key: '凭据前缀', value: key.key_prefix },
            { key: '操作 ID', value: key.operation_id },
            { key: '原始状态', value: key.status },
          ]} />
        </details>
      </div>)}
    </>}
  </section>;
}
