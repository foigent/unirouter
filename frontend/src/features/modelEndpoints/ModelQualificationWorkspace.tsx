import '../../i18n/models';
import { translate as t, translateLabel, useI18n } from '../../i18n';
import { useEffect, useRef, useState } from 'react';
import { Banner, Button, Checkbox, Input, Select, Space, Table, Typography } from '@douyinfe/semi-ui';
import { modelDeploymentApi, type CapabilityProbePlan, type CanonicalCapabilityQualification, type CanonicalModelApplication, type CanonicalOperation, type SharedProbeObservation } from '../../api/modelDeployments';
import { ApiError } from '../../api/transport';
import { useAuthStore } from '../../store/auth';
import { Surface } from '../../ui';
import { capabilityShortName } from './ModelCapabilityQualificationPanel';
import { endpointFailureText, EndpointUIError, type EndpointFailure } from './presentation';

type Props = { selected: CanonicalModelApplication; operation: CanonicalOperation; canManage: boolean; canApprove: boolean; gatewayLicensed: boolean };
type Run = { state: 'sending' | 'complete' | 'unconfirmed'; sourceRef?: string; observation?: SharedProbeObservation; message?: string };
function approvable(v: CanonicalCapabilityQualification) { return v.latest_probe?.current && v.latest_probe.success && v.latest_probe.approval_id === 0; }
function actor() { const s = useAuthStore.getState(), u = s.user; return s.isAuthenticated && u ? JSON.stringify([u.id, u.org_id, u.customer_context, u.organization_role, u.role]) : ''; }
function safeSourceRef(e: unknown) {
  if (!(e instanceof ApiError) || !e.payload || typeof e.payload !== 'object') return undefined;
  const nested = (e.payload as { error?: { source_ref?: unknown } }).error;
  return typeof nested?.source_ref === 'string' && /^op_[0-9a-f]{32}$/.test(nested.source_ref) ? nested.source_ref : undefined;
}
export default function ModelQualificationWorkspace({ selected, operation, canManage, canApprove, gatewayLicensed }: Props) {
  useI18n();
  const user = useAuthStore(s => s.user);
  const bindings = selected.credential.bindings.filter(b => b.eligible);
  const [bindingID, setBindingID] = useState(bindings.length === 1 ? bindings[0].id : 0);
  const [views, setViews] = useState<CanonicalCapabilityQualification[]>([]);
  const [plan, setPlan] = useState<CapabilityProbePlan | null>(null);
  const [runs, setRuns] = useState<Record<string, Run>>({});
  const [checked, setChecked] = useState<Record<string, number>>({});
  const [approvalUnconfirmed, setApprovalUnconfirmed] = useState(false);
  const currentViews = useRef<CanonicalCapabilityQualification[]>([]);
  const [reason, setReason] = useState(t("已审阅当前模型的逐项验证事实"));
  const [costAccepted, setCostAccepted] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState<EndpointFailure | null>(null);
  const generation = useRef(0), inFlight = useRef(false), mounted = useRef(true);
  const target = JSON.stringify([selected.deployment.id, selected.deployment.config_revision, selected.connection.id, selected.connection.config_revision,
    operation.operation_id, operation.revision, operation.profile_definition_digest, operation.protocol_profile_id, operation.protocol_profile_version,
    operation.capability_probe_contracts, selected.credential.bindings.map(b => [b.id, b.state_revision, b.eligible])]);
  const load = async (stamp: number, who: string) => {
    const rows = await Promise.all(operation.capability_probe_contracts.map(c => modelDeploymentApi.getCapabilityQualification(selected.deployment.id, operation.operation_id, c.capability_id)));
    if (mounted.current && stamp === generation.current && who === actor()) {
      currentViews.current = rows; setViews(rows);
      setChecked(old => Object.fromEntries(Object.entries(old).filter(([id, probe]) => rows.some(v => v.capability_id === id && v.latest_probe?.capability_probe_id === probe && approvable(v)))));
    }
  };
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; generation.current++; }; }, []);
  useEffect(() => {
    const stamp = ++generation.current, who = actor();
    currentViews.current = []; setViews([]); setPlan(null); setChecked({}); setApprovalUnconfirmed(false); setRuns({}); setError(null); setCostAccepted(false);
    void load(stamp, who).catch(e => { if (stamp === generation.current) setError({ cause: e, fallback: '验证状态读取失败' }); });
    if (canManage && bindingID && bindings.some(b => b.id === bindingID)) {
      void modelDeploymentApi.getCapabilityProbePlan(selected.deployment.id, operation.operation_id, bindingID).then(p => {
        if (mounted.current && stamp === generation.current && who === actor()) setPlan(p);
      }).catch(e => { if (stamp === generation.current) setError({ cause: e, fallback: '验证计划读取失败' }); });
    }
    return () => { generation.current++; };
  }, [target, bindingID, user, canManage, canApprove, gatewayLicensed]);
  const valid = (stamp: number, who: string) => mounted.current && stamp === generation.current && who === actor();
  const refresh = async () => {
    if (inFlight.current) return;
    const stamp = generation.current, who = actor(); setError(null); inFlight.current = true; setBusy(true);
    try { await load(stamp, who); if (valid(stamp, who)) setApprovalUnconfirmed(false); if (canManage && bindingID) { const p = await modelDeploymentApi.getCapabilityProbePlan(selected.deployment.id, operation.operation_id, bindingID); if (valid(stamp, who)) { setPlan(p); setCostAccepted(false); } } } catch (e) { if (valid(stamp, who)) setError({ cause: e, fallback: '读取失败' }); }
    finally { inFlight.current = false; if (mounted.current) setBusy(false); }
  };
  const verify = async () => {
    if (inFlight.current || !plan || !costAccepted || !canManage || !gatewayLicensed || !actor() || Object.values(runs).some(r => r.state === 'unconfirmed')) return;
    const stamp = generation.current, who = actor(), frozen = plan;
    inFlight.current = true; setBusy(true); setError(null);
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
      if (result.observation.source_ref !== sourceRef || result.observation.plan_digest !== digest) throw new EndpointUIError('观察身份不匹配');
      setRuns(old => ({ ...old, [digest]: { state: 'complete', sourceRef, observation: result.observation, message: '已回读完整观察；当前资格以逐项状态为准' } }));
      await load(stamp, who);
    } catch (e) { if (valid(stamp, who)) setError({ cause: e, fallback: '没有完整观察记录', prefix: '结果仍未确认：{error}。不会重新调用模型。' }); }
  };
  const approve = async () => {
    if (inFlight.current || !canApprove || !gatewayLicensed || !reason.trim() || !actor() || approvalUnconfirmed) return;
    const stamp = generation.current, who = actor();
    const items = Object.entries(checked), frozenReason = reason.trim();
    inFlight.current = true; setBusy(true); setError(null);
    try {
      for (const [id, probeID] of items) {
        if (!valid(stamp, who)) break;
        if (!currentViews.current.some(v => v.capability_id === id && v.latest_probe?.capability_probe_id === probeID && approvable(v))) {
          setError({ fallback: '所选验证事实已变化，请刷新后重新选择。' }); break;
        }
        const receipt = await modelDeploymentApi.approveCapability(selected.deployment.id, operation.operation_id, id, { capability_probe_id: probeID, reason: frozenReason });
        if (!valid(stamp, who)) break;
        if (!receipt || !Number.isSafeInteger(receipt.approval_id) || receipt.approval_id <= 0 ||
            receipt.deployment_id !== selected.deployment.id || receipt.operation_id !== operation.operation_id ||
            receipt.capability_id !== id || receipt.capability_probe_id !== probeID ||
            receipt.qualified !== true || receipt.qualification_state !== 'qualified' ||
            receipt.evidence_ref !== `capability_approval:${receipt.approval_id}`) {
          throw new EndpointUIError('批准回执不完整、身份不匹配或资格未确认');
        }
        setChecked(old => Object.fromEntries(Object.entries(old).filter(([c]) => c !== id))); await load(stamp, who);
      }
    } catch (e) { if (valid(stamp, who)) { setApprovalUnconfirmed(true); setChecked({}); setError({ cause: e, fallback: '批准失败', prefix: '批准未全部确认，请先刷新逐项状态：{error}' }); } }
    finally { inFlight.current = false; if (mounted.current) setBusy(false); }
  };
  const unresolved = Object.values(runs).some(r => r.state === 'unconfirmed');
  const count = plan?.groups.reduce((n, g) => n + g.planned_provider_requests, 0) || 0;
  return <Surface title={t("3 · 集中验证")} description={t("一次选择凭证，集中查看逐项事实和批准状态。验证成功后，再到发布工作台启用。")}>
    <Space vertical align="start" style={{ width: '100%' }}>
      {canManage && <><span id="qualification-binding-label">{t("验证使用的凭证")}</span><Select aria-labelledby="qualification-binding-label" style={{ minWidth: 260 }} value={bindingID || undefined} disabled={busy} placeholder={t("选择已保存的凭证")} optionList={bindings.map(b => ({ value: b.id, label: t('凭证 #{id} · {status}', { id: b.id, status: b.status }) }))} onChange={v => setBindingID(Number(v))}/></>}
      {error && <Banner type="warning" closeIcon={null} description={endpointFailureText(error)}/>}
      <Table rowKey="capability_id" dataSource={views} pagination={false} style={{ width: '100%' }} rowSelection={canApprove ? { selectedRowKeys: Object.keys(checked), onChange: ids => setChecked(Object.fromEntries(views.filter(v => ids?.includes(v.capability_id) && approvable(v)).map(v => [v.capability_id, v.latest_probe!.capability_probe_id]))), getCheckboxProps: (v: CanonicalCapabilityQualification) => ({ disabled: busy || approvalUnconfirmed || !gatewayLicensed || !approvable(v) }) } : undefined} columns={[
        { title: t("能力"), dataIndex: 'capability_id', render: (id: string) => capabilityShortName(id) },
        { title: t("最新验证"), render: (_: unknown, v: CanonicalCapabilityQualification) => v.latest_probe ? <>
          <div>{!v.latest_probe.current ? t("证据已失效") : !v.latest_probe.success ? t("验证未通过") : v.latest_probe.approval_id ? t("验证通过 · 此记录已批准") : v.latest_probe.approval_id === 0 ? t("验证通过 · 待批准") : t("验证通过 · 批准状态待回读")}</div>
          <Typography.Text type="tertiary">Probe #{v.latest_probe.capability_probe_id} · HTTP {v.latest_probe.status_code || t("未收到响应")}{v.latest_probe.approval_id ? ` · Approval #${v.latest_probe.approval_id}` : ''}</Typography.Text>
        </> : t("待验证") },
        { title: t("有效批准"), render: (_: unknown, v: CanonicalCapabilityQualification) => v.qualified ? `Approval #${v.preferred_approval_id} · Probe #${v.preferred_capability_probe_id}` : t("暂无有效批准") },
      ]}/>
      <Typography.Text type="tertiary">{t("新验证和批准不会自动更新活动 Release。逐项批准所选事实后，可到发布工作台核对并启用新的发布。")}</Typography.Text>
      {plan && <>
        <Typography.Text>{t('本轮合并验证预计 {calls} 次上游请求，覆盖 {capabilities} 项能力；费用按供应商实际处理计入。', { calls: count, capabilities: plan.groups.flatMap(g => g.capability_ids).length })}</Typography.Text>
        {plan.groups.map(g => { const run = runs[g.plan_digest]; return <div key={g.plan_digest}>
          <Typography.Text>{g.capability_ids.map(capabilityShortName).join('、')}  {t("· 1 次请求")}</Typography.Text>
          {run && <p>{run.state === 'sending' ? t("正在验证") : translateLabel(run.message || '')} {run.sourceRef && <code>{run.sourceRef}</code>} {run.observation && ` · ${run.observation.dispatch_state === 'not_started' ? t("未开始发送") : run.observation.dispatch_state === 'response_received' ? t("已收到供应商响应") : t("已开始发送，结果可能不明")}`}</p>}
          {run?.state === 'unconfirmed' && run.sourceRef && <Button disabled={busy} onClick={() => void readGroup(g.plan_digest, run.sourceRef!)}>{t("回读本次观察")}</Button>}
        </div>; })}
        {plan.separate_capabilities.length > 0 && <Typography.Text type="tertiary">{plan.separate_capabilities.map(capabilityShortName).join('、')}  {t("使用独立验证合同，可在详细验证中处理。")}</Typography.Text>}
        {unresolved && <Banner type="warning" closeIcon={null} description={t("存在结果不明的调用；本页已停止重发。若未取得观察标识，请到审计记录核对本次操作，不能用其他最近成功代替。")}/>}
        <Checkbox checked={costAccepted} disabled={busy || unresolved || !gatewayLicensed} onChange={e => setCostAccepted(Boolean(e.target.checked))}>{t('确认发起本轮 {count} 次付费验证', { count })}</Checkbox>
        <Button type="primary" loading={busy} disabled={!count || !costAccepted || unresolved || !gatewayLicensed} onClick={() => void verify()}>{t("验证这些能力")}</Button>
      </>}
      {canApprove && <><Input aria-label={t("集中批准理由")} value={reason} onChange={setReason} disabled={busy} maxLength={512}/><Button disabled={busy || !gatewayLicensed || approvalUnconfirmed || !Object.keys(checked).length || !reason.trim()} onClick={() => void approve()}>{t('批准已选 {count} 项', { count: Object.keys(checked).length })}</Button></>}
      <Button disabled={busy} onClick={() => void refresh()}>{t("刷新逐项状态")}</Button>
    </Space>
  </Surface>;
}
