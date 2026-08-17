import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  Button,
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
import { api } from '../api/client';
import type { ModelEndpointModel, ModelEndpointProjection, ModelEndpointTarget } from '../api/modelEndpoints';
import ModelEndpointCreateSheet from '../features/modelEndpoints/ModelEndpointCreateSheet';
import ModelEndpointDetailsSheet from '../features/modelEndpoints/ModelEndpointDetailsSheet';
import { endpointError, exactTestSucceeded, providerSummary, stateHelp, stateLabel } from '../features/modelEndpoints/presentation';
import { EmptyState, MetricTile, PageHeader, StatusTag, Surface } from '../ui';
import { providerPresets } from '../utils/providerEndpoints';
import ChannelKeysPage from './ChannelKeysPage';
import ModelGovernancePage from './ModelGovernancePage';
import ModelReadinessPage from './ModelReadinessPage';
import RuntimeChangeControlWorkspace from './RuntimeChangeControlWorkspace';

const { TabPane } = Tabs;
const { Text } = Typography;

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
  const [credentialTarget, setCredentialTarget] = useState<ModelEndpointTarget | null>(null);
  const [workingTarget, setWorkingTarget] = useState('');
  const [batchTesting, setBatchTesting] = useState(false);
  const requestVersion = useRef(0);

  const load = useCallback(async (keepSelection = true) => {
    const version = ++requestVersion.current;
    setLoading(true);
    setLoadError('');
    try {
      const result = await api.getModelEndpoints();
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
    setLockedPublicModel(publicModel);
    setCreateOpen(true);
  };

  const closeCreate = () => {
    setCreateOpen(false);
    setLockedPublicModel('');
  };

  const createdEndpoint = async (publicModel: string) => {
    closeCreate();
    setSelectedModel(publicModel);
    await load();
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
            loading && !projection ? <EmptyState title="正在加载模型端点" description="正在汇总模型、供应商端点、检测证据和当前路由，请稍候。" minHeight={320} /> :
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

    <ModelEndpointDetailsSheet
      selected={selected}
      workingTarget={workingTarget}
      onClose={closeSelectedModel}
      onTransitionCanary={transitionCanary}
      onTest={confirmTest}
      onActivate={confirmActivate}
      onToggle={toggleConnection}
      onManageCredential={setCredentialTarget}
      onDiagnose={(target) => changeAdvanced('diagnostics', target)}
      onAddTarget={openCreate}
    />

    <ModelEndpointCreateSheet
      open={createOpen}
      lockedPublicModel={lockedPublicModel}
      models={models}
      onCancel={closeCreate}
      onCreated={createdEndpoint}
    />
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
