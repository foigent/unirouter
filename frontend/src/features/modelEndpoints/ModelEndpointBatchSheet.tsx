import { useEffect, useMemo, useRef, useState } from 'react';
import { Banner, Button, Input, Select, SideSheet, Space, Table, Typography } from '@douyinfe/semi-ui';
import { modelDeploymentApi, type CanonicalModelApplication, type CanonicalModelApplicationCreateRequest, type ModelDiscoveryResult } from '../../api/modelDeployments';
import { loadOnboardingProfiles, modelExecutionProfileApi, type ProfileCatalogSource, type ModelExecutionProfile } from '../../api/modelExecutionProfiles';
import { useAuthStore } from '../../store/auth';
import { providerPresets, providerPresetAdapterConfigMatches } from '../../utils/providerEndpoints';
import { reviewedModelOnboardingLimits, reviewedModelOptions, type ReviewedModelOnboardingOption } from '../../utils/modelOnboarding';
import { prepareModelBatch, runModelBatch, type ModelBatch } from '../../utils/modelOnboardingBatch';
import { providerPresetBaseURLError } from './ModelEndpointCreateSheet';
type Props = {
  applications: CanonicalModelApplication[];
  initialConnectionID?: number;
  initialBindingID?: number;
  onClose: () => void;
  onChanged: () => Promise<void>;
};
type Row = {
  id: string;
  name: string;
  option?: ReviewedModelOnboardingOption;
  choices: ReviewedModelOnboardingOption[];
  configured: boolean;
};
function actorIdentity() {
  const s = useAuthStore.getState(), u = s.user;
  return s.isAuthenticated && u ? JSON.stringify([u.id, u.customer_context, u.org_id, u.organization_role, u.role]) : '';
}
export default function ModelEndpointBatchSheet({ applications, initialConnectionID = 0, initialBindingID = 0, onClose, onChanged }: Props) {
  const user = useAuthStore(s => s.user);
  const [connectionID, setConnectionID] = useState(initialConnectionID), [bindingID, setBindingID] = useState(initialBindingID);
  const [catalogSource, setCatalogSource] = useState<ProfileCatalogSource>('compiled');
  const [profileChoices, setProfileChoices] = useState<Record<string, string>>({});
  const [catalogLoading, setCatalogLoading] = useState(true);
  const [profiles, setProfiles] = useState<ModelExecutionProfile[]>([]);
  const [discovery, setDiscovery] = useState<ModelDiscoveryResult | null>(null);
  const discoveryQuery = useRef('');
  const [query, setQuery] = useState(''), [selected, setSelected] = useState<string[]>([]);
  const [names, setNames] = useState<Record<string, string>>({}), [modelIDs, setModelIDs] = useState<Record<string, number>>({});
  const [busy, setBusy] = useState(false), [error, setError] = useState('');
  const [, update] = useState(0);
  const lastActor = useRef(actorIdentity());
  const generation = useRef(0), batch = useRef<ModelBatch | null>(null), inFlight = useRef(false), mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; generation.current++; batch.current = null; };
  }, []);
  useEffect(() => {
    let current = true;
    setProfiles([]); setCatalogLoading(true);
    void loadOnboardingProfiles(catalogSource, modelExecutionProfileApi).then(r => {
      if (current) setProfiles(r.data);
    }).catch(() => { if (current) setError('模型合同目录读取失败，请重新选择来源或关闭后重试。'); })
      .finally(() => { if (current) setCatalogLoading(false); });
    return () => { current = false; };
  }, [catalogSource, user]);
  useEffect(() => {
    const actor = actorIdentity();
    if (actor === lastActor.current)
      return;
    lastActor.current = actor;
    generation.current++;
    batch.current = null;
    setSelected([]);
    setDiscovery(null);
    setBindingID(0);
    setConnectionID(0);
  }, [user]);
  const connections = useMemo(() => Array.from(new Map(applications.filter(a => !a.connection.retired_at).map(a => [a.connection.id, a.connection])).values()), [applications]);
  const connection = connections.find(c => c.id === connectionID);
  const bindings = Array.from(new Map(applications.filter(a => a.connection.id === connectionID).flatMap(a => a.credential.bindings).map(b => [b.id, b])).values());
  const binding = bindings.find(b => b.id === bindingID);
  const anchor = applications.find(a => a.connection.id === connectionID && !a.deployment.retired_at && a.deployment.admin_state === 'enabled' && a.credential.bindings.some(b => b.id === bindingID && b.eligible));
  const preset = providerPresets.find(p => p.provider === connection?.provider && p.adapterType === connection?.adapter_type && providerPresetAdapterConfigMatches(p, connection?.adapter_config)
    && p.apiSurface === 'openai_chat_completions' && !providerPresetBaseURLError(p.value, 'openai.chat_completions.v1', connection?.base_url));
  const options = preset ? reviewedModelOptions(profiles, preset) : [];
  const configured = (id: string) => applications.some(a => a.connection.id === connectionID && a.deployment.upstream_model === id && !a.deployment.retired_at && a.operations.some(o => o.operation_id === 'openai.chat_completions.v1'));
  const available = discovery ? discovery.models.map(m => ({ id: m.id, name: m.name || m.id }))
    : Array.from(new Map(options.map(o => [o.recommendedUpstreamModelID, { id: o.recommendedUpstreamModelID, name: o.profile.label }])).values());
  const rows: Row[] = available.map(m => {
    const choices = options.filter(o => o.upstreamModelIDs.includes(m.id));
    const option = profileChoices[m.id] ? choices.find(o => o.key === profileChoices[m.id])
      : catalogSource === 'compiled' && choices.length === 1 ? choices[0] : undefined;
    return { ...m, choices, option, configured: configured(m.id) };
  });
  const search = query.trim().toLocaleLowerCase();
  const visibleRows = discovery || !search ? rows : rows.filter(r => `${r.id} ${r.name}`.toLocaleLowerCase().includes(search));
  const previewErrors = new Map<string, string>();
  const newNames = new Map<string, string>();
  for (const id of selected) {
    const row = rows.find(r => r.id === id);
    if (!row?.option || row.configured) {
      previewErrors.set(id, '该模型已配置或合同已变化，请重新选择');
      continue;
    }
    if (modelIDs[id])
      continue;
    const name = names[id] ?? row.option.recommendedPublicModelID;
    if (!/^[a-z0-9][a-z0-9._-]{0,99}$/.test(name)) {
      previewErrors.set(id, '请填写有效的公共模型 ID');
    }
    else if (applications.some(a => a.public_model.model_name === name)) {
      previewErrors.set(id, '模型名已存在；请选择兼容的现有模型，或使用其他名称');
    }
    else if (newNames.has(name)) {
      previewErrors.set(id, '所选模型的公共名称重复');
      previewErrors.set(newNames.get(name)!, '所选模型的公共名称重复');
    }
    newNames.set(name, id);
  }
  const canDiscover = preset?.endpointClass === 'dashscope_cn_beijing_workspace_payg' && !!anchor && binding?.eligible;
  const locked = busy || catalogLoading || !!batch.current;
  const discover = async (page: number) => {
    if (!canDiscover || !connection || !binding || !anchor || inFlight.current)
      return;
    const revision = ++generation.current, actor = actorIdentity();
    const requestedQuery = page === 1 ? query : discoveryQuery.current;
    setBusy(true);
    setError('');
    setSelected([]);
    setDiscovery(null);
    try {
      const r = await modelDeploymentApi.discover(anchor.deployment.id, { credential_binding_id: binding.id, connection_revision: connection.config_revision, credential_revision: binding.state_revision, page, query: requestedQuery });
      if (revision === generation.current && actorIdentity() === actor) {
        discoveryQuery.current = requestedQuery;
        setDiscovery(r);
      }
    }
    catch {
      if (revision === generation.current)
        setError('未能读取该业务空间的模型列表。请检查连接、凭证和地域；现有配置未变化。');
    }
    finally {
      if (mounted.current)
        setBusy(false);
    }
  };
  const submit = async () => {
    if (inFlight.current || !connection || !binding?.eligible || !actorIdentity())
      return;
    if (!batch.current && previewErrors.size) {
      setError('请先处理预览中的名称或合同冲突。');
      return;
    }
    setError('');
    try {
      if (!batch.current)
        batch.current = prepareModelBatch(actorIdentity(), selected.map(id => {
          const row = rows.find(r => r.id === id);
          if (!row?.option || row.configured)
            throw new Error('模型选择已变化，请刷新后核对。');
          const o = row.option, limits = reviewedModelOnboardingLimits(o.profile), publicModelID = modelIDs[id] || 0, name = names[id] ?? o.recommendedPublicModelID;
          if (!publicModelID && !/^[a-z0-9][a-z0-9._-]{0,99}$/.test(name))
            throw new Error('请填写有效的公共模型 ID。');
          const payload: CanonicalModelApplicationCreateRequest = {
            connection: { id: connection.id }, credential: { binding_id: binding.id },
            public_model: publicModelID ? { id: publicModelID } : { model_name: name, display_name: row.name, provider_family: o.profile.provider, lifecycle: 'staging', execution_profile_id: o.profile.id, execution_profile_version: o.profile.version, context_length: limits.publicContextLength, output_limit: limits.publicOutputLimit },
            deployment: { ...(o.profile.profile_definition_digest ? { profile_definition_digest: o.profile.profile_definition_digest } : {}), upstream_model: id, priority: 100, routing_weight: 100, max_context: limits.deploymentMaxContext, default_max_output: limits.deploymentDefaultMaxOutput },
            operation: { operation_id: 'openai.chat_completions.v1' },
          };
          return { model: id, payload };
        }), Date.now());
      const intent = batch.current;
      inFlight.current = true;
      setBusy(true);
      await runModelBatch(intent, { actor: () => mounted.current ? actorIdentity() : '', now: Date.now, create: modelDeploymentApi.create, changed: () => { if (mounted.current)
          update(n => n + 1); } });
      if (mounted.current)
        await onChanged();
    }
    catch (e) {
      if (mounted.current)
        setError(e instanceof Error ? e.message : '创建未完成');
    }
    finally {
      inFlight.current = false;
      if (mounted.current)
        setBusy(false);
    }
  };
  const close = () => { if (busy)
    return; generation.current++; batch.current = null; onClose(); };
  const compatibleModels = (r: Row) => Array.from(new Map(applications.filter(a => a.public_model.lifecycle !== 'retired' && a.public_model.execution_profile_id === r.option?.profile.id && a.public_model.execution_profile_version === r.option?.profile.version).map(a => [a.public_model.id, a.public_model])).values());
  return <SideSheet title="批量添加模型" visible onCancel={close} width="min(920px, 100vw)">
  <Banner type="info" closeIcon={null} description="选择已保存的连接和凭证，一次勾选多个模型。创建仅保存配置；验证与发布分别进行。首次创建后的连接已保存，可继续添加模型；关闭后可进入模型详情完成验证和发布。"/>
  <span id="batch-profile-source-label">批量模型合同来源</span>
  <Select aria-labelledby="batch-profile-source-label" value={catalogSource} disabled={busy || !!batch.current}
    optionList={[{ value: 'compiled', label: '内置模型合同' }, { value: 'published', label: '已发布模型合同' }]}
    onChange={value => { setCatalogSource(value as ProfileCatalogSource); setSelected([]); setProfileChoices({}); setError(''); }} />
  <Space vertical align="start" style={{ width: '100%', margin: '16px 0' }}>
      <span id="batch-connection-label">批量现有连接</span>
      <Select aria-labelledby="batch-connection-label" filter placeholder="选择平台连接 / 业务空间" disabled={locked} value={connectionID || undefined} style={{ width: 550 }} optionList={connections.map(c => ({ value: c.id, label: `${c.name} · ${c.provider} · #${c.id}`, disabled: c.admin_state !== 'enabled' }))} onChange={v => { generation.current++; setConnectionID(Number(v)); setBindingID(0); setDiscovery(null); setSelected([]); setError(''); }}/>
      {connection && <Typography.Text type="tertiary">{connection.base_url}</Typography.Text>}
      <span id="batch-binding-label">批量现有凭证</span>
      <Select aria-labelledby="batch-binding-label" placeholder="选择要复用的凭证" disabled={locked} value={bindingID || undefined} style={{ width: 350 }} optionList={bindings.map(b => ({ value: b.id, label: `凭证 #${b.id} · ${b.status}`, disabled: !b.eligible }))} onChange={v => { generation.current++; setBindingID(Number(v)); setDiscovery(null); setSelected([]); }}/>
      <Space><Input aria-label="搜索供应商模型" placeholder="搜索模型名称或 ID" value={query} onChange={setQuery} disabled={locked}/><Button disabled={!canDiscover || locked} onClick={() => void discover(1)}>读取平台模型</Button></Space>
      <Typography.Text type="tertiary">{discovery ? `平台目录 · ${discovery.total} 个 · ${discovery.observed_at}。目录可见不代表该 Key 有执行权限或已通过验证。` : '下方为已建模的本地合同。北京业务空间连接支持在线读取平台目录；其他入口可直接使用本地合同选择。'}</Typography.Text>
  </Space>
  {error && <Banner type="warning" closeIcon={null} description={error}/>}
  <Table rowKey="id" dataSource={visibleRows} pagination={false} rowSelection={{ selectedRowKeys: selected, onChange: keys => setSelected((keys || []).map(String)), getCheckboxProps: (row: Row) => ({ disabled: locked || !row.option || row.configured }) }} columns={[
      { title: '模型', dataIndex: 'name', render: (_: unknown, r: Row) => <><strong>{r.name}</strong><br /><Typography.Text type="tertiary">{r.id}</Typography.Text></> },
      { title: '合同版本', render: (_: unknown, r: Row) => r.choices.length ? <><span id={`profile-version-${encodeURIComponent(r.id)}`} hidden>合同版本 {r.id}</span><Select aria-labelledby={`profile-version-${encodeURIComponent(r.id)}`} disabled={locked || r.configured} value={r.option?.key} placeholder="选择精确合同版本" optionList={r.choices.map(o => ({ value: o.key, label: `${o.profile.version}${o.profile.profile_definition_digest ? ` · ${o.profile.profile_definition_digest.slice(0, 12)}` : ''}` }))} onChange={value => { setProfileChoices(old => ({ ...old, [r.id]: String(value) })); setSelected(old => old.filter(id => id !== r.id)); setModelIDs(old => ({ ...old, [r.id]: 0 })); }} /></> : '—' },
      { title: '接入状态', render: (_: unknown, r: Row) => r.configured ? '已配置' : previewErrors.get(r.id) || (r.option ? '合同已建模 · 待验证' : r.choices.length ? '请选择合同版本' : '待支持 · 不可直接配置') },
      { title: '公共模型', render: (_: unknown, r: Row) => r.option && !r.configured && selected.includes(r.id) ? <Space vertical align="start">
    <span id={`batch-public-${r.id}`}>公共模型选择 {r.id}</span>
    <Select aria-labelledby={`batch-public-${r.id}`} value={modelIDs[r.id] || 0} disabled={locked} style={{ width: 250 }} optionList={[{ value: 0, label: '新建稳定模型名' }, ...compatibleModels(r).map(m => ({ value: m.id, label: `${m.model_name} · #${m.id}` }))]} onChange={v => setModelIDs(old => ({ ...old, [r.id]: Number(v) }))}/>
    {!modelIDs[r.id] && <Input aria-label={`公共模型 ID ${r.id}`} disabled={locked} value={names[r.id] ?? r.option.recommendedPublicModelID} onChange={v => setNames(old => ({ ...old, [r.id]: v }))}/>}
      </Space> : '—' },
    ]}/>
  {discovery && <Space style={{ marginTop: 12 }}><Button disabled={locked || discovery.page <= 1} onClick={() => void discover(discovery.page - 1)}>上一页</Button><Typography.Text>第 {discovery.page} 页</Typography.Text><Button disabled={locked || discovery.page * discovery.page_size >= discovery.total} onClick={() => void discover(discovery.page + 1)}>下一页</Button></Space>}
  {batch.current && <div aria-label="批量创建结果" style={{ marginTop: 16 }}>{batch.current.items.map(i => <p key={i.idempotencyKey}><strong>{i.model}</strong> · {i.state === 'succeeded' ? `已保存 #${i.result?.application.deployment.id}` : i.state === 'sending' ? '正在提交' : i.state === 'unconfirmed' ? i.error : '尚未提交'}</p>)}</div>}
  {!batch.current && selected.length > 0 && <Typography.Paragraph style={{ marginTop: 16 }}>
      已选 {selected.length} 个模型：复用 {selected.filter(id => !!modelIDs[id]).length} 个公共模型，新建 {selected.filter(id => !modelIDs[id]).length} 个名称。
      {previewErrors.size > 0 ? ` ${previewErrors.size} 项需处理。` : ' 仅保存配置，不会调用模型或切换流量。'}
  </Typography.Paragraph>}
  <Space style={{ marginTop: 20 }}><Button type="primary" loading={busy} disabled={catalogLoading || !binding?.eligible || selected.length === 0 || (!batch.current && previewErrors.size > 0) || batch.current?.items.every(i => i.state === 'succeeded')} onClick={() => void submit()}>{batch.current ? '重试未确认项并继续' : `保存 ${selected.length} 个模型`}</Button><Button disabled={busy} onClick={close}>关闭</Button></Space>
  </SideSheet>;
}
