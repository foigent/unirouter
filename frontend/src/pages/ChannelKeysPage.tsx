import { formatDateTime } from '../utils/formatDate';
import { useState, useEffect } from 'react';
import { Typography, Button, Table, Modal, Form, Tag, Toast } from '@douyinfe/semi-ui';
import { IconPlus, IconDelete, IconEdit } from '@douyinfe/semi-icons';
import { api } from '../api/client';

const { Title } = Typography;

interface ChannelKey {
  id: number;
  channel_id: number;
  channel_name: string;
  key_masked: string;
  allowed_models: string;
  status: string;
  fail_count: number;
  last_used_at: string | null;
  cooldown_until: string | null;
  created_at: string;
}

interface Channel {
  id: number;
  name: string;
}

export default function ChannelKeysPage() {
  const [keys, setKeys] = useState<ChannelKey[]>([]);
  const [channels, setChannels] = useState<Channel[]>([]);
  const [loading, setLoading] = useState(true);
  const [showModal, setShowModal] = useState(false);
  const [editKey, setEditKey] = useState<ChannelKey | null>(null);
  const [formApi, setFormApi] = useState<any>(null);

  const loadKeys = () => {
    setLoading(true);
    api.getChannelKeys()
      .then((data: any) => setKeys(data.data || []))
      .catch(console.error)
      .finally(() => setLoading(false));
  };

  const loadChannels = () => {
    api.getChannels(1, 100)
      .then((data: any) => setChannels(data.data || []))
      .catch(console.error);
  };

  useEffect(() => { loadKeys(); loadChannels(); }, []);

  const handleCreate = async (values: Record<string, any>) => {
    try {
      await api.createChannelKey(values);
      Toast.success('上游密钥创建成功');
      setShowModal(false);
      formApi?.reset();
      loadKeys();
    } catch {
      Toast.error('创建失败');
    }
  };

  const handleUpdate = async (values: Record<string, any>) => {
    if (!editKey) return;
    try {
      await api.updateChannelKey(editKey.id, {
        channel_id: values.channel_id,
        status: values.status,
        allowed_models: values.allowed_models || '',
      });
      Toast.success('更新成功');
      setEditKey(null);
      loadKeys();
    } catch {
      Toast.error('更新失败');
    }
  };

  const handleDelete = (record: ChannelKey) => {
    Modal.confirm({
      title: '确认删除',
      content: `确定要删除该上游密钥吗？`,
      onOk: async () => {
        try {
          await api.deleteChannelKey(record.id);
          Toast.success('删除成功');
          loadKeys();
        } catch {
          Toast.error('删除失败');
        }
      },
    });
  };

  const columns = [
    { title: 'ID', dataIndex: 'id', width: 60 },
    { title: '渠道', dataIndex: 'channel_name' },
    { title: '允许模型', dataIndex: 'allowed_models', render: (v: string) => v || '全部' },
    {
      title: '密钥',
      dataIndex: 'key_masked',
      render: (v: string) => <span style={{ fontFamily: 'monospace' }}>{v}</span>,
    },
    {
      title: '状态',
      dataIndex: 'status',
      render: (v: string) => (
        <Tag color={v === 'active' ? 'green' : v === 'cooldown' ? 'orange' : 'red'}>{v}</Tag>
      ),
    },
    { title: '失败次数', dataIndex: 'fail_count' },
    {
      title: '最后使用',
      dataIndex: 'last_used_at',
      render: (v: string | null) => (v ? formatDateTime(v) : '-'),
    },
    {
      title: '创建时间',
      dataIndex: 'created_at',
      render: (v: string) => formatDateTime(v),
    },
    {
      title: '操作',
      render: (_: any, record: ChannelKey) => (
        <div style={{ display: 'flex', gap: 8 }}>
          <Button
            size="small"
            icon={<IconEdit />}
            onClick={() => setEditKey(record)}
          >
            编辑
          </Button>
          <Button
            size="small"
            theme="solid"
            type="danger"
            icon={<IconDelete />}
            onClick={() => handleDelete(record)}
          >
            删除
          </Button>
        </div>
      ),
    },
  ];

  const channelOptions = channels.map((c) => ({
    label: c.name,
    value: c.id,
  }));

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <Title heading={4} style={{ margin: 0 }}>上游密钥管理</Title>
        <Button icon={<IconPlus />} theme="solid" onClick={() => setShowModal(true)}>
          新增密钥
        </Button>
      </div>
      <Table
        columns={columns}
        dataSource={keys}
        loading={loading}
        rowKey="id"
        pagination={false}
      />

      {/* Create Modal */}
      <Modal
        title="新增上游密钥"
        visible={showModal}
        onCancel={() => { setShowModal(false); formApi?.reset(); }}
        onOk={() => formApi?.submitForm()}
        okText="创建"
      >
        <Form getFormApi={(api) => setFormApi(api)} onSubmit={handleCreate}>
          <Form.Select
            field="channel_id"
            label="所属渠道"
            placeholder="选择渠道"
            optionList={channelOptions}
            rules={[{ required: true, message: '请选择渠道' }]}
          />
          <Form.Input
            field="key"
            label="API Key"
            placeholder="输入上游 API Key"
            rules={[{ required: true, message: '请输入 Key' }]}
          />
          <Form.Input field="allowed_models" label="允许模型 (逗号分隔，留空=全部)" placeholder="gpt-4o,gpt-4o-mini" />
        </Form>
      </Modal>

      {/* Edit Modal */}
      <Modal
        title="编辑上游密钥"
        visible={!!editKey}
        onCancel={() => setEditKey(null)}
        onOk={() => {
          const form = document.querySelector('.semi-modal-content form') as HTMLFormElement;
          if (form) form.requestSubmit();
        }}
        okText="保存"
      >
        <Form
          initValues={editKey ? { channel_id: editKey.channel_id, status: editKey.status, allowed_models: editKey.allowed_models || '' } : {}}
          onSubmit={handleUpdate}
        >
          <Form.Select
            field="channel_id"
            label="所属渠道"
            optionList={channelOptions}
            rules={[{ required: true, message: '请选择渠道' }]}
          />
          <Form.Select
            field="status"
            label="状态"
            optionList={[
              { label: 'active', value: 'active' },
              { label: 'disabled', value: 'disabled' },
              { label: 'cooldown', value: 'cooldown' },
            ]}
            rules={[{ required: true, message: '请选择状态' }]}
          />
        </Form>
      </Modal>
    </div>
  );
}
