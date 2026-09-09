import { useCallback, useEffect, useState } from 'react';
import { Banner, Button, Input, Popconfirm, Select, TextArea, Typography } from '@douyinfe/semi-ui';
import { parseSourceRegistration, sourceConnectionsApi, type SourceAction, type SourceConnection, type SourceProfile } from '../api/sourceConnections';

const labels: Record<SourceProfile, string> = {
  'unirouter.execution-context.v1': '执行上下文',
  'unirouter.execution-identity.v1': '人员与 Agent 身份（需明确授予）',
  'unirouter.cost-basis.v1': '费用依据',
};
const statusLabels = { not_configured: '未配置', paused: '已暂停', active: '读取已启用', revoked: '已撤销' };

export default function XInsightSourceConnection({ projectId, expansionAllowed: entitlementAllowed }: { projectId: number; expansionAllowed: boolean }) {
  const [status, setStatus] = useState<SourceConnection | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [registration, setRegistration] = useState('');
  const [ip, setIP] = useState('');
  const [profiles, setProfiles] = useState<SourceProfile[]>(['unirouter.execution-context.v1']);
  const refresh = useCallback(async () => {
    setBusy(true);
    try {
      const next = await sourceConnectionsApi.get(projectId);
      setStatus(next); setProfiles(next.profiles || ['unirouter.execution-context.v1']); setError('');
    } catch (e) { setStatus(null); setError(e instanceof Error ? e.message : '无法读取源连接'); }
    finally { setBusy(false); }
  }, [projectId]);
  useEffect(() => { void refresh(); }, [refresh]);
  const active = status?.credentials?.find(key => key.status === 'feed_active');
  const staged = status?.credentials?.find(key => key.status === 'feed_staged');
  const paused = status?.status === 'paused';
  const ready = !!status && status.status !== 'revoked';
  const expansionAllowed = entitlementAllowed && status?.capture_enabled !== false;
  const mutate = async (action: SourceAction) => {
    setBusy(true); setError('');
    try {
      const body = action === 'prepare' || action === 'rotate'
        ? { registration: parseSourceRegistration(registration, status!.source_instance, projectId), ...(action === 'prepare' ? { allowed_ip: ip } : {}) }
        : action === 'activate' || action === 'cancel' ? { credential_id: staged!.id }
        : action === 'profiles' ? { profiles: [...profiles].sort() } : {};
      await sourceConnectionsApi.control(projectId, action, body);
      setRegistration(''); await refresh();
    } catch (e) { setError(e instanceof Error ? e.message : '源连接操作失败'); }
    finally { setBusy(false); }
  };
  return <section aria-label="来源事实连接" style={{ display: 'grid', gap: 12, borderTop: '1px solid var(--semi-color-border)', paddingTop: 18 }}>
    <Typography.Title heading={5}>来源事实连接</Typography.Title>
    <Typography.Text>按项目授权执行上下文、身份与费用依据。首次读取从当前进度开始，保留窗口为 7 天；暂停期间继续采集。此连接单独管理。</Typography.Text>
    <div className="ur-toolbar"><Typography.Text>{status ? statusLabels[status.status] : '状态未读取'}{status && ` · 来源 ${status.source_instance}`}</Typography.Text><Button onClick={() => void refresh()} disabled={busy}>刷新来源状态</Button></div>
    {error && <Banner type="warning" description={error} closeIcon={null} />}
    {status?.capture_enabled === false && <Banner type="info" description="来源采集尚未启用。暂停、取消和撤销仍可使用。" closeIcon={null} />}
    {status?.credentials?.map(key => <Typography.Text key={key.id}>{key.status === 'feed_active' ? '当前凭据' : '待激活凭据'}：{key.key_prefix} · #{key.id}</Typography.Text>)}
    {ready && <>
      <Typography.Text>接收端生成独立凭据，仅将注册信息粘贴到此处；请勿粘贴 Bearer。注册信息必须绑定当前来源和项目，并明确列出获准字段。</Typography.Text>
      <TextArea aria-label="来源凭据注册信息" value={registration} onChange={setRegistration} disabled={busy || !expansionAllowed} placeholder="接收端的来源凭据注册 JSON" />
      {!active && !staged && <Input aria-label="来源接收端 IP" value={ip} onChange={setIP} placeholder="接收端固定 IP，例如 127.0.0.1/32" disabled={busy || !expansionAllowed} />}
      <div className="ur-toolbar">
        {!active && !staged && <Button disabled={busy || !expansionAllowed || !registration || !ip} onClick={() => void mutate('prepare')}>准备来源凭据</Button>}
        {active && paused && !staged && <Button disabled={busy || !expansionAllowed || !registration} onClick={() => void mutate('rotate')}>准备来源轮换</Button>}
        {staged && paused && <>
          <Popconfirm title="接收端已安装此凭据并验证来源和获准字段后，激活该凭据。连接仍保持暂停。" disabled={busy || !expansionAllowed} onConfirm={() => mutate('activate')}><span><Button disabled={busy || !expansionAllowed}>激活来源凭据</Button></span></Popconfirm>
          <Button disabled={busy} onClick={() => void mutate('cancel')}>取消待激活凭据</Button>
        </>}
        {status.status === 'active' && <Button disabled={busy} onClick={() => void mutate('pause')}>暂停来源读取</Button>}
        {paused && active && !staged && <Button disabled={busy || !expansionAllowed} onClick={() => void mutate('resume')}>恢复来源读取</Button>}
        {status.status !== 'not_configured' && <Popconfirm title="永久撤销该来源连接及其全部凭据？此操作不可恢复。" disabled={busy} onConfirm={() => mutate('revoke')}><span><Button type="danger" disabled={busy}>撤销来源连接</Button></span></Popconfirm>}
      </div>
      {paused && <>
        <Select multiple aria-label="来源字段权限" value={profiles} optionList={Object.entries(labels).map(([value, label]) => ({ value, label }))} onChange={value => setProfiles(value as SourceProfile[])} disabled={busy || !expansionAllowed} />
        <Popconfirm title="修改字段权限将撤销当前及待激活凭据，并使现有游标失效。需重新注册专用凭据，从新的读取进度开始。" disabled={busy || !expansionAllowed || profiles.length === 0 || JSON.stringify([...profiles].sort()) === JSON.stringify(status.profiles)} onConfirm={() => mutate('profiles')}><span><Button disabled={busy || !expansionAllowed || profiles.length === 0 || JSON.stringify([...profiles].sort()) === JSON.stringify(status.profiles)}>更新来源字段权限</Button></span></Popconfirm>
      </>}
    </>}
  </section>;
}
