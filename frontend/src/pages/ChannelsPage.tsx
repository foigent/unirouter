import { useState, useEffect } from 'react';
import { Button, Table, Modal, Form, Tag, Toast, Space, Tooltip, SideSheet } from '@douyinfe/semi-ui';
import { IconPlus, IconDelete, IconEdit } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { PageHeader, StatusTag, Surface } from '../ui';

interface Channel {
  id: number;
  name: string;
  provider: string;
  base_url: string;
  models: string;
  adapter_type?: string;
  adapter_config?: Record<string, unknown> | string;
  priority: number;
  degradation_threshold: number;
  status: string;
  circuit_state: string;
  fail_count: number;
  created_at: string;
}

export default function ChannelsPage() {
  const [channels, setChannels] = useState<Channel[]>([]);
  const [loading, setLoading] = useState(true);
  const [showDrawer, setShowDrawer] = useState(false);
  const [editing, setEditing] = useState<Channel | null>(null);
  const [formApi, setFormApi] = useState<any>(null);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [total, setTotal] = useState(0);

  const load = (p = page) => {
    setLoading(true);
    api.getChannels(p)
      .then((data) => {
        setChannels(data.data || []);
        setTotal(data.total || 0);
      })
      .catch(console.error)
      .finally(() => setLoading(false));
  };

  useEffect(() => { load(1); }, []);

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
    };
    try {
      if (editing) {
        await api.updateChannel(editing.id, payload);
        Toast.success('渠道更新成功');
      } else {
        await api.createChannel(payload);
        Toast.success('渠道创建成功');
      }
      closeDrawer();
      load();
    } catch {
      Toast.error(editing ? '更新失败' : '创建失败');
    }
  };

  const handleDelete = async (id: number) => {
    Modal.confirm({
      title: '确认删除',
      content: '删除后不可恢复，确定要删除这个渠道吗？',
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
    setTimeout(() => {
      formApi?.setValues({
        name: record.name,
        provider: record.provider,
        base_url: record.base_url,
        models: record.models,
        adapter_type: record.adapter_type || 'openai_compatible',
        adapter_config: formatAdapterConfig(record.adapter_config),
        priority: record.priority,
      });
    }, 100);
  };

  const openCreate = () => {
    setEditing(null);
    setShowDrawer(true);
    formApi?.reset();
    setTimeout(() => formApi?.setValues({ adapter_type: 'openai_compatible', adapter_config: '{}' }), 0);
  };

  const columns = [
    { title: 'ID', dataIndex: 'id', key: 'id', width: 60 },
    { title: '名称', dataIndex: 'name', key: 'name' },
    { title: 'Provider', dataIndex: 'provider', key: 'provider' },
    { title: 'Adapter', dataIndex: 'adapter_type', key: 'adapter_type', render: (v: string) => v || 'openai_compatible' },
    { title: 'Base URL', dataIndex: 'base_url', key: 'base_url', ellipsis: true },
    { title: '支持模型', dataIndex: 'models', key: 'models', ellipsis: true },
    { title: '优先级', dataIndex: 'priority', key: 'priority', width: 80 },
    { title: '降级阈值', dataIndex: 'degradation_threshold', key: 'degradation_threshold', width: 90 },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 80,
      render: (v: string) => v === 'deprecated'
        ? <Tooltip content="该渠道已废弃，建议尽快迁移到新渠道"><Tag color="yellow">deprecated ⚠</Tag></Tooltip>
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
          <Button icon={<IconEdit />} size="small" onClick={() => openEdit(record)} />
          <Button icon={<IconDelete />} size="small" type="danger" onClick={() => handleDelete(record.id)} />
        </Space>
      ),
    },
  ];

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="Traffic"
        title="上游通道"
        description="配置上游 provider、模型能力、优先级和熔断状态。"
        actions={<Button icon={<IconPlus />} type="primary" onClick={openCreate}>新增渠道</Button>}
      />

      <Surface>
        <div className="ur-table-shell">
          <Table
            dataSource={channels}
            columns={columns}
            rowKey="id"
            loading={loading}
            pagination={{
              currentPage: page,
              pageSize,
              total,
              pageSizeOpts: [20, 50, 100],
              showSizeChanger: true,
              onPageChange: (p: number) => { setPage(p); load(p); },
              onPageSizeChange: (s: number) => { setPageSize(s); setPage(1); load(1); },
            }}
          />
        </div>
      </Surface>

      <SideSheet
        title={editing ? '编辑渠道' : '新增渠道'}
        visible={showDrawer}
        onCancel={closeDrawer}
        width={480}
      >
        <Form getFormApi={(api: any) => setFormApi(api)} onSubmit={handleSubmit}>
          <Form.Input field="name" label="渠道名称" rules={[{ required: true, message: '必填' }]} />
          <Form.Select field="provider" label="Provider" rules={[{ required: true, message: '必填' }]}
            optionList={[
              { label: 'OpenAI', value: 'openai' },
              { label: 'Anthropic', value: 'anthropic' },
              { label: 'Azure', value: 'azure' },
              { label: '自定义', value: 'custom' },
            ]}
          />
          <Form.Input field="base_url" label="Base URL" rules={[{ required: true, message: '必填' }]} />
          <Form.Input field="models" label="支持模型" placeholder="多个模型用英文逗号分隔，如 gpt-4o, gpt-4o-mini" rules={[{ required: true, message: '必填' }]} />
          <Form.Select field="adapter_type" label="Adapter Type" initValue="openai_compatible"
            optionList={[
              { label: 'OpenAI Compatible', value: 'openai_compatible' },
              { label: 'Anthropic Native', value: 'anthropic_native' },
            ]}
          />
          <Form.TextArea field="adapter_config" label="Adapter Config (JSON)" placeholder='{"anthropic_version":"2023-06-01"}' rows={4} />
          <Form.InputNumber field="priority" label="优先级 (越小越优先)" initValue={100} />
          <Form.InputNumber field="degradation_threshold" label="降级阈值 (连续失败次数)" initValue={10} />
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 24 }}>
            <Button onClick={closeDrawer}>取消</Button>
            <Button type="primary" onClick={() => formApi?.submitForm()}>{editing ? '更新' : '创建'}</Button>
          </div>
        </Form>
      </SideSheet>
    </div>
  );
}
