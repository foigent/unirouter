import { useState, useEffect } from 'react';
import { Typography, Button, Table, Modal, Form, Tag, Toast, Space, Tooltip } from '@douyinfe/semi-ui';
import { IconPlus, IconDelete, IconEdit } from '@douyinfe/semi-icons';
import { api } from '../api/client';

const { Title } = Typography;

interface Channel {
  id: number;
  name: string;
  provider: string;
  base_url: string;
  models: string;
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
  const [showModal, setShowModal] = useState(false);
  const [editing, setEditing] = useState<Channel | null>(null);
  const [formApi, setFormApi] = useState<any>(null);
  const [page, setPage] = useState(1);
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

  const handleSubmit = async (values: Record<string, any>) => {
    try {
      if (editing) {
        await api.updateChannel(editing.id, values);
        Toast.success('渠道更新成功');
      } else {
        await api.createChannel(values);
        Toast.success('渠道创建成功');
      }
      setShowModal(false);
      setEditing(null);
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
    setShowModal(true);
    setTimeout(() => {
      formApi?.setValues({
        name: record.name,
        provider: record.provider,
        base_url: record.base_url,
        models: record.models,
        priority: record.priority,
      });
    }, 100);
  };

  const openCreate = () => {
    setEditing(null);
    setShowModal(true);
    formApi?.reset();
  };

  const statusColor = (s: string) => {
    if (s === 'active') return 'green';
    if (s === 'deprecated') return 'yellow';
    if (s === 'disabled') return 'grey';
    return 'red';
  };

  const circuitColor = (s: string) => {
    if (s === 'healthy') return 'green';
    if (s === 'half_open') return 'orange';
    return 'red';
  };

  const columns = [
    { title: 'ID', dataIndex: 'id', key: 'id', width: 60 },
    { title: '名称', dataIndex: 'name', key: 'name' },
    { title: 'Provider', dataIndex: 'provider', key: 'provider' },
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
        : <Tag color={statusColor(v) as any}>{v}</Tag>,
    },
    {
      title: '熔断',
      dataIndex: 'circuit_state',
      key: 'circuit',
      width: 100,
      render: (v: string) => <Tag color={circuitColor(v) as any}>{v}</Tag>,
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
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <Title heading={4} style={{ margin: 0 }}>渠道管理</Title>
        <Button icon={<IconPlus />} type="primary" onClick={openCreate}>新增渠道</Button>
      </div>

      <Table
        dataSource={channels}
        columns={columns}
        rowKey="id"
        loading={loading}
        pagination={{
          currentPage: page,
          pageSize: 20,
          total,
          onPageChange: (p) => { setPage(p); load(p); },
        }}
      />

      <Modal
        title={editing ? '编辑渠道' : '新增渠道'}
        visible={showModal}
        onCancel={() => { setShowModal(false); setEditing(null); }}
        onOk={() => formApi?.submitForm()}
        okText={editing ? '更新' : '创建'}
      >
        <Form getFormApi={(api) => setFormApi(api)} onSubmit={handleSubmit}>
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
          <Form.Input field="models" label="支持模型 (逗号分隔)" rules={[{ required: true, message: '必填' }]} />
          <Form.InputNumber field="priority" label="优先级 (越小越优先)" initValue={100} />
          <Form.InputNumber field="degradation_threshold" label="降级阈值 (连续失败次数)" initValue={10} />
        </Form>
      </Modal>
    </div>
  );
}
