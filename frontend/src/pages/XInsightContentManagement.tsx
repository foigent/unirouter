import { useCallback, useEffect, useState } from 'react';
import { Banner, Button, Descriptions, Input, Popconfirm, Select, Tag, TextArea, Toast, Typography } from '@douyinfe/semi-ui';
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
  const [retention, setRetention] = useState('');
  const [quota, setQuota] = useState('');
  const [maxSaved, setMaxSaved] = useState('');
  const [registration, setRegistration] = useState('');
  const [allowedIP, setAllowedIP] = useState('');
  const [busy, setBusy] = useState(false);

  const readPolicy = useCallback(async () => {
    const next = await contentManagementApi.getPolicy(projectId);
    setPolicy(next);
    setMode(next.desired.config.mode);
    setRetention(next.desired.config.retention_seconds?.toString() ?? '');
    setQuota(next.desired.config.quota_bytes?.toString() ?? '');
    setMaxSaved(next.desired.config.max_saved_bytes?.toString() ?? '');
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
        desired.retention_seconds = positiveInteger(retention, '保留秒数');
        desired.quota_bytes = positiveInteger(quota, '项目配额字节数');
        desired.max_saved_bytes = positiveInteger(maxSaved, '单项上限字节数');
        if (desired.max_saved_bytes > desired.quota_bytes || desired.max_saved_bytes > policy.limits.max_saved_bytes) {
          throw new Error('单项上限不得超过项目配额或部署上限');
        }
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
    <Text>正文策略按当前项目生效，与 A/B 来源事实授权及 Feed 状态分别管理。此页仅回读和显式提交；查看状态不会创建策略或启用正文。脱敏当前不受支持。</Text>
    <div className="ur-toolbar"><Text strong>项目正文策略</Text><Button disabled={busy} onClick={() => void refreshPolicy()}>刷新策略</Button></div>
    {policyError && <Banner type="warning" description={policyError} closeIcon={null} />}
    {policy && <>
      <Descriptions row data={[
        { key: '项目 / 来源', value: `${policy.project_id} / ${policy.source_instance}` },
        { key: '期望 / 生效', value: `${modeLabels[policy.desired.config.mode]} v${policy.desired.policy_version} / ${modeLabels[policy.effective.config.mode]} v${policy.effective.policy_version}` },
        { key: '应用状态', value: `${policy.application_status} · ${policy.application_reason}` },
        { key: '存储状态', value: policy.storage_status },
        { key: '部署单项上限', value: `${policy.limits.max_saved_bytes} 字节` },
        { key: '协议 / 观测点', value: `${policy.desired.config.supported_protocols.join(', ')} / ${policy.desired.config.observation_points.join(', ')}` },
      ]} />
      <div className="ur-toolbar" style={{ flexWrap: 'wrap' }}>
        <Select aria-label="正文策略模式" value={mode} optionList={Object.entries(modeLabels).map(([value, label]) => ({ value, label }))} onChange={value => setMode(value as PayloadMode)} disabled={busy} style={{ width: 150 }} />
        {mode === 'full_content' && <>
          <Input aria-label="正文保留秒数" placeholder="明确填写保留秒数" value={retention} onChange={setRetention} disabled={busy || !expansionAllowed} style={{ width: 175 }} />
          <Input aria-label="正文项目配额字节数" placeholder="明确填写项目配额" value={quota} onChange={setQuota} disabled={busy || !expansionAllowed} style={{ width: 185 }} />
          <Input aria-label="正文单项上限字节数" placeholder="明确填写单项上限" value={maxSaved} onChange={setMaxSaved} disabled={busy || !expansionAllowed} style={{ width: 185 }} />
          <Tag>用途：human_review；达限：记录 capture gap</Tag>
        </>}
        <Popconfirm title={mode === 'full_content' ? '确认按填写的期限和配额提交完整正文策略？是否生效以服务端回读为准。' : '确认更新当前项目的正文策略？'} onConfirm={() => void savePolicy()}>
          <Button type="primary" disabled={busy || !expansionAllowed && mode !== 'disabled'}>保存项目策略</Button>
        </Popconfirm>
      </div>
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
