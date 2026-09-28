import { useCallback, useEffect, useState } from 'react';
import { Banner, Button, Descriptions, Input, Popconfirm, Select, TextArea, Toast, Typography } from '@douyinfe/semi-ui';
import { ApiError } from '../api/transport';
import {
  contentManagementApi, parseContentRegistration,
  type ContentAction, type ContentConnection, type ContentControlRequest,
  type ContentControlResult, type PayloadConfig, type PayloadMode, type PayloadPolicy,
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
  if (!Number.isSafeInteger(value)) throw new Error(`${label}超出安全整数范围`);
  return value;
}

function changeUnit(quantity: Quantity, unit: Unit, fallback: number, label: string): Quantity {
  if (quantity.value === '') return { value: '', unit };
  const value = quantityValue(quantity, fallback, label);
  const multiplier = unit === '天' ? DAY : unit === '小时' ? HOUR : unit === 'GiB' ? GIB : unit === 'MiB' ? MIB : 1;
  if (value % multiplier !== 0) throw new Error(`${label}不能整除为${unit}，请保留当前单位或先修改数值`);
  return { value: String(value / multiplier), unit };
}

function formatQuantity(value: number, preferred: Unit, scale: number, base: Unit): string {
  const quantity = fromStored(value, value, preferred, scale, base);
  return `${quantity.value} ${quantity.unit}`;
}

function positiveInteger(raw: string, label: string): number {
  if (!/^[1-9][0-9]*$/.test(raw)) throw new Error(`${label}必须显式填写正整数`);
  const value = Number(raw);
  if (!Number.isSafeInteger(value)) throw new Error(`${label}超出安全整数范围`);
  return value;
}

function newOperationID(): string {
  const bytes = globalThis.crypto.getRandomValues(new Uint8Array(16));
  return Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join('');
}

function confirmControlResult(action: ContentAction, request: ContentControlRequest, result: ContentControlResult, fresh: ContentConnection) {
  if (result.action !== action || result.operation_id !== request.operation_id || result.state_version !== fresh.state_version ||
    result.content_scope_revision !== fresh.content_scope_revision) {
    throw new Error('操作响应与回读状态不一致，请刷新核对');
  }
  const key = action === 'prepare' || action === 'rotate' ? fresh.staged : action === 'activate' ? fresh.active : undefined;
  if (key && (key.credential_id !== result.credential_id || key.registration_hash !== result.registration_hash)) {
    throw new Error('凭据 ID/hash 与回读状态不一致，请刷新核对');
  }
  if (action !== 'rotate' && request.credential_id &&
    (result.credential_id !== request.credential_id || result.registration_hash !== request.registration_hash)) {
    throw new Error('操作响应中的凭据引用与提交值不一致，请刷新核对');
  }
  if ((action === 'pause' || action === 'resume') && (!fresh.active ||
    fresh.active.credential_id !== request.credential_id || fresh.active.registration_hash !== request.registration_hash)) {
    throw new Error('当前凭据 ID/hash 与回读状态不一致，请刷新核对');
  }
  if ((action === 'prepare' || action === 'rotate' || action === 'activate') && !key ||
    action === 'activate' && fresh.content_status !== 'active' ||
    action === 'cancel' && !!fresh.staged ||
    action === 'pause' && fresh.content_status !== 'paused' ||
    action === 'resume' && fresh.content_status !== 'active' ||
    action === 'revoke' && (fresh.content_status !== 'revoked' || !!fresh.active || !!fresh.staged)) {
    throw new Error('操作未达到预期状态，请刷新核对');
  }
}

export default function XInsightContentManagement({ projectId, expansionAllowed }: { projectId: number; expansionAllowed: boolean }) {
  const [policy, setPolicy] = useState<PayloadPolicy | null>(null);
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

  const readPolicy = useCallback(async () => {
    const next = await contentManagementApi.getPolicy(projectId);
    setPolicy(next);
    setMode(next.desired.config.mode);
    setRetention(fromStored(next.desired.config.retention_seconds, recommended.retention, '天', DAY, '秒'));
    setQuota(fromStored(next.desired.config.quota_bytes, recommended.quota, 'GiB', GIB, '字节'));
    setMaxSaved(fromStored(next.desired.config.max_saved_bytes, recommended.maxSaved, 'MiB', MIB, '字节'));
    setPolicyError('');
    return next;
  }, [projectId]);

  const readConnection = useCallback(async () => {
    const next = await contentManagementApi.getConnection(projectId);
    setConnection(next);
    setConnectionError('');
    return next;
  }, [projectId]);

  const refreshPolicy = useCallback(async () => {
    try { await readPolicy(); }
    catch (error) { setPolicy(null); setPolicyError(error instanceof Error ? error.message : '正文策略读取失败'); }
  }, [readPolicy]);
  const refreshConnection = useCallback(async () => {
    try { await readConnection(); }
    catch (error) { setConnection(null); setConnectionError(error instanceof Error ? error.message : '正文连接读取失败'); }
  }, [readConnection]);

  useEffect(() => { void refreshPolicy(); void refreshConnection(); }, [refreshPolicy, refreshConnection]);

  const resetRecommended = () => {
    setRetention({ value: '30', unit: '天' });
    setQuota({ value: '5', unit: 'GiB' });
    setMaxSaved({ value: '32', unit: 'MiB' });
    setPolicyError('');
  };

  const selectUnit = (quantity: Quantity, unit: Unit, fallback: number, label: string, set: (value: Quantity) => void) => {
    try { set(changeUnit(quantity, unit, fallback, label)); setPolicyError(''); }
    catch (error) { setPolicyError(error instanceof Error ? error.message : '单位转换失败'); }
  };

  let savePreview = '';
  if (mode === 'full_content') {
    try {
      quantityValue(retention, recommended.retention, '保留期限');
      quantityValue(quota, recommended.quota, '项目容量');
      quantityValue(maxSaved, recommended.maxSaved, '单项上限');
      savePreview = `保存将使用：保留期限 ${retention.value || '30'} ${retention.value ? retention.unit : '天'}；项目容量 ${quota.value || '5'} ${quota.value ? quota.unit : 'GiB'}；单项上限 ${maxSaved.value || '32'} ${maxSaved.value ? maxSaved.unit : 'MiB'}`;
    } catch { savePreview = '保存前请修正无效的期限或容量'; }
  }

  const savePolicy = async () => {
    if (!policy) return;
    setBusy(true);
    setPolicyError('');
    try {
      const desired: PayloadConfig = {
        mode,
        supported_protocols: [...policy.desired.config.supported_protocols],
        observation_points: [...policy.desired.config.observation_points],
        allowed_purposes: [],
      };
      if (mode === 'full_content') {
        desired.retention_seconds = quantityValue(retention, recommended.retention, '保留期限');
        desired.quota_bytes = quantityValue(quota, recommended.quota, '项目容量');
        desired.max_saved_bytes = quantityValue(maxSaved, recommended.maxSaved, '单项上限');
        if (desired.max_saved_bytes > desired.quota_bytes) throw new Error('单项上限不得超过项目容量');
        if (desired.max_saved_bytes > policy.limits.max_saved_bytes) throw new Error('单项上限不得超过部署上限');
        desired.allowed_purposes = ['human_review'];
        desired.on_limit = 'record_capture_gap';
      }
      const updated = await contentManagementApi.putPolicy(projectId, policy.desired.policy_version, desired);
      const fresh = await readPolicy();
      if (updated.desired.policy_version === policy.desired.policy_version ||
        fresh.desired.policy_version !== updated.desired.policy_version ||
        JSON.stringify(fresh.desired.config) !== JSON.stringify(updated.desired.config)) {
        throw new Error('策略保存响应与回读不一致，请刷新核对');
      }
      Toast.success(fresh.application_status === 'applied' ? '项目策略已生效' : '项目策略已保存，尚未生效');
    } catch (error) {
      if (error instanceof ApiError && error.status === 409) await refreshPolicy();
      setPolicyError(error instanceof Error ? error.message : '正文策略保存失败');
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
          if (!allowedIP.trim()) throw new Error('请填写正文接收端的精确 IP');
          body.allowed_ip = allowedIP.trim();
        }
      }
      const key = action === 'activate' || action === 'cancel' ? connection.staged : connection.active;
      if ((action === 'rotate' || action === 'activate' || action === 'cancel') && !key) throw new Error('缺少当前凭据引用，请刷新状态');
      if (key) {
        body.credential_id = key.credential_id;
        body.registration_hash = key.registration_hash;
      }
      const result = await contentManagementApi.control(projectId, action, body);
      const fresh = await readConnection();
      confirmControlResult(action, body, result, fresh);
      if (action === 'prepare' || action === 'rotate') setRegistration('');
      Toast.success(`正文连接${{ prepare: '准备', rotate: '轮换准备', activate: '激活', pause: '暂停', resume: '恢复', cancel: '取消', revoke: '撤销' }[action]}已回读确认`);
    } catch (error) {
      if (error instanceof ApiError && error.status === 409) await refreshConnection();
      setConnectionError(error instanceof Error ? error.message : '正文连接操作失败');
    } finally { setBusy(false); }
  };

  return <section aria-label="项目正文管理" style={{ display: 'grid', gap: 14, borderTop: '1px solid var(--semi-color-border)', paddingTop: 18 }}>
    <Title heading={5} style={{ margin: 0 }}>项目正文管理</Title>
    <Text>正文策略覆盖当前项目全部新请求；修改期限不会延长已有正文。查看或刷新不会启用正文；容量满时记录采集缺口。此处设置 UniRouter 留存；X-Insight 目前从 UniRouter 读取，UniRouter 正文到期后 X-Insight 也无法打开原文，尚未独立保存。</Text>
    <div className="ur-toolbar"><Text strong>项目正文策略</Text><Button disabled={busy} onClick={() => void refreshPolicy()}>刷新策略</Button></div>
    {policyError && <Banner type="warning" description={policyError} closeIcon={null} />}
    {policy && <>
      <Descriptions row data={[
        { key: '项目 / 来源', value: `${policy.project_id} / ${policy.source_instance}` },
        { key: '期望 / 生效', value: `${modeLabels[policy.desired.config.mode]} v${policy.desired.policy_version} / ${modeLabels[policy.effective.config.mode]} v${policy.effective.policy_version}` },
        { key: '应用状态', value: `${policy.application_status} · ${policy.application_reason}` },
        { key: '存储状态', value: policy.storage_status },
        { key: '正文脱敏', value: '不脱敏（当前版本尚不支持脱敏）' },
        { key: '部署单项上限', value: formatQuantity(policy.limits.max_saved_bytes, 'MiB', MIB, '字节') },
        { key: '协议 / 观测点', value: `${policy.desired.config.supported_protocols.join(', ')} / ${policy.desired.config.observation_points.join(', ')}` },
      ]} />
      <div className="ur-toolbar" style={{ flexWrap: 'wrap' }}>
        <Select aria-label="正文策略模式" value={mode} optionList={Object.entries(modeLabels).map(([value, label]) => ({ value, label }))} onChange={value => setMode(value as PayloadMode)} disabled={busy} style={{ width: 150 }} />
        {mode === 'full_content' && <>
          <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}><Text>保留期限</Text>
            <Input aria-label="正文保留期限" value={retention.value} onChange={value => setRetention({ ...retention, value })} disabled={busy || !expansionAllowed} style={{ width: 100 }} />
            <Select aria-label="正文保留期限单位" value={retention.unit} optionList={['天', '小时', '秒'].map(value => ({ value, label: value }))} onChange={value => selectUnit(retention, value as Unit, recommended.retention, '保留期限', setRetention)} disabled={busy || !expansionAllowed} style={{ width: 80 }} /></div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}><Text>项目容量</Text>
            <Input aria-label="正文项目容量" value={quota.value} onChange={value => setQuota({ ...quota, value })} disabled={busy || !expansionAllowed} style={{ width: 100 }} />
            <Select aria-label="正文项目容量单位" value={quota.unit} optionList={['GiB', 'MiB', '字节'].map(value => ({ value, label: value }))} onChange={value => selectUnit(quota, value as Unit, recommended.quota, '项目容量', setQuota)} disabled={busy || !expansionAllowed} style={{ width: 90 }} /></div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}><Text>单项上限</Text>
            <Input aria-label="正文单项上限" value={maxSaved.value} onChange={value => setMaxSaved({ ...maxSaved, value })} disabled={busy || !expansionAllowed} style={{ width: 100 }} />
            <Select aria-label="正文单项上限单位" value={maxSaved.unit} optionList={['MiB', '字节'].map(value => ({ value, label: value }))} onChange={value => selectUnit(maxSaved, value as Unit, recommended.maxSaved, '单项上限', setMaxSaved)} disabled={busy || !expansionAllowed} style={{ width: 90 }} /></div>
          <Button disabled={busy || !expansionAllowed} onClick={resetRecommended}>使用推荐设置</Button>
        </>}
        <Popconfirm title={mode === 'full_content' ? `确认提交完整正文策略？${savePreview}。是否生效以服务端回读为准。` : '确认更新当前项目的正文策略？'} onConfirm={() => void savePolicy()}>
          <Button type="primary" disabled={busy || !expansionAllowed && mode !== 'disabled'}>保存项目策略</Button>
        </Popconfirm>
      </div>
      {mode === 'full_content' && <Text>{savePreview}。留空的项目按推荐值计算。</Text>}
    </>}
    <div className="ur-toolbar"><Text strong>正文来源连接</Text><Button disabled={busy} onClick={() => void refreshConnection()}>刷新正文连接</Button></div>
    {connectionError && <Banner type="warning" description={connectionError} closeIcon={null} />}
    {connection && <>
      <Descriptions row data={[
        { key: '来源 / 项目', value: `${connection.source_instance} / ${connection.project_id}` },
        { key: 'A/B 父连接', value: connection.connection_status },
        { key: '正文连接', value: connection.content_status },
        { key: '状态版本 / 访问修订', value: `${connection.state_version} / ${connection.content_scope_revision}` },
        { key: '当前凭据 ID/hash', value: connection.active ? `${connection.active.credential_id} / ${connection.active.registration_hash}` : '-' },
        { key: '待激活凭据 ID/hash', value: connection.staged ? `${connection.staged.credential_id} / ${connection.staged.registration_hash}` : '-' },
        { key: 'C 读取授权', value: connection.content_access?.enabled ? '已授权' : '未授权' },
      ]} />
      <Text>接收端独立生成正文凭据。仅粘贴公开 registration，不接收明文 Bearer；C 读取授权不代表项目已捕获正文。准备和轮换不会自动修改项目正文策略。</Text>
      {(connection.content_status === 'not_configured' || connection.content_status === 'revoked' || connection.active && !connection.staged) && <TextArea aria-label="正文凭据注册信息" value={registration} onChange={setRegistration} disabled={busy || !expansionAllowed} placeholder="接收端生成的正文 registration JSON" rows={5} />}
      {!connection.active && !connection.staged && <Input aria-label="正文接收端精确 IP" value={allowedIP} onChange={setAllowedIP} disabled={busy || !expansionAllowed} placeholder="精确 /32 或 /128 接收端 IP" />}
      <div className="ur-toolbar" style={{ flexWrap: 'wrap' }}>
        {!connection.active && !connection.staged && <Button disabled={busy || !expansionAllowed} onClick={() => void control('prepare')}>准备正文连接</Button>}
        {connection.active && !connection.staged && connection.content_status === 'paused' && <Button disabled={busy || !expansionAllowed} onClick={() => void control('rotate')}>准备正文凭据轮换</Button>}
        {connection.staged && <>
          <Popconfirm title="确认接收端已安装该 staged 凭据？激活不修改项目正文捕获策略。" onConfirm={() => void control('activate')}><Button type="primary" disabled={busy || !expansionAllowed}>激活正文凭据</Button></Popconfirm>
          <Button disabled={busy} onClick={() => void control('cancel')}>取消待激活凭据</Button>
        </>}
        {connection.content_status === 'active' && <Button disabled={busy} onClick={() => void control('pause')}>暂停正文连接</Button>}
        {connection.content_status === 'paused' && connection.active && <Button disabled={busy || !expansionAllowed} onClick={() => void control('resume')}>恢复正文连接</Button>}
        {(connection.active || connection.staged) && <Popconfirm title="撤销正文凭据不可逆，请确认接收端已停止使用。" onConfirm={() => void control('revoke')}><Button type="danger" disabled={busy}>撤销正文连接</Button></Popconfirm>}
      </div>
    </>}
  </section>;
}
