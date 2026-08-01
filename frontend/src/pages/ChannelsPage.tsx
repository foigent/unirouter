import { useEffect, useMemo, useRef, useState } from 'react';
import { Button, Table, Modal, Form, Tag, Toast, Space, Tooltip, SideSheet, Input, Typography, Descriptions } from '@douyinfe/semi-ui';
import { IconPlus, IconDelete, IconEdit } from '@douyinfe/semi-icons';
import { api, ApiError } from '../api/client';
import { EmptyState, PageHeader, StatusTag, Surface } from '../ui';

interface Channel {
  id: number;
  name: string;
  provider: string;
  base_url: string;
  models: string;
  adapter_type?: string;
  adapter_config?: Record<string, unknown> | string;
  priority: number;
  routing_weight: number;
  routing_epoch: number;
  degradation_threshold: number;
  status: string;
  circuit_state: string;
  fail_count: number;
  created_at: string;
}

interface RoutingPreviewCandidate {
  channel_id: number;
  channel_name: string;
  provider: string;
  priority: number;
  weight: number;
  order?: number;
  circuit_state: string;
  eligible_key_count: number;
  reason: string;
  requires_probe: boolean;
}

interface RoutingPreview {
  policy_version: string;
  snapshot_version: string;
  snapshot_source: string;
  degraded: boolean;
  model: string;
  candidates: RoutingPreviewCandidate[];
  exclusions: RoutingPreviewCandidate[];
}

interface ProviderPackManifest {
  pack_id: string;
  pack_version: string;
  provider: string;
  adapter_type: string;
  conformance_level: 'fixture_verified';
  network_verified: boolean;
  endpoints: string[];
  modalities: string[];
  auth_profiles: Array<{ type: string; wire_header: string }>;
}

interface ProviderPack {
  manifest: ProviderPackManifest;
  manifest_sha256: string;
  schema_sha256: string;
}

const channelStatusOptions = [
  { label: '待发布（staged）', value: 'staged' },
  { label: '紧急禁用（disabled）', value: 'disabled' },
];

function channelUpdateErrorMessage(error: unknown) {
  if (!(error instanceof ApiError)) return '更新失败，请稍后重试';
  const backendMessage = error.message.toLowerCase();
  if (error.status === 400 && backendMessage.includes('status must be')) {
    return '通道状态仅支持 active、staged 或 disabled';
  }
  if (error.status === 400 && backendMessage.includes('adapter_type')) {
    return 'Adapter Type 不受支持';
  }
  if (error.status === 400 && backendMessage.includes('adapter_config')) {
    return 'Adapter Config 与所选协议不兼容';
  }
  if (error.status === 400) return '通道配置无效，请检查字段后重试';
  if (error.status === 403) return '当前账号无权更新此通道';
  if (error.status === 404) return '通道不存在或已被删除';
  if (error.status >= 500) return '服务暂时无法更新通道，请稍后重试';
  return `更新失败（HTTP ${error.status}）`;
}

function channelConflictMessage(error: ApiError) {
  const backendMessage = error.message.toLowerCase();
  if (backendMessage.includes('active channel runtime fields')) {
    return '生效通道的运行参数不能直接修改；请先改为 staged，再通过“模型治理 → 运行时发布”上线';
  }
  if (backendMessage.includes('direct channel activation')) {
    return '不能直接启用通道；请通过“模型治理 → 运行时发布”完成验证、灰度与提升';
  }
  return '该通道已被其他管理员修改或正由运行时发布接管，请刷新后重试';
}

const routingReasonLabel = (reason: string) => ({
  equal_priority_static_weight: '同优先级内按权重排序',
  priority_failover: '按优先级故障转移',
  channel_inactive: '通道未启用',
  circuit_recovery_pending: '熔断恢复等待中',
  probe_sample_not_selected: '本次未抽中恢复探测',
  circuit_unavailable: '熔断状态不可用',
  no_key: '没有凭证',
  no_active_key: '没有启用的凭证',
  key_cooldown: '凭证冷却中',
  key_model_not_allowed: '凭证不允许此模型',
  candidate_limit: '超过单次候选上限',
}[reason] || reason);

const routingSourceLabel = (source: string) => ({
  live: '刚从数据库刷新',
  cache: '本机 1 秒内快照',
  last_known_good: '数据库异常时的短时备用快照',
}[source] || source);

function selectedChannelIDFromParams() {
  const value = Number(new URLSearchParams(window.location.search).get('channel_id'));
  return Number.isSafeInteger(value) && value > 0 ? value : undefined;
}

function syncSelectedChannelID(id?: number) {
  const params = new URLSearchParams(window.location.search);
  if (id) params.set('channel_id', String(id));
  else params.delete('channel_id');
  const query = params.toString();
  window.history.replaceState(null, '', `${window.location.pathname}${query ? `?${query}` : ''}`);
}

function adapterConfigFacts(channel: Channel) {
  let config: Record<string, unknown> = {};
  if (channel.adapter_config && typeof channel.adapter_config === 'object') {
    config = channel.adapter_config;
  } else if (typeof channel.adapter_config === 'string') {
    try {
      const parsed = JSON.parse(channel.adapter_config);
      if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) config = parsed;
    } catch {
      return [{ key: 'config', label: '配置状态', value: '无法解析' }];
    }
  }
  const allowed = channel.adapter_type === 'azure_openai'
    ? [['deployment', 'Deployment'], ['api_version', 'API version'], ['auth_mode', 'Auth mode']]
    : channel.adapter_type === 'anthropic_native'
      ? [['anthropic_version', 'Anthropic version'], ['anthropic_beta', 'Anthropic beta']]
      : [];
  return allowed.flatMap(([key, label]) => {
    const value = config[key];
    return typeof value === 'string' && value.trim()
      ? [{ key, label, value }]
      : [];
  });
}

export default function ChannelsPage() {
  const [channels, setChannels] = useState<Channel[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const requestVersion = useRef(0);
  const [showDrawer, setShowDrawer] = useState(false);
  const [editing, setEditing] = useState<Channel | null>(null);
  const [formApi, setFormApi] = useState<any>(null);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [total, setTotal] = useState(0);
  const [previewModel, setPreviewModel] = useState('');
  const [previewLoading, setPreviewLoading] = useState(false);
  const [preview, setPreview] = useState<RoutingPreview | null>(null);
  const [providerPacks, setProviderPacks] = useState<ProviderPack[]>([]);
  const [providerPackError, setProviderPackError] = useState('');
  const [selectedChannelId, setSelectedChannelId] = useState<number | undefined>(() => selectedChannelIDFromParams());

  const load = (p = page, size = pageSize) => {
    const version = ++requestVersion.current;
    setLoading(true);
    setLoadError('');
    setChannels([]);
    setTotal(0);
    api.getChannels(p, size)
      .then((data) => {
        if (version !== requestVersion.current) return;
        const nextChannels = data.data || [];
        setChannels(nextChannels);
        setTotal(data.total || 0);
        setSelectedChannelId((current) => {
          const preferred = nextChannels.some((item: Channel) => item.id === current)
            ? current
            : nextChannels[0]?.id;
          syncSelectedChannelID(preferred);
          return preferred;
        });
      })
      .catch(() => {
        if (version === requestVersion.current) {
          setLoadError('无法加载上游通道列表。');
          setSelectedChannelId(undefined);
          syncSelectedChannelID(undefined);
        }
      })
      .finally(() => {
        if (version === requestVersion.current) setLoading(false);
      });
  };

  useEffect(() => { load(1, pageSize); }, []);
  useEffect(() => {
    let active = true;
    setProviderPackError('');
    api.getProviderPacks()
      .then((data) => {
        if (!active) return;
        setProviderPacks(data.data || []);
      })
      .catch(() => {
        if (!active) return;
        setProviderPacks([]);
        setProviderPackError('Provider Pack 目录不可用，当前兼容性状态不能判定。');
      });
    return () => { active = false; };
  }, []);

  const selectedChannel = useMemo(
    () => channels.find((channel) => channel.id === selectedChannelId),
    [channels, selectedChannelId],
  );
  const selectedPack = useMemo(
    () => providerPacks.find((pack) =>
      pack.manifest.provider.toLowerCase() === selectedChannel?.provider.toLowerCase() &&
      pack.manifest.adapter_type === (selectedChannel?.adapter_type || 'openai_compatible')),
    [providerPacks, selectedChannel],
  );
  const selectedConfigFacts = useMemo(
    () => selectedChannel ? adapterConfigFacts(selectedChannel) : [],
    [selectedChannel],
  );

  const selectChannel = (channel: Channel) => {
    setSelectedChannelId(channel.id);
    syncSelectedChannelID(channel.id);
  };

  const closeDrawer = () => { setShowDrawer(false); setEditing(null); };

  const normalizeAdapterConfig = (value: unknown) => {
    if (value === undefined || value === null || value === '') return {};
    if (typeof value === 'object') return value;
    if (typeof value === 'string') return JSON.parse(value);
    return {};
  };

  const formatAdapterConfig = (value: unknown) => {
    if (value === undefined || value === null || value === '') return '{}';
    if (typeof value === 'string') {
      try {
        return JSON.stringify(JSON.parse(value), null, 2);
      } catch {
        return value;
      }
    }
    return JSON.stringify(value, null, 2);
  };

  const handleSubmit = async (values: Record<string, any>) => {
    let adapterConfig: Record<string, unknown>;
    try {
      adapterConfig = normalizeAdapterConfig(values.adapter_config) as Record<string, unknown>;
    } catch {
      Toast.error('Adapter Config 必须是合法 JSON');
      return;
    }
    const payload = {
      ...values,
      adapter_type: values.adapter_type || 'openai_compatible',
      adapter_config: adapterConfig,
      ...(editing ? { expected_routing_epoch: editing.routing_epoch } : {}),
    };
    try {
      if (editing) {
        await api.updateChannel(editing.id, payload);
        Toast.success('通道更新成功');
      } else {
        await api.createChannel(payload);
        Toast.success('通道已创建为 staged；配置凭证后请在模型治理的运行时发布中验证和灰度');
      }
      closeDrawer();
      load();
    } catch (error) {
      if (editing && error instanceof ApiError && error.status === 409) {
			Toast.error(channelConflictMessage(error));
      } else {
        Toast.error(editing ? channelUpdateErrorMessage(error) : '创建失败');
      }
    }
  };

  const handlePreview = async () => {
    const model = previewModel.trim();
    if (!model) {
      Toast.warning('请输入要预览的模型名称');
      return;
    }
    setPreview(null);
    setPreviewLoading(true);
    try {
      setPreview(await api.previewRouting(model) as RoutingPreview);
    } catch {
      setPreview(null);
      Toast.error('路由预览失败，请检查模型和通道状态');
    } finally {
      setPreviewLoading(false);
    }
  };

  const handleDelete = async (id: number) => {
    Modal.confirm({
      title: '确认删除',
      content: '删除后不可恢复，确定要删除这个通道吗？',
      onOk: async () => {
        try {
          await api.deleteChannel(id);
          Toast.success('删除成功');
          load();
        } catch {
          Toast.error('删除失败');
        }
      },
    });
  };

  const openEdit = (record: Channel) => {
    setEditing(record);
    setShowDrawer(true);
  };

  const openCreate = () => {
    setEditing(null);
    setShowDrawer(true);
  };

  useEffect(() => {
    if (!showDrawer || !formApi) return;
    formApi.reset();
    formApi.setValues(editing ? {
      name: editing.name,
      provider: editing.provider,
      base_url: editing.base_url,
      models: editing.models,
      adapter_type: editing.adapter_type || 'openai_compatible',
      adapter_config: formatAdapterConfig(editing.adapter_config),
      priority: editing.priority,
      routing_weight: editing.routing_weight,
      degradation_threshold: editing.degradation_threshold,
      status: editing.status,
    } : {
      adapter_type: 'openai_compatible',
      adapter_config: '{}',
    });
  }, [editing, formApi, showDrawer]);

  const columns = [
    { title: 'ID', dataIndex: 'id', key: 'id', width: 60 },
    { title: '名称', dataIndex: 'name', key: 'name', width: 180 },
    { title: '供应商', dataIndex: 'provider', key: 'provider', width: 100 },
    { title: '协议适配', dataIndex: 'adapter_type', key: 'adapter_type', width: 160, render: (v: string) => v || 'openai_compatible' },
    {
      title: '一致性',
      key: 'conformance',
      width: 110,
      render: (_: unknown, record: Channel) => {
        if (providerPackError) return <Tag color="orange">unavailable</Tag>;
        const pack = providerPacks.find((item) =>
          item.manifest.provider.toLowerCase() === record.provider.toLowerCase() &&
          item.manifest.adapter_type === (record.adapter_type || 'openai_compatible'));
        return pack ? <Tag color="green">fixture verified</Tag> : <Tag color="orange">undeclared</Tag>;
      },
    },
    { title: '服务地址', dataIndex: 'base_url', key: 'base_url', width: 220, ellipsis: true },
    { title: '支持模型', dataIndex: 'models', key: 'models', width: 220, ellipsis: true },
    { title: '优先级', dataIndex: 'priority', key: 'priority', width: 80 },
    {
      title: '同级权重', dataIndex: 'routing_weight', key: 'routing_weight', width: 95,
      render: (v: number) => <Tooltip content="只和相同优先级的通道比较；权重 200 大约获得权重 100 的两倍首选机会">{v}</Tooltip>,
    },
    { title: '降级阈值', dataIndex: 'degradation_threshold', key: 'degradation_threshold', width: 90 },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 80,
      render: (v: string) => v === 'deprecated'
        ? <Tooltip content="该通道已废弃，建议尽快迁移到新通道"><Tag color="yellow">deprecated ⚠</Tag></Tooltip>
        : <StatusTag status={v}>{v === 'staged' ? '待发布' : v === 'active' ? '生效' : v === 'disabled' ? '禁用' : v}</StatusTag>,
    },
    {
      title: '熔断',
      dataIndex: 'circuit_state',
      key: 'circuit',
      width: 100,
      render: (v: string) => <StatusTag status={v}>{v}</StatusTag>,
    },
    {
      title: '操作',
      key: 'actions',
      width: 120,
      render: (_: any, record: Channel) => (
        <Space>
          <Button aria-label={`编辑通道 ${record.name}`} icon={<IconEdit />} size="small" onClick={(event) => { event.stopPropagation(); openEdit(record); }} />
          <Button aria-label={`删除通道 ${record.name}`} icon={<IconDelete />} size="small" type="danger" onClick={(event) => { event.stopPropagation(); handleDelete(record.id); }} />
        </Space>
      ),
    },
  ];

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="模型接入"
        title="上游通道"
        description="新通道默认 staged；配置凭证后必须到“模型治理 → 运行时发布”完成 Provider Pack 校验、影子、灰度和提升。紧急禁用始终立即生效。"
        actions={<Button icon={<IconPlus />} type="primary" onClick={openCreate}>新增通道</Button>}
      />

      <div className="ur-provider-explorer">
        <main className="ur-explorer-panel">
          <div className="ur-panel-title">
            <div>
              <Typography.Title heading={5} style={{ margin: 0 }}>通道列表</Typography.Title>
              <Typography.Text className="ur-panel-hint">默认选择首条记录 · 右侧持续显示兼容性</Typography.Text>
            </div>
            <Tag size="small">{total} channels</Tag>
          </div>
          <div className="ur-table-shell">
            <Table
              dataSource={channels}
              columns={columns}
              rowKey="id"
              loading={loading}
              scroll={{ x: 1645 }}
              onRow={(record) => ({
                onClick: () => record && selectChannel(record as Channel),
                onKeyDown: (event) => {
                  if (record && (event.key === 'Enter' || event.key === ' ')) {
                    event.preventDefault();
                    selectChannel(record as Channel);
                  }
                },
                tabIndex: 0,
                'aria-selected': selectedChannelId === (record as Channel)?.id,
                style: {
                  cursor: 'pointer',
                  background: selectedChannelId === (record as Channel)?.id ? 'var(--ur-bg-subtle)' : undefined,
                },
              })}
              empty={loadError ? (
                <EmptyState title="上游通道加载失败" description={loadError} actions={<Button type="primary" onClick={() => load()}>重新加载上游通道</Button>} minHeight={220} />
              ) : <EmptyState title="暂无上游通道" minHeight={220} />}
              pagination={{
                currentPage: page,
                pageSize,
                total,
                pageSizeOpts: [20, 50, 100],
                showSizeChanger: true,
                onPageChange: (p: number) => { setPage(p); load(p, pageSize); },
                onPageSizeChange: (s: number) => { setPageSize(s); setPage(1); load(1, s); },
              }}
            />
          </div>
        </main>

        <aside className="ur-explorer-panel ur-explorer-detail" aria-label="Provider 兼容性检查器">
          <div className="ur-detail-header">
            <div>
              <Typography.Text className="ur-panel-hint">Provider Conformance</Typography.Text>
              <Typography.Title heading={5} style={{ margin: 0 }}>兼容性检查器</Typography.Title>
            </div>
            <Tag size="small" color={selectedChannel ? 'blue' : undefined}>{selectedChannel ? '常驻检查器' : '等待通道'}</Tag>
          </div>

          {!selectedChannel ? (
            <div className="ur-receipt-empty" role="status">
              <Typography.Title heading={5}>{loadError ? '等待可验证的通道列表' : '暂无可检查通道'}</Typography.Title>
              <Typography.Text type="tertiary">
                {loadError
                  ? '列表加载失败时不会保留旧兼容性结论。'
                  : '通道出现后将自动选择首条记录，无需再次点击打开详情。'}
              </Typography.Text>
            </div>
          ) : (
            <div className="ur-detail-stack">
              <div className="ur-detail-hero">
                <div>
                  <Typography.Text type="tertiary" size="small">当前通道</Typography.Text>
                  <div><Typography.Text strong>{selectedChannel.name}</Typography.Text></div>
                  <Typography.Text code size="small">channel:{selectedChannel.id}</Typography.Text>
                </div>
                <StatusTag status={selectedChannel.status}>{selectedChannel.status}</StatusTag>
              </div>

              <div className="ur-receipt-summary" aria-label="Provider Pack 摘要">
                <div>
                  <Typography.Text type="tertiary" size="small">Adapter</Typography.Text>
                  <Typography.Text strong size="small">{selectedChannel.adapter_type || 'openai_compatible'}</Typography.Text>
                </div>
                <div>
                  <Typography.Text type="tertiary" size="small">Conformance</Typography.Text>
                  <StatusTag status={providerPackError || !selectedPack ? 'warning' : 'success'}>
                    {providerPackError ? 'unavailable' : selectedPack ? 'fixture verified' : 'undeclared'}
                  </StatusTag>
                </div>
                <div>
                  <Typography.Text type="tertiary" size="small">Network</Typography.Text>
                  <StatusTag status="warning">{selectedPack?.manifest.network_verified ? 'verified' : 'not verified'}</StatusTag>
                </div>
                <div>
                  <Typography.Text type="tertiary" size="small">Circuit</Typography.Text>
                  <StatusTag status={selectedChannel.circuit_state}>{selectedChannel.circuit_state}</StatusTag>
                </div>
              </div>

              {providerPackError ? (
                <div className="ur-detail-card ur-detail-card-error" role="alert">
                  <Typography.Text className="ur-detail-card-title">兼容性目录不可用</Typography.Text>
                  <Typography.Text size="small">{providerPackError}</Typography.Text>
                </div>
              ) : !selectedPack ? (
                <div className="ur-detail-card" role="status">
                  <Typography.Text className="ur-detail-card-title">未声明的适配器</Typography.Text>
                  <Typography.Text type="tertiary" size="small">
                    当前适配器没有权威 Provider Pack；不会把“可配置”解释为“已兼容”。
                  </Typography.Text>
                </div>
              ) : (
                <>
                  <div className="ur-detail-card">
                    <Typography.Text className="ur-detail-card-title">Pack 身份</Typography.Text>
                    <Descriptions
                      row
                      size="small"
                      data={[
                        { key: 'Pack', value: `${selectedPack.manifest.pack_id}@${selectedPack.manifest.pack_version}` },
                        { key: 'Provider', value: selectedPack.manifest.provider },
                        { key: 'Manifest SHA-256', value: <Typography.Text code size="small">{selectedPack.manifest_sha256}</Typography.Text> },
                        { key: 'Schema SHA-256', value: <Typography.Text code size="small">{selectedPack.schema_sha256}</Typography.Text> },
                      ]}
                    />
                  </div>
                  <div className="ur-detail-card">
                    <Typography.Text className="ur-detail-card-title">声明能力</Typography.Text>
                    <div className="ur-chip-row">
                      {selectedPack.manifest.endpoints.map((endpoint) => <Tag key={endpoint} color="blue">{endpoint}</Tag>)}
                      {selectedPack.manifest.modalities.map((modality) => <Tag key={modality} color="cyan">{modality}</Tag>)}
                    </div>
                    <Typography.Text type="tertiary" size="small">
                      鉴权线缆：{selectedPack.manifest.auth_profiles.map((profile) => `${profile.type} → ${profile.wire_header}`).join('；')}
                    </Typography.Text>
                  </div>
                </>
              )}

              <div className="ur-detail-card">
                <Typography.Text className="ur-detail-card-title">当前绑定</Typography.Text>
                <Descriptions
                  row
                  size="small"
                  data={[
                    { key: 'Base URL', value: selectedChannel.base_url },
                    { key: 'Models', value: selectedChannel.models },
                    ...selectedConfigFacts.map((fact) => ({ key: fact.label, value: fact.value })),
                  ]}
                />
                <Typography.Text type="tertiary" size="small">
                  敏感配置值不在检查器中回显；fixture verified 仅证明离线合成合同，不代表真实账号、网络、额度或账单。
                </Typography.Text>
              </div>

              <Button icon={<IconEdit />} onClick={() => openEdit(selectedChannel)}>编辑当前通道</Button>
							{selectedChannel.status === 'staged' && <Typography.Text type="tertiary" size="small">下一步：配置供应商凭证，再到模型治理的“运行时发布”生成确定性证据并灰度上线。</Typography.Text>}
            </div>
          )}
        </aside>
      </div>

      <Surface
        title="路由预览"
        description="只读解释指定模型当前会选择哪些通道以及排除原因。它不会调用上游、不会产生费用，也不会占用半开探针；真实请求仍以服务器请求 ID 进行稳定选择。"
      >
        <div className="ur-inline-controls">
          <Input
            value={previewModel}
            onChange={(value) => {
              setPreviewModel(value);
              setPreview(null);
            }}
            onEnterPress={handlePreview}
            placeholder="模型名称，例如 gpt-4o"
            style={{ width: 320 }}
          />
          <Button type="primary" loading={previewLoading} onClick={handlePreview}>预览路由</Button>
          {preview?.degraded && <Tag color="orange">数据库刷新失败，正在使用短时最后可用快照</Tag>}
        </div>
        {preview && (
          <div className="ur-routing-preview">
            <Typography.Text type="tertiary">
              模型 {preview.model} · 策略 {preview.policy_version} · 快照 {preview.snapshot_version} · {routingSourceLabel(preview.snapshot_source)}
            </Typography.Text>
            <div className="ur-routing-preview-grid">
              <div>
                <strong>候选顺序</strong>
                {preview.candidates.length === 0 && <p>没有可用通道</p>}
                {preview.candidates.map((item) => (
                  <div className="ur-routing-preview-item" key={`candidate-${item.channel_id}`}>
                    <Tag color="blue">#{item.order}</Tag>
                    <span>{item.channel_name}</span>
                    <span>优先级 {item.priority} / 权重 {item.weight}</span>
                    <span>{routingReasonLabel(item.reason)}</span>
                    {item.requires_probe && <Tag color="orange">需取得半开探针</Tag>}
                  </div>
                ))}
              </div>
              <div>
                <strong>未入选及原因</strong>
                {preview.exclusions.length === 0 && <p>没有被排除的通道</p>}
                {preview.exclusions.map((item) => (
                  <div className="ur-routing-preview-item" key={`exclusion-${item.channel_id}`}>
                    <span>{item.channel_name}</span>
                    <Tag color="grey">{routingReasonLabel(item.reason)}</Tag>
                  </div>
                ))}
              </div>
            </div>
          </div>
        )}
      </Surface>

      <SideSheet
        title={editing ? '编辑通道' : '新增通道'}
        visible={showDrawer}
        onCancel={closeDrawer}
        width={480}
      >
        <Form getFormApi={(api: any) => setFormApi(api)} onSubmit={handleSubmit}>
          <Form.Input field="name" label="通道名称" rules={[{ required: true, message: '必填' }]} />
          <Form.Select field="provider" label="供应商" rules={[{ required: true, message: '必填' }]}
            optionList={[
              { label: 'OpenAI', value: 'openai' },
              { label: 'Anthropic', value: 'anthropic' },
              { label: 'Azure', value: 'azure' },
              { label: 'Kimi', value: 'kimi' },
              { label: '自定义', value: 'custom' },
            ]}
          />
          <Form.Input field="base_url" label="服务地址（Base URL）" rules={[{ required: true, message: '必填' }]} />
          <Form.Input field="models" label="支持模型" placeholder="多个模型用英文逗号分隔，如 gpt-4o, gpt-4o-mini" rules={[{ required: true, message: '必填' }]} />
          <Form.Select field="adapter_type" label="Adapter Type" initValue="openai_compatible"
            optionList={[
              { label: 'OpenAI Compatible', value: 'openai_compatible' },
              { label: 'Anthropic Native', value: 'anthropic_native' },
              { label: 'Azure OpenAI (deployment-scoped)', value: 'azure_openai' },
            ]}
          />
          <Form.TextArea
            field="adapter_config"
            label="Adapter Config (JSON)"
            placeholder='Anthropic: {"anthropic_version":"2023-06-01"} · Azure: {"api_version":"2024-10-21","deployment":"gpt-4o-prod"}'
            rows={4}
          />
          <Form.InputNumber field="priority" label="优先级 (越小越优先)" initValue={100} />
          <Form.InputNumber field="routing_weight" label="同优先级权重 (1-1000)" initValue={100} extraText="只在优先级相同时分配流量；例如 100 与 200 约为 1:2。" />
          <Form.InputNumber field="degradation_threshold" label="熔断阈值 (连续失败次数)" initValue={10} extraText="达到次数后暂停该通道，恢复窗口到期后只允许一个探针请求。" />
          {editing && <Form.Select field="status" label="状态" optionList={editing.status === 'active'
            ? [{ label: '保持生效（仅未改运行字段时）', value: 'active' }, ...channelStatusOptions]
            : channelStatusOptions} extraText="staged/disabled 不能直接改回 active；请通过运行时发布提升。" />}
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 24 }}>
            <Button onClick={closeDrawer}>取消</Button>
            <Button type="primary" onClick={() => formApi?.submitForm()}>{editing ? '更新' : '创建'}</Button>
          </div>
        </Form>
      </SideSheet>
    </div>
  );
}
