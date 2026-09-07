import { useEffect, useRef, useState } from 'react';
import { Banner, Button, Checkbox, Input, Select, Space, Table, Typography } from '@douyinfe/semi-ui';
import { modelDeploymentApi, type CapabilityProbePlan, type CanonicalCapabilityQualification, type CanonicalModelApplication, type CanonicalOperation, type SharedProbeObservation } from '../../api/modelDeployments';
import { ApiError } from '../../api/transport';
import { useAuthStore } from '../../store/auth';
import { Surface } from '../../ui';
import { capabilityShortName } from './ModelCapabilityQualificationPanel';
import { endpointError } from './presentation';

type Props = { selected: CanonicalModelApplication; operation: CanonicalOperation; canManage: boolean; canApprove: boolean; gatewayLicensed: boolean };
type Run = { state: 'sending' | 'complete' | 'unconfirmed'; sourceRef?: string; observation?: SharedProbeObservation; message?: string };
function actor() { const s = useAuthStore.getState(), u = s.user; return s.isAuthenticated && u ? JSON.stringify([u.id, u.org_id, u.customer_context, u.organization_role, u.role]) : ''; }
function safeSourceRef(e: unknown) {
  if (!(e instanceof ApiError) || !e.payload || typeof e.payload !== 'object') return undefined;
  const nested = (e.payload as { error?: { source_ref?: unknown } }).error;
  return typeof nested?.source_ref === 'string' && /^op_[0-9a-f]{32}$/.test(nested.source_ref) ? nested.source_ref : undefined;
}
export default function ModelQualificationWorkspace({ selected, operation, canManage, canApprove, gatewayLicensed }: Props) {
  const user = useAuthStore(s => s.user);
  const bindings = selected.credential.bindings.filter(b => b.eligible);
  const [bindingID, setBindingID] = useState(bindings.length === 1 ? bindings[0].id : 0);
  const [views, setViews] = useState<CanonicalCapabilityQualification[]>([]);
  const [plan, setPlan] = useState<CapabilityProbePlan | null>(null);
  const [runs, setRuns] = useState<Record<string, Run>>({});
  const [checked, setChecked] = useState<string[]>([]);
  const [reason, setReason] = useState('已审阅当前模型的逐项验证事实');
  const [costAccepted, setCostAccepted] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState('');
  const generation = useRef(0), inFlight = useRef(false), mounted = useRef(true);
  const target = JSON.stringify([selected.deployment.id, selected.deployment.config_revision, selected.connection.id, selected.connection.config_revision,
    operation.operation_id, operation.revision, operation.profile_definition_digest, operation.protocol_profile_id, operation.protocol_profile_version,
    operation.capability_probe_contracts, selected.credential.bindings.map(b => [b.id, b.state_revision, b.eligible])]);
  const load = async (stamp: number, who: string) => {
    const rows = await Promise.all(operation.capability_probe_contracts.map(c => modelDeploymentApi.getCapabilityQualification(selected.deployment.id, operation.operation_id, c.capability_id)));
    if (mounted.current && stamp === generation.current && who === actor()) setViews(rows);
  };
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; generation.current++; }; }, []);
  useEffect(() => {
    const stamp = ++generation.current, who = actor();
    setViews([]); setPlan(null); setChecked([]); setRuns({}); setError(''); setCostAccepted(false);
    void load(stamp, who).catch(e => { if (stamp === generation.current) setError(endpointError(e, '验证状态读取失败')); });
    if (canManage && bindingID && bindings.some(b => b.id === bindingID)) {
      void modelDeploymentApi.getCapabilityProbePlan(selected.deployment.id, operation.operation_id, bindingID).then(p => {
        if (mounted.current && stamp === generation.current && who === actor()) setPlan(p);
      }).catch(e => { if (stamp === generation.current) setError(endpointError(e, '验证计划读取失败')); });
    }
    return () => { generation.current++; };
  }, [target, bindingID, user, canManage]);
  const valid = (stamp: number, who: string) => mounted.current && stamp === generation.current && who === actor();
  const refresh = async () => {
    if (inFlight.current) return;
    const stamp = generation.current, who = actor(); setError('');
    try { await load(stamp, who); if (canManage && bindingID) { const p = await modelDeploymentApi.getCapabilityProbePlan(selected.deployment.id, operation.operation_id, bindingID); if (valid(stamp, who)) { setPlan(p); setCostAccepted(false); } } } catch (e) { if (valid(stamp, who)) setError(endpointError(e, '读取失败')); }
  };
  const verify = async () => {
    if (inFlight.current || !plan || !costAccepted || !canManage || !gatewayLicensed || !actor() || Object.values(runs).some(r => r.state === 'unconfirmed')) return;
    const stamp = generation.current, who = actor(), frozen = plan;
    inFlight.current = true; setBusy(true); setError('');
    try {
      for (const g of frozen.groups) {
        if (!valid(stamp, who)) break;
        setRuns(old => ({ ...old, [g.plan_digest]: { state: 'sending' } }));
        try {
          const result = await modelDeploymentApi.probeCapabilityGroup(selected.deployment.id, operation.operation_id, { credential_binding_id: frozen.credential_binding_id, plan_digest: g.plan_digest, capability_ids: g.capability_ids });
          if (!valid(stamp, who)) break;
          setRuns(old => ({ ...old, [g.plan_digest]: { state: 'complete', sourceRef: result.observation.source_ref, observation: result.observation, message: result.results.every(r => r.success) ? '已取得逐项验证事实' : '有未通过或已失效的事实' } }));
          await load(stamp, who);
          if (!result.results.every(r => r.success)) break;
        } catch (e) {
          if (valid(stamp, who)) {
            const ref = safeSourceRef(e);
            const rejected = e instanceof ApiError && [400, 403, 404, 409, 422].includes(e.status) && !ref;
            setRuns(old => ({ ...old, [g.plan_digest]: { state: rejected ? 'complete' : 'unconfirmed', sourceRef: ref, message: rejected ? '发送前检查未通过，请刷新状态和计划后重新确认。' : '结果未确认，已停止后续调用。刷新不会重新发送。' } }));
          }
          break;
        }
      }
    } finally { inFlight.current = false; if (mounted.current) { setBusy(false); setCostAccepted(false); } }
  };
  const readGroup = async (digest: string, sourceRef: string) => {
    const stamp = generation.current, who = actor();
    try {
      const result = await modelDeploymentApi.getCapabilityProbeGroup(selected.deployment.id, operation.operation_id, sourceRef);
      if (!valid(stamp, who)) return;
      if (result.observation.source_ref !== sourceRef || result.observation.plan_digest !== digest) throw new Error('观察身份不匹配');
      setRuns(old => ({ ...old, [digest]: { state: 'complete', sourceRef, observation: result.observation, message: '已回读完整观察；当前资格以逐项状态为准' } }));
      await load(stamp, who);
    } catch (e) { if (valid(stamp, who)) setError(`结果仍未确认：${endpointError(e, '没有完整观察记录')}。不会重新调用模型。`); }
  };
  const approve = async () => {
    if (inFlight.current || !canApprove || !gatewayLicensed || !reason.trim() || !actor()) return;
    const stamp = generation.current, who = actor();
    const items = views.filter(v => checked.includes(v.capability_id) && !v.qualified && v.latest_probe?.success && v.latest_probe.current);
    inFlight.current = true; setBusy(true); setError('');
    try {
      for (const v of items) {
        if (!valid(stamp, who)) break;
        await modelDeploymentApi.approveCapability(selected.deployment.id, operation.operation_id, v.capability_id, { capability_probe_id: v.latest_probe!.capability_probe_id, reason: reason.trim() });
        if (!valid(stamp, who)) break;
        setChecked(old => old.filter(c => c !== v.capability_id)); await load(stamp, who);
      }
    } catch (e) { if (valid(stamp, who)) setError(`批准未全部确认，请先刷新逐项状态：${endpointError(e, '批准失败')}`); }
    finally { inFlight.current = false; if (mounted.current) setBusy(false); }
  };
  const unresolved = Object.values(runs).some(r => r.state === 'unconfirmed');
  const count = plan?.groups.reduce((n, g) => n + g.planned_provider_requests, 0) || 0;
  return <Surface title="3 · 集中验证" description="一次选择凭证，集中查看逐项事实和批准状态。验证成功后，再到发布工作台启用。">
    <Space vertical align="start" style={{ width: '100%' }}>
      {canManage && <><span id="qualification-binding-label">验证使用的凭证</span><Select aria-labelledby="qualification-binding-label" style={{ minWidth: 260 }} value={bindingID || undefined} disabled={busy} placeholder="选择已保存的凭证" optionList={bindings.map(b => ({ value: b.id, label: `凭证 #${b.id} · ${b.status}` }))} onChange={v => setBindingID(Number(v))}/></>}
      {error && <Banner type="warning" closeIcon={null} description={error}/>}
      <Table rowKey="capability_id" dataSource={views} pagination={false} style={{ width: '100%' }} rowSelection={canApprove ? { selectedRowKeys: checked, onChange: ids => setChecked((ids || []).map(String)), getCheckboxProps: (v: CanonicalCapabilityQualification) => ({ disabled: busy || v.qualified || !v.latest_probe?.success || !v.latest_probe.current }) } : undefined} columns={[
        { title: '能力', dataIndex: 'capability_id', render: (id: string) => capabilityShortName(id) },
        { title: '当前状态', render: (_: unknown, v: CanonicalCapabilityQualification) => v.qualified ? '已批准' : v.latest_probe?.current && v.latest_probe.success ? '验证通过 · 待批准' : v.latest_probe ? (v.latest_probe.current ? '验证未通过' : '证据已失效') : '待验证' },
        { title: '观察记录', render: (_: unknown, v: CanonicalCapabilityQualification) => v.latest_probe ? `#${v.latest_probe.capability_probe_id} · HTTP ${v.latest_probe.status_code || '未收到响应'}` : '—' },
      ]}/>
      {plan && <>
        <Typography.Text>本轮合并验证预计 {count} 次上游请求，覆盖 {plan.groups.flatMap(g => g.capability_ids).length} 项能力；费用按供应商实际处理计入。</Typography.Text>
        {plan.groups.map(g => { const run = runs[g.plan_digest]; return <div key={g.plan_digest}>
          <Typography.Text>{g.capability_ids.map(capabilityShortName).join('、')} · 1 次请求</Typography.Text>
          {run && <p>{run.state === 'sending' ? '正在验证' : run.message} {run.sourceRef && <code>{run.sourceRef}</code>} {run.observation && ` · ${run.observation.dispatch_state === 'not_started' ? '未开始发送' : run.observation.dispatch_state === 'response_received' ? '已收到供应商响应' : '已开始发送，结果可能不明'}`}</p>}
          {run?.state === 'unconfirmed' && run.sourceRef && <Button disabled={busy} onClick={() => void readGroup(g.plan_digest, run.sourceRef!)}>回读本次观察</Button>}
        </div>; })}
        {plan.separate_capabilities.length > 0 && <Typography.Text type="tertiary">{plan.separate_capabilities.map(capabilityShortName).join('、')} 使用独立验证合同，可在详细验证中处理。</Typography.Text>}
        {unresolved && <Banner type="warning" closeIcon={null} description="存在结果不明的调用；本页已停止重发。若未取得观察标识，请到审计记录核对本次操作，不能用其他最近成功代替。"/>}
        <Checkbox checked={costAccepted} disabled={busy || unresolved || !gatewayLicensed} onChange={e => setCostAccepted(Boolean(e.target.checked))}>确认发起本轮 {count} 次付费验证</Checkbox>
        <Button type="primary" loading={busy} disabled={!count || !costAccepted || unresolved || !gatewayLicensed} onClick={() => void verify()}>验证这些能力</Button>
      </>}
      {canApprove && <><Input aria-label="集中批准理由" value={reason} onChange={setReason} disabled={busy} maxLength={512}/><Button disabled={busy || !gatewayLicensed || !checked.length || !reason.trim()} onClick={() => void approve()}>批准已选 {checked.length} 项</Button></>}
      <Button disabled={busy} onClick={() => void refresh()}>刷新逐项状态</Button>
    </Space>
  </Surface>;
}
