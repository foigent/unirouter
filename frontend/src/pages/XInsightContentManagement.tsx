import '../i18n/integrations';
import { translate as t, useI18n, labelFor, translateLabel } from '../i18n';
import { useCallback, useEffect, useState } from 'react';
import { Banner, Button, Descriptions, Input, Popconfirm, Select, TextArea, Toast, Typography } from '@douyinfe/semi-ui';
import { ApiError } from '../api/transport';
import { api } from '../api/client';
import {
  contentManagementApi, parseContentRegistration,
  type ContentAction, type ContentConnection, type ContentControlRequest,
  type ContentControlResult, type PayloadConfig, type PayloadMode, type PayloadScope,
  type ExecutionStorageMode, type ScopedPayloadPolicy, type ScopedPolicyLayer,
} from '../api/contentManagement';

const { Text, Title } = Typography;
const modeLabels: Record<PayloadMode, string> = {
  disabled: '关闭', metadata_only: '仅元数据', full_content: '完整正文',
};
const DAY = 24 * 60 * 60;
const HOUR = 60 * 60;
const MIB = 1024 * 1024;
const GIB = 1024 * MIB;
const recommended = { retention: 30 * DAY, quota: 5 * GIB, maxSaved: 32 * MIB };
type Unit = '天' | '小时' | '秒' | 'GiB' | 'MiB' | '字节';
type Quantity = { value: string; unit: Unit };

function fromStored(value: number | undefined, fallback: number, preferred: Unit, scale: number, base: Unit): Quantity {
  const actual = value ?? fallback;
  return actual % scale === 0
    ? { value: String(actual / scale), unit: preferred }
    : { value: String(actual), unit: base };
}

function quantityValue(quantity: Quantity, fallback: number, label: string): number {
  if (quantity.value === '') return fallback;
  const multiplier = quantity.unit === '天' ? DAY : quantity.unit === '小时' ? HOUR : quantity.unit === 'GiB' ? GIB : quantity.unit === 'MiB' ? MIB : 1;
  const value = positiveInteger(quantity.value, label) * multiplier;
  if (!Number.isSafeInteger(value)) throw new Error(t("{v0}超出安全整数范围", { v0: String(label) }));
  return value;
}

function changeUnit(quantity: Quantity, unit: Unit, fallback: number, label: string): Quantity {
  if (quantity.value === '') return { value: '', unit };
  const value = quantityValue(quantity, fallback, label);
  const multiplier = unit === '天' ? DAY : unit === '小时' ? HOUR : unit === 'GiB' ? GIB : unit === 'MiB' ? MIB : 1;
  if (value % multiplier !== 0) throw new Error(t("{v0}不能整除为{v1}，请保留当前单位或先修改数值", { v0: String(label), v1: translateLabel(unit) }));
  return { value: String(value / multiplier), unit };
}

function formatQuantity(value: number, preferred: Unit, scale: number, base: Unit): string {
  const quantity = fromStored(value, value, preferred, scale, base);
  return `${quantity.value} ${translateLabel(quantity.unit)}`;
}

function positiveInteger(raw: string, label: string): number {
  if (!/^[1-9][0-9]*$/.test(raw)) throw new Error(t("{v0}必须显式填写正整数", { v0: String(label) }));
  const value = Number(raw);
  if (!Number.isSafeInteger(value)) throw new Error(t("{v0}超出安全整数范围", { v0: String(label) }));
  return value;
}

function newOperationID(): string {
  const bytes = globalThis.crypto.getRandomValues(new Uint8Array(16));
  return Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join('');
}

function confirmControlResult(action: ContentAction, request: ContentControlRequest, result: ContentControlResult, fresh: ContentConnection) {
  if (result.action !== action || result.operation_id !== request.operation_id || result.state_version !== fresh.state_version ||
    result.content_scope_revision !== fresh.content_scope_revision) {
    throw new Error(t("操作响应与回读状态不一致，请刷新核对"));
  }
  const key = action === 'prepare' || action === 'rotate' ? fresh.staged : action === 'activate' ? fresh.active : undefined;
  if (key && (key.credential_id !== result.credential_id || key.registration_hash !== result.registration_hash)) {
    throw new Error(t("凭据 ID/hash 与回读状态不一致，请刷新核对"));
  }
  if (action !== 'rotate' && request.credential_id &&
    (result.credential_id !== request.credential_id || result.registration_hash !== request.registration_hash)) {
    throw new Error(t("操作响应中的凭据引用与提交值不一致，请刷新核对"));
  }
  if ((action === 'pause' || action === 'resume') && (!fresh.active ||
    fresh.active.credential_id !== request.credential_id || fresh.active.registration_hash !== request.registration_hash)) {
    throw new Error(t("当前凭据 ID/hash 与回读状态不一致，请刷新核对"));
  }
  if ((action === 'prepare' || action === 'rotate' || action === 'activate') && !key ||
    action === 'activate' && fresh.content_status !== 'active' ||
    action === 'cancel' && !!fresh.staged ||
    action === 'pause' && fresh.content_status !== 'paused' ||
    action === 'resume' && fresh.content_status !== 'active' ||
    action === 'revoke' && (fresh.content_status !== 'revoked' || !!fresh.active || !!fresh.staged)) {
    throw new Error(t("操作未达到预期状态，请刷新核对"));
  }
}

export default function XInsightContentManagement({ projectId, expansionAllowed }: { projectId: number; expansionAllowed: boolean }) {
  useI18n();
  const [policy, setPolicy] = useState<ScopedPayloadPolicy | null>(null);
  const [scope, setScope] = useState<PayloadScope>('project');
  const [userID, setUserID] = useState('');
  const [memberQuery, setMemberQuery] = useState('');
  const [memberOptions, setMemberOptions] = useState<Array<{ value: string; label: string }>>([]);
  const [memberError, setMemberError] = useState('');
  const [orgID, setOrgID] = useState<number>();
  const [executionMode, setExecutionMode] = useState<ExecutionStorageMode>('temporary_allowed');
  const [baseConfig, setBaseConfig] = useState<PayloadConfig | null>(null);
  const [connection, setConnection] = useState<ContentConnection | null>(null);
  const [policyError, setPolicyError] = useState('');
  const [connectionError, setConnectionError] = useState('');
  const [mode, setMode] = useState<PayloadMode>('disabled');
  const [retention, setRetention] = useState<Quantity>({ value: '30', unit: '天' });
  const [quota, setQuota] = useState<Quantity>({ value: '5', unit: 'GiB' });
  const [maxSaved, setMaxSaved] = useState<Quantity>({ value: '32', unit: 'MiB' });
  const [registration, setRegistration] = useState('');
  const [allowedIP, setAllowedIP] = useState('');
  const [busy, setBusy] = useState(false);
  const recommendedMaxSaved = Math.min(recommended.maxSaved, policy?.limits.max_saved_bytes ?? recommended.maxSaved);

  const readMembers = useCallback(async (query = '') => {
    try {
      const response = await api.getProjectMembers(projectId, 1, 100, query) as { data?: Array<{ user_id: number; username: string; email?: string; status: string }> };
      setMemberOptions((response.data || []).filter(member => member.status === 'active').map(member => ({
        value: String(member.user_id), label: `${member.username}${member.email ? ` (${member.email})` : ''} · ${member.user_id}`,
      })));
      setMemberError('');
    } catch (error) {
      setMemberOptions([]);
      setMemberError(error instanceof Error ? error.message : "项目成员加载失败");
    }
  }, [projectId]);

  useEffect(() => { if (scope === 'user') void readMembers(); }, [scope, readMembers]);

  const readPolicy = useCallback(async () => {
    if (scope === 'user' && (!/^[1-9][0-9]*$/.test(userID) || !Number.isSafeInteger(Number(userID)))) {
      throw new Error(t("请填写当前项目中的有效用户 ID"));
    }
    const projectView = scope === 'enterprise' ? await contentManagementApi.getScopedPolicy(projectId) : null;
    const next = scope === 'enterprise'
      ? await contentManagementApi.getEnterprisePolicy(Number(projectView!.org_id))
      : await contentManagementApi.getScopedPolicy(projectId, scope === 'user' ? Number(userID) : undefined);
    setOrgID(Number(next.org_id));
    const layer = scope === 'enterprise' ? next.enterprise : scope === 'user' ? next.user : next.project;
    const capabilities = next.policy_capabilities;
    const recommendFull = scope === 'enterprise' && !layer?.desired && !layer?.effective &&
      next.full_content_enabled && next.deployment_mode !== 'disabled' && next.storage_status === 'ready' &&
      !!capabilities?.supported_protocols.length && !!capabilities.observation_points.length;
    const enterpriseDraft: PayloadConfig | undefined = scope === 'enterprise' && capabilities
      ? recommendFull
        ? { mode: 'full_content', supported_protocols: [...capabilities.supported_protocols],
          observation_points: [...capabilities.observation_points], allowed_purposes: ['human_review'],
          retention_seconds: recommended.retention, quota_bytes: recommended.quota,
          max_saved_bytes: Math.min(recommended.maxSaved, next.limits.max_saved_bytes), on_limit: 'record_capture_gap' }
        : { mode: 'disabled', supported_protocols: [...capabilities.supported_protocols],
          observation_points: [...capabilities.observation_points], allowed_purposes: [] }
      : undefined;
    const config = layer?.desired?.config ?? next.effective?.effective.config ?? next.enterprise.effective?.config ?? enterpriseDraft;
    if (!config) throw new Error(t("缺少可编辑的策略基线"));
    setPolicy(next);
    setBaseConfig(config);
    setMode(config.mode);
    setExecutionMode(layer?.desired_execution_storage_mode ?? next.effective?.execution_storage_mode ?? 'temporary_allowed');
    setRetention(fromStored(config.retention_seconds, recommended.retention, '天', DAY, '秒'));
    setQuota(fromStored(layer?.project_quota_bytes ?? layer?.desired_default_project_quota_bytes ?? next.effective?.project_quota_bytes ?? config.quota_bytes, recommended.quota, 'GiB', GIB, '字节'));
    setMaxSaved(fromStored(config.max_saved_bytes, Math.min(recommended.maxSaved, next.limits.max_saved_bytes), 'MiB', MIB, '字节'));
    setPolicyError('');
    return next;
  }, [projectId, scope, userID]);

  const readConnection = useCallback(async () => {
    const next = await contentManagementApi.getConnection(projectId);
    setConnection(next);
    setConnectionError('');
    return next;
  }, [projectId]);

  const refreshPolicy = useCallback(async () => {
    try { await readPolicy(); }
    catch (error) { setPolicy(null); setPolicyError(error instanceof Error ? error.message : "正文策略读取失败"); }
  }, [readPolicy]);
  const refreshConnection = useCallback(async () => {
    try { await readConnection(); }
    catch (error) { setConnection(null); setConnectionError(error instanceof Error ? error.message : "正文连接读取失败"); }
  }, [readConnection]);

  useEffect(() => { void refreshPolicy(); void refreshConnection(); }, [refreshPolicy, refreshConnection]);

  const resetRecommended = () => {
    setRetention({ value: '30', unit: '天' });
    setQuota({ value: '5', unit: 'GiB' });
    setMaxSaved(fromStored(undefined, recommendedMaxSaved, 'MiB', MIB, '字节'));
    setPolicyError('');
  };

  const selectUnit = (quantity: Quantity, unit: Unit, fallback: number, label: string, set: (value: Quantity) => void) => {
    try { set(changeUnit(quantity, unit, fallback, label)); setPolicyError(''); }
    catch (error) { setPolicyError(error instanceof Error ? error.message : "单位转换失败"); }
  };

  let savePreview = '';
  if (mode === 'full_content') {
    try {
      quantityValue(retention, recommended.retention, t("保留期限"));
      quantityValue(quota, recommended.quota, t("项目容量"));
      quantityValue(maxSaved, recommendedMaxSaved, t("单项上限"));
      const savedPreview = maxSaved.value ? maxSaved : fromStored(undefined, recommendedMaxSaved, 'MiB', MIB, '字节');
      savePreview = t("保存将使用：保留期限 {v0} {v1}；项目容量 {v2} {v3}；单项上限 {v4} {v5}", { v0: String(retention.value || '30'), v1: String(retention.value ? retention.unit : '天'), v2: String(quota.value || '5'), v3: String(quota.value ? quota.unit : 'GiB'), v4: String(savedPreview.value), v5: translateLabel(savedPreview.unit) });
    } catch { savePreview = t("保存前请修正无效的期限或容量"); }
  }

  const savePolicy = async () => {
    if (!policy) return;
    setBusy(true);
    setPolicyError('');
    try {
      const layer: ScopedPolicyLayer | undefined = scope === 'enterprise' ? policy.enterprise : scope === 'user' ? policy.user : policy.project;
      if (!layer || !orgID) throw new Error(t("策略目标不完整，请刷新"));
      const base = layer.desired?.config ?? baseConfig;
      if (!base) throw new Error(t("策略基线不可用，请刷新"));
      if (executionMode === 'strict_no_persist' && mode === 'full_content') throw new Error(t("严格不持久化不能同时保存完整正文"));
      const desired: PayloadConfig = {
        mode,
        supported_protocols: [...base.supported_protocols],
        observation_points: [...base.observation_points],
        allowed_purposes: [],
      };
      const quotaBytes = quantityValue(quota, recommended.quota, t("项目容量"));
      if (mode === 'full_content') {
        desired.retention_seconds = quantityValue(retention, recommended.retention, t("保留期限"));
        if (scope !== 'user') desired.quota_bytes = quotaBytes;
        desired.max_saved_bytes = quantityValue(maxSaved, recommendedMaxSaved, t("单项上限"));
        if (desired.max_saved_bytes > quotaBytes) throw new Error(t("单项上限不得超过项目容量"));
        if (desired.max_saved_bytes > policy.limits.max_saved_bytes) throw new Error(t("单项上限不得超过部署上限"));
        desired.allowed_purposes = ['human_review'];
        desired.on_limit = 'record_capture_gap';
      }
      const updated = await contentManagementApi.putScopedPolicy(scope, orgID, projectId, scope === 'user' ? Number(userID) : undefined, {
        schema_version: 'unirouter.payload-policy.v2', expected_policy_ref: layer.policy_ref,
        expected_version: layer.desired?.policy_version ?? '0', expected_vector: policy.version_vector,
        desired, execution_storage_mode: executionMode,
        ...(scope !== 'user' ? { project_quota_bytes: quotaBytes } : {}),
      });
      const fresh = await readPolicy();
      const updatedLayer = scope === 'enterprise' ? updated.enterprise : scope === 'user' ? updated.user : updated.project;
      const freshLayer = scope === 'enterprise' ? fresh.enterprise : scope === 'user' ? fresh.user : fresh.project;
      if (!updatedLayer?.desired || updatedLayer.desired.policy_version === layer.desired?.policy_version ||
        freshLayer?.desired?.policy_version !== updatedLayer.desired.policy_version ||
        JSON.stringify(freshLayer.desired.config) !== JSON.stringify(updatedLayer.desired.config)) {
        throw new Error(t("策略保存响应与回读不一致，请刷新核对"));
      }
      Toast.success(freshLayer.application_status === 'applied' ? t("策略已生效") : t("策略已保存，尚未生效"));
    } catch (error) {
      if (error instanceof ApiError && error.status === 409) await refreshPolicy();
      setPolicyError(error instanceof Error ? error.message : "正文策略保存失败");
    } finally { setBusy(false); }
  };

  const control = async (action: ContentAction) => {
    if (!connection) return;
    setBusy(true);
    setConnectionError('');
    try {
      const body: ContentControlRequest = {
        operation_id: newOperationID(),
        expected_state_version: connection.state_version,
      };
      if (action === 'prepare' || action === 'rotate') {
        body.registration = parseContentRegistration(registration, connection);
        body.operation_id = body.registration.operation_id;
        if (action === 'prepare') {
          if (!allowedIP.trim()) throw new Error(t("请填写正文接收端的精确 IP"));
          body.allowed_ip = allowedIP.trim();
        }
      }
      const key = action === 'activate' || action === 'cancel' ? connection.staged : connection.active;
      if ((action === 'rotate' || action === 'activate' || action === 'cancel') && !key) throw new Error(t("缺少当前凭据引用，请刷新状态"));
      if (key) {
        body.credential_id = key.credential_id;
        body.registration_hash = key.registration_hash;
      }
      const result = await contentManagementApi.control(projectId, action, body);
      const fresh = await readConnection();
      confirmControlResult(action, body, result, fresh);
      if (action === 'prepare' || action === 'rotate') setRegistration('');
      Toast.success(t("正文连接{v0}已回读确认", { v0: String({ prepare: t("准备"), rotate: t("轮换准备"), activate: t("激活"), pause: t("暂停"), resume: t("恢复"), cancel: t("取消"), revoke: t("撤销") }[action]) }));
    } catch (error) {
      if (error instanceof ApiError && error.status === 409) await refreshConnection();
      setConnectionError(error instanceof Error ? error.message : "正文连接操作失败");
    } finally { setBusy(false); }
  };

  return <section aria-label={t("项目正文管理")} style={{ display: 'grid', gap: 14, borderTop: '1px solid var(--semi-color-border)', paddingTop: 18 }}>
    <Title heading={5} style={{ margin: 0 }}>{t("项目正文管理")}</Title>
    <Text>{t("正文策略按企业、项目、个人密钥依次选择最具体的显式配置；共享密钥不使用个人例外。完整正文共用项目容量。执行必需暂存单独受严格模式约束；已有任务与正文不会因修改期限自动延长或删除。")}</Text>
    <div className="ur-toolbar" style={{ flexWrap: 'wrap' }}>
      <Text strong>{t("正文与执行暂存策略")}</Text>
      <Select aria-label={t("策略作用域")} value={scope} optionList={[{ value: 'enterprise', label: t("企业默认") }, { value: 'project', label: t("项目覆盖") }, { value: 'user', label: t("个人例外") }]} onChange={value => { setPolicy(null); setScope(value as PayloadScope); }} disabled={busy} style={{ width: 140 }} />
      {scope === 'user' && <>
        <Input aria-label={t("搜索项目成员")} value={memberQuery} onChange={setMemberQuery} placeholder={t("用户名或邮箱")} disabled={busy} style={{ width: 170 }} />
        <Button disabled={busy} onClick={() => void readMembers(memberQuery)}>{t("搜索成员")}</Button>
        <Select aria-label={t("选择项目成员")} value={userID} optionList={memberOptions} onChange={value => { setPolicy(null); setUserID(String(value)); }} placeholder={t("选择当前项目成员")} disabled={busy} filter style={{ width: 230 }} />
      </>}
      <Button disabled={busy} onClick={() => void refreshPolicy()}>{t("刷新策略")}</Button>
    </div>
    {policyError && <Banner type="warning" description={translateLabel(policyError)} closeIcon={null} />}
    {memberError && scope === 'user' && <Banner type="warning" description={translateLabel(memberError)} closeIcon={null} />}
    {policy && <>
      {scope === 'enterprise' && !policy.enterprise.explicit && <Banner type="info" description={t("企业默认尚未设置；以下是未保存的{v0}草稿，协议与观测点来自当前来源能力。请核对模式、执行暂存及每项目配额后保存。", { v0: String(mode === 'full_content' ? t("完整正文推荐") : t("关闭模式")) })} closeIcon={null} />}
      <Descriptions row data={[
        { key: t("企业 / 项目 / 用户"), value: `${policy.org_id} / ${policy.project_id || '—'} / ${policy.user_id || '—'}` },
        { key: t("企业默认"), value: policy.enterprise.effective ? `${labelFor(modeLabels, policy.enterprise.effective.config.mode)} v${policy.enterprise.effective.policy_version} · ${policy.enterprise.effective_execution_storage_mode}` : t("未显式设置") },
        { key: t("项目覆盖"), value: policy.project?.explicit && policy.project.effective ? `${labelFor(modeLabels, policy.project.effective.config.mode)} v${policy.project.effective.policy_version} · ${policy.project.effective_execution_storage_mode}` : t("继承企业默认") },
        { key: t("个人例外"), value: policy.user?.explicit && policy.user.effective ? `${labelFor(modeLabels, policy.user.effective.config.mode)} v${policy.user.effective.policy_version} · ${policy.user.effective_execution_storage_mode}` : t("无个人例外") },
        { key: t("当前解析结果"), value: policy.effective ? `${policy.effective.scope} / ${labelFor(modeLabels, policy.effective.effective.config.mode)} v${policy.effective.effective.policy_version} / ${policy.effective.execution_storage_mode}` : t("企业层设置") },
        { key: t("期望 / 生效"), value: (() => { const layer = scope === 'enterprise' ? policy.enterprise : scope === 'user' ? policy.user : policy.project; return `${layer?.desired ? `${labelFor(modeLabels, layer.desired.config.mode)} v${layer.desired.policy_version}` : t("继承")} / ${layer?.effective ? `${labelFor(modeLabels, layer.effective.config.mode)} v${layer.effective.policy_version}` : t("继承")}`; })() },
        { key: t("应用状态"), value: (() => { const layer = scope === 'enterprise' ? policy.enterprise : scope === 'user' ? policy.user : policy.project; return `${layer?.application_status || 'inherited'} · ${layer?.application_reason || 'none'}`; })() },
        { key: t("存储状态"), value: policy.storage_status },
        { key: t("正文脱敏"), value: t("不脱敏（当前版本尚不支持脱敏）") },
        { key: t("部署单项上限"), value: formatQuantity(policy.limits.max_saved_bytes, 'MiB', MIB, '字节') },
        { key: t("协议 / 观测点"), value: `${baseConfig?.supported_protocols.join(', ') || '—'} / ${baseConfig?.observation_points.join(', ') || '—'}` },
      ]} />
      <div className="ur-toolbar" style={{ flexWrap: 'wrap' }}>
        <Select aria-label={t("正文策略模式")} value={mode} optionList={Object.entries(modeLabels).map(([value, label]) => ({ value, label }))} onChange={value => setMode(value as PayloadMode)} disabled={busy} style={{ width: 150 }} />
        <Select aria-label={t("执行暂存模式")} value={executionMode} optionList={[{ value: 'temporary_allowed', label: t("允许执行必需暂存") }, { value: 'strict_no_persist', label: t("严格不持久化") }]} onChange={value => setExecutionMode(value as ExecutionStorageMode)} disabled={busy} style={{ width: 180 }} />
        {mode === 'full_content' && <>
          <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}><Text>{t("保留期限")}</Text>
            <Input aria-label={t("正文保留期限")} value={retention.value} onChange={value => setRetention({ ...retention, value })} disabled={busy || !expansionAllowed} style={{ width: 100 }} />
            <Select aria-label={t("正文保留期限单位")} value={retention.unit} optionList={['天', '小时', '秒'].map(value => ({ value, label: translateLabel(value) }))} onChange={value => selectUnit(retention, value as Unit, recommended.retention, t("保留期限"), setRetention)} disabled={busy || !expansionAllowed} style={{ width: 80 }} /></div>
          {scope !== 'user' && <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}><Text>{t("项目容量")}</Text>
            <Input aria-label={t("正文项目容量")} value={quota.value} onChange={value => setQuota({ ...quota, value })} disabled={busy || !expansionAllowed} style={{ width: 100 }} />
            <Select aria-label={t("正文项目容量单位")} value={quota.unit} optionList={['GiB', 'MiB', '字节'].map(value => ({ value, label: translateLabel(value) }))} onChange={value => selectUnit(quota, value as Unit, recommended.quota, t("项目容量"), setQuota)} disabled={busy || !expansionAllowed} style={{ width: 90 }} /></div>}
          <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}><Text>{t("单项上限")}</Text>
            <Input aria-label={t("正文单项上限")} value={maxSaved.value} onChange={value => setMaxSaved({ ...maxSaved, value })} disabled={busy || !expansionAllowed} style={{ width: 100 }} />
            <Select aria-label={t("正文单项上限单位")} value={maxSaved.unit} optionList={['MiB', '字节'].map(value => ({ value, label: translateLabel(value) }))} onChange={value => selectUnit(maxSaved, value as Unit, recommendedMaxSaved, t("单项上限"), setMaxSaved)} disabled={busy || !expansionAllowed} style={{ width: 90 }} /></div>
          <Button disabled={busy || !expansionAllowed} onClick={resetRecommended}>{t("使用推荐设置")}</Button>
        </>}
        <Popconfirm title={mode === 'full_content' ? t("确认提交{v0}完整正文策略？{v1}。以服务端回读为准。", { v0: String(scope), v1: String(savePreview) }) : t("确认更新{v0}作用域策略？", { v0: String(scope) })} onConfirm={() => void savePolicy()}>
          <Button type="primary" disabled={busy || !expansionAllowed && mode !== 'disabled'}>{t("保存作用域策略")}</Button>
        </Popconfirm>
      </div>
      {mode === 'full_content' && <Text>{savePreview}{t("。留空的项目按推荐值计算。")}</Text>}
    </>}
    <div className="ur-toolbar"><Text strong>{t("正文来源连接")}</Text><Button disabled={busy} onClick={() => void refreshConnection()}>{t("刷新正文连接")}</Button></div>
    {connectionError && <Banner type="warning" description={translateLabel(connectionError)} closeIcon={null} />}
    {connection && <>
      <Descriptions row data={[
        { key: t("来源 / 项目"), value: `${connection.source_instance} / ${connection.project_id}` },
        { key: t("A/B 父连接"), value: connection.connection_status },
        { key: t("正文连接"), value: connection.content_status },
        { key: t("状态版本 / 访问修订"), value: `${connection.state_version} / ${connection.content_scope_revision}` },
        { key: t("当前凭据 ID/hash"), value: connection.active ? `${connection.active.credential_id} / ${connection.active.registration_hash}` : '-' },
        { key: t("待激活凭据 ID/hash"), value: connection.staged ? `${connection.staged.credential_id} / ${connection.staged.registration_hash}` : '-' },
        { key: t("C 读取授权"), value: connection.content_access?.enabled ? t("已授权") : t("未授权") },
      ]} />
      <Text>{t("接收端独立生成正文凭据。仅粘贴公开 registration，不接收明文 Bearer；C 读取授权不代表项目已捕获正文。准备和轮换不会自动修改项目正文策略。")}</Text>
      {(connection.content_status === 'not_configured' || connection.content_status === 'revoked' || connection.active && !connection.staged) && <TextArea aria-label={t("正文凭据注册信息")} value={registration} onChange={setRegistration} disabled={busy || !expansionAllowed} placeholder={t("接收端生成的正文 registration JSON")} rows={5} />}
      {!connection.active && !connection.staged && <Input aria-label={t("正文接收端精确 IP")} value={allowedIP} onChange={setAllowedIP} disabled={busy || !expansionAllowed} placeholder={t("精确 /32 或 /128 接收端 IP")} />}
      <div className="ur-toolbar" style={{ flexWrap: 'wrap' }}>
        {!connection.active && !connection.staged && <Button disabled={busy || !expansionAllowed} onClick={() => void control('prepare')}>{t("准备正文连接")}</Button>}
        {connection.active && !connection.staged && connection.content_status === 'paused' && <Button disabled={busy || !expansionAllowed} onClick={() => void control('rotate')}>{t("准备正文凭据轮换")}</Button>}
        {connection.staged && <>
          <Popconfirm title={t("确认接收端已安装该 staged 凭据？激活不修改项目正文捕获策略。")} onConfirm={() => void control('activate')}><Button type="primary" disabled={busy || !expansionAllowed}>{t("激活正文凭据")}</Button></Popconfirm>
          <Button disabled={busy} onClick={() => void control('cancel')}>{t("取消待激活凭据")}</Button>
        </>}
        {connection.content_status === 'active' && <Button disabled={busy} onClick={() => void control('pause')}>{t("暂停正文连接")}</Button>}
        {connection.content_status === 'paused' && connection.active && <Button disabled={busy || !expansionAllowed} onClick={() => void control('resume')}>{t("恢复正文连接")}</Button>}
        {(connection.active || connection.staged) && <Popconfirm title={t("撤销正文凭据不可逆，请确认接收端已停止使用。")} onConfirm={() => void control('revoke')}><Button type="danger" disabled={busy}>{t("撤销正文连接")}</Button></Popconfirm>}
      </div>
    </>}
  </section>;
}
