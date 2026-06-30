import { useState, useEffect } from 'react';
import { Typography, Button, Table, Modal, Form, Tag, Toast, Space, SideSheet } from '@douyinfe/semi-ui';
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
  const [showDrawer, setShowDrawer] = useState(false);
  const [editing, setEditing] = useState<ApiKey | null>(null);
  const [formApi, setFormApi] = useState<any>(null);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [total, setTotal] = useState(0);
  const [availableModels, setAvailableModels] = useState<string[]>([]);

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
  useEffect(() => {
    api.getModelNames().then((d: any) => {
      const source = Array.isArray(d?.data) ? d.data : d;
      const models = (Array.isArray(source) ? source : [])
        .map((m: any) => typeof m === 'string' ? m : m.model)
        .filter(Boolean);
      setAvailableModels(models);
    }).catch(() => {});
  }, []);

  const closeDrawer = () => { setShowDrawer(false); setEditing(null); };

  const handleSubmit = async (values: Record<string, any>) => {
    // Convert allowed_models array to comma-separated string
    if (Array.isArray(values.allowed_models)) {
      values.allowed_models = values.allowed_models.join(',');
    }
    try {
      if (editing) {
        await api.updateKey(editing.id, values);
        Toast.success('密钥更新成功');
      } else {
        await api.createKey(values);
        Toast.success('密钥创建成功');
      }
      closeDrawer();
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
    setShowDrawer(true);
    setTimeout(() => {
      formApi?.setValues({
        name: record.name,
        allowed_ips: record.allowed_ips,
        allowed_models: record.allowed_models ? record.allowed_models.split(',').map(m => m.trim()).filter(Boolean) : [],
      });
    }, 100);
  };

  const openCreate = () => {
    setEditing(null);
    setShowDrawer(true);
    formApi?.reset();
  };

  const columns = [
    { title: 'ID', dataIndex: 'id', key: 'id', width: 60 },
    { title: '名称', dataIndex: 'name', key: 'name' },
    { title: 'Key 前缀', dataIndex: 'key_prefix', key: 'prefix', render: (v: string) => <code>{v}***</code> },
    { title: '允许 IP', dataIndex: 'allowed_ips', key: 'ips', render: (v: string) => v || '全部' },
    { title: '允许模型', dataIndex: 'allowed_models', key: 'models', render: (v: string) => v || '全部' },
    {
      title: '状态', dataIndex: 'status', key: 'status', width: 80,
      render: (v: string) => <Tag color={v === 'active' ? 'green' : 'red' as any}>{v}</Tag>,
    },
    {
      title: '操作', key: 'actions', width: 140,
      render: (_: any, record: ApiKey) => (
        <Space>
          <Button icon={<IconEdit />} size="small" onClick={() => openEdit(record)} />
          <Button icon={<IconDelete />} size="small" type="danger" onClick={() => handleDisable(record.id)} />
        </Space>
      ),
    },
  ];

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <Title heading={4} style={{ margin: 0 }}>API 密钥</Title>
        <Button icon={<IconPlus />} type="primary" onClick={openCreate}>新增密钥</Button>
      </div>

      <Table
        dataSource={keys}
        columns={columns}
        rowKey="id"
        loading={loading}
        pagination={{
          currentPage: page, pageSize, total,
          pageSizeOpts: [20, 50, 100], showSizeChanger: true,
          onPageChange: (p: number) => { setPage(p); load(p); },
          onPageSizeChange: (s: number) => { setPageSize(s); setPage(1); load(1); },
        }}
      />

      <SideSheet
        title={editing ? '编辑密钥' : '新增密钥'}
        visible={showDrawer}
        onCancel={closeDrawer}
        width={480}
      >
        <Form getFormApi={(api: any) => setFormApi(api)} onSubmit={handleSubmit}>
          <Form.Input field="name" label="密钥名称" rules={[{ required: true, message: '必填' }]} />
          <Form.Input field="allowed_ips" label="允许 IP (逗号分隔，留空=全部)" placeholder="127.0.0.1,10.0.0.0/8" />
          <Form.Select
            field="allowed_models"
            label="允许模型 (留空=全部)"
            placeholder="选择允许的模型"
            multiple
            optionList={availableModels.map(m => ({ label: m, value: m }))}
            style={{ width: '100%' }}
          />
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 24 }}>
            <Button onClick={closeDrawer}>取消</Button>
            <Button type="primary" onClick={() => formApi?.submitForm()}>{editing ? '更新' : '创建'}</Button>
          </div>
        </Form>
      </SideSheet>
    </div>
  );
}
