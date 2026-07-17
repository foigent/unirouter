import { useEffect, useRef, useState } from 'react';
import { Button, Table, Modal, Form, Tag, Toast, Space, Tooltip, SideSheet, Input, Typography } from '@douyinfe/semi-ui';
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

  const load = (p = page, size = pageSize) => {
    const version = ++requestVersion.current;
    setLoading(true);
    setLoadError('');
    setChannels([]);
    setTotal(0);
    api.getChannels(p, size)
      .then((data) => {
        if (version !== requestVersion.current) return;
        setChannels(data.data || []);
        setTotal(data.total || 0);
      })
      .catch(() => {
        if (version === requestVersion.current) setLoadError('无法加载上游通道列表。');
      })
      .finally(() => {
        if (version === requestVersion.current) setLoading(false);
      });
  };

  useEffect(() => { load(1, pageSize); }, []);

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
        Toast.success('通道创建成功');
      }
      closeDrawer();
      load();
    } catch (error) {
      if (editing && error instanceof ApiError && error.status === 409) {
        Toast.error('该通道已被其他管理员修改，请关闭编辑页、刷新后重试');
      } else {
        Toast.error(editing ? '更新失败' : '创建失败');
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
    } : {
      adapter_type: 'openai_compatible',
      adapter_config: '{}',
    });
  }, [editing, formApi, showDrawer]);

  const columns = [
    { title: 'ID', dataIndex: 'id', key: 'id', width: 60 },
    { title: '名称', dataIndex: 'name', key: 'name' },
    { title: '供应商', dataIndex: 'provider', key: 'provider' },
    { title: '协议适配', dataIndex: 'adapter_type', key: 'adapter_type', render: (v: string) => v || 'openai_compatible' },
    { title: '服务地址', dataIndex: 'base_url', key: 'base_url', ellipsis: true },
    { title: '支持模型', dataIndex: 'models', key: 'models', ellipsis: true },
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
        : <StatusTag status={v}>{v}</StatusTag>,
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
          <Button aria-label={`编辑通道 ${record.name}`} icon={<IconEdit />} size="small" onClick={() => openEdit(record)} />
          <Button aria-label={`删除通道 ${record.name}`} icon={<IconDelete />} size="small" type="danger" onClick={() => handleDelete(record.id)} />
        </Space>
      ),
    },
  ];

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="模型接入"
        title="上游通道"
        description="优先级决定先用哪一组通道；同级权重决定组内流量比例；熔断阈值决定连续失败多少次后暂停通道。"
        actions={<Button icon={<IconPlus />} type="primary" onClick={openCreate}>新增通道</Button>}
      />

      <Surface title="通道列表" description="这里是通道配置的权威入口。先确认通道、凭证和状态，再使用下方路由预览解释候选顺序。">
        <div className="ur-table-shell">
          <Table
            dataSource={channels}
            columns={columns}
            rowKey="id"
            loading={loading}
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
      </Surface>

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
              { label: '自定义', value: 'custom' },
            ]}
          />
          <Form.Input field="base_url" label="服务地址（Base URL）" rules={[{ required: true, message: '必填' }]} />
          <Form.Input field="models" label="支持模型" placeholder="多个模型用英文逗号分隔，如 gpt-4o, gpt-4o-mini" rules={[{ required: true, message: '必填' }]} />
          <Form.Select field="adapter_type" label="Adapter Type" initValue="openai_compatible"
            optionList={[
              { label: 'OpenAI Compatible', value: 'openai_compatible' },
              { label: 'Anthropic Native', value: 'anthropic_native' },
            ]}
          />
          <Form.TextArea field="adapter_config" label="Adapter Config (JSON)" placeholder='{"anthropic_version":"2023-06-01"}' rows={4} />
          <Form.InputNumber field="priority" label="优先级 (越小越优先)" initValue={100} />
          <Form.InputNumber field="routing_weight" label="同优先级权重 (1-1000)" initValue={100} extraText="只在优先级相同时分配流量；例如 100 与 200 约为 1:2。" />
          <Form.InputNumber field="degradation_threshold" label="熔断阈值 (连续失败次数)" initValue={10} extraText="达到次数后暂停该通道，恢复窗口到期后只允许一个探针请求。" />
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 24 }}>
            <Button onClick={closeDrawer}>取消</Button>
            <Button type="primary" onClick={() => formApi?.submitForm()}>{editing ? '更新' : '创建'}</Button>
          </div>
        </Form>
      </SideSheet>
    </div>
  );
}
