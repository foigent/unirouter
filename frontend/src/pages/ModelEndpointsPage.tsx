import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  Banner,
  Button,
  Descriptions,
  Form,
  Input,
  Modal,
  SideSheet,
  Space,
  Table,
  Tabs,
  Tag,
  Toast,
  Typography,
} from '@douyinfe/semi-ui';
import { IconPlus, IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { useSearchParams } from 'react-router-dom';
import { api, ApiError } from '../api/client';
import { EmptyState, LatencyText, MetricTile, PageHeader, PasswordField, StatusTag, Surface } from '../ui';
import { formatDateTime } from '../utils/formatDate';
import { providerPreset, providerPresets } from '../utils/providerEndpoints';
import ChannelKeysPage from './ChannelKeysPage';
import ModelGovernancePage from './ModelGovernancePage';
import ModelReadinessPage from './ModelReadinessPage';
import RuntimeChangeControlWorkspace from './RuntimeChangeControlWorkspace';

const { TabPane } = Tabs;
const { Text } = Typography;

type ModelEndpointProbe = {
  success: boolean;
  current: boolean;
  status_code: number;
  latency_ms: number;
  error_summary?: string;
  tested_at: string;
};

type ModelEndpointTarget = {
  target_ref: string;
  channel_id: number;
  connection_name: string;
  provider: string;
  base_url: string;
  adapter_type: string;
  public_model: string;
  upstream_model: string;
  config_status: string;
  routing_epoch: number;
  priority: number;
  weight: number;
  circuit_state: string;
  eligible_credential_count: number;
  stable_assignment: boolean;
  canary_assignment: boolean;
  route_candidate: boolean;
  route_exclusion_reason?: string;
  state: string;
  reason_code: string;
  last_probe?: ModelEndpointProbe;
};

type ModelEndpointModel = {
  public_model: string;
  display_name: string;
  lifecycle?: string;
  catalog_approved: boolean;
  state: string;
  reason_code: string;
  route_available: boolean;
  stable_release_ref?: string;
  stable_release_version?: number;
  canary_release_ref?: string;
  canary_release_version?: number;
  canary_basis_points: number;
  release_history_count: number;
  targets: ModelEndpointTarget[];
};

type ModelEndpointProjection = {
  contract_version: string;
  generated_at: string;
  models: ModelEndpointModel[];
  summary: { total: number; route_available: number; attention: number; canary: number };
};

const stateCopy: Record<string, { label: string; help: string }> = {
  active: { label: '稳定路由', help: '当前稳定发布可选中至少一个供应商端点。项目或应用能否调用仍由高级调用诊断确认。' },
  canary: { label: '灰度中', help: '稳定路由仍在，部分流量按服务端分桶进入候选路由。' },
  degraded: { label: '路由异常', help: '保留了稳定发布记录，但网关当前没有可用候选。' },
  legacy_active: { label: '兼容路由', help: '网关当前有候选端点，但尚未完全收敛到确定性发布记录。' },
  ready: { label: '可启用', help: '凭证和最新真实检测已就绪，尚未进入路由。' },
  test_failed: { label: '检测失败', help: '最近一次真实供应商请求失败。' },
  needs_test: { label: '待检测', help: '凭证已配置，需要对这个精确端点和模型发起真实检测。' },
  needs_credential: { label: '缺少凭证', help: '没有启用、未冷却且允许该模型的供应商凭证。' },
  unconfigured: { label: '尚无端点', help: '模型目录中存在记录，但还没有供应商端点。' },
  paused: { label: '已暂停', help: '连接被紧急禁用；发布记录仍用于审计和恢复。' },
  stale: { label: '版本不一致', help: '发布固定的连接版本与当前配置不一致。' },
  governance_blocked: { label: '治理阻断', help: '供应商路由与模型目录生命周期冲突；当前模型已退役，网关调用会被拒绝。' },
};

function stateLabel(state: string) {
  return stateCopy[state]?.label || state;
}

function stateHelp(state: string) {
  return stateCopy[state]?.help || '状态由配置、凭证、真实检测、发布槽和网关候选共同计算。';
}

function endpointError(error: unknown, fallback: string) {
  if (error instanceof ApiError) {
    const payload = error.payload as { code?: string; canonical_model?: string; message?: string } | null;
    if (payload?.code === 'model_identity_conflict' && payload.canonical_model) {
      return `检测到同一公共模型已存在：${payload.canonical_model}。请选择这个现有 ID，并把当前供应商配置为它的另一个端点。`;
    }
    if (payload?.code === 'model_retired') return '该公共模型已退役；请先在高级运营中恢复其生命周期，再启用端点。';
    if (payload?.code === 'automatic_release_cleanup_failed') return payload.message || '自动发布记录未能安全终止，请先到发布记录处理。';
    return error.message;
  }
  return error instanceof Error && error.message ? error.message : fallback;
}

function exactTestSucceeded(target: ModelEndpointTarget, response: any) {
  return response?.success === true && response?.model === target.public_model &&
    String(response?.upstream_model || target.upstream_model) === target.upstream_model;
}

function providerSummary(targets: ModelEndpointTarget[]) {
  if (targets.length === 0) return '尚无供应商端点';
  return targets.map(target => `${target.provider} · ${target.connection_name}`).join('；');
}

function routeCandidateCopy(target: ModelEndpointTarget) {
  if (target.route_candidate) return '已进入候选';
  switch (target.route_exclusion_reason) {
    case 'channel_inactive':
      return '尚未发布或当前已暂停';
    case 'no_key':
    case 'no_active_key':
    case 'no_eligible_key':
      return '没有可用凭证';
    case 'key_model_not_allowed':
      return '凭证未允许该模型';
    case 'key_cooldown':
      return '凭证正在冷却';
    case 'circuit_open':
      return '端点熔断中';
    case 'channel_model_not_found':
      return '端点未配置该公共模型';
    default:
      return target.route_exclusion_reason ? '暂不可用（查看高级诊断）' : '未进入候选';
  }
}

export default function ModelEndpointsPage() {
  const [search, setSearch] = useSearchParams();
  const activeTab = ['models', 'credentials', 'advanced'].includes(search.get('tab') || '')
    ? search.get('tab')!
    : 'models';
  const advancedView = ['catalog', 'diagnostics', 'releases'].includes(search.get('view') || '')
    ? search.get('view')!
    : 'catalog';
  const [projection, setProjection] = useState<ModelEndpointProjection | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [filter, setFilter] = useState('');
  const [selectedModel, setSelectedModel] = useState('');
  const [createOpen, setCreateOpen] = useState(false);
  const [lockedPublicModel, setLockedPublicModel] = useState('');
  const [formApi, setFormApi] = useState<any>(null);
  const [selectedPreset, setSelectedPreset] = useState('deepseek');
  const [publicModelMode, setPublicModelMode] = useState<'existing' | 'new'>('new');
  const [credentialTarget, setCredentialTarget] = useState<ModelEndpointTarget | null>(null);
  const [saving, setSaving] = useState(false);
  const [workingTarget, setWorkingTarget] = useState('');
  const [batchTesting, setBatchTesting] = useState(false);
  const requestVersion = useRef(0);

  const load = useCallback(async (keepSelection = true) => {
    const version = ++requestVersion.current;
    setLoading(true);
    setLoadError('');
    try {
      const result = await api.getModelEndpoints() as ModelEndpointProjection;
      if (version !== requestVersion.current) return;
      setProjection(result);
      setSelectedModel(current => {
        if (keepSelection && result.models.some(model => model.public_model === current)) return current;
        const modelTab = !search.get('tab') || search.get('tab') === 'models';
        if (!modelTab) return '';
        const requested = search.get('model') || '';
        if (result.models.some(model => model.public_model === requested)) return requested;
        return '';
      });
    } catch (error) {
      if (version !== requestVersion.current) return;
      setProjection(null);
      setLoadError(endpointError(error, '无法读取模型、端点和当前路由的权威状态。'));
    } finally {
      if (version === requestVersion.current) setLoading(false);
    }
  }, [search]);

  useEffect(() => { void load(false); }, [load]);

  useEffect(() => {
    if (!createOpen || !formApi) return;
    formApi.reset();
    formApi.setValues({
      preset: 'deepseek',
      base_url: providerPreset('deepseek').baseURL,
      adapter_type: providerPreset('deepseek').adapterType,
      public_model: lockedPublicModel,
      upstream_model: lockedPublicModel,
      route_role: 'primary',
    });
    setPublicModelMode(lockedPublicModel ? 'existing' : 'new');
  }, [createOpen, formApi, lockedPublicModel]);

  const models = projection?.models || [];
  const filteredModels = useMemo(() => {
    const keyword = filter.trim().toLowerCase();
    if (!keyword) return models;
    return models.filter(model => [model.public_model, model.display_name, ...model.targets.flatMap(target => [target.provider, target.connection_name, target.upstream_model])]
      .some(value => String(value || '').toLowerCase().includes(keyword)));
  }, [filter, models]);
  const selected = models.find(model => model.public_model === selectedModel);
  const testableTargets = useMemo(() => models.flatMap(model => model.targets)
    .filter(target => target.eligible_credential_count > 0 && (!target.last_probe?.current || !target.last_probe.success)), [models]);

  const changeTab = (tab: string, target?: ModelEndpointTarget) => {
    const next = new URLSearchParams(search);
    next.set('tab', tab);
    if (tab !== 'advanced') next.delete('view');
    if (target) {
      next.set('model', target.public_model);
      next.set('channel_id', String(target.channel_id));
      next.set('from', 'connections');
    }
    if (tab !== 'models') {
      setSelectedModel('');
    }
    setSearch(next, { replace: true });
  };

  const changeAdvanced = (view: string, target?: ModelEndpointTarget) => {
    const next = new URLSearchParams(search);
    next.set('tab', 'advanced');
    next.set('view', view);
    if (target) {
      next.set('model', target.public_model);
      next.set('channel_id', String(target.channel_id));
      next.set('from', 'connections');
    }
    setSelectedModel('');
    setSearch(next, { replace: true });
  };

  const closeSelectedModel = () => {
    const next = new URLSearchParams(search);
    next.delete('model');
    setSelectedModel('');
    setSearch(next, { replace: true });
  };

  const selectModel = (model: string) => {
    setSelectedModel(model);
    const next = new URLSearchParams(search);
    next.set('tab', 'models');
    next.set('model', model);
    setSearch(next, { replace: true });
  };

  const testTarget = async (target: ModelEndpointTarget, notify = true) => {
    setWorkingTarget(target.target_ref);
    try {
      const response = await api.testChannelModel(target.channel_id, target.public_model) as any;
      const exact = exactTestSucceeded(target, response);
      if (notify) {
        if (exact) Toast.success(`${target.connection_name} · ${target.public_model} 检测通过`);
        else Toast.error(response?.error || '供应商响应未通过精确模型身份校验');
      }
      await load();
      return exact;
    } catch (error) {
      if (notify) Toast.error(endpointError(error, '真实检测失败'));
      await load();
      return false;
    } finally {
      setWorkingTarget('');
    }
  };

  const confirmTest = (target: ModelEndpointTarget) => Modal.confirm({
    title: `检测 ${target.connection_name}？`,
    content: <div>
      <p>将向真实供应商发起 1 次最小非流式请求，可能产生少量供应商费用。</p>
      <p>公共模型：<Text code>{target.public_model}</Text></p>
      <p>上游模型：<Text code>{target.upstream_model}</Text></p>
      <p>不会检测其他端点，也不会自动启用。</p>
    </div>,
    okText: '发起真实检测',
    onOk: () => testTarget(target),
  });

  const executeBatchTests = async () => {
    setBatchTesting(true);
    let passed = 0;
    try {
      for (const target of testableTargets) {
        if (await testTarget(target, false)) passed += 1;
      }
      Toast.info(`批量检测完成：${passed}/${testableTargets.length} 个供应商端点通过`);
    } finally {
      setBatchTesting(false);
    }
  };

  const confirmBatchTests = () => Modal.confirm({
    title: `检测 ${testableTargets.length} 个缺少有效证据的端点？`,
    content: <div>
      <p>每个“供应商端点 × 公共模型”都会单独发起一次真实请求：</p>
      <ul>{testableTargets.map(target => <li key={target.target_ref}>{target.connection_name} · {target.public_model} → {target.upstream_model}</li>)}</ul>
      <p>不会用一条 Kimi 结果代替 DeepSeek，也不会只检测列表第一项。</p>
      <p>已经处于稳定路由的端点可以继续调用；这里更新的是该精确端点的当前检测证据。</p>
    </div>,
    okText: '确认逐项检测',
    onOk: executeBatchTests,
  });

  const activate = async (target: ModelEndpointTarget, mode: 'initial' | 'canary' | 'recover') => {
    setWorkingTarget(target.target_ref);
    try {
      const request = { channel_id: target.channel_id, model: target.public_model, expected_routing_epoch: target.routing_epoch };
      if (mode === 'canary') await api.startModelEndpointCanary({ ...request, canary_basis_points: 500 });
      else if (mode === 'recover') await api.recoverModelEndpoint(request);
      else await api.activateModelEndpoint(request);
      Toast.success(mode === 'canary' ? '5% 灰度已启动；稳定路由仍保留' : mode === 'recover' ? '失效的旧稳定路由已留存为历史，新端点已成为稳定路由' : `${target.public_model} 已首次启用`);
      await load();
    } catch (error) {
      Toast.error(endpointError(error, mode === 'canary' ? '启动灰度失败' : mode === 'recover' ? '恢复路由失败' : '首次启用失败'));
    } finally {
      setWorkingTarget('');
    }
  };

  const confirmActivate = (model: ModelEndpointModel, target: ModelEndpointTarget) => {
    const mode: 'initial' | 'canary' | 'recover' = !model.stable_release_ref ? 'initial' : model.route_available ? 'canary' : 'recover';
    Modal.confirm({
      title: mode === 'canary' ? `将 ${target.connection_name} 加入 5% 灰度？` : mode === 'recover' ? `用 ${target.connection_name} 恢复路由？` : `首次启用 ${target.public_model}？`,
      content: mode === 'canary'
        ? '候选发布会继承当前稳定端点并加入这个已检测端点；稳定路由不会被直接覆盖。确认灰度结果后再提升或回滚。'
        : mode === 'recover'
          ? '当前稳定发布已经没有可用路由。系统会把旧发布保留为“已回滚”历史，再用这个已检测端点创建新的稳定路由；不会把失效路由当成仍可承载流量。'
        : '系统会自动生成发布标识、验证连接版本并创建第一条稳定路由；不需要手填发布记录。',
      okText: mode === 'canary' ? '启动 5% 灰度' : mode === 'recover' ? '替换失效路由' : '确认首次启用',
      onOk: () => activate(target, mode),
    });
  };

  const toggleConnection = async (target: ModelEndpointTarget) => {
    const resume = target.config_status === 'disabled';
    if (resume && (!target.last_probe?.current || !target.last_probe.success)) {
      Toast.error('恢复前必须先用当前凭证完成一次成功检测');
      return;
    }
    setWorkingTarget(target.target_ref);
    try {
      await api.updateChannel(target.channel_id, {
        status: resume ? 'staged' : 'disabled',
        expected_routing_epoch: target.routing_epoch,
      });
      Toast.success(resume ? '连接已恢复；保留的稳定发布无需重建' : '连接已紧急暂停；发布记录和路由版本保持不变');
      await load();
    } catch (error) {
      Toast.error(endpointError(error, resume ? '恢复失败' : '暂停失败'));
    } finally {
      setWorkingTarget('');
    }
  };

  const transitionCanary = async (model: ModelEndpointModel, action: 'promote' | 'rollback') => {
    if (!model.canary_release_ref || !model.canary_release_version) return;
    try {
      if (action === 'promote') await api.promoteRuntimeChange(model.canary_release_ref, model.canary_release_version);
      else await api.rollbackRuntimeChange(model.canary_release_ref, model.canary_release_version);
      Toast.success(action === 'promote' ? '灰度端点已提升为稳定路由' : '灰度已回滚，稳定路由保持不变');
      await load();
    } catch (error) {
      Toast.error(endpointError(error, action === 'promote' ? '提升失败' : '回滚失败'));
    }
  };

  const openCreate = (publicModel = '') => {
    setSelectedPreset('deepseek');
    setPublicModelMode(publicModel ? 'existing' : 'new');
    setLockedPublicModel(publicModel);
    setCreateOpen(true);
  };

  const submitCreate = async (values: Record<string, any>) => {
    const preset = providerPreset(values.preset);
    const publicModel = String(values.public_model || '').trim();
    const upstreamModel = String(values.upstream_model || publicModel).trim();
    const provider = preset.value === 'custom' ? String(values.provider || 'custom').trim() : preset.provider;
    const baseURL = String(values.base_url || preset.baseURL).trim();
    const adapterType = preset.value === 'custom'
      ? String(values.adapter_type || 'openai_compatible') as 'openai_compatible' | 'anthropic_native'
      : preset.adapterType;
    const adapterConfig = adapterType === 'anthropic_native'
      ? { anthropic_version: '2023-06-01' }
      : adapterType === 'azure_openai'
        ? { api_version: String(values.azure_api_version || '').trim(), deployment: upstreamModel }
        : {};
    setSaving(true);
    try {
      await api.createChannel({
        name: String(values.connection_name || `${provider} · ${upstreamModel}`).trim(),
        provider,
        base_url: baseURL,
        models: publicModel,
        model_mapping: upstreamModel !== publicModel ? { [publicModel]: upstreamModel } : {},
        adapter_type: adapterType,
        adapter_config: adapterConfig,
        priority: values.route_role === 'backup' ? 200 : 100,
        routing_weight: 100,
        degradation_threshold: 10,
        api_key: String(values.api_key || '').trim(),
      });
      Toast.success('供应商端点与加密凭证已创建；下一步只需真实检测');
      setCreateOpen(false);
      setLockedPublicModel('');
      setSelectedModel(publicModel);
      await load(false);
    } catch (error) {
      if (error instanceof ApiError) {
        const payload = error.payload as { code?: string; canonical_model?: string } | null;
        if (payload?.code === 'model_identity_conflict' && payload.canonical_model) {
          formApi?.setValue('public_model', payload.canonical_model);
          formApi?.setValue('upstream_model', upstreamModel);
        }
      }
      Toast.error(endpointError(error, '创建供应商端点失败'));
    } finally {
      setSaving(false);
    }
  };

  const modelColumns = [
    {
      title: '公共模型', key: 'model', width: 230,
      render: (_: unknown, model: ModelEndpointModel) => <div className="ur-model-endpoint-name">
        <Text strong>{model.display_name || model.public_model}</Text>
        <Text code size="small">{model.public_model}</Text>
      </div>,
    },
    {
      title: '路由状态', key: 'state', width: 140,
      render: (_: unknown, model: ModelEndpointModel) => <Space vertical align="start" spacing={3}>
        <StatusTag status={model.state}>{stateLabel(model.state)}</StatusTag>
        {model.canary_basis_points > 0 && <Text type="tertiary" size="small">{model.canary_basis_points / 100}% 灰度</Text>}
      </Space>,
    },
    {
      title: '供应商端点', key: 'targets',
      render: (_: unknown, model: ModelEndpointModel) => <div className="ur-model-endpoint-targets">
        {model.targets.length === 0 ? <Text type="tertiary">尚无端点</Text> : model.targets.map(target => (
          <Tag key={target.target_ref} color={target.route_candidate ? 'green' : target.state === 'test_failed' ? 'red' : target.state === 'paused' ? 'grey' : 'blue'}>
            {target.provider} · {target.connection_name} · {stateLabel(target.state)}
          </Tag>
        ))}
      </div>,
    },
    {
      title: '当前结论', key: 'reason', width: 280,
      render: (_: unknown, model: ModelEndpointModel) => <Text type={model.state === 'degraded' || model.state === 'test_failed' ? 'danger' : 'tertiary'}>{stateHelp(model.state)}</Text>,
    },
    {
      title: '操作', key: 'actions', width: 120,
      render: (_: unknown, model: ModelEndpointModel) => <Button type="primary" theme="borderless" onClick={(event) => { event.stopPropagation(); selectModel(model.public_model); }}>管理端点</Button>,
    },
  ];

  return <div className="ur-page ur-page-wide">
    <PageHeader
      eyebrow="模型接入"
      title="模型与端点"
      description="一个公共模型可以连接多个官方或第三方 MaaS 端点；供应商地址、上游模型 ID、凭证、真实检测和当前路由在这里形成一条权威状态链。"
      actions={<Space>
        <Button icon={<IconRefresh />} loading={loading} onClick={() => void load()}>刷新</Button>
        <Button icon={<IconPlus />} type="primary" onClick={() => openCreate()}>新增供应商端点</Button>
      </Space>}
    />

    <Tabs activeKey={activeTab} onChange={changeTab} type="line">
      <TabPane tab="模型与路由" itemKey="models">
        <div className="ur-model-endpoint-kpis">
          <MetricTile label="公共模型" value={projection?.summary.total ?? '-'} meta="按对外调用 ID 聚合" />
          <MetricTile label="路由可用" value={projection?.summary.route_available ?? '-'} tone="green" meta="网关当前至少有一个候选端点" />
          <MetricTile label="需要处理" value={projection?.summary.attention ?? '-'} tone="amber" meta="凭证、检测、路由或生命周期尚未闭环" />
          <MetricTile label="灰度中" value={projection?.summary.canary ?? '-'} tone="blue" meta="稳定路由仍保留" />
        </div>

        <Surface
          title="模型"
          description="表格按公共模型聚合供应商端点。这里展示路由是否可用；项目、应用与 API Key 的最终调用权限只在高级诊断中判断。"
          actions={<Space>
            <Input prefix={<IconSearch />} value={filter} onChange={setFilter} placeholder="搜索模型、供应商或上游 ID" style={{ width: 280 }} />
            <Button disabled={testableTargets.length === 0 || batchTesting} loading={batchTesting} onClick={confirmBatchTests}>补齐端点检测证据（{testableTargets.length}）</Button>
          </Space>}
        >
          {loadError ? <EmptyState title="模型状态暂时不可用" description={loadError} actions={<Button type="primary" onClick={() => void load()}>重试</Button>} minHeight={320} /> :
            <Table
              rowKey="public_model"
              loading={loading}
              dataSource={filteredModels}
              columns={modelColumns}
              pagination={false}
              onRow={(model) => ({ onClick: () => selectModel((model as ModelEndpointModel).public_model), style: { cursor: 'pointer' } })}
              empty={<EmptyState title="还没有模型端点" description="新增一个供应商端点，填写公共模型 ID、上游模型 ID 和 Key，然后完成一次真实检测。" actions={<Button type="primary" onClick={() => openCreate()}>新增供应商端点</Button>} minHeight={300} />}
            />}
        </Surface>
      </TabPane>

      <TabPane tab="凭证" itemKey="credentials">
        {activeTab === 'credentials' && <ChannelKeysPage embedded />}
      </TabPane>

      <TabPane tab="高级运营" itemKey="advanced">
        {activeTab === 'advanced' && <>
          <Surface description="日常新增与启用不需要进入这里。目录、调用诊断和历史发布只在需要审计或排障时使用。">
            <Space wrap>
              <Button type={advancedView === 'catalog' ? 'primary' : 'tertiary'} onClick={() => changeAdvanced('catalog')}>模型目录与项目范围</Button>
              <Button type={advancedView === 'diagnostics' ? 'primary' : 'tertiary'} onClick={() => changeAdvanced('diagnostics')}>连接条件诊断</Button>
              <Button type={advancedView === 'releases' ? 'primary' : 'tertiary'} onClick={() => changeAdvanced('releases')}>发布记录与证据</Button>
            </Space>
          </Surface>
          {advancedView === 'catalog' && <ModelGovernancePage embedded />}
          {advancedView === 'diagnostics' && <ModelReadinessPage embedded forceConnectionMode />}
          {advancedView === 'releases' && <RuntimeChangeControlWorkspace />}
        </>}
      </TabPane>
    </Tabs>

    <SideSheet
      title={selected ? `${selected.display_name || selected.public_model} · 供应商端点` : '供应商端点'}
      visible={Boolean(selected)}
      onCancel={closeSelectedModel}
      width={760}
    >
      {selected && <div className="ur-model-endpoint-sheet">
        <div className="ur-model-endpoint-sheet-summary">
          <div><Text type="tertiary">公共模型</Text><Text code>{selected.public_model}</Text></div>
          <div><Text type="tertiary">当前路由状态</Text><StatusTag status={selected.state}>{stateLabel(selected.state)}</StatusTag></div>
          <div><Text type="tertiary">供应商端点</Text><Text>{selected.targets.length}</Text></div>
        </div>
        <Banner type={selected.route_available && selected.state !== 'governance_blocked' ? 'success' : ['degraded', 'governance_blocked'].includes(selected.state) ? 'danger' : 'info'} description={stateHelp(selected.state)} closeIcon={null} />
        {selected.canary_release_ref && <Surface title={`当前 ${selected.canary_basis_points / 100}% 灰度`} description="稳定路由仍在；确认真实业务指标后提升，异常时回滚。" actions={<Space>
          <Button type="danger" onClick={() => transitionCanary(selected, 'rollback')}>回滚灰度</Button>
          <Button type="primary" onClick={() => transitionCanary(selected, 'promote')}>提升为稳定</Button>
        </Space>}><Text type="tertiary">发布标识和并发控制版本属于系统内部证据，不需要在日常流程中查看或填写。</Text></Surface>}

        {selected.targets.map(target => <Surface
          key={target.target_ref}
          title={`${target.connection_name} · ${target.provider}`}
          description={`${target.public_model} → ${target.upstream_model}`}
          actions={<Space wrap>
            <Button loading={workingTarget === target.target_ref} disabled={workingTarget !== '' || target.eligible_credential_count === 0} onClick={() => confirmTest(target)}>真实检测</Button>
            {target.state === 'ready' && selected.state !== 'governance_blocked' && <Button type="primary" loading={workingTarget === target.target_ref} onClick={() => confirmActivate(selected, target)}>{!selected.stable_release_ref ? '首次启用' : selected.route_available ? '加入 5% 灰度' : '替换失效路由'}</Button>}
            {target.config_status === 'disabled'
              ? <Button type="primary" disabled={!target.last_probe?.current || !target.last_probe.success} loading={workingTarget === target.target_ref} onClick={() => toggleConnection(target)}>恢复连接</Button>
              : (target.stable_assignment || target.route_candidate) && <Button type="danger" theme="borderless" loading={workingTarget === target.target_ref} onClick={() => toggleConnection(target)}>紧急暂停</Button>}
          </Space>}
        >
          <Descriptions row size="small" data={[
            { key: '端点地址', value: target.base_url },
            { key: '协议适配', value: target.adapter_type },
            { key: '路由角色', value: target.priority <= 100 ? `主端点 · P${target.priority} / W${target.weight}` : `备用端点 · P${target.priority} / W${target.weight}` },
            { key: '凭证', value: target.eligible_credential_count > 0 ? `${target.eligible_credential_count} 个当前可用` : '缺少可用凭证' },
            { key: '熔断状态', value: target.circuit_state },
            { key: '网关候选', value: routeCandidateCopy(target) },
          ]} />
          <div className="ur-model-endpoint-probe">
            <Text strong>最近真实检测</Text>
            {!target.last_probe ? <Text type="tertiary">尚未检测</Text> : <Space wrap>
              <StatusTag status={target.last_probe.success && target.last_probe.current ? 'success' : target.last_probe.success ? 'stale' : 'failed'}>
                {target.last_probe.success ? (target.last_probe.current ? '通过且有效' : '通过但已失效') : '失败'}
              </StatusTag>
              {target.last_probe.latency_ms > 0 && <LatencyText value={target.last_probe.latency_ms} />}
              <Text type="tertiary">HTTP {target.last_probe.status_code || '-'} · {formatDateTime(target.last_probe.tested_at)}</Text>
              {target.last_probe.error_summary && <Text type="danger">{target.last_probe.error_summary}</Text>}
            </Space>}
          </div>
          <Space style={{ marginTop: 12 }}>
            <Button type="tertiary" onClick={() => setCredentialTarget(target)}>管理此端点凭证</Button>
            <Button type="tertiary" onClick={() => changeAdvanced('diagnostics', target)}>诊断此模型连接</Button>
          </Space>
        </Surface>)}
        <Button icon={<IconPlus />} onClick={() => openCreate(selected.public_model)}>为此公共模型增加供应商端点</Button>
        <details>
          <summary style={{ cursor: 'pointer' }}>高级审计信息</summary>
          <div style={{ marginTop: 12 }}>
            <Text type="tertiary">稳定发布：{selected.stable_release_ref ? '已建立' : '无'}；历史记录 {selected.release_history_count} 条。旧发布是不可变审计记录，不是重复的当前模型。</Text>
          </div>
        </details>
      </div>}
    </SideSheet>

    <SideSheet title="新增供应商端点" visible={createOpen} onCancel={() => { setCreateOpen(false); setLockedPublicModel(''); }} width={600}>
      <Banner type="info" description="公共模型 ID 是应用调用 UniRouter 时使用的名字；上游模型 ID 是这个供应商实际接受的模型或部署名。相同公共模型可以绑定多个供应商端点。" closeIcon={null} style={{ marginBottom: 16 }} />
      <Form getFormApi={setFormApi} onSubmit={submitCreate} initValues={{ preset: 'deepseek', base_url: providerPreset('deepseek').baseURL, public_model: lockedPublicModel, upstream_model: lockedPublicModel, route_role: 'primary' }}>
        <Form.Select
          field="preset"
          label="接入模板"
          optionList={providerPresets.map(preset => ({ label: preset.label, value: preset.value }))}
          rules={[{ required: true, message: '请选择接入模板' }]}
          onChange={(value) => {
            const preset = providerPresets.find(item => item.value === value);
            setSelectedPreset(String(value));
            if (preset) {
              formApi?.setValue('base_url', preset.baseURL);
              formApi?.setValue('adapter_type', preset.adapterType);
            }
          }}
          extraText={providerPresets.find(item => item.value === selectedPreset)?.note || '选择模板后仍以你的供应商控制台配置为准。'}
        />
        {selectedPreset !== 'custom' && <Button theme="borderless" onClick={() => {
          setSelectedPreset('custom');
          formApi?.setValue('preset', 'custom');
          formApi?.setValue('base_url', '');
          formApi?.setValue('adapter_type', 'openai_compatible');
        }}>列表中没有？新增自定义兼容供应商</Button>}
        {selectedPreset === 'custom' && <div className="ur-model-endpoint-form-grid">
          <Form.Input field="provider" label="供应商标识" placeholder="例如 minimax、mimo、volcengine" rules={[{ required: true, message: '必填' }]} extraText="这是审计和运营使用的稳定名称，不限制必须属于内置供应商。" />
          <Form.Select field="adapter_type" label="兼容协议" optionList={[
            { label: 'OpenAI Chat Completions 兼容', value: 'openai_compatible' },
            { label: 'Anthropic Messages 兼容', value: 'anthropic_native' },
          ]} rules={[{ required: true, message: '必填' }]} extraText="选择供应商明确声明兼容的协议；UniRouter 不会仅凭 URL 猜测。" />
        </div>}
        <Form.Input field="connection_name" label="连接名称（可选）" placeholder="例如 DeepSeek 官方 · 华北生产" />
        <Form.Input field="base_url" label="Base URL" placeholder="https://..." rules={[{ required: true, message: '必填' }]} />
        {!lockedPublicModel && <Form.RadioGroup
          field="public_model_mode"
          label="公共模型"
          initValue="new"
          type="button"
          buttonSize="middle"
          options={[
            { label: '创建新的公共模型 ID', value: 'new' },
            { label: '连接到已有公共模型', value: 'existing' },
          ]}
          onChange={(event) => {
            const mode = String(event?.target?.value || 'new') as 'existing' | 'new';
            setPublicModelMode(mode);
            formApi?.setValue('public_model', '');
          }}
          extraText="同一个模型来自官方、OpenRouter、百炼、Azure 等供应商时，选择已有公共模型；只有应用需要一个全新的对外调用 ID 时才创建。"
        />}
        <div className="ur-model-endpoint-form-grid">
          {lockedPublicModel ? <Form.Input
            field="public_model"
            label="公共模型 ID"
            disabled
            rules={[{ required: true, message: '必填' }]}
            extraText="正在为当前公共模型增加另一个供应商端点，公共 ID 不可改变。"
          /> : publicModelMode === 'existing' ? <Form.Select
            field="public_model"
            label="已有公共模型 ID"
            placeholder="选择一个已有公共模型"
            filter
            optionList={models.filter(model => model.lifecycle !== 'retired').map(model => ({
              value: model.public_model,
              label: model.display_name && model.display_name !== model.public_model ? `${model.display_name} · ${model.public_model}` : model.public_model,
            }))}
            rules={[{ required: true, message: '请选择已有公共模型' }]}
            onChange={(value) => {
              if (!String(formApi?.getValue('upstream_model') || '').trim()) formApi?.setValue('upstream_model', value);
            }}
          /> : <Form.Input
            field="public_model"
            label="新的公共模型 ID"
            placeholder="例如 deepseek-v4-flash"
            rules={[{ required: true, message: '请输入新的公共模型 ID' }]}
            extraText="这是应用请求 UniRouter 时使用的稳定 ID。建议使用小写字母、数字、点、短横线或下划线；不要把供应商名称重复写入同一模型的多个 ID。"
          />}
          <Form.Input field="upstream_model" label="上游模型 / 部署 ID" placeholder="供应商实际接受的精确 ID" rules={[{ required: true, message: '必填' }]} />
        </div>
        {selectedPreset === 'azure' && <div className="ur-model-endpoint-form-grid">
          <Text type="tertiary">上面的“上游模型 / 部署 ID”就是 Azure 部署名，无需重复填写。</Text>
          <Form.Input field="azure_api_version" label="API Version" placeholder="2024-10-21" rules={[{ required: true, message: '必填' }]} />
        </div>}
        <PasswordField field="api_key" label="供应商 API Key" rules={[{ required: true, message: '必填' }]} autoComplete="new-password" visibilityResetKey={createOpen ? 'create-open' : 'closed'} />
        <Form.Select field="route_role" label="路由角色" optionList={[
          { label: '主端点（优先使用）', value: 'primary' },
          { label: '备用端点（主端点不可用时使用）', value: 'backup' },
        ]} rules={[{ required: true, message: '请选择路由角色' }]} extraText="稍后可在高级发布中调整精确优先级和同级权重。" />
        <div className="ur-form-actions">
          <Button onClick={() => { setCreateOpen(false); setLockedPublicModel(''); }}>取消</Button>
          <Button type="primary" loading={saving} onClick={() => formApi?.submitForm()}>保存端点与凭证</Button>
        </div>
      </Form>
    </SideSheet>
    <SideSheet
      title={credentialTarget ? `${credentialTarget.connection_name} · 凭证` : '端点凭证'}
      visible={Boolean(credentialTarget)}
      onCancel={() => { setCredentialTarget(null); void load(); }}
      width={720}
    >
      {credentialTarget && <ChannelKeysPage
        embedded
        channelId={credentialTarget.channel_id}
        model={credentialTarget.public_model}
        scoped
      />}
    </SideSheet>
  </div>;
}

export { exactTestSucceeded, providerPresets, providerSummary, stateHelp, stateLabel };
