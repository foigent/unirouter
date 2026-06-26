import { useState, useEffect } from 'react';
import { Typography, Button, Table, Modal, Form, Tag, Toast, Space } from '@douyinfe/semi-ui';
import { IconPlus, IconDelete, IconEdit } from '@douyinfe/semi-icons';
import { api } from '../api/client';

const { Title } = Typography;

interface ApiKey {
  id: number;
  name: string;
  key_prefix: string;
  allowed_ips: string;
  allowed_models: string;
  status: string;
  created_at: string;
}

export default function KeysPage() {
  const [keys, setKeys] = useState<ApiKey[]>([]);
  const [loading, setLoading] = useState(true);
  const [showModal, setShowModal] = useState(false);
  const [editing, setEditing] = useState<ApiKey | null>(null);
  const [formApi, setFormApi] = useState<any>(null);
  const [page, setPage] = useState(1);
  const [total, setTotal] = useState(0);

  const load = (p = page) => {
    setLoading(true);
    api.getKeys(p)
      .then((data) => {
        setKeys(data.data || []);
        setTotal(data.total || 0);
      })
      .catch(console.error)
      .finally(() => setLoading(false));
  };

  useEffect(() => { load(1); }, []);

  const handleSubmit = async (values: Record<string, any>) => {
    try {
      if (editing) {
        await api.updateKey(editing.id, values);
        Toast.success('密钥更新成功');
      } else {
        await api.createKey(values);
        Toast.success('密钥创建成功');
      }
      setShowModal(false);
      setEditing(null);
      load();
    } catch {
      Toast.error(editing ? '更新失败' : '创建失败');
    }
  };

  const handleDisable = async (id: number) => {
    Modal.confirm({
      title: '确认禁用',
      content: '禁用后使用该密钥的请求将被拒绝，确定要禁用吗？',
      onOk: async () => {
        try {
          await api.disableKey(id);
          Toast.success('已禁用');
          load();
        } catch {
          Toast.error('禁用失败');
        }
      },
    });
  };

  const openEdit = (record: ApiKey) => {
    setEditing(record);
    setShowModal(true);
    setTimeout(() => {
      formApi?.setValues({
        name: record.name,
        allowed_ips: record.allowed_ips,
        allowed_models: record.allowed_models,
      });
    }, 100);
  };

  const openCreate = () => {
    setEditing(null);
    setShowModal(true);
    formApi?.reset();
  };

  const columns = [
    { title: 'ID', dataIndex: 'id', key: 'id', width: 60 },
    { title: '名称', dataIndex: 'name', key: 'name' },
    { title: 'Key', dataIndex: 'key_prefix', key: 'prefix', render: (v: string) => <code>{v}...</code> },
    { title: 'IP白名单', dataIndex: 'allowed_ips', key: 'ips', ellipsis: true, render: (v: string) => v || '不限' },
    { title: '模型限制', dataIndex: 'allowed_models', key: 'models', ellipsis: true, render: (v: string) => v || '全部' },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 80,
      render: (v: string) => <Tag color={v === 'active' ? 'green' : 'grey'}>{v === 'active' ? '启用' : '禁用'}</Tag>,
    },
    {
      title: '操作',
      key: 'actions',
      width: 120,
      render: (_: any, record: ApiKey) => (
        <Space>
          <Button icon={<IconEdit />} size="small" onClick={() => openEdit(record)} />
          {record.status === 'active' && (
            <Button icon={<IconDelete />} size="small" type="danger" onClick={() => handleDisable(record.id)} />
          )}
        </Space>
      ),
    },
  ];

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <Title heading={4} style={{ margin: 0 }}>密钥管理</Title>
        <Button icon={<IconPlus />} type="primary" onClick={openCreate}>创建密钥</Button>
      </div>

      <Table
        dataSource={keys}
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
        title={editing ? '编辑密钥' : '创建密钥'}
        visible={showModal}
        onCancel={() => { setShowModal(false); setEditing(null); }}
        onOk={() => formApi?.submitForm()}
        okText={editing ? '更新' : '创建'}
      >
        <Form getFormApi={(api) => setFormApi(api)} onSubmit={handleSubmit}>
          <Form.Input field="name" label="密钥名称" rules={[{ required: true, message: '必填' }]} />
          {!editing && <Form.Input field="org_id" label="组织ID" initValue={1} />}
          <Form.Input field="allowed_ips" label="IP白名单 (逗号分隔，留空不限)" />
          <Form.Input field="allowed_models" label="模型限制 (逗号分隔，留空全部)" />
          {editing && (
            <Form.Select field="status" label="状态" optionList={[
              { label: '启用', value: 'active' },
              { label: '禁用', value: 'disabled' },
            ]} />
          )}
        </Form>
      </Modal>
    </div>
  );
}
