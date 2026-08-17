import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Button, Table, Modal, Tag, Toast, Space, Tooltip, Select, Typography, Descriptions } from '@douyinfe/semi-ui';
import { IconPlus, IconDelete, IconEdit, IconRefresh } from '@douyinfe/semi-icons';
import { api, ApiError } from '../api/client';
import { EmptyState, PageHeader, StatusTag, Surface } from '../ui';
import { normalizeModelList } from '../utils/modelList';
import ConnectionModelPanel from '../components/ConnectionModelPanel';
import ChannelEditorSheet from '../components/ChannelEditorSheet';
import RuntimeChangeControlWorkspace from './RuntimeChangeControlWorkspace';
import { loadAllPages } from '../utils/loadAllPages';
import { parseChannelModels, type ChannelReadinessCredential } from '../utils/channelReadiness';
import { useAuthStore } from '../store/auth';
import { hasLicensedFeature, licenseRestrictionMessage } from '../navigation';
import {
  adapterConfigFacts,
  channelConflictCode,
  channelConflictMessage,
  channelHasRetainedRelease,
  channelUpdateErrorMessage,
  clearConnectionRecoveryParams,
  connectionAdvancedPath,
  effectiveChannelStatus,
  findProviderPackMatch,
  navigateWithinApp,
  recoveryModelFromParams,
  routingReasonLabel,
  routingSourceLabel,
  selectedChannelIDFromParams,
  syncSelectedChannelID,
  type Channel,
  type ProviderPack,
  type RoutingPreview,
} from '../utils/channelAdmin';
export default function ChannelsPage() {
  const licenseAccess = useAuthStore((state) => state.user?.license_access);
  const gatewayLicensed = hasLicensedFeature(licenseAccess, 'gateway');
  const gatewayRestriction = licenseAccess && !gatewayLicensed
    ? licenseRestrictionMessage(licenseAccess, 'gateway')
    : '';
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
  const [recoveryModel, setRecoveryModel] = useState(() => recoveryModelFromParams());
  const [channelKeys, setChannelKeys] = useState<ChannelReadinessCredential[]>([]);
  const [credentialLoadState, setCredentialLoadState] = useState<'loading' | 'ready' | 'error'>('loading');
  const [credentialLoadError, setCredentialLoadError] = useState('');
  const credentialRequestVersion = useRef(0);

  const load = (p = page, size = pageSize, preferredChannelID?: number) => {
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
          const requested = preferredChannelID || current;
          const preferred = nextChannels.some((item: Channel) => item.id === requested)
            ? requested
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

  const loadCredentials = useCallback(async () => {
    const version = ++credentialRequestVersion.current;
    setCredentialLoadState('loading');
    setCredentialLoadError('');
    try {
      const items = await loadAllPages<ChannelReadinessCredential>((nextPage, size) => api.getChannelKeys(nextPage, size));
      if (version !== credentialRequestVersion.current) return;
      setChannelKeys(items);
      setCredentialLoadState('ready');
    } catch (error: any) {
      if (version !== credentialRequestVersion.current) return;
      setChannelKeys([]);
      setCredentialLoadState('error');
      setCredentialLoadError(`供应商凭证状态加载失败${error?.message ? `：${error.message}` : ''}`);
    }
  }, []);

  useEffect(() => { load(1, pageSize); }, []);
  useEffect(() => { void loadCredentials(); }, [loadCredentials]);
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
  const selectedPackMatch = useMemo(
    () => findProviderPackMatch(providerPacks, selectedChannel),
    [providerPacks, selectedChannel],
  );
  const selectedPack = selectedPackMatch?.pack;
  const editingEffectiveStatus = editing ? effectiveChannelStatus(editing) : '';
  const editingIsActive = editingEffectiveStatus === 'active';
  const selectedConfigFacts = useMemo(
    () => selectedChannel ? adapterConfigFacts(selectedChannel) : [],
    [selectedChannel],
  );
  const modelsPublishedElsewhere = useMemo(() => {
    const published = new Set<string>();
    for (const channel of channels) {
      if (channel.id === selectedChannelId) continue;
      for (const model of parseChannelModels(channel.published_models)) published.add(model);
    }
    return published;
  }, [channels, selectedChannelId]);
	const routingModelOptions = useMemo(() => {
		const owners = new Map<string, string[]>();
		for (const channel of channels) {
			for (const model of parseChannelModels(channel.models)) {
				const names = owners.get(model) || [];
				if (!names.includes(channel.name)) names.push(channel.name);
				owners.set(model, names);
			}
		}
		return [...owners.entries()]
			.sort(([left], [right]) => left.localeCompare(right))
			.map(([model, names]) => ({ value: model, label: `${model} · ${names.join('、')}` }));
	}, [channels]);

  const selectChannel = (channel: Channel) => {
    setRecoveryModel('');
    clearConnectionRecoveryParams();
    setSelectedChannelId(channel.id);
    syncSelectedChannelID(channel.id);
  };

  const openConnectionRecovery = (channel: Channel, model = parseChannelModels(channel.published_models)[0] || '') => {
    if (!model) return;
    setShowDrawer(false);
    setEditing(null);
    setSelectedChannelId(channel.id);
    syncSelectedChannelID(channel.id);
    setRecoveryModel(model);
    navigateWithinApp(`${connectionAdvancedPath('/channels', channel.id, model)}&intent=recover`);
    window.setTimeout(() => document.getElementById('connection-recovery')?.scrollIntoView?.({ behavior: 'smooth', block: 'start' }), 0);
  };

  const openRouteManagement = (model: string, intent: 'replace' | 'recover' = 'replace') => {
    if (intent === 'recover' && selectedChannel) {
      openConnectionRecovery(selectedChannel, model);
      return;
    }
    if (selectedChannel) navigateWithinApp(`${connectionAdvancedPath('/model-governance', selectedChannel.id, model)}&tab=releases&intent=replace`);
  };

  const closeRecovery = () => {
    load(page, pageSize, selectedChannelId);
    setRecoveryModel('');
    clearConnectionRecoveryParams();
    window.setTimeout(() => document.getElementById('connection-models')?.scrollIntoView?.({ behavior: 'smooth', block: 'start' }), 0);
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
    if (editing && editingIsActive && values.status === 'disabled') {
      try {
        await api.updateChannel(editing.id, {
          status: 'disabled',
          expected_routing_epoch: editing.routing_epoch,
        });
        Toast.success(`${editing.name} 已紧急禁用；现在可以安全新增或轮换供应商凭证`);
        closeDrawer();
        load(page, pageSize, editing.id);
      } catch (error) {
        if (error instanceof ApiError && error.status === 409) {
          Toast.error(channelConflictMessage(error));
        } else {
          Toast.error(channelUpdateErrorMessage(error));
        }
      }
      return;
    }
    let adapterConfig: Record<string, unknown>;
    try {
      adapterConfig = normalizeAdapterConfig(values.adapter_config) as Record<string, unknown>;
    } catch {
      Toast.error('Adapter Config 必须是合法 JSON');
      return;
    }
    const payload = {
      ...values,
      models: normalizeModelList(values.models),
      adapter_type: values.adapter_type || 'openai_compatible',
      adapter_config: adapterConfig,
      ...(editing ? { expected_routing_epoch: editing.routing_epoch } : {}),
    };
    try {
      if (editing) {
        await api.updateChannel(editing.id, payload);
        Toast.success('通道更新成功');
      } else {
		const created = await api.createChannel(payload);
		Toast.success('模型连接和供应商凭证已保存，请在本页完成真实检测并启用');
		closeDrawer();
		setPage(1);
		setPageSize(100);
		setSelectedChannelId(created.id);
		syncSelectedChannelID(created.id);
		void loadCredentials();
		load(1, 100, created.id);
		return;
      }
      closeDrawer();
      load();
    } catch (error) {
      if (editing && error instanceof ApiError && error.status === 409) {
			Toast.error(channelConflictMessage(error));
			if (channelConflictCode(error) === 'channel_bound_to_active_release') {
				const channelID = editing.id;
				closeDrawer();
				load(page, pageSize, channelID);
			}
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
    } catch (error) {
      setPreview(null);
		Toast.error(error instanceof ApiError ? error.message : '路由预览失败，请检查模型和通道状态');
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
    if (channelHasRetainedRelease(record)) {
      openConnectionRecovery(record);
      return;
    }
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
      status: effectiveChannelStatus(editing) === 'active' ? 'disabled' : editing.status,
    } : {
      adapter_type: 'openai_compatible',
      adapter_config: '{}',
    });
  }, [editing, formApi, showDrawer]);

  const columns = [
    { title: '名称', dataIndex: 'name', key: 'name', width: 180 },
    { title: '供应商', dataIndex: 'provider', key: 'provider', width: 100 },
    { title: '协议适配', dataIndex: 'adapter_type', key: 'adapter_type', width: 160, render: (v: string) => v || 'openai_compatible' },
    {
      title: '离线协议夹具',
      key: 'conformance',
      width: 110,
      render: (_: unknown, record: Channel) => {
        if (providerPackError) return <Tag color="orange">unavailable</Tag>;
        const match = findProviderPackMatch(providerPacks, record);
        if (!match) return <Tag color="orange">无协议夹具</Tag>;
        return <Tag color={match.scope === 'provider_fixture' ? 'green' : 'blue'}>
          {match.scope === 'provider_fixture' ? '协议夹具（同名）' : '协议夹具（参考）'}
        </Tag>;
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
      key: 'status',
      width: 100,
		render: (_: unknown, record: Channel) => {
			const status = effectiveChannelStatus(record);
			if (status === 'deprecated') return <Tooltip content="该通道已废弃，建议尽快迁移到新通道"><Tag color="yellow">deprecated ⚠</Tag></Tooltip>;
			const label = record.status === 'staged' && status === 'active'
				? '运行时已启用'
				: status === 'active' ? '已启用' : status === 'staged' ? '待检测/启用' : status === 'disabled' ? '禁用' : status;
			return record.status === 'staged' && status === 'active'
				? <Tooltip content={`运行时已启用：${record.published_models || '已发布模型'}；底层配置仍保持可回滚的 staged 快照`}><StatusTag status="active">{label}</StatusTag></Tooltip>
				: <StatusTag status={status}>{label}</StatusTag>;
		},
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
      width: 150,
      render: (_: any, record: Channel) => (
        <Space>
          {channelHasRetainedRelease(record)
            ? <Button aria-label={`恢复连接 ${record.name}`} icon={<IconRefresh />} size="small" type="primary" onClick={(event) => { event.stopPropagation(); openConnectionRecovery(record); }}>恢复</Button>
            : <Button aria-label={`编辑通道 ${record.name}`} icon={<IconEdit />} size="small" onClick={(event) => { event.stopPropagation(); openEdit(record); }} />}
          {!channelHasRetainedRelease(record) && <Button aria-label={`删除通道 ${record.name}`} icon={<IconDelete />} size="small" type="danger" onClick={(event) => { event.stopPropagation(); handleDelete(record.id); }} />}
        </Space>
      ),
    },
  ];

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="模型接入"
        title="模型连接"
        description="填写供应商地址、模型和 Key，在同一页面完成真实检测与启用。内部仍保留凭证加密、发布证据和回滚能力。"
        actions={<Button icon={<IconPlus />} type="primary" onClick={openCreate}>新增模型连接</Button>}
      />

      <Surface
        title="三步完成模型上线"
        description="调用诊断、协议证据和发布记录都不是新增模型的必填步骤。模型上线后，如尚无应用凭证，再为应用创建 API Key。"
      >
        <ol className="ur-plain-list">
		  <li><strong>连接：</strong>填写真实供应商、Base URL、模型 ID 和供应商 Key。</li>
		  <li><strong>检测：</strong>逐个检测，或一次检测当前连接配置的全部模型。</li>
		  <li><strong>启用：</strong>检测通过后启用该模型；以后替换已生效连接时才进入灰度。</li>
        </ol>
      </Surface>

      <div className="ur-provider-explorer">
        <main className="ur-explorer-panel">
          <div className="ur-panel-title">
            <div>
              <Typography.Title heading={5} style={{ margin: 0 }}>模型连接</Typography.Title>
              <Typography.Text className="ur-panel-hint">选择一条连接后，在下方逐个检测和启用模型</Typography.Text>
            </div>
            <Tag size="small">{total} 个连接</Tag>
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

        <details className="ur-explorer-panel ur-explorer-detail">
          <summary style={{ cursor: 'pointer', fontWeight: 600 }}>高级：查看协议与连接证据</summary>
        <aside aria-label="Provider 兼容性检查器" style={{ marginTop: 12 }}>
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
				<StatusTag status={effectiveChannelStatus(selectedChannel)}>{selectedChannel.status === 'staged' && effectiveChannelStatus(selectedChannel) === 'active'
					? '运行时已启用（底层待发布）'
					: effectiveChannelStatus(selectedChannel) === 'active' ? '已启用' : effectiveChannelStatus(selectedChannel) === 'disabled' ? '禁用' : '待检测/启用'}</StatusTag>
              </div>

              <div className="ur-receipt-summary" aria-label="Provider Pack 摘要">
                <div>
                  <Typography.Text type="tertiary" size="small">Adapter</Typography.Text>
                  <Typography.Text strong size="small">{selectedChannel.adapter_type || 'openai_compatible'}</Typography.Text>
                </div>
                <div>
                  <Typography.Text type="tertiary" size="small">Conformance</Typography.Text>
                  <StatusTag status={providerPackError || !selectedPack ? 'warning' : 'success'}>
                    {providerPackError
                      ? 'unavailable'
                      : selectedPackMatch?.scope === 'provider_fixture'
                        ? '协议夹具（同名）'
                        : selectedPackMatch?.scope === 'adapter_protocol_fixture'
                          ? '协议夹具（参考）'
                          : '无协议夹具'}
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
                  <Typography.Text className="ur-detail-card-title">缺少离线协议夹具</Typography.Text>
                  <Typography.Text type="tertiary" size="small">
                    当前 Adapter 没有可复核的离线协议夹具；不会把“可配置”解释为“已兼容”。
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
                        { key: '实际供应商身份', value: selectedChannel.provider },
                        { key: '夹具参考供应商', value: selectedPack.manifest.provider },
                        { key: '证据范围', value: selectedPackMatch?.scope === 'provider_fixture' ? 'Pack provider 与供应商同名；仍仅为离线合成夹具' : '仅复用相同 Adapter 协议的参考夹具' },
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
                      鉴权线缆：{selectedPack.manifest.auth_profiles.map((profile) => `${profile.type} → ${profile.wire_header}`).join('；')}。这里只验证 UniRouter Adapter 对参考协议的离线请求/响应，不证明 {selectedChannel.provider} 的真实接口兼容。
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
                  敏感配置值不在检查器中回显；离线夹具不代表真实账号、网络、额度、模型权限或账单，真实状态以上游检测为准。
                </Typography.Text>
              </div>

			  <Typography.Text type="tertiary" size="small">
				真实检测就在本区域下方的“{selectedChannel.name} · 模型上线”中直接完成，无需再打开页面。
			  </Typography.Text>
			  <div>{channelHasRetainedRelease(selectedChannel)
				? <Button icon={<IconRefresh />} type="primary" onClick={() => openConnectionRecovery(selectedChannel)}>恢复连接</Button>
				: <Button icon={<IconEdit />} onClick={() => openEdit(selectedChannel)}>编辑当前连接</Button>}</div>
            </div>
          )}
        </aside>
        </details>
      </div>

      {selectedChannel && <div id="connection-models">
        <ConnectionModelPanel
          key={`${selectedChannel.id}:${selectedChannel.routing_epoch}:${selectedChannel.models}`}
          channel={selectedChannel}
          credentials={channelKeys}
          credentialLoadState={credentialLoadState}
          credentialLoadError={credentialLoadError}
          modelsPublishedElsewhere={modelsPublishedElsewhere}
          onModelEnabled={(model) => setChannels(current => current.map(item => item.id === selectedChannel.id
            ? {
                ...item,
                effective_status: 'active',
                published_models: normalizeModelList(`${item.published_models || ''},${model}`),
              }
            : item))}
          onManageCredentials={() => navigateWithinApp(connectionAdvancedPath('/channel-keys', selectedChannel.id))}
          onOpenRouteManagement={openRouteManagement}
          onDiagnose={(model) => navigateWithinApp(connectionAdvancedPath('/model-readiness', selectedChannel.id, model))}
          gatewayLicensed={gatewayLicensed}
          gatewayRestrictionMessage={gatewayRestriction}
          onOpenLicense={() => navigateWithinApp('/license')}
        />
      </div>}

      {selectedChannel && recoveryModel && <div id="connection-recovery">
        <RuntimeChangeControlWorkspace recoveryOnly onBack={closeRecovery} />
      </div>}

      <details>
        <summary style={{ cursor: 'pointer', fontWeight: 600, marginBottom: 12 }}>高级：预览当前路由</summary>
        <Surface
          title="路由预览"
          description="只读解释指定模型当前会选择哪些连接以及排除原因。它不会调用上游，也不会产生费用。"
        >
        <div className="ur-inline-controls">
          <div role="group" aria-label="选择精确模型 ID">
            <Select
              value={previewModel}
              onChange={(value) => {
                setPreviewModel(String(value || ''));
                setPreview(null);
              }}
              filter
              optionList={routingModelOptions}
              placeholder="选择精确模型 ID"
              style={{ width: 360 }}
            />
          </div>
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
				{preview.candidates.length === 0 && preview.exclusions.length === 0
					? <p>没有连接声明这个精确模型 ID；请刷新连接列表。</p>
					: preview.candidates.length === 0 && <p>没有可用通道</p>}
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
				{preview.exclusions.length === 0 && preview.candidates.length > 0 && <p>没有被排除的通道</p>}
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
      </details>

      <ChannelEditorSheet
        visible={showDrawer}
        editing={editing}
        editingIsActive={editingIsActive}
        formApi={formApi}
        setFormApi={setFormApi}
        onSubmit={handleSubmit}
        onCancel={closeDrawer}
      />
    </div>
  );
}
